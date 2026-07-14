package x

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/channels"
	"github.com/maritime-ds/arxiv-reader/internal/config"
)

const (
	// defaultAPIBaseURL is the real X API v2 host; overridable via
	// X_API_BASE_URL so tests point at an httptest.Server instead.
	defaultAPIBaseURL = "https://api.twitter.com"
	// defaultTimeout applies when cfg.Agent.RequestTimeoutSec is unset (0).
	defaultTimeout = 30 * time.Second
	// maxResponseBytes bounds how much of any response body is ever read —
	// drained/discarded on error paths (never echoed into an error, since a
	// response could reflect request headers) and decoded on success.
	maxResponseBytes = 64 * 1024
)

// init self-registers this channel with the registry (see
// channels.Register's doc comment for why this replaces a direct switch
// case). The binary must still blank-import this package for init to run —
// see cmd/server/main.go.
func init() {
	channels.Register("x", New)
}

// Channel is the X (Twitter) implementation of channels.Channel. It consumes
// channels.Brief and mechanically expands it into a numbered tweet thread —
// the Repurposer agent never sees anything thread-shaped.
type Channel struct {
	oauth   *tokenSource
	httpc   *http.Client
	baseURL string
}

// New builds an X Channel. Credentials are env-only (never config.yaml),
// mirroring dev.to's API-key loading — a secret must never land in a
// committed file. Missing vars fail fast with a clear, named list rather
// than a half-configured Channel surfacing a confusing error at publish time.
func New(cfg *config.Config) (channels.Channel, error) {
	clientID := os.Getenv("X_CLIENT_ID")
	clientSecret := os.Getenv("X_CLIENT_SECRET")
	refreshToken := os.Getenv("X_REFRESH_TOKEN")

	var missing []string
	if clientID == "" {
		missing = append(missing, "X_CLIENT_ID")
	}
	if clientSecret == "" {
		missing = append(missing, "X_CLIENT_SECRET")
	}
	if refreshToken == "" {
		missing = append(missing, "X_REFRESH_TOKEN")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("x: missing required env var(s): %s (see docs/channel-x-oauth-setup.md)", strings.Join(missing, ", "))
	}

	timeout := time.Duration(cfg.Agent.RequestTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	httpc := &http.Client{Timeout: timeout}

	baseURL := os.Getenv("X_API_BASE_URL")
	if baseURL == "" {
		baseURL = defaultAPIBaseURL
	}

	return &Channel{
		oauth:   newTokenSource(clientID, clientSecret, refreshToken, httpc),
		httpc:   httpc,
		baseURL: baseURL,
	}, nil
}

func (c *Channel) ID() string { return "x" }

func (c *Channel) Category() channels.Category { return channels.Brief }

// Validate confirms the body is non-empty and chunkable; the actual
// per-tweet 280-char enforcement happens inside chunk() by construction, so
// a passing Validate guarantees Publish's chunking never produces an
// oversized segment.
// Validate guarantees Publish's chunking never produces an oversized segment.
func (c *Channel) Validate(g channels.GeneratedContent) error {
	body := strings.TrimSpace(g.Body)
	if body == "" {
		return errors.New("x: body must not be empty")
	}
	if len(chunk(body)) == 0 {
		return errors.New("x: body could not be chunked into any tweet segment")
	}
	return nil
}

// tweetRequest/replyRef mirror X API v2's POST /2/tweets JSON shape.
type tweetRequest struct {
	Text  string    `json:"text"`
	Reply *replyRef `json:"reply,omitempty"`
}

type replyRef struct {
	InReplyToTweetID string `json:"in_reply_to_tweet_id"`
}

// tweetResponse captures only the field PublishResult needs. X returns the
// tweet id as a JSON STRING (unlike dev.to's numeric id).
type tweetResponse struct {
	Data struct {
		ID string `json:"id"`
	} `json:"data"`
}

// Publish chunks the brief into a tweet thread and posts each segment in
// order, chaining each to the previous via in_reply_to_tweet_id so the
// result is a real thread, not a set of unrelated tweets.
func (c *Channel) Publish(ctx context.Context, g channels.GeneratedContent) (channels.PublishResult, error) {
	if err := c.Validate(g); err != nil {
		return channels.PublishResult{}, err
	}
	segments := chunk(strings.TrimSpace(g.Body))

	accessToken, err := c.oauth.accessTokenValue(ctx)
	if err != nil {
		return channels.PublishResult{}, err
	}

	var firstID, prevID string
	for i, seg := range segments {
		id, err := c.postTweet(ctx, accessToken, seg, prevID)
		if err != nil {
			if i == 0 {
				return channels.PublishResult{}, fmt.Errorf("x: posting first tweet failed: %w", err)
			}
			// Partial failure: 1..i tweets are already live and NOT rolled
			// back (see phase spec — auto-deleting a partially-live thread
			// is its own failure mode). The error names exactly which
			// segment failed so the caller (P3's MarkFailed) can surface it.
			return channels.PublishResult{}, fmt.Errorf(
				"x: thread partially posted (%d of %d segments live); segment %d/%d failed: %w",
				i, len(segments), i+1, len(segments), err)
		}
		if i == 0 {
			firstID = id
		}
		prevID = id
	}

	return channels.PublishResult{
		ExternalURL: fmt.Sprintf("https://x.com/i/web/status/%s", firstID),
		ExternalID:  firstID,
	}, nil
}

// postTweet performs one POST /2/tweets call and returns the new tweet's id.
// replyToID == "" posts a standalone (thread-opening) tweet.
func (c *Channel) postTweet(ctx context.Context, accessToken, text, replyToID string) (string, error) {
	payload := tweetRequest{Text: text}
	if replyToID != "" {
		payload.Reply = &replyRef{InReplyToTweetID: replyToID}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("x: encoding tweet request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/2/tweets", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("x: building tweet request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return "", fmt.Errorf("x: tweet request failed: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		var parsed tweetResponse
		if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&parsed); err != nil {
			return "", fmt.Errorf("x: decoding tweet response: %w", err)
		}
		if parsed.Data.ID == "" {
			return "", errors.New("x: tweet response missing id")
		}
		return parsed.Data.ID, nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		discard(resp.Body)
		return "", errors.New("x: authentication rejected (check X_CLIENT_ID/X_CLIENT_SECRET/X_REFRESH_TOKEN)")
	case resp.StatusCode == http.StatusTooManyRequests:
		discard(resp.Body)
		return "", errors.New("x: rate limited by X, try again later")
	default:
		discard(resp.Body)
		return "", fmt.Errorf("x: tweet post failed with status %d", resp.StatusCode)
	}
}

// discard drains a bounded prefix of body so the connection can be reused,
// without ever making the content available to the caller (an error body
// could reflect request headers, including the bearer token).
func discard(body io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxResponseBytes))
}
