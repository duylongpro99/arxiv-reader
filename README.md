# ArXiv AI Paper Explainer Agent

A Next.js frontend (`:3000`) and a Go backend (`127.0.0.1:8080`) run together
with one command. Pick a source + query, choose a paper, and the agent fetches
its HTML, generates a reviewed explainer note, and writes it to your Obsidian
vault — with a live run timeline and durable history. The discovery source is
**declarative**: arXiv is a YAML file (`resources/arxiv.yaml`), and adding a new
source is config, not code (see [`docs/adding-a-resource.md`](docs/adding-a-resource.md)).

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
- **Obsidian** with a local vault — the generated notes are written to
  `{vault}/AI Papers/`. Set the vault path in `config.yaml` (`paths.obsidian_vault`).
- **One LLM API key** — Anthropic, OpenAI, or Google Gemini (see
  [LLM Provider Setup](#llm-provider-setup)). No PDF tooling (e.g. poppler) is
  required: papers are read from arXiv's HTML rendering, not PDFs.

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

## Timeline & history (optional)

Every run streams a live, ordered event timeline. To also keep a **durable
history** you can reopen after a restart, stand up the local Postgres and apply
the schema once (migrations are user-run):

```sh
docker compose up -d db
psql "$DATABASE_URL" -f backend/migrations/0001_run_timeline.sql
```

Set `DATABASE_URL` in `.env` (see `.env.example`), or run `make db` then
`make migrate-print` for the exact commands. Without a database, tracing runs
**in-memory only** — the live timeline still works, but history and cross-restart
reload are disabled. The paper pipeline never depends on the database.

During a run, an ordered **live timeline** streams each step (paper selected →
tool calls → LLM calls → decisions → result) below the progress indicator via
Server-Sent Events. The **Run history** link (top-right) opens `/runs`, where you
can browse past runs and reopen any one to replay its full timeline — persisted,
so it survives a backend restart.

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

## LLM Provider Setup

The backend supports three providers. Set **one** API key in `.env` and point
`config.yaml` at the matching provider/model.

| Provider | `.env` key | `llm.provider` | Recommended model (`llm.model`) |
|---|---|---|---|
| Anthropic | `LLM_API_KEY` | `anthropic` | `claude-sonnet-4-6` (default) |
| OpenAI | `LLM_API_KEY` | `openai` | `gpt-4o` |
| Google Gemini | `LLM_API_KEY` | `gemini` | `gemini-2.0-flash` (largest context) |

**Switching providers:**
1. In `config.yaml`, set `llm.provider` and `llm.model`.
2. In `.env`, set `LLM_API_KEY` to that provider's key (optionally override
   `LLM_PROVIDER` / `LLM_MODEL` there instead of editing `config.yaml`).
3. Restart with `make dev`.

The key is read **only** from `.env`, never from `config.yaml`, and is never logged.

## Estimated Cost Per Paper

Rough list-price estimates for a **single generation**. With the default
`max_review_iterations: 2`, a paper runs up to **~3–4 LLM calls** (generations +
reviews), so multiply accordingly. These are **estimates** — always check your
provider dashboard for actual billing.

| Provider / model | ~Input $/1M | ~Output $/1M | Typical per paper (2 iterations) |
|---|---|---|---|
| `claude-sonnet-4-6` | $3.00 | $15.00 | ~$0.10–0.40 |
| `gpt-4o` | $2.50 | $10.00 | ~$0.08–0.30 |
| `gpt-4o-mini` | $0.15 | $0.60 | ~$0.01–0.03 |
| `gemini-2.0-flash` | $0.10 | $0.40 | ~$0.01–0.02 |

The UI shows an estimated cost in the success panel **only** for models in the
pricing table (`backend/internal/llm/pricing.go`); unknown models hide the figure.
Set `max_review_iterations: 0` to skip review entirely (one generation, lowest cost).

## Configuration Reference

All defaults live in `config.yaml` (committed, no secrets). `~` in paths expands
to `$HOME` at load.

### `llm.*`
| Field | Type | Default | Description |
|---|---|---|---|
| `provider` | string | `anthropic` | `anthropic` \| `openai` \| `gemini` (override: `LLM_PROVIDER`) |
| `model` | string | `claude-sonnet-4-6` | Provider-valid model string (override: `LLM_MODEL`) |
| `max_tokens` | int | `4096` | Per-completion output cap (> 0) |
| `temperature` | float | `0.3` | Sampling temperature (0–2) |
| `request_timeout_seconds` | int | `120` | Per-LLM-call timeout |
| `base_url` | string | `""` | Optional custom endpoint/proxy; `""` = provider default |

### `paths.*`
| Field | Type | Default | Description |
|---|---|---|---|
| `obsidian_vault` | string | `~/obsidian/arxiv-papers` | Vault root; notes land in `{vault}/AI Papers/` (override: `OBSIDIAN_VAULT_PATH`) |
| `log_file` | string | `~/.arxiv-agent/processed.json` | Processed-paper dedup log (JSON) |
| `resources_dir` | string | `./resources` | Declarative resource engine dir (`resources/*.yaml`); override: `RESOURCES_DIR` |

### `agent.*`

> The discovery source is now **declarative**: arXiv lives in `resources/arxiv.yaml`
> (+ `resources/catalogs/arxiv-cs.yaml`), which consumes the `agent.arxiv_*` values
> below via `${...}` references — the env names are unchanged, so existing overrides
> keep working. Adding a new source is a YAML file, not code — see
> [`docs/adding-a-resource.md`](docs/adding-a-resource.md).

| Field | Type | Default | Description |
|---|---|---|---|
| `arxiv_category` | string | `cs.AI` | Default arXiv category (consumed by `resources/arxiv.yaml`) |
| `arxiv_base_url` | string | `https://export.arxiv.org/api/query` | arXiv Atom API endpoint |
| `fetch_limit` | int | `20` | Papers pulled from arXiv (buffer for dedup) |
| `display_limit` | int | `5` | Candidates surfaced to the user |
| `user_agent` | string | `arxiv-explainer-agent/1.0` | HTTP User-Agent for arXiv politeness |
| `request_timeout_seconds` | int | `10` | Per arXiv-request timeout |
| `min_request_interval_seconds` | int | `3` | Base retry backoff (429/5xx: 3s → 6s → 12s) |
| `max_retries` | int | `3` | Transient arXiv retry attempts |
| `arxiv_html_base_url` | string | `https://arxiv.org/html` | LaTeXML HTML rendering base (source of paper text) |
| `max_content_bytes` | int | `52428800` | 50MB cap on fetched HTML (OOM guard) |
| `max_review_iterations` | int | `2` | Critic→revision rounds/paper (0 disables the reviewer) |

There is **no** `pdf.*` / DPI configuration — the pipeline reads HTML, not PDFs.

## Troubleshooting

| Symptom | Cause | Fix |
|---|---|---|
| "arXiv is rate limiting requests" | arXiv 429 after retries | Wait a minute and retry; the UI shows a retry counter during backoff. |
| "arXiv is currently unavailable" | arXiv 5xx / network | Transient — retry. Check arXiv status if persistent. |
| "arXiv returned an unexpected response" | Malformed Atom feed | Usually transient — retry. |
| "Paper HTML not available on arXiv" | No LaTeXML HTML for that paper | Pick another paper — the UI returns you to the candidate list. |
| "Fetching the paper's HTML timed out" | Slow HTML fetch | Retry; increase `agent.request_timeout_seconds` if chronic. |
| "Could not fetch or convert the paper's HTML" | HTML fetch/convert failure | Retry, or pick another paper. |
| "The AI provider is rate limiting requests" | LLM 429 | Wait and retry. |
| "The AI request was rejected (the paper may be too large)" | LLM bad-request / context overflow | Switch to a larger-context model (e.g. `gemini-2.0-flash`) in `config.yaml`. See the context-window warning shown before generation. |
| "Generating/Reviewing … timed out" | LLM slow | Retry; raise `llm.request_timeout_seconds`. |
| "The AI provider is currently unavailable" | LLM 5xx | Transient — retry. |
| "Could not save the note (permission denied or disk full)" | Vault path unwritable / disk full | Fix `paths.obsidian_vault` permissions or free disk space, then retry. |
| "The processed-log file is corrupted" | `processed.json` is not valid JSON | Inspect/repair or delete `paths.log_file` (deleting re-surfaces past papers). |
| "⚠️ This paper (~N tokens) may exceed …" | Context-window pre-check | Advisory only — generation proceeds. Switch models if it fails. |

**Retry behaviour:** a mid-pipeline failure (generation/review/vault) retries
**from the failed stage** — your paper selection is preserved and cached work
(extracted text, generated note) is not recomputed. A transient vault failure
re-writes with zero additional LLM cost.

## Project Structure

```
arxiv-reader/
├── backend/                 # Go service (127.0.0.1:8080)
│   ├── cmd/server/          # main entrypoint
│   └── internal/
│       ├── agents/          # explainer + reviewer (LLM prompts)
│       ├── llm/             # provider clients, retry, pricing, context limits
│       ├── models/          # session state, DTOs, domain types
│       ├── orchestrator/    # pipeline sequencing + HTTP handlers
│       ├── server/          # routing, CORS, loopback bind
│       └── tools/           # arXiv discovery, HTML→Markdown, vault writer, dedup log
├── frontend/                # Next.js UI (:3000)
│   ├── app/api/             # thin proxy routes → backend
│   ├── components/          # discovery panel, progress, result, banners
│   └── lib/                 # api client, types (mirror Go json tags)
├── config.yaml              # committed defaults (no secrets)
└── .env.example             # copy to .env; holds LLM_API_KEY
```
