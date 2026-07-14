package store

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// TestPublicationRoundTrip drives the full lifecycle against a real DB:
// create a run (FK parent), insert a publication, list it, get it, edit its
// content, then mark it published — verifying every field round-trips,
// including nullable columns staying nil until explicitly set.
func TestPublicationRoundTrip(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// publications.run_id is a FK to runs(id), so a parent run row is required.
	runID := fmt.Sprintf("test-run-%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = s.pool.Exec(ctx, `DELETE FROM runs WHERE id = $1`, runID) })
	started := time.Now().UTC().Truncate(time.Millisecond)
	if err := s.CreateRun(ctx, RunRecord{ID: runID, Stage: "complete", Status: "complete", StartedAt: started}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	pubID := fmt.Sprintf("test-pub-%d", time.Now().UnixNano())
	created := time.Now().UTC().Truncate(time.Millisecond)
	rec := PublicationRecord{
		ID:             pubID,
		RunID:          runID,
		ChannelID:      "devto",
		Category:       "longform",
		Status:         "draft",
		AdaptedContent: "initial draft body",
		CreatedAt:      created,
	}

	inserted, err := s.CreatePublication(ctx, rec)
	if err != nil {
		t.Fatalf("CreatePublication: %v", err)
	}
	if !inserted {
		t.Fatalf("CreatePublication: expected inserted=true on first insert")
	}

	// Duplicate (run_id, channel_id) must be detected without error, so the
	// HTTP layer can 409 instead of re-posting.
	dupInserted, err := s.CreatePublication(ctx, rec)
	if err != nil {
		t.Fatalf("CreatePublication (duplicate): %v", err)
	}
	if dupInserted {
		t.Fatalf("CreatePublication (duplicate): expected inserted=false")
	}

	list, err := s.ListPublicationsByRun(ctx, runID)
	if err != nil {
		t.Fatalf("ListPublicationsByRun: %v", err)
	}
	if len(list) != 1 || list[0].ID != pubID {
		t.Fatalf("ListPublicationsByRun: %+v", list)
	}
	// Nullable columns must round-trip as nil, not zero-valued strings.
	if list[0].Title != nil || list[0].ExternalURL != nil || list[0].ExternalID != nil ||
		list[0].Error != nil || list[0].PublishedAt != nil {
		t.Fatalf("ListPublicationsByRun: expected nullable columns nil, got %+v", list[0])
	}

	got, err := s.GetPublication(ctx, pubID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if got.Status != "draft" || got.AdaptedContent != "initial draft body" {
		t.Fatalf("GetPublication mismatch: %+v", got)
	}

	if err := s.UpdatePublicationContent(ctx, pubID, "Edited Title", "edited body", "approved"); err != nil {
		t.Fatalf("UpdatePublicationContent: %v", err)
	}
	got, err = s.GetPublication(ctx, pubID)
	if err != nil {
		t.Fatalf("GetPublication (after edit): %v", err)
	}
	if got.Title == nil || *got.Title != "Edited Title" || got.AdaptedContent != "edited body" || got.Status != "approved" {
		t.Fatalf("UpdatePublicationContent did not persist: %+v", got)
	}

	if err := s.MarkPublished(ctx, pubID, "https://dev.to/x/1", "ext-1"); err != nil {
		t.Fatalf("MarkPublished: %v", err)
	}
	got, err = s.GetPublication(ctx, pubID)
	if err != nil {
		t.Fatalf("GetPublication (after publish): %v", err)
	}
	if got.Status != "published" || got.ExternalURL == nil || *got.ExternalURL != "https://dev.to/x/1" ||
		got.ExternalID == nil || *got.ExternalID != "ext-1" || got.PublishedAt == nil {
		t.Fatalf("MarkPublished did not persist: %+v", got)
	}
}

// TestPublicationMarkFailed verifies the failure transition records the error
// message and status without disturbing the row's identity/unique slot.
func TestPublicationMarkFailed(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	runID := fmt.Sprintf("test-run-%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = s.pool.Exec(ctx, `DELETE FROM runs WHERE id = $1`, runID) })
	started := time.Now().UTC().Truncate(time.Millisecond)
	if err := s.CreateRun(ctx, RunRecord{ID: runID, Stage: "complete", Status: "complete", StartedAt: started}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	pubID := fmt.Sprintf("test-pub-%d", time.Now().UnixNano())
	inserted, err := s.CreatePublication(ctx, PublicationRecord{
		ID: pubID, RunID: runID, ChannelID: "x", Category: "brief",
		Status: "approved", AdaptedContent: "brief body", CreatedAt: time.Now().UTC(),
	})
	if err != nil || !inserted {
		t.Fatalf("CreatePublication: inserted=%v err=%v", inserted, err)
	}

	if err := s.MarkFailed(ctx, pubID, "rate limited by channel API"); err != nil {
		t.Fatalf("MarkFailed: %v", err)
	}
	got, err := s.GetPublication(ctx, pubID)
	if err != nil {
		t.Fatalf("GetPublication: %v", err)
	}
	if got.Status != "failed" || got.Error == nil || *got.Error != "rate limited by channel API" {
		t.Fatalf("MarkFailed did not persist: %+v", got)
	}
}

// TestGetPublicationNotFound verifies the typed not-found signal.
func TestGetPublicationNotFound(t *testing.T) {
	s := testStore(t)
	if _, err := s.GetPublication(context.Background(), "does-not-exist-xyz"); !errors.Is(err, ErrPublicationNotFound) {
		t.Fatalf("GetPublication(unknown) = %v, want ErrPublicationNotFound", err)
	}
}
