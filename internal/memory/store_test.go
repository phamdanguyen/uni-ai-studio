package memory

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

func testMemLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// --- hotStore tests ---

func TestHotStore_SetGetRoundtrip(t *testing.T) {
	h := newHotStore()
	entry := &Entry{
		Key:        "k1",
		Value:      map[string]any{"hello": "world"},
		CreatedAt:  time.Now(),
		AccessedAt: time.Now(),
	}
	h.set("k1", entry)

	got := h.get("k1")
	if got == nil {
		t.Fatal("expected entry, got nil")
	}
	if got.Value["hello"] != "world" {
		t.Fatalf("expected Value['hello'] = 'world', got %v", got.Value["hello"])
	}
}

func TestHotStore_GetMissing(t *testing.T) {
	h := newHotStore()
	got := h.get("nonexistent")
	if got != nil {
		t.Fatalf("expected nil for missing key, got %v", got)
	}
}

func TestHotStore_Expiry(t *testing.T) {
	h := newHotStore()
	entry := &Entry{
		Key:        "k1",
		Value:      map[string]any{"data": "yes"},
		TTL:        1 * time.Millisecond,
		CreatedAt:  time.Now(),
		AccessedAt: time.Now(),
	}
	h.set("k1", entry)

	time.Sleep(5 * time.Millisecond)

	got := h.get("k1")
	if got != nil {
		t.Fatal("expected nil for expired entry")
	}
}

func TestHotStore_EvictionOnMaxSize(t *testing.T) {
	h := &hotStore{
		entries: make(map[string]*Entry),
		maxSize: 2,
	}

	now := time.Now()
	h.set("k1", &Entry{Key: "k1", Value: map[string]any{}, AccessedAt: now.Add(-2 * time.Second)})
	h.set("k2", &Entry{Key: "k2", Value: map[string]any{}, AccessedAt: now.Add(-1 * time.Second)})

	// Adding k3 should evict k1 (oldest AccessedAt)
	h.set("k3", &Entry{Key: "k3", Value: map[string]any{}, AccessedAt: now})

	if h.get("k1") != nil {
		t.Fatal("expected k1 to be evicted")
	}
	if h.get("k2") == nil {
		t.Fatal("expected k2 to still exist")
	}
	if h.get("k3") == nil {
		t.Fatal("expected k3 to exist")
	}
}

// --- Mock backends ---

type mockWarmBackend struct {
	data    map[string]*Entry
	getCalls int
	setCalls int
	delCalls int
}

func newMockWarm() *mockWarmBackend {
	return &mockWarmBackend{data: make(map[string]*Entry)}
}

func (m *mockWarmBackend) Get(_ context.Context, key string) (*Entry, error) {
	m.getCalls++
	e, ok := m.data[key]
	if !ok {
		return nil, nil
	}
	return e, nil
}

func (m *mockWarmBackend) Set(_ context.Context, key string, entry *Entry) error {
	m.setCalls++
	m.data[key] = entry
	return nil
}

func (m *mockWarmBackend) Delete(_ context.Context, key string) error {
	m.delCalls++
	delete(m.data, key)
	return nil
}

func (m *mockWarmBackend) Scan(_ context.Context, _ string, _ int) ([]*Entry, error) {
	return nil, nil
}

type mockColdBackend struct {
	data     map[string]*Entry
	getCalls int
	setCalls int
	delCalls int
}

func newMockCold() *mockColdBackend {
	return &mockColdBackend{data: make(map[string]*Entry)}
}

func (m *mockColdBackend) Get(_ context.Context, key string) (*Entry, error) {
	m.getCalls++
	e, ok := m.data[key]
	if !ok {
		return nil, nil
	}
	return e, nil
}

func (m *mockColdBackend) Set(_ context.Context, key string, entry *Entry) error {
	m.setCalls++
	m.data[key] = entry
	return nil
}

func (m *mockColdBackend) Delete(_ context.Context, key string) error {
	m.delCalls++
	delete(m.data, key)
	return nil
}

func (m *mockColdBackend) Query(_ context.Context, _ []string, _ int) ([]*Entry, error) {
	return nil, nil
}

// --- Store integration tests with mocks ---

func TestStore_HotHitNoWarmColdCalls(t *testing.T) {
	warm := newMockWarm()
	cold := newMockCold()
	s := NewStore(warm, cold, testMemLogger())
	ctx := context.Background()

	if err := s.Set(ctx, "k1", map[string]any{"v": 1}); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(ctx, "k1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected hot hit")
	}
	if warm.getCalls != 0 {
		t.Fatalf("expected 0 warm Get calls, got %d", warm.getCalls)
	}
	if cold.getCalls != 0 {
		t.Fatalf("expected 0 cold Get calls, got %d", cold.getCalls)
	}
}

