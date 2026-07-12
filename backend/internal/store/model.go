// Package store is the thin Postgres persistence layer for run-timeline history
// (Phase 7). It uses jackc/pgx/v5 with hand-written SQL — no ORM, matching the
// repo's stdlib-first style. It is OPTIONAL: a nil *Store (DB unavailable) means
// tracing runs in-memory only, and callers treat every method as best-effort.
package store

import (
	"encoding/json"
	"time"
)

// RunRecord is one row of the `runs` table — the durable header for a pipeline
// run. Nullable columns use pointers so a NULL round-trips as nil rather than a
// misleading zero value (e.g. a run that never chose a paper, or never finished).
// The column set mirrors backend/migrations/0001_run_timeline.sql exactly.
type RunRecord struct {
	ID           string
	PaperID      *string
	PaperTitle   *string
	Stage        string
	Status       string
	InputTokens  int
	OutputTokens int
	EstCostUSD   *float64
	ReviewPassed *bool
	StartedAt    time.Time
	CompletedAt  *time.Time
}

// EventRecord is one row of the `run_events` table — a single ordered timeline
// entry. Summary/PayloadFull are raw JSONB (already scrubbed upstream by the
// tracing package); PayloadFull is nil unless full-payload capture is opted in.
// DurationMS is nil for instantaneous/started events.
type EventRecord struct {
	RunID       string
	Seq         int
	EventType   string
	Stage       string
	Title       string
	Status      string
	Summary     json.RawMessage // JSONB; nil → SQL NULL
	PayloadFull json.RawMessage // JSONB; nil → SQL NULL (opt-in only)
	DurationMS  *int
	CreatedAt   time.Time
}
