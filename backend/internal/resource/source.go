// Package resource is the declarative resource engine's contract layer. It owns
// the Source interface the orchestrator depends on, the parsed YAML declaration
// shapes, the UI-facing descriptor types, and the capability registries
// (decoders, transforms, derivers, sanitizers, converters) that plug into the
// stable normalization spine.
//
// Like the old arxivquery leaf, this package imports only internal/models (for
// Paper) — never config or orchestrator — so config, the loader, and the
// orchestrator can all depend on it without an import cycle. All arXiv-specific
// behaviour lives in resources/*.yaml, never in this Go code.
package resource

import (
	"context"

	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// Source is a fully-configured, resource-agnostic data source the orchestrator
// sequences. Every concrete resource (arXiv and any future one) is served
// through the single DeclarativeSource implementation built from a YAML
// Declaration, so the orchestrator never imports a concrete resource.
type Source interface {
	// ID is the stable resource identifier (e.g. "arxiv").
	ID() string
	// Descriptor returns the UI-facing field schema for this resource.
	Descriptor() Descriptor
	// Discover fetches a page of candidates. req carries the validated field
	// values; start is the pagination offset; onRetry (nil-safe) fires per
	// transient fetch retry so the caller can surface a progress counter.
	Discover(ctx context.Context, req Request, start int, onRetry func(attempt int)) ([]models.Paper, error)
	// FetchContent fetches a single item's full content as Markdown.
	FetchContent(ctx context.Context, paperID string) (string, error)
	// ValidateValues validates + sanitizes request values against this source's
	// own schema (defaults, select whitelist, text sanitizer), returning the clean
	// map or an error. It is the authoritative safety gate (F21); the API layer
	// calls it too for an early reject.
	ValidateValues(values map[string]string) (map[string]string, error)
	// PageSize is the number of items fetched per page. The orchestrator derives
	// both the pagination cursor step and the "has more" heuristic from this, so
	// page size has a single owner — the engine (F9).
	PageSize() int
}

// Request carries the validated field values for one discovery call.
type Request struct {
	Values map[string]string
}
