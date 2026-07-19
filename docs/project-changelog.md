# Project Changelog
## ArXiv AI Paper Explainer Agent

All notable changes to this project are documented below, organized by release and phase.

---

## [Phase 9] — 2026-07-18

Declarative Resource Engine: Pluggable discovery abstraction. The entire paper discovery pipeline is no longer hardcoded to arXiv but driven by YAML declarations in `resources/*.yaml`. The core (orchestrator + agent pipeline) depends only on the `Source` interface + the resource registry, never on concrete implementations. Discovery is now resource-agnostic and extensible: adding a second source is pure YAML + optional catalog (no Go code changes required).

### Added

#### Backend
- **`internal/resource/` package** — the declarative resource engine with four-stage pipeline (transport → decoder → normalize → ACL)
  - `Source` interface: `Discover()`, `FetchDiscoverMore()`, `FetchContent()`, `ValidateValues()`, `Descriptor()`
  - `DeclarativeSource` — single implementation that loads YAML declarations
  - `Registry` — holds all registered resources; orchestrator depends on this + Source, not on concrete arXiv tools
  
- **Capability Registries** — pluggable seams for extensibility
  - `RegisterDecoder(format, Decoder)` — response format handlers (atom-xml v1, json/etc. future)
  - `RegisterTransform(name, transformer)` — string transforms (normalize, trim, lowercase)
  - `RegisterDeriver(name, deriver)` — node-aware field derivers (arxiv-id, arxiv-pdf-url)
  - `RegisterSanitizer(name, sanitizer)` — free-text validators (arxiv-terms)
  - `RegisterConverter(name, converter)` — content format converters (html-to-markdown)
  
- **YAML Declarations** (`resources/arxiv.yaml`, `resources/catalogs/arxiv-cs.yaml`)
  - `request.fields` — form inputs (select with catalog validation, text with sanitizer)
  - `fetch` — URL template, query builder, pagination, retry policy, timeout/size limits
  - `response` — format, items path, field mappings (path, @attr, multi, transforms, derive)
  - `content` — content fetch, converter, error handling (repick on 404)
  - Two interpolations: `${VAR}` (trusted config, YAML-escaped, resolved at load), `{{name}}` (untrusted runtime, validated+sanitized+URL-encoded)
  
- **`GET /resources`** — returns resource descriptors (id, label, description, fields schema) so UI renders resource picker + dynamic form automatically
- **`POST /discover` schema change** — body now `{resourceId?, values: {category, terms, ...}}` (empty body → defaults)
  - Backward compat shim: legacy `{category, terms}` folded into values via default resource ID

#### Frontend
- **ResourcePicker component** — dropdown of available resources (sourced from GET /resources)
- **DynamicRequestForm** — schema-driven form builder (zero hardcoding; generates UI from resource Descriptor.Fields)
- Both wired into DiscoveryPanel; no more hardcoded CategoryPicker or field definitions

#### Deleted Code
- **`/backend/internal/tools/discovery.go`** — DiscoveryTool (replaced by resource engine)
- **`/backend/internal/tools/papercontent.go`** — PaperContentTool (replaced by resource content fetch + converter)
- **`/backend/internal/tools/papercontent-cleanup.go`** — redundant cleanup
- **`/backend/internal/arxivquery/` package** — hardcoded arXiv query logic (now declarative in `resources/arxiv.yaml` + sanitizers)

#### Documentation
- **`docs/adding-a-resource.md`** — operator guide for adding new resources (Case A: existing capabilities → YAML only; Case B: new capability → register seam + YAML)
- **`docs/design-notes/2026-07-18-declarative-resource-engine.md`** — design rationale and tradeoffs

### Changed

#### Backend
- **Orchestrator.newSession()** — now takes `(resourceID string, values map[string]string)` instead of implicit arXiv
- **Orchestrator.HandleDiscover()** — parses `{resourceId?, values}` body; validates against resource's schema
- **PipelineSession** — new `source` field (resource identifier); removed hardcoded category state
- **`models.Paper`** — new non-persisted `Source` field (indicates which resource provided it)

#### Frontend
- **`/api/trigger`** — now accepts `{resourceId, values}` body (empty body preserved for backward compat)
- **Discovery UI** — resource picker drives which form appears (dynamic per resource)

### Fixed

- **Removed hardcoded arXiv dependency** — core orchestrator + agent pipeline now resource-agnostic
- **Security generalized** — whitelist validation (catalog), text sanitization, path traversal prevention all parameterized by resource declaration
- **Content converter extensible** — new formats (JSON, PDF, etc.) register a converter; no core changes

