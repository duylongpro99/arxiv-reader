package resource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"time"
)

// Generic transport sentinels replace the old arXiv-specific ErrArxiv*. They are
// bare (never wrap the underlying url.Error) so a resolved URL, query string, or
// header value can never leak into an error string (F13). The response body is
// likewise never embedded (F14).
var (
	ErrTransportRateLimit   = errors.New("rate limit exceeded after retries")
	ErrTransportUnavailable = errors.New("upstream unavailable")
	ErrTransportTimeout     = errors.New("request timed out")
	ErrTransportTooLarge    = errors.New("response too large")
)

// transport is the generic HTTP+retry engine stage, parameterized by a per-call
// FetchSpec (url/headers/retry/timeout) instead of a hardcoded config. backoffUnit
// is time.Second in production; tests shrink it to keep retry paths fast.
type transport struct {
	client      *http.Client
	backoffUnit time.Duration
}

// fetchRequest is one resolved request: everything transport needs, with no
// resource knowledge.
type fetchRequest struct {
	method   string
	url      string
	headers  map[string]string
	retry    RetrySpec
	maxBytes int64
}

// fetch performs the request, retrying transient failures per the FetchSpec's
// retry policy with exponential backoff, aborting promptly on ctx cancellation.
// It returns the body, the final HTTP status (needed for the content 404 path),
// and an error. onRetry (nil-safe) fires per transient retry.
func (t *transport) fetch(ctx context.Context, fr fetchRequest, onRetry func(attempt int)) ([]byte, int, error) {
	var lastTransient error
	var lastStatus int
	for attempt := 0; attempt <= fr.retry.MaxRetries; attempt++ {
		if attempt > 0 {
			backoff := t.backoffFor(fr.retry, attempt)
			if onRetry != nil {
				onRetry(attempt)
			}
			// Log host+path+status only — never the query string or headers (F13).
			slog.Warn("fetch failed, retrying",
				"component", "transport",
				"attempt", attempt,
				"backoff_ms", backoff.Milliseconds(),
				"url", redactURL(fr.url),
				"status", lastStatus,
			)
			if err := sleepCtx(ctx, backoff); err != nil {
				return nil, 0, err // ctx cancelled during backoff
			}
		}
		body, status, transient, err := t.do(ctx, fr)
		if err == nil {
			return body, status, nil
		}
		if !transient {
			return nil, status, err // permanent — do not retry
		}
		lastTransient, lastStatus = err, status
	}
	return nil, lastStatus, lastTransient
}

// do executes one request. transient=true means the caller may retry. Errors are
// bare sentinels (no wrapping of the url.Error) to prevent URL/secret leakage.
func (t *transport) do(ctx context.Context, fr fetchRequest) (body []byte, status int, transient bool, err error) {
	method := fr.method
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequestWithContext(ctx, method, fr.url, nil)
	if err != nil {
		return nil, 0, false, ErrTransportUnavailable
	}
	for k, v := range fr.headers {
		req.Header.Set(k, v)
	}

	resp, err := t.client.Do(req)
	if err != nil {
		switch {
		case errors.Is(err, context.Canceled):
			return nil, 0, false, ctx.Err() // abort, never retried
		case errors.Is(err, context.DeadlineExceeded):
			// Per-FetchSpec timeout policy (F5): when "timeout" is listed the
			// timeout is a transient network error (discovery — surfaces as
			// "unavailable" after retries, matching the old tool); otherwise it is
			// a terminal, distinct timeout (content — old ErrPaperHTMLTimeout).
			if fr.retry.has("timeout") {
				return nil, 0, true, ErrTransportUnavailable
			}
			return nil, 0, false, ErrTransportTimeout
		default:
			// Other network error: transient only when "network" is listed.
			return nil, 0, fr.retry.has("network"), ErrTransportUnavailable
		}
	}
	defer resp.Body.Close()
	status = resp.StatusCode

	switch {
	case status == http.StatusOK:
		b, oversize, readErr := readLimited(resp.Body, fr.maxBytes)
		if readErr != nil {
			// A body-read failure (incl. a Client.Timeout that fires mid-body) is a
			// network-class error: retryable only when the FetchSpec lists "network"
			// (M1). arXiv content lists it, so this stays transient there — matching
			// the old tool, which retried body-read errors too — while a resource
			// that omits "network" gets a terminal failure instead of a forced retry.
			return nil, status, fr.retry.has("network"), ErrTransportUnavailable
		}
		if oversize {
			return nil, status, false, ErrTransportTooLarge
		}
		return b, status, false, nil
	case status == http.StatusTooManyRequests:
		return nil, status, fr.retry.transientStatus(status), ErrTransportRateLimit
	case status >= 500:
		return nil, status, fr.retry.transientStatus(status), ErrTransportUnavailable
	default:
		// Other 4xx (including 404) — permanent. The caller inspects status to
		// special-case 404 before checking the error.
		return nil, status, false, ErrTransportUnavailable
	}
}

// backoffFor returns base * factor^(attempt-1); base is BackoffBaseSeconds scaled
// by backoffUnit (time.Second in prod). Defaults mirror the old tool (base=min
// interval, factor=2 → 3s,6s,12s).
func (t *transport) backoffFor(r RetrySpec, attempt int) time.Duration {
	unit := t.backoffUnit
	if unit <= 0 {
		unit = time.Second
	}
	base := time.Duration(r.BackoffBaseSeconds) * unit
	if base <= 0 {
		base = unit
	}
	factor := r.BackoffFactor
	if factor <= 0 {
		factor = 2
	}
	mult := 1
	for i := 1; i < attempt; i++ {
		mult *= factor
	}
	return base * time.Duration(mult)
}

// has reports whether a retry condition ("429"/"5xx"/"network"/"timeout") is
// listed for this FetchSpec.
func (r RetrySpec) has(cond string) bool {
	return slices.Contains(r.On, cond)
}

// transientStatus reports whether an HTTP status is retryable under this policy.
func (r RetrySpec) transientStatus(code int) bool {
	switch {
	case code == http.StatusTooManyRequests:
		return r.has("429")
	case code >= 500:
		return r.has("5xx")
	}
	return false
}

// readLimited reads r under an OOM guard. LimitReader silently truncates at the
// cap, so a body AT the cap is indistinguishable from a truncated one; we read
// cap+1 and flag oversize when the extra byte materializes (verbatim from the old
// content tool, now applied to discovery too per F14). maxBytes<=0 disables it.
func readLimited(r io.Reader, maxBytes int64) (body []byte, oversize bool, err error) {
	if maxBytes <= 0 {
		b, err := io.ReadAll(r)
		return b, false, err
	}
	b, err := io.ReadAll(io.LimitReader(r, maxBytes+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(b)) > maxBytes {
		return nil, true, nil
	}
	return b, false, nil
}

// redactURL returns scheme://host/path — dropping the query string and any
// resolved secrets it might carry (F13).
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "[unparseable url]"
	}
	return u.Scheme + "://" + u.Host + u.Path
}

// boundedSameHost is the content-fetch redirect policy (F4/V3 minimal SSRF): cap
// the redirect chain and require every hop to stay on the declared base
// host+scheme. A clear seam for the deferred private-IP/metadata denylist.
func boundedSameHost(scheme, host string, max int) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= max {
			return fmt.Errorf("stopped after %d redirects", max)
		}
		if req.URL.Host != host || req.URL.Scheme != scheme {
			return fmt.Errorf("redirect to disallowed host")
		}
		return nil
	}
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
