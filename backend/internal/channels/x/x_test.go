package x

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/maritime-ds/arxiv-reader/internal/channels"
	"github.com/maritime-ds/arxiv-reader/internal/config"
)

// setXEnv wires the three required credential env vars plus the token/tweet
// base URL overrides needed to point the Channel at local httptest servers.
func setXEnv(t *testing.T, tokenServerURL, tweetServerURL string) {
	t.Helper()
	t.Setenv("X_CLIENT_ID", "test-client-id")
	t.Setenv("X_CLIENT_SECRET", "test-client-secret")
	t.Setenv("X_REFRESH_TOKEN", "test-refresh-token")
	t.Setenv("X_TOKEN_BASE_URL", tokenServerURL)
	t.Setenv("X_API_BASE_URL", tweetServerURL)
}

// newFakeXServers stands up a token endpoint (always returns a fixed access
// token) and a tweets endpoint driven by handleTweet, wired together via env
// vars so New() picks them up.
func newFakeXServers(t *testing.T, handleTweet http.HandlerFunc) (tokenServer, tweetServer *httptest.Server) {
	t.Helper()
	tokenServer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tokenResponse{AccessToken: "test-access-token", ExpiresIn: 3600})
	}))
	tweetServer = httptest.NewServer(handleTweet)
	setXEnv(t, tokenServer.URL, tweetServer.URL)
	t.Cleanup(func() {
		tokenServer.Close()
		tweetServer.Close()
	})
	return tokenServer, tweetServer
}

// TestNewMissingCredentials asserts New fails fast, naming every missing var,
// rather than returning a half-configured Channel.
func TestNewMissingCredentials(t *testing.T) {
	t.Setenv("X_CLIENT_ID", "")
	t.Setenv("X_CLIENT_SECRET", "")
	t.Setenv("X_REFRESH_TOKEN", "")

	_, err := New(&config.Config{})
	if err == nil {
		t.Fatal("New() with no credentials: want error, got nil")
	}
	for _, name := range []string{"X_CLIENT_ID", "X_CLIENT_SECRET", "X_REFRESH_TOKEN"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not name missing var %s", err.Error(), name)
		}
	}
}

// TestPublishThreadChaining drives Publish against a fake tweets endpoint
// with a body long enough to require >= 3 tweets, asserting: every request
// carries the Bearer access token, each reply after the first chains
// in_reply_to_tweet_id to the PREVIOUS tweet's id (a real thread, not
// unrelated tweets), and the returned PublishResult points at the first
// tweet.
func TestPublishThreadChaining(t *testing.T) {
	var mu sync.Mutex
	var gotAuthHeaders []string
	var gotBodies []map[string]any
	var nextID int64

	_, _ = newFakeXServers(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/2/tweets" {
			t.Errorf("tweet request path = %q, want /2/tweets", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decoding tweet request body: %v", err)
		}

		mu.Lock()
		gotAuthHeaders = append(gotAuthHeaders, r.Header.Get("Authorization"))
		gotBodies = append(gotBodies, body)
		nextID++
		id := nextID
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": strconv.FormatInt(id, 10)}})
	})

	ch, err := New(&config.Config{})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	sentence := "The quick brown fox jumps over the lazy dog again and once more today."
	content := channels.GeneratedContent{
		Category: channels.Brief,
		Title:    "Brief",
		Body:     strings.Repeat(sentence+" ", 15), // forces multiple tweets
	}

	result, err := ch.Publish(context.Background(), content)
	if err != nil {
		t.Fatalf("Publish() error: %v", err)
	}

	if len(gotBodies) < 3 {
		t.Fatalf("posted %d tweets, want >= 3 to exercise chaining", len(gotBodies))
	}
	for i, hdr := range gotAuthHeaders {
		if hdr != "Bearer test-access-token" {
			t.Errorf("request %d Authorization = %q, want Bearer test-access-token", i, hdr)
		}
	}

	// First tweet must have no reply field.
	if _, has := gotBodies[0]["reply"]; has {
		t.Errorf("first tweet request has a reply field: %v", gotBodies[0])
	}
	// Every subsequent tweet must reply to the PREVIOUS tweet's id (1-indexed
	// ids assigned in posting order by the fake server).
	for i := 1; i < len(gotBodies); i++ {
		reply, ok := gotBodies[i]["reply"].(map[string]any)
		if !ok {
			t.Fatalf("tweet %d missing reply object: %v", i, gotBodies[i])
		}
		wantPrevID := strconv.Itoa(i) // tweet i-1 (1-indexed id == i)
		if reply["in_reply_to_tweet_id"] != wantPrevID {
			t.Errorf("tweet %d in_reply_to_tweet_id = %v, want %q", i, reply["in_reply_to_tweet_id"], wantPrevID)
		}
	}

	if result.ExternalID != "1" {
		t.Errorf("ExternalID = %q, want %q (first tweet)", result.ExternalID, "1")
	}
	if result.ExternalURL != "https://x.com/i/web/status/1" {
		t.Errorf("ExternalURL = %q, want https://x.com/i/web/status/1", result.ExternalURL)
	}
}

// TestPublishPartialFailure asserts that when segment 2 of a multi-tweet
// thread fails after segment 1 already posted, Publish returns an error
// naming which segment failed and states the thread is partially live — it
// must NOT attempt to delete the already-posted tweet.
func TestPublishPartialFailure(t *testing.T) {
	var calls int32
	_, _ = newFakeXServers(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": "111"}})
			return
		}
		// Second segment fails.
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	})

	ch, err := New(&config.Config{})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	sentence := "Another sentence that is reasonably long to help force multiple tweets here."
	content := channels.GeneratedContent{
		Category: channels.Brief,
		Title:    "Brief",
		Body:     strings.Repeat(sentence+" ", 15),
	}

	_, err = ch.Publish(context.Background(), content)
	if err == nil {
		t.Fatal("Publish() with failing 2nd segment: want error, got nil")
	}
	if !strings.Contains(err.Error(), "partially posted") {
		t.Errorf("error %q does not mention partial posting", err.Error())
	}
	if !strings.Contains(err.Error(), "segment 2") {
		t.Errorf("error %q does not name the failed segment", err.Error())
	}
	if strings.Contains(err.Error(), "boom") {
		t.Errorf("error %q leaks the raw response body", err.Error())
	}
}

// TestPublishAuthError asserts a 401 from the tweets endpoint maps to a
// clear, credential-naming error on the FIRST segment (no partial-thread
// language, since nothing posted yet).
func TestPublishAuthError(t *testing.T) {
	newFakeXServers(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"nope"}`))
	})

	ch, err := New(&config.Config{})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	_, err = ch.Publish(context.Background(), channels.GeneratedContent{Category: channels.Brief, Body: "short brief"})
	if err == nil {
		t.Fatal("Publish() with 401: want error, got nil")
	}
	if !strings.Contains(err.Error(), "X_CLIENT_ID") {
		t.Errorf("error %q does not mention credentials", err.Error())
	}
}

// TestValidateBounds covers Validate's fast local checks.
func TestValidateBounds(t *testing.T) {
	newFakeXServers(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("Validate must never make a network call")
	})
	ch, err := New(&config.Config{})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	cases := []struct {
		name    string
		content channels.GeneratedContent
		wantErr bool
	}{
		{"valid", channels.GeneratedContent{Body: "a brief"}, false},
		{"empty body", channels.GeneratedContent{Body: ""}, true},
		{"blank body", channels.GeneratedContent{Body: "   "}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ch.Validate(tc.content)
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
