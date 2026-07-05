package orchestrator

import (
	"errors"
	"os"
	"syscall"

	"github.com/maritime-ds/arxiv-reader/internal/llm"
	"github.com/maritime-ds/arxiv-reader/internal/tools"
)

// This file centralizes the mapping from pipeline errors to user-facing messages
// and recoverability, keeping the goroutine logic in orchestrator-pipeline.go
// free of long switch statements.
//
// Each describe* function also returns a machine-readable action hint the
// frontend uses to label the failure affordance. Values (Phase 6):
//
//	"retry"           — transient; a retry might succeed (the common case)
//	"fix_config"      — bad model / request too large; user must change config
//	"fix_permissions" — vault permission/disk problem; user must fix the vault
//	""                — non-recoverable with no self-service action (e.g. a
//	                    corrupt processed-log needing manual inspection)
const (
	actionRetry          = "retry"
	actionFixConfig      = "fix_config"
	actionFixPermissions = "fix_permissions"
)

// describeError maps a discovery/extraction error to a human-readable message,
// whether a retry might help, and the UI action hint. Shared by both background
// goroutines, so the default message is stage-neutral. Corrupt-log is the only
// non-recoverable failure here — it needs a manual fix (no action).
func describeError(err error) (message string, recoverable bool, action string) {
	switch {
	case errors.Is(err, tools.ErrArxivRateLimit):
		return "arXiv is rate limiting requests. Please try again in a minute.", true, actionRetry
	case errors.Is(err, tools.ErrArxivUnavailable):
		return "arXiv is currently unavailable. Please try again.", true, actionRetry
	case errors.Is(err, tools.ErrArxivParse):
		return "arXiv returned an unexpected response. Please try again.", true, actionRetry
	case errors.Is(err, tools.ErrLogCorrupted):
		return "The processed-log file is corrupted and needs manual inspection.", false, ""
	case errors.Is(err, tools.ErrPaperHTMLTimeout):
		return "Fetching the paper's HTML timed out. Please try again.", true, actionRetry
	case errors.Is(err, tools.ErrPaperHTMLFailed):
		return "Could not fetch or convert the paper's HTML. Please try again.", true, actionRetry
	default:
		return "The request failed unexpectedly. Please try again.", true, actionRetry
	}
}

// describeGenErr maps an LLM generation error to a user-facing message and action
// hint. Transient failures are recoverable (the paper is untouched — no vault
// file, log not updated — so a retry is safe). A bad-request (model wrong / paper
// too large) is the exception: config is immutable at runtime, so the caller
// marks it NON-recoverable and the fix_config action tells the user what to change.
func describeGenErr(err error) (message string, action string) {
	switch {
	case errors.Is(err, llm.ErrLLMRateLimit):
		return "The AI provider is rate limiting requests. Please try again in a minute.", actionRetry
	case errors.Is(err, llm.ErrLLMBadRequest):
		return "The AI request was rejected — the paper may be too large for this model. Switch to a larger-context model in config.yaml.", actionFixConfig
	case errors.Is(err, llm.ErrLLMTimeout):
		return "Generating the explainer timed out. Please try again.", actionRetry
	case errors.Is(err, llm.ErrLLMUnavailable):
		return "The AI provider is currently unavailable. Please try again.", actionRetry
	default:
		return "Generating the explainer failed unexpectedly. Please try again.", actionRetry
	}
}

// describeReviewErr maps a reviewer LLM error to a user-facing message and action
// hint. Like generation, transient reviewer failures are recoverable (no vault
// file written, log untouched); a bad-request is non-recoverable (fix_config).
// (A malformed-JSON parse error is handled separately in the loop — it stops the
// loop rather than failing.)
func describeReviewErr(err error) (message string, action string) {
	switch {
	case errors.Is(err, llm.ErrLLMRateLimit):
		return "The AI provider is rate limiting requests. Please try again in a minute.", actionRetry
	case errors.Is(err, llm.ErrLLMBadRequest):
		return "The AI review request was rejected. Try a different model in config.yaml.", actionFixConfig
	case errors.Is(err, llm.ErrLLMTimeout):
		return "Reviewing the explainer timed out. Please try again.", actionRetry
	case errors.Is(err, llm.ErrLLMUnavailable):
		return "The AI provider is currently unavailable. Please try again.", actionRetry
	default:
		return "Reviewing the explainer failed unexpectedly. Please try again.", actionRetry
	}
}

// vaultErrMsg is the user-facing message and action hint for a vault-write
// failure. Permission/disk failures need a manual fix (fix_permissions); others
// are transient and worth a retry.
func vaultErrMsg(err error) (message string, action string) {
	if !vaultRecoverable(err) {
		return "Could not save the note (permission denied or disk full). Check the vault path and free space.", actionFixPermissions
	}
	return "Could not save the note to the vault. Please try again.", actionRetry
}

// vaultRecoverable reports whether retrying the vault write might succeed.
// Errors that need manual intervention are non-recoverable: permission denied,
// disk full (ENOSPC), read-only filesystem (EROFS), and quota exceeded (EDQUOT).
// Everything else defaults to recoverable.
func vaultRecoverable(err error) bool {
	switch {
	case errors.Is(err, os.ErrPermission),
		errors.Is(err, syscall.ENOSPC),
		errors.Is(err, syscall.EROFS),
		errors.Is(err, syscall.EDQUOT):
		return false
	default:
		return true
	}
}
