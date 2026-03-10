// Package pipeline defines the full filmmaking workflow.
// Story → Director → Characters → Locations → Segmentation → Screenplay → Storyboard → Media → Voice → Output
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/uni-ai-studio/waoo-studio/internal/agent"
)

// Stage represents a step in the filmmaking pipeline.
type Stage string

const (
	StageAnalysis     Stage = "analysis"      // Director analyzes story
	StagePlanning     Stage = "planning"      // Director plans pipeline
	StageCharacters   Stage = "characters"    // Character agent designs characters
	StageLocations    Stage = "locations"     // Location agent designs locations
	StageSegmentation Stage = "segmentation" // Director chia story → N clips
	StageScreenplay   Stage = "screenplay"   // Director convert từng clip → screenplay JSON
	StageStoryboard   Stage = "storyboard"   // Storyboard agent creates panels
	StageMediaGen     Stage = "media_gen"    // Media agent generates images/videos
	StageQualityCheck Stage = "quality_check" // Quality gate evaluates output
	StageVoice        Stage = "voice"         // Voice agent generates TTS + lip sync
	StageAssembly     Stage = "assembly"      // Final assembly
	StageComplete     Stage = "complete"
)

// Pipeline orchestrates the full filmmaking workflow.
type Pipeline struct {
	bus        agent.MessageBus
	logger     *slog.Logger
	listeners  []ProgressListener
	checkpoint *CheckpointStore
	falKey     string
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

// ClipData là một đoạn text đã được Director phân đoạn
type ClipData struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Content string `json:"content"`
	Order   int    `json:"order"`
}

// ScreenplayData là kịch bản được convert từ một clip
type ScreenplayData struct {
	ClipID     string `json:"clipId"`
	Title      string `json:"title"`
	Screenplay string `json:"screenplay"` // JSON string từ LLM
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

// SetFALKey configures the FAL API key for inline media polling.
func (p *Pipeline) SetFALKey(key string) {
	p.falKey = key
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

	totalStages := 11 // fixed count for SSE progress reporting
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
		if p.checkpoint != nil {
			input := buildStageInput(s.stage, &req)
			_ = p.checkpoint.SaveStage(ctx, projectID, stageIdx, s.stage, "running", input, nil, nil)
		}

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
			if p.checkpoint != nil {
				_ = p.checkpoint.SaveStage(ctx, projectID, stageIdx, s.stage, "failed", nil, nil, err)
			}
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
			_ = p.checkpoint.SaveStage(ctx, projectID, stageIdx, s.stage, "completed", nil, stageResult.Data, nil)
		}
		stageIdx++
	}

	// Characters and Locations run in parallel — they both only need Analysis
	p.emit(ProgressEvent{ProjectID: projectID, Stage: StageCharacters, StageIndex: stageIdx, TotalStages: totalStages, Status: "started", Message: "Starting characters & locations (parallel)", Timestamp: time.Now()})
	if p.checkpoint != nil {
		_ = p.checkpoint.SaveStage(ctx, projectID, stageIdx, StageCharacters, "running", buildStageInput(StageCharacters, &req), nil, nil)
		_ = p.checkpoint.SaveStage(ctx, projectID, stageIdx+1, StageLocations, "running", buildStageInput(StageLocations, &req), nil, nil)
	}
	charResult, locResult, err := p.runCharactersAndLocations(ctx, &req)
	if err != nil {
		p.emit(ProgressEvent{ProjectID: projectID, Stage: StageCharacters, StageIndex: stageIdx, TotalStages: totalStages, Status: "failed", Message: err.Error(), Timestamp: time.Now()})
		if p.checkpoint != nil {
			_ = p.checkpoint.SaveStage(ctx, projectID, stageIdx, StageCharacters, "failed", nil, nil, err)
		}
		result.Error = err.Error()
		return result, err
	}
	result.Stages[StageCharacters] = *charResult
	result.Stages[StageLocations] = *locResult
	p.emit(ProgressEvent{ProjectID: projectID, Stage: StageLocations, StageIndex: stageIdx + 1, TotalStages: totalStages, Status: "completed", Message: "Characters & locations complete", Timestamp: time.Now()})
	if p.checkpoint != nil {
		_ = p.checkpoint.Save(ctx, projectID, stageIdx+1, StageLocations, &req)
		_ = p.checkpoint.SaveStage(ctx, projectID, stageIdx, StageCharacters, "completed", nil, charResult.Data, nil)
		_ = p.checkpoint.SaveStage(ctx, projectID, stageIdx+1, StageLocations, "completed", nil, locResult.Data, nil)
	}
	stageIdx += 2

	// Sequential stages after the parallel section
	remainingStages := []struct {
		stage Stage
		fn    func(context.Context, *PipelineRequest) (*StageResult, error)
	}{
		{StageSegmentation, p.runSegmentation},
		{StageScreenplay, p.runScreenplays},
		{StageStoryboard, p.runStoryboards},
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
		if p.checkpoint != nil {
			input := buildStageInput(s.stage, &req)
			_ = p.checkpoint.SaveStage(ctx, projectID, stageIdx, s.stage, "running", input, nil, nil)
		}

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
			if p.checkpoint != nil {
				_ = p.checkpoint.SaveStage(ctx, projectID, stageIdx, s.stage, "failed", nil, nil, err)
			}
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
			_ = p.checkpoint.SaveStage(ctx, projectID, stageIdx, s.stage, "completed", nil, stageResult.Data, nil)
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
			"analysis": req.Analysis,
			"budget":   req.Budget,
			"quality":  req.QualityLevel,
		},
	}, 120*time.Second)
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

