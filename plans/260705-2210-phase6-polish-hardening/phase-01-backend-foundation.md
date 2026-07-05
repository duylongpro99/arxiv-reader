# Phase 01 — Backend Foundation

**Priority:** Critical (blocks 02, 03) · **Status:** ✅ complete

Shared data-model + error-taxonomy groundwork every later phase builds on. Pure additive
field/accessor work — no behaviour change on the happy path.

## Context
- Session: `backend/internal/models/session.go` — mutex-encapsulated, private fields + accessors. **Never** add public fields; add private field + `Set*/Get*` methods, all under `mu`.
- Error mapping: `backend/internal/orchestrator/pipeline-errors.go` — `describeError`, `describeGenErr`, `describeReviewErr`, `vaultErrMsg`, `vaultRecoverable`.
- DTOs: `backend/internal/orchestrator/dto.go` — `StatusResponse`, `ResultResponse`.
- Reviewer verdict: `backend/internal/models/review.go` — `ReviewVerdict.TokensUsed` (total only).
- Explainer: `backend/internal/models/explainer.go` — already has `InputTokens`/`OutputTokens`.

## Requirements
1. Session gains encapsulated state for retry routing, cost, context warning, arXiv retries.
2. Error `describe*` funcs additionally return a machine-readable **action** hint.
3. `ReviewVerdict` carries `InputTokens`/`OutputTokens` (split), like `ExplainerOutput`.
4. DTOs expose the new fields (all `omitempty` / additive — no contract break).

## Implementation steps

### 1. Session fields + accessors (`models/session.go`)
Add private fields to `PipelineSession`:
```go
failedStage     PipelineStage   // stage active when Fail() was called (retry routing)
errorAction     string          // "retry" | "fix_config" | "fix_permissions" | "select_other"
inputTokens     int             // accumulated input tokens (cost)
outputTokens    int             // accumulated output tokens (cost)
arxivRetryCount int             // current arXiv retry attempt (0 = none)
contextWarning  *ContextWarning // nil unless pre-check tripped
```
- In `Fail(message string, recoverable bool)`: **capture `s.failedStage = s.stage` BEFORE** setting `s.stage = StageFailed`. Signature unchanged → zero caller churn.
- Add a `FailWithAction(message string, recoverable bool, action string)` variant OR extend the error-mapping to also set action via a new `SetErrorAction`. **Recommended:** keep `Fail` as-is; add `SetErrorAction(a string)` and have the orchestrator set it right after `Fail` using the new `describe*` return. (Simplest; no signature break.)
- Accessors (all mutex-guarded): `FailedStage()`, `ErrorAction()`, `SetErrorAction()`, `AddIO(in, out int)`, `InputTokens()`, `OutputTokens()`, `SetArxivRetryCount(n)`, `ArxivRetryCount()`, `SetContextWarning(*ContextWarning)`, `ContextWarning()`.
- Extend `Snapshot()` + `SessionSnapshot` with `ErrorAction`, `ArxivRetryCount`, `ContextWarning` (NOT the large in/out token fields — those go to `/result`, mirroring the existing pattern that keeps big/late data off `/status`).

### 2. `ContextWarning` type (`models/session.go` or new `models/context-warning.go`)
```go
type ContextWarning struct {
    EstimatedTokens int
    ModelLimit      int
    Model           string
    Suggestion      string
}
```

### 3. Error action hint (`orchestrator/pipeline-errors.go`)
- Change `describeError`/`describeGenErr`/`describeReviewErr` to also return an `action` string; `vaultErrMsg` returns action too (`fix_permissions` when non-recoverable, else `retry`).
- Actions: transient → `"retry"`; `ErrLLMBadRequest` (bad model / too large) → `"fix_config"`; vault permission/disk → `"fix_permissions"`; HTML 404 handled by existing re-pick path (`RecoverToSelection`) — no action needed.
- Update the 3 call sites in `orchestrator-pipeline.go` to pass the action to `SetErrorAction`.

### 4. `ReviewVerdict` token split (`models/review.go` + reviewer)
- Add `InputTokens int`, `OutputTokens int` to `ReviewVerdict` (keep `TokensUsed` for back-compat / total).
- `backend/internal/agents/reviewer.go`: the LLM client already returns usage — populate the split on the verdict (mirror how `explainer.go` fills `ExplainerOutput`).
- In `orchestrator-pipeline.go`, wherever `s.AddTokens(verdict.TokensUsed)` runs, ALSO call `s.AddIO(verdict.InputTokens, verdict.OutputTokens)`; likewise for the explainer (`s.AddIO(ex.InputTokens, ex.OutputTokens)` alongside the existing `AddTokens`).

### 5. DTO additions (`orchestrator/dto.go`)
- `StatusResponse`: `+ ErrorAction string`, `+ ArxivRetryCount int`, `+ ContextWarning *models.ContextWarning` — all `,omitempty`.
- `ResultResponse`: `+ InputTokens int`, `+ OutputTokens int`, `+ EstimatedCostUSD float64`, `+ CostKnown bool`. (Populated in Phase 03.)

## Related code files
- Modify: `models/session.go`, `models/review.go`, `agents/reviewer.go`, `orchestrator/pipeline-errors.go`, `orchestrator/dto.go`, `orchestrator/orchestrator-pipeline.go`, `orchestrator/orchestrator.go` (Status/Result handlers map new fields).
- Create: `models/context-warning.go` (optional).
- Tests: extend `models/session_test.go` for new accessors + `Fail` capturing `failedStage`.

## Todo
- [x] Add session fields + accessors; `Fail` captures `failedStage`
- [x] `SetErrorAction` + Snapshot/DTO wiring
- [x] `ContextWarning` type
- [x] `describe*` return action; update call sites
- [x] `ReviewVerdict` in/out tokens; reviewer populates; pipeline accumulates via `AddIO`
- [x] DTO field additions
- [x] `go build ./...` + `go test ./internal/models/... ./internal/orchestrator/...`

## Success criteria
- Builds; existing tests green. New accessors covered. `Fail` records the pre-fail stage.
- No public field added to `PipelineSession`; all access mutex-guarded.

## Risks
- **Race audit:** every new field touched only through `mu`-guarded accessors (R: data race). Verify with `go test -race`.
- **Reviewer usage source:** confirm the reviewer LLM call exposes input/output usage; if it only returns a total today, surface the split from the client layer (same source explainer uses).
