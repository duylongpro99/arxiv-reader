"use client";

import { useEffect, useState } from "react";
import { fetchMoreCandidates } from "@/lib/api";
import type { Paper } from "@/lib/types";
import { SpinnerIcon } from "./icons";
import { PaperCard } from "./paper-card";

// CandidateList renders the discovered papers, plus an optional notice (e.g.
// when fewer than the requested number of new papers were found), and a
// "Load more" control (Feature C) that fetches older arXiv candidates for the
// same discovery session.
export function CandidateList({
  candidates,
  notice,
  selectedId,
  onSelect,
  sessionId,
}: {
  candidates: Paper[];
  notice?: string;
  selectedId?: string | null;
  onSelect?: (paperId: string) => void;
  // Needed to call POST /discover/:sessionId/more. Optional so the component
  // still renders (without the "Load more" control) if the caller can't supply
  // it. See candidate-list ownership note in phase-04 for why this is a prop
  // rather than derived internally.
  sessionId?: string | null;
}) {
  // Extra pages fetched via "Load more". Kept separate from the `candidates`
  // prop because the parent's status poll pauses once the pipeline reaches the
  // "selection" stage (see discovery-panel's refetchInterval), so `candidates`
  // itself never grows on its own — appended pages must be tracked locally.
  const [extra, setExtra] = useState<Paper[]>([]);
  const [hasMore, setHasMore] = useState(true);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [showEmptyNotice, setShowEmptyNotice] = useState(false);

  // Reset all "load more" state when the session changes (a fresh discovery
  // run starts clean rather than carrying over the previous session's pages).
  useEffect(() => {
    setExtra([]);
    setHasMore(true);
    setLoadError(null);
    setShowEmptyNotice(false);
  }, [sessionId]);

  // Merge the initial page with any appended pages, deduping by paper id in
  // case arXiv's paging returns an overlapping result.
  const seen = new Set<string>();
  const merged: Paper[] = [];
  for (const p of [...candidates, ...extra]) {
    if (seen.has(p.id)) continue;
    seen.add(p.id);
    merged.push(p);
  }

  async function handleLoadMore() {
    if (!sessionId || loading) return;
    setLoading(true);
    setLoadError(null);
    setShowEmptyNotice(false);
    try {
      const res = await fetchMoreCandidates(sessionId);
      setHasMore(res.hasMore);
      if (res.candidates.length === 0) {
        setShowEmptyNotice(true);
      } else {
        setExtra((prev) => [...prev, ...res.candidates]);
      }
    } catch (err) {
      setLoadError(
        err instanceof Error ? err.message : "Failed to load more papers.",
      );
    } finally {
      setLoading(false);
    }
  }

  return (
    <section className="flex flex-col gap-4">
      {notice && (
        <p className="rounded-lg border border-warn/30 bg-warn-bg px-4 py-2 text-sm text-warn">
          {notice}
        </p>
      )}
      {merged.length === 0 ? (
        <p className="rounded-xl border border-dashed border-line px-4 py-8 text-center text-sm text-muted">
          No new papers right now. Try again later.
        </p>
      ) : (
        merged.map((p) => (
          <PaperCard
            key={p.id}
            paper={p}
            onSelect={onSelect}
            // Disable every card once one is chosen; mark the chosen one selected.
            disabled={selectedId != null && selectedId !== p.id}
            selected={selectedId === p.id}
          />
        ))
      )}

      {sessionId && hasMore && (
        <button
          type="button"
          onClick={handleLoadMore}
          disabled={loading}
          className="inline-flex w-fit cursor-pointer items-center gap-1.5 self-center rounded-md border border-line px-3 py-1.5 text-sm font-medium text-muted transition-colors hover:border-accent hover:text-ink disabled:cursor-not-allowed disabled:opacity-60"
        >
          {loading && <SpinnerIcon className="h-3.5 w-3.5 animate-spin" />}
          {loading ? "Loading…" : "Load more"}
        </button>
      )}
      {showEmptyNotice && (
        <p className="text-center text-sm text-muted">No older papers found.</p>
      )}
      {loadError && (
        <p className="rounded-lg border border-err/30 bg-err-bg px-4 py-2 text-sm text-err">
          {loadError}
        </p>
      )}
    </section>
  );
}
