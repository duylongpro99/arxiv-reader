// Package llm defines the provider-agnostic LLM client the agents call. The
// interface and DTOs are text-only by design (DocumentText, never images): any
// text-capable model is valid, so there is no vision validation anywhere.
package llm

import (
	"context"
	"errors"
	"fmt"

	"github.com/maritime-ds/arxiv-reader/internal/config"
)

// LLMClient is the single abstraction every agent depends on, fully decoupled
// from any concrete provider. Providers (Phase 04) implement Complete; the
// shared withRetry wrapper (retry.go) drives the retry policy for all of them.
type LLMClient interface {
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
}

// CompletionRequest is a text-only prompt. DocumentText carries the paper
// Markdown produced by PaperContentTool and is sent as a text block/part
// prefixed "Paper content:" — NOT as an image. There is deliberately no
// PageImages field.
type CompletionRequest struct {
	SystemPrompt string
	UserPrompt   string
	DocumentText string
	MaxTokens    int
	Temperature  float32
}

// CompletionResponse returns the generated text and the input/output token
// counts separately (every supported provider exposes both).
type CompletionResponse struct {
	Content      string
	InputTokens  int
	OutputTokens int
}

// Shared sentinels. Providers map their SDK errors to these; the retry wrapper
// decides what to do with each. They carry no request/response payload, so they
// are safe to log.
var (
	ErrLLMRateLimit   = errors.New("LLM provider rate limit exceeded")
	ErrLLMBadRequest  = errors.New("LLM bad request — check model name and config")
	ErrLLMUnavailable = errors.New("LLM provider unavailable")
	ErrLLMTimeout     = errors.New("LLM request timed out")
)

// NewLLMClient selects a concrete provider from config. config.validProviders
// already whitelists the three at load time, so the default branch is
// defense-in-depth: it returns a descriptive error (never a nil client) so an
// unknown provider can never surface as a nil-pointer panic downstream.
func NewLLMClient(cfg *config.LLMConfig) (LLMClient, error) {
	switch cfg.Provider {
	case "anthropic":
		return newAnthropicClient(cfg)
	case "openai":
		return newOpenAIClient(cfg)
	case "gemini":
		return newGeminiClient(cfg)
	default:
		return nil, fmt.Errorf("unknown LLM provider %q — implement the LLMClient interface for custom providers", cfg.Provider)
	}
}
