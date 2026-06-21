# Phase 6 — Polish & Hardening
## ArXiv AI Paper Explainer Agent

---

## Intent

Phases 1–5 built a working product. Phase 6 makes it a **trustworthy** one.

A tool that works 90% of the time is not a tool a practitioner will rely on. Every failure that surfaces as a cryptic error, every partial file left on disk, every silent retry that the user can't see — these erode trust incrementally until the tool is abandoned. Phase 6 exists to close every gap between "it works" and "it works reliably, transparently, and recovers gracefully when it doesn't."

This phase touches no new business logic. Every decision here serves one intent: **a practitioner should be able to use this tool daily without ever having to think about its internals.** When it works, it's invisible. When it fails, it's clear, honest, and actionable.

---

# Part 1 — Product Requirements

## 1. Problem Statement

A locally running tool with external API dependencies (arXiv, LLM providers) will encounter failures. Rate limits, timeouts, network blips, disk permission errors, malformed API responses — these are not edge cases, they are certainties over time. A tool that handles them poorly teaches the user to distrust it.

Phase 6 addresses three categories of gap:

1. **Error handling** — every failure mode surfaces clearly with the right message and the right action
2. **Observability** — the developer (and curious user) can always understand what the system did and why
3. **Developer experience** — a new developer can go from clone to productive in under 10 minutes

## 2. Target Users

**Primary (error handling + observability):** The end user — a practitioner using the tool daily who should never encounter a cryptic error or a stuck UI.

**Primary (DX):** The developer setting up or maintaining the system — who should never have to guess how something works or why it failed.

## 3. User Stories

- As a practitioner, I want clear, human-readable error messages so that I always know what went wrong and whether I should retry or take another action.
- As a practitioner, I want a retry button for recoverable errors so that transient failures don't require me to restart the whole workflow.
- As a practitioner, I want to see token usage and estimated cost after each run so that I can track my LLM spending.
- As a practitioner, I want to know if the arXiv rate limit might be affecting my run so that I understand why a discovery is slow.
- As a developer, I want structured logs for every pipeline event so that I can diagnose any failure without adding debug code.
- As a developer, I want a README that gets me from clone to running in under 10 minutes so that setup is never a blocker.
- As a developer, I want config validation that names every missing field so that misconfiguration is diagnosed at startup, not mid-run.

## 4. Functional Requirements

### F1 — Comprehensive Error Messages
Every error surfaced to the UI must include:
- **What happened** — plain English description of the failure
- **Where it happened** — which stage failed (`arXiv fetch`, `PDF download`, `LLM generation`, etc.)
- **What to do** — specific next action (`retry`, `check config`, `select a different paper`, `free disk space`)
- **Recoverable flag** — whether a retry button is shown

Full error message matrix:

| Failure | Message | Recoverable |
|---|---|---|
| arXiv 429 after retries | "arXiv is rate limiting requests. Wait 60 seconds and try again." | Yes |
| arXiv 5xx | "arXiv is currently unavailable. Try again in a few minutes." | Yes |
| arXiv XML parse failure | "Unexpected response from arXiv. Try again." | Yes |
| No new papers found | "No new cs.AI papers found since your last run. Check back later." | No |
| `pdftoppm` not found at startup | Startup fatal — not a UI error. See F9. | N/A |
| PDF render failure | "Failed to render PDF pages. Try again or select a different paper." | Yes |
| PDF render produces 0 pages | "PDF rendered no pages — the file may be corrupt. Select a different paper." | No |
| PDF 404 | "This paper's PDF is not available on arXiv. Select a different paper." | No |
| PDF timeout | "PDF download timed out after 30s. Try again or select a different paper." | Yes |
| LLM 429 after retries | "LLM provider rate limit exceeded. Wait a few minutes and try again." | Yes |
| LLM 400 (bad model) | "LLM config error: model '[model]' was rejected. Check llm.model in config." | No |
| LLM 400 (context too large) | "Paper is too long for the configured model. Try switching to Gemini (larger context window)." | No |
| LLM 500/503 | "LLM provider is temporarily unavailable. Try again in a few minutes." | Yes |
| LLM timeout | "LLM request timed out after [N]s. Try again." | Yes |
| Vault permission denied | "Cannot write to vault: permission denied at [path]. Check folder permissions." | No |
| Vault disk full | "Cannot write to vault: disk is full. Free up space and try again." | No |
| Config missing field | Shown at startup only — never surfaced via UI | N/A |

