# Phase 1 Requirements

Source: `docs/phase1/prd.md`

This document captures requirements only. It does not resolve ambiguities, make architecture decisions, or add implementation beyond the PRD.

## Ambiguities And Unstated Assumptions

1. The PRD states "Open Questions: None," but several behaviors are not fully specified.
2. The PRD says `.env` has highest priority over `config.yaml`, but the required config table lists `llm.provider` and `llm.model` as `config.yaml` fields while the config loader section says `LLM_PROVIDER` and `LLM_MODEL` may override them.
3. The PRD says "All other `.env` values map to their `config.yaml` equivalents," but it does not define the complete list of supported `.env` keys beyond `LLM_API_KEY`, `LLM_PROVIDER`, `LLM_MODEL`, and `OBSIDIAN_VAULT_PATH`.
4. The PRD requires `llm.model` to be "valid for the configured provider," but the validation rules only require the model to be non-empty. The definition of a valid model is unspecified.
5. The PRD requires `paths.obsidian_vault` to be an absolute path to an Obsidian vault directory, but it does not state whether startup validation must verify that the path exists, is a directory, is readable, or is writable.
6. The PRD requires `paths.log_file` to be an absolute path to a processed papers log file, but it does not state whether startup validation must verify that the file exists, that the parent directory exists, or that the path is writable.
7. The PRD requires reproducibility on macOS and Linux, but does not specify supported CPU architectures, shell requirements, package manager assumptions, or whether Windows/WSL is intentionally unsupported.
8. The PRD says a fresh clone plus `.env` setup must work, but it does not state whether `make dev` must install dependencies automatically or whether dependency installation is a separate prerequisite.
9. The PRD says a developer should run "one command" to get a working local environment, while also requiring `.env` setup. It is unclear whether the one command applies after manual `.env` creation only.
10. The PRD requires `make dev` to start both services in under 10 seconds, but it does not specify whether this excludes first-time dependency installation, Next.js compilation, Go module download, or `air` installation.
11. The PRD requires both services to support live reload, but it does not specify whether `air` must be installed globally, vendored, run through `go install`, or invoked another way.
12. The PRD requires startup failure of either service to surface immediately with a clear error message, but it does not define the process supervision behavior when one service exits after startup.
13. The PRD requires the health endpoint to be reachable from Next.js, but does not specify whether Phase 1 must include an actual frontend request to `/health` or only CORS/browser reachability.
14. The PRD requires the health endpoint to be reachable from the browser at `localhost:8080/health`, while the backend must bind to `127.0.0.1:8080`. It assumes `localhost` resolves to the bound address.
15. The PRD requires CORS to allow `localhost:3000` only, while the architecture section specifies `http://localhost:3000`. It does not state whether `http://127.0.0.1:3000`, IPv6 localhost, or other local origins must be denied.
16. The CORS policy includes `GET`, `POST`, and `OPTIONS`, although Phase 1 exposes only `GET /health`. It is unclear whether future methods should be enabled now or deferred.
17. The PRD requires shared contracts to be documented, but does not specify the documentation format, location, or minimum content.
18. The PRD says no data flows between the two services yet, but also requires the health endpoint to be reachable from Next.js. The required level of frontend-backend interaction is ambiguous.
19. The PRD says "Any UI beyond the Next.js default page" is out of scope, while the architecture says the frontend should serve a placeholder home page confirming it is running. It is unclear whether replacing the default page is in scope.
20. The PRD says "No premature complexity" and no cloud dependencies, while the architecture lists `google.golang.org/adk`, TanStack Query, placeholder API routes, placeholder component folders, and future model structs in Phase 1. The boundary between foundation and premature setup is unclear.
21. The PRD says Phase 1 has no external integration points, but it requires configuration for LLM providers and installing external dependencies. It is unclear whether "external integration" means runtime integrations only.
22. The PRD specifies exact framework/library versions for several dependencies, but labels some as `latest`. It does not define how version drift should be handled.
23. The PRD names "Next.js 16.2.7 LTS" and "Go 1.26.4"; it is unstated whether these versions must be used exactly or whether equivalent/current compatible versions are acceptable.
24. The PRD requires `slog` structured JSON logging and sample startup logs, but does not specify whether all logs in Phase 1 must be JSON or only startup logs.
25. The PRD says API keys must never be logged, but does not specify whether other configuration values, such as filesystem paths, may be logged.
26. The PRD requires named, actionable config errors, but does not define a complete error message format for every required field.
27. The PRD requires `os.Exit(1)` on config validation failure, but does not specify exit behavior for frontend startup failure or backend port binding failure.
28. The PRD requires `.env` in `.gitignore` and never committed, but does not specify whether other environment file variants such as `.env.local`, `.env.*`, or backend/frontend-specific `.env` files must also be ignored.
29. The PRD mentions `.env.example` in both general terms and under the backend project structure. It is unclear whether `.env.example` belongs at the repo root, under `/backend`, under `/frontend`, or in multiple locations.
30. The PRD does not specify where the runtime `.env` file should live or from which working directory the backend should load it.
31. The PRD says `config.yaml` is committed to version control, and the architecture places it under `/backend/internal/config/config.yaml`. It is unclear whether this path is mandatory.
32. The PRD requires all placeholder files to include one-line comments, but it does not specify whether placeholder files are acceptable for directories that contain no Phase 1 behavior.
33. The PRD requires all model structs in `models/paper.go`, but many model fields relate to future phases. It is unclear whether Phase 1 should include all future model contracts or only contracts needed by `/health`.
34. The PRD says shared contracts are "documented, not assumed," while also requiring Go model structs. It is unclear whether code structs count as documentation.
35. The PRD does not specify the required README content, even though success metrics depend on a developer following the README.
36. The PRD does not specify whether tests are required in Phase 1, and if so, which behaviors require automated test coverage.
37. The PRD does not specify whether `make dev` must work from the repository root only or from subdirectories.
38. The PRD does not specify how ports `3000` and `8080` should behave if already in use.
39. The PRD does not specify whether the backend version `0.1.0` should be hard-coded, loaded from config, or derived from build metadata.
40. The PRD does not specify the expected response headers for `/health`, including `Content-Type`.
41. The PRD does not specify whether non-`GET` requests to `/health` should return `404`, `405`, or another response.
42. The PRD does not specify whether `/health` should require valid config before the server starts, or whether health should be available even when optional config is invalid.
43. The PRD does not define validation rules for optional config fields such as max tokens, temperature, timeouts, base URL, agent settings, arXiv category, or explainer settings.
44. The PRD does not define default values for optional config fields.
45. The PRD does not specify whether `.env` should override optional fields beyond LLM provider/model and Obsidian path.
46. The PRD does not specify whether CORS should be applied to `/health` only or to all backend routes.
47. The PRD does not specify whether backend startup should fail if `config.yaml` is missing or malformed, though this is implied by config validation.
48. The PRD does not specify whether frontend startup depends on backend startup succeeding, or whether both should be independently runnable.

