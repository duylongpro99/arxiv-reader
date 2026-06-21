# Phase 3 — PDF Fetch, Page Rendering & LLM Client
## ArXiv AI Paper Explainer Agent

---

## Intent

The user has selected a paper. Now the system must do three things before any intelligence can be applied: **get the paper's full content, render it into a format any vision-capable model can read, and establish a reliable, provider-agnostic channel to the language model.**

Phase 3 is the infrastructure phase of the agent's core capability. No explainer is generated yet — but by the end of this phase, the system can put every page of a research paper — including diagrams, tables, figures, and equations — in front of any configured vision-capable LLM and get a response back.

The central design decision here is **page-as-image rendering**: every PDF page is converted to a PNG image using `poppler`'s `pdftoppm`, then sent to the LLM as vision input. This is the only approach that guarantees no visual content is lost regardless of what the paper contains — architecture diagrams, performance tables, loss curves, and image-embedded equations are all preserved exactly as a human reader would see them.

Every architectural decision here serves one long-term goal: **the choice of LLM provider must never be a constraint on what the agent can do — but the model must be vision-capable.**

---

# Part 1 — Product Requirements

## 1. Problem Statement

A user has selected a paper. The system now needs to acquire the paper's full content and route it to the configured LLM in a form that preserves everything — not just extractable text. AI research papers rely heavily on visual content: architecture diagrams, comparison tables, mathematical notation, and figure-referenced explanations. A text-only extraction approach silently discards this content, producing a lower-quality explainer without the system or user knowing why.

Three problems must be solved reliably:

1. **PDF acquisition** — arXiv hosts papers as PDFs. The system must download the correct file, handle redirects, and manage failures gracefully.
2. **PDF rendering** — every page must be converted to a PNG image that a vision LLM can read, at a resolution that preserves legible text and diagram detail.
3. **LLM provider abstraction** — the model is fully configurable and may be any vision-capable provider or custom endpoint. The system must work identically regardless of which provider is configured.

## 2. Target Users

**Primary:** The developer configuring and running the system locally.
**Indirect:** The end user, who benefits from full-fidelity paper rendering — diagrams and tables are understood by the LLM, not silently discarded.

## 3. User Stories

- As a practitioner, I want the system to automatically download and process the selected paper so that I never have to find, convert, or upload it manually.
- As a practitioner, I want diagrams and tables in the paper to be understood by the agent so that architectural figures and result tables are reflected in the explanation, not missing from it.
- As a practitioner, I want a clear error if the PDF cannot be downloaded or rendered so that I know whether to retry or select a different paper.
- As a developer, I want to switch LLM providers by changing one config value so that I'm never locked into a single provider.
- As a developer, I want the system to validate at startup that the configured model is vision-capable so that I discover misconfiguration immediately, not mid-pipeline.

## 4. Functional Requirements

### F1 — Paper Selection
- User selects one paper from the candidate list displayed in Phase 2
- Selection triggers the processing pipeline for that paper
- UI transitions to a processing state with live progress updates
- The "Select" action is irreversible within a session (no back navigation — start a new session to change)

### F2 — PDF Download
- System downloads the full PDF of the selected paper from arXiv
- PDF bytes held in memory for the duration of the pipeline run
- Download handles arXiv redirects to versioned PDF URLs automatically
- Download timeout: 30 seconds
- On failure: surface clear error with paper ID and failure reason

### F3 — PDF Page Rendering
- Every PDF page is rendered as a PNG image using `poppler`'s `pdftoppm`
- Resolution configurable via `pdf.dpi` in config (default: `150 DPI`)
  - 150 DPI: good quality, moderate token cost — recommended default
  - 200 DPI: higher quality for dense figures, higher token cost
  - 100 DPI: lower quality, lower token cost — for cost-sensitive runs
- Pages rendered in order and held in memory as `[][]byte` (one `[]byte` per page)
- On render failure: surface clear error with paper ID

### F4 — Vision Model Requirement
- The configured LLM model must support vision / image input
- System validates this at startup using a known-vision-models list in config
- If model is not in the known list: log a warning but proceed (custom/unknown models allowed)
- If model is in a known-non-vision list: fail at startup with a clear error
- README documents vision requirement explicitly as a prerequisite

