# Design Note: Runtime Category + Keyword Exploration

Date: 2026-07-18
Status: Approved (brainstorm) — pending implementation plan

## Problem

Discovery is locked to a single global category. `cfg.Agent.ArxivCategory` (e.g.
`cs.AI`) is read directly in `discovery.go:134` (`buildQueryURL`) and
`orchestrator-pipeline.go` (`runDiscovery`). There is no way for a user to
explore a different arXiv category, and no way to narrow by topic. The user
wants a more powerful explore mechanism: pick a category at runtime and
optionally add keywords.

Decisions locked in brainstorm:
- Control: **user picks at runtime** (UI picker per run).
- Breadth: **one category at a time** (query stays `cat:X`).
- Catalog: **cs.\* subcategories** only.
- Depth: **category required, free-text keywords optional** (`cat:X AND all:...`).
- Build approach: **A** — a `DiscoveryQuery` value object.

## Structure

The central shift: category moves from a **global constant** to **per-session
state**. Reason: "load more" pagination reads session state, so the chosen
category+terms must persist on the session, not a request-scoped variable, or
pagination would drift back to the config default.

Responsibilities divided as:

- **`DiscoveryQuery` (new value object)** — owns arXiv `search_query` syntax.
  `{Category, Terms}` → `SearchQuery()` renders `cat:cs.LG AND all:...`. Keeps
  query-building in one place, honoring the existing "DiscoveryTool owns the
  arXiv relationship" boundary. `buildQueryURL(start)` → `buildQueryURL(query, start)`;
  the tool stops reading `cfg.ArxivCategory`.
- **cs.\* catalog (hardcoded `{code, label}` list, ~40 entries)** — one source
  for both the validation whitelist and the UI dropdown. Exposed via `GET
  /categories`. Hardcoded, not scraped: the cs taxonomy is small and stable
  (KISS/YAGNI). Config `arxiv_category` becomes the *default selection* and must
  validate as a catalog member at load time.
- **Trigger (`POST /trigger`)** — reads optional `{category, terms}`; empty body
  falls back to config default (backward compatible). Unknown category → 400.
- **Session** — gains `Query DiscoveryQuery`; `runDiscovery` and the "load more"
  handler both read it instead of `cfg`.
- **Frontend** — category dropdown (human labels) + optional keyword input beside
  the trigger button; passed to `triggerDiscovery(category, terms)`.

Data flow: UI picker → POST /trigger {category, terms} → validate against catalog
→ store DiscoveryQuery on session → runDiscovery + load-more build arXiv query
from session.Query.

## Security posture change (critical)

`discovery.go:131` currently asserts "category comes from config, never user
input, so there is no injection surface." **That guarantee is voided.** New
posture:
- Category: safe via catalog whitelist (reject unknown → 400).
- Free-text: raw user input into the arXiv query string. Mitigate by
  URL-encoding (already via `url.Values`), capping length, and stripping arXiv
  control tokens (`AND`/`OR`/`ANDNOT`, stray quotes/parens) so a user cannot
  rewrite query semantics or trigger a 400.
The stale comment must be rewritten.

## Tradeoffs

- **A (chosen) vs B (loose strings):** A adds one small type + method but keeps
  query syntax and validation in one place and extends cleanly to
  multi-category/other sources later. B threads two strings through the call
  chain — less code now, but query syntax leaks into the orchestrator and each
  future addition re-threads the chain. Rejected B.
- **Hardcoded catalog vs scraped taxonomy:** scraping adds a network dependency
  and failure mode for a list that changes rarely. Rejected scraping.
- **Category required vs optional:** required keeps every result scoped to a cs
  subfield (matches "switch category" intent); keywords only narrow. Chosen.

## Unchanged

Sort order, retry/backoff, dedup, pagination offset mechanics, SSE/status
polling. The change is additive.
