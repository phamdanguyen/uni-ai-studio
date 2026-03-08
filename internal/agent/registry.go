package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// DefaultRegistry is the standard in-memory agent registry.
type DefaultRegistry struct {
	mu         sync.RWMutex
	agents     map[string]Agent
	bus        MessageBus
	supervisor *Supervisor
	logger     *slog.Logger
}

// NewRegistry creates a new agent registry.
// Pass a non-nil supervisor to enable automatic health tracking on every message.
func NewRegistry(bus MessageBus, supervisor *Supervisor, logger *slog.Logger) *DefaultRegistry {
	return &DefaultRegistry{
		agents:     make(map[string]Agent),
		bus:        bus,
		supervisor: supervisor,
		logger:     logger.With("component", "registry"),
	}
}

// Register adds an agent to the registry.
func (r *DefaultRegistry) Register(agent Agent) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := agent.Name()
	if _, exists := r.agents[name]; exists {
		return fmt.Errorf("agent %q already registered", name)
	}

	r.agents[name] = agent
	r.logger.Info("agent registered", "name", name, "skills", len(agent.Card().Skills))
	return nil
}

// Get returns an agent by name.
func (r *DefaultRegistry) Get(name string) (Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.agents[name]
	return a, ok
}

// List returns all registered agent cards.
func (r *DefaultRegistry) List() []AgentCard {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cards := make([]AgentCard, 0, len(r.agents))
	for _, a := range r.agents {
		cards = append(cards, a.Card())
	}
	return cards
}

// StartAll subscribes all registered agents to their NATS subjects.
// If a supervisor is configured, each handler is wrapped to record success/failure metrics.
func (r *DefaultRegistry) StartAll(ctx context.Context) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for name, agent := range r.agents {
		a := agent // capture for closure
		n := name  // capture for closure

		handler := func(ctx context.Context, msg Message) (*TaskResult, error) {
			start := time.Now()
			result, err := a.HandleMessage(ctx, msg)
			if r.supervisor != nil {
				if err != nil || (result != nil && result.Status == TaskStatusFailed) {
					r.supervisor.RecordFailure(n, err)
				} else {
					r.supervisor.RecordSuccess(n, time.Since(start))
				}
			}
			return result, err
		}

		if err := r.bus.Subscribe(name, handler); err != nil {
			return fmt.Errorf("subscribe agent %q: %w", name, err)
		}
		r.logger.Info("agent listening", "name", name)
	}

	return nil
}

// StopAll gracefully shuts down all agents.
func (r *DefaultRegistry) StopAll() error {
	r.logger.Info("stopping all agents")
	return r.bus.Close()
}