## EARS Requirements

### Functional Requirements

#### Startup

- FR-001: The system shall provide a `make dev` command.
- FR-002: When a developer runs `make dev`, the system shall start the Next.js frontend.
- FR-003: When a developer runs `make dev`, the system shall start the Go backend.
- FR-004: When a developer runs `make dev`, the system shall run the frontend and backend concurrently.
- FR-005: The frontend shall listen on port `3000`.
- FR-006: The Go backend shall listen on `127.0.0.1:8080`.
- FR-007: While running in development mode, the frontend shall support Next.js hot reload.
- FR-008: While running in development mode, the Go backend shall support live reload via `air`.
- FR-009: If the frontend fails during startup, the system shall surface the failure immediately with a clear error message.
- FR-010: If the Go backend fails during startup, the system shall surface the failure immediately with a clear error message.

#### Frontend

- FR-011: The system shall include a Next.js frontend project.
- FR-012: The frontend shall use the Next.js App Router.
- FR-013: The frontend shall include TypeScript support.
- FR-014: The frontend shall include Tailwind CSS support.
- FR-015: The frontend shall include TanStack Query as a dependency.
- FR-016: The frontend shall use Next.js version `16.2.7 LTS` as specified by the PRD.
- FR-017: The frontend shall use TypeScript version `5.x`.
- FR-018: The frontend shall use Tailwind CSS version `4.3.0`.
- FR-019: The frontend shall use TanStack Query version `5.101.0`.
- FR-020: The frontend shall serve a Phase 1 home page that confirms the frontend is running.
- FR-021: The frontend shall have dependency management through `package.json`.
- FR-022: The frontend shall include a root layout area.
- FR-023: The frontend shall include a page area for the home page.
- FR-024: The frontend shall include a components area for future shared UI components.
- FR-025: The frontend shall include a library area for future backend API client code.

