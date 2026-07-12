import Link from "next/link";
import { RunsHistory } from "@/components/runs-history";

// /runs — browse past runs. Click a row to reopen its full timeline.
export default function RunsPage() {
  return (
    <main className="mx-auto flex w-full max-w-3xl flex-col gap-6 px-6 py-12">
      <header className="flex items-center justify-between">
        <div className="flex flex-col gap-1">
          <h1 className="text-2xl font-bold">Run history</h1>
          <p className="text-sm text-gray-500 dark:text-gray-400">
            Every past run, newest first. Reopen one to replay its timeline.
          </p>
        </div>
        <Link
          href="/"
          className="rounded-md border border-gray-300 px-3 py-1.5 text-sm text-gray-700 hover:bg-gray-50 dark:border-gray-600 dark:text-gray-200 dark:hover:bg-gray-800"
        >
          ← New run
        </Link>
      </header>
      <RunsHistory />
    </main>
  );
}
