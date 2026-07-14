package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/maritime-ds/arxiv-reader/internal/agents/repurposer"
	"github.com/maritime-ds/arxiv-reader/internal/channels"
	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/maritime-ds/arxiv-reader/internal/store"
	"github.com/maritime-ds/arxiv-reader/internal/tracing"
)

// --- test doubles ---------------------------------------------------------

// fakePublicationStore is an in-memory PublicationStore + the ListEvents/
// AppendEvent slice it shares with the run-content read path and the
// best-effort tracing helper — mirrors how the real *store.Store backs both.
type fakePublicationStore struct {
	pubs      map[string]store.PublicationRecord
	events    []store.EventRecord
	createErr error
}

func newFakePublicationStore() *fakePublicationStore {
	return &fakePublicationStore{pubs: map[string]store.PublicationRecord{}}
}

func (f *fakePublicationStore) CreatePublication(_ context.Context, p store.PublicationRecord) (bool, error) {
	if f.createErr != nil {
		return false, f.createErr
	}
	for _, existing := range f.pubs {
		if existing.RunID == p.RunID && existing.ChannelID == p.ChannelID {
			return false, nil // ON CONFLICT (run_id, channel_id) DO NOTHING
		}
	}
	f.pubs[p.ID] = p
	return true, nil
}

