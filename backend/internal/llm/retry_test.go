package llm

import (
	"context"
	"errors"
	"testing"
	"time"
)

// scriptedFn returns the queued errors in order, one per call, then nil.
func scriptedFn(errs ...error) func() error {
	i := 0
	return func() error {
		if i < len(errs) {
			e := errs[i]
			i++
			return e
		}
		return nil
	}
}

func TestMain(m *testing.M) {
	// Shrink backoff so tests never sleep for real seconds.
	retryBaseUnit = time.Microsecond
	m.Run()
}

func TestWithRetryRateLimitThenSuccess(t *testing.T) {
	fn := scriptedFn(ErrLLMRateLimit, ErrLLMRateLimit, ErrLLMRateLimit) // 3 fails, then success
	if err := withRetry(context.Background(), fn); err != nil {
		t.Fatalf("expected success after 3 rate-limit retries, got %v", err)
	}
}

func TestWithRetryRateLimitExhausted(t *testing.T) {
	// 4 consecutive rate limits: 1 initial + 3 retries all fail → surface sentinel.
	fn := scriptedFn(ErrLLMRateLimit, ErrLLMRateLimit, ErrLLMRateLimit, ErrLLMRateLimit)
	err := withRetry(context.Background(), fn)
	if !errors.Is(err, ErrLLMRateLimit) {
		t.Fatalf("expected ErrLLMRateLimit after exhaustion, got %v", err)
	}
}

func TestWithRetryUnavailableRetriedOnce(t *testing.T) {
	fn := scriptedFn(ErrLLMUnavailable) // one 503, then success
	if err := withRetry(context.Background(), fn); err != nil {
		t.Fatalf("expected success after one 503 retry, got %v", err)
	}
}

func TestWithRetryUnavailableExhausted(t *testing.T) {
	fn := scriptedFn(ErrLLMUnavailable, ErrLLMUnavailable) // exceeds the single retry
	err := withRetry(context.Background(), fn)
	if !errors.Is(err, ErrLLMUnavailable) {
		t.Fatalf("expected ErrLLMUnavailable after exhaustion, got %v", err)
	}
}

func TestWithRetryBadRequestImmediate(t *testing.T) {
	calls := 0
	fn := func() error { calls++; return ErrLLMBadRequest }
	err := withRetry(context.Background(), fn)
	if !errors.Is(err, ErrLLMBadRequest) {
		t.Fatalf("expected ErrLLMBadRequest, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("400 must not retry; expected 1 call, got %d", calls)
	}
}

func TestWithRetryTimeoutImmediate(t *testing.T) {
	calls := 0
	fn := func() error { calls++; return ErrLLMTimeout }
	if err := withRetry(context.Background(), fn); !errors.Is(err, ErrLLMTimeout) {
		t.Fatalf("expected ErrLLMTimeout, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("timeout must not retry; expected 1 call, got %d", calls)
	}
}

func TestWithRetryCtxCancelDuringBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancelled up front — the first backoff must abort
	fn := scriptedFn(ErrLLMRateLimit, ErrLLMRateLimit)
	if err := withRetry(ctx, fn); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}
