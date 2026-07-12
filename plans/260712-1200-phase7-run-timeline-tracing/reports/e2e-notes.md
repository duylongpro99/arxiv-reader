# Phase 7 — E2E Verification Notes

Date: 2026-07-12

## Automated coverage (green)

`cd backend && go test -race ./...` and `cd frontend && npm run build && npm run lint` all pass.

The backend E2E test `internal/server/timeline_integration_test.go::TestTimelineEndToEnd`
drives the real `server.Handler` (fake arXiv / LLM / paper-HTML servers, temp vault
+ log) with tracing enabled, and asserts:

- Full pipeline completes with tracing ON (`discover → process → complete`).
- The live SSE stream (`GET /runs/:id/events`) contains the ordered story beats:
  `discovery.started`, `tool.discovery.completed`, `selection.chosen`,
  `tool.papercontent.completed`, `llm.explainer.completed`,
  `tool.vaultwriter.completed`, `run.completed`.
- No secret leakage: the fake API key never appears in the stream; the extracted
  HTML is summarized (size + preview), never shipped whole.
- **Degraded path (no `DATABASE_URL`, the CI default):** the pipeline still
  completes and the live timeline still streams; `GET /runs` returns 503 cleanly.
- **DB path (when `DATABASE_URL` is set):** the run + full timeline are readable
  from Postgres via `GET /runs` and `GET /runs/:id` (persisted-read branch).

Unit/handler coverage backing this: tracing 91.1%, orchestrator ~84–86%
(incl. event-sequence assertions for happy / revise-then-pass / max-iterations /
404-recover / recoverable-fail / non-recoverable-fail, and SSE replay / resume /
history-fallback / synthetic-terminal / 404 / 503).

## Manual browser checklist (to run against a live stack — `make dev`)

These require a running browser + services and were NOT executed in this session;
run them before shipping to users:

- [ ] `make db`; apply `0001_run_timeline.sql` (`make migrate-print` for the command); `make dev`.
- [ ] Trigger discovery → select a paper → watch the live timeline stream each step (≤ ~1s/step).
- [ ] Confirm `run.completed` shows tokens + cost; note saved to the vault.
- [ ] Open `/runs` → the run appears; reopen it → full timeline + header render.
- [ ] Restart the backend → `/runs` still lists the run; reopen still works (persistence proven).
- [ ] Stop Postgres → run a new pipeline → it completes and streams live; `/runs` degrades cleanly (503 → "history unavailable").
- [ ] Force a failure (bad model) → `run.failed` appears with an action; retry re-streams on the same connection.
- [ ] Grep the DB / stream for any secret or raw HTML → none present (scrub verified).

## Review fixes applied this phase (from milestone code reviews)

- H1: run-header INSERT serialized before event appends (single persist worker) — no FK-violation event loss.
- M2 (broker): live fan-out published under the recorder lock — seq-ordered delivery for Last-Event-ID resume.
- SSE: recoverable `run.failed` keeps the stream open (ends on broker Close, not on event kind); recorder evicted on Close when DB-backed; synthetic terminal for orphaned history replays (no reconnect storm).
- Frontend: SSE error flag clears on reconnect (no stuck banner).
- CORS accepts both `localhost:3000` and `127.0.0.1:3000`.
- Scrubber also redacts the DSN literal (defense-in-depth).
