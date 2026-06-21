# Phase 1 — Project Scaffolding & Config
## ArXiv AI Paper Explainer Agent

---

## Intent

Before any intelligence can be built, the system needs a reliable foundation that both services can grow on. Phase 1 exists to answer one question: **can a developer clone this repo, run one command, and have a working local environment that is ready to build on?**

Every decision in this phase serves that intent. Nothing is built here that isn't directly required to unblock Phase 2. The user sees nothing yet — this phase is entirely about developer confidence and system integrity.

---

# Part 1 — Product Requirements

## 1. Problem Statement

Without a solid local development foundation, every subsequent phase is built on uncertainty. Config errors surface late, service wiring fails silently, and developer onboarding becomes a friction-filled debugging session. Phase 1 eliminates that uncertainty by establishing a single-command startup, validated configuration, and a clear project structure that all future phases build into.

## 2. Target Users (Phase 1)

**Primary:** The developer implementing this system (you).
**Secondary:** Any future collaborator who clones the repo and needs to get running quickly.

The end user of the product is not yet involved in this phase.

## 3. User Stories

- As a developer, I want to run `make dev` so that both the Next.js frontend and Go backend start with a single command.
- As a developer, I want config validation at startup so that missing or invalid configuration fails loudly with a clear error rather than silently at runtime.
- As a developer, I want a documented `.env.example` so that I know exactly which keys are required and what they do before I start.
- As a developer, I want a health endpoint so that I can quickly verify the Go backend is running and reachable.

## 4. Functional Requirements

### F1 — Single Command Startup
- `make dev` starts both Next.js frontend (port 3000) and Go backend (port 8080) concurrently
- Both services support live reload during development (Next.js hot reload, Go via `air`)
- Startup failure of either service surfaces immediately with a clear error message

### F2 — Config System
- Config is loaded from two sources in priority order:
  1. `.env` file (machine-specific, not committed — highest priority)
  2. `config.yaml` (defaults, committed to version control)
- `.env` value for `OBSIDIAN_VAULT_PATH` overrides `config.yaml` `paths.obsidian_vault`
- All other `.env` values map to their `config.yaml` equivalents
- Config is validated at Go backend startup — missing required fields fail fast with a named error

### F3 — Required Config Fields
The following fields are required and must be present at startup:

| Field | Source | Description |
|---|---|---|
| `LLM_API_KEY` | `.env` | API key for configured LLM provider |
| `llm.provider` | `config.yaml` | One of: `anthropic`, `openai`, `gemini` |
| `llm.model` | `config.yaml` | Model string valid for the configured provider |
| `paths.obsidian_vault` | `config.yaml` / `.env` | Absolute path to Obsidian vault directory |
| `paths.log_file` | `config.yaml` | Absolute path to processed papers log file |

### F4 — Health Endpoint
- `GET /health` returns `200 { "status": "ok", "version": "0.1.0" }`
- Endpoint is reachable from Next.js and from the browser at `localhost:8080/health`

### F5 — Project Structure
- Frontend and backend are clearly separated in the repo
- Each has its own dependency management (`package.json`, `go.mod`)
- Shared contracts (API response shapes) are documented, not assumed

### F6 — Security Baseline
- Go backend binds to `127.0.0.1:8080` only — not exposed on `0.0.0.0`
- CORS configured to allow `localhost:3000` only
- `.env` is in `.gitignore` and never committed
- `.env.example` is committed with placeholder values and inline documentation

## 5. Non-Functional Requirements

- **Reproducibility** — a fresh clone + `.env` setup must work identically on any macOS or Linux machine
- **Fail-fast** — config errors must surface at startup, not mid-run
- **No premature complexity** — no database, no auth, no cloud dependencies in this phase

## 6. Success Metrics

- `make dev` starts both services in under 10 seconds on a modern laptop
- A fresh developer can go from clone to running environment in under 10 minutes following the README
- `GET /health` returns `200` reliably
- Startup with a missing required config field produces a named, actionable error message

## 7. Scope & Non-Goals

**In scope:**
- Project initialization (Next.js, Go module)
- Config system (YAML + `.env` merge, validation)
- Health endpoint
- Makefile with `make dev`
- `.env.example` with documentation
- Base project structure for both services

**Out of scope:**
- Any UI beyond the Next.js default page
- Any agent logic, tools, or LLM calls
- arXiv API integration
- Obsidian vault interaction
- Any business logic whatsoever

## 8. Open Questions

None for this phase. All requirements are fully defined.

---