### F2 — Retry Flow
- All errors with `recoverable: true` display a "Try Again" button in the UI
- Clicking retry restarts the pipeline from the failed stage, not from the beginning
  - Discovery failure → retry discovery
  - PDF failure → retry PDF download for same paper
  - LLM failure → retry LLM call for same paper
- Retry preserves the current session — user does not re-select a paper

### F3 — Token Usage & Cost Estimation
- Total token usage displayed in success state (already in Phase 4/5)
- Estimated cost displayed alongside token count:
  - Cost calculated from known per-token pricing for each provider
  - Pricing stored in a config-adjacent lookup table (not hardcoded in business logic)
  - Displayed as: `~$0.12` (approximate, based on [provider] pricing)
  - Note: "Pricing may vary. Check your provider's dashboard for exact usage."

### F4 — Context Window Pre-Check
Before sending page images to the LLM:
- Estimate total image token count (heuristic: ~1,000 tokens per page at 150 DPI; ~1,500 at 200 DPI; ~700 at 100 DPI)
- If estimated tokens + system prompt (~900) + max_output_tokens > model's known context limit:
  - Surface a warning: "This paper (~N pages) may be too long for [model]. Consider switching to Gemini (largest context window)."
  - Proceed anyway — let the user decide
  - If the LLM returns a 400 context error, surface the specific "too long" message from F1

### F5 — arXiv Rate Limit Advisory
- If arXiv discovery takes longer than expected due to retries:
  - Show in progress indicator: `"Connecting to arXiv (retry 2/3)..."`
  - Surface advisory on completion: "arXiv rate limited this request — consider waiting a few minutes before the next run."

### F6 — Structured Logging (complete)
Every pipeline event must be logged in structured JSON via Go `slog`:

| Event | Log level | Required fields |
|---|---|---|
| Config loaded | INFO | provider, model, vault_path, pdf_dpi |
| Poppler validated | INFO | pdftoppm_path |
| Server started | INFO | addr, port |
| Discovery started | INFO | session_id |
| arXiv fetch complete | INFO | session_id, count, duration_ms |
| arXiv retry | WARN | session_id, attempt, backoff_ms, error |
| Log check complete | INFO | session_id, unprocessed, returning |
| PDF fetch started | INFO | session_id, paper_id, url |
| PDF fetch complete | INFO | session_id, paper_id, size_bytes, duration_ms |
| PDF render started | INFO | session_id, paper_id, dpi |
| PDF render complete | INFO | session_id, paper_id, pages, duration_ms |
| LLM call started | INFO | session_id, provider, model, pages, iteration |
| LLM call complete | INFO | session_id, input_tokens, output_tokens, duration_ms |
| LLM retry | WARN | session_id, provider, attempt, backoff_ms, error |
| Review complete | INFO | session_id, iteration, score, pass |
| Vault write complete | INFO | session_id, vault_file, duration_ms |
| Log update complete | INFO | session_id, paper_id |
| Log update failed | WARN | session_id, paper_id, error |
| Pipeline complete | INFO | session_id, total_duration_ms, total_tokens, pages |
| Any stage failure | ERROR | session_id, stage, error, recoverable |

### F9 — Poppler Startup Validation
- Go backend validates `pdftoppm` is available in PATH at startup
- If not found: server fails to start with clear install instructions:
  ```
  FATAL: pdftoppm not found — PDF rendering requires poppler.
    macOS:  brew install poppler
    Linux:  apt install poppler-utils
  ```
- README documents `poppler-utils` as a required prerequisite alongside Node.js and Go

### F10 — DPI Config Documentation
- `pdf.dpi` documented in config reference with tradeoff explanation:
  - `100` — lower quality, lower token cost, faster rendering
  - `150` — balanced quality and cost (default, recommended)
  - `200` — higher quality for dense figures, higher token cost, slower rendering
- README notes that very long papers (40+ pages) at 200 DPI may approach model context limits

### F11 — README
The README must cover:
- Prerequisites (Node.js version, Go version, Obsidian)
- Setup steps (clone → `.env` → config → `make dev`)
- All config fields with descriptions and defaults
- LLM provider switching instructions (which fields to change, which API key to set)
- Estimated cost per paper per provider (with caveat about pricing variability)
- Troubleshooting section covering all F1 error scenarios
- `max_review_iterations` explanation (quality/cost tradeoff)

