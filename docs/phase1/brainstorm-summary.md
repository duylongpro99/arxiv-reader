# Phase 1 Brainstorm Summary — Scaffolding & Config

> Source PRD: `docs/phase1/prd.md` · Date: 2026-07-03 · Status: design agreed, plan/impl pending approval

## Problem
Establish a one-command local dev foundation (Next.js + Go) with validated two-source config, health endpoint, security baseline. No product logic. Must unblock Phase 2 without forcing refactors later.

## Decisions (locked with user)
- **Scaffold scope:** phase-by-phase minimal — Phase 1 creates ONLY what Phase 1 uses. No Phase 2–6 folders, placeholder files, or model structs.
- **Repo layout:** monorepo, **single `.env` at root**; `config.yaml` also at root (loaded from cwd=root, no relative-path fragility).
- **Versions:** latest stable, not the PRD's pins.

## Verified facts
- `google.golang.org/adk` resolves (HTTP 200) — real, deferred to Phase 2+.
- Latest: Next `16.2.10`, React `19.2.7`, TypeScript `6.0.3`, Tailwind `4.3.2`, TanStack Query `5.101.2` (deferred to Phase 4). Go deps: `gopkg.in/yaml.v3 v3.0.1`, `github.com/joho/godotenv v1.5.1`.
- **Phase 1 Go deps = yaml.v3 + godotenv only.**
- ⚠️ Go toolchain NOT installed on this machine — user prerequisite (node/npm present).

## PRD deviations (intentional, follow from YAGNI + phase-by-phase)
1. No Phase 2–6 placeholder tree (`tools/`, `agents/`, `llm/`, `orchestrator/`).
2. No upfront model structs — PRD exit criterion "all models in paper.go" dropped for Phase 1; models defined per-phase. (Tradeoff: lose upfront shared contract, gain zero dead code.)
3. No ADK / LLM SDK / TanStack Query install until their phase.

## Latent bugs in PRD — fixed in design
- **`~` paths fail validation:** defaults `~/obsidian/...` & `~/.arxiv-agent/...` but F3 requires absolute; Go doesn't expand `~`. Fix: expand `~`→`$HOME`, THEN assert absolute.
- **`api_key: "${LLM_API_KEY}"`** in config.yaml is misleading — yaml.v3 doesn't interpolate. Fix: drop it; key comes only from `.env`.
- **`.gitignore`** currently only `.claude`. Fix: add `.env`, `node_modules`, `/backend/tmp`, binaries.

## Target structure
```
/
  Makefile              # make dev (trap 'kill 0' EXIT; both bg; wait — portable mac/linux)
  .env / .env.example   # root, secrets gitignored
  config.yaml           # root, committed defaults
  .gitignore
  /frontend             # create-next-app: App Router, TS, Tailwind; placeholder page
  /backend
    cmd/server/main.go        # load config -> slog -> start
    internal/config/config.go # struct + loader + validator
    internal/server/server.go # mux, CORS, 127.0.0.1:8080
    internal/server/health.go # GET /health
    .air.toml
    go.mod
```

## Config behavior
Load `config.yaml` (defaults) → override from `.env` (`LLM_API_KEY`, `LLM_PROVIDER`, `LLM_MODEL`, `OBSIDIAN_VAULT_PATH`) → expand `~` on paths → validate → fail fast with named error + `os.Exit(1)`.
Validation: `LLM_API_KEY!=""`, `provider ∈ {anthropic,openai,gemini}`, `model!=""`, vault & log paths absolute after expansion. slog logs provider/model on success — **never the key**.

## Server
`net/http` stdlib · bind `127.0.0.1:8080` · CORS allow-origin `http://localhost:3000` only, methods GET/POST/OPTIONS · `GET /health` → `200 {"status":"ok","version":"0.1.0"}`.

## Exit criteria
`make dev` up <10s · `/health` 200 · missing `LLM_API_KEY` → named error+exit · invalid provider → named error+exit · `.env` gitignored & untracked · startup log confirms. (PRD "all models defined" criterion removed per deviation #2.)

## Risks
- Go not installed → document in README prerequisites; `make dev` fails loud if `go`/`air` missing.
- Latest TS 6 / Next 16 / Tailwind 4 are recent majors — pin exact resolved versions in lockfiles once scaffolded to keep reproducibility.

## Next steps (await user)
1. Confirm the 3 deviations (esp. #2 dropping upfront models).
2. On approval → `/ck:plan` for detailed Phase 1 phase docs, then implement.
