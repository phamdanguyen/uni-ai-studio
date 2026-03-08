// Package natsbus implements agent.MessageBus using NATS JetStream.
package natsbus

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/uni-ai-studio/waoo-studio/internal/agent"
	"github.com/uni-ai-studio/waoo-studio/internal/config"
)

// Bus implements agent.MessageBus backed by NATS.
type Bus struct {
	conn   *nats.Conn
	subs   []*nats.Subscription
	logger *slog.Logger
}

// New creates a new NATS message bus.
func New(cfg config.NATSConfig, logger *slog.Logger) (*Bus, error) {
	opts := []nats.Option{
		nats.Name("waoo-studio"),
		nats.MaxReconnects(cfg.MaxReconnects),
		nats.ReconnectWait(cfg.ReconnectWait),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			logger.Warn("NATS disconnected", "error", err)
		}),
		nats.ReconnectHandler(func(nc *nats.Conn) {
			logger.Info("NATS reconnected", "url", nc.ConnectedUrl())
		}),
	}

	nc, err := nats.Connect(cfg.URL, opts...)
	if err != nil {
		return nil, fmt.Errorf("connect to NATS at %s: %w", cfg.URL, err)
	}

	logger.Info("NATS connected", "url", nc.ConnectedUrl())

	return &Bus{
		conn:   nc,
		logger: logger.With("component", "nats-bus"),
	}, nil
}

// Publish sends a fire-and-forget message to the target agent's subject.
func (b *Bus) Publish(_ context.Context, msg agent.Message) error {
	subject := fmt.Sprintf("agent.%s.%s", msg.To, msg.SkillID)

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	b.logger.Debug("publishing message",
		"from", msg.From,
		"to", msg.To,
		"skill", msg.SkillID,
		"subject", subject,
	)

	return b.conn.Publish(subject, data)
}

// Request sends a message and waits for a reply.
func (b *Bus) Request(_ context.Context, msg agent.Message, timeout time.Duration) (*agent.TaskResult, error) {
	subject := fmt.Sprintf("agent.%s.%s", msg.To, msg.SkillID)

	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal message: %w", err)
	}

	b.logger.Debug("requesting",
		"from", msg.From,
		"to", msg.To,
		"skill", msg.SkillID,
		"timeout", timeout,
	)

	resp, err := b.conn.Request(subject, data, timeout)
	if err != nil {
		return nil, fmt.Errorf("request to %s: %w", subject, err)
	}

	var result agent.TaskResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &result, nil
}

// Subscribe registers a handler for messages to a specific agent.
// Uses NATS Queue Groups so multiple instances of the same agent
// share the load (only one receives each message).
func (b *Bus) Subscribe(agentName string, handler agent.MessageHandler) error {
	// Subscribe to all skills: agent.{name}.>
	subject := fmt.Sprintf("agent.%s.>", agentName)
	queueGroup := fmt.Sprintf("agent-%s-workers", agentName)

	sub, err := b.conn.QueueSubscribe(subject, queueGroup, func(natsMsg *nats.Msg) {
		var msg agent.Message
		if err := json.Unmarshal(natsMsg.Data, &msg); err != nil {
			b.logger.Error("unmarshal incoming message", "error", err, "subject", natsMsg.Subject)
			return
		}

		ctx := context.Background()
		result, err := handler(ctx, msg)
		if err != nil {
			b.logger.Error("handler error",
				"agent", agentName,
				"skill", msg.SkillID,
				"error", err,
			)
			result = &agent.TaskResult{
				Status: agent.TaskStatusFailed,
				Error:  err.Error(),
			}
		}

		// If this was a request (has reply subject), respond
		if natsMsg.Reply != "" {
			respData, _ := json.Marshal(result)
			if err := natsMsg.Respond(respData); err != nil {
				b.logger.Error("respond to request", "error", err)
			}
		}
	})
	if err != nil {
		return fmt.Errorf("subscribe to %s: %w", subject, err)
	}

	b.subs = append(b.subs, sub)
	b.logger.Info("subscribed", "subject", subject, "queue", queueGroup)
	return nil
}

// Close drains and closes the NATS connection.
func (b *Bus) Close() error {
	b.logger.Info("draining NATS connection")
	return b.conn.Drain()
}
