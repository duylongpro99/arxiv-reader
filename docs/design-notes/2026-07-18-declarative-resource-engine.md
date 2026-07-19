# Design Note — Declarative Resource Engine (2026-07-18)

## Problem

The pipeline was welded to arXiv: `tools/discovery.go`, `tools/papercontent.go`,
and `internal/arxivquery` hardcoded arXiv's Atom shape, query syntax, category
whitelist, and HTML endpoint. Adding any second source meant forking that Go
code. The structural challenge: make the source a **replaceable, declarative
input** so the core (orchestrator + agent pipeline) depends on an abstraction,
never on a concrete resource — without regressing arXiv's exact behaviour.

## Structure

A four-stage engine pipeline behind one hard boundary — the Normalization Layer
(Anti-Corruption Layer):

```
RAW BYTES → ① Transport (HTTP + retry + limits, per FetchSpec)
          → ② Decoder   (per-format; atom-xml in v1)
          ══ Normalization Layer (ACL) ══
          → ③ map paths + transforms/derivers → canonical models.Paper (+ require)
          → ④ content: fetch + html-to-markdown → canonical Markdown
          → Orchestrator · Agent pipeline (UNCHANGED, resource-agnostic)
```

Responsibilities divide as:

- **`internal/resource`** owns the `Source` interface, the parsed YAML
  `Declaration`, the UI `Descriptor`, and five **capability registries**
  (decoders, transforms, derivers, sanitizers, converters). It imports only
  `internal/models` — a leaf, like the old `arxivquery`, so `config`, the loader,
  and the orchestrator can all depend on it without a cycle.
- **`DeclarativeSource`** is the single `Source` implementation. All arXiv-ness
  lives in `resources/arxiv.yaml` + `resources/catalogs/arxiv-cs.yaml`; there is
  **zero per-resource Go code**.
- **The orchestrator** depends on `resource.Registry` + `resource.Source`. It
  binds a session to `(resourceID, values)` instead of an arXiv query.

Two interpolations, deliberately separated:

- `${...}` — trusted config/env, resolved on the **raw YAML bytes at load**
  (so numeric slots parse as ints), YAML-quoted/escaped, control-chars rejected.
- `{{...}}` — untrusted runtime values (`category`/`terms`/`start`/`paper.id`),
  schema-validated + sanitized + URL-encoded by the engine.

Security **generalizes, never weakens**: select fields are whitelist-validated
against a catalog (generalizing `arxivquery.IsValid`); text fields run a named
sanitizer (`arxiv-terms` = the old `SanitizeTerms`); `ValidateValues` is a method
on the `Source`, so every caller (discover, discover-more, rehydrated sessions)
gets the same gate.

## Tradeoffs

- **Rejected: in-process Go adapters per source.** More flexible, but every new
  source is code + a deploy; the whole point was config-only additions. The
  capability registries recover the needed flexibility at controlled seams.
- **Rejected: fully declarative from day 1 (arXiv's id/pdfUrl in YAML too).**
  arXiv's id extraction (old-style ids, version stripping) and pdfUrl selection
  (link predicate) are not cleanly expressible as declarative equality. They
  became **derivers** (`arxiv-id`, `arxiv-pdf-url`) — node-aware Go capabilities
  registered by name — instead of bloating the declaration DSL.
- **Rejected: a broad response DSL** (`firstOf`/`where`/`template`). Dropped as
  unbudgeted scope; v1 implements exactly what arXiv exercises (`path`/`@attr`/
  `multi`/transform-chain + `derive`). New shapes are added when a real resource
  needs them (YAGNI).
- **Retry policy is per-`FetchSpec`** (an `on:` list), so discovery treats a
  timeout as transient (→ "unavailable" after retries) while content treats it as
  terminal (→ "timed out") — preserving the old two-message split.
- **v1 SSRF is minimal** (`safePaperID` + same-host/scheme content anchor +
  redirect cap 3). The private-IP/link-local/metadata denylist is deferred to the
  first non-arXiv resource (arXiv's host is fixed + public); the seam is left open
  in `transport` — see `docs/adding-a-resource.md`.
- **No DB migration:** `models.Paper` gained a non-persisted `Source` field.

The right design is the simplest one that handles the real complexity (arXiv's
byte-level quirks) without collapsing under the future change (a second source).
The **golden regression** — the engine reproducing the old tools field-for-field —
was the gate that proved this before the old code was deleted.
