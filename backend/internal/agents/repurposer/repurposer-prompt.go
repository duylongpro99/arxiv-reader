package repurposer

import (
	"fmt"
	"strings"

	"github.com/maritime-ds/arxiv-reader/internal/channels"
)

// sharedAccessibilityNote is repeated verbatim in every category prompt: it
// tells the model to lean on the source ExplainerAgent's most accessible
// sections instead of re-deriving "explain simply" logic per category (DRY —
// one accessibility instruction, three depth/length variants).
const sharedAccessibilityNote = `The source material is an explainer note produced by another agent. It
already contains an "## Analogies & Intuition" section and a "## Glossary" of
plain-English term definitions. Lean on both heavily: reuse the analogies,
keep the glossary's plain-English framing, and never reintroduce jargon the
glossary already simplified.`

// longformSystemPrompt — category Longform: a full reader-friendly article
// with concrete examples and code where natural. v1 channel mapping: dev.to.
const longformSystemPrompt = `You are a technical writer adapting an AI research explainer into a
full-length, reader-friendly article for a technical developer blog.

` + sharedAccessibilityNote + `

Write a complete, standalone article:
- Open with a hook that states why a working engineer should care.
- Walk through the core idea and methodology using concrete examples; include
  a short illustrative code snippet ONLY where it genuinely clarifies a
  mechanism (e.g. pseudocode for an algorithm) — never pad with code for its
  own sake.
- Close with a clear "why it matters" takeaway.
- Use standard Markdown with a single leading "# Title" heading, then
  "## "-level subheadings for the body.
- Do not mention any publishing platform, channel, or content category by
  name — you are producing platform-agnostic content.`

// digestSystemPrompt — category Digest: a mid-length condensed summary.
// Reserved for a future RSS channel (daily.dev has no push API).
const digestSystemPrompt = `You are a technical writer condensing an AI research explainer into a
mid-length digest summary suitable for an RSS-style feed.

` + sharedAccessibilityNote + `

Write a condensed but complete summary:
- Cover the problem, core idea, and why it matters in a tight, connected
  narrative — not a bare bullet list.
- Include at most one concrete example or analogy; skip code entirely.
- Use a single leading "# Title" heading, then plain paragraphs (no deep
  subheading structure needed for this length).
- Do not mention any publishing platform, channel, or content category by
  name — you are producing platform-agnostic content.`

// briefSystemPrompt — category Brief: a punchy hook + key takeaway as ONE
// short, coherent piece. It is NOT pre-chunked into tweets/posts — the
// consuming Channel (e.g. X) owns mechanical chunking, never this agent.
const briefSystemPrompt = `You are a technical writer distilling an AI research explainer into one
short, punchy piece of standalone prose — a hook plus the single most
important takeaway.

` + sharedAccessibilityNote + `

Write ONE short, coherent piece of prose:
- Do NOT format it as a list, a numbered thread, or separate posts/tweets —
  write flowing prose that a platform-specific channel can mechanically split
  later if it needs to.
- Lead with the single most surprising or useful fact from the paper.
- End with why a practitioner should care, in one sentence.
- Use a single leading "# Title" heading followed by the prose body.
- Do not mention any publishing platform, channel, or content category by
  name — you are producing platform-agnostic content.`

// systemPromptFor selects the category-specific prompt. Generate already
// guards on Category.Valid() before calling this, so the default branch is
// unreachable in practice — kept as defense-in-depth rather than a panic, so
// a future Category addition without a matching prompt degrades to Digest
// (the middle-ground template) instead of crashing.
func systemPromptFor(category channels.Category) string {
	switch category {
	case channels.Longform:
		return longformSystemPrompt
	case channels.Digest:
		return digestSystemPrompt
	case channels.Brief:
		return briefSystemPrompt
	default:
		return digestSystemPrompt
	}
}

// buildUserPrompt assembles the per-paper user message: metadata plus the
// target word count resolved from config. The source explainer text itself
// rides in DocumentText (mirroring ExplainerAgent's text-only contract — see
// buildUserPrompt in agents/explainer-prompt.go), never inlined here.
func buildUserPrompt(in RepurposeInput, targetWords int) string {
	return fmt.Sprintf(
		"Paper metadata:\nTitle: %s\nAuthors: %s\nPublished: %s\narXiv ID: %s\n\n"+
			"Target length: approximately %d words. You may vary moderately, but do not "+
			"pad or truncate to hit the number artificially.\n\n"+
			"The source explainer note is provided as text below. Read it carefully, then "+
			"produce the requested content.",
		in.PaperMeta.Title,
		strings.Join(in.PaperMeta.Authors, ", "),
		in.PaperMeta.Published,
		in.PaperMeta.ID,
		targetWords,
	)
}
