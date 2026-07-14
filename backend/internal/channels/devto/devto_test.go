package devto

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maritime-ds/arxiv-reader/internal/channels"
	"github.com/maritime-ds/arxiv-reader/internal/config"
)

// TestNewMissingAPIKey asserts New fails fast (never a half-configured
// Channel) when the required env var is absent.
func TestNewMissingAPIKey(t *testing.T) {
	t.Setenv("DEVTO_API_KEY", "")
	if _, err := New(&config.Config{}); err == nil {
		t.Fatal("New() with no DEVTO_API_KEY: want error, got nil")
	}
}

// TestPublishSuccess drives Publish against an httptest.Server standing in
// for dev.to (via DEVTO_BASE_URL), asserting request shape (method, api-key
// header, article JSON body, sanitized/capped tags) and the parsed result.
func TestPublishSuccess(t *testing.T) {
	var gotMethod, gotAPIKey, gotContentType string
	var gotBody map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAPIKey = r.Header.Get("api-key")
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("server: decoding request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"url":"https://dev.to/user/my-article-123","id":123}`))
	}))
	defer server.Close()

	t.Setenv("DEVTO_API_KEY", "test-api-key")
	t.Setenv("DEVTO_BASE_URL", server.URL)

	ch, err := New(&config.Config{})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	content := channels.GeneratedContent{
		Category: channels.Longform,
		Title:    "Understanding Transformers",
		Body:     "# Intro\n\nSome markdown body.",
		Tags:     []string{"AI Research!", "Go", "  ML  ", "python", "extra-dropped"},
	}

	result, err := ch.Publish(context.Background(), content)
	if err != nil {
		t.Fatalf("Publish() error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want %q", gotMethod, http.MethodPost)
	}
	if gotAPIKey != "test-api-key" {
		t.Errorf("api-key header = %q, want %q", gotAPIKey, "test-api-key")
	}
	if !strings.Contains(gotContentType, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}

	article, ok := gotBody["article"].(map[string]any)
	if !ok {
		t.Fatalf("request body missing article object: %v", gotBody)
	}
	if article["title"] != content.Title {
		t.Errorf("title = %v, want %v", article["title"], content.Title)
	}
	if article["body_markdown"] != content.Body {
		t.Errorf("body_markdown = %v, want %v", article["body_markdown"], content.Body)
	}
	if published, _ := article["published"].(bool); !published {
		t.Errorf("published = %v, want true", article["published"])
	}
	tags, _ := article["tags"].([]any)
	if len(tags) != maxTags {
		t.Errorf("tags len = %d, want %d (sanitized + capped)", len(tags), maxTags)
	}

	if result.ExternalURL != "https://dev.to/user/my-article-123" {
		t.Errorf("ExternalURL = %q", result.ExternalURL)
	}
	if result.ExternalID != "123" {
		t.Errorf("ExternalID = %q, want %q", result.ExternalID, "123")
	}
}

// TestPublishAuthError asserts a 401 maps to a clear, key-free error message —
// crucially, the raw response body (which could echo request details) must
// never appear in the error.
func TestPublishAuthError(t *testing.T) {
	const bodyLeak = "super-secret-request-echo"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"` + bodyLeak + `"}`))
	}))
	defer server.Close()

	t.Setenv("DEVTO_API_KEY", "bad-key")
	t.Setenv("DEVTO_BASE_URL", server.URL)

	ch, err := New(&config.Config{})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	_, err = ch.Publish(context.Background(), channels.GeneratedContent{Title: "T", Body: "B"})
	if err == nil {
		t.Fatal("Publish() with 401 response: want error, got nil")
	}
	if !strings.Contains(err.Error(), "DEVTO_API_KEY") {
		t.Errorf("error %q does not mention DEVTO_API_KEY", err.Error())
	}
	if strings.Contains(err.Error(), bodyLeak) {
		t.Errorf("error %q leaks the raw response body", err.Error())
	}
}

// TestValidateBounds covers the fast-fail local checks Publish runs before
// any network call.
func TestValidateBounds(t *testing.T) {
	t.Setenv("DEVTO_API_KEY", "k")
	ch, err := New(&config.Config{})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}

	cases := []struct {
		name    string
		content channels.GeneratedContent
		wantErr bool
	}{
		{"valid", channels.GeneratedContent{Title: "T", Body: "B"}, false},
		{"empty title", channels.GeneratedContent{Title: "", Body: "B"}, true},
		{"blank title", channels.GeneratedContent{Title: "   ", Body: "B"}, true},
		{"empty body", channels.GeneratedContent{Title: "T", Body: ""}, true},
		{"title too long", channels.GeneratedContent{Title: strings.Repeat("a", maxTitleLen+1), Body: "B"}, true},
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
