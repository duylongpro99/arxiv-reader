package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/agents"
	"github.com/maritime-ds/arxiv-reader/internal/arxivquery"
	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/maritime-ds/arxiv-reader/internal/llm"
	"github.com/maritime-ds/arxiv-reader/internal/models"
	"github.com/maritime-ds/arxiv-reader/internal/tools"
)

// --- fakes ---

type fakeFetcher struct {
	papers []models.Paper
	err    error
}

func (f *fakeFetcher) FetchPapers(context.Context, arxivquery.Query, func(int)) ([]models.Paper, error) {
	return f.papers, f.err
}

type fakeUnprocessor struct {
	filter func([]models.Paper) ([]models.Paper, error)
}

func (f *fakeUnprocessor) FilterUnprocessed(p []models.Paper) ([]models.Paper, error) {
	return f.filter(p)
}

// passthrough returns papers unchanged (nothing already processed).
func passthrough() *fakeUnprocessor {
	return &fakeUnprocessor{filter: func(p []models.Paper) ([]models.Paper, error) { return p, nil }}
}

func testCfg(displayLimit int) *config.Config {
	return &config.Config{Agent: config.AgentConfig{DisplayLimit: displayLimit}}
}

func makePapers(n int) []models.Paper {
	out := make([]models.Paper, n)
	for i := range out {
		out[i] = models.Paper{ID: string(rune('a' + i)), Title: "P"}
	}
	return out
}

func newOrch(cfg *config.Config, f PaperFetcher, u Unprocessor) *Orchestrator {
	return &Orchestrator{cfg: cfg, disco: f, logCheck: u}
}

// --- process-path fakes ---

type fakeContent struct {
	md     string
	err    error
	called int32
}

func (f *fakeContent) FetchMarkdown(context.Context, string) (string, error) {
	atomic.AddInt32(&f.called, 1)
	return f.md, f.err
}

// fakeExplainer returns a canned ExplainerOutput or a forced error. called
// counts invocations so a retry test can assert the generate loop was skipped
// (extraction/generation cached) on resume.
type fakeExplainer struct {
	out    models.ExplainerOutput
	err    error
	called int32
}

func (f *fakeExplainer) Generate(context.Context, agents.ExplainerInput) (models.ExplainerOutput, error) {
	atomic.AddInt32(&f.called, 1)
	if f.err != nil {
		return models.ExplainerOutput{}, f.err
	}
	return f.out, nil
}

// fakeVault records the write and returns a canned path or a forced error.
type fakeVault struct {
	path         string
	err          error
	written      int32
	lastVerdict  *models.ReviewVerdict // captured for review-loop assertions
	lastCategory string                // captured to assert the run's category is recorded
}

func (f *fakeVault) WriteToVault(_ context.Context, _ models.ExplainerOutput, _ models.Paper, verdict *models.ReviewVerdict, category string) (string, error) {
	atomic.AddInt32(&f.written, 1)
	f.lastVerdict = verdict
	f.lastCategory = category
	if f.err != nil {
		return "", f.err
	}
	return f.path, nil
}

// canned is a fully-wired explainer for happy-path process tests.
func canned() *fakeExplainer {
	return &fakeExplainer{out: models.ExplainerOutput{
		PaperID: "a", Content: "# Note\n## Problem Statement\nbody",
		InputTokens: 1200, OutputTokens: 800,
	}}
}

// newProcessOrch builds an orchestrator with fake content + explainer + vault.
// Optional overrides let a test inject a failing explainer/vault.
func newProcessOrch(c PaperContent, opts ...func(*Orchestrator)) *Orchestrator {
	o := &Orchestrator{
		cfg: testCfg(5), disco: &fakeFetcher{}, logCheck: passthrough(),
		content: c, explainer: canned(), vault: &fakeVault{path: "/vault/AI Papers/note.md"},
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// selectionSession stores a session already at the selection stage with the
// given candidates, ready for HandleProcess.
func selectionSession(o *Orchestrator, candidates []models.Paper) *models.PipelineSession {
	s := models.NewSession("sess-test", time.Now(), arxivquery.Query{Category: "cs.AI"})
	s.Complete(candidates, "")
	o.sessions.Store(s.SessionID, s)
	return s
}

// process POSTs /process and returns the recorder.
func process(o *Orchestrator, sessionID, paperID string) *httptest.ResponseRecorder {
	body := `{"session_id":"` + sessionID + `","paper_id":"` + paperID + `"}`
	rec := httptest.NewRecorder()
	o.HandleProcess(rec, httptest.NewRequest(http.MethodPost, "/process", strings.NewReader(body)))
	return rec
}

// waitFor polls until pred() is true or the deadline passes.
func waitFor(t *testing.T, pred func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pred() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for pipeline outcome")
}

func TestProcessHappyPathReachesComplete(t *testing.T) {
	o := newProcessOrch(&fakeContent{md: "# Extracted paper"})
	s := selectionSession(o, makePapers(3))

	rec := process(o, s.SessionID, "a")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	// Pipeline now runs past the Phase 3 seam through generating→writing→complete.
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageComplete })

	if s.Markdown() != "# Extracted paper" {
		t.Fatalf("markdown not stored, got %q", s.Markdown())
	}
	if ex := s.Explainer(); ex == nil || ex.Content == "" {
		t.Fatal("explainer not stored on complete")
	}
	if s.VaultFile() != "/vault/AI Papers/note.md" {
		t.Fatalf("vault file not stored, got %q", s.VaultFile())
	}
	if s.TokensUsed() != 2000 {
		t.Fatalf("tokens not accumulated, got %d", s.TokensUsed())
	}
}

