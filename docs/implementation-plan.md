# Implementation Plan
## ArXiv AI Paper Explainer Agent

---

## Overview

Six phases ordered by dependency and risk. Each phase produces a working, testable slice of the system. No phase requires the next to be useful on its own.

| Phase | Focus | Key Output |
|---|---|---|
| 1 | Scaffolding & Config | Both services run, config validates |
| 2 | Discovery & Deduplication | 5 candidate papers in UI |
| 3 | HTML Extraction & LLM Client | Paper Markdown extracted, LLM calls work, 404 recovery implemented |
| 4 | Explainer & Vault Write | Full note saved to Obsidian |
| 5 | Reviewer & Revision Loop | Critic-generator loop working |
| 6 | Polish & Hardening | Error handling, logging, README |

---

## Phase 1 — Project Scaffolding & Config

**Goal:** Both services run locally with one command. Config loads correctly.

### Tasks

#### Next.js Frontend
- [ ] Initialize Next.js 16.2.7 LTS app (`create-next-app`, TypeScript, Tailwind CSS 4.3.0)
- [ ] Install TanStack Query 5.101.0
- [ ] Set up base project structure:
  ```
  /frontend
    /app
      /page.tsx          # main UI entry
      /api               # proxy routes to Go backend
    /components          # UI components
    /lib                 # API client helpers
  ```

#### Go Backend
- [ ] Initialize Go 1.26.4 module (`go mod init`)
- [ ] Install dependencies:
  - `google.golang.org/adk` (ADK Go)
  - `gopkg.in/yaml.v3`
  - `github.com/joho/godotenv`
- [ ] Install `air` for live reload
- [ ] Set up base project structure:
  ```
  /backend
    /cmd/server          # main entrypoint
    /internal
      /config            # config loader
      /orchestrator      # ADK agent runner
      /tools             # ADK tools
      /agents            # ADK LlmAgents
      /llm               # LLMClient interface + implementations
      /models            # shared data structs
    /config.yaml         # default config
    .env.example         # documented key template
  ```

#### Config System
- [ ] Implement `config.yaml` loader using `gopkg.in/yaml.v3`
- [ ] Implement `.env` loader using `godotenv`
- [ ] Implement path resolution: `.env` `OBSIDIAN_VAULT_PATH` overrides `config.yaml`
- [ ] Implement startup validation — fail fast with clear error on missing required fields
- [ ] Document all config fields in `.env.example`

**Default `config.yaml`:**
```yaml
llm:
  provider: "anthropic"             # anthropic | openai | gemini
  model: "claude-sonnet-4-6"
  api_key: "${LLM_API_KEY}"
  max_tokens: 8000
  temperature: 0.3
  timeout_seconds: 120
  base_url: ""

agent:
  max_review_iterations: 2          # 0 = disable review loop
  paper_fetch_limit: 5

arxiv:
  category: "cs.AI"

paths:
  obsidian_vault: "~/obsidian/AI Papers"  # overridden by OBSIDIAN_VAULT_PATH in .env
  log_file: "~/.arxiv-agent/processed.json"

explainer:
  target_words: 2500                # soft target; agent may exceed for complex papers
  follow_up_link_arxiv: true
```

#### Health Endpoint
- [ ] Implement `GET /health` → `{ "status": "ok", "version": "0.1.0" }`
- [ ] Bind Go backend to `127.0.0.1:8080` only
- [ ] Set CORS to allow `localhost:3000` only

#### Dev Tooling
- [ ] Create `Makefile` with `make dev` — starts both Next.js and Go backend concurrently
- [ ] Create `.env.example` with all required keys documented:
  ```
  LLM_API_KEY=your_key_here
  LLM_PROVIDER=anthropic
  OBSIDIAN_VAULT_PATH=/Users/you/obsidian/AI Papers
  ```
- [ ] Add `.env` to `.gitignore`

### Exit Criteria
- [ ] `make dev` starts both services without errors
- [ ] `GET localhost:8080/health` returns `200 { "status": "ok" }`
- [ ] Config validation rejects missing `LLM_API_KEY` with clear error message
- [ ] Config validation rejects missing vault path with clear error message

---

## Phase 2 — arXiv Discovery + Duplicate Detection

**Goal:** Agent can fetch, filter, and return 5 candidate papers.

### Tasks

