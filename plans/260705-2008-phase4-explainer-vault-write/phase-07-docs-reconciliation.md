# Phase 07 — Docs Reconciliation (rewrite stale PRD)

**Context:** CLAUDE.md "Document Up-to-date" rule · `docs/phase4/prd.md` (stale) · `docs/architecture.md` · `docs/phase4/brainstorm-summary.md`
**Priority:** Medium · **Status:** complete · **Depends on:** 01–05 (can start once 04 lands) · **Effort:** ~S

## Overview
`docs/phase4/prd.md` documents a **vision/PDF-image** pipeline that was never built and whose Go
samples don't compile against the codebase. Rewrite it to the **text-only** reality so the docs stop
contradicting the code (CLAUDE.md: "reconcile stale docs against live code… update stale sections
before proceeding"). Also refresh roadmap/changelog per the documentation-management rule.

## Key insights (what's stale in the current PRD)
- `ExplainerInput{ PageImages [][]byte }`, `PDFRendererTool`, `pdfFetchTool`, "read every page
  image/diagram" → **replace** with `MarkdownText` + text-only `LLMClient.Complete(DocumentText)`.
- `paper.Published.Format(...)`, `paper.Category` → `Published` is a **string**; **no `Category`**
  (use `config.Agent.ArxivCategory`).
- `session.PDF/PageImages/TokensUsed`, `o.setSession`, exported `session.Stage =` → **private
  mutex accessors**; `sync.Map`; no `setSession`.
- `VaultWriterTool.WriteToVault(..., verdict *ReviewVerdict)` → **no verdict param** in Phase 4;
  `ReviewVerdict` is Phase 5.
- Token budget / "12–18k image tokens" section → replace with text-token characterization.
- Keep intact (still accurate): the product intent, the 9-section structure, F2 quality rules,
  F3 follow-up link rules, F4 atomic write + filename, F5 processed-log rules, F6/F7, success
  metrics, exit criteria (text interpretation).

## Requirements
- Rewrite `docs/phase4/prd.md` so **Part 1 (Product Requirements)** stays intent-faithful and
  **Part 2 (Architecture)** matches the implemented text-only design and the real interfaces
  (as built in Phases 01–05). Every Go snippet must reflect actual signatures.
- Add a short note at the top: "Supersedes the earlier image-based draft; reconciled to the
  text-only implementation on 2026-07-05. See `brainstorm-summary.md` for the decision."
- Update `docs/architecture.md` §2.6 (ExplainerAgent) / §2.8 (VaultWriter) only if any signature
  drifted from what's now built (it already describes text-only + `WriteToVault(ctx, explainer,
  meta)` — verify and align the `ExplainerOutput` fields, e.g. `InputTokens/OutputTokens`).
- Update `docs/development-roadmap.md` (Phase 4 → complete) and `docs/project-changelog.md`
  (Phase 4 entry: explainer generation + atomic vault write + `/result` + preview), per
  `.claude/rules/documentation-management.md`. Create these files if absent (match existing docs style).

## Related code files
**Modify:**
- `docs/phase4/prd.md` — full reconciliation to text-only.
- `docs/architecture.md` — align ExplainerAgent/VaultWriter/`ExplainerOutput` if drifted.
- `docs/development-roadmap.md` — Phase 4 status (create if missing).
- `docs/project-changelog.md` — Phase 4 entry (create if missing).

## Implementation steps
1. Rewrite `docs/phase4/prd.md` Part 2 against the shipped code; keep Part 1 intent.
2. Diff `docs/architecture.md` ExplainerAgent/VaultWriter sections vs the built code; align.
3. Update roadmap + changelog (or create, matching repo doc conventions).
4. Verify every code snippet in the rewritten PRD compiles conceptually against actual signatures
   (grep the real functions).
5. Optionally delegate to the `docs-manager` agent for consistency pass.

## Todo
- [x] rewrite `docs/phase4/prd.md` to text-only (Part 2 matches built interfaces)
- [x] top-of-file supersedes note
- [x] align `docs/architecture.md` ExplainerAgent/VaultWriter/`ExplainerOutput` if drifted
- [x] roadmap: Phase 4 → complete
- [x] changelog: Phase 4 entry
- [x] every PRD snippet matches real signatures

## Success criteria
`docs/phase4/prd.md` contains **zero** references to PDF images / `PageImages` / `PDFRendererTool` /
vision / `paper.Category` / `paper.Published.Format` / `setSession`; all snippets match the shipped
code; roadmap + changelog reflect Phase 4 completion.

## Risk Assessment
| Risk | L×I | Mitigation |
|---|---|---|
| Docs drift again vs code | Low×Med | Snippets copied from actual source; grep-verify signatures before finishing. |
| Roadmap/changelog don't exist | Med×Low | Create them following existing `docs/` style + the documentation-management rule. |

## Backwards compatibility
Docs only — no code impact. Prior phase PRDs (1–3) untouched.

## Rollback
`git revert` the doc commit. No runtime effect.

## Security
None (documentation).

## Next Steps
Final step before `/ck:journal`. Leaves the repo's docs internally consistent for Phase 5.
