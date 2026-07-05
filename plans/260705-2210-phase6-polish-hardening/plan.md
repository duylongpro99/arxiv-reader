---
title: Phase 6 — Polish & Hardening (realigned to HTML-extraction architecture)
status: complete
created: 2026-07-05
completed: 2026-07-05
mode: auto
blockedBy: []
blocks: []
---

> **Status: ✅ Complete (2026-07-05).** All six phases implemented; backend
> `go test -race ./...` and frontend `npm run build` green. A code-review pass
> fixed a concurrent-retry race (atomic `BeginRetry`), made `ErrLLMBadRequest`
> non-recoverable, and re-runs the review loop on resume after a review-call
> failure. Cross-provider E2E is a manual user task — see
> `docs/phase6/e2e-validation.md`.

# Phase 6 — Polish & Hardening

Harden the **HTML→Markdown** product built in Phases 1–5. Every change is additive and
extends existing patterns — no rewrites. Source PRD (`docs/phase6/prd.md`) was written
against an abandoned PDF/vision architecture; this plan realigns it (see brainstorm
summary). All PDF/poppler/DPI content is **dropped**.

## Guiding constraints
- **Extend, don't rewrite.** Keep the sentinel-error + `describe*()` mapping and the
  mutex-encapsulated `PipelineSession` (private fields + accessors). No public-field access.
- **Additive only.** No new business logic, no breaking changes to Phases 1–5.
- **KISS / YAGNI / DRY.** Segment-level retry (not mid-loop resume). One pricing file.

## Phases

| # | Phase | Delivers | Depends on |
|---|-------|----------|------------|
| 01 | [Backend foundation](phase-01-backend-foundation.md) | Error `action` in `describe*`; session fields (`failedStage`, `errorAction`, in/out tokens, `arxivRetryCount`, `contextWarning`); `ReviewVerdict` token split; DTO additions | — |
| 02 | [Retry-from-failed-stage](phase-02-retry-from-stage.md) | Resumable `runPipeline` via cached session state; `POST /retry/{sessionId}` (F2) | 01 |
| 03 | [Cost, context pre-check, arXiv counter](phase-03-cost-context-arxiv.md) | `pricing.go` + `limits.go`; text-based context warning (F4); arXiv retry callback (F5); cost in result (F3) | 01 |
| 04 | [Frontend integration](phase-04-frontend-integration.md) | `/api/retry` route; retry wiring; cost display; context-warning banner; retry progress label; types | 02, 03 |
| 05 | [Logging & error audit](phase-05-logging-error-audit.md) | slog audit vs HTML event table; `pipeline complete` carries in/out tokens + cost; no-secret verification (F6, F1) | 02, 03 |
| 06 | [README & E2E validation](phase-06-readme-e2e.md) | README gap-fill (F11, poppler removed); E2E checklist across 3 providers (F8) | 04, 05 |

## Key dependencies
- Phases 1–5 complete (they are). Phase 01 is the shared foundation for 02/03; do it first.
- Frontend (04) needs the backend DTO shape from 02+03 frozen first.
- Audits (05) and docs (06) come last so they reflect final code.

## Out of scope (from PRD non-goals + realignment)
- Poppler / PDF rendering / DPI (never existed here) — F9, F10 deleted.
- Any PRD `PipelineError` struct rewrite — superseded by the extend-existing decision.
- Multi-category, ranking, batch, cloud, Obsidian plugin (PRD §7 non-goals).