### F5 — LLM Client Interface
- All LLM calls throughout the system go through a single `LLMClient` interface
- Interface accepts: system prompt, user prompt, page images (`[][]byte`), max tokens, temperature
- Interface returns: generated text content, token usage count (input + output separately)
- Concrete implementations exist for: Anthropic Claude, OpenAI, Google Gemini
- Custom/other providers: implement `LLMClient` interface (~50 lines)
- Active provider selected at startup from config — no runtime switching

### F6 — LLM Provider Configuration
- Provider, model, API key, max tokens, temperature, timeout, and optional base URL configurable
- Switching providers requires only changing `llm.provider` and `llm.model` in config (+ API key in `.env`)
- Adding a new provider requires only implementing the `LLMClient` interface

### F7 — LLM Error Handling
- `429 rate limited` → retry with exponential backoff, max 3 attempts
- `400 bad request` → surface as configuration error (wrong model name, payload too large)
- `500/503 provider error` → retry once, then surface as provider error
- Timeout → surface with duration and paper ID
- All errors set session to `failed` with `recoverable` flag

### F8 — Pipeline Status (Phase 3 stages)
- New stage labels surfaced to UI:
  - `"Downloading paper..."` (`fetching_pdf`)
  - `"Rendering pages..."` (`rendering_pdf`)
- `GET /status/:sessionId` updated to reflect new stages

## 5. Non-Functional Requirements

- **Full fidelity** — no visual content (diagrams, tables, figures) is lost between PDF and LLM input
- **Provider parity** — the `LLMClient` interface must be implementable by any vision-capable provider without leaking provider-specific concepts into calling code
- **Failure isolation** — a PDF download or render failure must not affect the session log or any persisted state
- **Configurability** — DPI, max tokens, temperature, provider, model — all configurable without code changes

## 6. Success Metrics

- PDF downloads successfully for any valid arXiv paper ID within 30 seconds
- All pages render to PNG images that preserve legible text and diagram detail at 150 DPI
- LLM call with page images returns a valid response for all three supported providers
- Switching `llm.provider` in config routes correctly to the new provider without code changes
- Startup with a known-non-vision model fails immediately with a clear, actionable error
- LLM 429 errors retry automatically and succeed in the majority of cases

## 7. Scope & Non-Goals

**In scope:**
- Paper selection UI (select button → triggers pipeline)
- PDF download from arXiv (`PDFFetchTool`)
- PDF page-to-image rendering via `poppler` (`PDFRendererTool`)
- `LLMClient` interface definition
- Anthropic, OpenAI, Gemini concrete implementations
- Config-driven provider selection and DPI setting
- Vision model validation at startup
- `POST /process` endpoint in Go backend
- Error handling for PDF download, render, and LLM failures
- New prerequisite: `poppler-utils` documented in README

**Out of scope:**
- Explainer generation (Phase 4)
- Any prompt engineering (Phase 4)
- Reviewer agent (Phase 5)
- Vault writing (Phase 4)
- Text extraction from PDF (not used — page-as-image is the only path)
- MarkItDown or any other text-based PDF conversion

## 8. Open Questions

None for this phase. All requirements are fully defined.

---

# Part 2 — Architecture

## Intent

Three components are introduced in this phase: `PDFFetchTool`, `PDFRendererTool`, and `LLMClient`. They are deliberately decoupled — the fetcher knows nothing about rendering, the renderer knows nothing about LLMs, and the LLM client knows nothing about arXiv or PDF. The Orchestrator connects them in sequence. This separation means each can be replaced or tested independently, and the LLM client is reused identically by both `ExplainerAgent` and `ReviewerAgent` in later phases.

The `PDFRendererTool` is the key new component in this phase. It shells out to `poppler`'s `pdftoppm` binary, which produces the highest quality page rendering available without CGO complexity. The external dependency (`poppler-utils`) is a single install command on any developer machine and is documented as a prerequisite.

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
    ├── Updates session { stage: "fetching_pdf" }
    ├── Spawns async goroutine for pipeline
    └── Returns { session_id } immediately (pipeline runs async)
    │
    ▼ (async goroutine)
PDFFetchTool.FetchPDF(ctx, paper.PDFURL)
    │
    └── GET https://arxiv.org/pdf/{arxiv_id} → []byte in memory
    │
    ▼