#### DiscoveryTool
- [ ] Implement arXiv API query:
  ```
  GET https://export.arxiv.org/api/query
    ?search_query=cat:cs.AI
    &sortBy=submittedDate
    &sortOrder=descending
    &max_results=20
  ```
- [ ] Implement Atom/XML parser using `encoding/xml`
- [ ] Extract per-paper: ID, title, authors, abstract, PDF URL, published date
- [ ] Implement 3-second minimum delay between requests
- [ ] Implement retry with exponential backoff on 429 (max 3 retries)
- [ ] Set descriptive `User-Agent` header per arXiv terms of use

#### LogCheckTool
- [ ] Implement `processed.json` reader
- [ ] Implement `FilterUnprocessed(papers []Paper) ([]Paper, error)` — cross-reference against log
- [ ] Return top 5 unprocessed papers (ordered by recency)
- [ ] Create log file and parent directory automatically if they don't exist
- [ ] Implement `MarkAsProcessed(paperID string) error` (used in Phase 4)

#### Orchestrator — Discovery Endpoint
- [ ] Implement `PipelineSession` struct and in-memory session store
- [ ] Implement `POST /discover`:
  - Create session with `stage: "discovery"`
  - Run DiscoveryTool + LogCheckTool
  - Update session with `stage: "selection"` and candidates
  - Return `{ session_id, candidates: [Paper] }`
- [ ] Implement `GET /status/:sessionId` — return current session stage + iteration + error

#### Next.js — Discovery UI
- [ ] Implement trigger button → `POST /api/trigger` → Go `/discover`
- [ ] Build candidate list UI per paper:
  - Title
  - Authors (comma-separated)
  - Abstract snippet (first 300 characters + ellipsis)
  - Published date
  - arXiv ID badge
- [ ] Show loading state during discovery
- [ ] Show error state if discovery fails

### Exit Criteria
- [ ] Clicking trigger returns 5 real `cs.AI` papers from arXiv
- [ ] Papers are ordered by recency (most recent first)
- [ ] Running trigger a second time after processing a paper does not re-surface that paper
- [ ] 429 from arXiv retries automatically and surfaces error if all retries fail

---

## Phase 3 — HTML Fetch + Markdown Conversion + LLM Client

**Goal:** Paper HTML extracted successfully and converted to Markdown. LLM client works with at least one provider. 404 recovery implemented.

### Tasks

#### PaperContentTool
- [x] Implement `FetchMarkdown(ctx, arxivID string) (string, error)`
- [x] Fetch from `https://arxiv.org/html/{arxivID}` (handle same-host redirect)
- [x] Implement `io.LimitReader` (50MB cap) for safety
- [x] Convert LaTeXML HTML to Markdown using `html-to-markdown/v2`
- [x] Extract main `ltx_document` body
- [x] Strip math, navigation, bibliography, appendix (keep headings + captions)
- [x] Implement retry logic for transient failures (429, 5xx, network)
- [x] Distinguish 404 → `ErrPaperHTMLNotFound` (recoverable re-pick)
- [x] Set timeout per `config.Agent.RequestTimeoutSec`

#### LLMClient Interface (Text-Only)
- [x] Define `LLMClient` interface:
  ```go
  type LLMClient interface {
      Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
  }

  type CompletionRequest struct {
      SystemPrompt string
      UserPrompt   string
      DocumentText string              // Markdown text (no images)
      MaxTokens    int
      Temperature  float32
  }

  type CompletionResponse struct {
      Content      string
      InputTokens  int
      OutputTokens int
  }
  ```
- [x] Implement config-driven provider selector (reads `llm.provider` from config)
- [x] Implement shared `withRetry` wrapper: 429 (3 retries), 503 (1 retry), 400 (immediate fail)

#### Anthropic Claude Implementation
- [x] Install `github.com/anthropics/anthropic-sdk-go`
- [x] Implement `AnthropicClient.Complete()`:
  - DocumentText as text block in `messages[]`
  - Map `CompletionRequest` → Anthropic API request format
  - Map response → `CompletionResponse` (extract input/output tokens separately)
- [x] Apply `withRetry` wrapper for error handling

#### OpenAI Implementation
- [x] Install `github.com/openai/openai-go`
- [x] Implement `OpenAIClient.Complete()`:
  - DocumentText as text content in messages
  - Map request/response formats
  - Apply `withRetry` wrapper

#### Google Gemini Implementation
- [x] Install `google.golang.org/genai`
- [x] Implement `GeminiClient.Complete()`:
  - DocumentText as inline text part
  - Map request/response formats
  - Apply `withRetry` wrapper

