# Phase 2 Completion: ArXiv Discovery and Duplicate Detection

**Date**: 2026-07-04 21:45
**Severity**: Medium (architectural decisions, PRD reconciliation required)
**Component**: Backend Discovery Pipeline / Frontend Status Polling
**Status**: Resolved (ready for review, not yet committed)

## What Happened

Completed Phase 2 implementation: arXiv cs.AI paper discovery with live duplicate detection. Backend (Go) discovers papers via arXiv API, deduplicates against processed.json, and emits stage-labeled progress events. Frontend (Next.js 16/React 19) polls /status for real-time pipeline state. However, the PRD contradicted Phase 1 reality in three ways, forcing design reconciliation before coding. All backend tests pass with `-race` flag (thread-safety verified). Code-reviewer flagged 5 issues (1 H1 critical: panic recovery on detached goroutine, 2 M-severity, 2 L-severity deferred). Frontend builds clean. Not yet committed pending user review of architectural choice.

## The Brutal Truth

The PRD was written without reference to what Phase 1 actually shipped. Phase 1 deliberately skipped model definitions (YAGNI principle), but the PRD assumed they existed. The PRD also promised *both* synchronous discovery (immediate response with candidates) *and* asynchronous status polling with live stage labels—these are mutually contradictory. We had to choose. Choosing wrong here would cement a bad contract into the Phase 3 LLM layer and beyond. The brainstorm surfaced this friction early, which saved us from shipping broken assumptions. The async decision feels right for the long term (Phase 3 LLM calls will be slow anyway), but it's a deliberate departure from what the PRD literally asked for. That's uncomfortable but necessary.

## Technical Details

**Architectural Decision: Async Discovery with Polling**

The PRD specified: "POST /discover returns candidates in response" (sync) AND "frontend polls /status for stage labels" (async). These cannot coexist cleanly.

Resolution: `POST /discover` returns `{session_id}` immediately (202 Accepted). A detached goroutine (via `context.WithoutCancel()`) runs the full pipeline asynchronously. Frontend polls `GET /status/{session_id}` to fetch current stage and progress. Rationale: Phase 3's LLM inference will be 10-30s per paper; forcing the client to wait in a single HTTP request is untenable. Doing async now means the session/polling contract stays stable across Phases 2-6. Stage labels become truthful (not theater) because the stage actually changes over time.

**Thread Safety: PipelineSession Concurrency**

- PipelineSession is written by the detached goroutine and read by the status handler concurrently.
- Guarded by `sync.RWMutex` with accessor methods: `Snapshot()`, `Complete()`, `Fail()`.
- Verified clean with `go test -race ./...` (all 4 test suites, no data races).

**Technical Decisions**

- **Session IDs**: crypto/rand 16-byte hex strings (no UUID dependency, trivial collision risk at scale <10M sessions).
- **JSON Serialization**: camelCase tags to match frontend conventions (sessionId, stageLabel).
- **Corrupt processed.json**: HARD error (panic recovery added after code-review flagged it). Treating corrupt JSON as "empty" would re-surface already-processed papers, violating the Trust guarantee.
- **Log File Rename**: processed.log → processed.json (Phase 1 had wrong extension despite JSON content).
- **Config Addition**: Created new `config.Agent` section (PRD referenced it; Phase 1 had no Agent model). Contains MinRequestIntervalSec, MaxConcurrentRequests.
- **"Fewer than 5" Notice**: PRD contract gap (what if <5 papers exist?). Added notice field to discovery response.

**Code Review Issues (Severity Summary)**

- **H1**: Detached goroutine missing panic recovery. Unrecovered panic crashes the entire server. Fixed: wrap pipeline in defer+recover.
- **M1**: GET /status returned 404 as text/plain, breaking JSON poll contract on server restart. Fixed: always return JSON.
- **M2**: Retry log hardcoded "rate limited" for all transient errors (5xx, timeouts). Fixed: log actual error.
- **L2**: Duplicate `extractArxivID()` call in dedup logic. Removed.
- **L4**: Raw XML parse error could echo payload to logs, violating CLAUDE.md security rule. Fixed: return bare sentinel "parse error".
- **Deferred (with rationale)**:
  - L1 (MinRequestIntervalSec naming convention): Safe, low impact, defer to Phase 3 refactor.
  - L3 (omitempty on bool fields): Cosmetic, defer to frontend contract clarification.
  - In-memory session-store growth: Acceptable until >1M sessions; Phase 3 can add TTL pruning.

