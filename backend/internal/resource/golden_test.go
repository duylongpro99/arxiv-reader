package resource

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// Engine regression gate. Through Phase 06 this diffed the engine against the old
// DiscoveryTool/PaperContentTool as a live A/B oracle; those tools are deleted in
// Phase 07 (the full e2e in server/integration_test.go having proven parity), so
// the gate is now a fixed regression: the SHIPPED arxiv.yaml, run through the
// engine, must produce these exact Papers — the values the old tool produced.

// goldenFeed exercises the seams the red team flagged: explicit rel=related PDF
// with /pdf/, a derived PDF (no link), an old-style id, a type=pdf link WITHOUT
// /pdf/ (must derive), and whitespace-wrapped title/summary/published.
const goldenFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <id>http://arxiv.org/abs/2401.12345v2</id>
    <title>A Study of
      Large Language Models</title>
    <summary>  We investigate
      scaling behaviour.  </summary>
    <published>  2024-01-20T10:30:00Z  </published>
    <author><name>Ada Lovelace</name></author>
    <author><name>Alan Turing</name></author>
    <link href="http://arxiv.org/pdf/2401.12345v2" rel="related" type="application/pdf"/>
  </entry>
  <entry>
    <id>http://arxiv.org/abs/2402.00001v1</id>
    <title>Second Paper</title>
    <summary>Another abstract.</summary>
    <published>2024-02-01T08:00:00Z</published>
    <author><name>Grace Hopper</name></author>
  </entry>
  <entry>
    <id>http://arxiv.org/abs/cs/0501001v1</id>
    <title>Old Style ID Paper</title>
    <summary>Legacy identifier.</summary>
    <published>2005-01-03T00:00:00Z</published>
    <author><name>Edsger Dijkstra</name></author>
  </entry>
  <entry>
    <id>http://arxiv.org/abs/2403.55555v1</id>
    <title>PDF Link Without Slash Pdf</title>
    <summary>Should derive.</summary>
    <published>2024-03-01T00:00:00Z</published>
    <author><name>Katherine Johnson</name></author>
    <link href="http://arxiv.org/abs/2403.55555" rel="related" type="application/pdf"/>
  </entry>
