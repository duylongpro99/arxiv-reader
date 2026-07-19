# Phase 03 — Loader + arxiv.yaml + Catalog + Golden Gate

**Priority:** Critical · **Status:** pending · **Depends on:** phase-02

## Overview

Wire declarations to the engine: a loader that reads `resources/*.yaml`, resolves
`${...}` against config/env, validates fail-fast, builds `DeclarativeSource`s,
and populates the `Registry`. Author `resources/arxiv.yaml` +
`resources/catalogs/arxiv-cs.yaml` reproducing today's arXiv. Register the
`arxiv-terms` sanitizer. **The acceptance gate of the whole plan lives here:** a
golden regression proving the engine yields byte-identical `Paper` output to the
current `DiscoveryTool`.

## Files to Create

- `backend/internal/resource/loader.go`
  - `func Load(dir string, resolve func(key string) (string, error)) (*Registry, error)`:
    read `*.yaml` (skip `catalogs/`), `yaml.Unmarshal` → `Declaration`, resolve
    every `${VAR}` via `resolve` (fail on unknown/empty required var), load
    referenced catalogs into `Options`, `validate(decl)`, build
    `DeclarativeSource`, `Register`. Empty registry → error (server must have ≥1
    resource). Mirror config's fail-fast + key-free error style.
  - `func validate(d Declaration) error`: id/label present; each field
    type ∈ {select,text}; select has a resolvable catalog with ≥1 option + a
    default that is in-catalog; referenced `format`/`transforms`/`sanitize`/
    `convert` names exist in their registries; `require` fields are mapped.
- `backend/internal/resource/catalog.go`
  - `func loadCatalog(path string) ([]Option, error)` — parse
    `catalogs/<name>.yaml` (`[{value,label}]`).
- `backend/internal/resource/sanitizers.go`
  - `RegisterSanitizer("arxiv-terms", ...)` — **move** `arxivquery.SanitizeTerms`
    logic here verbatim (strip AND/OR/ANDNOT standalone tokens + `" ( ) :`,
    collapse whitespace, cap 200 runes). This is the security seam.
- `resources/arxiv.yaml` — full declaration (per approved design):
  - request.fields: `category` (select, required, `default: ${AGENT_ARXIV_CATEGORY}`,
    `options.catalog: arxiv-cs`) + `terms` (text, optional, `sanitize: arxiv-terms`).
  - fetch: url `${AGENT_ARXIV_BASE_URL}`, `User-Agent: ${AGENT_USER_AGENT}`,
    query `search_query`={join `" AND "`, parts `cat:{{category}}` +
    (`all:{{terms}}` when terms)}, `sortBy/sortOrder`, `max_results:
    ${AGENT_FETCH_LIMIT}`, `start: {{start}}`; paginate offset/start; retry from
    `${AGENT_MAX_RETRIES}`/`${AGENT_MIN_REQUEST_INTERVAL_SECONDS}`; timeout
    `${AGENT_REQUEST_TIMEOUT_SECONDS}`.
  - response: format atom-xml, items `feed.entry`, field maps for
    id/title/abstract/published/authors/pdfUrl (firstOf link\@pdf → template),
    require [id,title].
  - content: url `${AGENT_ARXIV_HTML_BASE_URL}/{{paper.id}}`, followRedirects,
    maxBytes `${AGENT_MAX_CONTENT_BYTES}`, convert html-to-markdown, notFound repick.
- `resources/catalogs/arxiv-cs.yaml` — the 40 cs.* entries from
  `arxivquery.Categories` (catalog.go 20–61) as `[{value,label}]`.
- Tests:
  - `loader_test.go` — valid dir loads arxiv; unknown `${VAR}` fails; unknown
    capability name fails; missing catalog fails; default-not-in-catalog fails.
  - **`golden_test.go` — THE GATE:** run `arxiv.yaml` through the engine against
    the existing `discovery_test.go` Atom XML fixtures and assert the resulting
    `[]models.Paper` equals the current `DiscoveryTool` output field-for-field
    (id/title/authors/abstract/pdfURL/published). Keep the old `DiscoveryTool`
    temporarily to diff against (removed in Phase 04).
  - `sanitizers_test.go` — reuse `arxivquery/query_test.go` `SanitizeTerms` cases.

