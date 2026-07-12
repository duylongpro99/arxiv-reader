// Package tracing is the run-timeline seam: the orchestrator emits Events
// through a Recorder that dual-writes to a per-run in-memory ring buffer (→ SSE
// live) and, best-effort, to Postgres (→ durable history). It is ADDITIVE and
// never fatal to the paper pipeline — a nil Recorder or a down DB simply means
// less tracing, never a failed run.
package tracing

import "time"

// Status drives the timeline row's icon/colour in the UI.
type Status string

const (
	StatusInfo    Status = "info"
	StatusSuccess Status = "success"
	StatusWarning Status = "warning"
	StatusError   Status = "error"
)

// EventKind is the stable event taxonomy (design §4). The orchestrator is the
// single emission site; each constant marks one story beat of a run.
type EventKind string

const (
	KindDiscoveryStarted          EventKind = "discovery.started"
	KindToolDiscoveryCompleted    EventKind = "tool.discovery.completed"
	KindToolLogcheckCompleted     EventKind = "tool.logcheck.completed"
	KindSelectionPresented        EventKind = "selection.presented"
	KindSelectionChosen           EventKind = "selection.chosen"
	KindToolPaperContentStarted   EventKind = "tool.papercontent.started"
	KindToolPaperContentCompleted EventKind = "tool.papercontent.completed"
	KindToolPaperContentFailed    EventKind = "tool.papercontent.failed"
	KindContextWarning            EventKind = "context.warning"
	KindLLMExplainerStarted       EventKind = "llm.explainer.started"
	KindLLMExplainerCompleted     EventKind = "llm.explainer.completed"
	KindLLMReviewerStarted        EventKind = "llm.reviewer.started"
	KindLLMReviewerCompleted      EventKind = "llm.reviewer.completed"
	KindDecisionRevise            EventKind = "decision.revise"
	KindDecisionAccept            EventKind = "decision.accept"
	KindDecisionMaxIterations     EventKind = "decision.max_iterations"
	KindToolVaultWriterCompleted  EventKind = "tool.vaultwriter.completed"
	KindRunCompleted              EventKind = "run.completed"
	KindRunFailed                 EventKind = "run.failed"
	KindRunRecoveredToSelection   EventKind = "run.recovered_to_selection"
)

// IsTerminal reports whether this kind ends the run's SSE stream. A recovery
// (404 re-pick) is deliberately NOT terminal — the run continues after it.
func (k EventKind) IsTerminal() bool {
	return k == KindRunCompleted || k == KindRunFailed
}

// Event is one timeline entry. Seq/CreatedAt/RunID are stamped by the Recorder
// on Emit; callers fill Kind/Stage/Title/Status and the optional Summary/
// PayloadFull/DurationMS. Summary holds small structured fields (counts, tokens,
// ~500-char previews); PayloadFull holds full prompts/responses and is kept only
// when full-payload capture is opted in. Both are scrubbed at Emit time.
type Event struct {
	RunID       string
	Seq         int
	Kind        EventKind
	Stage       string // string form of models.PipelineStage (no import coupling)
	Title       string
	Status      Status
	Summary     map[string]any
	PayloadFull map[string]any
	DurationMS  *int
	CreatedAt   time.Time
}

// IsTerminal is a convenience passthrough to the kind.
func (e Event) IsTerminal() bool { return e.Kind.IsTerminal() }

// MS converts a duration to the *int milliseconds the Event/DB carry. Exported
// so emit sites (in the orchestrator package) get nil-free ergonomics:
// evt.DurationMS = tracing.MS(time.Since(start)).
func MS(d time.Duration) *int {
	ms := int(d.Milliseconds())
	return &ms
}
