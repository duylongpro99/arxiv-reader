# Phase 05 — API: GET /resources + POST /discover {resourceId, values}

**Priority:** High · **Status:** pending · **Depends on:** phase-04

## Overview

Expose the registry over HTTP: a new `GET /resources` returning descriptors +
field schema, and a generalized `POST /discover` that accepts `{resourceId,
values}`, validates `values` against the selected resource's schema server-side,
and defaults to `arxiv` for back-compat. Replace the arXiv-specific
`GET /categories`. All other endpoints are untouched.

## Files to Modify

- `backend/internal/orchestrator/orchestrator.go`
  - `HandleCategories` (222) → **`HandleResources`**: `writeJSON(200, o.registry.Descriptors())`.
    (Delete the arXiv-specific `categoriesResponse`.)
  - `HandleDiscover` (154) + `parseDiscoverQuery` (190) → **schema-driven**:
    - New body `discoverRequest{ ResourceID string `json:"resourceId"`; Values map[string]string `json:"values"` }`.
    - Resolve resource: `resourceId` (or default `"arxiv"` when empty →
      back-compat). Unknown id → 400.
    - **Back-compat shim:** if `Values` is nil but legacy `{category,terms}` keys
      are present in the raw body, fold them into `Values`. (Existing clients keep
      working through the migration.)
    - `validateValues(descriptor, values)`: required fields present; `select`
      value ∈ catalog options (generalizes `IsValid`); `text` run through the
      field's named sanitizer; apply defaults for omitted optionals. Unknown
      field key → 400. Return the validated `map[string]string`.
    - `newSession(resourceID, validatedValues)` → detach `runDiscovery` as today.
- `backend/internal/orchestrator/dto.go`
  - Remove `categoriesResponse`; the descriptor DTO is `resource.Descriptor`
    (already JSON-tagged) — serve it directly or add a thin `ResourceDTO` if field
    renaming is needed for the frontend contract in `frontend/lib/types.ts`.
- `backend/internal/server/server.go` — route `GET /categories` → `GET /resources`;
  keep `POST /discover`, `POST /discover/{sessionId}/more` paths unchanged.
- `backend/internal/orchestrator/runs-handlers.go` / `discover-more` — no change
  (reads session's resourceID+values from Phase 04).

## Files to Create

- `backend/internal/orchestrator/discover-validate.go` — `validateValues` +
  sanitizer application (keeps `orchestrator.go` small per <200-line rule).

## Tests

- `orchestrator_test.go` / new `discover-validate_test.go`:
  - `GET /resources` returns arxiv descriptor with category(select)+terms(text).
  - `POST /discover {resourceId:"arxiv", values:{category:"cs.LG", terms:"x"}}` → 200 + session.
  - empty body → defaults to arxiv + default category (back-compat).
  - legacy `{category,terms}` body (no resourceId/values) → folded, 200.
  - unknown resourceId → 400; off-catalog category → 400; unknown field key → 400.
  - text value with `OR`/quotes → sanitized before reaching the query.

## Red Team Fixes (2026-07-18) — applied

- **F7 (H3) — symmetric compat:** do NOT hard-remove `GET /categories`. Keep it as a
  temporary alias (returns the arxiv descriptor, or 301 → `/resources`) through Phase 06;
  removal happens in Phase 07 after the frontend cuts over. Otherwise the deployed frontend
  404s in the 05→06 window.
- **F17 (M2) — shim mechanism (a typed decode silently drops top-level keys):** retain the raw
  body (`json.RawMessage` or a second decode into a map); decode legacy `category`/`terms`
  strictly as `string` (type mismatch → 400); **fold BEFORE `validateValues`** so folded values
  get the whitelist + sanitizer. Add a test that posts the EXACT current `triggerDiscovery`
  body shape (`{category, terms}`) to the Phase-05 handler and expects 200.
- **F21 (M6):** `validateValues` at the handler is an early-reject convenience; the
  authoritative validation/sanitization gate is engine-level (Phase 02 F21).

## Todo

- [ ] HandleResources (replaces HandleCategories)
- [ ] discoverRequest {resourceId, values} + back-compat shim
- [ ] validateValues (select whitelist + text sanitizer + defaults) in discover-validate.go
- [ ] server route swap /categories → /resources
- [ ] handler tests (incl. back-compat + 400 paths)
- [ ] `go build ./... && go test ./internal/orchestrator/... ./internal/server/...`

## Success Criteria

- UI can enumerate resources + fields from `GET /resources`.
- `POST /discover` validates against the resource schema; legacy bodies still work.
- Validation is server-side and reuses the same catalog/sanitizer the engine trusts.

## Risks

- Contract drift with `frontend/lib/types.ts` — define the descriptor JSON shape
  once and mirror it in Phase 06.
- Back-compat shim is temporary — note it for removal once the frontend ships.
