# Phase 02 — PaperContentTool (HTML → Markdown)

**Context:** `docs/phase3/prd.md` §2.4, §4 (Fetch/Convert flow), §6 (arXiv HTML) · `brainstorm-summary.md` §4.1, §5
**Priority:** Critical · **Status:** complete · **Depends on:** 01 · **Effort:** ~L

## Overview
One responsibility: given a bare arXiv ID, return clean Markdown text. Fetch
`{arxiv_html_base_url}/{id}` (client follows the same-host redirect to the versioned URL),
read the body under an `io.LimitReader` cap, convert LaTeXML HTML → Markdown with pure-Go
`html-to-markdown/v2`, then apply minimal cleanup. Nothing outside this tool knows how arXiv
serves HTML. Reuses DiscoveryTool's retry/backoff + User-Agent (same domain, same politeness).

## Key insights (locked decisions)
- **`Paper.ID` is already bare** (version stripped by `extractArxivID`, e.g. `2312.00752`).
  Verified: `GET arxiv.org/html/2312.00752` → 200 after same-host redirect to `.../2312.00752v2`.
  Go's `http.Client` follows that automatically — no version handling needed here.
- **Reuse, don't reinvent** the retry/backoff loop, `sleepCtx`, `backoffFor`, and User-Agent from
  `discovery.go`. Same transient rules: retry 429/5xx/network, never retry permanent 4xx.
- **404 is special**: return `ErrPaperHTMLNotFound` (permanent, but the orchestrator treats it as
  *recoverable re-pick*, not a hard fail).
- **`<math>` nodes → stripped** to kill MathML noise. `alttext` (original LaTeX) is a future seam —
  do NOT wire it now, just don't make it hard to add later.
- **Keep figure/table captions** (context for dropped diagrams); trim bibliography + appendix.
- File budget: `papercontent.go` < 200 lines. If cleanup grows, split helpers into
  `papercontent-cleanup.go` (same package).

## Requirements (PRD F2, F3)
- `NewPaperContentTool(cfg *config.AgentConfig) *PaperContentTool` — http client with
  `RequestTimeoutSec` timeout (mirror `NewDiscoveryTool`).
- `FetchMarkdown(ctx, arxivID string) (string, error)`:
  - `GET cfg.ArxivHTMLBaseURL + "/" + arxivID`, `User-Agent: cfg.UserAgent`.
  - Retry transient failures with the discovery backoff pattern; abort on `ctx` cancel.
  - `resp.Body` → `io.LimitReader(body, cfg.MaxContentBytes)` → `io.ReadAll`.
  - Convert HTML → Markdown; apply cleanup; return trimmed Markdown.
- Sentinels: `ErrPaperHTMLNotFound` (404), `ErrPaperHTMLFailed` (network/convert/other),
  `ErrPaperHTMLTimeout` (timeout — or map ctx deadline to it).
- Structured logs: `html fetch started/complete` (`html_bytes`, `duration_ms`), `markdown
  conversion complete` (`markdown_bytes`), `paper html not found` (WARN). NEVER log raw HTML
  (CLAUDE.md: persisted/logged state = extracted text + metadata only).

## Related code files
**Create:**
- `backend/internal/tools/papercontent.go` — `PaperContentTool`, `NewPaperContentTool`,
  `FetchMarkdown`, fetch-with-retry, sentinels.
- `backend/internal/tools/papercontent-cleanup.go` — cleanup helpers (only if papercontent.go
  approaches 200 lines).
- `backend/internal/tools/papercontent_test.go` — `httptest.Server` fixtures: happy path (200 +
  LaTeXML sample → asserts headings/captions kept, nav/math/appendix stripped), 404 →
  `ErrPaperHTMLNotFound`, 5xx-then-200 retry, oversized body → `ErrPaperHTMLFailed`, ctx cancel.
**Modify:**
- `backend/go.mod` / `go.sum` — add `github.com/JohannesKaufmann/html-to-markdown/v2`.

## Design detail
```go
var (
    ErrPaperHTMLNotFound = errors.New("paper HTML not found on arXiv (404)")
    ErrPaperHTMLFailed   = errors.New("failed to fetch or convert paper HTML")
    ErrPaperHTMLTimeout  = errors.New("HTML fetch timed out")
)

type PaperContentTool struct {
    cfg         *config.AgentConfig
    httpClient  *http.Client
    backoffUnit time.Duration // time.Second in prod; tests shrink it (mirror DiscoveryTool)
}

func (t *PaperContentTool) FetchMarkdown(ctx context.Context, arxivID string) (string, error) {
    htmlBytes, err := t.fetchHTMLWithRetry(ctx, t.cfg.ArxivHTMLBaseURL+"/"+arxivID) // 404 → ErrPaperHTMLNotFound
    if err != nil { return "", err }
    md, err := convertToMarkdown(htmlBytes) // html-to-markdown/v2
    if err != nil { return "", ErrPaperHTMLFailed }
    return cleanup(md), nil
}
```
Cleanup (minimal, LLM-tolerant — order matters, comment each step):
1. Strip LaTeXML nav/header/footer chrome (either as HTML pre-pass by removing nodes, or as a
   Markdown post-pass on known boilerplate markers — choose the simpler, document why).
