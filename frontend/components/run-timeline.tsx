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
      className="rounded-xl border border-line bg-surface p-4"
    >
      <div className="mb-3 flex items-center justify-between">
        <h3 className="font-mono text-xs font-medium uppercase tracking-wide text-muted">
          Run timeline
        </h3>
        {live && !error && (
          <span className="flex items-center gap-1.5 font-mono text-xs text-accent">
            <span className="relative flex h-2 w-2" aria-hidden>
              <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-accent opacity-60" />
              <span className="relative inline-flex h-2 w-2 rounded-full bg-accent" />
            </span>
            streaming
          </span>
        )}
      </div>

      {error && (
        <p className="mb-3 flex items-center gap-1.5 text-xs text-warn">
          Live connection interrupted — reconnecting…
        </p>
      )}

      {events.length === 0 ? (
        <p className="py-2 font-mono text-sm text-muted">Waiting for the first event…</p>
      ) : (
        // The `before:` pseudo draws the vertical connector rail at x=12px, which
        // is the center of each 24px glyph; the glyphs (bg-surface) sit on top of
        // it, producing a clean connected timeline.
        <ol className="relative before:absolute before:bottom-3 before:left-3 before:top-3 before:w-px before:bg-line">
          {events.map((e, i) => (
            <RunEventRow
              key={e.seq}
              event={e}
              // The last event of a still-streaming run is the "running" step.
              active={live && !error && i === events.length - 1}
            />
          ))}
        </ol>
      )}
    </section>
  );
}
