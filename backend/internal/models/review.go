package models

import "time"

// ReviewVerdict is the structured judgement the ReviewerAgent (Phase 5) returns
// for one explainer iteration. Pass is the single source of truth for whether the
// explainer is accepted (trusted verbatim from the model); Score is advisory only
// (surfaced in the UI + frontmatter, never gates the loop). Feedback maps a
// section slug (see agents.sectionKeys) to an actionable revision note, used to
// build the revision prompt for the next generation.
type ReviewVerdict struct {
	PaperID    string
	Pass       bool
	Score      float32
	Feedback   map[string]string // section key → actionable revision note
	Iteration  int
	TokensUsed int // total (InputTokens + OutputTokens); kept for back-compat
	// Split token counts, mirroring ExplainerOutput, so the orchestrator can feed
	// AddIO for cost estimation. TokensUsed remains the authoritative total.
	InputTokens  int
	OutputTokens int
	CreatedAt    time.Time
}
