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

// SaveStage persists the result of a single pipeline stage to pipeline_stages.
func (cs *CheckpointStore) SaveStage(ctx context.Context, projectID string, stageIdx int, stage Stage, status string, input map[string]any, output map[string]any, stageErr error) error {
	var inputJSON []byte
	if input != nil {
		inputJSON, _ = json.Marshal(input)
	}
	var outputJSON []byte
	if output != nil {
		outputJSON, _ = json.Marshal(output)
	}
	var errMsg *string
	if stageErr != nil {
		s := stageErr.Error()
		errMsg = &s
	}

	_, err := cs.pool.Exec(ctx, `
		INSERT INTO pipeline_stages (project_id, stage, stage_index, status, input, output, error, started_at, finished_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7,
			CASE WHEN $4 = 'running' THEN NOW() ELSE NULL END,
			CASE WHEN $4 IN ('completed','failed') THEN NOW() ELSE NULL END,
			NOW()
		)
		ON CONFLICT (project_id, stage) DO UPDATE SET
			status     = EXCLUDED.status,
			input      = COALESCE(EXCLUDED.input, pipeline_stages.input),
			output     = COALESCE(EXCLUDED.output, pipeline_stages.output),
			error      = EXCLUDED.error,
			started_at = CASE WHEN EXCLUDED.status = 'running' AND pipeline_stages.started_at IS NULL THEN NOW() ELSE pipeline_stages.started_at END,
			finished_at= CASE WHEN EXCLUDED.status IN ('completed','failed') THEN NOW() ELSE pipeline_stages.finished_at END,
			updated_at = NOW()`,
		projectID, string(stage), stageIdx, status, inputJSON, outputJSON, errMsg,
	)
	return err
}

// StageInfo is returned by GetAllStages.
type StageInfo struct {
	Stage      string          `json:"stage"`
	StageIndex int             `json:"stageIndex"`
	Status     string          `json:"status"`
	Input      json.RawMessage `json:"input,omitempty"`
	Output     json.RawMessage `json:"output,omitempty"`
	Error      *string         `json:"error,omitempty"`
	StartedAt  *time.Time      `json:"startedAt,omitempty"`
	FinishedAt *time.Time      `json:"finishedAt,omitempty"`
}

// GetAllStages retrieves all stage records for a project.
func (cs *CheckpointStore) GetAllStages(ctx context.Context, projectID string) ([]StageInfo, error) {
	rows, err := cs.pool.Query(ctx, `
		SELECT stage, stage_index, status, input, output, error, started_at, finished_at
		FROM pipeline_stages
		WHERE project_id = $1
		ORDER BY stage_index ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stages []StageInfo
	for rows.Next() {
		var s StageInfo
		var inputBytes []byte
		var output []byte
		if err := rows.Scan(&s.Stage, &s.StageIndex, &s.Status, &inputBytes, &output, &s.Error, &s.StartedAt, &s.FinishedAt); err != nil {
			return nil, err
		}
		if inputBytes != nil {
			s.Input = json.RawMessage(inputBytes)
		}
		if output != nil {
			s.Output = json.RawMessage(output)
		}
		stages = append(stages, s)
	}
	return stages, rows.Err()
}

// UpdateStageOutput overwrites the output JSONB of an existing pipeline stage.
func (cs *CheckpointStore) UpdateStageOutput(ctx context.Context, projectID string, stage Stage, output map[string]any) error {
	outputJSON, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("marshal output: %w", err)
	}
	_, err = cs.pool.Exec(ctx, `
		UPDATE pipeline_stages
		SET output = $3, updated_at = NOW()
		WHERE project_id = $1 AND stage = $2`,
		projectID, string(stage), outputJSON,
	)
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
