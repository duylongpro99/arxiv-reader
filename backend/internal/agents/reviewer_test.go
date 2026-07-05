package agents

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/maritime-ds/arxiv-reader/internal/llm"
	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// reviewerCfg mirrors testCfg — the reviewer hardcodes MaxTokens/Temperature, so
// only a non-nil *config.Config is needed.
func sampleReview() (models.ExplainerOutput, models.Paper) {
	return models.ExplainerOutput{PaperID: "1706.03762", Content: fullNote},
		models.Paper{ID: "1706.03762", Title: "Attention Is All You Need"}
}

func TestReviewValidUnfencedJSON(t *testing.T) {
	body := `{"pass": true, "score": 0.88, "feedback": {"glossary": "add contrastive loss"}}`
	f := &fakeLLM{resp: llm.CompletionResponse{Content: body, InputTokens: 5000, OutputTokens: 400}}
	a := NewReviewer(f, testCfg())
	ex, p := sampleReview()

	v, err := a.Review(context.Background(), ex, p, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !v.Pass || v.Score != 0.88 {
		t.Fatalf("verdict wrong: %+v", v)
	}
	if v.PaperID != "1706.03762" || v.Iteration != 1 {
		t.Fatalf("meta wrong: %+v", v)
	}
	if v.Feedback["glossary"] != "add contrastive loss" || len(v.Feedback) != 1 {
		t.Fatalf("feedback wrong: %+v", v.Feedback)
	}
	if v.TokensUsed != 5400 {
		t.Fatalf("tokens = %d, want 5400", v.TokensUsed)
	}
	if v.CreatedAt.IsZero() {
		t.Fatal("CreatedAt not set")
	}
	// Reviewer must evaluate the explainer text only — no source paper in DocumentText.
	if f.lastReq.DocumentText != "" {
		t.Fatalf("reviewer must not send DocumentText, got %q", f.lastReq.DocumentText)
	}
	if f.lastReq.MaxTokens != 2000 || f.lastReq.Temperature != 0.1 {
		t.Fatalf("reviewer knobs wrong: %+v", f.lastReq)
	}
	// The explainer content must ride in the user prompt.
	if !strings.Contains(f.lastReq.UserPrompt, "Attention Is All You Need") {
		t.Fatalf("paper title not in review prompt: %q", f.lastReq.UserPrompt)
	}
}

func TestReviewFencedJSONParses(t *testing.T) {
	body := "```json\n{\"pass\": false, \"score\": 0.4, \"feedback\": {}}\n```"
	f := &fakeLLM{resp: llm.CompletionResponse{Content: body, InputTokens: 10, OutputTokens: 5}}
	a := NewReviewer(f, testCfg())
	ex, p := sampleReview()

	v, err := a.Review(context.Background(), ex, p, 2)
	if err != nil {
		t.Fatalf("fenced JSON should parse: %v", err)
	}
	if v.Pass || v.Score != 0.4 || len(v.Feedback) != 0 {
		t.Fatalf("verdict wrong: %+v", v)
	}
}

func TestReviewNullFeedbackFiltered(t *testing.T) {
	// Two null entries, one empty string, one real note — only the real one survives.
	body := `{"pass": false, "score": 0.6, "feedback": {"core_idea": null, "glossary": "", "methodology": "clarify the training loop"}}`
	f := &fakeLLM{resp: llm.CompletionResponse{Content: body}}
	a := NewReviewer(f, testCfg())
	ex, p := sampleReview()

	v, err := a.Review(context.Background(), ex, p, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(v.Feedback) != 1 || v.Feedback["methodology"] != "clarify the training loop" {
		t.Fatalf("null/empty feedback not filtered: %+v", v.Feedback)
	}
}

func TestReviewMalformedJSONIsParseSentinel(t *testing.T) {
	for _, body := range []string{
		"Sure! Here is my review: {pass: true}", // preamble + invalid
		"not json at all",
		`{"pass": true, "score": }`, // truncated
	} {
		f := &fakeLLM{resp: llm.CompletionResponse{Content: body, InputTokens: 400, OutputTokens: 100}}
		a := NewReviewer(f, testCfg())
		ex, p := sampleReview()

		v, err := a.Review(context.Background(), ex, p, 3)
		if !errors.Is(err, ErrReviewParse) {
			t.Fatalf("body %q: want ErrReviewParse, got %v", body, err)
		}
		// Even on parse failure the verdict carries the consumed tokens + fail
		// metadata so the orchestrator's token accounting stays accurate.
		if v.TokensUsed != 500 || v.Pass || v.Score != 0 || v.Iteration != 3 {
			t.Fatalf("body %q: parse-failure verdict wrong: %+v", body, v)
		}
	}
}

func TestReviewLLMErrorIsNotParseSentinel(t *testing.T) {
	f := &fakeLLM{err: llm.ErrLLMUnavailable}
	a := NewReviewer(f, testCfg())
	ex, p := sampleReview()

	_, err := a.Review(context.Background(), ex, p, 1)
	if !errors.Is(err, llm.ErrLLMUnavailable) {
		t.Fatalf("want LLM error propagated, got %v", err)
	}
	if errors.Is(err, ErrReviewParse) {
		t.Fatal("a real LLM error must NOT be reported as a parse error")
	}
}

// Score must never gate the verdict: Pass equals the model's pass field exactly,
// regardless of how high or low the score is.
func TestReviewScoreNeverGates(t *testing.T) {
	cases := []struct {
		body     string
		wantPass bool
	}{
		{`{"pass": true, "score": 0.5, "feedback": {}}`, true},    // low score, still pass
		{`{"pass": false, "score": 0.99, "feedback": {}}`, false}, // high score, still fail
	}
	for _, c := range cases {
		f := &fakeLLM{resp: llm.CompletionResponse{Content: c.body}}
		a := NewReviewer(f, testCfg())
		ex, p := sampleReview()

		v, err := a.Review(context.Background(), ex, p, 1)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v.Pass != c.wantPass {
			t.Fatalf("body %q: Pass = %v, want %v (score must not gate)", c.body, v.Pass, c.wantPass)
		}
	}
}
