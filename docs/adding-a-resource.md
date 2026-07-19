# Adding a Resource

The discovery source is **declarative**. A resource is a YAML file in
`resources/*.yaml`; arXiv itself is just `resources/arxiv.yaml`. The core
(orchestrator + agent pipeline) depends only on the `Source` interface + the
registry, never on any concrete resource.

There are two cases.

## Case A — the resource fits existing capabilities

If the new source speaks a format the engine already decodes (v1: `atom-xml`),
needs only existing transforms (`normalize`, `trim`), derivers (`arxiv-id`,
`arxiv-pdf-url`), sanitizers (`arxiv-terms`), and converters
(`html-to-markdown`), then **adding it is a single YAML file** — no Go changes.

1. Drop `resources/<id>.yaml`. Use `resources/arxiv.yaml` as the template.
2. If it has a select field, add its catalog to `resources/catalogs/<name>.yaml`
   as a `[{value, label}]` list, and reference it via `options.catalog: <name>`.
3. Restart. The loader validates fail-fast and registers it; `GET /resources`
   surfaces it and the UI renders its form automatically (**zero frontend
   changes** — the form is schema-driven).

### Declaration anatomy

- `request.fields[]` — `select` (whitelist-validated against a catalog) or
  `text` (run through a named `sanitize`r). Each becomes a UI form field.
- `fetch` — `url`, `headers`, a structured `query` (literal, or a
  `{join, parts}` where each part is dropped when its `when` guard value is
  empty), `paginate` (`kind`/`param`/`page_size` — the single owner of page
  size), `retry` (`max_retries` + an `on:` list of `429`/`5xx`/`network`/
  `timeout`), `timeout_seconds`, `max_bytes`.
- `response` — `format` (a registered decoder), `items` (the repeated element's
  dotted path), `fields` mapping each canonical Paper field
  (`id`/`title`/`abstract`/`authors`/`published`/`pdfUrl`) from a `path`
  (+ optional `@attr`, `multi`, `transforms: [...]`) **or** a `derive`, plus
  `require: [...]` (items missing a required field are skipped, not fatal).
- `content` — a second `fetch` for one item's body + a `convert`er + `not_found`
  behaviour (`repick` = recoverable re-pick on 404).

### Interpolation (two kinds, never mixed)

- `${VAR}` — **trusted** config/env, resolved on the raw YAML bytes at load.
  Only whitelisted keys resolve (see `config.Config.Resolve`); values are
  YAML-quoted/escaped and rejected if they contain control characters.
- `{{name}}` — **untrusted** runtime values (`category`/`terms`/`start`/
  `paper.id`), schema-validated + sanitized + URL-encoded by the engine.

## Case B — the resource needs a new shape

If it needs a format/transform/deriver/sanitizer/converter the engine does not
have yet, add the capability at its registry seam, then write the YAML (Case A).
Each seam lives in `backend/internal/resource`:

| Need | Register with | Signature |
|------|---------------|-----------|
| New response format (e.g. JSON) | `RegisterDecoder(format, Decoder)` | `Decode([]byte) (Node, error)` |
| New string transform | `RegisterTransform(name, factory)` | `func(string) string` |
| New node-aware field deriver | `RegisterDeriver(name, Deriver)` | `func(entry Node, p *Paper) (string, error)` |
| New free-text sanitizer | `RegisterSanitizer(name, Sanitizer)` | `func(string) string` |
| New content converter | `RegisterConverter(name, Converter)` | `func([]byte) (string, error)` |

Register in an `init()` (see `transforms.go`, `decode_atomxml.go`,
`convert_html.go`, `sanitizers.go` for examples). The loader's `validate` rejects
any declaration referencing an unregistered capability, so a typo fails fast at
startup rather than at request time.

## REQUIRED before enabling any non-arXiv resource: SSRF egress denylist

v1's SSRF posture is **minimal and arXiv-specific-safe**: `safePaperID` blocks
path traversal / scheme injection, the content fetch enforces same-host+scheme
against the declared base, and the redirect chain is capped at 3. This is
sufficient only because arXiv's host is fixed and public.

Before enabling a resource that fetches from a **configurable or user-influenced
host**, you MUST add the deferred egress denylist to `transport` /
`catalog.go`'s `checkEgress`:

- Resolve each request/redirect host and **reject RFC1918 / loopback /
  link-local (`169.254.*`) / cloud-metadata (`169.254.169.254`) addresses**,
  checked **per redirect hop** (not just the initial URL).
- Keep the scheme allowlist (http/https) and the redirect cap.

The seam is already isolated in `transport.boundedSameHost` /
`catalog.checkEgress`; the denylist drops in there without reshaping transport.