### Architecture

**Golden Regression:** Engine reproduces Phase 1–8 behavior field-for-field (validated via tests)
- `resources/arxiv.yaml` + `resources/catalogs/arxiv-cs.yaml` replicate old hardcoded arXiv discovery
- Content fetch + html-to-markdown converter replicate old PaperContentTool behavior
- Pagination cursor, retry policy, timeout/size limits all declared in YAML

**Security:** v1 SSRF is arXiv-safe (fixed public host, same-host/scheme validation, redirect cap 3). REQUIRED before enabling configurable-host resources: egress denylist (RFC1918, loopback, link-local, cloud-metadata) in transport layer — seam left open in `docs/adding-a-resource.md`.

### Test Coverage

- Engine integration: discovery → normalization → validation (end-to-end)
- Loader: YAML parse, interpolation (`${...}` and `{{...}}`), validation, registry binding
- Capability registries: decoders, transforms, derivers, sanitizers, converters
- Transport: retry logic, redirect handling, timeout/size limits
- Backward compat: legacy `{category, terms}` body → arXiv resource values

### Migration from Phase 8

1. **API:** `POST /discover` body now `{resourceId?, values}` (defaults to config default, values is map)
2. **Legacy compat:** Old `{category, terms}` body still works (mapped to arXiv resource via shim)
3. **Frontend:** Use new ResourcePicker + DynamicRequestForm (schema-driven, zero hardcoding)
4. **Config:** `agent.default_resource_id: "arxiv"` in config.yaml (optional; defaults to arXiv for backward compat)
5. **No database changes:** `models.Paper.Source` is non-persisted; no migration needed

---

## [Phase 8] — 2026-07-13

Full agent reasoning trace + history content replay + arXiv pagination. Three independent improvements shipped together: explainer/reviewer PayloadFull now populated with full prompts and responses for observability; history detail view includes the persisted markdown note with live reasoning timeline; arXiv discovery supports offset-based pagination within a session.

### Added

#### Backend
- **PayloadFull Enrichment** — explainer & reviewer decision events now include full content
  - Event.PayloadFull populated for llm.explainer.completed, llm.reviewer.completed, decision.* events
  - Explainer: {systemPrompt, userPrompt, response}
  - ReviewerAgent: same fields + decision events with {decision, onPass, flaggedSections, narrative}
  - Config tracing.full_payloads enables opt-in capture; default false (Phase 7 unchanged)
  - Secret scrubber preserves distinct payloadCap (100000 chars) and previewCap (500 chars) to avoid truncation

- **History Content Re-Show** — GET /runs/{id}/content endpoint
  - Reads persisted Obsidian .md note from disk (path from tool.vaultwriter.completed event)
  - Path traversal guarded by tools.ValidateWithinVault (now exported)
  - Returns {available: true, content: markdown} on success
  - Returns {available: false} HTTP 200 if file missing (graceful degradation)
  - Enables replay of note + timeline in history detail view

- **arXiv Pagination** — offset-based pagination within a session
  - DiscoveryTool.FetchPapersFrom(start int) offset parameter
  - Session model gains ConsumeNextStart (cursor) and AppendCandidates() method
  - POST /discover/{sessionId}/more extends same session's candidates
  - Guarded to StageSelection (returns 409 if not in selection stage)
  - Frontend accumulates candidates across calls, dedup by paper ID

#### Frontend
- **History Content Panel** — renders persisted note markdown + live timeline
  - New GET /api/runs/{id}/content proxy route
  - RunDetailView shows {available} check, renders if available
  - Markdown + timeline side-by-side in history detail

- **Load More UI** — pagination button in discovery candidates
  - POST /api/discover/{sessionId}/more triggers backend
  - Appends new candidates (dedups by ID client-side)
  - Preserves user's existing candidate list

#### Config
- tracing.full_payloads: boolean (default false) — opt-in full prompt/response capture
- Secret scrubber now uses distinct payloadCap (100000) vs previewCap (500)

### Changed

- **Event.PayloadFull** — now populated for explainer/reviewer calls when tracing.full_payloads=true
- **Session.Candidates** — can be appended via AppendCandidates() during StageSelection
- **DiscoveryTool.FetchPapersFrom** — new offset parameter (Phase 2 was implicit start=0)

### Architecture Points

