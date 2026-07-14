-- 0002_publications.sql — Channel Publishing schema (Phase 8).
--
-- USER-RUN MIGRATION. Per the project's no-migrations rule, the agent never
-- generates or executes migrations; this file is the single schema source and
-- YOU apply it once, by hand:
--
--     docker compose up -d db
--     psql "$DATABASE_URL" -f backend/migrations/0002_publications.sql
--
-- The backend scans exactly the columns declared here. Keep this file and the
-- store row structs (backend/internal/store/model.go) in lockstep.
--
-- Safe to re-run: every object uses IF NOT EXISTS.

-- publications: one row per (run, channel) publish attempt/draft. Public posts
-- are irreversible, so this state must be durable and idempotent — the
-- UNIQUE(run_id, channel_id) constraint is what lets the store detect a
-- duplicate publish attempt via ON CONFLICT DO NOTHING instead of double-posting.
CREATE TABLE IF NOT EXISTS publications (
    id              TEXT PRIMARY KEY,
    run_id          TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    channel_id      TEXT NOT NULL,             -- e.g. "devto", "x"
    category        TEXT NOT NULL,             -- longform | digest | brief
    status          TEXT NOT NULL,             -- draft | approved | published | failed
    adapted_content TEXT NOT NULL,             -- editable draft body (repurposer output)
    title           TEXT,                      -- null until the channel/edit sets one
    external_url    TEXT,                      -- null until published
    external_id     TEXT,                      -- null until published
    error           TEXT,                      -- null unless status = failed
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at    TIMESTAMPTZ,               -- null until published
    UNIQUE (run_id, channel_id)                -- idempotency: one publication per channel per run
);

-- Draft/status lookups always scope by run (list drafts for a run's UI page).
CREATE INDEX IF NOT EXISTS idx_publications_run ON publications (run_id);
