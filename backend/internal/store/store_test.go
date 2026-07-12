package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestOpenEmptyURLUnavailable verifies the degrade contract with no DB required:
// an empty DSN yields ErrStoreUnavailable (the caller logs + continues), never a
// panic or a live pool.
func TestOpenEmptyURLUnavailable(t *testing.T) {
	if _, err := Open(context.Background(), ""); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("Open(\"\") = %v, want ErrStoreUnavailable", err)
	}
}

// TestOpenBadURLUnavailable verifies an unreachable/garbage DSN also degrades
// (does not hang past pingTimeout, does not leak the DSN in the error).
func TestOpenBadURLUnavailable(t *testing.T) {
	// Loopback:1 has nothing listening → connect fails fast within pingTimeout.
	_, err := Open(context.Background(), "postgres://u:p@127.0.0.1:1/db?sslmode=disable")
	if !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("Open(bad) = %v, want ErrStoreUnavailable", err)
	}
}

// testStore connects to the DATABASE_URL Postgres or skips. The migration
// (0001_run_timeline.sql) must already be applied — see the package doc.
func testStore(t *testing.T) *Store {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set — skipping DB-backed store test")
	}
	s, err := Open(context.Background(), url)
	if err != nil {
		t.Fatalf("Open(DATABASE_URL): %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func ptr[T any](v T) *T { return &v }

// TestRunAndEventRoundTrip drives the full lifecycle against a real DB: create a
// run, set its paper, append ordered events, finalize, then read everything back.
func TestRunAndEventRoundTrip(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	// Unique id so parallel/re-runs never collide; cleaned up via ON DELETE CASCADE.
	id := fmt.Sprintf("test-%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = s.pool.Exec(ctx, `DELETE FROM runs WHERE id = $1`, id) })

	started := time.Now().UTC().Truncate(time.Millisecond)
	if err := s.CreateRun(ctx, RunRecord{ID: id, Stage: "discovery", Status: "running", StartedAt: started}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	// Idempotent re-create must not error (retry reuses the session id).
	if err := s.CreateRun(ctx, RunRecord{ID: id, Stage: "discovery", Status: "running", StartedAt: started}); err != nil {
		t.Fatalf("CreateRun (idempotent): %v", err)
	}

	if err := s.UpdateRunPaper(ctx, id, "2401.12345", "A Paper Title"); err != nil {
		t.Fatalf("UpdateRunPaper: %v", err)
	}

	events := []EventRecord{
		{RunID: id, Seq: 0, EventType: "discovery.started", Stage: "discovery", Title: "started", Status: "info", CreatedAt: started},
		{RunID: id, Seq: 1, EventType: "selection.chosen", Stage: "selection", Title: "chose paper", Status: "success",
			Summary: json.RawMessage(`{"paperId":"2401.12345"}`), DurationMS: ptr(620), CreatedAt: started.Add(time.Second)},
	}
	for _, e := range events {
		if err := s.AppendEvent(ctx, e); err != nil {
			t.Fatalf("AppendEvent seq=%d: %v", e.Seq, err)
		}
	}
	// Duplicate seq must be dropped, not duplicated (ON CONFLICT DO NOTHING).
	if err := s.AppendEvent(ctx, events[0]); err != nil {
		t.Fatalf("AppendEvent (duplicate): %v", err)
	}

	completed := started.Add(2 * time.Second)
	if err := s.FinalizeRun(ctx, RunRecord{
		ID: id, Stage: "complete", Status: "complete", InputTokens: 1200, OutputTokens: 800,
		EstCostUSD: ptr(0.11), ReviewPassed: ptr(true), CompletedAt: &completed,
	}); err != nil {
		t.Fatalf("FinalizeRun: %v", err)
	}

	got, err := s.GetRun(ctx, id)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != "complete" || got.InputTokens != 1200 || got.OutputTokens != 800 {
		t.Fatalf("GetRun finalized fields wrong: %+v", got)
	}
	if got.PaperID == nil || *got.PaperID != "2401.12345" {
		t.Fatalf("GetRun paper id: %+v", got.PaperID)
	}
	if got.EstCostUSD == nil || *got.EstCostUSD < 0.10 || *got.EstCostUSD > 0.12 {
		t.Fatalf("GetRun est cost: %+v", got.EstCostUSD)
	}

	all, err := s.ListEvents(ctx, id, -1)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(all) != 2 || all[0].Seq != 0 || all[1].Seq != 1 {
		t.Fatalf("ListEvents order/count wrong: %d events", len(all))
	}

	// sinceSeq resume: seq > 0 returns only the second event.
	since, err := s.ListEvents(ctx, id, 0)
	if err != nil {
		t.Fatalf("ListEvents(since=0): %v", err)
	}
	if len(since) != 1 || since[0].Seq != 1 {
		t.Fatalf("ListEvents resume filter wrong: %+v", since)
	}

	runs, total, err := s.ListRuns(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if total < 1 || len(runs) < 1 {
		t.Fatalf("ListRuns empty: total=%d len=%d", total, len(runs))
	}
}

// TestGetRunNotFound verifies the typed not-found signal.
func TestGetRunNotFound(t *testing.T) {
	s := testStore(t)
	if _, err := s.GetRun(context.Background(), "does-not-exist-xyz"); !errors.Is(err, ErrRunNotFound) {
		t.Fatalf("GetRun(unknown) = %v, want ErrRunNotFound", err)
	}
}
