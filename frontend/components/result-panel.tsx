"use client";

import type { ResultResponse } from "@/lib/types";
import { MarkdownPreview } from "./markdown-preview";

// ResultPanel is the terminal success view: a banner with the vault path, the
// token usage, and a rendered Markdown preview of the generated note. Shown only
// when the pipeline stage is "complete".
export function ResultPanel({ result }: { result: ResultResponse }) {
  return (
    <div className="flex flex-col gap-4">
      <SuccessBanner vaultFile={result.vaultFile} />
      <TokenUsage tokens={result.tokensUsed} />
      <div className="rounded-lg border border-gray-200 bg-white p-5 dark:border-gray-700 dark:bg-gray-900">
        <MarkdownPreview content={result.content} />
      </div>
    </div>
  );
}

function SuccessBanner({ vaultFile }: { vaultFile: string }) {
  return (
    <div className="rounded-lg border border-green-300 bg-green-50 p-4 dark:border-green-800 dark:bg-green-950">
      <div className="flex items-center gap-2 font-semibold text-green-800 dark:text-green-300">
        <span aria-hidden>✓</span>
        <span>Explainer saved to your vault</span>
      </div>
      <p className="mt-1 break-all font-mono text-xs text-green-700 dark:text-green-400">
        {vaultFile}
      </p>
      <p className="mt-2 text-xs text-green-700 dark:text-green-400">
        Open it in Obsidian to read the full note.
      </p>
    </div>
  );
}

function TokenUsage({ tokens }: { tokens: number }) {
  return (
    <div className="text-sm text-gray-600 dark:text-gray-400">
      Token usage:{" "}
      <span className="font-mono font-medium text-gray-800 dark:text-gray-200">
        {tokens.toLocaleString()}
      </span>
    </div>
  );
}
