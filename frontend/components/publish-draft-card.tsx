"use client";

import { useEffect, useRef, useState } from "react";
import type { Publication, PublicationStatus } from "@/lib/types";
import { usePatchPublication, usePublishPublication } from "@/lib/use-publications";
import { CheckCircleIcon, ExternalLinkIcon, SpinnerIcon } from "./icons";
import { MarkdownPreview } from "./markdown-preview";
import { PublishThreadPreview } from "./publish-thread-preview";

const DEBOUNCE_MS = 600;

const STATUS_STYLE: Record<PublicationStatus, { label: string; className: string }> = {
  draft: { label: "Draft", className: "border-line bg-card text-muted" },
  approved: { label: "Approved", className: "border-accent/30 bg-accent-bg text-accent" },
  publishing: { label: "Publishing…", className: "border-accent/30 bg-accent-bg text-accent" },
  published: { label: "Published", className: "border-ok/30 bg-ok-bg text-ok" },
  failed: { label: "Failed", className: "border-err/30 bg-err-bg text-err" },
};

// PublishDraftCard is one (run, channel) draft/attempt: a category-specific
// editor + preview, plus the approve/publish controls and outcome (external
// URL or scrubbed error). Edits are debounced into PATCH requests so a fast
// typist doesn't fire one request per keystroke.
export function PublishDraftCard({
  publication,
  runId,
}: {
  publication: Publication;
  runId: string;
}) {
  const patch = usePatchPublication(runId);
  const publish = usePublishPublication(runId);

  const [title, setTitle] = useState(publication.title ?? "");
  const [content, setContent] = useState(publication.content ?? "");
  // Tracks which publication row the local edit state belongs to — resync
  // only on an actual identity change (a different draft), never on a
  // server-driven status refresh (approve/publish), since edits are owned by
  // the user once mounted.
  const idRef = useRef(publication.id);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    if (idRef.current !== publication.id) {
      idRef.current = publication.id;
      setTitle(publication.title ?? "");
      setContent(publication.content ?? "");
    }
  }, [publication.id, publication.title, publication.content]);

  useEffect(() => {
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, []);

  function scheduleEdit(next: { title?: string; content?: string }) {
    if (timerRef.current) clearTimeout(timerRef.current);
    timerRef.current = setTimeout(() => {
      patch.mutate({ pid: publication.id, body: next });
    }, DEBOUNCE_MS);
  }

  const editable = publication.status !== "published";
  const canApprove = publication.status === "draft";
  const canPublish = publication.status === "approved" || publication.status === "failed";
  const style = STATUS_STYLE[publication.status] ?? STATUS_STYLE.draft;

  return (
    <div className="flex flex-col gap-4 rounded-xl border border-line bg-surface p-5">
      <header className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-baseline gap-2">
          <span className="font-medium text-ink">{publication.channelId}</span>
          <span className="font-mono text-xs text-muted">{publication.category}</span>
        </div>
        <span
          className={`rounded-full border px-2.5 py-0.5 font-mono text-[11px] uppercase tracking-wide ${style.className}`}
        >
          {style.label}
        </span>
      </header>

      {publication.category !== "brief" && (
        <input
          type="text"
          value={title}
          disabled={!editable}
          onChange={(e) => {
            setTitle(e.target.value);
            scheduleEdit({ title: e.target.value });
          }}
          placeholder="Title"
          className="rounded-md border border-line bg-base px-3 py-2 text-sm font-medium text-ink outline-none focus:border-accent disabled:cursor-not-allowed disabled:opacity-60"
        />
      )}

      <textarea
        value={content}
        disabled={!editable}
        onChange={(e) => {
          setContent(e.target.value);
          scheduleEdit({ content: e.target.value });
        }}
        rows={publication.category === "brief" ? 5 : 10}
        className="w-full resize-y rounded-md border border-line bg-base px-3 py-2 font-mono text-sm text-ink outline-none focus:border-accent disabled:cursor-not-allowed disabled:opacity-60"
      />

      {publication.category === "brief" ? (
        <PublishThreadPreview content={content} />
      ) : (
        <div className="rounded-xl border border-line bg-base p-4">
          <MarkdownPreview content={content} />
        </div>
      )}

      <PublishOutcome
        canApprove={canApprove}
        canPublish={canPublish}
        approving={patch.isPending}
        publishing={publish.isPending}
        status={publication.status}
        externalUrl={publication.externalUrl}
        error={publish.error?.message ?? (publication.status === "failed" ? publication.error : undefined)}
        onApprove={() => patch.mutate({ pid: publication.id, body: { approve: true } })}
        onPublish={() => publish.mutate(publication.id)}
      />
    </div>
  );
}

// PublishOutcome renders the approve/publish action row plus the resulting
// external link or scrubbed failure. Split out to keep the card's edit
// surface (above) readable at a glance.
function PublishOutcome({
  canApprove,
  canPublish,
  approving,
  publishing,
  status,
  externalUrl,
  error,
  onApprove,
  onPublish,
}: {
  canApprove: boolean;
  canPublish: boolean;
  approving: boolean;
  publishing: boolean;
  status: PublicationStatus;
  externalUrl?: string;
  error?: string;
  onApprove: () => void;
  onPublish: () => void;
}) {
  return (
    <div className="flex flex-col gap-2 border-t border-line pt-4">
      <div className="flex flex-wrap items-center gap-2">
        {canApprove && (
          <button
            type="button"
            onClick={onApprove}
            disabled={approving}
            className="inline-flex cursor-pointer items-center gap-1.5 rounded-md border border-line px-3 py-1.5 text-sm font-medium text-ink transition-colors hover:border-accent disabled:cursor-not-allowed disabled:opacity-60"
          >
            {approving && <SpinnerIcon className="h-3.5 w-3.5 animate-spin" />}
            Approve
          </button>
        )}
        {canPublish && (
          <button
            type="button"
            onClick={onPublish}
            disabled={publishing}
            className="inline-flex cursor-pointer items-center gap-1.5 rounded-md bg-accent-solid px-3 py-1.5 text-sm font-medium text-on-accent transition-colors hover:bg-accent-solid-hover disabled:cursor-not-allowed disabled:opacity-60"
          >
            {publishing && <SpinnerIcon className="h-3.5 w-3.5 animate-spin" />}
            {publishing ? "Publishing…" : status === "failed" ? "Retry publish" : "Publish"}
          </button>
        )}
        {status === "published" && externalUrl && (
          <a
            href={externalUrl}
            target="_blank"
            rel="noreferrer"
            className="inline-flex items-center gap-1.5 text-sm font-medium text-accent underline decoration-accent/40 underline-offset-2 hover:decoration-accent"
          >
            <CheckCircleIcon className="h-4 w-4" />
            View published post
            <ExternalLinkIcon className="h-3.5 w-3.5" />
          </a>
        )}
      </div>
      {error && (
        <p className="rounded-md border border-err/30 bg-err-bg px-3 py-2 text-sm text-err">
          {error}
        </p>
      )}
    </div>
  );
}
