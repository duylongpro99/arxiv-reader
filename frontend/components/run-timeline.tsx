"use client";

import type { TimelineEvent } from "@/lib/types";
import { RunEventRow } from "./run-event-row";

// RunTimeline is purely presentational: it renders an ordered event list. Both
// the live view (fed by useEventSource) and the history view (fed by useRun)
// reuse it, so it takes events + a couple of display flags and nothing else.
export function RunTimeline({
  events,
  live = false,
  error = false,
}: {
  events: TimelineEvent[];
  live?: boolean; // a live run still in progress (shows a "streaming" hint)
  error?: boolean; // SSE connection error
}) {
  return (
    <section
      aria-label="Run timeline"
      className="rounded-lg border border-gray-200 bg-white p-4 dark:border-gray-700 dark:bg-gray-900"
    >
      <div className="mb-2 flex items-center justify-between">
        <h3 className="text-sm font-semibold text-gray-700 dark:text-gray-200">Timeline</h3>
        {live && !error && (
          <span className="flex items-center gap-1.5 text-xs text-gray-400">
            <span
              className="inline-block h-2 w-2 animate-pulse rounded-full bg-blue-500"
              aria-hidden
            />
            streaming
          </span>
        )}
      </div>

      {error && (
        <p className="mb-2 text-xs text-amber-600 dark:text-amber-400">
          Live connection interrupted — reconnecting…
        </p>
      )}

      {events.length === 0 ? (
        <p className="py-2 text-sm text-gray-400">Waiting for the first event…</p>
      ) : (
        <ol className="flex flex-col">
          {events.map((e) => (
            <RunEventRow key={e.seq} event={e} />
          ))}
        </ol>
      )}
    </section>
  );
}
