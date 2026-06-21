# Architecture Document
## ArXiv AI Paper Explainer Agent

---

## 1. System Overview

### High-Level Description

The system is a **local, trigger-based, two-service application**:

- A **Next.js frontend** handles all user interaction — triggering runs, displaying paper candidates, selection, progress, and the final explainer preview.
- A **Go backend** built on ADK Go handles all agent logic — paper discovery, duplicate checking, PDF retrieval, explainer generation, critic-review loop, and Obsidian vault writing.

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
[PDFFetchTool] ──→ Download selected paper PDF
    │
    ▼
[ExplainerAgent] ──→ Read PDF + Generate rich Markdown explainer
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
│  - Explainer preview        │         │  - PDFFetchTool                  │
│                             │         │  - ExplainerAgent                │
│                             │         │  - ReviewerAgent                 │
│                             │         │  - VaultWriterTool               │
│                             │         │  - LLM Client (configurable)     │
└─────────────────────────────┘         └──────────────────────────────────┘
                                                        │
                                    ┌───────────────────┼───────────────────┐
                                    │                   │                   │
                             ┌──────▼─────┐    ┌───────▼──────┐   ┌───────▼──────┐
                             │ arXiv API  │    │  LLM Provider │   │Obsidian Vault│
                             │ (external) │    │  (configured) │   │ (local disk) │
                             └────────────┘    └──────────────┘   └──────────────┘
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
POST /discover           → triggers DiscoveryTool + LogCheckTool
POST /process            → triggers full pipeline for selected paper ID
GET  /status/:sessionId  → returns current pipeline stage + progress
GET  /result/:sessionId  → returns final explainer content
GET  /health             → sanity check endpoint
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

func FetchPapers(ctx context.Context, limit int) ([]Paper, error)
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

### 5. PDFFetchTool (ADK Tool)

**Purpose:** Download the PDF of the selected paper from arXiv.

**Responsibilities:**
- Fetch PDF binary from arXiv PDF URL
- Handle arXiv redirects to versioned PDF URLs automatically
- Store temporarily in memory
- Return raw PDF bytes for rendering

**Interface:**
```go
func FetchPDF(ctx context.Context, pdfURL string) ([]byte, error)
```

**Dependencies:** arXiv (external HTTP)

---

### 6. PDFRendererTool (ADK Tool)

**Purpose:** Convert raw PDF bytes into an ordered slice of PNG page images, one per page, ready for vision LLM input.

**Why page-as-image:** AI research papers rely heavily on visual content — architecture diagrams, result tables, mathematical figures. Text extraction silently discards this content. Rendering every page as an image ensures the LLM sees exactly what a human reader would see, preserving full visual fidelity regardless of paper content.

**Implementation:** Shells out to `poppler`'s `pdftoppm` binary — industry-standard quality, available as a single system package (`brew install poppler` / `apt install poppler-utils`), zero CGO complexity.

**Responsibilities:**
- Write PDF to temp file (required by `pdftoppm`)
- Execute `pdftoppm -png -r {dpi}` as a subprocess
- Read output PNGs in sorted order into memory
- Clean up all temp files on success or failure

**Interface:**
```go
func RenderPages(ctx context.Context, pdfBytes []byte) ([][]byte, error)
// Returns [][]byte — one PNG []byte per page, in reading order
```

**Config:** `pdf.dpi` (default `150`) — controls rendering resolution and token cost tradeoff.

**Startup validation:** `ValidatePoppler()` called in `main.go` — server fails fast with install instructions if `pdftoppm` not in PATH.

**Dependencies:** `os/exec` (poppler subprocess), `os` (temp files), `config`

---

### 7. ExplainerAgent (ADK LlmAgent)

**Purpose:** Core intelligence — read every page of the paper as a vision LLM would, reason about the authors' core intent, and generate the rich Markdown explainer.

**Responsibilities:**
- Receive page images (one PNG per page, in order) + structured prompt
- Reason about the paper's core intent — including visual content (diagrams, tables, figures)
- Generate all explainer sections per PRD spec
- Handle math and visual content contextually
- Accept structured revision feedback from ReviewerAgent and rewrite accordingly

