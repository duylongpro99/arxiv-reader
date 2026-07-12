// Package orchestrator sequences the discovery-pipeline tools and owns the
// in-memory session state. It is the conductor: it coordinates and tracks
// state, but contains no arXiv/dedup business logic of its own.
package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/maritime-ds/arxiv-reader/internal/agents"
	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/maritime-ds/arxiv-reader/internal/llm"
	"github.com/maritime-ds/arxiv-reader/internal/models"
	"github.com/maritime-ds/arxiv-reader/internal/store"
	"github.com/maritime-ds/arxiv-reader/internal/tools"
	"github.com/maritime-ds/arxiv-reader/internal/tracing"
)

// PaperFetcher and Unprocessor are the narrow tool contracts the orchestrator
// depends on. Defining them here (consumer-side) keeps the orchestrator
// testable with fakes and decoupled from the concrete tools.
type PaperFetcher interface {
	// onRetry (nil-safe) fires per transient arXiv retry so the orchestrator can
	// surface a progress counter (F5) without the tool touching the session.
	FetchPapers(ctx context.Context, onRetry func(attempt int)) ([]models.Paper, error)
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
	WriteToVault(ctx context.Context, ex models.ExplainerOutput, p models.Paper, verdict *models.ReviewVerdict) (string, error)
}

// Reviewer is the Phase 5 consumer contract for the critic in the revision loop.
// Defined here (consumer-side) so the orchestrator stays testable with a fake.
type Reviewer interface {
	Review(ctx context.Context, ex models.ExplainerOutput, paper models.Paper, iteration int) (models.ReviewVerdict, error)
}

// Orchestrator holds the session store and the tools it sequences.
type Orchestrator struct {
	sessions  sync.Map // sessionID -> *models.PipelineSession
	cfg       *config.Config
	disco     PaperFetcher
	logCheck  Unprocessor
	content   PaperContent // Phase 3: HTML → Markdown extraction
	explainer Explainer    // Phase 4: LLM re-teaching generation
	reviewer  Reviewer     // Phase 5: independent critic (revision loop)
	vault     VaultWriter  // Phase 4: atomic Obsidian vault write
	// Phase 7 run-timeline tracing. tracer is always non-nil after New (it just
	// skips the DB when unavailable); store backs the history read endpoints
	// (Phase 04) and is nil when Postgres is unreachable. Both are additive —
	// the pipeline never depends on them.
	tracer *tracing.Tracer
	store  RunReader // history reads (Phase 04); nil when Postgres is unavailable
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

	// Open the durable history store — best-effort. An empty/unreachable
	// DATABASE_URL degrades to in-memory-only tracing and is NEVER fatal: store
	// failure must not stop the server (mirrors the config fail-fast philosophy
	// only for genuinely required config, which the DB is not). The error is
	// DSN-free by construction (store.Open never echoes the password).
	var st *store.Store
	var ev tracing.EventWriter
	var rw tracing.RunWriter
	if cfg.Tracing.Enabled {
		opened, serr := store.Open(context.Background(), cfg.DatabaseURL)
		if serr != nil {
			slog.Warn("run history disabled — durable store unavailable (live timeline still works)",
				"reason", serr.Error())
		} else {
			st, ev, rw = opened, opened, opened
		}
	}
	tracer := tracing.New(cfg.Tracing.Enabled, ev, rw,
		cfg.Tracing.FullPayloads, cfg.Tracing.BufferSize, cfg.LLM.APIKey, cfg.DatabaseURL)

	o := &Orchestrator{
		cfg:       cfg,
		disco:     tools.NewDiscoveryTool(&cfg.Agent),
		logCheck:  logCheck,
		content:   tools.NewPaperContentTool(&cfg.Agent),
		explainer: agents.New(client, cfg),
		reviewer:  agents.NewReviewer(client, cfg), // shares the explainer's LLM client
		vault:     tools.NewVaultWriterTool(cfg, logCheck),
		tracer:    tracer,
	}
	// Assign the reader ONLY when the DB opened, so a nil *store.Store never
	// becomes a non-nil RunReader interface (which would panic on first call).
	if st != nil {
		o.store = st
	}
	return o, nil
}

