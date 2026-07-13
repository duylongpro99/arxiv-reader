# Architecture Document
## ArXiv AI Paper Explainer Agent

---

## 1. System Overview

### High-Level Description

The system is a **local, trigger-based, two-service application**:

- A **Next.js frontend** handles all user interaction — triggering runs, displaying paper candidates, selection, progress, and the final explainer preview.
- A **Go backend** built on ADK Go handles all agent logic — paper discovery, duplicate checking, HTML content extraction, explainer generation, critic-review loop, and Obsidian vault writing.

The two services communicate via HTTP. Both run locally on the user's machine.

### Pipeline Flow

```
User Trigger
    │
    ▼
[DiscoveryTool] ──→ Fetch top 5 new cs.AI papers from arXiv
    │
    ▼
[LogCheckTool] ──→ Filter already-processed paper IDs
    │
    ▼
[Next.js UI] ◀──→ Present candidates → User selects one paper
    │
    ▼
[PaperContentTool] ──→ Fetch arXiv HTML, convert to Markdown
    │         │
    │    [404 Not Found] ──→ Recoverable: return to selection, re-pick
    │
    ▼
[ExplainerAgent] ──→ Read Markdown text + Generate rich explainer
    │
    ▼
[ReviewerAgent] ──→ Critique explainer against quality rubric
    │         │
    │    [Pass] ──→ Proceed to save
    │         │
    │    [Fail] ──→ Send structured feedback back to ExplainerAgent
    │                    │
    └────────────────────┘ (loop until Pass OR max iterations reached)
    │
    ▼
[VaultWriterTool] ──→ Save .md to Obsidian vault + update log file
    │
    ▼
[Next.js UI] ──→ Display success + preview of generated note
```

### Service Map

```
┌─────────────────────────────┐         ┌──────────────────────────────────┐
│      Next.js App            │         │        Go ADK Backend            │
│  (localhost:3000)           │◀──────▶│       (localhost:8080)           │
│                             │  HTTP   │                                  │
│  - Trigger UI               │         │  - ADK Agent Orchestrator        │
│  - Paper selection UI       │         │  - DiscoveryTool                 │
│  - Progress display         │         │  - LogCheckTool                  │
│  - Explainer preview        │         │  - PaperContentTool (HTML→MD)    │
│  - Run timeline UI          │         │  - ExplainerAgent                │
│  - Runs history list        │         │  - ReviewerAgent                 │
│                             │         │  - VaultWriterTool               │
│                             │         │  - LLM Client (text-only)        │
│                             │         │  - RunRecorder (Phase 7)         │
│                             │         │  - SSE Broker (Phase 7)          │
└─────────────────────────────┘         └──────────────────────────────────┘
                                                        │
                                    ┌───────────────────┼───────────────────┬────────────┐
                                    │                   │                   │            │
                             ┌──────▼─────┐    ┌───────▼──────┐   ┌───────▼──────┐  ┌──▼──────────┐
                             │ arXiv API  │    │  LLM Provider │   │Obsidian Vault│  │ PostgreSQL  │
                             │ (external) │    │  (configured) │   │ (local disk) │  │ (optional)  │
                             └────────────┘    └──────────────┘   └──────────────┘  └─────────────┘
```

---

## 2. Component Breakdown

### 1. Next.js Frontend

**Purpose:** User interaction layer — trigger, select, monitor, preview.

**Responsibilities:**
- Trigger the discovery pipeline via API call to Go backend
- Display 5 candidate papers for user selection
- Show real-time progress during generation (polling)
- Preview the final generated Markdown explainer
- Display errors clearly when pipeline steps fail

**Interface:**
```
GET  /api/trigger        → calls Go backend to start discovery
POST /api/select         → sends selected paper ID to Go backend
GET  /api/progress       → polls pipeline status
GET  /api/preview        → fetches final generated content
```

**Dependencies:** Go ADK Backend (HTTP)

---

### 2. Go ADK Backend — Orchestrator

**Purpose:** Central ADK agent runner. Owns the pipeline lifecycle, coordinates tools and sub-agents, manages session state.

**Responsibilities:**
- Expose HTTP endpoints consumed by Next.js
- Initialize and run ADK agent sessions
- Coordinate tool execution sequence
- Manage the ExplainerAgent → ReviewerAgent loop
- Enforce max iteration cap from config
- Return structured responses and errors to frontend

**Interface:**
```
POST /discover                    → triggers DiscoveryTool + LogCheckTool (start=0)
POST /discover/:sessionId/more     → appends more candidates to existing session (offset pagination)
POST /process                     → triggers full pipeline for selected paper ID
GET  /status/:sessionId           → returns current pipeline stage + progress
GET  /result/:sessionId           → returns final explainer content
GET  /runs/:id/content            → returns persisted Obsidian .md note (Phase 8)
GET  /health                      → sanity check endpoint
```

**Dependencies:** All tools and sub-agents, LLM Client, Config

---

### 3. DiscoveryTool (ADK Tool)

**Purpose:** Fetch the latest papers from arXiv `cs.AI` category.

**Responsibilities:**
- Query arXiv API for most recent papers in `cs.AI`
- Extract paper ID, title, authors, abstract, PDF URL
- Return top N papers (N from config, default 5)

