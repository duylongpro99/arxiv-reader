// Package channels defines the publish-side half of the channel-publishing
// feature: a Channel interface + the content Category it consumes, plus a
// config-driven registry that mirrors internal/llm's provider pattern. This
// package is deliberately blind to content GENERATION — internal/agents/
// repurposer produces GeneratedContent knowing only a Category, never a
// concrete channel. See docs/design-notes/2026-07-14-channel-publishing.md.
package channels

// Category is the content-depth axis a Channel consumes: NOT a platform format.
// It is the single seam between the Repurposer agent (which only ever sees a
// Category) and a Channel (which registers the one Category it accepts).
// Splitting on category instead of per-channel format keeps adding a channel
// from rippling into the agent — see the design note's "key seam" section.
type Category string

const (
	// Longform is a deep, reader-friendly article with concrete examples and
	// code where natural. v1 mapping: dev.to.
	Longform Category = "longform"
	// Digest is a mid-length condensed summary. Reserved for a future RSS
	// channel (daily.dev has no push API — see design note "hard truths").
	Digest Category = "digest"
	// Brief is a punchy hook + key takeaway as a single short, coherent piece —
	// NOT pre-chunked into platform units. v1 mapping: X (the X channel is the
	// one that mechanically chunks it into tweets; the agent never emits a
	// "thread").
	Brief Category = "brief"
)

// Valid reports whether c is one of the three known categories. Mirrors the
// defense-in-depth style of config.validProviders: callers (the registry,
// config validation, the Repurposer's template selector) all guard on this
// before trusting a Category value that ultimately originated from user input
// or a config file, rather than trusting the compiler alone.
func (c Category) Valid() bool {
	switch c {
	case Longform, Digest, Brief:
		return true
	default:
		return false
	}
}
