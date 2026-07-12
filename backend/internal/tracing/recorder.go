package tracing

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/store"
)

// persistTimeout bounds each best-effort DB write so a slow/hung DB can never
// stall the tracing worker indefinitely.
const persistTimeout = 5 * time.Second

type recorderConfig struct {
	fullPayloads bool
	bufferSize   int
}

// paperUpdate carries a selection.chosen paper set onto the run header.
type paperUpdate struct{ id, title string }

// persistOp is one serialized durable write. Exactly one field is set. Routing
// ALL writes (run create, event append, paper update, finalize) through a single
// per-run worker goroutine guarantees ordering: the run-header INSERT always
// commits before any event/update that references it, so run_events' foreign key
// can never be violated (which would otherwise silently drop a run's opening
// events). It also keeps every write off the pipeline's hot path.
type persistOp struct {
	event *Event
	paper *paperUpdate
	final *store.RunRecord
}

// Recorder is the per-run dual-write seam. It owns the run's monotonic seq, a
// bounded in-memory ring buffer (feeds SSE replay), and a best-effort async
// persist worker. All methods are nil-safe: a nil *Recorder (tracing disabled or
// no recorder for a run) makes every call a no-op, so call sites stay terse.
type Recorder struct {
	runID     string
	startedAt time.Time
	cfg       recorderConfig
	scrub     *scrubber
	broker    *Broker
	events    EventWriter // nil → no durable persistence
	runs      RunWriter   // nil → no durable persistence

	mu     sync.Mutex
	seq    int
	buf    []Event
	closed bool // Close() called — run reached a true terminal (complete / fatal fail)

	persistCh chan persistOp

	// evict, if set, removes this recorder from the Tracer's registry on Close so
	// finished runs don't accumulate in memory. Set ONLY when a DB is present
	// (durable replay serves any post-eviction reconnect); nil in in-memory-only
	// mode, where the ring buffer is the sole copy and must be retained.
	evict func()
}

// newRecorder builds a recorder WITHOUT starting its persist worker, so a
// recorder that loses a registry race can be discarded without leaking a
// goroutine. The Tracer calls start() only on the winner.
func newRecorder(runID string, startedAt time.Time, cfg recorderConfig, scrub *scrubber, broker *Broker, ev EventWriter, rw RunWriter) *Recorder {
	return &Recorder{
		runID: runID, startedAt: startedAt, cfg: cfg, scrub: scrub, broker: broker, events: ev, runs: rw,
		persistCh: make(chan persistOp, cfg.bufferSize),
	}
}

// hasDB reports whether durable persistence is wired (ev and rw are set together
// or not at all by the Tracer).
func (r *Recorder) hasDB() bool { return r.events != nil }

// start launches the async persist worker (only when a DB is present).
func (r *Recorder) start() {
	if r.hasDB() {
		go r.persistLoop()
	}
}

// Emit stamps seq/time, scrubs, buffers, publishes live, and enqueues an async
// DB write — in that order (design §Data Flow). Non-blocking and safe from
// multiple goroutines (the pipeline goroutine plus the selection emit from the
// HTTP handler). The publish and persist-enqueue both happen UNDER the lock so
// that live fan-out order matches seq order (a reordered live delivery would
// corrupt a client's Last-Event-ID resume) and so the enqueue cannot race Close
// closing the channel.
func (r *Recorder) Emit(evt Event) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	evt.RunID = r.runID
	evt.Seq = r.seq
	r.seq++
	if evt.CreatedAt.IsZero() {
		evt.CreatedAt = time.Now()
	}
	// Scrub BOTH channels of leakage before the event is buffered/streamed/stored.
	evt.Summary = r.scrub.scrubMap(evt.Summary)
	if r.cfg.fullPayloads {
		evt.PayloadFull = r.scrub.scrubMap(evt.PayloadFull)
	} else {
		evt.PayloadFull = nil // opt-in only
	}
	r.pushLocked(evt)
	// Live fan-out is non-blocking (broker drops to a slow subscriber), so holding
	// the lock here is cheap and keeps delivery strictly seq-ordered.
	r.broker.Publish(r.runID, evt)
	r.enqueueLocked(persistOp{event: &evt})
}

// enqueueLocked schedules a best-effort durable write. Caller holds r.mu. A full
// queue drops the op (event stays live + replayable from the ring); never blocks.
func (r *Recorder) enqueueLocked(op persistOp) {
	if !r.hasDB() || r.closed {
		return
	}
	select {
	case r.persistCh <- op:
	default:
		slog.Warn("tracing: persist queue full; op dropped", "run_id", r.runID)
	}
}

// pushLocked appends to the ring, evicting the oldest when at capacity. The
// left-shift keeps the backing array bounded (no unbounded re-slice growth);
// bufferSize is small (default 256) so the O(n) shift is negligible.
func (r *Recorder) pushLocked(evt Event) {
	if len(r.buf) >= r.cfg.bufferSize {
		copy(r.buf, r.buf[1:])
		r.buf[len(r.buf)-1] = evt
		return
	}
	r.buf = append(r.buf, evt)
}

