# Phase 6 — Brainstorm Summary

**Date:** 2026-07-05 · **Outcome:** PRD realigned to actual architecture; plan at
`plans/260705-2210-phase6-polish-hardening/`.

## Problem statement
`docs/phase6/prd.md` prescribes hardening for a **PDF/vision** product (poppler, `pdftoppm`,
page images, DPI). The shipped product (Phases 1–5) is an **HTML→Markdown** pipeline
(arXiv LaTeXML → `html-to-markdown` → Markdown text → ExplainerAgent). The PRD is
substantially stale and, in parts, un-implementable as written.

## Findings (PRD vs. live code)
1. **PDF→HTML mismatch (decisive).** No poppler/PDF/DPI code anywhere. Stale PRD items:
   F9 (poppler validation), F10 (DPI docs), F4 heuristic (page/DPI token estimate),
   several F1 rows (pdftoppm/PDF render/404/timeout), PDF log events, `session.PDF`,
   `EstimateTokens(pdfBytes)`, `pdf.dpi` config.
2. **Error-system conflict.** PRD proposes a new `PipelineError` struct + factories +
   `failSession(*PipelineError)` + **public-field assignment** on the session. Reality:
   sentinel errors (`errors.Is`) mapped via `describe*()` in `pipeline-errors.go`, and a
   **mutex-encapsulated** `PipelineSession` (private fields + accessors). Implementing the
   PRD literally = a rewrite that breaks the "additive only" promise and the race guarantee.
3. **~40% already built.** README exists; error messages ~80% covered; recoverable flag +
   retry button UI exist; 404 re-pick path exists; explainer already splits in/out tokens;
   38 `slog` calls incl. `pipeline complete`; config validation with named-field errors exists.
4. **Genuinely new work:** retry-from-stage (F2, centerpiece — today Retry restarts discovery
   from scratch), cost estimation (F3), text-based context pre-check (F4), arXiv retry counter
   (F5); plus audits for logging (F6), README (F11), E2E (F8).

## Decisions (user-approved)
| # | Decision | Rationale |
|---|----------|-----------|
| D1 | **Realign PRD to HTML reality**; drop F9/F10, re-express F1/F4/F6 in HTML terms | Phase 6 hardens the product that exists; not a PDF feature. |
| D2 | **Extend the existing sentinel + `describe*` + encapsulated-session design**; no `PipelineError` rewrite | KISS, truly additive, preserves the documented race guarantee. |
| D3 | **Scope to net-new + audits** | Don't re-touch what already satisfies the PRD. |
| D4 | **Segment-level resumable pipeline** for retry (not mid-loop) | Keeps Phase 5 review-loop logic intact; cached `Markdown`/`Explainer` skip segments — vault retry costs no LLM re-call. |
| D5 | **Split reviewer tokens** (`ReviewVerdict` gains in/out) | Output priced ~4–5× input; review loop is the bigger spender at `max_review_iterations:2`. |

## Key design points
- Retry resumes by skipping cached segments; routes by a new `failedStage` captured in `Fail()`.
- Cost/limits in two config-adjacent files (`llm/pricing.go`, `llm/limits.go`), dated, labelled approximate.
- Context pre-check is text-length based (`len(md)/4`), advisory/non-blocking.
- arXiv retry surfaced via an `onRetry(attempt)` callback (keeps `DiscoveryTool` decoupled from the session).

## Risks
R1 reviewer token split touches `ReviewVerdict`+reviewer (small). R2 resume × review-loop →
segment granularity only. R3 `FetchPapers` signature change (callback). R4 pricing/limits
staleness → single dated file + UI/README caveat.

## Success metrics
Every real error → correct UI message + action; retry preserves selection; cost shown for
priced models; logs fully reconstruct a run; fresh setup <10 min; E2E passes across 3 providers.
