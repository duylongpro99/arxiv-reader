# Phase 4 — Brainstorm Summary

## ArXiv AI Paper Explainer Agent — Explainer Generation & Vault Write

**Date:** 2026-07-05
**Outcome:** Design approved (text-only). Ready for `/ck:plan`.

---

## 1. Problem Statement

Turn the extracted paper Markdown (already in `session.Markdown()` after Phase 3)
into a re-teaching explainer note, written atomically to the Obsidian vault and
previewable in the UI. First phase where the user sees real product value.

## 2. Critical Finding — PRD contradicted the codebase

`docs/phase4/prd.md` was authored against a **PDF-page-images + vision-model**
architecture. Phases 1–3 and `docs/architecture.md` built a **text-only
HTML→Markdown** pipeline. The PRD's Go samples do not compile against live code.

Verified mismatches:

| PRD assumed | Codebase reality |
|---|---|
| `ExplainerInput{ PageImages [][]byte }`, `PDFRendererTool`, `pdfFetchTool` | `LLMClient` is text-only (`DocumentText`; comment: "deliberately no PageImages field"). Input is `session.Markdown()` from `PaperContentTool`. |
| System prompt: "read every page image / diagram" | Paper arrives as Markdown text (math/nav/bib stripped, headings + figure captions kept). |
| `paper.Published.Format(...)`, `paper.Category` | `Paper.Published` is a **string**; **no `Category` field** (category is config `arxiv_category`). |
| `session.PDF/PageImages/TokensUsed`, `o.setSession`, exported `session.Stage =` | Session fields **private, mutex-guarded, accessor-only**; stored in `sync.Map`, mutated in place. |
| `ExplainerOutput`, `ReviewVerdict` in use | Neither type exists yet. `ReviewVerdict` is Phase 5. |
| stages generating/writing/complete exist | Only discovery/selection/extracting/failed defined. |

## 3. Decision — Text-only (as built)

Chosen over pivoting to vision. Rationale:
- Keeps Phase 3 HTML→Markdown work; no rework.
- No PDF rasterization → no CGO/poppler (explicitly avoided in Phase 3).
- Any text-capable model works; lower token cost.
- Honors documented tradeoff **T1** (text-only accepted) in `architecture.md`.
- Accepted limitation **R4**: diagrams/tables reach the model only via surviving
  figure captions.

Vision deferred, not designed-in (YAGNI). Escape hatch: an optional image channel
could be added later if caption-only proves insufficient.

## 4. Approved Design

### Models — `internal/models/explainer.go`
```go
type ExplainerOutput struct {
    PaperID      string
    Content      string            // full Markdown (title + 9 sections)
    Sections     map[string]string // parsed by heading; best-effort
    Iteration    int               // 1 in Phase 4
    InputTokens  int
    OutputTokens int
    CreatedAt    time.Time
}
```
`ReviewVerdict` stays out (Phase 5).

### ExplainerAgent — new `internal/agents` package
- `Generate(ctx, ExplainerInput{ MarkdownText, PaperMeta models.Paper, RevisionNote string }) (ExplainerOutput, error)`
- Calls `llm.LLMClient.Complete` with Markdown in `DocumentText`; temp/max-tokens from config.
- System prompt rewritten for **text**: drop image/diagram-reading language; keep
  re-teach-not-summarize, author-intent-first, layered analogies, math handling,
  9 exact `## ` headings, glossary 8–10 by importance, follow-up rules, ~2,500-word
  soft target, tone. Figures: captions are in-text; reference them, flag
  caption-only diagrams (R4).
- `RevisionNote` wired, always `""` in Phase 4 (Phase 5 seam).
- Section parse splits on `## ` → `sectionKeys`; missing sections = warning, full
  `Content` still saved (R1 graceful fallback).
- Split files to stay <200 lines: `explainer.go` + `explainer-prompt.go`.

### VaultWriterTool — new `internal/tools/vaultwriter.go`
- `WriteToVault(ctx, explainer, paper) (string, error)` — **no verdict param** (Phase 5 adds).
- Frontmatter (reconciled to real `Paper`):
  - `published` ← `paper.Published` string (date part; not `.Format()`)
  - `category` ← `config.Agent.ArxivCategory`
  - `review_iterations: 1`, `review_passed: true` — **included now** (forward-compatible;
    spares Phase 5 from re-scheming existing notes)
  - plus `arxiv_id`, `title` (YAML-escaped), `authors` (YAML list), `generated_at`
    (RFC3339 UTC), `tags: [ai, paper, explainer]`
- Filename `YYYY-MM-DD_arxivID_slug.md`; date parsed from `Published`; `slugify`
  (lowercase, spaces→`-`, strip non `[a-z0-9-]`, collapse, ≤60 at word boundary);
  arXiv ID sanitized.
- Atomic: `MkdirAll("AI Papers")` → write `.tmp` → `os.Rename` → cleanup tmp on
  failure. Path-traversal guard vs configured vault base.

### LogCheckTool.MarkAsProcessed — implement stub
Read log (missing=empty, **corrupt=hard error, never clobber**), append entry,
atomic temp→rename. Failure after vault write = **warning only** (note saved;
paper re-surfaces).

### Orchestrator — extend `runPipeline` past the Phase 4 seam
After `SetMarkdown`: `SetStage(generating)` → `Generate` → store explainer + sum
tokens → `SetStage(writing)` → `WriteToVault` → `MarkAsProcessed` →
`SetStage(complete)`, store vault path. Vault-write failure → `Fail` (recoverable:
permission/disk=false, else true), log not updated.
- New session accessors (mutex-guarded, server-only): `SetExplainer/Explainer`,
  `SetVaultFile/VaultFile`, `AddTokens/TokensUsed`, `SelectedPaper()` getter.
  `Snapshot()` unchanged.
- New stages: `generating`, `writing`, `complete`.
- `GET /result/:sessionId` → `HandleResult`: 404 unless complete; returns
  `{content, vaultFile, tokensUsed}`. Register route in server.

### Frontend
Add `react-markdown` + `remark-gfm`. New `app/api/result/route.ts` proxy →
`:8080/result`. `ResultPanel` (SuccessBanner + TokenUsage + MarkdownPreview) when
stage `complete`. New progress labels. Planner must inspect existing
`frontend/app` + components before slotting these in.

### Docs reconciliation (approved)
Rewrite `docs/phase4/prd.md` to the text-only reality as a plan task, so docs stop
contradicting the code.

## 5. Explicitly out of scope
No PDFFetchTool / PDFRendererTool / LLMClient image channel / vision constraint /
`ReviewVerdict` / review loop (Phase 5 or discarded).

## 6. Risks

| ID | Risk | Mitigation |
|---|---|---|
| R1 | LLM ignores section structure | Exact `## ` headings; parser falls back, full content saved; Phase 5 reviewer later. |
| R4 | Diagrams/tables lost (text-only) | Figure captions preserved; prompt flags caption-only diagrams. |
| — | `Published` string format varies | Parse RFC3339, fall back to first 10 chars for date. |
| — | Frontend not deep-read yet | Planner inspects existing components before implementation. |

## 7. Success criteria (from PRD, still valid)
9 sections present; valid YAML frontmatter; ~2,500-word soft target for typical
paper; atomic write leaves no `.tmp`; `processed.json` updated only after successful
write; `GET /result` returns content + vault path + tokens; Markdown preview renders.

## 8. Next steps
1. `/ck:plan` from this summary → phased implementation plan.
2. Plan includes a docs-reconciliation task (rewrite `docs/phase4/prd.md`).
