package tools

import (
	"errors"
	"os"
	"path/filepath"
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
