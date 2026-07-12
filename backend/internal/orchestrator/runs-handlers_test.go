package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/store"
	"github.com/maritime-ds/arxiv-reader/internal/tracing"
)

// fakeReader is an in-memory RunReader so the history endpoints can be tested
// without a real Postgres.
type fakeReader struct {
	run    store.RunRecord
	runErr error
	events []store.EventRecord
	runs   []store.RunRecord
	total  int
}

func (f *fakeReader) GetRun(_ context.Context, _ string) (store.RunRecord, error) {
	return f.run, f.runErr
}
func (f *fakeReader) ListRuns(_ context.Context, _, _ int) ([]store.RunRecord, int, error) {
	return f.runs, f.total, nil
}
func (f *fakeReader) ListEvents(_ context.Context, _ string, sinceSeq int) ([]store.EventRecord, error) {
	var out []store.EventRecord
	for _, e := range f.events {
		if e.Seq > sinceSeq {
			out = append(out, e)
		}
	}
	return out, nil
}

func getEvents(o *Orchestrator, id, lastEventID string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/runs/"+id+"/events", nil)
	req.SetPathValue("id", id)
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	o.HandleRunEvents(rec, req)
	return rec
}

// seededRun returns an orchestrator whose tracer holds a COMPLETED (terminal)
// recorder for "run1" with the given event kinds, plus the recorder.
func seededRun(t *testing.T, kinds ...tracing.EventKind) *Orchestrator {
	t.Helper()
	tr := tracing.New(true, nil, nil, false, 256, "")
	rec := tr.NewRecorder("run1", time.Now())
	for _, k := range kinds {
		rec.Emit(tracing.Event{Kind: k, Status: tracing.StatusInfo, Stage: "discovery", Title: string(k)})
	}
	rec.Close() // terminal → HandleRunEvents replays then ends (no blocking)
	return &Orchestrator{tracer: tr}
}

// countFrames counts SSE messages (separated by a blank line).
func countFrames(body string) int {
	n := 0
	for _, part := range strings.Split(body, "\n\n") {
		if strings.Contains(part, "data: ") {
			n++
		}
	}
	return n
}

func TestRunEventsReplaysTerminalRun(t *testing.T) {
	o := seededRun(t, tracing.KindDiscoveryStarted, tracing.KindToolDiscoveryCompleted, tracing.KindRunCompleted)
	rec := getEvents(o, "run1", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}
	body := rec.Body.String()
	if countFrames(body) != 3 {
		t.Fatalf("want 3 frames, got %d\n%s", countFrames(body), body)
	}
	// Frames carry id: <seq> for resume and the kind inside data.type (no named
	// SSE event field — the client reads type from the payload).
	if !strings.Contains(body, "id: 0") || !strings.Contains(body, `"type":"discovery.started"`) {
		t.Fatalf("missing first frame:\n%s", body)
	}
	if !strings.Contains(body, `"type":"run.completed"`) {
		t.Fatalf("missing terminal frame:\n%s", body)
	}
}

func TestRunEventsResumesFromLastEventID(t *testing.T) {
	o := seededRun(t, tracing.KindDiscoveryStarted, tracing.KindToolDiscoveryCompleted, tracing.KindRunCompleted)
	rec := getEvents(o, "run1", "1") // seq 0,1 already seen → only seq 2
	body := rec.Body.String()
	if countFrames(body) != 1 {
		t.Fatalf("resume want 1 frame, got %d\n%s", countFrames(body), body)
	}
	if !strings.Contains(body, "id: 2") || strings.Contains(body, "id: 0") {
		t.Fatalf("resume returned wrong frames:\n%s", body)
	}
}

func TestRunEventsHistoryFallbackFromDB(t *testing.T) {
	// No recorder for this id (empty tracer) → replay from the store.
	o := &Orchestrator{
		tracer: tracing.New(true, nil, nil, false, 256, ""),
		store: &fakeReader{
			run: store.RunRecord{ID: "old", Stage: "complete", Status: "complete"},
			events: []store.EventRecord{
				{RunID: "old", Seq: 0, EventType: "discovery.started", Stage: "discovery", Title: "s", Status: "info"},
				{RunID: "old", Seq: 1, EventType: "run.completed", Stage: "complete", Title: "done", Status: "success"},
			},
		},
	}
	rec := getEvents(o, "old", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	if countFrames(rec.Body.String()) != 2 {
		t.Fatalf("history replay want 2 frames:\n%s", rec.Body.String())
	}
}

// TestRunEventsLiveTailEndsOnClose drives the live path: a NON-terminal recorder
// with a buffered event is replayed, then the handler tails the broker until
// Close() (recorder terminal) ends the stream — exercising the select loop and
// the sub.Done exit (design M3: stream ends on Close, not on a terminal event).
func TestRunEventsLiveTailEndsOnClose(t *testing.T) {
	tr := tracing.New(true, nil, nil, false, 256, "")
	rec := tr.NewRecorder("live", time.Now())
	rec.Emit(tracing.Event{Kind: tracing.KindDiscoveryStarted, Status: tracing.StatusInfo, Stage: "discovery", Title: "s"})
	o := &Orchestrator{tracer: tr}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/runs/live/events", nil)
	req.SetPathValue("id", "live")
	done := make(chan struct{})
	go func() { o.HandleRunEvents(w, req); close(done) }()

	time.Sleep(50 * time.Millisecond) // let the handler subscribe + replay the buffer
	rec.Emit(tracing.Event{Kind: tracing.KindRunCompleted, Status: tracing.StatusSuccess, Stage: "complete", Title: "done"})
	rec.Close() // recorder terminal → broker Done → handler returns

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after Close")
	}
	if n := countFrames(w.Body.String()); n != 2 {
		t.Fatalf("live tail want 2 frames (1 replayed + 1 live), got %d\n%s", n, w.Body.String())
	}
}

