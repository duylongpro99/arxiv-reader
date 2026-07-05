# Phase 03 — VaultWriterTool + LogCheckTool.MarkAsProcessed

**Context:** `docs/phase4/brainstorm-summary.md` §4 (VaultWriterTool, MarkAsProcessed) · `docs/phase4/prd.md` F4/F5 · `backend/internal/tools/logcheck.go`
**Priority:** Critical · **Status:** complete · **Depends on:** 01 · **Effort:** ~M

## Overview
Reliability, not intelligence. `VaultWriterTool.WriteToVault` assembles frontmatter + content,
generates the filename, writes **atomically** to `{vault}/AI Papers/`, then calls
`MarkAsProcessed`. `MarkAsProcessed` (currently a stub returning an error) is implemented here.
The guarantee: the vault never holds a partial/corrupt file, and `processed.json` is updated only
after a successful write. Independent of Phase 02 — needs only Phase 01.

## Key insights (locked decisions)
- **`WriteToVault(ctx, explainer, paper) (string, error)` — NO verdict param.** Phase 5 adds it.
  (Follows `architecture.md`, not the stale PRD's `verdict *ReviewVerdict` signature — that type
  doesn't exist yet.)
- **`Paper.Published` is a string** (e.g. `2024-01-15` or `2024-01-15T00:00:00Z`) — do NOT call
  `.Format()`. For frontmatter `published` and the filename date, take the **date part**: parse
  RFC3339 and format `2006-01-02`; on parse failure fall back to the first 10 chars; if that isn't
  a plausible date, fall back to `"unknown"` (never crash).
- **`category` ← `config.Agent.ArxivCategory`** — there is no per-paper Category field.
- **Frontmatter includes `review_iterations: 1` + `review_passed: true`** now (forward-compat).
- **Atomic write:** `MkdirAll("AI Papers", 0755)` → `WriteFile(tmp)` → `os.Rename(tmp, final)`;
  on rename failure `os.Remove(tmp)`. No partial file, no orphan `.tmp`.
- **Path-traversal guard** before writing: cleaned final path must be within the cleaned configured
  vault base (`config.Paths.ObsidianVault`).
- **`MarkAsProcessed` is warning-not-fatal after a successful write** — the note exists; a log
  failure just means the paper re-surfaces next run (acceptable, PRD F5 / NFR idempotency).
- **`MarkAsProcessed` never clobbers a corrupt log.** Reuse `readLog()`: missing → empty log
  (first entry); `ErrLogCorrupted` → return the error (do NOT overwrite). Then atomic temp→rename.

## Requirements (PRD F4, F5)
- `VaultWriterTool`:
  ```go
  type VaultWriterTool struct { cfg *config.Config; logCheck *LogCheckTool }
  func NewVaultWriterTool(cfg *config.Config, logCheck *LogCheckTool) *VaultWriterTool
  func (t *VaultWriterTool) WriteToVault(ctx context.Context, explainer models.ExplainerOutput, paper models.Paper) (string, error) // returns final path
  ```
- Frontmatter fields (exact keys, YAML-safe): `arxiv_id`, `title` (escaped), `authors` (YAML list,
  each escaped), `published` (date part), `category` (config), `generated_at`
  (`explainer.CreatedAt.UTC().Format(time.RFC3339)`), `review_iterations: 1`, `review_passed: true`,
  `tags: [ai, paper, explainer]`. Followed by `explainer.Content`.
- Filename `YYYY-MM-DD_arxivID_slug.md`; `slugify(title)` = lowercase → spaces to `-` → strip
  non `[a-z0-9-]` → collapse repeats → trim ≤60 at a word boundary; **sanitize arXiv ID** to
  `[a-z0-9._-]` (handles `2401.12345v2` and old `cs.AI/0123456`).
- `MarkAsProcessed(paper models.Paper, vaultFile string) error` — real implementation replacing the
  Phase 2 stub: read (missing=empty, corrupt=error), append
  `{PaperID, Title, ProcessedAt: time.Now().UTC() (RFC3339 string), VaultFile}`, atomic temp→rename.
- Logs: `vault write started/complete` (filename, path, duration_ms), `log updated`, and on log
  failure `slog.Warn("vault write succeeded but log update failed", …)`.

## Related code files
**Create:**
- `backend/internal/tools/vaultwriter.go`
- `backend/internal/tools/vaultwriter_test.go`

**Modify:**
- `backend/internal/tools/logcheck.go` — implement `MarkAsProcessed` (currently returns
  `errors.New("… not implemented until Phase 4")`); add atomic-write helper. `processedEntry`
  already has the right fields.
- `backend/internal/tools/logcheck_test.go` — `MarkAsProcessed` cases: append to existing log;
  create on missing log; **corrupt log → error, file untouched**; round-trip visible to
  `FilterUnprocessed`.

## Design detail
```go
func (t *VaultWriterTool) WriteToVault(ctx context.Context, ex models.ExplainerOutput, p models.Paper) (string, error) {
    vaultDir := filepath.Join(t.cfg.Paths.ObsidianVault, "AI Papers")
    filename := t.generateFilename(p)
    finalPath := filepath.Join(vaultDir, filename)
    if err := validateWithinBase(t.cfg.Paths.ObsidianVault, finalPath); err != nil { return "", err }
    if err := os.MkdirAll(vaultDir, 0o755); err != nil { return "", fmt.Errorf("create vault dir: %w", err) }

    content := t.buildFrontmatter(p, ex) + ex.Content
    tmp := finalPath + ".tmp"
    if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil { return "", fmt.Errorf("write temp: %w", err) }
    if err := os.Rename(tmp, finalPath); err != nil { os.Remove(tmp); return "", fmt.Errorf("finalize: %w", err) }

    if err := t.logCheck.MarkAsProcessed(p, filename); err != nil {
        slog.Warn("vault write succeeded but log update failed", "paper_id", p.ID, "vault_file", filename, "error", err)
    }
    return finalPath, nil
}
```
> **`published` date:** helper `dateOnly(s string) string` — `time.Parse(time.RFC3339, s)` →
> `Format("2006-01-02")`; else `len(s)>=10 && looksLikeDate(s[:10]) → s[:10]`; else `"unknown"`.
> **YAML escape:** wrap strings in double quotes and escape `\` and `"` (titles routinely contain
> `:`); authors rendered `["A", "B"]` with each escaped.
> **`validateWithinBase`:** compare `filepath.Clean` of base vs target with a separator-aware prefix
> check (avoid the `/vault2` vs `/vault` false-match — compare against `base + string(os.PathSeparator)`).

