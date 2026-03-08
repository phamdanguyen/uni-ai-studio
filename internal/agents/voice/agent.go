// Package voice implements the Voice Agent — specialist in voice analysis,
// design, TTS generation, and lip sync.
package voice

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/uni-ai-studio/waoo-studio/internal/agent"
	"github.com/uni-ai-studio/waoo-studio/lib/prompts"
)

// Agent is the Voice Director responsible for analyzing voices, designing
// character-specific voices, generating TTS, and orchestrating lip sync.
type Agent struct {
	agent.BaseAgent
}

// New creates a new Voice Agent.
func New(bus agent.MessageBus, router agent.ModelRouter, tools agent.ToolRegistry, logger *slog.Logger) *Agent {
	card := agent.AgentCard{
		Name:        "voice",
		Version:     "2.0.0",
		Description: "Đạo diễn âm thanh AI — phân tích giọng, thiết kế voice, TTS, lip sync.",
		Skills: []agent.Skill{
			{ID: "analyze_voices", Name: "Phân tích voice cần thiết", Description: "Extract spoken lines, assign speakers, estimate emotions, map to panels", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "design_voice", Name: "Thiết kế giọng nói cho nhân vật", Description: "Design character-specific voice using AI voice tools", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "generate_tts", Name: "Sinh giọng nói (TTS)", Description: "Generate speech audio from text and voice ID", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
			{ID: "lip_sync", Name: "Lip sync audio với video", Description: "Synchronize audio with video for lip movements", InputModes: []string{"application/json"}, OutputModes: []string{"application/json"}},
		},
		Capabilities: agent.Capabilities{Streaming: true, StateTransitionHistory: true},
	}
	return &Agent{BaseAgent: agent.NewBaseAgent(card, bus, router, tools, logger)}
}

func (a *Agent) HandleMessage(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	a.Logger().Info("handling message", "skill", msg.SkillID)
	switch msg.SkillID {
	case "analyze_voices":
		return a.analyzeVoices(ctx, msg)
	case "design_voice":
		return a.designVoice(ctx, msg)
	case "generate_tts":
		return a.generateTTS(ctx, msg)
	case "lip_sync":
		return a.lipSync(ctx, msg)
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

// analyzeVoices extracts spoken lines from text, assigns speakers, estimates emotions,
// and maps each voice line to storyboard panels.
// Uses: voice_analysis prompt
func (a *Agent) analyzeVoices(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	input, _ := msg.Payload["input"].(string)
	if input == "" {
		// Backward compat: old callers may use "screenplay"
		input, _ = msg.Payload["screenplay"].(string)
	}
	charactersLibName, _ := msg.Payload["charactersLibName"].(string)
	charactersIntroduction, _ := msg.Payload["charactersIntroduction"].(string)
	storyboardJSON, _ := msg.Payload["storyboardJson"].(string)

	// If we have full context, use file-based prompt
	if storyboardJSON != "" {
		systemPrompt := prompts.MustLoadAndRender(prompts.CategoryNovelPromotion, prompts.PromptVoiceAnalysis, map[string]string{
			"input":                    input,
			"characters_lib_name":      charactersLibName,
			"characters_introduction":  charactersIntroduction,
			"storyboard_json":          storyboardJSON,
		})

		resp, err := a.CallLLM(ctx, agent.TierStandard, systemPrompt, input)
		if err != nil {
			return nil, fmt.Errorf("analyze voices: %w", err)
		}
		return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"voices": resp.Content}, Tokens: resp.Usage}, nil
	}

	// Fallback for simple analysis without storyboard context
	characters, _ := msg.Payload["characters"].(string)
	resp, err := a.CallLLM(ctx, agent.TierStandard,
		`You are a voice director. Analyze the screenplay and characters to determine voice requirements.
Return JSON: {"voiceRoles": [{"character":"...","voiceType":"male-deep/male-tenor/female-alto/female-soprano/child/narrator",
"accent":"...","emotion":"primary emotion","pace":"slow/normal/fast","lines":5}]}`,
		fmt.Sprintf("Screenplay:\n%s\n\nCharacters:\n%s", input, characters))
	if err != nil {
		return nil, fmt.Errorf("analyze voices: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: map[string]any{"voices": resp.Content}, Tokens: resp.Usage}, nil
}

func (a *Agent) designVoice(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	character, _ := msg.Payload["character"].(string)
	voiceType, _ := msg.Payload["voiceType"].(string)

	result, err := a.UseTool(ctx, "voice_designer", map[string]any{
		"character": character,
		"voiceType": voiceType,
	})
	if err != nil {
		return nil, fmt.Errorf("design voice: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: result}, nil
}

func (a *Agent) generateTTS(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	text, _ := msg.Payload["text"].(string)
	voiceID, _ := msg.Payload["voiceId"].(string)
	emotion, _ := msg.Payload["emotion"].(string)

	result, err := a.UseTool(ctx, "tts_generator", map[string]any{
		"text":    text,
		"voiceId": voiceID,
		"emotion": emotion,
	})
	if err != nil {
		return nil, fmt.Errorf("generate TTS: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: result}, nil
}

func (a *Agent) lipSync(ctx context.Context, msg agent.Message) (*agent.TaskResult, error) {
	videoURL, _ := msg.Payload["videoUrl"].(string)
	audioURL, _ := msg.Payload["audioUrl"].(string)

	result, err := a.UseTool(ctx, "lip_sync", map[string]any{
		"videoUrl": videoURL,
		"audioUrl": audioURL,
	})
	if err != nil {
		return nil, fmt.Errorf("lip sync: %w", err)
	}
	return &agent.TaskResult{Status: agent.TaskStatusCompleted, Output: result}, nil
}
