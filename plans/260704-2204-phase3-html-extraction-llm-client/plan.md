---
plan: phase-03-html-extraction-llm-client
title: "Phase 3 — HTML Fetch, Markdown Conversion & LLM Client"
project: ArXiv AI Paper Explainer Agent
status: complete
created: 2026-07-04
owner: long.dao@maritime-ds.com
source_prd: docs/phase3/prd.md
source_design: docs/phase3/brainstorm-summary.md
blockedBy: []          # Phase 2 (260704-2110-phase2-discovery-dedup) complete → unblocked
blocks: []
---

# Phase 3 — HTML Fetch, Markdown Conversion & LLM Client

## Overview
User picks a paper → backend fetches `arxiv.org/html/{id}`, converts LaTeXML HTML to clean
Markdown (pure Go, no poppler/vision), and stores it server-side for Phase 4. This phase also
lands the provider-agnostic `LLMClient` interface (text-only) with Anthropic/OpenAI/Gemini
implementations — wired but not yet invoked. Async pipeline continues: `POST /process` sets
`stage: extracting`, returns `{session_id}`, a goroutine runs `PaperContentTool.FetchMarkdown`.
On HTML 404 the session returns to `selection` (candidates preserved) so the user re-picks
without restarting.

Builds on **completed Phase 2** (discovery, dedup, async orchestrator, polling frontend).
See `docs/phase3/prd.md` + `docs/phase3/brainstorm-summary.md` for locked decisions.

## Key locked decisions
- **arXiv HTML → Markdown (pure Go)** via `html-to-markdown/v2`. No PDF, no poppler, no CGO.
- **LLMClient is text-only** — `DocumentText` (Markdown), no `PageImages`, no vision validation.
- **`markdownText` excluded from `Snapshot()`** — large, never shipped to frontend.
- **404 = recoverable re-pick** — session returns to `selection`, candidates preserved.
- All session mutation via mutex accessors (never touch private fields directly).

## Phases
| # | Phase | Status | Depends on |
|---|---|---|---|
| 01 | [Config & models foundation](phase-01-config-models-foundation.md) | complete | Phase 2 |
| 02 | [PaperContentTool (HTML→Markdown)](phase-02-paper-content-tool.md) | complete | 01 |
| 03 | [LLMClient interface + shared retry](phase-03-llm-client-interface.md) | complete | 01 |
| 04 | [Provider implementations (anthropic/openai/gemini)](phase-04-provider-implementations.md) | complete | 03 |
| 05 | [Orchestrator /process + runPipeline](phase-05-orchestrator-process.md) | complete | 02, 04 |
| 06 | [Frontend select + re-pick](phase-06-frontend-select-repick.md) | complete | 05 (API contract) |
| 07 | [Integration & exit-criteria verification](phase-07-integration-verification.md) | complete | 01–06 |

**Parallelizable:** 02 and 03 are independent (both need only 01). 04 needs 03.

## Key dependencies
- New Go deps: `github.com/JohannesKaufmann/html-to-markdown/v2`,
  `github.com/anthropics/anthropic-sdk-go`, `github.com/openai/openai-go`,
  `google.golang.org/genai` (new surface: `client.Models.GenerateContent`).
- Reused: DiscoveryTool retry/backoff + User-Agent (Phase 2), `PipelineSession` mutex accessors,
  `config.AgentConfig` / `config.LLMConfig`, async orchestrator + polling contract.
- External: `https://arxiv.org/html/{id}` (no auth, follows same-host version redirect); the three
  LLM provider APIs (keys from `.env` only).

## Success = PRD Exit Criteria (docs/phase3/prd.md)
Pure-Go build (no poppler/vision); Select transitions UI to `extracting`; HTML fetch handles
redirects + 30s timeout; 404 → back to `selection` (re-pick); Markdown keeps heading hierarchy +
captions; `LLMClient.Complete()` returns valid text for all 3 providers; provider switch is
config-only; LLM 429 retries ×3, 400 = config error naming the model; input/output tokens
returned separately; `markdownText` excluded from `Snapshot()`; no temp files after any run.
