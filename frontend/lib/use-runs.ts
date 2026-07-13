"use client";

import { useQuery } from "@tanstack/react-query";
import { getRunContent } from "./api";
import type { RunContent, RunDetail, RunsList } from "./types";

// HistoryUnavailableError marks the backend's 503 (no database configured), so
// the UI can show a "history disabled" hint rather than a generic error.
export class HistoryUnavailableError extends Error {}

// useRuns fetches the run-history list (newest first) via the /api/runs proxy.
export function useRuns() {
  return useQuery<RunsList>({
    queryKey: ["runs"],
    queryFn: async () => {
      const res = await fetch("/api/runs?limit=100");
      if (res.status === 503) {
        throw new HistoryUnavailableError(
          "Run history is unavailable — start Postgres and set DATABASE_URL.",
        );
      }
      if (!res.ok) throw new Error(`Failed to load runs (HTTP ${res.status})`);
      return res.json();
    },
  });
}

// useRun fetches one past run's header + full timeline via /api/runs/:id.
// Disabled until an id is provided.
export function useRun(id: string | null) {
  return useQuery<RunDetail>({
    queryKey: ["run", id],
    enabled: !!id,
    queryFn: async () => {
      const res = await fetch(`/api/runs/${encodeURIComponent(id as string)}`);
      if (res.status === 404) throw new Error("This run was not found.");
      if (res.status === 503) {
        throw new HistoryUnavailableError(
          "Run history is unavailable — start Postgres and set DATABASE_URL.",
        );
      }
      if (!res.ok) throw new Error(`Failed to load run (HTTP ${res.status})`);
      return res.json();
    },
  });
}

// useRunContent fetches a past run's generated note markdown via
// getRunContent, mirroring useRun's 503 handling: a non-OK response with
// status 503 (no DB configured) is rethrown as HistoryUnavailableError so the
// page can show the same "history disabled" hint instead of a generic error.
export function useRunContent(id: string | null) {
  return useQuery<RunContent>({
    queryKey: ["run-content", id],
    enabled: !!id,
    queryFn: async () => {
      try {
        return await getRunContent(id as string);
      } catch (err) {
        const status = (err as { status?: number }).status;
        if (status === 503) {
          throw new HistoryUnavailableError(
            "Run history is unavailable — start Postgres and set DATABASE_URL.",
          );
        }
        throw err;
      }
    },
  });
}
