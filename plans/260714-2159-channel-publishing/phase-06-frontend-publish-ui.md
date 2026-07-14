# Phase 06 — Frontend publish UI

**Context:** design-note · `plan.md` · consumes P3 endpoints · mirrors `frontend/app/runs/[id]`, `lib/use-runs.ts`, `lib/api.ts`
**Priority:** High · **Status:** complete · **Depends on:** P3 contracts
**Wave:** 4

## Overview
Publishing reachable from the run history detail page: pick channels → generate drafts → edit + approve each → publish → see external URLs. Matches the existing dark-instrument/reader design language.

## Flow / components
1. **Entry:** "Publish" action on `app/runs/[id]/page.tsx` (run detail). Disabled with tooltip if publishing unavailable (backend 503 / no DB).
2. **Channel picker** (`publish-channel-picker.tsx`): multiselect from `GET /channels` (shows id + category). "Generate drafts" → `POST /runs/{id}/publications`.
3. **Draft editor** (`publish-draft-panel.tsx`): one card per channel/draft with status badge (draft/approved/published/failed).
   - dev.to (`longform`): markdown editor + rendered preview (reuse existing markdown renderer from history content panel).
   - X (`brief`): plain editor + **live thread preview** (client mirrors the ≤280 chunking so the user sees tweet boundaries before publish; backend remains source of truth).
   - Edit → `PATCH /publications/{pid}` (debounced); "Approve" toggles status.
4. **Publish:** per-card "Publish" (enabled once approved) → `POST /publications/{pid}/publish`. On success show external URL (link out); on failure show scrubbed error + retry.

## Data
- `lib/types.ts`: `Channel`, `Publication`, request/response shapes (match P3 `dto.go`).
- `lib/use-publications.ts`: fetch/generate/patch/publish hooks (mirror `use-runs.ts`).
- `lib/api.ts` + `app/api/*` proxy routes for the new endpoints (mirror existing proxy pattern).

## Files
- Create: `frontend/components/publish-channel-picker.tsx`, `publish-draft-panel.tsx`, `publish-draft-card.tsx`, `frontend/lib/use-publications.ts`, proxy routes under `frontend/app/api/`
- Modify: `frontend/app/runs/[id]/page.tsx`, `frontend/lib/{api.ts,types.ts}`

## Todo
- [x] types matching P3 DTOs
- [x] proxy routes + `api.ts` methods
- [x] `use-publications` hooks
- [x] channel picker + generate
- [x] draft cards (markdown preview / thread preview by category)
- [x] edit (PATCH debounced) + approve + publish + URL/error display
- [x] unavailable state (503) handled gracefully
- [x] `npm run build` / `tsc` green

## Success criteria
From a run: pick dev.to + X → generate → edit both → approve → publish → both cards show live external URLs. Thread preview matches backend chunking. Publishing hidden/disabled cleanly when backend reports unavailable.

## Security
No tokens client-side; all channel calls go through the backend. Errors shown are the scrubbed backend messages.