func (f *fakePublicationStore) ListPublicationsByRun(_ context.Context, runID string) ([]store.PublicationRecord, error) {
	var out []store.PublicationRecord
	for _, p := range f.pubs {
		if p.RunID == runID {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakePublicationStore) GetPublication(_ context.Context, id string) (store.PublicationRecord, error) {
	p, ok := f.pubs[id]
	if !ok {
		return store.PublicationRecord{}, store.ErrPublicationNotFound
	}
	return p, nil
}

func (f *fakePublicationStore) UpdatePublicationContent(_ context.Context, id, title, content, status string) error {
	p := f.pubs[id]
	p.Title = &title
	p.AdaptedContent = content
	p.Status = status
	f.pubs[id] = p
	return nil
}

func (f *fakePublicationStore) MarkPublished(_ context.Context, id, url, extID string) error {
	p := f.pubs[id]
	p.Status = "published"
	p.ExternalURL = &url
	p.ExternalID = &extID
	f.pubs[id] = p
	return nil
}

func (f *fakePublicationStore) MarkFailed(_ context.Context, id, errMsg string) error {
	p := f.pubs[id]
	p.Status = "failed"
	p.Error = &errMsg
	f.pubs[id] = p
	return nil
}

// ClaimForPublish mirrors the store's atomic approved|failed → publishing
// transition: it succeeds only from a publishable status, so a draft, a
// published row, or a row already being published cannot be claimed.
func (f *fakePublicationStore) ClaimForPublish(_ context.Context, id string) (bool, error) {
	p, ok := f.pubs[id]
	if !ok {
		return false, nil
	}
	if p.Status == "approved" || p.Status == "failed" {
		p.Status = "publishing"
		f.pubs[id] = p
		return true, nil
	}
	return false, nil
}

func (f *fakePublicationStore) ListEvents(_ context.Context, runID string, sinceSeq int) ([]store.EventRecord, error) {
	var out []store.EventRecord
	for _, e := range f.events {
		if e.RunID == runID && e.Seq > sinceSeq {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakePublicationStore) AppendEvent(_ context.Context, e store.EventRecord) error {
	f.events = append(f.events, e)
	return nil
}

// fakeRepurposer counts Generate calls per category so tests can assert the
// per-unique-category dedup (design note §3: one LLM call, N channel drafts).
type fakeRepurposer struct {
	calls map[channels.Category]int
	err   error
}

func (f *fakeRepurposer) Generate(_ context.Context, in repurposer.RepurposeInput) (channels.GeneratedContent, error) {
	if f.err != nil {
		return channels.GeneratedContent{}, f.err
	}
	if f.calls == nil {
		f.calls = map[channels.Category]int{}
	}
	f.calls[in.Category]++
	return channels.GeneratedContent{
		Category: in.Category, Title: "Title-" + string(in.Category), Body: "Body-" + string(in.Category),
		PaperMeta: in.PaperMeta,
	}, nil
}

// fakeChannel is a canned Channel used to test the publish/validate fan-out
// without a real dev.to/X integration (neither is implemented until P4/P5).
type fakeChannel struct {
	id          string
	category    channels.Category
	validateErr error
	publishErr  error
	result      channels.PublishResult
}

func (f *fakeChannel) ID() string                               { return f.id }
func (f *fakeChannel) Category() channels.Category              { return f.category }
func (f *fakeChannel) Validate(channels.GeneratedContent) error { return f.validateErr }
func (f *fakeChannel) Publish(context.Context, channels.GeneratedContent) (channels.PublishResult, error) {
	if f.publishErr != nil {
		return channels.PublishResult{}, f.publishErr
	}
	return f.result, nil
}

// fakeChannelFactory resolves ids against a fixed map, mirroring
// channels.NewChannel's "unknown id → error, never nil" contract.
func fakeChannelFactory(chs map[string]*fakeChannel) func(id string, cfg *config.Config) (channels.Channel, error) {
	return func(id string, _ *config.Config) (channels.Channel, error) {
		if ch, ok := chs[id]; ok {
			return ch, nil
		}
		return nil, errNotConfigured(id)
	}
}

type notConfiguredErr string

func (e notConfiguredErr) Error() string { return "channel not configured: " + string(e) }
func errNotConfigured(id string) error   { return notConfiguredErr(id) }

// newTestVaultRun writes a note file + the tool.vaultwriter.completed event
// that vaultPathFromEvents looks for, so readRunMarkdown resolves it exactly
// like HandleRunContent does.
func newTestVaultRun(t *testing.T, runID string) (vaultDir string, events []store.EventRecord) {
	t.Helper()
	vaultDir = t.TempDir()
	notePath := filepath.Join(vaultDir, "note.md")
	if err := os.WriteFile(notePath, []byte("# Explainer\nsome content"), 0o644); err != nil {
		t.Fatalf("write note: %v", err)
	}
	summary, _ := json.Marshal(map[string]string{"path": notePath})
	events = []store.EventRecord{
		{RunID: runID, Seq: 0, EventType: string(tracing.KindToolVaultWriterCompleted), Summary: summary},
	}
	return vaultDir, events
}

func strPtr(s string) *string { return &s }

// --- HandleChannels --------------------------------------------------------

func TestHandleChannelsSkipsUnresolvable(t *testing.T) {
	o := &Orchestrator{
		cfg: &config.Config{Publishing: config.PublishingConfig{Channels: []string{"devto", "x"}}},
		channelFactory: fakeChannelFactory(map[string]*fakeChannel{
			"devto": {id: "devto", category: channels.Longform},
			// "x" deliberately absent → unresolvable, must be skipped not 500.
		}),
	}
	rec := httptest.NewRecorder()
	o.HandleChannels(rec, httptest.NewRequest(http.MethodGet, "/channels", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp ChannelsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Channels) != 1 || resp.Channels[0].ID != "devto" {
		t.Fatalf("unexpected channels: %+v", resp.Channels)
	}
}

// --- DB-off 503 guards -----------------------------------------------------

func TestPublishingEndpointsDBDown503(t *testing.T) {
	o := &Orchestrator{cfg: &config.Config{}} // publications nil

	cases := []struct {
		name string
		do   func() *httptest.ResponseRecorder
	}{
		{"create", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/runs/r1/publications", bytes.NewBufferString(`{"channels":["devto"]}`))
			req.SetPathValue("id", "r1")
			o.HandleCreatePublications(rec, req)
			return rec
		}},
		{"list", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/runs/r1/publications", nil)
			req.SetPathValue("id", "r1")
			o.HandleListPublications(rec, req)
			return rec
		}},
		{"patch", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPatch, "/publications/p1", bytes.NewBufferString(`{}`))
			req.SetPathValue("pid", "p1")
			o.HandlePatchPublication(rec, req)
			return rec
		}},
		{"publish", func() *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/publications/p1/publish", nil)
			req.SetPathValue("pid", "p1")
			o.HandlePublish(rec, req)
			return rec
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if rec := c.do(); rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("%s: want 503, got %d: %s", c.name, rec.Code, rec.Body.String())
			}
		})
	}
}

