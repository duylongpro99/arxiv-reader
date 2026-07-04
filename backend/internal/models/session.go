package models

import (
	"sync"
	"time"
)

// PipelineStage is the coarse-grained stage the frontend polls for. Only the
// three stages Phase 2 can reach are defined here; later phases add their own
// (fetching_pdf, generating, …) as they are implemented.
type PipelineStage string

const (
	StageDiscovery PipelineStage = "discovery" // fetching + filtering in progress
	StageSelection PipelineStage = "selection" // candidates ready, awaiting user pick
	StageFailed    PipelineStage = "failed"    // pipeline aborted; see Error/Recoverable
)

// PipelineSession is the in-memory state of one discovery run.
//
// It is written by the background discovery goroutine and read concurrently by
// the status-poll HTTP handler. That is a genuine data race, so every field is
// guarded by mu and callers MUST go through the accessor methods below — never
// touch the fields directly. Centralizing all locking here keeps the race
// guarantee in one auditable place.
type PipelineSession struct {
	mu          sync.RWMutex
	SessionID   string // immutable after newSession; safe to read but kept private-access for consistency
	stage       PipelineStage
	candidates  []Paper
	notice      string
	errMsg      string
	recoverable bool
	startedAt   time.Time
}

// SessionSnapshot is an immutable, mutex-free copy of a session's observable
// state, safe to serialize or read without holding the lock.
type SessionSnapshot struct {
	SessionID   string
	Stage       PipelineStage
	Candidates  []Paper
	Notice      string
	Error       string
	Recoverable bool
	StartedAt   time.Time
}

// NewSession creates a session in the discovery stage. id must be unique.
func NewSession(id string, startedAt time.Time) *PipelineSession {
	return &PipelineSession{
		SessionID: id,
		stage:     StageDiscovery,
		startedAt: startedAt,
	}
}

// Snapshot returns a lock-free copy of the current observable state. Candidates
// is shallow-copied so the caller cannot mutate the session's backing slice.
func (s *PipelineSession) Snapshot() SessionSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var cands []Paper
	if len(s.candidates) > 0 {
		cands = make([]Paper, len(s.candidates))
		copy(cands, s.candidates)
	}
	return SessionSnapshot{
		SessionID:   s.SessionID,
		Stage:       s.stage,
		Candidates:  cands,
		Notice:      s.notice,
		Error:       s.errMsg,
		Recoverable: s.recoverable,
		StartedAt:   s.startedAt,
	}
}

// Complete transitions the session to the selection stage with the final
// candidate list and an optional notice (e.g. "Only 3 new papers found").
func (s *PipelineSession) Complete(candidates []Paper, notice string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.candidates = candidates
	s.notice = notice
	s.stage = StageSelection
}

// Fail transitions the session to the failed stage with a human-readable
// message. recoverable indicates whether a retry might succeed.
func (s *PipelineSession) Fail(message string, recoverable bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errMsg = message
	s.recoverable = recoverable
	s.stage = StageFailed
}

// StartedAt exposes the immutable start time for duration logging.
func (s *PipelineSession) StartedAt() time.Time { return s.startedAt }
