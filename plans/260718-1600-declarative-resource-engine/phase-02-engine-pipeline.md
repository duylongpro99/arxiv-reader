# Phase 02 — Engine Pipeline (Transport · Decoder · Normalizer · Content)

**Priority:** High · **Status:** pending · **Depends on:** phase-01

## Overview

Implement the four engine stages behind the Phase 01 contracts, plus
`DeclarativeSource` (the single `Source` implementation for v1). Most logic is a
**refactor of the existing `discovery.go` / `papercontent.go`** into
declaration-parameterized form — not new invention. This phase is pure
package-internal work with unit tests; no orchestrator/config wiring yet.

## Files to Create

- `backend/internal/resource/transport.go` — generic HTTP + retry/backoff.
  - Port `discovery.go` `fetchWithRetry` / `doRequest` / `backoffFor` /
    `sleepCtx` (lines 156–253), parameterized by `FetchSpec` (url, headers,
    retry policy, timeout) instead of `cfg`.
  - Generic sentinel errors: `ErrTransportRateLimit`, `ErrTransportUnavailable`,
    `ErrTransportTimeout` (replace `ErrArxiv*`). `backoffUnit time.Duration`
    seam preserved for fast tests.
  - `func fetch(ctx, spec FetchSpec, vars runtimeVars, start int, onRetry) ([]byte, int, error)` — returns body + HTTP status (status needed for `notFound` handling in content).
- `backend/internal/resource/interpolate.go` — dual interpolation + query build.
  - `${...}` resolved at **load** (Phase 03 passes a resolver); this file handles
    the runtime `{{...}}` substitution: `category`/`terms`/`start`/`paper.id`.
  - `func buildQuery(spec map[string]QueryPart, vars runtimeVars) url.Values` —
    assembles `parts`+`join`, dropping a part whose `when` var is empty. ALL
    `{{...}}` values pass through `url.Values.Set` (transport encoding).
  - Runtime values are already sanitized upstream (schema validation, Phase 03/05);
    this file only substitutes + encodes.
- `backend/internal/resource/decode_atomxml.go` — `atom-xml` decoder + `Node`.
  - Generic XML → `Node` tree (namespace-agnostic, match by local name — mirror
    the current `xml:"..."` local-name matching). `Node.Get("feed.entry")`
    walks dotted paths; `Node.Get("author.name")` returns repeated nodes;
    `Attr("href")` reads attributes; `Text()` returns element chardata.
  - `RegisterDecoder("atom-xml", ...)` in an `init()`.
- `backend/internal/resource/transforms.go` — the v1 transform library.
  - `afterLast(sep)`, `stripVersion` (trailing `vN`), `normalize`
    (= `strings.Join(strings.Fields(s), " ")`), `trim`, `template` (fills
    `{{id}}` etc. from the in-progress paper). Register each via
    `RegisterTransform`. These reproduce `entryToPaper`/`extractArxivID`/
    `normalizeText`/`pdfURL` (discovery.go 255–313) as composable primitives.
- `backend/internal/resource/normalize.go` — **the Normalization Layer (ACL)**.
  - `func normalize(root Node, spec ResponseSpec, resourceID string) ([]models.Paper, error)`:
    for each `Items` node, resolve each `FieldMap` (path→Text/Attr, `multi`,
    `firstOf`, `where`, `transforms`, `template`) into a `models.Paper`
    (`ID/Title/Abstract/Authors/PDFURL/Published`, `Source: resourceID`).
  - Enforce `Require` (`id`,`title`) → `ErrNormalize` when missing. This is the
    single locus where a decoded tree becomes canonical Papers.
- `backend/internal/resource/convert_html.go` — `html-to-markdown` converter.
  - Port `papercontent.go` conversion (htmltomarkdown + cleanup, incl.
    `papercontent-cleanup.go`). `RegisterConverter("html-to-markdown", ...)`.
- `backend/internal/resource/declarative_source.go` — `DeclarativeSource`.
  - Holds the resolved `Declaration` + a `*http.Client`. Implements `Source`:
    - `Discover`: `buildQuery` → `transport.fetch` → `decoder.Decode` →
      `normalize`. Reads `FetchLimit`/`start` from spec/args.
    - `FetchContent`: build content URL (`{{paper.id}}`) → `fetch` (follow
      redirects, LimitReader `maxBytes`) → on 404 + `NotFound: repick` return
      `ErrContentNotFound` (recoverable re-pick) → `Converter`.
    - `Descriptor`: map `RequestSpec.Fields` (+ resolved catalog options) → `Descriptor`.
- Tests:
  - `normalize_test.go` — **isolation**: hand-built `Node` + `ResponseSpec` →
    expected `Paper`; `require` failure → `ErrNormalize`.
  - `transport_test.go` — httptest.Server: 200, 429→retry, 5xx→retry, 4xx→permanent, ctx-cancel (reuse `discovery_test.go` shapes).
  - `decode_atomxml_test.go` — parse a real arXiv Atom fixture; path/attr/multi lookups.
  - `transforms_test.go`, `convert_html_test.go`, `interpolate_test.go` (parts/join/when + encoding).