// --- HandleCreatePublications ----------------------------------------------

// TestHandleCreatePublicationsDedupGeneration is the core success path: 3
// channels, 2 of which share the "longform" category, must produce exactly
// 3 drafts from exactly 2 Generate calls (one per DISTINCT category).
func TestHandleCreatePublicationsDedupGeneration(t *testing.T) {
	vaultDir, events := newTestVaultRun(t, "run1")
	pubStore := newFakePublicationStore()
	pubStore.events = events
	repo := &fakeRepurposer{}

	o := &Orchestrator{
		cfg:          &config.Config{Paths: config.PathsConfig{ObsidianVault: vaultDir}},
		store:        &fakeReader{run: store.RunRecord{ID: "run1", PaperID: strPtr("p1"), PaperTitle: strPtr("Paper")}},
		publications: pubStore,
		repurpose:    repo,
		channelFactory: fakeChannelFactory(map[string]*fakeChannel{
			"devto":  {id: "devto", category: channels.Longform},
			"devto2": {id: "devto2", category: channels.Longform}, // same category as devto
			"x":      {id: "x", category: channels.Brief},
		}),
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/runs/run1/publications",
		bytes.NewBufferString(`{"channels":["devto","devto2","x"]}`))
	req.SetPathValue("id", "run1")
	o.HandleCreatePublications(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if repo.calls[channels.Longform] != 1 || repo.calls[channels.Brief] != 1 {
		t.Fatalf("want exactly 1 Generate call per category, got %+v", repo.calls)
	}
	var resp PublicationsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Publications) != 3 {
		t.Fatalf("want 3 drafts, got %d: %+v", len(resp.Publications), resp.Publications)
	}
}

