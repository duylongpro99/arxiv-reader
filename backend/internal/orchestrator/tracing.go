package orchestrator

import (
	"fmt"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/llm"
	"github.com/maritime-ds/arxiv-reader/internal/models"
	"github.com/maritime-ds/arxiv-reader/internal/tracing"
)

// This file holds the orchestrator's tracing seam: recorder lookup, a terse
// event builder, and run finalization. Emit CALL SITES live inline in
// orchestrator.go / orchestrator-pipeline.go — right where the code already
// logs the corresponding story beat. Every helper is nil-safe (a nil recorder,
// from disabled tracing or a raw-struct test orchestrator, no-ops), so tracing
// never adds an error path to the pipeline.

// rec returns the recorder for a session, creating it (and its run-header row)
// on first use. Returns nil when tracing is disabled or the orchestrator has no
// tracer (raw-struct unit tests) — callers rely on Recorder methods being nil-safe.
func (o *Orchestrator) rec(s *models.PipelineSession) *tracing.Recorder {
	if o.tracer == nil {
		return nil
	}
	if r := o.tracer.Recorder(s.SessionID); r != nil {
		return r
	}
	return o.tracer.NewRecorder(s.SessionID, s.StartedAt())
}

// tev builds a base event; callers set Summary/DurationMS before Emit as needed.
// Stage is stringified from the existing PipelineStage so tracing stays free of
// a models import.
func tev(kind tracing.EventKind, status tracing.Status, stage models.PipelineStage, title string) tracing.Event {
	return tracing.Event{Kind: kind, Status: status, Stage: string(stage), Title: title}
}

// withSummary attaches a summary map to an event in a single expression (keeps
// one-line emit call sites terse).
func withSummary(e tracing.Event, m map[string]any) tracing.Event {
	e.Summary = m
	return e
}

// preview returns the first n runes of s (rune-safe). A defence-in-depth cap
// lives in the scrubber too, but keeping previews small at the source avoids
// shipping large strings through the buffer/stream at all.
func preview(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// byteSize formats a byte count as a compact human string (e.g. "48KB") for
// event titles. Not exact SI — readability over precision.
func byteSize(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1fMB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%dKB", n/(1<<10))
	default:
		return fmt.Sprintf("%dB", n)
	}
}

// runCompletedTitle renders the run's closing one-liner, e.g.
// "Done · 30.4K tokens · ~$0.11 · 1m47s". Cost is omitted when the model's
// pricing is unknown (never show a guessed figure — mirrors the /result UI).
func runCompletedTitle(tokens int, cost float64, costKnown bool, d time.Duration) string {
	costPart := ""
	if costKnown {
		costPart = fmt.Sprintf(" · ~$%.2f", cost)
	}
	return fmt.Sprintf("Done · %s tokens%s · %s", compactCount(tokens), costPart, d.Round(time.Second))
}

// compactCount renders large counts compactly (e.g. 30400 → "30.4K").
func compactCount(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

// finalizeRun writes the terminal run-header (tokens, cost, review outcome) and,
// when close is true, closes the recorder (ending live streaming). close is
// false for a RECOVERABLE failure: a retry resumes the same run, so the recorder
// must stay open to keep emitting. Best-effort; nil-safe.
func (o *Orchestrator) finalizeRun(s *models.PipelineSession, status string, closeRecorder bool) {
	r := o.rec(s)
	if r == nil {
		return
	}
	in, out := s.InputTokens(), s.OutputTokens()
	var costPtr *float64
	if cost, known := llm.EstimateCost(o.cfg.LLM.Model, in, out); known {
		costPtr = &cost
	}
	var reviewPassed *bool
	if v := s.Verdict(); v != nil {
		rp := v.Pass
		reviewPassed = &rp
	}
	r.Finalize(tracing.Final{
		Stage:        string(s.Snapshot().Stage),
		Status:       status,
		InputTokens:  in,
		OutputTokens: out,
		EstCostUSD:   costPtr,
		ReviewPassed: reviewPassed,
		CompletedAt:  time.Now(),
	})
	if closeRecorder {
		r.Close()
	}
}
