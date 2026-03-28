// Package location implements the Location Agent — specialist in environment/setting design.
package location

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/uni-ai-studio/waoo-studio/internal/agent"
	"github.com/uni-ai-studio/waoo-studio/internal/memory"
	"github.com/uni-ai-studio/waoo-studio/lib/prompts"
)

// Agent is the Location Designer responsible for analyzing, creating, and
// maintaining visual consistency of locations/environments.
type Agent struct {
	agent.BaseAgent
}

// New creates a new Location Agent.
func New(bus agent.MessageBus, router agent.ModelRouter, tools agent.ToolRegistry, mem *memory.Store, logger *slog.Logger) *Agent {
	card := agent.AgentCard{
		Name:        "location",
		Version:     "2.0.0",
		Description: "Họa sĩ bối cảnh AI — phân tích, thiết kế, duy trì consistency cho các địa điểm/bối cảnh.",
		Skills: []agent.Skill{
			{ID: "analyze_locations", Name: "Phân tích bối cảnh từ truyện", Description: "Extract locations needing dedicated background assets", InputModes: []string{"text/plain"}, OutputModes: []string{"application/json"}},
			{ID: "create_location", Name: "Tạo bối cảnh mới", Description: "Generate one scene prompt for image generation", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "modify_location", Name: "Sửa đổi bối cảnh", Description: "Modify existing scene description while preserving identity", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "regenerate_variants", Name: "Tạo lại biến thể bối cảnh", Description: "Generate 3 new scene description variants", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "update_description", Name: "Cập nhật mô tả bối cảnh", Description: "Update location description with instruction + optional image context", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
		},
		Capabilities: agent.Capabilities{Streaming: true, StateTransitionHistory: true},
	}
	return &Agent{BaseAgent: agent.NewBaseAgent(card, bus, router, tools, mem, logger)}
}

func (a *Agent) HandleMessage(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	a.Logger().Info("handling message", "skill", msg.SkillID)
	switch msg.SkillID {
	case "analyze_locations":
		return a.analyzeLocations(ctx, msg)
	case "create_location":
		return a.createLocation(ctx, msg)
	case "modify_location":
		return a.modifyLocation(ctx, msg)
	case "regenerate_variants":
		return a.regenerateVariants(ctx, msg)
	case "update_description":
		return a.updateDescription(ctx, msg)
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

// analyzeLocations extracts locations needing dedicated background assets.
// Uses: select_location prompt
func (a *Agent) analyzeLocations(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	input, _ := msg.Payload["story"].(string)
	locationsLibName, _ := msg.Payload["locationsLibName"].(string)

	systemPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptSelectLocation, map[string]string{
		"input":              input,
		"locations_lib_name": locationsLibName,
	})

	resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt, input)
	if err != nil {
		return nil, fmt.Errorf("analyze locations: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"locations": resp.Content}, Tokens: resp.Usage}, nil
}

// createLocation generates one scene prompt for image generation.
// Uses: location_create prompt
func (a *Agent) createLocation(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	userInput, _ := msg.Payload["userInput"].(string)

	systemPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptLocationCreate, map[string]string{
		"user_input": userInput,
	})

	resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt, userInput)
	if err != nil {
		return nil, fmt.Errorf("create location: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"location": resp.Content}, Tokens: resp.Usage}, nil
}

// modifyLocation modifies existing scene description while preserving scene identity.
// Uses: location_modify prompt
func (a *Agent) modifyLocation(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	locationName, _ := msg.Payload["locationName"].(string)
	locationInput, _ := msg.Payload["locationInput"].(string)
	userInput, _ := msg.Payload["instruction"].(string)

	systemPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptLocationModify, map[string]string{
		"location_name":  locationName,
		"location_input": locationInput,
		"user_input":     userInput,
	})

	resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt,
		fmt.Sprintf("Modify %s: %s", locationName, userInput))
	if err != nil {
		return nil, fmt.Errorf("modify location: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"modified": resp.Content}, Tokens: resp.Usage}, nil
}

// regenerateVariants generates 3 new scene description variants for the same location.
// Uses: location_regenerate prompt
func (a *Agent) regenerateVariants(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	locationName, _ := msg.Payload["locationName"].(string)
	currentDescriptions, _ := msg.Payload["currentDescriptions"].(string)

	systemPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptLocationRegenerate, map[string]string{
		"location_name":        locationName,
		"current_descriptions": currentDescriptions,
	})

	resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt,
		fmt.Sprintf("Regenerate variants for location: %s", locationName))
	if err != nil {
		return nil, fmt.Errorf("regenerate variants: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"variants": resp.Content}, Tokens: resp.Usage}, nil
}

// updateDescription updates location description with instruction and optional image context.
// Uses: location_description_update prompt
func (a *Agent) updateDescription(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	locationName, _ := msg.Payload["locationName"].(string)
	originalDescription, _ := msg.Payload["originalDescription"].(string)
	modifyInstruction, _ := msg.Payload["modifyInstruction"].(string)
	imageContext, _ := msg.Payload["imageContext"].(string)

	systemPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptLocationDescriptionUpdate, map[string]string{
		"location_name":        locationName,
		"original_description": originalDescription,
		"modify_instruction":   modifyInstruction,
		"image_context":        imageContext,
	})

	resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt,
		fmt.Sprintf("Update %s: %s", locationName, modifyInstruction))
	if err != nil {
		return nil, fmt.Errorf("update description: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"updated": resp.Content}, Tokens: resp.Usage}, nil
}