### F8 — End-to-End Validation
A complete end-to-end test run must be documented and passing:
- Trigger → 5 papers displayed
- Select one → PDF downloaded → explainer generated → reviewed → revised → saved
- Verify note opens correctly in Obsidian
- Verify `processed.json` updated
- Verify duplicate prevention works on second run
- Run with all three LLM providers

## 5. Non-Functional Requirements

- **No new features** — Phase 6 hardens what exists, it does not add capability
- **No breaking changes** — all existing behaviour from Phases 1–5 is preserved
- **Auditability** — every pipeline run is fully reconstructable from logs alone
- **Setup time** — fresh developer setup in under 10 minutes following README

## 6. Success Metrics

- Every error scenario from F1 produces the correct message in the UI — verified manually
- Zero cryptic or technical error messages visible to the end user
- Retry works correctly for all recoverable failures — no re-selection required
- Structured logs contain all required fields for every event type
- Fresh setup from clone to running takes under 10 minutes
- Full end-to-end run completes without errors across all three LLM providers

## 7. Scope & Non-Goals

**In scope:**
- Complete error message coverage (F1 matrix)
- Retry flow for recoverable errors
- Token usage and cost estimation display
- Context window pre-check
- arXiv retry progress indicator
- Structured logging completeness audit and gap-fill
- README (full)
- End-to-end validation run

**Out of scope (explicitly — future enhancements):**
- Multiple arXiv category support
- Relevance ranking / keyword filtering
- Batch processing (multiple papers per run)
- Cloud hosting
- Obsidian plugin integration
- Section-level note regeneration
- Human feedback / revision loop
- Different LLM providers per agent (explainer vs reviewer)
- Token usage history / spending dashboard

## 8. Open Questions

None. Phase 6 closes all open items. The product is complete at the end of this phase.

---

# Part 2 — Architecture

## Intent

Phase 6 adds no new components. It completes, audits, and hardens every component built in Phases 1–5. The architecture changes are additive only — error handling enrichment, logging gap-fill, cost estimation, and a context window pre-check. Every change is localized to the component it hardens.

---

## 1. System Overview

No new services, no new components. Phase 6 changes are distributed across existing components:

```
┌─────────────────────────────┐         ┌──────────────────────────────────┐
│      Next.js App            │         │        Go ADK Backend            │
│                             │         │                                  │
│  [HARDENED]                 │         │  [HARDENED]                      │
│  - Full error message UI    │         │  - Complete slog coverage        │
│  - Retry flow               │         │  - Context window pre-check      │
│  - Cost estimation display  │         │  - Cost estimator                │
│  - arXiv retry indicator    │         │  - Enriched error types          │
│  - Token usage display      │         │  - Retry stage tracking          │
└─────────────────────────────┘         └──────────────────────────────────┘
```

---

## 2. Component Breakdown (changes only)

### 2.1 Error System — Go Backend

**Intent:** A single, consistent error system that every component uses. Error types carry enough context to produce the correct user-facing message without the Orchestrator needing to inspect error internals.

**Enriched error type:**
```go
// /internal/models/errors.go

type PipelineError struct {
    Stage       string  // which stage failed
    Code        string  // machine-readable error code
    Message     string  // human-readable message for the user
    Action      string  // what the user should do
    Recoverable bool
    Cause       error   // underlying error for logging
}

func (e *PipelineError) Error() string {
    return fmt.Sprintf("[%s] %s", e.Stage, e.Message)
}

// Error codes
const (
    ErrCodeArxivRateLimit      = "ARXIV_RATE_LIMIT"
    ErrCodeArxivUnavailable    = "ARXIV_UNAVAILABLE"
    ErrCodeArxivParse          = "ARXIV_PARSE"
    ErrCodeNoNewPapers         = "NO_NEW_PAPERS"
    ErrCodePDFNotFound         = "PDF_NOT_FOUND"
    ErrCodePDFTimeout          = "PDF_TIMEOUT"
    ErrCodePDFDownload         = "PDF_DOWNLOAD"
    ErrCodeLLMRateLimit        = "LLM_RATE_LIMIT"
    ErrCodeLLMBadModel         = "LLM_BAD_MODEL"
    ErrCodeLLMContextTooLarge  = "LLM_CONTEXT_TOO_LARGE"
    ErrCodeLLMUnavailable      = "LLM_UNAVAILABLE"
    ErrCodeLLMTimeout          = "LLM_TIMEOUT"
    ErrCodeVaultPermission     = "VAULT_PERMISSION"
    ErrCodeVaultDiskFull       = "VAULT_DISK_FULL"
)
```

