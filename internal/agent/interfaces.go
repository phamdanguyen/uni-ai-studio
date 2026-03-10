package agent

import (
	"context"
	"time"
)

// --- Message Bus (NATS abstraction) ---

// MessageBus abstracts the inter-agent communication layer.
// In production, this is backed by NATS JetStream.
type MessageBus interface {
	// Publish sends a fire-and-forget message.
	Publish(ctx context.Context, msg Message) error

	// Request sends a message and waits for a reply (request/reply pattern).
	Request(ctx context.Context, msg Message, timeout time.Duration) (*TaskResult, error)

	// Subscribe registers a handler for messages to a specific agent.
	Subscribe(agentName string, handler MessageHandler) error

	// Close gracefully shuts down the message bus connection.
	Close() error
}

// MessageHandler processes incoming messages for an agent.
type MessageHandler func(ctx context.Context, msg Message) (*TaskResult, error)

// --- Model Router (LLM abstraction) ---

// ModelTier represents the quality/cost tier for LLM selection.
type ModelTier string

const (
	TierFlash    ModelTier = "flash"    // Fast, cheap: Gemini 2.0 Flash, GPT-4o-mini
	TierStandard ModelTier = "standard" // Balanced: Claude Sonnet, GPT-4o
	TierPremium  ModelTier = "premium"  // Best quality: Claude Opus, o1
)

// ModelRouter selects and calls LLM models based on tier.
type ModelRouter interface {
	// Call makes an LLM request using the appropriate model for the given tier.
	Call(ctx context.Context, tier ModelTier, systemPrompt, userPrompt string) (*LLMResponse, error)

	// CallWithJSON makes an LLM request expecting structured JSON output.
	CallWithJSON(ctx context.Context, tier ModelTier, systemPrompt, userPrompt string, schema any) (*LLMResponse, error)

	// CallForAgent makes an LLM request with agent-specific model override.
	CallForAgent(ctx context.Context, agentName string, tier ModelTier, systemPrompt, userPrompt string) (*LLMResponse, error)

	// CallWithJSONForAgent is like CallWithJSON but with agent-specific override.
	CallWithJSONForAgent(ctx context.Context, agentName string, tier ModelTier, systemPrompt, userPrompt string, schema any) (*LLMResponse, error)

	// CheckBudget verifies the project hasn't exceeded its token budget.
	CheckBudget(ctx context.Context, projectID string) error

	// RecordUsage records token usage for billing and budget tracking.
	RecordUsage(ctx context.Context, projectID string, usage TokenUsage) error
}

// LLMResponse is the unified response from any LLM provider.
type LLMResponse struct {
	Content    string     `json:"content"`
	Model      string     `json:"model"`
	Usage      TokenUsage `json:"usage"`
	StopReason string     `json:"stopReason,omitempty"`
}

// --- Tool Registry ---

// ToolRegistry manages available tools that agents can use.
type ToolRegistry interface {
	// Register adds a tool to the registry.
	Register(name string, tool Tool) error

	// Execute runs a tool by name with the given input.
	Execute(ctx context.Context, name string, input map[string]any) (map[string]any, error)

	// List returns all registered tool names and descriptions.
	List() []ToolInfo
}

// Tool is an executable action an agent can perform.
type Tool struct {
	Info    ToolInfo
	Execute ToolFunc
}

// ToolFunc is the function signature for tool execution.
type ToolFunc func(ctx context.Context, input map[string]any) (map[string]any, error)

// ToolInfo describes a tool's purpose and parameters.
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	InputSchema any    `json:"inputSchema,omitempty"`
}

// --- Agent Registry ---

// Registry manages agent discovery and lifecycle.
type Registry interface {
	// Register adds an agent to the registry.
	Register(agent Agent) error

	// Get returns an agent by name.
	Get(name string) (Agent, bool)

	// List returns all registered agent cards.
	List() []AgentCard

	// StartAll begins listening for messages on all registered agents.
	StartAll(ctx context.Context) error

	// StopAll gracefully shuts down all agents.
	StopAll() error
}
