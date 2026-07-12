# Development Roadmap
## ArXiv AI Paper Explainer Agent

---

## Overview

The project is organized into sequential phases, each delivering a complete, working slice of functionality. As of **2026-07-12**, Phase 7 is complete. The system can discover papers, extract content, generate and review explainers, write them to Obsidian, and provide a complete live and persistent timeline of every run.

| Phase | Focus | Status | Completion |
|---|---|---|---|
| **1** | Scaffolding & Config | ✅ Complete | Phase 1 PR merged |
| **2** | Discovery & Deduplication | ✅ Complete | Phase 2 PR merged |
| **3** | HTML Extraction & LLM Client | ✅ Complete | Phase 3 PR merged |
| **4** | Explainer & Vault Write | ✅ Complete | 2026-07-05 |
| **5** | Reviewer & Revision Loop | ✅ Complete | 2026-07-05 |
| **6** | Polish & Hardening | ✅ Complete | 2026-07-05 |
| **7** | Run Timeline Tracing | ✅ Complete | 2026-07-12 |

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

### Phase 5 Limitations (resolved from Phase 4)
- ~~**Single-pass generation:** No revision loop; Phase 5 adds that~~ → Phase 5 adds critic-generator loop
- **Text-only:** Diagrams described by captions only; Phase ? may add optional image channel
- **No auto-linking:** Follow-up papers listed; user opens arXiv manually; Phase ? may add links
- **Obsidian only:** No other vault formats; future phases may add more targets
- **Single paper per run:** No batch processing; Phase ? may add multiple papers
- **Reviewer cost:** Default 2 iterations ≈ 200k tokens/paper; Phase 6 may add cost monitoring

### Recommended Additions (Post-Phase 6)
- Multi-category support (not just `cs.AI`)
- Relevance ranking / keyword filtering
- Batch processing multiple papers
- Obsidian plugin for auto-sync
- Cloud hosting / remote access (with strong privacy caveats)
- Full-text search across generated notes
- Categorization / tagging beyond static frontmatter

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
