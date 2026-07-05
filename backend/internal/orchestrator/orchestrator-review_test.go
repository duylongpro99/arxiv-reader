package orchestrator

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/maritime-ds/arxiv-reader/internal/agents"
	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/maritime-ds/arxiv-reader/internal/llm"
	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// --- Phase 5 review-loop fakes ---
//
// All fake methods run inside the single pipeline goroutine, sequentially; the
// test only reads their captured state after observing a terminal stage (which
// establishes a happens-before edge via the session mutex), so plain fields are
// race-safe here.

// scriptedExplainer returns a fixed body each call, recording the RevisionNote it
// received and (optionally) the session stage at call time.
type scriptedExplainer struct {
	body   string
	err    error
	calls  int
	notes  []string
	stages *[]models.PipelineStage
	sess   *models.PipelineSession
}

func (e *scriptedExplainer) Generate(_ context.Context, in agents.ExplainerInput) (models.ExplainerOutput, error) {
	e.calls++
	e.notes = append(e.notes, in.RevisionNote)
	if e.stages != nil && e.sess != nil {
		*e.stages = append(*e.stages, e.sess.Snapshot().Stage)
	}
	if e.err != nil {
		return models.ExplainerOutput{}, e.err
	}
	return models.ExplainerOutput{PaperID: in.PaperMeta.ID, Content: e.body, InputTokens: 100, OutputTokens: 50}, nil
}

// reviewOutcome scripts one Review call's return values.
type reviewOutcome struct {
	v   models.ReviewVerdict
	err error
}

// scriptedReviewer returns outcomes[callIndex] on each call.
type scriptedReviewer struct {
	outcomes []reviewOutcome
	calls    int
	stages   *[]models.PipelineStage
	sess     *models.PipelineSession
}

func (r *scriptedReviewer) Review(_ context.Context, _ models.ExplainerOutput, _ models.Paper, iteration int) (models.ReviewVerdict, error) {
	if r.stages != nil && r.sess != nil {
		*r.stages = append(*r.stages, r.sess.Snapshot().Stage)
	}
	idx := r.calls
	r.calls++
	oc := r.outcomes[idx]
	return oc.v, oc.err
}

// reviewOrch builds an orchestrator with the review loop wired at the given cap.
func reviewOrch(maxIter int, exp Explainer, rev Reviewer, v *fakeVault) *Orchestrator {
	return &Orchestrator{
		cfg:       &config.Config{Agent: config.AgentConfig{DisplayLimit: 5, MaxReviewIterations: maxIter}},
		disco:     &fakeFetcher{},
		logCheck:  passthrough(),
		content:   &fakeContent{md: "# extracted"},
		explainer: exp,
		reviewer:  rev,
		vault:     v,
	}
}

const reviewBody = "# Note\n## Problem Statement\nbody"

func TestLoopDisabledMaxZero(t *testing.T) {
	exp := &scriptedExplainer{body: reviewBody}
	rev := &scriptedReviewer{} // must never be called
	fv := &fakeVault{path: "/vault/AI Papers/n.md"}
	o := reviewOrch(0, exp, rev, fv)
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageComplete })

	if exp.calls != 1 {
		t.Fatalf("max=0 should generate exactly once, got %d", exp.calls)
	}
	if rev.calls != 0 {
		t.Fatalf("max=0 must not review, got %d calls", rev.calls)
	}
	if fv.lastVerdict != nil {
		t.Fatalf("max=0 → nil verdict at vault, got %+v", fv.lastVerdict)
	}
	if s.TokensUsed() != 150 {
		t.Fatalf("tokens = %d, want 150 (one generation)", s.TokensUsed())
	}
}