**Error factory functions (one per error code):**
```go
func ErrArxivRateLimit() *PipelineError {
    return &PipelineError{
        Stage:       "discovery",
        Code:        ErrCodeArxivRateLimit,
        Message:     "arXiv is rate limiting requests. Wait 60 seconds and try again.",
        Action:      "retry",
        Recoverable: true,
    }
}

func ErrLLMBadModel(model string) *PipelineError {
    return &PipelineError{
        Stage:       "generating",
        Code:        ErrCodeLLMBadModel,
        Message:     fmt.Sprintf("LLM config error: model '%s' was rejected. Check llm.model in config.", model),
        Action:      "fix_config",
        Recoverable: false,
    }
}

func ErrVaultPermission(path string) *PipelineError {
    return &PipelineError{
        Stage:       "writing",
        Code:        ErrCodeVaultPermission,
        Message:     fmt.Sprintf("Cannot write to vault: permission denied at %s. Check folder permissions.", path),
        Action:      "fix_permissions",
        Recoverable: false,
    }
}

// ... one factory per error code in F1 matrix
```

**Orchestrator `failSession` (updated):**
```go
func (o *Orchestrator) failSession(session *models.PipelineSession, err *models.PipelineError) {
    session.Stage = models.StageFailed
    session.Error = err.Message
    session.ErrorCode = err.Code
    session.ErrorAction = err.Action
    session.Recoverable = err.Recoverable
    o.setSession(session)

    slog.Error("pipeline stage failed",
        "session_id", session.SessionID,
        "stage", err.Stage,
        "code", err.Code,
        "recoverable", err.Recoverable,
        "cause", err.Cause,
    )
}
```

---

### 2.2 Retry Flow — Go Backend & Next.js

**Intent:** Retry must resume from the failed stage, not restart from discovery. The session already holds the paper selection — retrying should jump directly to the failed operation.

**New endpoint:**
```go
// POST /retry { session_id }
// Resumes pipeline from the failed stage of the given session

func (o *Orchestrator) HandleRetry(w http.ResponseWriter, r *http.Request) {
    var req RetryRequest
    json.NewDecoder(r.Body).Decode(&req)

    session, ok := o.getSession(req.SessionID)
    if !ok || session.Stage != models.StageFailed {
        http.Error(w, "session not retryable", http.StatusBadRequest)
        return
    }

    // Reset error state
    session.Error = ""
    session.ErrorCode = ""
    session.Recoverable = false

    // Resume from the failed stage
    switch session.ErrorCode {
    case models.ErrCodeArxivRateLimit,
         models.ErrCodeArxivUnavailable,
         models.ErrCodeArxivParse:
        // Restart discovery
        session.Stage = models.StageDiscovery
        o.setSession(session)
        go o.runDiscovery(context.Background(), session)

    case models.ErrCodePDFTimeout,
         models.ErrCodePDFDownload:
        // Restart from PDF fetch (paper already selected)
        session.Stage = models.StageFetching
        o.setSession(session)
        go o.runPipeline(context.Background(), session)

    case models.ErrCodeLLMRateLimit,
         models.ErrCodeLLMUnavailable,
         models.ErrCodeLLMTimeout:
        // Restart from generation (PDF already in memory if still alive)
        // If PDF is gone (process restarted), re-fetch
        if len(session.PDF) == 0 {
            session.Stage = models.StageFetching
        } else {
            session.Stage = models.StageGenerating
        }
        o.setSession(session)
        go o.runPipeline(context.Background(), session)

    default:
        // Non-recoverable or unknown — surface error
        http.Error(w, "this error is not retryable", http.StatusBadRequest)
        return
    }

    json.NewEncoder(w).Encode(RetryResponse{SessionID: session.SessionID})
}
```

**Next.js retry button:**
```typescript
// Shown when status.recoverable === true
<button
  onClick={() => retry(sessionId)}
  className="..."
>
  Try Again
</button>

// POST /api/retry { session_id }
// → POST localhost:8080/retry { session_id }
// ← { session_id } — resume polling status
```

**New Next.js API route:**
```typescript
// /app/api/retry/route.ts
POST /api/retry
  Body: { session_id: string }
  → POST http://localhost:8080/retry
  ← { session_id: string }
```

---

### 2.3 Context Window Pre-Check

**Intent:** Surface a warning before the LLM call rather than after a costly 400 error. The pre-check is a heuristic — it estimates, it does not guarantee.