</feed>`

// wantPapers is the known-good normalization of goldenFeed (Source omitted; it is
// asserted separately as "arxiv").
var wantPapers = []models.Paper{
	{ID: "2401.12345", Title: "A Study of Large Language Models", Abstract: "We investigate scaling behaviour.",
		Authors: []string{"Ada Lovelace", "Alan Turing"}, PDFURL: "http://arxiv.org/pdf/2401.12345v2", Published: "2024-01-20T10:30:00Z"},
	{ID: "2402.00001", Title: "Second Paper", Abstract: "Another abstract.",
		Authors: []string{"Grace Hopper"}, PDFURL: "https://arxiv.org/pdf/2402.00001", Published: "2024-02-01T08:00:00Z"},
	{ID: "cs/0501001", Title: "Old Style ID Paper", Abstract: "Legacy identifier.",
		Authors: []string{"Edsger Dijkstra"}, PDFURL: "https://arxiv.org/pdf/cs/0501001", Published: "2005-01-03T00:00:00Z"},
	{ID: "2403.55555", Title: "PDF Link Without Slash Pdf", Abstract: "Should derive.",
		Authors: []string{"Katherine Johnson"}, PDFURL: "https://arxiv.org/pdf/2403.55555", Published: "2024-03-01T00:00:00Z"},
}

func loadArxivSource(t *testing.T, base, htmlBase string) *DeclarativeSource {
	t.Helper()
	reg, err := Load(realResourcesDir, prodResolve(base, htmlBase))
	if err != nil {
		t.Fatalf("load arxiv.yaml: %v", err)
	}
	src, ok := reg.Get("arxiv")
	if !ok {
		t.Fatal("arxiv not registered")
	}
	return src.(*DeclarativeSource)
}

// TestArxivDiscoveryRegression is the discovery gate: shipped arxiv.yaml → exact Papers.
func TestArxivDiscoveryRegression(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(goldenFeed))
	}))
	defer srv.Close()

	src := loadArxivSource(t, srv.URL, srv.URL)
	got, err := src.Discover(context.Background(), Request{Values: map[string]string{"category": "cs.AI"}}, 0, nil)
	if err != nil {
		t.Fatalf("engine discover: %v", err)
	}
	if len(got) != len(wantPapers) {
		t.Fatalf("count mismatch: got=%d want=%d", len(got), len(wantPapers))
	}
	for i, w := range wantPapers {
		g := got[i]
		if g.ID != w.ID || g.Title != w.Title || g.Abstract != w.Abstract ||
			g.PDFURL != w.PDFURL || g.Published != w.Published {
			t.Errorf("entry %d mismatch:\n got=%+v\n want=%+v", i, g, w)
		}
		if len(g.Authors) != len(w.Authors) {
			t.Errorf("entry %d authors: got=%v want=%v", i, g.Authors, w.Authors)
			continue
		}
		for j := range w.Authors {
			if g.Authors[j] != w.Authors[j] {
				t.Errorf("entry %d author %d: got=%q want=%q", i, j, g.Authors[j], w.Authors[j])
			}
		}
		if g.Source != "arxiv" {
			t.Errorf("entry %d source = %q, want arxiv", i, g.Source)
		}
	}
}

// goldenHTML mirrors arXiv's LaTeXML shape: outer chrome outside the article,
// in-body math/appendix/bibliography inside — all dropped; headings kept.
const goldenHTML = `<!DOCTYPE html><html><head><title>t</title></head><body>
<div class="ltx_page_banner">BANNER CHROME</div>
<nav class="ltx_page_navbar">SITE NAV</nav>
<article class="ltx_document">
<h1>Attention Is All You Need</h1>
<section><h2>Introduction</h2><p>The core contribution paragraph.</p>
<math alttext="E=mc^2"><mrow>MATHML NOISE</mrow></math></section>
<section class="ltx_appendix"><h2>Appendix A</h2><p>APPENDIX TO DROP</p></section>
<section class="ltx_bibliography"><h2>References</h2><p>BIBLIOGRAPHY TO DROP</p></section>
</article>
<footer class="ltx_page_footer">FOOTER CHROME</footer>
</body></html>`

// TestArxivContentRegression: shipped content pipeline keeps the body, drops chrome.
func TestArxivContentRegression(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(goldenHTML))
	}))
	defer srv.Close()

	src := loadArxivSource(t, srv.URL, srv.URL)
	md, err := src.FetchContent(context.Background(), "2401.12345")
	if err != nil {
		t.Fatalf("engine content: %v", err)
	}
	for _, want := range []string{"Attention Is All You Need", "core contribution paragraph"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
	for _, drop := range []string{"BANNER CHROME", "SITE NAV", "FOOTER CHROME", "APPENDIX TO DROP", "BIBLIOGRAPHY TO DROP", "MATHML NOISE"} {
		if strings.Contains(md, drop) {
			t.Errorf("markdown should have stripped %q:\n%s", drop, md)
		}
	}
}

// F12: 404 → recoverable re-pick (ErrContentNotFound).
func TestArxivContent404Repick(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	src := loadArxivSource(t, srv.URL, srv.URL)
	if _, err := src.FetchContent(context.Background(), "2401.12345"); !errors.Is(err, ErrContentNotFound) {
		t.Fatalf("engine 404 should be ErrContentNotFound, got %v", err)
	}
}

// F14: content oversize is rejected, not truncated.
func TestArxivContentOversize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 100))
	}))
	defer srv.Close()

	resolve := func(key string) (string, error) {
		if key == "AGENT_MAX_CONTENT_BYTES" {
			return "10", nil
		}
		return prodResolve(srv.URL, srv.URL)(key)
	}
	reg, err := Load(realResourcesDir, resolve)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	src, _ := reg.Get("arxiv")
	if _, err := src.FetchContent(context.Background(), "2401.12345"); !errors.Is(err, ErrTransportTooLarge) {
		t.Fatalf("oversize should be ErrTransportTooLarge, got %v", err)
	}
}
