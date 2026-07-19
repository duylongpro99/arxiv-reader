---
title: Declarative Resource Engine
status: completed
created: 2026-07-18
completed: 2026-07-18
blockedBy: []
blocks: []
supersedes: plans/260718-1353-runtime-category-keyword-exploration
designNote: docs/design-notes/2026-07-18-declarative-resource-engine.md
---

# Declarative Resource Engine

Replace the hardcoded arXiv tools with a **declarative, config-driven resource
engine**. The core (orchestrator + agent pipeline) depends only on a `Source`
interface + a registry — never on any concrete resource. A resource is a YAML
declaration in `resources/*.yaml`; arXiv itself becomes `resources/arxiv.yaml`.

**Approved design (this session).** Four-stage engine pipeline with one hard
boundary — the Normalization Layer (Anti-Corruption Layer):

```
RAW BYTES → ① Transport (HTTP+retry+limits)
          → ② Decoder  (per-format; atom-xml in v1)
          ══ Normalization Layer (ACL) ══
          → ③ map paths+transforms → canonical models.Paper (+validate)
          → ④ content: fetch + html-to-markdown → canonical Markdown
          → Orchestrator · Agent pipeline (UNCHANGED, resource-agnostic)
```

## v1 Deliverable (scope fence)

- **In:** engine + registry + `Source` interface; `resources/arxiv.yaml`
  reproducing arXiv **byte-identically** (golden regression); resource-driven UI
  (`GET /resources` + dynamic form).
- **Out:** any second live resource, JSON decoder, date/number/auth fields.
  These are *additive engine capabilities* added only when a real resource needs
  one. The engine implements **exactly what arXiv exercises**.

## Key Decisions

- **Fully declarative** — zero per-resource Go code. arXiv-ness lives in the YAML.
- **Dual interpolation:** `${...}` = trusted config/env, resolved at load;
  `{{...}}` = untrusted runtime values (`category`/`terms`/`start`/`paper.id`),
  sanitized + URL-encoded by the engine.
