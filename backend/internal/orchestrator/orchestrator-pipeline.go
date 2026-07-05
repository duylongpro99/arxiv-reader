package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/agents"
	"github.com/maritime-ds/arxiv-reader/internal/llm"
	"github.com/maritime-ds/arxiv-reader/internal/models"
	"github.com/maritime-ds/arxiv-reader/internal/tools"
)

// systemPromptTokenAllowance is a rough, conservative allowance (in tokens) for
// the explainer's system prompt when estimating whether a request fits the
// model's context window. It only needs to be in the right ballpark — the check
// is advisory (F4).
const systemPromptTokenAllowance = 900

// This file holds the detached background goroutines (discovery + extraction)
// and their shared helpers, kept separate from the HTTP surface in orchestrator.go.

// runDiscovery executes the pipeline and records the result on the session.
func (o *Orchestrator) runDiscovery(ctx context.Context, session *models.PipelineSession) {
	// This goroutine is fully detached from the request lifecycle, so an
	// unrecovered panic here would take down the entire process (and every
	// other session). Contain it: fail this one session, keep the server up.
	defer func() {
		if r := recover(); r != nil {
			session.Fail("Discovery crashed unexpectedly. Please try again.", true)
			session.SetErrorAction(actionRetry)
			slog.Error("discovery panic", "session_id", session.SessionID, "panic", fmt.Sprintf("%v", r))
		}
	}()

	// Surface arXiv retry attempts as a progress counter (F5). On success we reset
	// it to 0 below so the "Connecting to arXiv (retry n/3)…" label disappears.
	papers, err := o.disco.FetchPapers(ctx, func(attempt int) {
		session.SetArxivRetryCount(attempt)
	})
	if err != nil {
		msg, recoverable, action := describeError(err)
		session.Fail(msg, recoverable)
		session.SetErrorAction(action)
		o.logFailure(session, err)
		return
	}
	session.SetArxivRetryCount(0) // fetch succeeded — clear any retry label

	unprocessed, err := o.logCheck.FilterUnprocessed(papers)
	if err != nil {
		msg, recoverable, action := describeError(err)
		session.Fail(msg, recoverable)
		session.SetErrorAction(action)
		o.logFailure(session, err)
		return
	}

	// Cap to the display limit; note when we have fewer than requested.
	limit := o.cfg.Agent.DisplayLimit
	candidates := unprocessed
	if len(candidates) > limit {
		candidates = candidates[:limit]
	}
	notice := ""
	if len(candidates) < limit {
		notice = fmt.Sprintf("Only %d new paper(s) found", len(candidates))
	}

	session.Complete(candidates, notice)
	slog.Info("discovery complete",
		"session_id", session.SessionID,
		"stage", string(models.StageSelection),
		"returning", len(candidates),
		"duration_ms", time.Since(session.StartedAt()).Milliseconds(),
	)
}

