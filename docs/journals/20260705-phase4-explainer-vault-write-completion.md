# Phase 4 Completion: Explainer Generation and Vault Write

**Date**: 2026-07-05 21:15
**Severity**: High (concurrency race in shared state, atomic write design, API contract maturity)
**Component**: Backend Explainer Agent / Vault Writer Tool / Orchestrator Pipeline / Frontend Result Display
**Status**: Resolved (all `-race` tests pass, fake-LLM e2e verified, live-key smoke test deferred per user choice)

## What Happened

Completed Phase 4: text-only `ExplainerAgent` that generates a 9-section re-teaching explainer from paper Markdown; `VaultWriterTool` that atomically writes YAML frontmatter + explainer to Obsidian vault; extended `runPipeline` through generating→writing→complete stages; added `GET /result/{sessionId}` endpoint returning final Markdown + vault path + token usage; and wired the frontend with `ResultPanel` showing success banner, token count, and rendered preview. All three LLM providers (Anthropic, OpenAI, Gemini) verified via fake-LLM httptest integration test. Atomic write safety confirmed via concurrent `MarkAsProcessed` test under `-race`. Explainer validates 9 exact section headings; missing heading degrades gracefully (slog.Warn, full Content still written). Empty generation result (degenerate case) treated as recoverable failure so paper re-surfaces. Result endpoint returns 404 until pipeline reaches complete stage, forcing polled visibility and preventing partial reads.

## The Brutal Truth

We shipped a feature with **architectural clarity but operational friction**. The sandbox's Go-cache and npm-cache restrictions meant every `go build`, `go test`, and `npm run build` command required `dangerouslyDisableSandbox: true` — which introduced a modal approval dialog for each invocation. On a typical loop of 5 compile-test cycles, this added 15+ seconds of context-switching per session. It's a loud reminder that AI development in restricted environments needs to bake sandbox expectations into the workflow (precompile binaries, memoize build results, batch commands). Not a showstopper, but absolutely exhausting.

The Phase 4 PRD (docs/phase4/prd.md) was **completely misaligned with what got built**. It described a PDF-page-images + vision-model pipeline: `PageImages`, `PDFRendererTool`, `paper.Category`, `paper.Published.Format`, etc. None of that exists. Phases 1–3 shipped text-only HTML→Markdown, and Phase 4 stays text-only. The PRD was written for a hypothetical future that never materialized. We relied on the Phase 4 brainstorm summary (the actual architecture doc) as the source of truth and wrote code against that. The reconciliation happened post-implementation (Phase 07 rewrote the PRD and created development-roadmap.md + project-changelog.md). This is a painful lesson: PRDs must be updated the moment reality diverges, or they become liabilities.

## Technical Details

**ExplainerAgent** (`internal/agents/explainer.go` + `explainer-prompt.go`)

- Text-only agent: consumes `session.Markdown()`, sends it through `llm.LLMClient.Complete()` as `CompletionRequest.DocumentText` (prefixed "Paper content:" inside the prompt). Zero image/vision paths.
- System prompt hardcodes 9 section headings: Problem Statement, Core Idea, Methodology, Key Findings, Limitations, Why It Matters, Analogies & Intuition, Glossary, Follow-Up Papers.
- Parse loop: splits response by `## `, matches each heading to a stable slug (e.g., "Problem Statement" → "problem_statement"), populates `ExplainerOutput.Sections` map.
- Missing heading → `slog.Warn`, but full `Content` always written regardless. Never silently drop a section; the user sees the warning, the vault file has the full text, and we can iterate.
- Returns `ExplainerOutput{Content, Sections map, InputTokens, OutputTokens}`. Errors propagated unchanged (retry is in the client, not here).

**VaultWriterTool** (`internal/tools/vaultwriter.go` + `vaultwriter-frontmatter.go`)

- Assembles YAML frontmatter (arxiv_id, title, authors, published date-part, category from `config.Agent.ArxivCategory`, generated_at timestamp, review_iterations:1 + review_passed:true for Phase 5 forward-compatibility, tags).
- Filename: `YYYY-MM-DD_arxivID_slug.md` (slug is titleized then kebab-cased).
- Path-traversal guard: `validateWithinBase(vaultBase, finalPath)` rejects `../` escapes.
- **Atomic write**: calls `writeFileAtomic(path, []byte(content))` which creates a unique temp (via `os.CreateTemp(dir, "."+filename+".*.tmp")`) in the same directory, writes+closes, then atomically renames. Temp is removed on ANY failure path (write failure, chmod failure, close failure, rename failure) — zero orphan `.tmp` files even if disk fills mid-write.
- Log update: calls `logCheck.MarkAsProcessed(paper, filename)`. If it fails, logs a WARNING (note already saved; only consequence is paper re-surfaces next run). Never fails the vault write for a log failure.

