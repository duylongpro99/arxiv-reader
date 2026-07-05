# Phase 03 — Cost, Context Pre-Check, arXiv Retry Counter (F3, F4, F5)

**Priority:** High · **Status:** ✅ complete · **Depends on:** 01

Three small, independent backend additions that surface via the DTOs added in Phase 01.

## F3 — Cost estimation
**New `backend/internal/llm/pricing.go`:**
```go
type TokenPricing struct{ InputPer1M, OutputPer1M float64 }
// Approximate; dated source comment. UI/README label as estimate.
var ModelPricing = map[string]TokenPricing{ /* models the project supports */ }
func EstimateCost(model string, in, out int) (float64, bool) { ... } // false if model absent
```
- Populate for the models config actually supports (default `claude-sonnet-4-6`) + the OpenAI/Gemini options documented in README. Keep in ONE file with a dated comment (R4).
- `/result` handler (`orchestrator.go` `HandleResult`): read `s.InputTokens()/s.OutputTokens()` (accumulated in Phase 01), call `EstimateCost(model, in, out)`, set `ResultResponse.{InputTokens, OutputTokens, EstimatedCostUSD, CostKnown}`. `CostKnown=false` when model not in table → UI hides cost.

## F4 — Context window pre-check (text-based, NOT PDF)
**New `backend/internal/llm/limits.go`:**
```go
var ModelContextLimits = map[string]int{ /* model → context window */ }
// ~4 chars per token for English prose; conservative. Input is Markdown TEXT.
func EstimateTokens(markdown string) int { return len(markdown) / 4 }
```
- In `orchestrator-pipeline.go`, AFTER extraction (markdown available) and BEFORE the generate loop:
  ```
  limit, known := llm.ModelContextLimits[model]
  if known {
     total := llm.EstimateTokens(md) + 900 /*system*/ + cfg.LLM.MaxTokens
     if total > limit { s.SetContextWarning(&models.ContextWarning{...,
        Suggestion:"Consider switching to Gemini (gemini-2.0-flash) for a larger context window."}) }
  }
  ```
- **Non-blocking:** attach warning to session (surfaced via `StatusResponse.ContextWarning` from Phase 01) and PROCEED. If the LLM later returns bad-request, the existing `ErrLLMBadRequest` path already messages "paper may be too large" (F1).

## F5 — arXiv retry progress counter
- `DiscoveryTool.FetchPapers(ctx)` is decoupled from the session. Add an **optional callback** rather than passing the session:
  ```go
  func (t *DiscoveryTool) FetchPapers(ctx context.Context, onRetry func(attempt int)) ([]Paper, error)
  ```
  In `fetchWithRetry`, call `onRetry(attempt)` on each 429/5xx retry (alongside the existing `slog.Warn`).
- Orchestrator `runDiscovery` passes `func(n int){ session.SetArxivRetryCount(n) }`. On success, reset to 0.
- Surfaced via `StatusResponse.ArxivRetryCount` (Phase 01). Frontend label handled in Phase 04.
- Update all `FetchPapers` call sites (prod + tests) to the new signature; pass `nil` where a counter isn't needed.

## Related code files
- Create: `llm/pricing.go`, `llm/limits.go`.
- Modify: `orchestrator/orchestrator.go` (result cost), `orchestrator/orchestrator-pipeline.go` (context pre-check + discovery callback), `tools/discovery.go` (callback param).
- Tests: `llm/` unit tests for `EstimateCost` (known/unknown model) + `EstimateTokens`; discovery test asserts `onRetry` fires per attempt; pipeline test asserts warning set when estimate exceeds a small injected limit.

## Todo
- [x] `pricing.go` + `EstimateCost` + tests
- [x] `limits.go` + `EstimateTokens` + tests
- [x] Result handler populates cost fields
- [x] Context pre-check in pipeline (non-blocking)
- [x] `FetchPapers` `onRetry` callback; wire `SetArxivRetryCount`; fix call sites
- [x] `go build ./... && go test ./internal/llm/... ./internal/tools/... ./internal/orchestrator/...`

## Success criteria
- Cost shown only for models in the table; `CostKnown=false` otherwise.
- Context warning set (not error) when estimate exceeds limit; pipeline still runs.
- `ArxivRetryCount` increments on simulated 429s and resets on success.

## Risks
- **R4 pricing/limit staleness:** single dated file + "approximate" UI/README caveat (accepted).
- **Signature change to `FetchPapers`:** callback keeps `DiscoveryTool` decoupled from `PipelineSession`. Update every call site to avoid build break.
- **Heuristic accuracy (R2 from PRD):** `len/4` is advisory only — never blocks. Real 400 caught by existing `ErrLLMBadRequest`.
