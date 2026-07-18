package arxivquery

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestIsValid(t *testing.T) {
	cases := []struct {
		code string
		want bool
	}{
		{"cs.AI", true},
		{"cs.LG", true},
		{"cs.SY", true},
		{"cs.NOPE", false}, // unknown code
		{"cs.ai", false},   // case-sensitive: arXiv codes are canonical
		{"cat:cs.AI", false}, // must be the bare code, not a filter fragment
		{"../etc", false},    // path-traversal-shaped junk
		{"", false},
	}
	for _, c := range cases {
		if got := IsValid(c.code); got != c.want {
			t.Errorf("IsValid(%q) = %v, want %v", c.code, got, c.want)
		}
	}
}

func TestSearchQuery(t *testing.T) {
	cases := []struct {
		name string
		q    Query
		want string
	}{
		{"category only", Query{Category: "cs.LG"}, "cat:cs.LG"},
		{"category + terms", Query{Category: "cs.LG", Terms: "transformer"}, "cat:cs.LG AND all:transformer"},
		{"multi-word terms", Query{Category: "cs.CL", Terms: "speech recognition"}, "cat:cs.CL AND all:speech recognition"},
	}
	for _, c := range cases {
		if got := c.q.SearchQuery(); got != c.want {
			t.Errorf("%s: SearchQuery() = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestSanitizeTerms(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "transformer attention", "transformer attention"},
		{"collapse whitespace", "  neural   nets  ", "neural nets"},
		{"strips OR operator", "x OR cat:cs.CR", "x catcs.CR"}, // "OR" dropped, ":" stripped
		{"strips AND operator", "foo AND bar", "foo bar"},
		{"strips ANDNOT", "foo ANDNOT bar", "foo bar"},
		{"lowercase operators too", "foo or bar", "foo bar"},
		{"strips quotes and parens", `"deep (learning)"`, "deep learning"},
		{"strips colon field prefix", "ti:injection", "tiinjection"},
		{"empty stays empty", "", ""},
		{"only operators", "AND OR", ""},
	}
	for _, c := range cases {
		if got := SanitizeTerms(c.in); got != c.want {
			t.Errorf("%s: SanitizeTerms(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestSanitizeTermsCapsLength(t *testing.T) {
	long := make([]byte, 500)
	for i := range long {
		long[i] = 'a'
	}
	got := SanitizeTerms(string(long))
	if len([]rune(got)) > maxTermsLen {
		t.Errorf("SanitizeTerms did not cap length: got %d runes, want <= %d", len([]rune(got)), maxTermsLen)
	}
}

// Truncation must cut on a rune boundary: a long multibyte string capped
// mid-rune would produce invalid UTF-8 and, downstream, a malformed arXiv query.
func TestSanitizeTermsTruncatesOnRuneBoundary(t *testing.T) {
	// 300 three-byte runes (well over the cap) with no spaces.
	long := strings.Repeat("界", 300)
	got := SanitizeTerms(long)
	if !utf8.ValidString(got) {
		t.Fatalf("SanitizeTerms produced invalid UTF-8: %q", got)
	}
	if len([]rune(got)) > maxTermsLen {
		t.Errorf("rune count %d exceeds cap %d", len([]rune(got)), maxTermsLen)
	}
}
