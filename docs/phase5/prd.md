# Phase 5 — ReviewerAgent & Revision Loop
## ArXiv AI Paper Explainer Agent

---

## Intent

A single-pass explainer is good. A reviewed and revised explainer is trustworthy.

Phase 4 delivered the product's core capability — generating a rich explanation of a research paper. But a single LLM pass has a fundamental weakness: the same model that generates the explanation also decides if it's good enough. There is no independent check.

Phase 5 introduces a **Critic-Generator loop**: a second agent — the `ReviewerAgent` — evaluates every explainer against a fixed quality rubric, identifies specific weaknesses section by section, and sends structured feedback back to the `ExplainerAgent` for revision. The loop runs until the reviewer approves the output or the configured iteration cap is reached.

**The core intent:** The practitioner should never receive an explainer that the system itself would not endorse. Every note that lands in Obsidian has been independently reviewed and either approved or given the maximum number of revision attempts before being saved with an honest flag.

---

# Part 1 — Product Requirements

## 1. Problem Statement

Single-pass LLM generation produces inconsistent quality. Some explanations are excellent; others miss the paper's core intent, use weak analogies, or leave key terms undefined. Without an independent review step, there is no systematic way to catch and correct these failures before the output is saved.

The practitioner's trust in the system depends on consistent quality. Phase 5 makes quality systematic — not dependent on a lucky first pass.

## 2. Target Users

**Primary:** Technical practitioners who rely on the system's output to understand research accurately. They trust that the saved note represents the system's best effort, not just its first attempt.

## 3. User Stories

- As a practitioner, I want the system to review its own output before saving it so that I receive an explainer the system itself considers high quality.
- As a practitioner, I want the reviewer to check specific things — not just general quality — so that weaknesses in analogies, glossary, or author intent are reliably caught.
- As a practitioner, I want to see in the saved note whether it passed review and how many iterations it took so that I can calibrate my trust in the output.
- As a practitioner, I want live progress updates during review and revision so that I know the system is actively improving the output.
- As a developer, I want to disable the review loop with a single config change so that I can trade quality for speed when needed.

## 4. Functional Requirements

### F1 — ReviewerAgent Evaluation
The ReviewerAgent evaluates every explainer against a fixed quality rubric with 6 criteria:

| Criterion | What is checked |
|---|---|
| **Author intent** | Is the core reason the authors wrote the paper clearly captured — not just what they built, but why? |
| **Analogy quality** | Are analogies accurate? Are they properly layered (everyday intuition → engineering bridge)? |
| **Math handling** | Is math translated or summarized appropriately? Is any math left unexplained? |
| **Glossary priority** | Are the 8–10 most important terms included? Are they ordered by importance, not alphabetically? |
| **Practitioner tone** | Does the explanation respect practitioner intelligence without over-simplifying or over-formalizing? |
| **Follow-up relevance** | Are follow-up papers relevant to the paper's contribution? Are arXiv links correct where provided? |

### F2 — Structured Verdict
The ReviewerAgent returns a structured verdict containing:
- `pass` (bool) — whether the explainer meets quality threshold
- `score` (float, 0.0–1.0) — overall quality score
- `feedback` (map) — section name → specific, actionable revision instruction
- `iteration` (int) — which review pass produced this verdict

Feedback must be specific and actionable. "Improve the analogies" is not acceptable. "The transformer attention analogy is too abstract — bridge it specifically to how a database index lookup works" is acceptable.

### F3 — Revision Loop
- If reviewer returns `pass: false` AND iterations < `max_review_iterations`:
  - ExplainerAgent receives the structured feedback as a revision note
  - ExplainerAgent rewrites the explainer incorporating the feedback
  - ReviewerAgent evaluates the revised output
  - Loop continues
- If reviewer returns `pass: true`:
  - Pipeline proceeds to vault write immediately
