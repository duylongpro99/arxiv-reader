// Package devto implements channels.Channel for the dev.to (Forem) publishing
// API — the first concrete channel, proving the interface end-to-end before
// Phase 5 adds X. It owns ALL dev.to mechanics (auth header, tag sanitizing,
// error mapping) behind the Channel seam; the Repurposer agent and registry
// never see anything Forem-specific. See docs/design-notes/2026-07-14-channel-publishing.md.
package devto

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/maritime-ds/arxiv-reader/internal/channels"
	"github.com/maritime-ds/arxiv-reader/internal/config"
)

const (
	// defaultBaseURL is the real Forem endpoint; overridable via DEVTO_BASE_URL
	// so tests can point at an httptest.Server instead of the live API.
	defaultBaseURL = "https://dev.to/api/articles"
	// defaultTimeout applies when cfg.Agent.RequestTimeoutSec is unset (0) —
	// publishing is a one-shot call, not latency-critical, so a generous
	// default is safe.
	defaultTimeout = 30 * time.Second
	// maxTags mirrors Forem's own limit (articles reject a 5th tag outright).
	maxTags = 4
	// maxTitleLen matches Forem's actual title cap; maxBodyLen is a generous
	// safety cap (Forem does not publish an exact markdown-body limit) that
	// only exists to reject obviously-broken payloads before a network call.
	maxTitleLen = 128
	maxBodyLen  = 800_000
	// maxResponseBytes bounds how much of a response we ever read — the body
	// is drained/discarded on error paths (never echoed in an error message,
	// since Forem error bodies can include request details) and decoded on
	// success, so this is purely an OOM guard.
	maxResponseBytes = 64 * 1024
	// publishRetryBackoff is the single fixed pause before the one 429 retry.
	// Deliberately not a general retry framework (per phase spec) — Forem's
	// rate limits are generous enough that one retry covers the common case.
	publishRetryBackoff = 2 * time.Second
)

// errRateLimited is an internal sentinel distinguishing a 429 from other
// non-2xx statuses so Publish can apply its single retry only to that case.
var errRateLimited = errors.New("devto: rate limited")

// init self-registers this channel with the registry (see
// channels.Register's doc comment for why this replaces a direct switch
// case — importing channels here, plus channels importing devto back for a
// switch case, would be a cycle). The binary must still import this package
// (blank import is enough) for init to run — see cmd/server/main.go.
func init() {
	channels.Register("devto", New)
}

// nonAlnum strips everything but lowercase letters/digits when sanitizing tags.
var nonAlnum = regexp.MustCompile(`[^a-z0-9]`)

// Channel is the dev.to implementation of channels.Channel.
type Channel struct {
	apiKey  string
	httpc   *http.Client
	baseURL string
}

