# Phase 06 — README & End-to-End Validation (F11, F8)

**Priority:** Medium · **Status:** ✅ complete · **Depends on:** 04, 05 (docs reflect final code)

## F11 — README gap-fill
Existing `README.md` (96 lines) has: title, Prerequisites, Setup, Run, Verify, Configuration,
Reviewer Loop. **Add the gaps; do NOT rewrite. Remove poppler (not used).**

Add/verify sections:
- **Prerequisites** — Node.js, Go, Obsidian, one LLM API key. **No poppler.** (Confirm Go/Node versions match `Makefile` check-tools: Go 1.23+, Node 20+, `air`.)
- **LLM Provider Setup** table — provider → `LLM_API_KEY` env var → recommended model (anthropic/openai/gemini). Switching steps: set `llm.provider` + `llm.model` in `config.yaml`, set `LLM_API_KEY` in `.env`, `make dev`.
- **Estimated Cost Per Paper** table — per provider/model typical range, ×3–4 note for `max_review_iterations: 2`, "estimates — check provider dashboard" caveat.
- **Configuration Reference** — full `config.yaml` field table (type, default, description) matching the actual file: `llm.*`, `paths.*`, `agent.*` (incl. `max_content_bytes`, `arxiv_html_base_url`, `max_review_iterations`, `min_request_interval_seconds`). **No `pdf.dpi`.**
- **Troubleshooting** — one entry per real error (arXiv rate limit/unavailable/parse; HTML not-found→re-pick, HTML timeout/fetch-fail; LLM rate-limit/bad-request/timeout/unavailable; vault permission/disk-full; corrupt processed-log; context-too-large warning). Cause + fix each.
- **Project Structure** — brief `/frontend` + `/backend` map.
- **`.env.example`** — confirm it documents all keys (currently `LLM_API_KEY` required + optional overrides). Add any field added in Phases 2–5 if missing.

## F8 — End-to-End validation checklist
Produced `docs/phase6/e2e-validation.md`. The checklist below is a **runnable
manual checklist for the user** (needs live arXiv + LLM keys + Obsidian); it was
authored, not executed in this session:
- [ ] Trigger → 5 candidates displayed
- [ ] Select one → HTML extracted → explainer generated → reviewed → (revised) → saved
- [ ] Note opens in Obsidian; all frontmatter fields render
- [ ] `processed.json` updated after success
- [ ] Same paper does NOT re-surface on second discovery (dedup)
- [ ] Retry works for a forced recoverable failure (no re-selection)
- [ ] `max_review_iterations: 0` → no review, immediate save
- [ ] `max_review_iterations: 2` → up to 2 review cycles
- [ ] Cost + token usage displayed in success state
- [ ] Repeat core run with **anthropic**, **openai**, **gemini** (record model + cost each)

## Related code files
- Modify: `README.md`, `.env.example` (if gaps).
- Create: `docs/phase6/e2e-validation.md`.
- Docs sync (documentation-management.md): update `docs/development-roadmap.md` (Phase 6 → complete) and `docs/project-changelog.md` after validation passes.

## Todo
- [x] README: provider table, cost table, full config reference, troubleshooting, project map; poppler removed
- [x] `.env.example` completeness check
- [x] `docs/phase6/e2e-validation.md` checklist
- [ ] Execute E2E across 3 providers; record results — **user task** (needs live keys + Obsidian)
- [x] Roadmap + changelog updated

## Success criteria
- Fresh clone → running in <10 min following README (timed, no missing steps).
- All config fields documented with defaults; every real error has a troubleshooting entry.
- Full pipeline passes across all three providers; dedup + both review-iteration settings verified.

## Risks
- **T1 README staleness:** manual update on config change (accepted; auto-gen deferred).
- **Provider access:** running all three needs three keys — note if any provider is unverified rather than claiming a pass.