#### Orchestrator — Process Endpoint + Async Pipeline
- [x] Implement `POST /process { session_id, paper_id }`:
  - Validate session exists and is in `selection` stage
  - Update stage to `extracting`
  - Return `{ session_id }` immediately
  - Run `runPipeline` detached goroutine
- [x] Implement 404 recovery in `runPipeline`:
  - On `ErrPaperHTMLNotFound`: call `session.RecoverToSelection(notice)`
  - Candidates preserved, return to selection UI for re-pick
- [x] Wire: `PaperContentTool.FetchMarkdown()` → store in session
- [x] Construct LLMClient at startup (`orchestrator.New()` returns error if config invalid)

#### Session Model Updates
- [x] Add `StageExtracting` pipeline stage
- [x] Add server-only fields: `selectedPaper`, `markdownText`
- [x] Add `SetSelectedPaper()`, `SetMarkdown()` accessors
- [x] Add `RecoverToSelection(notice)` for 404 re-pick
- [x] Update `Snapshot()` to exclude server-only fields

#### Next.js — Async Processing + Recovery UI
- [x] Update `POST /api/select` to trigger async `POST /orchestrator/process`
- [x] Display `extracting` stage in progress labels
- [x] Implement 404 re-pick UI:
  - Show notice: "Paper not available on arXiv. Please select another."
  - Re-enable candidate cards
  - Clear error state
- [x] Poll `GET /api/status` during extraction

### Exit Criteria
- [x] Given a valid arXiv paper ID, Go backend fetches HTML and converts to Markdown
- [x] Markdown text is stored server-side and excludes images/math
- [x] Anthropic LLM call with Markdown text returns a valid response
- [x] Switching `llm.provider` in config routes to the correct provider
- [x] 429 from LLM retries automatically per `withRetry` logic
- [x] 404 HTML not found triggers re-pick back to selection (candidates preserved)
- [x] Input/output tokens counted separately in LLM response

---

## Phase 4 — ExplainerAgent + Vault Write

**Goal:** Full single-pass explainer generated from Markdown and saved to Obsidian.

### Tasks

#### ExplainerAgent System Prompt
- [ ] Write system prompt covering:
  - Role: deep AI research explainer for technical practitioners
  - Audience: engineers who know ML basics, not research-level math
  - Goal: understand and re-explain the paper's core intent, not summarize
  - Input: clean Markdown text extracted from arXiv HTML (may reference "figure X" for diagrams)
  - Analogy approach: everyday intuition first, bridge to engineering mental models
  - Math handling: translate simple equations to plain English; summarize complex proofs at intent level only
  - Soft word target: ~2,500 words, may exceed for complex papers
  - Required sections and their purpose

- [ ] Required output sections:
  ```
  ## Problem Statement
  ## Core Idea
  ## Methodology
  ## Key Findings
  ## Limitations
  ## Why It Matters
  ## Analogies & Intuition
  ## Glossary        (top 8–10 terms, prioritized by importance)
  ## Follow-Up Papers (titles + arXiv links where identifiable)
  ```

#### ExplainerAgent Implementation
- [ ] Implement `Generate(ctx, ExplainerInput) (ExplainerOutput, error)`
- [ ] Build `CompletionRequest` from Markdown text + system prompt + paper metadata
- [ ] Parse LLM response into `ExplainerOutput.Sections` map (keyed by section name)
- [ ] Accept `RevisionNote` on subsequent passes — prepend structured feedback to prompt
- [ ] Update pipeline session stage to `generating` with iteration count

#### Follow-Up Paper Link Extraction
- [ ] Implement arXiv ID extractor from reference text (pattern: `\d{4}\.\d{4,5}`)
- [ ] Construct links: `https://arxiv.org/abs/{id}`
- [ ] Fall back to title-only when ID cannot be extracted

#### VaultWriterTool
- [ ] Implement frontmatter assembly:
  ```yaml
  ---
  arxiv_id: "2401.12345"
  title: "Paper Title"
  authors: ["Author One", "Author Two"]
  published: "2026-06-07"
  category: "cs.AI"
  generated_at: "2026-06-07T10:30:00Z"
  review_iterations: 1
  review_passed: true
  tags: [ai, paper, explainer]
  ---
  ```
- [ ] Implement filename generator: `YYYY-MM-DD_arxivID_slug-title.md`
  - Slug: lowercase, spaces → hyphens, strip special characters
  - Max slug length: 60 characters
