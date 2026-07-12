# Phase 02 — Tracing Core

## Context Links
- Design: `docs/superpowers/specs/2026-07-12-run-timeline-tracing-design.md` (§3, §4, §8)
- Depends on: Phase 01 (`internal/store`)
- Consumers next phase: `internal/orchestrator`

## Overview
- **Priority:** High
- **Status:** ✅ complete
- Build `internal/tracing`: the event model, the `Recorder` (dual-write: in-memory ring buffer +
  async Postgres), the per-run SSE `Broker`, and the secret `scrub`ber. This phase produces the
  seam the orchestrator will call in Phase 03. No orchestrator changes yet.

## Key Insights
- The `Recorder` owns the **per-run monotonic `seq`** — single writer per run (the pipeline
  goroutine) plus the selection emit from the HTTP handler; guard `seq` + buffer with a mutex.
- Persistence is **async and best-effort**: a bounded worker (or `go` per append with a buffered
  channel) writes to Postgres; failures log `warn` and are dropped, never blocking `Emit`.
- The ring buffer bounds memory (`buffer_size`, default 256). Replay for SSE reconnect reads from
  it; a client asking for a `seq` older than the buffer window falls back to the DB (Phase 04).
- Keep files < 200 lines; split by responsibility.

## Requirements
**Functional**
- `Event` value type with the design §4 fields; a `Kind`/`Status` enum set.
- `Recorder.Emit(evt)` assigns `seq`, timestamps, scrubs, appends to buffer, notifies broker,
  and schedules a DB append.
- `Recorder.Snapshot(sinceSeq)` returns buffered events with `seq > sinceSeq` (for SSE replay).
- `Broker.Subscribe(runID) (<-chan Event, cancel)` / fan-out on emit; `Close(runID)` on run end.
- `scrub(v any) any` redacts API-key/secret-looking strings before persistence/streaming.
- A `NoopRecorder` for when tracing is disabled or the store is unavailable (in-memory only still
  works because the broker/buffer are independent of the store).

**Non-functional**
- `Emit` is non-blocking and safe from multiple goroutines.
- Zero secret leakage: scrub runs on both `summary` and `payload_full`.

## Architecture
```
internal/tracing/
  ├── event.go     Event, EventKind consts, Status consts, helper constructors
  ├── recorder.go  Recorder: Emit, Snapshot(sinceSeq), Close; holds ring buffer + seq + mu
  ├── broker.go    Broker: Subscribe/Publish/Close; map[runID][]chan Event (mu-guarded)
  ├── scrub.go     scrub(): regex redaction of key/secret patterns + size caps on previews
  ├── tracer.go    Tracer facade: NewRecorder(runID) / Recorder(runID) lookup; owns Broker + store refs
  └── *_test.go
```
- **Tracer** is the single object the orchestrator holds. It wires: the `store` (Phase 01, may be
  nil → skip DB), the `Broker`, and config (`full_payloads`, `buffer_size`). `NewRecorder(runID,
  paper*)` creates the DB `runs` row (best-effort) and returns a `*Recorder`; `Recorder(runID)`
  returns the existing one (handlers + goroutines share it).
- Consumer-side store interfaces declared here (narrow): `runWriter`, `eventWriter` — satisfied by
  `*store.Store`. Passing `nil` yields a DB-less recorder.

## Data Flow (one Emit)
```
Emit(evt)
  → mu.Lock: evt.Seq = next++; evt.CreatedAt = now
  → evt.Summary = scrub(evt.Summary); evt.PayloadFull = maybe(full_payloads) ? scrub(full) : nil
  → ring.push(evt); mu.Unlock
  → broker.Publish(runID, evt)          // live SSE
  → select { persistCh <- evt } default { drop+warn }   // async DB, best-effort
```

## Related Code Files
**Create**
- `backend/internal/tracing/event.go`, `recorder.go`, `broker.go`, `scrub.go`, `tracer.go`
- `backend/internal/tracing/recorder_test.go` (seq ordering, buffer eviction, scrub, snapshot)
- `backend/internal/tracing/broker_test.go` (subscribe/publish/close, no-goroutine-leak)

**Modify**
- none in orchestrator yet (Phase 03).

## Implementation Steps
1. `event.go`: define `Event`, `EventKind` (the design §4 taxonomy as typed consts), `Status`.
   Add tiny constructors, e.g. `ToolCompleted(kind, title, summary)`.
2. `scrub.go`: redact `sk-…`, `Bearer …`, generic 32+ hex/base64 secrets, and any config API-key
   value; truncate string previews to ~500 chars. Unit-test known secret shapes.
3. `recorder.go`: ring buffer (slice + head index or container/ring), `seq` counter, mutex;
   `Emit`, `Snapshot(sinceSeq)`, `Close`; async persist worker draining a buffered channel.
4. `broker.go`: per-run subscriber registry with buffered channels; drop-oldest or block-with-
   timeout policy documented; `Close` signals subscribers to end the SSE stream.
5. `tracer.go`: facade holding `Broker` + optional store + config; `recorders sync.Map`.
6. Tests as above; all race-clean (`go test -race`).

## Todo List
- [ ] `event.go` — Event, kinds, statuses, constructors
- [ ] `scrub.go` + tests (secret redaction + preview truncation)
- [ ] `recorder.go` — seq, ring buffer, Emit, Snapshot, async persist, Close
- [ ] `broker.go` — subscribe/publish/close, buffered, leak-free
- [ ] `tracer.go` — facade + recorder registry
- [ ] Unit tests race-clean
- [ ] `go test -race ./...` green

## Success Criteria
- Emitting N events yields strictly increasing `seq` 0..N-1 under concurrent callers.
- `Snapshot(sinceSeq)` returns exactly the newer events; buffer never exceeds `buffer_size`.
- A subscriber receives live events; `Close` ends its channel.
- Scrub redacts a planted API key in both summary and full payload.
- With a nil store, everything above still works (in-memory only).

## Risk Assessment
- **Broker backpressure** (slow SSE client) — mitigate with buffered channels + drop policy; a
  dropped live event is still recoverable via DB/`Snapshot` on reconnect.
- **Goroutine leak** on unclosed subscribers — `Close(runID)` on run terminal event + context
  cancel on SSE handler (Phase 04).

## Security Considerations
- Scrubbing is mandatory on the persistence + stream path; `full_payloads` default off.
- No raw HTML enters events (only markdown byte size + preview) — enforced by callers in Phase 03,
  documented here as an invariant.

## Next Steps
- Phase 03 wires the Tracer into the orchestrator and emits real events.