# Part 2 — Architecture

## Intent

The architecture in this phase exists solely to create a clean, extensible foundation. Every structural decision made here — folder layout, config loading order, service binding — will be inherited by all subsequent phases. Getting these right now prevents costly refactoring later.

---

## 1. System Overview

Phase 1 establishes two locally running services connected by HTTP, with a shared understanding of configuration.

```
Developer runs: make dev
        │
        ├──→ Next.js (localhost:3000)   [frontend shell, no UI yet]
        │
        └──→ Go Backend (127.0.0.1:8080)
                │
                ├── Config Loader (config.yaml + .env)
                │     └── Validates required fields at startup
                │
                └── GET /health → { status: "ok" }
```

No data flows between the two services yet. The connection is verified manually via browser or curl.

---

## 2. Component Breakdown

### 2.1 Next.js Frontend Shell

**Intent:** Establish the frontend project with correct dependencies and structure so Phase 2 can add UI immediately without setup work.

**Why Next.js App Router:** App Router (Next.js 13+) is the current standard. It gives us React Server Components for static parts and Client Components for interactive UI (paper selection, progress polling) — a clean separation that will matter in Phase 4 and 5.

**Responsibilities (Phase 1 only):**
- Serve a placeholder home page confirming the frontend is running
- Establish folder structure for future phases

**Project structure:**
```
/frontend
  /app
    layout.tsx           # root layout, Tailwind base styles
    page.tsx             # placeholder home page
    /api
      /trigger
        route.ts         # proxy → Go /discover (Phase 2)
      /select
        route.ts         # proxy → Go /process (Phase 3)
      /status
        route.ts         # proxy → Go /status/:id (Phase 4)
      /result
        route.ts         # proxy → Go /result/:id (Phase 4)
  /components            # shared UI components (Phase 2+)
  /lib
    api.ts               # typed API client for Go backend calls
  package.json
  tailwind.config.ts
  tsconfig.json
  next.config.ts
```

**Why create API route stubs now:** Establishing the proxy route files early means Phase 2 only needs to fill in the logic, not create the structure. It also documents the full API contract in one place from the start.

**Dependencies:**
- Next.js 16.2.7 LTS
- TypeScript 5.x
- Tailwind CSS 4.3.0
- TanStack Query 5.101.0 (installed now, used Phase 4+)

---

### 2.2 Go Backend — Entry Point

**Intent:** Establish the Go service with correct structure and startup behaviour so all future phases add components into a predictable layout.

**Why `net/http` stdlib over a framework:** We have 5 routes total across all phases. A framework like Gin or Echo adds dependency weight and patterns that don't pay off at this scale. `net/http` is explicit, dependency-free, and perfectly sufficient.

**Responsibilities (Phase 1 only):**
- Load and validate config at startup
- Bind HTTP server to `127.0.0.1:8080`
- Serve `GET /health`
- Register CORS middleware

**Project structure:**
```
/backend
  /cmd
    /server
      main.go            # entrypoint: load config, wire server, start
  /internal
    /config
      config.go          # Config struct, loader, validator
      config.yaml        # default values (committed)
    /server
      server.go          # HTTP server setup, route registration, CORS
      health.go          # GET /health handler
    /orchestrator
      orchestrator.go    # ADK agent runner (Phase 2+)
    /tools
      discovery.go       # DiscoveryTool (Phase 2)
      logcheck.go        # LogCheckTool (Phase 2)
      pdffetch.go        # PDFFetchTool (Phase 3)
      vaultwriter.go     # VaultWriterTool (Phase 4)
    /agents
      explainer.go       # ExplainerAgent (Phase 4)
      reviewer.go        # ReviewerAgent (Phase 5)
    /llm
      client.go          # LLMClient interface
      anthropic.go       # Anthropic implementation (Phase 3)
      openai.go          # OpenAI implementation (Phase 3)
      gemini.go          # Gemini implementation (Phase 3)
    /models
      paper.go           # Paper, ExplainerOutput, ReviewVerdict, PipelineSession
  go.mod
  go.sum
  .air.toml              # air live reload config
  .env.example           # documented key template
```

**Why create all folders now:** Empty folders with placeholder files establish the full intended structure. Phase 2+ developers (or future-you) know exactly where to put new code without making layout decisions mid-implementation.

**Dependencies (Phase 1):**
- `google.golang.org/adk` (ADK Go — installed now, used Phase 2+)
- `gopkg.in/yaml.v3` (config parsing)
- `github.com/joho/godotenv` (`.env` loading)

