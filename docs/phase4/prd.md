# Phase 4 — Explainer Generation & Vault Write
## ArXiv AI Paper Explainer Agent

> **Reconciliation Notice:** Supersedes the earlier image-based draft; reconciled to the text-only implementation on 2026-07-05. See `brainstorm-summary.md` for the design decision rationale.

---

## Intent

This is the phase where the product's core promise is fulfilled: **a practitioner selects a paper and receives a rich, intuitive explanation saved permanently to their Obsidian vault.**

Everything built in Phases 1–3 was infrastructure. Phase 4 is where the agent does its actual work — reading the paper's extracted Markdown content (text-only, from Phase 3's HTML→Markdown pipeline), understanding what the authors were really trying to achieve, and re-explaining it in a way that makes a technical practitioner genuinely smarter. The output is not a summary. It is a re-teaching of the paper's core intent in the clearest possible terms.

Every decision in this phase serves that re-teaching mission. Prompt design, section structure, vault output format, and file naming all exist to produce a note the practitioner will actually read, trust, and return to. While diagrams and visual elements are preserved as figure captions in the Markdown, the core insight extraction and re-explanation are text-driven — this keeps token costs reasonable and is compatible with any text-capable LLM.

---

# Part 1 — Product Requirements

## 1. Problem Statement

A practitioner has selected a paper. The system has rendered every page as an image — text, diagrams, tables, equations all preserved. Now it must do what no simple summarizer does: understand why the authors wrote the paper, what problem they were really solving, and how to explain that to someone who is technically literate but not a domain expert.

The output must be saved permanently to Obsidian in a format that is immediately readable, well-structured, and useful without post-editing. This is the first phase where the end user sees real value from the product.

## 2. Target Users

**Primary:** Technical practitioners — ML engineers and developers who want to understand a paper's core value without reading it in full.

## 3. User Stories

- As a practitioner, I want the agent to explain the paper's core purpose in plain terms so that I understand what the authors were really trying to achieve, not just what they did.
- As a practitioner, I want analogies that bridge everyday intuition to engineering concepts so that I can build a mental model I can actually use.
- As a practitioner, I want a glossary of the most important terms so that I can understand the paper's language without getting lost.
- As a practitioner, I want the explanation saved to my Obsidian vault automatically so that it becomes part of my permanent knowledge base without any manual steps.
- As a practitioner, I want to see a preview of the generated note in the UI so that I can verify it before navigating to Obsidian.
- As a practitioner, I want live progress updates during generation so that I know the system is working and approximately how far along it is.

## 4. Functional Requirements

### F1 — ExplainerAgent Generation
The agent must produce a Markdown document with the following sections, in order:

| Section | Purpose |
|---|---|
| **Problem Statement** | What problem are the authors solving? Why does it matter? What was broken or missing before this paper? |
| **Core Idea** | The central contribution — explained intuitively first (everyday analogy), then bridged to an engineering mental model |
| **Methodology** | How did they approach the solution? Math handled contextually: translate simple equations to plain English; summarize complex proofs at intent level only |
| **Key Findings** | What did they prove, demonstrate, or measure? Concrete results, not vague claims |
| **Limitations** | What does the paper explicitly acknowledge it doesn't solve? What are the obvious gaps? |
| **Why It Matters** | Real-world implications for practitioners — what can you do differently or better because this paper exists? |
| **Analogies & Intuition** | Layered explanations: everyday analogy first, then engineering-anchored bridge. One or more per key concept |
| **Glossary** | Top 8–10 terms essential to understanding the paper's contribution, prioritized by importance — not alphabetical |
| **Follow-Up Papers** | Suggested reading from the paper's reference list (with arXiv links where extractable) + agent's training knowledge |

### F2 — Generation Quality Rules
The agent must follow these rules during generation:
- **Re-teach, don't summarize** — the goal is to make the reader understand, not to compress the paper
- **Author intent first** — always lead with why the authors wrote the paper before what they built
- **Analogy approach** — everyday intuition layer first, engineering mental model layer second
- **Math handling** — simple equations translated to plain English; complex proofs summarized at intent level only; never left as-is without explanation
- **Soft word target** — approximately 2,500 words; agent may exceed for genuinely complex papers
- **Tone** — respects practitioner intelligence; does not over-simplify or over-formalize

### F3 — Follow-Up Paper Links
- Agent attempts to extract arXiv IDs from the paper's reference list
- arXiv ID pattern: `\d{4}\.\d{4,5}` (e.g. `2401.12345`)
- Identified references rendered as: `[Paper Title](https://arxiv.org/abs/2401.12345)`
- References without identifiable arXiv IDs rendered as plain text titles
- Agent also suggests related papers from training knowledge, clearly labelled as "Suggested"

