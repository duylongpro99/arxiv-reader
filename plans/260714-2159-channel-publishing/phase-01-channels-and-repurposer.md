# Phase 01 — Channel abstraction + registry + config + Repurposer agent

**Context:** `docs/design-notes/2026-07-14-channel-publishing.md` · plan `plan.md`
**Priority:** Critical (foundation) · **Status:** complete
**Wave:** 1 (parallel with P2)

## Overview
Pure-domain backend core, zero external network. Defines the two decoupled halves — the `Channel` contract and the category-blind `Repurposer` agent — plus config. Fully unit-testable without any channel implementation or DB.

## Key insight
Agent keys on **Category**, channel keys on its **Category()**. Neither knows the other. Mirror `internal/llm` (interface + registry switch) and `internal/agents/explainer.go` (struct + prompt file + `Generate()`).

## Requirements
- `Category` type + enum `longform | digest | brief`, with a `Valid()` guard (mirror `config.validProviders` defense-in-depth).
- `Channel` interface + shared DTOs (`GeneratedContent`, `PublishResult`).
- `NewChannel(id, cfg)` registry switch (stub: unknown id → descriptive error, never nil). P4/P5 append cases.
- Config `publishing:` block: enabled channels list, per-category target word counts.
- `Repurposer` agent parametrized by category; category-specific prompt templates; single-shot (no reviewer loop).

## Architecture
```go
// internal/channels/category.go
type Category string
const ( Longform Category = "longform"; Digest Category = "digest"; Brief Category = "brief" )
func (c Category) Valid() bool { ... }

// internal/channels/channel.go
type GeneratedContent struct {
    Category  Category
    Title     string
    Body      string          // platform-agnostic markdown/plain text
    PaperMeta models.Paper
    Tags      []string
}
type PublishResult struct { ExternalURL, ExternalID string }
type Channel interface {
    ID() string
    Category() Category
    Validate(c GeneratedContent) error
    Publish(ctx context.Context, c GeneratedContent) (PublishResult, error)
}

// internal/channels/registry.go — mirrors llm.NewLLMClient
func NewChannel(id string, cfg *config.Config) (Channel, error) {
    switch id {
    // case "devto": return devto.New(cfg)   // added in P4
    // case "x":     return x.New(cfg)        // added in P5
    default:
        return nil, fmt.Errorf("unknown channel %q", id)
    }
}
```
```go
// internal/agents/repurposer/repurposer.go — mirrors agents/explainer.go
type Repurposer struct { llm llm.LLMClient; cfg *config.Config }
func New(client llm.LLMClient, cfg *config.Config) *Repurposer
type RepurposeInput struct { Raw string; Category channels.Category; PaperMeta models.Paper }
func (a *Repurposer) Generate(ctx, in RepurposeInput) (channels.GeneratedContent, error)
// picks prompt by in.Category; MaxTokens/Temperature from cfg.LLM; target words from cfg.Publishing
```
Prompt templates (`repurposer-prompt.go`): one per category. `longform` = full reader-friendly article w/ concrete examples + code where natural; `brief` = punchy hook + key takeaway (single coherent short piece — NOT pre-chunked); `digest` = mid summary. All instructed to lean on the explainer's *Analogies & Intuition* + *Glossary* for accessibility. Emit `GeneratedContent` (parse title from first `# ` / paper title fallback).

## Config (`config.yaml` + `config.go`)
```yaml
publishing:
  channels: ["devto", "x"]        # enabled channel ids
  categories:
    longform: { target_words: 1200 }
    digest:   { target_words: 500 }
    brief:    { target_words: 120 }
```
Add `Publishing PublishingConfig` to `Config`. Validate enabled ids are known; categories map to valid `Category`.

## Files
- Create: `internal/channels/{category.go,channel.go,registry.go}`, `internal/agents/repurposer/{repurposer.go,repurposer-prompt.go}`, `internal/models/publication.go` (if domain structs shared beyond channels)
- Modify: `internal/config/config.go`, `config.yaml`, `.env.example` (channel token placeholders, commented)

## Todo
- [x] `Category` + `Valid()` + tests
- [x] `Channel` interface + DTOs
- [x] `NewChannel` registry stub + unknown-id test
- [x] `PublishingConfig` + validation + config test
- [x] `Repurposer` + per-category prompts + `Generate` test (fake `llm.LLMClient`, assert prompt selected by category, no channel refs)
- [x] `go build ./... && go test ./...`

**Deviation:** Registry implemented as self-registration (channels.Register + init()) instead of switch — avoids import cycle. See plan.md completion notes.

## Success criteria
Registry returns descriptive error for unknown id (never nil). Repurposer produces `GeneratedContent` for each category using a fake LLM; no symbol in `agents/repurposer` references any concrete channel. Config round-trips.

## Security
No secrets here. Token placeholders in `.env.example` only (commented, empty).