- [ ] Implement filename sanitizer (alphanumeric, hyphens, underscores, dots only)
- [ ] Implement path validation (all writes must stay within configured vault directory)
- [ ] Create `AI Papers/` subfolder automatically if it doesn't exist
- [ ] Implement atomic write: write to `.tmp` file → `os.Rename()` to final path
- [ ] Call `LogCheckTool.MarkAsProcessed()` immediately after successful rename

#### Orchestrator — Full Pipeline (Phase 3 + Phase 4)
- [ ] Wire (Phase 3 → Phase 4): `PaperContentTool.FetchMarkdown()` → `ExplainerAgent.Generate()` → `VaultWriterTool.Write()` → `LogCheckTool.MarkAsProcessed()`
- [ ] On Phase 4 start: load Markdown from session, pass to ExplainerAgent
- [ ] Update session stage at each step
- [ ] Implement `GET /result/:sessionId` → `{ content, vault_file_path, tokens_used }`

#### Next.js — Progress + Preview UI
- [ ] Implement TanStack Query polling of `GET /api/status` every 2 seconds
- [ ] Display live stage labels:
  - `"Extracting paper..."`
  - `"Generating explainer (pass 1)..."`
  - `"Saving to vault..."`
  - `"Complete"`
- [ ] Build Markdown preview panel (rendered, not raw)
- [ ] Display vault file path on completion
- [ ] Display token usage on completion

### Exit Criteria
- [ ] Selecting a paper produces a complete `.md` file in the configured Obsidian vault
- [ ] All 9 required sections present in generated note
- [ ] Frontmatter is valid YAML
- [ ] Paper ID appears in `processed.json` after successful write
- [ ] Atomic write: no partial files left on disk if process is interrupted
- [ ] Progress UI updates in real time through all stages

---

## Phase 5 — ReviewerAgent + Revision Loop

**Goal:** Critic-generator loop working with configurable iteration cap.

### Tasks

#### ReviewerAgent System Prompt
- [ ] Write system prompt covering:
  - Role: strict quality reviewer, independent from the explainer
  - Rubric to evaluate against:
    1. Is the core author intent clearly captured — not just what they did, but why?
    2. Are analogies accurate and properly layered (everyday intuition → engineering bridge)?
    3. Is math handled appropriately (simple translated, complex summarized at intent)?
    4. Is the glossary prioritized by importance to understanding the paper's contribution?
    5. Is the tone right — respects practitioner intelligence, doesn't over-simplify or over-formalize?
    6. Are follow-up papers relevant and correctly linked?
  - Output format: structured JSON verdict

#### ReviewerAgent Implementation
- [ ] Implement `Review(ctx, ExplainerOutput, iteration int) (ReviewVerdict, error)`
- [ ] Parse LLM response as structured JSON:
  ```json
  {
    "pass": false,
    "score": 0.65,
    "feedback": {
      "core_idea": "Analogy is too abstract — bridge to transformer architecture specifically",
      "glossary": "Term 'contrastive loss' is missing, central to the paper's method"
    }
  }
  ```
- [ ] Update session stage to `reviewing`

#### Revision Loop in Orchestrator
- [ ] Wire ExplainerAgent → ReviewerAgent loop:
  ```
  iteration = 1
  loop:
    ExplainerAgent.Generate(input)  // RevisionNote empty on first pass
    ReviewerAgent.Review(output, iteration)
    if verdict.Pass OR iteration >= config.MaxReviewIterations:
        break
    input.RevisionNote = format(verdict.Feedback)
    iteration++
  ```
- [ ] Format `ReviewVerdict.Feedback` map into structured revision note for ExplainerAgent
- [ ] Update session stage to `revising` with current iteration count
- [ ] On max iterations reached without Pass:
  - Proceed with last ExplainerOutput
  - Set `review_passed: false` in Obsidian frontmatter
  - Set `review_iterations: N` in frontmatter
- [ ] Handle `max_review_iterations: 0` — skip ReviewerAgent entirely, save immediately

#### Next.js — Review/Revision Progress UI
- [ ] Update stage labels for review loop:
  - `"Reviewing (pass 1)..."`
  - `"Revising (pass 2)..."`
  - `"Reviewing (pass 2)..."`
- [ ] Show iteration count and reviewer score in progress display
- [ ] Show `review_passed` status on completion

