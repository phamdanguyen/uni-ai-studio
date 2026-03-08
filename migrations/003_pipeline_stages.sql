-- 003_pipeline_stages.sql
-- Per-stage result storage for pipeline resume + UI viewing

CREATE TABLE IF NOT EXISTS pipeline_stages (
    project_id   TEXT NOT NULL,
    stage        TEXT NOT NULL,
    stage_index  INTEGER NOT NULL,
    status       TEXT NOT NULL DEFAULT 'pending', -- pending, running, completed, failed
    output       JSONB,
    error        TEXT,
    started_at   TIMESTAMPTZ,
    finished_at  TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (project_id, stage)
);

CREATE INDEX idx_pipeline_stages_project ON pipeline_stages (project_id, stage_index);
CREATE INDEX idx_pipeline_stages_status  ON pipeline_stages (status);

-- View: project list với stage counts
CREATE OR REPLACE VIEW project_pipeline_summary AS
SELECT
    project_id,
    COUNT(*) AS total_stages,
    COUNT(*) FILTER (WHERE status = 'completed') AS completed_stages,
    COUNT(*) FILTER (WHERE status = 'failed') AS failed_stages,
    MIN(started_at) AS started_at,
    MAX(finished_at) AS finished_at,
    MAX(updated_at) AS last_updated,
    CASE
        WHEN COUNT(*) FILTER (WHERE status = 'failed') > 0 THEN 'failed'
        WHEN COUNT(*) FILTER (WHERE status = 'completed') = COUNT(*) THEN 'completed'
        WHEN COUNT(*) FILTER (WHERE status = 'running') > 0 THEN 'running'
        ELSE 'pending'
    END AS overall_status
FROM pipeline_stages
GROUP BY project_id;