## Files to Modify

- `backend/internal/config/config.go` — add `Paths.ResourcesDir` (default
  `./resources`; env `RESOURCES_DIR`). Provide a `resolve(key)` closure over the
  loaded `Config`+`os.Getenv` for the loader. `AGENT_ARXIV_*` keep working via
  `${...}`. Do **not** remove `AgentConfig` arXiv fields yet (loader reads them
  through `resolve`); prune unused ones in Phase 07.

## Red Team Fixes (2026-07-18) — applied

- **F2/F3 (C1/C2):** `arxiv.yaml` uses `id: [ arxiv-id ]` and
  `pdfUrl: { transform: arxiv-pdf-url }` (Go transforms from Phase 02) — NOT
  `afterLast`/`stripVersion` or `firstOf`/`where`/`template`.
- **F6 (H2):** add a `content.request.retry` block to `arxiv.yaml` mirroring the discovery
  policy (`${AGENT_MAX_RETRIES}`, on 429/5xx, backoff). Test content-path 5xx→retry.
- **F11 (H7) — resolution order/type:** resolve `${...}` as **text substitution on the raw
  YAML bytes BEFORE `yaml.Unmarshal`** (so numeric slots like `max_results: ${AGENT_FETCH_LIMIT}`
  parse as ints), and guard the substitution's injection posture. `resolve(key)` reads the
  **merged `*Config`** field map first, then falls back to `os.Getenv` (a YAML-only value is
  invisible to `os.Getenv`). Add a loader test: `config.yaml`-only category (no `.env`) boots.
- **F12 (H8) — golden must test the seams:** expand fixtures BEFORE the gate unblocks
  Phase 04 — old-style ID (`cs/0501001v1`→`cs/0501001`), `rel=related` PDF link, `type=pdf`
  link lacking `/pdf/`, empty `<author><name/>`, missing `<title>`, empty feed; plus
  content-pipeline tests (cleanup, `LimitReader` oversize, 404 re-pick, error taxonomy).
- **F16 (M1) — loader hardening:** constrain `options.catalog` to `^[a-z0-9-]+$` + resolve via
  `filepath.Join` with a containment check under the catalogs dir; validate every declared URL
  against the egress policy at load (fail-fast on a private/metadata host); cap YAML file size
  and limit alias expansion.
- **F20 (M5):** `arxiv.yaml` pins `published: [ trim ]` and title/abstract/authors `[ normalize ]`;
  add fixtures with an empty author and a whitespace-padded `published`.

<!-- Updated: Validation Session 1 — V1 derivers, V2 ${} safety -->
**V1 — arxiv.yaml uses derivers:** `id: { derive: arxiv-id }` and
`pdfUrl: { derive: arxiv-pdf-url }` (not `transforms`). Loader `validate` checks referenced
deriver names exist in the deriver registry too.
**V2 — `${...}` substitution safety in the loader:** allow `${...}` only in scalar value
positions; YAML-quote/escape each resolved value before splicing into the raw bytes; reject any
resolved value containing control chars. Add a test: a resolved value containing `:` or a quote
does not break parsing or inject structure.

## Todo

- [ ] loader.go, catalog.go, sanitizers.go
- [ ] resources/arxiv.yaml + resources/catalogs/arxiv-cs.yaml
- [ ] config ResourcesDir + resolve closure
- [ ] **golden_test.go passes (byte-identical vs DiscoveryTool)**
- [ ] loader/sanitizer tests
- [ ] `go build ./... && go test ./internal/resource/... ./internal/config/...`

## Success Criteria

- Loader builds a registry with `arxiv` from YAML at startup; fails fast on any
  bad declaration with a clear, key-free message.
- **Golden test green** — engine output ≡ current DiscoveryTool. This unblocks
  Phase 04 (safe to delete old tools).

## Risks

- Golden diffs from whitespace/version-strip/pdf-derive edge cases — fix in the
  transforms/normalizer (Phase 02), not by loosening the test.
- `${...}` resolution ordering vs `.env` — resolve after config load so env
  overrides already applied.
