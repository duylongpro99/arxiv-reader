# Development Roadmap
## ArXiv AI Paper Explainer Agent

---

## Overview

The project is organized into sequential phases, each delivering a complete, working slice of functionality. As of **2026-07-19**, Phase 10 is complete. The system can discover papers with pagination via a declarative resource engine (discovery is no longer hardcoded to arXiv but driven by YAML declarations, with the core depending only on the `Source` interface), extract content, generate and review explainers, write them to Obsidian, provide a complete live and persistent timeline of every run, replay run history with full reasoning traces, and publish explainers to social channels (dev.to and X) with human review and editing.

| Phase | Focus | Status | Completion |
|---|---|---|---|
| **1** | Scaffolding & Config | ✅ Complete | Phase 1 PR merged |
| **2** | Discovery & Deduplication | ✅ Complete | Phase 2 PR merged |
| **3** | HTML Extraction & LLM Client | ✅ Complete | Phase 3 PR merged |
| **4** | Explainer & Vault Write | ✅ Complete | 2026-07-05 |
| **5** | Reviewer & Revision Loop | ✅ Complete | 2026-07-05 |
| **6** | Polish & Hardening | ✅ Complete | 2026-07-05 |
| **7** | Run Timeline Tracing | ✅ Complete | 2026-07-12 |
| **8** | Full Reasoning Trace + Pagination | ✅ Complete | 2026-07-13 |
| **9** | Declarative Resource Engine | ✅ Complete | 2026-07-18 |
| **10** | Channel Publishing (dev.to + X) | ✅ Complete | 2026-07-19 |

---

## Phase 1 — Project Scaffolding & Config
**Status:** ✅ Complete

**Deliverables:**
- Next.js 16.2.7 LTS frontend with TypeScript and Tailwind CSS
- Go backend with ADK integration
- Config loader (YAML + `.env` override)
- Local server startup (`localhost:3000` and `localhost:8080`)

**Key Files:**
- `/frontend`: Next.js app structure
- `/backend`: Go service with orchestrator
- `/config.yaml`: Default configuration
- `.env.example`: Template for machine-specific overrides

---

## Phase 2 — Paper Discovery & Deduplication
**Status:** ✅ Complete

**Deliverables:**
- DiscoveryTool: Fetch top 5 papers from arXiv `cs.AI` category
- LogCheckTool: Track processed papers in local JSON log
- Selection UI: Display 5 candidates with title, authors, abstract
- User selection flow: Record picked paper in session

**Key Files:**
- `/backend/internal/tools/discovery.go`
- `/backend/internal/tools/logcheck.go`
- `/backend/internal/models/paper.go`
- `/backend/internal/models/session.go`
- `/frontend/components/PaperSelection.tsx`

**Success Metrics:**
- Zero duplicate papers presented across runs
- Papers correctly filtered against processed log
- UI renders all 5 candidates immediately after discovery

---

## Phase 3 — HTML Extraction & LLM Client
**Status:** ✅ Complete

**Deliverables:**
- PaperContentTool: Fetch arXiv HTML, convert to clean Markdown
- Pure-Go HTML→Markdown conversion (no CGO, no PDF rasterization)
- LLMClient interface with text-only design
- Anthropic, OpenAI, Google Gemini implementations
- Retry logic (429, 503, 400)
- 404 recovery: return to selection, re-pick

**Key Files:**
- `/backend/internal/tools/papercontent.go`
- `/backend/internal/llm/client.go`
- `/backend/internal/llm/anthropic.go`, `openai.go`, `gemini.go`
- `/backend/internal/llm/retry.go`
- `/backend/internal/orchestrator/orchestrator-pipeline.go`

**Success Metrics:**
- Markdown extraction preserves headings and figure captions
- All LLM calls return separate input/output token counts
- 404 gracefully returns user to selection without crashing

---

## Phase 4 — Explainer Generation & Vault Write
**Status:** ✅ Complete (2026-07-05)

