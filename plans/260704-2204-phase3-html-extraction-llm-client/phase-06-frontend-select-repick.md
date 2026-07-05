# Phase 06 — Frontend Select + Re-pick

**Context:** `docs/phase3/prd.md` §2.1, §2.2, §4 · `brainstorm-summary.md` §7 (re-pick)
**Priority:** High · **Status:** complete · **Depends on:** 05 (API contract) · **Effort:** ~M

## Overview
Wire the `Select` button (disabled since Phase 2). Click → `POST /api/select {session_id, paper_id}`
→ proxy → Go `POST /process`. Once one paper is chosen, disable the other Select buttons
(`selectedId` state). Add the `extracting` stage label. Handle the re-pick path: when status
returns to `selection` *after* a selection (i.e. a notice appears), clear `selectedId`, re-enable
the cards, and show the recoverable notice.

## Key insights (locked decisions)
- **New Next.js has breaking changes** (frontend/AGENTS.md) — read `node_modules/next/dist/docs/`
  before writing route/component code. Mirror the existing `/api/trigger` + `/api/status` proxy
  shape (they read `backendBaseURL()` server-side; backend URL never enters the client bundle).
- **Polling already runs** (Phase 2, TanStack Query, 2s). `extracting` is a non-terminal stage →
  `refetchInterval` must keep polling for it (currently stops only on `selection`/`failed`; adding
  `extracting` to the union does NOT stop polling — good, but re-verify the interval predicate).
- **Re-pick detection:** after a select, if the next status is `selection` *with a notice*, the
  fetch failed recoverably → clear `selectedId`, show notice, re-enable cards. Track a
  "have we selected yet" flag so the initial `selection` (no notice) doesn't false-trigger.
- Selection returns `{session_id}` (same session) — do NOT reset `sessionId`; the panel keeps polling.

## Requirements (PRD F1, F7)
- `PaperCard`: enable Select; `onSelect(paperId)`; `disabled` when another is selected.
- `POST /api/select` proxy → Go `POST /process`; relay `{session_id}` / errors (mirror trigger route).
- `DiscoveryPanel`: `selectedId` state; `useMutation` for select; disable all cards while a
  selection is in flight or active; on re-pick (`selection` + notice after select) clear `selectedId`.
- Stage label `extracting: "Extracting paper text..."`; render notice above the list on re-pick.
- `lib/types.ts`: add `"extracting"` to `PipelineStage`.

## Related code files
**Create:**
- `frontend/app/api/select/route.ts` — `POST` proxy → `${backendBaseURL()}/process`, relay body.
- `frontend/lib/api.ts` — add `selectPaper(sessionId, paperId): Promise<{session_id}>` (mirror
  `triggerDiscovery`).
**Modify:**
- `frontend/lib/types.ts` — add `"extracting"` to `PipelineStage`; add `SelectResponse` if useful.
- `frontend/components/paper-card.tsx` — enable button; `onSelect`/`disabled`/`selected` props.
- `frontend/components/candidate-list.tsx` — thread `selectedId` + `onSelect` down to cards.
- `frontend/components/discovery-panel.tsx` — `selectedId` state, select mutation, re-pick reset,
  pass handlers into `CandidateList`.
- `frontend/components/progress-indicator.tsx` — add the `extracting` label (see stage-label map).

