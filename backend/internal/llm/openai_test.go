package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openai/openai-go"
)

func TestOpenAICompleteHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"c1","object":"chat.completion","created":1,"model":"test-model",
			"choices":[{"index":0,"message":{"role":"assistant","content":"hello world"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":12,"completion_tokens":7,"total_tokens":19}}`))
	}))
	defer srv.Close()

	client, err := newOpenAIClient(llmTestCfg("openai", srv.URL))
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

func TestMapOpenAIErr(t *testing.T) {
	tests := []struct {
		name string
		in   error
		want error
	}{
		{"nil", nil, nil},
		{"timeout", context.DeadlineExceeded, ErrLLMTimeout},
		{"429", &openai.Error{StatusCode: 429}, ErrLLMRateLimit},
		{"400", &openai.Error{StatusCode: 400}, ErrLLMBadRequest},
		{"500", &openai.Error{StatusCode: 500}, ErrLLMUnavailable},
		{"unknown", errors.New("boom"), ErrLLMUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapOpenAIErr(tt.in); !errors.Is(got, tt.want) {
				t.Fatalf("mapOpenAIErr(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
