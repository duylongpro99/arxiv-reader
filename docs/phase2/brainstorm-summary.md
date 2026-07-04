# Phase 2 Brainstorm Summary — Discovery & Duplicate Detection

> Source PRD: `docs/phase2/prd.md` · Date: 2026-07-04 · Status: design agreed, plan pending

## Problem
Deliver the product's first user-facing promise: click one button → see 5 fresh `cs.AI`
papers from arXiv, never re-surfacing already-processed ones. No PDF, no LLM. Prove the
discovery pipeline before expensive ops arrive in Phase 3.

## Reality check vs PRD (scouted against live code)
Phase 1 was built deliberately minimal (locked deviation #2: no upfront models). Several
PRD assumptions are false against the code and are corrected here:

| PRD claim | Reality | Resolution |
|---|---|---|
| "models defined in Phase 1" (§3) | No `internal/models` pkg exists | Phase 2 **creates** models |
| `o.config.Agent.PaperFetchLimit` | No `Agent` config section | Add `config.Agent` section |
| reads `processed.json` | `config.yaml` → `processed.log` | Rename default → `.json` |
| TanStack Query polling | Not installed | Phase 2 adds the dep |
| `server.Run()` config-aware | `Run()` takes no config | Add `*config.Config` param |

## Decisions (locked with user)
1. **Async + real polling.** `POST /discover` returns `{session_id}` immediately, pipeline
   runs in a goroutine, `GET /status/:id` returns stage AND candidates when ready. F5 stage
   labels become truthful. Rationale: Phase 3 LLM calls are genuinely slow → async needed
   eventually; doing it now keeps the session/polling contract stable across Phases 2–6.
   Consequence: `candidates` leaves the trigger response; arrives via status poll.
2. **Rename log file → `~/.arxiv-agent/processed.json`** (extension matches JSON content).
3. **New `config.Agent` section** holds arXiv params (category, base_url, fetch_limit=20,
   display_limit=5, user_agent, timeouts, retries). Base URL configurable → httptest.

## Recommendations (accepted defaults, user may override)
- **Thread safety:** guard `PipelineSession` with `sync.RWMutex` (goroutine writes / poll
  handler reads = real data race the PRD's in-place mutation would have shipped).
- **Stage labels:** map `discovery → "Connecting to arXiv…"`, `selection → "Ready"`. Drop
  the "Filtering…" label — local JSON filter takes microseconds; no honest stage for it.
- **Session ID:** dep-free 16-byte `crypto/rand` hex (honors Phase 1 minimal-deps stance),
  not `google/uuid`.
- **JSON tags:** camelCase (`pdfUrl`, not Go-default `PDFURL`) to match frontend `Paper`.
- **Session cleanup:** `sync.Map` leaks one entry per trigger. Deferred (local single-user);
  noted as known item, not solved now.

## Contract gaps closed
- **"fewer than 5" notice** (F3/F6) — new `Notice` field on session + status response.
- **DiscoveryTool base URL** — from config, so unit tests hit `httptest.Server`.

## arXiv gotchas to handle
- Atom title/summary wrapped with newlines → normalize whitespace (`strings.Fields`).
- Entry ID is a URL (`.../abs/2401.12345v1`) → split on `/abs/`, strip `vN`.
- 429/5xx retry 3→6→12s backoff (≥ the 3s min-interval requirement).
- 10s request timeout; single connection (no concurrent arXiv requests).

## Exit criteria
Per PRD §Exit Criteria, with correction: "models defined in Phase 1" → "defined in Phase 2".