session { stage: "rendering_pdf" }
    │
    ▼
PDFRendererTool.RenderPages(ctx, pdfBytes)
    │
    └── shells out to pdftoppm → [][]byte (one PNG per page, in order)
    │
    ▼
[Phase 4 picks up here: ExplainerAgent.Generate(pageImages)]
    │
    ▼
LLMClient.Complete(CompletionRequest{ PageImages, prompts })
    │
    └── Routes to configured provider:
          anthropic → AnthropicClient
          openai    → OpenAIClient
          gemini    → GeminiClient
          custom    → implement LLMClient interface


Progress polling (parallel, established in Phase 2):
Next.js polls GET /api/status → { stage: "fetching_pdf" | "rendering_pdf", error }
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
  fetching_pdf:  "Downloading paper...",
  rendering_pdf: "Rendering pages...",
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
    if session.Stage != models.StageSelection {
        http.Error(w, "session not in selection stage", http.StatusBadRequest)
        return
    }

    // Find selected paper in candidates
    var selected *models.Paper
    for _, p := range session.Candidates {
        if p.ID == req.PaperID {
            selected = &p
            break
        }
    }
    if selected == nil {
        http.Error(w, "paper not found in session candidates", http.StatusBadRequest)
        return
    }

    session.SelectedPaper = selected
    session.Stage = models.StageFetching
    o.setSession(session)

    // Return immediately — pipeline runs async
    json.NewEncoder(w).Encode(ProcessResponse{SessionID: session.SessionID})

    go o.runPipeline(context.Background(), session)
}

func (o *Orchestrator) runPipeline(ctx context.Context, session *models.PipelineSession) {
    // Phase 3: PDF fetch + render
    pdf, err := o.pdfFetchTool.FetchPDF(ctx, session.SelectedPaper.PDFURL)
    if err != nil {
        o.failSession(session, err, true)
        return
    }
    session.PDF = pdf

    session.Stage = models.StageRendering
    o.setSession(session)

    pages, err := o.pdfRendererTool.RenderPages(ctx, pdf)
    if err != nil {
        o.failSession(session, err, true)
        return
    }
    session.PageImages = pages

    // Phase 4+: ExplainerAgent called here
}
```

**Dependencies:** PDFFetchTool, PDFRendererTool, `models`, `config`

---

### 2.4 PDFFetchTool

**Intent:** This tool owns one responsibility: given a PDF URL, return the bytes. It handles arXiv redirect behaviour and enforces download constraints. Nothing outside this tool needs to know how arXiv serves PDFs.

**Why in-memory, not disk:** The PDF is transient — it exists only for the duration of one pipeline run. Writing to disk adds a cleanup concern with no benefit. The bytes are passed directly to `PDFRendererTool`.

**Interface:**
```go
// /internal/tools/pdffetch.go

type PDFFetchTool struct {
    httpClient *http.Client  // configured with 30s timeout
}

func NewPDFFetchTool() *PDFFetchTool

func (t *PDFFetchTool) FetchPDF(ctx context.Context, pdfURL string) ([]byte, error)
```

**Implementation:**
```go
func (t *PDFFetchTool) FetchPDF(ctx context.Context, pdfURL string) ([]byte, error) {
    req, _ := http.NewRequestWithContext(ctx, "GET", pdfURL, nil)
    req.Header.Set("User-Agent", "arxiv-explainer-agent/1.0")

    resp, err := t.httpClient.Do(req)
    // http.Client follows redirects automatically
    if err != nil {
        return nil, fmt.Errorf("%w: %v", ErrPDFDownloadFailed, err)
    }
    defer resp.Body.Close()

    if resp.StatusCode == http.StatusNotFound {
        return nil, ErrPDFNotFound
    }
    if resp.StatusCode != http.StatusOK {
        return nil, fmt.Errorf("%w: status %d", ErrPDFDownloadFailed, resp.StatusCode)
    }

    return io.ReadAll(resp.Body)
}
```

**Error types:**
```go
var (
    ErrPDFNotFound       = errors.New("PDF not found on arXiv (404)")
    ErrPDFDownloadFailed = errors.New("PDF download failed")
    ErrPDFTimeout        = errors.New("PDF download timed out after 30s")
)
```

**Dependencies:** `net/http`, `io`

---

### 2.5 PDFRendererTool

**Intent:** This tool converts PDF bytes into an ordered slice of PNG page images. It is the bridge between raw PDF content and the vision LLM. Every page is rendered — text pages, figure pages, table pages, reference pages — so the LLM sees the complete paper exactly as a human reader would.

**Why `poppler` (`pdftoppm`):**
- Industry-standard rendering quality — text is crisp, diagrams are clear at 150 DPI
- Available as a single install: `brew install poppler` (macOS) / `apt install poppler-utils` (Linux)
- No CGO complexity — shells out as a subprocess
- Widely battle-tested for PDF rendering in production pipelines

**Why shell out rather than a Go library:**
- Pure Go PDF rendering libraries (`pdfcpu`, etc.) produce significantly lower quality output
- CGO-based libraries (`go-fitz` / MuPDF) require MuPDF headers at build time — more complex developer setup
- `poppler` is a single system package, simpler than CGO build dependencies

**Interface:**
```go
// /internal/tools/pdfrenderer.go

