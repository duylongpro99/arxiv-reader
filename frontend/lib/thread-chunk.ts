// Client-side PREVIEW mirror of the backend's X/brief thread chunker
// (backend/internal/channels/x/chunk.go). Intentionally a simpler
// approximation — word-boundary greedy packing only, no sentence-boundary
// preference — good enough to preview tweet boundaries while the user edits.
// The backend remains the source of truth for what actually gets published.

const MAX_TWEET_LEN = 280;
// Bounds the counter-width fixed-point loop below. Shrinking the budget can
// only grow segment count, which can only grow-or-hold the counter's digit
// width, so this converges in 1-2 passes for any realistic body — same
// reasoning as chunk.go's maxCounterFixups.
const MAX_COUNTER_FIXUPS = 8;

function counterText(i: number, n: number): string {
  return ` (${i}/${n})`;
}

// splitParagraphs breaks body on blank lines and collapses each paragraph's
// internal whitespace to single spaces, like splitParagraphs in chunk.go.
function splitParagraphs(body: string): string[] {
  return body
    .trim()
    .split(/\n\s*\n+/)
    .map((p) => p.split(/\s+/).filter(Boolean).join(" "))
    .filter((p) => p.length > 0);
}

// hardSplit is the last resort for a single word longer than budget — chopped
// into budget-sized (code-point-safe) pieces.
function hardSplit(word: string, budget: number): string[] {
  const out: string[] = [];
  const chars = Array.from(word);
  for (let i = 0; i < chars.length; i += budget) {
    out.push(chars.slice(i, i + budget).join(""));
  }
  return out;
}

// wrapParagraph greedily packs words into segments up to budget; a single
// word longer than budget is hard-split.
function wrapParagraph(para: string, budget: number): string[] {
  const out: string[] = [];
  let cur = "";
  for (const word of para.split(/\s+/).filter(Boolean)) {
    if (word.length > budget) {
      if (cur) out.push(cur);
      cur = "";
      out.push(...hardSplit(word, budget));
      continue;
    }
    const candidate = cur ? `${cur} ${word}` : word;
    if (candidate.length <= budget) {
      cur = candidate;
    } else {
      if (cur) out.push(cur);
      cur = word;
    }
  }
  if (cur) out.push(cur);
  return out;
}

function wrap(body: string, budget: number): string[] {
  const safeBudget = Math.max(budget, 1); // guard: keeps hardSplit terminating
  const out: string[] = [];
  for (const para of splitParagraphs(body)) {
    out.push(...wrapParagraph(para, safeBudget));
  }
  return out;
}

// chunkThreadPreview splits body into segments <=280 chars each, appending a
// " (i/N)" counter once more than one segment is produced (a lone tweet stays
// counter-free). Mirrors chunk()'s counter-reservation strategy: the budget is
// shrunk up front to fit the widest possible counter so no segment ever
// "almost fits" and needs a further split once counters are appended.
export function chunkThreadPreview(body: string): string[] {
  const trimmed = body.trim();
  if (!trimmed) return [];

  let segments = wrap(trimmed, MAX_TWEET_LEN);
  if (segments.length <= 1) return segments;

  let n = segments.length;
  for (let i = 0; i < MAX_COUNTER_FIXUPS; i++) {
    const budget = MAX_TWEET_LEN - counterText(n, n).length;
    segments = wrap(trimmed, budget);
    if (segments.length === n) break;
    n = segments.length;
  }

  return segments.map((seg, i) => seg + counterText(i + 1, segments.length));
}
