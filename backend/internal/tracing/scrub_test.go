package tracing

import (
	"strings"
	"testing"
)

func TestScrubRedactsLiteralAndPatterns(t *testing.T) {
	s := newScrubber("super-secret-api-key-value")
	in := map[string]any{
		"literal": "the key is super-secret-api-key-value here",
		"openai":  "Authorization uses sk-abcdefghijklmnop1234567890",
		"anthropic": "sk-ant-api03-abcdefghijklmnopqrstuvwxyz",
		"google":  "AIzaSyABCDEFGHIJKLMNOPQRSTUVWXYZ0123456",
		"bearer":  "Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9",
		"kv":      "api_key=abc123def456ghi789",
		"safe":    "this is a normal summary line",
		"count":   42,
	}
	out := s.scrubMap(in)
	for _, k := range []string{"literal", "openai", "anthropic", "google", "bearer", "kv"} {
		if str, _ := out[k].(string); !strings.Contains(str, redacted) {
			t.Errorf("field %q not redacted: %q", k, str)
		}
	}
	if strings.Contains(out["literal"].(string), "super-secret-api-key-value") {
		t.Error("literal secret leaked")
	}
	if out["safe"].(string) != "this is a normal summary line" {
		t.Errorf("safe field altered: %q", out["safe"])
	}
	if out["count"] != 42 {
		t.Errorf("numeric field altered: %v", out["count"])
	}
}

func TestScrubTruncatesLongStrings(t *testing.T) {
	s := newScrubber()
	long := strings.Repeat("x", previewCap+200)
	got := s.scrubString(long)
	// Truncated to previewCap runes + a "(+N chars)" note.
	if !strings.Contains(got, "(+200 chars)") {
		t.Errorf("expected truncation note, got tail %q", got[len(got)-20:])
	}
	if len([]rune(got)) > previewCap+40 {
		t.Errorf("truncated string too long: %d runes", len([]rune(got)))
	}
}

func TestScrubRedactsBeforeTruncating(t *testing.T) {
	// A secret sitting past previewCap must still be redacted — truncation must
	// not be an escape hatch. Redaction runs first, so the tail secret is gone.
	s := newScrubber()
	str := strings.Repeat("a", previewCap+10) + " sk-abcdefghijklmnop1234567890"
	got := s.scrubString(str)
	if strings.Contains(got, "sk-abcdefghijklmnop1234567890") {
		t.Error("secret past the preview cap leaked through truncation")
	}
}

func TestScrubNestedAndEmpty(t *testing.T) {
	s := newScrubber("topsecret")
	if s.scrubMap(nil) != nil {
		t.Error("empty map should scrub to nil (SQL NULL)")
	}
	out := s.scrubMap(map[string]any{
		"nested":  map[string]any{"inner": "value topsecret"},
		"list":    []any{"a topsecret b", "clean"},
		"strlist": []string{"topsecret one", "two"}, // []string type branch
	})
	if nested := out["nested"].(map[string]any); !strings.Contains(nested["inner"].(string), redacted) {
		t.Error("nested map not scrubbed")
	}
	if list := out["list"].([]any); !strings.Contains(list[0].(string), redacted) {
		t.Error("list element not scrubbed")
	}
	if sl := out["strlist"].([]string); !strings.Contains(sl[0], redacted) || sl[1] != "two" {
		t.Errorf("[]string not scrubbed correctly: %v", sl)
	}
}