2. Strip `<math>` nodes (MathML noise) — pre-conversion is cleanest.
3. Trim bibliography + appendix (cut from the "References"/"Appendix" heading onward).
4. **Keep** figure/table caption text.
5. Collapse 3+ blank lines → 1; trim leading/trailing whitespace.

> **Redirect handling:** default `http.Client` follows same-host redirects — leave `CheckRedirect`
> at default. Do NOT set it to block redirects or the versioned-URL fetch breaks.
>
> **LimitReader semantics:** `io.LimitReader` silently truncates at the cap — a doc *at* the cap
> is indistinguishable from a truncated one. Read `cap+1`; if `len == cap+1`, treat as oversized
> → `ErrPaperHTMLFailed` (recoverable "too large"). Document this off-by-one guard inline.

## Implementation steps
1. `go get github.com/JohannesKaufmann/html-to-markdown/v2`; confirm pure-Go (no CGO) build.
2. Sentinels + struct + constructor (copy DiscoveryTool's client/backoff wiring).
3. `fetchHTMLWithRetry`: reuse discovery's transient/permanent + backoff logic; 404 →
   `ErrPaperHTMLNotFound`; body under `LimitReader` with the +1 oversize guard.
4. `convertToMarkdown` using the v2 converter API (verify exact signature against the vendored
   lib in `node_modules`-equivalent — read the pkg docs before coding).
5. `cleanup` helpers (split file if > ~180 lines).
6. Structured logging at fetch/convert transitions (no raw HTML).
7. `papercontent_test.go` with `httptest.Server`; shrink `backoffUnit` to keep retry tests fast.
8. `go build ./...` + `go test ./internal/tools/...` green.

## Todo
- [x] add `html-to-markdown/v2` to go.mod/go.sum (pure-Go verified)
- [x] sentinels + `PaperContentTool` + constructor (client + backoffUnit)
- [x] `fetchHTMLWithRetry` (reuse discovery backoff; 404 sentinel; LimitReader +1 guard)
- [x] `convertToMarkdown` (v2 API)
- [x] `cleanup`: strip nav/math, trim biblio/appendix, keep captions, collapse whitespace
- [x] transition logging (html_bytes/markdown_bytes/duration_ms; no raw HTML)
- [x] `papercontent_test.go`: 200/404/5xx-retry/oversize/ctx-cancel via httptest
- [x] file(s) < 200 lines; build + tests green

## Success criteria
- Real recent cs.AI paper → Markdown with heading hierarchy intact and captions present.
- 404 → `ErrPaperHTMLNotFound`; oversize → `ErrPaperHTMLFailed`; transient 5xx retried then succeeds.
- No temp files written; no raw HTML logged; `go test -race` clean.
- `papercontent.go` (+ optional cleanup file) each < 200 lines.

## Risk Assessment
| Risk | L×I | Mitigation |
|---|---|---|
| `html-to-markdown/v2` API differs from PRD sketch (`md.NewConverter`) | Med×Med | Read the v2 pkg docs before coding; the PRD snippet is illustrative, not literal. |
| Cleanup over-trims body (cuts real sections) | Med×Med | Conservative markers; test asserts a known body section survives. Prefer under-cleaning (LLM tolerates noise). |
| Some papers lack HTML rendering (404) | Low×Low | Recoverable by design (Phase 05 re-pick); newest-first discovery makes it rare. |
| LimitReader truncation masks oversize | Med×Med | +1 read guard (see note) turns silent truncation into an explicit error. |

## Backwards compatibility
Net-new tool + net-new dependency; touches no Phase 2 code paths. DiscoveryTool logic is *reused
by copying the pattern*, not refactored — Phase 2 behavior is unchanged.

## Rollback
Delete the new files; `go mod tidy` to drop the dependency. No state/schema impact.

## Security
- Body capped by `io.LimitReader(MaxContentBytes)` — OOM guard (R4).
- Pure Go conversion, no subprocess/shell (no poppler) → no shell-injection surface.
- HTML held in memory only; never written to disk; never logged.

## Next Steps
Consumed by **Phase 05** (`runPipeline` calls `FetchMarkdown`). Independent of Phase 03/04 — can be
built in parallel. File ownership: this phase solely owns `internal/tools/papercontent*.go`; the
only shared touch is `go.mod` (coordinate the single add with Phase 04's SDK adds).
