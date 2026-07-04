# Phase 03 — LogCheckTool (dedup)

**Context:** `docs/phase2/prd.md` §2.5, §7 · `docs/phase2/brainstorm-summary.md`
**Priority:** Critical · **Status:** pending · **Depends on:** 01 · **Effort:** ~S/M

## Overview
The system's memory. Reads `~/.arxiv-agent/processed.json`, filters out already-processed
paper IDs. Read path only in Phase 2 (`FilterUnprocessed`); `MarkAsProcessed` is stubbed/
deferred to Phase 4. Guarantees a processed paper is never re-surfaced (PRD Trust NFR).

## Key insights
- Missing file = first run = all papers unprocessed (NOT an error).
- Malformed JSON IS an error (no silent empty-set — would falsely re-surface everything).
- Filtering is a local set lookup → microseconds → no user-visible "filtering" stage.

## Requirements (PRD F3, F6)
- Read log, build `Set<paper_id>`, return papers whose ID ∉ set (order preserved = recency).
- Missing file → return all papers, `nil` error.
- Corrupt JSON → typed error (`ErrLogCorrupted`), surfaced as non-recoverable.
- Path validated against configured base (no traversal).

## Related code files
**Create:**
- `backend/internal/tools/logcheck.go` — `LogCheckTool`, `FilterUnprocessed`, `readLog`,
  `MarkAsProcessed` (Phase 4 stub — signature only, returns `errNotImplemented` or minimal
  no-op documented as Phase 4). Keep read path fully implemented.
- `backend/internal/tools/logcheck_test.go` — missing file, empty log, some-processed,
  all-processed, corrupt JSON.

## Design detail
```go
package tools

type processedEntry struct {
    PaperID     string `json:"paper_id"`
    Title       string `json:"title"`
    ProcessedAt string `json:"processed_at"`
    VaultFile   string `json:"vault_file"`
}
type processedLog struct {
    Processed []processedEntry `json:"processed"`
}

type LogCheckTool struct { cfg *config.PathsConfig }
func NewLogCheckTool(cfg *config.PathsConfig) *LogCheckTool

var ErrLogCorrupted = errors.New("processed log file is corrupted")

// FilterUnprocessed returns papers not present in the log, preserving input order.
func (t *LogCheckTool) FilterUnprocessed(papers []models.Paper) ([]models.Paper, error)
```
`FilterUnprocessed` logic:
```go
log, err := t.readLog()
if errors.Is(err, os.ErrNotExist) { return papers, nil }   // first run
if err != nil { return nil, ErrLogCorrupted }              // parse failure
processed := set of log.Processed[].PaperID
return papers where !processed[p.ID]
```
> **Why corrupt ≠ empty:** treating a parse error as "empty log" would re-surface every
> processed paper — a direct violation of the Trust NFR. Fail loud, non-recoverable.

## Implementation steps
1. Define `processedEntry`/`processedLog`, tool + constructor.
2. `readLog`: `os.ReadFile` → distinguish `os.ErrNotExist` from unmarshal error.
3. `FilterUnprocessed`: set build + filter, order-preserving.
4. `MarkAsProcessed` stub with clear Phase-4 TODO doc comment.
5. Tests covering all branches (use `t.TempDir()` for fixtures; missing-file case = no write).
6. build + `go test ./internal/tools/` green.

## Todo
- [ ] log structs + tool + constructor
- [ ] `readLog` distinguishing not-exist vs corrupt
- [ ] `FilterUnprocessed` order-preserving set filter
- [ ] `MarkAsProcessed` Phase-4 stub (documented)
- [ ] `logcheck_test.go`: missing / empty / partial / all / corrupt
- [ ] build + test green

## Success criteria
- No `processed.json` → all papers returned, no error.
- Papers with IDs in log excluded; input order preserved.
- Corrupt JSON → `ErrLogCorrupted`.

## Risks
- Path traversal via config — validate log path resolves under expected base (Phase 1 already
  enforces absolute path; add a `filepath.Clean` check).

## Security (PRD §7)
- Validate `processed.json` path before read. No paper data persisted in Phase 2.
