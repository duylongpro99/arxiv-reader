package resource

import (
	"fmt"
	"sort"
)

// Registry is the set of loaded Sources, keyed by ID. It is built once at
// startup by the loader and read-only thereafter (the orchestrator only
// Gets/Lists), so it needs no locking.
type Registry struct {
	sources map[string]Source
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{sources: map[string]Source{}}
}

// Register adds a Source. A duplicate ID is a configuration error surfaced to
// the loader (two declarations claiming the same id).
func (r *Registry) Register(s Source) error {
	id := s.ID()
	if _, exists := r.sources[id]; exists {
		return fmt.Errorf("duplicate resource id %q", id)
	}
	r.sources[id] = s
	return nil
}

// Get returns the Source for id, or ok=false if none is registered.
func (r *Registry) Get(id string) (Source, bool) {
	s, ok := r.sources[id]
	return s, ok
}

// List returns all sources, stable-sorted by ID for deterministic output.
func (r *Registry) List() []Source {
	out := make([]Source, 0, len(r.sources))
	for _, s := range r.sources {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID() < out[j].ID() })
	return out
}

// Descriptors returns every source's UI descriptor (stable order). Always a
// non-nil slice so it serializes as a JSON array, never null.
func (r *Registry) Descriptors() []Descriptor {
	srcs := r.List()
	out := make([]Descriptor, 0, len(srcs))
	for _, s := range srcs {
		out = append(out, s.Descriptor())
	}
	return out
}

// Len reports how many sources are registered (the loader rejects an empty set).
func (r *Registry) Len() int { return len(r.sources) }