---

### 2.3 Config Loader

**Intent:** The config system is the single source of truth for all runtime behaviour. Getting the loading order and validation right in Phase 1 means no phase ever needs to re-examine "where does this value come from."

**Why two-source config (YAML + `.env`):**
- `config.yaml` holds defaults and is safe to commit to version control
- `.env` holds machine-specific secrets and paths — never committed
- This pattern (twelve-factor inspired) means the repo is immediately usable by a new developer after filling in `.env`

**Loading order:**
```
1. Load config.yaml → populate Config struct with defaults
2. Load .env → override specific fields:
     LLM_API_KEY         → config.LLM.APIKey
     LLM_PROVIDER        → config.LLM.Provider (if set)
     LLM_MODEL           → config.LLM.Model (if set)
     OBSIDIAN_VAULT_PATH → config.Paths.ObsidianVault
3. Validate required fields → fail fast if any are missing
```

**Config struct:**
```go
type Config struct {
    LLM      LLMConfig
    Agent    AgentConfig
    ArXiv    ArXivConfig
    Paths    PathsConfig
    Explainer ExplainerConfig
}

type LLMConfig struct {
    Provider       string  // anthropic | openai | gemini
    Model          string
    APIKey         string  // from .env
    MaxTokens      int
    Temperature    float32
    TimeoutSeconds int
    BaseURL        string  // optional override
}

type AgentConfig struct {
    MaxReviewIterations int
    PaperFetchLimit     int
}

type ArXivConfig struct {
    Category string
}

type PathsConfig struct {
    ObsidianVault string  // .env overrides config.yaml
    LogFile       string
}

type ExplainerConfig struct {
    TargetWords         int
    FollowUpLinkArxiv   bool
}
```

**Validation rules:**
```go
// Required — startup fails if any of these are empty
config.LLM.APIKey         != ""
config.LLM.Provider       in ["anthropic", "openai", "gemini"]
config.LLM.Model          != ""
config.Paths.ObsidianVault != ""
config.Paths.LogFile       != ""
```

**Error output on validation failure:**
```
FATAL config error: LLM_API_KEY is required but not set.
  → Add LLM_API_KEY=your_key_here to your .env file.

FATAL config error: llm.provider "gpt4" is not valid.
  → Must be one of: anthropic, openai, gemini
```

**Dependencies:** `gopkg.in/yaml.v3`, `github.com/joho/godotenv`

---

### 2.4 HTTP Server & CORS

**Intent:** The server setup exists to give the frontend a secure, predictable endpoint. Binding to `127.0.0.1` and restricting CORS to `localhost:3000` are security decisions made once here and inherited by all phases.

**Why bind to `127.0.0.1` not `0.0.0.0`:** This is a local tool. Binding to all interfaces would expose the backend on the local network — an unnecessary risk given the backend holds API keys in memory and writes to the local filesystem.

**CORS policy:**
```
Access-Control-Allow-Origin:  http://localhost:3000
Access-Control-Allow-Methods: GET, POST, OPTIONS
Access-Control-Allow-Headers: Content-Type
```

**Route registration (Phase 1):**
```go
mux.HandleFunc("GET /health", healthHandler)
// Phase 2+ routes registered in their respective phases
```

---

## 3. Data Model

Phase 1 establishes the shared model structs used across all phases. Defining them now prevents type drift between phases.

```go
// models/paper.go

type Paper struct {
    ID        string
    Title     string
    Authors   []string
    Abstract  string
    PDFURL    string
    Category  string
    Published time.Time
}

type ExplainerOutput struct {
    PaperID   string
    Content   string
    Sections  map[string]string
    Iteration int
    CreatedAt time.Time
}

type ReviewVerdict struct {
    PaperID   string
    Pass      bool
    Score     float32
    Feedback  map[string]string
    Iteration int
    CreatedAt time.Time
}

type PipelineStage string
const (
    StageDiscovery  PipelineStage = "discovery"
    StageSelection  PipelineStage = "selection"
    StageFetching   PipelineStage = "fetching_pdf"
    StageGenerating PipelineStage = "generating"
    StageReviewing  PipelineStage = "reviewing"
    StageRevising   PipelineStage = "revising"
    StageWriting    PipelineStage = "writing"
    StageComplete   PipelineStage = "complete"
    StageFailed     PipelineStage = "failed"
)

type PipelineSession struct {
    SessionID     string
    Stage         PipelineStage
    Candidates    []Paper
    SelectedPaper *Paper
    PDF           []byte
    Explainer     *ExplainerOutput
    LastVerdict   *ReviewVerdict
    Iterations    int
    Error         string
    Recoverable   bool
    StartedAt     time.Time
    CompletedAt   *time.Time
}
```