### F4 — Obsidian Vault Write
- Final Markdown is written to `{obsidian_vault}/AI Papers/{filename}.md`
- `AI Papers/` subfolder is created automatically if it doesn't exist
- Filename format: `YYYY-MM-DD_arxivID_slug-title.md`
  - Slug: lowercase, spaces → hyphens, special characters stripped, max 60 characters
- File includes YAML frontmatter with: `arxiv_id`, `title`, `authors`, `published`, `category`, `generated_at`, `review_iterations`, `review_passed`, `tags`
- Write is atomic: write to `.tmp` file first, then `os.Rename()` to final path
- Partial or corrupt files must never be left on disk

### F5 — Processed Log Update
- `processed.json` is updated immediately after successful vault write
- Log entry includes: `paper_id`, `title`, `processed_at`, `vault_file`
- Log is NOT updated if vault write fails — paper remains re-processable

### F6 — Result Delivery
- `GET /result/:sessionId` returns the generated Markdown content and vault file path
- Next.js renders a Markdown preview of the note in the UI
- UI displays: success message, vault file path, token usage count, "Open in Obsidian" hint

### F7 — Progress Updates (Phase 4 additions)
- New stage labels:
  - `"Generating explainer (pass 1)..."` (`generating`)
  - `"Saving to vault..."` (`writing`)
  - `"Complete"` (`complete`)

## 5. Non-Functional Requirements

- **Quality over speed** — generation takes as long as the LLM needs; 30–120 seconds is acceptable
- **No partial output** — the vault must never contain an incomplete note
- **Idempotency** — if a run fails after vault write but before log update, re-running produces a second note file (acceptable — paper re-surfaces); the system does not attempt deduplication at write time
- **Readability** — the generated note must be immediately readable in Obsidian without post-editing

## 6. Success Metrics

- Generated note contains all 9 required sections
- Frontmatter is valid YAML and renders correctly in Obsidian
- Soft word target (~2,500 words) is met for typical papers; agent exceeds it only for genuinely complex papers
- Note requires no post-editing to be useful and shareable
- `processed.json` is updated correctly after every successful run
- Atomic write guarantees no partial files on disk under any failure scenario

## 7. Scope & Non-Goals

**In scope:**
- ExplainerAgent system prompt and generation logic
- VaultWriterTool (frontmatter, filename, atomic write)
- LogCheckTool write path (`MarkAsProcessed`)
- Follow-up paper arXiv ID extraction and link construction
- `GET /result/:sessionId` endpoint
- Markdown preview UI
- Progress updates for generation and writing stages

**Out of scope:**
- ReviewerAgent and revision loop (Phase 5)
- Multi-pass generation (Phase 5)
- Any UI beyond preview and success state
- Obsidian plugin integration (non-goal)

## 8. Open Questions

None for this phase. All requirements are fully defined.

---

# Part 2 — Architecture

## Intent

Phase 4 introduces the two most intellectually significant components: `ExplainerAgent` and `VaultWriterTool`. The ExplainerAgent's value is entirely in its prompt design — the architecture must give the prompt full control over the output structure without fighting the LLM. The VaultWriterTool's value is in reliability — an atomic write strategy ensures the vault is never left in a broken state, no matter what.

The design is **text-only**: the agent receives Markdown extracted from arXiv HTML (Phase 3 output). This choice maximizes provider flexibility, reduces token costs, and is sufficient to capture paper intent via text and figure captions.

---

## 1. System Overview

```
[Phase 3 left off: Markdown extracted from arXiv HTML, pipeline running async]
    │
    ▼
session.SetStage("generating")
    │
    ▼
ExplainerAgent.Generate(ExplainerInput{ MarkdownText, PaperMeta, RevisionNote: "" })
    │
    ▼
LLMClient.Complete(CompletionRequest{ SystemPrompt, UserPrompt, DocumentText })
    │ (DocumentText = MarkdownText from Phase 3, never images)
    ▼
ExplainerOutput { Content (full Markdown), Sections (map), Iteration: 1, InputTokens, OutputTokens }
    │
    ▼ (Phase 5 inserts ReviewerAgent loop here)
    │
    ▼
session.SetStage("writing")
    │
    ▼
VaultWriterTool.WriteToVault(ExplainerOutput, Paper)
    ├── Assemble frontmatter (with date parsed from Paper.Published string)
    ├── Generate filename
    ├── Atomic write to Obsidian vault
    └── Call LogCheckTool.MarkAsProcessed()
    │
    ▼
session.SetStage("complete"), session.SetVaultFile(...), session.AddTokens(...)
    │
    ▼
Next.js: GET /result/:sessionId → renders Markdown preview + success state
```

