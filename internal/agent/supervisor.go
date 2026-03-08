// Package agent — Supervisor pattern for agent health monitoring.
// The Supervisor watches agents, detects failures, and coordinates recovery.
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// AgentHealth tracks the health status of an agent.
type AgentHealth struct {
	Name          string    `json:"name"`
	Status        string    `json:"status"` // "healthy", "degraded", "failed"
	LastHeartbeat time.Time `json:"lastHeartbeat"`
	TasksHandled  int64     `json:"tasksHandled"`
	TasksFailed   int64     `json:"tasksFailed"`
	AvgLatency    float64   `json:"avgLatencyMs"`
	ErrorRate     float64   `json:"errorRate"`
}

// Supervisor monitors agent health and handles failures.
type Supervisor struct {
	mu       sync.RWMutex
	agents   map[string]*supervisedAgent
	logger   *slog.Logger
	interval time.Duration
	stop     chan struct{}
}

type supervisedAgent struct {
	agent     Agent
	health    AgentHealth
	latencies []time.Duration
}

// NewSupervisor creates an agent supervisor.
func NewSupervisor(logger *slog.Logger) *Supervisor {
	return &Supervisor{
		agents:   make(map[string]*supervisedAgent),
		logger:   logger.With("component", "supervisor"),
		interval: 10 * time.Second,
		stop:     make(chan struct{}),
	}
}

// Watch adds an agent to supervision.
func (s *Supervisor) Watch(a Agent) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.agents[a.Name()] = &supervisedAgent{
		agent: a,
		health: AgentHealth{
			Name:          a.Name(),
			Status:        "healthy",
			LastHeartbeat: time.Now(),
		},
	}
}

// RecordSuccess records a successful task execution.
func (s *Supervisor) RecordSuccess(agentName string, latency time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sa, ok := s.agents[agentName]
	if !ok {
		return
	}

	sa.health.TasksHandled++
	sa.health.LastHeartbeat = time.Now()
	sa.latencies = append(sa.latencies, latency)

	// Keep last 100 latencies
	if len(sa.latencies) > 100 {
		sa.latencies = sa.latencies[len(sa.latencies)-100:]
	}
	sa.health.AvgLatency = avgDuration(sa.latencies)
	sa.health.ErrorRate = float64(sa.health.TasksFailed) / float64(sa.health.TasksHandled+sa.health.TasksFailed)

	if sa.health.Status == "degraded" && sa.health.ErrorRate < 0.1 {
		sa.health.Status = "healthy"
		s.logger.Info("agent recovered", "agent", agentName)
	}
}

// RecordFailure records a failed task execution.
func (s *Supervisor) RecordFailure(agentName string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sa, ok := s.agents[agentName]
	if !ok {
		return
	}

	sa.health.TasksFailed++
	sa.health.LastHeartbeat = time.Now()

	total := sa.health.TasksHandled + sa.health.TasksFailed
	if total > 0 {
		sa.health.ErrorRate = float64(sa.health.TasksFailed) / float64(total)
	}

	if sa.health.ErrorRate > 0.5 {
		sa.health.Status = "failed"
		s.logger.Error("agent marked as failed", "agent", agentName, "errorRate", sa.health.ErrorRate)
	} else if sa.health.ErrorRate > 0.2 {
		sa.health.Status = "degraded"
		s.logger.Warn("agent degraded", "agent", agentName, "errorRate", sa.health.ErrorRate, "error", err)
	}
}

// HealthReport returns health status of all supervised agents.
func (s *Supervisor) HealthReport() []AgentHealth {
	s.mu.RLock()
	defer s.mu.RUnlock()

	report := make([]AgentHealth, 0, len(s.agents))
	for _, sa := range s.agents {
		report = append(report, sa.health)
	}
	return report
}

// IsHealthy returns whether a specific agent is healthy.
func (s *Supervisor) IsHealthy(agentName string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sa, ok := s.agents[agentName]
	if !ok {
		return false
	}
	return sa.health.Status != "failed"
}