// runPipeline fetches the chosen paper's Markdown, generates + reviews the
// explainer, and writes it to the vault. A 404 is a *recoverable re-pick* (back
// to selection, candidates preserved); any other error fails the session.
//
// Phase 6 makes this RESUMABLE: each of the three heavy segments is guarded by
// its own cached output, so a retry re-runs only what actually failed —
//   - extraction   skipped when Markdown() is already cached
//   - generate+review skipped when Explainer() is already cached (one unit — we
//     never resume mid-loop, keeping the Phase 5 loop logic untouched)
//   - vault write   always runs; it is idempotent on retry because a prior
//     failure wrote no file and never marked the paper processed
//
// On a fresh run all caches are empty → the full pipeline runs.
func (o *Orchestrator) runPipeline(ctx context.Context, s *models.PipelineSession, paperID string) {
	// A panic on this fully-detached goroutine would crash the whole process;
	// contain it to this one session (mirrors runDiscovery).
	defer func() {
		if r := recover(); r != nil {
			s.Fail("Processing crashed unexpectedly. Please try again.", true)
			s.SetErrorAction(actionRetry)
			slog.Error("pipeline panic", "session_id", s.SessionID, "panic", fmt.Sprintf("%v", r))
		}
	}()

	// --- Segment 1: extraction (skipped on resume when markdown is cached). ---
	if s.Markdown() == "" {
		md, err := o.content.FetchMarkdown(ctx, paperID)
		if err != nil {
			if errors.Is(err, tools.ErrPaperHTMLNotFound) {
				s.RecoverToSelection("Paper HTML not available on arXiv. Please select another paper.")
				slog.Warn("paper html not found", "session_id", s.SessionID, "paper_id", paperID)
				return
			}
			msg, recoverable, action := describeError(err)
			s.Fail(msg, recoverable)
			s.SetErrorAction(action)
			o.logFailure(s, err, "paper_id", paperID)
			return
		}
		s.SetMarkdown(md)
		slog.Info("markdown stored", "session_id", s.SessionID, "paper_id", paperID, "markdown_bytes", len(md))
	}
	md := s.Markdown()

	// --- Phase 4/5: generate + review the explainer, write it, complete. ---

	// SelectedPaper carries the full metadata (title/authors/published) the
	// ExplainerAgent and VaultWriter need. HandleProcess always sets it before
	// spawning this goroutine; guard nil defensively rather than risk a panic.
	paper := s.SelectedPaper()
	if paper == nil {
		s.Fail("Internal error: no paper selected. Please try again.", true)
		s.SetErrorAction(actionRetry)
		o.logFailure(s, errors.New("no paper selected"), "paper_id", paperID)
		return
	}

	// --- Segment 2: bounded critic-generator loop (skipped on resume when the
	// explainer is cached — the loop is ONE resume unit; never resume mid-loop).
	//
	// The loop always terminates (via one of the explicit breaks), always stores
	// exactly one explainer (the last), and honours max=0 (reviewer disabled,
	// Phase-4 path). A revision note produced by a failing review is threaded back
	// into the next generation via the existing ExplainerInput.RevisionNote seam.
	// Re-run the loop when there is no cached explainer OR when the reviewer is
	// enabled but no verdict was recorded — the latter means a prior run failed
	// mid-loop (e.g. the review LLM call errored after a successful generation),
	// so a resume must re-run the WHOLE loop rather than write an unreviewed note
	// mislabeled review_passed:true. Re-generation cost is real and accounted for.
	// (A legitimately nil verdict — reviewer disabled, maxIter==0 — does NOT
	// trip this, so a vault-only retry still skips the loop at zero LLM cost.)
	reviewerEnabled := o.cfg.Agent.MaxReviewIterations > 0
	if s.Explainer() == nil || (reviewerEnabled && s.Verdict() == nil) {
		o.checkContextWindow(s, md) // F4: non-blocking over-limit advisory
		if !o.runGenerateReview(ctx, s, md, paper, paperID) {
			return // segment failed; error state already set on the session
		}
	}

	// --- Segment 3: vault write (ALWAYS runs; idempotent on retry). Read the
	// explainer from the session so a resumed run that skipped the loop still has
	// the cached note to write.
	ex := s.Explainer()
	if ex == nil { // defensive: reachable only if the loop stored nothing
		s.Fail("Internal error: no explainer to write. Please try again.", true)
		s.SetErrorAction(actionRetry)
		o.logFailure(s, errors.New("no explainer to write"), "paper_id", paperID)
		return
	}

	s.SetStage(models.StageWriting)
	path, err := o.vault.WriteToVault(ctx, *ex, *paper, s.Verdict())
	if err != nil {
		// Permission/disk failures won't fix themselves on retry; others might.
		msg, action := vaultErrMsg(err)
		s.Fail(msg, vaultRecoverable(err))
		s.SetErrorAction(action)
		o.logFailure(s, err, "paper_id", paperID)
		return
	}
	s.SetVaultFile(path)
	s.SetStage(models.StageComplete)

	// A full-run summary: token split, estimated cost, and the review outcome, so
	// a completed run is auditable from this single line (F6 event table).
	// review_iterations is 0 when the reviewer was disabled (verdict stays nil).
	cost, costKnown := llm.EstimateCost(o.cfg.LLM.Model, s.InputTokens(), s.OutputTokens())
	reviewIterations, reviewPassed := 0, false
	if v := s.Verdict(); v != nil {
		reviewIterations, reviewPassed = v.Iteration, v.Pass
	}
	slog.Info("pipeline complete",
		"session_id", s.SessionID, "paper_id", paperID,
		"input_tokens", s.InputTokens(), "output_tokens", s.OutputTokens(),
		"total_tokens", s.TokensUsed(),
		"estimated_cost_usd", cost, "cost_known", costKnown,
		"review_iterations", reviewIterations, "review_passed", reviewPassed,
		"total_duration_ms", time.Since(s.StartedAt()).Milliseconds(),
	)
}

