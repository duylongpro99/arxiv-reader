// Package models holds the shared data contracts for the discovery pipeline.
// Phase 1 deliberately shipped no models (YAGNI); they are defined per-phase as
// they are first needed. Phase 2 introduces Paper and the pipeline session.
package models

// Paper is a single arXiv candidate surfaced to the user.
//
// JSON tags are camelCase on purpose: this struct is serialized straight to the
// Next.js frontend, whose `Paper` type (see frontend/lib/types.ts) is camelCase.
// Without the explicit `pdfUrl` tag Go would emit `PDFURL`, breaking the contract.
type Paper struct {
	ID        string   `json:"id"`
	Title     string   `json:"title"`
	Authors   []string `json:"authors"`
	Abstract  string   `json:"abstract"`
	PDFURL    string   `json:"pdfUrl"`
	Published string   `json:"published"` // ISO-8601 date string as returned by arXiv
	// Source is the resource id this paper came from (e.g. "arxiv"). Set by the
	// resource engine's normalizer; NOT persisted (the store maps only
	// paper_id/title) so it needs no DB migration. omitempty keeps pre-engine
	// JSON output byte-identical when unset.
	Source string `json:"source,omitempty"`
}
