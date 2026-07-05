package llm

import "testing"

func TestEstimateTokensHeuristic(t *testing.T) {
	// ~4 chars per token: an 8-char string → 2 tokens.
	if got := EstimateTokens("abcdefgh"); got != 2 {
		t.Fatalf("EstimateTokens(8 chars) = %d, want 2", got)
	}
	if got := EstimateTokens(""); got != 0 {
		t.Fatalf("EstimateTokens(empty) = %d, want 0", got)
	}
}

func TestModelContextLimitsHasDefaultModel(t *testing.T) {
	// The default config model must have a known limit so the pre-check runs.
	if _, ok := ModelContextLimits["claude-sonnet-4-6"]; !ok {
		t.Fatal("default model missing from ModelContextLimits")
	}
}
