// Package pipeline — Step-by-Step executor.
// Chạy pipeline từng bước một, dừng lại sau mỗi bước chờ human approve/edit.
// State được lưu toàn bộ vào PostgreSQL nên không cần goroutine sống giữa các bước.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/uni-ai-studio/waoo-studio/internal/agent"
)

// stageOrder định nghĩa thứ tự cố định các bước trong Step-by-Step mode.
var stageOrder = []Stage{
	StageAnalysis,
	StagePlanning,
	StageCharacters,
	StageLocations,
	StageSegmentation,
	StageScreenplay,
	StageStoryboard,
	StageMediaGen,
	StageQualityCheck,
	StageVoice,
	StageAssembly,
}

// stageIndexOf trả về index của stage trong stageOrder, -1 nếu không tìm thấy.
func stageIndexOf(stage Stage) int {
	for i, s := range stageOrder {
		if s == stage {
			return i
		}
	}
	return -1
}

// CurrentStepInfo mô tả bước hiện tại để UI hiển thị review panel.
type CurrentStepInfo struct {
	Stage       Stage          `json:"stage"`
	StageIndex  int            `json:"stageIndex"`
	TotalStages int            `json:"totalStages"`
	Status      StepStatus     `json:"status"`
	Output      map[string]any `json:"output,omitempty"`
	IsLast      bool           `json:"isLast"`
	CanEdit     bool           `json:"canEdit"`
}

// StepByStepPipeline chạy pipeline từng bước một với human gate giữa các bước.
// Sau mỗi bước: lưu output vào DB, emit SSE "awaiting_approval", dừng lại.
// Human gọi RunNextStep() để tiếp tục, có thể kèm editedOutput để override.
type StepByStepPipeline struct {
	bus        agent.MessageBus
	logger     *slog.Logger
	listeners  []ProgressListener
	checkpoint *CheckpointStore
	falKey     string
	// inner dùng để tái dùng các runXxx functions từ Pipeline
	inner *Pipeline
}

// NewStepByStepPipeline tạo Step-by-Step executor.
func NewStepByStepPipeline(bus agent.MessageBus, logger *slog.Logger) *StepByStepPipeline {
	inner := NewPipeline(bus, logger)
	p := &StepByStepPipeline{
		bus:    bus,
		logger: logger.With("component", "stepbystep"),
		inner:  inner,
	}
	// Mirror progress events từ inner pipeline ra listeners của StepByStep
	inner.OnProgress(func(event ProgressEvent) {
		p.emit(event)
	})
	return p
}

// SetCheckpointStore enables state persistence.
func (p *StepByStepPipeline) SetCheckpointStore(cs *CheckpointStore) {
	p.checkpoint = cs
	p.inner.SetCheckpointStore(cs)
}

// SetFALKey enables FAL media URL resolution.
func (p *StepByStepPipeline) SetFALKey(key string) {
	p.falKey = key
	p.inner.SetFALKey(key)
}

// OnProgress registers a listener for SSE progress events.
func (p *StepByStepPipeline) OnProgress(listener ProgressListener) {
	p.listeners = append(p.listeners, listener)
}

// Start khởi động pipeline Step-by-Step.
// Chạy bước đầu tiên (StageAnalysis) ngay, sau đó dừng chờ human.
func (p *StepByStepPipeline) Start(ctx context.Context, req PipelineRequest) error {
	if p.checkpoint == nil {
		return fmt.Errorf("step-by-step mode requires checkpoint store (database)")
	}

	// Lưu run state ban đầu vào DB
	if err := p.checkpoint.UpsertRunStateFull(
		ctx, req.ProjectID, ModeStepByStep, StageAnalysis, StepStatusRunning, &req, "",
	); err != nil {
		p.logger.Warn("failed to persist run state", "error", err)
	}

	// Emit pipeline started
	p.emit(ProgressEvent{
		ProjectID:   req.ProjectID,
		Stage:       StageAnalysis,
		StageIndex:  0,
		TotalStages: len(stageOrder),
		Status:      "started",
		Message:     "Step-by-Step pipeline started",
		Timestamp:   time.Now(),
	})

	// Chạy bước đầu tiên (Analysis) ngay lập tức
	return p.runStepAndPause(ctx, req.ProjectID, StageAnalysis, &req)
}

