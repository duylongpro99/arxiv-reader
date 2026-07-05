package tools

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

// dropTags are elements removed wholesale before Markdown conversion:
//   - math: MathML noise (LaTeXML emits <math> with both a rendered form and an
//     alttext LaTeX attr; we drop the whole node — surfacing alttext is a future
//     seam, deliberately not wired now).
//   - script/style: non-content.
//   - nav/header/footer: page chrome (LaTeXML wraps site navigation in these).
var dropTags = map[string]bool{
	"math":   true,
	"script": true,
	"style":  true,
	"nav":    true,
	"header": true,
	"footer": true,
}

// dropClasses are LaTeXML section classes trimmed as low-signal for an LLM:
// bibliography and appendix add tokens without explaining the core contribution,
// and the page header/footer/navbar are pure chrome. Figure/table CAPTIONS are
// intentionally NOT listed — they stay (context for dropped diagrams).
var dropClasses = []string{
	"ltx_bibliography",
	"ltx_appendix",
	"ltx_page_header",
	"ltx_page_footer",
	"ltx_page_navbar",
}

// documentRoot returns the LaTeXML paper body — the <article class="ltx_document">
// subtree — if present, so all surrounding page chrome (arXiv banner, feedback
// dialog, license notice, site nav/footer) is excluded from conversion in one
// move. Falls back to the whole parsed tree when the marker is absent (e.g. a
// non-LaTeXML page or a test fixture), so conversion still produces something.
func documentRoot(n *html.Node) *html.Node {
	if found := findByClass(n, "ltx_document"); found != nil {
		return found
	}
	return n
}

// findByClass returns the first element node whose class attribute contains the
// given whitespace-delimited token (depth-first).
func findByClass(n *html.Node, class string) *html.Node {
	if n.Type == html.ElementNode {
		for _, attr := range n.Attr {
			if attr.Key == "class" {
				for _, tok := range strings.Fields(attr.Val) {
					if tok == class {
						return n
					}
				}
			}
		}
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findByClass(c, class); found != nil {
			return found
		}
	}
	return nil
}

// stripChrome walks the parsed tree and removes unwanted nodes in place. Nodes
// are removed by capturing NextSibling before RemoveChild so the sibling walk
// is not invalidated by the mutation; survivors are recursed into.
func stripChrome(n *html.Node) {
	var next *html.Node
	for c := n.FirstChild; c != nil; c = next {
		next = c.NextSibling
		if shouldDrop(c) {
			n.RemoveChild(c)
			continue
		}
		stripChrome(c)
	}
}

// shouldDrop reports whether an element node is chrome/noise to remove. Text and
// other node types are always kept.
func shouldDrop(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	if dropTags[strings.ToLower(n.Data)] {
		return true
	}
	return hasDropClass(n)
}

// hasDropClass reports whether the node's class attribute contains any dropClass
// token (whitespace-delimited exact match, so "ltx_appendix" does not match a
// hypothetical "ltx_appendixfoo").
func hasDropClass(n *html.Node) bool {
	for _, attr := range n.Attr {
		if attr.Key != "class" {
			continue
		}
		for _, tok := range strings.Fields(attr.Val) {
			for _, drop := range dropClasses {
				if tok == drop {
					return true
				}
			}
		}
	}
	return false
}

// blankLines matches 3+ consecutive newlines (allowing intermediate spaces/tabs
// on the "blank" lines) so runs of empty lines collapse to a single blank line.
var blankLines = regexp.MustCompile(`\n[ \t]*\n[ \t]*(\n[ \t]*)+`)

// cleanupMarkdown applies minimal, LLM-tolerant whitespace normalization after
// conversion: collapse 3+ blank lines to one, then trim surrounding whitespace.
// Order matters — collapse first so a trailing run reduces before the final trim.
func cleanupMarkdown(md string) string {
	md = blankLines.ReplaceAllString(md, "\n\n")
	return strings.TrimSpace(md)
}
