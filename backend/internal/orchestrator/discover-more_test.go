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
	"github.com/maritime-ds/arxiv-reader/internal/resource"
)

// fakePageSource is a resource.Source that records how many times Discover ran
// (so a test can assert a rejected /more never fetches) plus the last values it
// received (so a test can assert pagination uses the session's own values).
type fakePageSource struct {
	page       []models.Paper
	calls      int
	lastValues map[string]string
}

func (f *fakePageSource) ID() string                      { return "arxiv" }
func (f *fakePageSource) Descriptor() resource.Descriptor { return resource.Descriptor{ID: "arxiv"} }
func (f *fakePageSource) PageSize() int                   { return 5 }
func (f *fakePageSource) ValidateValues(v map[string]string) (map[string]string, error) {
	return v, nil
}
func (f *fakePageSource) Discover(_ context.Context, req resource.Request, _ int, _ func(int)) ([]models.Paper, error) {
	f.calls++
	f.lastValues = req.Values
	return f.page, nil
}
func (f *fakePageSource) FetchContent(context.Context, string) (string, error) { return "", nil }

func moreOrch(pf *fakePageSource) *Orchestrator {
	cfg := &config.Config{Agent: config.AgentConfig{DisplayLimit: 5, FetchLimit: 5}}
	return &Orchestrator{cfg: cfg, registry: regWith(pf), logCheck: passthrough()}
}

func discoverMore(o *Orchestrator, id string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/discover/"+id+"/more", nil)
	req.SetPathValue("sessionId", id)
	o.HandleDiscoverMore(rec, req)
	return rec
}

// A /more that lands before discovery finishes (session still in the discovery
// stage) must be rejected WITHOUT fetching — otherwise the fetched page would be
// silently overwritten by discovery's Complete() and the cursor left desynced.
func TestDiscoverMoreWrongStageIsRejectedAndDoesNotFetch(t *testing.T) {
	pf := &fakePageSource{page: makePapers(5)}
	o := moreOrch(pf)
	s := models.NewSession("sess-test", time.Now(), "arxiv", map[string]string{"category": "cs.AI"}) // still in discovery stage
	o.sessions.Store(s.SessionID, s)

	rec := discoverMore(o, s.SessionID)
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409 for non-selection stage, got %d", rec.Code)
	}
	if pf.calls != 0 {
		t.Fatalf("rejected /more must not fetch; FetchPapersFrom ran %d times", pf.calls)
	}
}

func TestDiscoverMoreAppendsCandidatesInSelection(t *testing.T) {
	pf := &fakePageSource{page: makePapers(5)} // full page → hasMore true
	o := moreOrch(pf)
	s := selectionSession(o, makePapers(3)) // 3 existing candidates, StageSelection

	rec := discoverMore(o, s.SessionID)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var resp DiscoverMoreDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Candidates) != 5 || !resp.HasMore {
		t.Fatalf("want 5 candidates + hasMore, got %d hasMore=%v", len(resp.Candidates), resp.HasMore)
	}
	// The page must be appended to the session so a later /process can find it.
	if got := len(s.Snapshot().Candidates); got != 8 {
		t.Fatalf("want 8 session candidates after append (3+5), got %d", got)
	}
}

func TestDiscoverMoreUnknownSessionIs404(t *testing.T) {
	o := moreOrch(&fakePageSource{})
	if rec := discoverMore(o, "nope"); rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 for unknown session, got %d", rec.Code)
	}
}
