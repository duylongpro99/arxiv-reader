package models

import "time"

// ExplainerOutput is the re-teaching explainer note produced by the
// ExplainerAgent (Phase 4) from a paper's extracted Markdown.
//
// It is a per-phase model (see paper.go: models are defined as they are first
// needed). Content holds the full Markdown body — the title plus the 9 required
// sections — exactly as returned by the LLM; frontmatter is NOT part of Content
// (VaultWriter prepends it at write time). Sections is a best-effort parse keyed
// by section slug: a missing heading is tolerated (the full Content is still
// saved), so callers must not assume every slug is present.
type ExplainerOutput struct {
	PaperID      string
	Content      string            // full Markdown: "# Title\n## Problem Statement\n…"
	Sections     map[string]string // keyed by section slug; best-effort (Phase 02 parses)
	Iteration    int               // 1 in Phase 4 (revision loop is Phase 5)
	InputTokens  int
	OutputTokens int
	CreatedAt    time.Time
}
