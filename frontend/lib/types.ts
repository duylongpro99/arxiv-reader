// Shared frontend types. Field names are camelCase and MUST match the Go DTOs
// (backend/internal/orchestrator, backend/internal/models). The Go structs use
// explicit json tags (e.g. `pdfUrl`) to guarantee this contract.

export interface Paper {
  id: string;
  title: string;
  authors: string[];
  abstract: string;
  pdfUrl: string;
  published: string; // ISO-8601 date string
}

// Only the stages Phase 2 reaches are exercised; the rest are declared so the
// contract is stable as later phases light them up.
export type PipelineStage =
  | "discovery"
  | "selection"
  | "extracting" // Phase 3: fetching + converting the paper HTML → Markdown
  | "failed"
  | "generating"
  | "reviewing"
  | "revising"
  | "writing"
  | "complete";

export interface PipelineStatus {
  stage: PipelineStage;
  candidates?: Paper[];
  notice?: string;
  error?: string;
  recoverable?: boolean;
  // Phase 5 review-loop progress. Omitted by the backend before review starts
  // (see the Go StatusResponse `omitempty` tags).
  iteration?: number;
  reviewScore?: number;
  reviewPassed?: boolean;
  // Phase 6 additive fields (match the Go StatusResponse json tags exactly).
  // errorAction is a machine hint for the failure ("retry" | "fix_config" |
  // "fix_permissions"); arxivRetryCount drives the discovery retry label;
  // contextWarning is the non-blocking over-limit advisory.
  errorAction?: string;
  arxivRetryCount?: number;
  contextWarning?: ContextWarning;
}

// ContextWarning mirrors the Go models.ContextWarning json tags. Advisory only —
// the pipeline proceeds; the UI shows it as a non-blocking notice.
export interface ContextWarning {
  estimatedTokens: number;
  modelLimit: number;
  model: string;
  suggestion: string;
}

export interface TriggerResponse {
  session_id: string;
}

// ResourceField describes one dynamic request input. Mirrors the Go
// resource.Field json tags exactly (name/type/label/required/default/options).
// The engine is self-describing: the form is built from these, not hardcoded.
export interface ResourceField {
  name: string;
  type: "select" | "text";
  label: string;
  required: boolean;
  default?: string; // omitempty on the backend
  options?: { value: string; label: string }[]; // present for select fields
}

// ResourceDescriptor is one resource served by GET /resources (Go
// resource.Descriptor json tags): its identity plus the field schema the UI
// renders. Adding a resource is a backend YAML file — zero frontend changes.
export interface ResourceDescriptor {
  id: string;
  label: string;
  description: string;
  fields: ResourceField[];
}

// Selecting a paper returns the same session id (the panel keeps polling it).
export interface SelectResponse {
  session_id: string;
}

// The finished explainer, served by /result once the pipeline is complete.
// Field names match the Go ResultResponse DTO (camelCase json tags).
export interface ResultResponse {
  content: string; // note body Markdown (no frontmatter)
  vaultFile: string; // absolute path of the written note
  tokensUsed: number;
  // Phase 6 cost breakdown (match the Go ResultResponse json tags). Present only
  // when known: costKnown is false/absent when the model isn't in the pricing
  // table, in which case the UI hides the cost figure.
  inputTokens?: number;
  outputTokens?: number;
  estimatedCostUSD?: number;
  costKnown?: boolean;
}

// Retrying a failed pipeline resumes the SAME session (backend routes by the
// failed stage); the id is echoed back so the panel keeps polling it.
export interface RetryResponse {
  session_id: string;
}

// --- Phase 7 run-timeline types (mirror the Go DTOs in orchestrator/dto.go) ---

// Drives a timeline row's icon + color.
export type EventStatus = "info" | "success" | "warning" | "error";

// A run's durable lifecycle status.
export type RunStatus = "running" | "complete" | "failed" | "recovered";

// LLMPayload is the full prompt/response trio the backend attaches to
// explainer/reviewer events when `full_payloads` tracing is enabled. Absent
// (undefined) on lighter-weight events or when tracing is off.
export interface LLMPayload {
  systemPrompt?: string;
  userPrompt?: string;
  response?: string;
}

// DecisionSummary is the shape of `summary` on `decision.*` events. It is a
// subset of the free-form `summary` map below — narrowed here so components
// can read known fields without casting. Legacy keys (finalScore,
// maxIterations, flagged) may still appear on older runs; callers should
// ignore/relabel them rather than rely on their presence.
export interface DecisionSummary {
  decision?: string;
  onPass?: number;
  flaggedSections?: string[];
  narrative?: string;
}

