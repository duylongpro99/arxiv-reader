package llm

// limits.go holds the model context-window sizes used by the Phase 6 pre-check.
// Like pricing.go these are APPROXIMATE and dated (captured 2026-07); the check
// is advisory only — it never blocks the pipeline. A genuine over-limit request
// is still caught later by the provider returning ErrLLMBadRequest.

// ModelContextLimits maps a model string to its total context window in tokens
// (input + output). A model absent here means "unknown limit" → skip the check.
var ModelContextLimits = map[string]int{
	// Anthropic — 200K context.
	"claude-sonnet-4-6": 200_000,
	"claude-opus-4-8":   200_000,
	"claude-haiku-4-5":  200_000,
	// OpenAI — 128K context.
	"gpt-4o":      128_000,
	"gpt-4o-mini": 128_000,
	// Google Gemini — very large context windows.
	"gemini-2.0-flash": 1_000_000,
	"gemini-1.5-pro":   2_000_000,
}

// EstimateTokens approximates the token count of English Markdown prose using
// the well-known ~4-characters-per-token heuristic. The input is TEXT (the
// extracted Markdown), never a PDF or image. Deliberately conservative and
// cheap — it only has to be good enough to warn before a likely-too-large call.
func EstimateTokens(markdown string) int {
	return len(markdown) / 4
}