// New builds a dev.to Channel. The API key is env-only (never config.yaml),
// mirroring how LLM_API_KEY is loaded — a secret must never land in a
// committed file. A missing key is a fail-fast error, not a silently
// half-configured Channel.
func New(cfg *config.Config) (channels.Channel, error) {
	apiKey := os.Getenv("DEVTO_API_KEY")
	if apiKey == "" {
		return nil, errors.New("devto: DEVTO_API_KEY is not set")
	}
	baseURL := os.Getenv("DEVTO_BASE_URL")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	timeout := time.Duration(cfg.Agent.RequestTimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	return &Channel{
		apiKey:  apiKey,
		httpc:   &http.Client{Timeout: timeout},
		baseURL: baseURL,
	}, nil
}

func (c *Channel) ID() string { return "devto" }

func (c *Channel) Category() channels.Category { return channels.Longform }

// Validate enforces the platform constraints Forem itself would reject on,
// surfaced locally and fast rather than as a failed network call.
func (c *Channel) Validate(g channels.GeneratedContent) error {
	title := strings.TrimSpace(g.Title)
	if title == "" {
		return errors.New("devto: title must not be empty")
	}
	if len(title) > maxTitleLen {
		return fmt.Errorf("devto: title exceeds %d characters (got %d)", maxTitleLen, len(title))
	}
	body := strings.TrimSpace(g.Body)
	if body == "" {
		return errors.New("devto: body must not be empty")
	}
	if len(body) > maxBodyLen {
		return fmt.Errorf("devto: body exceeds %d bytes (got %d)", maxBodyLen, len(body))
	}
	return nil
}

// articleRequest/articleBody mirror Forem's POST /api/articles JSON shape.
type articleRequest struct {
	Article articleBody `json:"article"`
}

type articleBody struct {
	Title        string   `json:"title"`
	BodyMarkdown string   `json:"body_markdown"`
	Published    bool     `json:"published"`
	Tags         []string `json:"tags,omitempty"`
}

// articleResponse captures only the fields PublishResult needs. Forem returns
// id as a JSON number, not a string — decoded as int64 then stringified.
type articleResponse struct {
	URL string `json:"url"`
	ID  int64  `json:"id"`
}

// Publish pushes the article live. A 429 gets exactly one retry after a fixed
// backoff; every other failure path (auth, other non-2xx, network) surfaces
// immediately — publish failures must stay visible, never silently swallowed.
func (c *Channel) Publish(ctx context.Context, g channels.GeneratedContent) (channels.PublishResult, error) {
	if err := c.Validate(g); err != nil {
		return channels.PublishResult{}, err
	}

	body, err := json.Marshal(articleRequest{Article: articleBody{
		Title:        g.Title,
		BodyMarkdown: g.Body,
		Published:    true,
		Tags:         sanitizeTags(g.Tags),
	}})
	if err != nil {
		return channels.PublishResult{}, fmt.Errorf("devto: encoding request: %w", err)
	}

	result, err := c.doPublish(ctx, body)
	if errors.Is(err, errRateLimited) {
		select {
		case <-time.After(publishRetryBackoff):
		case <-ctx.Done():
			return channels.PublishResult{}, ctx.Err()
		}
		result, err = c.doPublish(ctx, body)
		if errors.Is(err, errRateLimited) {
			return channels.PublishResult{}, errors.New("devto: rate limited by dev.to, try again later")
		}
	}
	return result, err
}

// doPublish performs one HTTP round trip and classifies the response. Error
// bodies are always drained-and-discarded, never included in a returned
// error — Forem's error payloads can echo request details (including
// headers), so surfacing them risks leaking the API key.
func (c *Channel) doPublish(ctx context.Context, body []byte) (channels.PublishResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(body))
	if err != nil {
		return channels.PublishResult{}, fmt.Errorf("devto: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("api-key", c.apiKey)

	resp, err := c.httpc.Do(req)
	if err != nil {
		return channels.PublishResult{}, fmt.Errorf("devto: request failed: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		var parsed articleResponse
		if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&parsed); err != nil {
			return channels.PublishResult{}, fmt.Errorf("devto: decoding response: %w", err)
		}
		return channels.PublishResult{ExternalURL: parsed.URL, ExternalID: fmt.Sprint(parsed.ID)}, nil
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		discard(resp.Body)
		return channels.PublishResult{}, errors.New("dev.to rejected the API key (check DEVTO_API_KEY)")
	case resp.StatusCode == http.StatusTooManyRequests:
		discard(resp.Body)
		return channels.PublishResult{}, errRateLimited
	default:
		discard(resp.Body)
		return channels.PublishResult{}, fmt.Errorf("devto: publish failed with status %d", resp.StatusCode)
	}
}

// discard drains a bounded prefix of body so the connection can be reused,
// without ever making the content available to the caller.
func discard(body io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, maxResponseBytes))
}

// sanitizeTags lowercases, strips non-alphanumeric characters, drops
// resulting empties, and caps at maxTags — Forem rejects tags outside this
// shape outright, so bad input is cleaned locally rather than failing the
// whole publish.
func sanitizeTags(tags []string) []string {
	out := make([]string, 0, maxTags)
	for _, t := range tags {
		clean := nonAlnum.ReplaceAllString(strings.ToLower(t), "")
		if clean == "" {
			continue
		}
		out = append(out, clean)
		if len(out) == maxTags {
			break
		}
	}
	return out
}
