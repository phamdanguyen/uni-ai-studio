package llm

import (
	"log/slog"
	"sync"
	"time"
)

// CircuitState represents the circuit breaker state.
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // Normal operation
	CircuitOpen                         // Failing, reject requests
	CircuitHalfOpen                     // Testing recovery
)

// CircuitBreaker protects LLM calls from cascading failures.
type CircuitBreaker struct {
	mu               sync.RWMutex
	state            CircuitState
	failures         int
	successes        int
	maxFailures      int
	recoveryTimeout  time.Duration
	halfOpenMax      int
	lastFailureTime  time.Time
	logger           *slog.Logger
}

// NewCircuitBreaker creates a circuit breaker with configurable thresholds.
func NewCircuitBreaker(maxFailures int, recoveryTimeout time.Duration, logger *slog.Logger) *CircuitBreaker {
	return &CircuitBreaker{
		state:           CircuitClosed,
		maxFailures:     maxFailures,
		recoveryTimeout: recoveryTimeout,
		halfOpenMax:     2,
		logger:          logger.With("component", "circuit-breaker"),
	}
}

// Allow checks if a request should be allowed through.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitClosed:
		return true

	case CircuitOpen:
		// Check if recovery timeout has elapsed
		if time.Since(cb.lastFailureTime) > cb.recoveryTimeout {
			cb.state = CircuitHalfOpen
			cb.successes = 0
			cb.logger.Info("circuit breaker → half-open", "after", cb.recoveryTimeout)
			return true
		}
		return false

	case CircuitHalfOpen:
		// Allow limited requests to test recovery
		return cb.successes < cb.halfOpenMax
	}

	return false
}

// RecordSuccess records a successful call.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitHalfOpen:
		cb.successes++
		if cb.successes >= cb.halfOpenMax {
			cb.state = CircuitClosed
			cb.failures = 0
			cb.logger.Info("circuit breaker → closed (recovered)")
		}
	case CircuitClosed:
		cb.failures = 0
	}
}

// RecordFailure records a failed call.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailureTime = time.Now()

	switch cb.state {
	case CircuitClosed:
		cb.failures++
		if cb.failures >= cb.maxFailures {
			cb.state = CircuitOpen
			cb.logger.Warn("circuit breaker → OPEN",
				"failures", cb.failures,
				"threshold", cb.maxFailures,
			)
		}
	case CircuitHalfOpen:
		// One failure in half-open → back to open
		cb.state = CircuitOpen
		cb.logger.Warn("circuit breaker → OPEN (half-open test failed)")
	}
}

// State returns the current state.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state
}

// StateString returns the current state as a string.
func (cb *CircuitBreaker) StateString() string {
	switch cb.State() {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}