**Interface:**
```go
type Paper struct {
    ID        string
    Title     string
    Authors   []string
    Abstract  string
    PDFURL    string
    Category  string
    Published time.Time
}

func FetchPapers(ctx context.Context, limit int) ([]Paper, error)         // start=0, implicit
func FetchPapersFrom(ctx context.Context, start int, limit int) ([]Paper, error) // Phase 8: offset pagination
```

**Dependencies:** arXiv API (external HTTP)

---

### 4. LogCheckTool (ADK Tool)

**Responsibilities:**
- Read processed paper IDs from local JSON log file
- Cross-reference fetched papers against log
- Return only unprocessed papers
- Append newly processed paper ID to log after successful vault write

**Interface:**
```go
func FilterUnprocessed(papers []Paper) ([]Paper, error)
func MarkAsProcessed(paperID string) error
```

**Dependencies:** Local filesystem (log file path from config)

---

### 5. PaperContentTool

**Purpose:** Fetch paper content from arXiv's LaTeXML HTML rendering and convert it to clean Markdown text.

**Responsibilities:**
- Query `https://arxiv.org/html/{arxiv_id}` (follows same-host redirect to versioned URL automatically)
- Fetch HTML under a 50MB size limit (`io.LimitReader`)
- Convert LaTeXML HTML to Markdown using pure-Go `html-to-markdown/v2` (no external dependencies, no CGO)
- Extract the main `ltx_document` body
- Strip math formulas, navigation elements, bibliography, and appendix sections (keep headings and figure captions)
- Return clean Markdown text

**Why Markdown over PDF-as-image:** Pure text extraction avoids the complexity and token cost of vision APIs while preserving the essential content — paper structure, reasoning, and key figures/captions. The Markdown approach is compatible with any text-capable LLM model, maximizing provider flexibility.

**Error Handling:**
- 404 Not Found → `ErrPaperHTMLNotFound` — treated as recoverable re-pick (return to selection)
- Transient failures (429, 5xx, network) → retry with exponential backoff (max 3 retries, same schedule as DiscoveryTool)
- Permanent failures (other 4xx, parse errors) → surface as non-recoverable error

**Interface:**
```go
func (t *PaperContentTool) FetchMarkdown(ctx context.Context, arxivID string) (string, error)
// Returns clean Markdown text ready for ExplainerAgent
```

**Dependencies:** `html-to-markdown/v2` (pure Go), `net/http`, `io`

---

### 6. ExplainerAgent (ADK LlmAgent)

**Purpose:** Core intelligence — read the paper's Markdown content and generate the rich, well-structured explainer.

**Responsibilities:**
- Receive paper Markdown text + metadata + structured prompt
- Reason about the paper's core intent and significance
- Generate all explainer sections per PRD spec
- Handle mathematical concepts and figures contextually
- Accept structured revision feedback from ReviewerAgent and rewrite accordingly

**Interface:**
```go
type ExplainerInput struct {
    MarkdownText string           // extracted paper Markdown from PaperContentTool
    PaperMeta    Paper
    RevisionNote string           // empty on first pass
}

type ExplainerOutput struct {
    PaperID      string            // paper's arXiv ID
    Content      string            // full Markdown body
    Sections     map[string]string // keyed by section name
    Iteration    int
    InputTokens  int               // tokens consumed by LLM
    OutputTokens int               // tokens produced by LLM
    CreatedAt    time.Time
}

func Generate(ctx context.Context, input ExplainerInput) (ExplainerOutput, error)
```

**Dependencies:** LLM Client (configurable)

---

### 7. ReviewerAgent (ADK LlmAgent)

**Purpose:** Independent critic — evaluates explainer quality and drives the revision loop.

**Responsibilities:**
- Evaluate **explainer text only** (paper Markdown NOT sent — cost optimization per design T3)
- Score against a fixed 6-criteria rubric:
  - Is the core author intent clearly captured?
  - Are analogies accurate and layered (intuition → engineering)?
  - Is math handled appropriately?
  - Are diagrams and figures described and explained correctly?
  - Is the glossary prioritized correctly?
  - Is the tone right for technical practitioners?
- Return structured verdict: `Pass` is the single source of truth (verbatim from model); `Score` is advisory only
- Generate section-level feedback for failed reviews (fed back to ExplainerAgent for revision)
- Reuse the same `llm.LLMClient` as the ExplainerAgent (distinct system prompt + low temperature provide meaningfully different evaluation behaviour)

**Design Decision 1 (Policy):** `Pass` gates the loop, never `Score`. Score is advisory for observability, never blocks progress.

**Design Decision 2 (Fault Handling):** Malformed reviewer JSON (not valid JSON after fence stripping) stops the loop immediately and saves the current explainer flagged as `review_passed: false` — no blind regeneration without guidance.

**Interface:**
```go
type ReviewVerdict struct {
    PaperID    string
    Pass       bool              // single source of truth; verbatim from model
    Score      float32           // 0.0 - 1.0, advisory only (never gates)
    Feedback   map[string]string // section slug → actionable revision note
    Iteration  int               // which review round this was (1st, 2nd, etc.)
    TokensUsed int               // tokens for this review call
    CreatedAt  time.Time
}

func (a *ReviewerAgent) Review(ctx context.Context, ex ExplainerOutput, paper Paper, iteration int) (ReviewVerdict, error)
```

