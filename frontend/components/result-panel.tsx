"use client";

import type { ResultResponse } from "@/lib/types";
import { CheckCircleIcon } from "./icons";
import { MarkdownPreview } from "./markdown-preview";

// ResultPanel is the terminal success view: a banner with the vault path, the
// token usage, and a rendered Markdown preview of the generated note. Shown only
// when the pipeline stage is "complete".
export function ResultPanel({ result }: { result: ResultResponse }) {
  return (
    <div className="flex flex-col gap-5">
      <SuccessBanner vaultFile={result.vaultFile} />
      <TokenUsage result={result} />
      {/* Reader surface: distinct from the instrument chrome — calm, high
          contrast, generous measure. */}
      <div className="rounded-xl border border-line bg-surface p-6 sm:p-8">
        <MarkdownPreview content={result.content} />
      </div>
    </div>
  );
}

function SuccessBanner({ vaultFile }: { vaultFile: string }) {
  return (
    <div className="rounded-xl border border-ok/30 bg-ok-bg p-4">
      <div className="flex items-center gap-2 font-semibold text-ok">
        <CheckCircleIcon className="h-5 w-5 shrink-0" />
        <span>Explainer saved to your vault</span>
      </div>
      <p className="mt-2 break-all font-mono text-xs text-ink/80">{vaultFile}</p>
      <p className="mt-1.5 text-xs text-muted">
        Open it in Obsidian to read the full note.
      </p>
    </div>
  );
}

// TokenUsage shows total tokens and, when the backend knows the model's pricing
// (costKnown), an estimated USD cost with an explicit "approximate" caveat. The
// cost is hidden entirely when costKnown is false/absent — never show a guess.
function TokenUsage({ result }: { result: ResultResponse }) {
  const showCost =
    result.costKnown && typeof result.estimatedCostUSD === "number";
  return (
    <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm text-muted">
      <span>Token usage:</span>
      <span className="font-mono font-medium text-ink tabular-nums">
        {result.tokensUsed.toLocaleString()}
      </span>
      {showCost && (
        <>
          <span aria-hidden>·</span>
          <span className="font-mono font-medium text-ink tabular-nums">
            ~${result.estimatedCostUSD!.toFixed(3)}
          </span>
          <span>estimated</span>
          <span className="text-xs text-muted/80">
            (approximate — check your provider dashboard)
          </span>
        </>
      )}
    </div>
  );
}
