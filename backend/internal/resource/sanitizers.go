package resource

import "strings"

// This file registers the arxiv-terms sanitizer — the security seam for arXiv
// free-text. The logic is moved verbatim from arxivquery.SanitizeTerms so the
// same guarantees (strip boolean operators + structural chars, cap length) hold
// under the engine, now reachable by any resource that names sanitize: arxiv-terms.

func init() { RegisterSanitizer("arxiv-terms", sanitizeArxivTerms) }

// maxTermsLen caps sanitized free-text length (verbatim from arxivquery).
const maxTermsLen = 200

// controlTokens are arXiv boolean operators stripped as standalone words so a
// user cannot splice new query clauses (compared case-insensitively).
var controlTokens = map[string]bool{"and": true, "or": true, "andnot": true}

// sanitizeArxivTerms strips structural characters (" ( ) :) and standalone
// boolean operators, collapses whitespace, and caps length by rune count.
// Verbatim from arxivquery.SanitizeTerms.
func sanitizeArxivTerms(s string) string {
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

	// Tokenize on whitespace (also collapsing runs), dropping bare operators.
	fields := strings.Fields(s)
	kept := fields[:0]
	for _, f := range fields {
		if controlTokens[strings.ToLower(f)] {
			continue
		}
		kept = append(kept, f)
	}
	out := strings.Join(kept, " ")

	// Cap length last, by RUNE count, trimming a mid-word cut's trailing space.
	if r := []rune(out); len(r) > maxTermsLen {
		out = strings.TrimSpace(string(r[:maxTermsLen]))
	}
	return out
}