func TestProcessGenerationErrorFailsRecoverable(t *testing.T) {
	// A transient LLM error (timeout) is recoverable — a retry may succeed.
	o := newProcessOrch(&fakeContent{md: "md"}, func(o *Orchestrator) {
		o.explainer = &fakeExplainer{err: llm.ErrLLMTimeout}
	})
	fv := &fakeVault{path: "x"}
	o.vault = fv
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageFailed })

	snap := s.Snapshot()
	if !snap.Recoverable || snap.Error == "" {
		t.Fatalf("transient generation failure should be recoverable with a message: %#v", snap)
	}
	// Vault must never be written when generation fails.
	if atomic.LoadInt32(&fv.written) != 0 {
		t.Fatal("vault must not be written on generation failure")
	}
	if s.VaultFile() != "" {
		t.Fatalf("no vault file expected, got %q", s.VaultFile())
	}
}

// A bad-request (paper too large / wrong model) is NOT recoverable: config is
// immutable at runtime, so an in-process retry would deterministically re-fail.
// The action hint is fix_config and /retry must reject it (400).
func TestProcessBadRequestNonRecoverable(t *testing.T) {
	o := newProcessOrch(&fakeContent{md: "md"}, func(o *Orchestrator) {
		o.explainer = &fakeExplainer{err: llm.ErrLLMBadRequest}
	})
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageFailed })

	if s.Snapshot().Recoverable {
		t.Fatal("bad-request must be non-recoverable")
	}
	if s.ErrorAction() != "fix_config" {
		t.Fatalf("bad-request action = %q, want fix_config", s.ErrorAction())
	}
	if rec := retry(o, s.SessionID); rec.Code != http.StatusBadRequest {
		t.Fatalf("retry of non-recoverable bad-request: want 400, got %d", rec.Code)
	}
}

func TestProcessEmptyGenerationFailsRecoverable(t *testing.T) {
	o := newProcessOrch(&fakeContent{md: "md"}, func(o *Orchestrator) {
		o.explainer = &fakeExplainer{out: models.ExplainerOutput{PaperID: "a", Content: "   \n"}}
	})
	fv := &fakeVault{path: "x"}
	o.vault = fv
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageFailed })

	if !s.Snapshot().Recoverable {
		t.Fatal("empty generation should be recoverable")
	}
	if atomic.LoadInt32(&fv.written) != 0 {
		t.Fatal("vault must not be written for empty content")
	}
}

func TestProcessVaultPermissionErrorNonRecoverable(t *testing.T) {
	o := newProcessOrch(&fakeContent{md: "md"}, func(o *Orchestrator) {
		o.vault = &fakeVault{err: os.ErrPermission}
	})
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageFailed })

	if s.Snapshot().Recoverable {
		t.Fatal("permission failure must be non-recoverable")
	}
}

// --- /result endpoint ---

func result(o *Orchestrator, id string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/result/"+id, nil)
	req.SetPathValue("sessionId", id)
	o.HandleResult(rec, req)
	return rec
}

func TestResultBeforeCompleteIs404(t *testing.T) {
	o := newProcessOrch(&fakeContent{md: "md"})
	s := selectionSession(o, makePapers(3)) // still at selection
	if rec := result(o, s.SessionID); rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 before complete, got %d", rec.Code)
	}
}

