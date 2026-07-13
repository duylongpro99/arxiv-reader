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
	// Phase 6 hardening state.
	//   failedStage     — the stage active when Fail() ran; drives retry routing
	//                     so a resumed run restarts the failed segment, not discovery.
	//   errorAction     — machine-readable UI hint: "retry" | "fix_config" |
	//                     "fix_permissions" | "select_other" (set right after Fail).
	//   inputTokens/outputTokens — split token accounting for cost estimation
	//                     (TokensUsed keeps the total for back-compat).
	//   arxivRetryCount — current arXiv 429/5xx retry attempt (0 = none), for a
	//                     "Connecting to arXiv (retry n/3)…" progress label.
	//   contextWarning  — non-blocking advisory when the estimate exceeds the
	//                     model's context window; nil unless the pre-check tripped.
	failedStage     PipelineStage
	errorAction     string
	inputTokens     int
	outputTokens    int
	arxivRetryCount int
	contextWarning  *ContextWarning
	// nextStart is the Feature C (arXiv pagination) cursor: the arXiv `start`
	// offset the NEXT /discover/{id}/more call should fetch from. Zero means "no
	// page has been fetched via /more yet" — the first discovery page already
	// consumed arXiv offsets [0, FetchLimit), so ConsumeNextStart treats a zero
	// cursor as "use FetchLimit" rather than needing runDiscovery (owned by a
	// parallel phase) to initialize it explicitly.
	nextStart int
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
	// Phase 6 poll-surfaced fields. ErrorAction accompanies a failure so the UI
	// can label the retry affordance; ArxivRetryCount drives the discovery retry
	// label; ContextWarning is the non-blocking over-limit advisory. The large
	// in/out token totals are deliberately NOT here — they ride /result, keeping
	// the frequent /status poll small (mirrors the existing markdown/token split).
	ErrorAction     string
	ArxivRetryCount int
	ContextWarning  *ContextWarning
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
		SessionID:       s.SessionID,
		Stage:           s.stage,
		Candidates:      cands,
		Notice:          s.notice,
		Error:           s.errMsg,
		Recoverable:     s.recoverable,
		StartedAt:       s.startedAt,
		Iteration:       s.iteration,
		ReviewScore:     score,
		ReviewPassed:    passed,
		ErrorAction:     s.errorAction,
		ArxivRetryCount: s.arxivRetryCount,
		ContextWarning:  s.contextWarning,
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
//
// It captures the currently-active stage into failedStage BEFORE overwriting
// s.stage with StageFailed — Phase 6 retry routing needs to know which segment
// failed (e.g. writing vs generating) to resume the right part of the pipeline.
// The signature is unchanged so no caller churns; the orchestrator sets the
// machine-readable action separately via SetErrorAction right after Fail.
func (s *PipelineSession) Fail(message string, recoverable bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failedStage = s.stage
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

// --- Phase 6 accessors (all mutex-guarded, mirroring the pattern above) ---

// BeginRetry atomically confirms the session is retryable (failed + recoverable)
// and, if so, clears the transient error state and transitions the stage OUT of
// StageFailed to the failed stage (the resume point), returning (failedStage,
// true). Doing the check-and-transition under one lock closes the TOCTOU window:
// a concurrent second retry observes a non-failed session and gets false, so it
// cannot double-spawn a pipeline goroutine (which would double-write the vault /
// double-count tokens). Cached outputs (markdown/explainer/verdict) are left
// intact so completed segments still skip on resume.
func (s *PipelineSession) BeginRetry() (PipelineStage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stage != StageFailed || !s.recoverable {
		return "", false
	}
	failed := s.failedStage
	s.errMsg = ""
	s.recoverable = false
	s.errorAction = ""
	s.stage = failed // leave StageFailed so a concurrent retry is rejected
	return failed, true
}

// FailedStage returns the stage that was active when Fail() ran. Used by the
// retry handler to route a resumed run to the correct pipeline segment.
func (s *PipelineSession) FailedStage() PipelineStage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.failedStage
}

// SetErrorAction records the machine-readable UI hint for the current failure
// ("retry" | "fix_config" | "fix_permissions" | "select_other"). The
// orchestrator calls this immediately after Fail(), using the describe* mapping.
func (s *PipelineSession) SetErrorAction(a string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.errorAction = a
}

// ErrorAction returns the machine-readable failure hint, or "" if none.
func (s *PipelineSession) ErrorAction() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.errorAction
}

// AddIO accumulates split input/output token usage across LLM calls. Additive
// (like AddTokens) so the Phase 5 revision loop can call it per iteration.
func (s *PipelineSession) AddIO(in, out int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inputTokens += in
	s.outputTokens += out
}

// InputTokens returns the accumulated input-token total (server-only; feeds the
// /result cost estimate, deliberately kept off the /status poll).
func (s *PipelineSession) InputTokens() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.inputTokens
}

// OutputTokens returns the accumulated output-token total (server-only).
func (s *PipelineSession) OutputTokens() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.outputTokens
}

// SetArxivRetryCount records the current arXiv retry attempt (0 resets it).
// Surfaced via Snapshot so the UI can show "Connecting to arXiv (retry n/3)…".
func (s *PipelineSession) SetArxivRetryCount(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.arxivRetryCount = n
}

// ArxivRetryCount returns the current arXiv retry attempt (0 = none).
func (s *PipelineSession) ArxivRetryCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.arxivRetryCount
}

// SetContextWarning attaches (or clears, with nil) the non-blocking over-limit
// advisory. Surfaced via Snapshot; never aborts the pipeline.
func (s *PipelineSession) SetContextWarning(w *ContextWarning) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.contextWarning = w
}

// ContextWarning returns the current over-limit advisory, or nil if none.
func (s *PipelineSession) ContextWarning() *ContextWarning {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.contextWarning
}

// --- Feature C: arXiv pagination via session extension ---

// ConsumeNextStart atomically returns the arXiv `start` offset to fetch next
// and advances the cursor by step (the configured FetchLimit) under a single
// lock, so two concurrent /more calls on the same session can never re-fetch
// or skip a page (a plain get-then-set from the handler would race across the
// network call in between). A zero cursor means "first /more call" — the
// initial discovery page already consumed [0, step), so the next page starts
// at step.
func (s *PipelineSession) ConsumeNextStart(step int) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	start := s.nextStart
	if start == 0 {
		start = step
	}
	s.nextStart = start + step
	return start
}

// AppendCandidates extends the session's candidate list with a newly fetched
// page (Feature C), keeping /status and /process consistent with the larger
// set — a subsequent /process can select any appended paper because it now
// lives in the same Candidates slice /status already reports.
func (s *PipelineSession) AppendCandidates(papers []Paper) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.candidates = append(s.candidates, papers...)
}
