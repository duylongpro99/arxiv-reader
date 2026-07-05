# Phase 04 — Orchestrator Pipeline + /result Endpoint

**Context:** `docs/phase4/brainstorm-summary.md` §4 (Orchestrator) · `docs/phase4/prd.md` F6/F7 · `backend/internal/orchestrator/*` · `backend/internal/server/server.go`
**Priority:** Critical · **Status:** complete · **Depends on:** 01, 02, 03 · **Effort:** ~M

## Overview
Wires the intelligence to the pipeline. `runPipeline` continues past the Phase 3 seam
(`SetMarkdown`) through `generating → writing → complete`: call the ExplainerAgent, store output +
tokens, write to the vault, mark processed, flip to complete. Add `GET /result/:sessionId` +
register it. This is the only backend phase that edits `orchestrator.go`/`server.go`.

## Key insights (locked decisions)
- **Consumer interfaces (test fakes) defined here**, mirroring the existing `PaperContent` pattern:
  ```go
  type Explainer interface {
      Generate(ctx context.Context, in agents.ExplainerInput) (models.ExplainerOutput, error)
  }
  type VaultWriter interface {
      WriteToVault(ctx context.Context, ex models.ExplainerOutput, p models.Paper) (string, error)
  }
  ```
- **`runPipeline` already fetches Markdown and stops at `SetMarkdown` (Phase 3 seam).** Extend the
  SAME function — do not fork. After `SetMarkdown(md)`, continue in-line.
- **Read the full paper via `s.SelectedPaper()`** (added in Phase 01) for `ExplainerInput.PaperMeta`
  + `WriteToVault`. `runPipeline`'s existing `paperID` arg stays (used for `FetchMarkdown` + logs).
