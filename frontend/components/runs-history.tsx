"use client";

import Link from "next/link";
import type { RunStatus, RunSummary } from "@/lib/types";
import { HistoryUnavailableError, useRuns } from "@/lib/use-runs";

// Outcome badge color per run status.
const STATUS_BADGE: Record<RunStatus, string> = {
  complete: "bg-green-100 text-green-800 dark:bg-green-950 dark:text-green-300",
  failed: "bg-red-100 text-red-800 dark:bg-red-950 dark:text-red-300",
  running: "bg-blue-100 text-blue-800 dark:bg-blue-950 dark:text-blue-300",
  recovered: "bg-amber-100 text-amber-800 dark:bg-amber-950 dark:text-amber-300",
};

function formatDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleString([], { dateStyle: "medium", timeStyle: "short" });
}

export function RunsHistory() {
  const { data, isLoading, error } = useRuns();

  if (isLoading) {
    return <p className="text-sm text-gray-500">Loading run history…</p>;
  }
  if (error instanceof HistoryUnavailableError) {
    return (
      <p className="rounded-lg bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:bg-amber-950 dark:text-amber-200">
        {error.message}
      </p>
    );
  }
  if (error) {
    return (
      <p className="rounded-lg bg-red-50 px-4 py-3 text-sm text-red-800 dark:bg-red-950 dark:text-red-200">
        Could not load run history. Is the backend running?
      </p>
    );
  }
  if (!data || data.runs.length === 0) {
    return <p className="text-sm text-gray-500">No runs yet. Trigger a discovery to get started.</p>;
  }

  return (
    <ul className="flex flex-col gap-2">
      {data.runs.map((run) => (
        <li key={run.id}>
          <RunRow run={run} />
        </li>
      ))}
    </ul>
  );
}

function RunRow({ run }: { run: RunSummary }) {
  const badge = STATUS_BADGE[run.status] ?? STATUS_BADGE.running;
  return (
    <Link
      href={`/runs/${run.id}`}
      className="flex items-center justify-between gap-4 rounded-lg border border-gray-200 bg-white px-4 py-3 hover:border-gray-300 hover:bg-gray-50 dark:border-gray-700 dark:bg-gray-900 dark:hover:bg-gray-800"
    >
      <div className="min-w-0">
        <p className="truncate text-sm font-medium text-gray-800 dark:text-gray-100">
          {run.paperTitle || run.paperId || "(no paper selected)"}
        </p>
        <p className="text-xs text-gray-400">{formatDate(run.startedAt)}</p>
      </div>
      <div className="flex shrink-0 items-center gap-3">
        {run.estCostUsd != null && (
          <span className="font-mono text-xs text-gray-500 dark:text-gray-400">
            ~${run.estCostUsd.toFixed(2)}
          </span>
        )}
        <span className={`rounded-full px-2 py-0.5 text-xs font-medium ${badge}`}>
          {run.status}
        </span>
      </div>
    </Link>
  );
}
