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
	ProjectID        string                     `json:"projectId"`
	LastStage        Stage                      `json:"lastStage"`
	LastStageIndex   int                        `json:"lastStageIndex"`
	IntermediateData map[string]json.RawMessage `json:"intermediateData"`
	CreatedAt        time.Time                  `json:"createdAt"`
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
		"clips":      req.Clips,
		"screenplays": req.Screenplays,
		"storyboards": req.Storyboards,
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

// UpdateStageInput overwrites the input JSONB of an existing pipeline stage.
func (cs *CheckpointStore) UpdateStageInput(ctx context.Context, projectID string, stage Stage, input map[string]any) error {
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return fmt.Errorf("marshal input: %w", err)
	}
	_, err = cs.pool.Exec(ctx, `
		UPDATE pipeline_stages
		SET input = $3, updated_at = NOW()
		WHERE project_id = $1 AND stage = $2`,
		projectID, string(stage), inputJSON,
	)
	return err
}

// GetStageInput reads the stored input JSON for a single stage.
func (cs *CheckpointStore) GetStageInput(ctx context.Context, projectID string, stage Stage) (map[string]any, error) {
	var inputBytes []byte
	err := cs.pool.QueryRow(ctx, `
		SELECT input FROM pipeline_stages
		WHERE project_id = $1 AND stage = $2`,
		projectID, string(stage),
	).Scan(&inputBytes)
	if err != nil {
		return nil, err
	}
	if inputBytes == nil {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal(inputBytes, &m); err != nil {
		return nil, fmt.Errorf("unmarshal input: %w", err)
	}
	return m, nil
}

// --- pipeline_runs helpers (dual-mode state) ---

// UpsertRunState creates or updates the run state row for a project.
func (cs *CheckpointStore) UpsertRunState(ctx context.Context, state RunState) error {
	_, err := cs.pool.Exec(ctx, `
		INSERT INTO pipeline_runs
		    (project_id, execution_mode, current_stage, current_status, story,
		     input_type, budget, quality_level, error, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		ON CONFLICT (project_id) DO UPDATE SET
		    execution_mode  = EXCLUDED.execution_mode,
		    current_stage   = EXCLUDED.current_stage,
		    current_status  = EXCLUDED.current_status,
		    error           = EXCLUDED.error,
		    updated_at      = NOW()`,
		state.ProjectID, string(state.Mode),
		string(state.CurrentStage), string(state.CurrentStatus),
		"", "novel", "medium", "standard", // story/meta stored in pipeline_stages input
		state.Error,
	)
	return err
}

// UpsertRunStateFull upserts run state including story metadata.
func (cs *CheckpointStore) UpsertRunStateFull(ctx context.Context, projectID string, mode ExecutionMode, stage Stage, status StepStatus, req *PipelineRequest, errMsg string) error {
	_, err := cs.pool.Exec(ctx, `
		INSERT INTO pipeline_runs
		    (project_id, execution_mode, current_stage, current_status, story,
		     input_type, budget, quality_level, error, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW())
		ON CONFLICT (project_id) DO UPDATE SET
		    execution_mode  = EXCLUDED.execution_mode,
		    current_stage   = EXCLUDED.current_stage,
		    current_status  = EXCLUDED.current_status,
		    story           = EXCLUDED.story,
		    input_type      = EXCLUDED.input_type,
		    budget          = EXCLUDED.budget,
		    quality_level   = EXCLUDED.quality_level,
		    error           = EXCLUDED.error,
		    updated_at      = NOW()`,
		projectID, string(mode), string(stage), string(status),
		req.Story, req.InputType, req.Budget, req.QualityLevel, errMsg,
	)
	return err
}

// MarkRunCompleted sets a run as completed with timestamp.
func (cs *CheckpointStore) MarkRunCompleted(ctx context.Context, projectID string) error {
	_, err := cs.pool.Exec(ctx, `
		UPDATE pipeline_runs
		SET current_status = 'completed', completed_at = NOW(), updated_at = NOW()
		WHERE project_id = $1`, projectID)
	return err
}

// MarkRunFailed sets a run as failed with error message.
func (cs *CheckpointStore) MarkRunFailed(ctx context.Context, projectID, errMsg string) error {
	_, err := cs.pool.Exec(ctx, `
		UPDATE pipeline_runs
		SET current_status = 'failed', error = $2, updated_at = NOW()
		WHERE project_id = $1`, projectID, errMsg)
	return err
}

// UpdateRunStageStatus updates only the current stage and status without touching story metadata.
func (cs *CheckpointStore) UpdateRunStageStatus(ctx context.Context, projectID string, stage Stage, status StepStatus, errMsg string) error {
	_, err := cs.pool.Exec(ctx, `
		UPDATE pipeline_runs
		SET current_stage = $2, current_status = $3, error = $4, updated_at = NOW()
		WHERE project_id = $1`, projectID, string(stage), string(status), errMsg)
	return err
}

// GetRunState retrieves the current run state for a project.
func (cs *CheckpointStore) GetRunState(ctx context.Context, projectID string) (*RunState, error) {
	var mode, stage, status, errMsg string
	err := cs.pool.QueryRow(ctx, `
		SELECT execution_mode, current_stage, current_status, COALESCE(error, '')
		FROM pipeline_runs WHERE project_id = $1`, projectID,
	).Scan(&mode, &stage, &status, &errMsg)
	if err != nil {
		return nil, err
	}
	return &RunState{
		ProjectID:     projectID,
		Mode:          ExecutionMode(mode),
		CurrentStage:  Stage(stage),
		CurrentStatus: StepStatus(status),
		Error:         errMsg,
	}, nil
}

// GetRunMeta retrieves story metadata for a project run.
func (cs *CheckpointStore) GetRunMeta(ctx context.Context, projectID string) (story, inputType, budget, qualityLevel string, err error) {
	err = cs.pool.QueryRow(ctx, `
		SELECT COALESCE(story,''), COALESCE(input_type,'novel'),
		       COALESCE(budget,'medium'), COALESCE(quality_level,'standard')
		FROM pipeline_runs WHERE project_id = $1`, projectID,
	).Scan(&story, &inputType, &budget, &qualityLevel)
	if err != nil {
		story, inputType, budget, qualityLevel = "", "", "", ""
		err = nil
	}
	return
}

// GetStageOutput reads the stored output JSON for a single stage.
func (cs *CheckpointStore) GetStageOutput(ctx context.Context, projectID string, stage Stage) (map[string]any, error) {
	var outputBytes []byte
	err := cs.pool.QueryRow(ctx, `
		SELECT output FROM pipeline_stages
		WHERE project_id = $1 AND stage = $2`,
		projectID, string(stage),
	).Scan(&outputBytes)
	if err != nil {
		return nil, err
	}
	if outputBytes == nil {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal(outputBytes, &m); err != nil {
		return nil, fmt.Errorf("unmarshal output: %w", err)
	}
	return m, nil
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

	// Restore clips
	if raw, ok := checkpoint.IntermediateData["clips"]; ok {
		var clips []ClipData
		if err := json.Unmarshal(raw, &clips); err == nil {
			req.Clips = clips
		}
	}
	// Restore screenplays
	if raw, ok := checkpoint.IntermediateData["screenplays"]; ok {
		var sps []ScreenplayData
		if err := json.Unmarshal(raw, &sps); err == nil {
			req.Screenplays = sps
		}
	}
	// Restore storyboards
	if raw, ok := checkpoint.IntermediateData["storyboards"]; ok {
		var sbs []map[string]any
		if err := json.Unmarshal(raw, &sbs); err == nil {
			req.Storyboards = sbs
		}
	}

	return nil
}