#### Backend

- FR-026: The system shall include a Go backend project.
- FR-027: The Go backend shall use Go version `1.26.4` as specified by the PRD.
- FR-028: The Go backend shall have dependency management through `go.mod`.
- FR-029: The Go backend shall use the Go standard library HTTP server.
- FR-030: The Go backend shall include `gopkg.in/yaml.v3` as a dependency for YAML parsing.
- FR-031: The Go backend shall include `github.com/joho/godotenv` as a dependency for `.env` loading.
- FR-032: The Go backend shall include `google.golang.org/adk` as a dependency for future phases.
- FR-033: The Go backend shall include `air` support for live reload.
- FR-034: The Go backend shall initialize structured logging at startup.
- FR-035: When the Go backend starts successfully, the backend shall log that config loaded successfully.
- FR-036: When the Go backend starts successfully, the backend shall log the configured LLM provider.
- FR-037: When the Go backend starts successfully, the backend shall log the configured LLM model.
- FR-038: When the Go backend starts successfully, the backend shall log the listening address.
- FR-039: The Go backend shall not log API keys.

#### Config Loading

- FR-040: When the Go backend starts, the backend shall load configuration from `config.yaml`.
- FR-041: When the Go backend starts, the backend shall load configuration from `.env`.
- FR-042: When a setting exists in both `.env` and `config.yaml`, the backend shall give the `.env` value priority.
- FR-043: When `OBSIDIAN_VAULT_PATH` is set in `.env`, the backend shall use it instead of `paths.obsidian_vault` from `config.yaml`.
- FR-044: When `LLM_API_KEY` is set in `.env`, the backend shall map it to the LLM API key config value.
- FR-045: When `LLM_PROVIDER` is set in `.env`, the backend shall map it to `llm.provider`.
- FR-046: When `LLM_MODEL` is set in `.env`, the backend shall map it to `llm.model`.
- FR-047: The backend shall parse YAML configuration.
- FR-048: The backend shall support `.env` file loading.
- FR-049: The backend config shape shall include an LLM section.
- FR-050: The backend LLM config shape shall include provider, model, API key, max tokens, temperature, timeout seconds, and base URL values.
- FR-051: The backend config shape shall include an agent section.
- FR-052: The backend agent config shape shall include max review iterations and paper fetch limit values.
- FR-053: The backend config shape shall include an arXiv section.
- FR-054: The backend arXiv config shape shall include a category value.
- FR-055: The backend config shape shall include a paths section.
- FR-056: The backend paths config shape shall include Obsidian vault and log file values.
- FR-057: The backend config shape shall include an explainer section.
- FR-058: The backend explainer config shape shall include target words and follow-up arXiv link values.

#### Config Validation

- FR-059: When the Go backend starts, the backend shall validate required configuration before starting the HTTP server.
- FR-060: If `LLM_API_KEY` is missing, the backend shall fail startup.
- FR-061: If `LLM_API_KEY` is missing, the backend shall report a named, actionable config error.
- FR-062: If `llm.provider` is missing, the backend shall fail startup.
- FR-063: If `llm.provider` is missing, the backend shall report a named, actionable config error.
- FR-064: If `llm.provider` is not one of `anthropic`, `openai`, or `gemini`, the backend shall fail startup.
- FR-065: If `llm.provider` is not one of `anthropic`, `openai`, or `gemini`, the backend shall report a named, actionable config error.
- FR-066: If `llm.model` is missing, the backend shall fail startup.
- FR-067: If `llm.model` is missing, the backend shall report a named, actionable config error.
- FR-068: If `paths.obsidian_vault` is missing, the backend shall fail startup.
- FR-069: If `paths.obsidian_vault` is missing, the backend shall report a named, actionable config error.
- FR-070: If `paths.log_file` is missing, the backend shall fail startup.
- FR-071: If `paths.log_file` is missing, the backend shall report a named, actionable config error.
- FR-072: If required configuration is invalid, the backend shall exit with status code `1`.

