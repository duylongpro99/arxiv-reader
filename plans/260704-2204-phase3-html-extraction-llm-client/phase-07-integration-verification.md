# Phase 07 — Integration & Exit-Criteria Verification

**Context:** `docs/phase3/prd.md` "Exit Criteria" (full list) · §6 (Success Metrics) · `brainstorm-summary.md` §8
**Priority:** High · **Status:** complete · **Depends on:** 01–06 · **Effort:** ~M

## Overview
Prove the whole Phase 3 slice works end to end and every PRD exit criterion is met. Automated
gates (`go test -race`, `go build`, frontend typecheck/lint) plus a scripted manual runbook against
live arXiv + a live LLM provider. This phase writes NO feature code — it verifies, fixes only what
the gates surface, and updates docs.

## Key insights
- Integration tests (Phase 05) use `httptest` for arXiv HTML — deterministic, no live network in
  CI (matches Phase 2's rule: httptest in tests, live only in the manual runbook).
- Live LLM verification is manual + key-gated (keys in `.env`, never committed). It confirms
  provider parity + config-only switching — the one thing httptest can't fully prove for real SDKs.
- `docs/development-roadmap.md` and `docs/project-changelog.md` **do not exist yet** — "update if
  present" → they are absent, so either create minimal ones or note their absence. Do NOT invent
  scope; check first, then decide with the user if creation is wanted.

## Requirements — verify every PRD Exit Criterion
Automated:
- `cd backend && go build ./... && go test -race ./...` green (config, models, tools, llm,
  orchestrator, server integration).
- `cd frontend && npm run build && npm run lint` (or `tsc --noEmit`) green.
- No poppler / vision / Python anywhere in build (grep for `poppler`, `pdftoppm`, `PageImages`,
  `vision` → zero hits).

Manual runbook (`make dev` backend + `npm run dev` frontend):
- Discover → Select a recent cs.AI paper → UI shows "Extracting paper text..." → markdown stored
  server-side (log line `markdown stored` with `markdown_bytes`).
- Force a 404 (select a paper whose HTML 404s, or temporarily point `arxiv_html_base_url` at a
  404 fixture) → session returns to `selection`, notice shown, cards re-enabled, re-pick works.
- LLM smoke test per provider: a tiny script/one-off calling `LLMClient.Complete` with a short
  `DocumentText` for anthropic, then openai, then gemini — flip `llm.provider`/`llm.model` in
  config (+ key), confirm valid `Content` + separate token counts, no code change.
- Confirm no temp files on disk after success or 404 (pure in-memory).

## Related code files
**Modify (only if gates fail):** any Phase 01–06 file flagged by tests/lint (delegate fixes back to
the owning phase's file set to preserve ownership boundaries).
**Docs:**
- `docs/development-roadmap.md` — create/update Phase 3 status **only if it exists or the user
  wants it** (absent today).
- `docs/project-changelog.md` — same; record Phase 3 features/deps if the file exists or is wanted.
- `docs/phase3/prd.md` — tick the Exit Criteria checklist as verified (allowed: it's the phase's
  own PRD, not a code file).

## Implementation steps
1. Run backend gates: `go build ./...`, `go test -race ./...`. Triage failures → route to owning phase.
2. Run frontend gates: `npm run build`, `npm run lint`/`tsc`. Triage.
3. Grep the tree to confirm zero poppler/vision/PageImages/Python references remain.
4. Manual runbook: happy path (real paper), 404 re-pick, no-temp-files check.
5. LLM provider parity: smoke-test all three via config switch (keys from `.env`).
6. Walk the PRD Exit Criteria list top to bottom; tick each; log any gap as an unresolved question.
7. Docs: check for roadmap/changelog; update if present, else confirm with user before creating.

## Todo
- [x] `go build ./...` + `go test -race ./...` green
- [x] `npm run build` + lint/tsc green
- [x] grep: no poppler / vision / PageImages / Python
- [x] happy path select → extracting → markdown stored (automated integration test + live arXiv smoke)
- [x] 404 → re-pick (selection + notice, cards re-enabled) — automated integration test
- [~] LLM parity: config-only switch + token split verified via httptest per provider; **live-key smoke test pending** (no `.env` keys in the automated run)
- [x] no temp files after success or failure
- [x] every PRD Exit Criterion ticked (or gap logged)
- [x] roadmap/changelog: confirmed absent — not created (avoid inventing scope; raise with user if wanted)

## Success criteria (= PRD Exit Criteria, condensed)
- Pure-Go build, no vision validation; Select → `extracting`; HTML fetch handles redirect + 30s
  timeout; 404 → `selection` re-pick (candidates preserved); Markdown keeps headings + captions;
  `LLMClient.Complete()` valid for all 3 providers; provider switch config-only; LLM 429 retries
  ×3, 400 = config error naming the model; tokens returned separately; markdown excluded from
  `Snapshot()`; no temp files after any run.

## Risk Assessment
| Risk | L×I | Mitigation |
|---|---|---|
| Live LLM smoke needs real keys/quota | Med×Low | Key-gated manual step; skip individual providers if no key, note which were verified. |
| A real paper 404s during happy-path demo | Low×Low | Newest-first discovery → HTML almost always present; the 404 path is a *separate* deliberate test. |
| Roadmap/changelog absent → ambiguous "update" | High×Low | Verified absent; confirm with user before creating (avoid inventing scope). |
| `-race` flakiness under load | Low×Med | Deterministic httptest fixtures; re-run; investigate any real race in Phase 01/05 accessors. |

## Backwards compatibility
Verification-only phase. The one deliverable that changes behavior is bug fixes routed to owning
phases; each must keep Phase 2 discovery/polling green (its tests are part of the suite run here).

## Rollback
Nothing net-new to roll back except doc edits. If a gate fix regresses, revert that specific fix.

## Security
- Live-run keys stay in `.env` (uncommitted); confirm no key appears in logs (grep run logs for the
  key prefix). No raw HTML / markdown persisted to disk.

## Next Steps
On green + all criteria ticked, Phase 3 is complete and **Phase 4 (ExplainerAgent)** is unblocked —
it resumes exactly at the `SetMarkdown` seam in `runPipeline` and calls the already-wired
`LLMClient`. Journal the phase (mirror the Phase 2 completion-notes pattern in `plan.md`).