---

## 2. Component Breakdown

### 2.1 ExplainerAgent

**Intent:** This is the product's intelligence. Its only job is to deeply understand a research paper (via its extracted Markdown) and re-explain it in a way that makes a practitioner genuinely smarter. The architecture must support this intent — the agent gets the full paper text, a detailed system prompt, and enough tokens to do the job properly.

**Why a dedicated agent (not a simple LLM call):** The ExplainerAgent encapsulates the full reasoning task — Markdown ingestion, intent extraction, analogy construction, math interpretation, glossary prioritization. Wrapping this in a focused agent gives it a clear boundary and makes it independently testable. In Phase 5, the same agent accepts revision feedback and rewrites — a clean interface enables that without restructuring.

**Interface:**
```go
// /internal/agents/explainer.go

type ExplainerAgent struct {
    llm llm.LLMClient
    cfg *config.Config
}

type ExplainerInput struct {
    MarkdownText string           // extracted paper Markdown from PaperContentTool (Phase 3)
    PaperMeta    models.Paper     // { ID, Title, Authors, Abstract, PDFURL, Published (string) }
    RevisionNote string           // empty on first pass; populated by ReviewerAgent in Phase 5
}

func New(client llm.LLMClient, cfg *config.Config) *ExplainerAgent

func (a *ExplainerAgent) Generate(ctx context.Context, in ExplainerInput) (models.ExplainerOutput, error)
```

**System prompt (text-only version):**
```
You are an expert AI research explainer. Your audience is technical practitioners —
software engineers and ML engineers who understand the basics of machine learning
but do not track academic research closely.

Your mission is NOT to summarize papers. Your mission is to re-teach them.

You will receive the paper as extracted Markdown text. This text preserves the
paper's structure, headings, and figure captions. While some visual elements
(diagrams, tables) may not render perfectly in text form, their captions are
included — describe what those captions tell you about the paper's findings.

For every paper you receive, you must:
1. Identify the core problem the authors set out to solve — not what they built,
   but what was broken or missing that motivated the work.
2. Understand the authors' core insight — the key idea that makes their approach
   work, expressed as clearly as possible.
3. Re-explain both to a practitioner who is intelligent but not a domain expert.

APPROACH:
- Lead with author intent — always explain WHY before WHAT.
- Use analogies layered in two steps:
    Step 1: Everyday analogy — something anyone can picture.
    Step 2: Engineering bridge — connect the analogy to a concept the practitioner
            already knows (APIs, pipelines, data structures, etc.).
- Handle math contextually:
    - Simple equations (loss functions, basic metrics): translate to plain English.
      Explain what the equation computes and why that matters, not the notation.
    - Complex proofs or derivations: summarize at intent level only.
      ("This proof shows that X is always bounded by Y, which guarantees the
       training process converges — you don't need the derivation to use this.")
    - Never leave math unexplained.
- Handle figure captions:
    - Describe what each caption tells you about the paper's findings.
    - If a diagram or table's caption is vague, flag it: "The paper includes a
      diagram here but the caption is brief — this likely shows [your inference]."
    - Reference captions by their position when needed: "As the methodology diagram
      shows..."

TONE:
- Respect the reader's intelligence. Do not over-simplify.
- Do not over-formalize. This is not an academic review.
- Write as if explaining to a smart colleague over coffee.

LENGTH:
- Target approximately 2,500 words.
- You may exceed this for genuinely complex papers where cutting content would
  sacrifice understanding. Do not pad for simple papers.

OUTPUT FORMAT:
Produce exactly the following sections in this order, using these exact headings:

## Problem Statement
## Core Idea
## Methodology
## Key Findings
## Limitations
## Why It Matters
## Analogies & Intuition
## Glossary
## Follow-Up Papers

GLOSSARY RULES:
- Include exactly 8–10 terms.
- Prioritize by importance to understanding the paper's core contribution.
- Do NOT list alphabetically. List by importance.
- Format: **Term** — one-sentence plain-English definition.

FOLLOW-UP PAPERS RULES:
- List papers from the reference list that are most relevant to understanding
  this paper's contribution. Include arXiv links where you can identify the
  arXiv ID from the reference (format: https://arxiv.org/abs/XXXX.XXXXX).
- Also suggest 2–3 related papers from your training knowledge that a
  practitioner interested in this work should read. Label these clearly as
  "Suggested:" to distinguish them from reference-list papers.
```

