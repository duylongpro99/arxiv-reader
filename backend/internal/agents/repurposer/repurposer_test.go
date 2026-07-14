package repurposer

import (
	"context"
	"strings"
	"testing"

	"github.com/maritime-ds/arxiv-reader/internal/channels"
	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/maritime-ds/arxiv-reader/internal/llm"
	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// fakeLLM is a deterministic LLMClient stub mirroring agents.fakeLLM: it
// records the last request so tests can assert prompt wiring, and returns a
// canned response or a forced error.
type fakeLLM struct {
	resp llm.CompletionResponse
	err  error
	last llm.CompletionRequest
}

func (f *fakeLLM) Complete(_ context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
	f.last = req
	if f.err != nil {
		return llm.CompletionResponse{}, f.err
	}
	return f.resp, nil
}

func testCfg() *config.Config {
	return &config.Config{
		LLM: config.LLMConfig{MaxTokens: 4096, Temperature: 0.4},
		Publishing: config.PublishingConfig{
			Categories: map[string]config.CategoryConfig{
				"longform": {TargetWords: 1200},
				"digest":   {TargetWords: 500},
				"brief":    {TargetWords: 120},
			},
		},
	}
}

func testPaper() models.Paper {
	return models.Paper{ID: "1706.03762", Title: "Attention Is All You Need", Authors: []string{"Vaswani"}, Published: "2017-06-12"}
}

// TestGeneratePicksPromptPerCategory asserts each Category selects its own
// system prompt template and that the target word count from config is woven
// into the user prompt.
func TestGeneratePicksPromptPerCategory(t *testing.T) {
	tests := []struct {
		category       channels.Category
		wantSystem     string
		wantTargetWord string
	}{
		{channels.Longform, longformSystemPrompt, "1200"},
		{channels.Digest, digestSystemPrompt, "500"},
		{channels.Brief, briefSystemPrompt, "120"},
	}

	for _, tt := range tests {
		t.Run(string(tt.category), func(t *testing.T) {
			f := &fakeLLM{resp: llm.CompletionResponse{Content: "# A Title\n\nBody text.", InputTokens: 10, OutputTokens: 20}}
			a := New(f, testCfg())

			out, err := a.Generate(context.Background(), RepurposeInput{
				Raw:       "explainer markdown with Analogies & Intuition and Glossary sections",
				Category:  tt.category,
				PaperMeta: testPaper(),
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if f.last.SystemPrompt != tt.wantSystem {
				t.Fatalf("wrong system prompt selected for category %q", tt.category)
			}
			if !strings.Contains(f.last.UserPrompt, tt.wantTargetWord) {
				t.Fatalf("target word count %q not in user prompt: %q", tt.wantTargetWord, f.last.UserPrompt)
			}
			// Source explainer text rides in DocumentText, not inlined in the prompt.
			if f.last.DocumentText != "explainer markdown with Analogies & Intuition and Glossary sections" {
				t.Fatalf("raw explainer text not sent as DocumentText: %q", f.last.DocumentText)
			}
			if out.Category != tt.category {
				t.Fatalf("GeneratedContent.Category = %q, want %q", out.Category, tt.category)
			}
			if out.Title != "A Title" {
				t.Fatalf("Title = %q, want parsed heading", out.Title)
			}

			// No symbol in this package may reference a concrete channel — assert
			// the prompts and output never mention one by name.
			for _, banned := range []string{"devto", "dev.to", "twitter", " x "} {
				if strings.Contains(strings.ToLower(tt.wantSystem), banned) {
					t.Fatalf("system prompt for %q references a concrete channel %q", tt.category, banned)
				}
			}
		})
	}
}

// TestGenerateTitleFallback asserts a missing `# ` heading falls back to the
// paper's own title rather than an empty string.
func TestGenerateTitleFallback(t *testing.T) {
	f := &fakeLLM{resp: llm.CompletionResponse{Content: "no heading here, just prose"}}
	a := New(f, testCfg())

	out, err := a.Generate(context.Background(), RepurposeInput{
		Raw:       "raw",
		Category:  channels.Brief,
		PaperMeta: testPaper(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.Title != testPaper().Title {
		t.Fatalf("Title = %q, want fallback to paper title %q", out.Title, testPaper().Title)
	}
}

// TestGenerateUsesDefaultTargetWordsWhenUnconfigured asserts a missing
// publishing.categories entry falls back to the design-note default instead
// of sending "approximately 0 words".
func TestGenerateUsesDefaultTargetWordsWhenUnconfigured(t *testing.T) {
	f := &fakeLLM{resp: llm.CompletionResponse{Content: "# T\nbody"}}
	// Empty publishing config — the feature-disabled, but still valid, state.
	a := New(f, &config.Config{LLM: config.LLMConfig{MaxTokens: 100, Temperature: 0.2}})

	_, err := a.Generate(context.Background(), RepurposeInput{Raw: "raw", Category: channels.Brief, PaperMeta: testPaper()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(f.last.UserPrompt, "120") {
		t.Fatalf("expected default target words (120) for brief, got prompt: %q", f.last.UserPrompt)
	}
}

// TestGenerateRejectsInvalidCategory asserts the Category.Valid() guard runs
// before any LLM call — no request should ever be sent for a bad category.
func TestGenerateRejectsInvalidCategory(t *testing.T) {
	f := &fakeLLM{resp: llm.CompletionResponse{Content: "should not be reached"}}
	a := New(f, testCfg())

	_, err := a.Generate(context.Background(), RepurposeInput{Raw: "raw", Category: channels.Category("thread"), PaperMeta: testPaper()})
	if err == nil {
		t.Fatal("expected error for invalid category, got nil")
	}
	if f.last.SystemPrompt != "" {
		t.Fatal("LLM must not be called for an invalid category")
	}
}

// TestGeneratePropagatesLLMError mirrors agents.TestGeneratePropagatesLLMError:
// a genuine client error is returned unchanged.
func TestGeneratePropagatesLLMError(t *testing.T) {
	f := &fakeLLM{err: llm.ErrLLMBadRequest}
	a := New(f, testCfg())

	_, err := a.Generate(context.Background(), RepurposeInput{Raw: "raw", Category: channels.Longform, PaperMeta: testPaper()})
	if err != llm.ErrLLMBadRequest {
		t.Fatalf("client error not propagated: %v", err)
	}
}
