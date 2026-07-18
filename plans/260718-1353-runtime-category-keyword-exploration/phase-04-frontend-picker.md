# Phase 04 — Frontend: Category Picker + Keyword Input

**Priority:** Medium · **Status:** pending · **Depends on:** Phase 3

## Overview

Add a cs.* category dropdown (human labels, sourced from `GET /categories`) and
an optional keyword input beside the trigger button. Pass the selection into
discovery. Proxy routes forward to the Go backend (backend address stays
server-side, per existing convention).

## Files to Create

- `frontend/app/api/categories/route.ts` — thin GET proxy → `{backend}/categories`
  (mirror the existing `trigger/route.ts` proxy shape).

## Files to Modify

- `frontend/app/api/trigger/route.ts`
  - `POST()` → accept a request body; read `{ category, terms }` and forward it
    as JSON to `{backend}/discover`. Keep the empty-body path working (forward
    `{}` or the raw body). Set `Content-Type: application/json`.
- `frontend/lib/api.ts`
  - `triggerDiscovery(category: string, terms?: string)` → POST `/api/trigger`
    with `{ category, terms }` body.
  - `fetchCategories(): Promise<Category[]>` → GET `/api/categories`.
- `frontend/lib/types.ts`
  - `interface Category { code: string; label: string }`.
- `frontend/components/category-picker.tsx` (new, <200 lines)
  - Dropdown of categories (labels) + a text input for keywords. Controlled;
    reports `{category, terms}` upward. Loads options via
    `useQuery(['categories'], fetchCategories)`; default selection = first /
    a sensible default (cs.AI) until user changes it.
- `frontend/components/discovery-panel.tsx`
  - Hold `category`/`terms` state (default cs.AI). Render `<CategoryPicker>` near
    `<TriggerButton>`. `start()` → `trigger.mutate({category, terms})`.
  - Update the `trigger` mutation `mutationFn` to accept `{category, terms}`.
- `frontend/components/trigger-button.tsx`
  - No functional change required; optionally disable while category unset
    (category is always defaulted, so likely no change).

## Implementation Steps

1. Add `/api/categories` proxy + forward body in `/api/trigger`.
2. `api.ts`: `fetchCategories`, `triggerDiscovery(category, terms)`.
3. `types.ts`: `Category`.
4. `CategoryPicker` component (dropdown + keyword input).
5. Wire state + picker into `DiscoveryPanel`; pass into trigger.
6. Manual verify: pick cs.CL + "speech", run discovery, confirm papers are from
   cs.CL; "load more" stays in cs.CL.

## Todo

- [ ] `/api/categories` proxy route
- [ ] `/api/trigger` forwards `{category, terms}` body
- [ ] `api.ts`: `fetchCategories` + `triggerDiscovery(category, terms)`
- [ ] `types.ts`: `Category`
- [ ] `CategoryPicker` component
- [ ] wire into `DiscoveryPanel` (state + start)
- [ ] `npm run build` (frontend) green

## Success Criteria

- Dropdown lists cs.* categories with readable labels.
- Selecting a category (+ optional keywords) changes discovery results.
- "Load more" respects the same category+terms.
- Empty keyword field → category-only query (no regression).

## Notes

- Follow `frontend/AGENTS.md`: this Next.js has breaking changes — read
  `node_modules/next/dist/docs/` before writing route/component code.
- Keep the picker minimal (KISS): a native `<select>` + `<input>` is sufficient;
  no new dependency.
