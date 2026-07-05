"use client";

import type { PipelineStatus } from "@/lib/types";

// STAGE_LABEL maps a static pipeline stage to a user-facing message. Filtering the
// local log is instantaneous, so there is no separate "Filtering…" stage — the
// discovery stage covers fetch + filter honestly (see brainstorm summary). The
// review-loop stages (reviewing/revising) are handled by getLabel below because
// they interpolate the live pass number.
const STAGE_LABEL: Partial<Record<PipelineStatus["stage"], string>> = {
  discovery: "Connecting to arXiv…",
  selection: "Ready — select a paper",
  extracting: "Extracting paper text…",
  generating: "Generating explainer…",
  writing: "Saving to vault…",
  complete: "Complete",
};

// getLabel special-cases the Phase 5 review stages so the pass number (and, for
// review, the reviewer score when present) renders; everything else falls back to
// the static map, then to the generic "Working…" default.
function getLabel(status: PipelineStatus): string {
  const pass = status.iteration ?? 1;
  switch (status.stage) {
    case "reviewing": {
      const score =
        typeof status.reviewScore === "number" && status.reviewScore > 0
          ? ` (score: ${status.reviewScore.toFixed(2)})`
          : "";
      return `Reviewing (pass ${pass})…${score}`;
    }
    case "revising":
      return `Revising (pass ${pass})…`;
    default:
      return STAGE_LABEL[status.stage] ?? "Working…";
  }
}

export function ProgressIndicator({ status }: { status: PipelineStatus }) {
  const label = getLabel(status);
  // The spinner shows only while work is in flight — the two idle/terminal
  // stages (awaiting a pick, and complete) show none.
  const showSpinner = status.stage !== "selection" && status.stage !== "complete";
  return (
    <div className="flex items-center gap-3 text-sm text-gray-600 dark:text-gray-300">
      {showSpinner && (
        <span
          className="inline-block h-4 w-4 animate-spin rounded-full border-2 border-gray-300 border-t-blue-600"
          aria-hidden
        />
      )}
      <span>{label}</span>
    </div>
  );
}
