package tools

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// samplePapers returns three papers in recency order for filter tests.
func samplePapers() []models.Paper {
	return []models.Paper{
		{ID: "2401.00001", Title: "First"},
		{ID: "2401.00002", Title: "Second"},
		{ID: "2401.00003", Title: "Third"},
	}
}

// newLogCheck writes content to a temp processed.json (unless content is "")
// and returns a tool pointed at it. content=="" leaves the file absent.
func newLogCheck(t *testing.T, content string) *LogCheckTool {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "processed.json")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}
	return NewLogCheckTool(&config.PathsConfig{LogFile: path})
}

func TestFilterUnprocessedFirstRun(t *testing.T) {
	// No file at all -> every paper is unprocessed, no error.
	got, err := newLogCheck(t, "").FilterUnprocessed(samplePapers())
	if err != nil {
		t.Fatalf("first run should not error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 papers, got %d", len(got))
	}
}

func TestFilterUnprocessedEmptyLog(t *testing.T) {
	got, err := newLogCheck(t, `{"processed":[]}`).FilterUnprocessed(samplePapers())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 papers, got %d", len(got))
	}
}

func TestFilterUnprocessedPartial(t *testing.T) {
	// Middle paper already processed -> excluded; order preserved.
	log := `{"processed":[{"paper_id":"2401.00002","title":"Second"}]}`
	got, err := newLogCheck(t, log).FilterUnprocessed(samplePapers())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 papers, got %d", len(got))
	}
	if got[0].ID != "2401.00001" || got[1].ID != "2401.00003" {
		t.Fatalf("order/filter wrong: %#v", got)
	}
}

func TestFilterUnprocessedAllProcessed(t *testing.T) {
	log := `{"processed":[
		{"paper_id":"2401.00001"},
		{"paper_id":"2401.00002"},
		{"paper_id":"2401.00003"}
	]}`
	got, err := newLogCheck(t, log).FilterUnprocessed(samplePapers())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 papers, got %d", len(got))
	}
}

func TestFilterUnprocessedCorrupt(t *testing.T) {
	_, err := newLogCheck(t, `{ this is not valid json `).FilterUnprocessed(samplePapers())
	if !errors.Is(err, ErrLogCorrupted) {
		t.Fatalf("expected ErrLogCorrupted, got %v", err)
	}
}

func TestMarkAsProcessedCreatesLog(t *testing.T) {
	lc := newLogCheck(t, "") // no file yet
	if err := lc.MarkAsProcessed(models.Paper{ID: "2401.00001", Title: "First"}, "2024-01-15_2401.00001_first.md"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	// Round-trip: the paper is now filtered out.
	got, err := lc.FilterUnprocessed(samplePapers())
	if err != nil {
		t.Fatalf("filter err: %v", err)
	}
	if len(got) != 2 || got[0].ID != "2401.00002" {
		t.Fatalf("processed paper not filtered: %#v", got)
	}
}

func TestMarkAsProcessedAppendsToExisting(t *testing.T) {
	lc := newLogCheck(t, `{"processed":[{"paper_id":"2401.00001","title":"First"}]}`)
	if err := lc.MarkAsProcessed(models.Paper{ID: "2401.00003", Title: "Third"}, "f.md"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	got, err := lc.FilterUnprocessed(samplePapers())
	if err != nil {
		t.Fatalf("filter err: %v", err)
	}
	// Both #1 and #3 processed now → only #2 remains.
	if len(got) != 1 || got[0].ID != "2401.00002" {
		t.Fatalf("append wrong: %#v", got)
	}
}

// Concurrent MarkAsProcessed calls on the shared tool must not lose entries
// (mutex-serialized read-modify-write). Runs under -race.
func TestMarkAsProcessedConcurrent(t *testing.T) {
	lc := newLogCheck(t, "")
	const n = 20
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "2401." + string(rune('a'+i))
			if err := lc.MarkAsProcessed(models.Paper{ID: id}, "f.md"); err != nil {
				t.Errorf("mark %s: %v", id, err)
			}
		}(i)
	}
	wg.Wait()

	// All n entries must survive (no lost update).
	log, err := lc.readLog()
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if len(log.Processed) != n {
		t.Fatalf("lost entries: got %d, want %d", len(log.Processed), n)
	}
}

// A corrupt log must NOT be overwritten by MarkAsProcessed — it aborts with the
// error and leaves the file byte-for-byte untouched.
func TestMarkAsProcessedCorruptLogUntouched(t *testing.T) {
	corrupt := `{ not valid json`
	lc := newLogCheck(t, corrupt)
	err := lc.MarkAsProcessed(models.Paper{ID: "x"}, "f.md")
	if !errors.Is(err, ErrLogCorrupted) {
		t.Fatalf("expected ErrLogCorrupted, got %v", err)
	}
	// File contents unchanged.
	raw, _ := os.ReadFile(filepath.Clean(lc.cfg.LogFile))
	if string(raw) != corrupt {
		t.Fatalf("corrupt log was modified: %q", string(raw))
	}
}
