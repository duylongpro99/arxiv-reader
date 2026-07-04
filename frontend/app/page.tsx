import { DiscoveryPanel } from "@/components/discovery-panel";

export default function Home() {
  return (
    <main className="mx-auto flex w-full max-w-3xl flex-col gap-8 px-6 py-12">
      <header className="flex flex-col gap-1">
        <h1 className="text-2xl font-bold">ArXiv AI Paper Explainer</h1>
        <p className="text-sm text-gray-500 dark:text-gray-400">
          Find the latest cs.AI papers from arXiv — no duplicates, no browsing.
        </p>
      </header>
      <DiscoveryPanel />
    </main>
  );
}
