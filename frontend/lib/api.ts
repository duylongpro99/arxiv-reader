// Client-side API helpers. These call the Next.js proxy routes (same origin),
// never the Go backend directly — the backend address stays server-side.

import type {
  PipelineStatus,
  ResultResponse,
  RetryResponse,
  SelectResponse,
  TriggerResponse,
} from "./types";

// triggerDiscovery starts a discovery run and returns the new session id.
export async function triggerDiscovery(): Promise<TriggerResponse> {
  const res = await fetch("/api/trigger", { method: "POST" });
  if (!res.ok) {
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
