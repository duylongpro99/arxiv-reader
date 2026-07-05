package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// ErrLogCorrupted signals that the processed-log file exists but could not be
// parsed. This is deliberately NOT treated as an empty log: doing so would
// re-surface every already-processed paper, violating the Trust guarantee.
var ErrLogCorrupted = errors.New("processed log file is corrupted")

// LogCheckTool is the system's memory. A single instance is shared across all
// pipeline sessions, so MarkAsProcessed's read-modify-write of processed.json is
// serialized by mu — without it, two sessions processing different papers
// concurrently could each read the log, append, and rename, losing one entry.
type LogCheckTool struct {
	cfg *config.PathsConfig
	mu  sync.Mutex // guards the MarkAsProcessed read-modify-write
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
// write. It never clobbers a corrupt log: readLog distinguishes missing (=> start
// a fresh log, first-run) from corrupt (=> ErrLogCorrupted, abort without
// writing, so the existing file is preserved for manual inspection). The write is
// atomic (temp → rename) so a crash mid-write can never truncate the log.
func (t *LogCheckTool) MarkAsProcessed(paper models.Paper, vaultFile string) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	log, err := t.readLog()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			log = &processedLog{} // first entry — no log yet
		} else {
			return err // ErrLogCorrupted: do NOT overwrite
		}
	}

	log.Processed = append(log.Processed, processedEntry{
		PaperID:     paper.ID,
		Title:       paper.Title,
		ProcessedAt: time.Now().UTC().Format(time.RFC3339),
		VaultFile:   vaultFile,
	})
	return t.writeLogAtomic(log)
}

// writeLogAtomic serializes the log and writes it via the shared atomic helper
// (unique temp → rename), so readers only ever see a complete file and no orphan
// temp is left on failure. Callers hold t.mu.
func (t *LogCheckTool) writeLogAtomic(log *processedLog) error {
	path := filepath.Clean(t.cfg.LogFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	raw, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal log: %w", err)
	}
	return writeFileAtomic(path, raw, 0o644)
}
