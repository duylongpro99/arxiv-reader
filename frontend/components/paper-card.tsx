"use client";

import type { Paper } from "@/lib/types";

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

// PaperCard displays one candidate. The "Select" button is intentionally
// disabled in Phase 2 — selection handling arrives in Phase 3.
export function PaperCard({ paper }: { paper: Paper }) {
  return (
    <article className="rounded-xl border border-gray-200 p-5 dark:border-gray-700">
      <div className="mb-2 flex items-start justify-between gap-4">
        <h3 className="text-lg font-semibold leading-snug">{paper.title}</h3>
        <span className="shrink-0 rounded-md bg-gray-100 px-2 py-1 font-mono text-xs text-gray-600 dark:bg-gray-800 dark:text-gray-300">
          {paper.id}
        </span>
      </div>
      <p className="mb-3 text-sm text-gray-500 dark:text-gray-400">
        {paper.authors.join(", ")}
      </p>
      <p className="mb-4 text-sm leading-relaxed text-gray-700 dark:text-gray-300">
        {abstractSnippet(paper.abstract)}
      </p>
      <div className="flex items-center justify-between">
        <time className="text-xs text-gray-400">{formatDate(paper.published)}</time>
        <button
          disabled
          title="Selection arrives in Phase 3"
          className="cursor-not-allowed rounded-md border border-gray-300 px-3 py-1.5 text-sm text-gray-400 dark:border-gray-600"
        >
          Select
        </button>
      </div>
    </article>
  );
}
