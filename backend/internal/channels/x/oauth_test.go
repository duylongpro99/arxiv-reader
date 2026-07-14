package x

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// tokenResponse is the shape a fake X token endpoint returns.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in,omitempty"`
}

// newFakeTokenServer builds an httptest server standing in for X's
// /2/oauth2/token endpoint, asserting Basic auth + refresh_token grant shape
// and counting calls so tests can assert caching behavior.
func newFakeTokenServer(t *testing.T, respond func(callCount int) tokenResponse) (*httptest.Server, *int32) {
	t.Helper()
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)

		if r.URL.Path != "/2/oauth2/token" {
			t.Errorf("token request path = %q, want /2/oauth2/token", r.URL.Path)
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != "test-client-id" || pass != "test-client-secret" {
			t.Errorf("token request missing/wrong Basic auth: user=%q pass=%q ok=%v", user, pass, ok)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parsing token request form: %v", err)
		}
		if got := r.PostForm.Get("grant_type"); got != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", got)
		}

		resp := respond(int(n))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	return server, &calls
}

// TestTokenSourceRefreshAndCache asserts the first accessTokenValue call
// hits the token endpoint and a second immediate call reuses the cached
// token without a second HTTP round trip.
func TestTokenSourceRefreshAndCache(t *testing.T) {
	server, calls := newFakeTokenServer(t, func(int) tokenResponse {
		return tokenResponse{AccessToken: "access-1", RefreshToken: "refresh-2", ExpiresIn: 3600}
	})
	defer server.Close()

	ts := newTokenSourceForTest(t, server.URL, "refresh-1")

	got, err := ts.accessTokenValue(context.Background())
	if err != nil {
		t.Fatalf("accessTokenValue() error: %v", err)
	}
	if got != "access-1" {
		t.Errorf("access token = %q, want access-1", got)
	}
	if atomic.LoadInt32(calls) != 1 {
		t.Fatalf("token endpoint calls = %d, want 1", *calls)
	}

	got2, err := ts.accessTokenValue(context.Background())
	if err != nil {
		t.Fatalf("second accessTokenValue() error: %v", err)
	}
	if got2 != "access-1" {
		t.Errorf("cached access token = %q, want access-1 (reused, not refreshed)", got2)
	}
	if atomic.LoadInt32(calls) != 1 {
		t.Fatalf("token endpoint calls after cached read = %d, want still 1", *calls)
	}
}

// TestTokenSourceRefreshesWhenExpired asserts a near-expiry cached token
// triggers a second refresh call rather than being reused past its skew
// window.
func TestTokenSourceRefreshesWhenExpired(t *testing.T) {
	server, calls := newFakeTokenServer(t, func(n int) tokenResponse {
		return tokenResponse{AccessToken: "access-" + string(rune('0'+n)), RefreshToken: "refresh-rotated", ExpiresIn: 3600}
	})
	defer server.Close()

	ts := newTokenSourceForTest(t, server.URL, "refresh-1")
	// Force an already-expired cache entry directly rather than sleeping.
	ts.mu.Lock()
	ts.accessToken = "stale-access"
	ts.expiresAt = time.Now().Add(-time.Hour)
	ts.mu.Unlock()

	got, err := ts.accessTokenValue(context.Background())
	if err != nil {
		t.Fatalf("accessTokenValue() error: %v", err)
	}
	if got == "stale-access" {
		t.Error("accessTokenValue() returned the stale token instead of refreshing")
	}
	if atomic.LoadInt32(calls) != 1 {
		t.Fatalf("token endpoint calls = %d, want 1 refresh", *calls)
	}
}

// TestTokenSourceCapturesRotatedRefreshToken asserts X's rotated
// refresh_token from the response replaces the in-memory one, and (when
// X_TOKEN_STORE points at a file) is persisted there with 0600 perms.
func TestTokenSourceCapturesRotatedRefreshToken(t *testing.T) {
	server, _ := newFakeTokenServer(t, func(int) tokenResponse {
		return tokenResponse{AccessToken: "access-1", RefreshToken: "rotated-refresh-token", ExpiresIn: 3600}
	})
	defer server.Close()

	storePath := filepath.Join(t.TempDir(), "x-refresh-token")
	t.Setenv("X_TOKEN_STORE", storePath)

	ts := newTokenSourceForTest(t, server.URL, "original-refresh-token")
	if _, err := ts.accessTokenValue(context.Background()); err != nil {
		t.Fatalf("accessTokenValue() error: %v", err)
	}

	ts.mu.Lock()
	gotRefresh := ts.refreshToken
	ts.mu.Unlock()
	if gotRefresh != "rotated-refresh-token" {
		t.Errorf("in-memory refresh token = %q, want rotated-refresh-token", gotRefresh)
	}

	data, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("reading token store file: %v", err)
	}
	if strings.TrimSpace(string(data)) != "rotated-refresh-token" {
		t.Errorf("persisted token store contents = %q, want rotated-refresh-token", string(data))
	}
	info, err := os.Stat(storePath)
	if err != nil {
		t.Fatalf("stat token store file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token store file perms = %o, want 0600", perm)
	}
}

