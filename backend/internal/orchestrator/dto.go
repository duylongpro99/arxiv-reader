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
}

type ProcessRequest struct {
	SessionID string `json:"session_id"`
	PaperID   string `json:"paper_id"`
}

type ProcessResponse struct {
	SessionID string `json:"session_id"`
}

// ResultResponse is the completed explainer, served by /result once the pipeline
// reaches complete. Content is the note body only (no frontmatter — that is added
// at vault-write time and is not part of ExplainerOutput.Content).
type ResultResponse struct {
	Content    string `json:"content"`
	VaultFile  string `json:"vaultFile"`
	TokensUsed int    `json:"tokensUsed"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
