package agent

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/uni-ai-studio/waoo-studio/internal/memory"
)

// BaseAgent provides shared functionality for all specialized agents.
// Embed this in concrete agent implementations.
type BaseAgent struct {
	card   AgentCard
	bus    MessageBus
	router ModelRouter
	tools  ToolRegistry
	memory *memory.Store
	logger *slog.Logger
	name   string // agent name for model routing
}

// NewBaseAgent creates a new BaseAgent with the given dependencies.
func NewBaseAgent(card AgentCard, bus MessageBus, router ModelRouter, tools ToolRegistry, mem *memory.Store, logger *slog.Logger) BaseAgent {
	return BaseAgent{
		card:   card,
		bus:    bus,
		router: router,
		tools:  tools,
		memory: mem,
		logger: logger.With("agent", card.Name),
		name:   card.Name,
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

// Memory returns the tiered memory store (may be nil).
func (b *BaseAgent) Memory() *memory.Store {
	return b.memory
}

// CallLLM makes an LLM call using the configured model router.
// Automatically uses per-agent model override if configured; falls back to global tier model.
func (b *BaseAgent) CallLLM(ctx context.Context, tier ModelTier, systemPrompt, userPrompt string) (*LLMResponse, error) {
	b.logger.Info("calling LLM",
		"tier", tier,
		"systemPromptLen", len(systemPrompt),
		"userPromptLen", len(userPrompt),
	)

	resp, err := b.router.CallForAgent(ctx, b.name, tier, systemPrompt, userPrompt)
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

// CallLLMWithJSON makes an LLM call expecting structured JSON output.
// Automatically uses per-agent model override if configured; falls back to global tier model.
func (b *BaseAgent) CallLLMWithJSON(ctx context.Context, tier ModelTier, systemPrompt, userPrompt string, schema any) (*LLMResponse, error) {
	b.logger.Info("calling LLM with JSON",
		"tier", tier,
		"systemPromptLen", len(systemPrompt),
		"userPromptLen", len(userPrompt),
	)

	resp, err := b.router.CallWithJSONForAgent(ctx, b.name, tier, systemPrompt, userPrompt, schema)
	if err != nil {
		b.logger.Error("LLM JSON call failed", "tier", tier, "error", err)
		return nil, fmt.Errorf("llm json call (tier=%s): %w", tier, err)
	}

	b.logger.Info("LLM JSON call completed",
		"tier", tier,
		"model", resp.Model,
		"inputTokens", resp.Usage.InputTokens,
		"outputTokens", resp.Usage.OutputTokens,
	)

	return resp, nil
}

// CallLLMForAgent is an explicit variant that passes agent name for model routing.
// Identical to CallLLM but provided for clarity in code that wants to be explicit.
func (b *BaseAgent) CallLLMForAgent(ctx context.Context, tier ModelTier, systemPrompt, userPrompt string) (*LLMResponse, error) {
	return b.CallLLM(ctx, tier, systemPrompt, userPrompt)
}

// CallLLMWithJSONForAgent is an explicit variant that passes agent name for model routing.
// Identical to CallLLMWithJSON but provided for clarity.
func (b *BaseAgent) CallLLMWithJSONForAgent(ctx context.Context, tier ModelTier, systemPrompt, userPrompt string, schema any) (*LLMResponse, error) {
	return b.CallLLMWithJSON(ctx, tier, systemPrompt, userPrompt, schema)
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

// BusRef returns the underlying MessageBus.
// Used by agents that need to publish raw events (e.g. Director → NATS pipeline events).
func (b *BaseAgent) BusRef() MessageBus {
	return b.bus
}

// UseTool executes a registered tool by name.
func (b *BaseAgent) UseTool(ctx context.Context, toolName string, input map[string]any) (map[string]any, error) {
	return b.tools.Execute(ctx, toolName, input)
}