#### Health Endpoint

- FR-073: The Go backend shall expose `GET /health`.
- FR-074: When a client sends `GET /health`, the backend shall return HTTP status `200`.
- FR-075: When a client sends `GET /health`, the backend shall return a JSON body containing `status` with value `ok`.
- FR-076: When a client sends `GET /health`, the backend shall return a JSON body containing `version` with value `0.1.0`.
- FR-077: The health endpoint shall be reachable from a browser at `localhost:8080/health`.
- FR-078: The health endpoint shall be reachable from the Next.js frontend origin allowed by CORS.

#### CORS And Binding

- FR-079: The Go backend shall bind to `127.0.0.1:8080`.
- FR-080: The Go backend shall not bind to `0.0.0.0`.
- FR-081: The backend shall configure CORS to allow origin `http://localhost:3000`.
- FR-082: The backend shall configure CORS to allow methods `GET`, `POST`, and `OPTIONS`.
- FR-083: The backend shall configure CORS to allow the `Content-Type` header.
- FR-084: The backend shall not allow CORS origins other than the Phase 1 frontend origin specified by the PRD.

#### Repository Structure

- FR-085: The repository shall clearly separate frontend code from backend code.
- FR-086: The repository shall include a `/frontend` area for the Next.js frontend.
- FR-087: The repository shall include a `/backend` area for the Go backend.
- FR-088: The backend shall include an entry point for loading config, wiring the server, and starting the process.
- FR-089: The backend shall include a config package or area for config structures, loading, and validation.
- FR-090: The backend shall include a server package or area for HTTP server setup, route registration, CORS, and the health handler.
- FR-091: The backend shall include model definitions shared across future phases as described by the PRD.
- FR-092: The repository shall document shared API response shapes.
- FR-093: The repository shall include placeholder structure for future frontend API routes described by the PRD.
- FR-094: The repository shall include placeholder structure for a future frontend trigger API route.
- FR-095: The repository shall include placeholder structure for a future frontend select API route.
- FR-096: The repository shall include placeholder structure for a future frontend status API route.
- FR-097: The repository shall include placeholder structure for a future frontend result API route.
- FR-098: The repository shall include placeholder structure for future backend orchestrator, tools, agents, LLM, and model areas described by the PRD.
- FR-099: Where the repository includes placeholder files for future phases, each placeholder file shall include a one-line comment explaining what belongs there.

#### Environment Files And Git Hygiene

- FR-100: The repository shall include a committed `.env.example`.
- FR-101: The `.env.example` file shall contain placeholder values.
- FR-102: The `.env.example` file shall include inline documentation for required keys.
- FR-103: The repository shall ignore `.env` through `.gitignore`.
- FR-104: The repository shall not track `.env`.
- FR-105: The repository shall not commit API keys.
- FR-106: The repository shall not commit session databases.
- FR-107: The repository shall not commit Chroma data.
- FR-108: The repository shall not commit logs.
- FR-109: The repository shall not commit exported reports.

#### Models And Contracts

- FR-110: The backend shall define a `Paper` model with the fields specified in the PRD.
- FR-111: The backend shall define an `ExplainerOutput` model with the fields specified in the PRD.
- FR-112: The backend shall define a `ReviewVerdict` model with the fields specified in the PRD.
- FR-113: The backend shall define a `PipelineStage` type with the stage values specified in the PRD.
- FR-114: The backend shall define a `PipelineSession` model with the fields specified in the PRD.

#### Documentation

- FR-115: The repository shall include README instructions sufficient for a fresh developer to create required environment configuration and start the local environment.
- FR-116: The repository shall document the health endpoint response shape.
- FR-117: The repository shall document required configuration keys and their purpose.

