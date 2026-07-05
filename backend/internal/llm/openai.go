package llm

import (
	"context"
	"errors"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// openaiClient is the text-only OpenAI (Chat Completions) implementation.
type openaiClient struct {
	cfg     *config.LLMConfig
	sdk     openai.Client // NewClient returns a value, not a pointer
	timeout time.Duration
}

func newOpenAIClient(cfg *config.LLMConfig) (LLMClient, error) {
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL)) // custom endpoint/proxy
	}
	return &openaiClient{
		cfg:     cfg,
		sdk:     openai.NewClient(opts...),
		timeout: time.Duration(cfg.RequestTimeoutSec) * time.Second,
	}, nil
}

// Complete sends a system message + a user message (DocumentText prepended as a
// leading text block) via Chat Completions, inside the shared withRetry loop.
func (c *openaiClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	var out CompletionResponse
	err := withRetry(ctx, func() error {
		callCtx, cancel := context.WithTimeout(ctx, c.timeout)
		defer cancel()

		msgs := make([]openai.ChatCompletionMessageParamUnion, 0, 3)
		if req.SystemPrompt != "" {
			msgs = append(msgs, openai.SystemMessage(req.SystemPrompt))
		}
		if req.DocumentText != "" {
			msgs = append(msgs, openai.UserMessage("Paper content:\n\n"+req.DocumentText))
		}
		msgs = append(msgs, openai.UserMessage(req.UserPrompt))

		resp, err := c.sdk.Chat.Completions.New(callCtx, openai.ChatCompletionNewParams{
			Model:               openai.ChatModel(c.cfg.Model),
			MaxCompletionTokens: openai.Int(int64(req.MaxTokens)),
			Temperature:         openai.Float(float64(req.Temperature)),
			Messages:            msgs,
		})
		if err != nil {
			return mapOpenAIErr(err)
		}
		if len(resp.Choices) == 0 {
			return ErrLLMUnavailable // no completion returned — treat as retryable
		}
		out = CompletionResponse{
			Content:      resp.Choices[0].Message.Content,
			InputTokens:  int(resp.Usage.PromptTokens),
			OutputTokens: int(resp.Usage.CompletionTokens),
		}
		return nil
	})
	return out, err
}

// mapOpenAIErr maps an SDK error to a shared sentinel (same shape as the other
// providers; see mapAnthropicErr for the rationale).
func mapOpenAIErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrLLMTimeout
	}
	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		return classifyHTTPStatus(apiErr.StatusCode)
	}
	return ErrLLMUnavailable
}
