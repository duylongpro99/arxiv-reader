package tracing

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/store"
)

// fakeStore captures store calls in memory so recorder tests can assert the
// best-effort persistence path without a real Postgres. Implements both the
// EventWriter and RunWriter interfaces.
type fakeStore struct {
	mu      sync.Mutex
	events  []store.EventRecord
	created []store.RunRecord
	papers  map[string][2]string
	final   map[string]store.RunRecord
	order   []string // records the sequence of write ops (guards H1 ordering)
}

func newFakeStore() *fakeStore {
	return &fakeStore{papers: map[string][2]string{}, final: map[string]store.RunRecord{}}
}

func (f *fakeStore) AppendEvent(_ context.Context, e store.EventRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
	f.order = append(f.order, "event")
	return nil
}
func (f *fakeStore) CreateRun(_ context.Context, r store.RunRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, r)
	f.order = append(f.order, "create")
	return nil
}
func (f *fakeStore) UpdateRunPaper(_ context.Context, id, pid, title string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.papers[id] = [2]string{pid, title}
	f.order = append(f.order, "paper")
	return nil
}
func (f *fakeStore) FinalizeRun(_ context.Context, r store.RunRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.final[r.ID] = r
	f.order = append(f.order, "final")
	return nil
}
func (f *fakeStore) eventCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.events)
}

// waitUntil polls pred until true or a deadline (for the async persist path).
func waitUntil(t *testing.T, pred func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pred() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}

func testRecorder(cfg recorderConfig, ev EventWriter, rw RunWriter) *Recorder {
	r := newRecorder("run-test", time.Now(), cfg, newScrubber("APIKEYSECRET"), NewBroker(), ev, rw)
	r.start()
	return r
}

func TestEmitAssignsMonotonicSeq(t *testing.T) {
	r := testRecorder(recorderConfig{bufferSize: 256}, nil, nil)
	for i := 0; i < 5; i++ {
		r.Emit(Event{Kind: KindDiscoveryStarted, Status: StatusInfo})
	}
	snap := r.Snapshot(-1)
	if len(snap) != 5 {
		t.Fatalf("want 5 events, got %d", len(snap))
	}
	for i, e := range snap {
		if e.Seq != i {
			t.Fatalf("event %d has seq %d", i, e.Seq)
		}
	}
}

func TestConcurrentEmitUniqueSeq(t *testing.T) {
	r := testRecorder(recorderConfig{bufferSize: 1024}, nil, nil)
	const n = 200
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Emit(Event{Kind: KindLLMExplainerCompleted, Status: StatusSuccess})
		}()
	}
	wg.Wait()
	snap := r.Snapshot(-1)
	if len(snap) != n {
		t.Fatalf("want %d events, got %d", n, len(snap))
	}
	seen := make(map[int]bool, n)
	for _, e := range snap {
		if seen[e.Seq] {
			t.Fatalf("duplicate seq %d", e.Seq)
		}
		seen[e.Seq] = true
	}
	for i := 0; i < n; i++ {
		if !seen[i] {
			t.Fatalf("missing seq %d", i)
		}
	}
}

func TestSnapshotSinceSeqAndEviction(t *testing.T) {
	r := testRecorder(recorderConfig{bufferSize: 3}, nil, nil)
	for i := 0; i < 5; i++ {
		r.Emit(Event{Kind: KindDiscoveryStarted})
	}
	// Buffer caps at 3, holding the newest (seq 2,3,4).
	snap := r.Snapshot(-1)
	if len(snap) != 3 || snap[0].Seq != 2 || snap[2].Seq != 4 {
		t.Fatalf("eviction wrong: %+v", seqs(snap))
	}
	// sinceSeq filters within the buffer window.
	if got := r.Snapshot(3); len(got) != 1 || got[0].Seq != 4 {
		t.Fatalf("since filter wrong: %+v", seqs(got))
	}
}

func seqs(evts []Event) []int {
	out := make([]int, len(evts))
	for i, e := range evts {
		out[i] = e.Seq
	}
	return out
}