- If max iterations reached without `pass: true`:
  - Pipeline proceeds with the last ExplainerOutput
  - Vault note frontmatter records `review_passed: false` and iteration count
  - No error is surfaced to the user — this is expected behaviour for difficult papers

### F4 — Loop Configuration
- `max_review_iterations` in config controls the loop cap
- Default: `2` (one revision pass)
- Setting to `0` disables the ReviewerAgent entirely — pipeline goes directly to vault write after first generation (Phase 4 behaviour)
- Setting to `1` allows one review; if it fails, saves with `review_passed: false`

### F5 — Progress Updates (Phase 5 additions)
- New stage labels:
  - `"Reviewing (pass {n})..."` (`reviewing`)
  - `"Revising (pass {n})..."` (`revising`)
- Reviewer score shown in progress indicator during review
- Final iteration count shown in success state

### F6 — Vault Frontmatter (updated)
The saved note's frontmatter must reflect the review outcome:
```yaml
review_iterations: 2
review_passed: false   # if max iterations reached without approval
```

## 5. Non-Functional Requirements

- **Reviewer independence:** The ReviewerAgent must evaluate against the fixed rubric — not against the ExplainerAgent's own framing. The system prompt must not allow the reviewer to rationalize weak output.
- **Feedback specificity:** Vague reviewer feedback produces vague revisions. The system prompt must require section-level, actionable feedback.
- **Cost awareness:** Each review iteration adds one full LLM call. Default cap of 2 means a maximum of 4 LLM calls per paper (2 generations + 2 reviews). This must be documented.
- **Configurability:** The loop cap must be adjustable without code changes.

## 6. Success Metrics

- Reviewer `pass: true` is reached before max iterations for the majority of papers
- Revised explainer demonstrably improves on the first pass (reviewer score increases)
- `review_passed: false` in frontmatter correctly reflects cases where max iterations were reached
- Setting `max_review_iterations: 0` produces Phase 4 behaviour with no reviewer overhead
- Reviewer feedback is specific enough that the ExplainerAgent's revision addresses it

## 7. Scope & Non-Goals

**In scope:**
- ReviewerAgent with fixed quality rubric
- Critic-Generator revision loop in Orchestrator
- Configurable iteration cap (`max_review_iterations`)
- Progress UI updates for review and revision stages
- Reviewer score and iteration count in UI
- Updated vault frontmatter (`review_passed`, `review_iterations`)

**Out of scope:**
- Human feedback loop (user reviews and requests revision) — non-goal for this version
- Different LLM providers for reviewer vs explainer — accepted tradeoff
- Section-level regeneration (rewrite one section only) — future enhancement
- Reviewer score history across multiple runs — future enhancement

## 8. Open Questions

None for this phase. All requirements are fully defined.

---

# Part 2 — Architecture

## Intent

The revision loop must be inserted between `ExplainerAgent` and `VaultWriterTool` in `runPipeline` with minimal structural change. Both agents share the same `LLMClient` interface established in Phase 3 — no new provider logic is needed. The Orchestrator's loop logic is deliberate and bounded: it always terminates, always produces output, and always communicates its outcome honestly through frontmatter.

---

## 1. System Overview

```
[Phase 4 left off: ExplainerAgent.Generate() returned ExplainerOutput]
    │
    ▼
iteration = 1

┌─── CRITIC-GENERATOR LOOP ────────────────────────────────────────────┐
│                                                                        │
│  ExplainerAgent.Generate({ PDF, PaperMeta, RevisionNote })            │
│      │                                                                 │
│      ▼                                                                 │
│  ExplainerOutput { Content, Sections, Iteration }                     │
│      │                                                                 │
│      ▼                                                                 │
│  ReviewerAgent.Review(ExplainerOutput, iteration)                     │
│      │                                                                 │
│      ▼                                                                 │
│  ReviewVerdict { Pass, Score, Feedback, Iteration }                   │
│      │                                                                 │
│      ├── Pass == true → EXIT LOOP                                     │
│      ├── iteration >= max_review_iterations → EXIT LOOP               │
│      └── else → format feedback as RevisionNote                       │
│                 iteration++                                            │
│                 loop back to ExplainerAgent.Generate()                │
│                                                                        │
└────────────────────────────────────────────────────────────────────────┘
    │
    ▼
VaultWriterTool.WriteToVault(FinalExplainerOutput, Paper, LastVerdict)
    │
    ▼
Obsidian note saved with review_passed, review_iterations in frontmatter
```

