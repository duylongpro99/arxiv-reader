package agents

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/maritime-ds/arxiv-reader/internal/llm"
	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// fakeLLM is a deterministic LLMClient stub. It records the last request so tests
// can assert prompt wiring, and returns a canned response or a forced error.
type fakeLLM struct {
	resp    llm.CompletionResponse
	err     error
	lastReq llm.CompletionRequest
}

func (f *fakeLLM) Complete(_ context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
	f.lastReq = req
	if f.err != nil {
		return llm.CompletionResponse{}, f.err
	}
	return f.resp, nil
}

func testCfg() *config.Config {
	return &config.Config{LLM: config.LLMConfig{MaxTokens: 4096, Temperature: 0.4}}
}

// fullNote is a well-formed response with all 9 headings in order.
const fullNote = `# Attention Is All You Need — Explained

## Problem Statement
Recurrent models process tokens sequentially, which is slow.

## Core Idea
Replace recurrence with self-attention.

## Methodology
Stacked encoder/decoder blocks of multi-head attention.

## Key Findings
State-of-the-art translation with far less training time.

## Limitations
Quadratic memory in sequence length.

## Why It Matters
Foundation of modern LLMs.

## Analogies & Intuition
Like a meeting where everyone hears everyone at once.

## Glossary
**Attention** — weighting inputs by relevance.

## Follow-Up Papers
- BERT (https://arxiv.org/abs/1810.04805)
- Suggested: GPT-3
`

func TestGenerateHappyPath(t *testing.T) {
	f := &fakeLLM{resp: llm.CompletionResponse{Content: fullNote, InputTokens: 1200, OutputTokens: 800}}
	a := New(f, testCfg())

	out, err := a.Generate(context.Background(), ExplainerInput{
		MarkdownText: "paper markdown body",
		PaperMeta:    models.Paper{ID: "1706.03762", Title: "Attention Is All You Need", Authors: []string{"Vaswani", "Shazeer"}, Published: "2017-06-12"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All 9 sections parsed.
	if len(out.Sections) != 9 {
		t.Fatalf("parsed %d sections, want 9: %v", len(out.Sections), keys(out.Sections))
	}
	if got := out.Sections["problem_statement"]; !strings.Contains(got, "Recurrent models") {
		t.Fatalf("problem_statement body wrong: %q", got)
	}
	// Metadata + token accounting.
	if out.PaperID != "1706.03762" || out.Iteration != 1 {
		t.Fatalf("unexpected output meta: %+v", out)
	}
	if out.InputTokens != 1200 || out.OutputTokens != 800 {
		t.Fatalf("token counts not propagated: %+v", out)
	}
	if out.CreatedAt.IsZero() {
		t.Fatal("CreatedAt not set")
	}

	// The paper text must ride in DocumentText (text-only contract), not the prompt.
	if f.lastReq.DocumentText != "paper markdown body" {
		t.Fatalf("markdown not sent as DocumentText: %q", f.lastReq.DocumentText)
	}
	if f.lastReq.MaxTokens != 4096 || f.lastReq.Temperature != 0.4 {
		t.Fatalf("config knobs not wired: %+v", f.lastReq)
	}
	// Published passed through as-is (string, not formatted).
	if !strings.Contains(f.lastReq.UserPrompt, "2017-06-12") {
		t.Fatalf("published not passed through as-is: %q", f.lastReq.UserPrompt)
	}
}

func TestGenerateMissingSectionsTolerated(t *testing.T) {
	partial := "# Title\n\n## Problem Statement\nonly this one\n"
	f := &fakeLLM{resp: llm.CompletionResponse{Content: partial, InputTokens: 10, OutputTokens: 20}}
	a := New(f, testCfg())

	out, err := a.Generate(context.Background(), ExplainerInput{MarkdownText: "md", PaperMeta: models.Paper{ID: "x"}})
	if err != nil {
		t.Fatalf("missing sections must not error: %v", err)
	}
	// Full content preserved even though only 1 section parsed.
	if out.Content != partial {
		t.Fatal("full Content must be saved verbatim")
	}
	if len(out.Sections) != 1 {
		t.Fatalf("want 1 parsed section, got %d", len(out.Sections))
	}
}

func TestGeneratePropagatesLLMError(t *testing.T) {
	f := &fakeLLM{err: llm.ErrLLMBadRequest}
	a := New(f, testCfg())

	_, err := a.Generate(context.Background(), ExplainerInput{MarkdownText: "md", PaperMeta: models.Paper{ID: "x"}})
	if !errors.Is(err, llm.ErrLLMBadRequest) {
		t.Fatalf("client error not propagated: %v", err)
	}
}

// The revision path is unused in Phase 4 but wired for Phase 5; assert the prompt
// carries the revision instructions when RevisionNote is set.
func TestBuildUserPromptRevisionAware(t *testing.T) {
	a := New(&fakeLLM{}, testCfg())
	p := a.buildUserPrompt(ExplainerInput{
		PaperMeta:    models.Paper{ID: "x", Title: "T", Published: "2020"},
		RevisionNote: "expand the glossary",
	})
	if !strings.Contains(p, "REVISION INSTRUCTIONS:") || !strings.Contains(p, "expand the glossary") {
		t.Fatalf("revision note not woven into prompt: %q", p)
	}
}

// The prompt must contain no image/vision language (text-only guarantee).
func TestPromptIsTextOnly(t *testing.T) {
	for _, banned := range []string{"page image", "page images", "read every"} {
		if strings.Contains(strings.ToLower(systemPrompt), banned) {
			t.Fatalf("system prompt contains image language %q", banned)
		}
	}
}

func keys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
