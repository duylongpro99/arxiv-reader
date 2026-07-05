# Phase 01 — Models, Session Accessors & Config

**Priority:** High · **Status:** complete · **Depends on:** none

Foundational, low-risk package changes with no behavioural coupling. Everything here is additive.

## Context Links
- PRD: `docs/phase5/prd.md` (§3 Data Model, F4)
- Files: `backend/internal/models/`, `backend/internal/config/`

## Requirements
- New `models.ReviewVerdict` type.
- `PipelineSession` gains verdict + iteration state (mutex-guarded, accessor pattern — **no naked field writes**).
- Two new pipeline stages.
- `AgentConfig.MaxReviewIterations` with default 2, validated `>= 0`.

## Related Code Files

**Create:**
- `backend/internal/models/review.go`

**Modify:**
- `backend/internal/models/session.go` — add fields, accessors, stage constants, Snapshot fields
- `backend/internal/config/config.go` — add field + validate
- `backend/config.yaml` — add `agent.max_review_iterations: 2`
- `backend/.env.example` — document optional override (if agent keys are overridable there; else config.yaml only)

## Implementation Steps

1. **`review.go`** — new file:
   ```go
   package models

   import "time"

   type ReviewVerdict struct {
       PaperID    string
       Pass       bool
       Score      float32
       Feedback   map[string]string // section key → actionable revision note
       Iteration  int
       TokensUsed int
       CreatedAt  time.Time
   }
   ```

2. **`session.go`** — add stage constants next to existing ones:
   ```go
   StageReviewing PipelineStage = "reviewing"
   StageRevising  PipelineStage = "revising"
   ```
   Add private fields `verdict *ReviewVerdict` and `iteration int`. Add accessors mirroring the
   existing mutex pattern (`Lock`/`defer Unlock` on writes, `RLock` on reads):
   - `SetVerdict(v *ReviewVerdict)`, `Verdict() *ReviewVerdict`
   - `SetIteration(n int)`, `Iteration() int`
   Extend `Snapshot()` (and its snapshot struct) with the fields the status DTO needs:
   `Iteration int`, `ReviewScore float32`, `ReviewPassed bool` — derived from `verdict`
   (`0`/`false` when `verdict == nil`). Keep `Snapshot()` the single read surface used by the handler.

3. **`config.go`** — add `MaxReviewIterations int` to `AgentConfig` with yaml tag
   `max_review_iterations`. In `AgentConfig.validate()` add: `if c.MaxReviewIterations < 0 { return err }`.
   Note: `0` is valid (disables reviewer).

4. **`config.yaml`** — add under `agent:`: `max_review_iterations: 2`.

5. **`.env.example`** — only add an override line if other `agent.*` values are `.env`-overridable
   today; otherwise leave a comment pointing to `config.yaml`. Do not invent a new override path.

## Todo List
- [x] Create `models/review.go` with `ReviewVerdict`
- [x] Add `StageReviewing`/`StageRevising` constants
- [x] Add `verdict`/`iteration` fields + 4 accessors to `PipelineSession`
- [x] Extend `Snapshot()` with `Iteration`/`ReviewScore`/`ReviewPassed`
- [x] Add `MaxReviewIterations` to `AgentConfig` + validate `>= 0`
- [x] Add default to `config.yaml`
- [x] `go build ./...` clean

## Success Criteria
- `go build ./...` passes.
- Config with `max_review_iterations: -1` fails fast at load; `0` and positive values load.
- `Snapshot()` returns zero-valued review fields when no verdict set.

## Risk Assessment
- **Low.** Additive only. Only risk: forgetting the mutex on new accessors → race. Mirror existing code exactly; run `go test -race ./internal/models/...` if a test exists.
