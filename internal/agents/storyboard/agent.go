// Package storyboard implements the Storyboard Agent — specialist in visual storytelling,
// panel layout, cinematography, and scene composition.
package storyboard

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/uni-ai-studio/waoo-studio/internal/agent"
	"github.com/uni-ai-studio/waoo-studio/lib/prompts"
)

// Agent is the Storyboard specialist responsible for converting screenplays
// into detailed panel-by-panel storyboards with shot types, camera moves,
// and acting directions.
type Agent struct {
	agent.BaseAgent
}

// New creates a new Storyboard Agent.
func New(bus agent.MessageBus, router agent.ModelRouter, tools agent.ToolRegistry, logger *slog.Logger) *Agent {
	card := agent.AgentCard{
		Name:        "storyboard",
		Version:     "2.0.0",
		Description: "Chuyên gia phân cảnh và thiết kế visual storytelling. Chuyển screenplay thành storyboard chi tiết với shot types, camera moves, và acting directions.",
		Skills: []agent.Skill{
			{ID: "create_storyboard", Name: "Tạo storyboard từ screenplay", Description: "4-phase pipeline: Plan → Cinematography → Acting → Detail merge", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "refine_panel", Name: "Tinh chỉnh panel cụ thể", Description: "Sửa đổi shot type, camera move, description của một panel", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "insert_panel", Name: "Chèn panel mới", Description: "Chèn panel transition giữa hai panels hiện có", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "analyze_shot_variants", Name: "Phân tích biến thể shot", Description: "Generate multiple shot variant ideas for a panel", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "generate_shot_variant", Name: "Tạo biến thể shot", Description: "Generate new variant image with camera variation", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "query_visual_context", Name: "Trả lời về visual context", Description: "Cung cấp thông tin về composition, lighting, mood cho panel/clip", InputModes: []string{"text/plain"}, OutputModes: []string{"application/json"}},
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
	case "create_storyboard":
		return a.createStoryboard(ctx, msg)
	case "refine_panel":
		return a.refinePanel(ctx, msg)
	case "insert_panel":
		return a.insertPanel(ctx, msg)
	case "analyze_shot_variants":
		return a.analyzeShotVariants(ctx, msg)
	case "generate_shot_variant":
		return a.generateShotVariant(ctx, msg)
	case "query_visual_context":
		return a.queryVisualContext(ctx, msg)
	default:
		return &agent.TaskResult{
			Status: agent.TaskStatusFailed,
			Error:  fmt.Sprintf("unknown skill: %s", msg.SkillID),
		}, nil
	}
}

// HandleStream implements streaming output for long-running storyboard generation.
func (a *Agent) HandleStream(ctx context.Context, msg agent.Message, stream chan<- agent.StreamEvent) error {
	defer close(stream)

	stream <- agent.StreamEvent{Type: "status", Payload: "Starting storyboard generation..."}

	result, err := a.HandleMessage(ctx, msg)
	if err != nil {
		stream <- agent.StreamEvent{Type: "error", Payload: err.Error()}
		return err
	}

	stream <- agent.StreamEvent{Type: "completed", Payload: result.Output}
	return nil
}

// --- Skill Implementations ---

// createStoryboard implements the 4-phase storyboard generation pipeline.
// Phase 1: Plan panels (agent_storyboard_plan)
// Phase 2a: Cinematography (agent_cinematographer)
// Phase 2b: Acting directions (agent_acting_direction)
// Phase 3: Detail merge (agent_storyboard_detail)
func (a *Agent) createStoryboard(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	// Extract all context from payload
	clipJSON, _ := msg.Payload["clipJson"].(string)
	clipContent, _ := msg.Payload["clipContent"].(string)
	if clipContent == "" {
		// Backward compat: old callers may use "screenplay"
		clipContent, _ = msg.Payload["screenplay"].(string)
	}
	charactersLibName, _ := msg.Payload["charactersLibName"].(string)
	locationsLibName, _ := msg.Payload["locationsLibName"].(string)
	charactersIntroduction, _ := msg.Payload["charactersIntroduction"].(string)
	charactersAppearanceList, _ := msg.Payload["charactersAppearanceList"].(string)
	charactersFullDescription, _ := msg.Payload["charactersFullDescription"].(string)
	charactersInfo, _ := msg.Payload["charactersInfo"].(string)
	charactersAgeGender, _ := msg.Payload["charactersAgeGender"].(string)
	locationsDescription, _ := msg.Payload["locationsDescription"].(string)

	panelCount := 9
	if pc, ok := msg.Payload["panelCount"].(float64); ok {
		panelCount = int(pc)
	}
	panelCountStr := fmt.Sprintf("%d", panelCount)

	// Phase 1: Plan panels
	a.Logger().Info("phase 1: planning panels", "panelCount", panelCount)
	planPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptStoryboardPlan, map[string]string{
		"characters_lib_name":        charactersLibName,
		"locations_lib_name":         locationsLibName,
		"characters_introduction":    charactersIntroduction,
		"characters_appearance_list": charactersAppearanceList,
		"characters_full_description": charactersFullDescription,
		"clip_json":                  clipJSON,
		"clip_content":               clipContent,
	})

	planResp, err := a.CallLLM(ctx, agent.TierStandard, planPrompt, clipContent)
	if err != nil {
		return nil, fmt.Errorf("phase 1 plan: %w", err)
	}

	// Phase 2a: Cinematography
	a.Logger().Info("phase 2a: cinematography")
	cinemaPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptCinematographer, map[string]string{
		"panel_count":           panelCountStr,
		"panels_json":           planResp.Content,
		"locations_description": locationsDescription,
		"characters_info":       charactersInfo,
	})

	cinemaResp, err := a.CallLLM(ctx, agent.TierStandard, cinemaPrompt,
		fmt.Sprintf("Panel plan:\n%s", planResp.Content))
	if err != nil {
		return nil, fmt.Errorf("phase 2a cinematography: %w", err)
	}

	// Phase 2b: Acting directions
	a.Logger().Info("phase 2b: acting directions")
	actingPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptActingDirection, map[string]string{
		"panel_count":     panelCountStr,
		"panels_json":     planResp.Content,
		"characters_info": charactersInfo,
	})

	actingResp, err := a.CallLLM(ctx, agent.TierStandard, actingPrompt,
		fmt.Sprintf("Panel plan:\n%s", planResp.Content))
	if err != nil {
		return nil, fmt.Errorf("phase 2b acting: %w", err)
	}

	// Query character appearances from Character Agent
	a.Logger().Info("querying character agent for visual references")
	charResult, err := a.AskAgent(ctx, "character", "query_appearances", map[string]any{
		"characters": charactersFullDescription,
		"projectId":  msg.ProjectID,
	}, 15*time.Second)
	if err != nil {
		a.Logger().Warn("character agent query failed, continuing without references", "error", err)
	}

	charVisuals := ""
	if charResult != nil && charResult.Output != nil {
		if cv, ok := charResult.Output["visuals"].(string); ok {
			charVisuals = cv
		}
	}

	// Phase 3: Detail merge
	a.Logger().Info("phase 3: detail merge")
	detailPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptStoryboardDetail, map[string]string{
		"panels_json":          planResp.Content,
		"characters_age_gender": charactersAgeGender,
		"locations_description": locationsDescription,
	})

	mergeResp, err := a.CallLLM(ctx, agent.TierStandard, detailPrompt,
		fmt.Sprintf("Plan:\n%s\n\nCinematography:\n%s\n\nActing:\n%s\n\nCharacter visuals:\n%s",
			planResp.Content, cinemaResp.Content, actingResp.Content, charVisuals))
	if err != nil {
		return nil, fmt.Errorf("phase 3 merge: %w", err)
	}

	// Parse final panels
	var panels []map[string]any
	if err := json.Unmarshal([]byte(mergeResp.Content), &panels); err != nil {
		a.Logger().Warn("panel JSON parse failed, returning raw", "error", err)
		return &agent.TaskResult{
			Status: agent.TaskStatusCompleted,
			Output: map[string]any{
				"raw":        mergeResp.Content,
				"panelCount": panelCount,
				"phases":     []string{"plan", "cinematography", "acting", "merge"},
			},
			Tokens: mergeResp.Usage,
		}, nil
	}

	return &agent.TaskResult{
		Status: agent.TaskStatusCompleted,
		Output: map[string]any{
			"panels":     panels,
			"panelCount": len(panels),
			"phases":     []string{"plan", "cinematography", "acting", "merge"},
		},
		Tokens: mergeResp.Usage,
	}, nil
}