**Error Handling:**
- JSON parse failure → returns `ErrReviewParse` sentinel (distinguishable from real LLM errors)
- LLM/network error → returns error unchanged (fails session recoverably, no write)
- Orchestrator respects both distinctions: parse error halts loop with `pass: false`, other errors fail the run

**Dependencies:** LLM Client (configurable), Config (max iterations)

---

### 8. VaultWriterTool (ADK Tool)

**Purpose:** Persist the explainer to the Obsidian vault atomically with review metadata.

**Responsibilities:**
- Format final Markdown with consistent YAML frontmatter (including Phase 5 review verdict)
- Generate filename: `YYYY-MM-DD_arxivID_slug-title.md` with date parsed from Paper.Published string
- Write file atomically to configured Obsidian vault subfolder (`.tmp` → `rename`)
- Trigger `LogCheckTool.MarkAsProcessed()` after successful write (failure is warning, not fatal)

**Interface:**
```go
type VaultWriterTool struct {
    cfg      *config.Config
    logCheck *LogCheckTool
}

func NewVaultWriterTool(cfg *config.Config, logCheck *LogCheckTool) *VaultWriterTool

func (t *VaultWriterTool) WriteToVault(ctx context.Context, ex ExplainerOutput, p Paper, verdict *ReviewVerdict) (string, error)
// returns the final absolute path written
// verdict is nil if reviewer disabled (maxReviewIterations=0), otherwise contains Phase 5 review result
```

**Frontmatter fields:**
- `arxiv_id`: from Paper.ID
- `title`, `authors` (YAML list): from Paper metadata
- `published`: date part of Paper.Published string (parsed RFC3339 or first 10 chars)
- `category`: from `config.Agent.ArxivCategory` (NOT from Paper, which has no Category field)
- `generated_at`: RFC3339 UTC timestamp
- **Phase 5 review fields** (per verdict):
  - If `verdict == nil` (reviewer disabled): `review_iterations: 0`, `review_passed: true`, no `review_score`
  - If `verdict != nil`: `review_iterations: {verdict.Iteration}`, `review_passed: {verdict.Pass}`, `review_score: {verdict.Score}`
- `tags`: `[ai, paper, explainer]`

**Dependencies:** Local filesystem (vault path from config), LogCheckTool, config

---

### 9. LLM Client (configurable, text-only)

**Purpose:** Provider-agnostic LLM interface. Decouples all agent logic from any specific LLM API. Text-only design (no vision) maximizes model and provider flexibility.

**Responsibilities:**
- Expose a unified `Complete(req)` interface
- Accept paper Markdown as text input (DocumentText field)
- Load provider, model, API key, and parameters from config at startup
- Concrete implementations for: Anthropic Claude, OpenAI, Google Gemini (others addable by implementing the interface)
- Implement shared retry logic: 429 (3 retries), 503 (1 retry), 400 (immediate fail)
- Return separate input/output token counts

**Interface:**
```go
type LLMClient interface {
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

type CompletionRequest struct {
    SystemPrompt string
    UserPrompt   string
    DocumentText string    // paper Markdown from PaperContentTool
    MaxTokens    int
    Temperature  float32
}

type CompletionResponse struct {
    Content      string
    InputTokens  int
    OutputTokens int
}
```

**Shared Retry Logic (`withRetry` wrapper):**
- 429 (rate limit): exponential backoff, max 3 retries
- 503 (unavailable): exponential backoff, max 1 retry
- 400 (bad request): fail immediately (configuration error)
- Timeout: fail immediately (not retried)

**Dependencies:** Config, respective provider SDKs, `internal/llm/retry.go`

---

### 10. Run Timeline Tracing (Phase 7)

**Purpose:** Capture, stream, and persist a complete ordered timeline of all pipeline events per run. Enables live monitoring via SSE and historical playback.

**Responsibilities:**
- **Recorder**: Per-run monotonic event sequence with bounded in-memory ring buffer
- **Event Broker**: Per-run non-blocking fan-out to multiple SSE subscribers
- **Secret Scrubber**: Redacts API keys, key-shaped patterns, caps previews (no raw HTML/full markdown)
- **Event Taxonomy**: Standardized event kinds across discovery, tool calls, LLM decisions, and completion
- **Persistence**: Async serialization to PostgreSQL with degrade-safe store access
- **Transport**: Server-Sent Events (SSE) with `Last-Event-ID` replay; REST endpoints for history and cross-restart reload

**Event Taxonomy** (event_type field):
```
discovery.started, tool.discovery.completed, tool.logcheck.completed,
selection.presented, selection.chosen,
tool.papercontent.started/completed/failed,
context.warning,
llm.explainer.started/completed, llm.reviewer.started/completed,
decision.revise/accept/max_iterations,
tool.vaultwriter.completed,
run.completed, run.failed, run.recovered_to_selection
```

**Interfaces:**

```go
type Recorder interface {
    Emit(evt *Event)             // Enqueue event to buffer + async persist
    Close()                       // Drain buffer, finalize run row
}

type Event struct {
    Seq        int          // Monotonic per-run counter (0,1,2…)
    EventType  string       // e.g. "selection.chosen"
    Stage      string       // PipelineStage
    Title      string       // Human one-liner
    Status     string       // "info" | "success" | "warning" | "error"
    Summary    JSONB        // Small structured fields (~500-char previews, capped)
    PayloadFull JSONB       // Nullable; opt-in full trace only (Phase 8+)
                           // For llm.explainer.completed: {systemPrompt, userPrompt, response}
                           // For llm.reviewer.completed: {systemPrompt, userPrompt, response}
                           // For decision.*: {decision, onPass, flaggedSections, narrative}
    DurationMs *int        // Optional duration
    CreatedAt  time.Time
}
```

