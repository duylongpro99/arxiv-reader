// Package agents holds the reasoning agents that turn pipeline inputs into
// product artifacts. It is the first agent package (mirroring internal/tools):
// each agent wraps the shared llm.LLMClient with a task-specific prompt and
// output contract. ExplainerAgent (Phase 4) is text-only — it consumes the
// paper's extracted Markdown, never images.
package agents

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/maritime-ds/arxiv-reader/internal/llm"
	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// sectionKeys maps each of the 9 exact `## ` headings to its stable slug. Kept in
// exact sync with the OUTPUT FORMAT block in systemPrompt — a heading typo in
// either place silently drops that section from the parsed map (still tolerated:
// full Content is always saved).
var sectionKeys = map[string]string{
	"Problem Statement":     "problem_statement",
	"Core Idea":             "core_idea",
	"Methodology":           "methodology",
	"Key Findings":          "key_findings",
	"Limitations":           "limitations",
	"Why It Matters":        "why_it_matters",
	"Analogies & Intuition": "analogies",
	"Glossary":              "glossary",
	"Follow-Up Papers":      "follow_up_papers",
}

// ExplainerAgent generates a re-teaching explainer note from a paper's Markdown.
// It holds *config.Config so Phase 04 can construct it with the shared config;
// only cfg.LLM (MaxTokens/Temperature) is read here.
type ExplainerAgent struct {
	llm llm.LLMClient
	cfg *config.Config
}

// New builds an ExplainerAgent over the shared, concurrency-safe LLM client.
func New(client llm.LLMClient, cfg *config.Config) *ExplainerAgent {
	return &ExplainerAgent{llm: client, cfg: cfg}
}

// ExplainerInput is the agent's request. MarkdownText comes from session.Markdown()
// (Phase 3). RevisionNote is always "" in Phase 4 (Phase 5 revision seam).
type ExplainerInput struct {
	MarkdownText string
	PaperMeta    models.Paper
	RevisionNote string
}

// Generate sends the paper Markdown to the text-only LLM client and parses the
// response into the 9 sections. The paper text rides in DocumentText (the client
// sends it as a text block prefixed "Paper content:", never as an image). Errors
// from the client are propagated unchanged — retry lives inside the client.
func (a *ExplainerAgent) Generate(ctx context.Context, in ExplainerInput) (models.ExplainerOutput, error) {
	start := time.Now()
	req := llm.CompletionRequest{
		SystemPrompt: systemPrompt,
		UserPrompt:   a.buildUserPrompt(in),
		DocumentText: in.MarkdownText,
		MaxTokens:    a.cfg.LLM.MaxTokens,
		Temperature:  a.cfg.LLM.Temperature,
	}

	resp, err := a.llm.Complete(ctx, req)
	if err != nil {
		return models.ExplainerOutput{}, err
	}

	// Best-effort parse: a missing heading warns but never fails generation —
	// the full Content is always saved (tradeoff R1; Phase 5 reviewer improves it).
	sections := parseSections(resp.Content)

	out := models.ExplainerOutput{
		PaperID:      in.PaperMeta.ID,
		Content:      resp.Content,
		Sections:     sections,
		Iteration:    1,
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
		CreatedAt:    time.Now().UTC(),
	}

	slog.Info("explainer generation complete",
		"paper_id", in.PaperMeta.ID,
		"input_tokens", resp.InputTokens,
		"output_tokens", resp.OutputTokens,
		"tokens_used", resp.InputTokens+resp.OutputTokens,
		"word_count", wordCount(resp.Content),
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return out, nil
}

// parseSections splits the Markdown on `## ` headings and maps recognized
// headings to their slug → body. It is deliberately lenient: unknown headings are
// ignored, and any of the 9 expected sections that is absent produces a single
// aggregated warning (never an error). Bodies are trimmed of surrounding
// whitespace; the title (a `# ` H1 above the first `## `) is not a section.
func parseSections(content string) map[string]string {
	sections := make(map[string]string, len(sectionKeys))

	// Walk line by line, accumulating the body under the current recognized
	// heading. A `## ` line closes the previous section and may open a new one.
	var curKey string
	var body strings.Builder
	flush := func() {
		if curKey != "" {
			sections[curKey] = strings.TrimSpace(body.String())
		}
		body.Reset()
	}

	for _, line := range strings.Split(content, "\n") {
		if h, ok := strings.CutPrefix(line, "## "); ok {
			flush()
			// Map the heading text (trimmed) to its slug; unknown → skip body.
			curKey = sectionKeys[strings.TrimSpace(h)]
			continue
		}
		if curKey != "" {
			body.WriteString(line)
			body.WriteByte('\n')
		}
	}
	flush()

	// Report any of the 9 that never appeared, as one warning.
	var missing []string
	for _, slug := range sectionKeys {
		if _, ok := sections[slug]; !ok {
			missing = append(missing, slug)
		}
	}
	if len(missing) > 0 {
		slog.Warn("explainer response missing sections", "missing", strings.Join(missing, ","))
	}
	return sections
}

// wordCount is a whitespace-split count for the ~2,500-word observability target.
func wordCount(s string) int {
	return len(strings.Fields(s))
}
