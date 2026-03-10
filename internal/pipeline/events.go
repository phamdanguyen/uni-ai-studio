// Package pipeline — shared event types cho cả Autopilot và Step-by-Step.
// Events được publish lên NATS subject: pipeline.{projectId}.events
// và đồng thời emit qua SSE cho frontend.
package pipeline

import (
	"fmt"
	"time"
)

// ExecutionMode quyết định cách pipeline chạy.
type ExecutionMode string

const (
	// ModeAutopilot: Director tự điều phối A2A từ đầu đến cuối.
	ModeAutopilot ExecutionMode = "autopilot"

	// ModeStepByStep: Mỗi bước dừng lại chờ human approve/edit.
	ModeStepByStep ExecutionMode = "step_by_step"
)

// PipelineEventType định nghĩa các loại sự kiện trong pipeline.
type PipelineEventType string

const (
	// Lifecycle events
	EventPipelineStarted   PipelineEventType = "pipeline.started"
	EventPipelineCompleted PipelineEventType = "pipeline.completed"
	EventPipelineFailed    PipelineEventType = "pipeline.failed"

	// Stage events (dùng cho cả 2 mode)
	EventStageStarted   PipelineEventType = "stage.started"
	EventStageCompleted PipelineEventType = "stage.completed"
	EventStageFailed    PipelineEventType = "stage.failed"

	// Step-by-Step specific: dừng chờ human
	EventStageAwaitingApproval PipelineEventType = "stage.awaiting_approval"

	// A2A coordination events (Autopilot only)
	EventAnalysisDone    PipelineEventType = "analysis.done"
	EventCharactersDone  PipelineEventType = "characters.done"
	EventLocationsDone   PipelineEventType = "locations.done"
	EventSegmentDone     PipelineEventType = "segment.done"
	EventScreenplaysDone PipelineEventType = "screenplays.done"
	EventStoryboardsDone PipelineEventType = "storyboards.done"
	EventMediaDone       PipelineEventType = "media.done"
	EventVoiceDone       PipelineEventType = "voice.done"
)

// PipelineEvent là sự kiện được publish lên NATS và emit qua SSE.
type PipelineEvent struct {
	Type      PipelineEventType `json:"type"`
	ProjectID string            `json:"projectId"`
	Stage     Stage             `json:"stage,omitempty"`
	Mode      ExecutionMode     `json:"mode"`
	Status    string            `json:"status,omitempty"` // "started", "completed", "failed", "awaiting_approval"
	Message   string            `json:"message,omitempty"`
	Data      map[string]any    `json:"data,omitempty"`
	Error     string            `json:"error,omitempty"`
	Timestamp time.Time         `json:"timestamp"`

	// Step-by-Step: index hiện tại và tổng số bước
	StageIndex  int `json:"stageIndex,omitempty"`
	TotalStages int `json:"totalStages,omitempty"`
}

// StepStatus là trạng thái của một bước trong Step-by-Step mode.
type StepStatus string

const (
	StepStatusPending           StepStatus = "pending"
	StepStatusRunning           StepStatus = "running"
	StepStatusAwaitingApproval  StepStatus = "awaiting_approval"
	StepStatusApproved          StepStatus = "approved"
	StepStatusCompleted         StepStatus = "completed"
	StepStatusFailed            StepStatus = "failed"
)

// RunState là trạng thái tổng thể của một pipeline run được lưu trong DB.
type RunState struct {
	ProjectID     string        `json:"projectId"`
	Mode          ExecutionMode `json:"mode"`
	CurrentStage  Stage         `json:"currentStage"`
	CurrentStatus StepStatus    `json:"currentStatus"`
	// Run metadata
	Story        string     `json:"story,omitempty"`
	InputType    string     `json:"inputType,omitempty"`
	Budget       string     `json:"budget,omitempty"`
	QualityLevel string     `json:"qualityLevel,omitempty"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
	Error        string     `json:"error,omitempty"`
}

// NATSSubject trả về NATS subject để publish/subscribe events cho một project.
func NATSSubject(projectID string) string {
	return fmt.Sprintf("pipeline.%s.events", projectID)
}

// NATSSubjectForType trả về NATS subject cho một event type cụ thể.
// Dùng trong Autopilot để agents subscribe đúng event.
func NATSSubjectForType(projectID string, eventType PipelineEventType) string {
	return fmt.Sprintf("pipeline.%s.%s", projectID, eventType)
}