// refinePanel modifies a specific panel's properties.
func (a *Agent) refinePanel(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	panelData, _ := json.Marshal(msg.Payload["panel"])
	instruction, _ := msg.Payload["instruction"].(string)

	resp, err := a.CallLLM(ctx, agent.TierStandard,
		"You are a storyboard artist. Modify the panel based on the instruction. Return updated panel as JSON.",
		fmt.Sprintf("Current panel:\n%s\n\nInstruction: %s", string(panelData), instruction),
	)
	if err != nil {
		return nil, fmt.Errorf("refine panel: %w", err)
	}

	return &agent.TaskResult{
		Status: agent.TaskStatusCompleted,
		Output: map[string]any{"refined": resp.Content},
		Tokens: resp.Usage,
	}, nil
}

// insertPanel creates a new transition panel between two existing panels.
// Uses: agent_storyboard_insert prompt
func (a *Agent) insertPanel(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	prevPanel, _ := json.Marshal(msg.Payload["prevPanel"])
	nextPanel, _ := json.Marshal(msg.Payload["nextPanel"])
	userInput, _ := msg.Payload["instruction"].(string)
	charactersFullDescription, _ := msg.Payload["charactersFullDescription"].(string)
	locationsDescription, _ := msg.Payload["locationsDescription"].(string)

	systemPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptStoryboardInsert, map[string]string{
		"prev_panel_json":            string(prevPanel),
		"next_panel_json":            string(nextPanel),
		"user_input":                 userInput,
		"characters_full_description": charactersFullDescription,
		"locations_description":      locationsDescription,
	})

	resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt,
		fmt.Sprintf("Insert panel between:\nPrev: %s\nNext: %s\nInstruction: %s",
			string(prevPanel), string(nextPanel), userInput))
	if err != nil {
		return nil, fmt.Errorf("insert panel: %w", err)
	}

	return &agent.TaskResult{
		Status: agent.TaskStatusCompleted,
		Output: map[string]any{"newPanel": resp.Content},
		Tokens: resp.Usage,
	}, nil
}

