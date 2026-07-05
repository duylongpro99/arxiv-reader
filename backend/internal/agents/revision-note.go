package agents

import (
	"fmt"
	"strings"

	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// sectionOrder is the fixed emission order for revision-note sections. Go map
// iteration is randomized, so ranging v.Feedback directly would make the note
// (and its tests) non-deterministic — we iterate this slice instead and emit only
// the keys present in the feedback map. Order mirrors the explainer's OUTPUT
// FORMAT section order.
var sectionOrder = []string{
	"problem_statement",
	"core_idea",
	"methodology",
	"key_findings",
	"limitations",
	"why_it_matters",
	"analogies",
	"glossary",
	"follow_up_papers",
}

// sectionDisplayNames maps a section slug to its title-cased display name for the
// revision note headings. A key not present here falls back to the raw slug.
var sectionDisplayNames = map[string]string{
	"problem_statement": "Problem Statement",
	"core_idea":         "Core Idea",
	"methodology":       "Methodology",
	"key_findings":      "Key Findings",
	"limitations":       "Limitations",
	"why_it_matters":    "Why It Matters",
	"analogies":         "Analogies & Intuition",
	"glossary":          "Glossary",
	"follow_up_papers":  "Follow-Up Papers",
}

// FormatRevisionNote renders a reviewer verdict's feedback into the natural-language
// revision instruction the existing ExplainerAgent.buildUserPrompt revision branch
// consumes. Exported because the orchestrator (a separate package) builds the note
// between review and the next generation. Sections are emitted in the fixed
// sectionOrder (deterministic output), including only sections that carry feedback.
func FormatRevisionNote(v models.ReviewVerdict) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("REVISION REQUIRED (Review pass %d, score: %.2f)\n\n", v.Iteration, v.Score))
	sb.WriteString("Please revise the following sections based on this feedback:\n\n")

	// Deterministic: walk the fixed order, emit only sections present in Feedback.
	for _, key := range sectionOrder {
		if note, ok := v.Feedback[key]; ok {
			sb.WriteString(fmt.Sprintf("### %s\n%s\n\n", sectionDisplayName(key), note))
		}
	}

	sb.WriteString("---\n\nFor sections without feedback above, keep the existing content unchanged.")
	return sb.String()
}

// sectionDisplayName returns the title-cased name for a section slug, falling back
// to the raw key for any slug not in the display map.
func sectionDisplayName(key string) string {
	if name, ok := sectionDisplayNames[key]; ok {
		return name
	}
	return key
}