- **No schema changes:** Reuses existing run_events.payload_full JSONB column (Phase 7)
- **Pagination cursor:** Session.ConsumeNextStart tracks arXiv API offset for next "load more"
- **Vault path lookup:** tool.vaultwriter.completed event's Summary.path field locates .md file
- **Graceful degradation:** Missing vault file returns {available:false} (history page can still show timeline)

### Migration Guide

1. **Enable full payloads (optional):** `tracing.full_payloads: true` in config.yaml
2. **No database changes:** Existing payload_full column holds new data
3. **Frontend:** History detail automatically renders content if GET /runs/{id}/content succeeds
4. **Pagination:** Opt-in via UI "Load more" button during StageSelection

### Backward Compatibility

- tracing.full_payloads defaults false; Phase 7 behavior unchanged without config edit
- GET /runs/{id}/content safe if vault file missing (returns {available:false})
- POST /discover/{sessionId}/more guarded to StageSelection; returns 409 if misused
- Session.ConsumeNextStart new field but optional (populated by pagination flow)

---

## [Phase 7] — 2026-07-12

Run Timeline Tracing: Live and persistent event timeline per run. Users can monitor
pipeline progress in real-time via Server-Sent Events, review complete histories
afterward via REST, and resume queries across backend restarts (with Postgres).
All tracing is additive and optional; pipelines work without the database.

### Added

#### Backend
- **RunStore & EventStore** (`internal/store/`) — pgx/v5 Postgres access layer
  - `Open(dsn)` with graceful degrade (returns ErrStoreUnavailable if DB unreachable)
  - `CreateRun/UpdateRunPaper/FinalizeRun/GetRun/ListRuns` for run records
  - `AppendEvent/ListEvents` for the ordered event timeline
  - Model structs (RunRecord, EventRecord) mirror the schema exactly

- **RunRecorder** (`internal/tracing/recorder.go`) — per-run event capture
  - Monotonic sequence counter (0,1,2…) per run
  - Bounded in-memory ring buffer (configurable size, default 256 events)
  - Async single-writer persistence (batch flushes to Postgres)
  - Degrade-safe: operates in-memory if store unavailable

- **SSE Broker** (`internal/tracing/broker.go`) — non-blocking per-run fan-out
  - Per-run subscriber map for live client connections
  - Last-Event-ID support for client reconnect/resume from sequence N
  - Closes on `run.completed` or `run.failed` event

- **Event Taxonomy** (`internal/tracing/event.go`)
  - Standardized EventKind constants (discovery.started, selection.chosen, tool.*, llm.*, decision.*, run.*)
  - Event struct with seq, event_type, stage, title, status, summary (JSONB), payload_full (optional)

- **Secret Scrubber** (`internal/tracing/scrub.go`) — redacts sensitive data
  - Redacts API keys and key-shaped patterns from summaries
  - Caps text previews to prevent huge payloads
  - Never stores raw HTML or full markdown (size + preview only)

- **Orchestrator Instrumentation** (`internal/orchestrator/tracing.go`)
  - Recorder created at orchestrator startup with degrade-safe store.Open
  - Emission on every decision point: discovery.started through run.completed/failed
  - Recoverable failures keep recorder open; only completion/non-recoverable close it

- **Transport Endpoints** (`internal/server/`)
  - `GET /runs/{id}/events` — SSE stream with Last-Event-ID resume
  - `GET /runs` — paginated list of runs (newest first)
  - `GET /runs/{id}` — single run's header + full timeline
  - Handles client disconnect cleanly; orphaned runs receive synthetic terminal event

#### Frontend
- **RunTimeline component** (`components/run-timeline.tsx`) — renders ordered event list
  - Per-event row with status icon (info/success/warning/error) and relative timestamp
  - Expandable rows for LLM/tool events (summary preview; full payload if trace-on)
  - Live update via SSE; falls back to REST on completion

- **RunEventRow component** (`components/run-event-row.tsx`) — individual event formatting

- **RunsHistory component** (`components/runs-history.tsx`) — list of past runs
  - Title, date, outcome badge, cost; click to reopen individual run

- **useEventSource hook** (`lib/use-event-source.ts`) — SSE client with Last-Event-ID
  - Auto-reconnect on disconnect; resumes from last known sequence

- **useRuns hook** (`lib/use-runs.ts`) — TanStack Query for history list

- **Pages** (`app/runs/page.tsx`, `app/runs/[id]/page.tsx`)
  - `/runs` — history list
  - `/runs/[id]` — timeline + result panel for individual run

- **Navigation** — link to runs history on home page, live timeline mounted in discovery-panel

- **CORS** — allows both `http://localhost:3000` and `http://127.0.0.1:3000`

