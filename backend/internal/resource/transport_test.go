package resource

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// discoveryRetry mirrors arXiv's discovery policy (timeout transient).
func discoveryRetry() RetrySpec {
	return RetrySpec{MaxRetries: 3, On: []string{"429", "5xx", "network", "timeout"}, BackoffBaseSeconds: 1, BackoffFactor: 2}
}

// contentRetry mirrors arXiv's content policy (timeout terminal — no "timeout").
func contentRetry() RetrySpec {
	return RetrySpec{MaxRetries: 3, On: []string{"429", "5xx", "network"}, BackoffBaseSeconds: 1, BackoffFactor: 2}
}

func fastTransport() *transport {
	return &transport{client: &http.Client{Timeout: 2 * time.Second}, backoffUnit: time.Millisecond}
}

func TestTransportHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "test-agent" {
			t.Errorf("User-Agent = %q", got)
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	body, status, err := fastTransport().fetch(context.Background(), fetchRequest{
		url: srv.URL, headers: map[string]string{"User-Agent": "test-agent"}, retry: discoveryRetry(),
	}, nil)
	if err != nil || status != 200 || string(body) != "ok" {
		t.Fatalf("fetch = %q, %d, %v", body, status, err)
	}
}

func TestTransportRetriesThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			_, _ = w.Write([]byte("ok"))
		}
	}))
	defer srv.Close()

	var attempts []int
	_, _, err := fastTransport().fetch(context.Background(), fetchRequest{url: srv.URL, retry: discoveryRetry()},
		func(a int) { attempts = append(attempts, a) })
	if err != nil {
		t.Fatalf("want success after retries, got %v", err)
	}
	if len(attempts) != 2 || attempts[0] != 1 || attempts[1] != 2 {
		t.Fatalf("onRetry attempts = %v, want [1 2]", attempts)
	}
}

func TestTransportRateLimitExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	_, _, err := fastTransport().fetch(context.Background(), fetchRequest{url: srv.URL, retry: discoveryRetry()}, nil)
	if !errors.Is(err, ErrTransportRateLimit) {
		t.Fatalf("want ErrTransportRateLimit, got %v", err)
	}
}

func TestTransportServerErrorExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	_, _, err := fastTransport().fetch(context.Background(), fetchRequest{url: srv.URL, retry: discoveryRetry()}, nil)
	if !errors.Is(err, ErrTransportUnavailable) {
		t.Fatalf("want ErrTransportUnavailable, got %v", err)
	}
}

func TestTransportPermanent4xxNotRetried(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	_, status, err := fastTransport().fetch(context.Background(), fetchRequest{url: srv.URL, retry: discoveryRetry()}, nil)
	if err == nil || status != http.StatusBadRequest {
		t.Fatalf("want permanent 400, got status=%d err=%v", status, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("4xx should not retry, calls=%d", calls.Load())
	}
}

func TestTransport404ReturnsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	_, status, _ := fastTransport().fetch(context.Background(), fetchRequest{url: srv.URL, retry: contentRetry()}, nil)
	if status != http.StatusNotFound {
		t.Fatalf("want 404 status surfaced, got %d", status)
	}
}

// F14: oversize body is rejected, not silently truncated.
func TestTransportOversizeRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("0123456789")) // 10 bytes > cap 4
	}))
	defer srv.Close()
	_, _, err := fastTransport().fetch(context.Background(), fetchRequest{url: srv.URL, retry: contentRetry(), maxBytes: 4}, nil)
	if !errors.Is(err, ErrTransportTooLarge) {
		t.Fatalf("want ErrTransportTooLarge, got %v", err)
	}
}

func TestTransportContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte("late"))
	}))
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := fastTransport().fetch(ctx, fetchRequest{url: srv.URL, retry: discoveryRetry()}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

// F13: a secret in the query string never appears in a retry log / error.
func TestRedactURLDropsQuery(t *testing.T) {
	got := redactURL("https://arxiv.org/api/query?search_query=cat:cs.AI&token=SECRET")
	if got != "https://arxiv.org/api/query" {
		t.Fatalf("redactURL leaked query: %q", got)
	}
}
