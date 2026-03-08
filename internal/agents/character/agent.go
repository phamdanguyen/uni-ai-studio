// Package character implements the Character Agent — specialist in character design,
// visual consistency, and profile management.
package character

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/uni-ai-studio/waoo-studio/internal/agent"
	"github.com/uni-ai-studio/waoo-studio/lib/prompts"
)

// Agent is the Character Designer responsible for analyzing, creating, and
// maintaining visual consistency of characters across a project.
type Agent struct {
	agent.BaseAgent
}

// New creates a new Character Agent.
func New(bus agent.MessageBus, router agent.ModelRouter, tools agent.ToolRegistry, logger *slog.Logger) *Agent {
	card := agent.AgentCard{
		Name:        "character",
		Version:     "2.0.0",
		Description: "Họa sĩ nhân vật AI — phân tích, thiết kế, duy trì consistency ngoại hình nhân vật.",
		Skills: []agent.Skill{
			{ID: "analyze_characters", Name: "Phân tích nhân vật từ truyện", Description: "Extract structured character profiles from story text", InputModes: []string{"text/plain"}, OutputModes: []string{"application/json"}},
			{ID: "design_visual", Name: "Thiết kế ngoại hình nhân vật", Description: "Generate image-ready appearance descriptions from profiles", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "create_character", Name: "Tạo nhân vật từ mô tả", Description: "Generate one image-ready character prompt from user request", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "modify_character", Name: "Sửa đổi ngoại hình nhân vật", Description: "Modify existing character description based on instruction", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "regenerate_variants", Name: "Tạo lại biến thể ngoại hình", Description: "Generate 3 new character appearance variants", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "update_description", Name: "Cập nhật mô tả nhân vật", Description: "Update character description with edit instruction + optional image context", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "image_to_description", Name: "Ảnh → mô tả nhân vật", Description: "Analyze character image and produce visual description", InputModes: []string{"application/json"}, OutputModes: []string{"text/plain"}},
			{ID: "reference_to_sheet", Name: "Ảnh tham chiếu → character sheet", Description: "Generate character sheet from reference image", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "query_appearances", Name: "Trả lời về ngoại hình cụ thể", Description: "Return concise visual references for image generation", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
		},
		Capabilities: agent.Capabilities{Streaming: true, StateTransitionHistory: true},
	}
	return &Agent{BaseAgent: agent.NewBaseAgent(card, bus, router, tools, logger)}
}

func (a *Agent) HandleMessage(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	a.Logger().Info("handling message", "skill", msg.SkillID)
	switch msg.SkillID {
	case "analyze_characters":
		return a.analyzeCharacters(ctx, msg)
	case "design_visual":
		return a.designVisual(ctx, msg)
	case "create_character":
		return a.createCharacter(ctx, msg)
	case "modify_character":
		return a.modifyCharacter(ctx, msg)
	case "regenerate_variants":
		return a.regenerateVariants(ctx, msg)
	case "update_description":
		return a.updateDescription(ctx, msg)
	case "image_to_description":
		return a.imageToDescription(ctx, msg)
	case "reference_to_sheet":
		return a.referenceToSheet(ctx, msg)
	case "query_appearances":
		return a.queryAppearances(ctx, msg)
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

// --- Skill Implementations ---

// analyzeCharacters extracts structured character profiles from story text.
// Uses: agent_character_profile prompt
func (a *Agent) analyzeCharacters(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	input, _ := msg.Payload["story"].(string)
	charactersLibInfo, _ := msg.Payload["charactersLibInfo"].(string)

	systemPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptCharacterProfile, map[string]string{
		"input":              input,
		"characters_lib_info": charactersLibInfo,
	})

	resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt, input)
	if err != nil {
		return nil, fmt.Errorf("analyze characters: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"characters": resp.Content}, Tokens: resp.Usage}, nil
}

// designVisual generates image-ready appearance descriptions from character profiles.
// Uses: agent_character_visual prompt
func (a *Agent) designVisual(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	profilesJSON, _ := json.Marshal(msg.Payload["characterProfiles"])

	systemPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptCharacterVisual, map[string]string{
		"character_profiles": string(profilesJSON),
	})

	resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt, "Generate visual descriptions for these characters.")
	if err != nil {
		return nil, fmt.Errorf("design visual: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"visuals": resp.Content}, Tokens: resp.Usage}, nil
}

// createCharacter generates one image-ready character prompt from user request.
// Uses: character_create prompt
func (a *Agent) createCharacter(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	userInput, _ := msg.Payload["userInput"].(string)

	systemPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptCharacterCreate, map[string]string{
		"user_input": userInput,
	})

	resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt, userInput)
	if err != nil {
		return nil, fmt.Errorf("create character: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"profile": resp.Content}, Tokens: resp.Usage}, nil
}

// modifyCharacter modifies an existing character description based on instruction.
// Uses: character_modify prompt
func (a *Agent) modifyCharacter(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	characterInput, _ := msg.Payload["characterInput"].(string)
	userInput, _ := msg.Payload["instruction"].(string)

	systemPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptCharacterModify, map[string]string{
		"character_input": characterInput,
		"user_input":      userInput,
	})

	resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt,
		fmt.Sprintf("Current: %s\nInstruction: %s", characterInput, userInput))
	if err != nil {
		return nil, fmt.Errorf("modify character: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"modified": resp.Content}, Tokens: resp.Usage}, nil
}

// regenerateVariants generates 3 new character appearance variants.
// Uses: character_regenerate prompt
func (a *Agent) regenerateVariants(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	characterName, _ := msg.Payload["characterName"].(string)
	changeReason, _ := msg.Payload["changeReason"].(string)
	currentDescriptions, _ := msg.Payload["currentDescriptions"].(string)
	novelText, _ := msg.Payload["novelText"].(string)

	systemPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptCharacterRegenerate, map[string]string{
		"character_name":       characterName,
		"change_reason":        changeReason,
		"current_descriptions": currentDescriptions,
		"novel_text":           novelText,
	})

	resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt,
		fmt.Sprintf("Regenerate variants for %s (%s)", characterName, changeReason))
	if err != nil {
		return nil, fmt.Errorf("regenerate variants: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"variants": resp.Content}, Tokens: resp.Usage}, nil
}

// updateDescription updates character description with edit instruction.
// Uses: character_description_update prompt
func (a *Agent) updateDescription(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	originalDescription, _ := msg.Payload["originalDescription"].(string)
	modifyInstruction, _ := msg.Payload["modifyInstruction"].(string)
	imageContext, _ := msg.Payload["imageContext"].(string)

	systemPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptCharacterDescriptionUpdate, map[string]string{
		"original_description": originalDescription,
		"modify_instruction":   modifyInstruction,
		"image_context":        imageContext,
	})

	resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt,
		fmt.Sprintf("Update: %s → %s", originalDescription, modifyInstruction))
	if err != nil {
		return nil, fmt.Errorf("update description: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"updated": resp.Content}, Tokens: resp.Usage}, nil
}

// imageToDescription analyzes character image and produces visual description.
// Uses: character_image_to_description prompt (character-reference category)
func (a *Agent) imageToDescription(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	systemPrompt := prompts.MustLoad(prompts.CategoryCharacterReference, prompts.PromptImageToDescription)
	imageURL, _ := msg.Payload["imageUrl"].(string)

	resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt,
		fmt.Sprintf("Analyze this character image: %s", imageURL))
	if err != nil {
		return nil, fmt.Errorf("image to description: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"description": resp.Content}, Tokens: resp.Usage}, nil
}

// referenceToSheet generates character sheet from reference image.
// Uses: character_reference_to_sheet prompt (character-reference category)
func (a *Agent) referenceToSheet(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	systemPrompt := prompts.MustLoad(prompts.CategoryCharacterReference, prompts.PromptReferenceToSheet)
	imageURL, _ := msg.Payload["imageUrl"].(string)

	resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt,
		fmt.Sprintf("Generate character sheet from reference: %s", imageURL))
	if err != nil {
		return nil, fmt.Errorf("reference to sheet: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"sheet": resp.Content}, Tokens: resp.Usage}, nil
}

// queryAppearances returns concise visual references for image generation prompts.
func (a *Agent) queryAppearances(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	characters, _ := msg.Payload["characters"].(string)
	resp, err := a.CallLLM(ctx, agent.TierFlash,
		"You are a character reference database. Given character descriptions, return concise visual references suitable for image generation prompts. Return JSON with character names as keys and visual description as values.",
		characters)
	if err != nil {
		return nil, err
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"visuals": resp.Content}, Tokens: resp.Usage}, nil
}
