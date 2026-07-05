package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/agents"
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

func (f *fakeFetcher) FetchPapers(context.Context) ([]models.Paper, error) {
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

// fakeExplainer returns a canned ExplainerOutput or a forced error.
type fakeExplainer struct {
	out models.ExplainerOutput
	err error
}

func (f *fakeExplainer) Generate(context.Context, agents.ExplainerInput) (models.ExplainerOutput, error) {
	if f.err != nil {
		return models.ExplainerOutput{}, f.err
	}
	return f.out, nil
}

// fakeVault records the write and returns a canned path or a forced error.
type fakeVault struct {
	path        string
	err         error
	written     int32
	lastVerdict *models.ReviewVerdict // captured for review-loop assertions
}

func (f *fakeVault) WriteToVault(_ context.Context, _ models.ExplainerOutput, _ models.Paper, verdict *models.ReviewVerdict) (string, error) {
	atomic.AddInt32(&f.written, 1)
	f.lastVerdict = verdict
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
	s := models.NewSession("sess-test", time.Now())
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
	o := newProcessOrch(&fakeContent{md: "md"}, func(o *Orchestrator) {
		o.explainer = &fakeExplainer{err: llm.ErrLLMBadRequest}
	})
	fv := &fakeVault{path: "x"}
	o.vault = fv
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageFailed })

	snap := s.Snapshot()
	if !snap.Recoverable || snap.Error == "" {
		t.Fatalf("generation failure should be recoverable with a message: %#v", snap)
	}
	// Vault must never be written when generation fails.
	if atomic.LoadInt32(&fv.written) != 0 {
		t.Fatal("vault must not be written on generation failure")
	}
	if s.VaultFile() != "" {
		t.Fatalf("no vault file expected, got %q", s.VaultFile())
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
	s := models.NewSession("sess-test", time.Now()) // still in discovery
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
