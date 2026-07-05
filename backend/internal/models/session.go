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
	StageGenerating PipelineStage = "generating" // Phase 4: LLM writing the explainer note
	StageReviewing  PipelineStage = "reviewing"  // Phase 5: ReviewerAgent scoring the explainer
	StageRevising   PipelineStage = "revising"   // Phase 5: ExplainerAgent revising after a failed review
	StageWriting    PipelineStage = "writing"    // Phase 4: atomic write to the Obsidian vault
	StageComplete   PipelineStage = "complete"   // Phase 4: note saved; /result is ready
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
	// Phase 4 explainer/vault state. Also server-only and excluded from
	// Snapshot(): explainer.Content is large and reaches the frontend only via
	// the dedicated /result endpoint, never the /status poll.
	explainer  *ExplainerOutput
	vaultFile  string
	tokensUsed int
	// Phase 5 review-loop state. verdict is the latest reviewer judgement (nil
	// before any review, and permanently nil when the reviewer is disabled via
	// max_review_iterations: 0). iteration is the current generate/review pass
	// (1-based). Both are surfaced to the frontend through Snapshot().
	verdict   *ReviewVerdict
	iteration int
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
	// Phase 5 review progress, derived from verdict/iteration. ReviewScore and
	// ReviewPassed are zero-valued (0/false) when no verdict is set yet.
	Iteration    int
	ReviewScore  float32
	ReviewPassed bool
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
	// Derive review fields from verdict; zero-valued when no review has run.
	var score float32
	var passed bool
	if s.verdict != nil {
		score = s.verdict.Score
		passed = s.verdict.Pass
	}
	return SessionSnapshot{
		SessionID:    s.SessionID,
		Stage:        s.stage,
		Candidates:   cands,
		Notice:       s.notice,
		Error:        s.errMsg,
		Recoverable:  s.recoverable,
		StartedAt:    s.startedAt,
		Iteration:    s.iteration,
		ReviewScore:  score,
		ReviewPassed: passed,
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

// SelectedPaper returns the paper the user picked (server-only; not in Snapshot).
// Phase 4 reads the full metadata (title/authors/published/id) here to feed the
// ExplainerAgent and VaultWriter. Returns nil if no paper has been selected.
func (s *PipelineSession) SelectedPaper() *Paper {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.selectedPaper
}

// SetExplainer stores the generated explainer note (server-only; not in Snapshot).
func (s *PipelineSession) SetExplainer(e *ExplainerOutput) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.explainer = e
}

// Explainer returns the generated explainer note, or nil before generation.
// The /result handler reads it here (server-only; kept out of /status).
func (s *PipelineSession) Explainer() *ExplainerOutput {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.explainer
}

// SetVaultFile records the absolute path of the written vault note (server-only).
func (s *PipelineSession) SetVaultFile(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.vaultFile = path
}

// VaultFile returns the written note's path, or "" before the vault write.
func (s *PipelineSession) VaultFile() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.vaultFile
}

// AddTokens accumulates LLM token usage across calls. Phase 4 adds once; the
// Phase 5 revision loop will add per iteration — an additive API future-proofs
// that at no cost now.
func (s *PipelineSession) AddTokens(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokensUsed += n
}

// TokensUsed returns the accumulated token total (server-only; not in Snapshot).
func (s *PipelineSession) TokensUsed() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.tokensUsed
}

// SetVerdict stores the latest reviewer judgement (Phase 5). Also drives the
// review fields in Snapshot(); server keeps the full struct for the vault write.
func (s *PipelineSession) SetVerdict(v *ReviewVerdict) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.verdict = v
}

// Verdict returns the latest reviewer judgement, or nil if none (reviewer
// disabled, or before the first review). The vault write reads it here.
func (s *PipelineSession) Verdict() *ReviewVerdict {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.verdict
}

// SetIteration records the current generate/review pass (1-based). Surfaced to
// the frontend via Snapshot() so the UI can label "Reviewing (pass N)".
func (s *PipelineSession) SetIteration(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.iteration = n
}

// Iteration returns the current generate/review pass, or 0 before the loop runs.
func (s *PipelineSession) Iteration() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.iteration
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