type PDFRendererTool struct {
    config *config.Config
}

func NewPDFRendererTool(cfg *config.Config) *PDFRendererTool

func (t *PDFRendererTool) RenderPages(ctx context.Context, pdfBytes []byte) ([][]byte, error)
// Returns [][]byte — one PNG []byte per page, in reading order
```

**Implementation:**
```go
func (t *PDFRendererTool) RenderPages(ctx context.Context, pdfBytes []byte) ([][]byte, error) {
    // Write PDF to temp file (pdftoppm requires a file path)
    tmpPDF, err := os.CreateTemp("", "arxiv-*.pdf")
    if err != nil {
        return nil, fmt.Errorf("failed to create temp PDF file: %w", err)
    }
    defer os.Remove(tmpPDF.Name())

    if _, err := tmpPDF.Write(pdfBytes); err != nil {
        return nil, fmt.Errorf("failed to write temp PDF: %w", err)
    }
    tmpPDF.Close()

    // Create temp dir for output PNGs
    tmpDir, err := os.MkdirTemp("", "arxiv-pages-*")
    if err != nil {
        return nil, fmt.Errorf("failed to create temp dir: %w", err)
    }
    defer os.RemoveAll(tmpDir)

    // Run pdftoppm
    dpi := strconv.Itoa(t.config.PDF.DPI)
    outputPrefix := filepath.Join(tmpDir, "page")
    cmd := exec.CommandContext(ctx, "pdftoppm",
        "-png",           // output format
        "-r", dpi,        // resolution (DPI)
        tmpPDF.Name(),    // input PDF
        outputPrefix,     // output prefix (pages saved as page-001.png, page-002.png, ...)
    )

    if output, err := cmd.CombinedOutput(); err != nil {
        return nil, fmt.Errorf("%w: %v — %s", ErrPDFRenderFailed, err, string(output))
    }

    // Read output PNGs in order
    entries, err := os.ReadDir(tmpDir)
    if err != nil {
        return nil, fmt.Errorf("failed to read rendered pages: %w", err)
    }

    // Sort entries — pdftoppm names files page-001.png, page-002.png, etc.
    sort.Slice(entries, func(i, j int) bool {
        return entries[i].Name() < entries[j].Name()
    })

    var pages [][]byte
    for _, entry := range entries {
        if !strings.HasSuffix(entry.Name(), ".png") {
            continue
        }
        data, err := os.ReadFile(filepath.Join(tmpDir, entry.Name()))
        if err != nil {
            return nil, fmt.Errorf("failed to read page %s: %w", entry.Name(), err)
        }
        pages = append(pages, data)
    }

    if len(pages) == 0 {
        return nil, ErrPDFRenderNoPages
    }

    return pages, nil
}
```

**Error types:**
```go
var (
    ErrPDFRenderFailed   = errors.New("PDF rendering failed — is poppler installed?")
    ErrPDFRenderNoPages  = errors.New("PDF rendering produced no pages")
    ErrPopplNotFound     = errors.New("pdftoppm not found — install poppler: brew install poppler")
)
```

**Startup validation (checks poppler is installed):**
```go
func ValidatePoppler() error {
    _, err := exec.LookPath("pdftoppm")
    if err != nil {
        return ErrPopplNotFound
    }
    return nil
}
// Called in main.go during startup — fails fast with install instructions
```

**Config field:**
```yaml
pdf:
  dpi: 150   # 100=fast/cheap, 150=balanced (default), 200=high quality
