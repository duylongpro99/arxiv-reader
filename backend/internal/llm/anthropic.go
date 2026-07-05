package llm

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/maritime-ds/arxiv-reader/internal/config"
)

// anthropicClient is the text-only Anthropic implementation of LLMClient.
type anthropicClient struct {
	cfg     *config.LLMConfig
	sdk     anthropic.Client // NewClient returns a value, not a pointer
	timeout time.Duration
}

func newAnthropicClient(cfg *config.LLMConfig) (LLMClient, error) {
	opts := []option.RequestOption{option.WithAPIKey(cfg.APIKey)}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL)) // custom endpoint/proxy
	}
	return &anthropicClient{
		cfg:     cfg,
		sdk:     anthropic.NewClient(opts...),
		timeout: time.Duration(cfg.RequestTimeoutSec) * time.Second,
	}, nil
}

// Complete sends SystemPrompt + (DocumentText as a leading text block) +
// UserPrompt, inside the shared withRetry loop. A per-attempt timeout is applied
// so each individual SDK call is bounded (the retry backoffs use the caller ctx).
func (c *anthropicClient) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	var out CompletionResponse
	err := withRetry(ctx, func() error {
		callCtx, cancel := context.WithTimeout(ctx, c.timeout)
		defer cancel()

		blocks := make([]anthropic.ContentBlockParamUnion, 0, 2)
		if req.DocumentText != "" {
			blocks = append(blocks, anthropic.NewTextBlock("Paper content:\n\n"+req.DocumentText))
		}
		blocks = append(blocks, anthropic.NewTextBlock(req.UserPrompt))

		resp, err := c.sdk.Messages.New(callCtx, anthropic.MessageNewParams{
			Model:       anthropic.Model(c.cfg.Model),
			MaxTokens:   int64(req.MaxTokens),
			Temperature: anthropic.Float(float64(req.Temperature)),
			System:      []anthropic.TextBlockParam{{Text: req.SystemPrompt}},
			Messages:    []anthropic.MessageParam{anthropic.NewUserMessage(blocks...)},
		})
		if err != nil {
			return mapAnthropicErr(err)
		}
		out = CompletionResponse{
			Content:      anthropicText(resp),
			InputTokens:  int(resp.Usage.InputTokens),
			OutputTokens: int(resp.Usage.OutputTokens),
		}
		return nil
	})
	return out, err
}

// anthropicText concatenates the text of every text content block in the reply.
func anthropicText(msg *anthropic.Message) string {
	var sb strings.Builder
	for _, block := range msg.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	return sb.String()
}

// mapAnthropicErr maps an SDK error to a shared sentinel. Timeout is detected
// first (it can wrap without an HTTP status), then the typed API error's status
// code is classified; an unrecognized error defaults to unavailable (retryable).
func mapAnthropicErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ErrLLMTimeout
	}
	var apiErr *anthropic.Error
	if errors.As(err, &apiErr) {
		return classifyHTTPStatus(apiErr.StatusCode)
	}
	return ErrLLMUnavailable
}
