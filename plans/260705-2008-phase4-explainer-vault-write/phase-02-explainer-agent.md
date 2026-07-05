# Phase 02 — ExplainerAgent (text-only)

**Context:** `docs/phase4/brainstorm-summary.md` §4 (ExplainerAgent) · `docs/phase4/prd.md` F1/F2/F3 (intent only — ignore its image/PDF mechanics) · `backend/internal/llm/client.go`
**Priority:** Critical · **Status:** complete · **Depends on:** 01 · **Effort:** ~M

## Overview
The product's intelligence. `ExplainerAgent.Generate` sends the paper Markdown to the existing
text-only `LLMClient`, parses the response into the 9 sections, and returns an `ExplainerOutput`.
Its value is the **prompt**. This phase is independent of VaultWriter (Phase 03) — both need only
Phase 01.

## Key insights (locked decisions)
- **NEW package `internal/agents`** (first agent; mirrors `internal/tools` layout + doc-comment style).
- **Text-only.** Paper Markdown goes in `CompletionRequest.DocumentText` — the client already
  prefixes it "Paper content:" and sends it as text (see `client.go` comment). **No PageImages
  field exists and none is added.** Rewrite the PRD's system prompt to remove all
  "page images / read every diagram" language.
- **Consumer interface lives in the orchestrator (Phase 04)**, not here — this package exports the
  concrete `*ExplainerAgent`. Keeps the dependency direction correct (orchestrator depends on a
  local interface it defines).
- **`RevisionNote` wired but always `""` in Phase 4.** The `buildUserPrompt` handles the non-empty
  case now so Phase 5 needs zero restructuring — but the orchestrator never sets it this phase.
- **Graceful section parse (R1).** Split on `## ` headings, map by exact heading → slug. Missing
  sections → `slog.Warn`, still return full `Content` (never fail generation on a parse miss).
- **File size <200 lines** → split:
  - `explainer.go` — struct, `New`, `Generate`, `parseSections`.
  - `explainer-prompt.go` — `systemPrompt` const + `buildUserPrompt`.

## Requirements (PRD F1, F2, F3 — text interpretation)
- Types:
  ```go
  type ExplainerAgent struct { llm llm.LLMClient; cfg *config.Config }
  func New(client llm.LLMClient, cfg *config.Config) *ExplainerAgent

  type ExplainerInput struct {
      MarkdownText string        // from session.Markdown()
      PaperMeta    models.Paper
      RevisionNote string        // "" in Phase 4
  }
  func (a *ExplainerAgent) Generate(ctx context.Context, in ExplainerInput) (models.ExplainerOutput, error)
  ```
- `Generate`:
  1. Build `CompletionRequest{ SystemPrompt, UserPrompt, DocumentText: in.MarkdownText,
     MaxTokens: cfg.LLM.MaxTokens, Temperature: cfg.LLM.Temperature }`.
  2. `a.llm.Complete(ctx, req)` — propagate errors (retry lives inside the client).
  3. Parse `resp.Content` into sections; assemble `ExplainerOutput{ PaperID: in.PaperMeta.ID,
     Content: resp.Content, Sections, Iteration: 1, InputTokens, OutputTokens, CreatedAt: time.Now().UTC() }`.
  4. Log `explainer generation complete` with `tokens_used`, `word_count`, `duration_ms` (§7 observability).
- **System prompt** (text version) preserves: re-teach-not-summarize; author-intent-first;
  two-layer analogies (everyday → engineering bridge); math handling (simple→plain English,
  complex→intent-level, never unexplained); **figures via captions** — "figure captions are
  included in the text; reference them and note where a diagram is central but only its caption is
  available"; exact 9 `## ` headings in order; glossary 8–10 by importance (not alphabetical);
  follow-up rules (extract arXiv IDs `\d{4}\.\d{4,5}` → `https://arxiv.org/abs/ID`, plus 2–3
  "Suggested:" from training); ~2,500-word soft target; practitioner tone.
- **`buildUserPrompt`**: paper metadata (Title, Authors joined, Published **string as-is**, ID) +
  "The paper content is provided as text." If `RevisionNote != ""`, prepend revision instructions
  (Phase 5 shape) — but it is always empty this phase.
