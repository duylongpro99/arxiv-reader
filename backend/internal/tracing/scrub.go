package tracing

import (
	"fmt"
	"regexp"
	"strings"
)

// previewCap bounds any single string stored/streamed in an event. Summaries are
// meant to be short previews; this is a defence-in-depth cap in case a caller
// forgets to truncate (e.g. a stray full-markdown string).
const previewCap = 500

const redacted = "[REDACTED]"

// secretPatterns match common credential shapes. Redaction is deliberately
// aggressive: a false positive costs a little readability; a false negative
// leaks a secret into durable storage. Ordered from most- to least-specific.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{16,}`),        // Anthropic
	regexp.MustCompile(`sk-[A-Za-z0-9_\-]{16,}`),            // OpenAI-style
	regexp.MustCompile(`AIza[A-Za-z0-9_\-]{20,}`),           // Google API keys
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]{16,}`), // Authorization: Bearer …
	regexp.MustCompile(`(?i)(api[_-]?key|token|secret)\s*[:=]\s*\S+`), // key: value forms
}

// scrubber redacts secrets from event summaries/payloads before they are
// buffered, streamed, or persisted. It knows the exact config secrets (the LLM
// API key) plus the generic patterns above.
type scrubber struct {
	literals []string // exact secrets to redact verbatim (e.g. the API key)
}

// newScrubber builds a scrubber that redacts the given literal secrets (empties
// ignored) in addition to the regex patterns.
func newScrubber(secrets ...string) *scrubber {
	var lits []string
	for _, s := range secrets {
		if strings.TrimSpace(s) != "" {
			lits = append(lits, s)
		}
	}
	return &scrubber{literals: lits}
}

// scrubMap deep-copies and scrubs a summary/payload map. Returns nil for an
// empty input so it round-trips as SQL NULL. The copy is important: callers pass
// maps built from live pipeline data, and we must not mutate their originals.
func (s *scrubber) scrubMap(m map[string]any) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = s.scrubValue(v)
	}
	return out
}

// scrubValue recurses through the common shapes an orchestrator summary contains
// (strings, nested maps, slices). Non-string scalars (numbers, bools) pass
// through untouched. Anything unrecognised is stringified and scrubbed, so no
// value can bypass redaction.
func (s *scrubber) scrubValue(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		return s.scrubString(t)
	case map[string]any:
		return s.scrubMap(t)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = s.scrubValue(e)
		}
		return out
	case []string:
		out := make([]string, len(t))
		for i, e := range t {
			out[i] = s.scrubString(e)
		}
		return out
	case bool, int, int32, int64, float32, float64:
		return v // safe scalars — no secret can hide here
	default:
		return s.scrubString(fmt.Sprintf("%v", v))
	}
}

// scrubString redacts literals and pattern matches, THEN caps length. Order
// matters: truncating first could split a secret so its pattern no longer
// matches, leaking the head of a key. Redact fully, then cap.
func (s *scrubber) scrubString(str string) string {
	for _, lit := range s.literals {
		str = strings.ReplaceAll(str, lit, redacted)
	}
	for _, re := range secretPatterns {
		str = re.ReplaceAllString(str, redacted)
	}
	return truncate(str, previewCap)
}

// truncate caps a string to n runes, appending a byte-count note so a reader
// knows content was elided. Rune-aware so we never split a multibyte character.
func truncate(str string, n int) string {
	r := []rune(str)
	if len(r) <= n {
		return str
	}
	return string(r[:n]) + fmt.Sprintf("… (+%d chars)", len(r)-n)
}
