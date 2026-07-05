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
| `config.yaml` | Committed defaults (provider, model, paths, reviewer settings). No secrets. |
| `.env` | Per-machine secrets & overrides. Gitignored, never committed. |

`.env` keys: `LLM_API_KEY` (required), `LLM_PROVIDER`, `LLM_MODEL`,
`OBSIDIAN_VAULT_PATH` (optional overrides). The API key is read only from `.env`
and is never logged.

### Reviewer Loop & Quality Control (Phase 5)

The system includes an optional reviewer loop that evaluates each generated
explainer against a quality rubric before writing it to the vault. This is
controlled via `config.yaml`:

```yaml
agent:
  max_review_iterations: 2  # Default: 2 rounds of review+revision
```

**How it works:**
1. **Generate** — Create initial explainer (iteration 1)
2. **Review** — Independent critic evaluates explainer against 6-criteria rubric
   - Clarity of author intent
   - Accuracy and layering of analogies
   - Mathematical correctness and appropriateness
   - Figure/diagram descriptions and explanations
   - Glossary prioritization
   - Tone for technical practitioners
3. **Revise (if needed)** — If reviewer says "Pass", proceed to vault. If "Fail" and iterations remain, feed structured feedback back to the explainer agent and retry.
4. **Repeat** — Loop until reviewer approves OR max iterations reached, then write to vault

**Disabling the reviewer:** Set `max_review_iterations: 0` to skip review entirely and write the first-pass explainer directly to the vault. This reproduces Phase 4 behavior at zero reviewer cost.

**Token cost:** Default settings (max 2 iterations) consume approximately **200k tokens per paper**
(2 generations + 2 reviews). Reviewers use the same LLM as the explainer generator but at very low temperature (0.1)
for consistent, repeatable evaluation.

**Frontmatter:** Each note includes review metadata:
- `review_iterations: N` — how many review rounds ran
- `review_passed: true/false` — whether the final explainer was approved
- `review_score: 0.00–1.00` — quality score from the reviewer (omitted if reviewer disabled)
