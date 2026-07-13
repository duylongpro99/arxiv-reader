package models

// LLMTrace is the captured prompt/response for one agent LLM call (explainer
// generation or reviewer evaluation). DocumentText (the full paper Markdown)
// is deliberately OMITTED: it is identical across every pass of a run and
// would bloat the run_events.payload_full JSONB column for no benefit — see
// agents.Generate / agents.Review, which populate this from the
// llm.CompletionRequest fields (never from DocumentText).
type LLMTrace struct {
	SystemPrompt string `json:"systemPrompt"`
	UserPrompt   string `json:"userPrompt"`
	RawResponse  string `json:"rawResponse"`
}
