# Phase 05 — X channel (`brief`) + thread chunking

**Context:** design-note · `plan.md` · implements `channels.Channel` (P1)
**Priority:** High · **Status:** complete · **Depends on:** P1 + P4 proven (registry sequence)
**Wave:** 3

## Overview
Hardest channel: OAuth2 user-context auth. Consumes `brief`. The agent emits ONE short piece; **this channel mechanically chunks it into a numbered ≤280-char tweet thread** — no LLM.

## The hard part — auth (isolate here)
- X API v2 `POST /2/tweets` requires **OAuth2 user-context** (Authorization Code + PKCE) with `tweet.write` scope. App must be registered on X's developer portal; free tier is heavily write-capped.
- **Local single-user strategy:** run the PKCE flow once (out of band / a small helper), store the **refresh token** in `.env` (`X_REFRESH_TOKEN`, gitignored). Channel exchanges refresh→access token at publish time; persist rotated refresh token back (local secrets file or instruct user to update `.env`). Document the one-time setup in README.
- If auth blocks progress: **ship dev.to alone**; X remains behind its config flag. Do not let X gate the release.

## Thread chunking (deterministic, no LLM)
- Split `GeneratedContent.Body` into segments ≤280 chars, prefer sentence/paragraph boundaries, never mid-word. Append ` (i/N)` counter (account for counter length in the 280 budget).
- First tweet via `POST /2/tweets`; each subsequent with `reply.in_reply_to_tweet_id` of the previous → a real thread. `PublishResult.ExternalURL` = first tweet URL.
- Partial-failure: if tweet k fails after 1..k-1 posted, `MarkFailed` with which segment failed (thread already partly live — surface clearly; do not auto-delete).

## Implementation (`internal/channels/x/x.go`)
```go
type Channel struct { oauth *tokenSource; httpc *http.Client }
func New(cfg *config.Config) (channels.Channel, error)   // refresh token from env; error if missing
func (c *Channel) ID() string                 { return "x" }
func (c *Channel) Category() channels.Category { return channels.Brief }
func (c *Channel) Validate(g) error            // non-empty; chunkable
func (c *Channel) Publish(ctx, g) (channels.PublishResult, error)  // refresh→access, chunk, post thread
```
- `tokenSource` (in `x/oauth.go`): refresh-token → access-token exchange w/ expiry cache.
- Register: append `case "x": return x.New(cfg)` to `registry.go` (**after** P4's case).

## Files
- Create: `internal/channels/x/{x.go,oauth.go,chunk.go,*_test.go}`
- Modify: `internal/channels/registry.go`, `.env.example`, README (one-time OAuth setup)

## Todo
- [x] `chunk.go` + exhaustive chunking tests (boundaries, counter budget, single-segment, long word)
- [x] `oauth.go` refresh→access (httptest)
- [x] `Publish` thread posting (httptest: assert reply chaining; partial-failure path)
- [x] registry self-registration (`init()` in x.go + blank import in main.go, per P1 deviation; sequenced after P4's devto)
- [x] `go build ./... && go test ./...`
- [ ] **Manual E2E** (user-run, real app): one-time PKCE → publish `brief` → thread appears → first-tweet url stored. *User action pending.* Requires X OAuth setup and `X_REFRESH_TOKEN` in `.env`. See `docs/channel-x-oauth-setup.md` for one-time setup instructions.

## Success criteria
Chunker + oauth + threading unit tests pass offline. With real credentials, a `brief` posts as a numbered thread; url stored; re-publish blocked. Feature degrades gracefully (config flag off) if auth unavailable.

## Security
Tokens in `.env` only; rotated refresh token persisted locally, never logged/traced/returned. Scrubber patterns cover bearer tokens.