**User prompt construction:**
```go
func (a *ExplainerAgent) buildUserPrompt(in ExplainerInput) string {
    prompt := fmt.Sprintf(
        "Paper metadata:\nTitle: %s\nAuthors: %s\nPublished: %s\narXiv ID: %s\n\n"+
        "Paper content:\n%s\n\n"+
        "Please read the paper carefully and generate the explainer.",
        in.PaperMeta.Title,
        strings.Join(in.PaperMeta.Authors, ", "),
        in.PaperMeta.Published,  // Published is already a string (e.g., "2024-01-12" or RFC3339)
        in.PaperMeta.ID,
        in.MarkdownText,
    )

    if in.RevisionNote != "" {
        prompt = "REVISION INSTRUCTIONS:\n" + in.RevisionNote +
                 "\n\n---\n\nOriginal paper metadata:\n" + prompt +
                 "\n\nPlease revise the explainer according to the instructions above."
    }

    return prompt
}
```

**Section parsing:**
```go
// Parse ExplainerOutput.Sections from raw Markdown content.
// Splits on "## " headings, maps recognized headings to section slugs.
// Missing sections are logged as a warning but do not fail generation.
var sectionKeys = map[string]string{
    "Problem Statement":     "problem_statement",
    "Core Idea":             "core_idea",
    "Methodology":           "methodology",
    "Key Findings":          "key_findings",
    "Limitations":           "limitations",
    "Why It Matters":        "why_it_matters",
    "Analogies & Intuition": "analogies",
    "Glossary":              "glossary",
    "Follow-Up Papers":      "follow_up_papers",
}
```

**ExplainerOutput data:**
```go
type ExplainerOutput struct {
    PaperID      string            // from in.PaperMeta.ID
    Content      string            // full Markdown: "# Title\n## Problem Statement\n…"
    Sections     map[string]string // keyed by section slug; best-effort (missing sections = warning)
    Iteration    int               // 1 in Phase 4 (revision loop is Phase 5)
    InputTokens  int               // from LLM response
    OutputTokens int               // from LLM response
    CreatedAt    time.Time         // time.Now().UTC()
}
```

**Dependencies:** `llm.LLMClient`, `models`, `config`

---

### 2.2 VaultWriterTool

**Intent:** This tool has one job: reliably write the final note to the Obsidian vault. "Reliably" means atomically — the vault must never contain a partial or corrupt file. The tool is simple by design; its value is in what it guarantees, not what it computes.

**Interface:**
```go
// /internal/tools/vaultwriter.go

type VaultWriterTool struct {
    cfg      *config.Config
    logCheck *LogCheckTool
}

func NewVaultWriterTool(cfg *config.Config, logCheck *LogCheckTool) *VaultWriterTool

func (t *VaultWriterTool) WriteToVault(
    ctx context.Context,
    ex models.ExplainerOutput,
    p models.Paper,
) (string, error)
// NO verdict param in Phase 4 (Phase 5 adds review state). Returns the final vault file path.
```

**Frontmatter assembly:**
```go
func (t *VaultWriterTool) buildFrontmatter(p models.Paper, ex models.ExplainerOutput) string {
    // Published is a string; extract date part (RFC3339, first 10 chars, or "unknown" fallback)
    publishedDate := dateOnly(p.Published)  // e.g., "2024-01-12"

    // category comes from config, NOT from Paper (Paper has no Category field)
    return fmt.Sprintf(`---
arxiv_id: %s
title: %s
authors:
%s
published: %s
category: %s
generated_at: %s
review_iterations: 1
review_passed: true
tags: [ai, paper, explainer]
---

`,
        escapeYAML(p.ID),
        escapeYAML(p.Title),
        formatAuthorsYAML(p.Authors),  // YAML list format
        escapeYAML(publishedDate),
        escapeYAML(t.cfg.Agent.ArxivCategory),  // from config, not paper
        escapeYAML(ex.CreatedAt.UTC().Format(time.RFC3339)),
    )
}
```

**Filename generation:**
```go
func (t *VaultWriterTool) generateFilename(p models.Paper) string {
    date := dateOnly(p.Published)        // YYYY-MM-DD
    id := sanitizeArxivID(p.ID)          // strip non-alphanumeric, lowercase
    slug := slugify(p.Title)              // lowercase, spaces→hyphens, strip special chars, max 60 chars
    return fmt.Sprintf("%s_%s_%s.md", date, id, slug)
}

func dateOnly(s string) string {
    // Triple fallback for robustness:
    // 1. Parse RFC3339 → format as 2006-01-02
    // 2. If first 10 chars look like YYYY-MM-DD, use them
    // 3. Otherwise return "unknown"
}

func slugify(title string) string {
    // 1. Lowercase and trim
    // 2. Replace spaces with hyphens
    // 3. Strip characters not in [a-z0-9\-]
    // 4. Collapse multiple hyphens
    // 5. Trim to 60 characters at word (hyphen) boundary
}

func sanitizeArxivID(id string) string {
    // Lowercase and strip any character outside [a-z0-9._-]
}
```

