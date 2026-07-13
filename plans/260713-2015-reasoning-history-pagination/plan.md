---
title: Reasoning Trace, History Content, arXiv Pagination
status: completed
created: 2026-07-13
completed: 2026-07-13
mode: parallel
blockedBy: []
blocks: []
design_note: docs/design-notes/2026-07-13-reasoning-history-pagination.md
---

# Plan: Reasoning Trace, History Content, arXiv Pagination

Three approved features for arxiv-reader (Go backend + Next.js frontend). Extends completed Phase 7 (run-timeline tracing). **No DB migrations** — reuses existing `run_events.payload_full` JSONB.

## Features
- **A — Full reasoning trace:** capture LLM prompt+response + human-readable decisions into `PayloadFull` (currently plumbed but never populated); surface in timeline.
- **B — History content:** `GET /runs/{id}/content` reads the persisted Obsidian `.md` back; render in history detail.
- **C — Load older batch:** parameterize arXiv `start` offset; `POST /discover/{sessionId}/more` extends the session's candidates; "Load more" UI.

## Phases (execution waves)

Wave 1 (parallel — different Go packages):
- [x] **phase-01** — Backend: reasoning trace capture (Feature A) → `phase-01-backend-reasoning-trace.md`
- [x] **phase-02** — Backend: history content + pagination (Feature B+C) → `phase-02-backend-history-pagination.md`

Wave 2 (parallel — T3 defines shared types first, then T4):
- [x] **phase-03** — Frontend: reasoning display (Feature A) → `phase-03-frontend-reasoning.md`
- [x] **phase-04** — Frontend: history content + load-more (Feature B+C) → `phase-04-frontend-history-pagination.md`

## Dependencies
- Wave 2 depends on Wave 1 (frontend consumes new API shapes). Frontend can scaffold against documented contracts if backend still in flight.
- phase-03 owns `frontend/lib/types.ts` reasoning types; phase-04 rebases on them (shared file — sequence, don't co-edit).
- phase-01 ↔ phase-02: independent Go packages, safe parallel. Both touch none of the same files.

## File ownership (no overlaps)
| Phase | Owns |
|-------|------|
| P1 | `backend/internal/agents/*`, `orchestrator-pipeline.go`, `internal/config/config.go`, `config.yaml`, `internal/models/{explainer,review}.go` |
| P2 | `backend/internal/server/server.go`, `orchestrator/runs-handlers.go`, `orchestrator.go`, `internal/tools/discovery.go`, `internal/models/session.go`, `orchestrator/dto.go` |
| P3 | `frontend/components/run-event-row.tsx`, `run-timeline.tsx`, `lib/types.ts` |
| P4 | `frontend/app/runs/[id]/page.tsx`, `components/result-panel.tsx`, `candidate-list.tsx`, `lib/use-runs.ts`, `lib/api.ts`, `app/api/*` proxy routes |

Note: P1 & P2 both touch `orchestrator/` package but distinct files (P1: `orchestrator-pipeline.go`; P2: `orchestrator.go`, `runs-handlers.go`, `dto.go`). Confirm no shared-symbol edits at integration.

## Validation
- Backend: `go build ./...` + `go test ./...` after each phase.
- Frontend: `npm run build` / type-check after each phase.
- No migrations. No secrets in payloads (existing `scrub.scrubMap` covers it).

## Key risks
- P1: `scrub.scrubMap` must strip API keys from full prompts (it already runs on `PayloadFull` at `recorder.go:110`). Verify secret patterns cover system prompts.
- P2: `/discover/{sessionId}/more` on an expired/evicted session → 404 with clear error; frontend handles.
- P2: arXiv rate-limits rapid paging — reuse existing `fetchWithRetry` backoff.

## Completion Summary

**All phases complete & validated.**

**Backend validation:** `go build ./...` + `go test ./...` green (all phases). Post-review fixes applied:
- M1: `/more` endpoint stage guard → 409 when pagination attempted on non-discovery sessions; regression test added.
- M2: Consolidated path-traversal guard into exported `tools.ValidateWithinVault`, eliminated duplicate guard logic.
- L3: Session `start` param renamed `cap` → `limit` in scrub.go for clarity.
- L4: Parse-failure decision events now include narrative ("Unable to parse draft for review").
- P1b: Payload cap raised to 100k (`tracing/scrub.go:payloadCap`) to preserve full reasoning in PayloadFull; regression test added.

**Frontend validation:** `tsc` + production build green (all phases). Discovery panel wired to sessionId; candidate dedup by paper ID implemented; load-more hides when `hasMore=false`; markdown rendering works for history content panel.

**Security verified:** Path traversal guarded; API keys scrubbed from payloads; no secrets in traces.
