# Phase 02 — ReviewerAgent & Revision-Note Formatter

**Priority:** High · **Status:** complete · **Depends on:** Phase 01

The independent critic. Pure agent logic — no orchestrator coupling yet. Testable in isolation.

## Context Links
- PRD: `docs/phase5/prd.md` (§2.1 ReviewerAgent, §2.2 Revision Note Formatter, F1/F2)
- Mirror: `backend/internal/agents/explainer.go`, `explainer-prompt.go`

## Requirements
- `ReviewerAgent.Review` returns a structured `ReviewVerdict` from the explainer text alone.
- Robust JSON parsing with a **distinguishable sentinel parse error** (so the orchestrator can apply
  design decision 2 — stop, don't fail the session).
- `formatRevisionNote` produces natural-language text the *existing* `buildUserPrompt` revision
  branch already consumes.

## Related Code Files

**Create:**
- `backend/internal/agents/reviewer.go` — agent + `Review`
- `backend/internal/agents/reviewer-prompt.go` — system prompt + `buildReviewPrompt`
- `backend/internal/agents/revision-note.go` — `formatRevisionNote` + `sectionDisplayName`

## Implementation Steps

1. **`reviewer.go`**:
   ```go
   type ReviewerAgent struct {
       llm llm.LLMClient
       cfg *config.Config
   }
   func New(client llm.LLMClient, cfg *config.Config) *ReviewerAgent { ... }

   var ErrReviewParse = errors.New("reviewer response is not valid JSON") // sentinel

   func (a *ReviewerAgent) Review(ctx context.Context, ex models.ExplainerOutput,
       paper models.Paper, iteration int) (models.ReviewVerdict, error)
   ```
   Note: package `agents` already has a `New` (ExplainerAgent). Since reviewer is a **separate file
   in the same package**, use a distinct constructor name to avoid collision — e.g. `NewReviewer`.
   Verify the existing explainer constructor name and pick a non-colliding one.

2. **`Review` body**:
   - Build request: `SystemPrompt: reviewerSystemPrompt`, `UserPrompt: a.buildReviewPrompt(ex, paper, iteration)`,
     `MaxTokens: 2000`, `Temperature: 0.1`, **`DocumentText` empty** (evaluate `ex.Content` only — T3).
   - `resp, err := a.llm.Complete(ctx, req)`; on err → return `(ReviewVerdict{}, err)` (real LLM error,
     NOT the sentinel).
   - Strip fences: `strings.TrimSpace`, trim leading ` ```json ` / ` ``` `, trim trailing ` ``` `.
   - Unmarshal into `struct{ Pass bool; Score float32; Feedback map[string]*string }`. On unmarshal
     error → `return ReviewVerdict{}, fmt.Errorf("%w: %v", ErrReviewParse, err)`.
   - Filter nil/empty feedback into `map[string]string`.
   - Return verdict: `Pass: raw.Pass` (verbatim — decision 1), `Score`, `Feedback`, `Iteration: iteration`,
     `TokensUsed: resp.InputTokens + resp.OutputTokens`, `CreatedAt: time.Now()`, `PaperID: paper.ID`.

3. **`reviewer-prompt.go`** — `const reviewerSystemPrompt = ...`:
   - Keep PRD's 6 criteria (author intent, analogy quality, math handling, glossary priority,
     practitioner tone, follow-up relevance), the JSON-only output contract, and the feedback
     specificity rules (BAD/GOOD examples).
   - **REMOVE** the contradictory `PASS THRESHOLD: score >= 0.80 AND ...` block (decision 1). Replace
     with a plain instruction: set `pass` to your holistic judgement of whether a practitioner would
     be well-served; use `score` to communicate confidence.
   - `buildReviewPrompt(ex, paper, iteration)` → `fmt.Sprintf("Review iteration: %d\n\nPaper: %s (arXiv: %s)\n\nExplainer to review:\n\n%s", iteration, paper.Title, paper.ID, ex.Content)`.

4. **`revision-note.go`**:
   - `formatRevisionNote(v models.ReviewVerdict) string` — header `REVISION REQUIRED (Review pass %d, score: %.2f)`, then `### {displayName}\n{feedback}\n` per section, then a trailing "keep unchanged sections as-is" line. Deterministic ordering: iterate a fixed section-key slice, emit only keys present in `v.Feedback` (map iteration order is random in Go — do NOT range the map directly).
   - `sectionDisplayName(key string) string` — map snake_case keys (`problem_statement`, `core_idea`,
     `methodology`, `key_findings`, `limitations`, `why_it_matters`, `analogies`, `glossary`,
     `follow_up_papers`) → title case; fallback to the raw key.

## Todo List
- [x] `reviewer.go` — struct, `NewReviewer`, `Review`, `ErrReviewParse` sentinel
- [x] `reviewer-prompt.go` — system prompt (threshold removed) + `buildReviewPrompt`
- [x] `revision-note.go` — `formatRevisionNote` (fixed-order) + `sectionDisplayName`
- [x] Confirm constructor name doesn't collide with existing `agents.New`
- [x] `go build ./...` clean

## Success Criteria
- Valid JSON (fenced and unfenced) parses to a correct verdict; nil feedback entries dropped.
- Malformed JSON returns an error that `errors.Is(err, ErrReviewParse)` matches.
- `Review` never gates on score — `Pass` equals the model's `pass` field exactly.
- `formatRevisionNote` output is stable across runs (fixed section ordering).

## Risk Assessment
- **Medium.** JSON robustness is the crux. Cover fenced/unfenced/preamble/malformed in tests (Phase 05).
- Map-range nondeterminism in `formatRevisionNote` would make tests flaky — use a fixed key slice.

## Security
- Reviewer input is the already-generated explainer text — no new external data. JSON parsed into a
  strict struct (no arbitrary execution).
