package store

import (
	"context"
	"fmt"
)

// This file holds the `run_events` table access methods.

// AppendEvent inserts one ordered timeline row. The (run_id, seq) primary key
// makes it idempotent under an accidental replay: a duplicate seq is dropped
// rather than duplicated. Summary/PayloadFull are raw JSONB (nil → NULL).
func (s *Store) AppendEvent(ctx context.Context, e EventRecord) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO run_events
			(run_id, seq, event_type, stage, title, status, summary, payload_full, duration_ms, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		ON CONFLICT (run_id, seq) DO NOTHING`,
		e.RunID, e.Seq, e.EventType, e.Stage, e.Title, e.Status,
		e.Summary, e.PayloadFull, e.DurationMS, e.CreatedAt)
	return err
}

// ListEvents returns a run's events in ascending seq order, restricted to
// seq > sinceSeq. Pass sinceSeq = -1 for the full timeline (reopen a run) or a
// concrete seq for SSE resume-from-Last-Event-ID history fallback (Phase 04).
func (s *Store) ListEvents(ctx context.Context, runID string, sinceSeq int) ([]EventRecord, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT run_id, seq, event_type, stage, title, status, summary, payload_full, duration_ms, created_at
		FROM run_events
		WHERE run_id = $1 AND seq > $2
		ORDER BY seq ASC`, runID, sinceSeq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EventRecord
	for rows.Next() {
		var e EventRecord
		if err := rows.Scan(&e.RunID, &e.Seq, &e.EventType, &e.Stage, &e.Title, &e.Status,
			&e.Summary, &e.PayloadFull, &e.DurationMS, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan event row: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
