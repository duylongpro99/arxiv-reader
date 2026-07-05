# Project Changelog
## ArXiv AI Paper Explainer Agent

All notable changes to this project are documented below, organized by release and phase.

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