// checkContextWindow attaches a non-blocking ContextWarning to the session when
// the estimated prompt size (extracted Markdown + system prompt + output budget)
// exceeds the configured model's context window. It NEVER aborts the pipeline —
// the estimate is a len/4 heuristic and a genuine over-limit is caught later by
// the provider's ErrLLMBadRequest. Unknown models (absent from the limits table)
// are skipped silently.
func (o *Orchestrator) checkContextWindow(s *models.PipelineSession, md string) {
	limit, known := llm.ModelContextLimits[o.cfg.LLM.Model]
	if !known {
		return
	}
	est := llm.EstimateTokens(md)
	total := est + systemPromptTokenAllowance + o.cfg.LLM.MaxTokens
	if total > limit {
		s.SetContextWarning(&models.ContextWarning{
			EstimatedTokens: est,
			ModelLimit:      limit,
			Model:           o.cfg.LLM.Model,
			Suggestion:      "Consider switching to Gemini (gemini-2.0-flash) for a larger context window.",
		})
	}
}

// runGenerateReview runs the Phase 5 bounded critic-generator loop as ONE
// resume unit: it stores the accepted (or last) explainer on the session and
// returns true, or sets the session's error state and returns false. Extracted
// from runPipeline so the resume guard (Explainer()==nil) wraps a single call
// and the vault-write segment stays linear.
func (o *Orchestrator) runGenerateReview(ctx context.Context, s *models.PipelineSession, md string, paper *models.Paper, paperID string) bool {
	maxIter := o.cfg.Agent.MaxReviewIterations
	revisionNote := ""

	for iteration := 1; ; iteration++ {
		s.SetIteration(iteration)
		// First pass is a fresh generation; later passes revise using the note.
		if iteration == 1 {
			s.SetStage(models.StageGenerating)
		} else {
			s.SetStage(models.StageRevising)
		}

		ex, err := o.explainer.Generate(ctx, agents.ExplainerInput{
			MarkdownText: md, PaperMeta: *paper, RevisionNote: revisionNote,
		})
		if err != nil {
			// Generation errors are retryable — no vault file written, log untouched.
			// A bad-request (paper too large / wrong model) won't fix itself on an
			// in-process retry — config is immutable at runtime — so it is NOT
			// recoverable; the fix_config action tells the user to change the model.
			// Transient errors stay recoverable.
			msg, action := describeGenErr(err)
			s.Fail(msg, action != actionFixConfig)
			s.SetErrorAction(action)
			o.logFailure(s, err, "paper_id", paperID, "iteration", iteration)
			return false
		}
		// A degenerate empty response (no error) would otherwise be written as a
		// frontmatter-only note and mark the paper processed. Treat it as a
		// recoverable generation failure so the paper re-surfaces for a retry.
		if strings.TrimSpace(ex.Content) == "" {
			s.Fail("The AI returned an empty explainer. Please try again.", true)
			s.SetErrorAction(actionRetry)
			o.logFailure(s, errors.New("empty explainer response"), "paper_id", paperID, "iteration", iteration)
			return false
		}
		ex.Iteration = iteration // explainer hardcodes 1; stamp the real loop value
		s.SetExplainer(&ex)
		s.AddTokens(ex.InputTokens + ex.OutputTokens)
		s.AddIO(ex.InputTokens, ex.OutputTokens) // split accounting for cost estimation

		if maxIter == 0 {
			break // reviewer disabled → Phase-4 path; verdict stays nil
		}

		s.SetStage(models.StageReviewing)
		verdict, err := o.reviewer.Review(ctx, ex, *paper, iteration)
		if errors.Is(err, agents.ErrReviewParse) {
			// Decision 2: malformed reviewer JSON stops the loop and saves the
			// current explainer flagged as not-passed — no blind, no-guidance regen.
			// The verdict carries {Pass:false, Score:0} plus the tokens the (successful)
			// review call consumed, so token accounting stays accurate.
			s.SetVerdict(&verdict)
			s.AddTokens(verdict.TokensUsed)
			s.AddIO(verdict.InputTokens, verdict.OutputTokens)
			slog.Warn("reviewer json parse failed; stopping loop", "session_id", s.SessionID, "iteration", iteration)
			break
		}
		if err != nil {
			// Real LLM/network error — recoverable, fail the session (no write).
			// Bad-request is non-recoverable (see the generation path); transient
			// review errors stay recoverable.
			msg, action := describeReviewErr(err)
			s.Fail(msg, action != actionFixConfig)
			s.SetErrorAction(action)
			o.logFailure(s, err, "paper_id", paperID, "iteration", iteration)
			return false
		}
		s.SetVerdict(&verdict)
		s.AddTokens(verdict.TokensUsed)
		s.AddIO(verdict.InputTokens, verdict.OutputTokens)

		if verdict.Pass {
			slog.Info("reviewer approved explainer", "session_id", s.SessionID, "iteration", iteration, "score", verdict.Score)
			break
		}
		if iteration >= maxIter {
			slog.Warn("max review iterations reached without approval", "session_id", s.SessionID, "final_score", verdict.Score)
			break
		}
		// Not passed and iterations remain: build the note for the next revision.
		revisionNote = agents.FormatRevisionNote(verdict)
	}
	return true // explainer stored on the session; vault write happens in runPipeline
}

