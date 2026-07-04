package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// ErrLogCorrupted signals that the processed-log file exists but could not be
// parsed. This is deliberately NOT treated as an empty log: doing so would
// re-surface every already-processed paper, violating the Trust guarantee.
var ErrLogCorrupted = errors.New("processed log file is corrupted")

// LogCheckTool is the system's memory. In Phase 2 only the read path
// (FilterUnprocessed) is used; MarkAsProcessed is wired up in Phase 4.
type LogCheckTool struct {
	cfg *config.PathsConfig
}

func NewLogCheckTool(cfg *config.PathsConfig) *LogCheckTool {
	return &LogCheckTool{cfg: cfg}
}

// processedEntry mirrors one record in processed.json.
type processedEntry struct {
	PaperID     string `json:"paper_id"`
	Title       string `json:"title"`
	ProcessedAt string `json:"processed_at"`
	VaultFile   string `json:"vault_file"`
}

type processedLog struct {
	Processed []processedEntry `json:"processed"`
}

// FilterUnprocessed returns the papers not present in the processed log,
// preserving input order (which is arXiv recency order). A missing log file is
// the first-run case: every paper is unprocessed. A corrupt file is a hard
// error (ErrLogCorrupted).
func (t *LogCheckTool) FilterUnprocessed(papers []models.Paper) ([]models.Paper, error) {
	log, err := t.readLog()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return papers, nil // first run — no log yet
		}
		return nil, err // ErrLogCorrupted
	}

	// Build the set of already-processed IDs for O(1) lookup.
	processed := make(map[string]struct{}, len(log.Processed))
	for _, e := range log.Processed {
		processed[e.PaperID] = struct{}{}
	}

	unprocessed := make([]models.Paper, 0, len(papers))
	for _, p := range papers {
		if _, seen := processed[p.ID]; !seen {
			unprocessed = append(unprocessed, p)
		}
	}
	return unprocessed, nil
}

// readLog reads and parses processed.json. It distinguishes a missing file
// (returned as os.ErrNotExist for the caller to treat as first-run) from a
// present-but-unparseable file (ErrLogCorrupted).
func (t *LogCheckTool) readLog() (*processedLog, error) {
	path := filepath.Clean(t.cfg.LogFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("%w: %v", ErrLogCorrupted, err)
	}
	var log processedLog
	if err := json.Unmarshal(raw, &log); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLogCorrupted, err)
	}
	return &log, nil
}

// MarkAsProcessed appends a paper to the processed log after a successful vault
// write. Deferred to Phase 4 (Phase 2 is read-only per the PRD scope); the
// signature is fixed now so the orchestrator contract is stable.
func (t *LogCheckTool) MarkAsProcessed(paper models.Paper, vaultFile string) error {
	return errors.New("MarkAsProcessed not implemented until Phase 4")
}
