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

	"github.com/maritime-ds/arxiv-reader/internal/agents"
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

// Explainer and VaultWriter are the Phase 4 consumer contracts, defined here so
// the orchestrator stays testable with fakes and decoupled from the concrete
// agent/tool implementations.
type Explainer interface {
	Generate(ctx context.Context, in agents.ExplainerInput) (models.ExplainerOutput, error)
}

type VaultWriter interface {
	WriteToVault(ctx context.Context, ex models.ExplainerOutput, p models.Paper) (string, error)
}

// Orchestrator holds the session store and the tools it sequences.
type Orchestrator struct {
	sessions  sync.Map // sessionID -> *models.PipelineSession
	cfg       *config.Config
	disco     PaperFetcher
	logCheck  Unprocessor
	content   PaperContent // Phase 3: HTML → Markdown extraction
	explainer Explainer    // Phase 4: LLM re-teaching generation
	vault     VaultWriter  // Phase 4: atomic Obsidian vault write
}

// New wires the orchestrator with the real tools built from config. It can fail:
// NewLLMClient rejects an unknown provider, and a misconfigured provider should
// stop the server at startup (matching the config fail-fast philosophy).
//
// The concrete *LogCheckTool is built once and shared: the orchestrator holds it
// as the read-only Unprocessor, while the VaultWriter needs its MarkAsProcessed
// (write) method — which is not on that interface. The single LLM client is
// shared with the ExplainerAgent (stateless / concurrency-safe).
func New(cfg *config.Config) (*Orchestrator, error) {
	client, err := llm.NewLLMClient(&cfg.LLM)
	if err != nil {
		return nil, err
	}
	logCheck := tools.NewLogCheckTool(&cfg.Paths)
	return &Orchestrator{
		cfg:       cfg,
		disco:     tools.NewDiscoveryTool(&cfg.Agent),
		logCheck:  logCheck,
		content:   tools.NewPaperContentTool(&cfg.Agent),
		explainer: agents.New(client, cfg),
		vault:     tools.NewVaultWriterTool(cfg, logCheck),
	}, nil
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

// HandleResult returns the finished explainer for a session, or 404 until the
// pipeline is complete. It reads the server-only fields (explainer/vaultFile/
// tokens) via their dedicated accessors — these are deliberately NOT part of
// Snapshot()/‌/status, so the large Content never rides the status poll.
func (o *Orchestrator) HandleResult(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionId")
	v, ok := o.sessions.Load(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	s := v.(*models.PipelineSession)
	if s.Snapshot().Stage != models.StageComplete {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "result not ready"})
		return
	}
	ex := s.Explainer()
	if ex == nil { // defensive: complete implies explainer set, but never nil-panic
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "result not ready"})
		return
	}
	writeJSON(w, http.StatusOK, ResultResponse{
		Content:    ex.Content,
		VaultFile:  s.VaultFile(),
		TokensUsed: s.TokensUsed(),
	})
}

