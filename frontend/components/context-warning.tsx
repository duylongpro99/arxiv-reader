"use client";

import type { ContextWarning } from "@/lib/types";

// ContextWarningBanner is a NON-BLOCKING advisory shown when the backend's
// pre-check estimates the paper may exceed the model's context window (F4). The
// pipeline proceeds regardless; this only warns and suggests a larger-context
// model. Amber, not red — it is not a failure.
export function ContextWarningBanner({ warning }: { warning: ContextWarning }) {
  return (
    <div className="rounded-lg border border-amber-300 bg-amber-50 p-4 text-sm text-amber-800 dark:border-amber-800 dark:bg-amber-950 dark:text-amber-200">
      <span aria-hidden>⚠️ </span>
      This paper (~{warning.estimatedTokens.toLocaleString()} tokens) may exceed{" "}
      {warning.model}&rsquo;s context limit (
      {warning.modelLimit.toLocaleString()}). Proceeding anyway — {warning.suggestion}
    </div>
  );
}