// An orphaned run (persisted timeline with no client-terminal event, e.g. the
// backend died mid-run) must get a SYNTHETIC terminal appended so the browser's
// EventSource stops reconnecting instead of polling forever.
func TestRunEventsHistorySyntheticTerminalForOrphan(t *testing.T) {
	o := &Orchestrator{
		tracer: tracing.New(true, nil, nil, false, 256, ""),
		store: &fakeReader{
			run: store.RunRecord{ID: "orphan", Stage: "discovery", Status: "running"},
			events: []store.EventRecord{
				{RunID: "orphan", Seq: 0, EventType: "discovery.started", Stage: "discovery", Title: "s", Status: "info"},
			},
		},
	}
	body := getEvents(o, "orphan", "").Body.String()
	if countFrames(body) != 2 { // 1 real + 1 synthetic terminal
		t.Fatalf("orphan want 2 frames (real + synthetic terminal), got %d\n%s", countFrames(body), body)
	}
	if !strings.Contains(body, `"type":"run.failed"`) || !strings.Contains(body, `"synthetic":true`) {
		t.Fatalf("missing synthetic terminal:\n%s", body)
	}
}

// A finished run's persisted timeline already ends in a client-terminal event,
// so NO synthetic marker is appended (no spurious failure row).
func TestRunEventsHistoryNoSyntheticWhenComplete(t *testing.T) {
	o := &Orchestrator{
		tracer: tracing.New(true, nil, nil, false, 256, ""),
		store: &fakeReader{
			run: store.RunRecord{ID: "done", Stage: "complete", Status: "complete"},
			events: []store.EventRecord{
				{RunID: "done", Seq: 0, EventType: "discovery.started", Stage: "discovery", Title: "s", Status: "info"},
				{RunID: "done", Seq: 1, EventType: "run.completed", Stage: "complete", Title: "done", Status: "success"},
			},
		},
	}
	body := getEvents(o, "done", "").Body.String()
	if countFrames(body) != 2 { // exactly the 2 real frames, no synthetic
		t.Fatalf("complete run want 2 frames (no synthetic), got %d\n%s", countFrames(body), body)
	}
	if strings.Contains(body, `"synthetic":true`) {
		t.Fatalf("unexpected synthetic terminal for a completed run:\n%s", body)
	}
}

func TestRunEventsUnknownRun404(t *testing.T) {
	o := &Orchestrator{
		tracer: tracing.New(true, nil, nil, false, 256, ""),
		store:  &fakeReader{runErr: store.ErrRunNotFound},
	}
	if rec := getEvents(o, "nope", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
}

func TestRunEventsDBDown503(t *testing.T) {
	o := &Orchestrator{tracer: tracing.New(true, nil, nil, false, 256, "")} // store nil
	if rec := getEvents(o, "any", ""); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
}

func TestRunsListReturnsRuns(t *testing.T) {
	title := "A Paper"
	o := &Orchestrator{store: &fakeReader{
		runs:  []store.RunRecord{{ID: "r1", PaperTitle: &title, Stage: "complete", Status: "complete"}},
		total: 1,
	}}
	rec := httptest.NewRecorder()
	o.HandleRunsList(rec, httptest.NewRequest(http.MethodGet, "/runs?limit=10", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var resp RunsListResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 1 || len(resp.Runs) != 1 || resp.Runs[0].PaperTitle != "A Paper" {
		t.Fatalf("unexpected list: %+v", resp)
	}
}

func TestRunsListDBDown503(t *testing.T) {
	o := &Orchestrator{} // store nil
	rec := httptest.NewRecorder()
	o.HandleRunsList(rec, httptest.NewRequest(http.MethodGet, "/runs", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("want 503, got %d", rec.Code)
	}
}

func TestRunDetailReturnsRunAndEvents(t *testing.T) {
	o := &Orchestrator{store: &fakeReader{
		run: store.RunRecord{ID: "r1", Stage: "complete", Status: "complete"},
		events: []store.EventRecord{
			{RunID: "r1", Seq: 0, EventType: "discovery.started", Stage: "discovery", Title: "s", Status: "info"},
		},
	}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/runs/r1", nil)
	req.SetPathValue("id", "r1")
	o.HandleRun(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var resp RunDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Run.ID != "r1" || len(resp.Events) != 1 || resp.Events[0].Type != "discovery.started" {
		t.Fatalf("unexpected detail: %+v", resp)
	}
}

func TestRunDetailUnknown404(t *testing.T) {
	o := &Orchestrator{store: &fakeReader{runErr: store.ErrRunNotFound}}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/runs/nope", nil)
	req.SetPathValue("id", "nope")
	o.HandleRun(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rec.Code)
	}
}