**Deliverables:**
- ExplainerAgent: LLM-powered re-teaching of paper content (text-only)
- 9-section structured output (Problem Statement, Core Idea, Methodology, Key Findings, Limitations, Why It Matters, Analogies & Intuition, Glossary, Follow-Up Papers)
- VaultWriterTool: Atomic write to Obsidian vault (`.tmp` → `rename`)
- YAML frontmatter with metadata (arxiv_id, title, authors, published, category, generated_at, review_iterations, review_passed, tags)
- LogCheckTool.MarkAsProcessed: Update processed log after successful write
- GET /result/:sessionId endpoint: Return generated content + vault path + token usage
- Next.js preview: Render Markdown with react-markdown + remark-gfm
- Progress indicators: "generating" and "writing" stages

**Key Files:**
- `/backend/internal/agents/explainer.go`
- `/backend/internal/agents/explainer-prompt.go`
- `/backend/internal/tools/vaultwriter.go`
- `/backend/internal/tools/vaultwriter-frontmatter.go`
- `/backend/internal/models/explainer.go`
- `/frontend/components/ResultPanel.tsx`
- `/frontend/app/api/result/route.ts`

**Architecture:**
- Text-only design: paper Markdown extracted in Phase 3, no images or vision
- Atomic vault write prevents partial files on disk
- Token accumulation across phases
- Session accessors (mutex-guarded, server-only)
- No ReviewVerdict in Phase 4 (Phase 5 feature)

**Success Metrics:**
- All 9 sections present in generated output for every paper
- YAML frontmatter renders correctly in Obsidian
- ~2,500-word soft target met for typical papers
- Atomic write guarantees no `.tmp` files left on any failure
- processed.json updated only after successful vault write
- Token usage displayed in UI
- Markdown preview renders correctly

**Documentation:**
- `docs/phase4/prd.md`: Fully reconciled to text-only implementation
- `docs/architecture.md`: §2.6 & §2.8 aligned with shipped signatures
- `docs/phase4/brainstorm-summary.md`: Design decision rationale

---

## Phase 5 — Reviewer & Revision Loop
**Status:** ✅ Complete (2026-07-05)

**Deliverables:**
- **ReviewerAgent**: Independent critic evaluating explainer against 6-criteria quality rubric
  - Evaluates explainer text alone (source paper not sent — cost optimization)
  - Returns structured ReviewVerdict with Pass (gates the loop), Score (advisory), and per-section Feedback
  - Reuses same LLM client as ExplainerAgent but with distinct system prompt + low temperature (0.1)
  - Error handling: malformed JSON stops loop gracefully with pass=false (no blind regen)
  
- **Bounded critic-generator loop**: Generate → Review → (Revise if fail & iterations remain) → Repeat
  - Loop terminates when reviewer approves OR max iterations reached
  - Always writes exactly one note (the last explainer generated)
  - Respects max_review_iterations config knob (0 disables reviewer entirely, reproduces Phase 4)
  - Configurable cost: default max=2 ≈ 200k tokens/paper
  
- **ReviewVerdict data model**: Pass/Fail decision with per-section feedback map
  - Iteration tracking: which review round (1st, 2nd, etc.)
  - Token accounting: separate count for each review call
  
- **Frontmatter enhancement**: Vault notes now reflect real verdict
  - review_iterations: actual count from verdict (0 if reviewer disabled)
  - review_passed: boolean approval status
  - review_score: quality score (omitted if reviewer disabled)
  
- **Progress UI updates**: Status DTO and frontend surface Phase 5 states
  - New stages: "reviewing" and "revising"
  - Iteration counter visible during polling
  - Progress message: "Reviewing (pass 1)…" and "Revising (pass 2)…"
  
- **Configuration**: New agent.max_review_iterations setting
  - Default: 2 (bounded loop with cost ≈ 200k tokens/paper)
  - Set to 0 to disable reviewer (Phase 4 behaviour at zero reviewer cost)