---

## 2. Component Breakdown

### 2.1 ReviewerAgent

**Intent:** The ReviewerAgent is a strict, independent evaluator. Its job is to find specific weaknesses in the explainer — not to confirm it is good enough. The system prompt must be adversarial by design: it should look for problems, not validate.

**Why a separate agent (not a self-review prompt):** Asking the ExplainerAgent to review its own output introduces a fundamental bias — the same model that made reasoning choices will tend to rationalize them. A separate agent with a different system prompt, focused entirely on critique, provides a meaningfully different perspective even when using the same underlying model.

**Interface:**
```go
// /internal/agents/reviewer.go

type ReviewerAgent struct {
    llmClient llm.LLMClient
    config    *config.Config
}

func (a *ReviewerAgent) Review(
    ctx context.Context,
    explainer models.ExplainerOutput,
    paper models.Paper,
    iteration int,
) (models.ReviewVerdict, error)
```

**System prompt (full specification):**
```
You are a strict quality reviewer for AI research explainers. Your job is to find
problems, not to confirm quality. Approach every explainer with the assumption that
it can be improved.

Your audience is the same as the explainer's: technical practitioners — ML engineers
and developers who understand ML basics but don't follow academic research closely.

EVALUATE AGAINST THESE 6 CRITERIA:

1. AUTHOR INTENT
   Does the explainer make clear WHY the authors wrote this paper — not just what
   they built, but what problem motivated the work? A practitioner who reads only
   the Problem Statement and Core Idea sections should understand the authors' goal.
   Failure: explains what the paper does without explaining why it was worth doing.

2. ANALOGY QUALITY
   Are the analogies in "Core Idea" and "Analogies & Intuition" sections:
   - Accurate? (does the analogy correctly represent the concept?)
   - Layered? (everyday intuition first, then engineering bridge?)
   - Specific? (a named engineering concept, not "it's like a pipeline")
   Failure: vague analogies ("it's similar to how computers process data"),
   inaccurate analogies, or analogies that don't bridge to engineering.

3. MATH HANDLING
   Is every equation or formal notation either:
   - Translated to plain English (for simple equations), or
   - Summarized at intent level (for complex proofs)?
   Is any math left unexplained?
   Failure: notation reproduced without explanation, or "the proof shows X" without
   explaining what X means in practice.

4. GLOSSARY PRIORITY
   Does the glossary contain exactly 8–10 terms? Are they:
   - The most important terms for understanding the paper's CONTRIBUTION
     (not just the most complex terms)?
   - Ordered by importance (most important first)?
   Failure: important terms missing, alphabetical ordering, or trivial terms included
   at the expense of central ones.

5. PRACTITIONER TONE
   Does the explainer:
   - Respect the reader's intelligence (no unnecessary hand-holding)?
   - Avoid academic formalism (no "the authors posit that...")?
   - Stay concrete (claims backed by specific findings, not vague praise)?
   Failure: condescending simplification, academic hedging, or unsupported claims.

6. FOLLOW-UP RELEVANCE
   Are follow-up papers directly relevant to understanding or extending this paper's
   contribution? Are any arXiv links present and correctly formatted?
   Failure: loosely related suggestions, broken link format, or no suggestions at all.

OUTPUT FORMAT:
Respond ONLY with a JSON object. No preamble, no explanation outside the JSON.

{
  "pass": true | false,
  "score": 0.0–1.0,
  "feedback": {
    "problem_statement": "specific issue or null if no issue",
    "core_idea": "specific issue or null if no issue",
    "methodology": "specific issue or null if no issue",
    "key_findings": "specific issue or null if no issue",
    "limitations": "specific issue or null if no issue",
    "why_it_matters": "specific issue or null if no issue",
    "analogies": "specific issue or null if no issue",
    "glossary": "specific issue or null if no issue",
    "follow_up_papers": "specific issue or null if no issue"
  }
}

PASS THRESHOLD: score >= 0.80 AND no critical failures in criteria 1 or 2.
A score of 0.79 with strong author intent and analogies should still pass.
A score of 0.85 with weak author intent should NOT pass.

FEEDBACK RULES:
- Only include non-null feedback for sections that have specific, fixable problems.
- Feedback must be actionable: tell the ExplainerAgent exactly what to change.
  BAD:  "The analogy is weak."
  GOOD: "The attention mechanism analogy compares it to 'reading carefully' —
         this is too vague. Bridge it to a database index lookup: the query
         is like a search key, keys are the index, values are the retrieved records."
```

