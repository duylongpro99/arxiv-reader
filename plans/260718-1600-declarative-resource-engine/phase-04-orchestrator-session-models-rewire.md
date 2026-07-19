# Phase 04 — Orchestrator + Session + Models Rewire

**Priority:** High · **Status:** pending · **Depends on:** phase-03 (golden green)

## Overview

Swap the orchestrator's three concrete arXiv tools for the `Registry`, bind the
session to `resourceID` + validated `values` instead of `arxivquery.Query`, add a
non-persisted `Source` field to `models.Paper`, and delete the now-dead code
(`tools/discovery.go`, `tools/papercontent.go`, `internal/arxivquery`). Only
proceed once Phase 03's golden test is green.

## Files to Modify

- `backend/internal/models/paper.go` — add `Source string` (`json:"source,omitempty"`).
  Not persisted (store maps only paper_id/title) → no DB migration.
- `backend/internal/models/session.go`
  - Replace `query arxivquery.Query` (line ~42) with `resourceID string` +
    `values map[string]string`. Update `NewSession` signature and `Query()` →
    `ResourceID()` + `Values()`. Keep `arxivRetryCount` (generic fetch-retry
    counter; rename optional — leave for minimal churn).
- `backend/internal/orchestrator/orchestrator.go`
  - Replace `disco/discoMore/content` fields (72–89) with `registry *resource.Registry`.
  - Replace consumer interfaces `PaperFetcher`/`PageFetcher`/`PaperContent`
    (29–53) with a dependency on `resource.Source` (fakes in tests implement it).
  - `New()` (99+): build the registry via `resource.Load(cfg.Paths.ResourcesDir, resolve)`
    instead of `NewDiscoveryTool`/`NewPaperContentTool`; fail startup on load error.
  - `parseDiscoverQuery` (190) moves to Phase 05 (becomes schema-driven); here just
    make `newSession` take `resourceID,string values`.
- `backend/internal/orchestrator/orchestrator-pipeline.go`
  - `runDiscovery` (43): `src,_ := o.registry.Get(session.ResourceID())`;
    `src.Discover(ctx, resource.Request{Values: session.Values()}, 0, onRetry)`.
  - `runPipeline` (141): `src.FetchContent(ctx, paperID)`; map `resource.ErrContentNotFound`
    to the existing recoverable re-pick branch (replaces `tools.ErrPaperHTMLNotFound`).
  - `describeQuery`/tracing summaries (81, 534): generalize — read
    `session.Values()` generically (e.g. join non-empty values) instead of
    `query.Category`/`query.Terms`. Keep the `category`/`terms` summary keys only
    if present in values (back-compat display), else emit `resource` + values.
  - `runDiscoverMore` (discover-more path): use `src.Discover(..., start, ...)`.
- `backend/internal/orchestrator/pipeline-errors.go` — map new sentinel errors
  (`resource.ErrTransport*`, `ErrNormalize`, `ErrContentNotFound`) to the same
  user messages/actions the old `ErrArxiv*`/`ErrPaperHTML*` produced.

<!-- Updated: Validation Session 1 — V4 keep old tools as oracle through Phase 07 -->
> **V4 (supersedes the deletion list below):** DO NOT delete the old tools in Phase 04.
> Rewire onto the engine but KEEP `discovery.go`/`papercontent.go`/`arxivquery` compiled behind
> a temporary build tag / unexported `useLegacyArxiv` switch, with the golden diff kept live as
> an A/B oracle. Deletion moves to **Phase 07** after the full e2e (vault + timeline +
> pagination) passes. This preserves rollback + a reference implementation through the risky
> window. The list below is the eventual Phase-07 deletion set, not a Phase-04 action.

## Files to Delete (in Phase 07, after full e2e green — NOT Phase 04)

- `backend/internal/tools/discovery.go` (+ `discovery_test.go` — port fixtures to golden first)
- `backend/internal/tools/papercontent.go`, `papercontent-cleanup.go` (+ tests) — logic moved to `resource/convert_html.go`
- `backend/internal/arxivquery/` (catalog/query + tests) — catalog → yaml, SanitizeTerms → sanitizer
- Remove `NewDiscoveryTool`/`NewPaperContentTool` references.

> `tools/logcheck.go`, `vaultwriter.go`, `atomic-write.go` STAY — dedup + vault
> are resource-agnostic and still used by the pipeline.

## Implementation Steps

1. `Paper.Source`. 2. Session resourceID+values. 3. Orchestrator registry +
`resource.Source` dependency. 4. Pipeline calls + error mapping + tracing
generalization. 5. Update orchestrator tests to fake `resource.Source`. 6. Delete
dead code once `go build ./... && go test ./...` is green.

## Red Team Fixes (2026-07-18) — applied

- **F8 (H4) — vault category (plan.md "untouched" was WRONG):** add the call site
  `orchestrator-pipeline.go:236` to the Files-to-Modify list. `WriteToVault(..., s.Query().Category)`
  → source the category from `s.Values()["category"]` (empty-safe). `vaultwriter.go` internals
  are unchanged, but the call site is not — correct plan.md's "Untouched" claim.
- **F9 (H5) — pagination single source:** expose page size via `Source.PageSize()` /
  `Descriptor`; `HandleDiscoverMore` derives BOTH the cursor step (`ConsumeNextStart`) and the
  `hasMore` heuristic from it — stop reading `cfg.Agent.FetchLimit` once the engine owns the query.
- **F1/F5 (H1) — error parity:** `pipeline-errors.go` MUST preserve the two distinct
  message/recoverability mappings for discovery vs content timeout/429 (they differ today).
- **F12 (H8) — gated deletion:** deleting `discovery.go`/`papercontent.go`/`arxivquery` is
  contingent on a **ported-test checklist** (content conversion, oversize, empty feed,
  require-tolerance, pdf `rel=related`, old-style id, error taxonomy) — not just the discovery
  golden. Do not delete until every item is covered by a new test.

## Todo

- [ ] Paper.Source (non-persisted)
- [ ] session resourceID + values
- [ ] orchestrator registry wiring + interface swap
- [ ] pipeline Discover/FetchContent + error mapping + tracing generalization
- [ ] fakes/tests updated
- [ ] delete discovery/papercontent/arxivquery
- [ ] `go build ./... && go test ./...`

## Success Criteria

- Full pipeline runs arXiv through the engine end-to-end (unit + orchestrator tests).
- Old arXiv tools + `arxivquery` gone; nothing imports them.
- Tracing timeline still tells the discovery story (generalized from values).

## Risks

- Tracing summary keys (`category`/`terms`) are read by the frontend timeline —
  keep emitting them when those values exist so the UI needs no change here.
- Error-mapping parity — cover every branch `describeError`/`vaultErrMsg` handled.
