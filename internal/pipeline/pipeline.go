// Package pipeline defines the full filmmaking workflow.
// Story → Director → Characters → Locations → Storyboard → Media → Voice → Output
package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/uni-ai-studio/waoo-studio/internal/agent"
)

// Stage represents a step in the filmmaking pipeline.
type Stage string

const (
	StageAnalysis     Stage = "analysis"     // Director analyzes story
	StagePlanning     Stage = "planning"     // Director plans pipeline
	StageCharacters   Stage = "characters"   // Character agent designs characters
	StageLocations    Stage = "locations"    // Location agent designs locations
	StageStoryboard   Stage = "storyboard"   // Storyboard agent creates panels
	StageMediaGen     Stage = "media_gen"    // Media agent generates images/videos
	StageQualityCheck Stage = "quality_check" // Quality gate evaluates output
	StageVoice        Stage = "voice"        // Voice agent generates TTS + lip sync
	StageAssembly     Stage = "assembly"     // Final assembly
	StageComplete     Stage = "complete"
)

// Pipeline orchestrates the full filmmaking workflow.
type Pipeline struct {
	bus        agent.MessageBus
	logger     *slog.Logger
	listeners  []ProgressListener
	checkpoint *CheckpointStore
}

// ProgressListener receives pipeline progress updates (for SSE, webhooks, etc.)
type ProgressListener func(event ProgressEvent)

// ProgressEvent describes a pipeline progress update.
type ProgressEvent struct {
	ProjectID   string    `json:"projectId"`
	Stage       Stage     `json:"stage"`
	StageIndex  int       `json:"stageIndex"`
	TotalStages int       `json:"totalStages"`
	Status      string    `json:"status"` // "started", "completed", "failed"
	Message     string    `json:"message"`
	Data        any       `json:"data,omitempty"`
	Timestamp   time.Time `json:"timestamp"`
}

// NewPipeline creates a new filmmaking pipeline.
func NewPipeline(bus agent.MessageBus, logger *slog.Logger) *Pipeline {
	return &Pipeline{
		bus:    bus,
		logger: logger.With("component", "pipeline"),
	}
}

// OnProgress registers a listener for progress events.
func (p *Pipeline) OnProgress(listener ProgressListener) {
	p.listeners = append(p.listeners, listener)
}

// SetCheckpointStore enables checkpoint persistence for resume support.
func (p *Pipeline) SetCheckpointStore(cs *CheckpointStore) {
	p.checkpoint = cs
}

// Run executes the full filmmaking pipeline for a project.
func (p *Pipeline) Run(ctx context.Context, req PipelineRequest) (*PipelineResult, error) {
	projectID := req.ProjectID
	result := &PipelineResult{
		ProjectID: projectID,
		StartedAt: time.Now(),
		Stages:    make(map[Stage]StageResult),
	}

	// Sequential stages before the parallel section
	seqStages := []struct {
		stage Stage
		fn    func(context.Context, *PipelineRequest) (*StageResult, error)
	}{
		{StageAnalysis, p.runAnalysis},
		{StagePlanning, p.runPlanning},
	}

	totalStages := 9 // fixed count for SSE progress reporting
	stageIdx := 0

	for _, s := range seqStages {
		p.emit(ProgressEvent{
			ProjectID:   projectID,
			Stage:       s.stage,
			StageIndex:  stageIdx,
			TotalStages: totalStages,
			Status:      "started",
			Message:     fmt.Sprintf("Starting %s", s.stage),
			Timestamp:   time.Now(),
		})

		stageResult, err := s.fn(ctx, &req)
		if err != nil {
			p.emit(ProgressEvent{
				ProjectID:   projectID,
				Stage:       s.stage,
				StageIndex:  stageIdx,
				TotalStages: totalStages,
				Status:      "failed",
				Message:     err.Error(),
				Timestamp:   time.Now(),
			})
			result.Error = err.Error()
			return result, err
		}

		result.Stages[s.stage] = *stageResult
		p.emit(ProgressEvent{
			ProjectID:   projectID,
			Stage:       s.stage,
			StageIndex:  stageIdx,
			TotalStages: totalStages,
			Status:      "completed",
			Message:     stageResult.Summary,
			Data:        stageResult.Data,
			Timestamp:   time.Now(),
		})
		if p.checkpoint != nil {
			_ = p.checkpoint.Save(ctx, projectID, stageIdx, s.stage, &req)
		}
		stageIdx++
	}

	// Characters and Locations run in parallel — they both only need Analysis
	p.emit(ProgressEvent{ProjectID: projectID, Stage: StageCharacters, StageIndex: stageIdx, TotalStages: totalStages, Status: "started", Message: "Starting characters & locations (parallel)", Timestamp: time.Now()})
	charResult, locResult, err := p.runCharactersAndLocations(ctx, &req)
	if err != nil {
		p.emit(ProgressEvent{ProjectID: projectID, Stage: StageCharacters, StageIndex: stageIdx, TotalStages: totalStages, Status: "failed", Message: err.Error(), Timestamp: time.Now()})
		result.Error = err.Error()
		return result, err
	}
	result.Stages[StageCharacters] = *charResult
	result.Stages[StageLocations] = *locResult
	p.emit(ProgressEvent{ProjectID: projectID, Stage: StageLocations, StageIndex: stageIdx + 1, TotalStages: totalStages, Status: "completed", Message: "Characters & locations complete", Timestamp: time.Now()})
	stageIdx += 2

	// Sequential stages after the parallel section
	remainingStages := []struct {
		stage Stage
		fn    func(context.Context, *PipelineRequest) (*StageResult, error)
	}{
		{StageStoryboard, p.runStoryboard},
		{StageMediaGen, p.runMediaGeneration},
		{StageQualityCheck, p.runQualityCheck},
		{StageVoice, p.runVoice},
		{StageAssembly, p.runAssembly},
	}

	for _, s := range remainingStages {
		p.emit(ProgressEvent{
			ProjectID:   projectID,
			Stage:       s.stage,
			StageIndex:  stageIdx,
			TotalStages: totalStages,
			Status:      "started",
			Message:     fmt.Sprintf("Starting %s", s.stage),
			Timestamp:   time.Now(),
		})

		stageResult, err := s.fn(ctx, &req)
		if err != nil {
			p.emit(ProgressEvent{
				ProjectID:   projectID,
				Stage:       s.stage,
				StageIndex:  stageIdx,
				TotalStages: totalStages,
				Status:      "failed",
				Message:     err.Error(),
				Timestamp:   time.Now(),
			})
			result.Error = err.Error()
			return result, err
		}

		result.Stages[s.stage] = *stageResult
		p.emit(ProgressEvent{
			ProjectID:   projectID,
			Stage:       s.stage,
			StageIndex:  stageIdx,
			TotalStages: totalStages,
			Status:      "completed",
			Message:     stageResult.Summary,
			Data:        stageResult.Data,
			Timestamp:   time.Now(),
		})
		if p.checkpoint != nil {
			_ = p.checkpoint.Save(ctx, projectID, stageIdx, s.stage, &req)
		}
		stageIdx++
	}

	result.CompletedAt = ptrTime(time.Now())
	return result, nil
}

