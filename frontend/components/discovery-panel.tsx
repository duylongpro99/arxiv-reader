"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { fetchStatus, selectPaper, triggerDiscovery } from "@/lib/api";
import { CandidateList } from "./candidate-list";
import { ErrorBanner } from "./error-banner";
import { ProgressIndicator } from "./progress-indicator";
import { TriggerButton } from "./trigger-button";

// DiscoveryPanel owns the discovery + selection flow: trigger -> poll -> pick ->
// extract. It holds the current session id and the chosen paper id; all pipeline
// state comes from the polled status.
export function DiscoveryPanel() {
  const [sessionId, setSessionId] = useState<string | null>(null);
  // The paper the user picked (null = none yet / re-pick reset).
  const [selectedId, setSelectedId] = useState<string | null>(null);
  // Whether we have ever committed a selection — gates the re-pick detector so
  // the initial "selection" stage (which has no notice) does not false-trigger.
  const hasSelected = useRef(false);
  const queryClient = useQueryClient();

  const trigger = useMutation({
    mutationFn: triggerDiscovery,
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

  const { data: status } = useQuery({
    queryKey: ["status", sessionId],
    queryFn: () => fetchStatus(sessionId as string),
    enabled: !!sessionId,
    // Denylist: poll every 2s until a terminal stage. "extracting" is NOT
    // terminal, so polling continues through it (PRD F5).
    refetchInterval: (query) => {
      const stage = query.state.data?.stage;
      return stage === "selection" || stage === "failed" ? false : 2000;
    },
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
    !!sessionId && status?.stage !== "selection" && status?.stage !== "failed";
  const isLoading = trigger.isPending || polling;

  const start = () => trigger.mutate();

  return (
    <div className="flex flex-col gap-6">
      <TriggerButton onClick={start} loading={isLoading} />

      {/* Trigger request itself failed (e.g. backend unreachable). */}
      {trigger.isError && (
        <ErrorBanner
          message="Could not start discovery. Is the backend running?"
          recoverable
          onRetry={start}
        />
      )}

      {sessionId && status && status.stage !== "failed" && (
        <ProgressIndicator stage={status.stage} />
      )}

      {status?.stage === "failed" && (
        <ErrorBanner
          message={status.error ?? "Discovery failed."}
          recoverable={status.recoverable}
          onRetry={start}
        />
      )}

      {status?.stage === "selection" && (
        <CandidateList
          candidates={status.candidates ?? []}
          notice={status.notice}
          selectedId={selectedId}
          onSelect={(id) => select.mutate(id)}
        />
      )}
    </div>
  );
}
