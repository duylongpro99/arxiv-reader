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

// hasDetail reports whether there is anything to expand. Mirrors the guards in
// EventDetail/ReasoningBlock so a row never renders an expand arrow that opens
// onto an empty box (e.g. a payloadFull object whose prompt fields are all
// empty strings would pass an Object.keys() check but have nothing to show).
function hasDetail(evt: TimelineEvent): boolean {
  const hasSummary = evt.summary != null && Object.keys(evt.summary).length > 0;
  const hasPayload =
    evt.payloadFull != null &&
    Boolean(evt.payloadFull.systemPrompt || evt.payloadFull.userPrompt || evt.payloadFull.response);
  return hasSummary || hasPayload;
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

// Cryptic backend summary keys relabeled to human-readable text. Anything not
// in this map falls back to `humanizeKey` below rather than being hidden —
// new/legacy keys (e.g. the old finalScore/maxIterations/flagged fields)
// still render, just without a hand-picked label.
const SUMMARY_LABELS: Record<string, string> = {
  reviewIterations: "Review passes",
  feedbackKeys: "Sections flagged",
  reviewPassed: "Review passed",
  finalScore: "Final score",
  maxIterations: "Max iterations",
  decision: "Decision",
  onPass: "Passed on iteration",
};

// humanizeKey turns an unmapped camelCase key into "Title Case words" so raw
// backend field names never leak into the UI verbatim.
function humanizeKey(key: string): string {
  const spaced = key.replace(/([A-Z])/g, " $1").trim();
  return spaced.charAt(0).toUpperCase() + spaced.slice(1);
}

// EventDetail renders the (already-scrubbed) summary and optional full payload as
// readable text. Rendered as text — never dangerouslySetInnerHTML.
function EventDetail({ event }: { event: TimelineEvent }) {
  return (
    <div className="mt-2 space-y-2">
      {event.summary && Object.keys(event.summary).length > 0 && (
        <SummaryBlock summary={event.summary} />
      )}
      {event.payloadFull &&
        (event.payloadFull.systemPrompt || event.payloadFull.userPrompt || event.payloadFull.response) && (
          <ReasoningBlock payload={event.payloadFull} />
        )}
    </div>
  );
}

// SummaryBlock pulls the two "known" decision fields (narrative, flaggedSections)
// out of the free-form summary map and renders them specially — a narrative
// reads as a sentence, flagged sections as chips — while every other key still
// falls back to a relabeled key/value row so no data is silently dropped.
function SummaryBlock({ summary }: { summary: Record<string, unknown> }) {
  const { narrative, flaggedSections, ...rest } = summary;
  const restEntries = Object.entries(rest);
  const chips = Array.isArray(flaggedSections) ? flaggedSections : undefined;

  return (
    <div className="rounded-md border border-line bg-card p-2.5">
      <p className="mb-1.5 font-mono text-[11px] font-medium uppercase tracking-wide text-muted">
        Summary
      </p>

      {typeof narrative === "string" && narrative.length > 0 && (
        <p className="mb-2 text-xs text-ink/90">{narrative}</p>
      )}

      {chips && chips.length > 0 && (
        <div className="mb-2 flex flex-wrap gap-1.5">
          {chips.map((section, i) => (
            <span
              key={`${section}-${i}`}
              className="rounded-full border border-warn/30 bg-warn-bg px-2 py-0.5 font-mono text-[11px] text-warn"
            >
              {section}
            </span>
          ))}
        </div>
      )}

      {restEntries.length > 0 && (
        <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-xs">
          {restEntries.map(([k, v]) => (
            <div key={k} className="col-span-2 grid grid-cols-subgrid">
              <dt className="font-mono text-muted">{SUMMARY_LABELS[k] ?? humanizeKey(k)}</dt>
              <dd className="break-words font-mono text-ink/85">{formatValue(v)}</dd>
            </div>
          ))}
        </dl>
      )}
    </div>
  );
}

// ReasoningBlock renders the raw LLM exchange behind an explainer/reviewer
// event — only present when the backend's `full_payloads` tracing is on.
// The system prompt is nested behind its own <details> (collapsed by
// default) since it's usually long, static boilerplate; the user prompt and
// response are the parts worth seeing at a glance.
function ReasoningBlock({ payload }: { payload: NonNullable<TimelineEvent["payloadFull"]> }) {
  return (
    <details className="group rounded-md border border-line bg-card p-2.5">
      <summary className="cursor-pointer font-mono text-[11px] font-medium uppercase tracking-wide text-muted marker:content-none">
        <span className="inline-flex items-center gap-1">
          <ChevronRightIcon className="h-3 w-3 group-open:hidden" />
          <ChevronDownIcon className="hidden h-3 w-3 group-open:inline" />
          Reasoning
        </span>
      </summary>

      <div className="mt-2 space-y-2">
        {payload.systemPrompt && (
          <details className="rounded border border-line/60 p-2">
            <summary className="cursor-pointer font-mono text-[10px] uppercase tracking-wide text-muted">
              System prompt
            </summary>
            <PromptText text={payload.systemPrompt} />
          </details>
        )}
        {payload.userPrompt && (
          <div>
            <p className="mb-1 font-mono text-[10px] uppercase tracking-wide text-muted">
              User prompt
            </p>
            <PromptText text={payload.userPrompt} />
          </div>
        )}
        {payload.response && (
          <div>
            <p className="mb-1 font-mono text-[10px] uppercase tracking-wide text-muted">
              Response
            </p>
            <PromptText text={payload.response} />
          </div>
        )}
      </div>
    </details>
  );
}

// PromptText is the shared scroll-contained text box for prompt/response
// bodies: monospace + whitespace-pre-wrap keeps formatting, but overflow-x
// stays local to this box so a long unbroken line never widens the page.
function PromptText({ text }: { text: string }) {
  return (
    <pre className="mt-1 max-h-64 max-w-full overflow-auto whitespace-pre-wrap break-words rounded bg-surface p-2 font-mono text-[11px] text-ink/85">
      {text}
    </pre>
  );
}

// formatValue renders a scalar as-is and an object/array compactly.
function formatValue(v: unknown): string {
  if (v == null) return "—";
  if (typeof v === "object") return JSON.stringify(v);
  return String(v);
}
