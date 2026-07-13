// Package tools holds the discovery-pipeline tools. DiscoveryTool owns the
// entire relationship with the arXiv API; nothing outside this file needs to
// know how arXiv is queried or how its Atom/XML is shaped.
package tools

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/config"
	"github.com/maritime-ds/arxiv-reader/internal/models"
)

// Sentinel errors let the orchestrator map failures to user-facing messages
// and decide recoverability without string-matching.
var (
	ErrArxivRateLimit   = errors.New("arXiv rate limit exceeded after retries")
	ErrArxivUnavailable = errors.New("arXiv API unavailable")
	ErrArxivParse       = errors.New("failed to parse arXiv response")
)

// DiscoveryTool queries arXiv for the most recent papers in a category.
type DiscoveryTool struct {
	cfg        *config.AgentConfig
	httpClient *http.Client
	// backoffUnit is the time unit multiplied by MinRequestIntervalSec to form
	// the retry backoff. It is time.Second in production; tests shrink it to
	// keep retry paths fast without changing the (integer-seconds) config.
	backoffUnit time.Duration
}

// NewDiscoveryTool builds a tool whose HTTP client enforces the configured
// per-request timeout.
func NewDiscoveryTool(cfg *config.AgentConfig) *DiscoveryTool {
	return &DiscoveryTool{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: time.Duration(cfg.RequestTimeoutSec) * time.Second,
		},
		backoffUnit: time.Second,
	}
}

// --- Atom/XML shapes (namespace-agnostic; encoding/xml matches on local name) ---

type arxivFeed struct {
	XMLName xml.Name     `xml:"feed"`
	Entries []arxivEntry `xml:"entry"`
}

type arxivEntry struct {
	ID        string        `xml:"id"`
	Title     string        `xml:"title"`
	Summary   string        `xml:"summary"`
	Published string        `xml:"published"`
	Authors   []arxivAuthor `xml:"author"`
	Links     []arxivLink   `xml:"link"`
}

type arxivAuthor struct {
	Name string `xml:"name"`
}

type arxivLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

// FetchPapers queries arXiv (newest first) for the FIRST page (start=0) and
// returns up to cfg.FetchLimit papers. It delegates to FetchPapersFrom so the
// one existing caller (orchestrator-pipeline.go's runDiscovery) needs no change
// while Feature C (pagination) is added — see FetchPapersFrom.
//
// onRetry (optional; pass nil when not needed) is invoked with the attempt
// number on each transient retry, letting the caller surface a progress counter
// (F5) WITHOUT this tool depending on the session. It keeps DiscoveryTool
// decoupled — the callback is the only seam.
func (t *DiscoveryTool) FetchPapers(ctx context.Context, onRetry func(attempt int)) ([]models.Paper, error) {
	return t.FetchPapersFrom(ctx, 0, onRetry)
}

// FetchPapersFrom queries arXiv (newest first) starting at the given offset and
// returns up to cfg.FetchLimit papers. It retries transient failures (429/5xx)
// with exponential backoff and aborts promptly if ctx is cancelled. An
// empty-but-well-formed feed returns an empty slice with no error — "no papers
// right now" is not a failure.
//
// start lets a caller page through older results (Feature C: "load more" via
// session extension) without this tool knowing anything about sessions.
func (t *DiscoveryTool) FetchPapersFrom(ctx context.Context, start int, onRetry func(attempt int)) ([]models.Paper, error) {
	fetchStart := time.Now()
	reqURL := t.buildQueryURL(start)

	body, err := t.fetchWithRetry(ctx, reqURL, onRetry)
	if err != nil {
		return nil, err
	}

	var feed arxivFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		// Return the bare sentinel — the underlying xml error can echo a token
		// of the response body, and CLAUDE.md forbids logging raw payloads.
		return nil, ErrArxivParse
	}

	papers := make([]models.Paper, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		papers = append(papers, entryToPaper(e))
	}

	slog.Info("arxiv fetch complete",
		"component", "discovery",
		"start", start,
		"count", len(papers),
		"duration_ms", time.Since(fetchStart).Milliseconds(),
	)
	return papers, nil
}

// buildQueryURL assembles the arXiv query at the given start offset. The
// category comes from config, never user input (PRD §7), so there is no
// injection surface.
func (t *DiscoveryTool) buildQueryURL(start int) string {
	q := url.Values{}
	q.Set("search_query", "cat:"+t.cfg.ArxivCategory)
	q.Set("sortBy", "submittedDate")
	q.Set("sortOrder", "descending")
	q.Set("max_results", fmt.Sprintf("%d", t.cfg.FetchLimit))
	q.Set("start", fmt.Sprintf("%d", start))
	return t.cfg.ArxivBaseURL + "?" + q.Encode()
}

