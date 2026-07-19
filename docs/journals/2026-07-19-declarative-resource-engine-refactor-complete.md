# Declarative Resource Engine Refactor Complete

**Date**: 2026-07-19 23:15
**Severity**: Medium (architectural change; no user-facing regression)
**Component**: Discovery pipeline, API, frontend resource selection
**Status**: Resolved

## What Happened

Completed the declarative resource engine refactor, replacing three years of hardcoded arXiv tools with a config-driven pipeline. Deleted `tools/discovery.go`, `tools/papercontent.go`, `tools/papercontent-cleanup.go`, and the `internal/arxivquery` package entirely — all responsibilities moved to a `Source` interface + a registry living in `internal/resource/`. arXiv is now `resources/arxiv.yaml` + `resources/catalogs/arxiv-cs.yaml`. Zero per-resource Go code. The orchestrator and agent pipeline are untouched and resource-agnostic.

## The Brutal Truth

This refactor took seven phases over two days because **deletion at scale requires armor**. We couldn't just rip out the old tools and hope. We designed a golden regression gate that byte-level validated the engine reproduced the old tools field-for-field before we were *allowed* to delete. We also ran a full red-team against the design (21 findings; 20 accepted and applied). Without those gates, this would have been a horror show — quiet regressions in pagination, retry semantics, SSRF handling, sanitization. The frustrating part: the refactor itself was elegant once designed, but the *permission to delete* only came from discipline, not from cleverness.

## Technical Details

**The Engine**: Four-stage pipeline behind one hard boundary (the Normalization Layer / Anti-Corruption Layer):
```
RAW BYTES → Transport (HTTP + retry + limits per FetchSpec)
          → Decoder (per-format; atom-xml in v1)
          ══ Normalization Layer ══
          → Normalize (path mapping + field transforms + derivers + validation)
          → Content (fetch + html-to-markdown conversion)
          → Orchestrator & Agent Pipeline (UNCHANGED, resource-agnostic)
```