**Known context limits (stored in config-adjacent lookup):**
```go
// /internal/llm/limits.go

var ModelContextLimits = map[string]int{
    // Anthropic
    "claude-opus-4-6":    200000,
    "claude-sonnet-4-6":  200000,
    "claude-haiku-4-5":   200000,
    // OpenAI
    "gpt-4o":             128000,
    "gpt-4o-mini":        128000,
    // Gemini
    "gemini-2.0-flash":   1000000,
    "gemini-2.5-pro":     1000000,
}

// Rough token estimation heuristic
func EstimateTokens(pdfBytes []byte) int {
    // Conservative estimate: 1 token per 3 bytes of PDF content
    // PDFs contain binary overhead — actual text token count is lower
    return len(pdfBytes) / 3
}
```

**Pre-check in Orchestrator (before ExplainerAgent call):**
```go
func (o *Orchestrator) checkContextWindow(pdf []byte) *ContextWarning {
    model := o.config.LLM.Model
    limit, known := llm.ModelContextLimits[model]
    if !known {
        return nil  // unknown model — skip check
    }

    estimated := llm.EstimateTokens(pdf)
    systemPromptTokens := 1000  // approximate
    maxOutputTokens := o.config.LLM.MaxTokens
    totalEstimated := estimated + systemPromptTokens + maxOutputTokens

    if totalEstimated > limit {
        return &ContextWarning{
            EstimatedTokens: totalEstimated,
            ModelLimit:      limit,
            Model:           model,
            Suggestion:      "Consider switching to Gemini (gemini-2.0-flash) for larger context support.",
        }
    }
    return nil
}
```

**Warning surfaced in UI (non-blocking):**
```typescript
// Shown in progress indicator if context warning exists
{contextWarning && (
  <div className="warning-banner">
    ⚠️ This paper may be too long for {contextWarning.model}.
    Proceeding anyway — {contextWarning.suggestion}
  </div>
)}
```

**Warning added to status response:**
```go
type StatusResponse struct {
    Stage          models.PipelineStage
    Iteration      int
    ReviewScore    float32
    ReviewPassed   bool
    ContextWarning *ContextWarning
    Error          string
    ErrorCode      string
    ErrorAction    string
    Recoverable    bool
}
```

---

### 2.4 Cost Estimator

**Intent:** Give the user visibility into what each run costs. Pricing is an estimate — provider pricing changes. The system must never present cost as exact.

**Pricing table (config-adjacent, not business logic):**
```go
// /internal/llm/pricing.go

type TokenPricing struct {
    InputPer1M  float64  // cost per 1M input tokens in USD
    OutputPer1M float64  // cost per 1M output tokens in USD
}

// Pricing as of June 2026 — update periodically
// Source: provider pricing pages
var ModelPricing = map[string]TokenPricing{
    "claude-sonnet-4-6":  {InputPer1M: 3.00,  OutputPer1M: 15.00},
    "claude-opus-4-6":    {InputPer1M: 15.00, OutputPer1M: 75.00},
    "gpt-4o":             {InputPer1M: 2.50,  OutputPer1M: 10.00},
    "gpt-4o-mini":        {InputPer1M: 0.15,  OutputPer1M: 0.60},
    "gemini-2.0-flash":   {InputPer1M: 0.10,  OutputPer1M: 0.40},
    "gemini-2.5-pro":     {InputPer1M: 1.25,  OutputPer1M: 10.00},
}

func EstimateCost(model string, inputTokens, outputTokens int) (float64, bool) {
    pricing, ok := ModelPricing[model]
    if !ok {
        return 0, false
    }
    cost := (float64(inputTokens)/1_000_000)*pricing.InputPer1M +
            (float64(outputTokens)/1_000_000)*pricing.OutputPer1M
    return cost, true
}
```

**Cost in result response:**
```go
type ResultResponse struct {
    Content          string
    VaultFile        string
    TotalTokensUsed  int
    InputTokens      int
    OutputTokens     int
    EstimatedCostUSD float64  // 0 if model not in pricing table
    CostKnown        bool     // false if model not in pricing table
}
```

**Cost display in UI:**
```typescript
// In success state
<div className="token-usage">
  Tokens used: {result.totalTokensUsed.toLocaleString()}
  {result.costKnown && (
    <span className="cost-estimate">
      · ~${result.estimatedCostUSD.toFixed(3)} estimated
      <span className="cost-note">(approximate — check your provider dashboard)</span>
    </span>
  )}
</div>
```

---

### 2.5 arXiv Retry Progress Indicator

