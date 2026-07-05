# Phase 3 — HTML Fetch, Markdown Conversion & LLM Client
## ArXiv AI Paper Explainer Agent

---

## Intent

The user has selected a paper. Now the system must do three things before any intelligence can be applied: **get the paper's full content in a clean, structured text format; convert it to Markdown for easy consumption; and establish a reliable, provider-agnostic channel to the language model.**

Phase 3 is the infrastructure phase of the agent's core capability. No explainer is generated yet — but by the end of this phase, the system can extract the full text of a research paper from arXiv's HTML rendering, convert it to clean Markdown with preserved structure and captions, and send it to any configured text-capable LLM to get a response back.

The central design decision here is **arXiv HTML → Markdown text extraction**: the system fetches each paper's HTML rendering from `arxiv.org/html/{id}`, converts it to Markdown using pure-Go `html-to-markdown`, and sends the text to the LLM. This approach eliminates the need for a vision model, system dependencies (no poppler), and expensive image tokens. We accept the tradeoff of dropping rendered diagrams/figures/equations for now — captions are retained, and alttext recovery is a cheap future seam.

Every architectural decision here serves one long-term goal: **the choice of LLM provider must never be a constraint on what the agent can do — and any text-capable model is sufficient.**

---

# Part 1 — Product Requirements

## 1. Problem Statement

A user has selected a paper. The system now needs to acquire the paper's full content and route it to the configured LLM in a clean, structured text format. Extracting text from PDF directly (e.g. using pdftotext) is fragile on arXiv papers: the two-column layout causes text interleaving, structure is lost, and the reading order becomes unintelligible. However, arXiv publishes a LaTeXML-rendered HTML version alongside every paper — linear reading order, real section headings, tables, captions, and equations as MathML — making HTML extraction far superior to PDF text extraction on this corpus.

Three problems must be solved reliably:

1. **HTML acquisition** — arXiv hosts papers as structured HTML at `arxiv.org/html/{id}`. The system must fetch the correct HTML document, handle redirects transparently, and manage failures gracefully.
2. **HTML-to-Markdown conversion** — the HTML must be converted to clean Markdown while preserving structure, captions, headings, and list hierarchy. Minimal cleanup: strip navigation boilerplate, bibliography, and appendix; remove MathML noise; collapse excess whitespace.
3. **LLM provider abstraction** — the model is fully configurable and may be any text-capable provider or custom endpoint. The system must work identically regardless of which provider is configured.

## 2. Target Users

**Primary:** The developer configuring and running the system locally.
**Indirect:** The end user, who benefits from full-fidelity paper rendering — diagrams and tables are understood by the LLM, not silently discarded.

## 3. User Stories

- As a practitioner, I want the system to automatically fetch and convert the selected paper to text so that I never have to find, extract, or manually process it.
- As a practitioner, I want table captions and figure references in the paper to be understood by the agent so that architectural context and result summaries are reflected in the explanation.
- As a practitioner, I want a clear error if the HTML cannot be fetched so that I know whether to retry or select a different paper.
- As a developer, I want to switch LLM providers by changing one config value so that I'm never locked into a single provider.
- As a developer, I want the system to work with any text-capable model, not just vision models, so I have maximum flexibility in provider choice.

## 4. Functional Requirements

### F1 — Paper Selection
- User selects one paper from the candidate list displayed in Phase 2
- Selection triggers the processing pipeline for that paper
- UI transitions to a processing state with live progress updates
- **Relaxed:** if HTML fetch fails with 404 or empty content, the session returns to the `selection` stage (candidates preserved) with a recoverable notice, allowing the user to pick a different paper without restarting the session

### F2 — HTML Fetch
- System fetches the full HTML of the selected paper from arXiv at `https://arxiv.org/html/{arxiv_id}`
- `arxiv_id` is bare (no version suffix, e.g. `2312.00752`); `http.Client` follows same-host redirects to versioned URL automatically
- HTML bytes held in memory for the duration of the pipeline run
- Fetch timeout: 30 seconds (reuses `agent.request_timeout_seconds` from config)
- Fetch respects arXiv politeness: retry backoff and User-Agent reuse discovery tool's pattern
- Wrapper in `io.LimitReader` prevents OOM on unexpectedly large documents
- Errors: `ErrPaperHTMLNotFound` (404 → recoverable), `ErrPaperHTMLFailed` (network/other), timeout

### F3 — HTML-to-Markdown Conversion
- HTML is converted to clean Markdown using pure-Go `github.com/JohannesKaufmann/html-to-markdown/v2`
- Minimal cleanup applied:
  - Strip LaTeXML-generated navigation, header, and footer boilerplate
  - Trim bibliography and appendix sections (preserve body + captions)
  - **Retain** figure and table captions (context for diagrams)
  - Strip `<math>` nodes (MathML noise; alttext seam noted for future)
  - Collapse excess whitespace and normalize line breaks
- Markdown held in memory as `markdownText` field (mutex-guarded, excluded from `Snapshot()` — large, never sent to frontend)
- On conversion failure: surface clear error with paper ID

