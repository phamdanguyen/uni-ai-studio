// Package director implements the Director Agent — the orchestrator responsible for
// analyzing input, planning the pipeline, and coordinating other agents.
package director

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/uni-ai-studio/waoo-studio/internal/agent"
	"github.com/uni-ai-studio/waoo-studio/internal/memory"
	"github.com/uni-ai-studio/waoo-studio/lib/prompts"
)

// Agent is the Director — the brain of the filmmaking pipeline.
type Agent struct {
	agent.BaseAgent
}

// New creates a new Director Agent.
func New(bus agent.MessageBus, router agent.ModelRouter, tools agent.ToolRegistry, mem *memory.Store, logger *slog.Logger) *Agent {
	card := agent.AgentCard{
		Name:        "director",
		Version:     "2.0.0",
		Description: "Đạo diễn AI — phân tích truyện, lập kế hoạch sản xuất, điều phối các agent chuyên biệt.",
		Skills: []agent.Skill{
			{ID: "analyze_story", Name: "Phân tích truyện/kịch bản", Description: "Phân tích nhân vật, bối cảnh, cảm xúc, cấu trúc narrative", InputModes: []string{"text/plain", "application/json"}, OutputModes: []string{"application/json"}},
			{ID: "plan_pipeline", Name: "Lập kế hoạch pipeline", Description: "Quyết định strategy phù hợp dựa trên input type", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "orchestrate_workflow", Name: "Điều phối workflow", Description: "Gọi các agents theo kế hoạch, thu thập kết quả", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "split_episodes", Name: "Chia tập cho truyện dài", Description: "Split long text into balanced episodes at natural breakpoints", InputModes: []string{"text/plain"}, OutputModes: []string{"application/json"}},
			{ID: "segment_clips", Name: "Phân đoạn clip", Description: "Split text into clip candidates for downstream conversion", InputModes: []string{"text/plain"}, OutputModes: []string{"application/json"}},
			{ID: "convert_screenplay", Name: "Chuyển đổi kịch bản", Description: "Convert clip text into structured screenplay JSON", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "start_production", Name: "Khởi động sản xuất Autopilot", Description: "Điều phối toàn bộ pipeline A2A từ story đến assembly", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
		},
		Capabilities: agent.Capabilities{
			Streaming:              true,
			StateTransitionHistory: true,
		},
	}

	return &Agent{
		BaseAgent: agent.NewBaseAgent(card, bus, router, tools, mem, logger),
	}
}
// HandleMessage dispatches incoming messages to the appropriate skill handler.
func (a *Agent) HandleMessage(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	a.Logger().Info("handling message", "skill", msg.SkillID, "from", msg.From)

	switch msg.SkillID {
	case "analyze_story":
		return a.analyzeStory(ctx, msg)
	case "plan_pipeline":
		return a.planPipeline(ctx, msg)
	case "orchestrate_workflow":
		return a.orchestrateWorkflow(ctx, msg)
	case "split_episodes":
		return a.splitEpisodes(ctx, msg)
	case "segment_clips":
		return a.segmentClips(ctx, msg)
	case "convert_screenplay":
		return a.convertScreenplay(ctx, msg)
	case "start_production":
		return a.startProduction(ctx, msg)
	default:
		return &agent.TaskResult{
			Status: agent.TaskStatusFailed,
			Error:  fmt.Sprintf("unknown skill: %s", msg.SkillID),
		}, nil
	}
}

// HandleStream implements streaming for long-running orchestrations.
func (a *Agent) HandleStream(ctx context.Context, msg agent.Message, stream chan<- agent.StreamEvent) error {
	defer close(stream)
	stream <- agent.StreamEvent{Type: "status", Payload: "Director analyzing..."}
	result, err := a.HandleMessage(ctx, msg)
	if err != nil {
		return err
	}
	stream <- agent.StreamEvent{Type: "completed", Payload: result.Output}
	return nil
}

// --- Skill Implementations ---

func (a *Agent) analyzeStory(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	story, _ := msg.Payload["story"].(string)
	inputType, _ := msg.Payload["inputType"].(string)
	if inputType == "" {
		inputType = "novel"
	}

	userPrompt := fmt.Sprintf("Input type: %s\n\nContent:\n%s", inputType, story)
	resp, err := a.CallLLM(ctx, agent.TierStandard, analyzeStoryPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("analyze story: %w", err)
	}

	return &agent.TaskResult{
		Status: agent.TaskStatusCompleted,
		Output: map[string]any{
			"analysis": resp.Content,
		},
		Tokens: resp.Usage,
	}, nil
}

func (a *Agent) planPipeline(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	analysis, _ := msg.Payload["analysis"].(string)
	storyLen, _ := msg.Payload["storyLength"].(float64)
	hasScreenplay, _ := msg.Payload["hasScreenplay"].(bool)
	hasCharacters, _ := msg.Payload["hasCharacters"].(bool)
	userPrompt2 := fmt.Sprintf("Analysis:\n%s\n\nStory length: %.0f chars\nHas screenplay: %v\nHas characters: %v",
		analysis, storyLen, hasScreenplay, hasCharacters)
	resp, err := a.CallLLM(ctx, agent.TierFlash, planPipelinePrompt, userPrompt2)
	if err != nil {
		return nil, fmt.Errorf("plan pipeline: %w", err)
	}

	return &agent.TaskResult{
		Status: agent.TaskStatusCompleted,
		Output: map[string]any{"plan": resp.Content},
		Tokens: resp.Usage,
	}, nil
}
func (a *Agent) orchestrateWorkflow(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	planJSON, _ := msg.Payload["plan"].(string)
	story, _ := msg.Payload["story"].(string)

	var plan struct {
		Steps []struct {
			Agent   string   `json:"agent"`
			Skill   string   `json:"skill"`
			Depends []string `json:"depends,omitempty"`
		} `json:"steps"`
	}
	if err := json.Unmarshal([]byte(planJSON), &plan); err != nil {
		return nil, fmt.Errorf("parse plan: %w", err)
	}

	results := make(map[string]any)

	for _, step := range plan.Steps {
		a.Logger().Info("orchestrating step", "agent", step.Agent, "skill", step.Skill)

		payload := map[string]any{
			"story":           story,
			"projectId":       msg.ProjectID,
			"previousResults": results,
		}

		result, err := a.AskAgent(ctx, step.Agent, step.Skill, payload, 60*time.Second)
		if err != nil {
			a.Logger().Error("step failed", "agent", step.Agent, "skill", step.Skill, "error", err)
			return &agent.TaskResult{
				Status: agent.TaskStatusFailed,
				Error:  fmt.Sprintf("step %s.%s failed: %s", step.Agent, step.Skill, err.Error()),
				Output: results,
			}, nil
		}

		stepKey := fmt.Sprintf("%s.%s", step.Agent, step.Skill)
		results[stepKey] = result.Output
	}

	return &agent.TaskResult{
		Status: agent.TaskStatusCompleted,
		Output: results,
	}, nil
}

// splitEpisodes splits long text into balanced episodes.
func (a *Agent) splitEpisodes(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	content, _ := msg.Payload["content"].(string)

	systemPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptEpisodeSplit, map[string]string{
		"CONTENT": content,
	})

	resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt, content)
	if err != nil {
		return nil, fmt.Errorf("split episodes: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"episodes": resp.Content}, Tokens: resp.Usage}, nil
}

// segmentClips splits text into clip candidates for downstream conversion.
func (a *Agent) segmentClips(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	input, _ := msg.Payload["input"].(string)
	locationsLibName, _ := msg.Payload["locationsLibName"].(string)
	charactersLibName, _ := msg.Payload["charactersLibName"].(string)
	charactersIntroduction, _ := msg.Payload["charactersIntroduction"].(string)

	systemPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptClipSegmentation, map[string]string{
		"input":                   input,
		"locations_lib_name":      locationsLibName,
		"characters_lib_name":     charactersLibName,
		"characters_introduction": charactersIntroduction,
	})

	resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt, input)
	if err != nil {
		return nil, fmt.Errorf("segment clips: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"clips": resp.Content}, Tokens: resp.Usage}, nil
}

// convertScreenplay converts clip text into structured screenplay JSON.
func (a *Agent) convertScreenplay(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	clipID, _ := msg.Payload["clipId"].(string)
	clipContent, _ := msg.Payload["clipContent"].(string)
	locationsLibName, _ := msg.Payload["locationsLibName"].(string)
	charactersLibName, _ := msg.Payload["charactersLibName"].(string)
	charactersIntroduction, _ := msg.Payload["charactersIntroduction"].(string)

	systemPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptScreenplayConversion, map[string]string{
		"clip_id":                 clipID,
		"clip_content":            clipContent,
		"locations_lib_name":      locationsLibName,
		"characters_lib_name":     charactersLibName,
		"characters_introduction": charactersIntroduction,
	})

	resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt, clipContent)
	if err != nil {
		return nil, fmt.Errorf("convert screenplay: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"screenplay": resp.Content}, Tokens: resp.Usage}, nil
}
// startProduction is the heart of Autopilot mode.
// Director self-orchestrates the full A2A pipeline from story to assembled output.
//
// Pipeline stages:
//  1. analyzeStory via LLM
//  2. Publish analysis.done event to NATS
//  3. Parallel via agent.ExecuteParallel: analyze_characters + analyze_locations
//  4. segment_clips
//  5. Parallel goroutines + sync.WaitGroup: N x convert_screenplay
//  6. Parallel goroutines + sync.WaitGroup: N x create_storyboard
//  7. generate_batch (media agent)
//  8. quality_review (media agent, non-fatal)
//  9. analyze_voices (voice agent, non-fatal)
// 10. Return assembled TaskResult
func (a *Agent) startProduction(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	story, _ := msg.Payload["story"].(string)
	projectID, _ := msg.Payload["projectId"].(string)
	budget, _ := msg.Payload["budget"].(string)
	qualityLevel, _ := msg.Payload["qualityLevel"].(string)

	if story == "" {
		return &agent.TaskResult{
			Status: agent.TaskStatusFailed,
			Error:  "start_production: story payload field is required",
		}, nil
	}
	if projectID == "" {
		projectID = fmt.Sprintf("proj-%d", time.Now().UnixMilli())
	}

	a.Logger().Info("start_production: beginning autopilot pipeline",
		"projectId", projectID,
		"budget", budget,
		"qualityLevel", qualityLevel,
	)

	// Stage 1: Analyze story (LLM call)
	analysisResult, err := a.analyzeStory(ctx, agent.Message{
		Payload: map[string]any{
			"story":     story,
			"inputType": "novel",
		},
		ProjectID: projectID,
	})
	if err != nil {
		return nil, fmt.Errorf("start_production stage=analysis: %w", err)
	}

	analysis, _ := analysisResult.Output["analysis"].(string)

	_ = a.NotifyAgent(ctx, "pipeline-events", "progress", map[string]any{
		"projectId": projectID,
		"stage":     "analysis",
		"status":    "completed",
	})

	// Publish analysis.done to NATS pipeline subject
	_ = a.NotifyAgent(ctx, fmt.Sprintf("pipeline.%s.analysis.done", projectID), "analysis.done", map[string]any{
		"projectId": projectID,
		"analysis":  analysis,
	})

	// Stage 2: Parallel via agent.ExecuteParallel
	charLocPayload := map[string]any{
		"story":     story,
		"analysis":  analysis,
		"projectId": projectID,
	}

	parallelResult, err := agent.ExecuteParallel(ctx, a.BusRef(), []agent.Message{
		{
			From:    a.Name(),
			To:      "character",
			SkillID: "analyze_characters",
			Payload: charLocPayload,
		},
		{
			From:    a.Name(),
			To:      "location",
			SkillID: "analyze_locations",
			Payload: charLocPayload,
		},
	}, 120*time.Second)
	if err != nil {
		return nil, fmt.Errorf("start_production stage=char_loc_parallel: %w", err)
	}

	charResult, charOK := parallelResult.Results["character:analyze_characters"]
	locResult, locOK := parallelResult.Results["location:analyze_locations"]

	if !charOK || charResult == nil {
		charErr := parallelResult.Errors["character:analyze_characters"]
		return nil, fmt.Errorf("start_production stage=characters failed: %v", charErr)
	}
	if !locOK || locResult == nil {
		locErr := parallelResult.Errors["location:analyze_locations"]
		return nil, fmt.Errorf("start_production stage=locations failed: %v", locErr)
	}

	_ = a.NotifyAgent(ctx, "pipeline-events", "progress", map[string]any{
		"projectId": projectID,
		"stage":     "characters_locations",
		"status":    "completed",
	})

	// Stage 3: segment_clips
	segResult, err := a.AskAgent(ctx, "director", "segment_clips", map[string]any{
		"input":     story,
		"projectId": projectID,
	}, 120*time.Second)
	if err != nil {
		return nil, fmt.Errorf("start_production stage=segment_clips: %w", err)
	}

	clipsRaw, _ := segResult.Output["clips"].(string)
	clipsRaw = directorStripCodeFences(clipsRaw)

	var clips []map[string]any
	if parseErr := json.Unmarshal([]byte(clipsRaw), &clips); parseErr != nil {
		a.Logger().Warn("start_production: clips JSON parse failed, using fallback", "error", parseErr)
		clips = []map[string]any{{"content": clipsRaw, "id": "clip-0"}}
	}

	_ = a.NotifyAgent(ctx, "pipeline-events", "progress", map[string]any{
		"projectId": projectID,
		"stage":     "segmentation",
		"status":    "completed",
		"clipCount": len(clips),
	})
	// Stage 4: Parallel goroutines — N x convert_screenplay (one per clip)
	type screenplayItem struct {
		index  int
		output map[string]any
		err    error
	}

	screenplayCh := make(chan screenplayItem, len(clips))
	var spWg sync.WaitGroup

	for i, clip := range clips {
		spWg.Add(1)
		go func(idx int, c map[string]any) {
			defer spWg.Done()
			clipID, _ := c["id"].(string)
			if clipID == "" {
				clipID = fmt.Sprintf("clip-%d", idx)
			}
			clipContent, _ := c["content"].(string)
			res, callErr := a.AskAgent(ctx, "director", "convert_screenplay", map[string]any{
				"clipId":      clipID,
				"clipContent": clipContent,
				"projectId":   projectID,
			}, 120*time.Second)
			if callErr != nil {
				screenplayCh <- screenplayItem{index: idx, err: callErr}
				return
			}
			screenplayCh <- screenplayItem{index: idx, output: res.Output}
		}(i, clip)
	}
	spWg.Wait()
	close(screenplayCh)

	screenplays := make([]map[string]any, len(clips))
	for item := range screenplayCh {
		if item.err != nil {
			a.Logger().Error("start_production: screenplay conversion failed",
				"clipIndex", item.index, "error", item.err)
			screenplays[item.index] = map[string]any{"error": item.err.Error()}
		} else {
			screenplays[item.index] = item.output
		}
	}

	_ = a.NotifyAgent(ctx, "pipeline-events", "progress", map[string]any{
		"projectId":       projectID,
		"stage":           "screenplay",
		"status":          "completed",
		"screenplayCount": len(screenplays),
	})

	// Stage 5: Parallel goroutines — N x create_storyboard (one per screenplay)
	type storyboardItem struct {
		index  int
		output map[string]any
		err    error
	}

	storyboardCh := make(chan storyboardItem, len(screenplays))
	var sbWg sync.WaitGroup

	for i, sp := range screenplays {
		sbWg.Add(1)
		go func(idx int, screenplay map[string]any) {
			defer sbWg.Done()
			res, callErr := a.AskAgent(ctx, "storyboard", "create_storyboard", map[string]any{
				"screenplay": screenplay,
				"projectId":  projectID,
				"clipIndex":  idx,
			}, 180*time.Second)
			if callErr != nil {
				storyboardCh <- storyboardItem{index: idx, err: callErr}
				return
			}
			storyboardCh <- storyboardItem{index: idx, output: res.Output}
		}(i, sp)
	}
	sbWg.Wait()
	close(storyboardCh)

	storyboards := make([]map[string]any, len(screenplays))
	for item := range storyboardCh {
		if item.err != nil {
			a.Logger().Error("start_production: storyboard creation failed",
				"screenplayIndex", item.index, "error", item.err)
			storyboards[item.index] = map[string]any{"error": item.err.Error()}
		} else {
			storyboards[item.index] = item.output
		}
	}

	_ = a.NotifyAgent(ctx, "pipeline-events", "progress", map[string]any{
		"projectId":       projectID,
		"stage":           "storyboard",
		"status":          "completed",
		"storyboardCount": len(storyboards),
	})

	combinedStoryboard := map[string]any{
		"projectId":   projectID,
		"storyboards": storyboards,
		"screenplays": screenplays,
	}
	// Stage 6: Media generate_batch
	mediaResult, err := a.AskAgent(ctx, "media", "generate_batch", map[string]any{
		"storyboard": combinedStoryboard,
		"projectId":  projectID,
		"budget":     budget,
		"quality":    qualityLevel,
	}, 300*time.Second)
	if err != nil {
		return nil, fmt.Errorf("start_production stage=media_generate_batch: %w", err)
	}

	_ = a.NotifyAgent(ctx, "pipeline-events", "progress", map[string]any{
		"projectId": projectID,
		"stage":     "media_generation",
		"status":    "completed",
	})

	// Stage 7: Media quality_review (non-fatal)
	reviewResult, reviewErr := a.AskAgent(ctx, "media", "quality_review", map[string]any{
		"media":     mediaResult.Output,
		"projectId": projectID,
	}, 120*time.Second)
	if reviewErr != nil {
		a.Logger().Warn("start_production: quality_review failed (non-fatal), continuing", "error", reviewErr)
		reviewResult = &agent.TaskResult{Output: map[string]any{"skipped": true}}
	}

	_ = a.NotifyAgent(ctx, "pipeline-events", "progress", map[string]any{
		"projectId": projectID,
		"stage":     "quality_review",
		"status":    "completed",
	})

	// Stage 8: Voice analyze_voices (non-fatal)
	voiceResult, voiceErr := a.AskAgent(ctx, "voice", "analyze_voices", map[string]any{
		"story":      story,
		"screenplay": screenplays,
		"projectId":  projectID,
	}, 120*time.Second)
	if voiceErr != nil {
		a.Logger().Warn("start_production: analyze_voices failed (non-fatal), continuing", "error", voiceErr)
		voiceResult = &agent.TaskResult{Output: map[string]any{"skipped": true}}
	}

	_ = a.NotifyAgent(ctx, "pipeline-events", "progress", map[string]any{
		"projectId": projectID,
		"stage":     "voice",
		"status":    "completed",
	})

	// Final: assembly
	_ = a.NotifyAgent(ctx, "pipeline-events", "progress", map[string]any{
		"projectId": projectID,
		"stage":     "assembly",
		"status":    "completed",
	})

	a.Logger().Info("start_production: autopilot pipeline completed", "projectId", projectID)

	return &agent.TaskResult{
		Status: agent.TaskStatusCompleted,
		Output: map[string]any{
			"projectId":  projectID,
			"characters": charResult.Output,
			"locations":  locResult.Output,
			"storyboard": combinedStoryboard,
			"media":      mediaResult.Output,
			"review":     reviewResult.Output,
			"voices":     voiceResult.Output,
			"assembled":  true,
		},
	}, nil
}

// directorStripCodeFences removes markdown code fence wrappers from a string.
// Mirrors the unexported stripCodeFences helper in the pipeline package.
func directorStripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx != -1 {
			s = s[idx+1:]
		}
	}
	if strings.HasSuffix(s, "```") {
		s = s[:strings.LastIndex(s, "```")]
	}
	return strings.TrimSpace(s)
}

