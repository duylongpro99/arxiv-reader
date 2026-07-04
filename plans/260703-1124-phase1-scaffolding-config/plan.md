---
plan: phase-01-scaffolding-config
title: "Phase 1 — Scaffolding & Config"
project: ArXiv AI Paper Explainer Agent
status: pending
created: 2026-07-03
owner: long.dao@maritime-ds.com
source_prd: docs/phase1/prd.md
source_design: docs/phase1/brainstorm-summary.md
blockedBy: []
blocks: []
status: complete
---

# Phase 1 — Scaffolding & Config

## Overview
Establish a one-command local dev foundation: Next.js frontend (`:3000`) + Go backend
(`127.0.0.1:8080`), two-source config (`config.yaml` defaults + `.env` overrides), fail-fast
validation, `GET /health`, and a security baseline. No product logic. Goal: `git clone` →
`cp .env.example .env` + fill key → `make dev` → both services up in <10s. Nothing built here
that Phase 2 does not need.

Scaffold is **phase-by-phase minimal** — Phase 1 creates ONLY what Phase 1 uses. No Phase 2–6
folders, placeholder files, or model structs. No ADK / LLM SDK / TanStack Query installs.

## Phases
| Phase | File | Status | Description |
|---|---|---|---|
| 01 | [phase-01-scaffolding-config.md](./phase-01-scaffolding-config.md) | complete | Root config + Makefile, Go backend (config/server/health), Next.js shell, security baseline |

Phase 1 is a single, self-contained phase. Subsequent phases (discovery, LLM, agents, vault
writer, UI) are out of scope here and are NOT pre-scaffolded.

## Locked Design Decisions
- Monorepo; single `.env` **and** `config.yaml` at repo **root** (loaded from cwd=root; Makefile
  guarantees cwd=root — no relative-path fragility).
- Versions: Next `16.2.10`, React `19.2.7`, TypeScript `6.0.3`, Tailwind `4.3.2`.
  Go deps: `gopkg.in/yaml.v3 v3.0.1` + `github.com/joho/godotenv v1.5.1` ONLY.
  Module path: `github.com/maritime-ds/arxiv-reader`.
- `net/http` stdlib (5 total routes across all phases — no framework).

## Baked-in Bug Fixes (from design review)
1. Expand leading `~`→`$HOME` on `obsidian_vault` & `log_file` **before** asserting absolute
   (PRD defaults are `~/...`, which would otherwise fail its own absolute-path check).
2. Drop `api_key: "${LLM_API_KEY}"` from `config.yaml` — yaml.v3 does NOT interpolate; key
   comes only from `.env` (`APIKey` field carries yaml tag `-`).
3. `.gitignore` currently holds only `.claude`; add `.env`, `node_modules/`, `/backend/tmp/`.

## Red-Team Resolutions (adversarial review pass)
| # | Sev | Finding | Resolution |
|---|---|---|---|
| RT1 | high | Fresh clone: `make dev` ran `npm run dev` with no `node_modules` → frontend fails | `dev` now depends on `install` target (`npm ci` if `node_modules` absent); README documents it |
| RT2 | high | Plain `wait` doesn't tear down survivor on single-service crash (bash 3.2 lacks `wait -n`) | Replaced with poll loop (`kill -0` on both pids) + `kill 0`. Ctrl-C, a `next` exit, or `air` exiting tears both down. CAVEAT: `air` supervises the backend binary, so it absorbs+restarts a `tmp/server` crash (dev-desirable) rather than exiting — teardown is not triggered by a bare `pkill -f tmp/server`. |
| RT3 | med | `server listening` logged before bind → false log if port in use | `net.Listen` first, then log, then `http.Serve` |
| RT4 | med | Exit-criteria grepped `{"msg":"config loaded"` but slog emits time/level/msg first | Doc greps `"msg":"config loaded"` (no leading brace) |
| RT5 | low | `127.0.0.1` bind vs `localhost` verify → IPv6 flake | README verify uses `curl 127.0.0.1:8080/health` |
| RT6 | low | Gitignore `/backend/server` didn't match air output (`backend/tmp/server`) | Dropped; `/backend/tmp/` covers it |
| RT7 | low | `server.Run(cfg)` took unused param (YAGNI) | Dropped param; `Run()` takes none until Phase 2 needs it |

## Dependencies & Prerequisites
- **Go toolchain NOT installed on dev machine** — user must install (README prerequisite).
  `make dev` fails loudly (`check-tools`) if `go`/`air`/`npm` missing.
- Node present. `air` installed via `go install github.com/air-verse/air@latest`.

## Exit Criteria
- `make dev` brings both services up <10s (fresh clone, after `cp .env.example .env` + key).
- `GET /health` → `200` body exactly `{"status":"ok","version":"0.1.0"}`.
- Missing `LLM_API_KEY` → named error + `os.Exit(1)`.
- Invalid `llm.provider` → named error + `os.Exit(1)`.
- `.env` gitignored & confirmed untracked (`git check-ignore .env` prints `.env`).
- Startup slog lines contain `"msg":"config loaded"` (provider+model) and `"msg":"server listening"` (addr); API key never logged.
- Ctrl-C (or a `next`/`air` exit) tears down the other (no orphaned `air`/`next`). A backend-binary crash is restarted by `air` by design, not a teardown trigger.
- (PRD "all model structs defined" criterion intentionally REMOVED for Phase 1.)