**Key Files:**
- `/backend/internal/agents/reviewer.go`: ReviewerAgent implementation
- `/backend/internal/models/review.go`: ReviewVerdict struct
- `/backend/internal/agents/revision-note.go`: Format feedback into revision prompts
- `/backend/internal/orchestrator/orchestrator-pipeline.go`: Bounded loop implementation
- `/backend/internal/tools/vaultwriter-frontmatter.go`: Frontmatter rendering with verdict fields
- `/config.yaml`: agent.max_review_iterations setting (default 2)

**Architecture:**
- **Design Decision 1 (Policy):** Pass is single source of truth; Score never gates the loop
- **Design Decision 2 (Fault Handling):** Malformed reviewer JSON stops loop and saves with pass=false (no blind regen)
- **Text-only reviewer:** Paper Markdown intentionally NOT sent to reviewer (cost optimization per T3)
- **Shared LLM:** Reviewer and explainer use same LLM client with different system prompts + temperature
- **Error resilience:** Reviewer LLM/network error fails session recoverably; parse errors save with pass=false

**Dependencies:**
- Phase 4 complete (ExplainerAgent and VaultWriterTool working)
- All 9 explainer sections stable (reviewer targets consistent structure)
- Config system supporting max_review_iterations knob

---

## Phase 6 — Polish & Hardening
**Status:** ✅ Complete (2026-07-05) — cross-provider E2E validation is a manual user task (see `docs/phase6/e2e-validation.md`)

**Delivered:**
- **Retry from failed stage (F2):** `POST /retry/{sessionId}` resumes the pipeline
  from the failed segment via cached outputs — selection preserved, no LLM recompute
  on a transient vault failure. Never resumes mid-review-loop.
- **Error action hints:** `describe*` mappings return a machine-readable action
  (`retry` / `fix_config` / `fix_permissions`) surfaced via `StatusResponse.errorAction`.
- **Cost estimation (F3):** `llm/pricing.go` + `/result` cost fields; UI hides cost
  for unpriced models.
- **Context-window pre-check (F4):** `llm/limits.go` + non-blocking `ContextWarning`
  (len/4 heuristic); pipeline always proceeds.
- **arXiv retry counter (F5):** `FetchPapers` `onRetry` callback → `arxivRetryCount`
  → "Connecting to arXiv (retry n/3)…" label.
- **Logging & security audit (F6/F1):** split input/output tokens on LLM-complete;
  cost + review outcome on `pipeline complete`; uniform stage-failure log
  (stage/action/recoverable/cause); source-scanning test asserts no secret is logged.
- **Frontend integration:** `/api/retry` route, retry wiring (preserves selection),
  context-warning banner, arXiv retry label, cost display.
- **Docs (F11):** README provider/cost tables, full config reference, troubleshooting,
  project map (poppler explicitly absent — HTML pipeline, no PDFs).

**Realignment note:** the original Phase 6 PRD targeted a PDF/vision architecture
that this project never used. All poppler/DPI/PDF-render scope was dropped; every
change extends the existing HTML→Markdown design (see `docs/phase6/brainstorm-summary.md`).

**Key Improvements:**
- Observability: split token accounting + estimated cost on the pipeline-complete log.
- Resilience: segment-level resume; transient vault failure re-writes at zero LLM cost.
- User experience: actionable error hints, retry progress, non-blocking context advisory.

---

## Phase 7 — Run Timeline Tracing
**Status:** ✅ Complete (2026-07-12)

**Delivered:**
- **RunStore & EventStore** — pgx/v5 Postgres access layer for durable timeline
  - Graceful degrade: ErrStoreUnavailable if DB unavailable; pipeline continues
  - Tables: `runs` (per-session header), `run_events` (ordered timeline per run)
  
- **RunRecorder** — per-run monotonic event sequencer with bounded ring buffer
  - In-memory buffer (configurable, default 256 events)
  - Async single-writer persistent flush to Postgres
  - Degrade-safe: works in-memory if store unavailable

- **SSE Broker** — non-blocking per-run fan-out to live clients
  - Last-Event-ID resume support for reconnecting clients
  - Closes on run completion/failure

