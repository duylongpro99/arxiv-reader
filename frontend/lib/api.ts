// Client-side API helpers. These call the Next.js proxy routes (same origin),
// never the Go backend directly — the backend address stays server-side.

import type {
  ChannelsResponse,
  DiscoverMoreResult,
  PatchPublicationRequest,
  PipelineStatus,
  Publication,
  PublicationsResponse,
  ResourceDescriptor,
  ResultResponse,
  RetryResponse,
  RunContent,
  SelectResponse,
  TriggerResponse,
} from "./types";

// fetchResources loads the registered resources + their field schemas so the UI
// can render a resource picker and a dynamic request form.
export async function fetchResources(): Promise<ResourceDescriptor[]> {
  const res = await fetch("/api/resources");
  if (!res.ok) {
    throw new Error(`Failed to load resources (HTTP ${res.status})`);
  }
  return res.json();
}

// triggerDiscovery starts a discovery run for the chosen resource with its
// validated field values and returns the new session id. The backend validates
// values against the resource schema (whitelist selects, sanitize text).
export async function triggerDiscovery(
  resourceId: string,
  values: Record<string, string>,
): Promise<TriggerResponse> {
  const res = await fetch("/api/trigger", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ resourceId, values }),
  });
  if (!res.ok) {
    // A 400 means a value failed the resource's schema — surface it clearly.
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

// withStatus attaches the HTTP status to a thrown Error so callers (the
// use-publications hooks) can special-case 503 (publishing unavailable, no
// DB configured) the same way getRunContent/useRunContent already do.
function withStatus(message: string, status: number): Error & { status?: number } {
  const err = new Error(message) as Error & { status?: number };
  err.status = status;
  return err;
}

// --- Phase 8 channel-publishing API helpers (mirror the /channels,
// /runs/:id/publications, /publications/:pid[/publish] proxy routes) ---

// getChannels lists every enabled, resolvable publish channel.
export async function getChannels(): Promise<ChannelsResponse> {
  const res = await fetch("/api/channels");
  if (!res.ok) {
    throw withStatus(`Failed to load channels (HTTP ${res.status})`, res.status);
  }
  return res.json();
}

// getRunPublications lists the publish drafts/attempts already created for a run.
export async function getRunPublications(runId: string): Promise<PublicationsResponse> {
  const res = await fetch(`/api/runs/${encodeURIComponent(runId)}/publications`);
  if (!res.ok) {
    throw withStatus(`Failed to load publications (HTTP ${res.status})`, res.status);
  }
  return res.json();
}

// generateDrafts creates one draft publication per requested channel id for a run.
export async function generateDrafts(
  runId: string,
  channels: string[],
): Promise<PublicationsResponse> {
  const res = await fetch(`/api/runs/${encodeURIComponent(runId)}/publications`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ channels }),
  });
  if (!res.ok) {
    throw withStatus(`Failed to generate drafts (HTTP ${res.status})`, res.status);
  }
  return res.json();
}

// patchPublication applies a partial edit (title/content) and/or approves a
// draft. Only the fields present in `body` are sent — the backend treats
// omitted fields as untouched.
export async function patchPublication(
  pid: string,
  body: PatchPublicationRequest,
): Promise<Publication> {
  const res = await fetch(`/api/publications/${encodeURIComponent(pid)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    throw withStatus(`Failed to update draft (HTTP ${res.status})`, res.status);
  }
  return res.json();
}

// publishPublication sends an approved draft to its channel. A 409 (already
// published) and 502 (channel error) are relayed as-is with the status
// attached so the draft card can show the scrubbed backend error + retry.
export async function publishPublication(pid: string): Promise<Publication> {
  const res = await fetch(`/api/publications/${encodeURIComponent(pid)}/publish`, {
    method: "POST",
  });
  if (!res.ok) {
    let message = `Failed to publish (HTTP ${res.status})`;
    try {
      const body = await res.json();
      if (typeof body?.error === "string" && body.error) message = body.error;
    } catch {
      // Non-JSON error body — fall back to the generic message above.
    }
    throw withStatus(message, res.status);
  }
  return res.json();
}
