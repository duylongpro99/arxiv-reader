import Link from "next/link";
import { RunsHistory } from "@/components/runs-history";
import { ArrowLeftIcon } from "@/components/icons";

// /runs — browse past runs. Click a row to reopen its full timeline.
export default function RunsPage() {
  return (
    <main className="mx-auto flex w-full max-w-3xl flex-col px-6 pb-16">
      <header className="sticky top-0 z-20 -mx-6 mb-8 flex items-center justify-between gap-4 border-b border-line bg-base/80 px-6 py-4 backdrop-blur">
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
          href="/"
          className="flex shrink-0 items-center gap-1.5 rounded-md border border-line px-2.5 py-1.5 text-xs font-medium text-muted transition-colors hover:border-accent hover:text-ink"
        >
          <ArrowLeftIcon className="h-3.5 w-3.5" />
          New run
        </Link>
      </header>

      <div className="mb-8 flex flex-col gap-2">
        <h1 className="text-2xl font-semibold tracking-tight text-ink">Run history</h1>
        <p className="text-sm leading-relaxed text-muted">
          Every past run, newest first. Reopen one to replay its timeline.
        </p>
      </div>

      <RunsHistory />
    </main>
  );
}
