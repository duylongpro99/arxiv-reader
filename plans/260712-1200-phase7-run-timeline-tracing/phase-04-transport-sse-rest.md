# Phase 04 — Transport (SSE + REST)

## Context Links
- Design: `docs/superpowers/specs/2026-07-12-run-timeline-tracing-design.md` (§6)
- Depends on: Phase 03 (events are being emitted)
- Files: `backend/internal/server/server.go`, `backend/internal/orchestrator/*`, `backend/internal/orchestrator/dto.go`

## Overview
- **Priority:** High
- **Status:** pending
- Expose the timeline: an SSE stream for live events (with `Last-Event-ID` replay) and two REST
  endpoints for the history list and a single reopened run. Add matching Next.js proxy routes for
  the REST endpoints; the browser connects the SSE stream directly to the backend.

## Key Insights
- SSE reconnect uses the standard `Last-Event-ID` request header (EventSource sets it
  automatically). On connect: replay `recorder.Snapshot(sinceSeq)`; if the recorder is gone
  (server restart / old run), fall back to `EventStore.List(runID)` from Postgres, then close.
- For a still-running run: replay buffered, then subscribe to the broker for live tail; end the
  stream when a terminal event arrives or the client disconnects (`r.Context().Done()`).
- Keep SSE **direct to backend** (`http://localhost:8080/runs/:id/events`) — CORS already allows
  the `localhost:3000` origin; proxying SSE through Next.js risks buffering. REST list/detail go
  through the existing Next proxy pattern.
- `http.ResponseController.Flush` (Go 1.20+) to flush each SSE frame; set
  `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`.

## Requirements
**Functional**
- `GET /runs/{id}/events` → SSE. Frame per event: `id: {seq}\nevent: {event_type}\ndata: {json}\n\n`.
  Replays from `Last-Event-ID+1` (or 0), then live-tails; closes on terminal or disconnect.
- `GET /runs?limit=&offset=` → JSON `{runs:[{id,paperId,paperTitle,status,estCostUSD,startedAt,completedAt}], total}`.
- `GET /runs/{id}` → JSON `{run:{…}, events:[EventDTO…]}` (full reopen; reads DB).
- Next.js proxy routes for `/runs` and `/runs/:id` (REST only).

**Non-functional**
- SSE handler leaks no goroutines: unsubscribe + return on `ctx.Done()`.
- Endpoints return 404 JSON for unknown run ids (consistent with existing handlers).

## Architecture
```
server.go mux (add):
  GET /runs                → orch.HandleRunsList
  GET /runs/{id}           → orch.HandleRun
  GET /runs/{id}/events    → orch.HandleRunEvents  (SSE)

HandleRunEvents:
  sinceSeq = parseLastEventID(r)
  rec = tracer.Recorder(id)
  if rec != nil:
      write Snapshot(sinceSeq)
      if run not terminal: sub = broker.Subscribe(id); stream until terminal/ctx.Done
  else:                                   // recorder evicted → history mode
      events = eventStore.List(id, sinceSeq); write all; close
```
- Add `EventDTO`/`RunDTO` to `dto.go` (camelCase json tags, mirror later in `lib/types.ts`).
- SSE writer helper in a new `backend/internal/orchestrator/sse.go` (keeps handler file small).

## Related Code Files
**Create**
- `backend/internal/orchestrator/runs-handlers.go` — `HandleRunsList`, `HandleRun`, `HandleRunEvents`.
- `backend/internal/orchestrator/sse.go` — SSE framing + flush helper + `Last-Event-ID` parse.
- `backend/internal/orchestrator/runs-handlers_test.go` — replay-from-Last-Event-ID, terminal
  close, history fallback, 404.
- `frontend/app/api/runs/route.ts`, `frontend/app/api/runs/[id]/route.ts` — Next proxies.

**Modify**
- `backend/internal/server/server.go` — register the three routes.
- `backend/internal/orchestrator/dto.go` — `RunDTO`, `EventDTO`, list response.

## Implementation Steps
1. Define `RunDTO`/`EventDTO`/`RunsListResponse` in `dto.go`.
2. `sse.go`: header setup, `writeEvent(w, rc, evt)`, `parseLastEventID(r)`.
3. `HandleRunEvents`: replay → subscribe → tail → terminal/disconnect close; history fallback
   when the recorder is absent.
4. `HandleRunsList` / `HandleRun`: read via `RunStore`/`EventStore`; JSON out; 404 on miss;
   graceful `503`/empty when DB is unavailable (documented — history simply unavailable).
5. Register routes; extend CORS methods if needed (already `GET`).
6. Next proxy routes mirroring existing `app/api/status/route.ts` style.
7. Tests: httptest-drive the SSE handler with a seeded recorder; assert frames + resume.

## Todo List
- [ ] DTOs in `dto.go`
- [ ] `sse.go` framing/flush/Last-Event-ID
- [ ] `HandleRunEvents` (replay + live tail + history fallback + close)
- [ ] `HandleRunsList` + `HandleRun`
- [ ] Register routes in `server.go`
- [ ] Next proxies `/api/runs`, `/api/runs/[id]`
- [ ] Handler tests (resume, terminal close, 404, DB-down)
- [ ] `go test -race ./...` + `npm run build` green

## Success Criteria
- `curl -N http://localhost:8080/runs/{id}/events` streams frames live during a run and ends on
  completion.
- Reconnect with `Last-Event-ID: k` replays only `seq > k`.
- After a restart, `GET /runs` lists past runs and `GET /runs/{id}` returns the full timeline.
- With DB down: live SSE still works for an in-flight run; `/runs` reports unavailable cleanly.

## Risk Assessment
- **Proxy buffering of SSE** — avoided by connecting EventSource straight to the backend.
- **Client never disconnects** — bounded by terminal-event close + server read timeouts.

## Security Considerations
- Same-origin CORS unchanged; endpoints are read-only and localhost-bound.
- Events are already scrubbed at emit time — the transport ships them verbatim.

## Next Steps
- Phase 05 renders the stream + history in the UI.