**Five Capability Registries** (all in `internal/resource/`):
1. **Decoders** — per-format (atom-xml shipped; JSON, CSV deferred).
2. **Transforms** — string pipelines (`normalize`, `trim`, custom sanitizers).
3. **Derivers** — node-aware field generators (arXiv's `arxiv-id` extraction handles old-style IDs + version stripping; `arxiv-pdf-url` picks the right PDF link).
4. **Sanitizers** — free-text cleaners (`arxiv-terms` = the old `SanitizeTerms`).
5. **Converters** — content processors (`html-to-markdown`).

**Dual Interpolation**:
- `${VAR}` — trusted config/env, resolved on raw YAML bytes at load (so `max_results: 10` parses as an int). YAML-quoted/escaped; control chars rejected.
- `{{name}}` — untrusted runtime values (`category`/`terms`/`start`/`paper.id`), schema-validated + sanitized + URL-encoded by the engine. The two never mix.

**API Changes**:
- `GET /resources` replaces `GET /categories` (returns resource descriptors, drives the UI).
- `POST /discover` now accepts `{resourceId, values}` with a legacy back-compat shim folding old `{category, terms}` bodies (empty bodies → defaults).

**Frontend**: `CategoryPicker` → `ResourcePicker` + `DynamicRequestForm`. The form is schema-driven from the resource descriptor — adding a new resource requires zero frontend changes.

## What We Tried

1. **Initial Design (Rejected)**: In-process Go adapters per source. Flexible but wrong — every new source = code + deploy. The capability registries recover flexibility at controlled, auditable seams instead.

2. **Fully Declarative arXiv (Rejected)**: Tried to express arXiv's id extraction (old-style IDs, version stripping) and PDF link selection as pure YAML transforms. Gave up; those are better as **derivers** — Go functions registered by name but invisible in the declaration.

3. **Broad Response DSL (Rejected)**: Started with `firstOf` / `where` / `template` / conditional logic. Cut it — unbudgeted scope. v1 implements exactly what arXiv exercises. New shapes added only when a real resource needs them (YAGNI).

## Root Cause Analysis

The real risk wasn't the design — it was **proving equivalence before deletion**. We could have deleted the old tools on day 1 if we'd been reckless, but then discovered in production that pagination had drifted by one page, or that the retry logic for discovery timeouts (transient) vs. content timeouts (terminal) had collapsed into one semantics. Instead:

1. **Phase 03's Golden Gate**: Built a regression test that loaded arXiv's declaration and a hand-curated golden feed (with edge cases: old-style IDs, explicit PDF links, derived PDFs, whitespace-wrapped fields). The engine must produce byte-for-field-identical output to the old tools. This gate authorized Phase 04's rewiring.

2. **Fixed Field Assignment**: Discovered mid-implementation that Go's random map iteration broke derivers — if the engine assigned fields in iteration order, arXiv's PDF deriver saw an empty `id` field. Fixed by sorting the field assignment order.

3. **Per-FetchSpec Retry Semantics**: Discovery times out → "unavailable" after retries (timeout is transient). Content times out → "timed out" (timeout is terminal). This split was in the old code; we preserved it per-FetchSpec, not globally.

4. **Red-Team Findings**: 21 items across security, retry/timeout semantics, SSRF, sanitization, pagination, back-compat. Applied 20; rejected 1 (the finding that the engine itself was scope creep — nope, we kept it). The discipline of addressing these *before deletion* was the difference between shipping and burning down.

## Lessons Learned

**Golden gates for authorization, not just verification**. We didn't delete code because tests passed — we deleted because a specific golden regression gate passed, proving the engine was byte-for-field identical to the old behavior. This is the right pattern for major refactors: a clearly-defined, reproducible acceptance gate that must pass *before* deletion is even considered. Without it, refactors drift.

**Derivers instead of DSL inflation**. We could have made the YAML DSL rich enough to express arXiv's id extraction, link selection, and PDF building. Instead, we kept the DSL lean and added a **deriver** capability — a Go function taking the parsed node + the partial Paper, returning a derived field. This is reusable, testable, and not a DSL bloat. Principle: when the declarative approach gets tangled, drop a registered capability instead of complicating the declaration.

**Dual interpolation boundaries matter**. `${...}` at load time (trusting config) and `{{...}}` at runtime (untrusting user input) are fundamentally different. We enforced that they never mix — you can't have `${VAR}` inside a `{{...}}` value, and vice versa. This turned interpolation from a footgun into a clear, auditable seam.

**Capability registries scale cleanly**. Each new resource (or new feature on an existing resource) lands as a capability: register a decoder, a transform, a sanitizer, a converter. The framework stays untouched. The arXiv yaml stays readable. This is how you make a system extensible without chaos.

## Next Steps

1. **Monitor for regressions** (1–2 weeks): Full e2e through vault, timeline, history. Watch metrics (vault write latency, pagination correctness, retry counter on timeout). If any drift appears, the old code is still in git history.

2. **Add the SSRF denylist before any non-arXiv resource**: v1 ships with minimal SSRF (same-host/scheme validation + 3-redirect cap), sufficient only because arXiv is fixed + public. Before enabling a config-driven or user-influenced host, add RFC1918 / loopback / link-local / metadata denylist to `transport.go` → `checkEgress`. The seam is already isolated; the denylist drops in without reshaping transport.

3. **Operationalize "add a resource"**: The plan included `docs/adding-a-resource.md` (new). Ensure one team member follows it end-to-end to add a dummy resource (even if unpublished) to validate the docs are correct.

## Quality Gates

All passed:
- `go build ./...` ✓
- `go test ./...` (full race detector) ✓
- Golden regression gate ✓
- Frontend `npm run build` ✓
- Code review: no critical/high issues; one medium fixed (body-read now honors retry policy; was terminating on first error).
- Pre-existing `candidate-list.tsx` lint issue left untouched per minimal-impact rule.

---

**Files modified**: 40+; **Files deleted**: 4 (discovery.go, papercontent.go, papercontent-cleanup.go, entire arxivquery/); **Files created**: 23 (resource engine, declarations, tests, docs).
