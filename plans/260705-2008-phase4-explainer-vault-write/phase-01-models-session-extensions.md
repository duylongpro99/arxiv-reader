# Phase 01 — Models & Session Extensions

**Context:** `docs/phase4/brainstorm-summary.md` §4 (Models, Orchestrator accessors) · `docs/architecture.md` §3
**Priority:** Critical · **Status:** complete · **Depends on:** Phase 3 · **Effort:** ~S

## Overview
Foundation for everything downstream: the `ExplainerOutput` model, the three new pipeline stages,
and the mutex-guarded session accessors the pipeline + `/result` handler need. No behavior yet —
pure data contracts. Keeping this isolated lets Phases 02 and 03 proceed in parallel against a
stable API.

## Key insights (locked decisions)
- **`ExplainerOutput` is a new file** `internal/models/explainer.go` (per-phase model convention;
  see `paper.go` header comment "models defined per-phase as needed"). Do NOT touch `paper.go`.
- **Session fields are private + mutex-guarded.** Add fields to the existing `PipelineSession`
  struct and expose only through accessors that take `s.mu`. The status-poll handler reads
  concurrently — never expose raw fields. New fields are **server-only**: `Snapshot()` stays
  unchanged (they must never inflate `/status`).
- **`SelectedPaper()` getter needed.** Phase 04 reads the full `Paper` metadata server-side
  (title/authors/published/id) for the ExplainerAgent + VaultWriter. Phase 3 stored it via
  `SetSelectedPaper` but exposed no getter; add one (this is option (b) the Phase 3 plan flagged
  for "when Phase 4 needs the full paper server-side").
- **Token accounting:** `AddTokens(n)` accumulates across calls (single call in Phase 4, but the
  loop in Phase 5 will add more — additive API is future-proof and costs nothing now).

## Requirements
- `models.ExplainerOutput`:
  ```go
  type ExplainerOutput struct {
      PaperID      string
      Content      string            // full Markdown: "# Title\n## Problem Statement\n…"
      Sections     map[string]string // keyed by section slug; best-effort (Phase 02 parses)
      Iteration    int               // 1 in Phase 4
      InputTokens  int
      OutputTokens int
      CreatedAt    time.Time
  }
  ```
- New stages in `session.go`:
  ```go
  StageGenerating PipelineStage = "generating"
  StageWriting    PipelineStage = "writing"
  StageComplete   PipelineStage = "complete"
  ```
- New private fields on `PipelineSession`: `selectedPaper *Paper` (already exists),
  `explainer *ExplainerOutput`, `vaultFile string`, `tokensUsed int`.
- New accessors (all lock the mutex; mirror existing accessor style + doc comments):
  - `SelectedPaper() *Paper` (RLock)
  - `SetExplainer(*ExplainerOutput)` / `Explainer() *ExplainerOutput`
  - `SetVaultFile(string)` / `VaultFile() string`
  - `AddTokens(int)` / `TokensUsed() int`
- `Snapshot()` / `SessionSnapshot` **unchanged** — new fields are server-only.

## Related code files
**Create:**
- `backend/internal/models/explainer.go` — `ExplainerOutput` (+ `time` import).

**Modify:**
- `backend/internal/models/session.go` — 3 stage consts; 3 private fields; 7 accessors.
- `backend/internal/models/session_test.go` — accessor round-trip tests; assert new fields absent
  from `Snapshot()` (server-only guarantee); `-race` concurrent read/write smoke test.

## Implementation steps
1. Create `explainer.go` with `ExplainerOutput` + doc comment.
2. Add stage consts to the `const (...)` block in `session.go` (keep the ordering comment honest —
   these are the Phase 4 stages).
3. Add private fields to `PipelineSession` with a short comment mirroring the existing
   "Phase 3 extraction state … excluded from Snapshot()" note.
4. Add the 7 accessors, each locking `s.mu` (Set* → `Lock`, getters → `RLock`), with doc comments
   matching the file's style.
5. Extend `session_test.go`; `go build ./...` + `go test -race ./...` green.

## Todo
- [x] `models/explainer.go` — `ExplainerOutput`
- [x] `session.go` — `StageGenerating`/`StageWriting`/`StageComplete`
- [x] `session.go` — private fields `explainer`/`vaultFile`/`tokensUsed`
- [x] `session.go` — accessors `SelectedPaper`, `Set/Get Explainer`, `Set/Get VaultFile`, `AddTokens/TokensUsed`
- [x] `session_test.go` — accessor + Snapshot-exclusion + `-race` tests
- [x] `go test -race ./...` green

## Success criteria
- `ExplainerOutput` compiles and is importable by `agents` + `tools`.
- Accessors are mutex-guarded; `-race` clean under concurrent Set/Get.
- `Snapshot()` output is byte-identical to Phase 3 (no new fields leak to `/status`).

## Risk Assessment
| Risk | L×I | Mitigation |
|---|---|---|
| New field accidentally added to `Snapshot()` | Low×Med | Explicit test asserting snapshot shape unchanged. |
| Direct field access from a goroutine bypassing the lock | Low×High | Only accessors exposed; getters return copies/pointers documented server-only. |

## Backwards compatibility
Additive only. Existing stages, `Snapshot()`, and all Phase 1–3 callers untouched.

## Rollback
Delete `explainer.go`; revert the additive `session.go` edits. In-memory only — no persistence.

## Security
No new external surface. Server-only fields never cross the `/status` boundary.

## Next Steps
Unblocks Phase 02 (needs `ExplainerOutput`) and Phase 03 (needs `ExplainerOutput`), then Phase 04
(needs stages + accessors). File ownership: this phase solely owns `models/*`.
