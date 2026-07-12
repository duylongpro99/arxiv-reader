package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrStoreUnavailable signals that durable history is not available (empty DSN
// or an unreachable Postgres). It is NOT fatal: the caller logs a warning and
// continues with in-memory-only tracing. Wrapped so errors.Is works while the
// underlying cause stays inspectable.
var ErrStoreUnavailable = errors.New("store unavailable")

// ErrRunNotFound is returned by GetRun for an unknown run id, so the HTTP layer
// can map it to a 404 without importing pgx.
var ErrRunNotFound = errors.New("run not found")

// pingTimeout bounds the startup connectivity probe so a misconfigured or absent
// DB degrades quickly instead of stalling server boot.
const pingTimeout = 3 * time.Second

// Store wraps a pgx connection pool and satisfies the narrow run/event writer
// and reader interfaces declared consumer-side (internal/tracing, handlers).
type Store struct {
	pool *pgxpool.Pool
}

// Open resolves a live Store, or ErrStoreUnavailable (wrapping the cause) when
// the DSN is empty or the DB cannot be reached within pingTimeout. pgxpool.New
// connects lazily, so we Ping to force an actual connection at boot — turning a
// dead DB into a clean degrade instead of a first-request surprise. The returned
// error never carries the DSN password (pgx redacts it), but callers should log
// a generic message regardless (see orchestrator.New).
func Open(ctx context.Context, url string) (*Store, error) {
	if url == "" {
		return nil, fmt.Errorf("%w: DATABASE_URL is empty", ErrStoreUnavailable)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot create pool", ErrStoreUnavailable)
	}
	pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("%w: ping failed", ErrStoreUnavailable)
	}
	return &Store{pool: pool}, nil
}

// Close releases the pool. Nil-safe so callers can defer it unconditionally even
// when Open failed and left them with a nil *Store.
func (s *Store) Close() {
	if s != nil && s.pool != nil {
		s.pool.Close()
	}
}
