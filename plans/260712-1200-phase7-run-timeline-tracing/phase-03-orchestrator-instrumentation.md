# Phase 03 — Orchestrator Instrumentation

## Context Links
- Design: `docs/superpowers/specs/2026-07-12-run-timeline-tracing-design.md` (§3, §4)
- Depends on: Phase 02 (`internal/tracing`)
- Files: `backend/internal/orchestrator/orchestrator.go`, `orchestrator-pipeline.go`

## Overview
- **Priority:** High
- **Status:** pending
- Wire the `Tracer` into the `Orchestrator` and emit the full event taxonomy from the existing
  emission points. Tools/agents stay untouched — the orchestrator already sees every input/output
  and every decision, so all `Emit` calls live here.

## Key Insights
- The orchestrator is the **single emission site** (design decision). Emit right where the code
  already logs via `slog` — those `slog.Info/Warn/Error` lines mark exactly the story beats.
- The Recorder for a run is created in `HandleDiscover`/`newSession` and looked up by both HTTP
  handlers (for `selection.chosen`) and the detached goroutines.
- Best-effort: every `Emit` is fire-and-forget; a nil/degraded tracer is a no-op. Never add an
  error path to the pipeline for tracing.
- **Invariant:** never put raw HTML or full markdown in an event summary — only sizes + previews.
  Full markdown goes to `payload_full` ONLY when `full_payloads` is on (already scrubbed).

## Requirements
**Functional — emit these, in order:**
| Point in code | Event(s) |
|---|---|
| `HandleDiscover` | `discovery.started` |
| `runDiscovery` after `FetchPapers` | `tool.discovery.completed` (count, duration) |
| `runDiscovery` after `FilterUnprocessed` | `tool.logcheck.completed` (filtered→candidates) |
| `runDiscovery` `Complete` | `selection.presented` (count, notice) |
| `HandleProcess` on valid pick | `selection.chosen` (paper id+title) + set `runs.paper_*` |
| `runPipeline` before/after `FetchMarkdown` | `tool.papercontent.started` / `.completed` (bytes, preview) / `.failed` |
| `runPipeline` 404 path | `run.recovered_to_selection` |
| `checkContextWindow` when tripped | `context.warning` |
| `runGenerateReview` per gen | `llm.explainer.started`/`.completed` (iter, in/out tokens, preview) |
| `runGenerateReview` per review | `llm.reviewer.started`/`.completed` (iter, pass, score, feedback keys) |
| `runGenerateReview` branch | `decision.revise` / `decision.accept` / `decision.max_iterations` |
| `runPipeline` after vault write | `tool.vaultwriter.completed` (path) |
| `runPipeline` complete | `run.completed` (tokens, cost, review outcome, duration) |
| any `Fail(...)` path | `run.failed` (message, action, recoverable) |

- On terminal events (`run.completed`/`run.failed`), call `recorder.Close(runID)` and
  `RunStore.Finalize` (status, tokens, cost, completed_at) — best-effort.

**Non-functional**
- No measurable latency added to the pipeline; emits are non-blocking.

## Architecture / Wiring
- Add to `Orchestrator`: `tracer *tracing.Tracer`. Build it in `orchestrator.New` from
  `cfg.Tracing` + the store opened in Phase 01 (New logs a warn and continues if the store is
  unavailable; `tracer` is always non-nil — it just skips DB).
- `New` signature unchanged (still `(*Orchestrator, error)`, error only for LLM provider).
  Open the store inside `New`; do not make store failure fatal.
- Helper: `func (o *Orchestrator) rec(s *models.PipelineSession) *tracing.Recorder` → looks up /
  lazily creates the recorder for `s.SessionID`. Keeps call sites terse: `o.rec(s).Emit(evt)`.
- Duration: capture `time.Now()` before a tool/LLM call, pass `time.Since(start)` into the
  `.completed` event (mirrors the existing `duration_ms` slog fields).

## Related Code Files
**Modify**
- `backend/internal/orchestrator/orchestrator.go` — struct field, `New` wiring, `HandleProcess`
  emit + `rec` helper.
- `backend/internal/orchestrator/orchestrator-pipeline.go` — emits across `runDiscovery`,
  `runPipeline`, `runGenerateReview`, `checkContextWindow`, `logFailure`.
- `backend/internal/orchestrator/orchestrator_test.go` / `orchestrator-review_test.go` — assert
  event sequences (use a fake/in-memory tracer recorder capturing emits).

**Create**
- (optional) `backend/internal/orchestrator/tracing.go` — the `rec` helper + small emit wrappers
  if `orchestrator-pipeline.go` would exceed ~200 lines; keep files focused.

## Implementation Steps
1. Extend `Orchestrator` + `New` to build/hold the `Tracer`; open the store; log-and-continue on
   unavailable.
2. Add the `rec` helper and create the recorder in `newSession`/`HandleDiscover`.
3. Insert emits at each row of the table above, pairing start/completed with duration and
   summaries built from data already in scope (counts, tokens, sizes, verdict).
4. On terminal states, `Finalize` the run row + `Close` the recorder.
5. Update orchestrator tests to assert the emitted `event_type` sequence for: happy path,
   revise-then-pass, reviewer-disabled (`max=0`), 404 re-pick, generation failure.

## Todo List
- [ ] `Orchestrator.tracer` + `New` store/tracer wiring (degrade-safe)
- [ ] `rec` helper + recorder creation on session start
- [ ] Discovery emits (started/discovery/logcheck/presented)
- [ ] `selection.chosen` in `HandleProcess` (+ set paper on run row)
- [ ] Extraction emits (started/completed/failed/recovered)
- [ ] context.warning emit
- [ ] Generate/Review/decision emits with iter + tokens + score
- [ ] vaultwriter.completed + run.completed (cost/tokens) + Finalize/Close
- [ ] run.failed emit from the Fail paths (via logFailure or adjacent)
- [ ] Sequence-assertion tests for 5 scenarios; `go test -race ./...` green

## Success Criteria
- Running a real (or faked) pipeline emits the exact ordered taxonomy for each scenario.
- With tracing disabled or DB down, the pipeline output is byte-identical to today (no behavior
  change) and no panics.
- `run.completed`/`run.failed` finalize the run row (when DB present) and close the stream.

## Risk Assessment
- **Missed emit / wrong order** — covered by sequence-assertion tests.
- **Double emit on retry** (resume re-runs a segment) — acceptable: events are append-only and
  seq-ordered; the timeline honestly shows the retry. Document this in the run.failed→retry story.

## Security Considerations
- Enforce the no-raw-HTML / no-full-markdown-in-summary invariant at every extraction/LLM emit.
- Reuse Phase 02 scrub; never hand the API key or `DocumentText` into a summary field.

## Next Steps
- Phase 04 exposes these events over SSE + REST.