**PayloadFull Population (Phase 8):** When config `tracing.full_payloads = true`, PayloadFull is populated for explainer/reviewer LLM calls and decision events. Secret scrubber redacts API keys from both Summary (previewCap: 500 chars) and PayloadFull (payloadCap: 100,000 chars) independently, preventing truncation of full payloads.

**Degradation:** Database unavailable → recorder operates in-memory only. Live SSE timeline works; history and cross-restart reload return 503. Pipeline completes normally, never fatal.

**Dependencies:** `github.com/jackc/pgx/v5`, PostgreSQL (optional), config `tracing:` block

---

### 11. Config

**Purpose:** Single source of truth for all runtime configuration.

```yaml
llm:
  provider: "anthropic"             # anthropic | openai | gemini
  model: "claude-sonnet-4-6"        # any text-capable model string
  api_key: "${LLM_API_KEY}"         # loaded from .env
  max_tokens: 8000
  temperature: 0.3
  timeout_seconds: 120
  base_url: ""                      # optional: override API endpoint

agent:
  max_review_iterations: 2          # Phase 5: critic→revision rounds per paper (0 = disable reviewer, reproduce Phase 4)
  arxiv_category: "cs.AI"           # papers to fetch from arXiv
  arxiv_base_url: "https://export.arxiv.org/api/query"  # arXiv API endpoint
  arxiv_html_base_url: "https://arxiv.org/html"  # arXiv HTML rendering endpoint
  fetch_limit: 20                   # papers fetched per discovery trigger
  display_limit: 5                  # papers shown to user for selection
  request_timeout_sec: 30           # timeout for HTTP requests (arXiv API, HTML fetch)
  max_retries: 3                    # max retries for transient failures (429, 503)
  max_content_bytes: 52428800       # 50MB size cap for fetched paper HTML

arxiv:
  category: "cs.AI"

paths:
  obsidian_vault: "~/obsidian/AI Papers"  # overridden by OBSIDIAN_VAULT_PATH in .env
  log_file: "~/.arxiv-agent/processed.json"

explainer:
  target_words: 2500                # soft target; agent may exceed for complex papers
  follow_up_link_arxiv: true        # attempt to extract arXiv IDs from references

tracing:
  enabled: true                     # master switch for the Recorder (Phase 7)
  full_payloads: false              # Phase 8: opt-in full prompts/responses for explainer/reviewer events
  buffer_size: 256                  # per-run in-memory ring capacity
  # Secret scrubber caps: summary ~500 chars, payload_full ~100k chars (distinct to avoid truncation)
```

> **Path resolution:** `.env` value for `OBSIDIAN_VAULT_PATH` takes precedence over `config.yaml`. This keeps `config.yaml` version-control safe while allowing machine-specific paths in `.env`.

---

## 3. Data Model

### Paper
```go
type Paper struct {
    ID        string
    Title     string
    Authors   []string
    Abstract  string
    PDFURL    string
    Category  string
    Published time.Time
}
```

### ExplainerOutput
```go
type ExplainerOutput struct {
    PaperID      string            // paper's arXiv ID
    Content      string            // final assembled Markdown body
    Sections     map[string]string // keyed by section name:
                                   // "problem_statement"
                                   // "core_idea"
                                   // "methodology"
                                   // "key_findings"
                                   // "limitations"
                                   // "why_it_matters"
                                   // "analogies"
                                   // "glossary"
                                   // "follow_up_papers"
    Iteration    int               // 1 in Phase 4 (revision loop is Phase 5)
    InputTokens  int               // tokens consumed by LLM
    OutputTokens int               // tokens produced by LLM
    CreatedAt    time.Time
}
```

### ReviewVerdict
```go
type ReviewVerdict struct {
    PaperID   string
    Pass      bool
    Score     float32
    Feedback  map[string]string  // section name → revision note
    Iteration int
    CreatedAt time.Time
}
```

### PipelineSession
```go
type PipelineSession struct {
    SessionID      string
    Stage          PipelineStage
    Iteration      int               // Phase 5: current reviewer/revision loop iteration (incremented each round)
    Candidates     []Paper           // frontend-visible: candidates for selection (Phase 8: append via AppendCandidates)
    nextStart      int               // Phase 8: arXiv offset cursor for next pagination page; claimed+advanced atomically via ConsumeNextStart(step)
    Notice         string            // optional user-facing message
    Error          string            // error message if stage = "failed"
    Recoverable    bool              // whether error is transient (can retry)
    
    // Server-only (excluded from Snapshot, never sent to frontend)
    SelectedPaper *Paper            // paper user selected
    MarkdownText  string            // extracted HTML→Markdown from PaperContentTool
    Explainer     *ExplainerOutput  // Phase 4+5: current explainer (updated each iteration)
    Verdict       *ReviewVerdict    // Phase 5: review result from last iteration (nil if reviewer disabled or not yet reviewed)
    // Note: the written note path is NOT stored on the session. GET /runs/{id}/content
    // recovers it from the persisted tool.vaultwriter.completed event's Summary["path"].
    
    StartedAt     time.Time
    CompletedAt   *time.Time
}

// Phase 8: AppendCandidates(newCandidates []Paper) appends to Candidates during StageSelection
// (safe concurrent access via session mutex)
```