**Atomic write:**
```go
func (t *VaultWriterTool) WriteToVault(ctx context.Context, ex models.ExplainerOutput, p models.Paper) (string, error) {
    vaultDir := filepath.Join(t.cfg.Paths.ObsidianVault, "AI Papers")
    filename := t.generateFilename(p)
    finalPath := filepath.Join(vaultDir, filename)

    // 1. Validate vault path (prevent path traversal)
    if err := validateWithinBase(t.cfg.Paths.ObsidianVault, finalPath); err != nil {
        return "", err
    }

    // 2. Create vault directory if needed
    if err := os.MkdirAll(vaultDir, 0o755); err != nil {
        return "", fmt.Errorf("create vault dir: %w", err)
    }

    // 3. Assemble full content (frontmatter + markdown body)
    content := t.buildFrontmatter(p, ex) + ex.Content

    // 4. Write to temp file first
    tmp := finalPath + ".tmp"
    if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
        return "", fmt.Errorf("write temp: %w", err)
    }

    // 5. Atomic rename temp → final path
    if err := os.Rename(tmp, finalPath); err != nil {
        os.Remove(tmp)  // best-effort cleanup; never leave a partial ".tmp"
        return "", fmt.Errorf("finalize: %w", err)
    }

    // 6. Update processed log ONLY after successful write
    // Failure here is logged as warning — note exists regardless, paper re-surfaces if log fails
    if err := t.logCheck.MarkAsProcessed(p, filename); err != nil {
        slog.Warn("vault write succeeded but log update failed",
            "paper_id", p.ID, "vault_file", filename, "error", err.Error())
    }

    return finalPath, nil
}

func validateWithinBase(base, target string) error {
    // Prevent path traversal: ensure target is under base (with separator-aware prefix check)
    cleanBase := filepath.Clean(base)
    cleanTarget := filepath.Clean(target)
    if cleanTarget == cleanBase {
        return fmt.Errorf("refusing to write vault base itself: %q", cleanTarget)
    }
    prefix := cleanBase + string(os.PathSeparator)
    if !strings.HasPrefix(cleanTarget, prefix) {
        return fmt.Errorf("path %q escapes vault base %q", cleanTarget, cleanBase)
    }
    return nil
}
```

**Dependencies:** `os`, `path/filepath`, `LogCheckTool`, `config`, `slog`

---

### 2.3 LogCheckTool — Write Path

**Intent:** `MarkAsProcessed` is the write half of `LogCheckTool`. It is called exactly once per successful pipeline run, immediately after vault write succeeds. Its failure is logged as a warning, not a fatal error — the note exists regardless.

```go
func (t *LogCheckTool) MarkAsProcessed(paper models.Paper, vaultFile string) error {
    // 1. Read existing log (missing=empty, corrupt=hard error, never clobber)
    log, err := t.readLog()
    if err != nil && !os.IsNotExist(err) {
        return err
    }

    // 2. Append new entry
    entry := ProcessedEntry{
        PaperID:     paper.ID,
        Title:       paper.Title,
        ProcessedAt: time.Now().UTC(),
        VaultFile:   vaultFile,
    }
    log.Processed = append(log.Processed, entry)

    // 3. Atomic write: marshal → temp file → rename
    data, _ := json.MarshalIndent(log, "", "  ")
    tmpPath := t.cfg.Paths.LogFile + ".tmp"
    if err := os.WriteFile(tmpPath, data, 0o644); err != nil {
        return err
    }
    return os.Rename(tmpPath, t.cfg.Paths.LogFile)
}
```

---

### 2.4 Orchestrator — Full Pipeline (Phase 4)

**Intent:** The Orchestrator's `runPipeline` function coordinates the text-only explainer generation and vault write. Phase 5 will insert the review loop between ExplainerAgent and VaultWriterTool.

