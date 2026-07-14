package orchestrator

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/models"
	"github.com/maritime-ds/arxiv-reader/internal/store"
	"github.com/maritime-ds/arxiv-reader/internal/tools"
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

// RunContentDTO serves the persisted Obsidian note for a past run (Feature B:
// GET /runs/{id}/content). Available=false (Path/Markdown omitted, or Path set
// but Markdown empty) means there is nothing to show — the run never reached
// the vault-write stage, or the note file no longer exists on disk. Both are
// normal, non-error states (HTTP 200), never a 500.
type RunContentDTO struct {
	Path      string `json:"path,omitempty"`
	Available bool   `json:"available"`
	Markdown  string `json:"markdown,omitempty"`
}

// DiscoverMoreDTO is the appended page of arXiv candidates returned by
// POST /discover/{sessionId}/more (Feature C: pagination via session
// extension). Candidates reuses models.Paper — the same shape
// StatusResponse.Candidates already uses — so the frontend's existing
// candidate rendering works unchanged. HasMore is a heuristic: true when the
// raw arXiv page (before dedup filtering) was full-sized.
type DiscoverMoreDTO struct {
	Candidates []models.Paper `json:"candidates"`
	HasMore    bool           `json:"hasMore"`
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

// --- Phase 10 channel-publishing DTOs (mirrored in frontend/lib/types.ts) ---

// ChannelDTO is one enabled, resolvable channel returned by GET /channels.
type ChannelDTO struct {
	ID       string `json:"id"`
	Category string `json:"category"`
}

// ChannelsResponse lists every enabled channel that resolved successfully.
// Channels that fail to construct (e.g. missing API key) are silently omitted
// rather than 500ing the whole list — see HandleChannels.
type ChannelsResponse struct {
	Channels []ChannelDTO `json:"channels"`
}

// CreatePublicationsRequest is the body of POST /runs/{id}/publications: the
// set of channel ids to draft content for.
type CreatePublicationsRequest struct {
	Channels []string `json:"channels"`
}

// PatchPublicationRequest is the body of PATCH /publications/{pid}. All
// fields are optional pointers so a partial edit (e.g. only Approve) leaves
// the other fields untouched by the caller's intent — HandlePatchPublication
// still resolves a full row before writing.
type PatchPublicationRequest struct {
	Title   *string `json:"title,omitempty"`
	Content *string `json:"content,omitempty"`
	Approve *bool   `json:"approve,omitempty"`
}

// PublicationDTO is one (run, channel) publish draft/attempt — the wire shape
// for a store.PublicationRecord with nullable columns dereferenced to their
// omitted/zero form (mirrors runDTOFromRecord's pointer-deref convention).
type PublicationDTO struct {
	ID          string     `json:"id"`
	RunID       string     `json:"runId"`
	ChannelID   string     `json:"channelId"`
	Category    string     `json:"category"`
	Status      string     `json:"status"`
	Title       string     `json:"title,omitempty"`
	Content     string     `json:"content,omitempty"`
	ExternalURL string     `json:"externalUrl,omitempty"`
	ExternalID  string     `json:"externalId,omitempty"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	PublishedAt *time.Time `json:"publishedAt,omitempty"`
}

// PublicationsResponse lists drafts for a run (GET/POST /runs/{id}/publications).
// SkippedChannels is additive (omitempty — never breaks a client parsing only
// Publications): it surfaces channel ids from a create-request that failed to
// resolve (e.g. a channel not yet implemented, or misconfigured), each paired
// with the reason, so the UI can show why fewer drafts came back than requested.
type PublicationsResponse struct {
	Publications    []PublicationDTO `json:"publications"`
	SkippedChannels []string         `json:"skippedChannels,omitempty"`
}

// publicationDTOFromRecord maps a persisted publication row to the wire shape.
func publicationDTOFromRecord(p store.PublicationRecord) PublicationDTO {
	dto := PublicationDTO{
		ID: p.ID, RunID: p.RunID, ChannelID: p.ChannelID,
		Category: p.Category, Status: p.Status,
		Content: p.AdaptedContent, CreatedAt: p.CreatedAt, PublishedAt: p.PublishedAt,
	}
	if p.Title != nil {
		dto.Title = *p.Title
	}
	if p.ExternalURL != nil {
		dto.ExternalURL = *p.ExternalURL
	}
	if p.ExternalID != nil {
		dto.ExternalID = *p.ExternalID
	}
	if p.Error != nil {
		dto.Error = *p.Error
	}
	return dto
}

// derefOr returns the dereferenced string, or "" for a nil pointer — used to
// flatten the store's nullable columns onto plain domain values.
func derefOr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// httpError pairs a status with a client-safe message, used by readRunMarkdown
// so its caller can writeJSON without a type switch on sentinel errors.
type httpError struct {
	status  int
	message string
}

func (e *httpError) Error() string { return e.message }

// readRunMarkdown reuses HandleRunContent's exact lookup path (timeline scan →
// vaultPathFromEvents → ValidateWithinVault → read) to fetch the Obsidian note
// a run produced, so publishing can never diverge from what /content serves.
// A missing event or missing file is a 422 (nothing to publish yet), never a
// 500 — this mirrors HandleRunContent treating both as normal, non-error states.
func (o *Orchestrator) readRunMarkdown(ctx context.Context, runID string) (string, *httpError) {
	events, err := o.publications.ListEvents(ctx, runID, -1)
	if err != nil {
		return "", &httpError{http.StatusInternalServerError, "cannot read run"}
	}
	path, ok := vaultPathFromEvents(events)
	if !ok {
		return "", &httpError{http.StatusUnprocessableEntity, "run has no generated note to publish"}
	}
	// Path-traversal guard: never read outside the configured Obsidian vault —
	// see HandleRunContent's identical check for the full rationale.
	if verr := tools.ValidateWithinVault(o.cfg.Paths.ObsidianVault, path); verr != nil {
		return "", &httpError{http.StatusBadRequest, "invalid vault path"}
	}
	content, rerr := os.ReadFile(path)
	if rerr != nil {
		if os.IsNotExist(rerr) {
			return "", &httpError{http.StatusUnprocessableEntity, "run has no generated note to publish"}
		}
		slog.Error("publish content read failed", "run_id", runID)
		return "", &httpError{http.StatusInternalServerError, "cannot read note"}
	}
	return string(content), nil
}

// secretLike matches common credential shapes in a channel error message
// (e.g. a dev.to/X API key or bearer token echoed by a client library's error
// string). This is a defence-in-depth backstop, deliberately separate from
// tracing's internal scrubber (which is keyed to the LLM API key literal and
// unexported): channel errors originate from third-party HTTP clients whose
// error text is never trusted verbatim before it reaches a response body or
// the durable `publications.error` column.
var secretLike = regexp.MustCompile(`(?i)(bearer\s+[A-Za-z0-9._\-]{16,}|sk-[A-Za-z0-9_\-]{16,}|(api[_-]?key|token|secret)\s*[:=]\s*\S+)`)

// scrubErr redacts anything secret-shaped from a channel error before it is
// stored (MarkFailed) or returned to the client — publish failures must be
// visible and actionable, but never leak a credential.
func scrubErr(msg string) string {
	return secretLike.ReplaceAllString(msg, "[REDACTED]")
}
