package orchestrator

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/models"
	"github.com/maritime-ds/arxiv-reader/internal/store"
	"github.com/maritime-ds/arxiv-reader/internal/tracing"
)

// This file holds the frontend-facing HTTP contract (request/response DTOs) and
// the shared JSON writer, kept separate from the handler logic in orchestrator.go.

type DiscoverResponse struct {
	SessionID string `json:"session_id"`
}

type StatusResponse struct {
	Stage       models.PipelineStage `json:"stage"`
	Candidates  []models.Paper       `json:"candidates,omitempty"`
	Notice      string               `json:"notice,omitempty"`
	Error       string               `json:"error,omitempty"`
	Recoverable bool                 `json:"recoverable,omitempty"`
	// Phase 5 review progress. omitempty keeps pre-review stages clean (no
	// iteration:0 noise); the definitive pass/fail is read from /result + the
	// vault note, not this poll.
	Iteration    int     `json:"iteration,omitempty"`
	ReviewScore  float32 `json:"reviewScore,omitempty"`
	ReviewPassed bool    `json:"reviewPassed,omitempty"`
	// Phase 6 additive fields (all omitempty → no contract break). ErrorAction is
	// the failure UI hint; ArxivRetryCount drives the discovery retry label;
	// ContextWarning is the non-blocking over-limit advisory (nil when absent).
	ErrorAction     string                 `json:"errorAction,omitempty"`
	ArxivRetryCount int                    `json:"arxivRetryCount,omitempty"`
	ContextWarning  *models.ContextWarning `json:"contextWarning,omitempty"`
}

type ProcessRequest struct {
	SessionID string `json:"session_id"`
	PaperID   string `json:"paper_id"`
}

type ProcessResponse struct {
	SessionID string `json:"session_id"`
}

// RetryResponse echoes the session id after a successful retry so the frontend
// can keep polling the SAME session (retry resumes in place — no new session).
type RetryResponse struct {
	SessionID string `json:"session_id"`
}

// ResultResponse is the completed explainer, served by /result once the pipeline
// reaches complete. Content is the note body only (no frontmatter — that is added
// at vault-write time and is not part of ExplainerOutput.Content).
type ResultResponse struct {
	Content    string `json:"content"`
	VaultFile  string `json:"vaultFile"`
	TokensUsed int    `json:"tokensUsed"`
	// Phase 6 cost breakdown (populated in Phase 03's HandleResult). Split token
	// counts + an estimated USD cost. CostKnown is false when the model is absent
	// from the pricing table → the UI hides the cost figure. omitempty keeps the
	// pre-Phase-03 shape unchanged.
	InputTokens      int     `json:"inputTokens,omitempty"`
	OutputTokens     int     `json:"outputTokens,omitempty"`
	EstimatedCostUSD float64 `json:"estimatedCostUSD,omitempty"`
	CostKnown        bool    `json:"costKnown,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// --- Phase 7 run-timeline DTOs (mirrored in frontend/lib/types.ts) ---
//
// EventDTO is the SINGLE wire contract for a timeline event, shared by the live
// SSE stream (mapped from tracing.Event) and the persisted history reads (mapped
// from store.EventRecord) — so the browser sees one shape either way.
type EventDTO struct {
	Seq         int             `json:"seq"`
	Type        string          `json:"type"` // event_type / kind
	Stage       string          `json:"stage"`
	Title       string          `json:"title"`
	Status      string          `json:"status"`
	Summary     json.RawMessage `json:"summary,omitempty"`
	PayloadFull json.RawMessage `json:"payloadFull,omitempty"`
	DurationMS  *int            `json:"durationMs,omitempty"`
	CreatedAt   time.Time       `json:"createdAt"`
}

// RunDTO is one run's durable header for the history list + reopen views.
type RunDTO struct {
	ID           string     `json:"id"`
	PaperID      string     `json:"paperId,omitempty"`
	PaperTitle   string     `json:"paperTitle,omitempty"`
	Stage        string     `json:"stage"`
	Status       string     `json:"status"`
	InputTokens  int        `json:"inputTokens"`
	OutputTokens int        `json:"outputTokens"`
	EstCostUSD   *float64   `json:"estCostUsd,omitempty"`
	ReviewPassed *bool      `json:"reviewPassed,omitempty"`
	StartedAt    time.Time  `json:"startedAt"`
	CompletedAt  *time.Time `json:"completedAt,omitempty"`
}

// RunsListResponse is the paginated history list (newest first).
type RunsListResponse struct {
	Runs  []RunDTO `json:"runs"`
	Total int      `json:"total"`
}

// RunDetailResponse reopens a single past run: its header + full timeline.
type RunDetailResponse struct {
	Run    RunDTO     `json:"run"`
	Events []EventDTO `json:"events"`
}

// eventDTOFromEvent maps a live tracing.Event to the wire shape, marshalling the
// (already-scrubbed) summary/payload maps to JSON.
func eventDTOFromEvent(e tracing.Event) EventDTO {
	return EventDTO{
		Seq: e.Seq, Type: string(e.Kind), Stage: e.Stage, Title: e.Title, Status: string(e.Status),
		Summary: marshalMap(e.Summary), PayloadFull: marshalMap(e.PayloadFull),
		DurationMS: e.DurationMS, CreatedAt: e.CreatedAt,
	}
}

// eventDTOFromRecord maps a persisted store.EventRecord to the same wire shape;
// its JSONB columns are already raw JSON.
func eventDTOFromRecord(e store.EventRecord) EventDTO {
	return EventDTO{
		Seq: e.Seq, Type: e.EventType, Stage: e.Stage, Title: e.Title, Status: e.Status,
		Summary: e.Summary, PayloadFull: e.PayloadFull,
		DurationMS: e.DurationMS, CreatedAt: e.CreatedAt,
	}
}

// runDTOFromRecord maps a persisted run header to the wire shape, dereferencing
// nullable columns to their zero/omitted form.
func runDTOFromRecord(r store.RunRecord) RunDTO {
	dto := RunDTO{
		ID: r.ID, Stage: r.Stage, Status: r.Status,
		InputTokens: r.InputTokens, OutputTokens: r.OutputTokens,
		EstCostUSD: r.EstCostUSD, ReviewPassed: r.ReviewPassed,
		StartedAt: r.StartedAt, CompletedAt: r.CompletedAt,
	}
	if r.PaperID != nil {
		dto.PaperID = *r.PaperID
	}
	if r.PaperTitle != nil {
		dto.PaperTitle = *r.PaperTitle
	}
	return dto
}

// marshalMap encodes a summary/payload map to raw JSON, or nil (omitted) when
// empty or on a marshal error — the transport never fails on a bad summary.
func marshalMap(m map[string]any) json.RawMessage {
	if len(m) == 0 {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	return b
}