**Intent:** When arXiv rate limits a request, the user sees the UI stall without explanation. A retry counter in the progress indicator makes the wait transparent.

**Updated session with retry tracking:**
```go
type PipelineSession struct {
    // ... existing fields ...
    ArxivRetryCount int  // current retry attempt (0 = no retries yet)
}
```

**DiscoveryTool updates session during retries:**
```go
func (t *DiscoveryTool) FetchPapers(ctx context.Context, session *models.PipelineSession, limit int) ([]models.Paper, error) {
    for attempt := 1; attempt <= maxRetries; attempt++ {
        resp, err := t.httpClient.Do(req)
        if err == nil && resp.StatusCode == http.StatusOK {
            session.ArxivRetryCount = 0
            return t.parseResponse(resp)
        }
        if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
            session.ArxivRetryCount = attempt
            // session updated for status polling
            slog.Warn("arXiv rate limited", "attempt", attempt, "backoff_ms", backoff)
            time.Sleep(time.Duration(backoff) * time.Millisecond)
            backoff *= 2
            continue
        }
        // other errors...
    }
    return nil, ErrArxivRateLimit()
}
```

**Updated status response:**
```go
type StatusResponse struct {
    // ... existing fields ...
    ArxivRetryCount int  // 0 means no retries; 1-3 means nth retry attempt
}
```

**Updated progress label in Next.js:**
```typescript
function getProgressLabel(status: PipelineStatus): string {
  if (status.stage === 'discovery' && status.arxivRetryCount > 0) {
    return `Connecting to arXiv (retry ${status.arxivRetryCount}/3)...`
  }
  // ... rest of stage labels
}
```

---

### 2.6 Structured Logging Audit

**Intent:** Audit every component for logging gaps and fill them. By end of Phase 6, every pipeline run must be fully reconstructable from logs alone.

**Logging standards (enforced across all components):**

```go
// Required fields on every log entry:
// - session_id (where applicable)
// - paper_id (from PDF fetch onwards)
// - component name for non-orchestrator logs

// Standard log patterns:

// Stage start
slog.Info("stage started", "session_id", sid, "paper_id", pid, "stage", stage)

// Stage complete
slog.Info("stage complete", "session_id", sid, "paper_id", pid,
    "stage", stage, "duration_ms", elapsed.Milliseconds())

// External call
slog.Info("external call complete", "session_id", sid,
    "service", "arxiv|anthropic|openai|gemini",
    "duration_ms", elapsed.Milliseconds())

// Retry
slog.Warn("retry", "session_id", sid, "service", svc,
    "attempt", n, "max", max, "backoff_ms", backoff, "error", err)

// LLM call (with token tracking)
slog.Info("llm call complete", "session_id", sid,
    "provider", provider, "model", model,
    "input_tokens", input, "output_tokens", output,
    "duration_ms", elapsed.Milliseconds())

// Pipeline complete
slog.Info("pipeline complete", "session_id", sid,
    "paper_id", pid,
    "total_tokens", tokens,
    "estimated_cost_usd", cost,
    "total_duration_ms", total.Milliseconds(),
    "review_iterations", iters,
    "review_passed", passed)

// Error
slog.Error("stage failed", "session_id", sid,
    "stage", stage, "code", errCode,
    "recoverable", recoverable, "cause", cause)
```

**Audit checklist (all must pass):**
- [ ] Config load — INFO with provider, model
- [ ] Server start — INFO with addr
- [ ] Discovery start/complete — INFO with session_id, count, duration_ms
- [ ] arXiv retry — WARN with attempt, backoff_ms
- [ ] Log check — INFO with unprocessed, returning
- [ ] PDF fetch start/complete — INFO with paper_id, size_bytes, duration_ms
- [ ] LLM call start/complete — INFO with provider, model, input_tokens, output_tokens, duration_ms
- [ ] LLM retry — WARN with attempt, backoff_ms, error
- [ ] Explainer generate start/complete — INFO with iteration, word_count, duration_ms
- [ ] Review start/complete — INFO with iteration, score, pass, duration_ms
- [ ] Vault write start/complete — INFO with vault_file, duration_ms
- [ ] Log update success/failure — INFO/WARN with paper_id
- [ ] Pipeline complete — INFO with all summary fields
- [ ] Any failure — ERROR with session_id, stage, code, recoverable, cause

---

### 2.7 README

**Intent:** The README is the product's front door for developers. It must be accurate, complete, and linear — a developer follows it top to bottom exactly once and ends up with a working system.