**Orchestrator Pipeline Extension** (`internal/orchestrator/orchestrator-pipeline.go`)

- `runPipeline` extended past Phase 3 SetMarkdown seam:
  - `s.SetStage(models.StageGenerating)`: invokes `o.explainer.Generate()`.
  - Empty-content guard: if response is empty after trim, fail recoverable (paper re-surfaces).
  - `s.SetStage(models.StageWriting)`: invokes `o.vault.WriteToVault()`.
  - `s.SetStage(models.StageComplete)`: pipeline complete.
- SelectedPaper defensive guard: verifies paper is set before invoking agents (prevents nil panic on a fully-detached goroutine).
- Error classification via `describeGenErr()` and `vaultRecoverable()`: permission/disk errors are non-recoverable (permission denied, quota exceeded); others are retryable.

**Result Endpoint** (`internal/orchestrator/orchestrator.go`)

- `HandleResult(w http.ResponseWriter, r *http.Request)` — `GET /result/{sessionId}`.
- Returns 404 until `session.Stage == StageComplete` (forces polling visibility).
- Reads server-only fields via dedicated accessors (`s.Explainer()`, `s.VaultFile()`, `s.TokensUsed()`) — these are NOT in `Snapshot()` to prevent large Content from riding the status poll.
- Returns `ResultResponse{Content, VaultFile, TokensUsed}`.

**Frontend Result Display** (`components/result-panel.tsx`, `components/markdown-preview.tsx`)

- `ResultPanel`: shows success banner (green, vault path), token count, and `MarkdownPreview` (react-markdown + remark-gfm for GFM + strikethrough support).
- `MarkdownPreview`: renders `react-markdown` with custom heading sizes.
- Error surface: added `ErrorBanner` to discovery-panel.tsx; if result fetch fails after `stage=complete`, shows error + retry button.
- Frontend polling (from discovery-panel.tsx) continues polling `/status` until `stage=complete`, then switches to polling `/result` to fetch the full content once.

**Code Review Fixes**

- **CRITICAL CONCURRENCY**: A single shared `*LogCheckTool` did an unsynchronized read-modify-write of processed.json in `MarkAsProcessed()` (read log → append → write). Two concurrent sessions processing different papers could lose a log entry (lost update race). FIX: added `sync.Mutex mu` to LogCheckTool, guard the entire MarkAsProcessed body with `t.mu.Lock()` / `defer t.mu.Unlock()`. Added `TestMarkAsProcessedConcurrent()` — 20 concurrent calls, verify all 20 entries survive (runs under `-race`, flag would fail without the mutex).

- **Atomic Write Uniqueness**: VaultWriterTool initially used a fixed temp name (`finalPath + ".tmp"`). Two concurrent sessions writing to the same paper (or different papers, same output dir) would share one temp file, interleaving writes. FIX: extracted `writeFileAtomic()` helper using `os.CreateTemp(dir, "."+filename+".*.tmp")` — guarantees unique temp name per write. Used by both VaultWriter and LogCheckTool's writeLogAtomic.

- **Orphan Cleanup**: If os.WriteFile fails mid-write (e.g., ENOSPC), the temp file remains. FIX: `writeFileAtomic` uses `defer os.Remove(tmp)` at the top, so any failure path removes the temp. Rename success path renames it away, so Remove becomes a harmless no-op.

- **Error Classification**: EROFS (read-only filesystem) and EDQUOT (disk quota exceeded) were missing from `vaultRecoverable()`. FIX: added alongside os.ErrPermission and ENOSPC.

- **Empty Content Guard**: An empty (but error-free) LLM response would be written as a frontmatter-only note and mark the paper processed, silently dropping the paper. FIX: after generation, if `strings.TrimSpace(ex.Content) == ""`, fail recoverable so paper re-surfaces. Added orchestrator test.

- **Frontend Error Surface**: No error UI if `/result` fetch failed after `stage=complete`. User saw a spinner forever. FIX: added ErrorBanner with retry button in discovery-panel.tsx.

- **File Size**: orchestrator.go exceeded 200 lines. FIX: split into `orchestrator.go` (handlers) + `dto.go` (ResultResponse + types) + `pipeline-errors.go` (error classifiers).

## What We Tried

