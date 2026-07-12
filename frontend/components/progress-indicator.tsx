"use client";

import type { PipelineStatus } from "@/lib/types";
import { SpinnerIcon } from "./icons";

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
    case "discovery": {
      // Surface arXiv 429/5xx backoff so a slow connect doesn't look hung. The
      // "/3" matches the backend's default max_retries.
      const retries = status.arxivRetryCount ?? 0;
      return retries > 0
        ? `Connecting to arXiv (retry ${retries}/3)…`
        : STAGE_LABEL.discovery!;
    }
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
    <div className="flex items-center gap-2.5 rounded-lg border border-line bg-surface px-4 py-2.5 text-sm text-ink">
      {showSpinner ? (
        <SpinnerIcon className="h-4 w-4 shrink-0 animate-spin text-accent" />
      ) : (
        <span className="h-1.5 w-1.5 shrink-0 rounded-full bg-ok" aria-hidden />
      )}
      <span className="font-mono text-[13px]">{label}</span>
    </div>
  );
}
