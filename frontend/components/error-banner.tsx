"use client";

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
    <div className="flex flex-col gap-3 rounded-lg border border-red-200 bg-red-50 p-4 dark:border-red-900 dark:bg-red-950">
      <p className="text-sm text-red-800 dark:text-red-200">{message}</p>
      {recoverable && (
        <button
          onClick={onRetry}
          className="self-start rounded-md bg-red-600 px-4 py-1.5 text-sm font-medium text-white hover:bg-red-700"
        >
          Retry
        </button>
      )}
    </div>
  );
}
