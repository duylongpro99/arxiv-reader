-- 0001_run_timeline.sql — Run Timeline Tracing schema (Phase 7).
--
-- USER-RUN MIGRATION. Per the project's no-migrations rule, the agent never
-- generates or executes migrations; this file is the single schema source and
-- YOU apply it once, by hand:
--
--     docker compose up -d db
--     psql "$DATABASE_URL" -f backend/migrations/0001_run_timeline.sql
--
-- The backend scans exactly the columns declared here. Keep this file and the
-- store row structs (backend/internal/store/model.go) in lockstep.
--
-- Safe to re-run: every object uses IF NOT EXISTS.

-- runs: one row per PipelineSession (the run's durable header).
CREATE TABLE IF NOT EXISTS runs (
    id            TEXT PRIMARY KEY,          -- existing session id
    paper_id      TEXT,                      -- null until selection.chosen
    paper_title   TEXT,
    stage         TEXT NOT NULL,             -- last known PipelineStage
    status        TEXT NOT NULL,             -- running | complete | failed | recovered
    input_tokens  INT  NOT NULL DEFAULT 0,
    output_tokens INT  NOT NULL DEFAULT 0,
    est_cost_usd  NUMERIC(10,4),
    review_passed BOOLEAN,
    started_at    TIMESTAMPTZ NOT NULL,
    completed_at  TIMESTAMPTZ
);

-- run_events: the ordered timeline; (run_id, seq) is the natural key.
CREATE TABLE IF NOT EXISTS run_events (
    run_id       TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    seq          INT  NOT NULL,             -- monotonic per run (0,1,2…)
    event_type   TEXT NOT NULL,
    stage        TEXT NOT NULL,
    title        TEXT NOT NULL,
    status       TEXT NOT NULL,             -- info | success | warning | error
    summary      JSONB,
    payload_full JSONB,                     -- nullable; opt-in full trace only
    duration_ms  INT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, seq)
);

-- History list is "newest runs first"; index the sort key.
CREATE INDEX IF NOT EXISTS idx_runs_started_at ON runs (started_at DESC);
