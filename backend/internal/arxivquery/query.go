package arxivquery

import "strings"

// maxTermsLen caps sanitized free-text length. arXiv rejects overly long
// queries, and an unbounded field is a needless abuse surface; 200 chars is far
// beyond any realistic keyword search.
const maxTermsLen = 200

// controlTokens are arXiv boolean operators. A user typing them as free-text
// could otherwise splice new clauses into the query (e.g. "x OR cat:cs.CR"),
// rewriting its semantics. We strip them as standalone words during
// sanitization. Compared case-insensitively (arXiv treats AND/and alike).
var controlTokens = map[string]bool{"and": true, "or": true, "andnot": true}

// Query is the value object that owns arXiv `search_query` syntax. Category is a
// validated cs.* code (whitelisted upstream); Terms is optional, already-
// sanitized free-text. Keeping the rendering here is the single place arXiv
// query syntax lives — callers never concatenate query strings themselves.
type Query struct {
	Category string
	Terms    string
}

// SearchQuery renders the arXiv `search_query` value. Category-only yields
// `cat:<code>`; with terms it becomes `cat:<code> AND all:<terms>`, scoping the
// keyword search to the chosen category (category is required by design). The
// returned string is NOT URL-encoded — the caller passes it through
// url.Values.Set, which handles transport encoding.
func (q Query) SearchQuery() string {
	base := "cat:" + q.Category
	if q.Terms == "" {
		return base
	}
	return base + " AND all:" + q.Terms
}

// SanitizeTerms is the single security seam for user free-text. It:
//   - collapses whitespace and trims,
//   - drops arXiv boolean operators (AND/OR/ANDNOT) used as standalone words so
//     they cannot rewrite query semantics,
//   - strips structural characters (quotes, parens, colon) that could open a
//     field prefix or phrase group, and
//   - caps length.
//
// It intentionally leaves ordinary alphanumeric words intact — over-stripping
// would silently drop legitimate searches. Category safety is handled separately
// by the IsValid whitelist; this function guards ONLY the free-text field.
func SanitizeTerms(s string) string {
	// Remove structural characters that could open a field prefix (":") or a
	// quoted/grouped sub-expression before we tokenize.
	s = strings.Map(func(r rune) rune {
		switch r {
		case '"', '(', ')', ':':
			return -1
		default:
			return r
		}
	}, s)

	// Tokenize on whitespace (this also collapses runs of spaces), dropping any
	// token that is a bare boolean operator.
	fields := strings.Fields(s)
	kept := fields[:0] // reuse backing array; we only ever shrink
	for _, f := range fields {
		if controlTokens[strings.ToLower(f)] {
			continue
		}
		kept = append(kept, f)
	}
	out := strings.Join(kept, " ")

	// Cap length last, on the cleaned string. Cap by RUNE count (not bytes): a
	// byte-slice cut could split a multibyte rune, yielding invalid UTF-8 that
	// url.Encode would then mangle into a malformed arXiv query. Trim again so a
	// mid-word cut does not leave a trailing space.
	if r := []rune(out); len(r) > maxTermsLen {
		out = strings.TrimSpace(string(r[:maxTermsLen]))
	}
	return out
}
