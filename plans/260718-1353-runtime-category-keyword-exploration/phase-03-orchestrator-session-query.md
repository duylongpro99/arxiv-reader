# Phase 03 — Orchestrator: Trigger Body, Session Query, /categories

**Priority:** High · **Status:** pending · **Depends on:** Phase 1, 2

## Overview

Accept `{category, terms}` on `POST /discover`, validate, store the resulting
`arxivquery.Query` on the session, and make both `runDiscovery` and
`HandleDiscoverMore` read it. Add `GET /categories` for the UI. Retry reuses the
stored query automatically (same session).

## Files to Modify

- `backend/internal/models/session.go`
  - Add field `query arxivquery.Query` to `PipelineSession` (mutex-guarded).
  - Extend `NewSession(id, startedAt, query)` OR add `SetQuery(q)` / `Query()`
    locked accessors. Prefer passing into `NewSession` so the session is never
    query-less. `Query()` returns a copy under `RLock`.
  - Do NOT add query to `Snapshot()` unless the UI needs to echo it (it can echo
    from its own request state — keep the poll payload small).

- `backend/internal/orchestrator/orchestrator.go`
  - `newSession()` builds the `Query` and passes it to `NewSession`.
  - `HandleDiscover`: decode optional JSON body `{category, terms}`. Empty
    body / empty category → config default category. Validate category via
    `arxivquery.IsValid`; unknown → `400`. Sanitize terms via
    `arxivquery.SanitizeTerms`. Build `Query`, create session with it.
    - Keep decoding tolerant: a nil/empty body must still work (existing tests
      POST `nil`) → treat EOF/empty as "use defaults".
  - `HandleDiscoverMore`: pass `s.Query()` into
    `o.discoMore.FetchPapersFrom(ctx, s.Query(), start, nil)`.
  - New `HandleCategories(w, r)`: return `arxivquery.Categories` as JSON
    (`[{code,label}]`).

- `backend/internal/orchestrator/orchestrator-pipeline.go`
  - `runDiscovery`: pass `session.Query()` into `FetchPapers`. Update the
    "Discovery triggered (%s)" narrative and `fetched.Summary` category to read
    `session.Query().Category` (+ terms if present) instead of
    `o.cfg.Agent.ArxivCategory`.

- `backend/internal/server/server.go`
  - `mux.HandleFunc("GET /categories", orch.HandleCategories)`.

## Implementation Steps

1. Add `query` field + accessors to session; thread through `NewSession`.
2. Parse + validate trigger body; build `Query`; store on session.
3. Point `runDiscovery` and `HandleDiscoverMore` at `session.Query()`.
4. Add `GET /categories` handler + route.
5. Tests:
   - `HandleDiscover` with `{category:"cs.LG"}` → session query category is cs.LG.
   - Unknown category → 400.
   - Empty body → default category (existing integration test stays green).
   - Terms sanitized before storage.
   - `HandleDiscoverMore` uses the session's category (assert fake arXiv URL).
   - `GET /categories` returns the catalog.

## Todo

- [ ] session `query` field + locked accessors + `NewSession` thread
- [ ] `HandleDiscover` body parse + validate (400 on unknown) + default fallback
- [ ] `runDiscovery` uses `session.Query()` (narrative + summary updated)
- [ ] `HandleDiscoverMore` uses `session.Query()`
- [ ] `HandleCategories` + route
- [ ] tests (discover, more, categories, defaults, 400)
- [ ] `go build ./... && go test ./internal/...`

## Success Criteria

- Category+terms chosen at trigger flow through initial run AND "load more".
- Retry preserves the original query (same session).
- Empty body backward-compatible; unknown category → 400.

## Risks

- Load-more cursor vs query: cursor already lives on the session; query joins it
  — both persist together, no desync.
- JSON decode on empty body must not 400 — handle `io.EOF` as "defaults".
