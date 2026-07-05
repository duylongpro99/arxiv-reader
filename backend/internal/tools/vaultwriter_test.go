package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/maritime-ds/arxiv-reader/internal/models"
	"gopkg.in/yaml.v3"
)

// newVaultWriter returns a writer whose vault + log live under a throwaway
// TempDir, so tests never touch a real Obsidian vault.
func newVaultWriter(t *testing.T) (*VaultWriterTool, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := &config.Config{
		Paths: config.PathsConfig{ObsidianVault: dir, LogFile: filepath.Join(dir, "processed.json")},
		Agent: config.AgentConfig{ArxivCategory: "cs.AI"},
	}
	return NewVaultWriterTool(cfg, NewLogCheckTool(&cfg.Paths)), dir
}

func sampleExplainer() models.ExplainerOutput {
	return models.ExplainerOutput{
		PaperID:   "2401.12345",
		Content:   "# Title\n\n## Problem Statement\nbody\n",
		Iteration: 1,
		CreatedAt: time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
	}
}

func TestWriteToVaultHappyPath(t *testing.T) {
	w, dir := newVaultWriter(t)
	paper := models.Paper{
		ID: "2401.12345v2", Title: "Attention: Is All You Need?",
		Authors: []string{"A. Vaswani", `N. "Noam" Shazeer`}, Published: "2017-06-12T00:00:00Z",
	}

	path, err := w.WriteToVault(context.Background(), sampleExplainer(), paper)
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	// File exists at {vault}/AI Papers/YYYY-MM-DD_id_slug.md, no .tmp left.
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("final file missing: %v", err)
	}
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "2017-06-12_2401.12345v2_") || !strings.HasSuffix(base, ".md") {
		t.Fatalf("filename format wrong: %q", base)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf(".tmp should not remain, stat err = %v", err)
	}

	// Frontmatter parses as valid YAML with all required keys.
	raw, _ := os.ReadFile(path)
	fm := parseFrontmatter(t, string(raw))
	for _, k := range []string{"arxiv_id", "title", "authors", "published", "category", "generated_at", "review_iterations", "review_passed", "tags"} {
		if _, ok := fm[k]; !ok {
			t.Fatalf("frontmatter missing key %q; got %v", k, fm)
		}
	}
	if fm["category"] != "cs.AI" {
		t.Fatalf("category should come from config: %v", fm["category"])
	}
	if fm["review_iterations"] != 1 || fm["review_passed"] != true {
		t.Fatalf("forward-compat review fields wrong: %v / %v", fm["review_iterations"], fm["review_passed"])
	}
	if fm["published"] != "2024-01-15" && fm["published"] != "2017-06-12" {
		// published is the RFC3339 date part -> 2017-06-12
		if fm["published"] != "2017-06-12" {
			t.Fatalf("published date part wrong: %v", fm["published"])
		}
	}

	// processed.json gained the entry after the write.
	got, err := NewLogCheckTool(&config.PathsConfig{LogFile: filepath.Join(dir, "processed.json")}).
		FilterUnprocessed([]models.Paper{{ID: "2401.12345v2"}})
	if err != nil {
		t.Fatalf("filter err: %v", err)
	}
	if len(got) != 0 {
		t.Fatal("paper should be marked processed after write")
	}
}

func TestWriteToVaultRenameFailureCleansTmp(t *testing.T) {
	w, dir := newVaultWriter(t)
	paper := models.Paper{ID: "2401.99999", Title: "Rename Clash", Published: "2024-02-02"}

	// Make the final path a *directory* so os.Rename(tmp, final) fails.
	filename := w.generateFilename(paper)
	vaultDir := filepath.Join(dir, "AI Papers")
	if err := os.MkdirAll(filepath.Join(vaultDir, filename), 0o755); err != nil {
		t.Fatalf("setup dir: %v", err)
	}

	_, err := w.WriteToVault(context.Background(), sampleExplainer(), paper)
	if err == nil {
		t.Fatal("expected rename failure")
	}
	// No orphan temp of any name left behind (unique temp names are removed on
	// failure by writeFileAtomic).
	leftovers, _ := filepath.Glob(filepath.Join(vaultDir, "*.tmp"))
	if len(leftovers) != 0 {
		t.Fatalf("orphan temp files left: %v", leftovers)
	}
}

func TestValidateWithinBaseRejectsTraversal(t *testing.T) {
	base := "/home/u/vault"
	if err := validateWithinBase(base, "/home/u/vault/AI Papers/note.md"); err != nil {
		t.Fatalf("legitimate child rejected: %v", err)
	}
	if err := validateWithinBase(base, "/home/u/vault2/note.md"); err == nil {
		t.Fatal("sibling /vault2 must be rejected (prefix false-match)")
	}
	if err := validateWithinBase(base, "/home/u/vault/../etc/passwd"); err == nil {
		t.Fatal("traversal must be rejected")
	}
}

func TestSlugifyEdgeCases(t *testing.T) {
	cases := map[string]string{
		"Attention Is All You Need":          "attention-is-all-you-need",
		"  Mixed   CASE & Symbols!! ":         "mixed-case-symbols",
		"":                                    "untitled",
		"—— ///":                              "untitled",
		strings.Repeat("word ", 30):           "", // long — only assert length below
	}
	for in, want := range cases {
		got := slugify(in)
		if want != "" && got != want {
			t.Fatalf("slugify(%q) = %q, want %q", in, got, want)
		}
		if len(got) > 60 {
			t.Fatalf("slug over 60 chars: %q (%d)", got, len(got))
		}
	}
}

func TestDateOnlyFallbacks(t *testing.T) {
	cases := map[string]string{
		"2017-06-12T00:00:00Z": "2017-06-12",
		"2024-01-15":           "2024-01-15",
		"garbage":              "unknown",
		"":                     "unknown",
		"2024-13-99":           "2024-13-99", // date-shaped first 10 chars pass through
	}
	for in, want := range cases {
		if got := dateOnly(in); got != want {
			t.Fatalf("dateOnly(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSanitizeArxivID(t *testing.T) {
	cases := map[string]string{
		"2401.12345v2": "2401.12345v2",
		"cs.AI/0123456": "cs.ai0123456",
		// Path separators are stripped (dots are filename-safe and kept), so no
		// "/" survives → the result can never traverse.
		"../../etc": "....etc",
	}
	for in, want := range cases {
		if got := sanitizeArxivID(in); got != want {
			t.Fatalf("sanitizeArxivID(%q) = %q, want %q", in, got, want)
		}
	}
}

// parseFrontmatter extracts the leading ---...--- block and unmarshals it as YAML.
func parseFrontmatter(t *testing.T, content string) map[string]any {
	t.Helper()
	if !strings.HasPrefix(content, "---\n") {
		t.Fatalf("content does not start with frontmatter: %.40q", content)
	}
	end := strings.Index(content[4:], "\n---\n")
	if end < 0 {
		t.Fatal("no closing frontmatter delimiter")
	}
	var fm map[string]any
	if err := yaml.Unmarshal([]byte(content[4:4+end]), &fm); err != nil {
		t.Fatalf("frontmatter is not valid YAML: %v", err)
	}
	return fm
}
