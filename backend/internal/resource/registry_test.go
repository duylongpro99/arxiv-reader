package resource

import (
	"context"
	"testing"

	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// fakeSource is a minimal Source for registry tests.
type fakeSource struct{ id string }

func (f fakeSource) ID() string             { return f.id }
func (f fakeSource) Descriptor() Descriptor { return Descriptor{ID: f.id, Label: f.id} }
func (f fakeSource) Discover(context.Context, Request, int, func(int)) ([]models.Paper, error) {
	return nil, nil
}
func (f fakeSource) FetchContent(context.Context, string) (string, error) { return "", nil }
func (f fakeSource) ValidateValues(v map[string]string) (map[string]string, error) {
	return v, nil
}
func (f fakeSource) PageSize() int { return 10 }

func TestRegistryRegisterGet(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(fakeSource{id: "arxiv"}); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, ok := r.Get("arxiv")
	if !ok || got.ID() != "arxiv" {
		t.Fatalf("Get(arxiv) = %v, %v", got, ok)
	}
	if _, ok := r.Get("missing"); ok {
		t.Fatalf("Get(missing) should be false")
	}
}

func TestRegistryDuplicateRejected(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(fakeSource{id: "arxiv"})
	if err := r.Register(fakeSource{id: "arxiv"}); err == nil {
		t.Fatalf("expected duplicate id error")
	}
}

func TestRegistryListSortedAndDescriptors(t *testing.T) {
	r := NewRegistry()
	_ = r.Register(fakeSource{id: "zenodo"})
	_ = r.Register(fakeSource{id: "arxiv"})
	list := r.List()
	if len(list) != 2 || list[0].ID() != "arxiv" || list[1].ID() != "zenodo" {
		t.Fatalf("List not stable-sorted by ID: %v", list)
	}
	desc := r.Descriptors()
	if len(desc) != 2 || desc[0].ID != "arxiv" {
		t.Fatalf("Descriptors wrong: %v", desc)
	}
}
