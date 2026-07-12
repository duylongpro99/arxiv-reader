"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { RunTimeline } from "@/components/run-timeline";
import type { RunSummary, TimelineEvent } from "@/lib/types";
import { HistoryUnavailableError, useRun } from "@/lib/use-runs";

// /runs/[id] — reopen one past run: its header summary + full persisted timeline.
// A client component so it can reuse the same hooks/components as the live view.
export default function RunDetailPage() {
  const params = useParams<{ id: string }>();
  const id = params?.id ?? null;
  const { data, isLoading, error } = useRun(id);

  return (
    <main className="mx-auto flex w-full max-w-3xl flex-col gap-6 px-6 py-12">
      <header className="flex items-center justify-between">
        <h1 className="text-2xl font-bold">Run detail</h1>
        <Link
          href="/runs"
          className="rounded-md border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-800"
        >
          ← All runs
        </Link>
      </header>

      {isLoading && <p className="text-sm text-gray-500">Loading run…</p>}
      {error instanceof HistoryUnavailableError && (
        <p className="rounded-lg bg-amber-50 px-4 py-3 text-sm text-amber-800 dark:bg-amber-950 dark:text-amber-200">
          {error.message}
        </p>
      )}
      {error && !(error instanceof HistoryUnavailableError) && (
        <p className="rounded-lg bg-red-50 px-4 py-3 text-sm text-red-800 dark:bg-red-950 dark:text-red-200">
          {error.message}
        </p>
      )}

      {data && (
        <>
          <RunHeaderPanel run={data.run} events={data.events} />
          <RunTimeline events={data.events} />
        </>
      )}
    </main>
  );
}

// vaultPath extracts the saved note path from the vaultwriter event, if present.
function vaultPath(events: TimelineEvent[]): string | null {
  const e = events.find((ev) => ev.type === "tool.vaultwriter.completed");
  const p = e?.summary?.path;
  return typeof p === "string" ? p : null;
}

function RunHeaderPanel({ run, events }: { run: RunSummary; events: TimelineEvent[] }) {
  const path = vaultPath(events);
  const totalTokens = run.inputTokens + run.outputTokens;
  return (
    <div className="rounded-lg border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-900">
      <p className="text-base font-semibold text-gray-800 dark:text-gray-100">
        {run.paperTitle || run.paperId || "(no paper selected)"}
      </p>
      <dl className="mt-3 grid grid-cols-2 gap-x-6 gap-y-1 text-sm text-gray-600 dark:text-gray-400 sm:grid-cols-3">
        <Stat label="Status" value={run.status} />
        <Stat label="Tokens" value={totalTokens.toLocaleString()} />
        {run.estCostUsd != null && <Stat label="Est. cost" value={`~$${run.estCostUsd.toFixed(3)}`} />}
        {run.reviewPassed != null && (
          <Stat label="Review" value={run.reviewPassed ? "passed" : "not passed"} />
        )}
      </dl>
      {path && (
        <p className="mt-3 break-all font-mono text-xs text-gray-500 dark:text-gray-400">
          Saved to: {path}
        </p>
      )}
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="text-xs text-gray-400">{label}</dt>
      <dd className="font-medium text-gray-700 dark:text-gray-200">{value}</dd>
    </div>
  );
}
