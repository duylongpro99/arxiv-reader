package resource

import (
	"fmt"
	"strings"

	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// This file registers the v1 capability library the arXiv declaration exercises:
// two generic string transforms (normalize/trim) and two node-aware derivers
// (arxiv-id/arxiv-pdf-url). Per the scope fence, only what arXiv actually uses is
// implemented — new primitives are added at these seams when a real resource
// needs one (YAGNI).
//
// Derivers exist because a value can depend on multiple sibling nodes: arXiv's
// pdfUrl reads the entry's <link> elements plus the already-derived id, which a
// plain func(string)string transform cannot see (Validation V1).

func init() {
	// normalize collapses arXiv's newline-wrapped, multi-space text into one
	// clean line (verbatim: strings.Join(strings.Fields(s), " ")).
	RegisterTransform("normalize", func(any) (Transform, error) {
		return func(s string) string { return strings.Join(strings.Fields(s), " ") }, nil
	})
	// trim strips surrounding whitespace (published dates arrive padded).
	RegisterTransform("trim", func(any) (Transform, error) {
		return strings.TrimSpace, nil
	})

	RegisterDeriver("arxiv-id", deriveArxivID)
	RegisterDeriver("arxiv-pdf-url", deriveArxivPDFURL)
}

// deriveArxivID reads the entry's <id> and reduces it to the bare arXiv ID.
// Reproduces extractArxivID (discovery.go) exactly — including the /abs/ anchor
// with a last-"/" fallback and the version-suffix guard — so old-style IDs
// (cs/0501001v1 → cs/0501001) survive, unlike a naive afterLast("/") (F2).
func deriveArxivID(entry Node, _ *models.Paper) (string, error) {
	ids := entry.Get("id")
	if len(ids) == 0 {
		return "", nil // a missing id is caught by the `require` list
	}
	return extractArxivID(ids[0].Text()), nil
}

// deriveArxivPDFURL prefers an explicit PDF link (type=application/pdf OR
// rel=related, AND href containing "/pdf/"); otherwise it derives the canonical
// URL from the version-stripped id. This predicate (F3) is not expressible as a
// declarative equality match, hence a deriver. Reads p.ID, so `id` must be
// assigned before `pdfUrl` (the normalizer's fixed field order guarantees this).
func deriveArxivPDFURL(entry Node, p *models.Paper) (string, error) {
	for _, l := range entry.Get("link") {
		typ, rel, href := l.Attr("type"), l.Attr("rel"), l.Attr("href")
		if typ == "application/pdf" || rel == "related" {
			if strings.Contains(href, "/pdf/") {
				return href, nil
			}
		}
	}
	if p.ID != "" {
		return "https://arxiv.org/pdf/" + p.ID, nil
	}
	return "", nil
}

// extractArxivID turns an entry-id URL ("http://arxiv.org/abs/2401.12345v2") into
// the bare ID ("2401.12345"): take the segment after "/abs/" (last-"/" fallback),
// then drop a trailing version suffix (vN). Verbatim from the old tool (F2).
func extractArxivID(rawID string) string {
	id := strings.TrimSpace(rawID)
	if i := strings.LastIndex(id, "/abs/"); i != -1 {
		id = id[i+len("/abs/"):]
	} else if i := strings.LastIndex(id, "/"); i != -1 {
		id = id[i+1:]
	}
	if i := strings.LastIndex(id, "v"); i > 0 {
		if _, err := fmt.Sscanf(id[i+1:], "%d", new(int)); err == nil {
			id = id[:i]
		}
	}
	return id
}
