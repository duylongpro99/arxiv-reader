package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/genai"
)

func TestGeminiCompleteHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"candidates":[{"content":{"parts":[{"text":"hello world"}],"role":"model"},"finishReason":"STOP"}],
			"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":7,"totalTokenCount":19}}`))
	}))
	defer srv.Close()

	client, err := newGeminiClient(llmTestCfg("gemini", srv.URL))
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

func TestMapGeminiErr(t *testing.T) {
	tests := []struct {
		name string
		in   error
		want error
	}{
		{"nil", nil, nil},
		{"timeout", context.DeadlineExceeded, ErrLLMTimeout},
		{"429", genai.APIError{Code: 429}, ErrLLMRateLimit},
		{"400", genai.APIError{Code: 400}, ErrLLMBadRequest},
		{"503", genai.APIError{Code: 503}, ErrLLMUnavailable},
		{"unknown", errors.New("boom"), ErrLLMUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapGeminiErr(tt.in); !errors.Is(got, tt.want) {
				t.Fatalf("mapGeminiErr(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
