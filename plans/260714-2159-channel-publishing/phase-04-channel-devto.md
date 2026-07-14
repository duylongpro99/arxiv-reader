# Phase 04 — dev.to channel (`longform`)

**Context:** design-note · `plan.md` · implements `channels.Channel` (P1)
**Priority:** High · **Status:** complete · **Depends on:** P1 (interface). Prove BEFORE P5.
**Wave:** 3

## Overview
First real channel — simplest auth (single API key). Proves the whole pipeline end-to-end. Consumes `longform`.

## dev.to Forem API
- Endpoint: `POST https://dev.to/api/articles` with header `api-key: <DEVTO_API_KEY>`.
- Body: `{"article":{"title","body_markdown","published":true,"tags":[...]}}`.
- Response: `{ "url": ..., "id": ... }` → `PublishResult{ExternalURL:url, ExternalID:id}`.
- Tags: dev.to allows ≤4, alphanumeric. Sanitize/truncate from `GeneratedContent.Tags`.

## Implementation (`internal/channels/devto/devto.go`)
```go
type Channel struct { apiKey string; httpc *http.Client; baseURL string }
func New(cfg *config.Config) (channels.Channel, error)   // key from cfg/env; error if missing
func (c *Channel) ID() string                 { return "devto" }
func (c *Channel) Category() channels.Category { return channels.Longform }
func (c *Channel) Validate(g channels.GeneratedContent) error  // title non-empty, body ≤ dev.to max
func (c *Channel) Publish(ctx, g) (channels.PublishResult, error)
```
- Reuse the repo's HTTP+retry convention (respect `cfg.Agent.RequestTimeoutSec`; map 429→retry, 401→clear auth error). Keep it minimal — Forem is forgiving.
- Register: append `case "devto": return devto.New(cfg)` to `internal/channels/registry.go` (**sequence before P5's edit**).

## Config / secrets
- `.env`: `DEVTO_API_KEY=` (gitignored). `.env.example`: commented placeholder.
- Key loaded via config (mirror LLM `api_key: "${LLM_API_KEY}"` pattern). Never logged.

## Files
- Create: `internal/channels/devto/{devto.go,devto_test.go}`
- Modify: `internal/channels/registry.go` (append case), `.env.example`, `config.yaml`/`config.go` if a `base_url` override wanted

## Todo
- [x] `devto.New` + key load (+ missing-key error)
- [x] `Publish` (httptest-backed unit test: assert request shape, parse url/id)
- [x] `Validate` bounds
- [x] registry case (self-registration via init + blank import, per P1 deviation)
- [x] `go build ./... && go test ./...`
- [ ] **Manual E2E** (user-run, real key): generate `longform` draft → edit → publish → article appears on dev.to → url stored. *User action pending.* Requires `DEVTO_API_KEY` in `.env`.

## Success criteria
httptest test passes offline. With a real key, a run publishes a live dev.to article and stores its URL; re-publish blocked (P3 409).

## Security
Key never in logs, traces, or responses (scrubber covers). 401 surfaces a clear "check DEVTO_API_KEY" message, not the raw body.
