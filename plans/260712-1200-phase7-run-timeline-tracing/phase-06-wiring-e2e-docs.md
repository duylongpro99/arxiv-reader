# Phase 06 — Wiring, E2E & Docs

## Context Links
- Design: `docs/superpowers/specs/2026-07-12-run-timeline-tracing-design.md`
- Depends on: Phases 01–05
- Docs: `docs/architecture.md`, `docs/project-changelog.md`, `docs/development-roadmap.md`, `README.md`

## Overview
- **Priority:** Medium
- **Status:** pending
- Prove the whole path end-to-end, then bring the project docs up to date and record the manual
  cross-run validation. This is the "make it real and documented" phase — no new product surface.

## Key Insights
- The most valuable automated test is a **backend integration test** driving `POST /discover →
  /process` and asserting the emitted timeline + `/runs/:id` persisted read (with a test Postgres),
  plus the degraded path (no `DATABASE_URL`) still completing.
- Docs must be reconciled against live code (CLAUDE.md "Document Up-to-date" rule): the new store,
  tracing package, endpoints, and the DB dependency all need to appear in `architecture.md`.

## Requirements
**Functional**
- Integration test: full discovery→process happy path emits the expected ordered event types and,
  when a test DB is present, the run + events are readable via `/runs` and `/runs/:id`.
- Degraded test: with no store, the pipeline still completes and live SSE still streams.
- Docs updated: architecture (new components + data model + DB), changelog, roadmap, README (compose
  + migration + `/runs` usage).

**Non-functional**
- `go test -race ./...` and `npm run build` both green.

## Related Code Files
**Create**
- `backend/internal/server/timeline_integration_test.go` (or extend `integration_test.go`) — E2E
  over `httptest` with a fake tracer/store and, DB-gated, a real store.

**Modify**
- `docs/architecture.md` — add §: Run Timeline Tracing (Recorder, Broker, store, SSE/REST, DB in
  the service map + data model tables `runs`/`run_events`).
- `docs/project-changelog.md` — Phase 7 entry.
- `docs/development-roadmap.md` — mark Phase 7 status.
- `README.md` — "Timeline & history" section: `docker compose up -d db`, run the migration, where
  `/runs` lives, and the degrade note.
- `Makefile` — optional `make db` / `make migrate-print` (prints the psql command; does NOT run it,
  per no-migrations rule).

## Implementation Steps
1. Write the E2E integration test (fake store for CI default; real-store branch gated on
   `DATABASE_URL`, `t.Skip` otherwise).
2. Add the degraded-path assertion (nil store → completes, streams).
3. Update `docs/architecture.md` with the new components, endpoints, and data model.
4. Update changelog + roadmap + README + optional Makefile helper.
5. Manual E2E checklist (below); capture notes in `plans/260712-1200-phase7-run-timeline-tracing/reports/`.

## Manual E2E Checklist
- [ ] `docker compose up -d db`; apply `0001_run_timeline.sql`; start `make dev`.
- [ ] Trigger discovery → select a paper → watch the live timeline stream each step.
- [ ] Confirm `run.completed` shows tokens + cost; note saved to vault.
- [ ] Open `/runs` → the run appears; reopen it → full timeline + result render.
- [ ] Restart backend → `/runs` still lists the run; reopen still works (persistence proven).
- [ ] Stop Postgres → run a new pipeline → it completes and streams live; `/runs` degrades cleanly.
- [ ] Force a failure (bad model) → `run.failed` appears with action; retry re-streams honestly.
- [ ] Grep the DB / stream for any secret or raw HTML → none present (scrub verified).

## Todo List
- [ ] E2E integration test (happy path + persisted read, DB-gated)
- [ ] Degraded-path test (no store)
- [ ] `docs/architecture.md` updated + reconciled to live code
- [ ] changelog + roadmap + README updated
- [ ] Optional `make db` helper (migration is print-only)
- [ ] Manual E2E checklist executed; notes saved to `reports/`
- [ ] `go test -race ./...` + `npm run build` green

## Success Criteria
- Full plan Success Criteria (see `plan.md`) all demonstrably met.
- Docs match the shipped code; a new contributor can stand up the DB and use `/runs` from the README.

## Risk Assessment
- **DB-gated test skipped in CI** hides a real regression — mitigate with the fake-store E2E always
  running, plus the DB branch in local/CI-with-service.
- **Docs drift** — this phase explicitly reconciles; keep the architecture doc's data-model tables
  in sync with `0001_run_timeline.sql`.

## Security Considerations
- Final scrub verification is part of the manual checklist (no secrets / no raw HTML persisted).

## Next Steps
- Optional future work (out of scope): retention/pruning of old runs, a per-run "download trace"
  export, token-level LLM streaming into the timeline.
