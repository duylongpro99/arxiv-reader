---
plan: phase-04-explainer-vault-write
title: "Phase 4 — Explainer Generation & Vault Write"
project: ArXiv AI Paper Explainer Agent
status: complete
created: 2026-07-05
owner: long.dao@maritime-ds.com
source_prd: docs/phase4/prd.md
source_design: docs/phase4/brainstorm-summary.md
blockedBy: []          # Phase 3 (260704-2204-phase3-html-extraction-llm-client) complete → unblocked
blocks: []
---

# Phase 4 — Explainer Generation & Vault Write

## Overview
The payoff phase: a selected paper's extracted Markdown (already in `session.Markdown()` from
Phase 3) becomes a re-teaching explainer note, written **atomically** to the Obsidian vault and
previewed in the UI. Pipeline continues past the Phase 3 seam: `generating → writing → complete`.
`GET /result/:sessionId` returns the note; Next.js renders a Markdown preview + token count.

**TEXT-ONLY** — consumes `session.Markdown()` through the existing `LLMClient.Complete`
(`DocumentText`, no images). The stale `docs/phase4/prd.md` describes a vision/PDF-image pipeline
that was **never built**; `docs/phase4/brainstorm-summary.md` supersedes it and is the source of
truth. Phase 07 rewrites the PRD to match reality.

Builds on **completed Phase 3** (HTML→Markdown extraction, text-only LLMClient wired, async
orchestrator, polling frontend).

## Key locked decisions (from brainstorm-summary)
- **Text-only.** Input is `session.Markdown()`; no PDF fetch, no renderer, no vision, no CGO.
  Honors tradeoff **T1**; diagrams/tables reach the model only via surviving figure captions (**R4**).
- **`ReviewVerdict` stays out** — Phase 5. `VaultWriter.WriteToVault(ctx, explainer, paper)` has
  **no verdict param** (matches `architecture.md`, not the PRD's forward-looking signature).
- **Frontmatter includes `review_iterations: 1` + `review_passed: true` now** (forward-compatible;
  spares Phase 5 from re-scheming existing notes).
- **`Paper.Published` is a string; there is no `Paper.Category`** → `category` ← `config.Agent.ArxivCategory`.
- **All session mutation via mutex accessors.** Server-only fields (explainer, vaultFile, tokens)
  excluded from `Snapshot()`; the `/result` handler reads them via dedicated accessors.
- **Atomic write** (`.tmp` → `os.Rename`); `MarkAsProcessed` only after successful vault write;
  post-write log failure is a **warning**, not fatal (paper re-surfaces — acceptable).
- **Files <200 lines** — ExplainerAgent split into `explainer.go` + `explainer-prompt.go`.

## Phases
| # | Phase | Status | Depends on |
|---|---|---|---|
| 01 | [Models & session extensions](phase-01-models-session-extensions.md) | complete | Phase 3 |
| 02 | [ExplainerAgent (text-only)](phase-02-explainer-agent.md) | complete | 01 |
| 03 | [VaultWriterTool + MarkAsProcessed](phase-03-vaultwriter-markprocessed.md) | complete | 01 |
| 04 | [Orchestrator pipeline + /result](phase-04-orchestrator-result-endpoint.md) | complete | 01, 02, 03 |
| 05 | [Frontend result preview](phase-05-frontend-result-preview.md) | complete | 04 (API contract) |
| 06 | [Integration & exit-criteria verification](phase-06-integration-verification.md) | complete | 01–05 |
| 07 | [Docs reconciliation (rewrite PRD)](phase-07-docs-reconciliation.md) | complete | 01–05 (can start after 04) |

**Parallelizable:** 02 and 03 are independent (both need only 01). 07 can run once 01–04 land.

## Key dependencies
- **No new Go deps.** Reuses `LLMClient` (Phase 3), `PipelineSession` mutex accessors,
  `config.{LLMConfig,AgentConfig,PathsConfig}`, `LogCheckTool`, async orchestrator + polling contract.
- **New frontend deps:** `react-markdown`, `remark-gfm` (Markdown preview only).
- **External:** LLM provider API (keys from `.env`); local filesystem (Obsidian vault, `processed.json`).
- ⚠️ **Frontend Next.js is a modified build** — Phase 05 MUST read `node_modules/next/dist/docs/`
  before writing any Next.js code (see `frontend/AGENTS.md`).

## Success = PRD Exit Criteria (docs/phase4/prd.md, text-only interpretation)
All 9 sections present for a valid cs.AI paper; note saved to `{vault}/AI Papers/` with correct
filename; valid YAML frontmatter renders in Obsidian; atomic write leaves no `.tmp` under any
failure; `processed.json` updated only after successful write (NOT on failure); paper re-surfaces if
pipeline fails pre-write; `GET /result` returns content + vault path + tokens; Markdown preview
renders (sections, bold, tables, links); token usage shown; all events logged with
`session_id`/`paper_id`/`duration_ms`; ~2,500-word soft target met for a typical 10-page paper.
