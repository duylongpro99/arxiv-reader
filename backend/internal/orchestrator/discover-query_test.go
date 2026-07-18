package orchestrator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/arxivquery"
	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// cfgWithCategory builds a config whose default category is set, so the
// empty-body path has a valid default to fall back to.
func cfgWithCategory(cat string) *config.Config {
	return &config.Config{Agent: config.AgentConfig{DisplayLimit: 5, FetchLimit: 5, ArxivCategory: cat}}
}

// postDiscover triggers a run with an explicit JSON body and returns the
// recorder plus the new session ID (empty on non-200).
func postDiscover(o *Orchestrator, body string) (*httptest.ResponseRecorder, string) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/discover", strings.NewReader(body))
	o.HandleDiscover(rec, req)
	if rec.Code != http.StatusOK {
		return rec, ""
	}
	var resp DiscoverResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return rec, resp.SessionID
}

func sessionQuery(t *testing.T, o *Orchestrator, id string) arxivquery.Query {
	t.Helper()
	v, ok := o.sessions.Load(id)
	if !ok {
		t.Fatalf("session %q not stored", id)
	}
	return v.(*models.PipelineSession).Query()
}

// A chosen category + keywords must be stored on the session (so both the run
// and pagination use them), with the free-text sanitized on the way in.
func TestHandleDiscoverStoresValidatedQuery(t *testing.T) {
	o := newOrch(cfgWithCategory("cs.AI"), &fakeFetcher{}, passthrough())
	rec, id := postDiscover(o, `{"category":"cs.LG","terms":"speech OR recognition"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	q := sessionQuery(t, o, id)
	if q.Category != "cs.LG" {
		t.Errorf("category = %q, want cs.LG", q.Category)
	}
	// "OR" is an arXiv boolean operator and must be stripped by SanitizeTerms.
	if q.Terms != "speech recognition" {
		t.Errorf("terms = %q, want sanitized \"speech recognition\"", q.Terms)
	}
}

// An empty body is the backward-compatible path: fall back to the config
// default category with no free-text.
func TestHandleDiscoverEmptyBodyUsesDefault(t *testing.T) {
	o := newOrch(cfgWithCategory("cs.CV"), &fakeFetcher{}, passthrough())
	rec, id := postDiscover(o, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if q := sessionQuery(t, o, id); q.Category != "cs.CV" || q.Terms != "" {
		t.Errorf("query = %+v, want {cs.CV }", q)
	}
}

// An explicitly-supplied unknown category is a client error → 400, and no
// session is created.
func TestHandleDiscoverUnknownCategoryIs400(t *testing.T) {
	o := newOrch(cfgWithCategory("cs.AI"), &fakeFetcher{}, passthrough())
	rec, _ := postDiscover(o, `{"category":"cs.NOPE"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for unknown category, got %d", rec.Code)
	}
}

// A malformed (non-empty, non-JSON) body is a client error → 400, not a silent
// downgrade to a default run.
func TestHandleDiscoverMalformedBodyIs400(t *testing.T) {
	o := newOrch(cfgWithCategory("cs.AI"), &fakeFetcher{}, passthrough())
	rec, _ := postDiscover(o, `{"category": broken`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for malformed body, got %d", rec.Code)
	}
}

// GET /categories returns the compiled-in cs.* catalog plus the configured
// default, so the UI can seed its picker from the same default the backend uses.
func TestHandleCategoriesReturnsCatalogAndDefault(t *testing.T) {
	o := newOrch(cfgWithCategory("cs.LG"), &fakeFetcher{}, passthrough())
	rec := httptest.NewRecorder()
	o.HandleCategories(rec, httptest.NewRequest(http.MethodGet, "/categories", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var resp struct {
		Default    string                `json:"default"`
		Categories []arxivquery.Category `json:"categories"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode categories: %v", err)
	}
	if resp.Default != "cs.LG" {
		t.Errorf("default = %q, want cs.LG (the configured default)", resp.Default)
	}
	if len(resp.Categories) != len(arxivquery.Categories) || resp.Categories[0].Code == "" {
		t.Fatalf("unexpected categories payload: %d entries", len(resp.Categories))
	}
}

// The category a run was discovered under must reach the vault write, so the
// exported note's frontmatter records the real category — not the config
// default (regression guard: category became runtime-selectable).
func TestProcessPassesSessionCategoryToVault(t *testing.T) {
	fv := &fakeVault{path: "/vault/AI Papers/note.md"}
	o := newProcessOrch(&fakeContent{md: "md"}, func(o *Orchestrator) { o.vault = fv })

	s := models.NewSession("sess-cat", time.Now(), arxivquery.Query{Category: "cs.CR"})
	s.Complete(makePapers(3), "")
	o.sessions.Store(s.SessionID, s)

	process(o, s.SessionID, "a")
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageComplete })

	if fv.lastCategory != "cs.CR" {
		t.Fatalf("vault category = %q, want cs.CR (the run's category)", fv.lastCategory)
	}
}

// "Load more" must fetch within the SAME category the session was created with,
// never the config default — otherwise pagination would drift categories.
func TestDiscoverMoreUsesSessionQuery(t *testing.T) {
	pf := &fakePageFetcher{page: makePapers(5)}
	o := moreOrch(pf)
	s := models.NewSession("sess-q", time.Now(), arxivquery.Query{Category: "cs.CV", Terms: "detection"})
	s.Complete(makePapers(5), "") // move to selection stage so /more is allowed
	o.sessions.Store(s.SessionID, s)

	rec := discoverMore(o, s.SessionID)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if pf.lastQry.Category != "cs.CV" || pf.lastQry.Terms != "detection" {
		t.Errorf("pagination query = %+v, want {cs.CV detection}", pf.lastQry)
	}
}