### Non-Functional Requirements

- NFR-001: The system shall be reproducible on supported macOS machines.
- NFR-002: The system shall be reproducible on supported Linux machines.
- NFR-003: When a fresh developer follows the README and provides required `.env` setup, the environment shall start in under 10 minutes.
- NFR-004: When a developer runs `make dev` on a modern laptop after setup, both services shall start in under 10 seconds.
- NFR-005: The system shall fail fast on required config errors during backend startup.
- NFR-006: The system shall not defer required config errors until mid-run.
- NFR-007: The system shall avoid database dependencies in Phase 1.
- NFR-008: The system shall avoid authentication features in Phase 1.
- NFR-009: The system shall avoid cloud runtime dependencies in Phase 1.
- NFR-010: The system shall avoid agent logic in Phase 1.
- NFR-011: The system shall avoid arXiv API integration in Phase 1.
- NFR-012: The system shall avoid Obsidian vault interaction in Phase 1.
- NFR-013: The system shall avoid business logic in Phase 1.
- NFR-014: The backend shall keep API keys in memory only as required for config and shall not expose them in logs or responses.
- NFR-015: The backend shall be reachable only from the local machine through its configured bind address.
- NFR-016: `GET /health` shall return `200` reliably while the backend is running.
- NFR-017: Config validation errors shall be actionable enough to identify the missing or invalid field and the required corrective action.
- NFR-018: The system shall keep Phase 1 changes limited to scaffolding, configuration, health checking, security baseline, and documentation.

## Explicit Phase Scope

### In Scope

- Repository scaffolding for separated frontend and backend services.
- Next.js frontend initialization.
- Go backend initialization.
- Dependency management for each service.
- `make dev` for concurrent local startup.
- Development live reload for both services.
- YAML plus `.env` config loading for the Go backend.
- Required config validation at Go backend startup.
- Named, actionable config validation errors.
- Local-only backend binding.
- CORS baseline for the frontend origin.
- `GET /health` returning the Phase 1 health response.
- `.env.example` with placeholder values and inline documentation.
- `.gitignore` protection for `.env`.
- Documentation of config setup and the health endpoint.
- Shared contract documentation for Phase 1 response shapes.
- PRD-specified placeholder project structure and model contracts, subject to the ambiguities listed above.

### Out Of Scope

- End-user product UI beyond the Phase 1 frontend/default placeholder.
- Agent behavior or orchestration logic.
- Tool implementations.
- LLM API calls.
- arXiv API calls.
- PDF fetching.
- Obsidian vault reads or writes.
- Processed-paper log behavior beyond config path definition.
- Paper discovery.
- Paper selection.
- Explainer generation.
- Review loops.
- Status polling.
- Result retrieval.
- Authentication.
- Database setup.
- Cloud deployment.
- Production hosting.
- Business logic.
- Resolving the ambiguities in this requirements document without user review.

## Ambiguity Resolutions