// TimelineEvent is one ordered event. `type` is the event kind (e.g.
// "selection.chosen"); `summary`/`payloadFull` are arbitrary structured objects
// (already scrubbed server-side). Matches the Go EventDTO json tags.
// `summary` stays a permissive free map (it's still an arbitrary object
// server-side) but is widened to admit the known `DecisionSummary` fields so
// consumers don't need to cast when reading `narrative`/`flaggedSections`.
export interface TimelineEvent {
  seq: number;
  type: string;
  stage: string;
  title: string;
  status: EventStatus;
  summary?: Record<string, unknown> & DecisionSummary;
  payloadFull?: LLMPayload;
  durationMs?: number;
  createdAt: string; // ISO-8601
}

// RunSummary is a run's header for the history list + reopen views (Go RunDTO).
export interface RunSummary {
  id: string;
  paperId?: string;
  paperTitle?: string;
  stage: string;
  status: RunStatus;
  inputTokens: number;
  outputTokens: number;
  estCostUsd?: number;
  reviewPassed?: boolean;
  startedAt: string; // ISO-8601
  completedAt?: string; // ISO-8601
}

// Go RunsListResponse.
export interface RunsList {
  runs: RunSummary[];
  total: number;
}

// Go RunDetailResponse — a reopened run's header + full timeline.
export interface RunDetail {
  run: RunSummary;
  events: TimelineEvent[];
}

// --- Phase 4 history-content + load-more types (mirror the Go DTOs in phase-02) ---

// RunContent is the response from GET /runs/{id}/content — the persisted note's
// markdown body. `available:false` means the vault file could not be read
// (moved or deleted) even though the HTTP call itself succeeds (200); `path` may
// still be present so the UI can show where it looked.
export interface RunContent {
  path?: string;
  available: boolean;
  // Optional to match the backend's `omitempty`: an empty saved note omits the
  // field entirely, so it is undefined at runtime even when available:true.
  markdown?: string;
}

// DiscoverMoreResult is the response from POST /discover/{sessionId}/more — the
// next page of candidate papers plus whether further pages may exist.
export interface DiscoverMoreResult {
  candidates: Paper[];
  hasMore: boolean;
}

// --- Phase 8 channel-publishing types (mirror the Go DTOs in
// backend/internal/orchestrator/dto.go) ---

// Category groups a channel's content shape: a long-form article (dev.to), a
// short digest, or a thread-chunked brief (X). Drives which editor/preview the
// draft card renders.
export type Category = "longform" | "digest" | "brief";

// PublicationStatus is a draft's lifecycle: generated (draft) -> reviewed
// (approved) -> claimed for the irreversible post (publishing, transient) ->
// sent (published), or failed on the publish attempt. "publishing" is the
// backend's atomic double-publish guard; it is normally momentary but can be
// observed by a racing read, so the UI must render it.
export type PublicationStatus =
  | "draft"
  | "approved"
  | "publishing"
  | "published"
  | "failed";

// Channel is one enabled, resolvable publish destination (Go ChannelDTO).
export interface Channel {
  id: string;
  category: Category;
}

// Go ChannelsResponse.
export interface ChannelsResponse {
  channels: Channel[];
}

// CreatePublicationsRequest is the body of POST /runs/{id}/publications.
export interface CreatePublicationsRequest {
  channels: string[];
}

// PatchPublicationRequest is the body of PATCH /publications/{pid}. All
// fields are optional — a partial edit (e.g. only `approve`) leaves the rest
// untouched, matching the Go handler's pointer-field semantics.
export interface PatchPublicationRequest {
  title?: string;
  content?: string;
  approve?: boolean;
}

// Publication is one (run, channel) publish draft/attempt (Go PublicationDTO).
// Optional fields are omitted by the backend (`omitempty`) rather than sent
// as null/empty, so they are undefined at runtime until set.
export interface Publication {
  id: string;
  runId: string;
  channelId: string;
  category: Category;
  status: PublicationStatus;
  title?: string;
  content?: string;
  externalUrl?: string;
  externalId?: string;
  error?: string;
  createdAt: string; // ISO-8601
  publishedAt?: string; // ISO-8601
}

// Go PublicationsResponse — GET/POST /runs/{id}/publications. skippedChannels
// lists channel ids from a create-request that failed to resolve, paired with
// omitempty semantics (undefined when nothing was skipped).
export interface PublicationsResponse {
  publications: Publication[];
  skippedChannels?: string[];
}