#### Infra
- **docker-compose.yml** — postgres:17-alpine service with healthcheck
  - Named volume pgdata for persistence
  - Credentials default to arxiv/arxiv (override via .env)
  - Port bound to 127.0.0.1:5432 only (local tool, never exposed)

- **Migration** (`backend/migrations/0001_run_timeline.sql`)
  - USER-RUN migration (agent never generates/executes; user applies manually)
  - `runs` table: id (PK), paper_id, title, stage, status, token counts, cost, review_passed, timestamps
  - `run_events` table: (run_id, seq) composite PK, event_type, stage, title, status, summary (JSONB), payload_full (optional), duration_ms
  - Index on runs.started_at DESC for efficient history queries
  - Safe to re-run: all objects use IF NOT EXISTS

- **Config** (`config.yaml` + `.env` example)
  - `tracing: { enabled, full_payloads, buffer_size }`
  - `DATABASE_URL` from `.env` (documented default in `.env.example`)

#### Docs
- `docs/superpowers/specs/2026-07-12-run-timeline-tracing-design.md` — approved brainstorm spec

### Changed

- **Orchestrator.New()** — creates Recorder with degrade-safe store.Open
- **Pipeline flow** — emits events at discovery, selection, extraction, generation, review, vault write, completion
- **Frontend discovery-panel** — now includes live timeline during run

### Known Limitations

- **Retention/pruning:** No automatic cleanup of old runs (can be added later if table grows)
- **Auth:** No access control on `/runs` endpoints (local single-user tool, bound to 127.0.0.1)
- **Multi-user:** No tenant separation (out of scope)

### Dependencies Added

**Backend:**
- `github.com/jackc/pgx/v5` — PostgreSQL driver

**Frontend:**
- No new dependencies (SSE is browser-native)

### Test Coverage

- Store: runs/events CRUD against test Postgres (or in-memory fake)
- Recorder: ordering, seq monotonicity, buffer wraparound
- Broker: per-run fan-out, client disconnect handling
- Orchestrator: full event sequence for pass, revise, 404-repick, failure scenarios
- SSE: replay-from-Last-Event-ID, orphaned run terminal event

### Migration Guide

1. **Optional:** Bring up Postgres: `docker compose up -d db`
2. **Optional:** Apply schema: `psql "$DATABASE_URL" -f backend/migrations/0001_run_timeline.sql`
3. **No code changes required** — recorder degrades gracefully if DB unavailable
4. **Frontend:** New `/runs` history page and timeline UI automatically enabled

---

## [Phase 6] — 2026-07-05

Polish & hardening of the HTML→Markdown product. All changes are additive and
extend the existing sentinel-error + mutex-encapsulated-session patterns; the
original PDF/vision-oriented PRD was realigned to the actual HTML architecture
(poppler/DPI scope dropped).

### Added

#### Backend
- **Retry from failed stage (F2)** — `POST /retry/{sessionId}` resumes a failed,
  recoverable pipeline from the segment that failed via cached outputs
  (markdown/explainer). Paper selection is preserved; a transient vault failure
  re-writes with **zero** additional LLM cost. An atomic `BeginRetry()` transition
  rejects a concurrent second retry (no double-spawn). `runPipeline` refactored
  into cache-guarded segments (extract / generate+review / write).
- **Error action hints** — `describe*` mappings return a machine-readable action
  (`retry` / `fix_config` / `fix_permissions`), surfaced via `StatusResponse.errorAction`.
  `ErrLLMBadRequest` is now non-recoverable (config is immutable at runtime).
- **Cost estimation (F3)** — `llm/pricing.go` (`EstimateCost`) + `/result` cost
  fields (`inputTokens`, `outputTokens`, `estimatedCostUSD`, `costKnown`). Unknown
  models report `costKnown:false` so the UI hides the figure.
- **Context-window pre-check (F4)** — `llm/limits.go` (`EstimateTokens`, len/4
  heuristic) + non-blocking `ContextWarning`; the pipeline always proceeds.
- **arXiv retry counter (F5)** — `FetchPapers` gains an `onRetry` callback wired to
  `arxivRetryCount`, driving a "Connecting to arXiv (retry n/3)…" label.
- **Split token accounting** — session `AddIO`; `ReviewVerdict` carries
  `InputTokens`/`OutputTokens`.
- **Logging & security (F6/F1)** — split tokens on LLM-complete; cost + review
  outcome on `pipeline complete`; uniform stage-failure log (stage/action/
  recoverable/cause); a source-scanning test (`internal/audit`) asserts no API key
  appears in any `slog` call.