- **Security generalizes, never weakens:** `select` fields whitelist-validated
  against a catalog (generalizes `arxivquery.IsValid`); `text` fields run a named
  sanitizer (`arxiv-terms` = today's `SanitizeTerms`).
- **Capabilities are a registered, growing library:** decoders, transforms,
  sanitizers, content-converters plug into the stable Normalization spine.
- **No DB migration:** `models.Paper` gains a non-persisted `Source` field.
- **Supersedes** `260718-1353-runtime-category-keyword-exploration`: its
  `arxivquery` package + `DiscoveryTool`/`PaperContentTool` are replaced.

## Phases

- [x] **phase-01** — Engine contracts + capability registries → `phase-01-engine-contracts-registries.md`
- [x] **phase-02** — Engine pipeline (transport/decoder/normalizer/content) → `phase-02-engine-pipeline.md`
- [x] **phase-03** — Loader + `arxiv.yaml` + catalog + **golden gate** → `phase-03-loader-arxiv-declaration-golden.md`
- [x] **phase-04** — Orchestrator + session + models rewire → `phase-04-orchestrator-session-models-rewire.md`
- [x] **phase-05** — API: `GET /resources` + `POST /discover {resourceId,values}` → `phase-05-api-resources-discover.md`
- [x] **phase-06** — Frontend: ResourcePicker + DynamicRequestForm → `phase-06-frontend-resource-picker-dynamic-form.md`
- [x] **phase-07** — Integration, docs, dead-code cleanup → `phase-07-integration-docs-cleanup.md`

## Dependencies

Sequential: 01 → 02 → **03 (golden regression = acceptance gate)** → 04 → 05 → 06 → 07.
Phase 03's golden test must pass before rewiring (04) — it proves the engine
reproduces arXiv before the old code is removed.

## Untouched (de-risking)

Explainer, reviewer/revision loop, tracing, run history, store, LLM clients —
all consume `models.Paper` + Markdown and are resource-agnostic.

**Caveat (red-team F8):** `vaultwriter`'s *internals* are unchanged, but its call
site is NOT — `runPipeline` passes an arXiv-specific `category` argument
(`orchestrator-pipeline.go:236`). Phase 04 re-sources that from
`values["category"]` (empty-safe). Not a fully resource-agnostic seam yet.

## Red Team Review

### Session — 2026-07-18
**Findings:** 21 (20 accepted, 1 = decision to keep the engine)
**Severity breakdown:** 4 Critical, 11 High, 6 Medium
**Reviewers:** Security Adversary · Failure Mode Analyst · Assumption Destroyer · Scope & Complexity Critic

| # | Finding | Sev | Disposition | Applied To |
|---|---------|-----|-------------|-----------|
| 1 | Declarative engine for single-resource v1 (scope) | Crit | Reject (keep engine) | — |
| 2 | Old-style arXiv IDs break (`afterLast` ≠ `/abs/`) | Crit | Accept → `arxiv-id` Go transform | P02/P03 |
| 3 | `pdfURL` not declaratively expressible | Crit | Accept → `arxiv-pdf-url` Go transform | P02/P03 |
| 4 | SSRF/path injection via `{{paper.id}}`, no egress policy | Crit | Accept | P02 |
| 5 | Divergent timeout/429 semantics collapsed | High | Accept → per-`FetchSpec` policy | P01/P02/P04 |
| 6 | Content spec omits `retry` | High | Accept | P03 |
| 7 | `GET /categories` removed with no alias | High | Accept → temp alias | P05/P06/P07 |
| 8 | Vault `category` source undefined ("untouched" false) | High | Accept | P04/P07 |
| 9 | Pagination page size duplicated | High | Accept → `Source.PageSize()` | P04 |
| 10 | `require` new hard batch-fail | High | Accept → per-item tolerance | P02 |
| 11 | `${...}` resolution order/type mis-specified | High | Accept → resolve raw bytes + Config-first | P03 |
| 12 | Golden gate circular (reuses old fixtures) | High | Accept → expand fixtures + gated deletion | P03/P04 |
| 13 | Secrets leak via resolved URLs/headers | High | Accept → redact to host+path+status | P02 |
| 14 | Raw body echoed in errors; discovery no LimitReader | High | Accept | P02 |
| 15 | Response layer is a bespoke DSL (unbudgeted) | High | Accept → shrink (drop where/firstOf/template) | P01/P02 |
| 16 | Loader catalog path traversal + no size cap | Med | Accept | P03 |
| 17 | Back-compat shim can't capture top-level keys | Med | Accept → RawMessage + fold-before-validate | P05 |
| 18 | Header CRLF injection | Med | Accept | P02 |
| 19 | Scalar-or-map `UnmarshalYAML` ambiguity | Med | Accept | P01 |
| 20 | `multi`/empty-author + per-field transform semantics | Med | Accept | P02/P03 |
| 21 | Sanitization only at API layer, not engine | Med | Accept | P02/P05 |

Fixes are recorded inline as "## Red Team Fixes (2026-07-18)" sections in each phase file.

## Validation Log

### Session 1 — 2026-07-18 (4 questions, all recommended)

- **V1 — Entry-level deriver capability.** A `Transform func(string) string` can't see the
  links array + id that `arxiv-pdf-url` needs. Add a distinct **`Deriver` capability**:
  `func(entry Node, p *Paper) (string, error)`, registered like a transform but node-aware.
  arXiv `id` and `pdfUrl` become **derivers** (`derive: arxiv-id` / `derive: arxiv-pdf-url`);
  simple fields keep cheap `func(string)string` transforms. → Phases 01, 02, 03.
- **V2 — `${...}` substitution safety.** Restrict `${...}` to scalar value positions;
  YAML-quote/escape every resolved value before splicing into raw bytes; reject resolved values
  containing control chars. → Phase 03.
- **V3 — SSRF v1 depth = minimal.** v1 ships `PathEscape` + arXiv-id regex on `{{paper.id}}`,
  reject any content URL whose host/scheme ≠ declared base, redirect cap (3). **Defer** the
  private-IP/link-local/metadata denylist to resource #2 (arXiv host is fixed + public in v1).
  → Phases 02, 07.
- **V4 — Keep old tools as oracle through Phase 07.** Phase 04 rewires onto the engine but
  KEEPS `discovery.go`/`papercontent.go`/`arxivquery` behind a temporary build tag / unexported
  switch + the live golden diff. Delete only in Phase 07 after full e2e (vault + timeline +
  pagination) passes. Preserves an A/B oracle + rollback. → Phases 04, 07.
