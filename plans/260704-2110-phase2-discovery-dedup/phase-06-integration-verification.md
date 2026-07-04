# Phase 06 — Integration & Exit-Criteria Verification

**Context:** `docs/phase2/prd.md` §Exit Criteria, §8 · all prior phases
**Priority:** High · **Status:** pending · **Depends on:** 01–05 · **Effort:** ~M

## Overview
Prove the full pipeline end-to-end against the PRD Exit Criteria. Unit tests live in their
own phases (02/03/04); this phase adds cross-component integration coverage + a manual
runbook, and confirms every exit checkbox.

## Requirements
- Go integration test: real `DiscoveryTool` (httptest arXiv) + real `LogCheckTool` (temp
  `processed.json`) + `Orchestrator`, driving `discover → poll → selection` over `httptest.Server`.
- Dedup regression: seed `processed.json` with one fetched ID → confirm it's excluded.
- First-run: no `processed.json` → succeeds, all unprocessed.
- `go test -race ./...` clean (async race guarantee).

## Related code files
**Create:**
- `backend/internal/orchestrator/integration_test.go` — wires real tools via `httptest`,
  exercises discover + status endpoints through the actual `server` mux.
**Modify (if gaps found):** any phase file's code — loop back, don't patch here.

## Test scenarios (map to Exit Criteria)
| Scenario | Exit criterion |
|---|---|
| 20 fetched → 5 returned, newest first | "exactly 5 unprocessed, most recent first" |
| Card fields present in status JSON | title/authors/abstract/date/ID |
| Seed 1 processed ID → excluded next run | "does not re-surface" |
| No `processed.json` → all returned | "first run works" |
| arXiv 429 ×2 then 200 → succeeds | "429 retries ×3" |
| arXiv all-429 → `failed` + recoverable msg | "failed surfaces clear error + retry" |
| poll after `selection` returns `refetchInterval:false` (FE) | "polling stops" |
| logs contain session_id/stage/duration_ms | "all events logged" |

## Manual runbook (documented in this file's output/PR)
1. `make dev` → frontend `:3000`, backend `:8080` up.
2. Ensure no `~/.arxiv-agent/processed.json` (first-run path).
3. Click "Find New Papers" → observe "Connecting to arXiv…" → 5 cards.
4. Manually append one shown ID to `processed.json`; re-trigger → that paper absent.
5. (Optional) point `arxiv_base_url` at an unreachable host → observe error banner + retry.

## Implementation steps
1. Write `integration_test.go` (httptest arXiv fixture + temp log + server mux).
2. `go test -race ./...` — fix any race/logic gaps in the owning phase.
3. `cd frontend && npm run build && npm run lint`.
4. Manual runbook click-through; capture that dedup + first-run behave.
5. Tick every PRD Exit Criteria checkbox; note any deviation.

## Todo
- [ ] `integration_test.go` (real tools via httptest, endpoints via server mux)
- [ ] dedup regression + first-run cases
- [ ] `go test -race ./...` green
- [ ] `npm run build && npm run lint` green
- [ ] manual runbook executed
- [ ] all PRD Exit Criteria verified & checked off

## Success criteria
- All PRD §Exit Criteria checkboxes pass.
- `go test -race ./...` and frontend build/lint clean.
- Dedup 100% reliable across two runs; first-run with no log works.

## Risks
- Live arXiv flakiness — integration tests MUST use httptest, never the real API (keeps CI
  deterministic and polite). Manual runbook is the only live-API touchpoint.

## Next steps
- On green: `/ck:plan archive` + journal. Phase 3 (selection + PDF fetch) picks up the stable
  session/polling contract established here.
