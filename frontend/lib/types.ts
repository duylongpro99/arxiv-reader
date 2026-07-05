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
