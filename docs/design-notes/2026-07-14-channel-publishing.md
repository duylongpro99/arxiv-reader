# Design Note — Channel Publishing (select doc → adapt → publish)

Date: 2026-07-14
Status: Approved (brainstorm) → plan pending

Add a feature that selects a persisted run's explainer and publishes it to social channels (dev.to, X now; daily.dev via RSS later). Content is **not** posted raw — a repurposer agent rewrites it into reader-friendly, example-rich content. Locked decisions: **human review + edit before publish**, **content-category adaptation (not per-channel)**, **select from runs history**, **dev.to + X first**, **DB required for publishing**.

---

## Problem

The system currently ends at Obsidian. Publishing to social channels raises three structural challenges:
1. **Decoupling channels** — adding/removing a channel (dev.to, X, RSS) must not ripple through the system. Mirrors the existing `LLMClient` provider pattern.
2. **Content transformation** — raw Obsidian markdown is unfit for social; an agent must produce reader-friendly content with examples. But the agent must not become coupled to the growing channel list.
3. **Durable, idempotent publish state** — public posts are irreversible; "already posted to X" cannot be lost or duplicated.

## The key seam — content **category**, not channel format

Two concerns were conflated and are now split:

| Layer | Knows about | Blind to |
|---|---|---|
| **Repurposer agent** | content **category** (`longform`/`digest`/`brief`) + its target length | that channels exist at all |
| **Channel** | which **category** it consumes + its own platform mechanics | how content is generated |

The agent emits **pure, platform-agnostic content** for a category. Each channel consumes content *of its registered category* and owns all platform mechanics — including non-LLM delivery shaping (X mechanically chunks a `brief` into ≤280-char numbered tweets; the agent never emits a "thread"). Category carries a soft target length defined centrally in config, not per channel.

**Category taxonomy:** `longform` (deep article w/ examples & code) · `digest` (condensed summary) · `brief` (punchy hook).
**v1 mapping:** dev.to → `longform`; X → `brief`. `digest` reserved for the future RSS channel.

## Structure

**1. Channel abstraction** — self-contained package per channel (`internal/channels/{devto,x,rss}`) + a registry map, config-enabled, mirroring LLM providers. System depends only on the interface.
```go
type Category string // "longform" | "digest" | "brief"

type Channel interface {
    ID() string
    Category() Category                                  // the ONLY contract with content
    Validate(c GeneratedContent) error                   // char limits / required fields
    Publish(ctx, c GeneratedContent) (PublishResult, error) // platform mechanics; returns external URL/ID
}
```

**2. Repurposer agent** — `internal/agents/repurposer`, mirrors Explainer (`struct + prompt + Generate()`). Parametrized by **category** (format-specific prompt templates), consumes the run's explainer markdown (its *Analogies & Intuition* + *Glossary* sections feed "easy to read + examples"). **Single-shot** — the human is the reviewer, no Reviewer loop.
```go
func (a *Repurposer) Generate(ctx, RepurposeInput{Raw, Category, PaperMeta}) (GeneratedContent, error)
// sees only Category — never a channel ID
```

**3. Generate per category, fan out per channel.** If two selected channels share a category, the agent runs **once**; each channel gets an independent editable `Publication` draft. One LLM call → N drafts.

**4. Publication state** — new PostgreSQL table (schema documented; user runs the migration per the no-migrations rule):
`publications(id, run_id FK, channel_id, category, status[draft|approved|published|failed], adapted_content, external_url, external_id, error, created_at, published_at)`; unique `(run_id, channel_id)` for idempotency.

**5. Flow (human-in-loop):** pick run → pick channels → agent generates per category → editable preview per channel (thread preview / markdown preview) → approve → publish per-channel (failures isolated, retryable).

**6. Endpoints** (existing REST style):
```
GET   /channels                    list enabled channels + categories
POST  /runs/:id/publications       generate drafts (Repurposer)
GET   /runs/:id/publications       list drafts + statuses
PATCH /publications/:pid           edit adapted_content / approve
POST  /publications/:pid/publish   push to channel
```

**7. Tracing** — reuse the Recorder: `publication.draft.generated | published | failed`.

## Tradeoffs / rejected

- **Rejected — per-channel transform agent:** duplicates prompt logic (violates DRY), N agents, couples content to delivery.
- **Rejected — out-of-process channel microservices:** massive YAGNI for a local single-user app (IPC, separate deploys, queue for zero benefit). In-process adapters already give isolated code + per-channel failure isolation.
- **Rejected — `format` axis (article/thread/feed):** leaks platform/delivery concepts into the content layer. `category` (depth/length) is delivery-agnostic.

## Hard truths / risks

- **DB required for publishing.** PostgreSQL is optional/degrade-safe today, but publish state must be durable. Publishing feature disables itself when DB is off; rest of app unaffected.
- **X auth is the real cost, not posting.** OAuth2 user-context + token refresh + app approval on X's developer portal; free tier heavily write-capped. dev.to is a trivial API key. Expect ~70% of X effort to be auth plumbing.
- **daily.dev has no push API** — ingests via RSS/Squads. Modeled as an RSS channel (generate a feed it subscribes to) in a later phase, not a `Publish()` transport.
- **Secrets** — channel tokens in `.env` (gitignored), never logged, run through the existing scrubber.

## Success criteria

Select a run → generate a `longform` (dev.to) and a `brief` (X) draft → edit + approve in UI → publish → both return live external URLs stored on the `publications` row → re-publishing the same (run, channel) is blocked. Adding a third channel touches only its own package + one registry line.
