# Run Timeline Tracing — Design Spec

**Date:** 2026-07-12
**Status:** Approved (brainstorming) → ready for implementation planning
**Author:** brainstorming session

---

## 1. Problem

Each pipeline run today exposes only a coarse `stage` string on the in-memory
`PipelineSession`, polled via `/status`. Rich detail (tool calls, LLM decisions,
tool inputs/outputs) is emitted only as `slog` lines to stdout and lost.

Users need a **well-structured, readable, traceable timeline** per run that tells
the story from beginning to final result:

- what paper ("post") they selected,
- what tools/LLM calls ran,
- what decisions the LLM/orchestrator made,
- the input/output of each tool and LLM call,
- the final outcome (saved note, cost, review result — or failure).

Everything that happens in a run must be transparent in an ordered timeline, live
during the run and reviewable afterward, surviving backend restarts.

## 2. Decisions (from brainstorming)

| Question | Decision |
|---|---|
| Lifetime | Live **and** reviewable after completion, **persisted** across restarts |
| Store | **PostgreSQL** in Docker |
| Capture depth | **Summaries + metadata by default**, optional full-payload column (opt-in via config) |
| Live transport | **Server-Sent Events (SSE)** |
| History | **Full history list page** to browse + reopen past runs |
| Emission site | **Orchestrator only** — the existing decision hub; tools/agents unchanged |

## 3. Architecture

The orchestrator already sits at every decision point (drives the review loop,
reads tool outputs, branches on the verdict). It emits events through a single
`Recorder` seam. Tools and agents are **not** modified — they return data as they
do today; the orchestrator, which already sees that data, records it.

```
Orchestrator (decision hub)
   │  rec.Emit(evt)
   ▼
RunRecorder ──┬──▶ in-memory ring buffer (per run) ──▶ SSE broker ──▶ browser (live)
              └──▶ async writer ──▶ Postgres (durable: history, reload)
```

**Dual-write.** The Recorder writes to (a) a per-run in-memory ring buffer that
feeds SSE subscribers instantly and lets a late/reconnecting client replay from
`seq 0`, and (b) an async Postgres persist for durable history. **Postgres or
broker failure is a logged warning, never fatal** — tracing must never take down
the paper pipeline.

The existing in-memory `PipelineSession` is unchanged and still owns live *stage*
state; the Recorder is purely additive beside it. The existing `/status` poll
remains for backward compatibility.

**Graceful degradation.** If `DATABASE_URL` is unset or Postgres is unreachable at
startup, tracing runs **in-memory only**: SSE + live timeline still work; history
and cross-restart reload are disabled. The pipeline never depends on the DB.

### Rejected alternatives

- **Derive events from stage transitions only** (wrap `SetStage`): too coarse —
  cannot capture tool I/O or the reviewer's pass/fail decisions.
- **Full event bus via `context.Context` to every component**: over-engineered
  for a local single-user tool; hides where events originate.

## 4. Event Model

Every event is one row with a stable shape:

| Field | Meaning |
|---|---|
| `seq` | Monotonic per-run counter (0,1,2…) — drives ordering + SSE resume |
| `event_type` | e.g. `selection.chosen`, `tool.papercontent.completed`, `llm.reviewer.completed`, `decision.revise` |
| `stage` | Existing `PipelineStage`, for grouping |
| `title` | Human one-liner, e.g. "Reviewer rejected — score 0.62" |
| `status` | `info` \| `success` \| `warning` \| `error` — drives icon/color |
| `summary` | JSONB of small structured fields (tokens, score, byte sizes, ~500-char previews) |
| `payload_full` | JSONB, **nullable** — full prompt/response/markdown; populated only when opt-in trace is on |
| `duration_ms`, `created_at` | Timing |

### Event taxonomy

- `discovery.started`
- `tool.discovery.completed` — fetched N papers (input: category, limit; output: N)
- `tool.logcheck.completed` — filtered M processed → K candidates
- `selection.presented` — K candidates shown
- `selection.chosen` — **user picked paper {id, title}** ← *what post they selected*
- `tool.papercontent.started` / `.completed` / `.failed` — HTML→MD (input: arxiv_id/url; output: markdown_bytes + preview) ← *tool I/O*
- `context.warning` — optional over-limit advisory
- `llm.explainer.started` / `.completed` (iteration N) — output: content preview, in/out tokens, duration ← *LLM call + output*
- `llm.reviewer.started` / `.completed` (iteration N) — decision: pass/fail, score, feedback summary ← *LLM decision*
- `decision.revise` / `decision.accept` / `decision.max_iterations` — orchestrator branching ← *decisions*
- `tool.vaultwriter.completed` — output: vault path
- `run.completed` — summary: tokens, cost, review outcome, duration
- `run.failed` — error, action, recoverable
- `run.recovered_to_selection` — 404 re-pick

### Example ordered story

```
● discovery.started            Discovery triggered (cs.AI)
● tool.discovery.completed      Fetched 20 papers from arXiv           (620ms)
● tool.logcheck.completed       15 new after filtering 5 processed
● selection.presented           5 candidates shown
● selection.chosen         ★    You selected "Paper Title" (2401.12345)
● tool.papercontent.completed   Fetched HTML → 48KB Markdown            (1.2s)
● llm.explainer.completed  ⚙    Explainer generated · 12.1K in / 3.4K out (28s)
● llm.reviewer.completed   ⚙    Reviewer: FAIL score 0.62               (9s)
● decision.revise          ↻    Revising — 3 sections flagged
● llm.explainer.completed  ⚙    Revised · 11.8K in / 3.1K out           (26s)
● llm.reviewer.completed   ⚙    Reviewer: PASS score 0.88               (8s)
● decision.accept          ✓    Explainer accepted
● tool.vaultwriter.completed    Saved to vault: 2401.12345_title.md
● run.completed            ✓    Done · 30.4K tokens · ~$0.11 · 1m47s
```

