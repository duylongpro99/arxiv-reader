# Phase 05 — Logging & Error Audit (F6, F1)

**Priority:** Medium · **Status:** ✅ complete · **Depends on:** 02, 03 (final code to audit)

Audit-and-fill, not build-from-scratch. Backend already has 38 `slog` calls across 10 files
incl. `pipeline complete`. Verify completeness against the **HTML** event table (no PDF).

## Corrected event table (HTML architecture)
| Event | Level | Required fields |
|---|---|---|
| Config loaded | INFO | provider, model, vault_path |
| Server started | INFO | addr |
| Discovery start / complete | INFO | session_id, count, duration_ms |
| arXiv retry | WARN | session_id, attempt, backoff_ms, error |
| Log check complete | INFO | session_id, unprocessed, returning |
| **HTML fetch** start / complete | INFO | session_id, paper_id, size_bytes?, duration_ms |
| **Markdown conversion** complete | INFO | session_id, paper_id, duration_ms |
| LLM (explainer) call start / complete | INFO | session_id, provider, model, input_tokens, output_tokens, duration_ms |
| LLM retry | WARN | session_id, attempt, backoff_ms, error |
| Review start / complete | INFO | session_id, iteration, score, pass, duration_ms |
| Vault write start / complete | INFO | session_id, vault_file, duration_ms |
| Log update success / failure | INFO/WARN | session_id, paper_id |
| Pipeline complete | INFO | session_id, paper_id, **input_tokens, output_tokens, total_tokens, estimated_cost_usd**, total_duration_ms, review_iterations, review_passed |
| Any stage failure | ERROR | session_id, stage, **code/action**, recoverable, cause |

## Steps
1. **Gap-fill:** diff existing `slog` calls (`discovery.go`, `papercontent.go`, `vaultwriter.go`, `llm/retry.go`, `agents/explainer.go`, `agents/reviewer.go`, `server.go`, `orchestrator-pipeline.go`, `orchestrator.go`, `cmd/server/main.go`) against the table. Add missing fields — priority: split `input_tokens`/`output_tokens` on LLM-complete and `estimated_cost_usd` on `pipeline complete` (now available from Phases 01/03).
2. **Failure logs:** ensure every `session.Fail(...)` path logs ERROR with the error action/recoverable + `cause` (wrap the existing `slog.Error("pipeline failed", ...)` to include action).
3. **Security (F1/PRD §7):** grep every `slog.*` arg — assert `LLM_API_KEY` / `cfg.LLM.APIKey` / raw `Authorization` headers never appear. Add a test or documented grep check.
4. **No raw HTML/secrets persisted** (CLAUDE.md): confirm logs carry extracted text/metadata only, never raw response bodies or keys.

## Related code files
- Modify: any of the 10 slog-bearing files with gaps (expected: `orchestrator-pipeline.go` for cost/token summary; `agents/reviewer.go` for token split logging).
- Add: a small audit note or test asserting no secret substrings in log output (optional but recommended).

## Todo
- [x] Diff slog calls vs HTML event table; list gaps
- [x] Add input/output tokens to LLM-complete logs
- [x] Add estimated_cost_usd + token split to `pipeline complete`
- [x] Failure logs include action + recoverable + cause
- [x] Secret-in-logs check (grep/test) passes
- [x] `go build ./... && go test ./...`

## Success criteria
- Every event-table row produces a log entry with all required fields.
- A failed run is fully reconstructable from logs alone.
- No API key / secret appears in any log entry (verified).

## Risks
- **Over-logging noise:** keep INFO to lifecycle + summary; don't log per-chunk. DRY the field set via the standard patterns already in use.
