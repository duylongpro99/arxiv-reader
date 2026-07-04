# Phase 05 — Frontend (UI + polling)

**Context:** `docs/phase2/prd.md` §2.1, §2.2, §4 · `docs/phase2/brainstorm-summary.md`
**Priority:** High · **Status:** pending · **Depends on:** 04 (API contract) · **Effort:** ~L

## Overview
One button → discovery → live progress → candidate cards. Next.js API routes proxy the Go
backend (keep `localhost:8080` out of the browser). TanStack Query polls `/api/status` every
2s until `selection`/`failed`. TanStack Query is new (deferred from Phase 1) → install here.

## Key insights
- Trigger returns `{session_id}` only; candidates arrive via the FIRST/subsequent status poll.
- Stage label map: `discovery → "Connecting to arXiv…"`, `selection → "Ready — select a paper"`,
  `failed → error banner`. No "Filtering…" label (local filter is instant).
- Poll `refetchInterval` returns `false` on `selection`/`failed` to stop.

## Requirements (PRD F1, F4, F5, F6)
- `TriggerButton` disabled + spinner while running.
- `PaperCard`: full title, all authors (comma-joined), abstract first 300 chars + ellipsis,
  human date ("June 7, 2026"), arXiv ID badge. "Select" button inactive (Phase 3 wires it).
- `ProgressIndicator` shows stage label; `ErrorBanner` shows message + retry when recoverable.
- `Notice` (fewer-than-5) rendered above the list.

## Related code files
**Create:**
- `frontend/lib/types.ts` — `Paper`, `PipelineStatus`, `PipelineStage`.
- `frontend/lib/api.ts` — `triggerDiscovery()`, `fetchStatus(sessionId)`.
- `frontend/app/api/trigger/route.ts` — `POST` → Go `POST /discover`.
- `frontend/app/api/status/route.ts` — `GET ?sessionId=` → Go `GET /status/{id}`.
- `frontend/app/providers.tsx` — `QueryClientProvider` (client component).
- `frontend/components/discovery-panel.tsx` — orchestrates trigger + poll + render.
- `frontend/components/trigger-button.tsx`
- `frontend/components/progress-indicator.tsx`
- `frontend/components/candidate-list.tsx` + `frontend/components/paper-card.tsx`
- `frontend/components/error-banner.tsx`
**Modify:**
- `frontend/app/layout.tsx` — wrap children in `<Providers>`.
- `frontend/app/page.tsx` — render `<DiscoveryPanel />`.
- `frontend/package.json` — add `@tanstack/react-query` (pin resolved version in lockfile).

## Design detail
```ts
// lib/types.ts
export interface Paper { id: string; title: string; authors: string[];
  abstract: string; pdfUrl: string; published: string }
export type PipelineStage = 'discovery' | 'selection' | 'failed'
  | 'fetching_pdf' | 'generating' | 'reviewing' | 'revising' | 'writing' | 'complete'
export interface PipelineStatus { stage: PipelineStage; candidates?: Paper[];
  notice?: string; error?: string; recoverable?: boolean }
```
```ts
// discovery-panel.tsx (sketch)
const trigger = useMutation({ mutationFn: triggerDiscovery,
  onSuccess: ({ session_id }) => setSessionId(session_id) })
const { data: status } = useQuery({
  queryKey: ['status', sessionId],
  queryFn: () => fetchStatus(sessionId!),
  enabled: !!sessionId,
  refetchInterval: (q) => {
    const s = q.state.data?.stage
    return s === 'selection' || s === 'failed' ? false : 2000
  },
})
```
Stage label + date helpers:
```ts
const STAGE_LABEL: Partial<Record<PipelineStage,string>> = {
  discovery: 'Connecting to arXiv…', selection: 'Ready — select a paper',
}
const fmtDate = (iso: string) =>
  new Date(iso).toLocaleDateString('en-US',{year:'numeric',month:'long',day:'numeric'})
const snippet = (a: string) => a.length > 300 ? a.slice(0,300)+'…' : a
```
> **Proxy routes read backend URL from env** (`process.env.BACKEND_URL ?? 'http://localhost:8080'`)
> server-side only — never shipped to the client bundle.

## Implementation steps
1. `npm i @tanstack/react-query` in `frontend/`; commit lockfile.
2. `providers.tsx` + wrap in `layout.tsx`.
3. `lib/types.ts`, `lib/api.ts`.
4. Proxy routes `/api/trigger`, `/api/status`.
5. Components: trigger, progress, card, list, error-banner, panel.
6. `page.tsx` → `<DiscoveryPanel />`.
7. `npm run build` (or `lint`) clean; manual click-through against running backend.

## Todo
- [ ] install TanStack Query + provider wiring
- [ ] `lib/types.ts` + `lib/api.ts`
- [ ] `/api/trigger` + `/api/status` proxy routes (backend URL server-side)
- [ ] components (trigger/progress/card/list/error)
- [ ] `page.tsx` composition + stage-label/date/snippet helpers
- [ ] `npm run build` clean; manual E2E click-through

## Success criteria
- Click → spinner + "Connecting to arXiv…" → cards render on `selection`.
- Card shows all required fields, 300-char abstract, human date, ID badge.
- Fewer-than-5 shows notice; failure shows banner + retry (when recoverable).
- Polling stops at `selection`/`failed` (verify in network tab).

## Risks
- Type drift between Go DTO and TS `Paper` — camelCase must match (Phase 01 tags). Verify field-by-field.
- Next 16 / React 19 App Router: providers must be a `'use client'` component.

## Security (PRD §2.2)
- Backend URL never in client bundle — only in server-side route handlers.
