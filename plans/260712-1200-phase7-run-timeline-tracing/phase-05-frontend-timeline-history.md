# Phase 05 — Frontend (Timeline + History)

## Context Links
- Design: `docs/superpowers/specs/2026-07-12-run-timeline-tracing-design.md` (§7)
- Depends on: Phase 04 (SSE + REST live)
- Files: `frontend/lib/types.ts`, `frontend/components/*`, `frontend/app/*`

## Overview
- **Priority:** High
- **Status:** ✅ complete
- Render the live, ordered timeline during a run via SSE, and a `/runs` history page to browse and
  reopen past runs. Augments (does not remove) the existing `ProgressIndicator`.

## Key Insights
- Live tail: a small `useEventSource(runId)` hook (native `EventSource`) accumulates events into an
  ordered array keyed by `seq` (dedupe on reconnect replay). On terminal event, close the source.
- Reopen a finished/old run: fetch `GET /api/runs/:id` (persisted) instead of SSE.
- Types must mirror the Go DTOs exactly (camelCase) — same discipline as the rest of `lib/types.ts`.
- Match existing Tailwind/dark-mode styling and component conventions (see `progress-indicator.tsx`,
  `result-panel.tsx`).

## Requirements
**Functional**
- `<RunTimeline runId>` — ordered event rows: per-`status` icon + color, `title`, relative time,
  `duration_ms`; expandable row reveals the `summary` preview (and `payload_full` when present).
- Live mode subscribes via `useEventSource`; on completion falls back to the persisted run for a
  stable final view.
- `/runs` page — list of past runs (paper title, date, outcome badge, cost); row click → run detail
  view (`<RunTimeline>` + existing result preview if complete).
- Wire `<RunTimeline>` into the active run view (in/under `discovery-panel.tsx`) so the story shows
  live during processing.

**Non-functional**
- No EventSource leak: close on unmount + on terminal event.
- Accessible: list semantics, aria on status icons, keyboard-expandable rows.

## Architecture
```
frontend/
  lib/types.ts            + TimelineEvent, RunSummary, RunDetail (mirror Go DTOs)
  lib/use-event-source.ts   useEventSource(runId): {events, done, error}
  lib/use-runs.ts           useRuns() + useRun(id) (TanStack Query over /api/runs)
  components/run-timeline.tsx     <RunTimeline> (presentational; takes events[])
  components/run-event-row.tsx    single expandable row
  components/runs-history.tsx     history list
  app/runs/page.tsx               /runs route (list)
  app/runs/[id]/page.tsx          reopen a single run
```
- Keep `<RunTimeline>` presentational (events in, render out) so both live (hook-fed) and history
  (query-fed) modes reuse it.
- EventSource connects to the backend directly (`NEXT_PUBLIC_BACKEND_URL` or the known
  `http://localhost:8080`); REST goes through `/api/runs*` proxies (Phase 04).

## Related Code Files
**Create**
- `frontend/lib/use-event-source.ts`, `frontend/lib/use-runs.ts`
- `frontend/components/run-timeline.tsx`, `run-event-row.tsx`, `runs-history.tsx`
- `frontend/app/runs/page.tsx`, `frontend/app/runs/[id]/page.tsx`

**Modify**
- `frontend/lib/types.ts` — add `TimelineEvent`, `RunSummary`, `RunDetail`, `RunStatus`, `EventStatus`.
- `frontend/components/discovery-panel.tsx` — mount `<RunTimeline>` for the active run.
- `frontend/app/page.tsx` or layout — add a link/nav to `/runs` history.

## Implementation Steps
1. Add the mirrored types to `lib/types.ts`.
2. `useEventSource`: open on `runId`, parse `event`/`data`, append+dedupe by `seq`, set `done` on
   terminal event, cleanup on unmount.
3. `run-event-row.tsx` + `run-timeline.tsx`: icon/color map by `EventStatus`, relative timestamps,
   expandable summary/payload.
4. Integrate `<RunTimeline>` into the active-run view; keep `ProgressIndicator` as the compact
   headline.
5. `use-runs.ts` + `runs-history.tsx` + `/runs` pages; row → detail (reuse `<RunTimeline>` and, if
   complete, the existing result preview).
6. Nav link to `/runs`.
7. `npm run build` + lint green; manual smoke against a live backend.

## Todo List
- [ ] Mirrored DTO types in `lib/types.ts`
- [ ] `useEventSource` hook (dedupe, terminal-close, cleanup)
- [ ] `<RunEventRow>` + `<RunTimeline>` (icons/colors/expand)
- [ ] Mount live timeline in `discovery-panel`
- [ ] `useRuns`/`useRun` + `<RunsHistory>` + `/runs` + `/runs/[id]` pages
- [ ] Nav link to history
- [ ] `npm run build` + lint green; manual smoke

## Success Criteria
- During a run the timeline updates live (≤ ~1s per step) and reads as a coherent story.
- After completion the final timeline is stable (persisted-backed).
- `/runs` lists past runs; clicking one reopens its full timeline (+ result if complete).
- No console errors; EventSource closes cleanly.

## Risk Assessment
- **Duplicate events on replay+live overlap** — dedupe by `seq` (idempotent merge).
- **EventSource unsupported edge** — acceptable for a local dev tool (all modern browsers support it).

## Security Considerations
- Render text as text (no `dangerouslySetInnerHTML` for summaries/payloads).
- Payloads already scrubbed server-side; the UI adds no new exposure.

## Next Steps
- Phase 06: end-to-end wiring test + docs + manual cross-run validation.
