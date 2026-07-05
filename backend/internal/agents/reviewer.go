package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/maritime-ds/arxiv-reader/internal/llm"
	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// ErrReviewParse is the sentinel returned when the reviewer's response is not
// valid JSON. It is distinguishable (via errors.Is) from a genuine LLM/network
// error so the orchestrator can apply the "stop the loop, save with pass:false"
// design decision rather than failing the whole session.
var ErrReviewParse = errors.New("reviewer response is not valid JSON")

// ReviewerAgent is the independent critic in the Phase 5 critic-generator loop.
// It scores an already-generated explainer against a fixed 6-criteria rubric and
// returns structured, section-level feedback. It shares the same llm.LLMClient as
// the ExplainerAgent (an accepted tradeoff — different system prompt + very low
// temperature give a meaningfully different, adversarial perspective).
type ReviewerAgent struct {
	llm llm.LLMClient
	cfg *config.Config
}

// NewReviewer builds a ReviewerAgent over the shared LLM client. The distinct
// constructor name avoids colliding with New (ExplainerAgent) in this package.
func NewReviewer(client llm.LLMClient, cfg *config.Config) *ReviewerAgent {
	return &ReviewerAgent{llm: client, cfg: cfg}
}

// Review evaluates the explainer text alone (T3: the source paper is NOT sent —
// DocumentText is empty — to keep reviewer cost low). It returns a ReviewVerdict
// whose Pass is the model's judgement verbatim (score never gates — design
// decision 1). A malformed JSON response yields an error wrapping ErrReviewParse;
// a real LLM/network error is returned as-is (not the sentinel).
func (a *ReviewerAgent) Review(ctx context.Context, ex models.ExplainerOutput, paper models.Paper, iteration int) (models.ReviewVerdict, error) {
	start := time.Now()
	req := llm.CompletionRequest{
		SystemPrompt: reviewerSystemPrompt,
		UserPrompt:   a.buildReviewPrompt(ex, paper, iteration),
		// DocumentText intentionally empty: evaluate ex.Content only (T3).
		MaxTokens:   2000, // structured JSON verdict, not long prose
		Temperature: 0.1,  // low → consistent, repeatable evaluation
	}

	resp, err := a.llm.Complete(ctx, req)
	if err != nil {
		// Genuine LLM/network error — return unchanged (NOT the parse sentinel).
		return models.ReviewVerdict{}, err
	}

	// Strip any markdown fences the LLM may wrap the JSON in.
	jsonStr := stripJSONFences(resp.Content)

	// Feedback values are *string so a JSON null distinguishes "no issue" from an
	// empty string; both are filtered out below.
	var raw struct {
		Pass     bool               `json:"pass"`
		Score    float32            `json:"score"`
		Feedback map[string]*string `json:"feedback"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		// Decision 2: a malformed response is a "not passed" verdict, not a session
		// failure. The LLM call itself SUCCEEDED and consumed tokens, so carry them
		// (plus the fail metadata) on the returned verdict even alongside the
		// sentinel error — the orchestrator accounts for them before it stops.
		v := models.ReviewVerdict{
			PaperID:      paper.ID,
			Pass:         false,
			Score:        0,
			Iteration:    iteration,
			TokensUsed:   resp.InputTokens + resp.OutputTokens,
			InputTokens:  resp.InputTokens,
			OutputTokens: resp.OutputTokens,
			CreatedAt:    time.Now().UTC(),
		}
		return v, fmt.Errorf("%w: %v", ErrReviewParse, err)
	}

	// Drop null/empty feedback so only actionable notes reach the revision prompt.
	feedback := make(map[string]string)
	for section, note := range raw.Feedback {
		if note != nil && strings.TrimSpace(*note) != "" {
			feedback[section] = *note
		}
	}

	verdict := models.ReviewVerdict{
		PaperID:      paper.ID,
		Pass:         raw.Pass, // verbatim — score is advisory, never gates (decision 1)
		Score:        raw.Score,
		Feedback:     feedback,
		Iteration:    iteration,
		TokensUsed:   resp.InputTokens + resp.OutputTokens,
		InputTokens:  resp.InputTokens,
		OutputTokens: resp.OutputTokens,
		CreatedAt:    time.Now().UTC(),
	}

	slog.Info("review complete",
		"paper_id", paper.ID,
		"iteration", iteration,
		"score", verdict.Score,
		"pass", verdict.Pass,
		"feedback_sections", len(feedback),
		"input_tokens", verdict.InputTokens,
		"output_tokens", verdict.OutputTokens,
		"tokens_used", verdict.TokensUsed,
		"duration_ms", time.Since(start).Milliseconds(),
	)
	return verdict, nil
}

// stripJSONFences trims surrounding whitespace and a leading ```json / ``` fence
// and trailing ``` fence that models sometimes add around JSON output.
func stripJSONFences(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