// RunNextStep tiếp tục chạy bước kế tiếp.
// editedOutput: nếu human đã chỉnh sửa output của bước hiện tại, truyền vào đây.
//
//	nil = dùng output gốc của agent.
func (p *StepByStepPipeline) RunNextStep(ctx context.Context, projectID string, editedOutput map[string]any) error {
	if p.checkpoint == nil {
		return fmt.Errorf("checkpoint store not available")
	}

	// Load run state để biết đang ở bước nào
	runState, err := p.checkpoint.GetRunState(ctx, projectID)
	if err != nil {
		return fmt.Errorf("load run state: %w", err)
	}

	// Nếu đã completed, không làm gì
	if runState.CurrentStatus == StepStatusCompleted {
		return fmt.Errorf("pipeline already completed")
	}
	if runState.CurrentStatus == StepStatusFailed {
		return fmt.Errorf("pipeline failed: %s", runState.Error)
	}

	currentIdx := stageIndexOf(runState.CurrentStage)
	if currentIdx < 0 {
		return fmt.Errorf("unknown current stage: %s", runState.CurrentStage)
	}

	// Nếu human có edit output bước hiện tại → ghi đè vào DB trước
	if editedOutput != nil {
		if err := p.checkpoint.UpdateStageOutput(ctx, projectID, runState.CurrentStage, editedOutput); err != nil {
			p.logger.Warn("failed to save edited output", "stage", runState.CurrentStage, "error", err)
		}
	}

	// Xác định bước tiếp theo
	nextIdx := currentIdx + 1
	if nextIdx >= len(stageOrder) {
		// Đã chạy hết, mark completed
		_ = p.checkpoint.MarkRunCompleted(ctx, projectID)
		p.emit(ProgressEvent{
			ProjectID:   projectID,
			Stage:       StageComplete,
			StageIndex:  len(stageOrder),
			TotalStages: len(stageOrder),
			Status:      "completed",
			Message:     "Pipeline completed",
			Timestamp:   time.Now(),
		})
		return nil
	}

	nextStage := stageOrder[nextIdx]

	// Build PipelineRequest từ tất cả output đã lưu trong DB
	req, err := p.buildRequestFromDB(ctx, projectID)
	if err != nil {
		return fmt.Errorf("rebuild request: %w", err)
	}

	// Update run state: đang chạy bước tiếp
	_ = p.checkpoint.UpsertRunStateFull(ctx, projectID, ModeStepByStep, nextStage, StepStatusRunning, req, "")

	return p.runStepAndPause(ctx, projectID, nextStage, req)
}

// GetCurrentStep trả về thông tin bước hiện tại để UI hiển thị.
func (p *StepByStepPipeline) GetCurrentStep(ctx context.Context, projectID string) (*CurrentStepInfo, error) {
	if p.checkpoint == nil {
		return nil, fmt.Errorf("checkpoint store not available")
	}

	runState, err := p.checkpoint.GetRunState(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("load run state: %w", err)
	}

	idx := stageIndexOf(runState.CurrentStage)
	if idx < 0 {
		idx = 0
	}

	output, _ := p.checkpoint.GetStageOutput(ctx, projectID, runState.CurrentStage)

	return &CurrentStepInfo{
		Stage:       runState.CurrentStage,
		StageIndex:  idx,
		TotalStages: len(stageOrder),
		Status:      runState.CurrentStatus,
		Output:      output,
		IsLast:      idx == len(stageOrder)-1,
		CanEdit:     runState.CurrentStatus == StepStatusAwaitingApproval,
	}, nil
}

// --- internal helpers ---

// runStepAndPause chạy 1 stage, lưu kết quả, emit "awaiting_approval".
func (p *StepByStepPipeline) runStepAndPause(ctx context.Context, projectID string, stage Stage, req *PipelineRequest) error {
	idx := stageIndexOf(stage)
	total := len(stageOrder)

	// Emit started
	p.emit(ProgressEvent{
		ProjectID:   projectID,
		Stage:       stage,
		StageIndex:  idx,
		TotalStages: total,
		Status:      "started",
		Message:     fmt.Sprintf("Running %s", stage),
		Timestamp:   time.Now(),
	})
	_ = p.checkpoint.SaveStage(ctx, projectID, idx, stage, "running",
		buildStageInput(stage, req), nil, nil)

	// Chạy stage thông qua stageRunner của inner Pipeline
	runner := p.inner.stageRunner(stage)
	if runner == nil {
		return fmt.Errorf("no runner for stage: %s", stage)
	}

	stageResult, err := runner(ctx, req)
	if err != nil {
		errMsg := err.Error()
		_ = p.checkpoint.SaveStage(ctx, projectID, idx, stage, "failed", nil, nil, err)
		_ = p.checkpoint.MarkRunFailed(ctx, projectID, errMsg)
		p.emit(ProgressEvent{
			ProjectID:   projectID,
			Stage:       stage,
			StageIndex:  idx,
			TotalStages: total,
			Status:      "failed",
			Message:     errMsg,
			Timestamp:   time.Now(),
		})
		return fmt.Errorf("stage %s: %w", stage, err)
	}

	// Resolve FAL URLs nếu cần
	if stage == StageMediaGen && p.falKey != "" {
		stageResult.Data = p.inner.resolveMediaURLs(ctx, stageResult.Data)
	}

	// Lưu output vào DB
	_ = p.checkpoint.SaveStage(ctx, projectID, idx, stage, "completed",
		nil, stageResult.Data, nil)

	// Emit completed
	p.emit(ProgressEvent{
		ProjectID:   projectID,
		Stage:       stage,
		StageIndex:  idx,
		TotalStages: total,
		Status:      "completed",
		Message:     stageResult.Summary,
		Data:        stageResult.Data,
		Timestamp:   time.Now(),
	})

	isLast := idx == len(stageOrder)-1
	if isLast {
		// Bước cuối: mark completed
		_ = p.checkpoint.MarkRunCompleted(ctx, projectID)
		_ = p.checkpoint.UpsertRunStateFull(ctx, projectID, ModeStepByStep, stage, StepStatusCompleted, req, "")
		p.emit(ProgressEvent{
			ProjectID:   projectID,
			Stage:       StageComplete,
			StageIndex:  total,
			TotalStages: total,
			Status:      "completed",
			Message:     "All steps completed — film production done",
			Timestamp:   time.Now(),
		})
		return nil
	}

	// Không phải bước cuối: dừng lại, chờ human
	_ = p.checkpoint.UpsertRunStateFull(ctx, projectID, ModeStepByStep, stage, StepStatusAwaitingApproval, req, "")
	p.emit(ProgressEvent{
		ProjectID:   projectID,
		Stage:       stage,
		StageIndex:  idx,
		TotalStages: total,
		Status:      "awaiting_approval",
		Message:     fmt.Sprintf("Step %s complete — review and approve to continue", stage),
		Data:        stageResult.Data,
		Timestamp:   time.Now(),
	})

	return nil
}

