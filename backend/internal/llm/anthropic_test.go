package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/maritime-ds/arxiv-reader/internal/config"
)

func llmTestCfg(provider, baseURL string) *config.LLMConfig {
	return &config.LLMConfig{
		Provider:          provider,
		Model:             "test-model",
		APIKey:            "test-key",
		MaxTokens:         100,
		Temperature:       0.3,
		RequestTimeoutSec: 10,
		BaseURL:           baseURL,
	}
}

func TestAnthropicCompleteHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"test-model",
			"content":[{"type":"text","text":"hello world"}],"stop_reason":"end_turn",
			"usage":{"input_tokens":12,"output_tokens":7}}`))
	}))
	defer srv.Close()

	client, err := newAnthropicClient(llmTestCfg("anthropic", srv.URL))
	if err != nil {
		t.Fatalf("constructor error: %v", err)
	}
	resp, err := client.Complete(context.Background(), CompletionRequest{
		SystemPrompt: "sys", UserPrompt: "hi", DocumentText: "doc", MaxTokens: 100, Temperature: 0.3,
	})
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if resp.Content != "hello world" {
		t.Errorf("content = %q, want %q", resp.Content, "hello world")
	}
	if resp.InputTokens != 12 || resp.OutputTokens != 7 {
		t.Errorf("tokens = (%d,%d), want (12,7)", resp.InputTokens, resp.OutputTokens)
	}
}

func TestMapAnthropicErr(t *testing.T) {
	tests := []struct {
		name string
		in   error
		want error
	}{
		{"nil", nil, nil},
		{"timeout", context.DeadlineExceeded, ErrLLMTimeout},
		{"429", &anthropic.Error{StatusCode: 429}, ErrLLMRateLimit},
		{"400", &anthropic.Error{StatusCode: 400}, ErrLLMBadRequest},
		{"401", &anthropic.Error{StatusCode: 401}, ErrLLMBadRequest},
		{"500", &anthropic.Error{StatusCode: 500}, ErrLLMUnavailable},
		{"unknown", errors.New("boom"), ErrLLMUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapAnthropicErr(tt.in); !errors.Is(got, tt.want) {
				t.Fatalf("mapAnthropicErr(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
