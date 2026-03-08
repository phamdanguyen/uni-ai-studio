// Package qualitygate — retry loop with LLM feedback.
package qualitygate

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/uni-ai-studio/waoo-studio/internal/agent"
)

// RetryConfig controls retry behavior.
type RetryConfig struct {
	MaxRetries int     // Max retries per panel (default: 2)
	Threshold  float64 // Quality threshold (default: 0.7)
}

// RetryLoop implements quality-driven regeneration.
// It generates → evaluates → refines prompt → regenerates until passing or max retries.
type RetryLoop struct {
	evaluator *Evaluator
	router    agent.ModelRouter
	config    RetryConfig
	logger    *slog.Logger
}

// NewRetryLoop creates a retry loop with quality feedback.
func NewRetryLoop(evaluator *Evaluator, router agent.ModelRouter, config RetryConfig, logger *slog.Logger) *RetryLoop {
	if config.MaxRetries == 0 {
		config.MaxRetries = 2
	}
	if config.Threshold == 0 {
		config.Threshold = 0.7
	}
	return &RetryLoop{
		evaluator: evaluator,
		router:    router,
		config:    config,
		logger:    logger.With("component", "retry-loop"),
	}
}

// RetryResult holds the final output after retry attempts.
type RetryResult struct {
	PanelIndex      int     `json:"panelIndex"`
	FinalScore      Score   `json:"finalScore"`
	Attempts        int     `json:"attempts"`
	RefinedPrompt   string  `json:"refinedPrompt,omitempty"`
	OriginalPrompt  string  `json:"originalPrompt"`
	ResultURL       string  `json:"resultUrl"`
	Passed          bool    `json:"passed"`
}

// GenerateFunc generates media from a prompt and returns the result URL.
type GenerateFunc func(ctx context.Context, prompt string, spec map[string]any) (string, error)

// RunPanel evaluates a panel and retries with refined prompts if quality is insufficient.
func (r *RetryLoop) RunPanel(
	ctx context.Context,
	panelIndex int,
	originalPrompt string,
	resultURL string,
	spec map[string]any,
	generate GenerateFunc,
) (*RetryResult, error) {
	result := &RetryResult{
		PanelIndex:     panelIndex,
		OriginalPrompt: originalPrompt,
		ResultURL:      resultURL,
		Attempts:       1,
	}

	// First evaluation
	score, err := r.evaluator.EvaluateImage(ctx, resultURL, originalPrompt, spec)
	if err != nil {
		return nil, fmt.Errorf("initial evaluation: %w", err)
	}

	result.FinalScore = *score
	if score.Pass {
		result.Passed = true
		return result, nil
	}

	// Retry loop
	currentPrompt := originalPrompt
	currentURL := resultURL

	for attempt := 1; attempt <= r.config.MaxRetries; attempt++ {
		r.logger.Info("retrying panel",
			"panel", panelIndex,
			"attempt", attempt,
			"score", score.Overall,
			"issues", score.Issues,
		)

		// Refine prompt using LLM feedback
		refinedPrompt, err := r.refinePrompt(ctx, currentPrompt, score)
		if err != nil {
			r.logger.Warn("prompt refinement failed, using previous", "error", err)
			refinedPrompt = currentPrompt
		}

		// Regenerate
		newURL, err := generate(ctx, refinedPrompt, spec)
		if err != nil {
			r.logger.Warn("regeneration failed", "attempt", attempt, "error", err)
			continue
		}

		// Re-evaluate
		score, err = r.evaluator.EvaluateImage(ctx, newURL, refinedPrompt, spec)
		if err != nil {
			r.logger.Warn("re-evaluation failed", "error", err)
			continue
		}

		result.Attempts = attempt + 1
		result.FinalScore = *score
		result.ResultURL = newURL
		result.RefinedPrompt = refinedPrompt
		currentPrompt = refinedPrompt
		currentURL = newURL
		_ = currentURL // suppress unused

		if score.Pass {
			result.Passed = true
			r.logger.Info("panel passed after retry",
				"panel", panelIndex,
				"attempt", attempt,
				"score", score.Overall,
			)
			return result, nil
		}
	}

	// Max retries reached — use best result available
	result.Passed = false
	r.logger.Warn("panel failed after max retries",
		"panel", panelIndex,
		"finalScore", result.FinalScore.Overall,
		"attempts", result.Attempts,
	)
	return result, nil
}

// refinePrompt asks LLM to improve the prompt based on quality feedback.
func (r *RetryLoop) refinePrompt(ctx context.Context, prompt string, score *Score) (string, error) {
	issuesList := ""
	for _, issue := range score.Issues {
		issuesList += "- " + issue + "\n"
	}

	resp, err := r.router.Call(ctx, agent.TierFlash,
		`You are a prompt engineer for AI image generation.
Given a prompt that produced a suboptimal result, improve it.
Fix the listed issues while preserving the core visual intent.
Return ONLY the improved prompt, no explanation.`,
		fmt.Sprintf(`Original prompt:
%s

Quality score: %.2f/1.0
Issues found:
%s
Suggestion: %s

Write an improved prompt:`, prompt, score.Overall, issuesList, score.Suggestion),
	)
	if err != nil {
		return "", err
	}

	return resp.Content, nil
}
