package agents

import (
	"strings"
	"testing"

	"github.com/maritime-ds/arxiv-reader/internal/models"
)

func TestFormatRevisionNoteStableOrdering(t *testing.T) {
	// Feedback in deliberately non-sorted insertion order. Because Go map iteration
	// is randomized, the only way output can be stable is the fixed sectionOrder
	// walk — assert byte-identical output across many runs.
	v := models.ReviewVerdict{
		Iteration: 1,
		Score:     0.62,
		Feedback: map[string]string{
			"glossary":          "add contrastive loss",
			"core_idea":         "bridge the analogy to a database index lookup",
			"follow_up_papers":  "the BERT link is malformed",
			"problem_statement": "state the motivation explicitly",
		},
	}
	first := FormatRevisionNote(v)
	for i := 0; i < 100; i++ {
		if got := FormatRevisionNote(v); got != first {
			t.Fatalf("non-deterministic output on run %d:\n--- first ---\n%s\n--- got ---\n%s", i, first, got)
		}
	}

	// Sections must appear in sectionOrder, not insertion/alpha order.
	psIdx := strings.Index(first, "Problem Statement")
	ciIdx := strings.Index(first, "Core Idea")
	glIdx := strings.Index(first, "Glossary")
	fuIdx := strings.Index(first, "Follow-Up Papers")
	if !(psIdx < ciIdx && ciIdx < glIdx && glIdx < fuIdx) {
		t.Fatalf("sections not in fixed order: ps=%d ci=%d gl=%d fu=%d", psIdx, ciIdx, glIdx, fuIdx)
	}
	// Header carries the pass number and score.
	if !strings.Contains(first, "REVISION REQUIRED (Review pass 1, score: 0.62)") {
		t.Fatalf("header wrong: %q", first)
	}
}

func TestFormatRevisionNoteEmptyFeedbackWellFormed(t *testing.T) {
	v := models.ReviewVerdict{Iteration: 2, Score: 0.5, Feedback: map[string]string{}}
	note := FormatRevisionNote(v)
	if !strings.Contains(note, "REVISION REQUIRED (Review pass 2, score: 0.50)") {
		t.Fatalf("header missing: %q", note)
	}
	if !strings.Contains(note, "keep the existing content unchanged") {
		t.Fatalf("trailer missing: %q", note)
	}
	// No section headings when there is no feedback.
	if strings.Contains(note, "### ") {
		t.Fatalf("empty feedback should emit no section headings: %q", note)
	}
}

func TestSectionDisplayName(t *testing.T) {
	cases := map[string]string{
		"problem_statement": "Problem Statement",
		"analogies":         "Analogies & Intuition",
		"follow_up_papers":  "Follow-Up Papers",
		"unknown_slug":      "unknown_slug", // fallback to raw key
	}
	for in, want := range cases {
		if got := sectionDisplayName(in); got != want {
			t.Fatalf("sectionDisplayName(%q) = %q, want %q", in, got, want)
		}
	}
}