func TestStore_WarmHitPromotesToHot(t *testing.T) {
	warm := newMockWarm()
	cold := newMockCold()
	s := NewStore(warm, cold, testMemLogger())
	ctx := context.Background()

	// Put directly in warm
	warm.data["k1"] = &Entry{Key: "k1", Value: map[string]any{"from": "warm"}}

	got, err := s.Get(ctx, "k1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Value["from"] != "warm" {
		t.Fatalf("expected warm hit, got %v", got)
	}
	if warm.getCalls != 1 {
		t.Fatalf("expected 1 warm Get call, got %d", warm.getCalls)
	}

	// Second get should be a hot hit
	warm.getCalls = 0
	got2, _ := s.Get(ctx, "k1")
	if got2 == nil {
		t.Fatal("expected hot hit on second get")
	}
	if warm.getCalls != 0 {
		t.Fatalf("expected 0 warm calls on second get (hot hit), got %d", warm.getCalls)
	}
}

func TestStore_ColdHitPromotesToWarmAndHot(t *testing.T) {
	warm := newMockWarm()
	cold := newMockCold()
	s := NewStore(warm, cold, testMemLogger())
	ctx := context.Background()

	// Put directly in cold
	cold.data["k1"] = &Entry{Key: "k1", Value: map[string]any{"from": "cold"}}

	got, err := s.Get(ctx, "k1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Value["from"] != "cold" {
		t.Fatalf("expected cold hit, got %v", got)
	}

	// Should have promoted to warm
	if warm.setCalls != 1 {
		t.Fatalf("expected 1 warm Set call (promotion), got %d", warm.setCalls)
	}

	// Should be in hot now
	warm.getCalls = 0
	cold.getCalls = 0
	got2, _ := s.Get(ctx, "k1")
	if got2 == nil {
		t.Fatal("expected hot hit after cold promotion")
	}
	if warm.getCalls != 0 {
		t.Fatalf("expected 0 warm calls (hot hit), got %d", warm.getCalls)
	}
}

func TestStore_AllMissReturnsNil(t *testing.T) {
	warm := newMockWarm()
	cold := newMockCold()
	s := NewStore(warm, cold, testMemLogger())
	ctx := context.Background()

	got, err := s.Get(ctx, "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("expected nil for all-miss, got %v", got)
	}
}

func TestStore_SetWithTierRouting(t *testing.T) {
	warm := newMockWarm()
	cold := newMockCold()
	s := NewStore(warm, cold, testMemLogger())
	ctx := context.Background()

	// Default (hot only) — warm and cold should not be called
	if err := s.Set(ctx, "k1", map[string]any{"v": 1}); err != nil {
		t.Fatal(err)
	}
	if warm.setCalls != 0 {
		t.Fatalf("expected 0 warm Set for hot tier, got %d", warm.setCalls)
	}

	// Warm tier — should write to warm too
	if err := s.Set(ctx, "k2", map[string]any{"v": 2}, WithTier(TierWarm)); err != nil {
		t.Fatal(err)
	}
	if warm.setCalls != 1 {
		t.Fatalf("expected 1 warm Set for warm tier, got %d", warm.setCalls)
	}

	// Cold tier — should write to warm + cold
	if err := s.Set(ctx, "k3", map[string]any{"v": 3}, WithTier(TierCold)); err != nil {
		t.Fatal(err)
	}
	if warm.setCalls != 2 {
		t.Fatalf("expected 2 warm Set for cold tier, got %d", warm.setCalls)
	}
	if cold.setCalls != 1 {
		t.Fatalf("expected 1 cold Set for cold tier, got %d", cold.setCalls)
	}
}

func TestStore_DeleteFromAllTiers(t *testing.T) {
	warm := newMockWarm()
	cold := newMockCold()
	s := NewStore(warm, cold, testMemLogger())
	ctx := context.Background()

	// Set in all tiers
	if err := s.Set(ctx, "k1", map[string]any{"v": 1}, WithTier(TierCold)); err != nil {
		t.Fatal(err)
	}

	err := s.Delete(ctx, "k1")
	if err != nil {
		t.Fatal(err)
	}

	if warm.delCalls != 1 {
		t.Fatalf("expected 1 warm Delete call, got %d", warm.delCalls)
	}
	if cold.delCalls != 1 {
		t.Fatalf("expected 1 cold Delete call, got %d", cold.delCalls)
	}

	// Hot should also be deleted
	got := s.hot.get("k1")
	if got != nil {
		t.Fatal("expected hot entry to be deleted")
	}
}

func TestStore_DegradedModeWarmNil(t *testing.T) {
	s := NewStore(nil, nil, testMemLogger())
	ctx := context.Background()

	// Should work with hot only
	if err := s.Set(ctx, "k1", map[string]any{"v": 1}); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(ctx, "k1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected hot-only get to work")
	}

	// Delete should not panic
	err = s.Delete(ctx, "k1")
	if err != nil {
		t.Fatal(err)
	}
}
