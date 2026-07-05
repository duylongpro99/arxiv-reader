// Package audit holds cross-cutting, source-level invariants that are easier to
// assert against the whole codebase than within any one package.
package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// forbiddenInLogs are identifiers that must NEVER appear as an argument to a
// slog call — logging any of them would leak the LLM API key or a raw auth
// header (F1 / PRD §7 / CLAUDE.md: logs carry extracted text + metadata only).
var forbiddenInLogs = []string{"APIKey", "LLM_API_KEY", "Authorization"}

// TestNoSecretsInLogCalls walks every non-test .go file in the backend and, for
// each slog.* call, captures the call's argument text (from "slog." to its
// balanced closing paren) and asserts none of the forbidden identifiers appear
// inside. Comments outside the call are excluded because the scan starts at the
// slog. token, so a "// NEVER log APIKey" comment does not trip the check.
func TestNoSecretsInLogCalls(t *testing.T) {
	root := filepath.Join("..", "..") // internal/audit → backend module root

	scanned := 0
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, call := range extractSlogCalls(string(src)) {
			scanned++
			for _, bad := range forbiddenInLogs {
				if strings.Contains(call, bad) {
					t.Errorf("%s: slog call may leak a secret (contains %q):\n\t%s", path, bad, call)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking source tree: %v", err)
	}
	// Guard against a silently-broken scanner reporting a false pass: the codebase
	// is known to have dozens of slog calls, so zero means the walk/parse failed.
	if scanned == 0 {
		t.Fatal("scanned 0 slog calls — the source scan is broken, not clean")
	}
}

// extractSlogCalls returns the source text of every slog.* call in src, spanning
// from the "slog." token through its balanced closing parenthesis (handles the
// multiline calls used throughout the codebase).
func extractSlogCalls(src string) []string {
	var calls []string
	const marker = "slog."
	for i := 0; i < len(src); {
		idx := strings.Index(src[i:], marker)
		if idx < 0 {
			break
		}
		start := i + idx
		// Advance to the first '(' after the slog.Method token.
		open := strings.IndexByte(src[start:], '(')
		if open < 0 {
			break
		}
		open += start
		// Walk forward tracking paren depth to find the matching close.
		depth := 0
		end := -1
		for j := open; j < len(src); j++ {
			switch src[j] {
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 {
					end = j
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			break // unbalanced (shouldn't happen in compiling source)
		}
		calls = append(calls, src[start:end+1])
		i = end + 1
	}
	return calls
}