## Design detail
```ts
// lib/types.ts
export type PipelineStage =
  | "discovery" | "selection" | "extracting" | "failed"
  | "generating" | "reviewing" | "revising" | "writing" | "complete";
// (drops the never-used "fetching_pdf" — HTML path replaces it; confirm no references remain)

// app/api/select/route.ts (mirror trigger route; server-side backend URL)
export async function POST(request: Request) {
  const { session_id, paper_id } = await request.json();
  try {
    const res = await fetch(`${backendBaseURL()}/process`, {
      method: "POST", headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ session_id, paper_id }),
    });
    const body = await res.text();
    return new Response(body, { status: res.status, headers: { "Content-Type": "application/json" } });
  } catch { return Response.json({ error: "Cannot reach the service..." }, { status: 502 }); }
}
```
```tsx
// discovery-panel.tsx additions (sketch)
const [selectedId, setSelectedId] = useState<string | null>(null);
const hasSelected = useRef(false);
const select = useMutation({
  mutationFn: (paperId: string) => selectPaper(sessionId!, paperId),
  onSuccess: (_r, paperId) => { setSelectedId(paperId); hasSelected.current = true; },
});
// re-pick: status came back to selection WITH a notice after we had selected → reset
useEffect(() => {
  if (status?.stage === "selection" && hasSelected.current && status.notice) {
    setSelectedId(null); // re-enable cards; notice renders via CandidateList
  }
}, [status?.stage, status?.notice]);

<CandidateList candidates={status.candidates ?? []} notice={status.notice}
  selectedId={selectedId} onSelect={(id) => select.mutate(id)} />
```
> **`extracting` keeps polling:** verify the `refetchInterval` predicate returns `2000` for
> `extracting` (it stops only on `selection`/`failed`). No change needed if the predicate is a
> denylist; confirm it isn't an allowlist.
>
> **Card disabled logic:** a card is `disabled` when `selectedId != null` (another chosen) or the
> select mutation `isPending`; the chosen card shows a "Selected"/spinner state.

## Implementation steps
1. `types.ts`: add `extracting`; sweep for `fetching_pdf` references and remove if unused.
2. `lib/api.ts`: `selectPaper()`.
3. `app/api/select/route.ts`: proxy (read Next docs for current route handler API).
4. `paper-card.tsx`: enable button + `onSelect`/`disabled`/`selected` props.
5. `candidate-list.tsx`: pass `selectedId`/`onSelect` through.
6. `discovery-panel.tsx`: state, mutation, re-pick `useEffect`, wire handlers.
7. `progress-indicator.tsx`: `extracting` label.
8. `npm run build` + `lint` clean; manual click-through against running backend (200 + 404 re-pick).

## Todo
- [x] `types.ts`: add `extracting` (remove dead `fetching_pdf` if unreferenced)
- [x] `lib/api.ts`: `selectPaper(sessionId, paperId)`
- [x] `app/api/select/route.ts` proxy (backend URL server-side)
- [x] `paper-card.tsx`: enable Select + onSelect/disabled/selected
- [x] `candidate-list.tsx`: thread selectedId/onSelect
- [x] `discovery-panel.tsx`: selectedId state + select mutation + re-pick reset
- [x] `progress-indicator.tsx`: "Extracting paper text..." label
- [x] `npm run build` + lint clean; manual E2E (select + 404 re-pick)

## Success criteria
- Clicking Select transitions UI to `extracting` ("Extracting paper text...").
- Other Select buttons disable once one is chosen.
- 404 re-pick: status back to `selection` with notice → cards re-enable, `selectedId` cleared,
  notice shown; user picks another without a page reload / new session.
- Backend URL never appears in the client bundle.

## Risk Assessment
| Risk | L×I | Mitigation |
|---|---|---|
| New Next.js route/handler API differs from training data | Med×Med | Read `node_modules/next/dist/docs/` first (AGENTS.md mandate); mirror existing trigger/status routes. |
| Re-pick false-trigger on the first `selection` | Med×Med | Gate on `hasSelected` ref + presence of `notice`; initial selection has no notice. |
| `extracting` accidentally stops polling | Low×High | Verify `refetchInterval` is a denylist (`selection`/`failed` only). |
| Type drift vs Go DTO | Low×Med | `stage` values must match Go consts exactly; verify against Phase 01/05. |

## Backwards compatibility
Discovery + polling behavior preserved. Adding `extracting` to the union is additive; existing
stages/labels unchanged. Removing `fetching_pdf` is safe only if unreferenced (sweep first).

## Rollback
Revert component/type edits; delete `app/api/select/route.ts` and `selectPaper`. Backend unaffected.

## Security (PRD §2.2)
- Backend base URL stays server-side in the proxy route (never in client JS) — same invariant as
  `/api/trigger` and `/api/status`.
- `paper_id`/`session_id` are opaque strings forwarded verbatim; the backend re-validates.

## Next Steps
Feeds **Phase 07** end-to-end verification. File ownership: this phase owns all `frontend/` files
listed — no other phase edits the frontend.
