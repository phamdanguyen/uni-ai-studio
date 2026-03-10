// Package pipeline — Autopilot executor.
// Autopilot mode: Pipeline chỉ khởi động Director với skill "start_production".
// Director tự điều phối toàn bộ A2A từ story → assembly.
// Autopilot subscribe NATS events để emit SSE progress ra UI.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/uni-ai-studio/waoo-studio/internal/agent"
	"github.com/uni-ai-studio/waoo-studio/internal/natsbus"
)

// AutopilotPipeline khởi động Director và để A2A tự chạy.
// Không có blocking goroutine — Director là orchestrator thực sự.
type AutopilotPipeline struct {
	bus        agent.MessageBus
	logger     *slog.Logger
	listeners  []ProgressListener
	checkpoint *CheckpointStore
	falKey     string
}

// NewAutopilotPipeline tạo Autopilot executor.
func NewAutopilotPipeline(bus agent.MessageBus, logger *slog.Logger) *AutopilotPipeline {
	return &AutopilotPipeline{
		bus:    bus,
		logger: logger.With("component", "autopilot"),
	}
}

// SetCheckpointStore enables state persistence.
func (p *AutopilotPipeline) SetCheckpointStore(cs *CheckpointStore) {
	p.checkpoint = cs
}

// SetFALKey enables FAL media URL resolution (passed through to Director payload).
func (p *AutopilotPipeline) SetFALKey(key string) {
	p.falKey = key
}

// OnProgress registers a listener for SSE progress events.
func (p *AutopilotPipeline) OnProgress(listener ProgressListener) {
	p.listeners = append(p.listeners, listener)
}

// Start khởi động Autopilot pipeline.
// Chỉ làm 2 việc:
//  1. Lưu run state vào DB
//  2. Publish 1 message đến Director với skill "start_production"
//
// Director tự điều phối toàn bộ phần còn lại qua A2A.
// Return ngay lập tức — không block.
func (p *AutopilotPipeline) Start(ctx context.Context, req PipelineRequest) error {
	// Lưu run state ban đầu
	if p.checkpoint != nil {
		if err := p.checkpoint.UpsertRunStateFull(
			ctx, req.ProjectID, ModeAutopilot, StageAnalysis, StepStatusRunning, &req, "",
		); err != nil {
			p.logger.Warn("failed to persist run state", "error", err)
		}
	}

	// Emit pipeline started qua SSE ngay lập tức
	p.emit(ProgressEvent{
		ProjectID:   req.ProjectID,
		Stage:       StageAnalysis,
		StageIndex:  0,
		TotalStages: len(stageOrder),
		Status:      "started",
		Message:     "Autopilot pipeline started — Director is taking control",
		Timestamp:   time.Now(),
	})

	// Subscribe NATS progress events từ Director để forward ra SSE
	// Director sẽ publish lên subject: pipeline.{projectId}.events
	if natsBus, ok := p.bus.(*natsbus.Bus); ok {
		subject := NATSSubject(req.ProjectID)
		if err := p.subscribeProgressEvents(natsBus, req.ProjectID, subject); err != nil {
			p.logger.Warn("failed to subscribe pipeline events", "error", err)
			// Non-fatal: autopilot vẫn chạy, chỉ SSE không có real-time updates
		}
	}

	// Publish 1 message duy nhất đến Director
	// Director tự làm phần còn lại
	msg := agent.Message{
		ID:        uuid.New().String(),
		From:      "pipeline",
		To:        "director",
		SkillID:   "start_production",
		ProjectID: req.ProjectID,
		Payload: map[string]any{
			"projectId":    req.ProjectID,
			"story":        req.Story,
			"inputType":    req.InputType,
			"budget":       req.Budget,
			"qualityLevel": req.QualityLevel,
			"falKey":       p.falKey,
		},
		Timestamp: time.Now(),
	}

	if err := p.bus.Publish(ctx, msg); err != nil {
		errMsg := fmt.Sprintf("failed to start Director: %s", err.Error())
		if p.checkpoint != nil {
			_ = p.checkpoint.MarkRunFailed(ctx, req.ProjectID, errMsg)
		}
		return fmt.Errorf("publish start_production: %w", err)
	}

	p.logger.Info("autopilot started — Director is orchestrating",
		"projectId", req.ProjectID,
		"story_len", len(req.Story),
	)

	return nil
}

// subscribeProgressEvents subscribes to NATS events từ Director và forward ra SSE.
// Director publish events theo format PipelineEvent lên subject pipeline.{projectId}.events
func (p *AutopilotPipeline) subscribeProgressEvents(bus *natsbus.Bus, projectID, subject string) error {
	return bus.SubscribeOnce(subject, func(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
		// Decode PipelineEvent từ payload
		payloadBytes, err := json.Marshal(msg.Payload)
		if err != nil {
			return nil, nil //nolint:nilerr // best-effort, skip malformed events
		}

		var event PipelineEvent
		if err := json.Unmarshal(payloadBytes, &event); err != nil {
			return nil, nil //nolint:nilerr
		}

		// Forward as ProgressEvent cho SSE listeners
		progress := ProgressEvent{
			ProjectID:   projectID,
			Stage:       event.Stage,
			StageIndex:  event.StageIndex,
			TotalStages: event.TotalStages,
			Status:      event.Status,
			Message:     event.Message,
			Data:        event.Data,
			Timestamp:   event.Timestamp,
		}
		if progress.Timestamp.IsZero() {
			progress.Timestamp = time.Now()
		}
		p.emit(progress)

		// Persist stage result nếu có checkpoint store
		if p.checkpoint != nil && event.Status == "completed" && event.Stage != "" {
			idx := stageIndexOf(event.Stage)
			if idx >= 0 {
				_ = p.checkpoint.SaveStage(
					context.Background(), projectID, idx, event.Stage,
					"completed", nil, event.Data, nil,
				)
				_ = p.checkpoint.UpsertRunStateFull(
					context.Background(), projectID, ModeAutopilot,
					event.Stage, StepStatusCompleted,
					&PipelineRequest{ProjectID: projectID}, "",
				)
			}
		}

		// Nếu pipeline hoàn thành, đánh dấu completed trong DB
		if event.Type == EventPipelineCompleted {
			if p.checkpoint != nil {
				_ = p.checkpoint.MarkRunCompleted(context.Background(), projectID)
			}
		}

		return &agent.TaskResult{Status: agent.TaskStatusCompleted}, nil
	})
}

// emit forwards ProgressEvent ra tất cả SSE listeners.
func (p *AutopilotPipeline) emit(event ProgressEvent) {
	for _, l := range p.listeners {
		l(event)
	}
}
