"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { RunTimeline } from "@/components/run-timeline";
import { ResultPanel } from "@/components/result-panel";
import { PublishDraftPanel } from "@/components/publish-draft-panel";
import { ArrowLeftIcon } from "@/components/icons";
import type { RunContent, RunSummary, TimelineEvent } from "@/lib/types";
import { HistoryUnavailableError, useRun, useRunContent } from "@/lib/use-runs";

// /runs/[id] — reopen one past run: its header summary + full persisted timeline.
// A client component so it can reuse the same hooks/components as the live view.
export default function RunDetailPage() {
  const params = useParams<{ id: string }>();
  const id = params?.id ?? null;
  const { data, isLoading, error } = useRun(id);
  // Separate query for the generated note (Feature B): it can fail/degrade
  // independently of the run header + timeline above.
  const {
    data: content,
    isLoading: contentLoading,
    error: contentError,
  } = useRunContent(id);

  return (
    <main className="mx-auto flex w-full max-w-3xl flex-col gap-6 px-6 pb-16">
      <header className="sticky top-0 z-20 -mx-6 flex items-center justify-between gap-4 border-b border-line bg-base/80 px-6 py-4 backdrop-blur">
        <Link href="/" className="flex items-center gap-2.5">
          <span
            className="grid h-6 w-6 place-items-center rounded-md bg-accent-solid text-[11px] font-bold text-on-accent"
            aria-hidden
          >
            aX
          </span>
          <span className="font-mono text-sm font-medium tracking-tight text-ink">
            arxiv<span className="text-muted">/</span>explainer
          </span>
        </Link>
        <Link
          href="/runs"
          className="flex shrink-0 items-center gap-1.5 rounded-md border border-line px-2.5 py-1.5 text-xs font-medium text-muted transition-colors hover:border-accent hover:text-ink"
        >
          <ArrowLeftIcon className="h-3.5 w-3.5" />
          All runs
        </Link>
      </header>

      <h1 className="text-2xl font-semibold tracking-tight text-ink">Run detail</h1>

      {isLoading && <p className="font-mono text-sm text-muted">Loading run…</p>}
      {error instanceof HistoryUnavailableError && (
        <p className="rounded-xl border border-warn/30 bg-warn-bg px-4 py-3 text-sm text-warn">
          {error.message}
        </p>
      )}
      {error && !(error instanceof HistoryUnavailableError) && (
        <p className="rounded-xl border border-err/30 bg-err-bg px-4 py-3 text-sm text-err">
          {error.message}
        </p>
      )}

      {data && (
        <>
          <RunHeaderPanel run={data.run} events={data.events} />
          <RunTimeline events={data.events} />
          <GeneratedNoteSection
            loading={contentLoading}
            content={content}
            error={contentError}
          />
          {/* Publishing only makes sense once there's a note to adapt — gate
              on the note's own availability (not run.status) since a
              "recovered" run may still have a usable note despite not
              cleanly reaching "complete". */}
          {content?.available && <PublishDraftPanel runId={data.run.id} />}
        </>
      )}
    </main>
  );
}

// GeneratedNoteSection re-shows the persisted note (Feature B) alongside the
// reasoning timeline above, so a reopened run reads as "what happened" +
// "what it produced". `available:false` (vault file moved/deleted) renders a
// clean empty state instead of crashing or showing a blank panel; the 503
// "history disabled" case is already surfaced by the run-header query above,
// so it is suppressed here to avoid a duplicate banner.
function GeneratedNoteSection({
  loading,
  content,
  error,
}: {
  loading: boolean;
  content?: RunContent;
  error: unknown;
}) {
  if (loading) {
    return <p className="font-mono text-sm text-muted">Loading note…</p>;
  }
  if (error instanceof HistoryUnavailableError) {
    return null;
  }
  if (error) {
    return (
      <p className="rounded-xl border border-err/30 bg-err-bg px-4 py-3 text-sm text-err">
        Could not load the generated note.
      </p>
    );
  }
  if (!content || !content.available) {
    // Backend returns available:false for two distinct cases: a `path` is present
    // when a note WAS written but its file is now gone (moved/deleted); no `path`
    // means the run never reached the writing stage (failed/in-progress) — so it
    // never had a note to lose. Distinguish them to avoid implying data loss.
    return (
      <div className="rounded-xl border border-dashed border-line px-4 py-8 text-center text-sm text-muted">
        {content?.path ? (
          <>
            <p>Note file moved or unavailable.</p>
            <p className="mt-2 break-all font-mono text-xs">{content.path}</p>
          </>
        ) : (
          <p>No note was generated for this run.</p>
        )}
      </div>
    );
  }
  return <ResultPanel markdown={content.markdown ?? ""} />;
}

// vaultPath extracts the saved note path from the vaultwriter event, if present.
function vaultPath(events: TimelineEvent[]): string | null {
  const e = events.find((ev) => ev.type === "tool.vaultwriter.completed");
  const p = e?.summary?.path;
  return typeof p === "string" ? p : null;
}

function RunHeaderPanel({ run, events }: { run: RunSummary; events: TimelineEvent[] }) {
  const path = vaultPath(events);
  const totalTokens = run.inputTokens + run.outputTokens;
  return (
    <div className="rounded-xl border border-line bg-surface p-5">
      <p className="text-base font-semibold text-ink">
        {run.paperTitle || run.paperId || "(no paper selected)"}
      </p>
      <dl className="mt-4 grid grid-cols-2 gap-x-6 gap-y-3 sm:grid-cols-3">
        <Stat label="Status" value={run.status} />
        <Stat label="Tokens" value={totalTokens.toLocaleString()} />
        {run.estCostUsd != null && <Stat label="Est. cost" value={`~$${run.estCostUsd.toFixed(3)}`} />}
        {run.reviewPassed != null && (
          <Stat label="Review" value={run.reviewPassed ? "passed" : "not passed"} />
        )}
      </dl>
      {path && (
        <p className="mt-4 break-all border-t border-line pt-3 font-mono text-xs text-muted">
          Saved to: {path}
        </p>
      )}
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <dt className="font-mono text-[11px] uppercase tracking-wide text-muted">{label}</dt>
      <dd className="mt-0.5 font-mono text-sm font-medium text-ink tabular-nums">{value}</dd>
    </div>
  );
}
