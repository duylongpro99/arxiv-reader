# Phase 01 — Catalog + Query Value Object + Config Validation

**Priority:** High · **Status:** pending · **Depends on:** —

## Overview

Create the foundation: a leaf package holding the cs.* catalog, the
`Query{Category, Terms}` value object that renders arXiv `search_query`, and
free-text sanitization. Wire config to validate its default category against the
catalog at load time.

## Why a new leaf package

`config`, `tools`, and `orchestrator` all need the catalog/query. Config
validating its default against the catalog would create a cycle if the catalog
lived in `models`/`tools` (which may import config). A dependency-free leaf
package (`internal/arxivquery`) breaks the cycle — everyone imports it, it
imports nothing internal.

## Files to Create

- `backend/internal/arxivquery/catalog.go`
  - `type Category struct { Code, Label string }`
  - `var Categories []Category` — hardcoded cs.* subcategories (~40 entries:
    cs.AI "Artificial Intelligence", cs.LG "Machine Learning", cs.CL
    "Computation and Language", cs.CV "Computer Vision and Pattern Recognition",
    cs.RO, cs.NE, cs.IR, cs.DS, cs.DB, cs.SE, cs.PL, cs.CR, cs.DC, cs.HC, … —
    full cs.* list from the arXiv taxonomy).
  - `func IsValid(code string) bool` — O(1) membership via a package-level
    `map[string]bool` built from `Categories`.
- `backend/internal/arxivquery/query.go`
  - `type Query struct { Category, Terms string }`
  - `func (q Query) SearchQuery() string` — returns `cat:<Category>` when Terms
    empty, else `cat:<Category> AND all:<sanitized-terms>`. Returns the raw
    string (NOT URL-encoded); the caller (`url.Values.Set`) encodes it.
  - `func SanitizeTerms(s string) string` — trim, collapse whitespace, cap
    length (e.g. 200 chars), strip arXiv boolean control tokens (case-insensitive
    `AND`/`OR`/`ANDNOT` as standalone words) and structural chars (`"`, `(`, `)`,
    `:`). Rationale: prevent a user from injecting field prefixes / boolean
    operators that rewrite query semantics or trip an arXiv 400.
- `backend/internal/arxivquery/catalog_test.go`, `query_test.go`

## Files to Modify

- `backend/internal/config/config.go`
  - In `validate()` (near line 273), after the non-empty check, add:
    `if !arxivquery.IsValid(a.ArxivCategory) { return error(...) }` so a
    misconfigured default fails fast with a clear message listing it must be a
    known cs.* code.

## Implementation Steps

1. Write `catalog.go` with the full cs.* list + `IsValid`. Source codes/labels
   from the arXiv category taxonomy (cs.* group).
2. Write `query.go`: `Query.SearchQuery()` + `SanitizeTerms`. Keep sanitization
   in ONE function (DRY) — it is the single security seam for free-text.
3. Add config default validation.
4. Tests:
   - `IsValid` true for cs.AI, false for `cs.NOPE`, `../etc`, `cat:cs.AI`.
   - `SearchQuery()`: category-only → `cat:cs.LG`; with terms → `cat:cs.LG AND all:transformer`.
   - `SanitizeTerms` strips `OR`, `AND`, quotes, parens, colons; caps length.
   - config load fails on an unknown `arxiv_category` default.

## Todo

- [ ] `catalog.go` (+ full cs.* list, `IsValid`)
- [ ] `query.go` (`Query`, `SearchQuery`, `SanitizeTerms`)
- [ ] config default validation
- [ ] unit tests (catalog, query, sanitize, config)
- [ ] `go build ./... && go test ./internal/arxivquery/... ./internal/config/...`

## Success Criteria

- Package compiles with zero internal imports.
- `SanitizeTerms` provably neutralizes control tokens (test-covered).
- Config rejects an unknown default category at load.

## Risks

- Catalog drift vs arXiv taxonomy — acceptable (stable list; design note).
- Over-aggressive sanitization dropping legitimate terms — keep it to control
  tokens + structural chars, not general alphanumerics.