- **Event Taxonomy** — standardized event kinds across all pipeline stages
  - discovery.started, tool.discovery.completed, selection.chosen, llm.reviewer.completed, etc.
  - status: info/success/warning/error; summary (JSONB); optional payload_full

- **Secret Scrubber** — redacts API keys, caps previews, no raw HTML/markdown

- **Orchestrator Instrumentation** — emits events at every decision point
  - Created at startup with degrade-safe store.Open
  - Only completion/non-recoverable failure closes recorder

- **Transport Endpoints**
  - `GET /runs/{id}/events` — SSE stream with Last-Event-ID replay
  - `GET /runs` — paginated history list (newest first)
  - `GET /runs/{id}` — single run header + full timeline

- **Frontend Components**
  - `/runs` history page with list of all runs
  - `/runs/[id]` individual run detail with live timeline (SSE) or loaded history
  - RunTimeline, RunEventRow, RunsHistory components
  - useEventSource hook for SSE client with auto-reconnect
  - useRuns hook for history list with TanStack Query
  - Live timeline mounted in discovery-panel during active run
  - Navigation link from home page to runs history

- **Infra & Config**
  - `docker-compose.yml` — postgres:17-alpine with healthcheck and named volume
  - `backend/migrations/0001_run_timeline.sql` — USER-RUN migration (safe to re-run)
  - `tracing: { enabled, full_payloads, buffer_size }` config block
  - `DATABASE_URL` from `.env` (documented in `.env.example`)

**Key Architecture Points:**
- **Additive & Optional:** Tracing never blocks the paper pipeline; works in-memory if DB unavailable
- **Dual-Write:** Events go to ring buffer (live SSE) + async Postgres persist (durable history)
- **Degrade Guarantee:** No DATABASE_URL or Postgres down → live SSE works; history returns 503
- **Event Lifecycle:** Each event has seq (resume key), timestamp, stage, and optional full payload (opt-in)

**Dependencies:**
- Phase 6 complete (pipeline stable)
- New: `github.com/jackc/pgx/v5` (Go driver)
- New: postgres:17-alpine (Docker, optional)

**Test Coverage:**
- Store: CRUD operations against test Postgres
- Recorder: seq ordering, buffer wraparound, async persistence
- Broker: per-run fan-out, subscriber cleanup, client disconnect
- Orchestrator: full event sequence for pass/revise/404/failure scenarios
- SSE: replay-from-Last-Event-ID, orphaned run terminal event synthesis

**Migration Guide:**
1. Optional: `docker compose up -d db` to start Postgres
2. Optional: `psql "$DATABASE_URL" -f backend/migrations/0001_run_timeline.sql` to apply schema
3. No code changes required — recorder degrades if DB unavailable
4. Frontend: new `/runs` history page and live timeline UI automatically enabled

---

## Phase 8 — Full Reasoning Trace + History Replay + arXiv Pagination
**Status:** ✅ Complete (2026-07-13)

**Delivered:**
- **PayloadFull Enrichment** — explainer/reviewer events now include full prompt+response (Phase 7 was opt-in summary only)
  - config `tracing.full_payloads` switch (default false; Phase 7 unchanged without edit)
  - Distinct caps: summary ~500 chars, payload_full ~100k chars (avoid truncation)
  - Explainer events: {systemPrompt, userPrompt, response}
  - Reviewer events: same + decision events with {decision, onPass, flaggedSections, narrative}

- **History Content Re-Show** — GET /runs/{id}/content endpoint
  - Reads persisted Obsidian .md from vault path (sourced from tool.vaultwriter.completed event)
  - Path traversal guarded by exported tools.ValidateWithinVault
  - Graceful degradation: returns {available: false} if file missing (HTTP 200)
  - Frontend history detail renders note markdown + timeline together

- **arXiv Pagination** — offset-based "Load more" within a session
  - DiscoveryTool.FetchPapersFrom(start int) offset parameter
  - POST /discover/{sessionId}/more extends candidates during StageSelection
  - Session tracks ConsumeNextStart cursor for next offset
  - Guarded: returns 409 if not in selection stage
  - Frontend accumulates candidates, dedup by ID