**Response parsing:**
```go
func (a *ReviewerAgent) Review(...) (models.ReviewVerdict, error) {
    req := llm.CompletionRequest{
        SystemPrompt: reviewerSystemPrompt,
        UserPrompt:   a.buildReviewPrompt(explainer, paper, iteration),
        MaxTokens:    2000,   // reviewer output is structured JSON, not long prose
        Temperature:  0.1,    // very low — we want consistent, deterministic evaluation
    }

    resp, err := a.llmClient.Complete(ctx, req)
    // handle error

    // Strip any markdown fences the LLM might add
    jsonStr := strings.TrimSpace(resp.Content)
    jsonStr = strings.TrimPrefix(jsonStr, "```json")
    jsonStr = strings.TrimSuffix(jsonStr, "```")

    var raw struct {
        Pass     bool               `json:"pass"`
        Score    float32            `json:"score"`
        Feedback map[string]*string `json:"feedback"`
    }
    if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
        return models.ReviewVerdict{}, fmt.Errorf("failed to parse reviewer response: %w", err)
    }

    // Filter null feedback entries
    feedback := make(map[string]string)
    for section, note := range raw.Feedback {
        if note != nil && *note != "" {
            feedback[section] = *note
        }
    }

    return models.ReviewVerdict{
        PaperID:   paper.ID,
        Pass:      raw.Pass,
        Score:     raw.Score,
        Feedback:  feedback,
        Iteration: iteration,
        CreatedAt: time.Now(),
    }, nil
}
```

**Review prompt construction:**
```go
func (a *ReviewerAgent) buildReviewPrompt(
    explainer models.ExplainerOutput,
    paper models.Paper,
    iteration int,
) string {
    return fmt.Sprintf(
        "Review iteration: %d\n\nPaper: %s (arXiv: %s)\n\n" +
        "Explainer to review:\n\n%s",
        iteration,
        paper.Title,
        paper.ID,
        explainer.Content,
    )
}
```

**Why temperature 0.1 for reviewer:** The reviewer must apply the rubric consistently. High temperature introduces variability in evaluation that undermines the review loop's purpose — a paper that passes on one run might fail on another. Low temperature makes the reviewer a reliable, repeatable quality gate.

**Dependencies:** `llm.LLMClient`, `models`, `encoding/json`, `config`

---

### 2.2 Revision Note Formatter

**Intent:** Raw reviewer feedback is a map of section names to issue descriptions. Before passing it to the ExplainerAgent, it must be formatted into a clear, structured revision instruction that the ExplainerAgent can act on directly.

```go
// /internal/agents/explainer.go (added in Phase 5)

