import Link from "next/link";
import { DiscoveryPanel } from "@/components/discovery-panel";

export default function Home() {
  return (
    <main className="mx-auto flex w-full max-w-3xl flex-col gap-8 px-6 py-12">
      <header className="flex items-start justify-between gap-4">
        <div className="flex flex-col gap-1">
          <h1 className="text-2xl font-bold">ArXiv AI Paper Explainer</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400">
            Find the latest cs.AI papers from arXiv — no duplicates, no browsing.
          </p>
        </div>
        <Link
          href="/runs"
          className="shrink-0 rounded-md border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-800"
        >
          Run history
        </Link>
      </header>
      <DiscoveryPanel />
    </main>
  );
}
