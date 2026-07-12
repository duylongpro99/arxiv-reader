"use client";

import type { ContextWarning } from "@/lib/types";
import { AlertTriangleIcon } from "./icons";

// ContextWarningBanner is a NON-BLOCKING advisory shown when the backend's
// pre-check estimates the paper may exceed the model's context window (F4). The
// pipeline proceeds regardless; this only warns and suggests a larger-context
// model. Amber, not red — it is not a failure.
export function ContextWarningBanner({ warning }: { warning: ContextWarning }) {
  return (
    <div className="flex items-start gap-2 rounded-xl border border-warn/30 bg-warn-bg p-4 text-sm text-warn">
      <AlertTriangleIcon className="mt-0.5 h-4 w-4 shrink-0" />
      <span className="text-ink/90">
        This paper (~{warning.estimatedTokens.toLocaleString()} tokens) may exceed{" "}
        {warning.model}&rsquo;s context limit (
        {warning.modelLimit.toLocaleString()}). Proceeding anyway — {warning.suggestion}
      </span>
    </div>
  );
}