func TestResultUnknownSessionIs404(t *testing.T) {
	o := newProcessOrch(&fakeContent{md: "md"})
	if rec := result(o, "nope"); rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 for unknown session, got %d", rec.Code)
	}
}

func TestResultAfterCompleteReturnsContent(t *testing.T) {
	o := newProcessOrch(&fakeContent{md: "# Extracted paper"})
	s := selectionSession(o, makePapers(3))
	process(o, s.SessionID, "a")
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageComplete })

	rec := result(o, s.SessionID)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var resp ResultResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if resp.Content == "" || resp.VaultFile != "/vault/AI Papers/note.md" || resp.TokensUsed != 2000 {
		t.Fatalf("unexpected result payload: %#v", resp)
	}
}

func TestProcess404RecoversToSelection(t *testing.T) {
	o := newProcessOrch(&fakeContent{err: tools.ErrPaperHTMLNotFound})
	s := selectionSession(o, makePapers(3))

	if rec := process(o, s.SessionID, "a"); rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageSelection && s.Snapshot().Notice != "" })

	snap := s.Snapshot()
	if len(snap.Candidates) != 3 {
		t.Fatalf("candidates must be preserved on re-pick, got %d", len(snap.Candidates))
	}
	if !snap.Recoverable {
		t.Fatal("re-pick must be recoverable")
	}
}

func TestProcessOtherErrorFailsRecoverable(t *testing.T) {
	o := newProcessOrch(&fakeContent{err: tools.ErrPaperHTMLFailed})
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageFailed })

	snap := s.Snapshot()
	if !snap.Recoverable || snap.Error == "" {
		t.Fatalf("expected recoverable failure with message, got %#v", snap)
	}
}

func TestProcessWrongStage400(t *testing.T) {
	o := newProcessOrch(&fakeContent{md: "x"})
	s := models.NewSession("sess-test", time.Now(), arxivquery.Query{Category: "cs.AI"}) // still in discovery
	o.sessions.Store(s.SessionID, s)

	if rec := process(o, s.SessionID, "a"); rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for non-selection stage, got %d", rec.Code)
	}
}

func TestProcessUnknownPaper400(t *testing.T) {
	o := newProcessOrch(&fakeContent{md: "x"})
	s := selectionSession(o, makePapers(3))

	if rec := process(o, s.SessionID, "zzz"); rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for unknown paper_id, got %d", rec.Code)
	}
}

func TestProcessUnknownSession404(t *testing.T) {
	o := newProcessOrch(&fakeContent{md: "x"})
	if rec := process(o, "nope", "a"); rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 for unknown session, got %d", rec.Code)
	}
}

// discover triggers a run and returns the new session ID.
func discover(t *testing.T, o *Orchestrator) string {
	t.Helper()
	rec := httptest.NewRecorder()
	o.HandleDiscover(rec, httptest.NewRequest(http.MethodPost, "/discover", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("discover status: %d", rec.Code)
	}
	var resp DiscoverResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode discover response: %v", err)
	}
	if resp.SessionID == "" {
		t.Fatal("empty session id")
	}
	return resp.SessionID
}

// waitStatus polls the status endpoint until the stage is terminal or times out.
func waitStatus(t *testing.T, o *Orchestrator, id string) StatusResponse {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := getStatus(t, o, id)
		if status.Stage == models.StageSelection || status.Stage == models.StageFailed {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for terminal stage")
	return StatusResponse{}
}

func getStatus(t *testing.T, o *Orchestrator, id string) StatusResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/status/"+id, nil)
	req.SetPathValue("sessionId", id)
	o.HandleStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status code: %d", rec.Code)
	}
	var s StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	return s
}

func TestDiscoverReachesSelection(t *testing.T) {
	o := newOrch(testCfg(5), &fakeFetcher{papers: makePapers(8)}, passthrough())
	id := discover(t, o)
	status := waitStatus(t, o, id)

	if status.Stage != models.StageSelection {
		t.Fatalf("stage: want selection, got %s", status.Stage)
	}
	if len(status.Candidates) != 5 { // capped to display limit
		t.Fatalf("expected 5 candidates, got %d", len(status.Candidates))
	}
	if status.Notice != "" {
		t.Fatalf("did not expect a notice, got %q", status.Notice)
	}
}

func TestDiscoverFewerThanLimitSetsNotice(t *testing.T) {
	o := newOrch(testCfg(5), &fakeFetcher{papers: makePapers(3)}, passthrough())
	id := discover(t, o)
	status := waitStatus(t, o, id)

	if len(status.Candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(status.Candidates))
	}
	if status.Notice == "" {
		t.Fatal("expected a fewer-than-limit notice")
	}
}

