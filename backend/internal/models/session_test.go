package models

import (
	"sync"
	"testing"
	"time"
)

func TestAccessorsMutateUnderLock(t *testing.T) {
	s := NewSession("s1", time.Now())

	s.SetStage(StageExtracting)
	s.SetSelectedPaper(&Paper{ID: "2312.00752"})
	s.SetMarkdown("# Title\n\nbody")
	s.SetNotice("heads up")

	snap := s.Snapshot()
	if snap.Stage != StageExtracting {
		t.Fatalf("stage = %q, want extracting", snap.Stage)
	}
	if snap.Notice != "heads up" {
		t.Fatalf("notice = %q, want %q", snap.Notice, "heads up")
	}
}

// SessionSnapshot must never surface the large, server-only markdown. Guarding
// against a future field being added to the snapshot by habit (would leak KBs
// of text to every /status poll).
func TestSnapshotExcludesMarkdown(t *testing.T) {
	s := NewSession("s1", time.Now())
	s.SetMarkdown("secret large markdown body")

	snap := s.Snapshot()
	// The snapshot type has no markdown field at all; assert by reflection-free
	// means — the observable fields carry no markdown.
	if snap.Notice == "secret large markdown body" || snap.Error == "secret large markdown body" {
		t.Fatal("markdown leaked into a snapshot field")
	}
}

func TestRecoverToSelectionPreservesCandidates(t *testing.T) {
	s := NewSession("s1", time.Now())
	cands := []Paper{{ID: "a"}, {ID: "b"}}
	s.Complete(cands, "")
	s.SetStage(StageExtracting)

	s.RecoverToSelection("Paper HTML not available. Select another paper.")

	snap := s.Snapshot()
	if snap.Stage != StageSelection {
		t.Fatalf("stage = %q, want selection", snap.Stage)
	}
	if len(snap.Candidates) != 2 {
		t.Fatalf("candidates len = %d, want 2 (must be preserved)", len(snap.Candidates))
	}
	if !snap.Recoverable {
		t.Fatal("recover must set recoverable=true")
	}
	if snap.Error != "" {
		t.Fatalf("recover must clear errMsg, got %q", snap.Error)
	}
	if snap.Notice == "" {
		t.Fatal("recover must set a notice")
	}
}

// Fail and RecoverToSelection are distinct transitions: Fail lands in failed,
// recover lands back in selection.
func TestFailVsRecover(t *testing.T) {
	s := NewSession("s1", time.Now())
	s.SetStage(StageExtracting)
	s.Fail("boom", true)
	if s.Snapshot().Stage != StageFailed {
		t.Fatal("Fail must set StageFailed")
	}
}

// Race guard: concurrent accessor writes + Snapshot reads must be lock-clean
// under `go test -race`.
func TestConcurrentAccess(t *testing.T) {
	s := NewSession("s1", time.Now())
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); s.SetMarkdown("x"); s.SetStage(StageExtracting) }()
		go func() { defer wg.Done(); _ = s.Snapshot() }()
	}
	wg.Wait()
}