func (p *Pipeline) runSegmentation(ctx context.Context, req *PipelineRequest) (*StageResult, error) {
	// Extract characters info từ req.Characters["characters"] (string JSON)
	charsJSON, _ := req.Characters["characters"].(string)
	locsJSON, _ := req.Locations["locations"].(string)

	resp, err := p.bus.Request(ctx, agent.Message{
		To:      "director",
		SkillID: "segment_clips",
		Payload: map[string]any{
			"input":                  req.Story,
			"charactersLibName":      "characters",
			"locationsLibName":       "locations",
			"charactersIntroduction": charsJSON,
		},
	}, 120*time.Second)
	if err != nil {
		return nil, fmt.Errorf("segmentation: %w", err)
	}

	// Parse clips từ resp.Output["clips"] (string JSON)
	clipsRaw, _ := resp.Output["clips"].(string)
	clipsRaw = stripCodeFences(clipsRaw)

	var clips []ClipData
	if err := json.Unmarshal([]byte(clipsRaw), &clips); err != nil {
		// Fallback: tạo 1 clip duy nhất từ toàn bộ story
		p.logger.Warn("clip parse failed, using single clip fallback", "error", err)
		clips = []ClipData{{ID: "clip-1", Title: "Full Story", Content: req.Story, Order: 1}}
	}

	// Đảm bảo mỗi clip có ID
	for i := range clips {
		if clips[i].ID == "" {
			clips[i].ID = fmt.Sprintf("clip-%d", i+1)
		}
		clips[i].Order = i + 1
	}

	req.Clips = clips
	_ = locsJSON // available for future use

	return &StageResult{
		Summary: fmt.Sprintf("Story segmented into %d clips", len(clips)),
		Data: map[string]any{
			"clips":     clips,
			"clipCount": len(clips),
		},
	}, nil
}

func (p *Pipeline) runScreenplays(ctx context.Context, req *PipelineRequest) (*StageResult, error) {
	if len(req.Clips) == 0 {
		// Fallback nếu segmentation không có clips
		req.Screenplays = []ScreenplayData{{ClipID: "clip-1", Title: "Full Story", Screenplay: req.Story}}
		return &StageResult{Summary: "Screenplay fallback (no clips)", Data: map[string]any{"count": 1}}, nil
	}

	charsJSON, _ := req.Characters["characters"].(string)

	var mu sync.Mutex
	var wg sync.WaitGroup
	screenplays := make([]ScreenplayData, len(req.Clips))

	for i, clip := range req.Clips {
		wg.Add(1)
		go func(idx int, c ClipData) {
			defer wg.Done()
			resp, err := p.bus.Request(ctx, agent.Message{
				To:      "director",
				SkillID: "convert_screenplay",
				Payload: map[string]any{
					"clipId":                 c.ID,
					"clipContent":            c.Content,
					"charactersLibName":      "characters",
					"locationsLibName":       "locations",
					"charactersIntroduction": charsJSON,
				},
			}, 120*time.Second)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				p.logger.Error("screenplay conversion failed", "clipId", c.ID, "error", err)
				screenplays[idx] = ScreenplayData{ClipID: c.ID, Title: c.Title, Screenplay: c.Content}
				return
			}
			sp, _ := resp.Output["screenplay"].(string)
			screenplays[idx] = ScreenplayData{ClipID: c.ID, Title: c.Title, Screenplay: sp}
		}(i, clip)
	}
	wg.Wait()

	req.Screenplays = screenplays
	return &StageResult{
		Summary: fmt.Sprintf("Converted %d screenplays", len(screenplays)),
		Data: map[string]any{
			"screenplays": screenplays,
			"count":       len(screenplays),
		},
	}, nil
}