#### Frontend
- `/api/retry` proxy route; retry wiring that resumes in place (preserves the
  paper pick, re-arms the same poll loop); non-blocking context-warning banner;
  arXiv retry progress label; estimated-cost line in the success panel.

#### Docs
- README: LLM provider table, estimated-cost table, full `config.yaml` reference,
  troubleshooting table, project map (poppler explicitly absent).
- `docs/phase6/e2e-validation.md`: runnable cross-provider validation checklist.

### Changed
- `PaperFetcher.FetchPapers` signature gains an `onRetry func(int)` parameter
  (all call sites + fakes updated).

---

## [Phase 5] — 2026-07-05

### Added

#### Core Features
- **ReviewerAgent**: Independent critic evaluating explainer quality
  - 6-criteria rubric: author intent clarity, analogies, math handling, figures, glossary, tone
  - Text-only review (source paper not sent — cost optimization)
  - Low temperature evaluation (0.1) for consistent, repeatable judgements
  - Returns ReviewVerdict with Pass (single source of truth), Score (advisory), Feedback (per-section)
  - JSON parsing with markdown fence stripping

- **Bounded Critic-Generator Loop**: Phase 5 orchestration in runPipeline
  - Generate → Review → (Revise if fail & iterations remain) → Repeat until Pass or max reached
  - Always terminates and writes exactly one note (the last explainer)
  - Respects max_review_iterations config (0 disables reviewer entirely)
  - Revision note formatting: structures feedback from failed reviews for next generation
  - Error handling: parse errors stop loop gracefully; LLM errors fail session recoverably

- **ReviewVerdict Data Model**: Structured review result
  - Pass: boolean verdict (gates the loop, never Score)
  - Score: 0.0–1.0 quality rating (advisory for observability)
  - Feedback: map[section_slug]revision_note for failed reviews
  - Iteration & TokensUsed: cost tracking per review call

- **Frontmatter Enhancement**: Vault notes reflect real review state
  - review_iterations: actual count from ReviewVerdict
  - review_passed: boolean approval status
  - review_score: included only when reviewer ran (omitted if max_review_iterations=0)

- **Session Enhancements**: Phase 5 pipeline state
  - New Iteration field: tracks current reviewer/revision loop round
  - SetIteration/SetVerdict accessor methods
  - Updated Explainer after each revision

- **Configuration**: New agent.max_review_iterations setting
  - Default: 2 (bounded loop, ~200k tokens/paper)
  - 0 = disable reviewer (Phase 4 behavior at zero cost)
  - >= 0 validation: negative values rejected at config load

- **Progress UI**: Frontend surfaces review loop states
  - New stages: StageReviewing ("reviewing") and StageRevising ("revising")
  - Iteration counter visible during status polling
  - Progress message shows "Reviewing (pass N)…" and "Revising (pass N)…"
  - Session accessors: Iteration, ReviewScore, ReviewPassed (Snapshot dto)

#### Documentation
- Updated `docs/architecture.md` §2.7 ReviewerAgent with detailed design decisions
- Updated `docs/architecture.md` Flow 2 with complete bounded loop diagram
- Updated `docs/architecture.md` frontmatter examples showing review_* fields
- Updated `docs/development-roadmap.md` Phase 5 to Complete with full deliverables
- Updated `docs/project-changelog.md` (this file) with Phase 5 entry
- Updated `README.md` with section on reviewer loop behavior, cost, and config knob

### Changed

- **Orchestrator.runPipeline**: Complete Phase 5 critic-generator loop
  - Flow: Generate (iteration 1) → Review → Check verdict → Revise (feedback) → Loop → Vault write
  - Stage transitions: generating → reviewing → revising (loop) → writing → complete
  - Token accumulation: both explainer and reviewer tokens tracked
  - Verdict handling: pass=true breaks loop; parse errors save with pass=false

- **VaultWriterTool.WriteToVault**: Now accepts verdict parameter
  - Signature: WriteToVault(ctx, explainer, paper, verdict *ReviewVerdict) (string, error)
  - Frontmatter rendering respects nil verdict: review_iterations=0, review_passed=true, no score

- **ExplainerOutput.Iteration**: Now accurate loop iteration
  - Phase 4: hardcoded to 1
  - Phase 5: stamped with real loop iteration (1, 2, 3, ...)

