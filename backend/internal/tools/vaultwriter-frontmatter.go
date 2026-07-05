package tools

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// arxivIDSanitize keeps only the characters valid in an arXiv ID for filenames
// (handles both new "2401.12345v2" and old "cs.AI/0123456" styles — the slash is
// stripped, collapsing the category prefix into the numeric part).
var (
	arxivIDSanitize = regexp.MustCompile(`[^a-z0-9._-]+`)
	slugStrip       = regexp.MustCompile(`[^a-z0-9-]+`)
	slugCollapse    = regexp.MustCompile(`-+`)
	dateShape       = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// buildFrontmatter renders the YAML frontmatter block that precedes the note
// body. Keys are fixed and values are YAML-escaped (titles routinely contain
// ":"). The review_* fields reflect the Phase 5 verdict: a nil verdict means the
// reviewer was disabled (max_review_iterations: 0) → review_iterations: 0,
// review_passed: true, and review_score is omitted (no review ran). A set verdict
// records its real iteration count, pass flag, and score.
func (t *VaultWriterTool) buildFrontmatter(p models.Paper, ex models.ExplainerOutput, verdict *models.ReviewVerdict) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString(fmt.Sprintf("arxiv_id: %s\n", escapeYAML(p.ID)))
	b.WriteString(fmt.Sprintf("title: %s\n", escapeYAML(p.Title)))
	b.WriteString("authors:\n")
	for _, a := range p.Authors {
		b.WriteString(fmt.Sprintf("  - %s\n", escapeYAML(a)))
	}
	b.WriteString(fmt.Sprintf("published: %s\n", escapeYAML(dateOnly(p.Published))))
	b.WriteString(fmt.Sprintf("category: %s\n", escapeYAML(t.cfg.Agent.ArxivCategory)))
	b.WriteString(fmt.Sprintf("generated_at: %s\n", escapeYAML(ex.CreatedAt.UTC().Format(time.RFC3339))))
	if verdict == nil {
		// Reviewer disabled: nothing was reviewed, so record an honest "0 rounds,
		// passed by default" and omit the score (there is no meaningful value).
		b.WriteString("review_iterations: 0\n")
		b.WriteString("review_passed: true\n")
	} else {
		b.WriteString(fmt.Sprintf("review_iterations: %d\n", verdict.Iteration))
		b.WriteString(fmt.Sprintf("review_passed: %t\n", verdict.Pass))
		b.WriteString(fmt.Sprintf("review_score: %.2f\n", verdict.Score))
	}
	b.WriteString("tags: [ai, paper, explainer]\n")
	b.WriteString("---\n\n")
	return b.String()
}

// generateFilename builds "YYYY-MM-DD_arxivID_slug.md". The date is the date part
// of Published (fallbacks in dateOnly), the arXiv ID is sanitized, and the title
// is slugified — so the whole name is filesystem- and traversal-safe.
func (t *VaultWriterTool) generateFilename(p models.Paper) string {
	date := dateOnly(p.Published)
	id := sanitizeArxivID(p.ID)
	slug := slugify(p.Title)
	return fmt.Sprintf("%s_%s_%s.md", date, id, slug)
}

// dateOnly extracts a YYYY-MM-DD date from the Published string (which may be a
// bare date or a full RFC3339 timestamp). Triple fallback so it never panics:
// parse RFC3339 → first 10 chars if date-shaped → "unknown".
func dateOnly(s string) string {
	if ts, err := time.Parse(time.RFC3339, s); err == nil {
		return ts.Format("2006-01-02")
	}
	if len(s) >= 10 && dateShape.MatchString(s[:10]) {
		return s[:10]
	}
	return "unknown"
}

// slugify lowercases, turns spaces into hyphens, strips non-[a-z0-9-], collapses
// repeated hyphens, and trims to <=60 chars at a word (hyphen) boundary.
func slugify(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	s = strings.ReplaceAll(s, " ", "-")
	s = slugStrip.ReplaceAllString(s, "")
	s = slugCollapse.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 60 {
		s = s[:60]
		// Trim back to the last hyphen so we don't cut a word mid-way.
		if i := strings.LastIndex(s, "-"); i > 0 {
			s = s[:i]
		}
		s = strings.Trim(s, "-")
	}
	if s == "" {
		return "untitled"
	}
	return s
}

// sanitizeArxivID lowercases and strips any character outside [a-z0-9._-] so the
// ID is always a safe filename component.
func sanitizeArxivID(id string) string {
	s := arxivIDSanitize.ReplaceAllString(strings.ToLower(id), "")
	if s == "" {
		return "unknown"
	}
	return s
}

// escapeYAML wraps a scalar in double quotes and escapes backslashes and quotes,
// preventing metadata injection and keeping values with ":" valid YAML.
func escapeYAML(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
