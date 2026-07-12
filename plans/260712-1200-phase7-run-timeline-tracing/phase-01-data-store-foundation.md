# Phase 01 — Data Store Foundation

## Context Links
- Design: `docs/superpowers/specs/2026-07-12-run-timeline-tracing-design.md` (§5)
- Config: `backend/internal/config/config.go`, `config.yaml`, `.env.example`
- Wiring: `backend/cmd/server/main.go`, `backend/internal/server/server.go`

## Overview
- **Priority:** High (foundation for all later phases)
- **Status:** ✅ complete
- Stand up PostgreSQL in Docker, the user-run migration, the `internal/store` repository
  (pgx + plain SQL), and the config surface (`tracing` block + `DATABASE_URL`). Nothing emits
  events yet — this phase only makes durable storage *available* and *optional*.

## Key Insights
- Tracing must **degrade gracefully**: if `DATABASE_URL` is empty or Postgres is unreachable at
  startup, the app runs with a no-op/in-memory store. The paper pipeline never depends on the DB.
- **No-migrations rule**: the agent ships the SQL as a file; the **user** runs it. Do NOT run
  `psql` migration commands or any auto-migrate on boot.
- Repo style is stdlib-first, no ORM. Use `jackc/pgx/v5` with hand-written SQL.

## Requirements
**Functional**
- A `RunStore` persists/updates `runs` rows; an `EventStore` appends/reads `run_events` rows.
- A `store.Open(ctx, url)` returns a live store, or a typed "unavailable" signal the caller
  logs and continues past (no fatal).
- Ordered read: `EventStore.List(runID)` returns events by ascending `seq`.
- History read: `RunStore.List(limit, offset)` returns runs by `started_at DESC`.

**Non-functional**
- Connection pool bounded (`pgxpool` default is fine for single-user local).
- No secret ever logged; DSN is read from env like `LLM_API_KEY`.

## Architecture
```
config.Config
  ├── DatabaseURL string        (from .env, like APIKey; yaml:"-")
  └── Tracing TracingConfig     (enabled, full_payloads, buffer_size)

internal/store/
  ├── store.go     Open(ctx,url) (*Store, error); Store wraps *pgxpool.Pool; Close()
  ├── runs.go      RunStore: Create, UpdateStage, Finalize, Get, List
  ├── events.go    EventStore: Append, List
  └── model.go     RunRecord, EventRecord (DB row structs; JSONB via []byte/json.RawMessage)
```
- `Store` implements both `RunStore` and `EventStore` interfaces (defined consumer-side in
  `internal/tracing` in Phase 02; for Phase 01 define them here and Phase 02 imports/aliases,
  OR keep interfaces in `store` and let tracing depend on them — pick one and document). **Decision:
  interfaces live in `internal/tracing` (consumer); `store.Store` satisfies them structurally.**
  For Phase 01, expose concrete methods; Phase 02 declares the narrow interfaces it needs.

## Related Code Files
**Create**
- `docker-compose.yml` (repo root) — `postgres:17-alpine`, named volume `pgdata`, healthcheck,
  port `5432:5432`, `POSTGRES_DB=arxiv_reader`, user/pass from env.
- `backend/migrations/0001_run_timeline.sql` — exact DDL from design §5 (user-run).
- `backend/internal/store/store.go`, `runs.go`, `events.go`, `model.go`
- `backend/internal/store/store_test.go` (+ optional integration test gated on `DATABASE_URL`).

**Modify**
- `backend/internal/config/config.go` — add `DatabaseURL` (env `DATABASE_URL`) + `TracingConfig`;
  extend `validate()` (buffer_size > 0 when tracing enabled; url may be empty → degrade).
- `config.yaml` — add `tracing:` block (design §9).
- `.env.example` — document `DATABASE_URL=postgres://arxiv:arxiv@localhost:5432/arxiv_reader?sslmode=disable`.
- `backend/go.mod` / `go.sum` — add `github.com/jackc/pgx/v5`.
- `README.md` — one line: run compose + apply migration before using history.

## Implementation Steps
1. Add `pgx/v5` to `go.mod` (`go get github.com/jackc/pgx/v5`), run with sandbox off if the
   module cache is blocked (see memory note on go/npm sandbox friction).
2. Write `docker-compose.yml` + `backend/migrations/0001_run_timeline.sql`.
3. Extend config: `DatabaseURL` + `TracingConfig{Enabled bool; FullPayloads bool; BufferSize int}`;
   parse env override for `DATABASE_URL`; add defaults in `config.yaml`; validate.
4. Implement `store.Open` (ping with a short timeout; on failure return a sentinel
   `ErrStoreUnavailable` wrapping the cause — caller logs warn + continues).
5. Implement `RunStore`/`EventStore` SQL methods (parameterized queries only; JSONB via
   `json.RawMessage`).
6. Tests: unit-test SQL builders / row scanning against a test pool when `DATABASE_URL` is set,
   else skip with `t.Skip` (keeps CI green without a DB).

## Todo List
- [ ] Add `pgx/v5` dependency
- [ ] `docker-compose.yml` with postgres + healthcheck + volume
- [ ] `backend/migrations/0001_run_timeline.sql` (runs + run_events + index)
- [ ] Config: `DatabaseURL` + `TracingConfig` + defaults + validation
- [ ] `.env.example` + `config.yaml` + `README.md` doc lines
- [ ] `internal/store`: Open, RunStore, EventStore, model
- [ ] `store_test.go` (DB-gated, skips cleanly without `DATABASE_URL`)
- [ ] `go build ./...` + `go test -race ./...` green

## Success Criteria
- `docker compose up -d db` starts Postgres; user applies migration and sees `runs`/`run_events`.
- Backend boots with `DATABASE_URL` unset → logs one warning, no crash.
- Backend boots with a valid `DATABASE_URL` → store connects; `store_test` passes against it.

## Risk Assessment
- **Docker not installed** → app still runs (degraded). Mitigation: degrade path is the default.
- **Migration drift** vs. code — mitigate by keeping the SQL file the single schema source, and
  code scanning exactly those columns.

## Security Considerations
- DSN only from `.env` (git-ignored); never logged. Postgres bound to localhost via compose.
- Parameterized SQL only (no string interpolation) — injection-safe.

## Next Steps
- Phase 02 builds the tracing core on top of these stores.
- **User action:** after this phase, run the migration once (command in README + design §5).
