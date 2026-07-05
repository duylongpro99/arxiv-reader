package orchestrator

import (
	"encoding/json"
	"net/http"

	"github.com/maritime-ds/arxiv-reader/internal/models"
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
