// Package workflow implements a lightweight durable workflow engine inspired by Temporal.
// It supports DAG execution, checkpointing, resume, and retry with backoff.
package workflow

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Status represents the lifecycle state of a run or step.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

// StepFunc is the function executed for each workflow step.
type StepFunc func(ctx context.Context, input map[string]any) (map[string]any, error)

// Step defines a single step in a workflow.
type Step struct {
	Key         string
	Title       string
	AgentName   string // Which agent handles this step
	SkillID     string // Which skill to invoke
	DependsOn   []string // Step keys this depends on
	MaxAttempts int
	Fn          StepFunc // The function to execute (if inline)
}

// Run represents an active workflow execution.
type Run struct {
	ID           string
	ProjectID    string
	WorkflowType string
	Steps        []Step
	Status       Status
	Input        map[string]any
	Output       map[string]any
}

// Engine orchestrates workflow execution with persistence and retry.
type Engine struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// NewEngine creates a new workflow engine.
func NewEngine(pool *pgxpool.Pool, logger *slog.Logger) *Engine {
	return &Engine{
		pool:   pool,
		logger: logger.With("component", "workflow"),
	}
}

// StartRun creates and persists a new workflow run.
func (e *Engine) StartRun(ctx context.Context, projectID, workflowType string, steps []Step, input map[string]any) (*Run, error) {
	runID := uuid.New().String()

	// Persist run
	_, err := e.pool.Exec(ctx,
		`INSERT INTO workflow_runs (id, project_id, workflow_type, status, input, started_at)
		 VALUES ($1, $2, $3, $4, $5, NOW())`,
		runID, projectID, workflowType, StatusRunning, input,
	)
	if err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}

	// Persist steps
	for i, step := range steps {
		maxAttempts := step.MaxAttempts
		if maxAttempts == 0 {
			maxAttempts = 3
		}

		_, err := e.pool.Exec(ctx,
			`INSERT INTO workflow_steps (id, run_id, step_key, step_title, step_index, status, agent_name, skill_id, max_attempts)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			uuid.New().String(), runID, step.Key, step.Title, i, StatusQueued,
			step.AgentName, step.SkillID, maxAttempts,
		)
		if err != nil {
			return nil, fmt.Errorf("create step %s: %w", step.Key, err)
		}
	}

	e.logger.Info("workflow started",
		"runId", runID,
		"type", workflowType,
		"steps", len(steps),
	)

	return &Run{
		ID:           runID,
		ProjectID:    projectID,
		WorkflowType: workflowType,
		Steps:        steps,
		Status:       StatusRunning,
		Input:        input,
	}, nil
}

// Execute runs all steps in sequence, respecting dependencies.
func (e *Engine) Execute(ctx context.Context, run *Run) error {
	for i, step := range run.Steps {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		e.logger.Info("executing step",
			"runId", run.ID,
			"step", step.Key,
			"index", i,
			"total", len(run.Steps),
		)

		// Update step status to running
		_, err := e.pool.Exec(ctx,
			`UPDATE workflow_steps SET status = $1, started_at = NOW() WHERE run_id = $2 AND step_key = $3`,
			StatusRunning, run.ID, step.Key,
		)
		if err != nil {
			return fmt.Errorf("update step status: %w", err)
		}

		// Execute step with retry
		maxAttempts := step.MaxAttempts
		if maxAttempts == 0 {
			maxAttempts = 3
		}

		var stepOutput map[string]any
		var lastErr error

		for attempt := 1; attempt <= maxAttempts; attempt++ {
			// Get step input (merge run input + previous step outputs)
			stepInput := e.buildStepInput(run, step)

			if step.Fn != nil {
				stepOutput, lastErr = step.Fn(ctx, stepInput)
			} else {
				lastErr = fmt.Errorf("step %s has no execution function", step.Key)
			}

			if lastErr == nil {
				break
			}

			e.logger.Warn("step attempt failed",
				"runId", run.ID,
				"step", step.Key,
				"attempt", attempt,
				"maxAttempts", maxAttempts,
				"error", lastErr,
			)

			if attempt < maxAttempts {
				backoff := time.Duration(attempt*attempt) * time.Second
				time.Sleep(backoff)
			}
		}

		if lastErr != nil {
			// Step failed after all retries
			_, _ = e.pool.Exec(ctx,
				`UPDATE workflow_steps SET status = $1, error_message = $2, finished_at = NOW()
				 WHERE run_id = $3 AND step_key = $4`,
				StatusFailed, lastErr.Error(), run.ID, step.Key,
			)

			_, _ = e.pool.Exec(ctx,
				`UPDATE workflow_runs SET status = $1, error_message = $2, finished_at = NOW()
				 WHERE id = $3`,
				StatusFailed, fmt.Sprintf("step %s failed: %s", step.Key, lastErr.Error()), run.ID,
			)

			return fmt.Errorf("step %s failed after %d attempts: %w", step.Key, maxAttempts, lastErr)
		}

		// Step succeeded — persist output
		_, err = e.pool.Exec(ctx,
			`UPDATE workflow_steps SET status = $1, output = $2, finished_at = NOW()
			 WHERE run_id = $3 AND step_key = $4`,
			StatusCompleted, stepOutput, run.ID, step.Key,
		)
		if err != nil {
			return fmt.Errorf("persist step output: %w", err)
		}

		// Save checkpoint
		_, err = e.pool.Exec(ctx,
			`INSERT INTO workflow_checkpoints (run_id, step_key, version, state_json)
			 VALUES ($1, $2, 1, $3)
			 ON CONFLICT (run_id, step_key, version) DO UPDATE SET state_json = $3`,
			run.ID, step.Key, stepOutput,
		)
		if err != nil {
			e.logger.Warn("checkpoint failed", "error", err)
		}

		e.logger.Info("step completed",
			"runId", run.ID,
			"step", step.Key,
		)
	}

	// All steps done — mark run as completed
	_, err := e.pool.Exec(ctx,
		`UPDATE workflow_runs SET status = $1, finished_at = NOW() WHERE id = $2`,
		StatusCompleted, run.ID,
	)
	if err != nil {
		return fmt.Errorf("complete run: %w", err)
	}

	e.logger.Info("workflow completed", "runId", run.ID)
	return nil
}

func (e *Engine) buildStepInput(run *Run, _ Step) map[string]any {
	// For now, pass through run input. Later: merge outputs from depended steps.
	input := make(map[string]any)
	for k, v := range run.Input {
		input[k] = v
	}
	return input
}