func (p *Pipeline) runStoryboards(ctx context.Context, req *PipelineRequest) (*StageResult, error) {
	// Extract characters và locations đúng field names mà storyboard agent cần
	charsJSON, _ := req.Characters["characters"].(string)
	charsJSON = stripCodeFences(charsJSON)
	locsJSON, _ := req.Locations["locations"].(string)
	locsJSON = stripCodeFences(locsJSON)

	screenplays := req.Screenplays
	if len(screenplays) == 0 {
		// Fallback nếu không có screenplays
		screenplays = []ScreenplayData{{ClipID: "clip-1", Title: "Full Story", Screenplay: req.Story}}
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	storyboards := make([]map[string]any, len(screenplays))

	for i, sp := range screenplays {
		wg.Add(1)
		go func(idx int, screenplay ScreenplayData) {
			defer wg.Done()

			// Tìm clip tương ứng để lấy title
			clipTitle := screenplay.Title
			for _, c := range req.Clips {
				if c.ID == screenplay.ClipID {
					clipTitle = c.Title
					break
				}
			}

			resp, err := p.bus.Request(ctx, agent.Message{
				To:      "storyboard",
				SkillID: "create_storyboard",
				Payload: map[string]any{
					// Fields mà storyboard agent thực sự đọc:
					"clipJson":                  fmt.Sprintf(`{"clipId":"%s","title":"%s"}`, screenplay.ClipID, clipTitle),
					"clipContent":               screenplay.Screenplay,
					"charactersLibName":         "characters",
					"locationsLibName":          "locations",
					"charactersIntroduction":    charsJSON,
					"charactersAppearanceList":  charsJSON,
					"charactersFullDescription": charsJSON,
					"charactersInfo":            charsJSON,
					"charactersAgeGender":       charsJSON,
					"locationsDescription":      locsJSON,
					"panelCount":                float64(9),
				},
			}, 180*time.Second)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				p.logger.Error("storyboard creation failed", "clipId", screenplay.ClipID, "error", err)
				storyboards[idx] = map[string]any{
					"clipId": screenplay.ClipID,
					"title":  clipTitle,
					"error":  err.Error(),
					"panels": []any{},
				}
				return
			}

			out := resp.Output
			if out == nil {
				out = map[string]any{}
			}
			out["clipId"] = screenplay.ClipID
			out["title"] = clipTitle
			storyboards[idx] = out
		}(i, sp)
	}
	wg.Wait()

	req.Storyboards = storyboards
	// Gộp tất cả panels thành 1 storyboard flat cho backward compat
	req.Storyboard = map[string]any{
		"storyboards": storyboards,
		"clipCount":   len(storyboards),
	}

	totalPanels := 0
	for _, sb := range storyboards {
		if panels, ok := sb["panels"].([]any); ok {
			totalPanels += len(panels)
		}
	}

	return &StageResult{
		Summary: fmt.Sprintf("Created %d storyboards, %d total panels", len(storyboards), totalPanels),
		Data: map[string]any{
			"storyboards": storyboards,
			"clipCount":   len(storyboards),
			"panelCount":  totalPanels,
		},
	}, nil
}

