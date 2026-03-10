-- 005_pipeline_run_state.sql
-- Lưu trạng thái tổng thể của pipeline run (mode, current step, approval state)
-- Dùng cho cả Autopilot (track progress) và Step-by-Step (human-gated control)

-- Bảng pipeline_runs: 1 row per project run
CREATE TABLE IF NOT EXISTS pipeline_runs (
    project_id       TEXT        NOT NULL PRIMARY KEY,
    execution_mode   TEXT        NOT NULL DEFAULT 'autopilot', -- 'autopilot' | 'step_by_step'
    current_stage    TEXT        NOT NULL DEFAULT 'analysis',
    current_status   TEXT        NOT NULL DEFAULT 'pending',
    -- 'pending' | 'running' | 'awaiting_approval' | 'completed' | 'failed'
    story            TEXT        NOT NULL DEFAULT '',
    input_type       TEXT        NOT NULL DEFAULT 'novel',
    budget           TEXT        NOT NULL DEFAULT 'medium',
    quality_level    TEXT        NOT NULL DEFAULT 'standard',
    error            TEXT,
    started_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at     TIMESTAMPTZ,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Index để query nhanh theo status (ví dụ: tìm tất cả runs đang awaiting_approval)
CREATE INDEX IF NOT EXISTS idx_pipeline_runs_status
    ON pipeline_runs (current_status);

CREATE INDEX IF NOT EXISTS idx_pipeline_runs_mode
    ON pipeline_runs (execution_mode);

-- Cập nhật view project_pipeline_summary để include execution_mode
-- (DROP + CREATE vì PostgreSQL không hỗ trợ ALTER VIEW thêm column)
DROP VIEW IF EXISTS project_pipeline_summary;

CREATE OR REPLACE VIEW project_pipeline_summary AS
SELECT
    ps.project_id,
    COALESCE(pr.execution_mode, 'autopilot')     AS execution_mode,
    COALESCE(pr.current_stage, '')               AS current_stage,
    COALESCE(pr.current_status, 'pending')       AS run_status,
    COUNT(ps.stage)                              AS total_stages,
    COUNT(ps.stage) FILTER (WHERE ps.status = 'completed')  AS completed_stages,
    COUNT(ps.stage) FILTER (WHERE ps.status = 'failed')     AS failed_stages,
    COUNT(ps.stage) FILTER (WHERE ps.status = 'running')    AS running_stages,
    MIN(ps.started_at)                           AS started_at,
    MAX(ps.finished_at)                          AS finished_at,
    MAX(ps.updated_at)                           AS last_updated,
    CASE
        WHEN COUNT(ps.stage) FILTER (WHERE ps.status = 'failed') > 0      THEN 'failed'
        WHEN pr.current_status = 'awaiting_approval'                        THEN 'awaiting_approval'
        WHEN pr.current_status = 'completed'                                THEN 'completed'
        WHEN COUNT(ps.stage) FILTER (WHERE ps.status = 'completed') = COUNT(ps.stage)
             AND COUNT(ps.stage) > 0                                        THEN 'completed'
        WHEN COUNT(ps.stage) FILTER (WHERE ps.status = 'running') > 0      THEN 'running'
        ELSE 'pending'
    END AS overall_status
FROM pipeline_stages ps
LEFT JOIN pipeline_runs pr ON pr.project_id = ps.project_id
GROUP BY ps.project_id, pr.execution_mode, pr.current_stage, pr.current_status;
