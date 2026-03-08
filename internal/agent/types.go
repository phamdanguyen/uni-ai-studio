// Package agent defines the core types and interfaces for the WAOO Studio agent system.
// All agents implement the Agent interface and communicate via A2A-compatible messages.
package agent

import (
	"context"
	"time"
)

// --- Agent Card (A2A-Compatible) ---

// AgentCard describes an agent's identity, capabilities, and skills.
// This follows the Google A2A Agent Card specification.
type AgentCard struct {
	Name         string       `json:"name" yaml:"name"`
	Version      string       `json:"version" yaml:"version"`
	Description  string       `json:"description" yaml:"description"`
	URL          string       `json:"url,omitempty" yaml:"url,omitempty"`
	Skills       []Skill      `json:"skills" yaml:"skills"`
	Capabilities Capabilities `json:"capabilities" yaml:"capabilities"`
}

// Skill represents a specific capability an agent can perform.
type Skill struct {
	ID          string   `json:"id" yaml:"id"`
	Name        string   `json:"name" yaml:"name"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	InputModes  []string `json:"inputModes" yaml:"inputModes"`
	OutputModes []string `json:"outputModes" yaml:"outputModes"`
}

// Capabilities describes what interaction modes the agent supports.
type Capabilities struct {
	Streaming              bool `json:"streaming" yaml:"streaming"`
	StateTransitionHistory bool `json:"stateTransitionHistory" yaml:"stateTransitionHistory"`
	PushNotifications      bool `json:"pushNotifications,omitempty" yaml:"pushNotifications,omitempty"`
}

// --- Messages (A2A-Compatible) ---

// Message is the core communication unit between agents.
type Message struct {
	ID        string         `json:"id"`
	From      string         `json:"from"`
	To        string         `json:"to"`
	SkillID   string         `json:"skillId"`
	TaskID    string         `json:"taskId"`
	ProjectID string         `json:"projectId"`
	Payload   map[string]any `json:"payload,omitempty"`
	ReplyTo   string         `json:"replyTo,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
	TraceID   string         `json:"traceId,omitempty"`
}

// TaskStatus represents the current state of a task.
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"
	TaskStatusRunning    TaskStatus = "running"
	TaskStatusCompleted  TaskStatus = "completed"
	TaskStatusFailed     TaskStatus = "failed"
	TaskStatusNeedsInput TaskStatus = "needs_input"
	TaskStatusCancelled  TaskStatus = "cancelled"
)

// TaskResult is the output of an agent handling a message.
type TaskResult struct {
	Status   TaskStatus     `json:"status"`
	Output   map[string]any `json:"output,omitempty"`
	Error    string         `json:"error,omitempty"`
	Tokens   TokenUsage     `json:"tokens"`
	Duration time.Duration  `json:"duration"`
}

// TokenUsage tracks LLM token consumption for a task.
type TokenUsage struct {
	InputTokens  int     `json:"inputTokens"`
	OutputTokens int     `json:"outputTokens"`
	TotalTokens  int     `json:"totalTokens"`
	CostUSD      float64 `json:"costUsd"`
	Model        string  `json:"model"`
	Tier         string  `json:"tier"` // flash, standard, premium
}

// StreamEvent is emitted during streaming agent execution.
type StreamEvent struct {
	Type    string `json:"type"` // progress, partial, status, error
	Payload any    `json:"payload,omitempty"`
}

// --- Agent Interface ---

// Agent is the core interface that all specialized agents must implement.
type Agent interface {
	// Card returns the agent's A2A-compatible card describing capabilities.
	Card() AgentCard

	// HandleMessage processes a message and returns a result.
	HandleMessage(ctx context.Context, msg Message) (*TaskResult, error)

	// HandleStream processes a message with streaming output.
	HandleStream(ctx context.Context, msg Message, stream chan<- StreamEvent) error

	// Name returns the agent's unique name.
	Name() string
}
