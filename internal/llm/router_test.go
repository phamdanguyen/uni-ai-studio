package llm

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/uni-ai-studio/waoo-studio/internal/agent"
	"github.com/uni-ai-studio/waoo-studio/internal/config"
)

func testRouterLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func testRouterCfg() config.LLMConfig {
	return config.LLMConfig{
		FlashModel:       "google/gemini-flash",
		StandardModel:    "anthropic/claude-sonnet",
		PremiumModel:     "anthropic/claude-opus",
		DefaultBudgetUSD: 10.0,
		RequestTimeoutS:  30,
	}
}

// newTestRouter creates a Router without starting the rate limiter goroutine.
func newTestRouter() *Router {
	cfg := testRouterCfg()
	rateCh := make(chan struct{}, 8)
	for i := 0; i < 8; i++ {
		rateCh <- struct{}{}
	}
	return &Router{
		cfg:         cfg,
		logger:      testRouterLogger(),
		budgets:     make(map[string]*Budget),
		rateCh:      rateCh,
		circuit:     NewCircuitBreaker(5, 0, testRouterLogger()),
		agentModels: make(map[string]map[agent.ModelTier]string),
	}
}

// --- maskKey tests ---

func TestMaskKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", ""},
		{"ab", "****"},
		{"sk-abc12345678", "****5678"},
	}
	for _, tt := range tests {
		got := maskKey(tt.input)
		if got != tt.want {
			t.Errorf("maskKey(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// --- isGemmaModel tests ---

func TestIsGemmaModel(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"gemma-7b", true},
		{"models/gemma-2b", true},
		{"claude-3", false},
		{"gem", false},
		{"", false},
	}
	for _, tt := range tests {
		got := isGemmaModel(tt.model)
		if got != tt.want {
			t.Errorf("isGemmaModel(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}

// --- tierTemperature tests ---

func TestTierTemperature(t *testing.T) {
	tests := []struct {
		tier agent.ModelTier
		want float64
	}{
		{agent.TierFlash, 0.3},
		{agent.TierStandard, 0.7},
		{agent.TierPremium, 0.5},
		{"unknown", 0.7},
	}
	for _, tt := range tests {
		got := tierTemperature(tt.tier)
		if got != tt.want {
			t.Errorf("tierTemperature(%q) = %f, want %f", tt.tier, got, tt.want)
		}
	}
}

// --- buildMessages tests ---

func TestBuildMessages_GemmaMerged(t *testing.T) {
	r := newTestRouter()
	msgs := r.buildMessages("gemma-7b", "system prompt", "user prompt")

	if len(msgs) != 1 {
		t.Fatalf("expected 1 message for Gemma, got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("expected role 'user', got %q", msgs[0].Role)
	}
	if msgs[0].Content != "system prompt\n\nuser prompt" {
		t.Errorf("expected merged content, got %q", msgs[0].Content)
	}
}

func TestBuildMessages_Normal(t *testing.T) {
	r := newTestRouter()
	msgs := r.buildMessages("claude-3", "system prompt", "user prompt")

	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages for normal model, got %d", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[0].Content != "system prompt" {
		t.Errorf("expected system message, got %+v", msgs[0])
	}
	if msgs[1].Role != "user" || msgs[1].Content != "user prompt" {
		t.Errorf("expected user message, got %+v", msgs[1])
	}
}

// --- selectModel tests ---

func TestSelectModel(t *testing.T) {
	r := newTestRouter()

	tests := []struct {
		tier agent.ModelTier
		want string
	}{
		{agent.TierFlash, "google/gemini-flash"},
		{agent.TierStandard, "anthropic/claude-sonnet"},
		{agent.TierPremium, "anthropic/claude-opus"},
		{"unknown", "anthropic/claude-sonnet"},
	}
	for _, tt := range tests {
		got := r.selectModel(tt.tier)
		if got != tt.want {
			t.Errorf("selectModel(%q) = %q, want %q", tt.tier, got, tt.want)
		}
	}
}

// --- Budget tests ---

func TestCheckBudget_NoBudgetReturnsNil(t *testing.T) {
	r := newTestRouter()
	if err := r.CheckBudget(context.Background(), "proj-1"); err != nil {
		t.Fatalf("expected nil for no budget, got %v", err)
	}
}

func TestCheckBudget_UnderBudgetReturnsNil(t *testing.T) {
	r := newTestRouter()
	r.SetBudget("proj-1", 10.0)
	if err := r.RecordUsage(context.Background(), "proj-1", agent.TokenUsage{CostUSD: 5.0}); err != nil {
		t.Fatal(err)
	}

	if err := r.CheckBudget(context.Background(), "proj-1"); err != nil {
		t.Fatalf("expected nil when under budget, got %v", err)
	}
}

func TestCheckBudget_OverBudgetReturnsError(t *testing.T) {
	r := newTestRouter()
	r.SetBudget("proj-1", 5.0)
	if err := r.RecordUsage(context.Background(), "proj-1", agent.TokenUsage{CostUSD: 6.0}); err != nil {
		t.Fatal(err)
	}

	if err := r.CheckBudget(context.Background(), "proj-1"); err == nil {
		t.Fatal("expected error when over budget")
	}
}

func TestRecordUsage_CreatesBudgetAndAccumulates(t *testing.T) {
	r := newTestRouter()

	if err := r.RecordUsage(context.Background(), "proj-1", agent.TokenUsage{CostUSD: 3.0}); err != nil {
		t.Fatal(err)
	}
	if err := r.RecordUsage(context.Background(), "proj-1", agent.TokenUsage{CostUSD: 2.0}); err != nil {
		t.Fatal(err)
	}

	r.mu.RLock()
	budget := r.budgets["proj-1"]
	r.mu.RUnlock()

	if budget == nil {
		t.Fatal("expected budget to be created")
	}
	if budget.SpentUSD != 5.0 {
		t.Fatalf("expected SpentUSD = 5.0, got %f", budget.SpentUSD)
	}
	if budget.LimitUSD != 10.0 { // DefaultBudgetUSD
		t.Fatalf("expected LimitUSD = 10.0 (default), got %f", budget.LimitUSD)
	}
}

func TestSetBudget(t *testing.T) {
	r := newTestRouter()
	r.SetBudget("proj-1", 25.0)

	r.mu.RLock()
	budget := r.budgets["proj-1"]
	r.mu.RUnlock()

	if budget == nil {
		t.Fatal("expected budget to be created")
	}
	if budget.LimitUSD != 25.0 {
		t.Fatalf("expected LimitUSD = 25.0, got %f", budget.LimitUSD)
	}
}

// --- Agent model override tests ---

func TestAgentModelOverride_SetGetClear(t *testing.T) {
	r := newTestRouter()

	// Set override
	r.SetAgentModel("director", agent.TierFlash, "custom/model-fast")

	// Get override
	overrides := r.GetAgentModels("director")
	if overrides["flash"] != "custom/model-fast" {
		t.Fatalf("expected override 'custom/model-fast', got %q", overrides["flash"])
	}

	// selectModelForAgent should use override
	got := r.selectModelForAgent("director", agent.TierFlash)
	if got != "custom/model-fast" {
		t.Fatalf("expected selectModelForAgent to return override, got %q", got)
	}

	// selectModelForAgent for non-overridden tier should fallback
	got = r.selectModelForAgent("director", agent.TierPremium)
	if got != "anthropic/claude-opus" {
		t.Fatalf("expected fallback for non-overridden tier, got %q", got)
	}

	// Clear override
	r.ClearAgentModel("director", agent.TierFlash)
	got = r.selectModelForAgent("director", agent.TierFlash)
	if got != "google/gemini-flash" {
		t.Fatalf("expected fallback after clear, got %q", got)
	}

	// GetAgentModels for unknown agent returns empty map
	empty := r.GetAgentModels("nonexistent")
	if len(empty) != 0 {
		t.Fatalf("expected empty map, got %v", empty)
	}
}

// --- GetConfig masks keys, UpdateConfig ignores masked keys ---

func TestGetConfig_MasksKeys(t *testing.T) {
	r := newTestRouter()
	r.cfg.OpenRouterAPIKey = "sk-abcdefgh12345678"
	r.cfg.GoogleAIKey = "AIza1234567890"
	r.cfg.AnthropicKey = "sk-ant-12345678"

	cfg := r.GetConfig()
	if cfg.OpenRouterApiKey != "****5678" {
		t.Errorf("expected masked OpenRouterApiKey, got %q", cfg.OpenRouterApiKey)
	}
	if cfg.GoogleAiKey != "****7890" {
		t.Errorf("expected masked GoogleAiKey, got %q", cfg.GoogleAiKey)
	}
	if cfg.AnthropicKey != "****5678" {
		t.Errorf("expected masked AnthropicKey, got %q", cfg.AnthropicKey)
	}
}

func TestUpdateConfig_IgnoresMaskedKeys(t *testing.T) {
	r := newTestRouter()
	r.cfg.OpenRouterAPIKey = "sk-original-key-12345678"

	r.UpdateConfig(LLMSettingsJSON{
		OpenRouterApiKey: "****5678", // masked value — should be ignored
		FlashModel:       "new-flash-model",
	})

	if r.cfg.OpenRouterAPIKey != "sk-original-key-12345678" {
		t.Fatalf("expected masked key to be ignored, got %q", r.cfg.OpenRouterAPIKey)
	}
	if r.cfg.FlashModel != "new-flash-model" {
		t.Fatalf("expected FlashModel update, got %q", r.cfg.FlashModel)
	}
}
