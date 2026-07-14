"use client";

import { useRunPublications } from "@/lib/use-publications";
import { HistoryUnavailableError } from "@/lib/use-runs";
import { PublishChannelPicker } from "./publish-channel-picker";
import { PublishDraftCard } from "./publish-draft-card";

// PublishDraftPanel is the run-detail page's "Publish" section: pick channels
// -> generate drafts -> one editable card per draft. A 503 (no DB configured)
// collapses the whole section to a subtle disabled hint instead of a full
// error banner, mirroring how the page already treats HistoryUnavailableError
// for the run header/content queries above it.
export function PublishDraftPanel({ runId }: { runId: string }) {
  const { data, isLoading, error } = useRunPublications(runId);

  if (error instanceof HistoryUnavailableError) {
    return (
      <section className="flex flex-col gap-3">
        <h2 className="text-lg font-semibold text-ink">Publish</h2>
        <p className="rounded-xl border border-line bg-card px-4 py-3 text-sm text-muted">
          {error.message}
        </p>
      </section>
    );
  }

  return (
    <section className="flex flex-col gap-4">
      <h2 className="text-lg font-semibold text-ink">Publish</h2>
      <PublishChannelPicker runId={runId} />

      {isLoading && <p className="font-mono text-sm text-muted">Loading drafts…</p>}
      {error && (
        <p className="rounded-xl border border-err/30 bg-err-bg px-4 py-3 text-sm text-err">
          Could not load publish drafts.
        </p>
      )}

      {data && data.publications.length > 0 && (
        <div className="flex flex-col gap-4">
          {data.publications.map((pub) => (
            <PublishDraftCard key={pub.id} publication={pub} runId={runId} />
          ))}
        </div>
      )}
    </section>
  );
}