func formatRevisionNote(verdict models.ReviewVerdict) string {
    var sb strings.Builder
    sb.WriteString(fmt.Sprintf(
        "REVISION REQUIRED (Review pass %d, score: %.2f)\n\n",
        verdict.Iteration, verdict.Score,
    ))
    sb.WriteString("Please revise the following sections based on this feedback:\n\n")

    for section, feedback := range verdict.Feedback {
        sb.WriteString(fmt.Sprintf("### %s\n%s\n\n", sectionDisplayName(section), feedback))
    }

    sb.WriteString("---\n\nFor sections without feedback above, keep the existing content unchanged.")
    return sb.String()
}
```

**Why format as structured text, not JSON:** The ExplainerAgent receives this as part of its user prompt. Natural language instructions produce more reliable revisions than machine-readable structured data in a prose generation context.

---

### 2.3 Orchestrator — Revision Loop

**Intent:** The loop is the Orchestrator's core logic addition in Phase 5. It must be bounded, deterministic, and always produce output — even when the reviewer never approves. The loop is inserted between ExplainerAgent and VaultWriterTool in `runPipeline`.

```go
func (o *Orchestrator) runPipeline(ctx context.Context, session *models.PipelineSession) {
    // PDF fetch (Phase 3)
    pdf, err := o.pdfFetchTool.FetchPDF(ctx, session.SelectedPaper.PDFURL)
    if err != nil { o.failSession(session, err.Error(), true); return }
    session.PDF = pdf

    var lastExplainer models.ExplainerOutput
    var lastVerdict *models.ReviewVerdict
    revisionNote := ""

    maxIterations := o.config.Agent.MaxReviewIterations

    for iteration := 1; ; iteration++ {
        // Generate (or revise) explainer
        session.Stage = models.StageGenerating
        if iteration > 1 {
            session.Stage = models.StageRevising
        }
        session.Iterations = iteration
        o.setSession(session)

        explainer, err := o.explainerAgent.Generate(ctx, agents.ExplainerInput{
            PDF:          pdf,
            PaperMeta:    *session.SelectedPaper,
            RevisionNote: revisionNote,
        })
        if err != nil { o.failSession(session, err.Error(), true); return }
        lastExplainer = explainer
        session.TokensUsed += explainer.TokensUsed

        // Skip review if disabled
        if maxIterations == 0 {
            break
        }

        // Review
        session.Stage = models.StageReviewing
        o.setSession(session)

        verdict, err := o.reviewerAgent.Review(ctx, explainer, *session.SelectedPaper, iteration)
        if err != nil { o.failSession(session, err.Error(), true); return }
        lastVerdict = &verdict
        session.LastVerdict = lastVerdict
        session.TokensUsed += verdict.TokensUsed

        slog.Info("review complete",
            "session_id", session.SessionID,
            "iteration", iteration,
            "score", verdict.Score,
            "pass", verdict.Pass,
        )

        // Exit conditions
        if verdict.Pass {
            slog.Info("reviewer approved explainer", "iteration", iteration)
            break
        }
        if iteration >= maxIterations {
            slog.Warn("max review iterations reached without approval",
                "session_id", session.SessionID,
                "final_score", verdict.Score,
            )
            break
        }

        // Prepare revision for next iteration
        revisionNote = formatRevisionNote(verdict)
    }

    // Write to vault
    session.Stage = models.StageWriting
    o.setSession(session)

    vaultPath, err := o.vaultWriterTool.WriteToVault(
        ctx, lastExplainer, *session.SelectedPaper, lastVerdict,
    )
    if err != nil { o.failSession(session, err.Error(), false); return }

    session.Stage = models.StageComplete
    session.VaultFile = vaultPath
    o.setSession(session)
}
```

**Why `for iteration := 1; ; iteration++` (infinite loop with explicit break):** This pattern makes the exit conditions explicit and readable. Both exit conditions (approval and cap) are clear `break` statements. An alternative `for iteration <= max` loop would require special-casing `max == 0`. The infinite loop with breaks is cleaner for this multi-exit logic.

---

### 2.4 Next.js — Review/Revision Progress UI

**Intent:** The progress UI must communicate that the system is actively working to improve the output — not just running the same thing twice. Showing the reviewer's score builds trust: the user can see the quality improving across iterations.

**Updated `<ProgressIndicator />`:**
```typescript
// Stage labels with iteration and score context
function getProgressLabel(status: PipelineStatus): string {
  switch (status.stage) {
    case 'generating':
      return status.iteration === 1
        ? 'Generating explainer...'
        : `Revising explainer (pass ${status.iteration})...`
    case 'reviewing':
      return `Reviewing (pass ${status.iteration})...`
    case 'revising':
      return `Revising (pass ${status.iteration})...`
    case 'writing':
      return 'Saving to vault...'
    case 'complete':
      return 'Complete'
    default:
      return '...'
  }
}
```

**Updated status response (Phase 5 additions):**
```typescript
interface PipelineStatus {
  stage: PipelineStage
  iteration: number
  reviewScore?: number    // shown during/after reviewing stage
  reviewPassed?: boolean  // shown in success state
  error?: string
  recoverable?: boolean
}
```

**Updated `GET /status/:sessionId` response:**
```go
json.NewEncoder(w).Encode(StatusResponse{
    Stage:       session.Stage,
    Iteration:   session.Iterations,
    ReviewScore: lastVerdictScore(session),  // 0 if no verdict yet
    ReviewPassed: lastVerdictPass(session),
    Error:       session.Error,
    Recoverable: session.Recoverable,
})
```

**Updated success state:**
```
✓ Note saved to vault
  Path: /Users/you/obsidian/AI Papers/2026-06-07_2401.12345_paper-title.md
  Tokens used: 18,420
  Review: Passed on iteration 2 (score: 0.87)   ← new in Phase 5