// buildRequestFromDB tái tạo PipelineRequest từ tất cả stage outputs đã lưu trong DB.
func (p *StepByStepPipeline) buildRequestFromDB(ctx context.Context, projectID string) (*PipelineRequest, error) {
	stages, err := p.checkpoint.GetAllStages(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("load stages: %w", err)
	}

	// Load run metadata via GetRunMeta (reads pipeline_runs table).
	var story, inputType, budget, quality string
	story, inputType, budget, quality, _ = p.checkpoint.GetRunMeta(ctx, projectID)

	req := &PipelineRequest{
		ProjectID:    projectID,
		Story:        story,
		InputType:    inputType,
		Budget:       budget,
		QualityLevel: quality,
	}

	// Populate từng field từ stage output đã lưu
	for _, s := range stages {
		if s.Status != "completed" || s.Output == nil {
			continue
		}
		var out map[string]any
		if err := json.Unmarshal(s.Output, &out); err != nil {
			continue
		}
		switch Stage(s.Stage) {
		case StageAnalysis:
			req.Analysis = out
		case StagePlanning:
			req.Plan = out
		case StageCharacters:
			req.Characters = out
		case StageLocations:
			req.Locations = out
		case StageSegmentation:
			if rawClips, ok := out["clips"]; ok {
				if b, err := json.Marshal(rawClips); err == nil {
					var clips []ClipData
					if err := json.Unmarshal(b, &clips); err == nil {
						req.Clips = clips
					}
				}
			}
		case StageScreenplay:
			if rawSps, ok := out["screenplays"]; ok {
				if b, err := json.Marshal(rawSps); err == nil {
					var sps []ScreenplayData
					if err := json.Unmarshal(b, &sps); err == nil {
						req.Screenplays = sps
					}
				}
			}
		case StageStoryboard:
			req.Storyboard = out
			if rawSbs, ok := out["storyboards"]; ok {
				if b, err := json.Marshal(rawSbs); err == nil {
					var sbs []map[string]any
					if err := json.Unmarshal(b, &sbs); err == nil {
						req.Storyboards = sbs
					}
				}
			}
		case StageMediaGen:
			req.Media = out
		case StageVoice:
			req.Voices = out
		}
	}

	return req, nil
}

// emit forwards a ProgressEvent to all registered listeners.
func (p *StepByStepPipeline) emit(event ProgressEvent) {
	for _, l := range p.listeners {
		l(event)
	}
}

// RetryStage re-runs a specific stage in step-by-step mode.
// It delegates to inner Pipeline.RetryStage and updates run state accordingly.
func (p *StepByStepPipeline) RetryStage(ctx context.Context, projectID string, stage Stage, inputOverride map[string]any) error {
	if p.checkpoint == nil {
		return fmt.Errorf("checkpoint store not available")
	}

	// Reset run state to point at the retried stage (without overwriting story metadata)
	_ = p.checkpoint.UpdateRunStageStatus(ctx, projectID, stage, StepStatusRunning, "")

	err := p.inner.RetryStage(ctx, projectID, stage, inputOverride)
	if err != nil {
		_ = p.checkpoint.UpdateRunStageStatus(ctx, projectID, stage, StepStatusFailed, err.Error())
		return err
	}

	// After retry success, set awaiting_approval again
	_ = p.checkpoint.UpdateRunStageStatus(ctx, projectID, stage, StepStatusAwaitingApproval, "")
	p.emit(ProgressEvent{
		ProjectID:   projectID,
		Stage:       stage,
		StageIndex:  stageIndexOf(stage),
		TotalStages: len(stageOrder),
		Status:      "awaiting_approval",
		Message:     fmt.Sprintf("Retry of %s complete — review and approve to continue", stage),
		Timestamp:   time.Now(),
	})
	return nil
}