**Key flow:**
```go
func (o *Orchestrator) runPipeline(ctx context.Context, session *models.PipelineSession) {
    // Fetch Markdown from Phase 3's HTML extraction
    md, err := o.content.FetchMarkdown(ctx, session.SelectedPaper().ID)
    if err != nil {
        session.Fail(err.Error(), true)
        return
    }
    session.SetMarkdown(md)

    // Generate explainer from Markdown
    session.SetStage(models.StageGenerating)
    
    explainer, err := o.explainer.Generate(ctx, agents.ExplainerInput{
        MarkdownText: md,
        PaperMeta:    *session.SelectedPaper(),
        RevisionNote: "",  // Phase 4: always empty (Phase 5 populates)
    })
    if err != nil {
        session.Fail(err.Error(), true)
        return
    }
    session.SetExplainer(&explainer)
    session.AddTokens(explainer.InputTokens + explainer.OutputTokens)

    // Phase 5: ReviewerAgent loop inserted here

    // Write to vault
    session.SetStage(models.StageWriting)
    
    vaultPath, err := o.vault.WriteToVault(ctx, explainer, *session.SelectedPaper())
    if err != nil {
        session.Fail(err.Error(), false)  // recoverable unless permission/disk-full
        return
    }
    session.SetVaultFile(vaultPath)
    session.SetStage(models.StageComplete)
}
```

**Session accessors (mutex-guarded, server-only, NOT in Snapshot):**
```go
func (s *PipelineSession) SetExplainer(e *ExplainerOutput)
func (s *PipelineSession) Explainer() *ExplainerOutput

func (s *PipelineSession) SetVaultFile(path string)
func (s *PipelineSession) VaultFile() string

func (s *PipelineSession) AddTokens(n int)
func (s *PipelineSession) TokensUsed() int

func (s *PipelineSession) SelectedPaper() *Paper
```

**New stage constants:**
```go
const (
    StageGenerating PipelineStage = "generating"  // Phase 4: explainer generation
    StageWriting    PipelineStage = "writing"     // Phase 4: atomic vault write
    StageComplete   PipelineStage = "complete"    // Phase 4: note saved; /result is ready
)
```

**New `GET /result/:sessionId` handler:**
```go
type ResultResponse struct {
    Content    string `json:"content"`
    VaultFile  string `json:"vaultFile"`
    TokensUsed int    `json:"tokensUsed"`
}

func (o *Orchestrator) HandleResult(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("sessionId")
    v, ok := o.sessions.Load(id)
    if !ok {
        writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
        return
    }
    s := v.(*models.PipelineSession)
    
    // Only return result if pipeline reached complete
    if s.Snapshot().Stage != models.StageComplete {
        writeJSON(w, http.StatusNotFound, map[string]string{"error": "result not ready"})
        return
    }
    
    ex := s.Explainer()
    if ex == nil {  // defensive
        writeJSON(w, http.StatusNotFound, map[string]string{"error": "result not ready"})
        return
    }
    
    writeJSON(w, http.StatusOK, ResultResponse{
        Content:    ex.Content,
        VaultFile:  s.VaultFile(),
        TokensUsed: s.TokensUsed(),
    })
}
```

---

### 2.5 Next.js — Preview UI

**Intent:** The preview gives the practitioner immediate access to the generated note without having to open Obsidian. It builds confidence that the output is good before they navigate to the vault.

**New components:**
```
/app/page.tsx
  └── <PipelineView>
        ├── <ProgressIndicator stage={status.stage} />
        └── [when stage === "complete"]
              <ResultPanel>
                ├── <SuccessBanner vaultFile={result.vaultFile} />
                ├── <TokenUsage count={result.tokensUsed} />
                └── <MarkdownPreview content={result.content} />
```

**Markdown rendering:**
- Use `react-markdown` with `remark-gfm` for GitHub-flavoured Markdown rendering
- Render inline in the UI — no modal, no separate page
- Code blocks, tables, and bold text must render correctly (Glossary uses them)

**New dependencies:**
```
react-markdown    — Markdown → React component rendering
remark-gfm        — GitHub Flavored Markdown plugin (tables, strikethrough)
```

**New Next.js API route:**
```typescript
// /app/api/result/route.ts
GET /api/result?sessionId=xxx
  → GET http://localhost:8080/result/{sessionId}
  ← { content: string, vaultFile: string, tokensUsed: number }
```

---

## 3. Data Model

**ExplainerOutput (Phase 4 output):**
```go
type ExplainerOutput struct {
    PaperID      string            // paper's arXiv ID
    Content      string            // full Markdown body: "# Title\n## Problem Statement\n…" (no frontmatter)
    Sections     map[string]string // keyed by section slug; best-effort (missing sections logged as warning)
    Iteration    int               // 1 in Phase 4 (revision loop is Phase 5)
    InputTokens  int               // tokens consumed by LLM
    OutputTokens int               // tokens produced by LLM
    CreatedAt    time.Time         // generation timestamp (UTC)
}
```

