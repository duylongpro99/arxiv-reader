"use client";

import { useState } from "react";
import { useChannels, useGenerateDrafts } from "@/lib/use-publications";
import { HistoryUnavailableError } from "@/lib/use-runs";
import { SpinnerIcon } from "./icons";

// PublishChannelPicker lets the user pick which configured channels to draft
// content for, then kicks off generation. Re-requesting a channel that
// already has a draft for this run is idempotent on the backend (the existing
// row comes back untouched), so selection doesn't need to track "already
// drafted" state.
export function PublishChannelPicker({ runId }: { runId: string }) {
  const { data, isLoading, error } = useChannels();
  const generate = useGenerateDrafts(runId);
  const [selected, setSelected] = useState<Set<string>>(new Set());

  if (isLoading) {
    return <p className="font-mono text-sm text-muted">Loading channels…</p>;
  }
  // Publishing unavailable (no DB) is handled one level up by
  // PublishDraftPanel (the publications-list query hits the same 503 first),
  // so this hides itself quietly here instead of duplicating the hint.
  if (error instanceof HistoryUnavailableError) {
    return null;
  }
  if (error) {
    return (
      <p className="rounded-xl border border-err/30 bg-err-bg px-4 py-3 text-sm text-err">
        Could not load publish channels.
      </p>
    );
  }

  const channels = data?.channels ?? [];
  if (channels.length === 0) {
    return (
      <p className="rounded-xl border border-dashed border-line px-4 py-8 text-center text-sm text-muted">
        No publish channels configured.
      </p>
    );
  }

  function toggle(id: string) {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  return (
    <div className="flex flex-col gap-3 rounded-xl border border-line bg-surface p-4">
      <div className="flex flex-wrap gap-2">
        {channels.map((c) => (
          <label
            key={c.id}
            className={`flex cursor-pointer items-center gap-2 rounded-md border px-3 py-1.5 text-sm transition-colors ${
              selected.has(c.id)
                ? "border-accent bg-accent-bg text-ink"
                : "border-line text-muted hover:border-accent hover:text-ink"
            }`}
          >
            <input
              type="checkbox"
              checked={selected.has(c.id)}
              onChange={() => toggle(c.id)}
              className="accent-accent-solid"
            />
            <span className="font-medium">{c.id}</span>
            <span className="font-mono text-xs text-muted">{c.category}</span>
          </label>
        ))}
      </div>

      <button
        type="button"
        onClick={() => generate.mutate(Array.from(selected))}
        disabled={selected.size === 0 || generate.isPending}
        className="inline-flex w-fit cursor-pointer items-center gap-1.5 rounded-md bg-accent-solid px-4 py-1.5 text-sm font-medium text-on-accent transition-colors hover:bg-accent-solid-hover disabled:cursor-not-allowed disabled:opacity-60"
      >
        {generate.isPending && <SpinnerIcon className="h-3.5 w-3.5 animate-spin" />}
        {generate.isPending ? "Generating…" : "Generate drafts"}
      </button>

      {generate.isError && (
        <p className="text-sm text-err">
          {generate.error instanceof HistoryUnavailableError
            ? generate.error.message
            : "Failed to generate drafts."}
        </p>
      )}
      {generate.isSuccess &&
        !!generate.data.skippedChannels?.length && (
          <p className="text-xs text-warn">
            Skipped: {generate.data.skippedChannels.join(", ")}
          </p>
        )}
    </div>
  );
}
