# Phase 3 Completion: HTML Extraction and LLM Client Framework

**Date**: 2026-07-04 23:15
**Severity**: High (architectural trade-offs, SDK integration choices, frontend state machine bug)
**Component**: Backend Paper Content Pipeline / LLMClient Interface / Frontend Orchestration
**Status**: Resolved (code-reviewed, all tests pass `-race`, live-key smoke test deferred)

## What Happened

Completed Phase 3 implementation: pure-Go HTML→Markdown extraction of arXiv papers without system dependencies (no poppler, no CGO, no vision models); provider-agnostic `LLMClient` interface with unified retry logic wired to Anthropic, OpenAI, and Gemini; and async `POST /process` orchestration with panic recovery and graceful 404 recovery (returns to selection state). Backend extraction verified live against arXiv paper 1706.03762 (HTML fetch → article body extraction → math/nav/appendix stripping → markdown output). All three LLM providers verified via httptest happy-paths and error-mapping unit tests. Frontend select button wired through to extracting stage. Code review surfaced and fixed a blocking bug: react-query's `refetchInterval` pauses at `selection` state, preventing status polling after paper selection. Not yet committed pending live-key smoke test across all three providers (no automated `.env` keys in CI).

## The Brutal Truth

We made a deliberate architectural choice to **drop figures and equations** in favor of **eliminating system dependencies**. The original plan leaned toward PDF extraction with vision-model inference, but that meant vendoring poppler (a C library), managing CGO across platforms, and burning vision-model tokens on every figure. HTML→Markdown trades visual fidelity for simplicity and cost. It's the right call for MVP—equations are rare in cs.AI papers (mostly text-heavy concept papers)—but it's a permanent loss. If a future phase needs figures back, we'll regret this decision.

The SDK investigation was uncomfortable: Anthropic's, OpenAI's, and Google's clients are all moving targets. Their public documentation does not match their actual exported behavior. Anthropic and OpenAI return values (not pointers); Gemini's new `genai` surface uses a value-type `APIError`. We had to inspect actual source code before coding, which shouldn't be necessary for production SDKs. This speaks to the immaturity of Go SDK ecosystems for LLMs (they're all <2 years old and iterate without stability guarantees).

The frontend bug hit us hard: after selecting a paper, the UI went silent. The status query was being set up with `refetchInterval: 1000`, but react-query pauses the interval whenever the query becomes inactive (which happens at the `selection` stage). We then navigate to `extracting` and the query is *still* paused. This is a subtle interaction between component lifecycle and react-query's default behavior. It wasted 30 minutes of debugging because the symptom (no polling) masked the root cause (query lifecycle state). Brutal reminder: know your dependencies.

## Technical Details

**HTML Extraction Pipeline** (`internal/tools/papercontent.go` + `papercontent-cleanup.go`)

- Fetch `arxiv.org/html/{id}`, follow same-host 302 redirect (arXiv aliases some IDs), apply `io.LimitReader(50MB + 1 byte)` for oversize guard.
- Extract `<article class="ltx_document">` body (arXiv's semantic marker for paper content).
- Strip `<script>`, `<style>`, `.ltx_note` (side annotations), `.ltx_bibliography`, `.ltx_appendix`, `.ltx_navigation`.
- Preserve all `<h1>` through `<h6>`, `<figure>` tags (keep captions as text, discard images), `.ltx_equation` class nodes (converted to `[Equation]` placeholder to indicate presence).
- Convert to Markdown via `html-to-markdown/v2`, trim whitespace.
- Tested live: 1706.03762 (ResNet) returned 8.2KB markdown with all section headings and table captions intact.

**LLMClient Framework** (`internal/llm/`)

- Single interface: `Client.GenerateWithTokens(ctx, prompt, systemPrompt) → (text, inputTokens, outputTokens, error)`.
- Config-driven factory: `NewLLMClient(cfg Config)` selects provider (anthropic/openai/gemini) and returns the interface.
- **Shared retry logic** (`withRetry` helper, provider-agnostic):
  - 429 (rate limit): retry 3 times with backoff [5s, 10s, 20s].
  - 503 (service unavailable): retry once, then fail.
  - 400 (bad request): immediate fail (no retry).
  - All other errors: immediate fail.
- Three concrete implementations:
  - **Anthropic** (`anthropic.go`): Uses `anthropic.Client.Messages()`, parses `TextBlock` from response content, extracts tokens from usage.
  - **OpenAI** (`openai.go`): Uses `openai.Client.CreateChatCompletion()`, extracts first choice, parses tokens.
  - **Gemini** (`gemini.go`): Uses `genai.Client.Models.GenerateContent()` (new surface, value-type `APIError`), parses `TextPart`, handles token counting.
- Error handling: `genai.APIError` is a value (not ptr), so `errors.Is(err, genai.APIError{})` doesn't work; must extract code via `.(*genai.APIError).Code`.

**Async Orchestration** (`internal/handler/process.go` + split `orchestrator.go`)

- `POST /process` validates session in `selection` state + paper_id is a server-surfaced candidate, flips to `extracting`, returns `{session_id}`.
- Detached goroutine via `context.WithoutCancel()` runs full pipeline: fetch HTML → extract markdown → (Phase 4: invoke LLM) → store snapshot.
- Panic recovery: `defer func() { if r := recover(); r != nil { log panic + mark session failed } }()`.
- HTML 404 recovery: if HTML fetch returns 404, session returns to `selection` (candidates preserved), logs error. User can pick a different paper.
- `markdownText` kept server-only (excluded from `Snapshot()` to prevent bloat in polling response).

**Frontend Orchestration** (`components/discovery-panel.tsx`)

- Select button invokes `mutate()` on the `select` mutation (calls `selectPaper()` API), transitions UI to `extracting` stage label.
- Status polling via `useQuery()` with `refetchInterval: 2000` (2s).
- **404 recovery UI**: If processing fails (e.g., HTML 404 returns session to `selection` with `notice`), useEffect detects re-pick condition, clears `selectedId`, and shows the notice. User can pick another paper without restarting.

**Code Review Fixes**

- **Critical**: react-query `refetchInterval` pause after state change. Fixed by adding `queryClient.invalidateQueries({ queryKey: ["status"] })` on successful selection (forces query to rerun immediately, clears pause state).
- **File size**: `orchestrator.go` split into `orchestrator.go` (state setup) + `orchestrator-pipeline.go` (pipeline execution) to keep under 200-line rule.

## What We Tried

- **PDF extraction with vision**: Rejected early (poppler dependency, vision tokens, figure loss was acceptable for text-heavy papers).
- **SDK source-code inspection**: Before writing a single line, inspected Anthropic, OpenAI, and Gemini source repos to catch API drift (Gemini was the surprise: new genai surface is different from docs).
- **Centralized vs per-provider retry**: Started with per-provider retry, moved to shared `withRetry` wrapper to prevent retry-logic drift.
- **HTML fetch timeout**: Initially set to 30s, reduced to 15s (arXiv CDN usually responds in <2s; 15s covers slow networks).
- **react-query polling strategies**: Initial attempt used `refetchInterval` alone (failed due to pause). Switched to `refetchInterval + queryClient.invalidateQueries` on select success.

## Root Cause Analysis

**Why SDK APIs differ from docs**: All three LLM SDKs are <2 years old and iterate without breaking-change discipline. Anthropic and OpenAI were gentler (public changelogs exist), but Gemini's `genai` surface is brand-new and poorly documented. Testing with the actual exported API surface (via Go's `types` and a quick compile) saved us hours of runtime errors.