func (p *Pipeline) runMediaGeneration(ctx context.Context, req *PipelineRequest) (*StageResult, error) {
	// Build combined storyboard từ nhiều storyboards
	combinedPanels := []any{}
	for _, sb := range req.Storyboards {
		if panels, ok := sb["panels"].([]any); ok {
			combinedPanels = append(combinedPanels, panels...)
		}
	}
	effectiveStoryboard := map[string]any{"panels": combinedPanels}
	if len(combinedPanels) == 0 {
		effectiveStoryboard = p.generateFallbackPanels(req.Story)
	}

	resp, err := p.bus.Request(ctx, agent.Message{
		To:      "media",
		SkillID: "generate_batch",
		Payload: map[string]any{
			"storyboard": effectiveStoryboard,
			"characters": req.Characters,
			"locations":  req.Locations,
		},
	}, 300*time.Second) // 5min for batch generation
	if err != nil {
		return nil, fmt.Errorf("media generation: %w", err)
	}

	// Resolve async FAL externalIds → actual image URLs
	if p.falKey != "" {
		resp.Output = p.resolveMediaURLs(ctx, resp.Output)
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
	InputType    string `json:"inputType"`    // novel, script, outline
	Budget       string `json:"budget"`       // low, medium, high
	QualityLevel string `json:"qualityLevel"` // draft, standard, premium
	Mode         string `json:"mode"`         // autopilot, step_by_step

	// Intermediate data populated during pipeline execution
	Analysis   map[string]any `json:"-"`
	Plan       map[string]any `json:"-"`
	Characters map[string]any `json:"-"`
	Locations  map[string]any `json:"-"`
	Storyboard map[string]any `json:"-"`
	Media      map[string]any `json:"-"`
	Voices     map[string]any `json:"-"`

	// New fields for segmentation/screenplay/storyboards flow
	Clips       []ClipData       `json:"-"` // Populated by runSegmentation
	Screenplays []ScreenplayData `json:"-"` // Populated by runScreenplays
	Storyboards []map[string]any `json:"-"` // Populated by runStoryboards (một per clip)
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

// hasPanels checks if storyboard output contains a parsed panels array.
func hasPanels(storyboard map[string]any) bool {
	if storyboard == nil {
		return false
	}
	panels, ok := storyboard["panels"].([]any)
	return ok && len(panels) > 0
}

// generateFallbackPanels creates simple scene panels from story text
// when the storyboard LLM fails to produce structured JSON.
func (p *Pipeline) generateFallbackPanels(story string) map[string]any {
	// Split story into sentences and use each as a panel prompt
	sentences := splitIntoScenes(story, 5)
	panels := make([]map[string]any, len(sentences))
	for i, s := range sentences {
		panels[i] = map[string]any{
			"index":       i,
			"imagePrompt": fmt.Sprintf("Cinematic scene, film still: %s. High quality, detailed, atmospheric lighting.", s),
			"description": s,
		}
	}
	return map[string]any{
		"panels":     panels,
		"panelCount": len(panels),
		"fallback":   true,
	}
}

// splitIntoScenes splits text into at most maxScenes chunks by sentence boundaries.
func splitIntoScenes(text string, maxScenes int) []string {
	// Simple split by period/newline
	var scenes []string
	current := ""
	for _, r := range text {
		current += string(r)
		if (r == '.' || r == '\n') && len(current) > 20 {
			scenes = append(scenes, strings.TrimSpace(current))
			current = ""
		}
	}
	if strings.TrimSpace(current) != "" {
		scenes = append(scenes, strings.TrimSpace(current))
	}
	if len(scenes) == 0 {
		return []string{text}
	}
	// Merge if too many
	if len(scenes) > maxScenes {
		merged := make([]string, maxScenes)
		chunkSize := len(scenes) / maxScenes
		for i := 0; i < maxScenes; i++ {
			start := i * chunkSize
			end := start + chunkSize
			if i == maxScenes-1 {
				end = len(scenes)
			}
			merged[i] = strings.Join(scenes[start:end], " ")
		}
		return merged
	}
	return scenes
}

// stripCodeFences removes markdown code fence wrappers (```json ... ```) from a string.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// Remove first line (```json or ```)
		if idx := strings.Index(s, "\n"); idx != -1 {
			s = s[idx+1:]
		}
	}
	if strings.HasSuffix(s, "```") {
		s = s[:strings.LastIndex(s, "```")]
	}
	return strings.TrimSpace(s)
}

