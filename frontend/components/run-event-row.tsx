"use client";

import { useState } from "react";
import type { EventStatus, TimelineEvent } from "@/lib/types";

// Per-status icon + color. Icons are decorative (aria-hidden); the status is
// conveyed in text via the row title, so screen readers are not left out.
const STATUS_STYLE: Record<EventStatus, { icon: string; className: string }> = {
  info: { icon: "●", className: "text-blue-500 dark:text-blue-400" },
  success: { icon: "✓", className: "text-green-600 dark:text-green-400" },
  warning: { icon: "▲", className: "text-amber-600 dark:text-amber-400" },
  error: { icon: "✕", className: "text-red-600 dark:text-red-400" },
};

// formatDuration renders durationMs compactly: 620ms, 1.2s, 1m47s.
function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  return `${m}m${Math.round(s % 60)}s`;
}

// formatClock renders the wall-clock time of the event (HH:MM:SS).
function formatClock(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return d.toLocaleTimeString([], { hour12: false });
}

// hasDetail reports whether there is anything to expand.
function hasDetail(evt: TimelineEvent): boolean {
  return (
    (evt.summary != null && Object.keys(evt.summary).length > 0) ||
    (evt.payloadFull != null && Object.keys(evt.payloadFull).length > 0)
  );
}

export function RunEventRow({ event }: { event: TimelineEvent }) {
  const [open, setOpen] = useState(false);
  const style = STATUS_STYLE[event.status] ?? STATUS_STYLE.info;
  const expandable = hasDetail(event);

  return (
    <li className="border-b border-gray-100 py-2 last:border-0 dark:border-gray-800">
      <div className="flex items-baseline gap-3">
        <span className={`select-none font-mono text-sm ${style.className}`} aria-hidden>
          {style.icon}
        </span>
        <div className="min-w-0 flex-1">
          {expandable ? (
            <button
              type="button"
              onClick={() => setOpen((o) => !o)}
              aria-expanded={open}
              className="text-left text-sm text-gray-800 hover:underline dark:text-gray-100"
            >
              {event.title}
              <span className="ml-1 text-xs text-gray-400" aria-hidden>
                {open ? "▾" : "▸"}
              </span>
            </button>
          ) : (
            <span className="text-sm text-gray-800 dark:text-gray-100">{event.title}</span>
          )}
          {open && expandable && <EventDetail event={event} />}
        </div>
        <span className="shrink-0 font-mono text-xs text-gray-400">
          {event.durationMs != null && (
            <span className="mr-2">{formatDuration(event.durationMs)}</span>
          )}
          {formatClock(event.createdAt)}
        </span>
      </div>
    </li>
  );
}

// EventDetail renders the (already-scrubbed) summary and optional full payload as
// readable key/value text. Rendered as text — never dangerouslySetInnerHTML.
function EventDetail({ event }: { event: TimelineEvent }) {
  return (
    <div className="mt-2 space-y-2">
      {event.summary && <DetailBlock label="Summary" data={event.summary} />}
      {event.payloadFull && <DetailBlock label="Full payload" data={event.payloadFull} />}
    </div>
  );
}

function DetailBlock({ label, data }: { label: string; data: Record<string, unknown> }) {
  return (
    <div className="rounded-md bg-gray-50 p-2 dark:bg-gray-800/60">
      <p className="mb-1 text-xs font-medium text-gray-500 dark:text-gray-400">{label}</p>
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
        {Object.entries(data).map(([k, v]) => (
          <div key={k} className="col-span-2 grid grid-cols-subgrid">
            <dt className="font-mono text-gray-500 dark:text-gray-400">{k}</dt>
            <dd className="break-words font-mono text-gray-700 dark:text-gray-300">
              {formatValue(v)}
            </dd>
          </div>
        ))}
      </dl>
    </div>
  );
}

// formatValue renders a scalar as-is and an object/array compactly.
function formatValue(v: unknown): string {
  if (v == null) return "—";
  if (typeof v === "object") return JSON.stringify(v);
  return String(v);
}
