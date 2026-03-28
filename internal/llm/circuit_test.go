package llm

import (
	"log/slog"
	"os"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestCircuitBreaker_StartsClosedAllowsRequests(t *testing.T) {
	cb := NewCircuitBreaker(3, 10*time.Second, testLogger())
	if cb.State() != CircuitClosed {
		t.Fatalf("expected CircuitClosed, got %v", cb.State())
	}
	if !cb.Allow() {
		t.Fatal("expected Allow() = true for closed circuit")
	}
}

func TestCircuitBreaker_OpensAfterMaxFailures(t *testing.T) {
	cb := NewCircuitBreaker(3, 10*time.Second, testLogger())

	for i := 0; i < 3; i++ {
		cb.RecordFailure()
	}

	if cb.State() != CircuitOpen {
		t.Fatalf("expected CircuitOpen after 3 failures, got %v", cb.State())
	}
	if cb.Allow() {
		t.Fatal("expected Allow() = false for open circuit")
	}
}

func TestCircuitBreaker_TransitionsToHalfOpenAfterRecoveryTimeout(t *testing.T) {
	cb := NewCircuitBreaker(1, 1*time.Millisecond, testLogger())
	cb.RecordFailure()

	if cb.State() != CircuitOpen {
		t.Fatalf("expected CircuitOpen, got %v", cb.State())
	}

	time.Sleep(5 * time.Millisecond)

	if !cb.Allow() {
		t.Fatal("expected Allow() = true after recovery timeout")
	}
	if cb.State() != CircuitHalfOpen {
		t.Fatalf("expected CircuitHalfOpen, got %v", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenLimitsRequests(t *testing.T) {
	cb := NewCircuitBreaker(1, 1*time.Millisecond, testLogger())
	cb.RecordFailure()
	time.Sleep(5 * time.Millisecond)

	// First Allow transitions to half-open and allows
	if !cb.Allow() {
		t.Fatal("expected first Allow() = true in half-open")
	}
	// Second Allow should still be allowed (halfOpenMax = 2, successes = 0)
	if !cb.Allow() {
		t.Fatal("expected second Allow() = true in half-open (successes < halfOpenMax)")
	}
}

func TestCircuitBreaker_HalfOpenClosesAfterSuccesses(t *testing.T) {
	cb := NewCircuitBreaker(1, 1*time.Millisecond, testLogger())
	cb.RecordFailure()
	time.Sleep(5 * time.Millisecond)

	cb.Allow() // transitions to half-open
	cb.RecordSuccess()
	cb.RecordSuccess()

	if cb.State() != CircuitClosed {
		t.Fatalf("expected CircuitClosed after enough successes, got %v", cb.State())
	}
}

func TestCircuitBreaker_HalfOpenReopensOnFailure(t *testing.T) {
	cb := NewCircuitBreaker(1, 1*time.Millisecond, testLogger())
	cb.RecordFailure()
	time.Sleep(5 * time.Millisecond)

	cb.Allow() // transitions to half-open
	cb.RecordFailure()

	if cb.State() != CircuitOpen {
		t.Fatalf("expected CircuitOpen after half-open failure, got %v", cb.State())
	}
}

func TestCircuitBreaker_StateString(t *testing.T) {
	tests := []struct {
		name     string
		state    CircuitState
		wantStr  string
	}{
		{"closed", CircuitClosed, "closed"},
		{"open", CircuitOpen, "open"},
		{"half-open", CircuitHalfOpen, "half-open"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cb := NewCircuitBreaker(5, 10*time.Second, testLogger())
			cb.mu.Lock()
			cb.state = tt.state
			cb.mu.Unlock()

			got := cb.StateString()
			if got != tt.wantStr {
				t.Errorf("StateString() = %q, want %q", got, tt.wantStr)
			}
		})
	}
}

func TestCircuitBreaker_SuccessResetsFailureCount(t *testing.T) {
	cb := NewCircuitBreaker(3, 10*time.Second, testLogger())

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess() // should reset failures to 0

	// Two more failures should NOT open the circuit (only 2, not 3)
	cb.RecordFailure()
	cb.RecordFailure()

	if cb.State() != CircuitClosed {
		t.Fatalf("expected CircuitClosed (failures reset), got %v", cb.State())
	}

	// One more failure should open it (3 consecutive)
	cb.RecordFailure()
	if cb.State() != CircuitOpen {
		t.Fatalf("expected CircuitOpen after 3 failures, got %v", cb.State())
	}
}
