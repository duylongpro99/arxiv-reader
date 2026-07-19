package resource

import "testing"

// Reuses the arxivquery.SanitizeTerms cases against the moved arxiv-terms
// sanitizer, proving the security seam behaves identically under the engine.
func TestArxivTermsSanitizer(t *testing.T) {
	san, err := lookupSanitizer("arxiv-terms")
	if err != nil {
		t.Fatalf("arxiv-terms not registered: %v", err)
	}
	cases := map[string]string{
		"transformer":            "transformer",
		"  spaced   out  words ": "spaced out words",
		`x OR cat:cs.CR`:         "x catcs.CR", // OR dropped, colon stripped
		`"quoted (grouped)"`:     "quoted grouped",
		"AND OR ANDNOT":          "", // all bare operators dropped
		"deep AND learning":      "deep learning",
	}
	for in, want := range cases {
		if got := san(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestArxivTermsCapsLength(t *testing.T) {
	san, _ := lookupSanitizer("arxiv-terms")
	long := make([]byte, 500)
	for i := range long {
		long[i] = 'a'
	}
	if got := san(string(long)); len([]rune(got)) != 200 {
		t.Fatalf("length not capped to 200, got %d", len([]rune(got)))
	}
}