**ProcessedLog entry (written for the first time in Phase 4):**
```json
{
  "processed": [
    {
      "paper_id": "2401.12345",
      "title": "Paper Title",
      "processed_at": "2026-06-07T10:30:00Z",
      "vault_file": "2026-06-07_2401.12345_paper-title-slug.md"
    }
  ]
}
```

**Obsidian Note structure (final output):**
```markdown
---
arxiv_id: "2401.12345"
title: "Attention Is All You Need"
authors:
  - Ashish Vaswani
  - Noam Shazeer
published: "2017-06-12"
category: "cs.AI"
generated_at: "2026-06-07T10:30:00Z"
review_iterations: 1
review_passed: true
tags: [ai, paper, explainer]
---

# Attention Is All You Need

## Problem Statement
...

## Core Idea
...

## Methodology
...

## Key Findings
...

## Limitations
...

## Why It Matters
...

## Analogies & Intuition
...

## Glossary
...

## Follow-Up Papers
...
```

---

## 4. Data Flow

### Full Single-Pass Pipeline Flow (Text-Only)

```
[Phase 3 left off: Markdown extracted, async goroutine running]
    │
    ▼
session.SetStage("generating")
    │
    ▼
ExplainerAgent.Generate({
    MarkdownText: session.Markdown(),
    PaperMeta:    paper,
    RevisionNote: ""  // empty in Phase 4
})
    │
    ├── Build system prompt (explainer instructions for text interpretation)
    ├── Build user prompt (metadata + paper Markdown)
    │
    ▼
LLMClient.Complete({
    SystemPrompt: "You are an expert AI research explainer...",
    UserPrompt:   "Title: X, Authors: Y..., Paper content: [Markdown]",
    DocumentText: session.Markdown(),  // paper Markdown rides here, never as images
    MaxTokens:    8000,
    Temperature:  0.3
})
    │
    ▼
LLM response: full Markdown with 9 sections
    │
    ▼
Parse into ExplainerOutput {
    Content:      "# Paper Title\n## Problem Statement\n..."
    Sections:     { "problem_statement": "...", "core_idea": "...", ... }
    Iteration:    1
    InputTokens:  1250
    OutputTokens: 2100
    CreatedAt:    time.Now().UTC()
}
    │
    ▼ [Phase 5: ReviewerAgent loop inserted here]
    │
    ▼
session.SetStage("writing")
    │
    ▼
VaultWriterTool.WriteToVault(explainer, paper)
    ├── Build frontmatter YAML (category from config, published date parsed from paper.Published string)
    ├── Generate filename: "2026-06-07_2401.12345_attention-is-all-you-need.md"
    ├── Concatenate frontmatter + explainer.Content
    ├── Write to: {vault}/AI Papers/{filename}.tmp
    ├── os.Rename() → final path (atomic, sub-millisecond)
    └── LogCheckTool.MarkAsProcessed(paper, filename)
          └── Append to processed.json (atomic temp → rename)
    │
    ▼
session.SetStage("complete"), session.SetVaultFile(...), session.AddTokens(...)
    │
    ▼
Next.js polls GET /status → { stage: "complete" }
Next.js calls GET /result → { content, vaultFile, tokensUsed }
    │
    ▼
UI renders:
    ✓ Success banner with vault file path
    ✓ Token usage: 3,350 tokens
    ✓ Rendered Markdown preview of the full note (sections, lists, code blocks, tables)
```

---

## 5. Tech Stack

**New dependency (Next.js):**

| Package | Why |
|---|---|
| `react-markdown` | Renders Markdown content as React components. Required for preview UI. |
| `remark-gfm` | GitHub Flavored Markdown — enables tables (Glossary), strikethrough, task lists. |

No new Go dependencies in Phase 4. All LLM calls go through the `LLMClient` interface established in Phase 3.

---

## 6. Integration Points

### LLM Provider (via LLMClient, text-only)

**Request characteristics (Phase 4):**
- System prompt: ~900 tokens (explainer instructions, text-specific guidance)
- User prompt: ~120 tokens (paper metadata)
- DocumentText (Markdown): ~50–200 tokens for a typical 10-page paper (text is compact vs images)
- Max output tokens: configured (default 8,000)
- Temperature: configured (default 0.3 — lower for consistent structure)

**Total token budget per call:** ~1,000–1,500 input + 8,000 output for a typical paper. Very long papers (40+ pages, 50KB+ Markdown) may approach model limits, but text-only processing scales well within typical LLM context windows.

### Local Filesystem

**Vault write:**
```
{OBSIDIAN_VAULT_PATH}/
  └── AI Papers/
        └── 2026-06-07_2401.12345_attention-is-all-you-need.md
```

**Log write:**
```
{LOG_FILE_PATH} (processed.json) — appended with new entry
```

