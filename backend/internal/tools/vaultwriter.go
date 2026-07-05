package tools

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// VaultWriterTool has one job: reliably write the final explainer note to the
// Obsidian vault. "Reliably" means atomically — the vault must never contain a
// partial or corrupt file, and processed.json is updated ONLY after a successful
// write. It holds the concrete *LogCheckTool (not the Unprocessor interface)
// because it calls MarkAsProcessed, which is not part of that read-only contract.
type VaultWriterTool struct {
	cfg      *config.Config
	logCheck *LogCheckTool
}

func NewVaultWriterTool(cfg *config.Config, logCheck *LogCheckTool) *VaultWriterTool {
	return &VaultWriterTool{cfg: cfg, logCheck: logCheck}
}

// WriteToVault assembles frontmatter + content, writes it atomically to
// {vault}/AI Papers/{filename}, then records the paper as processed. It returns
// the final absolute path. verdict is the Phase 5 review outcome: nil means the
// reviewer was disabled (frontmatter records review_iterations: 0, review_passed:
// true); a set verdict drives the real review_* fields.
//
// Atomicity: write to a sibling ".tmp" then os.Rename (atomic on the same
// filesystem). A rename failure removes the temp file so no orphan ".tmp" is
// left. A post-write log-update failure is a WARNING, not an error: the note is
// already saved; the only consequence is the paper re-surfaces next run.
func (t *VaultWriterTool) WriteToVault(ctx context.Context, ex models.ExplainerOutput, p models.Paper, verdict *models.ReviewVerdict) (string, error) {
	start := time.Now()
	vaultDir := filepath.Join(t.cfg.Paths.ObsidianVault, "AI Papers")
	filename := t.generateFilename(p)
	finalPath := filepath.Join(vaultDir, filename)

	// Defense-in-depth: even though slugify strips separators, verify the final
	// path stays within the configured vault base before touching the disk.
	if err := validateWithinBase(t.cfg.Paths.ObsidianVault, finalPath); err != nil {
		return "", err
	}
	if err := os.MkdirAll(vaultDir, 0o755); err != nil {
		return "", fmt.Errorf("create vault dir: %w", err)
	}

	slog.Info("vault write started", "paper_id", p.ID, "filename", filename)

	content := t.buildFrontmatter(p, ex, verdict) + ex.Content
	// Atomic + unique-temp write: no partial file ever appears at finalPath, and
	// concurrent writers to the same note never collide on one temp (see
	// writeFileAtomic). A failure leaves no orphan temp behind.
	if err := writeFileAtomic(finalPath, []byte(content), 0o644); err != nil {
		return "", err
	}

	slog.Info("vault write complete",
		"paper_id", p.ID, "filename", filename, "path", finalPath,
		"duration_ms", time.Since(start).Milliseconds(),
	)

	// Warning-not-fatal: the note exists regardless of the log outcome.
	if err := t.logCheck.MarkAsProcessed(p, filename); err != nil {
		slog.Warn("vault write succeeded but log update failed",
			"paper_id", p.ID, "vault_file", filename, "error", err.Error())
	} else {
		slog.Info("log updated", "paper_id", p.ID, "vault_file", filename)
	}
	return finalPath, nil
}

// validateWithinBase rejects a target path that escapes the configured vault
// base (e.g. via a "../" injected through a crafted title or arXiv ID). It
// compares cleaned paths with a separator-aware prefix so "/vault2" is not
// mistaken for a child of "/vault".
func validateWithinBase(base, target string) error {
	cleanBase := filepath.Clean(base)
	cleanTarget := filepath.Clean(target)
	if cleanTarget == cleanBase {
		return fmt.Errorf("refusing to write vault base itself: %q", cleanTarget)
	}
	prefix := cleanBase + string(os.PathSeparator)
	if !strings.HasPrefix(cleanTarget, prefix) {
		return fmt.Errorf("path %q escapes vault base %q", cleanTarget, cleanBase)
	}
	return nil
}
