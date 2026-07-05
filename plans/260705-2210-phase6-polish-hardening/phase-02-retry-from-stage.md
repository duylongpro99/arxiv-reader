# Phase 02 — Retry From Failed Stage (F2)

**Priority:** High (centerpiece) · **Status:** ✅ complete · **Depends on:** 01

Today `ErrorBanner.onRetry` restarts discovery from scratch — a generation/vault failure
discards the paper pick. F2: retry resumes from the failed stage, preserving the session.

## Key insight
The pipeline is linear with cached outputs already on the session:
`Markdown()` (extraction), `Explainer()` (generation), `Verdict()` (review). Make
`runPipeline` **resumable by skipping any segment whose output is already cached**. Resume
granularity = 4 segments (discovery / extraction / generate+review-loop / vault-write) —
**not** mid-loop (keeps Phase 5 loop logic untouched).

## Architecture
Segment skip logic at the top of the pipeline goroutine (`orchestrator-pipeline.go`):
```
if Markdown() == ""      → run extraction        (else skip)
if Explainer() == nil    → run generate+review    (else skip)   // loop is one unit
always                   → run vault write        (idempotent: no file on prior fail)
```
On a fresh run all caches are empty → full pipeline. On retry, cached segments skip:
- discovery fail → re-run discovery goroutine
- extraction fail → markdown empty → re-extract (+ downstream)
- generate/review fail → markdown cached, explainer nil → skip extract, re-run loop (LLM re-cost unavoidable)
- **transient vault fail → markdown + explainer cached → jump straight to write, ZERO LLM re-cost** ✅

## Requirements
1. `POST /retry/{sessionId}` — validate `stage==StageFailed && recoverable`; clear error state; route by `FailedStage()`; spawn the appropriate goroutine.
2. Non-recoverable failures return 400 (`"this error is not retryable"`) — UI never shows retry for them (already gated by `recoverable`).
3. Retry preserves `SelectedPaper()` — user does not re-pick.

## Implementation steps

### 1. Resumable pipeline (`orchestrator/orchestrator-pipeline.go`)
- Guard each segment with its cache check (above). Extraction already stores markdown via `SetMarkdown`; generation stores `SetExplainer`. Confirm no segment double-writes the processed-log on resume (log update must remain exactly-once, after a successful vault write only).
- Add a helper to reset transient error fields before resume: clear `errMsg`, `recoverable`, set stage to the resume stage.

### 2. Retry handler (`orchestrator/orchestrator.go` + route in `server/server.go`)
```go
func (o *Orchestrator) HandleRetry(w, r) {
    id := r.PathValue("sessionId")
    s, ok := o.getSession(id)
    if !ok || s.Snapshot().Stage != models.StageFailed || !s.Snapshot().Recoverable {
        http.Error(w, "session not retryable", 400); return
    }
    s.ClearErrorForRetry()            // new accessor: errMsg="", recoverable=false
    switch s.FailedStage() {
    case models.StageDiscovery:
        s.SetStage(models.StageDiscovery)
        go o.runDiscovery(context.Background(), s)
    case models.StageExtracting, models.StageGenerating,
         models.StageReviewing, models.StageRevising, models.StageWriting:
        s.SetStage(s.FailedStage())   // display; pipeline skips cached segments anyway
        go o.runPipeline(context.Background(), s)
    default:
        http.Error(w, "this error is not retryable", 400); return
    }
    writeJSON(w, 200, RetryResponse{SessionID: id})
}
```
- Register: `mux.HandleFunc("POST /retry/{sessionId}", orch.HandleRetry)` in `server.go`.
- Add `RetryResponse{SessionID string}` to `dto.go`.
- Add `ClearErrorForRetry()` accessor to `session.go` (mutex-guarded).

## Related code files
- Modify: `orchestrator/orchestrator-pipeline.go`, `orchestrator/orchestrator.go`, `orchestrator/dto.go`, `server/server.go`, `models/session.go` (`ClearErrorForRetry`).
- Tests: `orchestrator/orchestrator_test.go` + `server/integration_test.go` — retry resumes correct segment; cached-segment skip verified (mock a vault-only failure → retry writes without re-calling the LLM).

## Todo
- [x] Cache-guarded segments in `runPipeline`
- [x] `ClearErrorForRetry()` accessor
- [x] `HandleRetry` + route + `RetryResponse`
- [x] Tests: per-stage resume + vault-retry-skips-LLM
- [x] `go test -race ./internal/orchestrator/... ./internal/server/...`

## Success criteria
- Retry after a vault failure re-writes with no additional LLM call (assert token count unchanged).
- Retry after an LLM failure re-runs the loop but does NOT re-fetch HTML.
- Non-recoverable failure → `/retry` returns 400.
- Paper selection preserved across retry.

## Risks
- **R2 — loop × resume:** generate+review is ONE segment; never resume mid-loop. Enforced by the `Explainer()==nil` guard.
- **Exactly-once log update:** ensure the processed-log write stays after a successful vault write only, so a resumed run can't double-log or skip logging.
- **Concurrency:** `runPipeline` spawned on retry must not race a still-running goroutine — only reachable from `StageFailed`, which is terminal until retry, so safe.
