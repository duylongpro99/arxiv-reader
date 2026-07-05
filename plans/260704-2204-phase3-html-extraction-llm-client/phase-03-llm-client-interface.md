# Phase 03 — LLMClient Interface + Shared Retry

**Context:** `docs/phase3/prd.md` §2.5, §4 (LLM call flow), §6, §7 (error table) · `brainstorm-summary.md` §4.2
**Priority:** Critical · **Status:** complete · **Depends on:** 01 · **Effort:** ~M

## Overview
The system's most important abstraction: one `LLMClient` interface every agent (Phase 4/5) calls,
fully decoupled from any provider. Text-only — `DocumentText` (Markdown), never images. This phase
delivers the interface, request/response types, the config-driven provider selector, the shared
retry logic, and the shared sentinel errors. Concrete providers land in Phase 04.

## Key insights (locked decisions)
- **Text-only, no vision.** `CompletionRequest` has `DocumentText string`, NOT `PageImages`.
  The old PRD's `vision.go`, `KnownVisionModels`, `ValidateVisionSupport` are **not created**.
  Any text-capable model is valid → no startup vision validation anywhere.
- **Retry policy is shared, not per-provider** (DRY): 429 → backoff 5s→10s→20s, max 3 attempts;
  503 → retry once after 5s; 400 → surface immediately (no retry); timeout → surface with duration.
  Providers map their SDK errors to the shared sentinels; the retry wrapper does the rest.
- **Tokens returned separately** (`InputTokens`, `OutputTokens`) — every provider exposes both.
- Selector returns an **error** for unknown providers (with "implement LLMClient" hint), never
  a nil client. Note: `config.validProviders` already whitelists anthropic/openai/gemini, so an
  invalid provider fails at config load — the selector's default is defense-in-depth.

## Requirements (PRD F4, F5, F6)
- `LLMClient interface { Complete(ctx, CompletionRequest) (CompletionResponse, error) }`.
- `CompletionRequest{ SystemPrompt, UserPrompt, DocumentText string; MaxTokens int; Temperature float32 }`.
- `CompletionResponse{ Content string; InputTokens, OutputTokens int }`.
- `NewLLMClient(cfg *config.LLMConfig) (LLMClient, error)` — switch on `cfg.Provider`.
- Shared sentinels: `ErrLLMRateLimit`, `ErrLLMBadRequest`, `ErrLLMUnavailable`, `ErrLLMTimeout`.
- Shared retry helper reusable by all three providers.

## Related code files
**Create:**
- `backend/internal/llm/client.go` — interface, DTOs, `NewLLMClient` selector, sentinels.
- `backend/internal/llm/retry.go` — shared retry wrapper + backoff (own file to keep client.go small
  and let providers import one helper). Consider mirroring `sleepCtx` semantics (ctx-aware sleep).
- `backend/internal/llm/retry_test.go` — 429×3-then-success, 429 exhausted → `ErrLLMRateLimit`,
  503 retried once, 400 immediate (no retry), ctx-cancel during backoff.
**Modify:** none in this phase (config fields landed in Phase 01).

## Design detail
```go
// client.go
type LLMClient interface {
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

type CompletionRequest struct {
    SystemPrompt string
    UserPrompt   string
    DocumentText string   // paper Markdown (from PaperContentTool). NOT images.
    MaxTokens    int
    Temperature  float32
}

type CompletionResponse struct {
    Content      string
    InputTokens  int
    OutputTokens int
}

var (
    ErrLLMRateLimit   = errors.New("LLM provider rate limit exceeded")
    ErrLLMBadRequest  = errors.New("LLM bad request — check model name and config")
    ErrLLMUnavailable = errors.New("LLM provider unavailable")
    ErrLLMTimeout     = errors.New("LLM request timed out")
)

func NewLLMClient(cfg *config.LLMConfig) (LLMClient, error) {
    switch cfg.Provider {
    case "anthropic": return newAnthropicClient(cfg) // Phase 04
    case "openai":    return newOpenAIClient(cfg)     // Phase 04
    case "gemini":    return newGeminiClient(cfg)     // Phase 04
    default:
        return nil, fmt.Errorf("unknown LLM provider %q — implement the LLMClient interface for custom providers", cfg.Provider)
    }
}
```
```go
// retry.go — shared across providers
// attempt fn; classify returns one of the sentinels (or nil on success).
// 429 → backoff 5,10,20s (max 3 attempts); 503 → one retry after 5s; 400 → return now.
func withRetry(ctx context.Context, fn func() error) error
```
> **Wiring note:** in Phase 04, each provider's `Complete` body is `withRetry(ctx, func() error {
> ... call SDK, map error to sentinel ... })`. The mapping (SDK error → sentinel) lives in the
> provider file; the *decision to retry* lives here. Clean separation, single retry policy.
>
> **backoff testability:** parameterize the base unit (like DiscoveryTool's `backoffUnit`) so
> `retry_test.go` runs in milliseconds, not tens of seconds.

## Implementation steps
1. `client.go`: interface, `CompletionRequest`/`CompletionResponse`, sentinels, `NewLLMClient`.
   (Provider constructors are declared/called; their bodies arrive in Phase 04 — keep this phase
   compiling by landing 03+04 together, or stub constructors returning `ErrLLMUnavailable` until 04.)
2. `retry.go`: `withRetry` with 429/503/400 policy + ctx-aware, testable backoff.
3. `retry_test.go`: cover every branch with a fake `fn` returning scripted sentinels.
4. `go build ./...` (with Phase 04) + `go test ./internal/llm/...` green.

## Todo
- [x] `client.go`: interface + DTOs (text-only) + sentinels
- [x] `NewLLMClient` selector (unknown → error with hint)
- [x] `retry.go`: `withRetry` (429 ×3 / 503 ×1 / 400 now / timeout), testable backoff
- [x] `retry_test.go`: all branches, ctx-cancel
- [x] NO vision.go / KnownVisionModels / ValidateVisionSupport
- [x] build + tests green

## Success criteria
- Interface + DTOs are text-only (no image/vision types anywhere in the package).
- `withRetry` honors the PRD §6 policy exactly; proven by `retry_test.go` with no real sleeps.
- Unknown provider → descriptive error, never a nil client panic downstream.

## Risk Assessment
| Risk | L×I | Mitigation |
|---|---|---|
| Compile ordering: selector references Phase 04 constructors | High×Low | Land 03+04 as one buildable unit, or stub constructors in 03 then fill in 04. Documented above. |
| Retry policy drift between providers | Med×Med | Single `withRetry` in this package; providers only classify errors, never loop. |
| Real backoff makes tests slow/flaky | Med×Low | Parameterized backoff unit (ms in tests), mirroring DiscoveryTool. |

## Backwards compatibility
Net-new package. No existing code depends on it yet (orchestrator wires it in Phase 05). Zero impact
on Phase 2 behavior.

## Rollback
Delete `internal/llm/client.go`, `retry.go`, `retry_test.go`. No state/schema/config impact (LLM
config fields from Phase 01 are harmless if unused).

## Security
- No API key handling here beyond passing `cfg.LLMConfig` to provider constructors (Phase 04).
- Sentinels are value-free (no request/response bodies) → safe to log.

## Next Steps
**Blocks Phase 04** (providers implement this interface + import the sentinels/retry). Independent of
Phase 02. File ownership: this phase owns `internal/llm/client.go`, `retry.go`, `retry_test.go`;
Phase 04 owns the provider files in the same package (no overlapping files).