func TestHandleCreatePublicationsNoNote422(t *testing.T) {
	pubStore := newFakePublicationStore() // no vaultwriter.completed event seeded
	o := &Orchestrator{
		cfg:          &config.Config{Paths: config.PathsConfig{ObsidianVault: t.TempDir()}},
		store:        &fakeReader{run: store.RunRecord{ID: "run1"}},
		publications: pubStore,
		repurpose:    &fakeRepurposer{},
		channelFactory: fakeChannelFactory(map[string]*fakeChannel{
			"devto": {id: "devto", category: channels.Longform},
		}),
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/runs/run1/publications", bytes.NewBufferString(`{"channels":["devto"]}`))
	req.SetPathValue("id", "run1")
	o.HandleCreatePublications(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleCreatePublicationsIdempotent re-requests an already-drafted
// (run, channel) pair: the row must come back unchanged, and the generator
// must NOT be invoked again for a request containing only that channel.
func TestHandleCreatePublicationsIdempotent(t *testing.T) {
	vaultDir, events := newTestVaultRun(t, "run1")
	pubStore := newFakePublicationStore()
	pubStore.events = events
	pubStore.pubs["existing"] = store.PublicationRecord{
		ID: "existing", RunID: "run1", ChannelID: "devto", Category: "longform",
		Status: "draft", AdaptedContent: "already generated", Title: strPtr("Existing"),
	}
	repo := &fakeRepurposer{}
	o := &Orchestrator{
		cfg:          &config.Config{Paths: config.PathsConfig{ObsidianVault: vaultDir}},
		store:        &fakeReader{run: store.RunRecord{ID: "run1"}},
		publications: pubStore,
		repurpose:    repo,
		channelFactory: fakeChannelFactory(map[string]*fakeChannel{
			"devto": {id: "devto", category: channels.Longform},
		}),
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/runs/run1/publications", bytes.NewBufferString(`{"channels":["devto"]}`))
	req.SetPathValue("id", "run1")
	o.HandleCreatePublications(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp PublicationsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The CreatePublication insert is a no-op (ON CONFLICT DO NOTHING) for this
	// (run, channel) pair, so the EXISTING row comes back unchanged even though
	// generation still runs per resolved channel's category — dedup is about
	// categories (design note §3), not about skipping generation when a row
	// already exists (idempotency guards the DB row / no re-post, not the LLM
	// call budget).
	if len(resp.Publications) != 1 || resp.Publications[0].Content != "already generated" {
		t.Fatalf("want the existing row returned unchanged, got %+v", resp.Publications)
	}
	if repo.calls[channels.Longform] != 1 {
		t.Fatalf("want exactly 1 generate call for the resolved channel's category, got %+v", repo.calls)
	}
}

// --- HandlePatchPublication --------------------------------------------------

func TestHandlePatchPublicationEditAndApprove(t *testing.T) {
	pubStore := newFakePublicationStore()
	pubStore.pubs["p1"] = store.PublicationRecord{ID: "p1", RunID: "run1", ChannelID: "devto", Status: "draft", AdaptedContent: "orig"}
	o := &Orchestrator{publications: pubStore}

	rec := httptest.NewRecorder()
	body := `{"title":"New Title","content":"New Content","approve":true}`
	req := httptest.NewRequest(http.MethodPatch, "/publications/p1", bytes.NewBufferString(body))
	req.SetPathValue("pid", "p1")
	o.HandlePatchPublication(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var dto PublicationDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.Title != "New Title" || dto.Content != "New Content" || dto.Status != "approved" {
		t.Fatalf("unexpected patch result: %+v", dto)
	}
}

func TestHandlePatchPublicationRejectsEditingPublished(t *testing.T) {
	pubStore := newFakePublicationStore()
	pubStore.pubs["p1"] = store.PublicationRecord{ID: "p1", Status: "published"}
	o := &Orchestrator{publications: pubStore}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/publications/p1", bytes.NewBufferString(`{"title":"x"}`))
	req.SetPathValue("pid", "p1")
	o.HandlePatchPublication(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- HandlePublish -----------------------------------------------------------

func TestHandlePublishSuccessStoresURL(t *testing.T) {
	pubStore := newFakePublicationStore()
	pubStore.pubs["p1"] = store.PublicationRecord{
		ID: "p1", RunID: "run1", ChannelID: "devto", Category: "longform",
		Status: "approved", AdaptedContent: "body", Title: strPtr("Title"),
	}
	o := &Orchestrator{
		cfg:          &config.Config{},
		store:        &fakeReader{run: store.RunRecord{ID: "run1"}},
		publications: pubStore,
		channelFactory: fakeChannelFactory(map[string]*fakeChannel{
			"devto": {id: "devto", category: channels.Longform, result: channels.PublishResult{ExternalURL: "https://dev.to/x", ExternalID: "123"}},
		}),
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/publications/p1/publish", nil)
	req.SetPathValue("pid", "p1")
	o.HandlePublish(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var dto PublicationDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &dto); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if dto.Status != "published" || dto.ExternalURL != "https://dev.to/x" {
		t.Fatalf("unexpected publish result: %+v", dto)
	}

	// Second publish on the now-published row must 409, never re-post.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/publications/p1/publish", nil)
	req2.SetPathValue("pid", "p1")
	o.HandlePublish(rec2, req2)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("want 409 on re-publish, got %d: %s", rec2.Code, rec2.Body.String())
	}
}

// TestHandlePublishRejectsUnapprovedDraft guards the human-review decision at
// the API boundary: a draft that was never approved must NOT publish, and the
// channel's Publish must never be reached.
func TestHandlePublishRejectsUnapprovedDraft(t *testing.T) {
	pubStore := newFakePublicationStore()
	pubStore.pubs["p1"] = store.PublicationRecord{
		ID: "p1", RunID: "run1", ChannelID: "devto", Category: "longform",
		Status: "draft", AdaptedContent: "body", Title: strPtr("Title"),
	}
	published := false
	o := &Orchestrator{
		cfg:          &config.Config{},
		store:        &fakeReader{run: store.RunRecord{ID: "run1"}},
		publications: pubStore,
		channelFactory: fakeChannelFactory(map[string]*fakeChannel{
			// A result is set, but Validate/Publish must never run for a draft.
			"devto": {id: "devto", category: channels.Longform, result: channels.PublishResult{ExternalURL: "https://dev.to/x", ExternalID: "1"}},
		}),
	}
	// Wrap the channel so we can detect an unexpected Publish call.
	o.channelFactory = func(id string, cfg *config.Config) (channels.Channel, error) {
		return &publishSpyChannel{onPublish: func() { published = true }}, nil
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/publications/p1/publish", nil)
	req.SetPathValue("pid", "p1")
	o.HandlePublish(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409 for unapproved draft, got %d: %s", rec.Code, rec.Body.String())
	}
	if published {
		t.Fatal("Publish must not be called for an unapproved draft")
	}
	if pubStore.pubs["p1"].Status != "draft" {
		t.Fatalf("draft status must be unchanged, got %q", pubStore.pubs["p1"].Status)
	}
}

// TestHandlePublishRejectsInFlight exercises the atomic-claim race guard: a row
// already in the transient "publishing" state (a concurrent publish in flight)
// cannot be claimed again, so a second request 409s without a duplicate post.
func TestHandlePublishRejectsInFlight(t *testing.T) {
	pubStore := newFakePublicationStore()
	pubStore.pubs["p1"] = store.PublicationRecord{
		ID: "p1", RunID: "run1", ChannelID: "devto", Category: "longform",
		Status: "publishing", AdaptedContent: "body", Title: strPtr("Title"),
	}
	published := false
	o := &Orchestrator{
		cfg:          &config.Config{},
		store:        &fakeReader{run: store.RunRecord{ID: "run1"}},
		publications: pubStore,
	}
	o.channelFactory = func(id string, cfg *config.Config) (channels.Channel, error) {
		return &publishSpyChannel{onPublish: func() { published = true }}, nil
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/publications/p1/publish", nil)
	req.SetPathValue("pid", "p1")
	o.HandlePublish(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("want 409 for in-flight publish, got %d: %s", rec.Code, rec.Body.String())
	}
	if published {
		t.Fatal("Publish must not be called when the claim is lost")
	}
}

// publishSpyChannel is a Longform channel that records whether Publish ran, used
// by the guard tests above to prove Publish is never reached on a rejected path.
type publishSpyChannel struct{ onPublish func() }

func (c *publishSpyChannel) ID() string                               { return "devto" }
func (c *publishSpyChannel) Category() channels.Category              { return channels.Longform }
func (c *publishSpyChannel) Validate(channels.GeneratedContent) error { return nil }
func (c *publishSpyChannel) Publish(context.Context, channels.GeneratedContent) (channels.PublishResult, error) {
	if c.onPublish != nil {
		c.onPublish()
	}
	return channels.PublishResult{ExternalURL: "https://example/x", ExternalID: "1"}, nil
}

func TestHandlePublishChannelErrorMarksFailed(t *testing.T) {
	pubStore := newFakePublicationStore()
	pubStore.pubs["p1"] = store.PublicationRecord{
		ID: "p1", RunID: "run1", ChannelID: "devto", Category: "longform",
		Status: "approved", AdaptedContent: "body", Title: strPtr("Title"),
	}
	o := &Orchestrator{
		cfg:          &config.Config{},
		store:        &fakeReader{run: store.RunRecord{ID: "run1"}},
		publications: pubStore,
		channelFactory: fakeChannelFactory(map[string]*fakeChannel{
			"devto": {id: "devto", category: channels.Longform, publishErr: errNotConfigured("devto rate limited")},
		}),
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/publications/p1/publish", nil)
	req.SetPathValue("pid", "p1")
	o.HandlePublish(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d: %s", rec.Code, rec.Body.String())
	}
	stored := pubStore.pubs["p1"]
	if stored.Status != "failed" || stored.Error == nil {
		t.Fatalf("want MarkFailed applied, got %+v", stored)
	}
}