func TestDiscoverArxivFailureRecoverable(t *testing.T) {
	o := newOrch(testCfg(5), &fakeFetcher{err: tools.ErrArxivRateLimit}, passthrough())
	id := discover(t, o)
	status := waitStatus(t, o, id)

	if status.Stage != models.StageFailed {
		t.Fatalf("stage: want failed, got %s", status.Stage)
	}
	if !status.Recoverable {
		t.Fatal("rate-limit failure should be recoverable")
	}
	if status.Error == "" {
		t.Fatal("expected an error message")
	}
}

func TestDiscoverCorruptLogNotRecoverable(t *testing.T) {
	u := &fakeUnprocessor{filter: func([]models.Paper) ([]models.Paper, error) {
		return nil, tools.ErrLogCorrupted
	}}
	o := newOrch(testCfg(5), &fakeFetcher{papers: makePapers(4)}, u)
	id := discover(t, o)
	status := waitStatus(t, o, id)

	if status.Stage != models.StageFailed {
		t.Fatalf("stage: want failed, got %s", status.Stage)
	}
	if status.Recoverable {
		t.Fatal("corrupt log must NOT be recoverable")
	}
}

func TestStatusUnknownSession404(t *testing.T) {
	o := newOrch(testCfg(5), &fakeFetcher{}, passthrough())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/status/nope", nil)
	req.SetPathValue("sessionId", "nope")
	o.HandleStatus(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
}

// --- Phase 6: retry-from-failed-stage ---

// toggleVault fails the first WriteToVault call (transient, recoverable) then
// succeeds, so a retry test can verify the vault-only resume path.
type toggleVault struct {
	written int32
	path    string
}

func (v *toggleVault) WriteToVault(context.Context, models.ExplainerOutput, models.Paper, *models.ReviewVerdict, string) (string, error) {
	// A bare error is recoverable by default (vaultRecoverable), modelling a
	// transient disk hiccup that clears on retry.
	if atomic.AddInt32(&v.written, 1) == 1 {
		return "", errors.New("transient write hiccup")
	}
	return v.path, nil
}

// retry POSTs /retry/{sessionId} and returns the recorder.
func retry(o *Orchestrator, id string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/retry/"+id, nil)
	req.SetPathValue("sessionId", id)
	o.HandleRetry(rec, req)
	return rec
}

// A transient vault failure then retry must re-write WITHOUT re-fetching HTML or
// re-calling the LLM — the whole point of segment-level resume (zero LLM re-cost).
func TestRetryVaultFailureSkipsLLM(t *testing.T) {
	content := &fakeContent{md: "# Extracted paper"}
	o := newProcessOrch(content) // cfg has MaxReviewIterations=0 → reviewer disabled
	fv := &toggleVault{path: "/vault/AI Papers/note.md"}
	o.vault = fv
	expl := o.explainer.(*fakeExplainer)
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageFailed })

	// State after the first (failed) run: markdown + explainer cached, tokens spent.
	if !s.Snapshot().Recoverable {
		t.Fatal("transient vault failure must be recoverable")
	}
	if s.FailedStage() != models.StageWriting {
		t.Fatalf("failed stage = %q, want writing", s.FailedStage())
	}
	if got := atomic.LoadInt32(&content.called); got != 1 {
		t.Fatalf("content fetched %d times before retry, want 1", got)
	}
	if got := atomic.LoadInt32(&expl.called); got != 1 {
		t.Fatalf("explainer called %d times before retry, want 1", got)
	}
	tokensBefore := s.TokensUsed()

	// Retry: vault now succeeds. Must reach complete re-running ONLY the write.
	if rec := retry(o, s.SessionID); rec.Code != http.StatusOK {
		t.Fatalf("retry: want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageComplete })

	if got := atomic.LoadInt32(&content.called); got != 1 {
		t.Fatalf("extraction re-ran on retry (called=%d); markdown cache not honoured", got)
	}
	if got := atomic.LoadInt32(&expl.called); got != 1 {
		t.Fatalf("LLM re-called on retry (called=%d); explainer cache not honoured", got)
	}
	if s.TokensUsed() != tokensBefore {
		t.Fatalf("tokens changed on vault-only retry: before=%d after=%d", tokensBefore, s.TokensUsed())
	}
	if atomic.LoadInt32(&fv.written) != 2 {
		t.Fatalf("vault write count = %d, want 2 (one fail + one success)", fv.written)
	}
	if s.VaultFile() != "/vault/AI Papers/note.md" {
		t.Fatalf("vault file not set after retry: %q", s.VaultFile())
	}
}

// A generation failure then retry must re-run the generate loop but NOT re-fetch
// the HTML (markdown stays cached).
func TestRetryGenerationFailureReRunsLoopNotExtraction(t *testing.T) {
	content := &fakeContent{md: "# Extracted paper"}
	// Explainer errors on the FIRST call, succeeds after — model a transient LLM fail.
	expl := &fakeExplainer{err: llm.ErrLLMTimeout}
	o := newProcessOrch(content, func(o *Orchestrator) { o.explainer = expl })
	fv := &fakeVault{path: "/vault/AI Papers/note.md"}
	o.vault = fv
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageFailed })
	if s.FailedStage() != models.StageGenerating {
		t.Fatalf("failed stage = %q, want generating", s.FailedStage())
	}
	if s.Explainer() != nil {
		t.Fatal("explainer must be nil after generation failure")
	}

	// Clear the error and let the explainer succeed on the retry.
	expl.err = nil
	expl.out = models.ExplainerOutput{PaperID: "a", Content: "# Note\nbody", InputTokens: 100, OutputTokens: 50}

	if rec := retry(o, s.SessionID); rec.Code != http.StatusOK {
		t.Fatalf("retry: want 200, got %d", rec.Code)
	}
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageComplete })

	if got := atomic.LoadInt32(&content.called); got != 1 {
		t.Fatalf("extraction re-ran on generation retry (called=%d), want 1", got)
	}
	if got := atomic.LoadInt32(&expl.called); got != 2 {
		t.Fatalf("explainer called %d times, want 2 (1 fail + 1 retry)", got)
	}
	// Paper selection must survive the retry.
	if p := s.SelectedPaper(); p == nil || p.ID != "a" {
		t.Fatalf("selected paper not preserved across retry: %+v", p)
	}
}