- **Vision-Model Pipeline**: Rejected early (Phase 4 brainstorm summary superseded the PRD's vision spec because Phases 1–3 never built image support).
- **Fixed Temp Filenames**: Initial approach (`finalPath + ".tmp"`). Failed: concurrent writes collided. Switched to `os.CreateTemp` for unique names.
- **Per-Session LogCheckTool**: Initial approach created a new `LogCheckTool` per session. Failed: no global state → no de-duplication across sessions. Switched to shared instance with mutex.
- **Hard-Fail Empty Content**: Initial approach — treat empty response as a client error. Changed to recoverable (better UX; paper re-surfaces).

## Root Cause Analysis

**Why Concurrency Race Happened**: LogCheckTool was shared across sessions but treated as immutable (no synchronization). The read-modify-write of processed.json is inherently stateful. A single mutex around the critical section (read + append + write) is the minimal fix. This is non-obvious in Go because the tool is a simple service that *looks* stateless to callers (callers don't track state), but internally it's reading and modifying a persisted file — that is always a critical section.

**Why PRD Diverged**: The PRD was written early (Phase 4 scoping) with vision assumptions (PDF rendering, image inference). Phases 1–3 shipped text-only architecture without revisiting the PRD. No checkpoint to validate PRD-vs-code parity until Phase 4 implementation began. Lesson: PRDs have a shelf life; they must be reviewed at the start of each phase, or they become actively harmful (wrong function signatures, missing error cases, outdated architecture).

**Why Sandbox Friction Happened**: Go's build system aggressively caches in `~/Library/Caches/go-build` and `~/Library/Application Support/go`. The sandbox denies access. Each command requires approval. This is an environmental constraint we can't code around — it's a workflow inefficiency that compounds over multiple iterations. Future sessions should batch-compile or use prebuilt binaries.

## Lessons Learned

**Concurrency in Persistent State Requires Explicit Guards**: A service that reads/modifies a file is always a critical section, even if it *looks* stateless to callers. Always wrap file-level read-modify-write with a mutex. A single line of code (`mu sync.Mutex`) prevented data loss. This is cheap insurance for shared tools.

**Atomic Write is Non-Negotiable for Crash-Safety**: Using `temp → rename` for writes is standard practice, but the temp name must be unique (via `os.CreateTemp`), and the temp must be removed on any failure. A single `defer os.Remove(tmp)` at the top prevents orphans even in partial-write scenarios (ENOSPC, EROFS, etc.). This is table-stakes for production file I/O.

**Empty Results are Degenerate Cases**: A successful LLM call that returns no text should not be treated the same as a successful write. It indicates a problem upstream (prompt mismatch, truncation, client/model failure). Fail it recoverable so the paper re-surfaces and the user can retry with diagnostics. This prevents silent data loss.

**PRD Needs Checkpoint Review**: Architectural decisions made in Phase N often look wrong by Phase N+2. Always read the PRD at the start of implementation and flag divergences immediately. Waiting until post-implementation creates rework and confusion. A 15-minute checkpoint review (PRD vs latest architecture doc) saves hours of redoing work.

**Frontend Error Surfaces Must be Complete**: If a fetch can fail, render an error state with a retry path. Don't let spinners block the user indefinitely. This is low-complexity, high-value polish.

**Graceful Degradation of Sections**: Missing section headings should log and continue, never silently drop the section. The explainer is still saved with whatever sections were present; the user sees the warning in logs. This respects the principle that a partial artifact is better than data loss.

## Next Steps

- **Live-Key Smoke Test** (OPTIONAL, deferred per user choice): Run one real-LLM call (Anthropic, OpenAI, or Gemini) end-to-end. Verify token parsing, generation quality, and vault write on an actual paper. Expected: 30 minutes, confirms live behavior parity with fake-LLM tests.
- **Phase 5 Prep** (Review Loop): `ExplainerOutput` has forward-compatibility fields (`review_iterations`, `review_passed`). Phase 5 will extend this with a revision agent and feedback loop. No structural changes needed.
- **Docs Update**: Reconciliation of Phase 4 PRD, development-roadmap.md, and project-changelog.md was done post-implementation. This has been captured; no further action.
- **Commit**: Squash and merge to master as commit a984097 — "feat: phase 4 explainer generation and vault write".

---

**Session context**: Delivered via implementation workflow. Code review caught critical race condition in shared LogCheckTool (high-severity catch; data loss prevented). Atomic write design verified safe under concurrent writes (`-race` clean). Fake-LLM integration test exercised full pipeline to complete stage + GET /result + on-disk YAML frontmatter, plus a vault-failure e2e. All 7 backend packages pass `-race`, frontend builds clean, linting clean. Backend ready for Phase 5 (review loop). Frontend ready for result display and error recovery. Sandbox friction noted for future workflow optimization.
