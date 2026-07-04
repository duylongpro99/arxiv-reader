# ArXiv AI Paper Explainer Agent

Local dev foundation: a Next.js frontend (`:3000`) and a Go backend
(`127.0.0.1:8080`) run together with one command. Phase 1 is scaffolding + config
only — no product logic yet.

## Prerequisites

- **Go 1.23+** — NOT installed by default on this machine. Install from
  <https://go.dev/dl/>.
- **Node 20+** — install from <https://nodejs.org>.
- **`air`** (Go live-reload):
  ```sh
  go install github.com/air-verse/air@latest
  ```
  Then ensure `$(go env GOPATH)/bin` is on your `PATH` so the `air` command is
  found.

`make dev` fails loudly with actionable instructions if `go`, `air`, or `npm` is
missing.

## Setup

```sh
cp .env.example .env
# open .env and set LLM_API_KEY=your_key_here
```

Adjust `config.yaml` paths (`obsidian_vault`, `log_file`) if the defaults don't
suit your machine. `~` is expanded to `$HOME` at load. `make dev` auto-runs
`npm ci` for the frontend on first run.

## Run

```sh
make dev
```

Starts the frontend on `:3000` and the backend on `127.0.0.1:8080` concurrently
with live reload. Ctrl-C tears both down (no orphaned processes). Note: `air`
supervises the backend and auto-restarts it on a crash/rebuild (so a backend
crash reloads rather than stopping the frontend); a frontend crash stops both.

## Verify

```sh
curl -s 127.0.0.1:8080/health
# → {"status":"ok","version":"0.1.0"}
```

Use `127.0.0.1`, not `localhost` — the backend binds the IPv4 loopback only.

## Configuration

| Source | Purpose |
|---|---|
| `config.yaml` | Committed defaults (provider, model, paths). No secrets. |
| `.env` | Per-machine secrets & overrides. Gitignored, never committed. |

`.env` keys: `LLM_API_KEY` (required), `LLM_PROVIDER`, `LLM_MODEL`,
`OBSIDIAN_VAULT_PATH` (optional overrides). The API key is read only from `.env`
and is never logged.
