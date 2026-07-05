package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

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

	s.SetMarkdown(md) // Phase 4 seam: ExplainerAgent picks up from here.
	slog.Info("markdown stored", "session_id", s.SessionID, "paper_id", paperID, "markdown_bytes", len(md))
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

// describeError maps a pipeline error to a human-readable message and whether a
// retry might help. Shared by both background goroutines, so the default message
// is stage-neutral. Corrupt-log is the only non-recoverable failure — it needs a
// manual fix.
func describeError(err error) (message string, recoverable bool) {
	switch {
	case errors.Is(err, tools.ErrArxivRateLimit):
		return "arXiv is rate limiting requests. Please try again in a minute.", true
	case errors.Is(err, tools.ErrArxivUnavailable):
		return "arXiv is currently unavailable. Please try again.", true
	case errors.Is(err, tools.ErrArxivParse):
		return "arXiv returned an unexpected response. Please try again.", true
	case errors.Is(err, tools.ErrLogCorrupted):
		return "The processed-log file is corrupted and needs manual inspection.", false
	case errors.Is(err, tools.ErrPaperHTMLTimeout):
		return "Fetching the paper's HTML timed out. Please try again.", true
	case errors.Is(err, tools.ErrPaperHTMLFailed):
		return "Could not fetch or convert the paper's HTML. Please try again.", true
	default:
		return "The request failed unexpectedly. Please try again.", true
	}
}

func (o *Orchestrator) logFailure(session *models.PipelineSession, err error) {
	slog.Error("pipeline failed",
		"session_id", session.SessionID,
		"stage", string(models.StageFailed),
		"error", err.Error(),
		"duration_ms", time.Since(session.StartedAt()).Milliseconds(),
	)
}