### F4 — LLM Client Interface
- All LLM calls throughout the system go through a single `LLMClient` interface
- Interface accepts: system prompt, user prompt, document text (Markdown), max tokens, temperature
- Interface returns: generated text content, token usage count (input + output separately)
- Concrete implementations exist for: Anthropic Claude, OpenAI, Google Gemini
- Custom/other providers: implement `LLMClient` interface (~50 lines)
- Active provider selected at startup from config — no runtime switching
- **Simplified:** no vision validation needed; any text model is valid

### F5 — LLM Provider Configuration
- Provider, model, API key, max tokens, temperature, timeout, and optional base URL configurable
- Switching providers requires only changing `llm.provider` and `llm.model` in config (+ API key in `.env`)
- Adding a new provider requires only implementing the `LLMClient` interface

### F6 — LLM Error Handling
- `429 rate limited` → retry with exponential backoff, max 3 attempts
- `400 bad request` → surface as configuration error (wrong model name, context too large)
- `500/503 provider error` → retry once, then surface as provider error
- Timeout → surface with duration and paper ID
- All errors set session to `failed` with `recoverable` flag

### F7 — Pipeline Status (Phase 3 stages)
- New stage label surfaced to UI:
  - `"Extracting paper text..."` (`extracting`) — replaces old `fetching_pdf` + `rendering_pdf`
- `GET /status/:sessionId` updated to reflect new stages

## 5. Non-Functional Requirements

- **Correctness** — extracted Markdown has correct reading order (no column interleave), preserves structure, and includes captions
- **Provider parity** — the `LLMClient` interface must be implementable by any text-capable provider without leaking provider-specific concepts into calling code
- **Failure recovery** — on HTML 404/empty, session returns to selection stage (not failure) so user can pick another paper
- **Configurability** — HTML base URL, content size cap, max tokens, temperature, provider, model — all configurable without code changes
- **No system dependencies** — pure-Go HTML-to-Markdown conversion; no poppler, no Python, no CGO

## 6. Success Metrics

- HTML fetches successfully for any valid arXiv paper ID within 30 seconds
- Markdown conversion produces readable text with correct heading hierarchy and captions preserved
- LLM call with Markdown text returns a valid response for all three supported providers (Anthropic, OpenAI, Gemini)
- Switching `llm.provider` in config routes correctly to the new provider without code changes
- HTML 404 gracefully transitions session back to selection stage with recoverable notice
- LLM 429 errors retry automatically and succeed in the majority of cases
- No poppler, no vision model requirement, no Python dependencies in the build

## 7. Scope & Non-Goals

**In scope:**
- Paper selection UI (select button → triggers pipeline)
- HTML fetch from arXiv (`PaperContentTool.FetchMarkdown`)
- HTML-to-Markdown conversion with minimal cleanup (`PaperContentTool`)
- `LLMClient` interface definition (text-only)
- Anthropic, OpenAI, Gemini concrete implementations (text-only clients)
- Config-driven provider selection and HTML base URL
- `POST /process` endpoint in Go backend
- Error handling for HTML fetch, conversion, and LLM failures
- Recovery path: on HTML 404/empty, return to selection stage (no restart)

**Out of scope:**
- Explainer generation (Phase 4)
- Any prompt engineering (Phase 4)
- Reviewer agent (Phase 5)
- Vault writing (Phase 4)
- Rendered equations / diagrams / figure images (deferred — captions retained)
- PDF extraction (not used — HTML is the only path)
- Vision model requirement (any text model works)

## 8. Open Questions

None for this phase. All requirements are fully defined.

---

# Part 2 — Architecture

## Intent

Two components are introduced in this phase: `PaperContentTool` and `LLMClient`. They are deliberately decoupled — the content fetcher knows nothing about LLMs, and the LLM client knows nothing about arXiv or HTML. The Orchestrator connects them in sequence. This separation means each can be replaced or tested independently, and the LLM client is reused identically by both `ExplainerAgent` and `ReviewerAgent` in later phases.

The `PaperContentTool` is the key new component in this phase. It combines HTML fetching, conversion, and cleanup in one responsible unit. Unlike the old PDF-based approach, it requires no system dependencies — the pure-Go `html-to-markdown` library handles all conversion logic. This eliminates build complexity and makes the system portable across platforms without special install steps.

---

## 1. System Overview

