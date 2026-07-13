# Design Note: Reasoning Trace, History Content, arXiv Pagination

Date: 2026-07-13
Status: Approved (brainstorm) → plan pending

Three independent features. User decisions locked: **full raw trace**, **read Obsidian file** (no DB persist), **load older batch via arXiv pagination**.

---

## Feature A — Full agent reasoning trace

**Problem:** Reasoning exists at runtime but is dropped before display. `PayloadFull` is plumbed end-to-end (DTO → store → `run_events` JSONB) but **never populated** — flipping `fullPayloads` alone shows empty. Decisions render as cryptic key/values (`reviewIterations 1`).

**Structure:**
- Capture seam = the agents. `explainer.Generate` / `reviewer.Review` hold the `CompletionRequest` (system+user prompt) + raw response. Add `Trace{systemPrompt,userPrompt,rawResponse}` to `ExplainerOutput`/`ReviewVerdict`. Pipeline emit sites attach it to `event.PayloadFull` for `explainer.*`, `reviewer.*`, `decision.*`.
- Bound size: EXCLUDE full paper `DocumentText` from per-event payloads (identical every pass). Existing `scrub.scrubMap` strips secrets when flag on (`recorder.go:110`) — reuse.
- Enrich decision-event `Summary` with human-readable fields: flagged section names + short reason + decision verb ("Revised: methodology & limitations flagged" / "Accepted on pass 2").
- Config: enable `Tracing.fullPayloads`.
- Frontend: `run-event-row.tsx` gains "Reasoning" expander (prompt/response per pass) + relabel cryptic keys.

**Tradeoffs:** JSONB grows per run — mitigated (no doc text; single-user tool). Rejected separate trace table (over-engineering; `payload_full` exists for this).

---

## Feature B — Re-show generated content in history

**Problem:** History content-free; note lives only in Obsidian `.md`. Path sits in `tool.vaultwriter.completed` event `Summary["path"]` (JSONB), not a column.

**Structure:**
- Backend: `GET /runs/{id}/content` → find run's vaultwriter-completed event → extract path → `validateWithinBase(vault, path)` guard → read file → return `{markdown, path, available}`. Missing file → `available:false` (no 500).
- Frontend: history detail (`app/runs/[id]/page.tsx`) renders markdown in `result-panel` + Feature A trace. "File moved/unavailable" empty state.

**Tradeoffs:** couples to vault file presence (accepted). Zero migration. Rejected DB persistence per user choice.

---

## Feature C — Load older batch (arXiv pagination)

**Problem:** `start` hardcoded `0` (`discovery.go:124`); no load-more. `/process` requires paper to live in a **session's** `Candidates` (`orchestrator.go:161-178`) → pagination must extend that session.

**Structure:**
- Backend: parameterize `buildQueryURL`/`FetchPapers` with `start` offset. Session model gains cursor (next `start`) + `AppendCandidates`. New `POST /discover/{sessionId}/more` → fetch next page at cursor → `FilterUnprocessed` → append to `session.Candidates` → advance cursor → return new page.
- Frontend: "Load more" on `candidate-list.tsx` → append + dedup by ID.

**Tradeoffs:** arXiv rate limits + eventual-consistency at high offsets (surface empty-result notice). Keeps session-coupling = minimal blast radius. Rejected decoupled browse endpoint (would rework `/process`).

---

## Task split (parallel subagents, file-ownership boundaries)

| Task | Feature | Owns |
|------|---------|------|
| T1 backend | A | `internal/agents/*`, `orchestrator-pipeline.go`, `internal/config`, `internal/models/{explainer,review}.go` |
| T2 backend | B+C | `internal/server/server.go`, `orchestrator/runs-handlers.go`, `orchestrator.go`, `internal/tools/discovery.go`, `internal/models/session.go` |
| T3 frontend | A | `run-event-row.tsx`, `run-timeline.tsx`, `lib/types.ts` (reasoning types) |
| T4 frontend | B+C | `app/runs/[id]/page.tsx`, `result-panel.tsx`, `candidate-list.tsx`, `lib/use-runs.ts`, `lib/api.ts` |

B+C share `server.go`/`runs-handlers.go` → single backend agent (T2). `lib/types.ts` shared → T3 defines reasoning types first, T4 rebases. T1↔T2 different packages (safe parallel).

## No-migration note
No DB schema changes. Feature A reuses existing `run_events.payload_full` JSONB column. Feature B reads files. Feature C is in-memory session state. Per project rule: no migrations generated.