**Interface:**
```go
type ExplainerInput struct {
    PageImages   [][]byte  // one PNG per page, in reading order
    PaperMeta    Paper
    RevisionNote string    // empty on first pass
}

type ExplainerOutput struct {
    Content   string            // full Markdown
    Sections  map[string]string // keyed by section name
    Iteration int
    CreatedAt time.Time
}

func Generate(ctx context.Context, input ExplainerInput) (ExplainerOutput, error)
```

**Dependencies:** LLM Client (configurable)

---

### 8. ReviewerAgent (ADK LlmAgent)

**Purpose:** Independent critic — evaluates explainer quality and drives the revision loop.

**Responsibilities:**
- Evaluate explainer against a fixed quality rubric:
  - Is the core author intent clearly captured?
  - Are analogies accurate and layered (intuition → engineering)?
  - Is math handled appropriately?
  - Are diagrams and figures described and explained correctly?
  - Is the glossary prioritized correctly?
  - Is the tone right for technical practitioners?
- Return structured verdict: `Pass` or `Fail` with actionable per-section feedback
- Signal loop termination when quality threshold is met or max iterations reached

**Interface:**
```go
type ReviewVerdict struct {
    Pass      bool
    Score     float32            // 0.0 - 1.0
    Feedback  map[string]string  // section → revision note
    Iteration int
    CreatedAt time.Time
}

func Review(ctx context.Context, explainer ExplainerOutput, iteration int) (ReviewVerdict, error)
```

**Dependencies:** LLM Client (configurable), Config (max iterations)

---

### 9. VaultWriterTool (ADK Tool)

**Purpose:** Persist the approved explainer to the Obsidian vault.

**Responsibilities:**
- Format final Markdown with consistent frontmatter
- Generate filename: `YYYY-MM-DD_arxivID_slug-title.md`
- Write file atomically to configured Obsidian vault subfolder
- Trigger `LogCheckTool.MarkAsProcessed()` after successful write

**Interface:**
```go
func WriteToVault(ctx context.Context, explainer ExplainerOutput, meta Paper) (string, error)
// returns the file path written
```

**Dependencies:** Local filesystem (vault path from config), LogCheckTool

---

### 10. LLM Client (configurable)

**Purpose:** Provider-agnostic LLM interface. Decouples all agent logic from any specific LLM API. Accepts page images as vision input — not PDF bytes — making it compatible with any vision-capable model regardless of native PDF support.

**Responsibilities:**
- Expose a unified `Complete(prompt, images)` interface
- Accept page images (PNG bytes, one per page) as vision input — universal across all vision-capable providers
- Load provider, model, API key, and parameters from config
- Concrete implementations for: Anthropic Claude, OpenAI, Google Gemini (others addable by implementing the interface)
- Validate at startup that configured model is vision-capable

**Interface:**
```go
type LLMClient interface {
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

type CompletionRequest struct {
    SystemPrompt string
    UserPrompt   string
    PageImages   [][]byte  // one PNG per page, in reading order
    MaxTokens    int
    Temperature  float32
}

type CompletionResponse struct {
    Content      string
    InputTokens  int
    OutputTokens int
}
```

**Dependencies:** Config, respective provider SDKs

---

### 11. Config

**Purpose:** Single source of truth for all runtime configuration.

