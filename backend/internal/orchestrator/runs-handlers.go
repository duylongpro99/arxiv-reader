package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/store"
)

// This file holds the Phase 7 read/stream endpoints: the live SSE timeline and
// the two history REST endpoints. All are read-only and localhost-bound.

// RunReader is the narrow, consumer-side history-read contract. *store.Store
// satisfies it; a nil RunReader means history is unavailable (no DB). Declared
// here (not in store) so the orchestrator can be tested with an in-memory fake.
type RunReader interface {
	GetRun(ctx context.Context, id string) (store.RunRecord, error)
	ListRuns(ctx context.Context, limit, offset int) ([]store.RunRecord, int, error)
	ListEvents(ctx context.Context, runID string, sinceSeq int) ([]store.EventRecord, error)
}

// dbReadTimeout bounds a history read so a slow DB can't hang the request.
const dbReadTimeout = 5 * time.Second

// HandleRunEvents streams a run's timeline as SSE. Live runs replay the buffered
// events (from Last-Event-ID) then tail the broker until the run reaches a TRUE
// terminal (recorder Closed → completion / non-recoverable failure) or the client
// disconnects. A RECOVERABLE failure keeps the stream open so a retry's events
// keep flowing on the same connection (design M3 resolution: end on broker Done,
// NOT on a terminal event kind). If the recorder is gone (server restart / old
// run), it falls back to replaying the persisted timeline from Postgres.
func (o *Orchestrator) HandleRunEvents(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sinceSeq := parseSinceSeq(r)
	rec := o.tracer.Recorder(id)

	if rec == nil {
		// No live recorder: run finished + evicted, or lost to a restart, or never
		// existed here → replay from the DB (or 404/503).
		o.streamHistory(w, r, id, sinceSeq)
		return
	}

	// Subscribe BEFORE snapshotting so an event emitted in the gap is not missed;
	// dedupe by seq (an overlapping event appears in both the snapshot and the
	// live channel — we write it once).
	sub := o.tracer.Broker().Subscribe(id)
	defer sub.Cancel()

	setSSEHeaders(w)
	rc := http.NewResponseController(w)
	lastWritten := sinceSeq
	for _, evt := range rec.Snapshot(sinceSeq) {
		if writeEventDTO(w, rc, eventDTOFromEvent(evt)) != nil {
			return
		}
		lastWritten = evt.Seq
	}
	// A finished run has nothing more to stream — replayed, done.
	if rec.IsTerminal() {
		return
	}

	for {
		select {
		case evt := <-sub.Events:
			if evt.Seq <= lastWritten {
				continue // dedupe replay/live overlap
			}
			if writeEventDTO(w, rc, eventDTOFromEvent(evt)) != nil {
				return // client gone
			}
			lastWritten = evt.Seq
		case <-sub.Done:
			return // recorder Closed → true terminal
		case <-r.Context().Done():
			return // client disconnected
		}
	}
}

// streamHistory replays a finished run's persisted timeline over SSE, then ends.
// Used when the in-memory recorder is gone (restart / evicted). Returns 503 when
// history is unavailable (no DB) and 404 for an unknown run.
func (o *Orchestrator) streamHistory(w http.ResponseWriter, r *http.Request, id string, sinceSeq int) {
	if o.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "run history is unavailable (no database configured)"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dbReadTimeout)
	defer cancel()

	if _, err := o.store.GetRun(ctx, id); err != nil {
		if errors.Is(err, store.ErrRunNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "run not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read run"})
		return
	}
	events, err := o.store.ListEvents(ctx, id, sinceSeq)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read events"})
		return
	}
	setSSEHeaders(w)
	rc := http.NewResponseController(w)
	sawTerminal := false
	lastSeq := sinceSeq
	for _, e := range events {
		if writeEventDTO(w, rc, eventDTOFromRecord(e)) != nil {
			return
		}
		sawTerminal = sawTerminal || isClientTerminalEvent(e)
		lastSeq = e.Seq
	}
	// streamHistory only runs when there is NO live recorder, so nothing more will
	// ever stream. If the persisted timeline lacks a client-recognized terminal
	// (an orphaned run whose process died mid-flight, or an abandoned recoverable
	// failure), emit a synthetic terminal so the browser's EventSource stops
	// reconnecting instead of polling this endpoint forever (~every 3s).
	if !sawTerminal {
		_ = writeEventDTO(w, rc, syntheticTerminal(lastSeq+1))
	}
}

// isClientTerminalEvent reports whether an event ends the client's stream: a
// completion, or a NON-recoverable failure (matches the frontend's
// isStreamTerminal — a recoverable failure keeps the stream open for a retry).
func isClientTerminalEvent(e store.EventRecord) bool {
	if e.EventType == "run.completed" {
		return true
	}
	if e.EventType == "run.failed" {
		var s struct {
			Recoverable bool `json:"recoverable"`
		}
		if json.Unmarshal(e.Summary, &s) == nil {
			return !s.Recoverable
		}
	}
	return false
}

// syntheticTerminal is the closing marker for a history replay of a run that
// never reached a real terminal. recoverable:false makes the client treat it as
// terminal and close the connection.
func syntheticTerminal(seq int) EventDTO {
	return EventDTO{
		Seq: seq, Type: "run.failed", Stage: "failed", Status: "error",
		Title:   "Run interrupted — no live stream (reopen from history)",
		Summary: json.RawMessage(`{"recoverable":false,"synthetic":true}`),
	}
}

// HandleRunsList returns the paginated run history, newest first.
func (o *Orchestrator) HandleRunsList(w http.ResponseWriter, r *http.Request) {
	if o.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "run history is unavailable (no database configured)"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dbReadTimeout)
	defer cancel()

	limit := queryInt(r, "limit", 50)
	offset := queryInt(r, "offset", 0)
	runs, total, err := o.store.ListRuns(ctx, limit, offset)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read run history"})
		return
	}
	dtos := make([]RunDTO, 0, len(runs))
	for _, run := range runs {
		dtos = append(dtos, runDTOFromRecord(run))
	}
	writeJSON(w, http.StatusOK, RunsListResponse{Runs: dtos, Total: total})
}

// HandleRun reopens one past run: its header + full persisted timeline.
func (o *Orchestrator) HandleRun(w http.ResponseWriter, r *http.Request) {
	if o.store == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "run history is unavailable (no database configured)"})
		return
	}
	id := r.PathValue("id")
	ctx, cancel := context.WithTimeout(r.Context(), dbReadTimeout)
	defer cancel()

	run, err := o.store.GetRun(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrRunNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "run not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read run"})
		return
	}
	events, err := o.store.ListEvents(ctx, id, -1)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "cannot read events"})
		return
	}
	dtos := make([]EventDTO, 0, len(events))
	for _, e := range events {
		dtos = append(dtos, eventDTOFromRecord(e))
	}
	writeJSON(w, http.StatusOK, RunDetailResponse{Run: runDTOFromRecord(run), Events: dtos})
}

// queryInt reads a non-negative integer query param, falling back to def on
// absence or a parse error.
func queryInt(r *http.Request, key string, def int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return def
	}
	return n
}