- **`New` constructs the concrete tools once.** `logCheck` is currently held as the `Unprocessor`
  interface; construct the **concrete `*tools.LogCheckTool`** as a local, assign it to the
  `logCheck` field AND pass it to `NewVaultWriterTool` (VaultWriter needs `MarkAsProcessed`, which
  isn't on `Unprocessor`). Construct `agents.New(client, cfg)` for the explainer.
- **`/result` reads server-only fields** via Phase 01 accessors (`Explainer()`, `VaultFile()`,
  `TokensUsed()`); 404 unless stage is `complete`. It is NOT part of `Snapshot()`/`/status`.
- **Failure mapping:** generation error → `Fail(msg, recoverable=true)` (retryable). Vault-write
  error → `Fail`; permission/disk = **non-recoverable**, other = recoverable (extend
  `describeError` or classify inline). Log NOT updated on vault failure (handled in Phase 03).

## Requirements (PRD F6, F7)
- Extend `runPipeline(ctx, s, paperID)` after `s.SetMarkdown(md)`:
  ```go
  paper := s.SelectedPaper() // *models.Paper (server-held)
  s.SetStage(models.StageGenerating)
  ex, err := o.explainer.Generate(ctx, agents.ExplainerInput{ MarkdownText: md, PaperMeta: *paper })
  if err != nil { s.Fail(describeGenErr(err)); return }
  s.SetExplainer(&ex); s.AddTokens(ex.InputTokens + ex.OutputTokens)

  s.SetStage(models.StageWriting)
  path, err := o.vault.WriteToVault(ctx, ex, *paper)
  if err != nil { s.Fail(vaultErrMsg(err), vaultRecoverable(err)); return }
  s.SetVaultFile(path)
  s.SetStage(models.StageComplete)
  ```
  Add `pipeline complete` structured log (`session_id`, `paper_id`, `total_duration_ms`).
- `Orchestrator` struct gains `explainer Explainer`, `vault VaultWriter`; wired in `New`.
- `ResultResponse` DTO + `HandleResult`:
  ```go
  type ResultResponse struct {
      Content    string `json:"content"`
      VaultFile  string `json:"vaultFile"`
      TokensUsed int    `json:"tokensUsed"`
  }
  func (o *Orchestrator) HandleResult(w, r) {
      id := r.PathValue("sessionId")
      v, ok := o.sessions.Load(id); if !ok { 404 JSON }
      s := v.(*models.PipelineSession)
      if s.Snapshot().Stage != models.StageComplete { 404 "result not ready" }
      writeJSON(w, 200, ResultResponse{ s.Explainer().Content, s.VaultFile(), s.TokensUsed() })
  }
  ```
- Register `mux.HandleFunc("GET /result/{sessionId}", orch.HandleResult)` in `server.go`.
- Extend CORS methods? No — `GET` already allowed. No change needed.

## Related code files
**Modify:**
- `backend/internal/orchestrator/orchestrator.go` — `Explainer`/`VaultWriter` interfaces; struct
  fields; `New` wiring (concrete logCheck local → field + vaultwriter); `ResultResponse`;
  `HandleResult`.
- `backend/internal/orchestrator/orchestrator-pipeline.go` — extend `runPipeline`; add
  `describeGenErr`/`vaultErrMsg`/`vaultRecoverable` helpers (or extend `describeError`).
- `backend/internal/server/server.go` — register `GET /result/{sessionId}`.
- `backend/internal/orchestrator/orchestrator_test.go` — fakes for `Explainer` + `VaultWriter`;
  cases: full happy path → `complete` + explainer/vaultFile/tokens set; generation error →
  `failed` recoverable; vault error → `failed` (perm=non-recoverable); `/result` before complete →
  404; `/result` unknown session → 404; `/result` after complete → content+path+tokens.
- `backend/internal/server/integration_test.go` — extend the existing discover→select→process flow
  with a fake LLM (httptest or injected) + a `t.TempDir()` vault: poll to `complete`, then
  `GET /result` returns content + vault path, and the file exists on disk with valid frontmatter.

## Design detail
```go
// New (concrete logCheck shared with vaultwriter)
func New(cfg *config.Config) (*Orchestrator, error) {
    client, err := llm.NewLLMClient(&cfg.LLM); if err != nil { return nil, err }
    logCheck := tools.NewLogCheckTool(&cfg.Paths)
    return &Orchestrator{
        cfg: cfg,
        disco: tools.NewDiscoveryTool(&cfg.Agent),
        logCheck: logCheck,
        content: tools.NewPaperContentTool(&cfg.Agent),
        llm: client,
        explainer: agents.New(client, cfg),
        vault: tools.NewVaultWriterTool(cfg, logCheck),
    }, nil
}
```
> **`llm` field:** now that `explainer` holds the client, the orchestrator's own `llm` field may be
> unused — remove it if so (KISS), or keep if a test references it. Verify with the compiler.
> **Error classify:** a small `vaultRecoverable(err) bool` inspecting `errors.Is(err, os.ErrPermission)`
> and disk-full (`syscall.ENOSPC` best-effort) → `false`; default `true`. Keep simple; the PRD's
> §7 table is the spec.

## Implementation steps
1. Add `Explainer`/`VaultWriter` interfaces + struct fields; wire `New` (shared concrete logCheck).
2. Extend `runPipeline` past `SetMarkdown` through generating→writing→complete with logs.
3. Add `HandleResult` + `ResultResponse`; register route in `server.go`.
4. Failure classification helpers (gen retryable; vault perm/disk non-recoverable).
5. Orchestrator tests (fakes for explainer + vault) + integration test (fake LLM + TempDir vault).
6. Remove now-unused `llm` field if the compiler flags it.
7. `go build ./...` + `go test -race ./...` green.

## Todo
- [x] `Explainer` + `VaultWriter` consumer interfaces + struct fields
- [x] `New` wiring: shared concrete `LogCheckTool`, `agents.New`, `NewVaultWriterTool`
- [x] extend `runPipeline`: generating → SetExplainer/AddTokens → writing → SetVaultFile → complete
- [x] failure mapping (gen recoverable; vault perm/disk non-recoverable; log untouched on fail)
- [x] `HandleResult` + `ResultResponse` (404 unless complete) + route registration
- [x] orchestrator_test.go: happy / gen-error / vault-error / result-not-ready / result-ok
- [x] integration_test.go: discover→select→process→complete→`GET /result` + file on disk
- [x] `go test -race ./...` green

## Success criteria
- Full single-pass run reaches `complete`; `/result` returns content + vault path + tokens.
- Generation failure → `failed` recoverable (paper re-processable, no vault file, log untouched).
- Vault permission/disk failure → `failed` non-recoverable; no `processed.json` update.
- `/result` 404s until `complete`. No data races.

## Risk Assessment
| Risk | L×I | Mitigation |
|---|---|---|
| `SelectedPaper()` nil in goroutine | Low×High | `HandleProcess` always `SetSelectedPaper` before spawning; guard nil → `Fail` defensively. |
| Sharing one `LLMClient` across explainer (+Phase 5 reviewer) | Low×Low | Client is stateless/concurrency-safe; single instance intended. |
| Unused `llm` field breaks build | Low×Low | Remove if flagged; caught immediately by `go build`. |
| Long paper exceeds context window | Med×Med | Surfaced as LLM 400/error → `failed` recoverable; Gemini fallback via config (R5). |

## Backwards compatibility
`/discover`, `/process`, `/status` untouched. `StatusResponse` unchanged — new stages
(`generating`/`writing`/`complete`) flow through the existing `stage` field; the frontend keeps
polling until Phase 05 adds labels + the result fetch. `New` signature unchanged (still
`(*Orchestrator, error)`).

## Rollback
Revert the `runPipeline` extension (stops at `SetMarkdown` again), remove `HandleResult` + route +
struct fields. In-memory only.

## Security
`/result` keyed by server-issued session ID; returns only that session's own generated content +
its vault path. No new external input surface. Loopback bind + narrow CORS unchanged.

## Next Steps
Unblocks Phase 05 (result UI) — API contract `{content, vaultFile, tokensUsed}` is now fixed.
File ownership: solely owns `orchestrator.go`/`orchestrator-pipeline.go`/`server.go` + their tests.
