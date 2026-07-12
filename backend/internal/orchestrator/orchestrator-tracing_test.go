package orchestrator

import (
	"testing"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/llm"
	"github.com/maritime-ds/arxiv-reader/internal/models"
	"github.com/maritime-ds/arxiv-reader/internal/tools"
	"github.com/maritime-ds/arxiv-reader/internal/tracing"
)

// This file asserts the EMITTED EVENT SEQUENCE (design §4 taxonomy) for the key
// pipeline scenarios. It uses a real, DB-less Tracer (nil store) so the ring
// buffer captures the ordered timeline, then reads it back via Snapshot.

// withTracer attaches an enabled, in-memory-only tracer and returns it so the
// test can read the run's recorder afterwards.
func withTracer(o *Orchestrator) *tracing.Tracer {
	tr := tracing.New(true, nil, nil, false, 256, "")
	o.tracer = tr
	return tr
}

func kindsOf(evts []tracing.Event) []tracing.EventKind {
	out := make([]tracing.EventKind, len(evts))
	for i, e := range evts {
		out[i] = e.Kind
	}
	return out
}

func assertKinds(t *testing.T, got, want []tracing.EventKind) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("event sequence length = %d, want %d\n got: %v\nwant: %v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event[%d] = %q, want %q\n got: %v\nwant: %v", i, got[i], want[i], got, want)
		}
	}
}

