# Phase 04 — Provider Implementations (Anthropic / OpenAI / Gemini)

**Context:** `docs/phase3/prd.md` §2.6–2.8, §5 (SDKs), §6 (provider APIs) · `brainstorm-summary.md` §4.2, §4.5
**Priority:** Critical · **Status:** complete · **Depends on:** 03 · **Effort:** ~L

## Overview
Three concrete `LLMClient` implementations, one file each, all **text-only**: send
`SystemPrompt` + (`DocumentText` prepended to / as a text block alongside `UserPrompt`), map SDK
errors to the shared sentinels, return input/output tokens separately. Each `Complete` runs inside
the shared `withRetry` from Phase 03. No base64, no image blocks, no vision.

## Key insights (locked decisions)
- **DocumentText as a text block/part**, prefixed `"Paper content:\n\n"`, added before the
  user prompt. Same shape across all three providers → consistent behavior.
- **Gemini uses the NEW `google.golang.org/genai` surface**: `client.Models.GenerateContent(...)`,
  NOT the deprecated `GenerativeModel`/`SetTemperature` API the PRD sketch shows. The PRD §2.8
  snippet is stale — follow the current genai client. Read the pkg docs before coding.
- **Error mapping is the provider's job**; the retry loop is shared. Map SDK HTTP status /
  typed errors → `ErrLLMRateLimit` (429), `ErrLLMBadRequest` (400), `ErrLLMUnavailable` (5xx),
  `ErrLLMTimeout` (deadline). Anything unmapped → `ErrLLMUnavailable` (retryable-safe default).
- **API key from `cfg.APIKey`** (already loaded from `.env` in Phase 1 config). Optional
  `cfg.BaseURL` → set the SDK's base URL when non-empty (custom endpoints / proxies).
- Each file < 200 lines. All three SDKs compiled in regardless of active provider (accepted, T3).

## Requirements (PRD F4, F5, F6)
- `newAnthropicClient/newOpenAIClient/newGeminiClient(cfg *config.LLMConfig) (LLMClient, error)`.
- Each `Complete` builds a text-only request, applies `MaxTokens`/`Temperature`, calls the SDK
  inside `withRetry`, returns `CompletionResponse{Content, InputTokens, OutputTokens}`.
- Per-call context deadline from `cfg.RequestTimeoutSec` (LLM calls are slow; distinct from the
  arXiv timeout) — set via `context.WithTimeout` inside `Complete` or at the caller; document choice.

## Related code files
**Create:**
- `backend/internal/llm/anthropic.go` — `client.Messages.New`, System + user TextBlocks, usage tokens.
- `backend/internal/llm/openai.go` — `client.Chat.Completions.New`, System + text content parts, usage.
- `backend/internal/llm/gemini.go` — `client.Models.GenerateContent`, system instruction + text parts,
  `UsageMetadata` tokens.