```

---

## 3. Data Model

**Updated `PipelineSession`:**
```go
type PipelineSession struct {
    // ... existing fields ...
    LastVerdict  *models.ReviewVerdict  // most recent reviewer output
    TokensUsed   int                    // accumulated across all LLM calls (explainer + reviewer)
}
```

**`ReviewVerdict` (defined in Phase 1, first used in Phase 5):**
```go
type ReviewVerdict struct {
    PaperID    string
    Pass       bool
    Score      float32
    Feedback   map[string]string  // section → actionable revision note
    Iteration  int
    TokensUsed int
    CreatedAt  time.Time
}
```

**Updated Obsidian frontmatter:**
```yaml
---
arxiv_id: "2401.12345"
title: "Paper Title"
authors: ["Author One"]
published: "2026-06-07"
category: "cs.AI"
generated_at: "2026-06-07T10:30:00Z"
review_iterations: 2
review_passed: true      # false if max iterations reached without approval
review_score: 0.87       # final reviewer score
tags: [ai, paper, explainer]
---
```

---

## 4. Data Flow

### Full Revision Loop Flow

```
[ExplainerAgent.Generate() returned first ExplainerOutput — iteration 1]
    │
    ▼
ReviewerAgent.Review(explainer, paper, iteration=1)
    ├── Build review prompt: system rubric + explainer content
    ├── LLMClient.Complete({ temperature: 0.1, max_tokens: 2000 })
    ├── Parse JSON verdict
    └── Return ReviewVerdict {
            pass: false,
            score: 0.68,
            feedback: {
                "core_idea": "Analogy compares attention to 'reading carefully' —
                              bridge to database index lookup specifically",
                "glossary":  "Term 'contrastive loss' missing — central to method"
            }
        }
    │
    ▼
verdict.Pass == false AND iteration(1) < maxIterations(2)
    │
    ▼
formatRevisionNote(verdict) →
    "REVISION REQUIRED (Review pass 1, score: 0.68)
     ### Core Idea
     Analogy compares attention to 'reading carefully' — bridge to database index lookup specifically
     ### Glossary
     Term 'contrastive loss' missing — central to method"
    │
    ▼