```yaml
llm:
  provider: "anthropic"             # anthropic | openai | gemini
  model: "claude-sonnet-4-6"        # any vision-capable model string
  api_key: "${LLM_API_KEY}"         # loaded from .env
  max_tokens: 8000
  temperature: 0.3
  timeout_seconds: 120
  base_url: ""                      # optional: override API endpoint

agent:
  max_review_iterations: 2          # reviewer loop cap (0 = disable review)
  paper_fetch_limit: 5              # top N papers per discovery run

arxiv:
  category: "cs.AI"

pdf:
  dpi: 150                          # 100=fast/cheap, 150=balanced, 200=high quality

paths:
  obsidian_vault: "~/obsidian/AI Papers"  # overridden by OBSIDIAN_VAULT_PATH in .env
  log_file: "~/.arxiv-agent/processed.json"

explainer:
  target_words: 2500                # soft target; agent may exceed for complex papers
  follow_up_link_arxiv: true        # attempt to extract arXiv IDs from references
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
    PaperID   string
    Content   string            // final assembled Markdown
    Sections  map[string]string // keyed by section name:
                                // "problem_statement"
                                // "core_idea"
                                // "methodology"
                                // "key_findings"
                                // "limitations"
                                // "why_it_matters"
                                // "analogies"
                                // "glossary"
                                // "follow_up_papers"
    Iteration int
    CreatedAt time.Time
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
    SessionID     string
    Stage         PipelineStage
    Candidates    []Paper
    SelectedPaper *Paper
    PDF           []byte
    Explainer     *ExplainerOutput
    LastVerdict   *ReviewVerdict
    Iterations    int
    Error         string
    StartedAt     time.Time
    CompletedAt   *time.Time
}

type PipelineStage string
const (
    StageDiscovery  PipelineStage = "discovery"
    StageSelection  PipelineStage = "selection"
    StageFetching   PipelineStage = "fetching_pdf"
    StageGenerating PipelineStage = "generating"
    StageReviewing  PipelineStage = "reviewing"
    StageRevising   PipelineStage = "revising"
    StageWriting    PipelineStage = "writing"
    StageComplete   PipelineStage = "complete"
    StageFailed     PipelineStage = "failed"
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

### Obsidian Note (Markdown output)
```markdown
---
arxiv_id: "2401.12345"
title: "Paper Title"
authors: ["Author One", "Author Two"]
published: "2026-06-07"
category: "cs.AI"
generated_at: "2026-06-07T10:30:00Z"
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
| PDF bytes | In-memory `[]byte` | Go backend process | Until rendering complete |
| Page images | In-memory `[][]byte` | Go backend process | Single run |
| Config | YAML + `.env` | Project root | Permanent |

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

### Flow 2 — PDF Fetch + Render + Explainer Generation + Review Loop

```
User selects one paper in Next.js UI
    │
    ▼
Next.js → POST /process { paper_id: "2401.12345" } → Go Orchestrator
    │
    ▼
PDFFetchTool → GET https://arxiv.org/pdf/2401.12345 → []byte in memory
    │
    ▼
PDFRendererTool
    → pdftoppm -png -r 150 → [][]byte (one PNG per page, in reading order)
    → temp files cleaned up
    │
    ▼
ExplainerAgent (iteration 1)
    → CompletionRequest { system_prompt, user_prompt, page_images: [PNG×N], revision_note: "" }
    → LLMClient.Complete()
    → ExplainerOutput { content, sections, iteration: 1 }
    │
    ▼
ReviewerAgent
    → CompletionRequest { system_prompt, rubric, explainer sections (text only, no images) }
    → LLMClient.Complete()
    → ReviewVerdict { pass, score, feedback, iteration }
    │
    ▼
┌─── Orchestrator evaluates verdict ───────────────────────────────┐
│                                                                   │
│  if verdict.Pass == true OR iterations >= config.MaxIterations:  │
│      → proceed to Flow 3                                         │
│                                                                   │
│  else:                                                            │
│      → pass verdict.Feedback back to ExplainerAgent              │
│      → ExplainerAgent rebuilds with revision_note from feedback  │
│      → loop                                                       │
└───────────────────────────────────────────────────────────────────┘
```

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
Go Orchestrator returns { stage, iteration, error }
    │
    ▼
Next.js displays live status:
"Fetching PDF..." → "Generating explainer (pass 1)..."
→ "Reviewing..." → "Revising (pass 2)..." → "Saving to vault..."
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
| **`net/http`** | stdlib | Expose HTTP endpoints to Next.js. No framework overhead needed for 3 routes. |
| **`gopkg.in/yaml.v3`** | latest | Parse YAML config file. |
| **`godotenv`** | latest | Load `.env` for API keys in local development. |
| **`air`** | latest | Live reload for Go backend during development. |
| **`poppler-utils`** | system package | Provides `pdftoppm` for high-quality PDF page rendering. `brew install poppler` / `apt install poppler-utils`. Required prerequisite — server validates at startup. |

### LLM Providers (configurable, vision-capable required)

All providers sit behind the `LLMClient` interface. Page images (PNG bytes) are sent as vision input — not PDF bytes — making the interface compatible with any vision-capable model or custom endpoint.

