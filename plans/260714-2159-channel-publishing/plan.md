---
title: Channel Publishing (select run → adapt → publish)
status: complete
created: 2026-07-14
completed: 2026-07-15
mode: hard
blockedBy: []
blocks: []
design_note: docs/design-notes/2026-07-14-channel-publishing.md
---

# Plan: Channel Publishing

Publish a persisted run's explainer to social channels. Content is **adapted by a category-blind agent** (never posted raw), reviewed/edited by a human, then pushed to **decoupled in-process channels**. Extends completed Phase 8. v1 channels: **dev.to (`longform`) + X (`brief`)**; daily.dev/RSS (`digest`) is future.

## Locked decisions (from brainstorm)
- Human review + edit before publish. No auto-publish.
- **Content category** taxonomy: `longform` / `digest` / `brief`. Agent knows only the category + its config target length — never a channel. Channels declare which category they consume and own all platform mechanics (X chunks a `brief` into ≤280-char numbered tweets; agent never emits a "thread").
- Select doc from **runs history** (reuse `RunRecord` + `GET /runs/{id}/content`).
- **DB required for publishing** — feature disables itself when Postgres is off; rest of app unaffected.
- **No migrations run by agent** — schema documented, user applies it (`.claude/rules/no-migrations-rule.md`).

## Key seam
`Repurposer agent` (blind to channels, keyed on `Category`) ↔ `Channel` (blind to generation, keyed on its `Category()`). Generate **per unique category**, fan out one editable `Publication` draft per channel.

## Phases (execution waves)

Wave 1 — backend core (parallel; different packages):
- [x] **phase-01** — Channel abstraction + registry + config + Repurposer agent → `phase-01-channels-and-repurposer.md`
- [x] **phase-02** — Publications store + schema (user-run migration) → `phase-02-publications-store.md`

Wave 2 — backend integration (depends on W1):
- [x] **phase-03** — Orchestrator endpoints + wiring + tracing + DB guard → `phase-03-orchestrator-endpoints.md`

Wave 3 — channel implementations (depend on P1 interface; sequence the registry edit):
- [x] **phase-04** — dev.to channel (`longform`, API-key) — prove end-to-end FIRST → `phase-04-channel-devto.md`
- [x] **phase-05** — X channel (`brief`, OAuth2 + thread chunking) — hardest → `phase-05-channel-x.md`

Wave 4 — frontend (depends on P3 contracts):
- [x] **phase-06** — Publish UI from runs history → `phase-06-frontend-publish-ui.md`

Wave 5:
- [x] **phase-07** — Docs sync (architecture / roadmap / changelog) → `phase-07-docs.md`

## Dependencies
- W2 (P3) depends on W1 (P1 interface + DTOs, P2 store methods).
- P4 & P5 depend on P1's `Channel` interface + registry. Both append a case to the registry switch (`internal/channels/registry.go`) — **sequence, don't co-edit** (P4 then P5). Prove dev.to end-to-end before starting X.
- P6 consumes P3's API shapes; can scaffold against documented contracts while P3 in flight.
- P7 last.

## File ownership (no overlaps)
| Phase | Owns |
|-------|------|
| P1 | `backend/internal/channels/{channel.go,registry.go,category.go}`, `internal/agents/repurposer/*`, `internal/config/config.go` (+`config.yaml`), `internal/models/publication.go` (domain DTOs) |
| P2 | `backend/internal/store/{publications.go,model.go}`, `backend/migrations/0002_publications.sql` |
| P3 | `backend/internal/orchestrator/{publish-handlers.go,dto.go}`, `internal/server/server.go`, `cmd/server/main.go` (wiring) |
| P4 | `backend/internal/channels/devto/*`, registry case (append) |
| P5 | `backend/internal/channels/x/*`, registry case (append) |
| P6 | `frontend/app/runs/[id]/*`, `frontend/components/publish-*.tsx`, `frontend/lib/{api.ts,use-publications.ts,types.ts}`, `frontend/app/api/*` proxy routes |
| P7 | `docs/architecture.md`, `docs/development-roadmap.md`, `docs/project-changelog.md` |

Note: P1 & P2 both add to `internal/models` / `internal/store` respectively — distinct files, safe parallel. P3 edits `dto.go` (P1 does not).

