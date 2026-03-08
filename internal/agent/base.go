package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// BaseAgent provides shared functionality for all specialized agents.
// Embed this in concrete agent implementations.
type BaseAgent struct {
	card   AgentCard
	bus    MessageBus
	router ModelRouter
	tools  ToolRegistry
	logger *slog.Logger
}

// NewBaseAgent creates a new BaseAgent with the given dependencies.
func NewBaseAgent(card AgentCard, bus MessageBus, router ModelRouter, tools ToolRegistry, logger *slog.Logger) BaseAgent {
	return BaseAgent{
		card:   card,
		bus:    bus,
		router: router,
		tools:  tools,
		logger: logger.With("agent", card.Name),
	}
}

// Card returns the agent's A2A card.
func (b *BaseAgent) Card() AgentCard {
	return b.card
}

// Name returns the agent's unique name.
func (b *BaseAgent) Name() string {
	return b.card.Name
}

// Logger returns the agent's logger.
func (b *BaseAgent) Logger() *slog.Logger {
	return b.logger
}

// CallLLM makes an LLM call using the configured model router.
// The tier determines which model class is used (flash/standard/premium).
func (b *BaseAgent) CallLLM(ctx context.Context, tier ModelTier, systemPrompt, userPrompt string) (*LLMResponse, error) {
	b.logger.Info("calling LLM",
		"tier", tier,
		"systemPromptLen", len(systemPrompt),
		"userPromptLen", len(userPrompt),
	)

	resp, err := b.router.Call(ctx, tier, systemPrompt, userPrompt)
	if err != nil {
		b.logger.Error("LLM call failed", "tier", tier, "error", err)
		return nil, fmt.Errorf("llm call (tier=%s): %w", tier, err)
	}

	b.logger.Info("LLM call completed",
		"tier", tier,
		"model", resp.Model,
		"inputTokens", resp.Usage.InputTokens,
		"outputTokens", resp.Usage.OutputTokens,
	)

	return resp, nil
}

// AskAgent sends a request to another agent and waits for a response.
// This implements the A2A request/reply pattern over NATS.
func (b *BaseAgent) AskAgent(ctx context.Context, targetAgent, skillID string, payload map[string]any, timeout time.Duration) (*TaskResult, error) {
	msg := Message{
		ID:        uuid.New().String(),
		From:      b.card.Name,
		To:        targetAgent,
		SkillID:   skillID,
		Payload:   payload,
		Timestamp: time.Now(),
	}

	b.logger.Info("asking agent",
		"target", targetAgent,
		"skill", skillID,
	)

	result, err := b.bus.Request(ctx, msg, timeout)
	if err != nil {
		b.logger.Error("agent request failed",
			"target", targetAgent,
			"skill", skillID,
			"error", err,
		)
		return nil, fmt.Errorf("ask %s.%s: %w", targetAgent, skillID, err)
	}

	return result, nil
}

// NotifyAgent sends a fire-and-forget message to another agent.
func (b *BaseAgent) NotifyAgent(ctx context.Context, targetAgent, skillID string, payload map[string]any) error {
	msg := Message{
		ID:        uuid.New().String(),
		From:      b.card.Name,
		To:        targetAgent,
		SkillID:   skillID,
		Payload:   payload,
		Timestamp: time.Now(),
	}

	return b.bus.Publish(ctx, msg)
}

// UseTool executes a registered tool by name.
func (b *BaseAgent) UseTool(ctx context.Context, toolName string, input map[string]any) (map[string]any, error) {
	return b.tools.Execute(ctx, toolName, input)
}