func TestEmitScrubsSummaryLiveAndPersisted(t *testing.T) {
	fs := newFakeStore()
	r := testRecorder(recorderConfig{bufferSize: 16}, fs, fs)
	r.Emit(Event{
		Kind:    KindLLMExplainerCompleted,
		Status:  StatusSuccess,
		Summary: map[string]any{"note": "leaked APIKEYSECRET in here"},
	})
	// Live (buffered) event is scrubbed.
	snap := r.Snapshot(-1)
	if got := snap[0].Summary["note"].(string); strings.Contains(got, "APIKEYSECRET") {
		t.Fatalf("live summary not scrubbed: %q", got)
	}
	// Persisted event is scrubbed too.
	waitUntil(t, func() bool { return fs.eventCount() == 1 })
	fs.mu.Lock()
	persisted := string(fs.events[0].Summary)
	fs.mu.Unlock()
	if strings.Contains(persisted, "APIKEYSECRET") {
		t.Fatalf("persisted summary not scrubbed: %s", persisted)
	}
	if !strings.Contains(persisted, redacted) {
		t.Fatalf("expected redaction marker in persisted summary: %s", persisted)
	}
}

func TestFullPayloadGating(t *testing.T) {
	// Off (default): PayloadFull dropped.
	off := testRecorder(recorderConfig{bufferSize: 8, fullPayloads: false}, nil, nil)
	off.Emit(Event{Kind: KindLLMExplainerCompleted, PayloadFull: map[string]any{"prompt": "x"}})
	if off.Snapshot(-1)[0].PayloadFull != nil {
		t.Fatal("payload should be dropped when full_payloads is off")
	}
	// On: PayloadFull kept (and scrubbed).
	on := testRecorder(recorderConfig{bufferSize: 8, fullPayloads: true}, nil, nil)
	on.Emit(Event{Kind: KindLLMExplainerCompleted, PayloadFull: map[string]any{"prompt": "has APIKEYSECRET"}})
	pf := on.Snapshot(-1)[0].PayloadFull
	if pf == nil {
		t.Fatal("payload should be kept when full_payloads is on")
	}
	if strings.Contains(pf["prompt"].(string), "APIKEYSECRET") {
		t.Fatal("payload not scrubbed")
	}
}

func TestNilStoreInMemoryOnly(t *testing.T) {
	// No DB: Emit/Snapshot/broker still work; nothing panics.
	r := testRecorder(recorderConfig{bufferSize: 8}, nil, nil)
	sub := r.broker.Subscribe(r.runID)
	defer sub.Cancel()
	r.Emit(Event{Kind: KindDiscoveryStarted})
	select {
	case e := <-sub.Events:
		if e.Seq != 0 {
			t.Fatalf("unexpected seq %d", e.Seq)
		}
	case <-time.After(time.Second):
		t.Fatal("no live event with nil store")
	}
	r.SetPaper("2401.1", "Title") // no-op, must not panic
	r.Finalize(Final{Status: "complete"})
	r.Close()
}

func TestSetPaperAndFinalizePersist(t *testing.T) {
	fs := newFakeStore()
	r := testRecorder(recorderConfig{bufferSize: 8}, fs, fs)
	r.SetPaper("2401.12345", "A Title")
	waitUntil(t, func() bool {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		return fs.papers[r.runID] == [2]string{"2401.12345", "A Title"}
	})
	r.Finalize(Final{Stage: "complete", Status: "complete", InputTokens: 100, CompletedAt: time.Now()})
	waitUntil(t, func() bool {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		_, ok := fs.final[r.runID]
		return ok
	})
	r.Close()
}