**Why define all models in Phase 1:** Models are the shared language between components. Defining them upfront means Phase 2 and beyond implement against a stable contract rather than defining structs ad hoc and creating inconsistencies.

---

## 4. Data Flow

Phase 1 has one data flow: the health check.

```
Developer/Browser
    │
    GET localhost:8080/health
    │
    ▼
Go HTTP Server
    │
    ▼
healthHandler
    │
    ▼
Response: 200 { "status": "ok", "version": "0.1.0" }
```

Config loading happens at startup, not per-request:

```
Go process starts
    │
    ▼
Load config.yaml → Config struct (defaults)
    │
    ▼
Load .env → override specific fields
    │
    ▼
Validate required fields
    │
    ├── [Invalid] → log FATAL error with actionable message → os.Exit(1)
    │
    └── [Valid] → start HTTP server on 127.0.0.1:8080
```

---

## 5. Tech Stack

| Technology | Version | Why |
|---|---|---|
| **Next.js** | 16.2.7 LTS | Full-stack React framework. App Router for clean server/client component separation. |
| **TypeScript** | 5.x | Type safety on API contracts between frontend and Go backend. |
| **Tailwind CSS** | 4.3.0 | Utility-first styling. Zero config needed for Phase 1. |
| **TanStack Query** | 5.101.0 | Installed now for use in Phase 4+. No cost to install early. |
| **Go** | 1.26.4 | Single binary, fast startup, excellent stdlib HTTP support. |
| **`gopkg.in/yaml.v3`** | latest | Idiomatic YAML parsing in Go. |
| **`github.com/joho/godotenv`** | latest | `.env` file loading. Minimal, widely used. |
| **`google.golang.org/adk`** | latest | Installed now for use in Phase 2+. |
| **`air`** | latest | Go live reload. Eliminates manual restart during development. |

---

## 6. Integration Points

Phase 1 has no external integration points. Both services run locally and the only connection between them is verified manually.

**Internal connection (manual verification only):**
```
Next.js (localhost:3000) → browser navigates to → localhost:8080/health
```

---

## 7. Cross-Cutting Concerns

### Security
- Go backend binds to `127.0.0.1:8080` only — established here, inherited by all phases
- CORS restricted to `localhost:3000` — established here, inherited by all phases
- `.env` in `.gitignore` — established here, enforced forever
- API key never logged, even in error messages — established as a rule here

### Error Handling
- Config validation: named errors with actionable messages, `os.Exit(1)` on failure
- Server startup failure: logged with error details, process exits

### Observability
- Go `slog` initialized in `main.go` — structured JSON logging established for all phases
- Startup log confirms config loaded successfully and server is listening:
  ```json
  {"level":"INFO","msg":"config loaded","provider":"anthropic","model":"claude-sonnet-4-6"}
  {"level":"INFO","msg":"server listening","addr":"127.0.0.1:8080"}
  ```

### Developer Experience
- `make dev` is the only command a developer needs to remember
- `.env.example` is the first file a new developer reads
- All placeholder files in `/internal` subfolders include a one-line comment explaining what goes there

---

## 8. Risks & Tradeoffs

| ID | Risk/Tradeoff | Mitigation |
|---|---|---|
| T1 | Defining all model structs upfront may require revision as phases reveal new needs | Structs are defined in one file — easy to update. Phase 1 models are additive; no phase removes fields. |
| T2 | Creating empty placeholder files adds initial noise | Each placeholder has a comment explaining its Phase. Developers know immediately what belongs where. |
| R1 | `air` live reload may behave differently across OS versions | `air` is widely used and well-maintained. If issues arise, `go run ./cmd/server` is a direct fallback. |

---

## Exit Criteria

All of the following must be true before Phase 2 begins:

- [ ] `make dev` starts both services without errors
- [ ] `GET localhost:8080/health` returns `200 { "status": "ok", "version": "0.1.0" }`
- [ ] Starting Go backend with missing `LLM_API_KEY` produces a named, actionable error and exits
- [ ] Starting Go backend with invalid `llm.provider` produces a named, actionable error and exits
- [ ] `.env` is in `.gitignore` and confirmed not tracked by git
- [ ] All model structs defined in `models/paper.go`
- [ ] All placeholder files created with intent comments
- [ ] Startup log confirms config loaded and server is listening
