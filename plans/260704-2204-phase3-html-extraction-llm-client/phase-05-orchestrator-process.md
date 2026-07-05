# Phase 05 — Orchestrator /process + runPipeline

**Context:** `docs/phase3/prd.md` §2.3, §4 (Selection/Fetch flow, Error flow), §7 · `brainstorm-summary.md` §7
**Priority:** Critical · **Status:** complete · **Depends on:** 02, 04 · **Effort:** ~L

## Overview
The handoff point. `POST /process` validates the session is in `selection`, finds the chosen paper
in `candidates`, records it, transitions to `extracting`, returns `{session_id}` immediately, and
spawns a detached goroutine `runPipeline`. `runPipeline` calls `PaperContentTool.FetchMarkdown`;
on 404 it recovers to `selection` (re-pick), on other errors it fails recoverably, on success it
stores the Markdown and stops (Phase 4 resumes here). Also constructs and holds the `LLMClient`
now (unused until Phase 4) so the wiring is proven this phase.

## Key insights (locked decisions)
- **Reuse the Phase 2 async contract**: immediate `{session_id}`, detached
  `context.WithoutCancel`, panic-recovery on the goroutine (mirror `runDiscovery` exactly —
  an unrecovered panic on a detached goroutine kills the whole process).
- **All session mutation via accessors** (Phase 01): `SetSelectedPaper`, `SetStage`, `SetMarkdown`,
  `RecoverToSelection`, `Fail`. Never touch private fields — the poll handler reads concurrently.
- **404 → `RecoverToSelection`**, NOT `Fail`: candidates preserved, stage back to `selection`,
  recoverable notice set. Every other fetch error → `failSession(recoverable=true)`.
- **Selected paper lookup uses `snap.Candidates`** (Snapshot is lock-free). Match on `p.ID`;
  copy into a local so the goroutine reads a stable `*Paper` (`&p` in a range loop — bind per
  iteration to avoid the classic loop-variable alias; Go 1.22 per-iteration scoping helps, but be
  explicit).
- `StatusResponse` already carries `stage/candidates/notice/error/recoverable` — the new
  `extracting` stage and the recover notice flow through the existing DTO unchanged.

## Requirements (PRD F1, F2, F7)
- `POST /process` body `{session_id, paper_id}`:
  - unknown session → 404 JSON; not in `selection` → 400; paper_id not in candidates → 400.
  - else: `SetSelectedPaper`, `SetStage(StageExtracting)`, respond `{session_id}`, spawn goroutine.
- `runPipeline(ctx, session)`: panic-recover; `FetchMarkdown(ctx, selected.ID)`:
  - `ErrPaperHTMLNotFound` → `RecoverToSelection("Paper HTML not available. Select another paper.")`.
  - `ErrPaperHTMLFailed`/`ErrPaperHTMLTimeout`/other → `failSession(session, err, true)`.
  - success → `SetMarkdown(md)`; log; stop (Phase 4 seam).
- Register `POST /process` in `server.go`. Construct `paperContentTool` + `llmClient` in `New`.
- Structured logs: `process requested`, `html fetch ...` (from tool), `paper html not found`
  (WARN, re-pick), `markdown stored`.

## Related code files
**Modify:**
- `backend/internal/orchestrator/orchestrator.go` — add `ProcessRequest`/`ProcessResponse`,
  `HandleProcess`, `runPipeline`, `failSession` (if not already present), extend `Orchestrator`
  struct with `content` + `llm`, wire them in `New`, extend `describeError` for the new sentinels.
- `backend/internal/server/server.go` — `mux.HandleFunc("POST /process", orch.HandleProcess)`.
- `backend/internal/orchestrator/orchestrator_test.go` — fakes for content tool + llm client;
  cases: process happy path → `extracting` then markdown stored; 404 → back to `selection` +
  notice + candidates intact; other error → `failed` + recoverable; wrong stage → 400; unknown
  paper_id → 400; unknown session → 404.
- `backend/internal/server/integration_test.go` — end-to-end via httptest: discover → select a
  candidate → `POST /process` → poll to `extracting`/`selection`, using an httptest arXiv-HTML
  server (200 and 404 variants) pointed at via `arxiv_html_base_url`.

