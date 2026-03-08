// Package pipeline — checkpoint/resume support.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Checkpoint stores pipeline state for resume after failure.
type Checkpoint struct {
	ProjectID      string         `json:"projectId"`
	LastStage      Stage          `json:"lastStage"`
	LastStageIndex int            `json:"lastStageIndex"`
	IntermediateData map[string]json.RawMessage `json:"intermediateData"`
	CreatedAt      time.Time      `json:"createdAt"`
}

// CheckpointStore persists pipeline checkpoints to PostgreSQL.
type CheckpointStore struct {
	pool   *pgxpool.Pool
	logger *slog.Logger
}

// NewCheckpointStore creates a checkpoint persistence layer.
func NewCheckpointStore(pool *pgxpool.Pool, logger *slog.Logger) *CheckpointStore {
	return &CheckpointStore{
		pool:   pool,
		logger: logger.With("component", "pipeline-checkpoint"),
	}
}

// Save persists a checkpoint after a stage completes.
func (cs *CheckpointStore) Save(ctx context.Context, projectID string, stageIndex int, stage Stage, req *PipelineRequest) error {
	data := make(map[string]json.RawMessage)
	fields := map[string]any{
		"analysis":   req.Analysis,
		"plan":       req.Plan,
		"characters": req.Characters,
		"locations":  req.Locations,
		"storyboard": req.Storyboard,
		"media":      req.Media,
		"voices":     req.Voices,
	}

	for k, v := range fields {
		if v != nil {
			j, _ := json.Marshal(v)
			data[k] = j
		}
	}

	dataJSON, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal checkpoint: %w", err)
	}

	_, err = cs.pool.Exec(ctx,
		`INSERT INTO pipeline_checkpoints (project_id, last_stage, last_stage_index, data, created_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (project_id) DO UPDATE SET
		   last_stage = EXCLUDED.last_stage,
		   last_stage_index = EXCLUDED.last_stage_index,
		   data = EXCLUDED.data,
		   created_at = NOW()`,
		projectID, string(stage), stageIndex, dataJSON,
	)
	if err != nil {
		return fmt.Errorf("save checkpoint: %w", err)
	}

	cs.logger.Info("checkpoint saved", "projectId", projectID, "stage", stage, "index", stageIndex)
	return nil
}

// Load retrieves the latest checkpoint for a project.
func (cs *CheckpointStore) Load(ctx context.Context, projectID string) (*Checkpoint, error) {
	var lastStage string
	var lastStageIndex int
	var dataJSON []byte
	var createdAt time.Time

	err := cs.pool.QueryRow(ctx,
		`SELECT last_stage, last_stage_index, data, created_at
		 FROM pipeline_checkpoints WHERE project_id = $1`,
		projectID,
	).Scan(&lastStage, &lastStageIndex, &dataJSON, &createdAt)

	if err != nil {
		return nil, nil // No checkpoint
	}

	var data map[string]json.RawMessage
	if err := json.Unmarshal(dataJSON, &data); err != nil {
		return nil, fmt.Errorf("unmarshal checkpoint: %w", err)
	}

	return &Checkpoint{
		ProjectID:        projectID,
		LastStage:        Stage(lastStage),
		LastStageIndex:   lastStageIndex,
		IntermediateData: data,
		CreatedAt:        createdAt,
	}, nil
}

// Delete removes a checkpoint (after pipeline completes).
func (cs *CheckpointStore) Delete(ctx context.Context, projectID string) error {
	_, err := cs.pool.Exec(ctx,
		`DELETE FROM pipeline_checkpoints WHERE project_id = $1`, projectID)
	return err
}

// Restore populates a PipelineRequest from checkpoint data.
func (cs *CheckpointStore) Restore(checkpoint *Checkpoint, req *PipelineRequest) error {
	restore := func(key string, target *map[string]any) {
		if raw, ok := checkpoint.IntermediateData[key]; ok {
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err == nil {
				*target = m
			}
		}
	}

	restore("analysis", &req.Analysis)
	restore("plan", &req.Plan)
	restore("characters", &req.Characters)
	restore("locations", &req.Locations)
	restore("storyboard", &req.Storyboard)
	restore("media", &req.Media)
	restore("voices", &req.Voices)

	return nil
}
