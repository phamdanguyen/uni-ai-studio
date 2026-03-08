// Package llm provides the model router for hybrid token routing across LLM providers.
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/uni-ai-studio/waoo-studio/internal/agent"
	"github.com/uni-ai-studio/waoo-studio/internal/config"
)

// Router implements agent.ModelRouter with hybrid tier-based model selection.
type Router struct {
	cfg        config.LLMConfig
	httpClient *http.Client
	logger     *slog.Logger
	circuit    *CircuitBreaker

	// Token budget tracking (per-project)
	mu      sync.RWMutex
	budgets map[string]*Budget
}

// Budget tracks per-project token spending.
type Budget struct {
	LimitUSD float64
	SpentUSD float64
}

// NewRouter creates a new LLM router.
func NewRouter(cfg config.LLMConfig, logger *slog.Logger) *Router {
	return &Router{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: time.Duration(cfg.RequestTimeoutS) * time.Second,
		},
		logger:  logger.With("component", "llm-router"),
		budgets: make(map[string]*Budget),
		circuit: NewCircuitBreaker(5, 30*time.Second, logger),
	}
}

// Call makes an LLM request using the appropriate model for the given tier.
func (r *Router) Call(ctx context.Context, tier agent.ModelTier, systemPrompt, userPrompt string) (*agent.LLMResponse, error) {
	model := r.selectModel(tier)

	reqBody := openRouterRequest{
		Model: model,
		Messages: []openRouterMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: tierTemperature(tier),
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		r.cfg.OpenRouterBaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.cfg.OpenRouterAPIKey)
	req.Header.Set("HTTP-Referer", "https://uni-ai.studio")
	req.Header.Set("X-Title", "Uni AI Studio")

	if !r.circuit.Allow() {
		return nil, fmt.Errorf("LLM circuit breaker open: provider temporarily unavailable")
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		r.circuit.RecordFailure()
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		r.circuit.RecordFailure()
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		r.circuit.RecordFailure()
		return nil, fmt.Errorf("LLM API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var orResp openRouterResponse
	if err := json.Unmarshal(respBody, &orResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(orResp.Choices) == 0 {
		r.circuit.RecordFailure()
		return nil, fmt.Errorf("empty response from model %s", model)
	}

	r.circuit.RecordSuccess()
	return &agent.LLMResponse{
		Content: orResp.Choices[0].Message.Content,
		Model:   orResp.Model,
		Usage: agent.TokenUsage{
			InputTokens:  orResp.Usage.PromptTokens,
			OutputTokens: orResp.Usage.CompletionTokens,
			TotalTokens:  orResp.Usage.TotalTokens,
			CostUSD:      orResp.Usage.Cost,
			Model:        orResp.Model,
			Tier:         string(tier),
		},
		StopReason: orResp.Choices[0].FinishReason,
	}, nil
}

// CallWithJSON makes an LLM request expecting structured JSON output.
// Enables response_format: json_object for models that support it, and
// appends a JSON-only instruction for models that don't.
func (r *Router) CallWithJSON(ctx context.Context, tier agent.ModelTier, systemPrompt, userPrompt string, _ any) (*agent.LLMResponse, error) {
	model := r.selectModel(tier)
	enhancedSystem := systemPrompt + "\n\nYou MUST respond with valid JSON only. No markdown, no explanation."

	reqBody := openRouterRequest{
		Model: model,
		Messages: []openRouterMessage{
			{Role: "system", Content: enhancedSystem},
			{Role: "user", Content: userPrompt},
		},
		Temperature:    tierTemperature(tier),
		ResponseFormat: &openRouterResponseFormat{Type: "json_object"},
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		r.cfg.OpenRouterBaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.cfg.OpenRouterAPIKey)
	req.Header.Set("HTTP-Referer", "https://uni-ai.studio")
	req.Header.Set("X-Title", "Uni AI Studio")

	if !r.circuit.Allow() {
		return nil, fmt.Errorf("LLM circuit breaker open: provider temporarily unavailable")
	}

	resp, err := r.httpClient.Do(req)
	if err != nil {
		r.circuit.RecordFailure()
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		r.circuit.RecordFailure()
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		r.circuit.RecordFailure()
		return nil, fmt.Errorf("LLM API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var orResp openRouterResponse
	if err := json.Unmarshal(respBody, &orResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	if len(orResp.Choices) == 0 {
		r.circuit.RecordFailure()
		return nil, fmt.Errorf("empty response from model %s", model)
	}

	r.circuit.RecordSuccess()
	return &agent.LLMResponse{
		Content: orResp.Choices[0].Message.Content,
		Model:   orResp.Model,
		Usage: agent.TokenUsage{
			InputTokens:  orResp.Usage.PromptTokens,
			OutputTokens: orResp.Usage.CompletionTokens,
			TotalTokens:  orResp.Usage.TotalTokens,
			CostUSD:      orResp.Usage.Cost,
			Model:        orResp.Model,
			Tier:         string(tier),
		},
		StopReason: orResp.Choices[0].FinishReason,
	}, nil
}

// CheckBudget verifies the project hasn't exceeded its token budget.
func (r *Router) CheckBudget(_ context.Context, projectID string) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	budget, exists := r.budgets[projectID]
	if !exists {
		return nil // No budget set = unlimited
	}

	if budget.SpentUSD >= budget.LimitUSD {
		return fmt.Errorf("token budget exceeded for project %s: spent $%.2f / limit $%.2f",
			projectID, budget.SpentUSD, budget.LimitUSD)
	}

	return nil
}

// RecordUsage records token usage for billing and budget tracking.
func (r *Router) RecordUsage(_ context.Context, projectID string, usage agent.TokenUsage) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	budget, exists := r.budgets[projectID]
	if !exists {
		budget = &Budget{LimitUSD: r.cfg.DefaultBudgetUSD}
		r.budgets[projectID] = budget
	}

	budget.SpentUSD += usage.CostUSD
	r.logger.Info("token usage recorded",
		"project", projectID,
		"model", usage.Model,
		"tokens", usage.TotalTokens,
		"cost", usage.CostUSD,
		"totalSpent", budget.SpentUSD,
	)

	return nil
}

// SetBudget sets the token budget for a project.
func (r *Router) SetBudget(projectID string, limitUSD float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	budget, exists := r.budgets[projectID]
	if !exists {
		budget = &Budget{}
		r.budgets[projectID] = budget
	}
	budget.LimitUSD = limitUSD
}

// LLMSettingsJSON is the JSON representation of LLM settings for the API.
type LLMSettingsJSON struct {
	OpenRouterApiKey  string  `json:"openRouterApiKey"`
	OpenRouterBaseUrl string  `json:"openRouterBaseUrl"`
	GoogleAiKey       string  `json:"googleAiKey"`
	AnthropicKey      string  `json:"anthropicKey"`
	FlashModel        string  `json:"flashModel"`
	StandardModel     string  `json:"standardModel"`
	PremiumModel      string  `json:"premiumModel"`
	DefaultBudgetUsd  float64 `json:"defaultBudgetUsd"`
	RequestTimeoutS   int     `json:"requestTimeoutS"`
}

// maskKey returns a masked version of an API key for safe display.
func maskKey(key string) string {
	if len(key) <= 4 {
		if key == "" {
			return ""
		}
		return "****"
	}
	return "****" + key[len(key)-4:]
}

// GetConfig returns the current LLM configuration (with masked API keys).
func (r *Router) GetConfig() LLMSettingsJSON {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return LLMSettingsJSON{
		OpenRouterApiKey:  maskKey(r.cfg.OpenRouterAPIKey),
		OpenRouterBaseUrl: r.cfg.OpenRouterBaseURL,
		GoogleAiKey:       maskKey(r.cfg.GoogleAIKey),
		AnthropicKey:      maskKey(r.cfg.AnthropicKey),
		FlashModel:        r.cfg.FlashModel,
		StandardModel:     r.cfg.StandardModel,
		PremiumModel:      r.cfg.PremiumModel,
		DefaultBudgetUsd:  r.cfg.DefaultBudgetUSD,
		RequestTimeoutS:   r.cfg.RequestTimeoutS,
	}
}

// UpdateConfig updates runtime LLM configuration.
// API keys starting with "****" are ignored (masked values from frontend).
func (r *Router) UpdateConfig(update LLMSettingsJSON) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Only update API keys if they are not masked
	if update.OpenRouterApiKey != "" && update.OpenRouterApiKey[:4] != "****" {
		r.cfg.OpenRouterAPIKey = update.OpenRouterApiKey
	}
	if update.OpenRouterBaseUrl != "" {
		r.cfg.OpenRouterBaseURL = update.OpenRouterBaseUrl
	}
	if update.GoogleAiKey != "" && (len(update.GoogleAiKey) < 4 || update.GoogleAiKey[:4] != "****") {
		r.cfg.GoogleAIKey = update.GoogleAiKey
	}
	if update.AnthropicKey != "" && (len(update.AnthropicKey) < 4 || update.AnthropicKey[:4] != "****") {
		r.cfg.AnthropicKey = update.AnthropicKey
	}

	// Always update model routing and budget
	if update.FlashModel != "" {
		r.cfg.FlashModel = update.FlashModel
	}
	if update.StandardModel != "" {
		r.cfg.StandardModel = update.StandardModel
	}
	if update.PremiumModel != "" {
		r.cfg.PremiumModel = update.PremiumModel
	}
	if update.DefaultBudgetUsd > 0 {
		r.cfg.DefaultBudgetUSD = update.DefaultBudgetUsd
	}
	if update.RequestTimeoutS > 0 {
		r.cfg.RequestTimeoutS = update.RequestTimeoutS
		r.httpClient.Timeout = time.Duration(update.RequestTimeoutS) * time.Second
	}

	r.logger.Info("LLM config updated",
		"flashModel", r.cfg.FlashModel,
		"standardModel", r.cfg.StandardModel,
		"premiumModel", r.cfg.PremiumModel,
	)
}

func (r *Router) selectModel(tier agent.ModelTier) string {
	switch tier {
	case agent.TierFlash:
		return r.cfg.FlashModel
	case agent.TierStandard:
		return r.cfg.StandardModel
	case agent.TierPremium:
		return r.cfg.PremiumModel
	default:
		return r.cfg.StandardModel
	}
}

func tierTemperature(tier agent.ModelTier) float64 {
	switch tier {
	case agent.TierFlash:
		return 0.3 // More deterministic for parsing/routing
	case agent.TierStandard:
		return 0.7 // Balanced for creative + structured
	case agent.TierPremium:
		return 0.5 // Careful reasoning
	default:
		return 0.7
	}
}

// --- OpenRouter API types ---

type openRouterResponseFormat struct {
	Type string `json:"type"` // "json_object" | "text"
}

type openRouterRequest struct {
	Model          string                    `json:"model"`
	Messages       []openRouterMessage       `json:"messages"`
	Temperature    float64                   `json:"temperature,omitempty"`
	MaxTokens      int                       `json:"max_tokens,omitempty"`
	ResponseFormat *openRouterResponseFormat `json:"response_format,omitempty"`
}

type openRouterMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openRouterResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Message      openRouterMessage `json:"message"`
		FinishReason string            `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int     `json:"prompt_tokens"`
		CompletionTokens int     `json:"completion_tokens"`
		TotalTokens      int     `json:"total_tokens"`
		Cost             float64 `json:"cost"` // USD cost from OpenRouter
	} `json:"usage"`
}
