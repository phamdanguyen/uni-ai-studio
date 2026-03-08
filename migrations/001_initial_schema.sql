-- 001_initial_schema.sql
-- WAOO Studio initial PostgreSQL schema

-- World State: Source of truth per project
CREATE TABLE IF NOT EXISTS world_states (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID NOT NULL UNIQUE,
    version      INT NOT NULL DEFAULT 1,
    state_data   JSONB NOT NULL DEFAULT '{}',
    updated_at   TIMESTAMPTZ DEFAULT NOW(),
    updated_by   TEXT NOT NULL DEFAULT 'system'
);

CREATE INDEX idx_world_states_project ON world_states(project_id);

-- Event Store: All state changes as immutable events
CREATE TABLE IF NOT EXISTS world_events (
    id           BIGSERIAL PRIMARY KEY,
    project_id   UUID NOT NULL,
    event_type   TEXT NOT NULL,
    agent_name   TEXT NOT NULL,
    payload      JSONB NOT NULL,
    version      INT NOT NULL,
    created_at   TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_events_project ON world_events(project_id, version);
CREATE INDEX idx_events_type ON world_events(event_type);

-- Agent Decision Audit Trail
CREATE TABLE IF NOT EXISTS agent_decisions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id    UUID NOT NULL,
    task_id       UUID,
    agent_name    TEXT NOT NULL,
    skill_id      TEXT NOT NULL,
    model_used    TEXT,
    model_tier    TEXT,
    tokens_in     INT DEFAULT 0,
    tokens_out    INT DEFAULT 0,
    cost_usd      DECIMAL(10,6) DEFAULT 0,
    duration_ms   INT DEFAULT 0,
    reasoning     TEXT,
    output_ref    TEXT,
    quality_score DECIMAL(3,2),
    created_at    TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_decisions_project ON agent_decisions(project_id, created_at);
CREATE INDEX idx_decisions_agent ON agent_decisions(agent_name);

-- Projects
CREATE TABLE IF NOT EXISTS projects (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name         TEXT NOT NULL,
    description  TEXT,
    status       TEXT DEFAULT 'active',
    user_id      TEXT NOT NULL,
    settings     JSONB DEFAULT '{}',
    created_at   TIMESTAMPTZ DEFAULT NOW(),
    updated_at   TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_projects_user ON projects(user_id);

-- Workflow Runs (inspired by GraphRun from waoowaoo)
CREATE TABLE IF NOT EXISTS workflow_runs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    workflow_type   TEXT NOT NULL,
    status          TEXT DEFAULT 'queued',
    input           JSONB,
    output          JSONB,
    error_message   TEXT,
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW()
);

CREATE INDEX idx_runs_project ON workflow_runs(project_id, status);

-- Workflow Steps
CREATE TABLE IF NOT EXISTS workflow_steps (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id          UUID NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    step_key        TEXT NOT NULL,
    step_title      TEXT NOT NULL,
    step_index      INT NOT NULL,
    status          TEXT DEFAULT 'pending',
    agent_name      TEXT,
    skill_id        TEXT,
    input           JSONB,
    output          JSONB,
    error_message   TEXT,
    attempt         INT DEFAULT 0,
    max_attempts    INT DEFAULT 3,
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    updated_at      TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(run_id, step_key)
);

CREATE INDEX idx_steps_run ON workflow_steps(run_id, step_index);

-- Workflow Checkpoints (for resume)
CREATE TABLE IF NOT EXISTS workflow_checkpoints (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id      UUID NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    step_key    TEXT NOT NULL,
    version     INT NOT NULL,
    state_json  JSONB NOT NULL,
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(run_id, step_key, version)
);

CREATE INDEX idx_checkpoints_run ON workflow_checkpoints(run_id);
