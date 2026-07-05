# Development Roadmap
## ArXiv AI Paper Explainer Agent

---

## Overview

The project is organized into six sequential phases, each delivering a complete, working slice of functionality. As of **2026-07-05**, Phase 4 is complete. The system can discover papers, extract content, generate rich explainers, and write them to Obsidian.

| Phase | Focus | Status | Completion |
|---|---|---|---|
| **1** | Scaffolding & Config | ✅ Complete | Phase 1 PR merged |
| **2** | Discovery & Deduplication | ✅ Complete | Phase 2 PR merged |
| **3** | HTML Extraction & LLM Client | ✅ Complete | Phase 3 PR merged |
| **4** | Explainer & Vault Write | ✅ Complete | 2026-07-05 |
| **5** | Reviewer & Revision Loop | ⏳ Planned | Q3 2026 |
| **6** | Polish & Hardening | ⏳ Planned | Q4 2026 |

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
**Status:** ⏳ Planned

**Expected Deliverables:**
- ReviewerAgent: Evaluate explainer against quality rubric
- Revision loop: Generate → Review → Revise until Pass or max iterations
- ReviewVerdict: Pass/Fail with per-section feedback
- Iteration tracking: Explainer.Iteration incremented per revision
- Frontmatter enhancement: review_iterations and review_passed from verdict
- Progress indicator: "reviewing" and "revising" stages

**Key Changes:**
- VaultWriterTool.WriteToVault now accepts verdict parameter
- Orchestrator.runPipeline inserts review loop between generation and vault write
- LLM client supports structured output for verdict JSON

**Dependencies:**
- Phase 4 must be complete (explainer generation working)
- All 9 sections stable from Phase 4 (reviewer expects consistent structure)

---

## Phase 6 — Polish & Hardening
**Status:** ⏳ Planned

**Expected Deliverables:**
- Comprehensive error handling with user-friendly messages
- Enhanced logging (structured JSON, trace-level detail)
- README with setup instructions, environment variables, troubleshooting
- Test suite: unit tests for tools, agents, orchestrator
- Documentation: API reference, config guide, operator manual
- Performance optimization: token budgets, LLM timeouts, cleanup handlers

**Key Improvements:**
- Observability: Every significant event logged with session_id, paper_id, duration_ms
- Resilience: Graceful degradation on transient failures
- User experience: Clear progress updates, actionable error messages
- Developer experience: Contributing guide, local dev setup, CI/CD pipeline

---

## Key Milestones

| Milestone | Date | Achieved |
|---|---|---|
| Phase 1 scaffolding complete | 2026-06-14 | ✅ Yes |
| Phase 2 discovery working | 2026-06-21 | ✅ Yes |
| Phase 3 HTML extraction & LLM | 2026-06-28 | ✅ Yes |
| Phase 4 explainer & vault write | 2026-07-05 | ✅ Yes |
| User can trigger → select → receive note | 2026-07-05 | ✅ Yes |

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

### Phase 4 Limitations
- **Single-pass generation:** No revision loop; Phase 5 adds that
- **Text-only:** Diagrams described by captions only; Phase ? may add optional image channel
- **No auto-linking:** Follow-up papers listed; user opens arXiv manually; Phase ? may add links
- **Obsidian only:** No other vault formats; future phases may add more targets

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
