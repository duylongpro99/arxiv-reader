package channels

import (
	"context"

	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// GeneratedContent is the platform-agnostic output of the Repurposer agent.
// It is the ONLY contract between content generation and publishing: a Channel
// receives exactly this shape regardless of which category-specific prompt
// produced it. Body is markdown/plain text — never HTML, never pre-chunked
// into platform-specific units (e.g. a Channel, not the agent, splits Brief
// into ≤280-char tweets).
type GeneratedContent struct {
	Category  Category
	Title     string
	Body      string // platform-agnostic markdown/plain text
	PaperMeta models.Paper
	Tags      []string
}

// PublishResult is what a Channel returns after a successful Publish call —
// enough to persist a durable, idempotent publication record (see the design
// note's `publications` table) and to link back to the live post.
type PublishResult struct {
	ExternalURL string
	ExternalID  string
}

// Channel is the single abstraction the rest of the system depends on for
// publishing. Each concrete channel (dev.to, X, ...) lives in its own
// sub-package and owns ALL platform mechanics — including non-LLM delivery
// shaping — behind this interface. Mirrors llm.LLMClient: one small interface,
// one registry switch (registry.go), zero coupling from callers to concrete
// providers.
type Channel interface {
	// ID is the channel's stable identifier, matching the id used in
	// config.PublishingConfig.Channels and the registry switch (e.g. "devto").
	ID() string

	// Category is the ONLY content contract a Channel has with the Repurposer:
	// which single category of GeneratedContent this channel consumes.
	Category() Category

	// Validate checks platform constraints (char limits, required fields)
	// before Publish is attempted, so failures surface as fast, local errors
	// rather than failed network calls.
	Validate(c GeneratedContent) error

	// Publish pushes the content live and returns the external URL/ID needed
	// to record a durable, idempotent publication. Errors are channel-specific
	// but must never be silently swallowed — publish failures must be visible
	// and retryable per-channel (design note point 4).
	Publish(ctx context.Context, c GeneratedContent) (PublishResult, error)
}