iteration = 2
ExplainerAgent.Generate({ PDF, PaperMeta, RevisionNote: "REVISION REQUIRED..." })
    ├── Builds user prompt prepended with revision instructions
    └── Returns revised ExplainerOutput { Iteration: 2 }
    │
    ▼
ReviewerAgent.Review(revisedExplainer, paper, iteration=2)
    └── Returns ReviewVerdict { pass: true, score: 0.87, feedback: {} }
    │
    ▼
verdict.Pass == true → EXIT LOOP
    │
    ▼
VaultWriterTool.WriteToVault(revisedExplainer, paper, verdict)
    └── Frontmatter: review_iterations: 2, review_passed: true, review_score: 0.87
```

### Max Iterations Reached Flow

```
iteration = 2, verdict.Pass == false, iteration >= maxIterations(2)
    │
    ▼
slog.Warn("max review iterations reached without approval", score: 0.74)
    │
    ▼
EXIT LOOP with lastExplainer = iteration-2 output, lastVerdict.Pass = false
    │
    ▼
VaultWriterTool.WriteToVault(lastExplainer, paper, lastVerdict)
    └── Frontmatter: review_iterations: 2, review_passed: false, review_score: 0.74
    │
    ▼
Pipeline completes normally — no error surfaced to user
Note saved with honest review_passed: false flag
```

### Loop Disabled Flow (`max_review_iterations: 0`)

```
iteration = 1
ExplainerAgent.Generate() → ExplainerOutput
    │
    ▼
maxIterations == 0 → skip review, EXIT LOOP immediately
    │
    ▼
VaultWriterTool.WriteToVault(explainer, paper, verdict=nil)
    └── Frontmatter: review_iterations: 0, review_passed: true (default)
```

---

## 5. Tech Stack

No new technologies introduced in Phase 5. ReviewerAgent uses the same `LLMClient` interface established in Phase 3. All loop logic is pure Go.

**Key design note — reviewer uses same provider as explainer:** Both agents call `LLMClient.Complete()`. The same provider and model handles both generation and review. This is an accepted tradeoff (shared blind spots) in exchange for single-config simplicity. See Risks & Tradeoffs.

---

## 6. Integration Points

### LLM Provider (ReviewerAgent calls)

**Request characteristics (Phase 5 — reviewer):**
- System prompt: ~600 tokens (rubric + JSON format instructions)
- User prompt: ~3,000–5,000 tokens (explainer content)
- No PDF document (reviewer evaluates the explainer text, not the source paper)
- Max output tokens: 2,000 (structured JSON verdict)
- Temperature: 0.1 (consistent, deterministic evaluation)

**Cost implication:** Each review iteration adds approximately 5,000–7,000 input tokens + 500 output tokens. With default `max_review_iterations: 2`, a full run involves:
- 2 × ExplainerAgent calls: ~90,000 tokens each (PDF-heavy)
- 2 × ReviewerAgent calls: ~7,000 tokens each
- **Total per paper: ~200,000 tokens** (varies significantly by paper length and provider)

This cost must be documented clearly in the README.

---

## 7. Cross-Cutting Concerns

### Error Handling

| Failure | Behaviour | Recoverable |
|---|---|---|
| ReviewerAgent LLM failure | Surface error, session → failed | Yes — retry run |
| Reviewer JSON parse failure | Log warning, treat as `pass: false` with empty feedback, continue loop | N/A — degraded gracefully |
| Loop produces no improvement | Max iterations reached, save with `review_passed: false` | N/A — expected behaviour |
| Reviewer score parse error | Default to 0.0, continue | N/A — degraded gracefully |

**JSON parse failure handling:** If the reviewer returns malformed JSON (e.g. the LLM adds a preamble), the system logs a warning and treats it as a failed review with no specific feedback. The ExplainerAgent will retry on the next iteration without targeted guidance — still better than crashing.

### Observability

```json
{"level":"INFO","msg":"review started","session_id":"abc123","iteration":1,"paper_id":"2401.12345"}
{"level":"INFO","msg":"review complete","session_id":"abc123","iteration":1,"score":0.68,"pass":false,"duration_ms":8200}
{"level":"INFO","msg":"revision started","session_id":"abc123","iteration":2,"feedback_sections":["core_idea","glossary"]}
{"level":"INFO","msg":"review complete","session_id":"abc123","iteration":2,"score":0.87,"pass":true,"duration_ms":7900}
{"level":"INFO","msg":"reviewer approved","session_id":"abc123","final_iteration":2,"final_score":0.87}