// buildStageInput returns the input payload for a given stage based on the
// current pipeline request state. Called just before each stage executes so
// that upstream results populated into req are included.
func buildStageInput(stage Stage, req *PipelineRequest) map[string]any {
	switch stage {
	case StageAnalysis:
		return map[string]any{"story": req.Story, "inputType": req.InputType}
	case StagePlanning:
		return map[string]any{"analysis": req.Analysis, "budget": req.Budget, "quality": req.QualityLevel}
	case StageCharacters:
		return map[string]any{"story": req.Story, "analysis": req.Analysis}
	case StageLocations:
		return map[string]any{"story": req.Story, "analysis": req.Analysis}
	case StageSegmentation:
		return map[string]any{"story": req.Story, "characters": req.Characters, "locations": req.Locations}
	case StageScreenplay:
		return map[string]any{"clips": req.Clips, "characters": req.Characters}
	case StageStoryboard:
		return map[string]any{"screenplays": req.Screenplays, "characters": req.Characters, "locations": req.Locations}
	case StageMediaGen:
		return map[string]any{"storyboard": req.Storyboard, "characters": req.Characters, "locations": req.Locations}
	case StageQualityCheck:
		return map[string]any{"media": req.Media, "storyboard": req.Storyboard}
	case StageVoice:
		return map[string]any{"story": req.Story, "characters": req.Characters, "storyboard": req.Storyboard}
	case StageAssembly:
		return map[string]any{"media": req.Media, "voices": req.Voices, "storyboard": req.Storyboard}
	default:
		return nil
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

// stageIndexMap maps stage names to their index in the pipeline.
var stageIndexMap = map[Stage]int{
	StageAnalysis:     0,
	StagePlanning:     1,
	StageCharacters:   2,
	StageLocations:    3,
	StageSegmentation: 4,
	StageScreenplay:   5,
	StageStoryboard:   6,
	StageMediaGen:     7,
	StageQualityCheck: 8,
	StageVoice:        9,
	StageAssembly:     10,
}

// stageRunner maps each stage to its run function.
// Characters and Locations only run individually on retry (not parallel).
func (p *Pipeline) stageRunner(stage Stage) func(context.Context, *PipelineRequest) (*StageResult, error) {
	switch stage {
	case StageAnalysis:
		return p.runAnalysis
	case StagePlanning:
		return p.runPlanning
	case StageCharacters:
		return p.runCharacters
	case StageLocations:
		return p.runLocations
	case StageSegmentation:
		return p.runSegmentation
	case StageScreenplay:
		return p.runScreenplays
	case StageStoryboard:
		return p.runStoryboards
	case StageMediaGen:
		return p.runMediaGeneration
	case StageQualityCheck:
		return p.runQualityCheck
	case StageVoice:
		return p.runVoice
	case StageAssembly:
		return p.runAssembly
	}
	return nil
}

// RetryStage re-runs a single pipeline stage using the stored input from DB,
// optionally overriding specific fields from inputOverride.
// It emits SSE events identical to Run() and persists the new output.
func (p *Pipeline) RetryStage(ctx context.Context, projectID string, stage Stage, inputOverride map[string]any) error {
	if p.checkpoint == nil {
		return fmt.Errorf("checkpoint store not available")
	}

	runner := p.stageRunner(stage)
	if runner == nil {
		return fmt.Errorf("unknown stage: %s", stage)
	}

	stageIdx := stageIndexMap[stage]
	totalStages := 11

	// Load stored input from DB
	storedInput, err := p.checkpoint.GetStageInput(ctx, projectID, stage)
	if err != nil {
		return fmt.Errorf("load stage input: %w", err)
	}
	if storedInput == nil {
		storedInput = map[string]any{}
	}

	// Merge override on top of stored input
	for k, v := range inputOverride {
		storedInput[k] = v
	}

	// Persist the merged input before running
	_ = p.checkpoint.UpdateStageInput(ctx, projectID, stage, storedInput)

	// Build a minimal PipelineRequest from the merged input
	req := &PipelineRequest{ProjectID: projectID}
	if v, ok := storedInput["story"].(string); ok {
		req.Story = v
	}
	if v, ok := storedInput["inputType"].(string); ok {
		req.InputType = v
	}
	if v, ok := storedInput["budget"].(string); ok {
		req.Budget = v
	}
	if v, ok := storedInput["quality"].(string); ok {
		req.QualityLevel = v
	}
	if v, ok := storedInput["analysis"].(map[string]any); ok {
		req.Analysis = v
	}
	if v, ok := storedInput["characters"].(map[string]any); ok {
		req.Characters = v
	}
	if v, ok := storedInput["locations"].(map[string]any); ok {
		req.Locations = v
	}
	if v, ok := storedInput["storyboard"].(map[string]any); ok {
		req.Storyboard = v
	}
	if v, ok := storedInput["media"].(map[string]any); ok {
		req.Media = v
	}
	if v, ok := storedInput["voices"].(map[string]any); ok {
		req.Voices = v
	}

	// Emit "started"
	p.emit(ProgressEvent{
		ProjectID:   projectID,
		Stage:       stage,
		StageIndex:  stageIdx,
		TotalStages: totalStages,
		Status:      "started",
		Message:     fmt.Sprintf("Retrying %s", stage),
		Timestamp:   time.Now(),
	})
	_ = p.checkpoint.SaveStage(ctx, projectID, stageIdx, stage, "running", storedInput, nil, nil)

	// Run the stage
	result, err := runner(ctx, req)
	if err != nil {
		p.emit(ProgressEvent{
			ProjectID:   projectID,
			Stage:       stage,
			StageIndex:  stageIdx,
			TotalStages: totalStages,
			Status:      "failed",
			Message:     err.Error(),
			Timestamp:   time.Now(),
		})
		_ = p.checkpoint.SaveStage(ctx, projectID, stageIdx, stage, "failed", nil, nil, err)
		return fmt.Errorf("stage %s failed: %w", stage, err)
	}

	// Resolve FAL media URLs if applicable
	if stage == StageMediaGen && p.falKey != "" {
		result.Data = p.resolveMediaURLs(ctx, result.Data)
	}

	p.emit(ProgressEvent{
		ProjectID:   projectID,
		Stage:       stage,
		StageIndex:  stageIdx,
		TotalStages: totalStages,
		Status:      "completed",
		Message:     result.Summary,
		Data:        result.Data,
		Timestamp:   time.Now(),
	})
	_ = p.checkpoint.SaveStage(ctx, projectID, stageIdx, stage, "completed", nil, result.Data, nil)

	return nil
}

// resolveMediaURLs walks through media output and replaces async externalIds with real URLs.
func (p *Pipeline) resolveMediaURLs(ctx context.Context, output map[string]any) map[string]any {
	results, ok := output["results"].([]any)
	if !ok {
		return output
	}

	resolved := make([]any, len(results))
	for i, item := range results {
		panelResult, ok := item.(map[string]any)
		if !ok {
			resolved[i] = item
			continue
		}

		result, ok := panelResult["result"].(map[string]any)
		if !ok {
			resolved[i] = panelResult
			continue
		}

		externalID, _ := result["externalId"].(string)
		isAsync, _ := result["async"].(bool)

		if isAsync && strings.HasPrefix(externalID, "FAL:") {
			imageURL, err := p.pollFALResult(ctx, externalID)
			if err != nil {
				p.logger.Warn("FAL poll failed", "externalId", externalID, "error", err)
				panelResult["imageUrl"] = ""
				panelResult["error"] = err.Error()
			} else {
				panelResult["imageUrl"] = imageURL
				panelResult["status"] = "completed"
			}
		}
		resolved[i] = panelResult
	}

	output["results"] = resolved
	return output
}

// pollFALResult polls the FAL queue until the task completes and returns the image URL.
// ExternalID format: FAL:IMAGE:endpoint:requestId
func (p *Pipeline) pollFALResult(ctx context.Context, externalID string) (string, error) {
	parts := strings.SplitN(externalID, ":", 4)
	if len(parts) < 4 {
		return "", fmt.Errorf("invalid FAL external ID: %s", externalID)
	}
	mediaType, endpoint, requestID := parts[1], parts[2], parts[3]

	client := &http.Client{Timeout: 30 * time.Second}
	statusURL := fmt.Sprintf("https://queue.fal.run/%s/requests/%s/status", endpoint, requestID)
	headers := map[string]string{"Authorization": "Key " + p.falKey}

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}

		data, err := falHTTPGet(ctx, client, statusURL, headers)
		if err != nil {
			return "", fmt.Errorf("poll status: %w", err)
		}

		status, _ := data["status"].(string)
		switch status {
		case "COMPLETED":
			resultURL := fmt.Sprintf("https://queue.fal.run/%s/requests/%s", endpoint, requestID)
			resultData, err := falHTTPGet(ctx, client, resultURL, headers)
			if err != nil {
				return "", fmt.Errorf("fetch result: %w", err)
			}
			return extractFALURL(resultData, mediaType), nil
		case "FAILED":
			errMsg, _ := data["error"].(string)
			return "", fmt.Errorf("FAL task failed: %s", errMsg)
		default:
			// IN_QUEUE or IN_PROGRESS — wait and retry
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(3 * time.Second):
			}
		}
	}
}

func falHTTPGet(ctx context.Context, client *http.Client, url string, headers map[string]string) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	var data map[string]any
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	return data, nil
}

func extractFALURL(data map[string]any, mediaType string) string {
	switch mediaType {
	case "IMAGE":
		images, _ := data["images"].([]any)
		if len(images) > 0 {
			img, _ := images[0].(map[string]any)
			url, _ := img["url"].(string)
			return url
		}
	case "VIDEO":
		video, _ := data["video"].(map[string]any)
		url, _ := video["url"].(string)
		return url
	}
	return ""
}
