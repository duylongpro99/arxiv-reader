"use client";

import { chunkThreadPreview } from "@/lib/thread-chunk";

// PublishThreadPreview renders the live, client-side approximation of how the
// draft will be chunked into an X thread — one card per tweet-sized segment,
// numbered when there is more than one. Preview only: the backend re-chunks
// authoritatively at publish time.
export function PublishThreadPreview({ content }: { content: string }) {
  const segments = chunkThreadPreview(content);

  if (segments.length === 0) {
    return <p className="text-sm text-muted">Nothing to preview yet.</p>;
  }

  return (
    <div className="flex flex-col gap-2">
      <p className="font-mono text-[11px] uppercase tracking-wide text-muted">
        Thread preview ({segments.length} {segments.length === 1 ? "tweet" : "tweets"})
      </p>
      {segments.map((seg, i) => (
        <div
          key={i}
          className="rounded-lg border border-line bg-card px-3 py-2 text-sm whitespace-pre-wrap text-ink/90"
        >
          {seg}
          <span className="mt-1 block text-right font-mono text-[11px] text-muted tabular-nums">
            {seg.length}/280
          </span>
        </div>
      ))}
    </div>
  );
}