// A non-recoverable failure must never be retryable.
func TestRetryNonRecoverableReturns400(t *testing.T) {
	o := newProcessOrch(&fakeContent{md: "md"}, func(o *Orchestrator) {
		o.vault = &fakeVault{err: os.ErrPermission} // permission → non-recoverable
	})
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageFailed })
	if s.Snapshot().Recoverable {
		t.Fatal("precondition: permission failure must be non-recoverable")
	}

	if rec := retry(o, s.SessionID); rec.Code != http.StatusBadRequest {
		t.Fatalf("retry of non-recoverable: want 400, got %d", rec.Code)
	}
}

func TestRetryUnknownSessionReturns404(t *testing.T) {
	o := newProcessOrch(&fakeContent{md: "md"})
	if rec := retry(o, "nope"); rec.Code != http.StatusNotFound {
		t.Fatalf("retry unknown session: want 404, got %d", rec.Code)
	}
}

// --- Phase 6: context-window pre-check (F4) ---

// An oversized paper must attach a NON-BLOCKING ContextWarning: the pipeline
// still completes; the warning is advisory only.
func TestContextWarningSetButNonBlocking(t *testing.T) {
	// gpt-4o's limit is 128k tokens. ~600k chars → ~150k estimated tokens > limit.
	bigMD := strings.Repeat("x", 600_000)
	o := newProcessOrch(&fakeContent{md: bigMD})
	o.cfg.LLM.Model = "gpt-4o"
	o.cfg.LLM.MaxTokens = 4096
	s := selectionSession(o, makePapers(1))

	process(o, s.SessionID, "a")
	// Non-blocking: the pipeline still reaches complete despite the warning.
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageComplete })

	cw := s.ContextWarning()
	if cw == nil {
		t.Fatal("expected a context warning for oversized markdown")
	}
	if cw.Model != "gpt-4o" || cw.ModelLimit != 128_000 || cw.EstimatedTokens < 128_000 {
		t.Fatalf("unexpected warning: %+v", cw)
	}
}

// A normal-sized paper (or an unknown model) must NOT attach a warning.
func TestNoContextWarningForNormalPaper(t *testing.T) {
	o := newProcessOrch(&fakeContent{md: "# Short paper\n\nA few paragraphs."})
	o.cfg.LLM.Model = "claude-sonnet-4-6"
	o.cfg.LLM.MaxTokens = 4096
	s := selectionSession(o, makePapers(1))

	process(o, s.SessionID, "a")
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageComplete })

	if cw := s.ContextWarning(); cw != nil {
		t.Fatalf("did not expect a context warning: %+v", cw)
	}
}