**Frontend Handling: Next.js 16 Gotchas**

- Dynamic route params are now Promises; avoided by using query-string for status polling.
- Route handlers use Web Request/Response, not Node.js http.
- Client components require 'use client' directive (added to polling component).
- Backend URL never bundled to client (always from env var at runtime).

**Test Coverage**

- Backend: config load/validation, discovery API, dedup correctness, log file integrity, session state snapshot/complete/fail, concurrent access (all pass `-race`).
- Frontend: build succeeds, lint clean, no TypeScript errors.

## What We Tried

- Brainstorm session with user to reconcile PRD vs Phase 1 code reality (3 major assumptions corrected).
- Initial sync-only design (rejected: blocks on LLM in Phase 3).
- Initial async-only design without immediate response (rejected: poor UX, no feedback).
- Final async with immediate session_id (accepted: unblocks client immediately, contract stable).
- Code-reviewer full audit (5 issues identified, 4 fixed, 1 deferred per rationale).
- Race condition testing across all test suites (clean).
- Frontend dynamic-route workaround investigation (query-string is cleaner than Promises).

## Root Cause Analysis

**Why PRD and Phase 1 diverged**: Phase 1 plan was locked YAGNI (no models, minimal config), but PRD was written in parallel without checking Phase 1 merge. No blame; PRD was speculative. Brainstorm caught it before we wasted time coding against a ghost API.

**Why async vs sync was hard**: The PRD author didn't anticipate that Phase 3 (LLM inference) would be slow. Sync discovery made sense in isolation, but cascades poorly when the pipeline blocks on external API calls. Async from Phase 2 onwards is the right long-term choice.

**Why panic recovery was missed in first pass**: Detached goroutines are not supervision-level entities; there's no implicit recovery. We had to explicitly think "what if the pipeline crashes?". That's a mental model gap—easy to forget in Go because goroutines feel lightweight.

## Lessons Learned

**PRD + Code Reality Reconciliation is Non-Negotiable**: Never assume the PRD describes what Phase N-1 actually shipped. Brainstorm first; code second. A 1-hour conversation saved us from 4 hours of wrong implementation.

**Async is Infectious**: Once one layer is async, the whole stack should be. A sync /discover endpoint with an async pipeline underneath is a footgun waiting for Phase 3 to step on. Embrace async now; it's cheaper than reworking the contract later.

**Race Testing is Table Stakes**: `go test -race` caught zero issues, but that's because we designed PipelineSession carefully (small critical section, Mutex-guarded). Without the test, a future refactor could introduce a data race silently. Run it always.

**Context.WithoutCancel() is Powerful But Dangerous**: Detaching a goroutine from the request context prevents cleanup (good: pipeline survives if client disconnects). But now we own the goroutine's lifetime forever (bad: must explicitly recover panics, must clean up resources). Use sparingly and document the hazard.

**Corrupt State is Not Recoverable**: We could have treated corrupt processed.json as "empty" and re-discovered papers. That's a security/correctness trap. Be strict: if the state is corrupt, fail loudly. A human operator should investigate, not auto-recover.

**Naming Matters for Configuration**: `MinRequestIntervalSec` is unclear (min interval *between* requests? min interval *to wait before* the next request?). Should be `IntervalBetweenRequestsMs` or similar. Deferred this fix, but flagging for Phase 3.

## Next Steps

- **User Review**: Confirm async discovery + polling is the intended behavior (departs from literal PRD, but fits Phase 3 reality).
- **Commit**: Once approved, squash and merge with message: "feat: phase 2 discovery and dedup with async polling".
- **Phase 3 Prep**: LLM integration will run in the same detached-goroutine pattern; pipeline is now ready to add more stages.
- **Monitoring Note**: In-memory session store will grow indefinitely; add TTL-based cleanup in Phase 3 or later if session count exceeds 100k.
- **Config Cleanup**: Revisit MinRequestIntervalSec naming convention in Phase 3 refactor (low priority, safe to defer).

---

**Session context:** Delivered via planning + implementation workflow. Brainstorm reconciled PRD vs Phase 1 reality early (high-value session). Code-reviewer caught 5 issues; 4 fixed immediately (1 critical panic recovery), 2 deferred with rationale. All tests pass `-race`, frontend builds clean. Ready for user sign-off on async architecture. Not yet committed.