// TestPersistOrderCreateRunFirst guards the H1 fix: the run-header INSERT must
// be the FIRST durable write, before any event append — otherwise run_events'
// foreign key to runs would be violated and opening events silently dropped.
func TestPersistOrderCreateRunFirst(t *testing.T) {
	fs := newFakeStore()
	r := testRecorder(recorderConfig{bufferSize: 32}, fs, fs)
	for i := 0; i < 5; i++ {
		r.Emit(Event{Kind: KindDiscoveryStarted})
	}
	r.SetPaper("2401.1", "T")
	r.Finalize(Final{Status: "complete", CompletedAt: time.Now()})
	waitUntil(t, func() bool {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		return len(fs.order) >= 7 // create + 5 events + paper + final (>=)
	})
	fs.mu.Lock()
	defer fs.mu.Unlock()
	if fs.order[0] != "create" {
		t.Fatalf("first durable write = %q, want create (FK safety): %v", fs.order[0], fs.order)
	}
}

// TestCloseIdempotent verifies a second Close is a safe no-op (no double
// channel-close panic).
func TestCloseIdempotent(t *testing.T) {
	r := testRecorder(recorderConfig{bufferSize: 4}, nil, nil)
	r.Close()
	r.Close() // must not panic
	if !r.IsTerminal() {
		t.Fatal("closed recorder must report terminal")
	}
}

func TestEmitAfterCloseIsNoop(t *testing.T) {
	r := testRecorder(recorderConfig{bufferSize: 8}, nil, nil)
	r.Emit(Event{Kind: KindRunCompleted})
	r.Close()
	r.Emit(Event{Kind: KindDiscoveryStarted}) // must not panic or add
	if len(r.Snapshot(-1)) != 1 {
		t.Fatal("emit after close should be a no-op")
	}
}

func TestNilRecorderMethodsAreSafe(t *testing.T) {
	var r *Recorder
	r.Emit(Event{Kind: KindDiscoveryStarted})
	r.SetPaper("a", "b")
	r.Finalize(Final{})
	r.Close()
	if r.Snapshot(-1) != nil || r.IsTerminal() {
		t.Fatal("nil recorder should return zero values")
	}
}

// --- Tracer facade ---

func TestTracerDisabledReturnsNilRecorder(t *testing.T) {
	tr := New(false, nil, nil, false, 16, "")
	if tr.NewRecorder("run1", time.Now()) != nil {
		t.Fatal("disabled tracer must return a nil recorder")
	}
}

func TestTracerEvictsClosedRecorderWithDB(t *testing.T) {
	fs := newFakeStore()
	tr := New(true, fs, fs, false, 16, "")
	rec := tr.NewRecorder("run1", time.Now())
	rec.Emit(Event{Kind: KindRunCompleted})
	rec.Close()
	// DB present → a finished run is evicted so it doesn't accumulate; a reconnect
	// would fall back to the persisted timeline.
	if tr.Recorder("run1") != nil {
		t.Fatal("closed recorder should be evicted from the registry when a DB is present")
	}
}

func TestTracerRetainsClosedRecorderWithoutDB(t *testing.T) {
	tr := New(true, nil, nil, false, 16, "")
	rec := tr.NewRecorder("run1", time.Now())
	rec.Emit(Event{Kind: KindRunCompleted})
	rec.Close()
	// In-memory-only mode: the ring buffer is the sole copy, so it must be kept.
	if tr.Recorder("run1") == nil {
		t.Fatal("in-memory-only recorder must be retained after close (no DB fallback)")
	}
}

func TestTracerRecorderRegistryAndCreateRun(t *testing.T) {
	fs := newFakeStore()
	tr := New(true, fs, fs, false, 16, "APIKEYSECRET")
	rec := tr.NewRecorder("run1", time.Now())
	if rec == nil {
		t.Fatal("enabled tracer must create a recorder")
	}
	// Idempotent + looked up by id.
	if tr.NewRecorder("run1", time.Now()) != rec || tr.Recorder("run1") != rec {
		t.Fatal("recorder registry not idempotent")
	}
	if tr.Recorder("missing") != nil {
		t.Fatal("unknown run should yield nil recorder")
	}
	// Header row created best-effort.
	waitUntil(t, func() bool {
		fs.mu.Lock()
		defer fs.mu.Unlock()
		return len(fs.created) == 1 && fs.created[0].ID == "run1"
	})
}
