// Package orchestrator sequences the discovery-pipeline tools and owns the
// in-memory session state. It is the conductor: it coordinates and tracks
// state, but contains no arXiv/dedup business logic of its own.
package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/maritime-ds/arxiv-reader/internal/agents"
	"github.com/maritime-ds/arxiv-reader/internal/arxivquery"
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
	// query carries the per-session category + free-text; onRetry (nil-safe)
	// fires per transient arXiv retry so the orchestrator can surface a progress
	// counter (F5) without the tool touching the session.
	FetchPapers(ctx context.Context, query arxivquery.Query, onRetry func(attempt int)) ([]models.Paper, error)
}

// PageFetcher is the narrow, consumer-side contract for Feature C (arXiv
// pagination via session extension): fetching an arbitrary page by start
// offset. Kept as its OWN interface (rather than adding a method to
// PaperFetcher) so the existing PaperFetcher fakes used by orchestrator_test.go
// keep compiling unchanged — this is strictly additive.
type PageFetcher interface {
	FetchPapersFrom(ctx context.Context, query arxivquery.Query, start int, onRetry func(attempt int)) ([]models.Paper, error)
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
	WriteToVault(ctx context.Context, ex models.ExplainerOutput, p models.Paper, verdict *models.ReviewVerdict, category string) (string, error)
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
	discoMore PageFetcher // Feature C: pagination — same concrete tool as disco, narrower interface
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

	// One concrete DiscoveryTool instance backs both the PaperFetcher (first
	// page, used by runDiscovery) and PageFetcher (arbitrary page, used by
	// HandleDiscoverMore) roles — it implements both methods; only the narrower
	// interface each caller depends on differs.
	disco := tools.NewDiscoveryTool(&cfg.Agent)
	o := &Orchestrator{
		cfg:       cfg,
		disco:     disco,
		discoMore: disco,
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
	query, err := o.parseDiscoverQuery(r)
	if err != nil {
		// Malformed body or unknown category — both client errors, so 400.
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	session := o.newSession(query)
	o.sessions.Store(session.SessionID, session)
	o.rec(session) // create the recorder + run-header row at run start

	slog.Info("discovery started", "session_id", session.SessionID)

	// Detach from the request context: it is cancelled the moment this handler
	// returns (which is immediately), and using it would abort discovery
	// instantly. The HTTP client's own timeout bounds the background work.
	go o.runDiscovery(context.WithoutCancel(r.Context()), session)

	writeJSON(w, http.StatusOK, DiscoverResponse{SessionID: session.SessionID})
}

// discoverRequest is the optional JSON body for POST /discover. Both fields are
// optional: an empty/absent body (existing clients) falls back to the config
// default category with no free-text.
type discoverRequest struct {
	Category string `json:"category"`
	Terms    string `json:"terms"`
}

// parseDiscoverQuery decodes the optional {category, terms} body into a
// validated arxivquery.Query. An empty body (EOF) is the backward-compatible
// default path, not an error. It returns an error (→ 400) for a malformed body
// or an explicitly-supplied unknown category. Free-text is sanitized here (the
// single trust boundary for keyword input); the category is checked against the
// cs.* whitelist.
func (o *Orchestrator) parseDiscoverQuery(r *http.Request) (arxivquery.Query, error) {
	var req discoverRequest
	// EOF means no body → use defaults. Any other decode error is a genuinely
	// malformed request, which we reject rather than silently downgrading to a
	// default run (that would mask client bugs).
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err != io.EOF {
		return arxivquery.Query{}, fmt.Errorf("invalid request body")
	}

	category := strings.TrimSpace(req.Category)
	if category == "" {
		category = o.cfg.Agent.ArxivCategory // config default (validated at load)
	} else if !arxivquery.IsValid(category) {
		return arxivquery.Query{}, fmt.Errorf("unknown category %q", category)
	}

	return arxivquery.Query{
		Category: category,
		Terms:    arxivquery.SanitizeTerms(req.Terms),
	}, nil
}

// categoriesResponse carries the cs.* catalog plus the configured default so the
// UI seeds its picker from the SAME default the empty-body discovery path uses —
// no divergence between what the frontend shows and what the backend defaults to.
type categoriesResponse struct {
	Default    string               `json:"default"`
	Categories []arxivquery.Category `json:"categories"`
}

// HandleCategories returns the cs.* catalog + configured default for the UI
// dropdown. Static read of the compiled-in list + config — no session, no arXiv.
func (o *Orchestrator) HandleCategories(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, categoriesResponse{
		Default:    o.cfg.Agent.ArxivCategory,
		Categories: arxivquery.Categories,
	})
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

// HandleDiscoverMore extends an EXISTING discovery session with the next page
// of arXiv results (Feature C). Pagination is deliberately tied to the session
// rather than a decoupled browse endpoint: HandleProcess only accepts a
// paper_id that is already present in the session's own Candidates, so a
// separate/stateless "browse more" endpoint would produce papers /process
// could never select. Synchronous (unlike HandleDiscover) because a single
// arXiv page fetch is fast enough to return inline — no polling needed.
func (o *Orchestrator) HandleDiscoverMore(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("sessionId")
	v, ok := o.sessions.Load(id)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	s := v.(*models.PipelineSession)

	// Only paginate while candidates are ready and awaiting a pick. Discovery
	// runs in a detached goroutine and finalizes with Complete(), which REPLACES
	// the candidate slice — so a /more that lands before discovery completes would
	// fetch a page, AppendCandidates it, and then have it silently overwritten
	// (wasted arXiv call + a desynced cursor). Guard before consuming the cursor
	// so a rejected call never advances nextStart.
	if s.Snapshot().Stage != models.StageSelection {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "discovery not ready for pagination"})
		return
	}

	// Claim the next page under the session's own lock (ConsumeNextStart) so two
	// concurrent /more calls on the same session can never re-fetch or skip a
	// page — see the method doc for why a plain get-then-set would race here.
	fetchLimit := o.cfg.Agent.FetchLimit
	start := s.ConsumeNextStart(fetchLimit)

	// Pass the session's own query so pagination stays within the same category +
	// free-text the user chose for this run (never drifting to the config default).
	papers, err := o.discoMore.FetchPapersFrom(r.Context(), s.Query(), start, nil)
	if err != nil {
		msg, _, _ := describeError(err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": msg})
		return
	}

	unprocessed, err := o.logCheck.FilterUnprocessed(papers)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot filter processed papers"})
		return
	}
	s.AppendCandidates(unprocessed)

	// hasMore is a heuristic on the RAW page (before dedup filtering): a
	// full-sized page suggests arXiv likely has more beyond it; a short page
	// means we hit the end of the feed.
	hasMore := len(papers) == fetchLimit

	slog.Info("discover more",
		"session_id", id, "start", start, "fetched", len(papers), "new", len(unprocessed))

	// Timeline: record the extra fetch so history reflects it (optional per the
	// plan; reuses the existing discovery-completed kind rather than adding a
	// new EventKind constant, which lives in a file owned by a parallel phase).
	more := tev(tracing.KindToolDiscoveryCompleted, tracing.StatusSuccess, models.StageSelection,
		fmt.Sprintf("Fetched %d more papers from arXiv (%d new)", len(papers), len(unprocessed)))
	more.Summary = map[string]any{"start": start, "count": len(papers), "new": len(unprocessed)}
	o.rec(s).Emit(more)

	writeJSON(w, http.StatusOK, DiscoverMoreDTO{Candidates: unprocessed, HasMore: hasMore})
}
