# Phase 03 — Frontend: Reasoning Display (Feature A)

**Priority:** High · **Status:** completed · **Wave:** 2 (defines shared `types.ts` FIRST, before phase-04)
**Owner agent:** frontend (T3)
**Completed:** 2026-07-13
**Depends on:** phase-01 (API contract). Can scaffold against the documented contract if backend in flight.

## Context Links
- Design note: `docs/design-notes/2026-07-13-reasoning-history-pagination.md`
- Backend contract: phase-01 "Interfaces exposed to frontend".

## Problem
The timeline shows cryptic summaries (`reviewIterations 1`) and never surfaces the agent's reasoning even when `payloadFull` is present. `DetailBlock` (`run-event-row.tsx:117-133`) renders raw summary key/values only.

## Backend contract (from phase-01)
- Explainer/reviewer events carry `payloadFull: { systemPrompt, userPrompt, response }`.
- Decision events carry `summary: { decision, onPass?, flaggedSections?, narrative }`.

## Related code files
**Modify:**
- `frontend/lib/types.ts` — add reasoning types (**define these FIRST — phase-04 rebases on this file**):
  ```ts
  export interface LLMPayload { systemPrompt?: string; userPrompt?: string; response?: string }
  export interface DecisionSummary { decision?: string; onPass?: number; flaggedSections?: string[]; narrative?: string }
  ```
  Extend the existing event type so `payloadFull?: LLMPayload` and `summary` is typed to include decision fields (keep it permissive — summary is a free map today).
- `frontend/components/run-event-row.tsx` — add a "Reasoning" expander; relabel cryptic keys.
- `frontend/components/run-timeline.tsx` — no logic change expected; verify it passes through `payloadFull` (it's presentational, shared by live + history).

## Design
1. **Relabel summary keys** — a small label map so `reviewIterations` → "Review passes", `feedbackKeys` → "Sections flagged", `reviewPassed` → "Review passed", etc. Prefer the decision `narrative` string when present (render it as a sentence, not a key/value row).
2. **Reasoning expander** — when an event has `payloadFull` with prompt/response, render a collapsible "Reasoning" section below the summary showing:
   - System prompt (collapsed by default)
   - User prompt
   - Response (the draft/verdict)
   Use `<details>`/`<summary>` or the existing expand pattern. Monospace, `overflow-x:auto`, whitespace-pre-wrap. Long text scrolls within its own container (no page horizontal scroll).
3. **Decision narrative** — for `decision.*` events, show `summary.narrative` prominently + `flaggedSections` as chips.
4. Guard everything: `payloadFull` and decision fields are optional (absent when `full_payloads` off or on old runs).

## Implementation steps
1. Add types to `lib/types.ts`. **Commit/announce this file is done so phase-04 can rebase.**
2. Add label map + narrative rendering in `run-event-row.tsx`.
3. Add the Reasoning expander (prompt/response), optional + scroll-contained.
4. `npm run build` / type-check.

## Todo
- [x] Reasoning types in `lib/types.ts` (done first, announced)
- [x] Summary key relabeling map
- [x] Decision narrative + flaggedSections chips
- [x] Reasoning expander (system/user/response), optional + scroll-contained
- [x] type-check / build green

## Success criteria
- On a run with `full_payloads` on, expanding an explainer/reviewer event shows its prompt + response.
- Decision events read as sentences ("Revised: methodology & limitations flagged"), not `reviewIterations 1`.
- Runs without payloads render exactly as before (no crashes, no empty expanders).
- No horizontal page scroll; long prompts scroll inside their box.

## Shared-file protocol
`lib/types.ts` is co-owned with phase-04. **Phase-03 lands its type additions first**; phase-04 pulls and builds on them. Do not both edit simultaneously.