**Structure:**
```markdown
# ArXiv AI Paper Explainer Agent

## What it does
One paragraph. What the tool does, for whom, why it's useful.

## Prerequisites
- Node.js 22.x+
- Go 1.26.4+
- poppler-utils (`brew install poppler` / `apt install poppler-utils`)
- Obsidian (any version with a local vault)
- API key for at least one vision-capable LLM provider

## Setup

### 1. Clone the repo
### 2. Configure environment
   - Copy .env.example → .env
   - Fill in LLM_API_KEY
   - Set OBSIDIAN_VAULT_PATH
### 3. Review config.yaml (optional)
   - Full field reference table
### 4. Start the app
   make dev
### 5. Open the UI
   http://localhost:3000

## LLM Provider Setup
| Provider | API Key env var | Recommended model |
|---|---|---|
| Anthropic | LLM_API_KEY | claude-sonnet-4-6 |
| OpenAI | LLM_API_KEY | gpt-4o |
| Gemini | LLM_API_KEY | gemini-2.0-flash |

### Switching providers
1. Set llm.provider in config.yaml
2. Set llm.model to the correct model string
3. Update LLM_API_KEY in .env
4. Restart the backend: make dev

## Estimated Cost Per Paper
| Provider | Model | Typical cost |
|---|---|---|
| Anthropic | claude-sonnet-4-6 | $0.05–$0.20 |
| OpenAI | gpt-4o | $0.03–$0.15 |
| Gemini | gemini-2.0-flash | $0.01–$0.05 |

With max_review_iterations: 2 (default), multiply by ~3–4x for generation + review calls.
These are estimates — check your provider dashboard for exact usage.

## Configuration Reference
Full table of all config.yaml fields with type, default, and description.

## Troubleshooting
One section per error code from the F1 matrix.
Each section: what causes it, how to fix it.

## Project Structure
Brief map of /frontend and /backend directories.

## License
```

---

## 3. Data Model

No new entities. Phase 6 adds fields to existing types:

```go
// PipelineSession additions
type PipelineSession struct {
    // ... existing fields ...
    ErrorCode      string  // machine-readable error code for retry routing
    ErrorAction    string  // "retry" | "fix_config" | "fix_permissions" | "select_other"
    ArxivRetryCount int    // for retry progress indicator
    InputTokens    int     // accumulated input tokens
    OutputTokens   int     // accumulated output tokens (for cost estimation)
    ContextWarning *ContextWarning
}

type ContextWarning struct {
    EstimatedTokens int
    ModelLimit      int
    Model           string
    Suggestion      string
}
```

---

## 4. Data Flow

### Retry Flow

```
User sees error: "PDF download timed out. Try again."
    │
    ▼
User clicks "Try Again"
    │
    ▼
Next.js → POST /api/retry { session_id }
    │
    ▼
Go Orchestrator.HandleRetry()
    ├── Validates session is in "failed" stage
    ├── Routes by ErrorCode:
    │     ErrCodePDFTimeout → resume from PDF fetch
    ├── Resets error fields
    └── Spawns goroutine: runPipeline(ctx, session)
          (PDF is re-fetched; paper selection preserved)
    │
    ▼
Next.js resumes polling GET /status/:sessionId
    │
    ▼
Pipeline continues from fetching_pdf stage
```

### Context Pre-Check Flow

```
PDF downloaded successfully (session.PDF populated)
    │
    ▼
Orchestrator.checkContextWindow(session.PDF)
    ├── EstimateTokens(pdf) → ~45,000 tokens
    ├── ModelContextLimits["claude-sonnet-4-6"] = 200,000
    ├── totalEstimated = 45,000 + 1,000 + 8,000 = 54,000
    └── 54,000 < 200,000 → no warning
    │
    ▼
[If warning: attach to session, surface in next status poll]
    │
    ▼
ExplainerAgent.Generate() proceeds
```

---

## 5. Tech Stack

No new dependencies in Phase 6. All changes use existing packages.

**Phase 6 uses:**
- `log/slog` — structured logging (stdlib, established Phase 1)
- `encoding/json` — error response serialization
- `os` — error type detection (permission denied, disk full via `errors.Is`)
- `strings` — retry label construction

---

## 6. Integration Points

No new external integrations. Phase 6 hardens existing integrations:

**arXiv:** Retry count now propagated to session for UI display.
**LLM Providers:** Token counts now split into `input_tokens` and `output_tokens` for accurate cost estimation. All three providers must return separate input/output counts.
**Filesystem:** `os.IsPermission(err)` and disk full detection (`syscall.ENOSPC`) used for specific vault error messages.

