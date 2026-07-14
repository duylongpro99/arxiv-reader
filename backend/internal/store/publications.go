package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// This file holds the `publications` table access methods (Phase 10: channel
// publishing). All queries are parameterized (never string-interpolated) —
// injection-safe by construction. DB availability itself (nil *Store refusing
// to publish) is enforced by the HTTP layer (Phase 3), not here — this layer
// just does correct CRUD against a live pool.

// ErrPublicationNotFound is returned by GetPublication for an unknown id, so
// the HTTP layer can map it to a 404 without importing pgx.
var ErrPublicationNotFound = errors.New("publication not found")

// CreatePublication inserts a new draft. ON CONFLICT (run_id, channel_id) DO
// NOTHING makes re-generating a draft for the same run+channel idempotent: a
// public post must never be silently duplicated. inserted reports whether the
// row was actually created — false means the (run_id, channel_id) pair
// already exists, letting the HTTP layer 409 without a fresh post attempt.
func (s *Store) CreatePublication(ctx context.Context, p PublicationRecord) (inserted bool, err error) {
	res, err := s.pool.Exec(ctx, `
		INSERT INTO publications
			(id, run_id, channel_id, category, status, adapted_content,
			 title, external_url, external_id, error, created_at, published_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (run_id, channel_id) DO NOTHING`,
		p.ID, p.RunID, p.ChannelID, p.Category, p.Status, p.AdaptedContent,
		p.Title, p.ExternalURL, p.ExternalID, p.Error, p.CreatedAt, p.PublishedAt)
	if err != nil {
		return false, err
	}
	return res.RowsAffected() == 1, nil
}

// ListPublicationsByRun returns every draft/attempt for a run, oldest first
// (stable ordering for the review UI's channel list).
func (s *Store) ListPublicationsByRun(ctx context.Context, runID string) ([]PublicationRecord, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, run_id, channel_id, category, status, adapted_content,
			title, external_url, external_id, error, created_at, published_at
		FROM publications
		WHERE run_id = $1
		ORDER BY created_at ASC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []PublicationRecord
	for rows.Next() {
		var p PublicationRecord
		if err := rows.Scan(&p.ID, &p.RunID, &p.ChannelID, &p.Category, &p.Status, &p.AdaptedContent,
			&p.Title, &p.ExternalURL, &p.ExternalID, &p.Error, &p.CreatedAt, &p.PublishedAt); err != nil {
			return nil, fmt.Errorf("scan publication row: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetPublication reads one publication by id. Returns ErrPublicationNotFound
// for an unknown id so the HTTP layer can 404 without importing pgx.
func (s *Store) GetPublication(ctx context.Context, id string) (PublicationRecord, error) {
	var p PublicationRecord
	err := s.pool.QueryRow(ctx, `
		SELECT id, run_id, channel_id, category, status, adapted_content,
			title, external_url, external_id, error, created_at, published_at
		FROM publications WHERE id = $1`, id).
		Scan(&p.ID, &p.RunID, &p.ChannelID, &p.Category, &p.Status, &p.AdaptedContent,
			&p.Title, &p.ExternalURL, &p.ExternalID, &p.Error, &p.CreatedAt, &p.PublishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return PublicationRecord{}, ErrPublicationNotFound
	}
	if err != nil {
		return PublicationRecord{}, err
	}
	return p, nil
}

// UpdatePublicationContent persists a human edit to the draft (title, body,
// status — e.g. draft → approved) prior to publishing. Publish-specific
// columns (external_url/id, published_at) are untouched here; they are only
// ever set by MarkPublished.
func (s *Store) UpdatePublicationContent(ctx context.Context, id, title, content, status string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE publications SET title = $2, adapted_content = $3, status = $4 WHERE id = $1`,
		id, title, content, status)
	return err
}

// ClaimForPublish atomically transitions a publication from a publishable
// status ("approved", or "failed" for a retry) to the transient "publishing"
// state, returning claimed=true ONLY when this call won the transition. It is
// the guard against the design note's #1 hazard — a concurrent double-publish
// (double-click, a client retry, or a retry after a spurious timeout) posting
// the same content to a live channel twice. Because the check-and-set is a
// single UPDATE ... WHERE, two racing callers cannot both observe a claimable
// row: exactly one gets RowsAffected==1. A row that is "draft" (never
// approved — the human-review gate), already "published", or already
// "publishing" (a racing claim) yields claimed=false, so the caller refuses to
// post. Note: a process crash between claim and MarkPublished/MarkFailed leaves
// the row stuck at "publishing" — an accepted, rare trade-off for a single-user
// local tool, strictly preferable to an irreversible duplicate public post.
func (s *Store) ClaimForPublish(ctx context.Context, id string) (claimed bool, err error) {
	res, err := s.pool.Exec(ctx,
		`UPDATE publications SET status = 'publishing'
		 WHERE id = $1 AND status IN ('approved', 'failed')`, id)
	if err != nil {
		return false, err
	}
	return res.RowsAffected() == 1, nil
}

// MarkPublished records a successful channel post: status becomes
// "published", the returned external identifiers are stored, and
// published_at is stamped now() — the durable proof that this (run, channel)
// pair has gone live and must never be reposted.
func (s *Store) MarkPublished(ctx context.Context, id, url, extID string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE publications
		SET status = 'published', external_url = $2, external_id = $3, published_at = now()
		WHERE id = $1`,
		id, url, extID)
	return err
}

// MarkFailed records a failed publish attempt: status becomes "failed" and
// the error is stored for the retry UI. The row (and its unique run/channel
// slot) is left in place so a retry can transition it back to published
// rather than needing a fresh draft.
func (s *Store) MarkFailed(ctx context.Context, id, errMsg string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE publications SET status = 'failed', error = $2 WHERE id = $1`,
		id, errMsg)
	return err
}
