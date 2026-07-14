# Phase 8: Reasoning History + Pagination — Fully-Plumbed Fields and Silent Sanitizer Sabotage

**Date**: 2026-07-13
**Severity**: Medium (shipped successfully, but near-misses deserve documenting)
**Component**: Agent tracing, history content recovery, arXiv pagination
**Status**: Resolved

## What Happened

Phase 8 shipped three interconnected features: full agent reasoning traces (systemPrompt/userPrompt/rawResponse), content re-hydration for persisted history notes, and arXiv pagination within session context. All passed tests and reviews. Two adversarial code reviews caught real bugs before merge. No DB schema changes needed.

## The Brutal Truth

The tracing feature almost shipped broken because a field existed in the type system end-to-end but was never actually written anywhere. We spent development time on everything except the most critical part: verifying data flows into the database. Only discovered during review when someone asked "does the field actually get populated?" The answer was no. Second shock: a shared sanitizer that truncates ALL strings to 500 chars nearly gutted a feature that needed 100KB payloads. Both are humbling reminders that infrastructure assumptions are invisible until they aren't.

## Technical Details

**Feature A — Reasoning Traces**
- `run_events.payload_full` existed as a JSONB column but was never populated. The tracing subsystem had flags and types but no emit sites actually captured and wrote data.
- Fix: Added LLMTrace struct capture in explainer/reviewer agents with systemPrompt, userPrompt, rawResponse fields.
- Discovery: `tracing/scrub.go` applies a global previewCap=500 truncation to ALL strings before writing. This silently destroyed payloads. Added distinct payloadCap=100000 (sanitization pass unchanged, only length backstop differs).
- Human-readable decision narratives replaced cryptic "reviewIterations 1" on accept/revise/max-iterations/parse-failure events.

**Feature B — History Content Recovery**
- Content not stored in DB; only a breadcrumb path in tool.vaultwriter.completed event's Summary["path"].
- GET /runs/{id}/content reads that path and recovers markdown from Obsidian vault.
- Path-traversal guarded. Consolidated duplicated read/write validators into single exported `tools.ValidateWithinVault`.
- Missing file returns `available:false` with HTTP 200 (never 500).

**Feature C — arXiv Pagination**
- `/process` endpoint looks papers up inside session Candidates, so pagination HAD to extend the same session (POST /discover/{sessionId}/more), not a decoupled browse endpoint.
- Atomic `ConsumeNextStart` cursor prevents TOCTOU between concurrent /more calls.
- Added `FetchPapersFrom(start)` while keeping `FetchPapers` delegating to `start=0` for backward compatibility.

## What We Tried

- Automated testing of the full_payloads flag: caught that the field existed but was empty.
- Adversarial code review #1 (backend): caught a real race where /more landing before async discovery completes gets its page silently overwritten by Complete(). Fixed with StageSelection guard returning 409.
- Adversarial code review #2 (frontend): caught misleading empty-state UX (never-generated vs moved/deleted note).

## Root Cause Analysis

**Tracing Field Never Populated**: We followed the type system (`payload_full: json.RawMessage`) but never verified that code actually writes to it. The field existed in migrations and DTO; the flag existed in config. But zero emit sites captured LLMTrace data. This is a plumbing-vs-water problem — you can have perfect pipes with nothing flowing through them.

**Scrubber Truncation**: We added a global defense-in-depth sanitizer to limit string lengths for safety (prevent DB bloat, log noise). But we didn't audit existing features that depended on large payloads. The sanitizer ran BEFORE feature development, so the cap was invisible until the feature tried to exceed it.

**Session-Coupling in Pagination**: The /process endpoint's design — papers are looked up inside a session's Candidates list — forced pagination to extend the same session. This created architectural constraints that weren't obvious until we had to route the feature.

## Lessons Learned

1. **Verify Data Actually Flows**: Type existence is not the same as data presence. When plumbing a feature end-to-end, add a checkpoint: log or assert that the field is populated at emit time. A single log statement ("payload_full size: X bytes") prevents shipping dead code.

2. **Audit Shared Sanitizers Before Relying on New Data**: Any global string truncation, redaction, or filtering applies to features that come later. Before committing to a large-payload feature, check what preprocessing happens. Document exceptions (payload_cap vs preview_cap).

3. **Pagination Architecture Locked by Lookup Scope**: If /process requires papers to come from a session's Candidates, pagination cannot be a stateless browse operation. This constraint should surface early in design discussions. Session-scoped pagination is harder than global pagination.

4. **Missing Content ≠ Error**: When persisting only metadata (the file path, not the content), design recovery to fail gracefully. HTTP 200 + `available:false` is better than 500 — it's not a server error, just missing source data.

## Next Steps

- **Audit other emit sites** for fully-typed but unpopulated fields. Run a grep for fields marked `omitempty` in json tags but never assigned in code.
- **Document sanitizer exceptions** in tracing/scrub.go. Add a comment explaining why payloadCap differs from previewCap and what features depend on it.
- **Add CI checks** for payload_full population: emit a test run with tracing.full_payloads enabled and assert that run_events.payload_full is non-empty.
- **Monitor**: If pagination sees concurrent /more requests on same session, StageSelection guard should log. If it logs, we have a concurrency pattern worth documenting.

---

**Files Modified**: backend/{explainer,reviewer}/agents.go, tracing/scrub.go, tools/vault.go, routes/discover.go, routes/process.go, frontend/HistoryDetail.tsx, schema (JSONB existing column, no migration needed)

**Test Status**: All backend build/vet/test + frontend tsc/build + adversarial reviews green.
