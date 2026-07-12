---
plan: phase-07-run-timeline-tracing
title: "Phase 7 — Run Timeline Tracing"
project: ArXiv AI Paper Explainer Agent
status: complete
created: 2026-07-12
completed: 2026-07-12
owner: long.dao@maritime-ds.com
source_design: docs/superpowers/specs/2026-07-12-run-timeline-tracing-design.md
blockedBy: []
blocks: []
---

# Phase 7 — Run Timeline Tracing

## Overview

Give every run a **structured, readable, traceable timeline** that tells the story from
paper selection → tool calls → LLM calls → decisions → final result. Events are emitted
from a single `Recorder` seam in the orchestrator, dual-written to an in-memory ring
buffer (→ SSE live) and PostgreSQL (→ durable history), and rendered as a live
`<RunTimeline>` plus a `/runs` history page. **Tracing is additive and never breaks the
paper pipeline**: if Postgres is down, tracing degrades to in-memory only.

Design source of truth: `docs/superpowers/specs/2026-07-12-run-timeline-tracing-design.md`.

## Key Decisions (locked in brainstorming)

- Emission from the **orchestrator only**; tools/agents untouched.
- **Summaries + metadata by default**; optional full-payload column (config opt-in).
- **PostgreSQL in Docker**; migration is a **user-run** file (no-migrations rule).
- **SSE** live transport with `Last-Event-ID` resume; existing `/status` poll stays.
- **Full history list page** (`/runs`) to browse + reopen past runs.

## Phases

| Phase | Title | Status | Depends on |
|-------|-------|--------|-----------|
| 01 | [Data Store Foundation](phase-01-data-store-foundation.md) — Postgres, compose, migration, `internal/store`, config | ✅ complete | — |
| 02 | [Tracing Core](phase-02-tracing-core.md) — `internal/tracing`: event model, Recorder, ring buffer, broker, scrubber | ✅ complete | 01 |
| 03 | [Orchestrator Instrumentation](phase-03-orchestrator-instrumentation.md) — emit events across the pipeline | ✅ complete | 02 |
| 04 | [Transport — SSE + REST](phase-04-transport-sse-rest.md) — `/runs/:id/events`, `/runs`, `/runs/:id`, Next proxies | ✅ complete | 03 |
| 05 | [Frontend — Timeline + History](phase-05-frontend-timeline-history.md) — `<RunTimeline>`, `/runs` page, hooks | ✅ complete | 04 |
| 06 | [Wiring, E2E & Docs](phase-06-wiring-e2e-docs.md) — integration test, arch/changelog docs, manual E2E | ✅ complete | 05 |

## Key Dependencies

- New Go dep: `github.com/jackc/pgx/v5`.
- New infra: `docker-compose.yml` (postgres:17-alpine) + `DATABASE_URL` in `.env`.
- **User action required**: run `backend/migrations/0001_run_timeline.sql` before Phase 03
  produces persisted rows (Phase 01 documents the exact command).

## Success Criteria (whole plan)

- A live run streams an ordered event timeline to the UI within ~1s of each step.
- After completion (and after a backend restart), the run + full timeline reopen from `/runs`.
- With Postgres stopped, the pipeline still completes and the live timeline still streams
  (history/reopen disabled, warning logged) — no pipeline failure.
- `go test -race ./...` green; frontend `npm run build` green.
- No secrets or raw HTML persisted.
