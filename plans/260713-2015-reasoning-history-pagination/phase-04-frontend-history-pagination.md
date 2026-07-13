# Phase 04 — Frontend: History Content + Load More (Features B & C)

**Priority:** High · **Status:** completed · **Wave:** 2 (rebases on phase-03's `types.ts`)
**Owner agent:** frontend (T4)
**Completed:** 2026-07-13
**Depends on:** phase-02 (API), phase-03 (`lib/types.ts` reasoning types landed first).

## Context Links
- Design note: `docs/design-notes/2026-07-13-reasoning-history-pagination.md`
- Backend contract: phase-02 "Interfaces exposed to frontend".

## Problem
- **B:** Opening a history entry (`app/runs/[id]/page.tsx`) shows only stat tiles + timeline — never the generated note.
- **C:** `candidate-list.tsx` shows one page of candidates; no way to fetch older papers.

## Backend contract (from phase-02)
- `GET /runs/{id}/content` → `{ path, available, markdown }`.
- `POST /discover/{sessionId}/more` → `{ candidates, hasMore }`.

## Related code files
**Modify:**
- `frontend/lib/api.ts` — add `getRunContent(id)` and `fetchMoreCandidates(sessionId)`.
- `frontend/lib/use-runs.ts` — add `useRunContent(id)` query hook (mirror `useRun`, `:29-45`). Handle `available:false`.
- `frontend/app/runs/[id]/page.tsx` — render note content in `result-panel` + graceful "file moved/unavailable" state.
- `frontend/components/result-panel.tsx` — accept markdown + render (reuse its existing markdown rendering; if it only renders live results, generalize to accept a markdown string prop).
- `frontend/components/candidate-list.tsx` — "Load more" button.
- `frontend/app/api/*` — add proxy routes: `app/api/runs/[id]/content/route.ts`, and a discover-more proxy (mirror existing `app/api/runs/route.ts` and the trigger/select proxies).

## Design

### Feature B — history content panel
1. `useRunContent(id)` fetches `/api/runs/{id}/content`. States: loading / available (render markdown) / unavailable (`available:false` → "Note file moved or unavailable" empty state, show `path` if present).
2. In `app/runs/[id]/page.tsx`, add a "Generated note" section rendering the markdown via `result-panel`. Place alongside the reasoning timeline (Feature A, phase-03) so history shows **content + reasoning together**.
3. 503 (history unavailable / no DB) → reuse existing `HistoryUnavailableError` handling (`use-runs.ts:8,16-19`).

### Feature C — load more candidates
1. `fetchMoreCandidates(sessionId)` → POST `/api/discover/{sessionId}/more` (proxy → backend).
2. `candidate-list.tsx`: "Load more" button below the list. On click: call, **append** results, **dedup by paper ID** (guard against overlap). Disable + spinner while loading. Hide when `hasMore === false`.
3. Empty result (`candidates:[]`) → inline notice "No older papers found" (arXiv paging can dry up). Do not error.
4. Selection still works: appended papers are in the session's Candidates, so the existing select→`/process` flow is unchanged.

## Implementation steps
1. Wait for phase-03 `types.ts` landing (reasoning types). Pull.
2. `api.ts` client fns + `app/api` proxy routes.
3. `useRunContent` hook.
4. History page content panel + unavailable state.
5. `candidate-list` load-more (append + dedup + hasMore).
6. type-check / build.

## Todo
- [x] `getRunContent` + `fetchMoreCandidates` in `api.ts`
- [x] proxy routes under `app/api` (content + discover-more)
- [x] `useRunContent` hook w/ available:false handling
- [x] history page renders note markdown + unavailable empty state
- [x] `result-panel` accepts markdown string prop (if needed)
- [x] load-more button: append + dedup-by-id + hasMore hide + empty notice
- [x] type-check / build green

## Success criteria
- Opening a history entry with an existing vault file shows the full generated note.
- A run whose file was moved shows a clean "unavailable" state (no crash/blank).
- "Load more" appends older papers; no duplicates; selecting an appended paper processes successfully.
- Button hides when `hasMore=false`; empty fetch shows a notice.
- type-check / build pass.

## Shared-file protocol
`lib/types.ts` — phase-03 lands reasoning types first; this phase rebases and adds any content/pagination types (`RunContent`, `DiscoverMoreResult`). Sequence, don't co-edit.
