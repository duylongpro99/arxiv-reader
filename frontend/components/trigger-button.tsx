"use client";

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
      className="rounded-lg bg-blue-600 px-5 py-2.5 font-medium text-white transition-colors hover:bg-blue-700 disabled:cursor-not-allowed disabled:opacity-60"
    >
      {loading ? "Finding papers…" : "Find New Papers"}
    </button>
  );
}