type PipelineStage string
const (
    StageDiscovery  PipelineStage = "discovery"      // fetching + filtering papers
    StageSelection  PipelineStage = "selection"      // candidates ready, awaiting pick
    StageExtracting PipelineStage = "extracting"     // fetching + converting HTML → Markdown
    StageGenerating PipelineStage = "generating"     // Phase 4+5: initial explainer generation
    StageReviewing  PipelineStage = "reviewing"      // Phase 5: quality review (critic evaluation)
    StageRevising   PipelineStage = "revising"       // Phase 5: revision loop (iterate on feedback)
    StageWriting    PipelineStage = "writing"        // Phase 4+5: vault write (after all reviews pass)
    StageComplete   PipelineStage = "complete"       // success
    StageFailed     PipelineStage = "failed"         // pipeline aborted
)
```

### ProcessedLog (JSON on disk)
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

### RunRecord (PostgreSQL — Phase 7, optional)
```sql
CREATE TABLE runs (
    id            TEXT PRIMARY KEY,          -- existing session id
    paper_id      TEXT,                      -- null until selection.chosen
    paper_title   TEXT,
    stage         TEXT NOT NULL,             -- last known stage
    status        TEXT NOT NULL,             -- running | complete | failed | recovered
    input_tokens  INT  NOT NULL DEFAULT 0,
    output_tokens INT  NOT NULL DEFAULT 0,
    est_cost_usd  NUMERIC(10,4),
    review_passed BOOLEAN,
    started_at    TIMESTAMPTZ NOT NULL,
    completed_at  TIMESTAMPTZ
);
```

### EventRecord (PostgreSQL — Phase 7+, optional)
```sql
CREATE TABLE run_events (
    run_id       TEXT NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    seq          INT  NOT NULL,             -- monotonic per run
    event_type   TEXT NOT NULL,
    stage        TEXT NOT NULL,
    title        TEXT NOT NULL,
    status       TEXT NOT NULL,             -- info | success | warning | error
    summary      JSONB,
    payload_full JSONB,                     -- Phase 8: opt-in via tracing.full_payloads config
                                           -- Populated for llm.explainer.completed, llm.reviewer.completed, decision.* events
    duration_ms  INT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (run_id, seq)
);
CREATE INDEX idx_runs_started_at ON runs (started_at DESC);
```

### Obsidian Note (Markdown output)
```markdown
---
arxiv_id: "2401.12345"
title: "Paper Title"
authors: ["Author One", "Author Two"]
published: "2026-06-07"
category: "cs.AI"
generated_at: "2026-06-07T10:30:00Z"
review_iterations: 2
review_passed: true
review_score: 0.89
tags: [ai, paper, explainer]
---

# Paper Title

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

### Storage Summary

| Data | Format | Location | Lifetime |
|---|---|---|---|
| PipelineSession | In-memory Go struct | Go backend process | Single run |
| ProcessedLog | JSON file on disk | Configured path | Permanent |
| Obsidian Note | Markdown file | Obsidian vault folder | Permanent |
| Paper Markdown | In-memory `string` | Go backend process (session) | Single run |
| Config | YAML + `.env` | Project root | Permanent |
| **Run Timeline (Phase 7)** | **JSONB rows** | **PostgreSQL (optional)** | **Permanent** |
| **Run Header** | **RunRecord** | **PostgreSQL** | **Permanent** |
| **Events** | **EventRecords** | **PostgreSQL** | **Permanent** |
| **Live Events** | **Ring buffer** | **In-memory per run** | **Single run** |

---

## 4. Data Flow

### Flow 1 — Discovery & Selection

```
User clicks "Find New Papers" in Next.js UI
    │
    ▼
Next.js → POST /discover → Go Orchestrator
    │
    ▼
Orchestrator creates PipelineSession { stage: "discovery" }
    │
    ▼
DiscoveryTool
    → GET https://export.arxiv.org/api/query?cat=cs.AI&max=20&sortBy=submittedDate
    → Parse Atom/XML response
    → Extract top 20 papers as []Paper
    │
    ▼
LogCheckTool
    → Read processed.json from disk
    → Filter out already-processed paper IDs
    → Return top 5 unprocessed papers
    │
    ▼
Orchestrator updates PipelineSession { stage: "selection", candidates: []Paper }
    │
    ▼
Next.js renders candidate list (title, authors, abstract snippet, date, arXiv ID)
```

### Flow 2 — HTML Fetch + Markdown Conversion + Explainer Generation + Review Loop