## 5. Persistence — Postgres, Docker, Migration

**Docker.** Add a `postgres:17-alpine` service to a new `docker-compose.yml` with a
named volume and healthcheck. Connection via `DATABASE_URL` in `.env` (already
`.gitignore`'d), with a documented default in `.env.example`.

**Access layer.** `jackc/pgx/v5` with plain SQL in a thin `internal/store`
repository (`RunStore`, `EventStore`). No ORM — consistent with this repo's
stdlib-first style.

**Schema** (`backend/migrations/0001_run_timeline.sql`):

```sql
-- runs: one row per PipelineSession
CREATE TABLE runs (
    id            TEXT PRIMARY KEY,          -- existing session id
    paper_id      TEXT,                      -- null until selection.chosen
    paper_title   TEXT,
    stage         TEXT NOT NULL,             -- last known stage
    status        TEXT NOT NULL,             -- running | complete | failed | recovered
    input_tokens  INT  NOT NULL DEFAULT 0,
    output_tokens INT  NOT NULL DEFAULT 0,
    est_cost_usd  NUMERIC(10,4),
    review_passed BOOLEAN,
    started_at    TIMESTAMPTZ NOT NULL,
    completed_at  TIMESTAMPTZ
);

-- run_events: the ordered timeline
CREATE TABLE run_events (
    run_id       TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    seq          INT  NOT NULL,             -- monotonic per run
    event_type   TEXT NOT NULL,
    stage        TEXT NOT NULL,
    title        TEXT NOT NULL,
    status       TEXT NOT NULL,             -- info | success | warning | error
    summary      JSONB,
    payload_full JSONB,                     -- nullable; opt-in full trace only
    duration_ms  INT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, seq)
);
CREATE INDEX idx_runs_started_at ON runs (started_at DESC);
```

> **No-migrations rule (project policy).** The agent MUST NOT generate or run
> migrations. The SQL above is delivered as a documented file
> `backend/migrations/0001_run_timeline.sql`; the **user** runs it themselves:
> `psql "$DATABASE_URL" -f backend/migrations/0001_run_timeline.sql`.

## 6. Transport — SSE + REST

New Go backend endpoints (plus Next.js proxy routes):

```
GET /runs/:id/events   → SSE stream. On connect, replays buffered events from
                         Last-Event-ID (or 0), then pushes new ones live.
                         Each message: id: <seq>, event: <event_type>, data: <json>.
                         Closes on run.completed / run.failed.
GET /runs              → history list: [{id, paperTitle, status, cost, startedAt}],
                         newest first, paginated.
GET /runs/:id          → one run's full record + all events (reopen a past run).
```

SSE fan-out uses a small per-run **broker** (`map[runID][]chan Event`) that the
Recorder's in-memory side notifies. `Last-Event-ID` gives free reconnect/resume.

## 7. Frontend

- **`<RunTimeline>`** — renders the ordered event list with per-`status` icon +
  color, relative timestamps, durations, and expandable rows (click an LLM/tool
  event to see the summary preview; full payload when trace-on). Consumes SSE via
  a small `useEventSource` hook; on completion falls back to persisted
  `GET /runs/:id`. Augments/replaces the single-line `ProgressIndicator` during a
  run.
- **History page** (`/runs`) — list of past runs (title, date, outcome badge,
  cost) → click reopens `<RunTimeline>` + result panel for that run. New list
  endpoint + `useRuns` hook.
- Types mirror the Go DTOs in `lib/types.ts` (camelCase, matching json tags),
  following existing contract discipline.

## 8. Cross-Cutting

- **Security.** Summaries/payloads pass through a scrubber that redacts
  API-key/secret patterns before persistence; **raw HTML is never stored** (only
  converted-Markdown size + preview), honoring CLAUDE.md. Full-payload capture is
  off by default (`tracing.full_payloads: false` in config).
- **Error handling.** DB/broker failures are warnings, never fatal to the
  pipeline. SSE handles client disconnect and run-not-found cleanly.
- **Testing.** `RunStore`/`EventStore` against a test Postgres (or an in-memory
  fake implementing the store interface); Recorder unit tests for ordering/seq; an
  orchestrator test asserting the exact event sequence for pass, revise-then-pass,
  404-repick, and failure; SSE handler test for replay-from-Last-Event-ID.
- **File-size discipline (200-line rule).** `internal/store` (runs.go, events.go),
  `internal/tracing` (recorder.go, broker.go, event.go, scrub.go), and the SSE
  handler stay small and focused.

## 9. Config additions

```yaml
tracing:
  enabled: true                 # master switch for the Recorder
  full_payloads: false          # opt-in: store full prompts/responses/markdown
  buffer_size: 256              # per-run in-memory ring capacity
```

`DATABASE_URL` is read from `.env` (documented default in `.env.example`).

## 10. Out of scope (YAGNI)

- Retention/pruning policy for old runs (add later if the table grows).
- Auth on `/runs` (local single-user tool, bound to 127.0.0.1).
- Multi-user/multi-tenant separation.
- Streaming token-by-token LLM output into the timeline (events are per-call).