- **PipelineSession**: New fields and accessors for Phase 5
  - Added Iteration field (tracking reviewer/revision loop round)
  - Added Verdict field (*ReviewVerdict, nil if not yet reviewed or reviewer disabled)
  - Renamed LastVerdict → Verdict (clearer semantics)
  - New accessor: SetVerdict, Verdict, Iteration

### Fixed

- Explainer iteration tracking: now reflects actual loop round (Phase 4 hardcoded 1)
- Frontmatter consistency: review_* fields always present, never nil
- Config validation: max_review_iterations >= 0 enforced at load time

### Architecture

**Design Decisions (Phase 5):**
- **Decision 1 (Policy):** Pass gates the loop; Score never blocks (advisory only)
- **Decision 2 (Fault Handling):** Malformed JSON stops loop and saves with pass=false (no blind regen)
- **Design T3 (Cost):** Reviewer never receives source paper (text-only review at lower cost)
- **Shared LLM:** Same client as explainer with different system prompt + temperature (0.1)

**Error Handling:**
- JSON parse error (ErrReviewParse sentinel) → loop halts, saves with pass=false
- LLM/network error → session fails recoverably
- Empty generator response → session fails recoverably
- Reviewer LLM/network error → session fails recoverably (no write, paper re-surfacable)

**Loop Termination Guarantees:**
- Always terminates via one of: verdict.Pass, max iterations, or parse error
- Always writes exactly one note (the final explainer)
- Respects maxReviewIterations=0 (reviewer disabled)

### Tradeoffs & Acceptance

| Tradeoff | Rationale | Residual Risk |
|---|---|---|
| Same LLM for reviewer & explainer | Simpler config, single model. Distinct prompts + low temp provide different behavior. | Reviewer may inherit explainer's biases; optional separate reviewer LLM could improve independence. |
| Text-only reviewer | Reduces token cost by ~50% vs. sending source paper. Reviewer evaluates clarity of explainer alone. | Reviewer cannot catch errors in figure descriptions if source not provided; acceptable by design. |
| Configurable max=2 default | Balances cost (~200k tok) vs. quality iterations. Cost-conscious deployments can set to 0. | Higher iterations increase cost; users must balance quality vs. budget. |

### Dependencies Added

**Backend:**
- No new Go dependencies (ReviewerAgent reuses existing llm.LLMClient)

**Frontend:**
- No new dependencies (existing Status DTO and polling already support iteration fields)

### Test Coverage

- ReviewerAgent: JSON parsing with/without markdown fences
- ReviewerAgent: per-section feedback filtering (nulls and empty strings dropped)
- Orchestrator loop: termination on Pass, max iterations, parse error
- VaultWriterTool: frontmatter with nil verdict (review_iterations=0, no score)
- Session: Iteration and Verdict accessor thread safety

### Known Issues & Limitations

- **Score not persisted to vault:** Score in frontmatter only (advisory); not queryable later
- **Single loop per run:** Cannot restart a failed review loop; must re-trigger
- **No reviewer quality metrics:** No feedback on reviewer confidence/disagreement rates
- **Config immutable per run:** max_review_iterations cannot be changed mid-pipeline

### Breaking Changes

- VaultWriterTool.WriteToVault signature changed: now requires verdict parameter (was optional in Phase 4)
- PipelineSession: LastVerdict renamed to Verdict; old code will not compile
- Orchestrator.runPipeline: internal flow changed; any orchestrator subclasses must adapt

### Migration Guide (if upgrading from Phase 4)

1. Update VaultWriterTool calls to pass verdict (or nil if reviewer disabled): `WriteToVault(ex, paper, s.Verdict())`
2. Update config.yaml: add `agent.max_review_iterations: 2` (or 0 to disable reviewer)
3. Update session access: `s.LastVerdict` → `s.Verdict()`, new `s.Iteration()` field
4. Ensure LLMClient is configured; same client serves both explainer and reviewer
5. Frontend status polling unchanged; new `iteration` and `reviewPassed` fields in DTO are optional

### Commit References

- 📦 Phase 5 implementation: commit hash TBD (when merged)
- 📚 Docs reconciliation: commit hash TBD (when merged)

---

## [Phase 4] — 2026-07-05

### Added

#### Core Features
- **ExplainerAgent**: Text-only LLM-based paper explainer generating 9-section structured output
  - System prompt designed for re-teaching (not summarizing) papers to technical practitioners
  - Supports revision feedback seam for Phase 5 (RevisionNote parameter, always empty in Phase 4)
  - Automatic section parsing with graceful fallback for missing headings
  - Observability: logs generation time, token usage, and word count