---

## 7. Cross-Cutting Concerns

### Error Handling (complete)

Phase 6 closes all remaining error handling gaps:

- Every `PipelineError` has a machine-readable code, human message, and action
- Every error flows through `failSession` — no ad hoc error strings
- `failSession` logs `ERROR` with full context before updating session
- Retry routing is deterministic — error code maps to exactly one resume point
- Non-recoverable errors never show a retry button

### Observability (complete)

Phase 6 completes the logging audit. After Phase 6:
- Every pipeline run produces a complete log trace from start to finish
- Every LLM call is logged with input and output token counts
- Every retry is logged with attempt number, backoff, and cause
- Pipeline summary log includes total tokens, estimated cost, review outcome

### Security (complete)

Phase 6 adds no new attack surface. Existing security measures verified:
- API keys never appear in logs (verified across all slog calls)
- Vault path still validated before every write
- Error messages never expose internal paths or stack traces to the UI

### Developer Experience (complete)

- `make dev` verified on macOS and Linux
- README tested by following it on a fresh clone
- `.env.example` updated with all fields added in Phases 2–5
- All placeholder files from Phase 1 now contain real implementations

---

## 8. Risks & Tradeoffs

| ID | Risk/Tradeoff | Severity | Mitigation |
|---|---|---|---|
| R1 | Token pricing table becomes stale as providers change pricing | Low | Pricing table is in one file with a source comment and date. README notes pricing is approximate. Cost display includes "check your provider dashboard" caveat. |
| R2 | Context window pre-check heuristic is inaccurate for non-text-heavy PDFs | Low | Pre-check is advisory only — never blocks the pipeline. Surfaces a warning, not an error. Actual 400 from provider is caught and mapped to the specific "too long" error message. |
| R3 | Retry resumes from stage but PDF may be gone if process restarted | Low | `HandleRetry` checks `len(session.PDF) == 0` and re-fetches if needed. In-memory PDF loss is handled gracefully. |
| T1 | README maintenance burden as config evolves | Accepted | Config reference table is generated from config struct comments in a future enhancement. For now, manual update on each config change is the process. |
| T2 | Cost estimation is approximate — could mislead budget-conscious users | Accepted | Clear "approximate" labelling and "check provider dashboard" note in UI and README. Never presented as exact. |

---

## Exit Criteria

All of the following must be true for the product to be considered complete:

### Error Handling
- [ ] Every error scenario from the F1 matrix produces the correct UI message — verified manually
- [ ] All recoverable errors show a "Try Again" button
- [ ] All non-recoverable errors show a specific action (no generic "something went wrong")
- [ ] Retry resumes from the correct stage for all recoverable error codes
- [ ] No internal paths, stack traces, or technical details appear in user-facing error messages

### Observability
- [ ] Structured logging audit checklist passes — all 20+ event types produce correct log entries
- [ ] API keys never appear in any log entry — verified by log review
- [ ] Pipeline complete log includes: total_tokens, estimated_cost_usd, total_duration_ms, review_iterations, review_passed
- [ ] Any failed run is fully reconstructable from logs alone

### Cost & Context
- [ ] Token usage displayed correctly in success state
- [ ] Estimated cost displayed for all models in pricing table
- [ ] Context window warning shown for papers estimated to exceed model limit
- [ ] Warning is non-blocking — pipeline proceeds after warning

### Developer Experience
- [ ] README: fresh setup from clone to running in under 10 minutes (timed)
- [ ] README: all config fields documented with defaults
- [ ] README: all F1 error scenarios covered in Troubleshooting section
- [ ] `.env.example` contains all required and optional keys with inline documentation
- [ ] `make dev` works on macOS and Linux without modification

### End-to-End Validation
- [ ] Full pipeline run completes without errors: trigger → select → generate → review → revise → save
- [ ] Generated note opens correctly in Obsidian with all frontmatter fields rendered
- [ ] `processed.json` updated correctly after successful run
- [ ] Paper does not re-surface in second discovery run
- [ ] Full pipeline verified with Anthropic Claude
- [ ] Full pipeline verified with OpenAI GPT-4o
- [ ] Full pipeline verified with Google Gemini
- [ ] `max_review_iterations: 0` produces correct behaviour (no review, immediate save)
- [ ] `max_review_iterations: 2` produces correct behaviour (up to 2 review cycles)