// runCharactersAndLocations runs character and location analysis concurrently.
// Both stages only depend on req.Analysis from the planning stage.
func (p *Pipeline) runCharactersAndLocations(ctx context.Context, req *PipelineRequest) (*StageResult, *StageResult, error) {
	var (
		charResult *StageResult
		locResult  *StageResult
		charErr    error
		locErr     error
		wg         sync.WaitGroup
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		charResult, charErr = p.runCharacters(ctx, req)
	}()
	go func() {
		defer wg.Done()
		locResult, locErr = p.runLocations(ctx, req)
	}()
	wg.Wait()

	if charErr != nil {
		return nil, nil, fmt.Errorf("character design: %w", charErr)
	}
	if locErr != nil {
		return nil, nil, fmt.Errorf("location design: %w", locErr)
	}
	return charResult, locResult, nil
}

// --- Stage implementations ---

func (p *Pipeline) runAnalysis(ctx context.Context, req *PipelineRequest) (*StageResult, error) {
	resp, err := p.bus.Request(ctx, agent.Message{
		To:      "director",
		SkillID: "analyze_story",
		Payload: map[string]any{
			"story":     req.Story,
			"inputType": req.InputType,
		},
	}, 60*time.Second)
	if err != nil {
		return nil, fmt.Errorf("story analysis: %w", err)
	}

	// Store analysis result for downstream stages
	req.Analysis = resp.Output

	return &StageResult{
		Summary: "Story analysis complete",
		Data:    resp.Output,
	}, nil
}

func (p *Pipeline) runPlanning(ctx context.Context, req *PipelineRequest) (*StageResult, error) {
	resp, err := p.bus.Request(ctx, agent.Message{
		To:      "director",
		SkillID: "plan_pipeline",
		Payload: map[string]any{
			"analysis":  req.Analysis,
			"budget":    req.Budget,
			"quality":   req.QualityLevel,
		},
	}, 30*time.Second)
	if err != nil {
		return nil, fmt.Errorf("pipeline planning: %w", err)
	}

	req.Plan = resp.Output
	return &StageResult{Summary: "Pipeline planned", Data: resp.Output}, nil
}

func (p *Pipeline) runCharacters(ctx context.Context, req *PipelineRequest) (*StageResult, error) {
	resp, err := p.bus.Request(ctx, agent.Message{
		To:      "character",
		SkillID: "analyze_characters",
		Payload: map[string]any{
			"story":    req.Story,
			"analysis": req.Analysis,
		},
	}, 60*time.Second)
	if err != nil {
		return nil, fmt.Errorf("character design: %w", err)
	}

	req.Characters = resp.Output
	return &StageResult{Summary: "Characters designed", Data: resp.Output}, nil
}

