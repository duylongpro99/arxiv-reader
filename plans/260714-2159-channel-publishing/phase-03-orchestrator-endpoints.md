# Phase 03 — Orchestrator endpoints + wiring + tracing + DB guard

**Context:** design-note · `plan.md` · mirrors `orchestrator/runs-handlers.go`, `server/server.go`
**Priority:** Critical · **Status:** complete · **Depends on:** P1, P2
**Wave:** 2

## Overview
HTTP surface tying the pieces together: list channels, generate drafts (per unique category, fan out per channel), list/edit/approve/publish. Reuses the run-content read path and the tracing Recorder. Publishing refuses to run without a DB.

## Endpoints (Go 1.22 method routing in `server.go`)
```
GET   /channels                    → enabled channels [{id, category}]
POST  /runs/{id}/publications      → body {channels:[ids]}; generate drafts
GET   /runs/{id}/publications      → list drafts + statuses
PATCH /publications/{pid}          → body {title?, content?, approve?}; edit/approve
POST  /publications/{pid}/publish  → push to channel; store url; 409 if already published
```

## Draft generation flow (`POST /runs/{id}/publications`)
1. **DB guard:** `store == nil` → 503 `{"error":"publishing requires the database"}`.
2. Read the run's Obsidian markdown via the **existing** run-content path (same file lookup `HandleRunContent` uses: vaultwriter-completed event → path → `tools.ValidateWithinVault` → read). Missing → 4xx clear error.
3. Resolve requested channel ids → `channels.NewChannel(id, cfg)`; collect their `Category()`.
4. **Generate per UNIQUE category** (dedup): one `Repurposer.Generate` call per distinct category → cache `GeneratedContent` by category.
5. Fan out: per channel, `CreatePublication{status:"draft", category, adapted_content: content.Body, title}` (idempotent — existing `(run,channel)` skipped, returned as-is).
6. Emit tracing `publication.draft.generated` per channel (reuse `o.tracer`/Recorder; scrub applies). Return the draft list.

## Publish flow (`POST /publications/{pid}/publish`)
1. DB guard. Load publication; if `status == "published"` → 409 (idempotent, no re-post).
2. Rebuild `GeneratedContent` from the (possibly edited) row; `ch.Validate` → 422 on failure (e.g. over char budget after edits).
3. `ch.Publish(ctx, content)` → on success `MarkPublished(url, extID)`, emit `publication.published`; on error `MarkFailed(err)`, emit `publication.failed`, return 502 with channel error (scrubbed).

## Wiring (`cmd/server/main.go`)
- Build `Repurposer` from existing shared `llm.LLMClient` + cfg.
- Pass `store` (may be nil) + `tracer` into the new handlers (mirror how runs-handlers get `RunReader`). Define a narrow `PublicationStore` consumer interface in orchestrator (like `RunReader`) for testability.

## Files
- Create: `internal/orchestrator/publish-handlers.go`
- Modify: `internal/orchestrator/dto.go` (request/response DTOs), `internal/server/server.go` (routes), `cmd/server/main.go` (wiring)

## Todo
- [x] `PublicationStore` consumer interface + in-memory fake for tests
- [x] `GET /channels`
- [x] `POST /runs/{id}/publications` (DB guard, content read, per-category dedup, fan-out, tracing)
- [x] `GET /runs/{id}/publications`
- [x] `PATCH /publications/{pid}` (edit/approve)
- [x] `POST /publications/{pid}/publish` (validate, publish, 409/502, tracing)
- [x] Register routes; wire in `orchestrator.New` (NOT main.go — see deviation note below)
- [x] handler tests (fake store + fake channel returning canned `PublishResult`/error)
- [x] `go build ./... && go test ./...`

**Deviations:**
- **Wiring Location**: Lives in `internal/orchestrator/orchestrator.go`'s `New(cfg)`, not `cmd/server/main.go`. The ownership table originally said P3 owns main.go wiring; in practice, main.go only gains blank imports for channel self-registration (from P1 deviation), while the actual Repurposer + channelFactory wiring is in orchestrator.New().
- **Store-Open Gate**: Widened to `DatabaseURL != "" || Tracing.Enabled`. Publishing needs durable state even when tracing is off. Tracing writers are only wired when tracing is enabled, so publishing never silently turns tracing on.
- **Timeouts (Code Review)**: Initial `dbReadTimeout: 5s` would cancel every real LLM generation and publish action. Added context-scoped timeouts: `generateTimeout` (LLM budget, ~100s) and `publishTimeout` (full publish, ~30s).

**Code Review Findings (applied):**
- Approval gate: draft cannot be published; status must be "approved" (409 if still draft).
- Atomic ClaimForPublish: prevents concurrent double-posting via transient status transition (approved/failed → publishing).
- Timeout tuning: as noted above.

## Success criteria
With DB off → all publishing endpoints 503, rest of app unaffected. With DB on + a stub channel: generate → 2 drafts from 2 categories with a single agent call per category; edit via PATCH; publish → url stored, second publish → 409. Tracing events visible on the run timeline.

## Security
API keys/tokens never in responses or events (existing `tracing/scrub`). Channel publish errors scrubbed before returning.
