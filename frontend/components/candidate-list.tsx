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
        <p className="rounded-lg border border-warn/30 bg-warn-bg px-4 py-2 text-sm text-warn">
          {notice}
        </p>
      )}
      {candidates.length === 0 ? (
        <p className="rounded-xl border border-dashed border-line px-4 py-8 text-center text-sm text-muted">
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
