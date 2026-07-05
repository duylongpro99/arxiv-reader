// Package orchestrator sequences the discovery-pipeline tools and owns the
// in-memory session state. It is the conductor: it coordinates and tracks
// state, but contains no arXiv/dedup business logic of its own.
package orchestrator

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"

	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/maritime-ds/arxiv-reader/internal/llm"
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

// PaperContent is the narrow contract for turning a chosen paper into Markdown.
// Consumer-side interface so the orchestrator stays testable with a fake tool.
type PaperContent interface {
	FetchMarkdown(ctx context.Context, arxivID string) (string, error)
}

// Orchestrator holds the session store and the tools it sequences.
type Orchestrator struct {
	sessions sync.Map // sessionID -> *models.PipelineSession
	cfg      *config.Config
	disco    PaperFetcher
	logCheck Unprocessor
	content  PaperContent  // Phase 3: HTML → Markdown extraction
	llm      llm.LLMClient // constructed now, invoked in Phase 4
}

// New wires the orchestrator with the real tools built from config. It can fail:
// NewLLMClient rejects an unknown provider, and a misconfigured provider should
// stop the server at startup (matching the config fail-fast philosophy).
func New(cfg *config.Config) (*Orchestrator, error) {
	client, err := llm.NewLLMClient(&cfg.LLM)
	if err != nil {
		return nil, err
	}
	return &Orchestrator{
		cfg:      cfg,
		disco:    tools.NewDiscoveryTool(&cfg.Agent),
		logCheck: tools.NewLogCheckTool(&cfg.Paths),
		content:  tools.NewPaperContentTool(&cfg.Agent),
		llm:      client,
	}, nil
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

type ProcessRequest struct {
	SessionID string `json:"session_id"`
	PaperID   string `json:"paper_id"`
}

type ProcessResponse struct {
	SessionID string `json:"session_id"`
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

// HandleProcess records the user's paper choice and kicks off extraction. It
// validates the session is awaiting selection and the paper_id is one the server
// itself surfaced (never an arbitrary client-supplied fetch target), then flips
// to extracting and detaches the pipeline goroutine — returning {session_id}
// immediately so the client keeps polling.
func (o *Orchestrator) HandleProcess(w http.ResponseWriter, r *http.Request) {
	var req ProcessRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	v, ok := o.sessions.Load(req.SessionID)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	s := v.(*models.PipelineSession)
	snap := s.Snapshot()
	if snap.Stage != models.StageSelection {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session is not awaiting selection"})
		return
	}

	// Match paper_id against the session's own candidates (Snapshot is lock-free).
	// Bind the match into a local copy so the detached goroutine reads a stable
	// *Paper rather than aliasing the loop variable / backing slice.
	var selected *models.Paper
	for i := range snap.Candidates {
		if snap.Candidates[i].ID == req.PaperID {
			p := snap.Candidates[i]
			selected = &p
			break
		}
	}
	if selected == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "paper_id is not among the candidates"})
		return
	}

	s.SetSelectedPaper(selected)
	s.SetStage(models.StageExtracting)
	slog.Info("process requested", "session_id", s.SessionID, "paper_id", selected.ID)
	writeJSON(w, http.StatusOK, ProcessResponse{SessionID: s.SessionID})

	// Detach from the request context (cancelled once this handler returns) so
	// extraction survives; pass the ID explicitly to avoid a cross-goroutine
	// read of the private selectedPaper field.
	go o.runPipeline(context.WithoutCancel(r.Context()), s, selected.ID)
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
