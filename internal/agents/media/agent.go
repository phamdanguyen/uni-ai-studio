// Package media implements the Media Agent — specialist in image/video generation
// using external AI providers with quality assurance.
package media

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/uni-ai-studio/waoo-studio/internal/agent"
	"github.com/uni-ai-studio/waoo-studio/lib/prompts"
)

// Agent is the Media Producer responsible for generating images and videos
// from storyboard panels using multiple AI providers.
type Agent struct {
	agent.BaseAgent
}

// New creates a new Media Agent.
func New(bus agent.MessageBus, router agent.ModelRouter, tools agent.ToolRegistry, logger *slog.Logger) *Agent {
	card := agent.AgentCard{
		Name:        "media",
		Version:     "2.0.0",
		Description: "Nhà sản xuất media AI — sinh ảnh, video từ storyboard sử dụng FAL/Ark/Vidu/MiniMax/Gemini/Veo.",
		Skills: []agent.Skill{
			{ID: "generate_image", Name: "Sinh ảnh từ prompt", Description: "Generate single panel image using AI providers", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "generate_video", Name: "Sinh video từ prompt + ảnh", Description: "Generate video clip from prompt and optional reference image", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "quality_review", Name: "Đánh giá chất lượng media", Description: "LLM-based quality evaluation of generated media", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "generate_batch", Name: "Sinh batch media cho panels", Description: "Batch generate images for multiple storyboard panels", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "modify_prompt", Name: "Tinh chỉnh image/video prompt", Description: "Refine storyboard image and video prompts based on instruction", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "edit_panel_image", Name: "Chỉnh sửa ảnh panel", Description: "Edit panel image based on user instruction and reference", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
		},
		Capabilities: agent.Capabilities{Streaming: true, StateTransitionHistory: true},
	}
	return &Agent{BaseAgent: agent.NewBaseAgent(card, bus, router, tools, logger)}
}

func (a *Agent) HandleMessage(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	a.Logger().Info("handling message", "skill", msg.SkillID)
	switch msg.SkillID {
	case "generate_image":
		return a.generateImage(ctx, msg)
	case "generate_video":
		return a.generateVideo(ctx, msg)
	case "quality_review":
		return a.qualityReview(ctx, msg)
	case "generate_batch":
		return a.generateBatch(ctx, msg)
	case "modify_prompt":
		return a.modifyPrompt(ctx, msg)
	case "edit_panel_image":
		return a.editPanelImage(ctx, msg)
	default:
		return &agent.TaskResult{Status: agent.TaskStatusFailed, Error: fmt.Sprintf("unknown skill: %s", msg.SkillID)}, nil
	}
}

func (a *Agent) HandleStream(ctx context.Context, msg agent.Message, stream chan<- agent.StreamEvent) error {
	defer close(stream)
	result, err := a.HandleMessage(ctx, msg)
	if err != nil {
		return err
	}
	stream <- agent.StreamEvent{Type: "completed", Payload: result.Output}
	return nil
}

// generateImage generates a single panel image.
// Uses: single_panel_image prompt for system context
func (a *Agent) generateImage(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	prompt, _ := msg.Payload["prompt"].(string)
	provider, _ := msg.Payload["provider"].(string)
	if provider == "" {
		provider = "fal"
	}

	result, err := a.UseTool(ctx, "image_generator", map[string]any{
		"prompt":   prompt,
		"provider": provider,
	})
	if err != nil {
		return nil, fmt.Errorf("generate image: %w", err)
	}

	return &agent.TaskResult{
		Status: agent.TaskStatusCompleted,
		Output: result,
	}, nil
}

// generateVideo uses configured video generators (Vidu, MiniMax, Veo, etc.)
func (a *Agent) generateVideo(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	prompt, _ := msg.Payload["prompt"].(string)
	imageURL, _ := msg.Payload["imageUrl"].(string)
	provider, _ := msg.Payload["provider"].(string)
	if provider == "" {
		provider = "vidu"
	}

	result, err := a.UseTool(ctx, "video_generator", map[string]any{
		"prompt":   prompt,
		"imageUrl": imageURL,
		"provider": provider,
	})
	if err != nil {
		return nil, fmt.Errorf("generate video: %w", err)
	}

	return &agent.TaskResult{
		Status: agent.TaskStatusCompleted,
		Output: result,
	}, nil
}

// qualityReview uses multimodal LLM to evaluate generated media quality.
func (a *Agent) qualityReview(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	mediaURL, _ := msg.Payload["mediaUrl"].(string)
	expectedDesc, _ := msg.Payload["expectedDescription"].(string)
	mediaType, _ := msg.Payload["mediaType"].(string)

	resp, err := a.CallLLM(ctx, agent.TierFlash,
		fmt.Sprintf(`You are a quality reviewer for AI-generated %s.
Evaluate the generated media against the expected description.
Return JSON: {"score": 0.0-1.0, "pass": true/false, "issues": ["..."], "suggestion": "..."}
Score >= 0.7 means pass.`, mediaType),
		fmt.Sprintf("Media URL: %s\nExpected: %s", mediaURL, expectedDesc))
	if err != nil {
		return nil, fmt.Errorf("quality review: %w", err)
	}

	return &agent.TaskResult{
		Status: agent.TaskStatusCompleted,
		Output: map[string]any{"review": resp.Content},
		Tokens: resp.Usage,
	}, nil
}

// generateBatch processes multiple panels for image/video generation.
func (a *Agent) generateBatch(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	panels, ok := msg.Payload["panels"].([]any)
	if !ok {
		return &agent.TaskResult{Status: agent.TaskStatusFailed, Error: "panels must be array"}, nil
	}

	a.Logger().Info("batch generation", "panelCount", len(panels))

	results := make([]map[string]any, 0, len(panels))
	for i, panel := range panels {
		panelMap, _ := panel.(map[string]any)
		prompt, _ := panelMap["imagePrompt"].(string)
		if prompt == "" {
			a.Logger().Warn("panel missing imagePrompt", "index", i)
			results = append(results, map[string]any{"index": i, "status": "skipped"})
			continue
		}

		result, err := a.UseTool(ctx, "image_generator", map[string]any{
			"prompt":   prompt,
			"provider": "fal",
		})
		if err != nil {
			a.Logger().Error("panel image generation failed", "index", i, "error", err)
			results = append(results, map[string]any{"index": i, "status": "failed", "error": err.Error()})
			continue
		}

		results = append(results, map[string]any{"index": i, "status": "completed", "result": result})
	}

	return &agent.TaskResult{
		Status: agent.TaskStatusCompleted,
		Output: map[string]any{"results": results, "total": len(panels), "completed": len(results)},
	}, nil
}

// modifyPrompt refines storyboard image and video prompts based on instruction.
// Uses: image_prompt_modify prompt
func (a *Agent) modifyPrompt(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	promptInput, _ := msg.Payload["promptInput"].(string)
	videoPromptInput, _ := msg.Payload["videoPromptInput"].(string)
	userInput, _ := msg.Payload["instruction"].(string)

	systemPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptImagePromptModify, map[string]string{
		"prompt_input":       promptInput,
		"video_prompt_input": videoPromptInput,
		"user_input":         userInput,
	})

	resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt, userInput)
	if err != nil {
		return nil, fmt.Errorf("modify prompt: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"modified": resp.Content}, Tokens: resp.Usage}, nil
}

// editPanelImage edits a panel image based on user instruction.
// Uses: storyboard_edit prompt
func (a *Agent) editPanelImage(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	userInput, _ := msg.Payload["instruction"].(string)

	systemPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptStoryboardEdit, map[string]string{
		"user_input": userInput,
	})

	resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt, userInput)
	if err != nil {
		return nil, fmt.Errorf("edit panel image: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"edited": resp.Content}, Tokens: resp.Usage}, nil
}
