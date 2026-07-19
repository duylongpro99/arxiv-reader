package models

import (
	"sync"
	"testing"
	"time"

)

func TestAccessorsMutateUnderLock(t *testing.T) {
	s := NewSession("s1", time.Now(), "arxiv", map[string]string{"category": "cs.AI"})

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

// Phase 4 accessors round-trip under the lock, and none of the new server-only
// fields leak into Snapshot() (which would inflate every /status poll).
func TestPhase4AccessorsRoundTrip(t *testing.T) {
	s := NewSession("s1", time.Now(), "arxiv", map[string]string{"category": "cs.AI"})

	p := &Paper{ID: "2401.12345", Title: "Attention Is All You Need"}
	s.SetSelectedPaper(p)
	if got := s.SelectedPaper(); got == nil || got.ID != "2401.12345" {
		t.Fatalf("SelectedPaper round-trip failed: %+v", got)
	}

	ex := &ExplainerOutput{PaperID: "2401.12345", Content: "# Title\n## Problem Statement\nbody"}
	s.SetExplainer(ex)
	if got := s.Explainer(); got == nil || got.PaperID != "2401.12345" {
		t.Fatalf("Explainer round-trip failed: %+v", got)
	}

	s.SetVaultFile("/vault/AI Papers/2024-01-15_2401.12345_title.md")
	if got := s.VaultFile(); got != "/vault/AI Papers/2024-01-15_2401.12345_title.md" {
		t.Fatalf("VaultFile round-trip failed: %q", got)
	}

	// AddTokens accumulates across calls.
	s.AddTokens(1200)
	s.AddTokens(800)
	if got := s.TokensUsed(); got != 2000 {
		t.Fatalf("TokensUsed = %d, want 2000 (additive)", got)
	}

	// Server-only guarantee: the Phase 4 fields must not surface in Snapshot.
	snap := s.Snapshot()
	if snap.Stage == StageComplete {
		t.Fatal("Snapshot stage should reflect only what SetStage set, not the explainer")
	}
	// SessionSnapshot has no explainer/vaultFile/tokens fields by type; the
	// observable string fields must carry none of that content.
	if snap.Notice != "" || snap.Error != "" {
		t.Fatalf("unexpected snapshot content: notice=%q error=%q", snap.Notice, snap.Error)
	}
}

// SessionSnapshot must never surface the large, server-only markdown. Guarding
// against a future field being added to the snapshot by habit (would leak KBs
// of text to every /status poll).
func TestSnapshotExcludesMarkdown(t *testing.T) {
	s := NewSession("s1", time.Now(), "arxiv", map[string]string{"category": "cs.AI"})
	s.SetMarkdown("secret large markdown body")

	snap := s.Snapshot()
	// The snapshot type has no markdown field at all; assert by reflection-free
	// means — the observable fields carry no markdown.
	if snap.Notice == "secret large markdown body" || snap.Error == "secret large markdown body" {
		t.Fatal("markdown leaked into a snapshot field")
	}
}

func TestRecoverToSelectionPreservesCandidates(t *testing.T) {
	s := NewSession("s1", time.Now(), "arxiv", map[string]string{"category": "cs.AI"})
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
	s := NewSession("s1", time.Now(), "arxiv", map[string]string{"category": "cs.AI"})
	s.SetStage(StageExtracting)
	s.Fail("boom", true)
	if s.Snapshot().Stage != StageFailed {
		t.Fatal("Fail must set StageFailed")
	}
}

// Race guard: concurrent accessor writes + Snapshot reads must be lock-clean
// under `go test -race`.
func TestConcurrentAccess(t *testing.T) {
	s := NewSession("s1", time.Now(), "arxiv", map[string]string{"category": "cs.AI"})
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); s.SetMarkdown("x"); s.SetStage(StageExtracting) }()
		go func() { defer wg.Done(); _ = s.Snapshot() }()
	}
	wg.Wait()
}

// Fail must snapshot the stage that was active BEFORE the transition to failed —
// this is what Phase 6 retry routing keys off to resume the correct segment.
func TestFailCapturesFailedStage(t *testing.T) {
	s := NewSession("s1", time.Now(), "arxiv", map[string]string{"category": "cs.AI"})
	s.SetStage(StageWriting)
	s.Fail("disk full", false)

	if s.Snapshot().Stage != StageFailed {
		t.Fatal("Fail must land in StageFailed")
	}
	if got := s.FailedStage(); got != StageWriting {
		t.Fatalf("FailedStage = %q, want %q (the pre-fail stage)", got, StageWriting)
	}
}

// The Phase 6 accessors round-trip under the lock, split-token accounting is
// additive, and the poll-surfaced fields (errorAction/arxivRetryCount/
// contextWarning) reach Snapshot while the large in/out totals do NOT.
func TestPhase6AccessorsRoundTrip(t *testing.T) {
	s := NewSession("s1", time.Now(), "arxiv", map[string]string{"category": "cs.AI"})

	s.SetErrorAction("fix_config")
	if got := s.ErrorAction(); got != "fix_config" {
		t.Fatalf("ErrorAction = %q, want fix_config", got)
	}

	// AddIO accumulates input/output separately across calls.
	s.AddIO(1000, 200)
	s.AddIO(500, 100)
	if in, out := s.InputTokens(), s.OutputTokens(); in != 1500 || out != 300 {
		t.Fatalf("AddIO totals = (%d,%d), want (1500,300)", in, out)
	}

	s.SetArxivRetryCount(2)
	if got := s.ArxivRetryCount(); got != 2 {
		t.Fatalf("ArxivRetryCount = %d, want 2", got)
	}

	cw := &ContextWarning{EstimatedTokens: 90000, ModelLimit: 64000, Model: "m", Suggestion: "switch"}
	s.SetContextWarning(cw)
	if got := s.ContextWarning(); got == nil || got.EstimatedTokens != 90000 {
		t.Fatalf("ContextWarning round-trip failed: %+v", got)
	}

	// Snapshot surfaces the small poll fields...
	snap := s.Snapshot()
	if snap.ErrorAction != "fix_config" || snap.ArxivRetryCount != 2 || snap.ContextWarning == nil {
		t.Fatalf("Snapshot missing Phase 6 poll fields: %+v", snap)
	}
	// ...but SessionSnapshot has no in/out token fields by type (compile-time
	// guarantee they stay off /status); the accessors remain the only source.
}
