"use client";

import { useMutation, useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { fetchStatus, triggerDiscovery } from "@/lib/api";
import { CandidateList } from "./candidate-list";
import { ErrorBanner } from "./error-banner";
import { ProgressIndicator } from "./progress-indicator";
import { TriggerButton } from "./trigger-button";

// DiscoveryPanel owns the discovery flow: trigger -> poll -> render. It holds
// only the current session id; all pipeline state comes from the polled status.
export function DiscoveryPanel() {
  const [sessionId, setSessionId] = useState<string | null>(null);

  const trigger = useMutation({
    mutationFn: triggerDiscovery,
    onSuccess: ({ session_id }) => setSessionId(session_id),
  });

  const { data: status } = useQuery({
    queryKey: ["status", sessionId],
    queryFn: () => fetchStatus(sessionId as string),
    enabled: !!sessionId,
    // Poll every 2s until a terminal stage; then stop (PRD F5).
    refetchInterval: (query) => {
      const stage = query.state.data?.stage;
      return stage === "selection" || stage === "failed" ? false : 2000;
    },
  });

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
        />
      )}
    </div>
  );
}
