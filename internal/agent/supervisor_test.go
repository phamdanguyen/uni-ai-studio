package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"
)

func testSupervisorLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// stubAgent is a minimal Agent implementation for testing.
type stubAgent struct {
	name string
}

func (s *stubAgent) Card() AgentCard                                                  { return AgentCard{Name: s.name} }
func (s *stubAgent) HandleMessage(_ context.Context, _ Message) (*TaskResult, error)  { return nil, nil }
func (s *stubAgent) HandleStream(_ context.Context, _ Message, _ chan<- StreamEvent) error { return nil }
func (s *stubAgent) Name() string                                                     { return s.name }

func TestSupervisor_RecordSuccess(t *testing.T) {
	sup := NewSupervisor(testSupervisorLogger())
	sup.Watch(&stubAgent{name: "test-agent"})

	sup.RecordSuccess("test-agent", 100*time.Millisecond)

	report := sup.HealthReport()
	if len(report) != 1 {
		t.Fatalf("expected 1 agent in report, got %d", len(report))
	}
	if report[0].TasksHandled != 1 {
		t.Fatalf("expected TasksHandled = 1, got %d", report[0].TasksHandled)
	}
	if report[0].AvgLatency != 100 {
		t.Fatalf("expected AvgLatency = 100ms, got %.1f", report[0].AvgLatency)
	}
}

func TestSupervisor_RecordFailureDegraded(t *testing.T) {
	sup := NewSupervisor(testSupervisorLogger())
	sup.Watch(&stubAgent{name: "a1"})

	// 3 successes + 1 failure = 25% error rate → degraded (>20%)
	for i := 0; i < 3; i++ {
		sup.RecordSuccess("a1", 10*time.Millisecond)
	}
	sup.RecordFailure("a1", fmt.Errorf("oops"))

	report := sup.HealthReport()
	if report[0].Status != "degraded" {
		t.Fatalf("expected 'degraded', got %q (errorRate=%.2f)", report[0].Status, report[0].ErrorRate)
	}
}

func TestSupervisor_RecordFailureFailed(t *testing.T) {
	sup := NewSupervisor(testSupervisorLogger())
	sup.Watch(&stubAgent{name: "a1"})

	// All failures → >50% error rate → failed
	sup.RecordFailure("a1", fmt.Errorf("err"))
	sup.RecordFailure("a1", fmt.Errorf("err"))

	report := sup.HealthReport()
	if report[0].Status != "failed" {
		t.Fatalf("expected 'failed', got %q (errorRate=%.2f)", report[0].Status, report[0].ErrorRate)
	}
}

func TestSupervisor_RecoveryDegradedToHealthy(t *testing.T) {
	sup := NewSupervisor(testSupervisorLogger())
	sup.Watch(&stubAgent{name: "a1"})

	// Create degraded state: 3 successes, 1 failure = 25% error rate
	for i := 0; i < 3; i++ {
		sup.RecordSuccess("a1", 10*time.Millisecond)
	}
	sup.RecordFailure("a1", fmt.Errorf("err"))

	// Verify degraded
	report := sup.HealthReport()
	if report[0].Status != "degraded" {
		t.Fatalf("expected 'degraded', got %q", report[0].Status)
	}

	// Add many successes to bring error rate below 10%
	for i := 0; i < 20; i++ {
		sup.RecordSuccess("a1", 10*time.Millisecond)
	}

	report = sup.HealthReport()
	if report[0].Status != "healthy" {
		t.Fatalf("expected recovery to 'healthy', got %q (errorRate=%.2f)", report[0].Status, report[0].ErrorRate)
	}
}

func TestSupervisor_IsHealthy(t *testing.T) {
	sup := NewSupervisor(testSupervisorLogger())
	sup.Watch(&stubAgent{name: "a1"})

	// Healthy agent
	if !sup.IsHealthy("a1") {
		t.Fatal("expected healthy agent to return true")
	}

	// Unknown agent
	if sup.IsHealthy("nonexistent") {
		t.Fatal("expected unknown agent to return false")
	}

	// Force failed state
	sup.RecordFailure("a1", fmt.Errorf("err"))
	sup.RecordFailure("a1", fmt.Errorf("err"))

	if sup.IsHealthy("a1") {
		t.Fatal("expected failed agent to return false")
	}
}