// newSession creates a session with a unique, dependency-free random ID.
func (o *Orchestrator) newSession() *models.PipelineSession {
	return models.NewSession(newSessionID(), time.Now())
}

// newSessionID returns a 32-hex-char (16 random bytes) session ID. crypto/rand
// avoids adding a UUID dependency while giving ample collision resistance for a
// local, single-user tool.
func newSessionID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is effectively impossible; fall back to a
		// time-based ID so the pipeline can still proceed.
		return fmt.Sprintf("sess-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

// logFailure emits the standard ERROR entry for a failed stage. It reads the
// failure metadata the caller just set via Fail + SetErrorAction (stage, action,
// recoverable) so a failed run is fully reconstructable from logs alone (F6
// event table). MUST be called AFTER Fail + SetErrorAction. extra carries any
// stage-specific context (paper_id, iteration) as key/value pairs.
func (o *Orchestrator) logFailure(session *models.PipelineSession, cause error, extra ...any) {
	snap := session.Snapshot()
	args := []any{
		"session_id", session.SessionID,
		"stage", string(session.FailedStage()),
		"action", session.ErrorAction(),
		"recoverable", snap.Recoverable,
		"cause", cause.Error(),
		"duration_ms", time.Since(session.StartedAt()).Milliseconds(),
	}
	args = append(args, extra...)
	slog.Error("pipeline failed", args...)
}