- **`parseSections`**: map of the 9 headings → slugs (`Problem Statement`→`problem_statement`, …,
  `Analogies & Intuition`→`analogies`, `Follow-Up Papers`→`follow_up_papers`).

## Related code files
**Create:**
- `backend/internal/agents/explainer.go`
- `backend/internal/agents/explainer-prompt.go`
- `backend/internal/agents/explainer_test.go`

## Design detail
```go
// explainer.go
func (a *ExplainerAgent) Generate(ctx context.Context, in ExplainerInput) (models.ExplainerOutput, error) {
    start := time.Now()
    req := llm.CompletionRequest{
        SystemPrompt: systemPrompt,
        UserPrompt:   a.buildUserPrompt(in),
        DocumentText: in.MarkdownText,
        MaxTokens:    a.cfg.LLM.MaxTokens,
        Temperature:  a.cfg.LLM.Temperature,
    }
    resp, err := a.llm.Complete(ctx, req)
    if err != nil { return models.ExplainerOutput{}, err }
    sections := parseSections(resp.Content) // best-effort; warns on gaps
    out := models.ExplainerOutput{
        PaperID: in.PaperMeta.ID, Content: resp.Content, Sections: sections,
        Iteration: 1, InputTokens: resp.InputTokens, OutputTokens: resp.OutputTokens,
        CreatedAt: time.Now().UTC(),
    }
    slog.Info("explainer generation complete", "paper_id", in.PaperMeta.ID,
        "tokens_used", resp.InputTokens+resp.OutputTokens,
        "word_count", wordCount(resp.Content), "duration_ms", time.Since(start).Milliseconds())
    return out, nil
}
```
> **Config access:** `MaxTokens`/`Temperature` are on `cfg.LLM` (`config.LLMConfig`); the agent
> holds `*config.Config` so Phase 04 can construct it with the shared config. (If preferred, pass
> `*config.LLMConfig` — but the brainstorm spec holds `*config.Config`; keep that for consistency
> with the PRD interface.)

## Implementation steps
1. Create `agents` package; `ExplainerAgent` struct + `New`.
2. `explainer-prompt.go`: `systemPrompt` const (text-only) + `buildUserPrompt` (revision-aware).
3. `explainer.go`: `Generate` + `parseSections` + small `wordCount` helper.
4. Tests with a **fake `llm.LLMClient`**: happy path (all 9 sections parsed, tokens/word_count set);
   missing-section response (warns, still returns full Content); LLM error propagated;
   revision-note path builds the revision prompt (unit-level, even though unused in Phase 4).
5. `go build ./...` + `go test -race ./...` green.

## Todo
- [x] `agents` package + `ExplainerAgent` + `New`
- [x] `explainer-prompt.go` — text-only `systemPrompt` + revision-aware `buildUserPrompt`
- [x] `explainer.go` — `Generate` + `parseSections` + `wordCount`
- [x] Both files <200 lines
- [x] `explainer_test.go` — fake client: happy / missing-section / error / revision-note
- [x] `go test -race ./...` green

## Success criteria
- `Generate` returns all 9 sections for a well-formed response; never fails on a parse gap.
- No image/PDF/vision references anywhere in the prompt or code.
- Token counts + word count logged; errors from the client surface unchanged.

## Risk Assessment
| Risk | L×I | Mitigation |
|---|---|---|
| LLM returns free-form text, no headings | Med×Med | Exact `## ` headings in prompt; parser degrades to warnings; full Content saved; Phase 5 reviewer later. |
| Prompt drifts back toward image language | Low×Med | Prompt is text-only by construction; test asserts no "image"/"page image" substring (optional guard). |
| File exceeds 200 lines | Med×Low | Prompt const isolated in `explainer-prompt.go`. |

## Backwards compatibility
New package, no existing caller. Wired into the pipeline only in Phase 04.

## Rollback
Delete the `agents` package. Nothing else references it until Phase 04.

## Security
No filesystem/network of its own — all I/O via the vetted `LLMClient` (key stays server-side).
Paper metadata is server-held (from candidates), not raw client input.

## Next Steps
Feeds Phase 04 (`runPipeline` calls `Generate`). Parallel with Phase 03.