func TestLoopPassFirstIteration(t *testing.T) {
	exp := &scriptedExplainer{body: reviewBody}
	rev := &scriptedReviewer{outcomes: []reviewOutcome{
		{v: models.ReviewVerdict{Pass: true, Score: 0.9, Iteration: 1, TokensUsed: 500}},
	}}
	fv := &fakeVault{path: "/vault/n.md"}
	o := reviewOrch(2, exp, rev, fv)
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageComplete })

	if exp.calls != 1 || rev.calls != 1 {
		t.Fatalf("pass on iter 1: want 1 gen/1 review, got %d/%d", exp.calls, rev.calls)
	}
	if exp.notes[0] != "" {
		t.Fatalf("first generation must have no revision note, got %q", exp.notes[0])
	}
	if fv.lastVerdict == nil || !fv.lastVerdict.Pass {
		t.Fatalf("passed verdict expected at vault, got %+v", fv.lastVerdict)
	}
	if s.TokensUsed() != 650 {
		t.Fatalf("tokens = %d, want 650 (150 gen + 500 review)", s.TokensUsed())
	}
}

func TestLoopFailThenPassThreadsRevisionNote(t *testing.T) {
	exp := &scriptedExplainer{body: reviewBody}
	rev := &scriptedReviewer{outcomes: []reviewOutcome{
		{v: models.ReviewVerdict{Pass: false, Score: 0.5, Iteration: 1, TokensUsed: 500, Feedback: map[string]string{"core_idea": "bridge the analogy"}}},
		{v: models.ReviewVerdict{Pass: true, Score: 0.9, Iteration: 2, TokensUsed: 500}},
	}}
	fv := &fakeVault{path: "/vault/n.md"}
	o := reviewOrch(2, exp, rev, fv)
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageComplete })

	if exp.calls != 2 || rev.calls != 2 {
		t.Fatalf("fail-then-pass: want 2 gen/2 review, got %d/%d", exp.calls, rev.calls)
	}
	if exp.notes[0] != "" {
		t.Fatalf("iter 1 must have empty revision note, got %q", exp.notes[0])
	}
	if !strings.Contains(exp.notes[1], "REVISION REQUIRED") || !strings.Contains(exp.notes[1], "bridge the analogy") {
		t.Fatalf("iter 2 must receive the formatted revision note, got %q", exp.notes[1])
	}
	if fv.lastVerdict == nil || !fv.lastVerdict.Pass {
		t.Fatalf("final passed verdict expected, got %+v", fv.lastVerdict)
	}
	// Tokens: 2 generations (300) + 2 reviews (1000) = 1300.
	if s.TokensUsed() != 1300 {
		t.Fatalf("tokens = %d, want 1300", s.TokensUsed())
	}
}

func TestLoopFailTwiceStopsAtCap(t *testing.T) {
	exp := &scriptedExplainer{body: reviewBody}
	rev := &scriptedReviewer{outcomes: []reviewOutcome{
		{v: models.ReviewVerdict{Pass: false, Score: 0.5, Iteration: 1, TokensUsed: 500, Feedback: map[string]string{"glossary": "add terms"}}},
		{v: models.ReviewVerdict{Pass: false, Score: 0.6, Iteration: 2, TokensUsed: 500}},
	}}
	fv := &fakeVault{path: "/vault/n.md"}
	o := reviewOrch(2, exp, rev, fv)
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageComplete })

	if exp.calls != 2 || rev.calls != 2 {
		t.Fatalf("fail twice: want 2 gen/2 review, got %d/%d", exp.calls, rev.calls)
	}
	// Note is still saved, flagged not-passed.
	if fv.lastVerdict == nil || fv.lastVerdict.Pass {
		t.Fatalf("max reached → failed verdict saved, got %+v", fv.lastVerdict)
	}
	if s.Snapshot().Stage != models.StageComplete {
		t.Fatalf("must complete (not fail) at cap, got %s", s.Snapshot().Stage)
	}
}

