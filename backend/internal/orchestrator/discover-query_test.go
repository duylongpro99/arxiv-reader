package orchestrator

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/maritime-ds/arxiv-reader/internal/models"
	"github.com/maritime-ds/arxiv-reader/internal/resource"
)

// cfgWithCategory builds a config whose default category is set (used by the
// temporary /categories alias, which still reads cfg.Agent.ArxivCategory).
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

func sessionValues(t *testing.T, o *Orchestrator, id string) map[string]string {
	t.Helper()
	v, ok := o.sessions.Load(id)
	if !ok {
		t.Fatalf("session %q not stored", id)
	}
	return v.(*models.PipelineSession).Values()
}

// The engine-validated values must be stored on the session (so both the run and
// pagination use them). The handler delegates whitelist/sanitize to the source's
// ValidateValues; here we assert the wiring stores what ValidateValues returned.
func TestHandleDiscoverStoresValidatedValues(t *testing.T) {
	src := &fakeSource{validated: map[string]string{"category": "cs.LG", "terms": "speech recognition"}}
	o := newOrch(cfgWithCategory("cs.AI"), src, passthrough())
	rec, id := postDiscover(o, `{"resourceId":"arxiv","values":{"category":"cs.LG","terms":"speech OR recognition"}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	vals := sessionValues(t, o, id)
	if vals["category"] != "cs.LG" || vals["terms"] != "speech recognition" {
		t.Errorf("session values = %+v, want validated map", vals)
	}
}

// An empty body defaults to the arxiv resource with empty (default-filled) values.
func TestHandleDiscoverEmptyBodyDefaults(t *testing.T) {
	src := &fakeSource{}
	o := newOrch(cfgWithCategory("cs.CV"), src, passthrough())
	rec, id := postDiscover(o, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if id == "" {
		t.Fatal("expected a session id")
	}
}

// F17: the exact legacy {category, terms} body (no resourceId/values) must fold
// into values and reach ValidateValues — existing clients keep working.
func TestHandleDiscoverLegacyBodyFolded(t *testing.T) {
	src := &fakeSource{}
	o := newOrch(cfgWithCategory("cs.AI"), src, passthrough())
	rec, _ := postDiscover(o, `{"category":"cs.LG","terms":"transformer"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("legacy body want 200, got %d (%s)", rec.Code, rec.Body.String())
	}
	if src.lastValidateIn["category"] != "cs.LG" || src.lastValidateIn["terms"] != "transformer" {
		t.Errorf("legacy values not folded before validation: %+v", src.lastValidateIn)
	}
}

// F17: a legacy field with the wrong JSON type is a 400 (not a silent drop).
func TestHandleDiscoverLegacyWrongTypeIs400(t *testing.T) {
	o := newOrch(cfgWithCategory("cs.AI"), &fakeSource{}, passthrough())
	rec, _ := postDiscover(o, `{"category":123}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for wrong-typed legacy field, got %d", rec.Code)
	}
}

// An unknown resourceId is a client error → 400, no session created.
func TestHandleDiscoverUnknownResourceIs400(t *testing.T) {
	o := newOrch(cfgWithCategory("cs.AI"), &fakeSource{}, passthrough())
	rec, _ := postDiscover(o, `{"resourceId":"nope"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for unknown resource, got %d", rec.Code)
	}
}

// A value rejected by the resource's schema (e.g. off-catalog category) → 400.
func TestHandleDiscoverValidationErrorIs400(t *testing.T) {
	src := &fakeSource{validateErr: errors.New("invalid value for \"category\"")}
	o := newOrch(cfgWithCategory("cs.AI"), src, passthrough())
	rec, _ := postDiscover(o, `{"resourceId":"arxiv","values":{"category":"cs.NOPE"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for schema violation, got %d", rec.Code)
	}
}

// A malformed (non-empty, non-JSON) body → 400.
func TestHandleDiscoverMalformedBodyIs400(t *testing.T) {
	o := newOrch(cfgWithCategory("cs.AI"), &fakeSource{}, passthrough())
	rec, _ := postDiscover(o, `{"resourceId": broken`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for malformed body, got %d", rec.Code)
	}
}

// GET /resources returns the registry's descriptors for the UI.
func TestHandleResourcesReturnsDescriptors(t *testing.T) {
	src := &fakeSource{descriptor: resource.Descriptor{
		ID: "arxiv", Label: "arXiv",
		Fields: []resource.Field{
			{Name: "category", Type: resource.FieldSelect},
			{Name: "terms", Type: resource.FieldText},
		},
	}}
	o := newOrch(cfgWithCategory("cs.AI"), src, passthrough())
	rec := httptest.NewRecorder()
	o.HandleResources(rec, httptest.NewRequest(http.MethodGet, "/resources", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var got []resource.Descriptor
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode resources: %v", err)
	}
	if len(got) != 1 || got[0].ID != "arxiv" || len(got[0].Fields) != 2 {
		t.Fatalf("unexpected resources payload: %+v", got)
	}
}

// F8: the category a run was discovered under must reach the vault write, sourced
// from the session's values (not the config default).
func TestProcessPassesSessionCategoryToVault(t *testing.T) {
	fv := &fakeVault{path: "/vault/AI Papers/note.md"}
	o := newProcessOrch(&fakeSource{md: "md"}, func(o *Orchestrator) { o.vault = fv })

	s := models.NewSession("sess-cat", time.Now(), "arxiv", map[string]string{"category": "cs.CR"})
	s.Complete(makePapers(3), "")
	o.sessions.Store(s.SessionID, s)

	process(o, s.SessionID, "a")
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageComplete })

	if fv.lastCategory != "cs.CR" {
		t.Fatalf("vault category = %q, want cs.CR (the run's category)", fv.lastCategory)
	}
}

// "Load more" must fetch within the SAME values the session was created with.
func TestDiscoverMoreUsesSessionValues(t *testing.T) {
	pf := &fakePageSource{page: makePapers(5)}
	o := moreOrch(pf)
	s := models.NewSession("sess-q", time.Now(), "arxiv", map[string]string{"category": "cs.CV", "terms": "detection"})
	s.Complete(makePapers(5), "")
	o.sessions.Store(s.SessionID, s)

	rec := discoverMore(o, s.SessionID)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if pf.lastValues["category"] != "cs.CV" || pf.lastValues["terms"] != "detection" {
		t.Errorf("pagination values = %+v, want {cs.CV detection}", pf.lastValues)
	}
}
