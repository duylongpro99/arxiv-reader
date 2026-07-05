# Phase 05 — Tests & Docs

**Priority:** High · **Status:** complete · **Depends on:** Phase 01–04

Prove the loop and reviewer behave per the exit criteria; update project docs. No test may use
mocks that hide real logic — use a fake `LLMClient` that returns scripted responses, per the
existing Phase 3/4 test pattern.

## Context Links
- PRD: `docs/phase5/prd.md` (Exit Criteria, §7 Error Handling)
- Existing test patterns: `backend/internal/agents/*_test.go`, `orchestrator/*_test.go`, `tools/*_test.go`

## Requirements
- Unit + integration coverage for reviewer parsing, revision-note formatting, loop exits, frontmatter.
- Update existing tests broken by new signatures (`WriteToVault` +verdict).
- Docs reflect the review loop + cost.

## Test Matrix

**Reviewer (`reviewer_test.go`)** — fake `LLMClient`:
- valid unfenced JSON → correct verdict, `Pass` = model value
- ` ```json `-fenced JSON → parses (fence stripped)
- JSON with `feedback` nulls → nils filtered out of map
- malformed JSON / preamble → `errors.Is(err, ErrReviewParse)` true
- `TokensUsed == InputTokens + OutputTokens`
- score never gates: `{pass:true, score:0.5}` → `Pass==true`; `{pass:false, score:0.99}` → `Pass==false`

**Revision note (`revision-note_test.go`)**:
- multi-section feedback → stable ordering across 100 runs (guard against map-range flakiness)
- empty feedback → note still well-formed
- `sectionDisplayName` known keys + fallback

**Orchestrator loop (`orchestrator_*_test.go`)** — fake explainer + fake reviewer:
- `max=0` → 1 generate, 0 reviews, `verdict==nil`, reaches `complete`
- `max=2`, review pass on iteration 1 → 1 generate, 1 review, no revision
- `max=2`, fail then pass → 2 generates (2nd is `revising`), revision note threaded, `complete`
- `max=2`, fail twice → stops at iteration 2, `review_passed:false`
- `max=1`, fail → 1 review, no revision, saved failed
- reviewer parse error on iteration 1 → loop stops, current explainer saved, verdict `Pass:false,Score:0`
- reviewer LLM error → session `failed`, recoverable
- token total accumulates across generations + reviews
- stages emitted in order incl. `reviewing`/`revising`

**Frontmatter (`vaultwriter-frontmatter_test.go`)**:
- `verdict==nil` → `review_iterations:0`, `review_passed:true`, no `review_score`
- passed verdict → real iteration/pass/score
- failed verdict → `review_passed:false`, `review_score` present

**Config (`config_test.go`)**:
- `max_review_iterations: -1` → load error; `0` and `2` → ok; default is `2`

## Implementation Steps
1. Update any existing `WriteToVault` call sites in tests to pass a verdict (nil where review absent).
2. Add the test files above using the project's existing fake-`LLMClient` helper (reuse; do not
   invent a new one).
3. `cd backend && go test ./... -race` — all pass.
4. Frontend: `cd frontend && <typecheck/lint/build>` per existing scripts — clean.

## Docs (update, verify against live code first)
- **README** — new section: review loop behaviour, `max_review_iterations` (0 disables, default 2),
  cost note (~200k tok/paper at max=2; 2 gen + 2 review).
- **`docs/architecture.md`** — document the critic-generator loop in the pipeline flow + ReviewerAgent
  component (the overview already mentions "critic-review loop" — flesh it out).
- **`docs/development-roadmap.md`** — mark Phase 5 status.
- **`docs/project-changelog.md`** — Phase 5 entry (ReviewerAgent, revision loop, frontmatter, UI).

## Todo List
- [x] Fix existing tests for new `WriteToVault` signature
- [x] Reviewer parse/verdict tests
- [x] Revision-note ordering tests
- [x] Orchestrator loop exit-condition tests (all rows above)
- [x] Frontmatter tests (nil/pass/fail)
- [x] Config validation test
- [x] `go test ./... -race` green
- [x] Frontend typecheck/lint/build green
- [x] README + 3 docs updated

## Success Criteria
- All PRD Exit Criteria demonstrably covered by a passing test or documented manual check.
- No skipped/failing tests. No fake data used to force green.

## Risk Assessment
- **Low-Medium.** Main effort is breadth. The map-ordering and off-by-one loop cases are the
  bug-prone spots — they have dedicated tests above.
