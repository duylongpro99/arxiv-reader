"use client";

import type { Paper } from "@/lib/types";
import { PaperCard } from "./paper-card";

// CandidateList renders the discovered papers, plus an optional notice (e.g.
// when fewer than the requested number of new papers were found).
export function CandidateList({
  candidates,
  notice,
  selectedId,
  onSelect,
}: {
  candidates: Paper[];
  notice?: string;
  selectedId?: string | null;
  onSelect?: (paperId: string) => void;
}) {
  return (
    <section className="flex flex-col gap-4">
      {notice && (
        <p className="rounded-lg bg-amber-50 px-4 py-2 text-sm text-amber-800 dark:bg-amber-950 dark:text-amber-200">
          {notice}
        </p>
      )}
      {candidates.length === 0 ? (
        <p className="text-sm text-gray-500">
          No new papers right now. Try again later.
        </p>
      ) : (
        candidates.map((p) => (
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
    </section>
  );
}
