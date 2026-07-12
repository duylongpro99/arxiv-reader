"use client";

import type { Paper } from "@/lib/types";
import { CheckIcon } from "./icons";

// abstractSnippet trims the abstract to the first 300 chars + ellipsis (PRD F4).
function abstractSnippet(abstract: string): string {
  return abstract.length > 300 ? abstract.slice(0, 300) + "…" : abstract;
}

// formatDate renders an ISO date as a human-readable string ("June 7, 2026").
// Falls back to the raw value if the date is unparseable.
function formatDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleDateString("en-US", {
    year: "numeric",
    month: "long",
    day: "numeric",
  });
}

// PaperCard displays one candidate with an active Select button. Once any paper
// is chosen, sibling cards are disabled; the chosen card shows a "Selected" state.
export function PaperCard({
  paper,
  onSelect,
  disabled = false,
  selected = false,
}: {
  paper: Paper;
  onSelect?: (paperId: string) => void;
  disabled?: boolean;
  selected?: boolean;
}) {
  return (
    <article
      className={`group rounded-xl border bg-surface p-5 transition-all ${
        selected
          ? "border-accent ring-1 ring-accent"
          : disabled
            ? "border-line opacity-55"
            : "border-line hover:border-accent hover:shadow-sm"
      }`}
    >
      <div className="mb-2 flex items-start justify-between gap-4">
        <h3 className="text-base font-semibold leading-snug text-ink">
          {paper.title}
        </h3>
        <span className="shrink-0 rounded-md bg-card px-2 py-1 font-mono text-xs text-muted">
          {paper.id}
        </span>
      </div>
      <p className="mb-3 text-sm text-muted">{paper.authors.join(", ")}</p>
      <p className="mb-4 text-sm leading-relaxed text-ink/85">
        {abstractSnippet(paper.abstract)}
      </p>
      <div className="flex items-center justify-between">
        <time className="font-mono text-xs text-muted">
          {formatDate(paper.published)}
        </time>
        <button
          onClick={() => onSelect?.(paper.id)}
          disabled={disabled || selected}
          className={
            selected
              ? "inline-flex items-center gap-1.5 rounded-md bg-accent-solid px-3 py-1.5 text-sm font-medium text-on-accent"
              : disabled
                ? "cursor-not-allowed rounded-md border border-line px-3 py-1.5 text-sm text-muted"
                : "cursor-pointer rounded-md border border-accent px-3 py-1.5 text-sm font-medium text-accent transition-colors hover:bg-accent-bg"
          }
        >
          {selected && <CheckIcon className="h-4 w-4" />}
          {selected ? "Selected" : "Select"}
        </button>
      </div>
    </article>
  );
}
