// Package orchestrator sequences the discovery-pipeline tools and owns the
// in-memory session state. It is the conductor: it coordinates and tracks
// state, but contains no arXiv/dedup business logic of its own.
package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/maritime-ds/arxiv-reader/internal/models"
	"github.com/maritime-ds/arxiv-reader/internal/tools"
)

// PaperFetcher and Unprocessor are the narrow tool contracts the orchestrator
// depends on. Defining them here (consumer-side) keeps the orchestrator
// testable with fakes and decoupled from the concrete tools.
type PaperFetcher interface {
	FetchPapers(ctx context.Context) ([]models.Paper, error)
}

type Unprocessor interface {
	FilterUnprocessed(papers []models.Paper) ([]models.Paper, error)
}

// Orchestrator holds the session store and the tools it sequences.
type Orchestrator struct {
	sessions sync.Map // sessionID -> *models.PipelineSession
	cfg      *config.Config
	disco    PaperFetcher
	logCheck Unprocessor
}

// New wires the orchestrator with the real tools built from config.
func New(cfg *config.Config) *Orchestrator {
	return &Orchestrator{
		cfg:      cfg,
		disco:    tools.NewDiscoveryTool(&cfg.Agent),
		logCheck: tools.NewLogCheckTool(&cfg.Paths),
	}
}

// --- response DTOs (the frontend-facing contract) ---

type DiscoverResponse struct {
	SessionID string `json:"session_id"`
}

type StatusResponse struct {
	Stage       models.PipelineStage `json:"stage"`
	Candidates  []models.Paper       `json:"candidates,omitempty"`
	Notice      string               `json:"notice,omitempty"`
	Error       string               `json:"error,omitempty"`
	Recoverable bool                 `json:"recoverable,omitempty"`
}

// HandleDiscover creates a session, kicks off discovery in the background, and
// returns the session ID immediately. The client learns the outcome by polling
// HandleStatus. Discovery is async because later phases (LLM calls) are slow;
// establishing the contract now keeps it stable across phases.
func (o *Orchestrator) HandleDiscover(w http.ResponseWriter, r *http.Request) {
	session := o.newSession()
	o.sessions.Store(session.SessionID, session)

	slog.Info("discovery started", "session_id", session.SessionID)

	// Detach from the request context: it is cancelled the moment this handler
	// returns (which is immediately), and using it would abort discovery
	// instantly. The HTTP client's own timeout bounds the background work.
	go o.runDiscovery(context.WithoutCancel(r.Context()), session)

	writeJSON(w, http.StatusOK, DiscoverResponse{SessionID: session.SessionID})
}

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

// HandleStatus returns the current session snapshot, or 404 for an unknown ID.
func (o *Orchestrator) HandleStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionId")
	v, ok := o.sessions.Load(id)
	if !ok {
		// Return JSON (not text/plain) so the polling client can always parse
		// the body. The realistic trigger is a server restart wiping the
		// in-memory store while the frontend still holds an old session_id.
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	snap := v.(*models.PipelineSession).Snapshot()
	writeJSON(w, http.StatusOK, StatusResponse{
		Stage:       snap.Stage,
		Candidates:  snap.Candidates,
		Notice:      snap.Notice,
		Error:       snap.Error,
		Recoverable: snap.Recoverable,
	})
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
// retry might help. Corrupt-log is the only non-recoverable failure — it needs
// a manual fix.
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
	default:
		return "Discovery failed unexpectedly. Please try again.", true
	}
}

func (o *Orchestrator) logFailure(session *models.PipelineSession, err error) {
	slog.Error("discovery failed",
		"session_id", session.SessionID,
		"stage", string(models.StageFailed),
		"error", err.Error(),
		"duration_ms", time.Since(session.StartedAt()).Milliseconds(),
	)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
