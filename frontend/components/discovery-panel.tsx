"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import {
  fetchResources,
  fetchResult,
  fetchStatus,
  retryPipeline,
  selectPaper,
  triggerDiscovery,
} from "@/lib/api";
import type { ResourceDescriptor } from "@/lib/types";
import { useEventSource } from "@/lib/use-event-source";
import { CandidateList } from "./candidate-list";
import { ContextWarningBanner } from "./context-warning";
import { DynamicRequestForm } from "./dynamic-request-form";
import { ErrorBanner } from "./error-banner";
import { ProgressIndicator } from "./progress-indicator";
import { ResourcePicker } from "./resource-picker";
import { ResultPanel } from "./result-panel";
import { RunTimeline } from "./run-timeline";
import { TriggerButton } from "./trigger-button";

// defaultsFor seeds a values map from a resource's field defaults (empty string
// when a field has no default), so the form starts on the backend-declared state.
function defaultsFor(r: ResourceDescriptor): Record<string, string> {
  const out: Record<string, string> = {};
  for (const f of r.fields) {
    out[f.name] = f.default ?? "";
  }
  return out;
}

// DiscoveryPanel owns the discovery + selection flow: trigger -> poll -> pick ->
// extract. It holds the current session id and the chosen paper id; all pipeline
// state comes from the polled status.
export function DiscoveryPanel() {
  const [sessionId, setSessionId] = useState<string | null>(null);
  // The selected resource + its validated field values driving the next run. Both
  // are seeded from the backend descriptor (id = first resource, values = field
  // defaults) so the UI never diverges from the backend's declared defaults; the
  // user then edits them. Kept here so `start()` reads the current selection.
  const [resourceId, setResourceId] = useState("");
  const [values, setValues] = useState<Record<string, string>>({});
  // Guards the one-time seed below so setting state during render can't loop.
  const [seeded, setSeeded] = useState(false);
  // The paper the user picked (null = none yet / re-pick reset).
  const [selectedId, setSelectedId] = useState<string | null>(null);
  // Whether we have ever committed a selection — gates the re-pick detector so
  // the initial "selection" stage (which has no notice) does not false-trigger.
  const hasSelected = useRef(false);
  const queryClient = useQueryClient();

  // Load the resource catalog once; it rarely changes within a session.
  const { data: resources } = useQuery({
    queryKey: ["resources"],
    queryFn: fetchResources,
    staleTime: Infinity,
  });

  // Seed the first resource + its default values once the catalog loads. This is
  // the React-sanctioned "initialize state from async data" pattern: set state
  // DURING render behind a one-shot guard (not in an effect), so there is no
  // extra commit/flash and no cascading-render lint violation.
  if (!seeded && resources && resources.length > 0) {
    setSeeded(true);
    setResourceId(resources[0].id);
    setValues(defaultsFor(resources[0]));
  }

  const current = resources?.find((r) => r.id === resourceId);

  // Switching resources resets the form to the new resource's defaults AND clears
  // any in-flight session, so a stale session from the previous resource can't
  // render candidates against the new form.
  const onResourceChange = (id: string) => {
    const next = resources?.find((r) => r.id === id);
    setResourceId(id);
    setValues(next ? defaultsFor(next) : {});
    setSessionId(null);
    setSelectedId(null);
    hasSelected.current = false;
  };

  const onFieldChange = (name: string, value: string) =>
    setValues((v) => ({ ...v, [name]: value }));

  const trigger = useMutation({
    mutationFn: () => triggerDiscovery(resourceId, values),
    onSuccess: ({ session_id }) => {
      setSessionId(session_id);
      setSelectedId(null);
      hasSelected.current = false;
    },
  });

  const select = useMutation({
    mutationFn: (paperId: string) => selectPaper(sessionId as string, paperId),
    // Optimistically disable the cards on click; on failure, re-enable so the
    // user can retry the pick.
    onMutate: (paperId: string) => setSelectedId(paperId),
    onSuccess: () => {
      hasSelected.current = true;
      // The status query pauses (refetchInterval:false) at the "selection" stage.
      // The backend sets "extracting" synchronously before responding, so force
      // a refetch now to re-arm polling; once it reads "extracting", the 2s
      // interval resumes and carries the flow through extraction / 404 re-pick.
      queryClient.invalidateQueries({ queryKey: ["status", sessionId] });
    },
    onError: () => setSelectedId(null),
  });

  // Retry resumes a failed pipeline IN PLACE — the backend routes by the failed
  // stage and preserves the paper pick, so we do NOT call `start` (which would
  // re-run discovery and drop the selection). On success the backend has already
  // set the resume stage, so invalidating re-arms the paused status poll (the
  // same loop, not a second one). If the session is gone (404 → onError), fall
  // back to a fresh discovery run.
  const retry = useMutation({
    mutationFn: () => retryPipeline(sessionId as string),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["status", sessionId] });
    },
    onError: () => trigger.mutate(),
  });

  // Live run timeline (Phase 7): SSE stream straight from the backend, running
  // alongside the existing status poll. Purely additive — the poll still drives
  // the stage UI; the timeline tells the full story.
  const { events, done, error: sseError } = useEventSource(sessionId);

  const { data: status } = useQuery({
    queryKey: ["status", sessionId],
    queryFn: () => fetchStatus(sessionId as string),
    enabled: !!sessionId,
    // Denylist: poll every 2s until a terminal stage. "extracting"/"generating"/
    // "writing" are NOT terminal, so polling continues through them; "complete"
    // is terminal alongside "selection"/"failed".
    refetchInterval: (query) => {
      const stage = query.state.data?.stage;
      return stage === "selection" || stage === "failed" || stage === "complete"
        ? false
        : 2000;
    },
  });

  // Once the pipeline is complete, fetch the finished note once (no polling).
  const {
    data: result,
    isError: resultFailed,
    refetch: refetchResult,
  } = useQuery({
    queryKey: ["result", sessionId],
    queryFn: () => fetchResult(sessionId as string),
    enabled: status?.stage === "complete",
  });

  // Re-pick detection: after we have selected, a return to "selection" WITH a
  // notice means the fetch failed recoverably (e.g. HTML 404) — clear the
  // selection so the cards re-enable and the notice renders.
  useEffect(() => {
    if (status?.stage === "selection" && hasSelected.current && status.notice) {
      setSelectedId(null);
    }
  }, [status?.stage, status?.notice]);

  // Running once we have a session but haven't reached a terminal stage yet.
  const polling =
    !!sessionId &&
    status?.stage !== "selection" &&
    status?.stage !== "failed" &&
    status?.stage !== "complete";
  const isLoading = trigger.isPending || polling;

  const start = () => trigger.mutate();

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-col gap-4">
        <ResourcePicker
          resources={resources ?? []}
          resourceId={resourceId}
          onChange={onResourceChange}
          disabled={isLoading}
        />
        {current && (
          <DynamicRequestForm
            fields={current.fields}
            values={values}
            onChange={onFieldChange}
            disabled={isLoading}
          />
        )}
        <TriggerButton onClick={start} loading={isLoading} />
      </div>

      {/* Trigger request itself failed (e.g. backend unreachable). */}
      {trigger.isError && (
        <ErrorBanner
          message="Could not start discovery. Is the backend running?"
          recoverable
          onRetry={start}
        />
      )}

      {sessionId && status && status.stage !== "failed" && (
        <ProgressIndicator status={status} />
      )}

      {/* Live, ordered event timeline for the current run (Phase 7). */}
      {sessionId && events.length > 0 && (
        <RunTimeline events={events} live={!done} error={sseError && !done} />
      )}

      {/* Non-blocking over-limit advisory (F4): the pipeline keeps running. */}
      {status?.contextWarning && (
        <ContextWarningBanner warning={status.contextWarning} />
      )}

      {status?.stage === "failed" && (
        <ErrorBanner
          message={status.error ?? "Discovery failed."}
          recoverable={status.recoverable}
          onRetry={() => retry.mutate()}
        />
      )}

      {status?.stage === "selection" && (
        <CandidateList
          candidates={status.candidates ?? []}
          notice={status.notice}
          selectedId={selectedId}
          onSelect={(id) => select.mutate(id)}
          sessionId={sessionId}
        />
      )}

      {status?.stage === "complete" && result && <ResultPanel result={result} />}

      {/* Pipeline finished but the note fetch failed (e.g. backend restarted). */}
      {status?.stage === "complete" && resultFailed && (
        <ErrorBanner
          message="The explainer was generated but could not be loaded. It is saved in your vault; you can retry loading the preview."
          recoverable
          onRetry={() => refetchResult()}
        />
      )}
    </div>
  );
}
