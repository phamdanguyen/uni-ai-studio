-- 002_phase25_tables.sql
-- Agent memory, pipeline checkpoints, task tracking, workflow events

-- ========================================
-- Agent Memory (Tiered Storage - Cold Tier)
-- ========================================
CREATE TABLE IF NOT EXISTS agent_memory (
    key         TEXT PRIMARY KEY,
    value       JSONB NOT NULL,
    tier        TEXT NOT NULL DEFAULT 'cold',
    tags        TEXT[] DEFAULT '{}',
    ttl_seconds INTEGER,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    accessed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_agent_memory_tags ON agent_memory USING GIN(tags);
CREATE INDEX idx_agent_memory_tier ON agent_memory (tier);
CREATE INDEX idx_agent_memory_accessed ON agent_memory (accessed_at);

-- ========================================
-- Pipeline Checkpoints
-- ========================================
CREATE TABLE IF NOT EXISTS pipeline_checkpoints (
    project_id       TEXT PRIMARY KEY,
    last_stage       TEXT NOT NULL,
    last_stage_index INTEGER NOT NULL,
    data             JSONB NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ========================================
-- Async Task Tracking
-- ========================================
CREATE TABLE IF NOT EXISTS async_tasks (
    external_id  TEXT PRIMARY KEY,
    provider     TEXT NOT NULL,
    media_type   TEXT NOT NULL,
    project_id   TEXT NOT NULL,
    panel_index  INTEGER,
    status       TEXT NOT NULL DEFAULT 'pending',
    result_url   TEXT,
    error        TEXT,
    attempts     INTEGER NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX idx_async_tasks_status ON async_tasks (status);
CREATE INDEX idx_async_tasks_project ON async_tasks (project_id);

-- ========================================
-- Workflow Events (Event Streaming Log)
-- ========================================
CREATE TABLE IF NOT EXISTS workflow_events (
    id          TEXT PRIMARY KEY,
    event_type  TEXT NOT NULL,
    run_id      TEXT NOT NULL REFERENCES workflow_runs(id),
    step_key    TEXT,
    project_id  TEXT NOT NULL,
    data        JSONB,
    error       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_workflow_events_run ON workflow_events (run_id);
CREATE INDEX idx_workflow_events_project ON workflow_events (project_id);
CREATE INDEX idx_workflow_events_type ON workflow_events (event_type);
