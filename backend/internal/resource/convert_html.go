package resource

import (
	"bytes"
	"regexp"
	"strings"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"golang.org/x/net/html"
)

// html-to-markdown is the v1 content converter, ported verbatim from the old
// papercontent.go / papercontent-cleanup.go. It narrows to the LaTeXML paper
// body, strips chrome/math/bibliography nodes BEFORE conversion, converts the
// surviving tree, then applies minimal whitespace cleanup. Registered as a
// Converter capability so any resource serving LaTeXML HTML can reuse it.

func init() { RegisterConverter("html-to-markdown", convertHTMLToMarkdown) }

func convertHTMLToMarkdown(htmlBytes []byte) (string, error) {
	doc, err := html.Parse(bytes.NewReader(htmlBytes))
	if err != nil {
		return "", err
	}
	root := documentRoot(doc)
	stripChrome(root)
	md, err := htmltomarkdown.ConvertNode(root)
	if err != nil {
		return "", err
	}
	return cleanupMarkdown(string(md)), nil
}

// dropTags are elements removed wholesale before conversion: MathML noise,
// non-content script/style, and LaTeXML page chrome (nav/header/footer).
var dropTags = map[string]bool{
	"math": true, "script": true, "style": true,
	"nav": true, "header": true, "footer": true,
}

// dropClasses are LaTeXML section classes trimmed as low-signal for an LLM
// (bibliography/appendix add tokens without explaining the contribution; the
// page header/footer/navbar are pure chrome). Figure/table captions are kept.
var dropClasses = []string{
	"ltx_bibliography", "ltx_appendix",
	"ltx_page_header", "ltx_page_footer", "ltx_page_navbar",
}

// documentRoot returns the <article class="ltx_document"> subtree if present so
// all surrounding page chrome is excluded in one move; falls back to the whole
// tree when the marker is absent (non-LaTeXML page or a test fixture).
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

// stripChrome removes unwanted nodes in place, capturing NextSibling before
// RemoveChild so the sibling walk survives the mutation; survivors are recursed.
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

// shouldDrop reports whether an element node is chrome/noise. Non-elements stay.
func shouldDrop(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	if dropTags[strings.ToLower(n.Data)] {
		return true
	}
	return hasDropClass(n)
}

// hasDropClass reports whether the node's class contains any dropClass token
// (whitespace-delimited exact match).
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

// blankLines matches 3+ consecutive newlines (allowing intermediate spaces/tabs)
// so runs of empty lines collapse to a single blank line.
var blankLines = regexp.MustCompile(`\n[ \t]*\n[ \t]*(\n[ \t]*)+`)

// cleanupMarkdown collapses 3+ blank lines to one, then trims — order matters so
// a trailing run reduces before the final trim.
func cleanupMarkdown(md string) string {
	md = blankLines.ReplaceAllString(md, "\n\n")
	return strings.TrimSpace(md)
}
