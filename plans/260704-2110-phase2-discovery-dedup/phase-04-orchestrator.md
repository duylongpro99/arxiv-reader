# Phase 04 — Orchestrator (async pipeline + endpoints)

**Context:** `docs/phase2/prd.md` §2.3, §4 · `docs/phase2/brainstorm-summary.md` (async decision)
**Priority:** Critical · **Status:** pending · **Depends on:** 01, 02, 03 · **Effort:** ~L

## Overview
The conductor. Owns the in-memory session store and sequences `DiscoveryTool → LogCheckTool`
**asynchronously**: `POST /discover` creates a session, spawns a goroutine, returns
`{session_id}` immediately. `GET /status/:id` reports live stage + candidates when ready.
Contains no business logic — only coordination + state.

## Key insights (locked decisions)
- **Async, not sync.** PRD's synchronous handler + polling was contradictory; goroutine makes
  the F5 stage labels truthful and keeps the contract stable for Phase 3's slow LLM calls.
- **Trigger response drops `candidates`** — they arrive via `/status`. `DiscoverResponse` = `{session_id}`.
- Session mutated by goroutine, read by poll handler → all access via `PipelineSession`
  mutex accessors (Phase 01).

## Requirements (PRD F1, F5, F6)
- `POST /discover` → create session `stage: discovery`, spawn goroutine, respond `{session_id}` (202/200).
- Goroutine: `FetchPapers(ctx)` → `FilterUnprocessed` → cap to `display_limit` → set candidates,
  `Notice` if `< display_limit`, `stage: selection`. On error: `stage: failed` + message + `recoverable`.
- `GET /status/:sessionId` → `{stage, candidates?, notice?, error?, recoverable?}`; 404 if unknown.
- Structured logs: started / arxiv-complete / logcheck-complete / complete / failed, with
  `session_id`, `stage`, `duration_ms`.

## Related code files
**Create:**
- `backend/internal/orchestrator/orchestrator.go` — `Orchestrator`, session store, handlers,
  `runDiscovery` goroutine, ID generator, response DTOs.
- `backend/internal/orchestrator/orchestrator_test.go` — inject fake tools (interfaces) to test
  discover→selection, fewer-than-5 notice, failure→failed+recoverable, unknown session 404.
**Modify:**
- `backend/internal/server/server.go` — register `POST /discover`, `GET /status/{sessionId}`;
  construct `Orchestrator` from cfg; keep CORS/loopback.
- `backend/cmd/server/main.go` — no change beyond passing cfg (done in Phase 01).

## Design detail
```go
type Orchestrator struct {
    sessions sync.Map            // sessionID -> *models.PipelineSession
    cfg      *config.Config
    disco    PaperFetcher        // interface → testable
    logCheck Unprocessor         // interface → testable
}

// small interfaces the tools already satisfy (defined here, implemented in tools)
type PaperFetcher interface { FetchPapers(ctx context.Context) ([]models.Paper, error) }
type Unprocessor  interface { FilterUnprocessed([]models.Paper) ([]models.Paper, error) }

type DiscoverResponse struct { SessionID string `json:"session_id"` }
type StatusResponse struct {
    Stage       models.PipelineStage `json:"stage"`
    Candidates  []models.Paper       `json:"candidates,omitempty"`
    Notice      string               `json:"notice,omitempty"`
    Error       string               `json:"error,omitempty"`
    Recoverable bool                 `json:"recoverable,omitempty"`
}
```
Handlers:
```go
func (o *Orchestrator) HandleDiscover(w, r) {
    s := o.newSession()               // crypto/rand hex ID, stage=discovery, StartedAt
    o.sessions.Store(s.SessionID, s)
    go o.runDiscovery(context.WithoutCancel(r.Context()), s)  // detach: request ctx dies at response
    writeJSON(w, DiscoverResponse{s.SessionID})
}

func (o *Orchestrator) runDiscovery(ctx, s) {
    papers, err := o.disco.FetchPapers(ctx)
    if err != nil { s.Fail(mapArxivError(err)); return }
    unproc, err := o.logCheck.FilterUnprocessed(papers)
    if err != nil { s.Fail(logError(err)); return }     // corrupt log → recoverable=false
    if len(unproc) > o.cfg.Agent.DisplayLimit { unproc = unproc[:o.cfg.Agent.DisplayLimit] }
    notice := ""
    if len(unproc) < o.cfg.Agent.DisplayLimit {
        notice = fmt.Sprintf("Only %d new paper(s) found", len(unproc))
    }
    s.Complete(unproc, notice)        // sets candidates + notice + stage=selection
}

func (o *Orchestrator) HandleStatus(w, r) {
    id := r.PathValue("sessionId")
    v, ok := o.sessions.Load(id); if !ok { 404 }
    snap := v.(*models.PipelineSession).Snapshot()
    writeJSON(w, StatusResponse{...from snap...})
}
```
> **`context.WithoutCancel`:** the goroutine outlives the HTTP request (which returns
> immediately), so it must NOT use the request ctx directly — that ctx is canceled on response
> and would abort discovery instantly. Detach it; keep an internal timeout via the http client.

Error mapping (PRD §7 table) → human messages + `recoverable`:
- `ErrArxivRateLimit` → "arXiv is rate limiting requests. Please try again in a minute." (recoverable)
- `ErrArxivUnavailable` → "arXiv is currently unavailable. Please try again." (recoverable)
- `ErrArxivParse` → "arXiv returned an unexpected response." (recoverable)
- `ErrLogCorrupted` → "Processed-log file is corrupted; manual fix required." (NOT recoverable)

## Implementation steps
1. Define `Orchestrator`, interfaces, DTOs, `newSession` (crypto/rand hex).
2. `HandleDiscover` (spawn detached goroutine) + `runDiscovery` pipeline.
3. `HandleStatus` (snapshot → DTO, 404).
4. `mapArxivError`/log-error → message + recoverable.
5. Structured logging at each transition (`duration_ms` via `time.Since(StartedAt)`).
6. Wire routes in `server.go`; build.
7. Tests with fake `PaperFetcher`/`Unprocessor` for all branches.

## Todo
- [ ] Orchestrator + interfaces + DTOs + `newSession`
- [ ] `HandleDiscover` + detached `runDiscovery`
- [ ] `HandleStatus` (snapshot, 404)
- [ ] error→message+recoverable mapping
- [ ] transition logging with session_id/stage/duration_ms
- [ ] route registration in `server.go`
- [ ] `orchestrator_test.go`: selection, notice<5, failure+recoverable, corrupt=non-recoverable, 404
- [ ] build + test green

## Success criteria
- `POST /discover` returns `{session_id}` fast (no block on arXiv).
- Poll transitions `discovery → selection` with candidates; `< display_limit` sets notice.
- arXiv failure → `failed` + recoverable message; corrupt log → non-recoverable.
- Unknown session → 404. No data races (`go test -race`).

## Risks
- Goroutine + request ctx: MUST detach ctx (see note) or discovery aborts on response.
- Session store leak (one per trigger) — accepted/deferred (local single-user); documented.
- Race safety depends entirely on Phase 01 accessors — run `go test -race`.

## Security
- Loopback + narrow CORS unchanged (Phase 1). No user input reaches arXiv query.
