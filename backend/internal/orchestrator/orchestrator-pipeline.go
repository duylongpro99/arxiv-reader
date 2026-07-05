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
	"github.com/maritime-ds/arxiv-reader/internal/models"
	"github.com/maritime-ds/arxiv-reader/internal/tools"
)

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
			slog.Error("discovery panic", "session_id", session.SessionID, "panic", fmt.Sprintf("%v", r))
		}
	}()

	papers, err := o.disco.FetchPapers(ctx)
	if err != nil {
		msg, recoverable := describeError(err)
		session.Fail(msg, recoverable)
		o.logFailure(session, err)
		return
	}

	unprocessed, err := o.logCheck.FilterUnprocessed(papers)
	if err != nil {
		msg, recoverable := describeError(err)
		session.Fail(msg, recoverable)
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

// runPipeline fetches the chosen paper's Markdown and records the outcome. A 404
// is a *recoverable re-pick* (back to selection, candidates preserved) rather
// than a hard failure; any other error fails the session recoverably. On success
// it stores the Markdown and stops — Phase 4's ExplainerAgent resumes here.
func (o *Orchestrator) runPipeline(ctx context.Context, s *models.PipelineSession, paperID string) {
	// A panic on this fully-detached goroutine would crash the whole process;
	// contain it to this one session (mirrors runDiscovery).
	defer func() {
		if r := recover(); r != nil {
			s.Fail("Processing crashed unexpectedly. Please try again.", true)
			slog.Error("pipeline panic", "session_id", s.SessionID, "panic", fmt.Sprintf("%v", r))
		}
	}()

	md, err := o.content.FetchMarkdown(ctx, paperID)
	if err != nil {
		if errors.Is(err, tools.ErrPaperHTMLNotFound) {
			s.RecoverToSelection("Paper HTML not available on arXiv. Please select another paper.")
			slog.Warn("paper html not found", "session_id", s.SessionID, "paper_id", paperID)
			return
		}
		msg, recoverable := describeError(err)
		s.Fail(msg, recoverable)
		slog.Error("pipeline failed", "session_id", s.SessionID, "paper_id", paperID, "error", err.Error())
		return
	}

	s.SetMarkdown(md)
	slog.Info("markdown stored", "session_id", s.SessionID, "paper_id", paperID, "markdown_bytes", len(md))

	// --- Phase 4: generate the explainer, write it to the vault, complete. ---

	// SelectedPaper carries the full metadata (title/authors/published) the
	// ExplainerAgent and VaultWriter need. HandleProcess always sets it before
	// spawning this goroutine; guard nil defensively rather than risk a panic.
	paper := s.SelectedPaper()
	if paper == nil {
		s.Fail("Internal error: no paper selected. Please try again.", true)
		slog.Error("pipeline missing selected paper", "session_id", s.SessionID, "paper_id", paperID)
		return
	}

	// --- Phase 5: bounded critic-generator loop. ---
	//
	// The loop always terminates (via one of the explicit breaks), always writes
	// exactly one note (the last explainer), and honours max=0 (reviewer disabled,
	// Phase-4 path). A revision note produced by a failing review is threaded back
	// into the next generation via the existing ExplainerInput.RevisionNote seam.
	maxIter := o.cfg.Agent.MaxReviewIterations
	var lastEx models.ExplainerOutput
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
			s.Fail(describeGenErr(err), true)
			slog.Error("explainer generation failed", "session_id", s.SessionID, "paper_id", paperID, "iteration", iteration, "error", err.Error())
			return
		}
		// A degenerate empty response (no error) would otherwise be written as a
		// frontmatter-only note and mark the paper processed. Treat it as a
		// recoverable generation failure so the paper re-surfaces for a retry.
		if strings.TrimSpace(ex.Content) == "" {
			s.Fail("The AI returned an empty explainer. Please try again.", true)
			slog.Error("explainer generation empty", "session_id", s.SessionID, "paper_id", paperID, "iteration", iteration)
			return
		}
		ex.Iteration = iteration // explainer hardcodes 1; stamp the real loop value
		lastEx = ex
		s.SetExplainer(&ex)
		s.AddTokens(ex.InputTokens + ex.OutputTokens)

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
			slog.Warn("reviewer json parse failed; stopping loop", "session_id", s.SessionID, "iteration", iteration)
			break
		}
		if err != nil {
			// Real LLM/network error — recoverable, fail the session (no write).
			s.Fail(describeReviewErr(err), true)
			slog.Error("reviewer failed", "session_id", s.SessionID, "paper_id", paperID, "iteration", iteration, "error", err.Error())
			return
		}
		s.SetVerdict(&verdict)
		s.AddTokens(verdict.TokensUsed)

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

	s.SetStage(models.StageWriting)
	path, err := o.vault.WriteToVault(ctx, lastEx, *paper, s.Verdict())
	if err != nil {
		// Permission/disk failures won't fix themselves on retry; others might.
		s.Fail(vaultErrMsg(err), vaultRecoverable(err))
		slog.Error("vault write failed", "session_id", s.SessionID, "paper_id", paperID, "error", err.Error())
		return
	}
	s.SetVaultFile(path)
	s.SetStage(models.StageComplete)

	slog.Info("pipeline complete",
		"session_id", s.SessionID, "paper_id", paperID,
		"total_duration_ms", time.Since(s.StartedAt()).Milliseconds(),
	)
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

func (o *Orchestrator) logFailure(session *models.PipelineSession, err error) {
	slog.Error("pipeline failed",
		"session_id", session.SessionID,
		"stage", string(models.StageFailed),
		"error", err.Error(),
		"duration_ms", time.Since(session.StartedAt()).Milliseconds(),
	)
}