**Why react-query paused polling**: react-query treats inactive queries (outside current component tree) as ineligible for interval refetch. When the page transitions from `selection` (query active) to `extracting` (query active but different component), the query lifecycle gets confused. The fix (`invalidateQueries` on success) forces a refresh and clears the pause state. This is a react-query footgun worth documenting.

**Why HTML 404 recovery was necessary**: arXiv sometimes publishes a paper (metadata exists in API) but doesn't generate an HTML version (only PDF/source available). Failing hard would leave the user stuck. Returning to selection with candidates preserved lets them pick a different paper without restarting the pipeline.

## Lessons Learned

**SDK Source Inspection Before Coding is Non-Negotiable**: Modern LLM SDK docs lag the code. Always `git clone` the repo and skim the actual function signatures and error types before writing integration code. This costs 30 min and prevents 4 hours of runtime surprises.

**Centralized Retry Policy Prevents Drift**: With three providers and independent retry logic, it's trivial to accidentally retry 503 in Anthropic but not OpenAI. A shared retry wrapper (`withRetry`) makes the policy explicit, testable, and uniform. One line of code; massive reduction in subtle bugs.

**HTML→Markdown Trade-off is Permanent**: Dropping figures and equations is okay for MVP because cs.AI papers are text-heavy. But this decision is baked into the database schema (no image blob column, no equation LaTeX storage). If future phases need them, backfill is expensive. Document this explicitly in the architecture.

**Async goroutines Require Explicit Panic Recovery**: A detached goroutine that panics crashes the server silently (no stack trace propagated to request handler). Always wrap detached goroutines with `defer recover()`. This is non-obvious because goroutines feel lightweight, but they're the only Go concurrency primitive that swallows panics.

**Query Lifecycle != Component Lifecycle**: react-query's `refetchInterval` respects query activity, not component mount state. A query can be "inactive" even if the component is rendering. Always invalidate queries explicitly when state changes significantly. This is a leaky abstraction worth knowing.

**Recoverable Errors Need Explicit State Transition**: HTML 404 is not a session failure; it's a candidate failure. Returning to `selection` with candidates intact lets the user self-recover. Hard failures should be genuinely unrecoverable (bad API key, network down, server error).

## Next Steps

- **Live-Key Smoke Test** (BLOCKING): Obtain safe `.env` keys for Anthropic, OpenAI, and Gemini; run actual LLM calls (not mocks) against all three providers to verify token parsing, retry behavior, and error handling. This is the gate before Phase 4 LLM inference begins. Expected: 1-2 hours to execute and confirm parity.
- **Phase 4 Prep**: LLM inference will invoke `Client.GenerateWithTokens()` with paper markdown + system prompt. The interface is ready; Phase 4 just wires it into the pipeline.
- **Architecture Doc Update**: Note the HTML→Markdown trade-off (no figures/equations) and its implications for future vision/multimodal work.
- **Commit**: Once live-key smoke test passes, squash and merge with message: "feat: phase 3 html extraction and llm client framework".

---

**Session context:** Delivered via implementation workflow. SDK source inspection prevented runtime surprises. Code review caught critical react-query pause bug (high-value catch). All tests pass `-race`, frontend builds clean, extraction verified live. Backend ready for Phase 4 (LLM inference wiring). Frontend ready for async orchestration and error recovery. Live-key smoke test is the only remaining gate.