// --- Legacy Prompts (kept for analyze_story and plan_pipeline) ---
const analyzeStoryPrompt = `You are an expert film director analyzing a story for video production.

Extract and return as JSON:
{
  "title": "story title or suggested title",
  "genre": "drama/comedy/action/horror/romance/fantasy/sci-fi",
  "mood": "overall mood",
  "themes": ["theme1", "theme2"],
  "characters": [
    {"name": "...", "role": "protagonist/antagonist/supporting", "description": "..."}
  ],
  "locations": [
    {"name": "...", "description": "...", "mood": "..."}
  ],
  "plotStructure": {
    "setup": "...",
    "conflict": "...",
    "climax": "...",
    "resolution": "..."
  },
  "estimatedEpisodes": 1,
  "estimatedDurationMinutes": 5,
  "suggestedArtStyle": "realistic/anime/comic/watercolor"
}`

const planPipelinePrompt = `You are a production planner for an AI filmmaking studio.
Given the story analysis, decide the optimal execution pipeline.

Return a JSON plan:
{
  "strategy": "full/skip-analysis/screenplay-direct/incremental",
  "reasoning": "why this strategy",
  "steps": [
    {"agent": "character", "skill": "analyze_characters", "depends": []},
    {"agent": "location", "skill": "analyze_locations", "depends": []},
    {"agent": "storyboard", "skill": "create_storyboard", "depends": ["character.analyze_characters", "location.analyze_locations"]}
  ]
}

Rules:
- Short story (<2000 chars): skip episode split
- Has screenplay already: skip story_to_script, go direct to storyboard
- Has characters defined: skip character analysis
- Always include storyboard as final creative step`
