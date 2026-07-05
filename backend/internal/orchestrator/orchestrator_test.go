package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

// fakeLLM satisfies llm.LLMClient; Phase 3 never invokes it, so it is inert.
type fakeLLM struct{}

func (fakeLLM) Complete(context.Context, llm.CompletionRequest) (llm.CompletionResponse, error) {
	return llm.CompletionResponse{}, nil
}

func newProcessOrch(c PaperContent) *Orchestrator {
	return &Orchestrator{cfg: testCfg(5), disco: &fakeFetcher{}, logCheck: passthrough(), content: c, llm: fakeLLM{}}
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

func TestProcessHappyPathStoresMarkdown(t *testing.T) {
	o := newProcessOrch(&fakeContent{md: "# Extracted paper"})
	s := selectionSession(o, makePapers(3))

	rec := process(o, s.SessionID, "a")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	waitFor(t, func() bool { return s.Markdown() != "" })
	if s.Snapshot().Stage != models.StageExtracting {
		t.Fatalf("stage: want extracting, got %s", s.Snapshot().Stage)
	}
	if s.Markdown() != "# Extracted paper" {
		t.Fatalf("markdown not stored, got %q", s.Markdown())
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