```
User clicks "Select" on a paper card
    │
    ▼
Next.js UI → POST /api/select { session_id, paper_id }
    │
    ▼
Next.js API route → POST localhost:8080/process { session_id, paper_id }
    │
    ▼
Go Orchestrator.HandleProcess()
    │
    ├── Validates session exists and is in "selection" stage
    ├── Sets session.SelectedPaper
    ├── Updates session { stage: "extracting" }
    ├── Spawns async goroutine for pipeline
    └── Returns { session_id } immediately (pipeline runs async)
    │
    ▼ (async goroutine)
PaperContentTool.FetchMarkdown(ctx, arxivID)
    │
    ├── GET https://arxiv.org/html/{arxiv_id} (follows same-host redirect to versioned URL)
    ├── Wrap body in io.LimitReader (prevent OOM)
    ├── Parse HTML; convert to Markdown with minimal cleanup
    │   ├── Strip navigation/header/footer boilerplate
    │   ├── Trim bibliography and appendix
    │   ├── Retain captions (figures, tables)
    │   ├── Strip <math> nodes (MathML noise)
    │   └── Collapse whitespace
    │
    ├── On 404: return ErrPaperHTMLNotFound (recoverable)
    │   → session transitions back to "selection" stage
    │   → frontend clears selectedId, re-enables candidate list
    │   → user picks another paper without restart
    │
    └── Return markdownText or error
    │
    ▼
[Phase 4 picks up here: ExplainerAgent.Generate(markdownText)]
    │
    ▼
LLMClient.Complete(CompletionRequest{ DocumentText: markdownText, prompts })
    │
    └── Routes to configured provider:
          anthropic → AnthropicClient
          openai    → OpenAIClient
          gemini    → GeminiClient
          custom    → implement LLMClient interface


Progress polling (parallel, established in Phase 2):
Next.js polls GET /api/status → { stage: "extracting", error }
```

---

## 2. Component Breakdown

### 2.1 Next.js — Paper Selection UI

**Intent:** The selection action is the user's commitment to process a specific paper. The UI must make the action clear, confirm it was received, and immediately communicate that work has begun.

**Why async pipeline with immediate response:** PDF download + rendering + LLM generation can take 30–120 seconds. Holding the HTTP connection open for that duration is fragile. Returning `session_id` immediately and polling for status is more resilient and gives the user live feedback.

**Responsibilities:**
- Render "Select" button on each `<PaperCard />`
- On click: call `POST /api/select { session_id, paper_id }`
- Disable all other "Select" buttons once one is chosen
- Transition UI to processing state with live stage label
- Continue TanStack Query polling (already running from Phase 2)

**Updated component structure:**
```
/app/page.tsx
  └── <DiscoveryPanel>
        ├── <TriggerButton />
        ├── <ProgressIndicator stage={status.stage} iteration={status.iteration} />
        ├── <CandidateList>
        │     └── <PaperCard onSelect={handleSelect} disabled={!!selectedId} />
        └── <ErrorBanner error={status.error} recoverable={status.recoverable} />
```

**New API route:**
```typescript
// /app/api/select/route.ts
POST /api/select
  Body: { session_id: string, paper_id: string }
  → POST http://localhost:8080/process
  ← { session_id: string }
```

**Stage labels (Phase 3 additions):**
```typescript
const stageLabels: Record<PipelineStage, string> = {
  discovery:     "Connecting to arXiv...",
  selection:     "Select a paper to continue",
  extracting:    "Extracting paper text...",
  generating:    "Generating explainer (pass {iteration})...",
  reviewing:     "Reviewing (pass {iteration})...",
  revising:      "Revising (pass {iteration})...",
  writing:       "Saving to vault...",
  complete:      "Complete",
  failed:        "Something went wrong",
}
```

**Dependencies:** TanStack Query 5.101.0, `/lib/types.ts`

---

### 2.2 Next.js API Route — Select Proxy

**Intent:** Thin proxy. Forwards the selection to Go backend and returns the session ID. No logic here.

```typescript
// /app/api/select/route.ts
export async function POST(req: Request) {
  const { session_id, paper_id } = await req.json()
  const res = await fetch(`http://localhost:8080/process`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ session_id, paper_id }),
  })
  return Response.json(await res.json())
}
```

---

### 2.3 Go Orchestrator — Process Endpoint

**Intent:** `POST /process` is the handoff point between user selection and agent execution. It validates the session state, transitions the pipeline into async execution, and returns immediately.

```go
func (o *Orchestrator) HandleProcess(w http.ResponseWriter, r *http.Request) {
    var req ProcessRequest
    json.NewDecoder(r.Body).Decode(&req)

    session, ok := o.getSession(req.SessionID)
    if !ok {
        http.Error(w, "session not found", http.StatusNotFound)
        return
    }
    snap := session.Snapshot()
    if snap.Stage != models.StageSelection {
        http.Error(w, "session not in selection stage", http.StatusBadRequest)
        return
    }

    // Find selected paper in candidates
    var selected *models.Paper
    for _, p := range snap.Candidates {
        if p.ID == req.PaperID {
            selected = &p
            break
        }
    }
    if selected == nil {
        http.Error(w, "paper not found in session candidates", http.StatusBadRequest)
        return
    }

    session.SetSelectedPaper(selected)
    session.SetStage(models.StageExtracting)

    // Return immediately — pipeline runs async
    json.NewEncoder(w).Encode(ProcessResponse{SessionID: session.SessionID})

    go o.runPipeline(context.Background(), session)
}