```
User selects one paper in Next.js UI
    │
    ▼
Next.js → POST /process { session_id, paper_id: "2401.12345" } → Go Orchestrator
    │
    ▼
Orchestrator sets stage → "extracting" (async, returns session_id immediately)
    │
    ▼
PaperContentTool detached goroutine
    → GET https://arxiv.org/html/2401.12345 (follows same-host redirect)
    → HTML → Markdown conversion
    → Return clean Markdown text OR ErrPaperHTMLNotFound (404)
    │         │
    │    [404 Not Found] ──→ Orchestrator.RecoverToSelection() 
    │                          (candidates preserved, return to selection)
    │
    ▼
┌─────────────────────────────────────────────────────────────┐
│  Phase 5: Bounded Critic-Generator Loop (config-driven)     │
│                                                              │
│  maxIter := config.Agent.MaxReviewIterations (0 = skip)    │
│                                                              │
│  for iteration := 1; ; iteration++ {                        │
│    ┌──────────────────────────────────────────────────────┐ │
│    │ GENERATE (Iteration N)                               │ │
│    │  → if N == 1: fresh generation                       │ │
│    │  → if N > 1: revision with feedback from prev round  │ │
│    │  → ExplainerAgent.Generate(markdown, revisionNote)   │ │
│    │  → ExplainerOutput { content, sections, iteration:N} │ │
│    │  → stage := "generating"/"revising"                  │ │
│    │  → SetExplainer(&ex); AddTokens()                    │ │
│    └──────────────────────────────────────────────────────┘ │
│    │                                                         │
│    │  if maxIter == 0 → break (reviewer disabled)           │
│    │                                                         │
│    ▼                                                         │
│    ┌──────────────────────────────────────────────────────┐ │
│    │ REVIEW (Pass N)                                      │ │
│    │  → stage := "reviewing"                              │ │
│    │  → ReviewerAgent.Review(explainer, paper, iter=N)    │ │
│    │  → ReviewVerdict { pass, score, feedback, iter:N }   │ │
│    │  → SetVerdict(&verdict); AddTokens()                 │ │
│    │  → AddTokens(verdict.TokensUsed)                     │ │
│    └──────────────────────────────────────────────────────┘ │
│    │                                                         │
│    │  if verdict.Pass == true → break (approved)            │
│    │  if iteration >= maxIter → break (max reached)         │
│    │  else: continue to REVISE                              │
│    │                                                         │
│    ▼                                                         │
│    ┌──────────────────────────────────────────────────────┐ │
│    │ BUILD REVISION FEEDBACK                              │ │
│    │  → formatRevisionNote(verdict.Feedback)              │ │
│    │  → revisionNote := structured feedback string         │ │
│    │  → loop back to GENERATE with revisionNote           │ │
│    └──────────────────────────────────────────────────────┘ │
│  }                                                            │
│                                                              │
│  ► Loop terminates: either verdict.Pass OR max iterations   │
│  ► Always writes exactly one note (lastEx)                  │
│  ► Always honors maxIter == 0 (Phase 4 path, no reviewer)   │
└─────────────────────────────────────────────────────────────┘
    │
    ▼
Proceed to Flow 3: Vault Write & Completion
```

**Error Handling in Review Loop:**
- **Reviewer JSON parse error** → Stop loop, save explainer with `review_passed: false` (Decision 2: no blind regen)
- **Reviewer LLM/network error** → Fail session recoverably (no write, paper can re-surface)
- **Generator error** → Fail session recoverably
- **Empty generator response** → Fail session recoverably (prevents all-whitespace notes)

### Flow 3 — Vault Write & Completion

```
Orchestrator receives approved ExplainerOutput
    │
    ▼
VaultWriterTool
    → Assemble Markdown frontmatter + ExplainerOutput.Content
    → Generate filename: "2026-06-07_2401.12345_paper-title-slug.md"
    → Atomic write (temp file → rename) to obsidian_vault/AI Papers/
    │
    ▼
LogCheckTool.MarkAsProcessed()
    → Append to processed.json
    │
    ▼
Orchestrator → PipelineSession { stage: "complete" }
    │
    ▼
Next.js renders success + Markdown preview + vault file path
```

### Progress Polling

```
Next.js polls GET /status/:sessionId every 2 seconds via TanStack Query
    │
    ▼
Go Orchestrator returns { stage, candidates, notice, error, recoverable }
    │
    ▼
Next.js displays live status:
"Fetching paper..." → "Extracting HTML..." → "Generating explainer (pass 1)..."
→ "Reviewing..." → "Revising (pass 2)..." → "Saving to vault..."

On 404 (recoverable): candidates re-enabled, selection UI re-shown
```

---

## 5. Tech Stack & Rationale

### Frontend

| Technology | Version | Why |
|---|---|---|
| **Next.js** | 16.2.7 LTS | Full-stack React framework — UI + API proxy routes in one project. Runs locally with `next dev`. |
| **TypeScript** | 5.x | Type safety across API contracts between frontend and Go backend. |
| **Tailwind CSS** | 4.3.0 | Utility-first styling. Fast to build a clean local UI. |
| **TanStack Query** | 5.101.0 | Handles polling `/status/:sessionId` cleanly with automatic retries and loading states. |

### Backend

| Technology | Version | Why |
|---|---|---|
| **Go** | 1.26.4 | Single binary, fast startup, excellent concurrency. No runtime to manage locally. |
| **Google ADK Go** | `google.golang.org/adk` latest | Agent orchestration primitives — tool registration, session management, agent loop. Used for orchestration only, not model binding. |
| **`net/http`** | stdlib | Expose HTTP endpoints to Next.js. No framework overhead needed for routes. |
| **`gopkg.in/yaml.v3`** | latest | Parse YAML config file. |
| **`godotenv`** | latest | Load `.env` for API keys in local development. |
| **`air`** | latest | Live reload for Go backend during development. |
| **`html-to-markdown/v2`** | latest | Pure-Go HTML-to-Markdown conversion (no CGO, no external dependencies). Converts arXiv LaTeXML HTML to clean text. |
| **`github.com/jackc/pgx/v5`** | latest | PostgreSQL driver (Phase 7); optional for run timeline tracing. |

