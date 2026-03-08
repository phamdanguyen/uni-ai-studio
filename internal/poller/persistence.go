// Package poller — PostgreSQL persistence for async task tracking.
package poller

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PersistentStore saves and loads tracked tasks from PostgreSQL.
type PersistentStore struct {
	pool *pgxpool.Pool
}

// NewPersistentStore creates a task persistence layer.
func NewPersistentStore(pool *pgxpool.Pool) *PersistentStore {
	return &PersistentStore{pool: pool}
}

// Save persists a tracked task to the database.
func (ps *PersistentStore) Save(ctx context.Context, task TrackedTask) error {
	_, err := ps.pool.Exec(ctx,
		`INSERT INTO async_tasks (external_id, provider, media_type, project_id, panel_index, status, result_url, error, attempts, created_at, completed_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 ON CONFLICT (external_id) DO UPDATE SET
		   status = EXCLUDED.status,
		   result_url = EXCLUDED.result_url,
		   error = EXCLUDED.error,
		   attempts = EXCLUDED.attempts,
		   completed_at = EXCLUDED.completed_at`,
		task.ExternalID, task.Provider, task.MediaType,
		task.ProjectID, task.PanelIndex,
		string(task.Status), task.ResultURL, task.Error,
		task.Attempts, task.CreatedAt, task.CompletedAt,
	)
	return err
}

// LoadPending returns all pending/processing tasks for recovery on restart.
func (ps *PersistentStore) LoadPending(ctx context.Context) ([]TrackedTask, error) {
	rows, err := ps.pool.Query(ctx,
		`SELECT external_id, provider, media_type, project_id, panel_index,
		        status, result_url, error, attempts, created_at, completed_at
		 FROM async_tasks
		 WHERE status IN ('pending', 'processing')
		 ORDER BY created_at ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("load pending: %w", err)
	}
	defer rows.Close()

	var tasks []TrackedTask
	for rows.Next() {
		var t TrackedTask
		var status string
		err := rows.Scan(
			&t.ExternalID, &t.Provider, &t.MediaType,
			&t.ProjectID, &t.PanelIndex,
			&status, &t.ResultURL, &t.Error,
			&t.Attempts, &t.CreatedAt, &t.CompletedAt,
		)
		if err != nil {
			return nil, err
		}
		t.Status = TaskStatus(status)
		tasks = append(tasks, t)
	}
	return tasks, nil
}

// MarkCompleted updates a task's status to completed.
func (ps *PersistentStore) MarkCompleted(ctx context.Context, externalID, resultURL string) error {
	now := time.Now()
	_, err := ps.pool.Exec(ctx,
		`UPDATE async_tasks SET status = 'completed', result_url = $1, completed_at = $2
		 WHERE external_id = $3`,
		resultURL, now, externalID,
	)
	return err
}

// MarkFailed updates a task's status to failed.
func (ps *PersistentStore) MarkFailed(ctx context.Context, externalID, errMsg string) error {
	now := time.Now()
	_, err := ps.pool.Exec(ctx,
		`UPDATE async_tasks SET status = 'failed', error = $1, completed_at = $2
		 WHERE external_id = $3`,
		errMsg, now, externalID,
	)
	return err
}
