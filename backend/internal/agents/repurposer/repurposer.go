// Package repurposer holds the Repurposer agent: the generation half of the
// channel-publishing feature. It mirrors internal/agents's ExplainerAgent
// (struct + prompt file + Generate()) but is parametrized by content
// Category instead of a fixed output shape, and is deliberately single-shot —
// the human reviewing/editing the draft is the "reviewer", not another LLM
// pass (see docs/design-notes/2026-07-14-channel-publishing.md).
//
// This package imports internal/channels ONLY for the Category type and the
// GeneratedContent/PublishResult DTOs it produces. No symbol here may
// reference a concrete channel (devto/x/...) — that coupling would defeat the
// entire point of splitting generation from delivery.
package repurposer

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/channels"
	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/maritime-ds/arxiv-reader/internal/llm"
	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// defaultTargetWords is the design-note fallback used when
// cfg.Publishing.Categories has no entry for a category (publishing config is
// optional — an empty `publishing:` block is valid, see config.go). Keeping a
// sane default here means Generate never sends "approximately 0 words" to the
// model just because the operator left the category unconfigured.
var defaultTargetWords = map[channels.Category]int{
	channels.Longform: 1200,
	channels.Digest:   500,
	channels.Brief:    120,
}

// Repurposer generates platform-agnostic GeneratedContent from a run's
// explainer Markdown, for exactly one Category per call. It holds the shared
// llm.LLMClient and *config.Config the same way ExplainerAgent does.
type Repurposer struct {
	llm llm.LLMClient
	cfg *config.Config
}

// New builds a Repurposer over the shared, concurrency-safe LLM client.
func New(client llm.LLMClient, cfg *config.Config) *Repurposer {
	return &Repurposer{llm: client, cfg: cfg}
}

// RepurposeInput is the agent's request. Raw is the source explainer's full
// Markdown (its Analogies & Intuition + Glossary sections are what the
// category prompts lean on for accessibility). Category selects which prompt
// template runs and the target length; PaperMeta rides through unchanged onto
// the resulting GeneratedContent.
type RepurposeInput struct {
	Raw       string
	Category  channels.Category
	PaperMeta models.Paper
}

// Generate sends the source explainer text to the LLM using the prompt
// template selected by in.Category, single-shot (no reviewer loop — the human
// approving the draft is the review step). The source text rides in
// DocumentText, mirroring ExplainerAgent's text-only contract.
func (a *Repurposer) Generate(ctx context.Context, in RepurposeInput) (channels.GeneratedContent, error) {
	start := time.Now()

	// Defense-in-depth: an invalid category should never reach the LLM call —
	// Category values can originate from an HTTP request body upstream, so this
	// boundary check is the last line of defense before a prompt lookup.
	if !in.Category.Valid() {
		return channels.GeneratedContent{}, fmt.Errorf("repurposer: unknown category %q", in.Category)
	}

	targetWords := a.targetWords(in.Category)
	req := llm.CompletionRequest{
		SystemPrompt: systemPromptFor(in.Category),
		UserPrompt:   buildUserPrompt(in, targetWords),
		DocumentText: in.Raw,
		MaxTokens:    a.cfg.LLM.MaxTokens,
		Temperature:  a.cfg.LLM.Temperature,
	}

	resp, err := a.llm.Complete(ctx, req)
	if err != nil {
		return channels.GeneratedContent{}, err
	}

	out := channels.GeneratedContent{
		Category:  in.Category,
		Title:     parseTitle(resp.Content, in.PaperMeta.Title),
		Body:      resp.Content,
		PaperMeta: in.PaperMeta,
		// Tags are left empty here by design: each Channel sanitizes/derives
		// its own tags from platform rules (e.g. dev.to's 4-tag limit), so
		// deriving them in the category-blind agent would just be discarded
		// or re-validated downstream anyway (YAGNI).
		Tags: nil,
	}

	slog.Info("repurposer generation complete",
		"paper_id", in.PaperMeta.ID,
		"category", string(in.Category),
		"input_tokens", resp.InputTokens,
		"output_tokens", resp.OutputTokens,
		"tokens_used", resp.InputTokens+resp.OutputTokens,
		"word_count", wordCount(resp.Content),
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return out, nil
}

// targetWords resolves the soft target length for a category from
// cfg.Publishing.Categories, falling back to defaultTargetWords when the
// operator hasn't configured that category (publishing config is optional).
func (a *Repurposer) targetWords(category channels.Category) int {
	if cc, ok := a.cfg.Publishing.Categories[string(category)]; ok && cc.TargetWords > 0 {
		return cc.TargetWords
	}
	return defaultTargetWords[category]
}

// parseTitle extracts the first `# ` (H1) heading from the generated content
// and falls back to the paper's own title when no heading is found — the
// model is instructed to emit one, but generation output is never trusted
// blindly.
func parseTitle(content, fallback string) string {
	for _, line := range strings.Split(content, "\n") {
		if h, ok := strings.CutPrefix(strings.TrimSpace(line), "# "); ok {
			if t := strings.TrimSpace(h); t != "" {
				return t
			}
		}
	}
	return fallback
}

// wordCount is a whitespace-split count for the target-length observability
// log line, mirroring agents.wordCount.
func wordCount(s string) int {
	return len(strings.Fields(s))
}
