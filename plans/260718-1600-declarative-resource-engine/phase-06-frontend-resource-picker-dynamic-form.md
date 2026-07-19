# Phase 06 — Frontend: ResourcePicker + DynamicRequestForm

**Priority:** High · **Status:** pending · **Depends on:** phase-05

## Overview

Make the UI resource-driven: a first step to pick a resource (from
`GET /resources`), then a **dynamically rendered** request form built from that
resource's field schema (replacing the hardcoded `CategoryPicker`). The rest of
the flow — candidate list, selection, timeline, result — is unchanged.

> `frontend/AGENTS.md`: this Next.js has breaking changes — read the relevant
> guide in `node_modules/next/dist/docs/` before writing code.

## Files to Modify

- `frontend/lib/types.ts` — add `ResourceDescriptor { id, label, description, fields: Field[] }`,
  `Field { name, type: "select"|"text", label, required, default?, options?: {value,label}[] }`.
  Mirror the Phase 05 JSON shape exactly.
- `frontend/lib/api.ts`
  - Add `fetchResources(): Promise<ResourceDescriptor[]>` (`GET /resources`).
  - Change `triggerDiscovery(resourceId, values)` → POST `{resourceId, values}`
    (was `triggerDiscovery(category, terms)`).
- `frontend/app/api/resources/route.ts` — new proxy route (mirror
  `frontend/app/api/categories/route.ts`); remove/redirect the categories route.
- `frontend/app/api/discover/...` / `trigger/route.ts` — forward the new body shape.
- `frontend/components/discovery-panel.tsx`
  - Replace `category`/`terms` state (32–33) with `resourceId: string` +
    `values: Record<string,string>`.
  - Load resources via `useQuery(["resources"], fetchResources)`; auto-select the
    first resource; seed `values` from each field's `default`.
  - `trigger.mutationFn` → `triggerDiscovery(resourceId, values)` (42).
  - Render `<ResourcePicker>` (if >1 resource) + `<DynamicRequestForm>` in place
    of `<CategoryPicker>` (133). Everything below (CandidateList, RunTimeline,
    ResultPanel, ErrorBanner) unchanged.

## Files to Create

- `frontend/components/resource-picker.tsx` — dropdown of resources
  (`{label}` + `{description}` hint); `onChange(resourceId)` resets `values` to
  the new resource's defaults. Hidden when only one resource exists.
- `frontend/components/dynamic-request-form.tsx` — renders `fields` by type:
  - `select` → dropdown from `options` (+ default preselected),
  - `text` → text input (optional placeholder from label).
  - Emits `onChange(name, value)`; `disabled` while loading. This is the
    self-describing UI — no field is hardcoded.

## Files to Delete

- `frontend/components/category-picker.tsx` (replaced by DynamicRequestForm)
- `frontend/app/api/categories/route.ts` (replaced by resources route)

## Tests / Verification

- `DynamicRequestForm` renders a select+text from a descriptor fixture; defaults
  preselected; emits changes.
- `ResourcePicker` lists resources; switching resets values.
- Manual: `npm run build` + click-through — pick arxiv → category defaults →
  optional keywords → fetch → identical candidate/selection/result behavior.

## Red Team Fixes (2026-07-18) — applied

- **F7 (H3):** this phase migrates the frontend off `GET /categories` onto `GET /resources`.
  The temporary `/categories` alias added in Phase 05 stays live until this phase ships; its
  removal is deferred to Phase 07 (do NOT remove it here).

## Todo

- [ ] types.ts descriptor/field types
- [ ] api.ts fetchResources + triggerDiscovery(resourceId, values)
- [ ] resources proxy route
- [ ] resource-picker.tsx + dynamic-request-form.tsx
- [ ] discovery-panel.tsx rewire
- [ ] delete category-picker + categories route
- [ ] `npm run lint && npm run build`

## Success Criteria

- User selects resource → sees that resource's declared fields → fetches.
- arXiv end-to-end behaves exactly as before through the new UI.
- Adding a future resource needs **zero frontend changes** (form is schema-driven).

## Risks

- Next.js breaking changes — heed `frontend/AGENTS.md`; read the bundled docs.
- Query-key/caching: reset candidate/session state when the resource changes so a
  stale session from another resource can't render.