1. Treat the PRD's "Open Questions: None" statement as incorrect. These items are review decisions before implementation.
2. `.env` shall override `config.yaml` for `LLM_PROVIDER`, `LLM_MODEL`, and `OBSIDIAN_VAULT_PATH`; `LLM_API_KEY` shall exist only in `.env`.
3. Phase 1 shall support only these `.env` keys: `LLM_API_KEY`, `LLM_PROVIDER`, `LLM_MODEL`, and `OBSIDIAN_VAULT_PATH`. Broad environment-variable mapping is out of scope.
4. `llm.model` shall be validated as non-empty only. Provider-specific model validation is out of scope for Phase 1.
5. `paths.obsidian_vault` shall be validated as an absolute path that exists and is a directory. Write access validation is out of scope for Phase 1.
6. `paths.log_file` shall be validated as an absolute path whose parent directory exists. The log file itself does not need to exist before startup.
7. Phase 1 shall support macOS and Linux on `amd64` and `arm64`, with `make`, `go`, and `npm` installed. Windows and WSL are out of scope.
8. `make dev` shall not install dependencies. Dependency setup shall be documented as a prerequisite.
9. The "one command" startup expectation applies after clone, dependency installation, and `.env` setup are complete.
10. The 10-second startup metric excludes first-time dependency installation, Go module download, `air` installation, and initial Next.js compilation.
11. `air` shall be available through documented setup, preferably via a project-local or Go-installed invocation rather than an undocumented global assumption.
12. If either service exits under `make dev`, the dev runner shall stop the other service and return a non-zero exit status.
13. Phase 1 only requires CORS and browser reachability for the health endpoint. No frontend health-check UI is required.
14. The backend shall bind to `127.0.0.1`; documentation may use `localhost` for browser access.
15. CORS shall allow only `http://localhost:3000`. `http://127.0.0.1:3000`, IPv6 localhost, and other origins shall be denied unless added in a later phase.
16. CORS shall allow `GET`, `POST`, and `OPTIONS` in Phase 1 because the PRD explicitly lists them.
17. Shared API contracts shall be documented in `docs/contracts/api.md`, including the `/health` response shape.
18. There shall be no active frontend-to-backend data flow in Phase 1. "Reachable from Next.js" means permitted by CORS and manually fetchable.
19. The frontend shall keep the default Next.js page or replace it only with a minimal "frontend running" placeholder. Product UI is out of scope.
20. Phase 1 shall use minimal scaffolding. Future-phase folders/files may exist only where explicitly required by the PRD or exit criteria.
21. "No external integration points" means no runtime external integrations. Installing dependencies and configuring provider names is allowed.
22. Exact dependency versions shall be pinned where the PRD specifies them. Dependencies listed as `latest` shall be resolved once during implementation and locked.
23. PRD-specified versions shall be treated as exact target versions. If a version is unavailable, implementation shall flag that instead of silently substituting.
24. Backend logs shall use JSON `slog` output in Phase 1.
25. Startup logs may include provider, model, and listen address. API keys shall never be logged; filesystem paths should not be logged unless necessary.
26. Config validation errors shall use this general format: `FATAL config error: <field> <problem>.` followed by a corrective hint line.
27. Backend config and bind failures shall exit non-zero. Frontend startup failures shall cause the dev runner to return non-zero.
28. Git ignore rules shall ignore `.env`, `.env.*`, service-specific env files, and keep example env files trackable with exceptions such as `!.env.example`.
29. `.env.example` shall live at the repository root.
30. Runtime `.env` shall live at the repository root, and backend development startup shall load it from that location.
31. `config.yaml` shall live at `/backend/internal/config/config.yaml` unless the PRD is changed.
32. Placeholder files are acceptable only for PRD-named future folders, and each shall contain a minimal intent comment.
33. Phase 1 shall include all PRD-required model structs, but no behavior around those future-phase models.
34. Code structs do not count as API contract documentation. `/health` shall be documented separately.
35. README content shall cover prerequisites, `.env` setup, dependency installation, `make dev`, and `/health` verification.
36. Phase 1 shall include focused backend tests for config validation and the health handler. Frontend tests are not required unless frontend test tooling already exists.
37. `make dev` only needs to work from the repository root.
38. If ports `3000` or `8080` are already in use, startup shall fail clearly. The system shall not auto-select alternate ports.
39. Backend version `0.1.0` shall be a package constant for Phase 1.
40. `/health` shall return `Content-Type: application/json`.
41. Non-`GET` requests to `/health` shall return `405 Method Not Allowed`.
42. The backend shall validate required config before serving `/health`; invalid required config shall prevent server startup.
43. Optional config fields shall be validated only when present and obviously invalid, such as negative token counts or non-positive timeouts.
44. Optional config defaults shall live in `config.yaml` where practical.
45. Optional `.env` overrides beyond `LLM_PROVIDER`, `LLM_MODEL`, and `OBSIDIAN_VAULT_PATH` are out of scope for Phase 1.
46. CORS middleware shall apply to all backend routes.
47. Backend startup shall fail if `config.yaml` is missing or malformed.
48. Frontend and backend shall be independently runnable, while `make dev` shall fail if either service cannot start.
