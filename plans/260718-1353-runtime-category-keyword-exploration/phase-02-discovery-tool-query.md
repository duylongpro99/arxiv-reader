# Phase 02 — DiscoveryTool: Query-Driven Fetch

**Priority:** High · **Status:** pending · **Depends on:** Phase 1

## Overview

Stop `DiscoveryTool` reading `cfg.ArxivCategory`. Make fetching driven by an
`arxivquery.Query` argument so the same tool serves any category+terms. This is
the seam that lets per-session queries flow into the arXiv call.

## Files to Modify

- `backend/internal/tools/discovery.go`
  - `buildQueryURL(start int)` → `buildQueryURL(q arxivquery.Query, start int)`:
    replace `q.Set("search_query", "cat:"+t.cfg.ArxivCategory)` with
    `q.Set("search_query", query.SearchQuery())`. `url.Values.Encode()` still
    handles transport encoding — verify spaces in `cat:X AND all:Y` encode in a
    form arXiv accepts (space→`+` or `%20`; arXiv accepts both).
  - `FetchPapersFrom(ctx, start, onRetry)` → `FetchPapersFrom(ctx, query, start, onRetry)`.
  - `FetchPapers(ctx, onRetry)` → `FetchPapers(ctx, query, onRetry)` (delegates
    to `FetchPapersFrom(ctx, query, 0, onRetry)`).
  - **Rewrite the stale comment** at the old line ~131: the "category comes from
    config, never user input, so there is no injection surface" claim is now
    FALSE. Replace with: category is validated against the catalog whitelist
    upstream; free-text is sanitized via `arxivquery.SanitizeTerms` and
    URL-encoded here — no operator/prefix injection reaches arXiv.

- `backend/internal/orchestrator/orchestrator.go`
  - Update the `PaperFetcher` / `PageFetcher` interface signatures (lines ~29,
    ~38) to take `arxivquery.Query`.

## Files to Update (callers — coordinate with Phase 3)

- `orchestrator-pipeline.go` `runDiscovery` and `orchestrator.go` `HandleDiscoverMore`
  now pass a `Query`. Full wiring lives in Phase 3; this phase updates signatures
  and fixes compile by passing a query built from the config default as a
  temporary bridge if Phase 3 is not yet merged. Prefer landing 2+3 together.

## Implementation Steps

1. Thread `arxivquery.Query` through `buildQueryURL`, `FetchPapersFrom`,
   `FetchPapers`.
2. Rewrite the injection comment.
3. Update orchestrator fetcher interfaces + the mock/fake in tests.
4. Update `discovery_test.go`: assert the built URL contains the expected
   `search_query` for (a) category-only and (b) category+terms; keep existing
   retry/backoff/parse tests (pass a fixed `Query`).

## Todo

- [ ] `buildQueryURL(q, start)` uses `q.SearchQuery()`
- [ ] `FetchPapersFrom` / `FetchPapers` take `Query`
- [ ] rewrite injection-posture comment
- [ ] update `PaperFetcher`/`PageFetcher` interfaces + test fakes
- [ ] discovery_test.go query-shape assertions
- [ ] `go build ./... && go test ./internal/tools/...`

## Success Criteria

- No reference to `cfg.ArxivCategory` remains in `discovery.go`.
- URL for cs.LG + "transformer" contains an arXiv-valid encoded
  `cat:cs.LG AND all:transformer`.
- Existing retry/parse/pagination tests still pass.

## Risks

- Encoding of `AND`/spaces — verify against arXiv (integration test hits the
  fake server URL, so assert on the decoded `search_query` value).
