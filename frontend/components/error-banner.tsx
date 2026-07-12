"use client";

import { AlertTriangleIcon } from "./icons";

// ErrorBanner surfaces a failed pipeline. A retry button is shown only when the
// failure is recoverable (PRD F6) — e.g. a transient arXiv error, not a corrupt
// log that needs manual intervention.
export function ErrorBanner({
  message,
  recoverable,
  onRetry,
}: {
  message: string;
  recoverable?: boolean;
  onRetry: () => void;
}) {
  return (
    <div
      role="alert"
      className="flex flex-col gap-3 rounded-xl border border-err/30 bg-err-bg p-4"
    >
      <p className="flex items-start gap-2 text-sm text-err">
        <AlertTriangleIcon className="mt-0.5 h-4 w-4 shrink-0" />
        <span className="text-ink">{message}</span>
      </p>
      {recoverable && (
        <button
          onClick={onRetry}
          className="inline-flex cursor-pointer items-center gap-1.5 self-start rounded-md bg-err px-4 py-1.5 text-sm font-medium text-white transition-opacity hover:opacity-90"
        >
          Retry
        </button>
      )}
    </div>
  );
}
