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
	StageDiscovery  PipelineStage = "discovery"  // fetching + filtering in progress
	StageSelection  PipelineStage = "selection"  // candidates ready, awaiting user pick
	StageExtracting PipelineStage = "extracting" // fetching + converting paper HTML → Markdown
	StageFailed     PipelineStage = "failed"     // pipeline aborted; see Error/Recoverable
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
	// Phase 3 extraction state. Both are server-only and deliberately excluded
	// from Snapshot(): markdownText is large (~50KB–500KB) and never shipped to
	// the frontend; selectedPaper's metadata already rides along in candidates.
	selectedPaper *Paper
	markdownText  string
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

// SetStage transitions the session to an arbitrary stage under the lock. Used by
// the orchestrator to move selection → extracting once a paper is chosen.
func (s *PipelineSession) SetStage(stage PipelineStage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stage = stage
}

// SetSelectedPaper records the paper the user picked (server-only; not in Snapshot).
func (s *PipelineSession) SetSelectedPaper(p *Paper) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.selectedPaper = p
}

// SetMarkdown stores the extracted paper Markdown (server-only; excluded from
// Snapshot so it never inflates the /status payload).
func (s *PipelineSession) SetMarkdown(md string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.markdownText = md
}

// SetNotice sets a user-facing notice under the lock without changing the stage.
func (s *PipelineSession) SetNotice(n string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notice = n
}

// RecoverToSelection returns an extracting session to selection WITHOUT touching
// candidates, clearing any prior error and setting a recoverable notice. This is
// the 404 re-pick path — distinct from Fail (which sets StageFailed). Marking it
// recoverable lets the frontend re-enable the candidate cards for another pick.
func (s *PipelineSession) RecoverToSelection(notice string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stage = StageSelection
	s.notice = notice
	s.errMsg = ""
	s.recoverable = true
}

// Markdown returns the stored paper Markdown under the lock. Server-only (kept
// out of Snapshot); Phase 4's ExplainerAgent reads it here as its input.
func (s *PipelineSession) Markdown() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.markdownText
}

// StartedAt exposes the immutable start time for duration logging.
func (s *PipelineSession) StartedAt() time.Time { return s.startedAt }