### LLM Providers (configurable, text-capable required)

All providers sit behind the `LLMClient` interface. Paper content is sent as **text only** (Markdown) — not images — making the interface compatible with any text-capable model.

**No vision requirement:** Any text-capable model works. No validation for vision capability. This maximizes flexibility: cheaper models, longer context windows, models without vision support.

| Provider | SDK | Default Model | Text Input Format |
|---|---|---|---|
| **Anthropic Claude** | `github.com/anthropics/anthropic-sdk-go` | `claude-sonnet-4-6` | Text blocks in messages array |
| **OpenAI** | `github.com/openai/openai-go` | `gpt-4o` (or `gpt-4-turbo`) | Text content in messages array |
| **Google Gemini** | `google.golang.org/genai` | `gemini-2.0-flash` (large context) | Inline text parts; largest context window |

**LLM configuration block:**
```yaml
llm:
  provider: "anthropic"
  model: "claude-sonnet-4-6"
  api_key: "${LLM_API_KEY}"
  max_tokens: 8000
  temperature: 0.3
  timeout_seconds: 120
  base_url: ""
```

### External APIs

| API | Cost | Rate Limit | Notes |
|---|---|---|---|
| **arXiv API** | Free, no auth required | 1 request per 3 seconds, single connection | Our usage (one trigger = one query) is well within limits |
| **LLM Provider API** | Pay-per-token | Provider-specific | Configured via `.env` |

### Storage

| Technology | Why |
|---|---|
| **Local filesystem** | Obsidian vault is a local folder. Direct file write is simplest and most reliable. |
| **JSON file (processed log)** | Flat list of paper IDs needs no database. Human-readable and manually editable. |
| **In-memory (session state)** | Transient data — pipeline session and paper Markdown exist only for duration of one run. |
| **PostgreSQL 17 (Phase 7, optional)** | Run timeline tracing: durable history of events, cross-restart replay, live SSE fan-out. Gracefully degrades if unavailable. Docker Compose supplied. |

---

## 6. Integration Points

### 1. arXiv API

**Base URL:** `https://export.arxiv.org/api/query`
**Auth:** None

```
GET https://export.arxiv.org/api/query
  ?search_query=cat:cs.AI
  &sortBy=submittedDate
  &sortOrder=descending
  &max_results=20
  &start=0
```

**Response:** Atom/XML, parsed via `encoding/xml`

**Constraints:**
- Single connection at a time
- Minimum 3-second delay between requests
- Retry with exponential backoff on 429 (max 3 retries)
- Descriptive `User-Agent` header required

---

### 2. arXiv HTML Rendering

**URL pattern:** `https://arxiv.org/html/{arxiv_id}`
**Auth:** None

- Direct `GET` to HTML rendering endpoint
- Follows same-host redirect (arXiv redirects to versioned URL automatically)
- Timeout: configurable (`config.Agent.RequestTimeoutSec`, default 30s)
- Size limit: 50MB (`io.LimitReader`) for safety
- Retry on transient failures (429, 503, network) per `config.Agent.MaxRetries`
- **404 Not Found** → treated as recoverable (return to selection, allow re-pick)

---

### 3. LLM Provider APIs (Text-Only)

All providers receive paper content as **text only** (Markdown). No images, no vision APIs.

**Anthropic Claude:**
```
POST https://api.anthropic.com/v1/messages
Headers: x-api-key, anthropic-version
Content: DocumentText as message content block
```

**OpenAI:**
```
POST https://api.openai.com/v1/chat/completions
Headers: Authorization: Bearer {key}
Content: DocumentText as message content
```

**Google Gemini:**
```
POST https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent
Headers: Authorization: Bearer {key}
Content: DocumentText as inline text part
```

**Error handling (shared `withRetry` logic):**

| Error | Behaviour |
|---|---|
| `429` | Exponential backoff, max 3 retries |
| `503` | Exponential backoff, max 1 retry |
| `400` | Surface immediately as configuration error (not retried) |
| Timeout | Surface immediately (not retried) |

---

### 4. Local Filesystem

**Write targets:**
```
{obsidian_vault_path}/AI Papers/
    └── 2026-06-07_2401.12345_paper-title-slug.md

{log_file_path}/
    └── processed.json
```

- Vault subfolder created automatically if it doesn't exist
- Atomic write strategy: write to temp file → rename
- All filenames sanitized (alphanumeric, hyphens, underscores, dots only)
- All paths validated against configured base path (path traversal prevention)

---

### 5. Next.js ↔ Go Backend

**Communication:** HTTP on localhost only. CORS restricted to `localhost:3000`.

```
POST   localhost:8080/discover                 → { session_id, candidates: [Paper] }
POST   localhost:8080/discover/:sessionId/more → { candidates: [Paper] } (Phase 8: offset pagination)
POST   localhost:8080/process                  → { session_id } (async processing begins)
GET    localhost:8080/status/:id               → { stage, iteration, error }
GET    localhost:8080/result/:id               → { content, vault_file_path }
GET    localhost:8080/runs/:id/content         → { available: bool, content?: string } (Phase 8)
GET    localhost:8080/health                   → { status: "ok" }
```