func TestLoopMaxOneNoRevision(t *testing.T) {
	exp := &scriptedExplainer{body: reviewBody}
	rev := &scriptedReviewer{outcomes: []reviewOutcome{
		{v: models.ReviewVerdict{Pass: false, Score: 0.5, Iteration: 1, TokensUsed: 500}},
	}}
	fv := &fakeVault{path: "/vault/n.md"}
	o := reviewOrch(1, exp, rev, fv)
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageComplete })

	// max=1: generate once, review once, never revise.
	if exp.calls != 1 || rev.calls != 1 {
		t.Fatalf("max=1: want 1 gen/1 review, got %d/%d", exp.calls, rev.calls)
	}
	if fv.lastVerdict == nil || fv.lastVerdict.Pass {
		t.Fatalf("max=1 fail → failed verdict saved, got %+v", fv.lastVerdict)
	}
}

func TestLoopParseErrorStopsAndSaves(t *testing.T) {
	exp := &scriptedExplainer{body: reviewBody}
	// The real reviewer returns the tokens the (successful) call consumed alongside
	// the parse sentinel; mirror that here so token accounting can be asserted.
	rev := &scriptedReviewer{outcomes: []reviewOutcome{
		{v: models.ReviewVerdict{PaperID: "a", Pass: false, Score: 0, Iteration: 1, TokensUsed: 500}, err: fmt.Errorf("%w: bad token", agents.ErrReviewParse)},
	}}
	fv := &fakeVault{path: "/vault/n.md"}
	o := reviewOrch(2, exp, rev, fv)
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageComplete })

	// Parse error stops the loop after one generate + one review; note is saved.
	if exp.calls != 1 || rev.calls != 1 {
		t.Fatalf("parse error: want 1 gen/1 review, got %d/%d", exp.calls, rev.calls)
	}
	if fv.lastVerdict == nil || fv.lastVerdict.Pass || fv.lastVerdict.Score != 0 {
		t.Fatalf("parse error → {Pass:false,Score:0} verdict saved, got %+v", fv.lastVerdict)
	}
	// Tokens consumed by the failed-parse review must still be counted: 150 + 500.
	if s.TokensUsed() != 650 {
		t.Fatalf("parse-error token accounting = %d, want 650 (150 gen + 500 review)", s.TokensUsed())
	}
}

func TestLoopReviewerLLMErrorFailsSession(t *testing.T) {
	exp := &scriptedExplainer{body: reviewBody}
	rev := &scriptedReviewer{outcomes: []reviewOutcome{
		{err: llm.ErrLLMUnavailable},
	}}
	fv := &fakeVault{path: "/vault/n.md"}
	o := reviewOrch(2, exp, rev, fv)
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageFailed })

	snap := s.Snapshot()
	if !snap.Recoverable || snap.Error == "" {
		t.Fatalf("reviewer LLM error → recoverable failure with message, got %#v", snap)
	}
	// A reviewer failure must not write the note.
	if atomic.LoadInt32(&fv.written) != 0 {
		t.Fatalf("vault must not be written on reviewer failure, got %d writes", atomic.LoadInt32(&fv.written))
	}
}

func TestLoopStagesEmittedInOrder(t *testing.T) {
	var stages []models.PipelineStage
	exp := &scriptedExplainer{body: reviewBody, stages: &stages}
	rev := &scriptedReviewer{
		outcomes: []reviewOutcome{
			{v: models.ReviewVerdict{Pass: false, Score: 0.5, Iteration: 1, TokensUsed: 500, Feedback: map[string]string{"core_idea": "fix"}}},
			{v: models.ReviewVerdict{Pass: true, Score: 0.9, Iteration: 2, TokensUsed: 500}},
		},
		stages: &stages,
	}
	fv := &fakeVault{path: "/vault/n.md"}
	o := reviewOrch(2, exp, rev, fv)
	s := selectionSession(o, makePapers(3))
	exp.sess, rev.sess = s, s // wire the session for stage capture before the run

	process(o, s.SessionID, "a")
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageComplete })

	// Interleaved gen/review capture order: generating → reviewing → revising → reviewing.
	want := []models.PipelineStage{
		models.StageGenerating, models.StageReviewing,
		models.StageRevising, models.StageReviewing,
	}
	if len(stages) != len(want) {
		t.Fatalf("stage sequence = %v, want %v", stages, want)
	}
	for i := range want {
		if stages[i] != want[i] {
			t.Fatalf("stage[%d] = %s, want %s (full: %v)", i, stages[i], want[i], stages)
		}
	}
}