**Key Files:**
- Backend: tracing/scrub.go (distinct caps), run events for PayloadFull content, discovery.go (FetchPapersFrom)
- Endpoints: GET /runs/{id}/content, POST /discover/{sessionId}/more
- Frontend: GET /api/runs/{id}/content route, "Load more" button, history content panel

**Architecture Notes:**
- No new database schema: reuses run_events.payload_full JSONB from Phase 7
- Session.ConsumeNextStart new field for pagination cursor
- VaultFile field in PipelineSession enables content lookup

**Test Coverage:**
- PayloadFull scrubbing with distinct caps
- Missing vault file returns {available: false} gracefully
- Pagination cursor tracking and offset accumulation
- Dedup logic in frontend candidate merge

---

## Phase 9 — Declarative Resource Engine
**Status:** ✅ Complete (2026-07-18)

**Delivered:**
- **Resource Engine** — declarative, config-driven abstraction for paper discovery
  - Four-stage pipeline: transport (HTTP) → decoder (format-specific) → normalize (canonical models) → ACL (validation)
  - Source interface: implementations provide Discover/FetchDiscoverMore/FetchContent/ValidateValues
  - Single implementation: DeclarativeSource loads YAML from `resources/*.yaml`
  
- **Capability Registries** — pluggable components for extensibility
  - Decoders: atom-xml (v1), extensible to json, etc.
  - Transforms: normalize, trim, lowercase (for string values)
  - Derivers: arxiv-id (extract from metadata), arxiv-pdf-url (select from links)
  - Sanitizers: arxiv-terms (whitelist operators, cap length)
  - Converters: html-to-markdown (for content)
  
- **YAML Declarations** (`resources/*.yaml`)
  - `request.fields`: form inputs (select with catalog validation, text with sanitizer)
  - `fetch`: URL template, query builder, pagination, retry policy, timeout/size limits
  - `response`: format, items path, field mappings (path, @attr, multi, transforms, derive)
  - `content`: second fetch for item body, converter, error handling (repick on 404)
  - Two interpolation styles: `${VAR}` (trusted config, resolved at load), `{{name}}` (untrusted runtime, validated)
  
- **API Changes**
  - `GET /resources` → returns array of resource descriptors (id, label, description, fields schema)
  - `POST /discover` → accepts `{resourceId, values}` body (empty body → defaults)
  - Legacy backward compat: `{category, terms}` folded into `values` via `defaultResourceID` (Phase 9 artifact)
  
- **Frontend**
  - ResourcePicker component (replaces hardcoded CategoryPicker)
  - DynamicRequestForm (schema-driven form builder, replaces hardcoded category+terms fields)
  - Zero form changes when adding a resource (pure YAML)

- **Deleted**
  - `/backend/internal/tools/discovery.go` — replaced by resource engine
  - `/backend/internal/tools/papercontent.go` — replaced by content fetch + converter
  - `/backend/internal/tools/papercontent-cleanup.go` — redundant cleanup
  - `/backend/internal/arxivquery/` package — hardcoded arXiv query logic (now in `resources/arxiv.yaml` + `sanitizers.go`)

- **New Code Locations**
  - `/backend/internal/resource/` — the engine and all registries
  - `/resources/arxiv.yaml` — arXiv resource declaration (golden regression: reproduces Phase 1–8 behaviour field-for-field)
  - `/resources/catalogs/arxiv-cs.yaml` — cs.* category whitelist and labels
  - `/docs/adding-a-resource.md` — operator guide (two cases: existing capabilities vs. new seam)

**Key Architecture Points:**
- **Decoupling achieved:** Orchestrator depends on `resource.Registry` + `resource.Source`, not concrete arXiv tools
- **Security generalized:** whitelist validation (e.g., arxiv-cs catalog), text sanitization (arxiv-terms), path validation (same-host content fetch)
- **Zero Go code per resource:** New sources are pure YAML + catalog (if needed)
- **Golden regression:** Engine reproduces old DiscoveryTool/PaperContentTool output exactly (validated via tests)
- **v1 SSRF minimal:** safe for arXiv (fixed public host); REQUIRED egress denylist for configurable hosts (documented in `adding-a-resource.md`)

