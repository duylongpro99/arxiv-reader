# Phase 02 — DiscoveryTool (arXiv client)

**Context:** `docs/phase2/prd.md` §2.4, §6 · `docs/phase2/brainstorm-summary.md` (arXiv gotchas)
**Priority:** Critical · **Status:** pending · **Depends on:** 01 · **Effort:** ~L

## Overview
Own the entire relationship with arXiv: query, Atom/XML parse, polite rate-limit + retry.
Nothing outside this tool knows how arXiv works. Returns `[]models.Paper` (up to `fetch_limit`).

## Key insights
- arXiv entry `id` is a URL (`http://arxiv.org/abs/2401.12345v1`) → extract numeric ID.
- Atom `title`/`summary` are whitespace-wrapped → normalize with `strings.Fields` join.
- Base URL comes from `config.Agent.ArxivBaseURL` → unit tests point it at `httptest.Server`.
- Backoff 3→6→12s already satisfies the 3s min-interval between requests.

## Requirements (PRD F2, NFR reliability)
- `GET {base}?search_query=cat:{category}&sortBy=submittedDate&sortOrder=descending&max_results={fetch_limit}&start=0`.
- `User-Agent` header from config.
- Retry on 429 and 5xx with exponential backoff, max `MaxRetries`; per-request timeout.
- Typed errors: `ErrArxivRateLimit`, `ErrArxivUnavailable`, `ErrArxivParse`.
- Structured logs per attempt (WARN on retry) and on success (`count`, `duration_ms`).

## Related code files
**Create:**
- `backend/internal/tools/discovery.go` — `DiscoveryTool`, `FetchPapers`, XML structs, helpers.
- `backend/internal/tools/discovery_test.go` — httptest-driven: happy path, 429-then-200,
  parse failure, ID extraction, whitespace normalization, retries-exhausted.

## Design detail
```go
package tools

type DiscoveryTool struct {
    cfg        *config.AgentConfig
    httpClient *http.Client   // timeout = RequestTimeoutSec
}

func NewDiscoveryTool(cfg *config.AgentConfig) *DiscoveryTool

// FetchPapers queries arXiv and returns up to cfg.FetchLimit papers, newest first.
// ctx cancellation aborts in-flight request + backoff sleeps.
func (t *DiscoveryTool) FetchPapers(ctx context.Context) ([]models.Paper, error)
```
XML structs (namespace-agnostic field names work with encoding/xml):
```go
type arxivFeed struct {
    XMLName xml.Name     `xml:"feed"`
    Entries []arxivEntry `xml:"entry"`
}
type arxivEntry struct {
    ID        string        `xml:"id"`
    Title     string        `xml:"title"`
    Summary   string        `xml:"summary"`
    Published string        `xml:"published"`
    Authors   []arxivAuthor `xml:"author"`
    Links     []arxivLink   `xml:"link"`
}
type arxivAuthor struct { Name string `xml:"name"` }
type arxivLink   struct { Href string `xml:"href,attr"`; Rel string `xml:"rel,attr"`; Type string `xml:"type,attr"` }
```
Helpers:
```go
// "http://arxiv.org/abs/2401.12345v2" → "2401.12345"
func extractArxivID(rawID string) string        // split on "/abs/", strip trailing vN
func normalizeText(s string) string             // strings.Join(strings.Fields(s), " ")
func pdfURL(links []arxivLink) string            // prefer rel="related"/type="application/pdf", else derive from ID
```

### Retry loop (intent)
```
for attempt 0..MaxRetries:
    do request (ctx-aware)
    if 200 → parse; on parse error return ErrArxivParse (no retry)
    if 429 or 5xx:
        if attempt == MaxRetries → return ErrArxivRateLimit / ErrArxivUnavailable
        log WARN {attempt, backoff_ms}; sleep backoff (respect ctx); backoff = base * 2^attempt
    else (other 4xx) → return ErrArxivUnavailable (no retry)
```
> **Why no retry on parse/4xx:** malformed XML or 400 won't fix itself — retrying wastes the
> user's 10s budget and hammers arXiv. Only transient (429/5xx) is retried.

## Implementation steps
1. Define structs + `NewDiscoveryTool` (client with timeout).
2. Build request (query params, User-Agent, ctx).
3. Retry/backoff loop with typed errors + WARN logs.
4. Parse feed → `[]models.Paper` (map fields, extract ID, normalize text, resolve PDF URL).
5. Tests via `httptest.Server` serving canned Atom fixtures (store fixture inline or `testdata/`).
6. `go build ./... && go test ./internal/tools/` green.

## Todo
- [ ] XML structs + tool constructor
- [ ] Request builder (params + User-Agent + ctx)
- [ ] Retry/backoff with typed errors + attempt logging
- [ ] Feed → `[]Paper` mapping (ID extract, whitespace normalize, PDF URL)
- [ ] `discovery_test.go`: happy, 429→200, exhausted-429, 5xx, parse-fail, ID/whitespace units
- [ ] build + test green

## Success criteria
- Happy path returns ≤ fetch_limit papers, newest first, IDs stripped of version suffix.
- 429 then 200 succeeds within retry budget; all-429 returns `ErrArxivRateLimit`.
- Malformed XML → `ErrArxivParse`, no retry.
- No live network in tests (httptest only).

## Risks
- arXiv occasionally returns 200 + empty feed → return empty slice (not error); Phase 03/04
  surface "no new papers". Do NOT treat empty as failure.
- Title/summary normalization must not collapse into empty on odd input — guard.

## Security
- Category injected from config, not user input. No user data in outbound request.
- Never log raw HTML/XML body (per CLAUDE.md) — log counts/durations only.
