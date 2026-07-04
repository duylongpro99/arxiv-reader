package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/config"
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