**Test Coverage:**
- Golden tests: discovery/content output identical to Phase 8 behavior
- Loader: YAML parse, interpolation, validation, cyclic reference detection
- Engine: transport retries, decoder logic, normalization, ACL (whitelist, sanitizer)
- Cascade: full end-to-end resource execution (discover → normalize → validate)
- RegisterSuite: all capability registries (decoders, transforms, derivers, sanitizers, converters)

**Migration from Phase 8:**
1. `POST /discover` body now `{resourceId?, values}` (resourceId defaults to config default, values is map[string]string)
2. Old `{category, terms}` body still works (internally mapped to arXiv resource's values schema)
3. Frontend uses new ResourcePicker + DynamicRequestForm (schema-driven, zero hardcoding)
4. User can configure `agent.default_resource_id: arxiv` in config.yaml to preserve old discovery defaults

---

## Phase 10 — Channel Publishing (dev.to + X)
**Status:** ✅ Complete (2026-07-19)

**Deliverables:**
- **Channel Abstraction & Registry**: Self-registration pattern (mirrors LLM provider); no import cycles; config-driven initialization
  - Channel interface: `ID()`, `Category()`, `Validate()`, `Publish()`
  - Category taxonomy: `longform` (deep article), `digest` (condensed, reserved for RSS), `brief` (punchy hook)
  - v1 channels: **dev.to** (`longform`, API key), **X** (`brief`, OAuth2 + thread chunking)

- **Repurposer Agent**: Single-shot content generation (no LLM reviewer loop)
  - Category-blind: knows only `Category`, never a channel
  - Reuses explainer's Analogies & Intuition + Glossary sections for accessibility
  - Category-specific prompt templates; configurable per-category target word counts
  - One LLM call per unique category; channels sharing category get one repurposed draft each

- **Publications Store & Schema**: Durable, idempotent publication record
  - `publications` table: (run_id, channel_id) UNIQUE constraint prevents duplicates
  - Status lifecycle: draft → approved → publishing (transient claim guard) → published/failed
  - ClaimForPublish atomic transition guards concurrent double-publish

- **Orchestrator Integration**: Five new endpoints
  - `GET /channels`: List enabled channels + categories
  - `POST /runs/:id/publications`: Generate repurposer drafts per category
  - `GET /runs/:id/publications`: List all drafts for a run
  - `PATCH /publications/:pid`: Human edit endpoint (title, body, status)
  - `POST /publications/:pid/publish`: Atomic claim + Channel.Publish() + store result

- **dev.to Channel**: Publishes longform articles
  - API key auth from `DEVTO_API_KEY` env var
  - Creates article via dev.to API; returns live post URL + ID
  - Validation: content length checks per dev.to limits

- **X (Twitter) Channel**: Publishes brief content as numbered tweets
  - OAuth2 user-context flow: 3-legged auth, token refresh (env: `X_CLIENT_ID/SECRET/REFRESH_TOKEN`)
  - Mechanical chunking: splits brief into ≤280-char tweets with `(i/N)` counters
  - Reply threading: subsequent tweets reply to previous (X API mechanics, not agent)
  - Returns thread URL as external URL; thread root post ID as external_id

- **Configuration**: `publishing:` block in config.yaml
  - `channels`: List of enabled channel ids
  - `categories`: Per-category soft target word counts
  - Channel secrets in `.env` only (API keys, OAuth tokens, never logged)

- **UI Components**: Publish workflow in frontend
  - Publish panel: select channels, preview per-channel (thread/article view)
  - Draft editor: edit title + body before approval
  - Live links to published posts

- **Documentation**: 
  - Design note: `docs/design-notes/2026-07-14-channel-publishing.md`
  - X OAuth setup: `docs/channel-x-oauth-setup.md`
  - Architecture sections: Channels, Repurposer, Publications, Data Flow 4

**Key Files:**
- Backend:
  - `internal/channels/{channel.go,category.go,registry.go}`
  - `internal/channels/devto/{devto.go,*_test.go}`
  - `internal/channels/x/{x.go,oauth.go,chunk.go,*_test.go}`
  - `internal/agents/repurposer/{repurposer.go,repurposer-prompt.go,*_test.go}`
  - `internal/store/{publications.go,model.go,*_test.go}`
  - `backend/migrations/0002_publications.sql` (user-run migration)
  - `internal/orchestrator/{publish-handlers.go,dto.go}`
  - `cmd/server/main.go` (blank imports: `_ "internal/channels/devto"`, `_ "internal/channels/x"`)

- Frontend:
  - `app/runs/[id]/page.tsx` (publish UI integration)
  - `components/publish-*.tsx` (UI components)
  - `lib/use-publications.ts` (API hooks)
  - `app/api/publish/*` (proxy routes)

**Architecture:**
- **Decoupling:** Repurposer (category-blind) ↔ Channel (mechanics-blind) seam via GeneratedContent DTO
- **Idempotency:** UNIQUE (run_id, channel_id) + ClaimForPublish atomic transition
- **DB-Required:** Publishing endpoints return 503 when Postgres unavailable; pipeline unaffected
- **Single-shot:** Repurposer generates once per category; human review is the "reviewer" step, not another LLM pass

**Success Metrics:**
- Select a run → repurpose per category (one LLM call per unique category) → draft for each channel
- Edit + approve draft in UI (no re-generation)
- Publish → both dev.to and X return live external URLs stored in publications row
- Re-publishing same (run, channel) pair blocked (409)
- Adding a third channel: edit one channel package + add registry entry + re-import in main.go

**Known Limitations:**
- daily.dev has no push API → RSS channel is future work (modeled as `digest` category)
- X auth is the cost (OAuth2 setup + token refresh); dev.to is trivial API key
- X free tier is write-capped (~300 posts/15min); heavy publishing requires paid tier

---

## Key Milestones

| Milestone | Date | Achieved |
|---|---|---|
| Phase 1 scaffolding complete | 2026-06-14 | ✅ Yes |
| Phase 2 discovery working | 2026-06-21 | ✅ Yes |
| Phase 3 HTML extraction & LLM | 2026-06-28 | ✅ Yes |
| Phase 4 explainer & vault write | 2026-07-05 | ✅ Yes |
| User can trigger → select → receive note | 2026-07-05 | ✅ Yes |
| Phase 5 reviewer & revision loop | 2026-07-05 | ✅ Yes |
| Full pipeline with quality review | 2026-07-05 | ✅ Yes |
| Phase 6 polish & hardening | 2026-07-05 | ✅ Yes |
| Phase 7 run timeline tracing (live & persistent) | 2026-07-12 | ✅ Yes |
| User can browse and reopen past runs | 2026-07-12 | ✅ Yes |
| Phase 8 full reasoning trace + content replay + pagination | 2026-07-13 | ✅ Yes |
| History detail shows note markdown + live timeline | 2026-07-13 | ✅ Yes |
| Discovery supports "Load more" pagination | 2026-07-13 | ✅ Yes |
| Phase 9 declarative resource engine shipped | 2026-07-18 | ✅ Yes |
| ArXiv tools deleted; resource YAML replaces hardcoding | 2026-07-18 | ✅ Yes |
| Frontend resource picker + dynamic form (zero hardcoding) | 2026-07-18 | ✅ Yes |
| New sources require YAML only (no Go changes) | 2026-07-18 | ✅ Yes |
| Phase 10 channel publishing (dev.to + X) | 2026-07-19 | ✅ Yes |
| Repurposer agent + Channel registry working | 2026-07-19 | ✅ Yes |
| Select run → publish to dev.to and X with human review | 2026-07-19 | ✅ Yes |
| Re-publishing blocked (idempotent) | 2026-07-19 | ✅ Yes |

---

## Architectural Decisions

### Text-Only LLM Processing (Phase 4)
**Decision:** Process paper Markdown text, not PDF page images.
**Rationale:** 
- Pure text extraction (Phase 3) avoids CGO/poppler complexity
- Any text-capable LLM works; lower token cost
- Markdown preserves structure + figure captions
- Compatible with Phase 5 reviewer (text-only review rubric)

**Tradeoff:** Diagrams/tables reach the model only via captions. Acceptable in Phase 4; optional image channel could be added later.

### Atomic Vault Write with Temp File Pattern
**Decision:** Write to `.tmp`, then `os.Rename()` to final path.
**Rationale:**
- Atomic on the same filesystem (sub-millisecond)
- Crash during write leaves vault clean (no partial `.md` files)
- Log update failure post-write is warning-not-fatal (note already saved)

### Session Accessors Over Direct Field Access
**Decision:** All session mutations via mutex-guarded accessor methods.
**Rationale:**
- Safe concurrent access (background pipeline goroutine + HTTP polling)
- Server-only fields (Explainer, VaultFile, TokensUsed) excluded from Snapshot()
- Large Content never serialized to status poll

---

## Known Limitations & Future Work

### Phase 9 Status: Channel Publishing Shipped
Publishing to dev.to and X is now complete with human-in-the-loop review. daily.dev support deferred (no push API → future RSS channel).

### Resolved Limitations
- ~~**Obsidian only:** Phase 9 adds social channel publishing (dev.to, X)~~
- ~~**Single-pass generation:** Phase 5 added critic-generator revision loop~~
- ~~**No publishing:** Phase 9 adds Channel abstraction + Repurposer agent~~

### Remaining Limitations
- **Text-only:** Diagrams/tables described by captions only; optional image channel reserved for future
- **No auto-linking:** Follow-up papers listed; user opens arXiv manually
- **Single paper per run:** No batch processing per session
- **daily.dev no push API:** RSS channel (digest category) is future work; daily.dev ingests feeds, not POST

### Recommended Additions (Post-Phase 9)
- **daily.dev RSS Channel** — Publish digest content as RSS feed
- **Multi-category arXiv** — Support beyond `cs.AI` (e.g., cs.LG, cs.NE)
- **Relevance ranking** — Keyword filtering / semantic relevance (not just recency)
- **Batch processing** — Multiple papers per run
- **Obsidian plugin** — Direct vault integration + Obsidian-side UI
- **Cloud hosting** — Remote access with privacy guardrails (local-only LLM inference)
- **Full-text search** — Across all generated notes
- **Auto-linking** — Hyperlink follow-up papers in explainers

---

## Dependencies & Environment

### Go Backend
- Go 1.26.4
- `google.golang.org/adk`: ADK agent framework
- `github.com/go-resty/resty`: HTTP client
- `gopkg.in/yaml.v3`: YAML config parsing
- `github.com/joho/godotenv`: `.env` loader
- `github.com/go-text/typesetting`: Text layout (if Markdown rendering needed)

### Next.js Frontend
- Node.js 20+
- Next.js 16.2.7 LTS
- React 19
- TypeScript 5
- Tailwind CSS 4.3.0
- TanStack Query 5.101.0
- `react-markdown`: Markdown rendering (Phase 4)
- `remark-gfm`: GitHub Flavored Markdown (Phase 4)

### External Services
- **arXiv API**: Paper discovery and HTML rendering
- **LLM Provider**: Anthropic Claude, OpenAI, or Google Gemini (configurable)
- **Local Obsidian Vault**: Markdown output

---

## How to Contribute

1. **Read the plan for the phase you're working on** (in `docs/phase*/` subdirectory)
2. **Check CLAUDE.md for project principles** (simplicity, no laziness, minimal impact)
3. **Follow code standards** from `docs/code-standards.md`
4. **Write tests** for new logic; verify existing tests pass
5. **Update docs** if behavior changes (this roadmap, phase PRD, architecture)
6. **Create conventional commits** with clear messages
7. **Open PR for review** before merging to main

---

## Questions?

See `docs/prd.md` (product overview), `docs/architecture.md` (system design), or phase-specific PRDs in `docs/phase*/prd.md`.
