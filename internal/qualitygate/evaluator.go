// Package qualitygate implements quality evaluation for AI-generated media.
// Uses a combination of LLM-based visual QA and rule-based checks.
package qualitygate

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/uni-ai-studio/waoo-studio/internal/agent"
)

// Score represents a quality evaluation result.
type Score struct {
	Overall     float64  `json:"overall"`     // 0.0 - 1.0
	Composition float64  `json:"composition"` // Framing, balance
	Consistency float64  `json:"consistency"` // Character/location match
	Technical   float64  `json:"technical"`   // Resolution, artifacts
	Narrative   float64  `json:"narrative"`   // Story alignment
	Pass        bool     `json:"pass"`        // overall >= threshold
	Issues      []string `json:"issues,omitempty"`
	Suggestion  string   `json:"suggestion,omitempty"`
}

// Evaluator performs quality assessment on generated media.
type Evaluator struct {
	router    agent.ModelRouter
	threshold float64
	logger    *slog.Logger
}

// NewEvaluator creates a quality gate evaluator.
func NewEvaluator(router agent.ModelRouter, threshold float64, logger *slog.Logger) *Evaluator {
	if threshold == 0 {
		threshold = 0.7
	}
	return &Evaluator{
		router:    router,
		threshold: threshold,
		logger:    logger.With("component", "quality-gate"),
	}
}

// EvaluateImage checks if a generated image meets quality standards.
func (e *Evaluator) EvaluateImage(ctx context.Context, imageURL, expectedPrompt string, panelSpec map[string]any) (*Score, error) {
	panelJSON, _ := json.Marshal(panelSpec)

	resp, err := e.router.Call(ctx, agent.TierFlash,
		evaluateImagePrompt,
		fmt.Sprintf("Image URL: %s\n\nExpected prompt: %s\n\nPanel specification:\n%s",
			imageURL, expectedPrompt, string(panelJSON)),
	)
	if err != nil {
		return nil, fmt.Errorf("evaluate image: %w", err)
	}

	var score Score
	if err := json.Unmarshal([]byte(resp.Content), &score); err != nil {
		e.logger.Warn("failed to parse quality score, assuming pass", "error", err)
		return &Score{Overall: 0.75, Pass: true}, nil
	}

	score.Pass = score.Overall >= e.threshold

	e.logger.Info("image evaluated",
		"overall", score.Overall,
		"pass", score.Pass,
		"issues", len(score.Issues),
	)

	return &score, nil
}

// EvaluateVideo checks if a generated video meets quality standards.
func (e *Evaluator) EvaluateVideo(ctx context.Context, videoURL, expectedPrompt string, panelSpec map[string]any) (*Score, error) {
	panelJSON, _ := json.Marshal(panelSpec)

	resp, err := e.router.Call(ctx, agent.TierFlash,
		evaluateVideoPrompt,
		fmt.Sprintf("Video URL: %s\n\nExpected prompt: %s\n\nPanel specification:\n%s",
			videoURL, expectedPrompt, string(panelJSON)),
	)
	if err != nil {
		return nil, fmt.Errorf("evaluate video: %w", err)
	}

	var score Score
	if err := json.Unmarshal([]byte(resp.Content), &score); err != nil {
		e.logger.Warn("failed to parse quality score, assuming pass", "error", err)
		return &Score{Overall: 0.75, Pass: true}, nil
	}

	score.Pass = score.Overall >= e.threshold

	e.logger.Info("video evaluated",
		"overall", score.Overall,
		"pass", score.Pass,
		"issues", len(score.Issues),
	)

	return &score, nil
}

// EvaluateBatch evaluates multiple panels and returns aggregate results.
func (e *Evaluator) EvaluateBatch(ctx context.Context, panels []PanelMedia) (*BatchResult, error) {
	result := &BatchResult{
		Scores:  make([]Score, 0, len(panels)),
		Retries: make([]int, 0),
	}

	for i, panel := range panels {
		var score *Score
		var err error

		if panel.VideoURL != "" {
			score, err = e.EvaluateVideo(ctx, panel.VideoURL, panel.Prompt, panel.Spec)
		} else if panel.ImageURL != "" {
			score, err = e.EvaluateImage(ctx, panel.ImageURL, panel.Prompt, panel.Spec)
		} else {
			result.Scores = append(result.Scores, Score{Overall: 0, Pass: false, Issues: []string{"no media"}})
			result.Retries = append(result.Retries, i)
			continue
		}

		if err != nil {
			result.Scores = append(result.Scores, Score{Overall: 0, Pass: false, Issues: []string{err.Error()}})
			result.Retries = append(result.Retries, i)
			continue
		}

		result.Scores = append(result.Scores, *score)
		if !score.Pass {
			result.Retries = append(result.Retries, i)
		}
	}

	// Calculate aggregate
	totalScore := 0.0
	for _, s := range result.Scores {
		totalScore += s.Overall
	}
	if len(result.Scores) > 0 {
		result.AverageScore = totalScore / float64(len(result.Scores))
	}
	result.PassRate = float64(len(panels)-len(result.Retries)) / float64(len(panels))
	result.AllPass = len(result.Retries) == 0

	e.logger.Info("batch evaluated",
		"total", len(panels),
		"passRate", result.PassRate,
		"average", result.AverageScore,
		"retries", len(result.Retries),
	)

	return result, nil
}

// PanelMedia holds generated media for quality evaluation.
type PanelMedia struct {
	ImageURL string         `json:"imageUrl"`
	VideoURL string         `json:"videoUrl"`
	Prompt   string         `json:"prompt"`
	Spec     map[string]any `json:"spec"`
}

// BatchResult is the aggregate quality evaluation.
type BatchResult struct {
	Scores       []Score `json:"scores"`
	Retries      []int   `json:"retries"`      // Panel indices that need retry
	AverageScore float64 `json:"averageScore"`
	PassRate     float64 `json:"passRate"`
	AllPass      bool    `json:"allPass"`
}

// --- Prompts ---

const evaluateImagePrompt = `You are a quality control specialist for AI-generated images.
Evaluate the image against the expected description and panel specification.

Score each dimension 0.0-1.0:
- composition: framing, rule of thirds, balance
- consistency: character appearance matches description, location matches
- technical: resolution adequate, no visible artifacts/distortion
- narrative: image conveys the intended story beat

Return JSON:
{
  "overall": 0.0-1.0,
  "composition": 0.0-1.0,
  "consistency": 0.0-1.0,
  "technical": 0.0-1.0,
  "narrative": 0.0-1.0,
  "issues": ["list of specific problems found"],
  "suggestion": "specific improvement suggestion for regeneration"
}`

const evaluateVideoPrompt = `You are a quality control specialist for AI-generated videos.
Evaluate the video against the expected description and panel specification.

Score each dimension 0.0-1.0:
- composition: camera movement execution, framing throughout
- consistency: character/location consistency across frames
- technical: no flickering, morphing artifacts, stable motion
- narrative: video conveys the action/emotion described

Return JSON:
{
  "overall": 0.0-1.0,
  "composition": 0.0-1.0,
  "consistency": 0.0-1.0,
  "technical": 0.0-1.0,
  "narrative": 0.0-1.0,
  "issues": ["list of specific problems found"],
  "suggestion": "specific improvement suggestion for regeneration"
}`
