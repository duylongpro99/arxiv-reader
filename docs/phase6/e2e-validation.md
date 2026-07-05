# Phase 6 — End-to-End Validation Checklist (F8)

A runnable manual checklist for the full HTML→Markdown→explainer→vault pipeline.
Run it once per provider you have a key for. This is **not** automated — the Go
unit/integration suites cover logic; this verifies the real system end-to-end
against live arXiv + a real LLM + a real Obsidian vault.

## Preconditions
- [ ] `make dev` runs; frontend on `:3000`, backend on `127.0.0.1:8080`.
- [ ] `curl -s 127.0.0.1:8080/health` → `{"status":"ok",...}`.
- [ ] `config.yaml` `paths.obsidian_vault` points at a real, writable vault.
- [ ] `.env` has a valid `LLM_API_KEY` for the provider under test.

## Core run
- [ ] Trigger discovery → up to 5 candidates displayed.
- [ ] Select one → stage advances: extracting → generating → (reviewing/revising) → writing → complete.
- [ ] Note opens in Obsidian under `AI Papers/`; all frontmatter fields render
      (`title`, `authors`, `published`, `arxiv_id`, `review_iterations`,
      `review_passed`, `review_score` when reviewer enabled).
- [ ] `processed.json` updated after success (paper recorded).
- [ ] Trigger discovery again → the same paper does **not** re-surface (dedup).
- [ ] Success panel shows token usage; cost shown when the model is priced.

## Retry (forced recoverable failure)
- [ ] Force a recoverable failure (e.g. temporarily make the vault dir read-only,
      or point `paths.obsidian_vault` at a bad path, trigger a run, then fix it).
- [ ] Click **Retry** → pipeline resumes **without re-selecting a paper**.
- [ ] Vault-only failure retry re-writes with **no additional LLM tokens**
      (compare token count before/after — unchanged).
- [ ] Non-recoverable failure (e.g. permission denied) shows **no** retry button.

## Reviewer settings
- [ ] `max_review_iterations: 0` → no review; explainer saved immediately
      (`review_iterations: 0`, `review_passed: true` in frontmatter).
- [ ] `max_review_iterations: 2` → up to 2 review cycles; frontmatter reflects
      the real iteration count and pass/fail.

## Context-window advisory (optional)
- [ ] Select an unusually large paper (or lower a model's entry in
      `limits.go` locally) → an amber, non-blocking context warning appears and
      the pipeline still proceeds.

## Cross-provider matrix
Record model + observed cost for each provider you can run. If a provider is
unverified (no key), note it rather than claiming a pass.

| Provider | Model | Ran? | Notes / observed cost |
|---|---|---|---|
| anthropic | `claude-sonnet-4-6` | ☐ | |
| openai | `gpt-4o` | ☐ | |
| gemini | `gemini-2.0-flash` | ☐ | |

## Result
- Date run: ______
- Providers verified: ______
- Issues found: ______
