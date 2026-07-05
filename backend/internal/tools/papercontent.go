package tools

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	htmltomarkdown "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/maritime-ds/arxiv-reader/internal/config"
	"golang.org/x/net/html"
)

// Sentinel errors let the orchestrator map failures to user-facing messages and
// decide recoverability. 404 is special: permanent for this paper, but the
// orchestrator treats it as a recoverable re-pick, not a hard pipeline failure.
var (
	ErrPaperHTMLNotFound = errors.New("paper HTML not found on arXiv (404)")
	ErrPaperHTMLFailed   = errors.New("failed to fetch or convert paper HTML")
	ErrPaperHTMLTimeout  = errors.New("HTML fetch timed out")
)

// PaperContentTool owns the relationship with arXiv's LaTeXML HTML rendering:
// given a bare arXiv ID it returns clean Markdown. Nothing outside this file
// knows how arXiv serves HTML. It mirrors DiscoveryTool's retry/backoff and
// User-Agent politeness (same domain, same transient-failure rules).
type PaperContentTool struct {
	cfg        *config.AgentConfig
	httpClient *http.Client
	// backoffUnit is time.Second in production; tests shrink it to keep retry
	// paths fast without changing the (integer-seconds) config. Mirrors
	// DiscoveryTool.backoffUnit.
	backoffUnit time.Duration
}

// NewPaperContentTool builds a tool whose HTTP client enforces the configured
// per-request timeout. CheckRedirect is left at its default so the client
// follows arXiv's same-host redirect from /html/{id} to the versioned URL.
func NewPaperContentTool(cfg *config.AgentConfig) *PaperContentTool {
	return &PaperContentTool{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: time.Duration(cfg.RequestTimeoutSec) * time.Second,
		},
		backoffUnit: time.Second,
	}
}

// FetchMarkdown fetches {ArxivHTMLBaseURL}/{arxivID}, converts the LaTeXML HTML
// to Markdown, and returns the cleaned text. arXiv IDs are already bare (version
// stripped upstream); the client follows the same-host redirect to the versioned
// URL automatically, so no version handling is needed here.
func (t *PaperContentTool) FetchMarkdown(ctx context.Context, arxivID string) (string, error) {
	start := time.Now()
	reqURL := t.cfg.ArxivHTMLBaseURL + "/" + arxivID
	slog.Info("html fetch started", "component", "papercontent", "arxiv_id", arxivID)

	htmlBytes, err := t.fetchHTMLWithRetry(ctx, reqURL)
	if err != nil {
		return "", err
	}
	slog.Info("html fetch complete",
		"component", "papercontent",
		"arxiv_id", arxivID,
		"html_bytes", len(htmlBytes),
		"duration_ms", time.Since(start).Milliseconds(),
	)

	md, err := convertToMarkdown(htmlBytes)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrPaperHTMLFailed, err)
	}
	md = cleanupMarkdown(md)
	slog.Info("markdown conversion complete",
		"component", "papercontent",
		"arxiv_id", arxivID,
		"markdown_bytes", len(md),
	)
	return md, nil
}

// fetchHTMLWithRetry retries only transient (429/5xx/network) failures with the
// discovery backoff schedule; permanent 4xx are surfaced immediately. 404 maps
// to the dedicated ErrPaperHTMLNotFound so the orchestrator can offer a re-pick.
func (t *PaperContentTool) fetchHTMLWithRetry(ctx context.Context, reqURL string) ([]byte, error) {
	var lastTransient error
	for attempt := 0; attempt <= t.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := t.backoffFor(attempt)
			slog.Warn("html request failed, retrying",
				"component", "papercontent",
				"attempt", attempt,
				"backoff_ms", backoff.Milliseconds(),
				"error", lastTransient.Error(),
			)
			if err := sleepCtx(ctx, backoff); err != nil {
				return nil, err // ctx cancelled during backoff
			}
		}
		body, transient, err := t.doHTMLRequest(ctx, reqURL)
		if err == nil {
			return body, nil
		}
		if !transient {
			return nil, err // permanent (404, other 4xx) — do not retry
		}
		lastTransient = err
	}
	return nil, lastTransient // retries exhausted
}

// doHTMLRequest executes one request. transient=true means the caller may retry.
// The body is read under an io.LimitReader OOM guard: LimitReader silently
// truncates at the cap, so a doc *at* the cap is indistinguishable from a
// truncated one. We read cap+1 bytes; if we actually get cap+1, the real body
// exceeded the cap → treat as oversized (recoverable "too large") rather than
// silently feeding a truncated document to the converter.
func (t *PaperContentTool) doHTMLRequest(ctx context.Context, reqURL string) (body []byte, transient bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrPaperHTMLFailed, err)
	}
	req.Header.Set("User-Agent", t.cfg.UserAgent)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		// A context deadline surfaces as a distinct timeout sentinel; other
		// network errors are transient and worth a retry.
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, false, fmt.Errorf("%w: %v", ErrPaperHTMLTimeout, err)
		}
		return nil, true, fmt.Errorf("%w: %v", ErrPaperHTMLFailed, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		limited := io.LimitReader(resp.Body, t.cfg.MaxContentBytes+1)
		b, readErr := io.ReadAll(limited)
		if readErr != nil {
			return nil, true, fmt.Errorf("%w: %v", ErrPaperHTMLFailed, readErr)
		}
		if int64(len(b)) > t.cfg.MaxContentBytes {
			return nil, false, fmt.Errorf("%w: response exceeds %d bytes", ErrPaperHTMLFailed, t.cfg.MaxContentBytes)
		}
		return b, false, nil
	case resp.StatusCode == http.StatusNotFound:
		slog.Warn("paper html not found", "component", "papercontent", "url", reqURL)
		return nil, false, ErrPaperHTMLNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, true, fmt.Errorf("%w: rate limited (429)", ErrPaperHTMLFailed)
	case resp.StatusCode >= 500:
		return nil, true, fmt.Errorf("%w: upstream status %d", ErrPaperHTMLFailed, resp.StatusCode)
	default:
		return nil, false, fmt.Errorf("%w: unexpected status %d", ErrPaperHTMLFailed, resp.StatusCode)
	}
}

// backoffFor returns base * 2^(attempt-1), mirroring DiscoveryTool.backoffFor.
func (t *PaperContentTool) backoffFor(attempt int) time.Duration {
	unit := t.backoffUnit
	if unit <= 0 {
		unit = time.Second
	}
	base := time.Duration(t.cfg.MinRequestIntervalSec) * unit
	if base <= 0 {
		base = unit
	}
	return base * time.Duration(1<<(attempt-1))
}

// convertToMarkdown parses the HTML, strips LaTeXML chrome/math/bibliography
// nodes, then converts the surviving tree to Markdown. Node-level stripping
// (before conversion) is cleaner than post-processing Markdown text.
func convertToMarkdown(htmlBytes []byte) (string, error) {
	doc, err := html.Parse(bytes.NewReader(htmlBytes))
	if err != nil {
		return "", err
	}
	// Narrow to the paper body first (drops outer page chrome wholesale), then
	// strip in-body noise (math, bibliography, appendix) from that subtree.
	root := documentRoot(doc)
	stripChrome(root)
	md, err := htmltomarkdown.ConvertNode(root)
	if err != nil {
		return "", err
	}
	return string(md), nil
}
