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
}