## Implementation Steps

1. Transport (port + parameterize). 2. atom-xml decoder + `Node`. 3. transforms.
4. Normalizer (ACL) + `require`. 5. html-to-markdown converter. 6. interpolation
+ query assembly. 7. `DeclarativeSource` wiring the stages. 8. tests per file.

## Red Team Fixes (2026-07-18) — applied

- **F2 (C1):** register a Go transform **`arxiv-id`** = `extractArxivID` verbatim
  (`/abs/`-anchored split with last-`/` fallback + `Sscanf` version guard). Do NOT
  use `afterLast("/")` — it mangles old-style IDs (`.../abs/cs/0501001v1` → `cs/0501001`).
- **F3 (C2):** register a Go transform **`arxiv-pdf-url`** = `(type==pdf OR rel==related)
  AND href contains "/pdf/"`, else derive from the version-stripped id. `firstOf`/`where`
  equality cannot express this; pdfUrl uses this transform (drop the declarative predicate).
- **F4 (C3) SSRF:** context-aware interpolation — path-position `{{...}}` via
  `url.PathEscape` + a per-field format validator (arXiv-id regex `^\d{4}\.\d{4,5}$` or the
  old-style form). Transport egress policy: scheme allowlist; block RFC1918/link-local/
  loopback/`169.254.*` resolved **per redirect hop**; redirect cap (3); reject a content URL
  whose host/scheme differs from the declared base.
- **F5 (H1):** transport honors the per-`FetchSpec` transient/timeout policy and keeps
  **two distinct** user-message mappings (discovery timeout = transient/retried; content
  timeout = terminal `ErrPaperHTMLTimeout`-equivalent). Test content-path timeout terminality.
- **F10 (H6):** `normalize` skips/blanks an item failing `require` and logs it — it MUST
  NOT fail the whole batch (match today's tolerant `entryToPaper`).
- **F13 (H9):** transport errors / `onRetry` / trace records carry host+path+status only —
  never query strings, header values, or resolved `${...}`. Test: a secret-bearing URL
  never appears in any error/trace field.
- **F14 (H10):** apply `io.LimitReader(maxBytes)` to the **discovery** fetch too; sentinel
  errors never embed response bytes. Test a garbage upstream body.
- **F15 (H11):** normalizer implements only `path`/`@attr`/`multi`/transform-chain —
  drop `firstOf`/`where`/`template`.
- **F18 (M3):** reject control chars (`\r\n\x00`) in header names/values at load (`${...}`)
  and substitution (`{{...}}`); deny `{{...}}` in header positions unless explicitly escaped.
- **F20 (M5):** `multi` drops empty results (today's code drops empty authors).
- **F21 (M6):** `DeclarativeSource.Discover`/`FetchContent` validate + sanitize `Values`
  against the declaration's own schema before use — safety is a property of the Source, not
  of each caller (discover-more, golden test, rehydrated sessions all reach these).

<!-- Updated: Validation Session 1 — V1 derivers, V3 minimal SSRF -->
**V1 — `arxiv-id` and `arxiv-pdf-url` are DERIVERS, not string transforms** (they need the
whole entry node + partial Paper). Register via `RegisterDeriver`. `arxiv-pdf-url` reads the
entry's `link` elements (type/rel/href) + the derived id; `arxiv-id` reads the `<id>` text.
Field transforms stay `func(string)string` for title/abstract/published/authors.
**V3 — SSRF scope for v1 is MINIMAL:** implement `url.PathEscape` + arXiv-id regex on
`{{paper.id}}`, reject a content URL whose host/scheme ≠ the declared base, and a redirect cap
(3). Do NOT build the private-IP/link-local/metadata denylist yet — deferred to resource #2
(F4's full policy). Leave a clear seam so the denylist drops in without reshaping transport.

## Todo

- [ ] transport.go, interpolate.go, decode_atomxml.go
- [ ] transforms.go, normalize.go, convert_html.go
- [ ] declarative_source.go
- [ ] unit tests (normalizer isolation is the priority)
- [ ] `go build ./... && go test ./internal/resource/...`

## Success Criteria

- Normalizer converts a decoded tree → canonical `Paper` in isolation.
- Transport reproduces the current retry/backoff/status semantics.
- Nothing arXiv-specific in Go — all arXiv-ness stays in the (Phase 03) YAML.

## Risks

- **Generic XML `Node`** is the hardest piece — the current code leans on
  `encoding/xml` struct tags. Keep `Node` minimal (Get/Text/Attr) and back it
  with a small recursive decode; validate against the golden test in Phase 03.
- `firstOf`/`where`/`template` scope creep — implement only what `pdfUrl` needs.
