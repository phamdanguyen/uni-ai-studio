// Package director implements the Director Agent — the orchestrator responsible for
// analyzing input, planning the pipeline, and coordinating other agents.
package director

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/uni-ai-studio/waoo-studio/internal/agent"
	"github.com/uni-ai-studio/waoo-studio/lib/prompts"
)

// Agent is the Director — the brain of the filmmaking pipeline.
// It analyzes stories, plans execution, and coordinates specialized agents.
type Agent struct {
	agent.BaseAgent
}

// New creates a new Director Agent.
func New(bus agent.MessageBus, router agent.ModelRouter, tools agent.ToolRegistry, logger *slog.Logger) *Agent {
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
		},
		Capabilities: agent.Capabilities{
			Streaming:              true,
			StateTransitionHistory: true,
		},
	}

	return &Agent{
		BaseAgent: agent.NewBaseAgent(card, bus, router, tools, logger),
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
	inputType, _ := msg.Payload["inputType"].(string) // "novel", "script", "screenplay"
	if inputType == "" {
		inputType = "novel"
	}

	resp, err := a.CallLLM(ctx, agent.TierStandard, analyzeStoryPrompt,
		fmt.Sprintf("Input type: %s\n\nContent:\n%s", inputType, story))
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

	resp, err := a.CallLLM(ctx, agent.TierFlash, planPipelinePrompt,
		fmt.Sprintf("Analysis:\n%s\n\nStory length: %.0f chars\nHas screenplay: %v\nHas characters: %v",
			analysis, storyLen, hasScreenplay, hasCharacters))
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

		result, err := a.AskAgent(ctx, step.Agent, step.Skill, payload, 60_000_000_000)
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
// Uses: episode_split prompt
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
// Uses: agent_clip prompt
func (a *Agent) segmentClips(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	input, _ := msg.Payload["input"].(string)
	locationsLibName, _ := msg.Payload["locationsLibName"].(string)
	charactersLibName, _ := msg.Payload["charactersLibName"].(string)
	charactersIntroduction, _ := msg.Payload["charactersIntroduction"].(string)

	systemPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptClipSegmentation, map[string]string{
		"input":                    input,
		"locations_lib_name":       locationsLibName,
		"characters_lib_name":      charactersLibName,
		"characters_introduction":  charactersIntroduction,
	})

	resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt, input)
	if err != nil {
		return nil, fmt.Errorf("segment clips: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"clips": resp.Content}, Tokens: resp.Usage}, nil
}

// convertScreenplay converts clip text into structured screenplay JSON.
// Uses: screenplay_conversion prompt
func (a *Agent) convertScreenplay(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	clipID, _ := msg.Payload["clipId"].(string)
	clipContent, _ := msg.Payload["clipContent"].(string)
	locationsLibName, _ := msg.Payload["locationsLibName"].(string)
	charactersLibName, _ := msg.Payload["charactersLibName"].(string)
	charactersIntroduction, _ := msg.Payload["charactersIntroduction"].(string)

	systemPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptScreenplayConversion, map[string]string{
		"clip_id":                  clipID,
		"clip_content":             clipContent,
		"locations_lib_name":       locationsLibName,
		"characters_lib_name":      charactersLibName,
		"characters_introduction":  charactersIntroduction,
	})

	resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt, clipContent)
	if err != nil {
		return nil, fmt.Errorf("convert screenplay: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"screenplay": resp.Content}, Tokens: resp.Usage}, nil
}

// --- Legacy Prompts (kept for analyze_story and plan_pipeline which have no file-based equivalent) ---

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