// waitTerminal waits until the recorder for id is closed (true terminal —
// completion or non-recoverable failure), guaranteeing run.completed/run.failed
// was emitted before we snapshot.
func waitTerminal(t *testing.T, tr *tracing.Tracer, id string) *tracing.Recorder {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r := tr.Recorder(id); r != nil && r.IsTerminal() {
			return r
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("recorder never reached terminal")
	return nil
}

// waitLastKind waits until the recorder's newest buffered event is `kind` (used
// for non-terminal outcomes like recoverable failure / 404 re-pick, where the
// recorder stays open).
func waitLastKind(t *testing.T, tr *tracing.Tracer, id string, kind tracing.EventKind) *tracing.Recorder {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if r := tr.Recorder(id); r != nil {
			if snap := r.Snapshot(-1); len(snap) > 0 && snap[len(snap)-1].Kind == kind {
				return r
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("recorder never emitted %q as its last event", kind)
	return nil
}

func TestTraceHappyPathSequence(t *testing.T) {
	// newProcessOrch has MaxReviewIterations=0 → reviewer disabled (no review events).
	o := newProcessOrch(&fakeContent{md: "# Extracted paper"})
	tr := withTracer(o)
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	rec := waitTerminal(t, tr, s.SessionID)

	assertKinds(t, kindsOf(rec.Snapshot(-1)), []tracing.EventKind{
		tracing.KindSelectionChosen,
		tracing.KindToolPaperContentStarted,
		tracing.KindToolPaperContentCompleted,
		tracing.KindLLMExplainerStarted,
		tracing.KindLLMExplainerCompleted,
		tracing.KindToolVaultWriterCompleted,
		tracing.KindRunCompleted,
	})
}

func TestTraceReviseThenPassSequence(t *testing.T) {
	exp := &scriptedExplainer{body: reviewBody}
	rev := &scriptedReviewer{outcomes: []reviewOutcome{
		{v: models.ReviewVerdict{Pass: false, Score: 0.6, Iteration: 1, TokensUsed: 500, Feedback: map[string]string{"core_idea": "fix"}}},
		{v: models.ReviewVerdict{Pass: true, Score: 0.9, Iteration: 2, TokensUsed: 500}},
	}}
	o := reviewOrch(2, exp, rev, &fakeVault{path: "/vault/n.md"})
	tr := withTracer(o)
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	rec := waitTerminal(t, tr, s.SessionID)

	assertKinds(t, kindsOf(rec.Snapshot(-1)), []tracing.EventKind{
		tracing.KindSelectionChosen,
		tracing.KindToolPaperContentStarted,
		tracing.KindToolPaperContentCompleted,
		tracing.KindLLMExplainerStarted,   // pass 1 generate
		tracing.KindLLMExplainerCompleted,
		tracing.KindLLMReviewerStarted,    // pass 1 review → FAIL
		tracing.KindLLMReviewerCompleted,
		tracing.KindDecisionRevise,
		tracing.KindLLMExplainerStarted,   // pass 2 revise
		tracing.KindLLMExplainerCompleted,
		tracing.KindLLMReviewerStarted,    // pass 2 review → PASS
		tracing.KindLLMReviewerCompleted,
		tracing.KindDecisionAccept,
		tracing.KindToolVaultWriterCompleted,
		tracing.KindRunCompleted,
	})
}

func TestTraceMaxIterationsSequence(t *testing.T) {
	exp := &scriptedExplainer{body: reviewBody}
	rev := &scriptedReviewer{outcomes: []reviewOutcome{
		{v: models.ReviewVerdict{Pass: false, Score: 0.5, Iteration: 1, TokensUsed: 500, Feedback: map[string]string{"glossary": "x"}}},
		{v: models.ReviewVerdict{Pass: false, Score: 0.6, Iteration: 2, TokensUsed: 500}},
	}}
	o := reviewOrch(2, exp, rev, &fakeVault{path: "/vault/n.md"})
	tr := withTracer(o)
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	rec := waitTerminal(t, tr, s.SessionID)

	// The loop hits the cap on pass 2: decision.max_iterations, then vault + complete.
	got := kindsOf(rec.Snapshot(-1))
	last3 := got[len(got)-3:]
	assertKinds(t, last3, []tracing.EventKind{
		tracing.KindDecisionMaxIterations,
		tracing.KindToolVaultWriterCompleted,
		tracing.KindRunCompleted,
	})
}

func TestTrace404RecoverSequence(t *testing.T) {
	o := newProcessOrch(&fakeContent{err: tools.ErrPaperHTMLNotFound})
	tr := withTracer(o)
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	rec := waitLastKind(t, tr, s.SessionID, tracing.KindRunRecoveredToSelection)

	assertKinds(t, kindsOf(rec.Snapshot(-1)), []tracing.EventKind{
		tracing.KindSelectionChosen,
		tracing.KindToolPaperContentStarted,
		tracing.KindRunRecoveredToSelection,
	})
	// A recovery is NOT terminal — the recorder stays open for the next pick.
	if rec.IsTerminal() {
		t.Fatal("recovery must not close the recorder (run continues after re-pick)")
	}
}

func TestTraceGenerationFailureSequence(t *testing.T) {
	// Transient generation error (timeout) → recoverable failure.
	o := newProcessOrch(&fakeContent{md: "md"}, func(o *Orchestrator) {
		o.explainer = &fakeExplainer{err: llm.ErrLLMTimeout}
	})
	tr := withTracer(o)
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	rec := waitLastKind(t, tr, s.SessionID, tracing.KindRunFailed)

	assertKinds(t, kindsOf(rec.Snapshot(-1)), []tracing.EventKind{
		tracing.KindSelectionChosen,
		tracing.KindToolPaperContentStarted,
		tracing.KindToolPaperContentCompleted,
		tracing.KindLLMExplainerStarted, // failed here — no completed
		tracing.KindRunFailed,
	})
	// Recoverable failure keeps the recorder OPEN (retry resumes the same run).
	if rec.IsTerminal() {
		t.Fatal("recoverable failure must not close the recorder")
	}
}

func TestTraceNonRecoverableFailureClosesRecorder(t *testing.T) {
	// A bad-request generation error is non-recoverable → true terminal → closed.
	o := newProcessOrch(&fakeContent{md: "md"}, func(o *Orchestrator) {
		o.explainer = &fakeExplainer{err: llm.ErrLLMBadRequest}
	})
	tr := withTracer(o)
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	rec := waitTerminal(t, tr, s.SessionID) // IsTerminal ⇒ closed
	got := kindsOf(rec.Snapshot(-1))
	if got[len(got)-1] != tracing.KindRunFailed {
		t.Fatalf("last event = %q, want run.failed", got[len(got)-1])
	}
}

func TestTraceDiscoverySequence(t *testing.T) {
	o := newOrch(testCfg(5), &fakeFetcher{papers: makePapers(8)}, passthrough())
	tr := withTracer(o)
	o.cfg.Agent.ArxivCategory = "cs.AI"

	id := discover(t, o)
	waitStatus(t, o, id) // reaches selection

	rec := waitLastKind(t, tr, id, tracing.KindSelectionPresented)
	assertKinds(t, kindsOf(rec.Snapshot(-1)), []tracing.EventKind{
		tracing.KindDiscoveryStarted,
		tracing.KindToolDiscoveryCompleted,
		tracing.KindToolLogcheckCompleted,
		tracing.KindSelectionPresented,
	})
}
