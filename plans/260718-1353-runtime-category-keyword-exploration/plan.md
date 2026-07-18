---
title: Runtime Category + Keyword Exploration for arXiv Discovery
status: completed
created: 2026-07-18
completed: 2026-07-18
blockedBy: []
blocks: []
designNote: docs/design-notes/2026-07-18-runtime-category-keyword-exploration.md
---

# Runtime Category + Keyword Exploration

Let the user pick one cs.* subcategory at runtime (default from config) and
optionally add keywords, driving the arXiv query for both the initial run and
"load more". Replaces the single global `cfg.Agent.ArxivCategory` constant with
per-session query state.

**Approach A** (approved): a `Query{Category, Terms}` value object owns arXiv
query syntax; a hardcoded cs.* catalog serves as validation whitelist + UI
source. See design note for rationale and rejected alternatives.

## Key Decisions

- Category **required** (default from config), keywords **optional** → `cat:X AND all:...`.
- One category per run (no OR/multi-category). cs.* scope only.
- Catalog + `Query` type live in a **new leaf package** (`internal/arxivquery`)
  with zero internal deps — lets config, tools, and orchestrator all import it
  without an import cycle.
- Backward compatible: empty `/discover` body → config default category.
- Security posture flips: category safe via whitelist; free-text sanitized.

## Phases

| # | Phase | Status | Depends |
|---|-------|--------|---------|
| 1 | [Catalog + Query value object + config validation](phase-01-catalog-query-package.md) | completed | — |
| 2 | [DiscoveryTool: query-driven fetch](phase-02-discovery-tool-query.md) | completed | 1 |
| 3 | [Orchestrator: trigger body, session query, /categories](phase-03-orchestrator-session-query.md) | completed | 1, 2 |
| 4 | [Frontend: category picker + keyword input](phase-04-frontend-picker.md) | completed | 3 |

## Dependencies

- arXiv API boolean query syntax (`cat:X AND all:Y`) and field prefixes.
- Go 1.22+ method-based routing (existing `server.go` pattern).
- Existing mutex-guarded `PipelineSession` accessor pattern.

## Success Criteria

- User selects a cs.* category + optional keywords in the UI; discovery + "load
  more" both honor the selection.
- Unknown category → HTTP 400; empty body → config default (existing tests pass).
- Free-text cannot alter query semantics (control tokens stripped) or crash the
  arXiv call.
- `go build ./...` + `go test ./...` green; frontend builds.