- **VaultWriterTool**: Atomic Obsidian vault write with YAML frontmatter
  - Filename generation: `YYYY-MM-DD_arxivID_slug.md` with date parsed from Paper.Published string
  - Frontmatter includes arxiv_id, title, authors (YAML list), published (string), category (from config), generated_at, review_iterations, review_passed, tags
  - Atomic write pattern: `.tmp` file → `os.Rename()` (no partial files on disk)
  - Path traversal protection: validates all paths against vault base
  - Post-write logging: processed.json updated only after successful vault write

- **ExplainerOutput**: New data model with token tracking
  - Fields: PaperID, Content, Sections (map), Iteration, InputTokens, OutputTokens, CreatedAt
  - Supports Phase 5 revision loop seam via Iteration field

- **Session Enhancement**: Server-only accessors for pipeline state
  - New stages: `generating`, `writing`, `complete`
  - Accessors: SetExplainer/Explainer, SetVaultFile/VaultFile, AddTokens/TokensUsed
  - Token accumulation across phases for observability
  - Mutex-guarded concurrent access

- **GET /result/:sessionId Endpoint**: Returns completed explainer
  - Response: `{ content, vaultFile, tokensUsed }`
  - Returns 404 unless pipeline stage is `complete`
  - Content streamed directly (large Markdown not in status poll)

- **NextJS Result Preview**: Markdown rendering in UI
  - `react-markdown` + `remark-gfm` for GitHub Flavored Markdown
  - Renders sections, tables (glossary), lists, code blocks, bold text
  - Progress indicators: "Generating explainer...", "Saving to vault...", "Complete"
  - Success banner with vault file path and token usage count

#### Documentation
- Reconciled `docs/phase4/prd.md` to text-only implementation (removed all image-based references)
- Created `docs/development-roadmap.md` tracking Phase 1–6 status
- Created `docs/project-changelog.md` (this file)
- Updated `docs/architecture.md` sections 2.6 and 2.8 with correct ExplainerOutput fields and VaultWriterTool signature

### Changed

- **LogCheckTool.MarkAsProcessed**: Now takes `(paper models.Paper, vaultFile string)` and updates processed.json atomically
  - Failure post-vault-write is logged as warning (note already saved, paper can re-surface)
  - Corrupt log file triggers ErrLogCorrupted (preserves file for manual inspection)

- **LLMClient**: DocumentText field used for paper Markdown (text-only design)
  - No PageImages field; no vision/image support
  - Token counting: separate InputTokens and OutputTokens returned

- **Orchestrator.runPipeline**: Complete Phase 4 pipeline
  - Flow: SetStage(generating) → Generate → SetExplainer/AddTokens → SetStage(writing) → WriteToVault → SetVaultFile → SetStage(complete)
  - Vault write failure → Fail (sets stage to failed, log not updated)

### Fixed

- Paper.Published is now correctly treated as a string (not time.Time.Format)
  - VaultWriterTool.dateOnly() handles RFC3339, first-10-char fallback, and "unknown" gracefully
- Paper has no Category field; category sourced from `config.Agent.ArxivCategory`
- ReviewVerdict intentionally absent from Phase 4 (Phase 5 feature)

### Architecture

**Key Design Points:**
- **Text-only processing**: Markdown from Phase 3 HTML extraction replaces non-existent PDF-image pipeline
- **Atomic vault write**: Crash safety via .tmp → Rename pattern
- **Session safety**: Concurrent access guarded by mutex; server-only fields excluded from Snapshot()
- **Token tracking**: Accumulated per-phase for observability (enables monitoring of LLM cost and performance)
- **Forward-compatible**: review_iterations=1 and review_passed=true hardcoded now, Phase 5 updates from ReviewVerdict

### Dependencies Added

**Frontend:**
- `react-markdown@^18.0.0`: Markdown → React component rendering
- `remark-gfm@^4.0.0`: GitHub Flavored Markdown support (tables, strikethrough, task lists)

**Backend:**
- No new Go dependencies (text-only uses existing LLMClient interface)

### Test Coverage

- ExplainerAgent section parsing: missing/malformed headings logged as warning, content still saved
- VaultWriterTool atomic write: .tmp cleanup verified on rename failure
- Path traversal validation: ../ and absolute paths rejected
- YAML escape: special characters in titles/authors properly quoted

### Known Issues & Limitations

