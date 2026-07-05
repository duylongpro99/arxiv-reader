# Phase 04 — Frontend Integration (F2 UI, F3, F4, F5)

**Priority:** High · **Status:** ✅ complete · **Depends on:** 02, 03 (backend DTO shape frozen)

Wire the new backend contract into the Next.js UI. Field names are camelCase and MUST match
the Go json tags (see `frontend/lib/types.ts` contract note).

## Context
- `frontend/lib/types.ts` — `PipelineStatus`, `ResultResponse` (extend, don't rename).
- `frontend/components/error-banner.tsx` — retry button already exists (shown when `recoverable`).
- `frontend/components/discovery-panel.tsx` — 3 `ErrorBanner` sites; today `onRetry={start}` (restarts discovery).
- `frontend/components/progress-indicator.tsx` — stage labels.
- `frontend/components/result-panel.tsx` — success state (token usage lives here).
- `frontend/app/api/` — existing routes: `trigger`, `select`, `status`, `result`. Add `retry`.
- `frontend/lib/api.ts` / `backend.ts` — client + backend base URL helpers.

## Implementation steps

### 1. Types (`lib/types.ts`)
- `PipelineStatus += errorAction?: string; arxivRetryCount?: number; contextWarning?: ContextWarning`.
- `ResultResponse += inputTokens?: number; outputTokens?: number; estimatedCostUSD?: number; costKnown?: boolean`.
- New `interface ContextWarning { estimatedTokens; modelLimit; model; suggestion }`.

### 2. `/api/retry` route (`app/api/retry/route.ts`)
- Mirror existing `select` route: `POST { session_id }` → `POST {backend}/retry/{session_id}` → `{ session_id }`. Reuse `backend.ts` base URL + error handling pattern.

### 3. Retry wiring (`discovery-panel.tsx`)
- For a **failed pipeline** session (mid-pipeline failure), `onRetry` calls `/api/retry` then resumes polling — do NOT call `start` (which re-runs discovery and drops the pick).
- Keep `start` only for the discovery-stage failure banner (the failed stage was discovery; `/retry` also handles this, so prefer routing everything through `/api/retry` for consistency — the backend routes by `failedStage`).
- The `/result` fetch-failed banner (backend restarted) keeps `refetchResult()`.

### 4. Context-warning banner (`progress-indicator.tsx` or a small `context-warning.tsx`)
- When `status.contextWarning` present, show a non-blocking warning:
  `⚠️ This paper (~{estimatedTokens} tokens) may exceed {model}'s limit. Proceeding — {suggestion}`.

### 5. arXiv retry label (`progress-indicator.tsx`)
- `if (stage === 'discovery' && (arxivRetryCount ?? 0) > 0) → "Connecting to arXiv (retry {n}/3)…"`.

### 6. Cost display (`result-panel.tsx`)
- Extend the existing token-usage line:
  `Tokens: {tokensUsed.toLocaleString()}` + if `costKnown`: ` · ~${estimatedCostUSD.toFixed(3)} estimated` with a muted note `(approximate — check your provider dashboard)`.

## Related code files
- Create: `app/api/retry/route.ts`, optionally `components/context-warning.tsx`.
- Modify: `lib/types.ts`, `components/discovery-panel.tsx`, `components/progress-indicator.tsx`, `components/result-panel.tsx`, possibly `lib/api.ts`.

## Todo
- [x] Extend types (match Go json tags exactly)
- [x] `/api/retry` route
- [x] Retry button → `/api/retry` (preserve selection), resume polling
- [x] Context-warning banner (non-blocking)
- [x] arXiv retry progress label
- [x] Cost line in success state
- [x] `npm run build` (frontend) clean; `npm run lint`

## Success criteria
- Clicking Retry on a generation/vault failure resumes without re-selecting a paper.
- Cost shows only when `costKnown`; hidden otherwise.
- Context warning renders and pipeline continues (non-blocking).
- Retry counter label appears during arXiv 429 backoff.

## Risks
- **Contract drift:** camelCase field names must match Go tags — verify against `dto.go` after Phase 02/03. A mismatch silently drops the field (`omitempty`).
- **Double-polling:** ensure retry resumes the SAME poll loop, not a second one.
