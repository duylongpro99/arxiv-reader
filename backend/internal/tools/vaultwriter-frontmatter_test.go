package tools

import (
	"testing"

	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// fmPaper is a minimal paper for frontmatter assertions.
func fmPaper() models.Paper {
	return models.Paper{ID: "2401.12345", Title: "A Paper", Authors: []string{"X"}, Published: "2024-01-15"}
}

func TestBuildFrontmatterNilVerdict(t *testing.T) {
	w, _ := newVaultWriter(t)
	fm := parseFrontmatter(t, w.buildFrontmatter(fmPaper(), sampleExplainer(), nil)+"body")

	if fm["review_iterations"] != 0 {
		t.Fatalf("nil verdict → review_iterations should be 0, got %v", fm["review_iterations"])
	}
	if fm["review_passed"] != true {
		t.Fatalf("nil verdict → review_passed should be true, got %v", fm["review_passed"])
	}
	if _, ok := fm["review_score"]; ok {
		t.Fatalf("nil verdict → review_score must be omitted, got %v", fm["review_score"])
	}
}

func TestBuildFrontmatterPassedVerdict(t *testing.T) {
	w, _ := newVaultWriter(t)
	v := &models.ReviewVerdict{PaperID: "2401.12345", Pass: true, Score: 0.87, Iteration: 2}
	fm := parseFrontmatter(t, w.buildFrontmatter(fmPaper(), sampleExplainer(), v)+"body")

	if fm["review_iterations"] != 2 {
		t.Fatalf("review_iterations = %v, want 2", fm["review_iterations"])
	}
	if fm["review_passed"] != true {
		t.Fatalf("review_passed = %v, want true", fm["review_passed"])
	}
	// YAML unmarshals 0.87 as float64.
	if score, ok := fm["review_score"].(float64); !ok || score != 0.87 {
		t.Fatalf("review_score = %v, want 0.87", fm["review_score"])
	}
}

func TestBuildFrontmatterFailedVerdict(t *testing.T) {
	w, _ := newVaultWriter(t)
	v := &models.ReviewVerdict{PaperID: "2401.12345", Pass: false, Score: 0.74, Iteration: 2}
	fm := parseFrontmatter(t, w.buildFrontmatter(fmPaper(), sampleExplainer(), v)+"body")

	if fm["review_passed"] != false {
		t.Fatalf("failed verdict → review_passed should be false, got %v", fm["review_passed"])
	}
	if score, ok := fm["review_score"].(float64); !ok || score != 0.74 {
		t.Fatalf("failed verdict → review_score = %v, want 0.74", fm["review_score"])
	}
	if fm["review_iterations"] != 2 {
		t.Fatalf("review_iterations = %v, want 2", fm["review_iterations"])
	}
}