// fetchWithRetry performs the request, retrying only on transient (429/5xx)
// responses. Parse-level or permanent 4xx failures are NOT retried — they will
// not fix themselves and retrying only burns the user's time budget.
func (t *DiscoveryTool) fetchWithRetry(ctx context.Context, reqURL string, onRetry func(attempt int)) ([]byte, error) {
	var lastTransient error

	// attempt 0 is the first try; up to MaxRetries additional attempts follow,
	// each preceded by a backoff of base * 2^(attempt-1): 3s -> 6s -> 12s.
	for attempt := 0; attempt <= t.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := t.backoffFor(attempt)
			// Notify the caller so it can surface a retry progress counter (F5),
			// alongside the structured log below.
			if onRetry != nil {
				onRetry(attempt)
			}
			// Log the actual prior failure — the loop retries both 429 and
			// 5xx/network, so a hardcoded "rate limited" message would mislead
			// during a real arXiv outage.
			slog.Warn("arxiv request failed, retrying",
				"component", "discovery",
				"attempt", attempt,
				"backoff_ms", backoff.Milliseconds(),
				"error", lastTransient.Error(),
			)
			if err := sleepCtx(ctx, backoff); err != nil {
				return nil, err // ctx cancelled during backoff
			}
		}

		body, transient, err := t.doRequest(ctx, reqURL)
		if err == nil {
			return body, nil
		}
		if !transient {
			return nil, err // permanent — do not retry
		}
		lastTransient = err
	}
	return nil, lastTransient // retries exhausted; err is already ErrArxivRateLimit/Unavailable
}

// doRequest executes one HTTP request. transient=true means the caller may
// retry (429 or 5xx); false means the error is permanent.
func (t *DiscoveryTool) doRequest(ctx context.Context, reqURL string) (body []byte, transient bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrArxivUnavailable, err)
	}
	req.Header.Set("User-Agent", t.cfg.UserAgent)

	resp, err := t.httpClient.Do(req)
	if err != nil {
		// network/timeout errors are treated as transient (worth a retry).
		return nil, true, fmt.Errorf("%w: %v", ErrArxivUnavailable, err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusOK:
		b, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, true, fmt.Errorf("%w: %v", ErrArxivUnavailable, readErr)
		}
		return b, false, nil
	case resp.StatusCode == http.StatusTooManyRequests:
		return nil, true, ErrArxivRateLimit
	case resp.StatusCode >= 500:
		return nil, true, ErrArxivUnavailable
	default:
		// other 4xx — permanent
		return nil, false, fmt.Errorf("%w: unexpected status %d", ErrArxivUnavailable, resp.StatusCode)
	}
}

// backoffFor returns base * 2^(attempt-1); base is the configured min interval
// scaled by backoffUnit (time.Second in production).
func (t *DiscoveryTool) backoffFor(attempt int) time.Duration {
	unit := t.backoffUnit
	if unit <= 0 {
		unit = time.Second
	}
	base := time.Duration(t.cfg.MinRequestIntervalSec) * unit
	if base <= 0 {
		base = unit
	}
	mult := 1 << (attempt - 1) // 1, 2, 4, ...
	return base * time.Duration(mult)
}

// sleepCtx sleeps for d unless ctx is cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// entryToPaper maps a parsed Atom entry to a Paper, normalizing whitespace and
// extracting the clean arXiv ID and PDF URL.
func entryToPaper(e arxivEntry) models.Paper {
	authors := make([]string, 0, len(e.Authors))
	for _, a := range e.Authors {
		if name := normalizeText(a.Name); name != "" {
			authors = append(authors, name)
		}
	}
	id := extractArxivID(e.ID)
	return models.Paper{
		ID:        id,
		Title:     normalizeText(e.Title),
		Authors:   authors,
		Abstract:  normalizeText(e.Summary),
		PDFURL:    pdfURL(e.Links, id),
		Published: strings.TrimSpace(e.Published),
	}
}

// extractArxivID turns an entry-id URL ("http://arxiv.org/abs/2401.12345v2")
// into the bare ID ("2401.12345"): take the segment after "/abs/", drop "vN".
func extractArxivID(rawID string) string {
	id := strings.TrimSpace(rawID)
	if i := strings.LastIndex(id, "/abs/"); i != -1 {
		id = id[i+len("/abs/"):]
	} else if i := strings.LastIndex(id, "/"); i != -1 {
		id = id[i+1:]
	}
	// strip a trailing version suffix: v1, v2, ...
	if i := strings.LastIndex(id, "v"); i > 0 {
		if _, err := fmt.Sscanf(id[i+1:], "%d", new(int)); err == nil {
			id = id[:i]
		}
	}
	return id
}

// normalizeText collapses arXiv's newline-wrapped, multi-space text into a
// single clean line.
func normalizeText(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// pdfURL prefers an explicit PDF link from the entry; if none is present it
// derives the canonical PDF URL from the arXiv ID.
func pdfURL(links []arxivLink, id string) string {
	for _, l := range links {
		if l.Type == "application/pdf" || l.Rel == "related" {
			if strings.Contains(l.Href, "/pdf/") {
				return l.Href
			}
		}
	}
	if id != "" {
		return "https://arxiv.org/pdf/" + id
	}
	return ""
}
