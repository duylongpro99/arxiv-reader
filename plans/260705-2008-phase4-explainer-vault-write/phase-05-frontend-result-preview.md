# Phase 05 — Frontend Result Preview

**Context:** `docs/phase4/brainstorm-summary.md` §4 (Frontend) · `docs/phase4/prd.md` F6/F7 · `frontend/components/discovery-panel.tsx`, `frontend/lib/*`
**Priority:** High · **Status:** complete · **Depends on:** 04 (API contract) · **Effort:** ~M

## Overview
When the pipeline reaches `complete`, fetch `GET /api/result?sessionId=…` and render a result
panel: success banner (vault path), token count, and a Markdown preview of the note. Add the three
new progress labels so the user sees `generating → writing → complete` live.

## ⚠️ Read first — modified Next.js
`frontend/AGENTS.md`: *"This is NOT the Next.js you know… Read the relevant guide in
`node_modules/next/dist/docs/` before writing any code."* Before touching routes/components, read
the route-handler + app-router docs there. Match the existing proxy pattern in
`app/api/{status,select,trigger}/route.ts` exactly (they are the ground truth for this build's API).

## Key insights (locked decisions)
- **`complete` is terminal** — extend the `refetchInterval` denylist in `discovery-panel.tsx`
  (currently stops on `selection`/`failed`) to also stop on `complete`. Same for the `polling` flag.
- **New proxy route mirrors `status/route.ts`** (query param `sessionId`, relay body + status,
  502 on unreachable backend). Backend base URL stays server-side (`lib/backend.ts`).
- **Types already declare the stages** (`generating/writing/complete` exist in `lib/types.ts`).
  Add only `ResultResponse` + a `fetchResult` helper in `lib/api.ts`.
- **Result fetch is a separate query**, enabled only when `status.stage === "complete"` (one-shot,
  no polling). Keeps the status poll unchanged.
- **Two new deps:** `react-markdown`, `remark-gfm` (GFM tables for the glossary; bold; links).

## Requirements (PRD F6, F7)
- `app/api/result/route.ts` — `GET /api/result?sessionId=xxx` → `GET {backend}/result/{sessionId}`,
  relay unchanged (incl. 404 "result not ready"); 502 on fetch throw. (Mirror `status/route.ts`.)
- `lib/types.ts` — `export interface ResultResponse { content: string; vaultFile: string; tokensUsed: number }`.
- `lib/api.ts` — `fetchResult(sessionId): Promise<ResultResponse>` (throws on non-OK, like `selectPaper`).
- `components/progress-indicator.tsx` — add labels: `generating: "Generating explainer…"`,
  `writing: "Saving to vault…"`, `complete: "Complete"`. (`complete` shows no spinner — gate the
  spinner on `stage !== "selection" && stage !== "complete"`.)
- `components/result-panel.tsx` — new: props `{ result: ResultResponse }`; renders
  `SuccessBanner` (✓ + vault path + "Open in Obsidian" hint), `TokenUsage` (count), and
  `MarkdownPreview` (`react-markdown` + `remark-gfm`). Keep <200 lines; small sub-components can
  live in the same file or split (`success-banner.tsx`, `token-usage.tsx`, `markdown-preview.tsx`)
  to respect the size rule.
- `components/discovery-panel.tsx` — when `status?.stage === "complete"`, run the result query and
  render `<ResultPanel result={result} />`; extend the poll denylist + `polling` flag for `complete`.

## Related code files
**Create:**
- `frontend/app/api/result/route.ts`
- `frontend/components/result-panel.tsx` (+ optional `markdown-preview.tsx`, `success-banner.tsx`, `token-usage.tsx`)

**Modify:**
- `frontend/lib/types.ts` — `ResultResponse`.
- `frontend/lib/api.ts` — `fetchResult`.
- `frontend/components/progress-indicator.tsx` — 3 labels + spinner gate.
- `frontend/components/discovery-panel.tsx` — result query (enabled on `complete`) + render + poll denylist.
- `frontend/package.json` — add `react-markdown`, `remark-gfm` (install with the repo's package manager).

## Design detail
```tsx
// discovery-panel.tsx — result query, gated on complete
const { data: result } = useQuery({
  queryKey: ["result", sessionId],
  queryFn: () => fetchResult(sessionId as string),
  enabled: status?.stage === "complete",
});
// poll denylist:
refetchInterval: (q) => {
  const s = q.state.data?.stage;
  return s === "selection" || s === "failed" || s === "complete" ? false : 2000;
},
// render:
{status?.stage === "complete" && result && <ResultPanel result={result} />}
```
```tsx
// markdown-preview.tsx
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
export function MarkdownPreview({ content }: { content: string }) {
  return <div className="prose …"><ReactMarkdown remarkPlugins={[remarkGfm]}>{content}</ReactMarkdown></div>;
}
```
> **Note content includes YAML frontmatter** (the backend returns `explainer.Content`, which is the
> body WITHOUT frontmatter — verify: Phase 04 `/result` returns `Explainer().Content`, and
> `ExplainerOutput.Content` is the LLM body only; frontmatter is added by VaultWriter at write time
> and is NOT in `Content`). So the preview renders clean Markdown with no frontmatter. Confirm this
> during implementation; if frontmatter ever leaks in, strip a leading `---…---` block before render.
> **Tailwind typography:** if `@tailwindcss/typography` (`prose`) isn't installed, style headings/
> lists/tables with plain utility classes — do NOT add another dep just for `prose`.

## Implementation steps
1. Read `node_modules/next/dist/docs/` route-handler guide; confirm the proxy pattern.
2. Add `react-markdown` + `remark-gfm` to `package.json`; install.
3. `app/api/result/route.ts` (mirror `status/route.ts`).
4. `lib/types.ts` `ResultResponse`; `lib/api.ts` `fetchResult`.
5. `progress-indicator.tsx` labels + spinner gate.
6. `result-panel.tsx` (+ sub-components) with `react-markdown`.
7. `discovery-panel.tsx`: result query, render, poll denylist, `polling` flag.
8. `npm run build` (or repo script) + `npm run lint` green; manual smoke against a live backend.

## Todo
- [x] Read modified-Next.js docs before coding
- [x] add `react-markdown` + `remark-gfm`
- [x] `app/api/result/route.ts` proxy (mirror status route)
- [x] `ResultResponse` type + `fetchResult` helper
- [x] progress labels `generating`/`writing`/`complete` + spinner gate
- [x] `ResultPanel` (SuccessBanner + TokenUsage + MarkdownPreview), files <200 lines
- [x] wire into `discovery-panel.tsx`; `complete` terminal in poll denylist + `polling`
- [x] `build` + `lint` green; manual smoke test

## Success criteria
- On `complete`: success banner with vault path, token count, and rendered Markdown (headings,
  bold, GFM tables for the glossary, links) — no raw Markdown, no frontmatter block.
- Progress shows `Generating explainer… → Saving to vault… → Complete`.
- Polling stops at `complete` (no needless refetch loop).

## Risk Assessment
| Risk | L×I | Mitigation |
|---|---|---|
| Modified Next.js API differs from training data | Med×Med | Read `node_modules/next/dist/docs/`; copy the existing route pattern verbatim. |
| Result query fires before backend ready | Low×Low | `enabled: stage === "complete"` gates it; `/result` 404s otherwise (handled). |
| `react-markdown` renders unsafe HTML | Low×Med | Default react-markdown does NOT render raw HTML; do not add `rehype-raw`. |
| Missing `prose` styles look unstyled | Med×Low | Fall back to utility classes; no new dep. |

## Backwards compatibility
Discovery/selection/extraction UI unchanged. New behavior only on the previously-unreached
`complete` stage. Proxy pattern identical to existing routes.

## Rollback
Remove `result-panel.tsx` + `app/api/result/route.ts`, revert `discovery-panel.tsx`/`api.ts`/
`types.ts`/`progress-indicator.tsx`, drop the two deps. No backend impact.

## Security
Backend base URL stays server-side (proxy). No secrets in client bundle. react-markdown sanitizes
by default (no raw HTML). CORS unchanged (server-side proxy calls the backend).

## Next Steps
Completes the user-visible loop. Feeds Phase 06 verification (manual UI smoke + exit criteria).
