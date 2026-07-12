package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// This file holds the `runs` table access methods. All queries are
// parameterized (never string-interpolated) — injection-safe by construction.

// CreateRun inserts the run header at run start. ON CONFLICT DO NOTHING makes it
// idempotent: a retry reuses the same session id, and re-creating the row must
// not clobber the existing header or error out.
func (s *Store) CreateRun(ctx context.Context, r RunRecord) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO runs
			(id, paper_id, paper_title, stage, status, input_tokens, output_tokens,
			 est_cost_usd, review_passed, started_at, completed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (id) DO NOTHING`,
		r.ID, r.PaperID, r.PaperTitle, r.Stage, r.Status, r.InputTokens, r.OutputTokens,
		r.EstCostUSD, r.ReviewPassed, r.StartedAt, r.CompletedAt)
	return err
}

// UpdateRunPaper records the chosen paper once the user selects one
// (selection.chosen). Separate from CreateRun because the paper is unknown at
// run start (discovery precedes selection).
func (s *Store) UpdateRunPaper(ctx context.Context, id, paperID, paperTitle string) error {
	_, err := s.pool.Exec(ctx,
		`UPDATE runs SET paper_id = $2, paper_title = $3 WHERE id = $1`,
		id, paperID, paperTitle)
	return err
}

// FinalizeRun writes the terminal header on run.completed / run.failed /
// recovered: final stage + status, the token/cost totals, the review outcome,
// and completed_at. Only the fields known at finish are updated (paper columns
// were set earlier by UpdateRunPaper).
func (s *Store) FinalizeRun(ctx context.Context, r RunRecord) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE runs
		SET stage = $2, status = $3, input_tokens = $4, output_tokens = $5,
			est_cost_usd = $6, review_passed = $7, completed_at = $8
		WHERE id = $1`,
		r.ID, r.Stage, r.Status, r.InputTokens, r.OutputTokens,
		r.EstCostUSD, r.ReviewPassed, r.CompletedAt)
	return err
}

// GetRun reads one run header. est_cost_usd is cast to float8 in SQL so it scans
// cleanly into *float64 (avoids pgx numeric-codec scan ambiguity). Returns
// ErrRunNotFound for an unknown id so the HTTP layer can 404 without importing pgx.
func (s *Store) GetRun(ctx context.Context, id string) (RunRecord, error) {
	var r RunRecord
	err := s.pool.QueryRow(ctx, `
		SELECT id, paper_id, paper_title, stage, status, input_tokens, output_tokens,
			est_cost_usd::float8, review_passed, started_at, completed_at
		FROM runs WHERE id = $1`, id).
		Scan(&r.ID, &r.PaperID, &r.PaperTitle, &r.Stage, &r.Status, &r.InputTokens,
			&r.OutputTokens, &r.EstCostUSD, &r.ReviewPassed, &r.StartedAt, &r.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return RunRecord{}, ErrRunNotFound
	}
	if err != nil {
		return RunRecord{}, err
	}
	return r, nil
}

// ListRuns returns a page of run headers newest-first plus the total row count
// for pagination. limit is clamped to [1,200]; a non-positive offset is treated
// as 0. Two queries (count + page) keep it simple for this single-user tool.
func (s *Store) ListRuns(ctx context.Context, limit, offset int) ([]RunRecord, int, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	if offset < 0 {
		offset = 0
	}

	var total int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM runs`).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id, paper_id, paper_title, stage, status, input_tokens, output_tokens,
			est_cost_usd::float8, review_passed, started_at, completed_at
		FROM runs
		ORDER BY started_at DESC
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []RunRecord
	for rows.Next() {
		var r RunRecord
		if err := rows.Scan(&r.ID, &r.PaperID, &r.PaperTitle, &r.Stage, &r.Status,
			&r.InputTokens, &r.OutputTokens, &r.EstCostUSD, &r.ReviewPassed,
			&r.StartedAt, &r.CompletedAt); err != nil {
			return nil, 0, fmt.Errorf("scan run row: %w", err)
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}