{"level":"WARN","msg":"max iterations reached without approval","session_id":"abc123","final_score":0.74}
{"level":"WARN","msg":"reviewer json parse failed","session_id":"abc123","iteration":1,"raw_response":"...","treating_as":"fail"}
```

### Security
- Reviewer prompt includes the explainer content — no new external data introduced
- Reviewer response is parsed as JSON with strict schema — no arbitrary code execution risk
- No new filesystem or network operations introduced in Phase 5

---

## 8. Risks & Tradeoffs

| ID | Risk/Tradeoff | Severity | Mitigation |
|---|---|---|---|
| R1 | Same LLM for reviewer and explainer — shared blind spots | Medium | Accepted tradeoff. Both agents have different system prompts and temperatures. Reviewer is adversarial by design. Future: support different provider per agent via config. |
| R2 | Reviewer feedback quality depends on LLM capability | Medium | System prompt requires specific, actionable feedback format. JSON structure enforces section-level granularity. Low temperature (0.1) reduces variability. |
| R3 | Reviewer approves weak output (false positive) | Low | Rubric is specific and pass threshold requires both score ≥ 0.80 AND strong author intent. False positives are visible to the user via review_score in frontmatter. |
| R4 | Revision makes output worse (regression) | Low | Uncommon in practice — targeted section feedback rarely degrades unrelated sections. Reviewer catches regression in next pass. Max iterations is a safety valve. |
| R5 | Token cost is significantly higher than Phase 4 | Medium | Documented in README. `max_review_iterations: 0` disables at zero cost. Default of 2 is a reasonable quality/cost balance. |
| T1 | Same provider for both agents | Accepted | Single config, simpler operation. Real-world impact is limited because system prompts and temperatures create meaningfully different evaluation behaviour. |
| T2 | No human feedback loop | Accepted | Explicitly out of scope. The automated reviewer is the quality gate for this version. |
| T3 | Reviewer evaluates explainer text only — not source PDF | Accepted | Including the PDF in reviewer calls would double input token cost. The explainer text contains enough information for rubric evaluation. |

---

## Exit Criteria

All of the following must be true before Phase 6 begins:

- [ ] ReviewerAgent produces a valid JSON verdict for any explainer input
- [ ] Verdict includes `pass`, `score`, and section-level `feedback` for failing sections
- [ ] Feedback is specific and actionable (not generic) — verified manually on 2–3 runs
- [ ] Revision loop runs for at most `max_review_iterations` iterations
- [ ] Setting `max_review_iterations: 0` bypasses reviewer and saves immediately
- [ ] Setting `max_review_iterations: 1` runs review once; saves with `review_passed: false` if not approved
- [ ] Reviewer score improves between iteration 1 and iteration 2 for at least 3 test papers
- [ ] `review_passed: false` correctly appears in frontmatter when max iterations reached without approval
- [ ] `review_score` correctly reflects the final reviewer score in frontmatter
- [ ] Total token usage accumulated correctly across all explainer and reviewer calls
- [ ] Progress UI shows `"Reviewing (pass N)..."` and `"Revising (pass N)..."` with correct iteration numbers
- [ ] Reviewer score shown in UI progress during review stage
- [ ] All review events logged: `session_id`, `iteration`, `score`, `pass`, `duration_ms`
- [ ] Malformed reviewer JSON handled gracefully — logged as warning, loop continues