// HandleDiscover creates a session, kicks off discovery in the background, and
// returns the session ID immediately. The client learns the outcome by polling
// HandleStatus. Discovery is async because later phases (LLM calls) are slow;
// establishing the contract now keeps it stable across phases.
func (o *Orchestrator) HandleDiscover(w http.ResponseWriter, r *http.Request) {
	session := o.newSession()
	o.sessions.Store(session.SessionID, session)
	o.rec(session) // create the recorder + run-header row at run start

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

	// Timeline: record WHAT the user selected (the ★ story beat) and stamp the
	// paper onto the run-header row. Both best-effort (nil-safe).
	chosen := tev(tracing.KindSelectionChosen, tracing.StatusSuccess, models.StageSelection,
		fmt.Sprintf("Selected %q (%s)", selected.Title, selected.ID))
	chosen.Summary = map[string]any{"paperId": selected.ID, "title": selected.Title}
	o.rec(s).Emit(chosen)
	o.rec(s).SetPaper(selected.ID, selected.Title)

	writeJSON(w, http.StatusOK, ProcessResponse{SessionID: s.SessionID})

	// Detach from the request context (cancelled once this handler returns) so
	// extraction survives; pass the ID explicitly to avoid a cross-goroutine
	// read of the private selectedPaper field.
	go o.runPipeline(context.WithoutCancel(r.Context()), s, selected.ID)
}

// HandleRetry resumes a failed, recoverable pipeline from the segment that
// failed — WITHOUT discarding the user's paper pick or re-running already-cached
// segments. It validates the session is retryable, clears the transient error
// state, then routes by the failed stage: a discovery failure re-runs discovery;
// any pipeline-stage failure re-enters runPipeline, which skips cached segments
// (markdown/explainer) and re-runs only what's missing.
//
// Safety: the session is only reachable here from StageFailed, which is terminal
// until a retry, so the spawned goroutine cannot race a still-running pipeline.
func (o *Orchestrator) HandleRetry(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionId")
	v, ok := o.sessions.Load(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	s := v.(*models.PipelineSession)

	// failedStage and the selected paper are immutable once a failure occurs, so
	// we validate routing against them BEFORE the atomic transition. Reject an
	// unknown/unroutable failed stage up front.
	failed := s.FailedStage()
	switch failed {
	case models.StageDiscovery, models.StageExtracting, models.StageGenerating,
		models.StageReviewing, models.StageRevising, models.StageWriting:
		// routable
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "this error is not retryable"})
		return
	}
	// Pipeline stages need the selected paper (discovery does not); the pick is
	// preserved across the failure so the user does NOT re-select.
	if failed != models.StageDiscovery && s.SelectedPaper() == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no paper selected to retry"})
		return
	}

	// Atomically confirm retryable + transition out of StageFailed. A concurrent
	// second retry gets false here and is rejected, so only one goroutine spawns.
	if _, ok := s.BeginRetry(); !ok {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session not retryable"})
		return
	}

	// Detach from the request context (cancelled once this handler returns) so the
	// resumed work survives — mirrors HandleDiscover/HandleProcess.
	ctx := context.WithoutCancel(r.Context())
	slog.Info("retry", "session_id", s.SessionID, "from_stage", string(failed))
	if failed == models.StageDiscovery {
		go o.runDiscovery(ctx, s)
	} else {
		// runPipeline skips cached segments; it reads the paper ID from the
		// selection preserved on the session.
		go o.runPipeline(ctx, s, s.SelectedPaper().ID)
	}

	writeJSON(w, http.StatusOK, RetryResponse{SessionID: id})
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
		Stage:           snap.Stage,
		Candidates:      snap.Candidates,
		Notice:          snap.Notice,
		Error:           snap.Error,
		Recoverable:     snap.Recoverable,
		Iteration:       snap.Iteration,
		ReviewScore:     snap.ReviewScore,
		ReviewPassed:    snap.ReviewPassed,
		ErrorAction:     snap.ErrorAction,
		ArxivRetryCount: snap.ArxivRetryCount,
		ContextWarning:  snap.ContextWarning,
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
	// Cost is estimated from the split token totals against the configured model.
	// CostKnown=false when the model isn't in the pricing table → the UI hides it.
	in, out := s.InputTokens(), s.OutputTokens()
	cost, known := llm.EstimateCost(o.cfg.LLM.Model, in, out)
	writeJSON(w, http.StatusOK, ResultResponse{
		Content:          ex.Content,
		VaultFile:        s.VaultFile(),
		TokensUsed:       s.TokensUsed(),
		InputTokens:      in,
		OutputTokens:     out,
		EstimatedCostUSD: cost,
		CostKnown:        known,
	})
}