func (o *Orchestrator) runPipeline(ctx context.Context, session *models.PipelineSession) {
    // Phase 3: HTML fetch + Markdown conversion
    snap := session.Snapshot()
    markdown, err := o.paperContentTool.FetchMarkdown(ctx, snap.SelectedPaper.ID)
    if err != nil {
        // On 404, recover by returning to selection stage (candidates already held)
        if errors.Is(err, tools.ErrPaperHTMLNotFound) {
            session.SetStage(models.StageSelection)
            session.SetNotice("Paper HTML not available. Select another paper.")
            return
        }
        o.failSession(session, err, true)
        return
    }
    
    session.SetMarkdown(markdown)

    // Phase 4+: ExplainerAgent called here
}
```

**Important:** `SetStage()` and `SetMarkdown()` are accessor methods that hold the mutex lock. Never mutate `session.Stage` or `session.markdownText` directly — the mutex guards concurrent access from the status-poll handler.

**Dependencies:** PaperContentTool, `models`, `config`

---

### 2.4 PaperContentTool

**Intent:** This tool owns one responsibility: given an arXiv ID, return clean Markdown text. It handles HTML fetching, conversion, and minimal cleanup. Combines the old PDF fetch and render responsibilities into one unit. Nothing outside this tool needs to know how arXiv serves HTML or how conversion works.

**Why pure-Go `html-to-markdown`:**
- Pure Go library — zero system dependencies, no poppler, no Python
- Fast in-memory conversion — no subprocess, no temp files
- Integrates seamlessly with the discovery tool's existing retry/backoff pattern

**Interface:**
```go
// /internal/tools/papercontent.go

type PaperContentTool struct {
    cfg        *config.AgentConfig
    httpClient *http.Client  // configured with request timeout
}

func NewPaperContentTool(cfg *config.AgentConfig) *PaperContentTool

func (t *PaperContentTool) FetchMarkdown(ctx context.Context, arxivID string) (string, error)
// Returns clean Markdown text or an error
```

**Implementation outline:**
```go
func (t *PaperContentTool) FetchMarkdown(ctx context.Context, arxivID string) (string, error) {
    // 1. Fetch HTML from arxiv.org/html/{id} (bare id, no version)
    htmlURL := t.cfg.ArxivHTMLBaseURL + "/" + arxivID
    req, _ := http.NewRequestWithContext(ctx, "GET", htmlURL, nil)
    req.Header.Set("User-Agent", t.cfg.UserAgent)
    
    // Wrap response in io.LimitReader to prevent OOM on large documents
    resp, err := t.httpClient.Do(req)
    if resp.StatusCode == http.StatusNotFound {
        return "", ErrPaperHTMLNotFound  // recoverable — user can re-pick
    }
    if err != nil || resp.StatusCode != http.StatusOK {
        return "", ErrPaperHTMLFailed
    }
    defer resp.Body.Close()
    
    limitedBody := io.LimitReader(resp.Body, 50*1024*1024)  // 50MB cap
    htmlBytes, _ := io.ReadAll(limitedBody)
    
    // 2. Convert HTML to Markdown
    converter := md.NewConverter("", true, &md.Options{...})
    markdown, err := converter.ConvertString(string(htmlBytes))
    if err != nil {
        return "", ErrPaperHTMLFailed
    }
    
    // 3. Minimal cleanup: strip nav, trim appendix, remove math nodes, collapse whitespace
    cleaned := t.cleanup(markdown)
    return cleaned, nil
}

// cleanup strips boilerplate and normalizes whitespace
func (t *PaperContentTool) cleanup(markdown string) string {
    // Remove header/footer/nav sections (LaTeXML patterns)
    // Trim bibliography and appendix if present
    // Strip MathML noise (alttext seam retained for future)
    // Collapse multiple newlines
    // ... implementation details ...
    return cleaned
}
```

**Error types:**
```go
var (
    ErrPaperHTMLNotFound = errors.New("paper HTML not found on arXiv (404)")
    ErrPaperHTMLFailed   = errors.New("failed to fetch or convert paper HTML")
    ErrPaperHTMLTimeout  = errors.New("HTML fetch timed out")
)
```

**Reused from DiscoveryTool:**
- User-Agent politeness header
- Retry backoff pattern (same domain, same politeness rules)
- HTTP client timeout configuration

**Dependencies:** `net/http`, `io`, `github.com/JohannesKaufmann/html-to-markdown/v2`, `config`

---

### 2.5 LLMClient Interface

**Intent:** The `LLMClient` interface is the most important abstraction in the system. It decouples every agent from any specific LLM provider. The interface accepts plain Markdown text — not images — making it compatible with any text-capable model. No vision requirement anywhere.

**Why text not images in the interface:** Text is a universal input supported by every language model. Standardising on text in the interface means the LLM client implementations are simpler and more consistent, token costs are lower, and custom/unknown models can be supported without special cases.

**Interface:**
```go
// /internal/llm/client.go

