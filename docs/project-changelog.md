# Project Changelog
## ArXiv AI Paper Explainer Agent

All notable changes to this project are documented below, organized by release and phase.

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

## Unreleased (Phase 5+)

### Planned

#### Phase 5 — Reviewer & Revision Loop
- ReviewerAgent with quality rubric evaluation
- Revision loop: Generate → Review → Feedback → Revise → Vault write
- ReviewVerdict with Pass/Fail and per-section feedback
- Iteration tracking in Explainer and frontmatter
- New stages: reviewing, revising

#### Phase 6 — Polish & Hardening
- Comprehensive error handling and user-facing messages
- Structured logging with session_id, paper_id, duration_ms
- Test suite: unit + integration tests
- Documentation: README, config guide, operator manual
- Performance tuning: token budgets, timeouts, cleanup

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