### Exit Criteria
- [ ] Explainer is reviewed and revised at least once before saving
- [ ] Setting `max_review_iterations: 0` saves immediately without review
- [ ] Setting `max_review_iterations: 2` runs at most 2 review cycles
- [ ] Note frontmatter correctly reflects `review_passed` and `review_iterations`
- [ ] Progress UI shows review and revision stages with iteration counts

---

## Phase 6 — Polish & Hardening

**Goal:** Production-ready local tool with clean error handling, logging, and documentation.

### Tasks

#### Error Handling
- [ ] Implement `recoverable` flag in all error responses:
  ```json
  { "stage": "failed", "error": "HTML fetch timed out after 30s", "recoverable": true }
  ```
- [ ] Implement 404 recovery: `stage: "selection"`, candidates preserved, notice displayed
- [ ] Retry button in UI for recoverable errors
- [ ] Clear non-recoverable error messages (e.g. config errors, permission errors)
- [ ] Verify vault write failure does NOT update `processed.json`
- [ ] Verify log write failure after vault write logs warning but does not surface as error to user

#### Structured Logging
- [ ] Implement `slog` throughout Go backend
- [ ] Log at every pipeline stage transition:
  ```json
  {"level":"INFO","session_id":"abc123","stage":"generating","paper_id":"2401.12345","iteration":1}
  ```
- [ ] Log every LLM call:
  ```json
  {"level":"INFO","provider":"anthropic","model":"claude-sonnet-4-6","tokens_used":4821,"duration_ms":18400}
  ```
- [ ] Log every retry:
  ```json
  {"level":"WARN","component":"arxiv","attempt":2,"error":"429 rate exceeded","backoff_ms":6000}
  ```
- [ ] Log every vault write:
  ```json
  {"level":"INFO","vault_file":"2026-06-07_2401.12345_paper-title.md","duration_ms":12}
  ```

#### Security Hardening
- [ ] Verify Go backend binds to `127.0.0.1` only
- [ ] Verify all file writes validated within configured vault directory
- [ ] Verify filename sanitization strips all non-allowed characters
- [ ] Verify `.env` never exposed to frontend or logs

#### Developer Experience
- [ ] Write `README.md` covering:
  - Prerequisites (Node.js, Go, Obsidian)
  - Setup steps (clone, copy `.env.example` → `.env`, fill keys, set vault path)
  - `make dev` usage
  - Config reference (all `config.yaml` fields explained)
  - LLM provider switching instructions
  - Cost guidance (estimated token usage per paper, per provider)
  - Troubleshooting common errors
- [ ] Validate `make dev` works on macOS and Linux
- [ ] Ensure fresh setup completes in under 10 minutes following README

#### Final Integration Test
- [ ] Full end-to-end run: trigger → select → generate → review → revise → save
- [ ] Verify generated note opens correctly in Obsidian
- [ ] Verify all frontmatter fields render correctly in Obsidian
- [ ] Verify `processed.json` deduplication across multiple runs
- [ ] Verify provider switching works (run once with Anthropic, once with OpenAI)

### Exit Criteria
- [ ] All error paths surface clearly in UI with appropriate `recoverable` flag
- [ ] Structured logs readable and useful for debugging
- [ ] README allows fresh setup in under 10 minutes
- [ ] Full end-to-end run completes without manual intervention
- [ ] No partial or corrupt files left on disk under any failure scenario

---

## Dependency Graph

```
Phase 1 (Scaffolding)
    │
    ▼
Phase 2 (Discovery)
    │
    ▼
Phase 3 (HTML Extraction + LLM)
    │
    ▼
Phase 4 (Explainer + Vault)   ← first fully working end-to-end slice
    │
    ▼
Phase 5 (Reviewer + Loop)
    │
    ▼
Phase 6 (Polish + Hardening)
```

Each phase depends on the previous. Phase 4 is the first point where a complete paper note is produced end-to-end — useful as a milestone demo.

---

## Open Questions (Resolved)

| ID | Question | Resolution |
|---|---|---|
| OQ1 | Obsidian vault path configuration | `config.yaml` default + `.env` override (`OBSIDIAN_VAULT_PATH`). `.env` takes precedence. |
| OQ2 | Abstract display in selection UI | First 300 characters + ellipsis |
| OQ3 | Follow-up paper format | Titles + arXiv links where ID is extractable; title-only fallback |
| OQ4 | Maximum note length | Soft 2,500-word target in system prompt; agent may exceed for complex papers |