type LLMClient interface {
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

type CompletionRequest struct {
    SystemPrompt string   // system instruction
    UserPrompt   string   // user message (may reference DocumentText)
    DocumentText string   // paper Markdown (extracted from arXiv HTML)
    MaxTokens    int
    Temperature  float32
}

type CompletionResponse struct {
    Content      string
    InputTokens  int
    OutputTokens int
}
```

**Provider selector (config-driven):**
```go
func NewLLMClient(cfg *config.LLMConfig) (LLMClient, error) {
    switch cfg.Provider {
    case "anthropic":
        return NewAnthropicClient(cfg)
    case "openai":
        return NewOpenAIClient(cfg)
    case "gemini":
        return NewGeminiClient(cfg)
    default:
        // Custom or unknown provider — return error with instructions
        return nil, fmt.Errorf(
            "unknown LLM provider: %q — implement the LLMClient interface for custom providers",
            cfg.Provider,
        )
    }
}
```

**No vision validation needed:** Any text-capable model works. No startup checks required.

**Shared retry logic:**
```go
// Retry on 429: backoff 5s → 10s → 20s, max 3 attempts
// Retry on 503: once after 5s
// Surface ErrLLMBadRequest immediately on 400 (no retry)
```

**Error types:**
```go
var (
    ErrLLMRateLimit   = errors.New("LLM provider rate limit exceeded")
    ErrLLMBadRequest  = errors.New("LLM bad request — check model name and config")
    ErrLLMUnavailable = errors.New("LLM provider unavailable")
    ErrLLMTimeout     = errors.New("LLM request timed out")
)
```

**Dependencies:** `config`

---

### 2.6 Anthropic Claude Implementation

**Intent:** Send Markdown text as a text block in the messages array. No vision required; any Anthropic text model works.

```go
// /internal/llm/anthropic.go

func (c *AnthropicClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
    var contentBlocks []anthropic.ContentBlock

    // Add document text as context
    if req.DocumentText != "" {
        contentBlocks = append(contentBlocks, anthropic.TextBlock{
            Text: "Paper content:\n\n" + req.DocumentText,
        })
    }

    // Add user prompt
    contentBlocks = append(contentBlocks, anthropic.TextBlock{Text: req.UserPrompt})

    resp, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
        Model:     anthropic.Model(c.config.Model),
        MaxTokens: int64(req.MaxTokens),
        System:    []anthropic.TextBlockParam{{Text: req.SystemPrompt}},
        Messages: []anthropic.MessageParam{
            {Role: "user", Content: contentBlocks},
        },
    })
    // handle errors, map to shared error types

    return CompletionResponse{
        Content:      resp.Content[0].Text,
        InputTokens:  int(resp.Usage.InputTokens),
        OutputTokens: int(resp.Usage.OutputTokens),
    }, nil
}
```

**Dependencies:** `github.com/anthropics/anthropic-sdk-go`

---

### 2.7 OpenAI Implementation

**Intent:** Send Markdown text as a text content part in the user message. Any OpenAI text model works.

```go
// /internal/llm/openai.go

func (c *OpenAIClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
    var messages []openai.ChatCompletionMessageParam

    messages = append(messages, openai.SystemMessage(req.SystemPrompt))

    // Build user message with document text
    var userContent []openai.ChatCompletionContentPartUnionParam
    if req.DocumentText != "" {
        userContent = append(userContent, openai.TextContentPart(
            "Paper content:\n\n" + req.DocumentText,
        ))
    }
    userContent = append(userContent, openai.TextContentPart(req.UserPrompt))
    messages = append(messages, openai.UserMessageParts(userContent...))

    resp, err := c.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
        Model:       openai.ChatModel(c.config.Model),
        MaxTokens:   openai.Int(int64(req.MaxTokens)),
        Temperature: openai.Float(float64(req.Temperature)),
        Messages:    messages,
    })
    // handle errors, map to shared error types

    return CompletionResponse{
        Content:      resp.Choices[0].Message.Content,
        InputTokens:  int(resp.Usage.PromptTokens),
        OutputTokens: int(resp.Usage.CompletionTokens),
    }, nil
}
```

**Dependencies:** `github.com/openai/openai-go`

---

### 2.8 Google Gemini Implementation

**Intent:** Send Markdown text as a text part in the content parts array. Any Gemini text model works.

```go
// /internal/llm/gemini.go

func (c *GeminiClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
    model := c.client.GenerativeModel(c.config.Model)
    model.SystemInstruction = &genai.Content{
        Parts: []genai.Part{genai.Text(req.SystemPrompt)},
    }
    model.SetTemperature(req.Temperature)
    model.SetMaxOutputTokens(int32(req.MaxTokens))

    var parts []genai.Part
    // Add document text as context
    if req.DocumentText != "" {
        parts = append(parts, genai.Text("Paper content:\n\n"+req.DocumentText))
    }
    parts = append(parts, genai.Text(req.UserPrompt))

    resp, err := model.GenerateContent(ctx, parts...)
    // handle errors, map to shared error types

    return CompletionResponse{
        Content:      fmt.Sprintf("%v", resp.Candidates[0].Content.Parts[0]),
        InputTokens:  int(resp.UsageMetadata.PromptTokenCount),
        OutputTokens: int(resp.UsageMetadata.CandidatesTokenCount),
    }, nil
}
```

**Dependencies:** `google.golang.org/genai`

---

## 3. Data Model

Phase 3 adds one mutex-guarded field to `PipelineSession` and introduces new config fields:

```go
// PipelineSession additions (see session.go for full structure)
type PipelineSession struct {
    // ... existing fields from Phase 2 ...
    mu              sync.RWMutex
    markdownText    string   // mutex-guarded; EXCLUDED from Snapshot()
    selectedPaper   *Paper   // holds chosen paper; accessed via SetSelectedPaper()
}