// Start begins the health check loop.
func (s *Supervisor) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stop:
				return
			case <-ticker.C:
				s.checkHealth()
			}
		}
	}()
	s.logger.Info("supervisor started", "interval", s.interval)
}

// Stop halts supervision.
func (s *Supervisor) Stop() {
	close(s.stop)
}

func (s *Supervisor) checkHealth() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for name, sa := range s.agents {
		// Check heartbeat staleness (>60s without activity)
		if now.Sub(sa.health.LastHeartbeat) > 60*time.Second && sa.health.Status == "healthy" {
			sa.health.Status = "degraded"
			s.logger.Warn("agent heartbeat stale", "agent", name,
				"lastHeartbeat", sa.health.LastHeartbeat)
		}
	}
}

// --- Parallel Executor ---

// ParallelResult holds results from parallel agent execution.
type ParallelResult struct {
	Results map[string]*TaskResult `json:"results"`
	Errors  map[string]error       `json:"errors,omitempty"`
}

// ExecuteParallel runs multiple agent tasks concurrently and waits for all.
func ExecuteParallel(ctx context.Context, bus MessageBus, tasks []Message, timeout time.Duration) (*ParallelResult, error) {
	result := &ParallelResult{
		Results: make(map[string]*TaskResult),
		Errors:  make(map[string]error),
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, task := range tasks {
		wg.Add(1)
		go func(msg Message) {
			defer wg.Done()

			resp, err := bus.Request(ctx, msg, timeout)

			mu.Lock()
			defer mu.Unlock()

			key := fmt.Sprintf("%s:%s", msg.To, msg.SkillID)
			if err != nil {
				result.Errors[key] = err
			} else {
				result.Results[key] = resp
			}
		}(task)
	}

	wg.Wait()
	return result, nil
}

// --- Conflict Resolver ---

// ConflictPolicy determines how conflicts are resolved.
type ConflictPolicy string

const (
	PolicyLastWriteWins ConflictPolicy = "last_write_wins"
	PolicyOwnerPriority ConflictPolicy = "owner_priority"
	PolicyMerge         ConflictPolicy = "merge"
)

// ConflictResolver handles concurrent modifications to shared state.
type ConflictResolver struct {
	policy ConflictPolicy
	logger *slog.Logger
}

// NewConflictResolver creates a conflict resolver with the given policy.
func NewConflictResolver(policy ConflictPolicy, logger *slog.Logger) *ConflictResolver {
	return &ConflictResolver{
		policy: policy,
		logger: logger.With("component", "conflict-resolver"),
	}
}

// Resolve handles a write conflict between two agents on the same section.
func (cr *ConflictResolver) Resolve(board *Blackboard, section, key string,
	agentA, agentB string, valueA, valueB any) (any, string) {

	switch cr.policy {
	case PolicyOwnerPriority:
		cr.logger.Info("conflict resolved by ownership",
			"section", section, "key", key,
			"agentA", agentA, "agentB", agentB)

		owner := ""
		board.mu.RLock()
		if sec, ok := board.sections[section]; ok {
			owner = sec.Owner
		}
		board.mu.RUnlock()

		if owner == agentA {
			return valueA, agentA
		}
		return valueB, agentB

	case PolicyMerge:
		// Try to merge maps
		mapA, okA := valueA.(map[string]any)
		mapB, okB := valueB.(map[string]any)
		if okA && okB {
			merged := make(map[string]any)
			for k, v := range mapA {
				merged[k] = v
			}
			for k, v := range mapB {
				merged[k] = v
			}
			cr.logger.Info("conflict resolved by merge",
				"section", section, "key", key)
			return merged, "merged"
		}
		// Fall through to last-write-wins
		fallthrough

	default: // PolicyLastWriteWins
		cr.logger.Info("conflict resolved by last-write-wins",
			"section", section, "key", key, "winner", agentB)
		return valueB, agentB
	}
}

func avgDuration(durations []time.Duration) float64 {
	if len(durations) == 0 {
		return 0
	}
	var total time.Duration
	for _, d := range durations {
		total += d
	}
	return float64(total.Milliseconds()) / float64(len(durations))
}