**Vision requirement:** The configured model must support image/vision input. Validated at startup against a known-models list. Unknown/custom models produce a warning but are allowed.

| Provider | SDK | Default Model | Vision Input Format |
|---|---|---|---|
| **Anthropic Claude** | `github.com/anthropics/anthropic-sdk-go` | `claude-sonnet-4-6` | Base64 PNG image blocks in messages array |
| **OpenAI** | `github.com/openai/openai-go` | `gpt-4o` | Base64 data URL image_url content parts |
| **Google Gemini** | `google.golang.org/genai` | `gemini-2.0-flash` | Inline PNG blob parts; largest context window |

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
| **In-memory (PDF + session)** | Transient data — exists only for duration of one run. |

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

### 2. arXiv PDF Download

**URL pattern:** `https://arxiv.org/pdf/{arxiv_id}`
**Auth:** None

- Direct `GET` to PDF URL, streamed into `[]byte` in memory
- Handle redirects (arXiv redirects to versioned PDF URL)
- Timeout: 30 seconds
- Respect same 3-second delay as API calls

---

### 3. LLM Provider APIs

**Anthropic Claude:**
```
POST https://api.anthropic.com/v1/messages
Headers: x-api-key, anthropic-version
PDF: base64 document block in messages[]
```

**OpenAI:**
```
POST https://api.openai.com/v1/chat/completions
Headers: Authorization: Bearer {key}
PDF: base64 encoded content block
```

**Google Gemini:**
```
POST https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent
Headers: Authorization: Bearer {key}
PDF: inline_data base64 part
```

**Error handling across all providers:**

| Error | Behaviour |
|---|---|
| `429` | Exponential backoff, max 3 retries |
| `400` | Surface as configuration error |
| `500/503` | Retry once, then surface as provider error |
| Timeout | Surface with duration and paper ID |

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
POST   localhost:8080/discover         → { session_id, candidates: [Paper] }
POST   localhost:8080/process          → { session_id } (async processing begins)
GET    localhost:8080/status/:id       → { stage, iteration, error }
GET    localhost:8080/result/:id       → { content, vault_file_path }
GET    localhost:8080/health           → { status: "ok" }
```

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

**Philosophy:** Fail loudly, never silently. Never write a partial note to the vault.

| Stage | Failure | Behaviour |
|---|---|---|
| arXiv fetch | Network error / 429 | Retry 3x with backoff, surface error if all fail |
| PDF download | Timeout / 404 | Surface error with paper ID, abort run |
| LLM call | 429 / timeout | Retry 3x with backoff, surface provider error |
| LLM call | 400 bad request | Surface as config error |
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
| R3 | Very long papers (40+ pages) may exceed model context window at 150 DPI | Medium | Context window pre-check warns before LLM call. Gemini available as large-context fallback. DPI configurable to reduce token cost. |
| R4 | `poppler` version differences across OS produce slightly different rendering | Low | Text legibility consistent across versions. Cosmetic differences do not affect LLM comprehension. |
| R5 | Reviewer loop adds cost and latency | Low | Configurable cap; can set to 0 to disable. Full run with 2 cycles may cost $0.05–$0.50 per paper depending on length and provider. |
| R6 | ADK Go maturity (released Nov 2025) | Medium | Used for orchestration only; blast radius limited. Pinning dependency version in `go.mod` reduces surprise upgrades. |
| T1 | Page-as-image uses significantly more tokens than text extraction | Accepted | Full visual fidelity is the core design goal — diagrams, tables, and figures are central to understanding AI papers. DPI is configurable to manage cost. MarkItDown and text-extraction approaches were evaluated and rejected. |
| T2 | External `poppler` dependency | Accepted | Single install command. Well-maintained, available on all target platforms. CGO-based alternatives add more complexity, not less. |
| T3 | Same LLM for reviewer and explainer | Accepted | Single config, simpler operation. Different system prompts and temperatures create meaningfully different evaluation behaviour. |
| T4 | Single paper per run | Accepted | Human-in-the-loop control, simpler state management. |
| T5 | In-memory session state | Accepted | Zero infrastructure complexity. Crash recovery not required for a local, single-user tool. |
| T6 | Recency ranking only (no relevance) | Accepted | Explicitly deferred per PRD. Simple, predictable, zero complexity. |