// TestNewTokenSourceReadsExistingStore asserts a pre-existing X_TOKEN_STORE
// file (simulating a rotated token surviving a process restart) takes
// priority over the refreshToken passed to newTokenSource (which usually
// comes from a possibly-stale .env value).
func TestNewTokenSourceReadsExistingStore(t *testing.T) {
	storePath := filepath.Join(t.TempDir(), "x-refresh-token")
	if err := os.WriteFile(storePath, []byte("token-from-disk\n"), 0o600); err != nil {
		t.Fatalf("seeding token store file: %v", err)
	}
	t.Setenv("X_TOKEN_STORE", storePath)

	ts := newTokenSource("cid", "csecret", "token-from-env", &http.Client{})
	if ts.refreshToken != "token-from-disk" {
		t.Errorf("refreshToken = %q, want token-from-disk (disk overrides stale env value)", ts.refreshToken)
	}
}

// TestTokenSourceRefreshErrorStatus asserts a non-200 token response yields
// a clear, credential-naming error without leaking any response body.
func TestTokenSourceRefreshErrorStatus(t *testing.T) {
	const bodyLeak = "secret-echo-in-error-body"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"` + bodyLeak + `"}`))
	}))
	defer server.Close()

	ts := newTokenSourceForTest(t, server.URL, "refresh-1")
	_, err := ts.accessTokenValue(context.Background())
	if err == nil {
		t.Fatal("accessTokenValue() with 401 response: want error, got nil")
	}
	if !strings.Contains(err.Error(), "X_CLIENT_ID") {
		t.Errorf("error %q does not mention credentials", err.Error())
	}
	if strings.Contains(err.Error(), bodyLeak) {
		t.Errorf("error %q leaks the raw response body", err.Error())
	}
}

// newTokenSourceForTest builds a tokenSource pointed at a local test server
// via X_TOKEN_BASE_URL, with fixed test client credentials.
func newTokenSourceForTest(t *testing.T, serverURL, refreshToken string) *tokenSource {
	t.Helper()
	t.Setenv("X_TOKEN_BASE_URL", serverURL)
	ts := newTokenSource("test-client-id", "test-client-secret", refreshToken, &http.Client{})
	if ts.tokenURL != serverURL+"/2/oauth2/token" {
		t.Fatalf("tokenURL = %q, want %s/2/oauth2/token", ts.tokenURL, serverURL)
	}
	return ts
}

// TestTokenSourceConcurrentAccess drives many goroutines through
// accessTokenValue simultaneously (starting with no cached token, forcing a
// race to be the one that refreshes) and asserts: no data race (run with
// -race), exactly one token-endpoint call, and every goroutine observes the
// same resulting access token — proving the mutex correctly serializes the
// refresh instead of letting concurrent Publish calls double-refresh.
func TestTokenSourceConcurrentAccess(t *testing.T) {
	server, calls := newFakeTokenServer(t, func(int) tokenResponse {
		return tokenResponse{AccessToken: "access-concurrent", ExpiresIn: 3600}
	})
	defer server.Close()

	ts := newTokenSourceForTest(t, server.URL, "refresh-1")

	const goroutines = 20
	var wg sync.WaitGroup
	results := make([]string, goroutines)
	errs := make([]error, goroutines)
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = ts.accessTokenValue(context.Background())
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: accessTokenValue() error: %v", i, err)
		}
		if results[i] != "access-concurrent" {
			t.Errorf("goroutine %d: access token = %q, want access-concurrent", i, results[i])
		}
	}
	if got := atomic.LoadInt32(calls); got != 1 {
		t.Errorf("token endpoint calls under concurrent access = %d, want exactly 1", got)
	}
}

// TestNewTokenSourceDefaultTokenBaseURL asserts the real X host is used when
// X_TOKEN_BASE_URL is unset, and that it's built from a valid URL.
func TestNewTokenSourceDefaultTokenBaseURL(t *testing.T) {
	t.Setenv("X_TOKEN_BASE_URL", "")
	ts := newTokenSource("cid", "csecret", "rt", &http.Client{})
	if ts.tokenURL != defaultTokenBaseURL+"/2/oauth2/token" {
		t.Errorf("tokenURL = %q, want default", ts.tokenURL)
	}
	if _, err := url.Parse(ts.tokenURL); err != nil {
		t.Errorf("tokenURL is not a valid URL: %v", err)
	}
}
