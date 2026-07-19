package resource

import (
	"net/url"
	"regexp"
	"strings"
)

// This file handles RUNTIME interpolation only — the `{{...}}` placeholders for
// untrusted runtime values (category/terms/start/paper.id). Trusted `${...}`
// config references are resolved earlier, at LOAD time, on the raw YAML bytes
// (loader.go, Phase 03). Keeping the two interpolations in separate stages is
// the dual-interpolation guarantee: config-trusted vs runtime-untrusted never mix.

// runtimeVars maps a placeholder name to its (already schema-validated) value.
type runtimeVars map[string]string

// placeholderRe matches {{name}} with an optional surrounding space and a
// dotted-name body (e.g. paper.id).
var placeholderRe = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.]+)\s*\}\}`)

// substitute replaces every {{name}} with vars[name]. An unknown name resolves
// to empty — a missing optional value simply drops out. Values are NOT
// URL-encoded here; url.Values.Encode() (transport) handles that once, matching
// the old tool's "SearchQuery returns raw, Set encodes" contract.
func substitute(s string, vars runtimeVars) string {
	return placeholderRe.ReplaceAllStringFunc(s, func(m string) string {
		name := placeholderRe.FindStringSubmatch(m)[1]
		return vars[name]
	})
}

// buildQuery assembles a url.Values from the declared query spec and the runtime
// vars. A literal part is substituted directly; a join-of-parts assembles its
// parts with the given Join, DROPPING any part whose When guard var is empty
// (e.g. the `all:{{terms}}` clause when no free-text was supplied).
func buildQuery(spec map[string]QueryPart, vars runtimeVars) url.Values {
	q := url.Values{}
	for key, part := range spec {
		if part.IsLiteral() {
			q.Set(key, substitute(part.Literal, vars))
			continue
		}
		pieces := make([]string, 0, len(part.Parts))
		for _, p := range part.Parts {
			if p.When != "" && strings.TrimSpace(vars[p.When]) == "" {
				continue // conditional clause dropped when its guard is empty
			}
			pieces = append(pieces, substitute(p.Value, vars))
		}
		q.Set(key, strings.Join(pieces, part.Join))
	}
	return q
}