// --- Phase 6: resume after a review-call failure ---

// errorReviewer errors on its first call, then returns a passing verdict, so a
// test can drive a transient review failure followed by a successful retry.
type errorReviewer struct {
	calls int
}

func (r *errorReviewer) Review(context.Context, models.ExplainerOutput, models.Paper, int) (models.ReviewVerdict, error) {
	r.calls++
	if r.calls == 1 {
		return models.ReviewVerdict{}, llm.ErrLLMTimeout // transient → recoverable
	}
	return models.ReviewVerdict{Pass: true, Score: 0.9, Iteration: 1, TokensUsed: 500, InputTokens: 400, OutputTokens: 100}, nil
}

// With the reviewer enabled, a review-call failure leaves verdict==nil after a
// successful generation. On retry, the WHOLE loop must re-run (so the note is
// actually reviewed) rather than skipping to a vault write that would mislabel
// an unreviewed note as review_passed:true.
func TestRetryReviewFailureReRunsLoop(t *testing.T) {
	exp := &scriptedExplainer{body: reviewBody}
	rev := &errorReviewer{}
	fv := &fakeVault{path: "/vault/AI Papers/n.md"}
	o := reviewOrch(2, exp, rev, fv)
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageFailed })

	// After the failure: generation ran once, explainer cached, verdict still nil.
	if exp.calls != 1 {
		t.Fatalf("generation calls before retry = %d, want 1", exp.calls)
	}
	if s.Verdict() != nil {
		t.Fatal("verdict must be nil after a review-call failure")
	}
	if !s.Snapshot().Recoverable {
		t.Fatal("transient review failure must be recoverable")
	}

	// Retry: the loop must re-run (regenerate + review), producing a real verdict.
	if rec := retry(o, s.SessionID); rec.Code != http.StatusOK {
		t.Fatalf("retry: want 200, got %d", rec.Code)
	}
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageComplete })

	if exp.calls != 2 {
		t.Fatalf("generation calls after retry = %d, want 2 (loop re-ran)", exp.calls)
	}
	v := s.Verdict()
	if v == nil || !v.Pass {
		t.Fatalf("expected a passing verdict after retry, got %+v", v)
	}
	// The vault write must carry the real (passing) verdict, not a nil that
	// buildFrontmatter would render as review_passed:true for a disabled reviewer.
	if fv.lastVerdict == nil || !fv.lastVerdict.Pass {
		t.Fatalf("vault write verdict = %+v, want a real passing verdict", fv.lastVerdict)
	}
}

// A second retry issued after the first has already transitioned the session out
// of StageFailed must be rejected (400) — the atomic BeginRetry closes the
// double-spawn window. Sequential calls make this deterministic: the first retry
// synchronously leaves StageFailed before returning.
func TestRetrySecondIsRejected(t *testing.T) {
	content := &fakeContent{md: "# extracted"}
	o := newProcessOrch(content)
	fv := &toggleVault{path: "/vault/AI Papers/n.md"} // fails once, then succeeds
	o.vault = fv
	s := selectionSession(o, makePapers(3))

	process(o, s.SessionID, "a")
	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageFailed })

	if rec := retry(o, s.SessionID); rec.Code != http.StatusOK {
		t.Fatalf("first retry: want 200, got %d", rec.Code)
	}
	// The first retry has already left StageFailed synchronously, so a second one
	// is not retryable.
	if rec := retry(o, s.SessionID); rec.Code != http.StatusBadRequest {
		t.Fatalf("second retry: want 400, got %d", rec.Code)
	}

	waitFor(t, func() bool { return s.Snapshot().Stage == models.StageComplete })
	// Exactly one fail + one success write — no double-spawn from the 2nd retry.
	if atomic.LoadInt32(&fv.written) != 2 {
		t.Fatalf("vault writes = %d, want 2 (one fail + one success)", fv.written)
	}
}
