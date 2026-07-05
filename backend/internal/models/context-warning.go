package models

// ContextWarning is a non-blocking advisory attached to a session when the
// estimated prompt size (extracted Markdown + system prompt + output budget)
// exceeds the selected model's context window. It is surfaced via
// StatusResponse.ContextWarning so the UI can warn the user WITHOUT aborting the
// pipeline — the estimate (len/4 heuristic) is advisory only; a genuine
// over-limit request is still caught later by the provider's ErrLLMBadRequest.
type ContextWarning struct {
	EstimatedTokens int    `json:"estimatedTokens"`
	ModelLimit      int    `json:"modelLimit"`
	Model           string `json:"model"`
	Suggestion      string `json:"suggestion"`
}
