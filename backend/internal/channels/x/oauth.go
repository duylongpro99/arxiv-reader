// Package x implements channels.Channel for X (Twitter) — the hardest
// channel because publishing requires OAuth2 user-context auth, not a static
// API key. This file (oauth.go) isolates ALL token mechanics: exchanging a
// long-lived refresh token for a short-lived access token, caching it, and
// tracking X's refresh-token rotation. x.go and chunk.go never see a raw
// token beyond what's needed to attach one Authorization header.
package x

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	// defaultTokenBaseURL is the real X OAuth2 host; overridable via
	// X_TOKEN_BASE_URL so tests point at an httptest.Server instead.
	defaultTokenBaseURL = "https://api.twitter.com"
	// tokenExpirySkew is subtracted from the token's reported lifetime so a
	// refresh happens slightly before actual expiry, never after (avoids a
	// request racing an already-dead token).
	tokenExpirySkew = 60 * time.Second
	// defaultExpirySeconds is used only if X ever omits expires_in — X's
	// documented access-token lifetime is ~2h; this is a safe, conservative
	// fallback that triggers a refresh sooner rather than risking a stale
	// token being reused.
	defaultExpirySeconds = 7200
)

// tokenSource exchanges a rotating OAuth2 refresh token for short-lived
// access tokens, per X's confidential-client (client_id + client_secret)
// flow. Safe for concurrent use — Publish may be invoked concurrently across
// runs, and all fields are guarded by mu.
type tokenSource struct {
	clientID     string
	clientSecret string
	httpc        *http.Client
	tokenURL     string
	storePath    string // optional on-disk cache for the rotated refresh token; empty = in-memory only

	mu           sync.Mutex
	refreshToken string
	accessToken  string
	expiresAt    time.Time
}

// newTokenSource builds a tokenSource. If X_TOKEN_STORE is set and readable,
// its contents (the most recently rotated refresh token) take priority over
// the refreshToken passed in (which usually comes straight from .env and may
// be stale after a prior rotation) — this is what lets a process restart
// survive X's mandatory refresh-token rotation.
func newTokenSource(clientID, clientSecret, refreshToken string, httpc *http.Client) *tokenSource {
	storePath := os.Getenv("X_TOKEN_STORE")
	if storePath != "" {
		if data, err := os.ReadFile(storePath); err == nil {
			if stored := strings.TrimSpace(string(data)); stored != "" {
				refreshToken = stored
			}
		}
	}

	tokenBase := os.Getenv("X_TOKEN_BASE_URL")
	if tokenBase == "" {
		tokenBase = defaultTokenBaseURL
	}

	return &tokenSource{
		clientID:     clientID,
		clientSecret: clientSecret,
		httpc:        httpc,
		tokenURL:     tokenBase + "/2/oauth2/token",
		storePath:    storePath,
		refreshToken: refreshToken,
	}
}

// accessToken returns a live access token, refreshing it first if the cached
// one is missing or within tokenExpirySkew of expiring. Never logs or
// returns the refresh token itself.
func (t *tokenSource) accessTokenValue(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.accessToken != "" && time.Now().Before(t.expiresAt.Add(-tokenExpirySkew)) {
		return t.accessToken, nil
	}
	if err := t.refreshLocked(ctx); err != nil {
		return "", err
	}
	return t.accessToken, nil
}

// refreshLocked performs the refresh_token->access_token exchange. Caller
// must hold mu. On success, X's rotated refresh_token (if present in the
// response — X always rotates it) replaces the in-memory one and is
// best-effort persisted to storePath so a restart doesn't fall back to a
// now-invalid token from .env.
func (t *tokenSource) refreshLocked(ctx context.Context) error {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", t.refreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("x: building token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(t.clientID, t.clientSecret) // confidential-client auth per X's OAuth2 spec

	resp, err := t.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("x: token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		discard(resp.Body)
		// Status-only error: the response body could echo request details
		// (never surfaced), and the tokens themselves are never included.
		return fmt.Errorf("x: token refresh rejected with status %d (check X_CLIENT_ID/X_CLIENT_SECRET/X_REFRESH_TOKEN)", resp.StatusCode)
	}

	var parsed struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&parsed); err != nil {
		return fmt.Errorf("x: decoding token response: %w", err)
	}
	if parsed.AccessToken == "" {
		return errors.New("x: token response missing access_token")
	}

	t.accessToken = parsed.AccessToken
	expiresIn := parsed.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = defaultExpirySeconds
	}
	t.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)

	if parsed.RefreshToken != "" && parsed.RefreshToken != t.refreshToken {
		t.refreshToken = parsed.RefreshToken
		t.persistRefreshToken()
	}
	return nil
}

// persistRefreshToken best-effort writes the current refresh token to
// storePath (mode 0600). A write failure never fails the publish — the
// rotated token is already live in memory for this process's lifetime; only
// a subsequent restart would lose it, an acceptable degradation documented
// in docs/channel-x-oauth-setup.md.
func (t *tokenSource) persistRefreshToken() {
	if t.storePath == "" {
		return
	}
	_ = os.WriteFile(t.storePath, []byte(t.refreshToken), 0o600)
}
