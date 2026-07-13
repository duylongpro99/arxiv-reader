package tracing

import (
	"fmt"
	"regexp"
	"strings"
)

// previewCap bounds any single string in an event Summary. Summaries are meant
// to be short previews; this is a defence-in-depth cap in case a caller forgets
// to truncate (e.g. a stray full-markdown string).
const previewCap = 500

// payloadCap bounds any single string in an event's opt-in PayloadFull. Full
// payloads intentionally carry whole prompts/responses (the reasoning trace),
// so the cap is generous — high enough never to truncate real prompts/drafts,
// but still bounded as a backstop against a pathological multi-megabyte value.
const payloadCap = 100_000

const redacted = "[REDACTED]"

// secretPatterns match common credential shapes. Redaction is deliberately
// aggressive: a false positive costs a little readability; a false negative
// leaks a secret into durable storage. Ordered from most- to least-specific.
var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{16,}`),                  // Anthropic
	regexp.MustCompile(`sk-[A-Za-z0-9_\-]{16,}`),                      // OpenAI-style
	regexp.MustCompile(`AIza[A-Za-z0-9_\-]{20,}`),                     // Google API keys
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-]{16,}`),           // Authorization: Bearer …
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

// scrubMap deep-copies and scrubs a summary map with the short preview cap.
// Returns nil for an empty input so it round-trips as SQL NULL.
func (s *scrubber) scrubMap(m map[string]any) map[string]any {
	return s.scrubMapCap(m, previewCap)
}

// scrubMapFull scrubs a PayloadFull map with the generous payload cap, so full
// prompts/responses survive intact while still being secret-redacted. Same
// redaction pass as scrubMap — only the length backstop differs.
func (s *scrubber) scrubMapFull(m map[string]any) map[string]any {
	return s.scrubMapCap(m, payloadCap)
}

// scrubMapCap is the shared implementation. The copy is important: callers pass
// maps built from live pipeline data, and we must not mutate their originals.
func (s *scrubber) scrubMapCap(m map[string]any, limit int) map[string]any {
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = s.scrubValue(v, limit)
	}
	return out
}

// scrubValue recurses through the common shapes an orchestrator summary contains
// (strings, nested maps, slices). Non-string scalars (numbers, bools) pass
// through untouched. Anything unrecognised is stringified and scrubbed, so no
// value can bypass redaction.
func (s *scrubber) scrubValue(v any, limit int) any {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		return s.scrubStringCap(t, limit)
	case map[string]any:
		return s.scrubMapCap(t, limit)
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = s.scrubValue(e, limit)
		}
		return out
	case []string:
		out := make([]string, len(t))
		for i, e := range t {
			out[i] = s.scrubStringCap(e, limit)
		}
		return out
	case bool, int, int32, int64, float32, float64:
		return v // safe scalars — no secret can hide here
	default:
		return s.scrubStringCap(fmt.Sprintf("%v", v), limit)
	}
}

// scrubString redacts literals and pattern matches, THEN caps length. Order
// matters: truncating first could split a secret so its pattern no longer
// matches, leaking the head of a key. Redact fully, then cap.
func (s *scrubber) scrubString(str string) string {
	return s.scrubStringCap(str, previewCap)
}

// scrubStringCap redacts literals and pattern matches, THEN caps to the given
// length. Order matters: truncating first could split a secret so its pattern no
// longer matches, leaking the head of a key. Redact fully, then cap.
func (s *scrubber) scrubStringCap(str string, limit int) string {
	for _, lit := range s.literals {
		str = strings.ReplaceAll(str, lit, redacted)
	}
	for _, re := range secretPatterns {
		str = re.ReplaceAllString(str, redacted)
	}
	return truncate(str, limit)
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