**Phase 8 Endpoints:**
- `POST /discover/:sessionId/more` — appends additional arXiv candidates to existing session during StageSelection; returns 409 if not in selection stage
- `GET /runs/:id/content` — fetches persisted Obsidian note markdown from disk; returns {available: false} gracefully if file missing (vault write failed or file deleted)

---

## 7. Cross-Cutting Concerns

### Security

- API keys stored in `.env` only — never hardcoded, never committed
- `.env` in `.gitignore` by default
- Go backend reads keys at startup — never passed to frontend
- Go backend binds to `127.0.0.1:8080` only (not `0.0.0.0`)
- CORS restricted to `localhost:3000`
- Filenames sanitized; all writes validated within configured vault directory

### Error Handling

**Philosophy:** Fail loudly, never silently. Distinguish between transient (retryable) and permanent failures.

| Stage | Failure | Behaviour |
|---|---|---|
| arXiv fetch | Network error / 429 | Retry 3x with backoff, surface error if all fail |
| HTML fetch | Network error / 429 / 503 | Retry with backoff per config.Agent.MaxRetries |
| HTML fetch | 404 Not Found | **Recoverable re-pick**: RecoverToSelection(), candidates preserved, return to selection UI |
| HTML fetch | Other 4xx / timeout | Surface as non-recoverable error |
| LLM call | 429 rate limit | Retry 3x with backoff (shared withRetry logic) |
| LLM call | 503 unavailable | Retry 1x with backoff (shared withRetry logic) |
| LLM call | 400 bad request | Surface immediately as config error (not retried) |
| LLM call | Timeout | Surface immediately (not retried) |
| Reviewer loop | Max iterations reached | Accept last output, proceed with warning flag in frontmatter |
| Vault write | Permission error / disk full | Surface error, do NOT update processed log |
| Log write | Failure after vault write | Log warning — paper remains re-processable |

### Observability

- Go `slog` (stdlib) for structured JSON logs to stdout
- Every pipeline stage logged with `session_id`, `paper_id`, `stage`, `duration_ms`
- Every LLM call logged with `provider`, `model`, `tokens_used`, `duration_ms`
- Every retry logged with `attempt`, `error`, `backoff_ms`
- Token usage surfaced in UI on completion

### Performance

| Bottleneck | Mitigation |
|---|---|
| LLM generation (30–120s) | Non-blocking async pipeline, UI polls status |
| PDF download size | Stream into memory, 30s timeout |
| Reviewer loop | Configurable cap, default 2 iterations |
| arXiv rate limit | Single sequential request per trigger |

### Resilience

- Crash mid-run: session lost, vault not written, log not updated — safe to re-trigger
- Vault write success + log write failure: paper re-surfaces on next run (acceptable)
- Obsidian sync conflicts: atomic rename reduces write window to milliseconds

### Developer Experience

- `make dev` starts both services concurrently
- `.env.example` committed with all keys documented
- Config YAML validated at Go startup with clear error messages
- `GET /health` endpoint for sanity checks

---

## 8. Risks & Tradeoffs

### Risks

| ID | Risk | Severity | Mitigation | Residual Risk |
|---|---|---|---|---|
| R1 | LLM hallucination in explainer content | Medium | ReviewerAgent catches inconsistencies against explainer text. arXiv ID and link in frontmatter enable source verification. |
| R2 | arXiv API instability / 429 enforcement | Low | Retry with backoff; sequential requests stay within limits. |
| R3 | arXiv HTML rendering unavailable (404) for some papers | Low | 404 is treated as recoverable re-pick. User can select another paper without losing session. |
| R4 | HTML-to-Markdown conversion loses important visual structure (diagrams, tables) | Medium | Figure captions are preserved in Markdown. Limitations documented in system prompt — agents instructed to note "see figure X" for complex diagrams. |
| R5 | Very long papers (40+ pages) may exceed model context window | Medium | Context window pre-check in ExplainerAgent Phase 4. Gemini available as large-context fallback. |
| R6 | Reviewer loop adds cost and latency | Low | Configurable cap; can set to 0 to disable. Full run with 2 cycles may cost $0.05–$0.50 per paper depending on length and provider. |
| R7 | ADK Go maturity (released Nov 2025) | Medium | Used for orchestration only; blast radius limited. Pinning dependency version in `go.mod` reduces surprise upgrades. |
| T1 | Text-only vs. vision-capable models | Accepted | Pure text approach maximizes flexibility (cheaper models, longer context, more providers). HTML conversion with figure captions provides sufficient semantic content. Vision reserved for future enhancement if needed. |
| T2 | HTML rendering via `html-to-markdown/v2` (pure Go) | Accepted | No external dependencies, no CGO, no poppler. Clean, simple, maintainable. Converts semantic structure well. |
| T3 | Same LLM for reviewer and explainer | Accepted | Single config, simpler operation. Different system prompts and temperatures create meaningfully different evaluation behaviour. |
| T4 | Single paper per run | Accepted | Human-in-the-loop control, simpler state management. |
| T5 | In-memory session state | Accepted | Zero infrastructure complexity. Crash recovery not required for a local, single-user tool. |
| T6 | Recency ranking only (no relevance) | Accepted | Explicitly deferred per PRD. Simple, predictable, zero complexity. |
