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
)

// fakePageFetcher satisfies both PaperFetcher and PageFetcher (as the real
// DiscoveryTool does), recording how many times the paginated fetch ran so a
// test can assert a rejected /more never reaches arXiv.
type fakePageFetcher struct {
	page  []models.Paper
	calls int
}

func (f *fakePageFetcher) FetchPapers(context.Context, func(int)) ([]models.Paper, error) {
	return f.page, nil
}

func (f *fakePageFetcher) FetchPapersFrom(_ context.Context, _ int, _ func(int)) ([]models.Paper, error) {
	f.calls++
	return f.page, nil
}

func moreOrch(pf *fakePageFetcher) *Orchestrator {
	cfg := &config.Config{Agent: config.AgentConfig{DisplayLimit: 5, FetchLimit: 5}}
	return &Orchestrator{cfg: cfg, disco: pf, discoMore: pf, logCheck: passthrough()}
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
	pf := &fakePageFetcher{page: makePapers(5)}
	o := moreOrch(pf)
	s := models.NewSession("sess-test", time.Now()) // still in discovery stage
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
	pf := &fakePageFetcher{page: makePapers(5)} // full page → hasMore true
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
	o := moreOrch(&fakePageFetcher{})
	if rec := discoverMore(o, "nope"); rec.Code != http.StatusNotFound {
		t.Fatalf("want 404 for unknown session, got %d", rec.Code)
	}
}
