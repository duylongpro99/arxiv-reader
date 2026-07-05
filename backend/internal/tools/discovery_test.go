package tools

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/config"
)

// sampleFeed is a minimal but realistic arXiv Atom response with two entries,
// deliberately including the newline-wrapped title/summary arXiv actually emits.
const sampleFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <id>http://arxiv.org/abs/2401.12345v2</id>
    <title>A Study of
      Large Language Models</title>
    <summary>  We investigate
      scaling behaviour of LLMs.  </summary>
    <published>2024-01-20T10:30:00Z</published>
    <author><name>Ada Lovelace</name></author>
    <author><name>Alan Turing</name></author>
    <link href="http://arxiv.org/abs/2401.12345v2" rel="alternate" type="text/html"/>
    <link href="http://arxiv.org/pdf/2401.12345v2" rel="related" type="application/pdf"/>
  </entry>
  <entry>
    <id>http://arxiv.org/abs/2402.00001v1</id>
    <title>Second Paper</title>
    <summary>Another abstract.</summary>
    <published>2024-02-01T08:00:00Z</published>
    <author><name>Grace Hopper</name></author>
  </entry>
</feed>`

func testAgentCfg(baseURL string) *config.AgentConfig {
	return &config.AgentConfig{
		ArxivCategory:         "cs.AI",
		ArxivBaseURL:          baseURL,
		FetchLimit:            20,
		DisplayLimit:          5,
		UserAgent:             "arxiv-explainer-agent/test",
		RequestTimeoutSec:     5,
		MinRequestIntervalSec: 1,
		MaxRetries:            3,
	}
}

// newFastTool returns a tool with millisecond backoff so retry paths are quick.
func newFastTool(cfg *config.AgentConfig) *DiscoveryTool {
	tool := NewDiscoveryTool(cfg)
	tool.backoffUnit = time.Millisecond
	return tool
}

func TestFetchPapersHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "arxiv-explainer-agent/test" {
			t.Errorf("missing/wrong User-Agent: %q", got)
		}
		if got := r.URL.Query().Get("search_query"); got != "cat:cs.AI" {
			t.Errorf("wrong search_query: %q", got)
		}
		w.Header().Set("Content-Type", "application/atom+xml")
		_, _ = w.Write([]byte(sampleFeed))
	}))
	defer srv.Close()

	papers, err := newFastTool(testAgentCfg(srv.URL)).FetchPapers(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(papers) != 2 {
		t.Fatalf("expected 2 papers, got %d", len(papers))
	}

	p := papers[0]
	if p.ID != "2401.12345" {
		t.Errorf("ID: want 2401.12345, got %q", p.ID)
	}
	if p.Title != "A Study of Large Language Models" {
		t.Errorf("title not normalized: %q", p.Title)
	}
	if p.Abstract != "We investigate scaling behaviour of LLMs." {
		t.Errorf("abstract not normalized: %q", p.Abstract)
	}
	if len(p.Authors) != 2 || p.Authors[0] != "Ada Lovelace" {
		t.Errorf("authors wrong: %#v", p.Authors)
	}
	if p.PDFURL != "http://arxiv.org/pdf/2401.12345v2" {
		t.Errorf("pdfUrl wrong: %q", p.PDFURL)
	}
	// second entry has no pdf link -> derived from ID
	if papers[1].PDFURL != "https://arxiv.org/pdf/2402.00001" {
		t.Errorf("derived pdfUrl wrong: %q", papers[1].PDFURL)
	}
}

func TestFetchPapersRetriesThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests) // first call: 429
			return
		}
		_, _ = w.Write([]byte(sampleFeed)) // second call: 200
	}))
	defer srv.Close()

	papers, err := newFastTool(testAgentCfg(srv.URL)).FetchPapers(context.Background(), nil)
	if err != nil {
		t.Fatalf("expected success after retry, got %v", err)
	}
	if len(papers) != 2 {
		t.Fatalf("expected 2 papers, got %d", len(papers))
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 calls, got %d", calls.Load())
	}
}

// F5: onRetry must fire once per transient retry, with the attempt number, so
// the orchestrator can surface a "retry n/3" progress counter.
func TestFetchPapersOnRetryCallbackFires(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Fail the first two attempts (429, then 5xx), succeed on the third.
		switch calls.Add(1) {
		case 1:
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			_, _ = w.Write([]byte(sampleFeed))
		}
	}))
	defer srv.Close()

	var attempts []int
	_, err := newFastTool(testAgentCfg(srv.URL)).FetchPapers(context.Background(), func(attempt int) {
		attempts = append(attempts, attempt)
	})
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	// Two failures → two retries → attempts 1 and 2 (attempt 0 is the first try).
	if len(attempts) != 2 || attempts[0] != 1 || attempts[1] != 2 {
		t.Fatalf("onRetry attempts = %v, want [1 2]", attempts)
	}
}

func TestFetchPapersRateLimitExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	_, err := newFastTool(testAgentCfg(srv.URL)).FetchPapers(context.Background(), nil)
	if !errors.Is(err, ErrArxivRateLimit) {
		t.Fatalf("expected ErrArxivRateLimit, got %v", err)
	}
}

func TestFetchPapersServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := newFastTool(testAgentCfg(srv.URL)).FetchPapers(context.Background(), nil)
	if !errors.Is(err, ErrArxivUnavailable) {
		t.Fatalf("expected ErrArxivUnavailable, got %v", err)
	}
}

func TestFetchPapersParseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("this is not xml <<<"))
	}))
	defer srv.Close()

	_, err := newFastTool(testAgentCfg(srv.URL)).FetchPapers(context.Background(), nil)
	if !errors.Is(err, ErrArxivParse) {
		t.Fatalf("expected ErrArxivParse, got %v", err)
	}
}

func TestFetchPapersEmptyFeedIsNotError(t *testing.T) {
	const emptyFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom"></feed>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(emptyFeed))
	}))
	defer srv.Close()

	papers, err := newFastTool(testAgentCfg(srv.URL)).FetchPapers(context.Background(), nil)
	if err != nil {
		t.Fatalf("empty feed should not error, got %v", err)
	}
	if len(papers) != 0 {
		t.Fatalf("expected 0 papers, got %d", len(papers))
	}
}

func TestExtractArxivID(t *testing.T) {
	cases := map[string]string{
		"http://arxiv.org/abs/2401.12345v2": "2401.12345",
		"http://arxiv.org/abs/2401.12345":   "2401.12345",
		"2402.00001v1":                      "2402.00001",
		"http://arxiv.org/abs/cs/0501001v1": "cs/0501001", // older-style ID
	}
	for in, want := range cases {
		if got := extractArxivID(in); got != want {
			t.Errorf("extractArxivID(%q) = %q, want %q", in, got, want)
		}
	}
}
