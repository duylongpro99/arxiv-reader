package llm

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// Shared retry policy (PRD §6), applied identically to every provider so the
// behavior can never drift between them. Providers classify their SDK errors
// into the shared sentinels; this wrapper owns the *decision to retry*.
const (
	maxRateLimitRetries   = 3 // 429: retry up to 3 times (backoff 5s → 10s → 20s)
	maxUnavailableRetries = 1 // 503: retry once after 5s
)

// retryBaseUnit scales all backoffs. It is time.Second in production; tests
// override it to milliseconds so the retry paths run instantly (mirrors
// DiscoveryTool.backoffUnit).
var retryBaseUnit = time.Second

// withRetry runs fn, retrying per the shared policy based on the sentinel fn
// returns. 429 → exponential backoff (5s,10s,20s) up to 3 attempts; 503 → one
// retry after 5s; 400 and timeout → surface immediately; unknown errors are not
// retried. A cancelled context aborts an in-progress backoff.
func withRetry(ctx context.Context, fn func() error) error {
	var rateLimitAttempts, unavailableAttempts int
	for {
		err := fn()
		if err == nil {
			return nil
		}
		switch {
		case errors.Is(err, ErrLLMRateLimit):
			if rateLimitAttempts >= maxRateLimitRetries {
				return err // retries exhausted — surface the rate-limit error
			}
			// 5s, 10s, 20s: 5 * 2^attempt.
			backoff := 5 * (1 << rateLimitAttempts) * retryBaseUnit
			rateLimitAttempts++
			slog.Warn("llm rate limited, retrying",
				"component", "llm", "attempt", rateLimitAttempts, "backoff_ms", backoff.Milliseconds())
			if serr := sleepCtx(ctx, backoff); serr != nil {
				return serr
			}
		case errors.Is(err, ErrLLMUnavailable):
			if unavailableAttempts >= maxUnavailableRetries {
				return err
			}
			unavailableAttempts++
			backoff := 5 * retryBaseUnit
			slog.Warn("llm unavailable, retrying once",
				"component", "llm", "backoff_ms", backoff.Milliseconds())
			if serr := sleepCtx(ctx, backoff); serr != nil {
				return serr
			}
		default:
			// ErrLLMBadRequest, ErrLLMTimeout, or any unmapped error: no retry.
			return err
		}
	}
}

// classifyHTTPStatus maps an HTTP status code to a shared sentinel. It is the
// common half of every provider's error mapping (DRY): 429 → rate limit; 5xx →
// unavailable (retryable); other 4xx (400 bad request, 401 auth, 404 model) →
// bad request, surfaced immediately since retrying a config/auth error is
// pointless; anything else → unavailable (retryable-safe default).
func classifyHTTPStatus(code int) error {
	switch {
	case code == http.StatusTooManyRequests:
		return ErrLLMRateLimit
	case code >= 500:
		return ErrLLMUnavailable
	case code >= 400:
		return ErrLLMBadRequest
	default:
		return ErrLLMUnavailable
	}
}

// sleepCtx sleeps for d unless ctx is cancelled first (ctx-aware, like
// DiscoveryTool's sleepCtx — duplicated here to keep the llm package self-contained).
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