- **Single-pass generation**: No revision loop; Phase 5 adds ReviewerAgent
- **Text-only limitation**: Diagrams/tables visible only via captions; escape hatch for optional image channel in future
- **No follow-up linking**: arXiv IDs extracted but not auto-linked; user opens manually
- **Markdown rendering**: Some edge cases in complex Markdown (nested lists, mixed formatting) may not render perfectly in Next.js preview

### Breaking Changes

- Session API changed from direct field access (`session.PDF`, `session.Stage =`) to accessor methods (SetStage, SetExplainer, etc.)
- VaultWriterTool.WriteToVault signature changed: removed `verdict *ReviewVerdict` parameter (Phase 5 will add it back)
- ExplainerOutput now requires InputTokens and OutputTokens (was optional, now required for metrics)

### Migration Guide (if upgrading from Phase 3)

1. Update session access from `session.Stage = StageGenerating` to `session.SetStage(StageGenerating)`
2. Update VaultWriterTool calls: remove verdict parameter, pass only `(explainer, paper)`
3. Ensure LLMClient returns separate InputTokens/OutputTokens
4. Install new Frontend dependencies: `npm install react-markdown remark-gfm`
5. Update GET /result endpoint registration in server.go

### Commit References

- 📦 Phase 4 implementation: commit hash TBD (when merged)
- 📚 Docs reconciliation: commit hash TBD (when merged)

---

## [Phase 3] — 2026-06-28

### Added

- **PaperContentTool**: Fetch arXiv HTML and convert to Markdown
  - Pure-Go html-to-markdown conversion (no CGO, no PDF dependency)
  - Strips math, navigation, bibliography, appendix; preserves structure and figure captions
  - 50MB size limit with `io.LimitReader`
  - Retry logic: exponential backoff for 429 (3 retries) and 503 (1 retry)

- **LLMClient Interface**: Provider-agnostic LLM abstraction
  - Anthropic Claude, OpenAI, Google Gemini implementations
  - CompletionRequest: SystemPrompt, UserPrompt, DocumentText, MaxTokens, Temperature
  - CompletionResponse: Content, InputTokens, OutputTokens
  - Shared retry wrapper (429, 503, 400 handling)

- **New Stages**: StageExtracting, StageGenerating, StageWriting, StageComplete

- **404 Recovery**: If arXiv HTML not found, return to selection without failing

---

## [Phase 2] — 2026-06-21

### Added

- **DiscoveryTool**: Fetch top 5 papers from arXiv `cs.AI` category
- **LogCheckTool**: JSON-based processed paper tracking
- **FilterUnprocessed**: Cross-reference papers against log before surfacing
- **Selection UI**: Display candidates with title, authors, abstract
- **PipelineSession**: In-memory session state management

---

## [Phase 1] — 2026-06-14

### Added

- **Project Scaffolding**: Next.js frontend + Go backend structure
- **Config System**: YAML + `.env` with validation and override
- **ADK Integration**: Google ADK agent framework wired up
- **Local Services**: Both frontend (3000) and backend (8080) running locally
- **Base Architecture**: Orchestrator pattern for pipeline coordination

---

## Unreleased (Phase 6+)

### Planned

#### Phase 6 — Polish & Hardening
- Comprehensive error handling and user-facing messages
- Structured logging with session_id, paper_id, duration_ms, reviewer_iterations
- Test suite: unit + integration tests (ReviewerAgent, revision feedback formatting)
- Documentation: API reference, troubleshooting guide, operator manual
- Performance tuning: token budgets, request timeouts, cleanup handlers
- Frontend: error UI improvements, retry buttons, session recovery

#### Future Considerations
- Multi-category arXiv support (not just cs.AI)
- Relevance ranking / keyword filtering
- Batch processing
- Obsidian plugin
- Cloud hosting (with privacy guardrails)
- Full-text search across vault
- Auto-linking follow-up papers

---

## Release Philosophy

- **Semantic Versioning**: Not yet applied (pre-1.0)
- **Phases not Versions**: Release via phase completion, not minor/patch
- **Backwards Compatibility**: Prioritized within phases; Phase boundaries allow breaking changes
- **Documentation**: Updated alongside code (docs = code)
- **Testing**: Coverage increases per phase (Phase 1 scaffolding → Phase 6 hardening)

---

## How to Report Issues

1. Check `docs/phase*/` for known limitations in that phase
2. Open a GitHub issue with: phase, steps to reproduce, expected vs actual
3. For security issues, contact the maintainer privately
4. Reference this changelog when discussing version/phase context

---

## Questions?

See the project README, `docs/prd.md` (vision), `docs/architecture.md` (design), or phase-specific docs in `docs/phase*/`.
