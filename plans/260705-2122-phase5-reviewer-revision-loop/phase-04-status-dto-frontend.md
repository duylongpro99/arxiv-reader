# Phase 04 — Status DTO & Frontend Progress UI

**Priority:** Medium · **Status:** complete · **Depends on:** Phase 01, 03

Surface review progress to the user: iteration + score on `/status`, and labelled
`reviewing`/`revising` stages in the UI. Closes the latent gap where the frontend already
declares these stages but the backend never emitted them and the label map has no entry.

## Context Links
- PRD: `docs/phase5/prd.md` (§2.4 Progress UI, F5)
- Files: `backend/internal/orchestrator/dto.go`, `orchestrator.go` (`HandleStatus`),
  `frontend/lib/types.ts`, `frontend/components/progress-indicator.tsx`

## Requirements
- `/status` response carries `iteration`, `reviewScore`, `reviewPassed`.
- Frontend `PipelineStatus` mirrors those fields.
- `STAGE_LABEL` renders `reviewing`/`revising` with pass number.

## Related Code Files

**Modify:**
- `backend/internal/orchestrator/dto.go` — `StatusResponse` fields
- `backend/internal/orchestrator/orchestrator.go` — `HandleStatus` populates from `Snapshot()`
- `frontend/lib/types.ts` — `PipelineStatus` fields
- `frontend/components/progress-indicator.tsx` — `reviewing`/`revising` labels

## Implementation Steps

1. **`dto.go`** — add to `StatusResponse`:
   ```go
   Iteration    int     `json:"iteration,omitempty"`
   ReviewScore  float32 `json:"reviewScore,omitempty"`
   ReviewPassed bool    `json:"reviewPassed,omitempty"`
   ```
   (`omitempty` keeps pre-review stages clean.)

2. **`HandleStatus`** — populate the three fields from `session.Snapshot()` (the Snapshot fields
   added in Phase 01). No new locking — `Snapshot()` is the single read surface.

3. **`frontend/lib/types.ts`** — extend `PipelineStatus`:
   ```ts
   iteration?: number
   reviewScore?: number
   reviewPassed?: boolean
   ```
   (`PipelineStage` union already includes `reviewing`/`revising` — no change.)

4. **`progress-indicator.tsx`** — the current `STAGE_LABEL` is a static map. Either add static
   entries or switch to a small function so the pass number renders:
   ```ts
   // reviewing → `Reviewing (pass ${status.iteration ?? 1})…`
   // revising  → `Revising (pass ${status.iteration ?? 1})…`
   ```
   If the component only has the static map today, add a `getLabel(status)` that special-cases these
   two stages and falls back to the map for the rest. Keep the existing `?? "Working…"` default.
   Optionally show `reviewScore` beside the reviewing label when present (`score: 0.87`).

## Todo List
- [x] `StatusResponse` + 3 fields (`omitempty`)
- [x] `HandleStatus` populates from `Snapshot()`
- [x] `PipelineStatus` TS fields
- [x] `reviewing`/`revising` labels with pass number
- [x] `go build ./...` + frontend typecheck/lint clean

## Success Criteria
- Polling during review shows "Reviewing (pass 1)…"; during revision "Revising (pass 2)…".
- Pre-review stages omit the new JSON fields (no `iteration: 0` noise).
- Polling continues through `reviewing`/`revising` (already handled — verify no regression).

## Risk Assessment
- **Low.** Additive DTO/UI. Only watch: `omitempty` on `reviewPassed` hides `false` — fine for
  in-progress; final pass/fail is read from the vault note + `/result`, not `/status`.

## Security
- No new data exposure — score/iteration are non-sensitive progress metadata.
