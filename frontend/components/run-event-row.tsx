"use client";

import { useState } from "react";
import type { EventStatus, TimelineEvent } from "@/lib/types";
import {
  AlertTriangleIcon,
  CheckCircleIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  DotIcon,
  XIcon,
} from "./icons";

// Per-status glyph + semantic color token. Icons are decorative (aria-hidden);
// the status is conveyed in the row title text, so screen readers aren't left out
// and color is never the sole signal.
const STATUS_STYLE: Record<
  EventStatus,
  { Icon: (p: { className?: string }) => React.ReactNode; color: string }
> = {
  info: { Icon: DotIcon, color: "text-info" },
  success: { Icon: CheckCircleIcon, color: "text-ok" },
  warning: { Icon: AlertTriangleIcon, color: "text-warn" },
  error: { Icon: XIcon, color: "text-err" },
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

// `active` marks the currently-running (last live) row so it gets the accent glow.
export function RunEventRow({
  event,
  active = false,
}: {
  event: TimelineEvent;
  active?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const style = STATUS_STYLE[event.status] ?? STATUS_STYLE.info;
  const expandable = hasDetail(event);
  const { Icon } = style;

  return (
    <li className="ev-in relative flex gap-3 pb-4 last:pb-0">
      {/* Rail glyph — sits on the connector line drawn by the parent <ol>. */}
      <span
        className={`relative z-10 mt-0.5 grid h-6 w-6 shrink-0 place-items-center rounded-full border border-line bg-surface ${style.color} ${
          active ? "run-glow border-accent" : ""
        }`}
      >
        <Icon className="h-3.5 w-3.5" />
      </span>

      <div className="min-w-0 flex-1 pt-0.5">
        {expandable ? (
          <button
            type="button"
            onClick={() => setOpen((o) => !o)}
            aria-expanded={open}
            className="flex cursor-pointer items-center gap-1 text-left text-sm text-ink transition-colors hover:text-accent"
          >
            {event.title}
            {open ? (
              <ChevronDownIcon className="h-3.5 w-3.5 text-muted" />
            ) : (
              <ChevronRightIcon className="h-3.5 w-3.5 text-muted" />
            )}
          </button>
        ) : (
          <span className="text-sm text-ink">{event.title}</span>
        )}
        {open && expandable && <EventDetail event={event} />}
      </div>

      <span className="shrink-0 pt-0.5 text-right font-mono text-xs text-muted tabular-nums">
        {event.durationMs != null && (
          <span className="mr-2 text-accent">{formatDuration(event.durationMs)}</span>
        )}
        {formatClock(event.createdAt)}
      </span>
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
    <div className="rounded-md border border-line bg-card p-2.5">
      <p className="mb-1.5 font-mono text-[11px] font-medium uppercase tracking-wide text-muted">
        {label}
      </p>
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
        {Object.entries(data).map(([k, v]) => (
          <div key={k} className="col-span-2 grid grid-cols-subgrid">
            <dt className="font-mono text-muted">{k}</dt>
            <dd className="break-words font-mono text-ink/85">{formatValue(v)}</dd>
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
