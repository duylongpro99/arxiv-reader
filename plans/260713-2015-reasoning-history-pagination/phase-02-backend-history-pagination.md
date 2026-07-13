# Phase 02 — Backend: History Content + arXiv Pagination (Features B & C)

**Priority:** High · **Status:** completed · **Wave:** 1 (parallel with phase-01)
**Owner agent:** backend (T2)
**Completed:** 2026-07-13

## Context Links
- Design note: `docs/design-notes/2026-07-13-reasoning-history-pagination.md`

## Problem
- **B:** History is content-free. The generated note lives only in the Obsidian `.md`; its path is in the `tool.vaultwriter.completed` event `Summary["path"]` (set at `orchestrator-pipeline.go:230-233`), not a first-class column. No endpoint serves it.
- **C:** arXiv `start` is hardcoded `"0"` (`discovery.go:124`); no way to fetch older papers. `/process` (`orchestrator.go:161-178`) requires the paper to exist in the **session's** `Candidates`, so pagination must extend that session — a decoupled browse endpoint would break selection.

---

## Feature B — `GET /runs/{id}/content`

### Related code files
- `backend/internal/server/server.go:27-37` — register route.
- `backend/internal/orchestrator/runs-handlers.go` — add `HandleRunContent`.
- Reuse `store.ListEvents(ctx, id, -1)` (`store/events.go:27-48`) to find the vaultwriter event.
- Reuse `validateWithinBase` (`tools/vaultwriter.go:84-95`) — may need to export it or add a small read helper in `tools`.

### Design
1. Route: `GET /runs/{id}/content` → `HandleRunContent`.
2. Guard: if `o.store == nil` → 503 (mirror `HandleRunsList` `runs-handlers.go:162-165`).
3. Load events, find the one with `event_type == "tool.vaultwriter.completed"` (kind constant in `tracing/event.go`), read `Summary["path"]` (string).
4. Validate path is within the configured Obsidian vault base (`cfg.ObsidianVault`) using `validateWithinBase` — **guards path traversal**. Reject otherwise (400).
5. Read file. Return JSON:
   ```json
   { "path": "…", "available": true, "markdown": "…" }
   ```
   File missing / no vaultwriter event → `{ "available": false, "path": "…"|null }`, HTTP 200 (NOT 500).
6. On read error other than not-exist → 500 with generic message (no raw content in logs).

### Response DTO
Add to `orchestrator/dto.go`:
```go
type RunContentDTO struct {
    Path      string `json:"path,omitempty"`
    Available bool   `json:"available"`
    Markdown  string `json:"markdown,omitempty"`
}
```

---

## Feature C — arXiv pagination via session extension

### Related code files
- `backend/internal/tools/discovery.go:87,118-126` — add `start` offset param.
- `backend/internal/models/session.go` — add cursor + `AppendCandidates`.
- `backend/internal/orchestrator/orchestrator.go` — add `HandleDiscoverMore`.
- `backend/internal/server/server.go` — register route.

### Design
1. **Parameterize fetch:**
   - `buildQueryURL(start int)` → `q.Set("start", fmt.Sprintf("%d", start))` (replace hardcoded `"0"` at `:124`).
   - `FetchPapers(ctx, start int, onRetry func(int))` → threads `start` into `buildQueryURL`. Update the one existing caller in `orchestrator-pipeline.go` (⚠ owned by phase-01 — **coordinate**: this single call-site change is small; either phase-02 makes it with phase-01's awareness, or expose an overload. Prefer: keep `FetchPapers(ctx, start, onRetry)` and pass `0` at the existing discovery call. Flag at integration.).
     - **Ownership note:** the discovery-run caller lives in `orchestrator-pipeline.go` (phase-01's file). To avoid co-editing, add a thin `FetchPapersFrom(ctx, start, onRetry)` and keep `FetchPapers` delegating with `start=0`, so phase-01's file needs NO change. **Recommended.**
2. **Session cursor:** `models.Session` gains `NextStart int` (or a method `Cursor()`), initialized to `FetchLimit` after the first discovery page. Add `AppendCandidates([]models.Paper)` that appends + updates any snapshot used by `/status`/`/process`. Respect existing concurrency (session likely guarded by a mutex — match the pattern).
3. **Handler `HandleDiscoverMore`** (`POST /discover/{sessionId}/more`):
   - Load session (`o.sessions.Load`); missing → 404 clear error (session expired/evicted).
   - Fetch next page at `session.NextStart` via `FetchPapersFrom`.
   - `logCheck.FilterUnprocessed` (`orchestrator.go:33` interface) to drop already-processed.
   - `session.AppendCandidates(newPapers)`; advance `NextStart += FetchLimit`.
   - Emit a timeline event (optional, additive) so history reflects the extra fetch.
   - Return the appended page: `{ "candidates": [...], "hasMore": bool }` (hasMore heuristic: `len(fetchedBeforeFilter) == FetchLimit`).
4. Route in `server.go`: `mux.HandleFunc("POST /discover/{sessionId}/more", o.HandleDiscoverMore)`.

### Response DTO
```go
type DiscoverMoreDTO struct {
    Candidates []PaperDTO `json:"candidates"`
    HasMore    bool       `json:"hasMore"`
}
```
(Reuse whatever candidate DTO `/status` uses — see `dto.go` `StatusResponse.Candidates`.)

## Implementation steps
1. B: add `RunContentDTO`, `HandleRunContent`, route; reuse `validateWithinBase`. Handle missing-file gracefully.
2. C: add `FetchPapersFrom(start)` (leave `FetchPapers` delegating start=0 → no phase-01 edit).
3. C: session cursor + `AppendCandidates` (match existing session mutex pattern).
4. C: `HandleDiscoverMore` + route + `DiscoverMoreDTO`.
5. `go build ./...` && `go test ./...`.

## Todo
- [x] `RunContentDTO` + `HandleRunContent` + `GET /runs/{id}/content` route
- [x] Path-traversal guard via `validateWithinBase`; missing file → available:false, 200
- [x] `FetchPapersFrom(start)` in discovery.go (no phase-01 file edit)
- [x] Session cursor + `AppendCandidates`
- [x] `HandleDiscoverMore` + `POST /discover/{sessionId}/more` route + `DiscoverMoreDTO`
- [x] build + test green

## Success criteria
- `GET /runs/{id}/content` on a completed run returns the note markdown; on a run whose vault file is gone returns `available:false` (200).
- `POST /discover/{sessionId}/more` returns older papers not previously shown; a subsequent `/process` of one of them succeeds (paper found in session Candidates).
- Path traversal outside the vault is rejected.
- `go build ./...` && `go test ./...` pass.

## Security
- Path validation mandatory (`validateWithinBase`) — never read outside `cfg.ObsidianVault`.
- Expired session → 404, not a panic.
- Do not log file contents or raw arXiv payloads (CLAUDE.md).

## Interfaces exposed to frontend (phase-04 contract)
- `GET /runs/{id}/content` → `{ path, available, markdown }`.
- `POST /discover/{sessionId}/more` → `{ candidates, hasMore }`.