Both writes are atomic (temp → rename).

---

## 7. Cross-Cutting Concerns

### Error Handling

| Failure | Behaviour | Recoverable |
|---|---|---|
| LLM generation failure | Surface error, session → failed | Yes — retry run |
| LLM response missing sections | Log warning, proceed with partial content | Yes — output may be incomplete but usable |
| Vault directory permission error | "Cannot write to vault: permission denied" | No — fix filesystem permissions |
| Vault disk full | "Cannot write to vault: disk full" | No — free disk space |
| Vault write failure | session → failed, log NOT updated | Yes — paper re-processable |
| Log update failure after vault write | Logged as warning, note exists in vault | N/A — note already saved |

### Observability

```json
{"level":"INFO","msg":"explainer generation started","session_id":"abc123","paper_id":"2401.12345"}
{"level":"INFO","msg":"explainer generation complete","session_id":"abc123","tokens_used":3350,"duration_ms":42000,"word_count":2618}
{"level":"INFO","msg":"vault write started","paper_id":"2401.12345","filename":"2026-06-07_2401.12345_attention-is-all-you-need.md"}
{"level":"INFO","msg":"vault write complete","paper_id":"2401.12345","path":"/path/to/note.md","duration_ms":12}
{"level":"INFO","msg":"log updated","paper_id":"2401.12345","vault_file":"2026-06-07_2401.12345_attention-is-all-you-need.md"}
{"level":"INFO","msg":"pipeline complete","session_id":"abc123","total_duration_ms":44200}
```

### Security
- Vault path validated against configured base path before every write (path traversal prevention)
- Filename sanitized: only `[a-z0-9\-_.]` allowed
- Temp file cleaned up on rename failure
- YAML frontmatter values escaped to prevent injection into Obsidian metadata
- arXiv IDs sanitized to strip separators and non-alphanumeric characters

---

## 8. Risks & Tradeoffs

| ID | Risk/Tradeoff | Severity | Mitigation |
|---|---|---|---|
| R1 | LLM ignores section structure — returns free-form text | Medium | System prompt uses exact `## ` section headings. Section parser falls back gracefully — missing sections logged as warnings, content still saved. Phase 5 reviewer catches structural failures. |
| R2 | Generated content misrepresents paper findings (hallucination) | Medium | Accepted risk in Phase 4 (single pass). Phase 5 reviewer agent catches factual inconsistencies. arXiv ID and paper link in frontmatter enable source verification. |
| R3 | Word count significantly exceeds 2,500 for simple papers | Low | Soft target in prompt. Reviewer agent in Phase 5 can flag verbosity. Acceptable in Phase 4. |
| R4 | Text-only misses visual-heavy insights (diagrams/tables) | Low | Figure captions preserved in Markdown and passed to LLM. Caption-only interpretation is flagged in prompt ("The paper includes a diagram here but the caption is brief..."). Escape hatch: optional image channel could be added later if needed. |
| R5 | Published string format varies across arXiv responses | Low | dateOnly() function handles RFC3339 parsing, first-10-char fallback, and "unknown" default. Never panics. |
| T1 | No review loop in Phase 4 — single pass only | Accepted | Phase 5 adds the review loop. Phase 4 establishes baseline output quality that Phase 5 improves. Single-pass output is already useful. |
| T2 | `react-markdown` adds a frontend dependency | Accepted | Markdown preview is core to result UX. Without rendering, user sees raw Markdown. `react-markdown` is lightweight and widely maintained. |

---

## Exit Criteria

All of the following must be true before Phase 5 begins:

- [ ] ExplainerAgent produces output containing all 9 required sections for any valid arXiv CS paper
- [ ] Generated Markdown comes from text-only LLM call (DocumentText field, not images)
- [ ] Generated note is saved to `{vault}/AI Papers/` with correct filename format
- [ ] Frontmatter is valid YAML with category from config, published date parsed from paper.Published string
- [ ] Frontmatter renders correctly in Obsidian
- [ ] Atomic write guarantees: no `.tmp` files left on disk under any failure scenario
- [ ] `processed.json` is updated immediately after every successful vault write
- [ ] `processed.json` is NOT updated if vault write fails
- [ ] Paper re-surfaces in discovery if pipeline fails before vault write completes
- [ ] `GET /result/:sessionId` returns content, vault file path, and token usage
- [ ] Markdown preview renders correctly in Next.js UI (sections, bold, links, tables)
- [ ] Token usage displayed in success state (input + output tokens)
- [ ] All pipeline events logged with `session_id`, `paper_id`, and `duration_ms`
- [ ] Soft word target (~2,500 words) met for a typical 10-page CS paper