// Snapshot returns buffered events with seq > sinceSeq (ascending). Drives SSE
// replay on (re)connect; pass -1 for everything currently buffered.
func (r *Recorder) Snapshot(sinceSeq int) []Event {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Event
	for _, e := range r.buf {
		if e.Seq > sinceSeq {
			out = append(out, e)
		}
	}
	return out
}

// IsTerminal reports whether the recorder has been Closed — i.e. the run reached
// a TRUE terminal state (completion, or a non-recoverable failure). A recoverable
// failure does NOT close the recorder, because a retry resumes the same run and
// keeps emitting on it. The SSE handler uses this to replay-and-close a finished
// run instead of subscribing to a stream that will never produce more events.
func (r *Recorder) IsTerminal() bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

// SetPaper records the chosen paper on the run header (selection.chosen). Routed
// through the persist worker so it is ordered AFTER the run-header INSERT.
func (r *Recorder) SetPaper(paperID, title string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enqueueLocked(persistOp{paper: &paperUpdate{id: paperID, title: title}})
}

// Final carries the terminal run-header fields written on completion/failure.
type Final struct {
	Stage        string
	Status       string
	InputTokens  int
	OutputTokens int
	EstCostUSD   *float64
	ReviewPassed *bool
	CompletedAt  time.Time
}

// Finalize writes the terminal run header (best-effort, ordered on the worker).
func (r *Recorder) Finalize(f Final) {
	if r == nil {
		return
	}
	ct := f.CompletedAt
	rec := store.RunRecord{
		ID: r.runID, Stage: f.Stage, Status: f.Status,
		InputTokens: f.InputTokens, OutputTokens: f.OutputTokens,
		EstCostUSD: f.EstCostUSD, ReviewPassed: f.ReviewPassed, CompletedAt: &ct,
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enqueueLocked(persistOp{final: &rec})
}

// Close stops accepting emits, drains+ends the persist worker (via channel
// close), and ends live streaming for the run. Called on a terminal event.
// Idempotent and nil-safe.
func (r *Recorder) Close() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	close(r.persistCh) // worker drains remaining buffered ops, then exits
	r.mu.Unlock()
	r.broker.Close(r.runID)
	// Drop the finished recorder from the registry (DB-backed mode only). Any
	// in-flight SSE handler already holds this *Recorder, so eviction doesn't
	// interrupt it; a later reconnect falls back to the persisted timeline.
	if r.evict != nil {
		r.evict()
	}
}

// persistLoop serializes all durable writes for the run. It creates the run
// header FIRST so no event/update can reference a not-yet-committed row, then
// drains ops until Close closes the channel.
func (r *Recorder) persistLoop() {
	r.createRun()
	for op := range r.persistCh {
		switch {
		case op.event != nil:
			r.appendEvent(*op.event)
		case op.paper != nil:
			r.updatePaper(*op.paper)
		case op.final != nil:
			r.finalizeRun(*op.final)
		}
	}
}

func (r *Recorder) createRun() {
	if r.runs == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
	defer cancel()
	rec := store.RunRecord{ID: r.runID, Stage: "discovery", Status: "running", StartedAt: r.startedAt}
	if err := r.runs.CreateRun(ctx, rec); err != nil {
		slog.Warn("tracing: create run row failed", "run_id", r.runID, "err", err)
	}
}

func (r *Recorder) appendEvent(evt Event) {
	ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
	defer cancel()
	rec := store.EventRecord{
		RunID: evt.RunID, Seq: evt.Seq, EventType: string(evt.Kind),
		Stage: evt.Stage, Title: evt.Title, Status: string(evt.Status),
		Summary: marshalJSON(evt.Summary), PayloadFull: marshalJSON(evt.PayloadFull),
		DurationMS: evt.DurationMS, CreatedAt: evt.CreatedAt,
	}
	if err := r.events.AppendEvent(ctx, rec); err != nil {
		slog.Warn("tracing: append event failed", "run_id", evt.RunID, "seq", evt.Seq, "err", err)
	}
}

func (r *Recorder) updatePaper(p paperUpdate) {
	if r.runs == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
	defer cancel()
	if err := r.runs.UpdateRunPaper(ctx, r.runID, p.id, p.title); err != nil {
		slog.Warn("tracing: update run paper failed", "run_id", r.runID, "err", err)
	}
}

func (r *Recorder) finalizeRun(rec store.RunRecord) {
	if r.runs == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), persistTimeout)
	defer cancel()
	if err := r.runs.FinalizeRun(ctx, rec); err != nil {
		slog.Warn("tracing: finalize run failed", "run_id", r.runID, "err", err)
	}
}

// marshalJSON encodes a summary/payload map to JSONB bytes, or nil (SQL NULL)
// for an empty map. A marshal error degrades to NULL rather than failing a write.
func marshalJSON(m map[string]any) json.RawMessage {
	if len(m) == 0 {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return b
}