// Accessor methods (MUST be used, never mutate directly)
func (s *PipelineSession) SetStage(stage PipelineStage)
func (s *PipelineSession) SetMarkdown(markdown string)
func (s *PipelineSession) SetSelectedPaper(paper *Paper)
func (s *PipelineSession) Snapshot() SessionSnapshot  // excludes markdownText

// New pipeline stage
const StageExtracting PipelineStage = "extracting"
```

**Why excluded from Snapshot():** The markdown text is large (~50KB–500KB per paper) and transient. It is never sent to the frontend — only held server-side for the pipeline. Excluding it from Snapshot() prevents accidental serialization and keeps the status endpoint cheap.

```go
// New config fields (Phase 3 additions to AgentConfig)
type AgentConfig struct {
    // ... existing fields from Phase 2 ...
    ArxivHTMLBaseURL  string `yaml:"arxiv_html_base_url"`  // default https://arxiv.org/html
    ContentSizeCap    int    `yaml:"content_size_cap"`     // bytes; default 50MB (prevents OOM)
}
```

```yaml
# config.yaml additions
agent:
  # ... existing Phase 2 fields ...
  arxiv_html_base_url: https://arxiv.org/html
  content_size_cap: 52428800   # 50MB in bytes; prevents OOM on unusual documents
```

**Memory note:** For a typical 12-page AI paper, the Markdown text is approximately 50–200KB. Total memory per pipeline run: ~200KB–500KB (vs 6–12MB with page images). Markdown is discarded after pipeline completes.

---

## 4. Data Flow

### Selection, Fetch & Conversion Flow

```
1. User clicks "Select" on paper card
      │
      ▼
2. Next.js → POST /api/select { session_id, paper_id }
      │
      ▼
3. Go Orchestrator.HandleProcess()
      ├── Validate session in "selection" stage
      ├── Find paper in session.Candidates
      ├── Set session.SelectedPaper
      ├── Set session.Stage = "extracting"
      ├── Return { session_id } immediately
      └── Spawn goroutine: runPipeline(ctx, session)
      │
      ▼ (async goroutine)