## Design detail
```go
// Orchestrator additions
type PaperContent interface { FetchMarkdown(ctx context.Context, arxivID string) (string, error) }

type Orchestrator struct {
    sessions sync.Map
    cfg      *config.Config
    disco    PaperFetcher
    logCheck Unprocessor
    content  PaperContent   // NEW (interface → testable with a fake)
    llm      llm.LLMClient  // NEW (constructed now, invoked in Phase 4)
}

func New(cfg *config.Config) (*Orchestrator, error) { // signature grows: llm ctor can error
    client, err := llm.NewLLMClient(&cfg.LLM)
    if err != nil { return nil, err }
    return &Orchestrator{ cfg: cfg, disco: tools.NewDiscoveryTool(&cfg.Agent),
        logCheck: tools.NewLogCheckTool(&cfg.Paths),
        content: tools.NewPaperContentTool(&cfg.Agent), llm: client }, nil
}

type ProcessRequest  struct { SessionID string `json:"session_id"`; PaperID string `json:"paper_id"` }
type ProcessResponse struct { SessionID string `json:"session_id"` }
```
```go
func (o *Orchestrator) HandleProcess(w, r) {
    var req ProcessRequest; json.NewDecoder(r.Body).Decode(&req)
    v, ok := o.sessions.Load(req.SessionID); if !ok { 404 JSON }
    s := v.(*models.PipelineSession); snap := s.Snapshot()
    if snap.Stage != models.StageSelection { 400 "not in selection" }
    var selected *models.Paper
    for i := range snap.Candidates { if snap.Candidates[i].ID == req.PaperID { p := snap.Candidates[i]; selected = &p; break } }
    if selected == nil { 400 "paper not in candidates" }
    s.SetSelectedPaper(selected); s.SetStage(models.StageExtracting)
    writeJSON(w, ProcessResponse{s.SessionID})
    go o.runPipeline(context.WithoutCancel(r.Context()), s)
}

func (o *Orchestrator) runPipeline(ctx, s) {
    defer recover→ s.Fail("Processing crashed unexpectedly. Please try again.", true)
    snap := s.Snapshot() // selectedPaper is private → pass ID some other way (see note)
    md, err := o.content.FetchMarkdown(ctx, <selected paper ID>)
    if err != nil {
        if errors.Is(err, tools.ErrPaperHTMLNotFound) { s.RecoverToSelection("Paper HTML not available. Select another paper."); return }
        msg, rec := describeError(err); s.Fail(msg, rec); return
    }
    s.SetMarkdown(md) // Phase 4 resumes here
}
```
> **Selected-paper ID access:** `selectedPaper` is private and excluded from `Snapshot()`. Two
> clean options — pick one and document it: (a) `runPipeline(ctx, s, paperID string)` — pass the
> ID captured in `HandleProcess`; or (b) add a small locked getter `SelectedPaperID() string` in
> Phase 01's accessor set. **(a) is simpler (KISS)** and avoids widening the model API — prefer it
> unless Phase 4 needs the full paper server-side (it needs metadata → then (b)). Coordinate with
> Phase 01 if (b) is chosen.
>
> **`New` now returns `(*Orchestrator, error)`** because `NewLLMClient` can fail. Update
> `server.go` `Handler` to propagate the error (log-fatal at startup — a bad provider should stop
> the server, matching config fail-fast). This is a small breaking change to `New`'s signature —
> `server.go` is the only caller.

## Implementation steps
1. Extend `Orchestrator` struct + `New` (construct content tool + llm client; return error).
2. `PaperContent` consumer interface (for test fakes), DTOs.
3. `HandleProcess`: validation ladder + accessor mutations + detached goroutine.
4. `runPipeline`: panic-recover, fetch, 404→recover, error→fail, success→SetMarkdown.
5. Extend `describeError` with `ErrPaperHTMLFailed`/`Timeout` messages (§7 table).
6. Register `POST /process` in `server.go`; propagate `New` error.
7. Orchestrator tests (fake content tool + fake llm) for all branches; integration test with
   httptest arXiv-HTML (200 + 404).
8. `go build ./...` + `go test -race ./...` green.

## Todo
- [x] `Orchestrator` struct + `New` construct content tool + `llm` client (returns error)
- [x] `PaperContent` interface + `ProcessRequest`/`ProcessResponse`
- [x] `HandleProcess` validation (404/400/400) + accessors + detached goroutine
- [x] `runPipeline` panic-recover; 404→`RecoverToSelection`; other→`Fail`; success→`SetMarkdown`
- [x] `describeError` cases for paper-HTML sentinels
- [x] register `POST /process`; propagate `New` error in `server.go`
- [x] orchestrator_test.go all branches (fake tool + fake llm)
- [x] integration_test.go discover→select→process→poll (httptest 200 + 404)
- [x] `go test -race ./...` green

## Success criteria
- `POST /process` returns `{session_id}` fast; status polls `extracting`.
- 404 → status returns to `selection` with candidates intact + recoverable notice (no `failed`).
- Other fetch error → `failed` + recoverable message; success stores markdown (not in Snapshot).
- Unknown session 404, wrong stage 400, unknown paper 400. No data races.

## Risk Assessment
| Risk | L×I | Mitigation |
|---|---|---|
| Detached goroutine + request ctx → aborts on response | High×High | `context.WithoutCancel` (proven in Phase 2 `runDiscovery`). |
| Panic on goroutine kills process | Med×High | `defer recover()` → `Fail(recoverable)`, mirror `runDiscovery`. |
| `New` signature change breaks callers | Low×Med | Only caller is `server.go`; update in the same phase; integration test guards it. |
| Reading private `selectedPaper` in goroutine | Med×Med | Pass ID as arg (option a) — no cross-goroutine private read. |

## Backwards compatibility
Discovery flow (`/discover`, `/status`) untouched. `StatusResponse` DTO unchanged — `extracting`
and the recover notice reuse existing fields, so the frontend keeps polling with no contract break
until Phase 06 adds the label. `New`'s new error return is internal (server-side only).

## Rollback
Revert orchestrator + server edits and remove the new route; delete new tests. In-memory only —
no state/schema/migration to undo.

## Security
- `paper_id` is validated against the session's own `candidates` (server-held) — no arbitrary
  fetch target from the client; ID never concatenated from untrusted free-text.
- Loopback bind + narrow CORS unchanged. API key stays server-side in the llm client.

## Next Steps
**Blocks Phase 06** (frontend select button targets `POST /process`). Consumes Phase 02 (content
tool) + Phase 04 (llm client). File ownership: this phase owns `orchestrator.go`/`server.go` + their
tests — no other Phase 3 phase edits them.