func TestSupervisor_HealthReport(t *testing.T) {
	sup := NewSupervisor(testSupervisorLogger())
	sup.Watch(&stubAgent{name: "a1"})
	sup.Watch(&stubAgent{name: "a2"})

	report := sup.HealthReport()
	if len(report) != 2 {
		t.Fatalf("expected 2 agents in report, got %d", len(report))
	}
}

func TestSupervisor_UnknownAgentIgnored(t *testing.T) {
	sup := NewSupervisor(testSupervisorLogger())

	// Should not panic
	sup.RecordSuccess("ghost", 10*time.Millisecond)
	sup.RecordFailure("ghost", fmt.Errorf("err"))
}

func TestConflictResolver_LastWriteWins(t *testing.T) {
	cr := NewConflictResolver(PolicyLastWriteWins, testSupervisorLogger())
	bb := NewBlackboard("p1", "r1")

	result, winner := cr.Resolve(bb, "s1", "k1", "agentA", "agentB", "valA", "valB")
	if result != "valB" {
		t.Fatalf("expected last write wins (valB), got %v", result)
	}
	if winner != "agentB" {
		t.Fatalf("expected winner = agentB, got %v", winner)
	}
}

func TestConflictResolver_OwnerPriority(t *testing.T) {
	cr := NewConflictResolver(PolicyOwnerPriority, testSupervisorLogger())
	bb := NewBlackboard("p1", "r1")
	bb.Set("agentA", "s1", "k1", "initial")
	bb.SetOwner("s1", "agentA")

	result, winner := cr.Resolve(bb, "s1", "k1", "agentA", "agentB", "valA", "valB")
	if result != "valA" {
		t.Fatalf("expected owner value (valA), got %v", result)
	}
	if winner != "agentA" {
		t.Fatalf("expected owner winner = agentA, got %v", winner)
	}
}

func TestConflictResolver_MergeMaps(t *testing.T) {
	cr := NewConflictResolver(PolicyMerge, testSupervisorLogger())
	bb := NewBlackboard("p1", "r1")

	valA := map[string]any{"keyA": "a"}
	valB := map[string]any{"keyB": "b"}

	result, winner := cr.Resolve(bb, "s1", "k1", "agentA", "agentB", valA, valB)

	merged, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected merged map, got %T", result)
	}
	if merged["keyA"] != "a" || merged["keyB"] != "b" {
		t.Fatalf("expected merged map with both keys, got %v", merged)
	}
	if winner != "merged" {
		t.Fatalf("expected winner = 'merged', got %v", winner)
	}
}

func TestConflictResolver_MergeNonMapsFallsThrough(t *testing.T) {
	cr := NewConflictResolver(PolicyMerge, testSupervisorLogger())
	bb := NewBlackboard("p1", "r1")

	// Non-map values should fall through to last-write-wins
	result, winner := cr.Resolve(bb, "s1", "k1", "agentA", "agentB", "stringA", "stringB")
	if result != "stringB" {
		t.Fatalf("expected fallthrough to last-write-wins (stringB), got %v", result)
	}
	if winner != "agentB" {
		t.Fatalf("expected winner = agentB, got %v", winner)
	}
}

func TestAvgDuration_Empty(t *testing.T) {
	got := avgDuration(nil)
	if got != 0 {
		t.Fatalf("expected 0 for empty, got %f", got)
	}
}

func TestAvgDuration_Single(t *testing.T) {
	got := avgDuration([]time.Duration{100 * time.Millisecond})
	if got != 100 {
		t.Fatalf("expected 100, got %f", got)
	}
}

func TestAvgDuration_Multiple(t *testing.T) {
	got := avgDuration([]time.Duration{100 * time.Millisecond, 200 * time.Millisecond})
	if got != 150 {
		t.Fatalf("expected 150, got %f", got)
	}
}
