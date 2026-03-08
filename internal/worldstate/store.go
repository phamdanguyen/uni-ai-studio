// Package worldstate manages the shared world state with event sourcing and optimistic locking.
package worldstate

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store manages the shared world state in PostgreSQL.
type Store struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// NewStore creates a new world state store.
func NewStore(pool *pgxpool.Pool, logger *slog.Logger) *Store {
	return &Store{
		pool:   pool,
		logger: logger.With("component", "worldstate"),
	}
}

// WorldEvent represents an event that changes the world state.
type WorldEvent struct {
	ProjectID       string         `json:"projectId"`
	EventType       string         `json:"eventType"`
	AgentName       string         `json:"agentName"`
	Payload         map[string]any `json:"payload"`
	ExpectedVersion int            `json:"expectedVersion"`
}

// AgentDecision records an agent's decision for audit trail.
type AgentDecision struct {
	ProjectID    string  `json:"projectId"`
	TaskID       string  `json:"taskId,omitempty"`
	AgentName    string  `json:"agentName"`
	SkillID      string  `json:"skillId"`
	ModelUsed    string  `json:"modelUsed"`
	ModelTier    string  `json:"modelTier"`
	TokensIn     int     `json:"tokensIn"`
	TokensOut    int     `json:"tokensOut"`
	CostUSD      float64 `json:"costUsd"`
	DurationMs   int     `json:"durationMs"`
	Reasoning    string  `json:"reasoning,omitempty"`
	OutputRef    string  `json:"outputRef,omitempty"`
	QualityScore float64 `json:"qualityScore,omitempty"`
}

// ErrOptimisticLockConflict is returned when a concurrent write is detected.
var ErrOptimisticLockConflict = fmt.Errorf("optimistic lock conflict: state was modified by another agent")

// InitProject creates a new world state for a project.
func (s *Store) InitProject(ctx context.Context, projectID string, initialState map[string]any) error {
	stateJSON, err := json.Marshal(initialState)
	if err != nil {
		return fmt.Errorf("marshal initial state: %w", err)
	}

	_, err = s.pool.Exec(ctx,
		`INSERT INTO world_states (project_id, version, state_data, updated_by) 
		 VALUES ($1, 1, $2, 'system')
		 ON CONFLICT (project_id) DO NOTHING`,
		projectID, stateJSON,
	)
	if err != nil {
		return fmt.Errorf("init project state: %w", err)
	}

	return nil
}

// GetState retrieves the current world state and version for a project.
func (s *Store) GetState(ctx context.Context, projectID string) (map[string]any, int, error) {
	var stateJSON []byte
	var version int

	err := s.pool.QueryRow(ctx,
		`SELECT state_data, version FROM world_states WHERE project_id = $1`,
		projectID,
	).Scan(&stateJSON, &version)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, 0, fmt.Errorf("project %s not found", projectID)
		}
		return nil, 0, fmt.Errorf("get state: %w", err)
	}

	var state map[string]any
	if err := json.Unmarshal(stateJSON, &state); err != nil {
		return nil, 0, fmt.Errorf("unmarshal state: %w", err)
	}

	return state, version, nil
}

// ApplyEvent atomically applies an event to the world state with optimistic locking.
func (s *Store) ApplyEvent(ctx context.Context, event WorldEvent) (int, error) {
	payloadJSON, err := json.Marshal(event.Payload)
	if err != nil {
		return 0, fmt.Errorf("marshal event payload: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// 1. Lock and read current version
	var currentVersion int
	err = tx.QueryRow(ctx,
		`SELECT version FROM world_states WHERE project_id = $1 FOR UPDATE`,
		event.ProjectID,
	).Scan(&currentVersion)
	if err != nil {
		return 0, fmt.Errorf("read version: %w", err)
	}

	// 2. Check optimistic lock
	if event.ExpectedVersion != 0 && event.ExpectedVersion != currentVersion {
		return 0, ErrOptimisticLockConflict
	}

	newVersion := currentVersion + 1

	// 3. Insert event
	_, err = tx.Exec(ctx,
		`INSERT INTO world_events (project_id, event_type, agent_name, payload, version, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		event.ProjectID, event.EventType, event.AgentName, payloadJSON, newVersion, time.Now(),
	)
	if err != nil {
		return 0, fmt.Errorf("insert event: %w", err)
	}

	// 4. Merge payload into state (JSONB || operator)
	_, err = tx.Exec(ctx,
		`UPDATE world_states 
		 SET version = $1, state_data = state_data || $2, updated_at = NOW(), updated_by = $3
		 WHERE project_id = $4`,
		newVersion, payloadJSON, event.AgentName, event.ProjectID,
	)
	if err != nil {
		return 0, fmt.Errorf("update state: %w", err)
	}

	// 5. Notify listeners
	notifyPayload := fmt.Sprintf("%s:%d:%s", event.ProjectID, newVersion, event.EventType)
	_, err = tx.Exec(ctx, "SELECT pg_notify('world_state_change', $1)", notifyPayload)
	if err != nil {
		s.logger.Warn("pg_notify failed", "error", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit: %w", err)
	}

	s.logger.Info("event applied",
		"project", event.ProjectID,
		"event", event.EventType,
		"agent", event.AgentName,
		"version", newVersion,
	)

	return newVersion, nil
}

// RecordDecision saves an agent's decision to the audit trail.
func (s *Store) RecordDecision(ctx context.Context, d AgentDecision) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO agent_decisions 
		 (project_id, task_id, agent_name, skill_id, model_used, model_tier,
		  tokens_in, tokens_out, cost_usd, duration_ms, reasoning, output_ref, quality_score)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
		d.ProjectID, nilIfEmpty(d.TaskID), d.AgentName, d.SkillID, d.ModelUsed, d.ModelTier,
		d.TokensIn, d.TokensOut, d.CostUSD, d.DurationMs, nilIfEmpty(d.Reasoning),
		nilIfEmpty(d.OutputRef), d.QualityScore,
	)
	if err != nil {
		return fmt.Errorf("record decision: %w", err)
	}
	return nil
}

// GetEvents retrieves events for a project, optionally after a specific version.
func (s *Store) GetEvents(ctx context.Context, projectID string, afterVersion int) ([]map[string]any, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, event_type, agent_name, payload, version, created_at
		 FROM world_events 
		 WHERE project_id = $1 AND version > $2
		 ORDER BY version ASC`,
		projectID, afterVersion,
	)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var events []map[string]any
	for rows.Next() {
		var id int64
		var eventType, agentName string
		var payload []byte
		var version int
		var createdAt time.Time

		if err := rows.Scan(&id, &eventType, &agentName, &payload, &version, &createdAt); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}

		var p map[string]any
		json.Unmarshal(payload, &p)

		events = append(events, map[string]any{
			"id":        id,
			"eventType": eventType,
			"agentName": agentName,
			"payload":   p,
			"version":   version,
			"createdAt": createdAt,
		})
	}

	return events, nil
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
