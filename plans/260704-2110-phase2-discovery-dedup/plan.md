---
plan: phase-02-discovery-dedup
title: "Phase 2 — arXiv Discovery & Duplicate Detection"
project: ArXiv AI Paper Explainer Agent
status: complete
created: 2026-07-04
owner: long.dao@maritime-ds.com
source_prd: docs/phase2/prd.md
source_design: docs/phase2/brainstorm-summary.md
blockedBy: []          # Phase 1 (260703-1124-phase1-scaffolding-config) complete → unblocked
blocks: []
---

# Phase 2 — arXiv Discovery & Duplicate Detection

## Overview
Click "Find New Papers" → within seconds see up to 5 fresh `cs.AI` papers from arXiv, never
re-surfacing already-processed ones. No PDF, no LLM. Async pipeline: `POST /discover` returns
a `session_id` immediately, a goroutine runs `DiscoveryTool → LogCheckTool`, the frontend polls
`GET /status/:id` (TanStack Query, 2s) until `selection` (candidates ready) or `failed`.

Builds on **completed Phase 1** (config loader, `net/http` server on `127.0.0.1:8080`, CORS,
`GET /health`). See `docs/phase2/brainstorm-summary.md` for locked decisions and PRD corrections.

## Key locked decisions
- **Async + real polling** (not the PRD's contradictory sync-return + poll).
- **Models are net-new** — Phase 1 dropped upfront models; Phase 2 creates `internal/models`.
- **New `config.Agent` section**; log file renamed `processed.log → processed.json`.
- Thread-safe session (`sync.RWMutex`); dep-free `crypto/rand` session IDs; camelCase JSON.

## Phases
| # | Phase | Status | Depends on |
|---|---|---|---|
| 01 | [Models & config foundation](phase-01-models-config-foundation.md) | ✅ complete | Phase 1 |
| 02 | [DiscoveryTool (arXiv client)](phase-02-discovery-tool.md) | ✅ complete | 01 |
| 03 | [LogCheckTool (dedup)](phase-03-logcheck-tool.md) | ✅ complete | 01 |
| 04 | [Orchestrator (async pipeline + endpoints)](phase-04-orchestrator.md) | ✅ complete | 01, 02, 03 |
| 05 | [Frontend (UI + polling)](phase-05-frontend.md) | ✅ complete | 04 (API contract) |
| 06 | [Integration & exit-criteria verification](phase-06-integration-verification.md) | ✅ complete | 01–05 |

**Parallelizable:** 02 and 03 are independent (both need only 01).

## Completion notes (2026-07-04)
Backend + frontend implemented; `go test -race ./...` and `npm run build`/lint all green.
Code-reviewer audited the backend (DONE_WITH_CONCERNS) — H1 (panic recovery on detached
goroutine), M1 (JSON 404 contract), M2 (retry log label), L2 (dup ID call), L4 (raw parse
error in logs) all fixed. Deferred: L1 (interval naming), L3 (omitempty bool), session-store
cleanup — documented, non-blocking for a local single-user tool.
**Remaining live-API touchpoint:** the manual `make dev` runbook in phase-06 (integration
tests use httptest, never live arXiv, to keep CI deterministic).

## Key dependencies
- Go stdlib: `encoding/xml`, `encoding/json`, `net/http`, `net/http/httptest`, `sync`,
  `crypto/rand`, `context`, `time`, `os`.
- Frontend: `@tanstack/react-query` (new — deferred from Phase 1).
- External: arXiv API `https://export.arxiv.org/api/query` (no auth, Atom/XML).

## Success = PRD Exit Criteria
5 unprocessed `cs.AI` papers, recency-ordered; card shows title/authors/abstract(300)/date/ID;
processed paper never re-surfaces; first run (no `processed.json`) works; 429 retries ×3;
`failed` shows human error + retry when `recoverable`; polling stops on `selection`/`failed`;
all discovery events logged with `session_id`, `stage`, `duration_ms`.