// analyzeShotVariants generates multiple shot variant ideas for a panel.
// Uses: agent_shot_variant_analysis prompt
func (a *Agent) analyzeShotVariants(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	panelDescription, _ := msg.Payload["panelDescription"].(string)
	shotType, _ := msg.Payload["shotType"].(string)
	cameraMove, _ := msg.Payload["cameraMove"].(string)
	location, _ := msg.Payload["location"].(string)
	charactersInfo, _ := msg.Payload["charactersInfo"].(string)

	systemPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptShotVariantAnalysis, map[string]string{
		"panel_description": panelDescription,
		"shot_type":         shotType,
		"camera_move":       cameraMove,
		"location":          location,
		"characters_info":   charactersInfo,
	})

	resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt, panelDescription)
	if err != nil {
		return nil, fmt.Errorf("analyze shot variants: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"variants": resp.Content}, Tokens: resp.Usage}, nil
}

// generateShotVariant generates a new variant image with camera variation.
// Uses: agent_shot_variant_generate prompt
func (a *Agent) generateShotVariant(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	originalDescription, _ := msg.Payload["originalDescription"].(string)
	originalShotType, _ := msg.Payload["originalShotType"].(string)
	originalCameraMove, _ := msg.Payload["originalCameraMove"].(string)
	location, _ := msg.Payload["location"].(string)
	charactersInfo, _ := msg.Payload["charactersInfo"].(string)
	variantTitle, _ := msg.Payload["variantTitle"].(string)
	variantDescription, _ := msg.Payload["variantDescription"].(string)
	targetShotType, _ := msg.Payload["targetShotType"].(string)
	targetCameraMove, _ := msg.Payload["targetCameraMove"].(string)
	videoPrompt, _ := msg.Payload["videoPrompt"].(string)
	characterAssets, _ := msg.Payload["characterAssets"].(string)
	locationAsset, _ := msg.Payload["locationAsset"].(string)
	aspectRatio, _ := msg.Payload["aspectRatio"].(string)
	style, _ := msg.Payload["style"].(string)

	systemPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptShotVariantGenerate, map[string]string{
		"original_description": originalDescription,
		"original_shot_type":   originalShotType,
		"original_camera_move": originalCameraMove,
		"location":             location,
		"characters_info":      charactersInfo,
		"variant_title":        variantTitle,
		"variant_description":  variantDescription,
		"target_shot_type":     targetShotType,
		"target_camera_move":   targetCameraMove,
		"video_prompt":         videoPrompt,
		"character_assets":     characterAssets,
		"location_asset":       locationAsset,
		"aspect_ratio":         aspectRatio,
		"style":                style,
	})

	resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt, variantDescription)
	if err != nil {
		return nil, fmt.Errorf("generate shot variant: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"variant": resp.Content}, Tokens: resp.Usage}, nil
}

// queryVisualContext answers questions about visual composition rules.
func (a *Agent) queryVisualContext(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	question, _ := msg.Payload["question"].(string)
	context_, _ := json.Marshal(msg.Payload["context"])

	resp, err := a.CallLLM(ctx, agent.TierFlash,
		"You are a cinematography expert. Answer the question about visual context briefly and precisely. Return JSON.",
		fmt.Sprintf("Context:\n%s\n\nQuestion: %s", string(context_), question),
	)
	if err != nil {
		return nil, fmt.Errorf("query visual: %w", err)
	}

	return &agent.TaskResult{
		Status: agent.TaskStatusCompleted,
		Output: map[string]any{"answer": resp.Content},
		Tokens: resp.Usage,
	}, nil
}
