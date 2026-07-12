"use client";

import Link from "next/link";
import type { RunStatus, RunSummary } from "@/lib/types";
import { HistoryUnavailableError, useRuns } from "@/lib/use-runs";

// Outcome badge color per run status.
const STATUS_BADGE: Record<RunStatus, string> = {
  complete: "border border-ok/30 bg-ok-bg text-ok",
  failed: "border border-err/30 bg-err-bg text-err",
  running: "border border-accent/30 bg-accent-bg text-accent",
  recovered: "border border-warn/30 bg-warn-bg text-warn",
};

function formatDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleString([], { dateStyle: "medium", timeStyle: "short" });
}

export function RunsHistory() {
  const { data, isLoading, error } = useRuns();

  if (isLoading) {
    return <p className="font-mono text-sm text-muted">Loading run history…</p>;
  }
  if (error instanceof HistoryUnavailableError) {
    return (
      <p className="rounded-xl border border-warn/30 bg-warn-bg px-4 py-3 text-sm text-warn">
        {error.message}
      </p>
    );
  }
  if (error) {
    return (
      <p className="rounded-xl border border-err/30 bg-err-bg px-4 py-3 text-sm text-err">
        Could not load run history. Is the backend running?
      </p>
    );
  }
  if (!data || data.runs.length === 0) {
    return (
      <p className="rounded-xl border border-dashed border-line px-4 py-8 text-center text-sm text-muted">
        No runs yet. Trigger a discovery to get started.
      </p>
    );
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
      className="group flex items-center justify-between gap-4 rounded-xl border border-line bg-surface px-4 py-3 transition-all hover:border-accent hover:shadow-sm"
    >
      <div className="min-w-0">
        <p className="truncate text-sm font-medium text-ink group-hover:text-accent">
          {run.paperTitle || run.paperId || "(no paper selected)"}
        </p>
        <p className="mt-0.5 font-mono text-xs text-muted">{formatDate(run.startedAt)}</p>
      </div>
      <div className="flex shrink-0 items-center gap-3">
        {run.estCostUsd != null && (
          <span className="font-mono text-xs text-muted tabular-nums">
            ~${run.estCostUsd.toFixed(2)}
          </span>
        )}
        <span className={`rounded-full px-2.5 py-0.5 font-mono text-xs font-medium ${badge}`}>
          {run.status}
        </span>
      </div>
    </Link>
  );
}
