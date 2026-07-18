// Client-side API helpers. These call the Next.js proxy routes (same origin),
// never the Go backend directly — the backend address stays server-side.

import type {
  CategoriesResponse,
  DiscoverMoreResult,
  PipelineStatus,
  ResultResponse,
  RetryResponse,
  RunContent,
  SelectResponse,
  TriggerResponse,
} from "./types";

// fetchCategories loads the cs.* catalog + configured default for the picker.
export async function fetchCategories(): Promise<CategoriesResponse> {
  const res = await fetch("/api/categories");
  if (!res.ok) {
    throw new Error(`Failed to load categories (HTTP ${res.status})`);
  }
  return res.json();
}

// triggerDiscovery starts a discovery run for the chosen category (+ optional
// keywords) and returns the new session id. category is required; terms is
// optional free-text the backend sanitizes and AND-s onto the category filter.
export async function triggerDiscovery(
  category: string,
  terms?: string,
): Promise<TriggerResponse> {
  const res = await fetch("/api/trigger", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ category, terms: terms ?? "" }),
  });
  if (!res.ok) {
    // A 400 means the category was not recognised — surface it clearly.
    throw new Error(`Failed to start discovery (HTTP ${res.status})`);
  }
  return res.json();
}

// selectPaper submits the chosen paper for extraction. The backend keeps the
// same session id and moves it to the "extracting" stage; the panel keeps polling.
export async function selectPaper(
  sessionId: string,
  paperId: string,
): Promise<SelectResponse> {
  const res = await fetch("/api/select", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ session_id: sessionId, paper_id: paperId }),
  });
  if (!res.ok) {
    throw new Error(`Failed to select paper (HTTP ${res.status})`);
  }
  return res.json();
}

// retryPipeline resumes a failed, recoverable pipeline from the stage that
// failed WITHOUT re-selecting a paper (the backend routes by the failed stage
// and skips cached segments). Throws on a non-OK response — the caller (panel)
// falls back to a fresh discovery when the session is gone (404).
export async function retryPipeline(sessionId: string): Promise<RetryResponse> {
  const res = await fetch("/api/retry", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ session_id: sessionId }),
  });
  if (!res.ok) {
    throw new Error(`Failed to retry (HTTP ${res.status})`);
  }
  return res.json();
}

// fetchStatus polls the pipeline status for a session. A 404 (e.g. the backend
// restarted and lost the in-memory session) is surfaced as a failed status so
// the UI degrades gracefully instead of throwing on an unknown session.
export async function fetchStatus(sessionId: string): Promise<PipelineStatus> {
  const res = await fetch(`/api/status?sessionId=${encodeURIComponent(sessionId)}`);
  if (res.status === 404) {
    return {
      stage: "failed",
      error: "This discovery session expired. Please try again.",
      recoverable: true,
    };
  }
  if (!res.ok) {
    throw new Error(`Failed to fetch status (HTTP ${res.status})`);
  }
  return res.json();
}

// fetchResult retrieves the finished explainer once the pipeline is complete.
// Only called when the status stage is "complete" (the backend 404s otherwise);
// a non-OK response throws, like selectPaper.
export async function fetchResult(sessionId: string): Promise<ResultResponse> {
  const res = await fetch(
    `/api/result?sessionId=${encodeURIComponent(sessionId)}`,
  );
  if (!res.ok) {
    throw new Error(`Failed to fetch result (HTTP ${res.status})`);
  }
  return res.json();
}

// getRunContent retrieves a past run's generated note markdown via the
// /api/runs/:id/content proxy. `available:false` is still a 200 (no note to
// show, not an error); non-OK responses (e.g. 503 when history is unavailable)
// throw with the HTTP status attached so callers can special-case it, mirroring
// how useRun/useRuns detect the 503 "history disabled" case.
export async function getRunContent(id: string): Promise<RunContent> {
  const res = await fetch(`/api/runs/${encodeURIComponent(id)}/content`);
  if (!res.ok) {
    const err = new Error(`Failed to fetch run content (HTTP ${res.status})`) as Error & {
      status?: number;
    };
    err.status = res.status;
    throw err;
  }
  return res.json();
}

// fetchMoreCandidates asks the backend for the next page of candidate papers for
// an existing discovery session ("load more" on the candidate list). The call is
// synchronous — the response already contains the new page, no polling needed.
// Throws on a non-OK response (e.g. 404 when the session expired).
export async function fetchMoreCandidates(
  sessionId: string,
): Promise<DiscoverMoreResult> {
  const res = await fetch(
    `/api/discover/${encodeURIComponent(sessionId)}/more`,
    { method: "POST" },
  );
  if (!res.ok) {
    // 404 is the contract's "session expired/evicted" case — give an actionable
    // message rather than a bare HTTP code the user can't act on.
    if (res.status === 404) {
      throw new Error("This search session expired — start a new search to load more.");
    }
    throw new Error(`Failed to load more papers (HTTP ${res.status})`);
  }
  return res.json();
}
