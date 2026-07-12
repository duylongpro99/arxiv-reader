import Link from "next/link";
import { DiscoveryPanel } from "@/components/discovery-panel";
import { ClockIcon } from "@/components/icons";

export default function Home() {
  return (
    <main className="mx-auto flex w-full max-w-3xl flex-col px-6 pb-16">
      {/* Instrument top bar — mono wordmark + secondary nav. Sticky so it reads
          like an app chrome rather than a document header. */}
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
          href="/runs"
          className="flex shrink-0 items-center gap-1.5 rounded-md border border-line px-2.5 py-1.5 text-xs font-medium text-muted transition-colors hover:border-accent hover:text-ink"
        >
          <ClockIcon className="h-3.5 w-3.5" />
          Run history
        </Link>
      </header>

      <div className="mb-8 flex flex-col gap-2">
        <h1 className="text-2xl font-semibold tracking-tight text-ink">
          Discover the latest cs.AI papers
        </h1>
        <p className="max-w-prose text-sm leading-relaxed text-muted">
          Pull the newest arXiv papers, pick one, and get a deep, layered
          explainer written for engineers — saved straight to your Obsidian vault.
          No duplicates, no browsing.
        </p>
      </div>

      <DiscoveryPanel />
    </main>
  );
}