## Validation
- Backend: `go build ./...` + `go test ./...` after each phase (sandbox off for Go — cache dirs blocked; see memory).
- Frontend: `npm run build` / `tsc` after P6.
- No migrations executed by agent. Secrets (dev.to key, X tokens) in `.env`, never logged, run through existing `tracing/scrub` scrubber.
- End-to-end gate: select a run → generate `longform`+`brief` drafts → edit+approve → publish → both return live external URLs stored on `publications`; re-publish of same (run, channel) blocked.

## Key risks
- **X OAuth2** is the dominant cost (app registration, user-context token + refresh, write-capped free tier). Isolated to P5 so dev.to (P4) proves the pipeline first. If X auth blocks, ship dev.to alone.
- **DB-required**: publishing endpoints return 503 with a clear message when `store == nil`. Non-publishing app paths untouched.
- **Idempotency**: unique `(run_id, channel_id)`; `POST /publish` on an already-`published` row → 409, no re-post.
- **Run content availability**: `POST /runs/{id}/publications` reuses the vault-file read path; missing file → 4xx with clear error (mirror `HandleRunContent`).
- **Registry shared file** (P4/P5): append-only cases, sequence to avoid conflict.

## Completion & Implementation Deviations (2026-07-15)

**Status:** All 7 phases complete. All tests pass (`go test ./...` green; frontend `tsc`/build green). Code review (3 ship-blockers) resolved.

**Notable deviations from plan:**

1. **Registry Pattern (P1)**: Implemented self-registration (`channels.Register` + `init()` + blank imports in `cmd/server/main.go`) instead of the originally-planned `switch` in `registry.go`. Reason: a direct registry.go switch would have caused a Go import cycle (every channel must import channels.Channel, so channels importing devto/x back would create a cycle). Self-registration (pattern: `database/sql` driver registration) breaks the cycle. **Consequence:** P4/P5 no longer co-edit `registry.go`; each self-registers via init(). The "sequence the shared registry edit" risk is void.

2. **Wiring Location (P3)**: Wiring lives in `internal/orchestrator/orchestrator.go`'s `New()` function, not `cmd/server/main.go`. The ownership table originally stated P3 owns `cmd/server/main.go` wiring; in practice, `main.go` only gains blank imports for channel self-registration, while the actual `Repurposer` + channel factory wiring happens in `orchestrator.New()`. This is cleaner: main.go stays minimal; the wiring is co-located with where these fields live (the Orchestrator struct).

3. **Store-Open Gate (P3)**: Widened from "Tracing.Enabled" to `DatabaseURL != "" || Tracing.Enabled`. Rationale: publishing needs durable state even when tracing is off (a user may want to publish without full run tracing). The store opens for either reason; tracing writers are only wired when tracing is enabled, so publishing never silently turns tracing on.

4. **Tracing for Publications (P3 integration)**: Publications state is persisted best-effort via the store (computing next seq from `ListEvents` + `AppendEvent`), not via a fresh `NewRecorder` call. Reason: a past run's live in-memory recorder is gone at publish time (publish may happen minutes/hours after generation). Computing seq from the store ensures idempotent timeline entries across publish restarts. The initial `dbReadTimeout: 5s` would have cancelled real LLM generation; it was adjusted to `generateTimeout` (LLM budget) and `publishTimeout` (full publish action).

5. **Code Review Findings (applied)**: Three ship-blockers were found and fixed:
   - **Approval Gate**: Server-side guard: a draft cannot be published; status must be "approved" first. Returns 409 if publication is still in "draft" state.
   - **Atomic Publish Guard**: New `ClaimForPublish` transient status prevents concurrent double-posting. Atomically transitions approved/failed → publishing before sending to channel.
   - **Timeout Tuning**: Initial 5s `dbReadTimeout` would cancel every real LLM generation and every publish action. Added `generateTimeout` (scoped to LLM budget, ~100s default) and `publishTimeout` (full action, ~30s default).

**Plan/Code Lockstep Verification (0001 Banner Rule):**
- `backend/migrations/0002_publications.sql` ✓ matches `backend/internal/store/model.go` `PublicationRecord`
- Schema: 12 columns, types, nullability, indices — all verified in sync
- Comments in migration file explain the user-run deployment step
