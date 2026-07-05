# Phase 01 — Config & Models Foundation

**Context:** `docs/phase3/prd.md` §3 (Data Model), §5, §7 · `docs/phase3/brainstorm-summary.md` §4.3–4.4
**Priority:** Critical · **Status:** complete · **Depends on:** Phase 2 (complete) · **Effort:** ~M

## Overview
Lay the config + session state groundwork every later phase leans on: a testable arXiv-HTML base
URL, a content-size cap, a mutex-guarded `markdownText` field (excluded from `Snapshot()`), a
`selectedPaper` field, the `StageExtracting` stage, and the accessors that mutate them safely.
No HTTP, no LLM, no HTML here — just the contracts. Also confirm the LLM config carries the
fields Phase 3–4 need (MaxTokens/Temperature/Timeout/BaseURL) before those phases consume them.

## Key insights (locked decisions)
- `PipelineSession` is **mutex-guarded, private fields only** (`session.go:26`). The old PRD's
  direct `session.Stage = ...` mutation **will not compile** — every write goes through an
  accessor that takes the lock. Mirror the existing `Complete`/`Fail` pattern exactly.
- `markdownText` is **large (~50KB–500KB) and transient** → MUST be excluded from `Snapshot()`
  (never serialized to the frontend, keeps `/status` cheap).
- 404 recovery needs a transition **from `extracting` back to `selection`** that **preserves
  `candidates`** and sets a recoverable notice — NOT `Fail` (which sets `failed`).
- `arxiv_html_base_url` mirrors the existing `arxiv_base_url` pattern purely so tests can point
  at an `httptest.Server` (same reason Phase 2 made `arxiv_base_url` configurable).
- **No `pdf.dpi` exists** in `config.go` today — the task is simply to NOT add any PDF/dpi field.

## Requirements (PRD F1, F3, F7)
- Config: `agent.arxiv_html_base_url` (default `https://arxiv.org/html`) + `agent.max_content_bytes`
  (cap, default `52428800` = 50MB) with fail-fast validation.
- LLM config: ensure `MaxTokens`, `Temperature`, `RequestTimeoutSec`, `BaseURL` exist (add any
  missing — Phase 3/4 read them). Check `config.go` first; only add what is absent.
- Models: `StageExtracting = "extracting"`; private `markdownText` + `selectedPaper`; accessors
  `SetStage`, `SetMarkdown`, `SetSelectedPaper`, `SetNotice`, and a `RecoverToSelection(notice)`
  transition (extracting → selection, candidates intact, recoverable notice set).
- `Snapshot()` unchanged in shape (must NOT include `markdownText`).

## Related code files
**Modify:**
- `backend/internal/config/config.go` — add `ArxivHTMLBaseURL`, `MaxContentBytes` to `AgentConfig`
  + validation in `AgentConfig.validate()`; add missing `LLMConfig` fields (see below).
- `config.yaml` — add `agent.arxiv_html_base_url`, `agent.max_content_bytes`; add any new `llm.*`
  fields (keys only; API key stays in `.env`).
- `backend/internal/config/config_test.go` — cover new field defaults + validation failures.
- `backend/internal/models/session.go` — add stage const, private fields, accessors, transition.
- `backend/internal/models/session_test.go` — **create** (no session test exists yet): assert
  accessors mutate under lock, `Snapshot()` omits markdown, `RecoverToSelection` keeps candidates.