func (p *Pipeline) runLocations(ctx context.Context, req *PipelineRequest) (*StageResult, error) {
	resp, err := p.bus.Request(ctx, agent.Message{
		To:      "location",
		SkillID: "analyze_locations",
		Payload: map[string]any{
			"story":    req.Story,
			"analysis": req.Analysis,
		},
	}, 60*time.Second)
	if err != nil {
		return nil, fmt.Errorf("location design: %w", err)
	}

	req.Locations = resp.Output
	return &StageResult{Summary: "Locations designed", Data: resp.Output}, nil
}

func (p *Pipeline) runStoryboard(ctx context.Context, req *PipelineRequest) (*StageResult, error) {
	resp, err := p.bus.Request(ctx, agent.Message{
		To:      "storyboard",
		SkillID: "create_storyboard",
		Payload: map[string]any{
			"story":      req.Story,
			"analysis":   req.Analysis,
			"characters": req.Characters,
			"locations":  req.Locations,
		},
	}, 120*time.Second)
	if err != nil {
		return nil, fmt.Errorf("storyboard creation: %w", err)
	}

	req.Storyboard = resp.Output
	return &StageResult{Summary: "Storyboard created", Data: resp.Output}, nil
}

func (p *Pipeline) runMediaGeneration(ctx context.Context, req *PipelineRequest) (*StageResult, error) {
	resp, err := p.bus.Request(ctx, agent.Message{
		To:      "media",
		SkillID: "generate_batch",
		Payload: map[string]any{
			"storyboard": req.Storyboard,
			"characters": req.Characters,
			"locations":  req.Locations,
		},
	}, 300*time.Second) // 5min for batch generation
	if err != nil {
		return nil, fmt.Errorf("media generation: %w", err)
	}

	req.Media = resp.Output
	return &StageResult{Summary: "Media generated", Data: resp.Output}, nil
}

func (p *Pipeline) runQualityCheck(ctx context.Context, req *PipelineRequest) (*StageResult, error) {
	resp, err := p.bus.Request(ctx, agent.Message{
		To:      "media",
		SkillID: "quality_review",
		Payload: map[string]any{
			"media":      req.Media,
			"storyboard": req.Storyboard,
		},
	}, 120*time.Second)
	if err != nil {
		return nil, fmt.Errorf("quality check: %w", err)
	}

	return &StageResult{Summary: "Quality verified", Data: resp.Output}, nil
}

func (p *Pipeline) runVoice(ctx context.Context, req *PipelineRequest) (*StageResult, error) {
	resp, err := p.bus.Request(ctx, agent.Message{
		To:      "voice",
		SkillID: "analyze_voices",
		Payload: map[string]any{
			"story":      req.Story,
			"characters": req.Characters,
			"storyboard": req.Storyboard,
		},
	}, 120*time.Second)
	if err != nil {
		return nil, fmt.Errorf("voice generation: %w", err)
	}

	req.Voices = resp.Output
	return &StageResult{Summary: "Voices generated", Data: resp.Output}, nil
}

func (p *Pipeline) runAssembly(_ context.Context, req *PipelineRequest) (*StageResult, error) {
	// Assembly combines all generated assets into final output
	return &StageResult{
		Summary: "Assembly complete",
		Data: map[string]any{
			"projectId":  req.ProjectID,
			"panels":     req.Storyboard,
			"media":      req.Media,
			"voices":     req.Voices,
			"characters": req.Characters,
			"locations":  req.Locations,
		},
	}, nil
}

func (p *Pipeline) emit(event ProgressEvent) {
	p.logger.Info("pipeline progress",
		"projectId", event.ProjectID,
		"stage", event.Stage,
		"status", event.Status,
		"message", event.Message,
	)
	for _, listener := range p.listeners {
		listener(event)
	}
}

// --- Types ---

// PipelineRequest is the input for a filmmaking pipeline run.
type PipelineRequest struct {
	ProjectID    string `json:"projectId"`
	Story        string `json:"story"`
	InputType    string `json:"inputType"` // novel, script, outline
	Budget       string `json:"budget"`    // low, medium, high
	QualityLevel string `json:"qualityLevel"` // draft, standard, premium

	// Intermediate data populated during pipeline execution
	Analysis   map[string]any `json:"-"`
	Plan       map[string]any `json:"-"`
	Characters map[string]any `json:"-"`
	Locations  map[string]any `json:"-"`
	Storyboard map[string]any `json:"-"`
	Media      map[string]any `json:"-"`
	Voices     map[string]any `json:"-"`
}

// PipelineResult is the output of a pipeline run.
type PipelineResult struct {
	ProjectID   string                `json:"projectId"`
	StartedAt   time.Time             `json:"startedAt"`
	CompletedAt *time.Time            `json:"completedAt,omitempty"`
	Stages      map[Stage]StageResult `json:"stages"`
	Error       string                `json:"error,omitempty"`
}

// StageResult holds the output from one pipeline stage.
type StageResult struct {
	Summary string         `json:"summary"`
	Data    map[string]any `json:"data,omitempty"`
}

func ptrTime(t time.Time) *time.Time { return &t }