```

**Temp file cleanup:** Both the temp PDF file and temp directory are removed via `defer` — no cleanup required on success or failure.

**Dependencies:** `os`, `os/exec`, `path/filepath`, `sort`, `strconv`, `config`

---

### 2.6 LLMClient Interface

**Intent:** The `LLMClient` interface is the most important abstraction in the system. It decouples every agent from any specific LLM provider. The interface accepts page images — not PDF bytes — making it compatible with any vision-capable model regardless of whether the provider has native PDF support.

**Why images not PDF bytes in the interface:** PDF-as-document is a provider-specific feature (Anthropic and Gemini support it natively; others do not). Images are a universal vision input supported by every vision-capable model. Standardising on images in the interface means the LLM client implementations are simpler and more consistent, and custom/unknown models can be supported without special cases.

**Interface:**
```go
// /internal/llm/client.go

type LLMClient interface {
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

type CompletionRequest struct {
    SystemPrompt string
    UserPrompt   string
    PageImages   [][]byte  // one PNG []byte per page, in reading order
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

**Vision model validation:**
```go
// /internal/llm/vision.go

// Models known to support vision input
var KnownVisionModels = map[string]bool{
    "claude-opus-4-6":   true,
    "claude-sonnet-4-6": true,
    "claude-haiku-4-5":  true,
    "gpt-4o":            true,
    "gpt-4o-mini":       true,
    "gemini-2.0-flash":  true,
    "gemini-2.5-pro":    true,
}

// Models known NOT to support vision input
var KnownNonVisionModels = map[string]bool{
    "gpt-3.5-turbo": true,
    "o1-mini":       true,
}

func ValidateVisionSupport(model string) error {
    if KnownNonVisionModels[model] {
        return fmt.Errorf(
            "model %q does not support vision input — this system requires a vision-capable model.\n"+
            "Supported models include: claude-sonnet-4-6, gpt-4o, gemini-2.0-flash",
            model,
        )
    }
    if !KnownVisionModels[model] {
        // Unknown model — warn but allow (custom/self-hosted models)
        slog.Warn("model not in known-vision list — proceeding, but vision support is unverified",
            "model", model)
    }
    return nil
}
// Called in main.go during startup
```

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

**Dependencies:** `config`, `slog`

---

### 2.7 Anthropic Claude Implementation

**Intent:** Send page images as base64-encoded vision blocks in the messages array. Anthropic's vision API accepts PNG images natively — one content block per page.

```go
// /internal/llm/anthropic.go

func (c *AnthropicClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
    var contentBlocks []anthropic.ContentBlock

    // Attach each page as a vision image block
    for _, pageImage := range req.PageImages {
        contentBlocks = append(contentBlocks, anthropic.ImageBlock{
            Source: anthropic.Base64ImageSource{
                MediaType: "image/png",
                Data:      base64.StdEncoding.EncodeToString(pageImage),
            },
        })
    }

    // Add user prompt after images
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

### 2.8 OpenAI Implementation

**Intent:** Send page images as base64-encoded image_url content parts in the user message. This is OpenAI's standard vision input format — works with gpt-4o and any future vision-capable model.

```go
// /internal/llm/openai.go

func (c *OpenAIClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
    var messages []openai.ChatCompletionMessageParam

    messages = append(messages, openai.SystemMessage(req.SystemPrompt))

    // Build user message with page images
    var userContent []openai.ChatCompletionContentPartUnionParam
    for _, pageImage := range req.PageImages {
        dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pageImage)
        userContent = append(userContent, openai.ImageContentPart(
            openai.ChatCompletionContentPartImageParam{
                ImageURL: openai.ChatCompletionContentPartImageImageURLParam{
                    URL:    dataURL,
                    Detail: openai.ImageURLDetailAuto,
                },
            },
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

### 2.9 Google Gemini Implementation

**Intent:** Send page images as inline base64 PNG blobs in the content parts array. Gemini's vision API accepts PNG blobs natively alongside text parts.

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
    // Attach each page as an image blob
    for _, pageImage := range req.PageImages {
        parts = append(parts, genai.Blob{
            MIMEType: "image/png",
            Data:     pageImage,
        })
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

Phase 3 adds two fields to `PipelineSession` and introduces one new config section:

```go
// PipelineSession additions
type PipelineSession struct {
    // ... existing fields from Phase 2 ...
    PDF        []byte    // raw PDF bytes (set after fetch, cleared after rendering)
    PageImages [][]byte  // one PNG per page (set after rendering, held for LLM calls)
}

// New pipeline stage
const StageRendering PipelineStage = "rendering_pdf"
```

```go
// New config section
type PDFConfig struct {
    DPI int  // default 150
}

type Config struct {
    // ... existing fields ...
    PDF PDFConfig
}
```

```yaml
# config.yaml addition
pdf:
  dpi: 150
```

**Memory note:** For a typical 12-page AI paper at 150 DPI, each PNG page is approximately 500KB–1MB. Total page image memory: ~6–12MB per pipeline run. Cleared from session after pipeline completes.

---

## 4. Data Flow

### Selection, Fetch & Render Flow

```
1. User clicks "Select" on paper card
      │
      ▼
2. Next.js → POST /api/select { session_id, paper_id }
      │
      ▼
3. Go Orchestrator.HandleProcess()
      ├── Validate session in "selection" stage
      ├── Set session.SelectedPaper
      ├── Set session.Stage = "fetching_pdf"
      ├── Return { session_id } immediately
      └── Spawn goroutine: runPipeline(ctx, session)
      │
      ▼ (async goroutine)
4. PDFFetchTool.FetchPDF(ctx, paper.PDFURL)
      ├── GET https://arxiv.org/pdf/2401.12345
      │     (follows redirect → https://arxiv.org/pdf/2401.12345v2)
      ├── Read response body → []byte (~500KB–5MB)
      └── session.PDF = pdfBytes
      │
      ▼
5. session.Stage = "rendering_pdf"
      │
      ▼
6. PDFRendererTool.RenderPages(ctx, session.PDF)
      ├── Write PDF to temp file
      ├── exec: pdftoppm -png -r 150 /tmp/arxiv-xxx.pdf /tmp/arxiv-pages-xxx/page
      ├── Read output PNGs in sorted order
      ├── Return [][]byte (e.g. 12 pages × ~700KB each)
      └── session.PageImages = pages
      │
      ▼
7. [Phase 4: ExplainerAgent.Generate({ PageImages, PaperMeta }) called here]
```

### LLM Call Flow (wired in Phase 3, invoked in Phase 4)

```
ExplainerAgent.Generate(ExplainerInput{ PageImages, PaperMeta, RevisionNote })
      │
      ▼
Build CompletionRequest {
    SystemPrompt: "...",
    UserPrompt:   "Paper: Title...\nPlease generate the explainer.",
    PageImages:   session.PageImages,  // [][]byte — one PNG per page
    MaxTokens:    config.LLM.MaxTokens,
    Temperature:  config.LLM.Temperature,
}
      │
      ▼
LLMClient.Complete(ctx, request)
      │
      ▼
Provider router → AnthropicClient | OpenAIClient | GeminiClient
      │
      ▼
Each page PNG sent as vision input block (base64 encoded)
      │
      ▼
CompletionResponse { Content: "...", InputTokens: 42000, OutputTokens: 3200 }
```

### Error Flow — poppler Not Installed

```
main.go startup: ValidatePoppler()
      │
      ▼
exec.LookPath("pdftoppm") → not found
      │
      ▼
FATAL: pdftoppm not found — install poppler:
  macOS:  brew install poppler
  Linux:  apt install poppler-utils
      │
      ▼
os.Exit(1) — server does not start
```

---

## 5. Tech Stack

**New system dependency (Phase 3):**

| Dependency | Install | Why |
|---|---|---|
| `poppler-utils` | `brew install poppler` / `apt install poppler-utils` | Provides `pdftoppm` for high-quality PDF page rendering. Industry standard, zero CGO complexity. |

**New Go dependencies (Phase 3):**

| Package | Provider | Why |
|---|---|---|
| `github.com/anthropics/anthropic-sdk-go` | Anthropic | Official Go SDK. Vision image blocks in messages array. |
| `github.com/openai/openai-go` | OpenAI | Official Go SDK. Vision image_url content parts. |
| `google.golang.org/genai` | Google Gemini | Official Go SDK. Native image blob support. |

All three provider SDKs installed regardless of active provider. Unused clients add minimal binary size overhead (~5–10MB).

**Config addition:**
```yaml
pdf:
  dpi: 150  # rendering resolution — tradeoff between quality and token cost
```

---

## 6. Integration Points

### arXiv PDF Download

**URL pattern:** `https://arxiv.org/pdf/{arxiv_id}`
**Auth:** None
**Redirects:** arXiv redirects to versioned URL — `http.Client` follows automatically
**Timeout:** 30 seconds
**Rate limit:** Respect 3-second delay (same domain as arXiv API)

---

### poppler (`pdftoppm`)

**Binary:** `pdftoppm` (part of `poppler-utils` package)
**Input:** PDF file path
**Output:** PNG files written to output directory, named `{prefix}-001.png`, `{prefix}-002.png`, etc.
**Key flags:**
```
-png          output format PNG
-r 150        resolution in DPI (from config)
```
**Validated at startup** — server fails fast if `pdftoppm` not found in PATH.

---

### Anthropic Messages API

**Endpoint:** `https://api.anthropic.com/v1/messages`
**Auth:** `x-api-key` header
**Vision input:** Base64 PNG image blocks in `messages[]` content array — one block per page
**Token cost note:** Image tokens vary by resolution. At 150 DPI, a typical page ≈ 800–1,500 input tokens.

---

### OpenAI Chat Completions API

**Endpoint:** `https://api.openai.com/v1/chat/completions`
**Auth:** `Authorization: Bearer` header
**Vision input:** Base64 data URL image content parts in user message — one part per page
**Key constraint:** Model must be vision-capable (e.g. `gpt-4o`) — validated at startup.

---

### Google Gemini API

**Endpoint:** `https://generativelanguage.googleapis.com/v1beta/models/{model}:generateContent`
**Auth:** `Authorization: Bearer` header
**Vision input:** Inline PNG blob parts in content array — one blob per page
**Advantage:** Largest context window — best choice for very long papers (60+ pages).

---

## 7. Cross-Cutting Concerns

### Error Handling

| Failure | Behaviour | Recoverable |
|---|---|---|
| `pdftoppm` not found at startup | Fatal startup error with install instructions | N/A — fix before starting |
| PDF 404 | "Paper PDF not found on arXiv. Select a different paper." | No |
| PDF timeout (30s) | "PDF download timed out. Try again." | Yes |
| PDF download error | "PDF download failed: [reason]. Try again." | Yes |
| PDF render failure | "Failed to render PDF pages. Try again." | Yes |
| PDF render produces 0 pages | "PDF rendered no pages — the file may be corrupt." | No — select different paper |
| LLM 429 after retries | "LLM rate limit exceeded. Try again shortly." | Yes |
| LLM 400 (bad model) | "LLM config error: model '[model]' rejected. Check llm.model in config." | No |
| LLM 400 (context too large) | "Too many pages for model '[model]'. Try a shorter paper or switch to Gemini." | No |
| LLM 500/503 | "LLM provider unavailable. Try again shortly." | Yes |
| LLM timeout | "LLM request timed out after [N]s. Try again." | Yes |

### Observability

```json
{"level":"INFO","msg":"pdf fetch started","session_id":"abc123","paper_id":"2401.12345"}
{"level":"INFO","msg":"pdf fetch complete","session_id":"abc123","paper_id":"2401.12345","size_bytes":1245184,"duration_ms":2100}
{"level":"INFO","msg":"pdf render started","session_id":"abc123","paper_id":"2401.12345","dpi":150}
{"level":"INFO","msg":"pdf render complete","session_id":"abc123","paper_id":"2401.12345","pages":12,"duration_ms":3400}
{"level":"INFO","msg":"llm call started","session_id":"abc123","provider":"anthropic","model":"claude-sonnet-4-6","pages":12}
{"level":"INFO","msg":"llm call complete","session_id":"abc123","input_tokens":42800,"output_tokens":3200,"duration_ms":18400}
{"level":"WARN","msg":"llm rate limited","session_id":"abc123","provider":"anthropic","attempt":1,"backoff_ms":5000}
{"level":"ERROR","msg":"pdf render failed","session_id":"abc123","error":"pdftoppm not found"}
```

### Security
- PDF bytes and page images held in memory only — never written to permanent storage
- Temp files (`/tmp/arxiv-*.pdf`, `/tmp/arxiv-pages-*/`) cleaned up via `defer os.Remove` / `defer os.RemoveAll`
- `pdftoppm` subprocess runs without shell (`exec.Command`, not `exec.Command("sh", "-c", ...)`) — no shell injection risk
- API keys never logged, never passed to frontend

### Token Cost Awareness
At 150 DPI, a typical 12-page AI paper sends approximately:
- ~800–1,500 input tokens per page (vision)
- ~10,000–18,000 total input tokens for page images alone
- Plus system prompt (~800 tokens) and user prompt (~100 tokens)

This is significantly more than text-only approaches. The tradeoff is full visual fidelity. Documented in README.

---

## 8. Risks & Tradeoffs

| ID | Risk/Tradeoff | Severity | Mitigation |
|---|---|---|---|
| R1 | Very long papers (60+ pages) generate very high token counts | Medium | Context window pre-check added in Phase 6. Gemini recommended for long papers (largest context). `pdf.dpi` can be lowered to 100 to reduce token cost. |
| R2 | `poppler` version differences across OS produce slightly different output | Low | PDF rendering quality differences are cosmetic — text legibility is consistent across versions. Validated on macOS (brew) and Ubuntu (apt). |
| R3 | Provider SDK API changes break concrete implementations | Low | Each provider isolated behind interface. Breaking SDK change affects one file. Versions pinned in `go.mod`. |
| R4 | Temp files not cleaned up if process is killed mid-render | Low | Temp files in `/tmp` — OS cleans on reboot. For long-running processes, `/tmp` cleanup is standard OS behaviour. |
| T1 | Page-as-image uses significantly more tokens than text extraction | Accepted | Full visual fidelity is the core design goal. Diagrams, tables, and figures are central to understanding AI papers. Token cost is documented; DPI is configurable to manage cost. |
| T2 | External `poppler` dependency | Accepted | Single install command. Well-maintained, available on all target platforms. Alternative (CGO libraries) adds more complexity, not less. |
| T3 | All three provider SDKs installed regardless of active provider | Accepted | Binary size overhead minimal (~5–10MB). Any provider is one config change away. |
| T4 | No streaming LLM responses | Accepted | Streaming adds significant UI complexity. Progress communicated via stage polling. |

---

## Exit Criteria

All of the following must be true before Phase 4 begins:

- [ ] `poppler` (`pdftoppm`) validated at startup — server fails with install instructions if not found
- [ ] Vision model validated at startup — known-non-vision models rejected with clear error
- [ ] Unknown/custom models produce a warning log but allow startup
- [ ] Clicking "Select" on a paper card transitions UI to processing state
- [ ] PDF downloads successfully for any valid arXiv paper ID within 30 seconds
- [ ] PDF download handles arXiv redirects to versioned URLs automatically
- [ ] PDF timeout (30s) surfaces a clear, recoverable error in the UI
- [ ] All PDF pages render to PNG images at configured DPI (default 150)
- [ ] Page images are ordered correctly (page 1 first, last page last)
- [ ] Progress UI shows `"Rendering pages..."` stage after download
- [ ] `LLMClient.Complete()` with page images returns a valid response for Anthropic
- [ ] `LLMClient.Complete()` with page images returns a valid response for OpenAI
- [ ] `LLMClient.Complete()` with page images returns a valid response for Gemini
- [ ] Switching `llm.provider` in config routes to the correct provider without code changes
- [ ] LLM 429 retries automatically (max 3 attempts with backoff) before surfacing error
- [ ] LLM 400 surfaces as a config error naming the configured model
- [ ] PDF size, page count, render duration, and LLM token counts logged for every run
- [ ] Input and output tokens returned separately in `CompletionResponse` for all providers
- [ ] Temp PDF and temp page directory cleaned up after every run (success or failure)