4. PaperContentTool.FetchMarkdown(ctx, arxiv_id)
      ├── GET https://arxiv.org/html/2401.12345
      │     (follows same-host redirect to https://arxiv.org/html/2401.12345v2)
      ├── Wrap body in io.LimitReader (50MB cap)
      ├── Read HTML bytes (~100KB–500KB)
      │
      ├── On 404: return ErrPaperHTMLNotFound
      │   ↓
      │   session.SetStage("selection")
      │   session.SetNotice("Paper HTML not available. Select another paper.")
      │   (candidates preserved, selectedId cleared on frontend)
      │   ↓
      │   Return to user — no restart needed
      │
      ├── Convert HTML to Markdown using html-to-markdown/v2
      ├── Minimal cleanup:
      │   ├── Strip LaTeXML nav/header/footer
      │   ├── Trim bibliography and appendix
      │   ├── Retain figure/table captions
      │   ├── Strip <math> nodes
      │   └── Collapse whitespace
      │
      └── session.SetMarkdown(markdown)  // mutex-guarded
      │
      ▼
5. [Phase 4: ExplainerAgent.Generate({ markdownText, PaperMeta }) called here]
```

### LLM Call Flow (wired in Phase 3, invoked in Phase 4)

```
ExplainerAgent.Generate(ExplainerInput{ markdownText, PaperMeta, RevisionNote })
      │
      ▼
Build CompletionRequest {
    SystemPrompt:  "...",
    UserPrompt:    "Paper: Title...\nPlease generate the explainer.",
    DocumentText:  session.markdownText,  // Markdown string
    MaxTokens:     config.LLM.MaxTokens,
    Temperature:   config.LLM.Temperature,
}
      │
      ▼
LLMClient.Complete(ctx, request)
      │
      ▼
Provider router → AnthropicClient | OpenAIClient | GeminiClient
      │
      ▼
Markdown text sent as text input
      │
      ▼
CompletionResponse { Content: "...", InputTokens: 2400, OutputTokens: 3200 }
```

### Error Flow — HTML Fetch Fails

```
PaperContentTool.FetchMarkdown(ctx, arxiv_id)
      │
      ├─ 404 Not Found: ErrPaperHTMLNotFound (recoverable)
      │  ↓
      │  runPipeline() calls session.SetStage(StageSelection)
      │  ↓
      │  Frontend shows recoverable notice
      │  ↓
      │  User picks different paper (same session)
      │
      ├─ Network error / timeout: ErrPaperHTMLFailed (recoverable)
      │  ↓
      │  failSession(session, err, true)
      │  ↓
      │  Frontend shows "Try again" option
      │
      └─ Size exceeded: ErrPaperHTMLFailed (recoverable)
         ↓
         failSession(session, err, true)
         ↓
         Frontend shows "Try another paper" option
```

---

## 5. Tech Stack

**New system dependencies (Phase 3):**

None. This phase requires zero system dependencies — all work is done in pure Go.

**New Go dependencies (Phase 3):**

| Package | Provider | Why |
|---|---|---|
| `github.com/JohannesKaufmann/html-to-markdown/v2` | Johannes Kaufmann | Pure-Go HTML-to-Markdown conversion. No dependencies, works cross-platform. |
| `github.com/anthropics/anthropic-sdk-go` | Anthropic | Official Go SDK. Text messages in messages array. |
| `github.com/openai/openai-go` | OpenAI | Official Go SDK. Text content parts. |
| `google.golang.org/genai` | Google Gemini | Official Go SDK. Native text part support. |

All three provider SDKs installed regardless of active provider. Unused clients add minimal binary size overhead (~5–10MB).

**Config additions:**
```yaml
agent:
  arxiv_html_base_url: https://arxiv.org/html
  content_size_cap: 52428800   # 50MB in bytes
```

---

## 6. Integration Points

### arXiv HTML Endpoint

**URL pattern:** `https://arxiv.org/html/{arxiv_id}` (bare id, no version suffix)
**Auth:** None
**Redirects:** arXiv redirects to versioned URL (e.g. `/html/2401.12345v2`) — `http.Client` follows automatically
**Timeout:** 30 seconds (reuses `agent.request_timeout_seconds` from config)
**Rate limit:** Respect `agent.min_request_interval_seconds` delay (same domain as discovery API)
**Response:** HTML document (~100KB–500KB typical). Size capped by `agent.content_size_cap` to prevent OOM.
**Availability:** Near-100% for recent papers in cs.AI and related categories. Older papers may lack HTML rendering (404 → recoverable fail).

---

### Anthropic Messages API

**Endpoint:** `https://api.anthropic.com/v1/messages`
**Auth:** `x-api-key` header
**Text input:** Plain text in `messages[]` content array (TextBlock)
**Token cost note:** Markdown text is typically 2,000–10,000 input tokens depending on paper length. Much cheaper than vision paths.

---

### OpenAI Chat Completions API

**Endpoint:** `https://api.openai.com/v1/chat/completions`
**Auth:** `Authorization: Bearer` header
**Text input:** Plain text content parts in user message
**Any model works:** No vision requirement. Text-capable models like `gpt-4`, `gpt-4-turbo`, or `gpt-3.5-turbo` all work.

---

### Google Gemini API

**Endpoint:** `https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent`
**Auth:** `Authorization: Bearer` header
**Text input:** Plain text parts in content array
**Advantage:** Largest context window — useful choice for very long papers (60+ pages).

---

## 7. Cross-Cutting Concerns

### Error Handling

| Failure | Behaviour | Recoverable |
|---|---|---|
| HTML 404 | "Paper HTML not available on arXiv. Select a different paper." | Yes — return to selection stage |
| HTML timeout (30s) | "HTML fetch timed out. Try again." | Yes |
| HTML fetch error | "Failed to fetch paper HTML: [reason]. Try again." | Yes |
| Markdown conversion failed | "Failed to convert paper to text. Try again." | Yes |
| Content size exceeded (>50MB) | "Paper HTML is too large. Select a shorter paper." | Yes — return to selection stage |
| LLM 429 after retries | "LLM rate limit exceeded. Try again shortly." | Yes |
| LLM 400 (bad model) | "LLM config error: model '[model]' rejected. Check llm.model in config." | No |
| LLM 400 (context too large) | "Paper is too long for model '[model]'. Try a shorter paper or switch to Gemini." | No |
| LLM 500/503 | "LLM provider unavailable. Try again shortly." | Yes |
| LLM timeout | "LLM request timed out after [N]s. Try again." | Yes |

### Observability

```json
{"level":"INFO","msg":"html fetch started","session_id":"abc123","paper_id":"2401.12345"}
{"level":"INFO","msg":"html fetch complete","session_id":"abc123","paper_id":"2401.12345","html_bytes":234567,"duration_ms":1200}
{"level":"INFO","msg":"markdown conversion started","session_id":"abc123","paper_id":"2401.12345"}
{"level":"INFO","msg":"markdown conversion complete","session_id":"abc123","paper_id":"2401.12345","markdown_bytes":45678,"duration_ms":350}
{"level":"INFO","msg":"llm call started","session_id":"abc123","provider":"anthropic","model":"claude-sonnet-4-6"}
{"level":"INFO","msg":"llm call complete","session_id":"abc123","input_tokens":4200,"output_tokens":3200,"duration_ms":8400}
{"level":"WARN","msg":"llm rate limited","session_id":"abc123","provider":"anthropic","attempt":1,"backoff_ms":5000}
{"level":"WARN","msg":"paper html not found","session_id":"abc123","paper_id":"2401.12345"}
```

### Security
- HTML bytes held in memory only — never written to permanent storage (no temp files)
- Markdown text held in memory only — never persisted (excluded from Snapshot)
- Size limit enforced by `io.LimitReader` to prevent OOM attacks
- API keys never logged, never passed to frontend
- No shell execution — pure Go conversion, no subprocess risk

### Token Cost Awareness
A typical 12-page AI paper in Markdown format sends approximately:
- ~2,000–8,000 input tokens (text-based)
- Plus system prompt (~500 tokens) and user prompt (~100 tokens)
- Total: ~2,600–8,600 tokens vs 10,000–18,000+ with vision

Text extraction is 2–3x cheaper than page-as-image. This enables more iterations and longer papers within budget.

---

## 8. Risks & Tradeoffs

| ID | Risk/Tradeoff | Severity | Mitigation |
|---|---|---|---|
| R1 | Very old papers may lack HTML rendering on arXiv | Low | Discovery sorts newest-first (Phase 2), so 404 hits are rare in typical usage. 404 is recoverable — user picks another paper without restart. |
| R2 | LaTeXML HTML rendering artifacts (minor formatting noise) | Low | Minimal cleanup strategy + LLM tolerance. Users see captions (context preserved), not images. |
| R3 | Provider SDK API changes break concrete implementations | Low | Each provider isolated behind interface. Breaking SDK change affects one file. Versions pinned in `go.mod`. |
| R4 | Unusual documents >50MB HTML size | Low | Capped by `io.LimitReader(content_size_cap)`. Oversized fetch fails gracefully with recoverable error. |
| T1 | Diagrams, figures, rendered equations are dropped (for now) | Accepted | Captions preserved, LaTeX in `alttext` forms a seam for future recovery. Text-only approach eliminates vision model requirement and system dependencies. Lower ceiling on figure-heavy papers, but rare in typical cs.AI workflows. |
| T2 | Depends on arXiv HTML endpoint availability | Accepted | Same domain already trusted for discovery (Phase 2). Retry/backoff reused from discovery tool. 404 is recoverable (user re-picks). |
| T3 | All three provider SDKs installed regardless of active provider | Accepted | Binary size overhead minimal (~5–10MB). Any provider is one config change away. |
| T4 | No streaming LLM responses | Accepted | Streaming adds significant UI complexity. Progress communicated via stage polling. |

---

## Exit Criteria

All of the following must be true before Phase 4 begins.
Legend: [x] verified · [~] verified via httptest, live-key smoke test pending (no `.env` keys in the automated run).

- [x] No poppler or system dependency validation at startup — pure Go only _(verified: `CGO_ENABLED=0 go build ./...` succeeds; grep finds no poppler/pdftoppm)_
- [x] No vision model validation needed — any text-capable model works _(no vision code; text-only `CompletionRequest.DocumentText`)_
- [x] Clicking "Select" on a paper card transitions UI to processing state _(`discovery-panel.tsx`; polling re-armed on select)_
- [x] HTML fetches successfully for any valid arXiv paper ID within 30 seconds _(live: 1706.03762 fetched in 303ms; timeout from `request_timeout_seconds`)_
- [x] HTML fetch handles arXiv redirects to versioned URLs automatically _(default `CheckRedirect`; live redirect to v7 followed)_
- [x] HTML timeout surfaces a clear, recoverable error in the UI _(`ErrPaperHTMLTimeout` → recoverable message via `describeError`)_
- [x] HTML 404 transitions session back to selection stage (candidates preserved, selectedId cleared) _(`RecoverToSelection` + frontend re-pick; integration + unit tests)_
- [x] HTML-to-Markdown conversion produces readable text with heading hierarchy intact _(live test: `#`/`##` headings present)_
- [x] Captions (figures, tables) are preserved in the Markdown _(unit test asserts `figcaption` kept)_
- [x] Progress UI shows `"Extracting paper text..."` stage during extraction _(`progress-indicator.tsx`)_
- [~] `LLMClient.Complete()` with Markdown text returns a valid response for Anthropic _(httptest happy-path: content + tokens parsed)_
- [~] `LLMClient.Complete()` with Markdown text returns a valid response for OpenAI _(httptest happy-path: content + tokens parsed)_
- [~] `LLMClient.Complete()` with Markdown text returns a valid response for Gemini _(httptest happy-path: content + tokens parsed)_
- [x] Switching `llm.provider` in config routes to the correct provider without code changes _(`NewLLMClient` config switch)_
- [x] LLM 429 retries automatically (max 3 attempts with backoff) before surfacing error _(`retry_test.go`)_
- [~] LLM 400 surfaces as a config error naming the configured model _(sentinel `ErrLLMBadRequest` mapped; user-facing model-naming surfaces when the LLM is invoked in Phase 4)_
- [x] HTML size, markdown size, extraction duration logged for every run; LLM token counts logged when invoked (Phase 4) _(`papercontent.go` structured logs)_
- [x] Input and output tokens returned separately in `CompletionResponse` for all providers _(`InputTokens`/`OutputTokens`; httptest-verified per provider)_
- [x] Markdown text excluded from `Snapshot()` — never sent to frontend _(`session_test.go`)_
- [x] Session recovers to selection stage on HTML 404 — user can re-pick without restarting session _(integration test)_
- [x] No temp files left on disk after any run (success or failure) _(HTML held in memory only; no filesystem writes in the tool)_