## Implementation steps
1. `vaultwriter.go`: struct + `New` + `WriteToVault` + `buildFrontmatter` + `generateFilename` +
   `slugify` + `dateOnly` + `escapeYAML` + `validateWithinBase`. Keep <200 lines (split a
   `vaultwriter-frontmatter.go` if needed).
2. `logcheck.go`: implement `MarkAsProcessed` + atomic-write helper; reuse `readLog()`.
3. Tests: vaultwriter into a `t.TempDir()` — asserts final file exists, no `.tmp` left, frontmatter
   parses as YAML, filename format, slug edge cases (unicode/long/symbols), traversal attempt
   rejected, rename-failure cleanup (e.g. make `finalPath` a directory). logcheck tests as above.
4. `go build ./...` + `go test -race ./...` green.

## Todo
- [x] `vaultwriter.go` — `WriteToVault` (atomic temp→rename, path guard, MkdirAll)
- [x] frontmatter builder (published-as-date, category-from-config, review fields, YAML-escaped)
- [x] `generateFilename` + `slugify` + arXiv-ID sanitize
- [x] implement `LogCheckTool.MarkAsProcessed` (missing=empty, corrupt=error, atomic write)
- [x] warning-not-fatal on post-write log failure
- [x] `vaultwriter_test.go` (TempDir): file present, no `.tmp`, YAML valid, traversal rejected, cleanup
- [x] `logcheck_test.go` — append / create / corrupt-untouched / round-trip
- [x] `go test -race ./...` green

## Success criteria
- Note lands at `{vault}/AI Papers/YYYY-MM-DD_id_slug.md`; frontmatter is valid YAML.
- No `.tmp` remains under success OR failure (rename error cleans up).
- `processed.json` gains an entry only after a successful write; corrupt log is never overwritten.
- Traversal attempt (`..`) is rejected before any write.

## Risk Assessment
| Risk | L×I | Mitigation |
|---|---|---|
| Partial file if process dies mid-write | Low×High | Write `.tmp` then atomic `os.Rename`; readers only ever see the complete file. |
| Corrupt log clobbered → all papers re-surface | Low×High | `readLog()` returns `ErrLogCorrupted`; `MarkAsProcessed` aborts without writing. |
| `Published` unparseable → crash/bad filename | Med×Med | `dateOnly` triple fallback → `"unknown"`, never panics. |
| Path traversal via crafted title/id | Low×High | Slug strips separators; `validateWithinBase` guards final path. |
| Obsidian sync grabs `.tmp` | Low×Low | Rename window is sub-ms; sync conflict files don't overwrite the original (R4). |

## Backwards compatibility
`MarkAsProcessed` signature is unchanged from the Phase 2 stub — only the body changes.
`FilterUnprocessed` and the log schema are untouched (`processedEntry` already matches).

## Rollback
Delete `vaultwriter.go`; restore the `MarkAsProcessed` stub. `processed.json` format is stable, so
any entries written remain valid.

## Security (PRD §7)
Path validated within the configured vault base; filename sanitized to `[a-z0-9._-]`; YAML values
escaped to prevent metadata injection; temp file removed on failure. No network.

## Next Steps
Feeds Phase 04 (`runPipeline` calls `WriteToVault`). Parallel with Phase 02. File ownership: owns
`tools/vaultwriter.go` + the `MarkAsProcessed` body in `tools/logcheck.go`.
