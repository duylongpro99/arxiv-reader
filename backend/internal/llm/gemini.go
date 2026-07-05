package llm

import (
	"context"
	"errors"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/config"
	"google.golang.org/genai"
)

// geminiClient is the text-only Gemini implementation, using the current
// client.Models.GenerateContent surface (NOT the deprecated GenerativeModel API).
type geminiClient struct {
	cfg     *config.LLMConfig
	sdk     *genai.Client
	timeout time.Duration
}

func newGeminiClient(cfg *config.LLMConfig) (LLMClient, error) {
	cc := &genai.ClientConfig{APIKey: cfg.APIKey, Backend: genai.BackendGeminiAPI}
	if cfg.BaseURL != "" {
		cc.HTTPOptions = genai.HTTPOptions{BaseURL: cfg.BaseURL} // custom endpoint/proxy
	}
	// The client is long-lived; construct it with a background context (per-call
	// deadlines are applied in Complete, not here).
	client, err := genai.NewClient(context.Background(), cc)
	if err != nil {
		return nil, err
	}
	return &geminiClient{
		cfg:     cfg,
		sdk:     client,
		timeout: time.Duration(cfg.RequestTimeoutSec) * time.Second,
	}, nil
}

// Complete sends a system instruction + a single user content (DocumentText
// prepended to the prompt) via GenerateContent, inside the shared withRetry loop.
func (c *geminiClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	var out CompletionResponse
	err := withRetry(ctx, func() error {
		callCtx, cancel := context.WithTimeout(ctx, c.timeout)
		defer cancel()

		userText := req.UserPrompt
		if req.DocumentText != "" {
			userText = "Paper content:\n\n" + req.DocumentText + "\n\n" + req.UserPrompt
		}
		genCfg := &genai.GenerateContentConfig{
			Temperature:     genai.Ptr(req.Temperature),
			MaxOutputTokens: int32(req.MaxTokens),
		}
		if req.SystemPrompt != "" {
			genCfg.SystemInstruction = genai.NewContentFromText(req.SystemPrompt, genai.RoleUser)
		}

		resp, err := c.sdk.Models.GenerateContent(callCtx, c.cfg.Model, genai.Text(userText), genCfg)
		if err != nil {
			return mapGeminiErr(err)
		}
		out.Content = resp.Text()
		// UsageMetadata is a pointer and may be nil on error-free-but-empty replies.
		if resp.UsageMetadata != nil {
			out.InputTokens = int(resp.UsageMetadata.PromptTokenCount)
			out.OutputTokens = int(resp.UsageMetadata.CandidatesTokenCount)
		}
		return nil
	})
	return out, err
}

// mapGeminiErr maps an SDK error to a shared sentinel. genai.APIError is returned
// BY VALUE (value receiver on Error()), so errors.As targets a value, not a pointer.
func mapGeminiErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrLLMTimeout
	}
	var apiErr genai.APIError
	if errors.As(err, &apiErr) {
		return classifyHTTPStatus(apiErr.Code)
	}
	return ErrLLMUnavailable
}