- `backend/internal/llm/anthropic_test.go`, `openai_test.go`, `gemini_test.go` — where the SDK allows
  a custom `BaseURL`/httpClient, point at an `httptest.Server` returning canned success + 429 + 400
  bodies; assert content, token parsing, and sentinel mapping. If an SDK can't be redirected, unit-
  test only the error-mapping + request-building helpers (extract them so they're testable).
**Modify:**
- `backend/go.mod` / `go.sum` — add `github.com/anthropics/anthropic-sdk-go`,
  `github.com/openai/openai-go`, `google.golang.org/genai`.
- `backend/internal/llm/client.go` — only if Phase 03 left constructors stubbed (fill the switch).

## Design detail
```go
// anthropic.go (shape — verify exact SDK types before coding)
func (c *anthropicClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
    var out CompletionResponse
    err := withRetry(ctx, func() error {
        blocks := []anthropic.ContentBlockParamUnion{}
        if req.DocumentText != "" {
            blocks = append(blocks, anthropic.NewTextBlock("Paper content:\n\n"+req.DocumentText))
        }
        blocks = append(blocks, anthropic.NewTextBlock(req.UserPrompt))
        resp, err := c.sdk.Messages.New(ctx, anthropic.MessageNewParams{
            Model: anthropic.Model(c.cfg.Model), MaxTokens: int64(req.MaxTokens),
            Temperature: anthropic.Float(float64(req.Temperature)),
            System: []anthropic.TextBlockParam{{Text: req.SystemPrompt}},
            Messages: []anthropic.MessageParam{{Role: anthropic.MessageParamRoleUser, Content: blocks}},
        })
        if err != nil { return mapAnthropicErr(err) } // → shared sentinel
        out = CompletionResponse{Content: firstText(resp), InputTokens: int(resp.Usage.InputTokens), OutputTokens: int(resp.Usage.OutputTokens)}
        return nil
    })
    return out, err
}
```
> **All three snippets in the PRD are illustrative.** SDK type names drift — confirm current
> signatures (Messages/ChatCompletion/GenerateContent params, usage field names) against the
> vendored module docs before writing. Gemini especially: `client.Models.GenerateContent`, build
> `[]*genai.Content` / `genai.Text(...)`, read `resp.UsageMetadata.PromptTokenCount` /
> `CandidatesTokenCount`.
>
> **Error mapping helper per provider** (`mapAnthropicErr`, `mapOpenAIErr`, `mapGeminiErr`):
> extract so it's unit-testable without a live API. Inspect the SDK's typed error / status code.

## Implementation steps
1. `go get` the three SDKs; `go build` to pull types; `go mod tidy`.
2. `anthropic.go`: constructor (APIKey, optional BaseURL), `Complete` via `withRetry`, `mapAnthropicErr`.
3. `openai.go`: same shape with Chat Completions text parts + `Usage.PromptTokens/CompletionTokens`.
4. `gemini.go`: NEW genai client; `client.Models.GenerateContent`; `UsageMetadata` tokens.
5. Fill `NewLLMClient` switch (if stubbed in 03).
6. Provider tests: httptest-backed where the SDK supports a custom base URL; else test the extracted
   mapping/builder helpers.
7. Each file < 200 lines; `go build ./...` + `go test ./internal/llm/...` green.

## Todo
- [x] add 3 SDKs to go.mod/go.sum; `go mod tidy`
- [x] `anthropic.go` (text blocks, usage tokens, error map) via `withRetry`
- [x] `openai.go` (text content parts, usage tokens, error map) via `withRetry`
- [x] `gemini.go` NEW surface `client.Models.GenerateContent` (NOT GenerativeModel)
- [x] per-provider error-mapping helpers → shared sentinels (unit-tested)
- [x] optional `cfg.BaseURL` honored per provider (custom endpoints)
- [x] provider tests (httptest or extracted-helper); each file < 200 lines
- [x] build + tests green

## Success criteria
- Each provider returns valid `Content` + separate input/output token counts for a canned success.
- 429 → `ErrLLMRateLimit` (retried by `withRetry`); 400 → `ErrLLMBadRequest` (immediate); 5xx →
  `ErrLLMUnavailable`.
- Switching `llm.provider` in config routes to the right client with zero code change (verified
  in Phase 07).
- No vision/image code path exists in any provider file.

## Risk Assessment
| Risk | L×I | Mitigation |
|---|---|---|
| Stale PRD SDK snippets (esp. Gemini `GenerativeModel`) | High×Med | Explicitly use new genai surface; verify all three against current module docs before coding. |
| SDK doesn't allow custom BaseURL → hard to httptest | Med×Med | Extract request-builder + error-mapper as pure funcs; unit-test those; smoke-test live in Phase 07. |
| Token field names differ per SDK version | Med×Low | Pin SDK versions in go.mod; read usage struct from the vendored version. |
| Binary size from 3 SDKs | Low×Low | Accepted (T3, ~5–10MB); any provider is one config change away. |

## Backwards compatibility
Net-new files in a net-new package + new deps. No Phase 2 code touched. `go.mod` grows only.

## Rollback
Delete provider files; `go mod tidy` to drop SDKs; revert `NewLLMClient` switch to error-only.

## Security
- API keys sourced from `cfg.APIKey` (`.env`); NEVER logged, NEVER returned to the frontend.
- Sentinels carry no payload → safe to log. Log provider+model only (mirror `main.go`).
- Optional `BaseURL` lets custom/self-hosted endpoints avoid third-party key exposure.

## Next Steps
**Blocks Phase 05** (orchestrator constructs an `LLMClient` and holds it for Phase 4). File
ownership: this phase owns `internal/llm/{anthropic,openai,gemini}.go` + their tests; shares only
`go.mod` (coordinate with Phase 02's single dependency add) and `client.go`'s switch (owned by 03).
