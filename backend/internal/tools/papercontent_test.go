package tools

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/config"
)

// latexmlSample mimics arXiv's LaTeXML output. Outer page chrome (banner, nav,
// footer) lives OUTSIDE <article class="ltx_document"> — dropped by container
// extraction. In-body noise (math, appendix, bibliography) lives INSIDE it —
// dropped by stripChrome. Headings and the figure caption must survive.
const latexmlSample = `<!DOCTYPE html><html><head><title>t</title></head><body>
<div class="ltx_page_banner">ARXIV NONPROFIT BANNER CHROME</div>
<nav class="ltx_page_navbar">SITE NAV LINKS</nav>
<article class="ltx_document">
<h1>Attention Is All You Need</h1>
<section><h2>Introduction</h2><p>The core contribution paragraph.</p>
<math alttext="E=mc^2"><mrow>MATHML NOISE</mrow></math></section>
<figure><img src="fig1.png"><figcaption class="ltx_caption">Figure 1: The architecture diagram.</figcaption></figure>
<section class="ltx_appendix"><h2>Appendix A</h2><p>APPENDIX BODY TO DROP</p></section>
<section class="ltx_bibliography"><h2>References</h2><p>BIBLIOGRAPHY BODY TO DROP</p></section>
</article>
<footer class="ltx_page_footer">FOOTER CHROME</footer>
</body></html>`

func htmlTestCfg(baseURL string) *config.AgentConfig {
	return &config.AgentConfig{
		ArxivHTMLBaseURL:      baseURL,
		UserAgent:             "arxiv-explainer-agent/test",
		RequestTimeoutSec:     10,
		MinRequestIntervalSec: 1,
		MaxRetries:            3,
		MaxContentBytes:       52428800,
	}
}

// newTestTool builds a tool pointed at the httptest server with a millisecond
// backoff so retry tests do not sleep for real seconds.
func newTestTool(baseURL string) *PaperContentTool {
	t := NewPaperContentTool(htmlTestCfg(baseURL))
	t.backoffUnit = time.Millisecond
	return t
}

func TestFetchMarkdownHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(latexmlSample))
	}))
	defer srv.Close()

	md, err := newTestTool(srv.URL).FetchMarkdown(context.Background(), "2312.00752")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Kept: heading hierarchy + caption.
	for _, want := range []string{"Attention Is All You Need", "Introduction", "core contribution", "Figure 1: The architecture diagram."} {
		if !strings.Contains(md, want) {
			t.Errorf("expected markdown to contain %q\n---\n%s", want, md)
		}
	}
	// Dropped: outer chrome (container extraction) + in-body noise (stripChrome).
	for _, gone := range []string{"ARXIV NONPROFIT BANNER CHROME", "SITE NAV LINKS", "FOOTER CHROME", "MATHML NOISE", "APPENDIX BODY TO DROP", "BIBLIOGRAPHY BODY TO DROP"} {
		if strings.Contains(md, gone) {
			t.Errorf("expected markdown to NOT contain %q\n---\n%s", gone, md)
		}
	}
}

func TestFetchMarkdown404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := newTestTool(srv.URL).FetchMarkdown(context.Background(), "0000.00000")
	if !errors.Is(err, ErrPaperHTMLNotFound) {
		t.Fatalf("expected ErrPaperHTMLNotFound, got %v", err)
	}
}

func TestFetchMarkdownRetriesThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			http.Error(w, "boom", http.StatusInternalServerError) // transient 5xx first
			return
		}
		w.Write([]byte(latexmlSample))
	}))
	defer srv.Close()

	md, err := newTestTool(srv.URL).FetchMarkdown(context.Background(), "2312.00752")
	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if atomic.LoadInt32(&calls) != 2 {
		t.Fatalf("expected 2 calls (1 fail + 1 success), got %d", calls)
	}
	if !strings.Contains(md, "Introduction") {
		t.Error("expected converted body after retry")
	}
}

func TestFetchMarkdownOversized(t *testing.T) {
	big := strings.Repeat("A", 2000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(big))
	}))
	defer srv.Close()

	tool := newTestTool(srv.URL)
	tool.cfg.MaxContentBytes = 1000 // smaller than the response → oversize
	_, err := tool.FetchMarkdown(context.Background(), "2312.00752")
	if !errors.Is(err, ErrPaperHTMLFailed) {
		t.Fatalf("expected ErrPaperHTMLFailed for oversized body, got %v", err)
	}
}

func TestFetchMarkdownCtxCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError) // force retry loop
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled before the call — first backoff sleep must abort
	_, err := newTestTool(srv.URL).FetchMarkdown(ctx, "2312.00752")
	if err == nil {
		t.Fatal("expected error on cancelled context")
	}
}
