package tracing

import (
	"context"
	"sync"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/store"
)

// EventWriter / RunWriter are the narrow, consumer-side store contracts the
// recorder needs. *store.Store satisfies both structurally; passing a nil
// interface (DB unavailable) yields a DB-less tracer that still streams live.
type EventWriter interface {
	AppendEvent(ctx context.Context, e store.EventRecord) error
}

type RunWriter interface {
	CreateRun(ctx context.Context, r store.RunRecord) error
	UpdateRunPaper(ctx context.Context, id, paperID, paperTitle string) error
	FinalizeRun(ctx context.Context, r store.RunRecord) error
}

// Tracer is the single object the orchestrator holds. It owns the SSE Broker,
// the (optional) store writers, the scrubber, and the per-run recorder registry.
type Tracer struct {
	enabled bool
	broker  *Broker
	events  EventWriter
	runs    RunWriter
	cfg     recorderConfig
	scrub   *scrubber

	recorders sync.Map // runID → *Recorder
}

// New builds a Tracer. ev/rw may be nil (no DB → in-memory-only tracing).
// bufferSize <= 0 falls back to 256. secrets are exact strings redacted from
// every event (the LLM API key and the DB DSN — defence-in-depth). When enabled
// is false, NewRecorder returns nil and all recording no-ops.
func New(enabled bool, ev EventWriter, rw RunWriter, fullPayloads bool, bufferSize int, secrets ...string) *Tracer {
	if bufferSize <= 0 {
		bufferSize = 256
	}
	return &Tracer{
		enabled: enabled,
		broker:  NewBroker(),
		events:  ev,
		runs:    rw,
		cfg:     recorderConfig{fullPayloads: fullPayloads, bufferSize: bufferSize},
		scrub:   newScrubber(secrets...),
	}
}

// Broker exposes the SSE fan-out for the transport layer (Phase 04).
func (t *Tracer) Broker() *Broker { return t.broker }

// NewRecorder creates (and registers) the recorder for a run and best-effort
// inserts its header row. Returns nil when tracing is disabled — callers rely on
// the Recorder methods being nil-safe. Idempotent per runID: an existing
// recorder is returned as-is (a retry reuses the same session/run).
func (t *Tracer) NewRecorder(runID string, startedAt time.Time) *Recorder {
	if t == nil || !t.enabled {
		return nil
	}
	if existing, ok := t.recorders.Load(runID); ok {
		return existing.(*Recorder)
	}
	r := newRecorder(runID, startedAt, t.cfg, t.scrub, t.broker, t.events, t.runs)
	// LoadOrStore guards against a concurrent creator: if we lost the race, drop
	// ours un-started (no worker goroutine leaked) and use the winner.
	actual, loaded := t.recorders.LoadOrStore(runID, r)
	if loaded {
		return actual.(*Recorder)
	}
	// When a DB is present, evict the recorder from the registry on Close so
	// finished runs don't accumulate in memory (a reconnect then replays from
	// Postgres). Without a DB the ring buffer is the only copy — keep it.
	if t.events != nil {
		r.evict = func() { t.recorders.Delete(runID) }
	}
	// The persist worker creates the run-header row as its FIRST action, before
	// draining any event/update — so run_events can never reference a
	// not-yet-committed run (no FK violation, no dropped opening events).
	r.start()
	return r
}

// Recorder returns the live recorder for a run, or nil if none exists: never
// created, tracing disabled, a completed run evicted after Close (DB-backed
// mode), or wiped by a server restart. A nil result tells the SSE handler to
// fall back to the DB for history.
func (t *Tracer) Recorder(runID string) *Recorder {
	if t == nil {
		return nil
	}
	if v, ok := t.recorders.Load(runID); ok {
		return v.(*Recorder)
	}
	return nil
}
