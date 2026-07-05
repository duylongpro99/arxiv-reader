---
plan: phase-05-reviewer-revision-loop
title: "Phase 5 — ReviewerAgent & Revision Loop"
project: ArXiv AI Paper Explainer Agent
status: complete
created: 2026-07-05
owner: long.dao@maritime-ds.com
source_prd: docs/phase5/prd.md
source_design: brainstorm (this session)
blockedBy: []          # Phase 4 (260705-2008-phase4-explainer-vault-write) complete → unblocked
blocks: []
---

# Phase 5 — ReviewerAgent & Revision Loop

## Overview

Insert a bounded critic-generator loop between `explainer.Generate` and `vault.WriteToVault`
in `runPipeline`. A new independent `ReviewerAgent` scores every explainer against a fixed
6-criteria rubric and returns structured, section-level feedback; failing output is fed back
to the ExplainerAgent (via the *already-wired* `RevisionNote` seam) for revision. Loop runs
until the reviewer passes or `max_review_iterations` is hit, then saves with honest
`review_passed`/`review_score`/`review_iterations` frontmatter. `max_review_iterations: 0`
reproduces Phase-4 behaviour at zero reviewer cost.

Phase 4 pre-wired the seams (`ExplainerInput.RevisionNote`, revision branch in `buildUserPrompt`,
forward-compat frontmatter keys, frontend `reviewing`/`revising` stage strings, token counts on
`CompletionResponse`) — so the real surface area is small.

## Approved design decisions (from brainstorm)

1. **Pass authority** — trust the LLM's `pass` bool as single source of truth. Score is advisory
   (UI + frontmatter only, never gates). Remove the PRD's self-contradictory numeric threshold from
   the reviewer system prompt.
2. **Parse failure** — on malformed reviewer JSON, STOP the loop and save the current explainer with
   `{Pass:false, Score:0}` → `review_passed: false`. No blind no-guidance regeneration. Reviewer
   LLM/network error still fails the session (recoverable) unchanged.

## Phases

| Phase | Title | Status | Depends on |
|-------|-------|--------|-----------|
| [01](phase-01-models-config.md) | Models, Session Accessors & Config | complete | — |
| [02](phase-02-reviewer-agent.md) | ReviewerAgent & Revision-Note Formatter | complete | 01 |
| [03](phase-03-orchestrator-loop-vault.md) | Orchestrator Loop, Reviewer Interface & Vault Frontmatter | complete | 01, 02 |
| [04](phase-04-status-dto-frontend.md) | Status DTO & Frontend Progress UI | complete | 01, 03 |
| [05](phase-05-tests-docs.md) | Tests & Docs | complete | 01–04 |

## Non-Goals

Different provider per agent · section-level regeneration · reviewer score history ·
human feedback loop · retry-on-parse-failure. (All PRD non-goals or explicitly rejected.)

## Key Dependencies

- Reviewer reuses the single `LLMConfig` provider/model from Phase 3 (`llm.LLMClient`).
- Revision text is consumed by the existing `buildUserPrompt` revision branch — do not rebuild it.
- Cost: default `max=2` ≈ ~200k tok/paper (2 gen + 2 review). Documented in README.

## Docs impact

`docs/architecture.md`, `docs/development-roadmap.md`, `docs/project-changelog.md`, `README` — Phase 05.
