# Phase 07 — Integration, Docs, Dead-Code Cleanup

**Priority:** Medium · **Status:** pending · **Depends on:** phase-06

## Overview

Verify the whole system end-to-end, remove residual dead config/code, and bring
the docs in line with the new architecture. Ship the "add a resource = a YAML
file" story explicitly, including the contract for adding a new engine capability.

## End-to-End Verification

- Full stack up (`make` / docker-compose): pick arxiv → default category →
  optional keywords → fetch → select → explainer/reviewer → vault write → run
  timeline → history. Confirm **behavior identical to pre-refactor** (this is the
  v1 promise).
- Exercise: 404 re-pick, arXiv retry counter, "load more" pagination, context
  warning — all still work (they route through the engine now).
- Confirm `GET /resources` drives the UI; empty/legacy `/discover` body still works.

<!-- Updated: Validation Session 1 — V4 old-tool deletion moved here; V3 deferred egress -->
**V4 — delete the old arXiv tools HERE** (moved from Phase 04): remove
`discovery.go`/`papercontent.go`/`papercontent-cleanup.go`/`arxivquery` + the temporary
`useLegacyArxiv` build tag/switch, only after the full e2e below is green. Until this point
they stayed compiled as the A/B golden oracle.
**V3 — note the deferred egress denylist:** the private-IP/link-local/metadata SSRF denylist is
intentionally NOT in v1 (arXiv host is fixed + public). Record it in `docs/adding-a-resource.md`
as a required step before any config-driven / non-arXiv resource is enabled.

## Cleanup

- Prune `AgentConfig` fields that are now ONLY referenced via `${...}` from
  `arxiv.yaml` and nowhere in Go — decide per field: keep those still validated
  globally (fetch/display limits) vs. move fully into the declaration. Remove
  truly-dead validation in `config.go` `AgentConfig.validate()` (275–318).
- Remove any leftover imports of `arxivquery`/`tools.DiscoveryTool` (should be
  none after Phase 04).
- `.env.example` — document `RESOURCES_DIR`; note `AGENT_ARXIV_*` are now consumed
  by `resources/arxiv.yaml` (unchanged names).

## Docs

- `docs/design-notes/2026-07-18-declarative-resource-engine.md` — write the
  approved design (Problem/Structure/Tradeoffs per CLAUDE.md template): four-stage
  pipeline, Normalization Layer (ACL) guarantees, dual interpolation, capability
  registries, v1 scope fence, rejected alternatives (in-process adapters;
  fully-declarative-including-arxiv-from-day-1; broad capability set).
- `docs/architecture.md` — replace the arXiv-tools section with the engine +
  registry + `Source` interface; note the agent pipeline is unchanged.
- **`docs/adding-a-resource.md`** (new) — the operator guide: (a) resource that
  fits existing capabilities = drop a `resources/<id>.yaml`; (b) resource needing
  a new shape = add the capability (decoder/field-type/transform/converter) at its
  registry seam, then the YAML. Include the capability-registration contract so
  "extend the engine" is well-defined and low-risk.
- `docs/development-roadmap.md` + `docs/project-changelog.md` — mark the feature;
  note it supersedes `260718-1353-runtime-category-keyword-exploration`.
- Mark `plans/260718-1353-...` superseded in its overview (already completed).

## Red Team Fixes (2026-07-18) — applied

- **F7 (H3):** remove the temporary `GET /categories` alias here (after Phase 06 shipped).
- **F8 (H4):** add a **vault-output assertion** to the e2e — the written note's frontmatter
  `category` is unchanged vs pre-refactor (candidate-field equality alone is insufficient).
- **F12 (H8):** confirm the ported-test checklist (Phase 04) is fully green before final
  dead-code removal; the discovery golden alone does not authorize deletion.

## Todo

- [ ] full e2e click-through (arXiv identical behavior)
- [ ] config/dead-code prune
- [ ] .env.example RESOURCES_DIR
- [ ] design note + architecture.md + adding-a-resource.md
- [ ] roadmap + changelog
- [ ] `go build ./... && go test ./... && (cd frontend && npm run build)`

## Success Criteria

- End-to-end arXiv flow identical to pre-refactor, now through the engine.
- No dead arXiv-specific Go code or config remains.
- A newcomer can add a resource from `docs/adding-a-resource.md` alone.

## Risks

- Config pruning breaking `${...}` resolution — verify each removed field isn't
  referenced by any declaration before deleting.
- Doc drift — write docs against the shipped code, not this plan (CLAUDE.md rule).
