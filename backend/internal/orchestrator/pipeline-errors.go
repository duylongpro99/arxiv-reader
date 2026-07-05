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

// describeError maps a discovery/extraction error to a human-readable message
// and whether a retry might help. Shared by both background goroutines, so the
// default message is stage-neutral. Corrupt-log is the only non-recoverable
// failure here — it needs a manual fix.
func describeError(err error) (message string, recoverable bool) {
	switch {
	case errors.Is(err, tools.ErrArxivRateLimit):
		return "arXiv is rate limiting requests. Please try again in a minute.", true
	case errors.Is(err, tools.ErrArxivUnavailable):
		return "arXiv is currently unavailable. Please try again.", true
	case errors.Is(err, tools.ErrArxivParse):
		return "arXiv returned an unexpected response. Please try again.", true
	case errors.Is(err, tools.ErrLogCorrupted):
		return "The processed-log file is corrupted and needs manual inspection.", false
	case errors.Is(err, tools.ErrPaperHTMLTimeout):
		return "Fetching the paper's HTML timed out. Please try again.", true
	case errors.Is(err, tools.ErrPaperHTMLFailed):
		return "Could not fetch or convert the paper's HTML. Please try again.", true
	default:
		return "The request failed unexpectedly. Please try again.", true
	}
}

// describeGenErr maps an LLM generation error to a user-facing message. All
// generation failures are treated as recoverable (the paper is untouched — no
// vault file, log not updated — so a retry is always safe).
func describeGenErr(err error) string {
	switch {
	case errors.Is(err, llm.ErrLLMRateLimit):
		return "The AI provider is rate limiting requests. Please try again in a minute."
	case errors.Is(err, llm.ErrLLMBadRequest):
		return "The AI request was rejected (the paper may be too large). Please try again."
	case errors.Is(err, llm.ErrLLMTimeout):
		return "Generating the explainer timed out. Please try again."
	case errors.Is(err, llm.ErrLLMUnavailable):
		return "The AI provider is currently unavailable. Please try again."
	default:
		return "Generating the explainer failed unexpectedly. Please try again."
	}
}

// vaultErrMsg is the user-facing message for a vault-write failure.
func vaultErrMsg(err error) string {
	if !vaultRecoverable(err) {
		return "Could not save the note (permission denied or disk full). Check the vault path and free space."
	}
	return "Could not save the note to the vault. Please try again."
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
