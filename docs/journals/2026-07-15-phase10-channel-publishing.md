# Phase 10: Channel Publishing — Silent Timeouts, Atomic Races, and API Guards Sleeping

**Date**: 2026-07-15
**Severity**: Critical (shipped successfully, but three bugs would have destroyed production credibility)
**Component**: Publish endpoints, Repurposer agent, Channel registry, publications database, frontend UI
**Status**: Resolved (pending user migration + credential setup)

## What Happened

Phase 10 shipped Channel Publishing: persist an explainer to social channels via a category-blind Repurposer agent that adapts content (one LLM call per unique category), a self-registering Channel registry with dev.to (longform) and X (brief), durable publications store with idempotent writes, five publish endpoints, and frontend UI on run-detail. All tests green. Code review found and fixed three defects invisible to the test suite — one would silently timeout every real LLM call, one would allow double-publishes to live channels, one would let unapproved drafts escape the UI guard.

## The Brutal Truth

We almost shipped a feature that would work perfectly in testing but fail silently in production. The fakes returned instantly, so timeouts stayed hidden. The tests ran serially in memory, so concurrent races never fired. The UI guard existed, but the API never checked it. You can have 100% test coverage and still ship three separate bugs that all require either time, concurrency, or boundary conditions to manifest. The humbling part: a human code review's adversarial read of the real deadlines and atomic guarantees caught all three. The tests gave us false confidence.

## Technical Details

**Feature A — Category-Blind Repurposer + Channel Registry**
- Repurposer agent adapts a draft explainer per Category (longform/digest/brief) — keys only on Category, blind to which channels consume it.
- Channel registry: self-registering pattern (database/sql-style). Each channel package imports `internal/channels`, calls `channels.Register(Category(), Handler)` in its init().
- Registry lives in `internal/channels/registry.go` with a map keyed by (Channel, Category) tuple.
- Adding a channel touches only its own package + a blank import in main.go.

**Feature B — Publications Store & Idempotent Writes**
- Migration `0002_publications.sql`: UNIQUE(run_id, channel_id) enforces one publication per run-channel pair.
- Publish workflow: claim run for publishing (transient state), generate/fetch draft, publish to channel, mark published.
- Five endpoints: POST /publishes/draft (generate), GET /publishes/{id}/preview (fetch), POST /publishes/{id}/approve, POST /publishes/{id}/publish, DELETE /publishes/{id}.

**Feature C — Dev.to + X Channels**
- dev.to: POST to API with Authorization header (DEVTO_API_KEY), longform HTML content, tag "arxiv".
- X: OAuth2 refresh-token stored in DB, PKCE one-time flow, deterministic ≤280-char thread chunking (recursive sentence split), thread as reply chain.

## What We Tried

- In-memory fake channels and publications store: fast, green tests, zero insight into real behavior.
- Serial test execution: no concurrent publish attempts, no race detection.
- UI-only approval guard: endpoints never validated approval server-side.

## Root Cause Analysis

**Bug 1 — LLM Timeout Silenced**: Draft generation used the same `dbReadTimeout=5s` to bound the entire operation, including the LLM call. But LLM budget is 120s. In testing, fakes returned instantly, so the 5s bound never fired. In production, every real Repurposer call would hit the timeout and get silently cancelled. The timeout existed for database reads; we reused it for LLM calls without auditing the deadline.

**Bug 2 — Double-Publish Race**: Publish workflow was non-atomic: read status, post to channel, mark published. A concurrent request could read "draft" before the first request marked published, then both would post to the live channel. The tests ran serially, so the TOCTOU window never opened. Fix: atomic `ClaimForPublish` transition to transient `publishing` status; post/mark complete under that lock.

**Bug 3 — API Trusts UI Guard**: Approve endpoint enforced approval only in the frontend. An unapproved draft could POST /publishes/{id}/publish directly (API had no check). Return 409 if draft is not approved. Tests never tried this path because the UI prevented it.

## Lessons Learned

1. **Fakes That Return Instantly Hide Timeout Bugs**: If your test double completes in 1ms but real implementation has 120s budget and 5s guard, tests never see the collision. Audit context deadlines in production paths: Do guards make sense for this operation? Add an assertion-style test that checks the actual deadline, not just "it runs fast."

2. **Serial Tests Hide Concurrency Bugs**: If every test case runs one request at a time, TOCTOU windows never open. Add at least one concurrent test (two goroutines, same resource, non-atomic operation). For publish, we should have had: two clients call publish() on the same draft simultaneously; verify exactly one post succeeds (or both fail).

3. **API Guards Must Match UI Guards**: The UI is not a security boundary. If approval is required, enforce it server-side. Same for state transitions: if the UI says "can only publish from draft state," the API should return 409 if that's violated. Don't trust the browser to send valid requests.

4. **Architectural Seams Require Test Bridges**: The Repurposer is blind to channels, and channels are blind to Repurposer. This is good coupling, but it means integration tests must prove the seam works. A test that calls Repurposer.Adapt() → passes result to real Channel.Publish() would have caught if the category mapping was wrong.

5. **Dependency Injection for Test Doubles**: Using real channels in tests (even in-memory ones) meant we could inject fakes that had different behavior. Instead, we should have had tests that swap the timeout context or inject a real-but-slow channel to measure actual behavior.

## Next Steps

- **User actions** (before fleet deployment):
  - Apply migration `0002_publications.sql` to all Postgres instances.
  - Set environment variable `DEVTO_API_KEY` (or run migration 0003 to create secrets table).
  - Run one-time X OAuth PKCE flow (`cmd/x-oauth-setup/main.go`) to seed refresh token.

- **Code hardening**:
  - Add context-aware timeout test: `TestPublishRespectsDraftGenerationDeadline` that simulates a slow LLM and verifies we respect the 120s budget, not 5s.
  - Add concurrent publish test: two goroutines, same run, verify idempotency guard fires.
  - Add API state guard test: POST /publish on non-approved draft returns 409.
  - Add integration test that flows Repurposer output → Channel.Publish for each channel.

- **Monitoring**:
  - Log all publish attempts: timestamp, run_id, channel, status (draft/claimed/published/failed).
  - Alert if any publish takes >60s (LLM timeout boundary, gives 120s budget - 60s for other ops).
  - Alert if ClaimForPublish ever fails with 409 (indicates concurrent attempt).

- **Documentation**:
  - Add `internal/agents/repurposer/README.md`: explain Category concept, how to add a new Category.
  - Add `internal/channels/README.md`: explain self-registration pattern, contract for Channel.Publish, how to add a channel.
  - Document timeout budgets in `routes/publish.go`: why 120s, why not inherited from dbReadTimeout.

---

**Files Modified**: 
- Backend: `internal/agents/repurposer/repurposer.go`, `internal/channels/{registry,devto,x}.go`, `routes/publish.go`, `middleware/publish-guard.go`, `schema/migrations/0002_publications.sql`
- Frontend: `src/components/RunDetail/PublishPanel.tsx`, `src/hooks/usePublish.ts`
- Config: `main.go` (blank imports for channels/devto, channels/x)

**Test Status**: All backend build/vet/test + frontend tsc/build passed. Code review (adversarial read of deadlines, atomic ops, API guards) identified and fixed three pre-ship defects. Green on all gates.
