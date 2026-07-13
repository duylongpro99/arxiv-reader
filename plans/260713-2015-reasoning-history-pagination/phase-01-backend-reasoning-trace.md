# Phase 01 — Backend: Full Reasoning Trace (Feature A)

**Priority:** High · **Status:** completed · **Wave:** 1 (parallel with phase-02)
**Owner agent:** backend (T1)
**Completed:** 2026-07-13

## Context Links
- Design note: `docs/design-notes/2026-07-13-reasoning-history-pagination.md`
- Prior work: Phase 7 tracing (`docs/2026-07-12-run-timeline-tracing-design.md`)

## Problem
`PayloadFull` is plumbed end-to-end (`orchestrator/dto.go:90,127,137` → `store/events.go:20,42` → `run_events.payload_full` JSONB) but **NEVER populated** at any emit site. The recorder keeps it only when `Tracing.FullPayloads` is on (`recorder.go:109-113`), else nils it. So enabling the flag today shows empty. Also, decision events render as cryptic key/values (`reviewIterations 1`) with no what/why.

## Requirements (functional)
1. Capture `{systemPrompt, userPrompt, rawResponse}` for each explainer & reviewer LLM call and attach to the corresponding timeline event's `PayloadFull`.
2. EXCLUDE the full paper `DocumentText` from payloads (identical every pass; tens of KB). Store prompts + response text only.
3. Enrich decision events with human-readable summary fields (flagged sections + decision verb).
4. Turn on `full_payloads` in config.

## Related code files
**Modify:**
- `backend/internal/models/explainer.go` — add `Trace` field to `ExplainerOutput`.
- `backend/internal/models/review.go` — add `Trace` field to `ReviewVerdict`.
- `backend/internal/agents/explainer.go` — populate `Trace` from the `CompletionRequest` (`SystemPrompt`, `UserPrompt` built at `:63-64`) + `resp` text (`:70`).
- `backend/internal/agents/reviewer.go` — same, from `:47-48,54`.
- `backend/internal/orchestrator/orchestrator-pipeline.go` — attach `Trace` to `PayloadFull` at explainer emit (`~:352 genDone`), reviewer emit (`~:377,410 rc`), and enrich decision emits (`:413 accept`, `:418-421 max`, `:426-429 revise`).
- `backend/internal/config/config.go` — no struct change needed (`FullPayloads bool` exists at `:33`); optionally document.
- `config.yaml:39` — set `full_payloads: true`.

**Create:** none (reuse existing structures).

## Design detail

### 1. Trace model (shared shape)
Add a small struct in `models` (e.g. in `explainer.go` or a new `models/trace.go` — implementer's call, keep it in `models`):
```go
// LLMTrace is the captured prompt/response for one agent LLM call. DocumentText
// is deliberately omitted (identical every pass; would bloat run_events JSONB).
type LLMTrace struct {
    SystemPrompt string `json:"systemPrompt"`
    UserPrompt   string `json:"userPrompt"`
    RawResponse  string `json:"rawResponse"`
}
```
Add `Trace *LLMTrace` (pointer, omitempty) to `ExplainerOutput` and `ReviewVerdict`.

### 2. Populate in agents
In `explainer.Generate` after `resp, err := a.llm.Complete(ctx, req)` succeeds, set:
```go
out.Trace = &models.LLMTrace{
    SystemPrompt: req.SystemPrompt,
    UserPrompt:   req.UserPrompt,   // req.UserPrompt excludes DocumentText already; verify
    RawResponse:  resp.Text,
}
```
Same in `reviewer.Review`. **Check:** `CompletionRequest` may carry `DocumentText` separately from `UserPrompt` (see `llm/client.go:26-27` and gemini prepending at `gemini.go:48`). Confirm `UserPrompt` does NOT embed the full paper; if it does, capture a truncated/summarized prompt instead. Do NOT store `req.DocumentText`.

### 3. Attach to PayloadFull at emit sites
The pipeline emits via `tev(...)` + `withSummary(...)`. There is no `withPayload` helper yet — **add one** mirroring `withSummary`:
```go
// withPayload attaches an opt-in full payload map to an event. The recorder
// drops it unless Tracing.FullPayloads is on (recorder.go:109-113), so callers
// can attach unconditionally.
func withPayload(e tracing.Event, kv map[string]any) tracing.Event {
    e.PayloadFull = kv
    return e
}
```
(Confirm the `tracing.Event.PayloadFull` field type — it is `map[string]any` scrubbed by `scrubMap`.)

At explainer-done emit (`~:352`): `withPayload(genDone, map[string]any{"systemPrompt": ex.Trace.SystemPrompt, "userPrompt": ex.Trace.UserPrompt, "response": ex.Trace.RawResponse})` (nil-guard `ex.Trace`).
At reviewer emit (`~:377/410`): same from `verdict.Trace`.

### 4. Enrich decision events
Decision emits at `:413-429`. Add summary fields via existing `withSummary`:
- Accept (`:413`): `withSummary(tev(KindDecisionAccept,...), map[string]any{"decision": "accepted", "onPass": iteration, "narrative": fmt.Sprintf("Accepted on pass %d — reviewer found no blocking issues", iteration)})`.
- Max iterations (`:418`): `"decision": "max_iterations"`, narrative "Stopped after N passes (max reached); last review still flagged: <sections>".
- Revise (`:426`): `"decision": "revise"`, `"flaggedSections"`: section slugs from `verdict.Feedback` keys, narrative "Revised: <sections> flagged". Section slugs come from `verdict.Feedback` (`models/review.go`), the 9 rubric slugs in `agents/revision-note.go:14-24`.

## Implementation steps
1. Add `LLMTrace` + `Trace` fields to the two models. `go build ./...`.
2. Populate `Trace` in `explainer.go` and `reviewer.go`. Verify `UserPrompt` excludes paper body.
3. Add `withPayload` helper; attach payloads at explainer/reviewer emits.
4. Enrich the three decision emits with human-readable summary fields.
5. Set `config.yaml` `full_payloads: true`.
6. `go build ./...` && `go test ./...`.

## Todo
- [x] `LLMTrace` model + `Trace` fields on `ExplainerOutput`/`ReviewVerdict`
- [x] Populate `Trace` in explainer + reviewer (exclude DocumentText)
- [x] `withPayload` helper in orchestrator
- [x] Attach payloads at explainer/reviewer emit sites (nil-guarded)
- [x] Enrich accept/max/revise decision summaries
- [x] `full_payloads: true` in config.yaml
- [x] build + test green

## Success criteria
- With `full_payloads: true`, a fresh run's `run_events.payload_full` contains prompt+response for explainer/reviewer events (verify via `psql` or `/runs/{id}`).
- Decision events carry a readable `narrative` + `flaggedSections`.
- Paper body text is NOT present in any payload.
- `go build ./...` && `go test ./...` pass.

## Security
- `scrub.scrubMap` runs on `PayloadFull` when flag on (`recorder.go:110`). **Verify** its secret patterns catch API keys that might appear in system prompts. If system prompts can contain no secrets, still confirm. Never log raw payloads (CLAUDE.md).

## Interfaces exposed to frontend (phase-03 contract)
`payloadFull` on explainer/reviewer events: `{ systemPrompt, userPrompt, response }`.
Decision-event `summary`: `{ decision, onPass?, flaggedSections?, narrative }`.
