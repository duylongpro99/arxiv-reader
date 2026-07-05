# Phase 03 — Orchestrator Loop, Reviewer Interface & Vault Frontmatter

**Priority:** Critical · **Status:** complete · **Depends on:** Phase 01, 02

The integration core. Replaces the linear generate→write span of `runPipeline` with the bounded
loop, adds the `Reviewer` interface + wiring, and makes the vault frontmatter reflect the verdict.

## Context Links
- PRD: `docs/phase5/prd.md` (§2.3 Orchestrator loop, §4 Data Flow, F3, F6)
- Files: `backend/internal/orchestrator/orchestrator.go`, `orchestrator-pipeline.go`,
  `backend/internal/tools/vaultwriter.go`, `vaultwriter-frontmatter.go`, `backend/cmd/.../main.go`

## Requirements
- New `Reviewer` interface (mirrors `Explainer`/`VaultWriter`), new `reviewer` field on `Orchestrator`,
  wired in the constructor and `main`.
- Bounded loop: always terminates, always writes, honours `max=0` (disabled) and parse-failure stop.
- `WriteToVault` accepts a verdict; frontmatter reflects real review outcome.

## Related Code Files

**Modify:**
- `backend/internal/orchestrator/orchestrator.go` — `Reviewer` interface, struct field, constructor arg
- `backend/internal/orchestrator/orchestrator-pipeline.go` — the loop in `runPipeline`
- `backend/internal/tools/vaultwriter.go` — `WriteToVault` signature (+verdict param)
- `backend/internal/tools/vaultwriter-frontmatter.go` — verdict-driven frontmatter
- `backend/cmd/<server>/main.go` (or wherever Orchestrator is constructed) — build & inject `ReviewerAgent`

## Implementation Steps

1. **`Reviewer` interface** (orchestrator.go, next to `Explainer`/`VaultWriter`):
   ```go
   type Reviewer interface {
       Review(ctx context.Context, ex models.ExplainerOutput, paper models.Paper, iteration int) (models.ReviewVerdict, error)
   }
   ```
   Add `reviewer Reviewer` to the `Orchestrator` struct. Add it as a constructor parameter (keep param
   order consistent with existing `explainer`/`vault`). Update `main` to build the `ReviewerAgent`
   (same `llm.LLMClient` + `cfg` as the explainer) and pass it in.

2. **`VaultWriter` interface** — update its `WriteToVault` method signature to add
   `verdict *models.ReviewVerdict` as the final param (both interface and concrete tool).

3. **`runPipeline` loop** — replace the current generate→write span (keep the `defer recover()`,
   the `FetchMarkdown` + `RecoverToSelection`/`Fail` handling, the nil-paper guard, and the
   empty-content guard exactly as-is). New span:
   ```go
   maxIter := o.cfg.Agent.MaxReviewIterations
   var lastEx models.ExplainerOutput
   revisionNote := ""

   for iteration := 1; ; iteration++ {
       s.SetIteration(iteration)
       if iteration == 1 {
           s.SetStage(models.StageGenerating)
       } else {
           s.SetStage(models.StageRevising)
       }
       ex, err := o.explainer.Generate(ctx, agents.ExplainerInput{
           MarkdownText: md, PaperMeta: *paper, RevisionNote: revisionNote,
       })
       if err != nil { s.Fail(describeGenErr(err), true); return }
       if strings.TrimSpace(ex.Content) == "" { s.Fail("The AI returned an empty explainer...", true); return }
       ex.Iteration = iteration               // explainer hardcodes 1 today; stamp real value
       lastEx = ex
       s.SetExplainer(&ex)
       s.AddTokens(ex.InputTokens + ex.OutputTokens)

       if maxIter == 0 { break }              // reviewer disabled → Phase-4 path, verdict stays nil

       s.SetStage(models.StageReviewing)
       verdict, err := o.reviewer.Review(ctx, ex, *paper, iteration)
       if errors.Is(err, agents.ErrReviewParse) {
           // decision 2: stop, save current, flag failed — no blind regen
           v := models.ReviewVerdict{PaperID: paper.ID, Pass: false, Score: 0, Iteration: iteration, CreatedAt: time.Now()}
           s.SetVerdict(&v)
           slog.Warn("reviewer json parse failed; stopping loop", "session_id", s.SessionID, "iteration", iteration)
           break
       }
       if err != nil { s.Fail(describeReviewErr(err), true); return }  // real LLM/network error
       s.SetVerdict(&verdict)
       s.AddTokens(verdict.TokensUsed)
       slog.Info("review complete", "session_id", s.SessionID, "iteration", iteration, "score", verdict.Score, "pass", verdict.Pass)

       if verdict.Pass { break }
       if iteration >= maxIter {
           slog.Warn("max review iterations reached without approval", "session_id", s.SessionID, "final_score", verdict.Score)
           break
       }
       revisionNote = formatRevisionNote(verdict)
   }

   s.SetStage(models.StageWriting)
   path, err := o.vault.WriteToVault(ctx, lastEx, *paper, s.Verdict())
   if err != nil { s.Fail(vaultErrMsg(err), vaultRecoverable(err)); return }
   s.SetVaultFile(path)
   s.SetStage(models.StageComplete)
   ```
   - Reuse existing helpers (`describeGenErr`, `vaultErrMsg`, `vaultRecoverable`, empty-content message)
     verbatim. Add a small `describeReviewErr` (or reuse a generic describer) for reviewer LLM errors.
   - Add `errors`, `time`, `strings`, `slog` imports if not already present.

4. **`vaultwriter-frontmatter.go`** — `buildFrontmatter` gains the verdict param. Replace the two
   hardcoded lines:
   - `verdict == nil` (reviewer disabled): `review_iterations: 0`, `review_passed: true`, **omit** `review_score`.
   - `verdict != nil`: `review_iterations: <verdict.Iteration>`, `review_passed: <verdict.Pass>`,
     `review_score: <verdict.Score, %.2f>`.
   Keep the `tags:` line and all existing keys.

## Todo List
- [x] `Reviewer` interface + `reviewer` field + constructor param
- [x] Build & inject `ReviewerAgent` in `main`
- [x] `WriteToVault` + `VaultWriter` interface signature (+verdict)
- [x] Rewrite `runPipeline` generate→write span with the loop
- [x] `ex.Iteration = iteration` stamp (explainer hardcodes 1)
- [x] Parse-error branch (stop + flag), reviewer LLM-error branch (fail)
- [x] `buildFrontmatter` verdict-driven (nil vs set)
- [x] `describeReviewErr` helper
- [x] `go build ./...` clean

## Success Criteria
- `max=0` → one generation, no review, `verdict==nil`, note saved with `review_iterations: 0`,
  `review_passed: true`, no `review_score`.
- `max=2`, first pass fails → second (revising) generation runs with a revision note, second review
  decides; loop stops on pass OR at iteration 2.
- Reviewer parse error mid-loop → loop stops, current explainer saved, `review_passed: false`, `review_score: 0.00`.
- Reviewer network error → session `failed`, recoverable=true.
- Token total accumulates across all generations + reviews.

## Risk Assessment
- **Medium-High.** Highest-coupling change. Preserve every existing guard/error path; only the
  generate→write span changes. Verify import additions compile.
- Off-by-one on `iteration >= maxIter`: with `max=1`, generate→review once, never revise (correct).
  With `max=2`, at most one revision. Cover both in Phase 05 tests.

## Security
- No new external I/O. Reviewer failure never surfaces raw LLM output to the user.
