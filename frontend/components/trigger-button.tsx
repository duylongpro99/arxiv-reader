"use client";

import { SparkIcon, SpinnerIcon } from "./icons";

// TriggerButton is the single, unambiguous entry point (PRD F1). It disables and
// shows a loading label while discovery is running.
export function TriggerButton({
  onClick,
  loading,
}: {
  onClick: () => void;
  loading: boolean;
}) {
  return (
    <button
      onClick={onClick}
      disabled={loading}
      className="group inline-flex w-fit cursor-pointer items-center gap-2 rounded-lg bg-accent-solid px-5 py-2.5 text-sm font-medium text-on-accent shadow-sm transition-all hover:bg-accent-solid-hover disabled:cursor-not-allowed disabled:opacity-60"
    >
      {loading ? (
        <SpinnerIcon className="h-4 w-4 animate-spin" />
      ) : (
        <SparkIcon className="h-4 w-4 transition-transform group-hover:scale-110" />
      )}
      {loading ? "Finding papers…" : "Find New Papers"}
    </button>
  );
}
