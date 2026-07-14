# Phase 02 — Publications store + schema

**Context:** design-note · `plan.md` · mirrors `internal/store/{model.go,runs.go}` + `migrations/0001_run_timeline.sql`
**Priority:** Critical · **Status:** complete
**Wave:** 1 (parallel with P1)

## Overview
Durable, idempotent publication state. Thin pgx/v5 hand-written SQL layer matching the Phase 7 store style. **DB required** for publishing — store methods are best-effort elsewhere but publishing endpoints (P3) refuse to run when `store == nil`.

## Schema (USER-RUN migration — agent does NOT execute)
`backend/migrations/0002_publications.sql`, `IF NOT EXISTS`, same header banner as 0001:
```sql
CREATE TABLE IF NOT EXISTS publications (
    id           TEXT PRIMARY KEY,
    run_id       TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    channel_id   TEXT NOT NULL,
    category     TEXT NOT NULL,                 -- longform | digest | brief
    status       TEXT NOT NULL,                 -- draft | approved | published | failed
    adapted_content TEXT NOT NULL,              -- editable draft body
    title        TEXT,
    external_url TEXT,
    external_id  TEXT,
    error        TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ,
    UNIQUE (run_id, channel_id)                 -- idempotency: one publication per channel per run
);
CREATE INDEX IF NOT EXISTS idx_publications_run ON publications (run_id);
```
Plan `plan.md` and `store/model.go` stay in lockstep with this file (per 0001 banner rule).

## Store API (`internal/store/publications.go`)
```go
type PublicationRecord struct {
    ID, RunID, ChannelID, Category, Status, AdaptedContent string
    Title, ExternalURL, ExternalID, Error *string   // nullable → pointer
    CreatedAt time.Time
    PublishedAt *time.Time
}
func (s *Store) CreatePublication(ctx, PublicationRecord) error          // ON CONFLICT (run_id,channel_id) DO NOTHING → detect dup
func (s *Store) ListPublicationsByRun(ctx, runID string) ([]PublicationRecord, error)
func (s *Store) GetPublication(ctx, id string) (PublicationRecord, error)
func (s *Store) UpdatePublicationContent(ctx, id, title, content, status string) error  // edit/approve
func (s *Store) MarkPublished(ctx, id, url, extID string) error
func (s *Store) MarkFailed(ctx, id, errMsg string) error
```
Add `PublicationRecord` to `model.go`. `CreatePublication` uses `ON CONFLICT DO NOTHING` and returns a sentinel/`bool` so P3 can 409 on duplicates without re-posting.

## Files
- Create: `internal/store/publications.go`, `migrations/0002_publications.sql`
- Modify: `internal/store/model.go`

## Todo
- [x] `0002_publications.sql` (do NOT run — document apply command in banner)
- [x] `PublicationRecord` in `model.go`
- [x] CRUD + state-transition methods
- [x] store tests (guard with existing DB-available test pattern from `store_test.go`; skip when no DB)
- [x] `go build ./... && go test ./...`

**Migration:** User action pending. Apply `backend/migrations/0002_publications.sql` once in your environment:
```bash
docker compose up -d db
psql "$DATABASE_URL" -f backend/migrations/0002_publications.sql
```

## Success criteria
Insert → list → get → update content → mark published round-trips. Duplicate `(run_id, channel_id)` insert is detected (no error thrown to a re-post). Nullable columns round-trip as nil.

## Notes for user (migration)
```
docker compose up -d db
psql "$DATABASE_URL" -f backend/migrations/0002_publications.sql
```
Agent must NOT invoke psql/migration generation (`.claude/rules/no-migrations-rule.md`).
