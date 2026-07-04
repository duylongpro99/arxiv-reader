# Phase 01 — Models & Config Foundation

**Context:** `docs/phase2/prd.md` §2.2, §2.3, §3 · `docs/phase2/brainstorm-summary.md`
**Priority:** Critical (blocks 02–06) · **Status:** pending · **Effort:** ~M

## Overview
Create the shared data contracts (`internal/models`) and extend config with the `Agent`
section arXiv needs. Phase 1 deliberately shipped no models, so this is net-new. Nothing
here talks to arXiv or disk yet — pure types, config parsing, validation.

## Key insights
- PRD's `o.config.Agent.PaperFetchLimit` references a section that does not exist.
- Frontend `Paper` type (PRD §2.2) is camelCase → Go structs need explicit `json:"pdfUrl"`.
- `server.Run()` currently takes no args; orchestrator (Phase 04) needs config → add param now.

## Requirements
- `models.Paper`, `models.PipelineSession`, `models.PipelineStage` + constants.
- `config.Agent` section parsed from `config.yaml`, validated fail-fast.
- `server.Run(cfg *config.Config)` signature; `main.go` passes `cfg`.

## Related code files
**Create:**
- `backend/internal/models/paper.go` — `Paper` struct (camelCase JSON tags).
- `backend/internal/models/session.go` — `PipelineStage` + constants, `PipelineSession`
  (with `sync.RWMutex` + accessor methods to keep the mutex unexported/safe).
**Modify:**
- `backend/internal/config/config.go` — add `AgentConfig`, wire into `Config`, extend `validate()`.
- `config.yaml` — add `agent:` block; rename `log_file` → `~/.arxiv-agent/processed.json`.
- `.env.example` — document optional `ARXIV_CATEGORY` override if desired (optional; keep minimal).
- `backend/internal/server/server.go` — `Run(cfg *config.Config) error`.
- `backend/cmd/server/main.go` — `server.Run(cfg)`.

## Design detail

### models/paper.go
```go
package models

type Paper struct {
    ID        string   `json:"id"`
    Title     string   `json:"title"`
    Authors   []string `json:"authors"`
    Abstract  string   `json:"abstract"`
    PDFURL    string   `json:"pdfUrl"`     // camelCase for frontend
    Published string   `json:"published"`  // ISO-8601 string
}
```

### models/session.go
```go
type PipelineStage string
const (
    StageDiscovery PipelineStage = "discovery"
    StageSelection PipelineStage = "selection"
    StageFailed    PipelineStage = "failed"
    // full enum (fetching_pdf, generating, …) added in later phases as reached
)

// PipelineSession is mutated by the discovery goroutine and read by the status
// handler concurrently → all field access goes through the RWMutex.
type PipelineSession struct {
    mu          sync.RWMutex
    SessionID   string
    Stage       PipelineStage
    Candidates  []Paper
    Notice      string   // e.g. "Only 3 new papers found"
    Error       string
    Recoverable bool
    StartedAt   time.Time
}

// Provide Snapshot() (copy under RLock) + setters (SetStage, Fail, Complete) so
// callers never touch fields directly. Keeps the race guarantee in one place.
```
> **Why accessor methods:** single-writer (goroutine) + single-reader (poll) still races on
> slice/string fields. Centralizing locking in `Snapshot()`/setters prevents scattered `mu` use.

### config Agent section
```yaml
# config.yaml
agent:
  arxiv_category:  cs.AI
  arxiv_base_url:  https://export.arxiv.org/api/query
  fetch_limit:     20      # buffer pulled from arXiv
  display_limit:   5       # candidates surfaced to user
  user_agent:      "arxiv-explainer-agent/1.0"
  request_timeout_seconds:      10
  min_request_interval_seconds: 3
  max_retries:                  3

paths:
  log_file: ~/.arxiv-agent/processed.json   # was processed.log
```
```go
type AgentConfig struct {
    ArxivCategory        string `yaml:"arxiv_category"`
    ArxivBaseURL         string `yaml:"arxiv_base_url"`
    FetchLimit           int    `yaml:"fetch_limit"`
    DisplayLimit         int    `yaml:"display_limit"`
    UserAgent            string `yaml:"user_agent"`
    RequestTimeoutSec    int    `yaml:"request_timeout_seconds"`
    MinRequestIntervalSec int   `yaml:"min_request_interval_seconds"`
    MaxRetries           int    `yaml:"max_retries"`
}
// Config gains: Agent AgentConfig `yaml:"agent"`
```
**validate() additions:** `arxiv_category != ""`, `arxiv_base_url != ""`, `fetch_limit > 0`,
`display_limit > 0`, `display_limit <= fetch_limit`, `max_retries >= 0`, timeouts `> 0`.
Named, key-free error messages consistent with existing style.

## Implementation steps
1. Create `internal/models/paper.go`.
2. Create `internal/models/session.go` with mutex + `Snapshot()`/setters.
3. Add `AgentConfig` to `config.go`, wire into `Config`, extend `validate()`.
4. Update `config.yaml` (`agent:` block + rename log path).
5. Change `server.Run` to accept `*config.Config`; update `main.go`.
6. `go build ./...` — must compile clean.

## Todo
- [ ] `models/paper.go` with camelCase tags
- [ ] `models/session.go` (mutex, Snapshot, setters, stage constants)
- [ ] `config.AgentConfig` + validation
- [ ] `config.yaml` agent block + log path rename
- [ ] `server.Run(cfg)` + `main.go` wiring
- [ ] `go build ./...` clean; unit test for config Agent validation

## Success criteria
- `go build ./...` passes.
- Config with valid `agent` block loads; `display_limit > fetch_limit` fails with named error.
- Missing `agent` block → clear validation error (no silent zero-values downstream).

## Risks
- Zero-value trap: if `agent:` absent, ints default 0 → validation MUST catch (`fetch_limit>0`).
- Log-path rename is user-visible: existing `~/.arxiv-agent/processed.log` won't be read. First
  run creates fresh `.json` (Phase 4 writes it). Acceptable pre-launch; note in README if needed.

## Security
- No secrets in `config.yaml`. Category from config, never user input (PRD §7).
- Reuse Phase 1 key-free error convention.