## Design detail
```go
// config.go — AgentConfig additions
ArxivHTMLBaseURL string `yaml:"arxiv_html_base_url"` // default https://arxiv.org/html
MaxContentBytes  int64  `yaml:"max_content_bytes"`   // io.LimitReader cap; > 0

// AgentConfig.validate() additions (name field + fix, key-free like existing msgs)
if a.ArxivHTMLBaseURL == "" { return fmt.Errorf("agent.arxiv_html_base_url is required ...") }
if a.MaxContentBytes <= 0  { return fmt.Errorf("agent.max_content_bytes must be > 0 ...") }

// LLMConfig — ADD ONLY IF ABSENT (Phase 3/4 consumers):
MaxTokens   int     `yaml:"max_tokens"`     // > 0
Temperature float32 `yaml:"temperature"`    // 0..2
RequestTimeoutSec int `yaml:"request_timeout_seconds"` // > 0 (LLM calls are slow)
BaseURL     string  `yaml:"base_url"`       // optional; "" = provider default
```
```go
// session.go — additions
const StageExtracting PipelineStage = "extracting"

// private fields on PipelineSession
markdownText  string
selectedPaper *Paper

func (s *PipelineSession) SetStage(stage PipelineStage)      // lock; s.stage = stage
func (s *PipelineSession) SetSelectedPaper(p *Paper)         // lock; s.selectedPaper = p
func (s *PipelineSession) SetMarkdown(md string)             // lock; s.markdownText = md
func (s *PipelineSession) SetNotice(n string)                // lock; s.notice = n

// RecoverToSelection returns an extracting session to selection WITHOUT touching
// candidates, clearing any prior error and setting a recoverable notice. This is the
// 404 re-pick path (F1 relaxed) — distinct from Fail (which sets StageFailed).
func (s *PipelineSession) RecoverToSelection(notice string)  // lock; stage=selection, notice=n, errMsg="", recoverable=true
```
> **Snapshot() stays as-is:** do NOT add `markdownText`/`selectedPaper` to `SessionSnapshot`.
> Markdown is server-only; the selected paper's metadata is already in `candidates`.

## Implementation steps
1. `config.go`: add `AgentConfig` fields + validation; add missing `LLMConfig` fields.
2. `config.yaml`: add the new keys with committed defaults (no secrets).
3. `config_test.go`: table cases — valid load populates new fields; empty base URL fails;
   `max_content_bytes <= 0` fails; new LLM field validation (if added).
4. `session.go`: stage const, private fields, accessors, `RecoverToSelection`.
5. `session_test.go`: accessor round-trips; `Snapshot()` excludes markdown; recover keeps candidates.
6. `go build ./...` + `go test ./internal/config/... ./internal/models/...` green.

## Todo
- [x] `AgentConfig` fields `ArxivHTMLBaseURL`, `MaxContentBytes` + validation
- [x] `LLMConfig` MaxTokens/Temperature/RequestTimeoutSec/BaseURL (only if missing)
- [x] `config.yaml` new keys with defaults
- [x] `config_test.go` defaults + validation-failure cases
- [x] `session.go` `StageExtracting` + `markdownText`/`selectedPaper` + accessors
- [x] `RecoverToSelection(notice)` transition (candidates preserved, recoverable)
- [x] `session_test.go` (new) accessor + Snapshot-exclusion + recover coverage
- [x] `go build` + package tests green

## Success criteria
- Config loads with new agent fields defaulted; missing/invalid values fail fast with a named,
  key-free message (matches existing validation style).
- `Snapshot()` never exposes `markdownText`; `go test -race ./internal/models/...` clean.
- `RecoverToSelection` yields `stage=selection`, candidates intact, `recoverable=true`.
- No PDF/dpi field introduced anywhere.

## Risk Assessment
| Risk | L×I | Mitigation |
|---|---|---|
| Adding a field to `SessionSnapshot` by habit → leaks markdown to frontend | Low×High | Explicit test asserting `SessionSnapshot` has no markdown field; call out in PR. |
| Duplicate LLM field if one already exists | Med×Low | Read `config.go` (done) — only `Provider/Model/APIKey` today; add the rest. |
| Validation too strict breaks Phase 2 config | Low×Med | New fields default in `config.yaml`; existing tests must stay green. |

## Backwards compatibility
Phase 2 sessions/config keep working: new fields are additive with defaults; `Snapshot()` shape
unchanged so the `/status` contract and frontend types are untouched by this phase.

## Rollback
Revert `config.go`/`session.go`/`config.yaml` edits; new test file deleted. No data/state migration
(in-memory sessions only). No DB, no migrations.

## Security
- API key still `.env`-only, never in `config.yaml` (existing invariant preserved).
- `max_content_bytes` is the OOM guard consumed by Phase 02's `io.LimitReader`.

## Next Steps
Unblocks **Phase 02** (needs `ArxivHTMLBaseURL`, `MaxContentBytes`) and **Phase 03** (needs
`LLMConfig` fields). File ownership: this phase solely owns `config.go`, `config.yaml`,
`session.go` and their tests — no other phase edits them.
