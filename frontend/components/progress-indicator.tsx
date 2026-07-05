"use client";

import type { PipelineStage } from "@/lib/types";

// STAGE_LABEL maps a pipeline stage to a user-facing message. Filtering the
// local log is instantaneous, so there is no separate "Filtering…" stage — the
// discovery stage covers fetch + filter honestly (see brainstorm summary).
const STAGE_LABEL: Partial<Record<PipelineStage, string>> = {
  discovery: "Connecting to arXiv…",
  selection: "Ready — select a paper",
  extracting: "Extracting paper text…",
};

export function ProgressIndicator({ stage }: { stage: PipelineStage }) {
  const label = STAGE_LABEL[stage] ?? "Working…";
  return (
    <div className="flex items-center gap-3 text-sm text-gray-600 dark:text-gray-300">
      {stage !== "selection" && (
        <span
          className="inline-block h-4 w-4 animate-spin rounded-full border-2 border-gray-300 border-t-blue-600"
          aria-hidden
        />
      )}
      <span>{label}</span>
    </div>
  );
}
