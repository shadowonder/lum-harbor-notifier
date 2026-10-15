package background

import (
	"sync"
	"time"
)

// JobSnapshot is an immutable point-in-time view of one registered Job's
// runtime state.
type JobSnapshot struct {
	ID string

	Running bool

	LastAttemptAt *time.Time
	LastSuccessAt *time.Time
	LastFailureAt *time.Time

	LastDuration time.Duration
	LastError    string

	ConsecutiveFailures int

	NextRunAt *time.Time
}

// jobRuntimeState owns the mutable execution metadata for one registered Job.
//
// The state may be updated by a Job execution goroutine while another
// goroutine reads a diagnostic snapshot. The mutex therefore belongs with the
// state itself rather than requiring every caller to coordinate locking.
type jobRuntimeState struct {
	mu sync.RWMutex

	id string

	running bool

	lastAttemptAt *time.Time
	lastSuccessAt *time.Time
	lastFailureAt *time.Time

	lastDuration time.Duration
	lastError    string

	consecutiveFailures int

	nextRunAt *time.Time
}

// newJobRuntimeState creates empty runtime state for a registered Job.
func newJobRuntimeState(jobID string) *jobRuntimeState {
	return &jobRuntimeState{
		id: jobID,
	}
}

// markStarted records the beginning of a Job execution.
func (s *jobRuntimeState) markStarted(startedAt time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.running = true
	s.lastAttemptAt = &startedAt
}

// markSucceeded records a successfully completed Job execution.
func (s *jobRuntimeState) markSucceeded(endAt time.Time, duration time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastSuccessAt = &endAt
	s.lastDuration = duration
	s.consecutiveFailures = 0
	s.lastError = ""
	s.running = false
}

// markFailed records an unsuccessful Job execution.
func (s *jobRuntimeState) markFailed(endAt time.Time, duration time.Duration, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastFailureAt = &endAt
	s.lastDuration = duration
	s.running = false
	s.consecutiveFailures++

	if err != nil {
		s.lastError = err.Error()
	} else {
		s.lastError = ""
	}
}

// setNextRun records the next periodic execution time.
func (s *jobRuntimeState) setNextRun(nextRun *time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.nextRunAt = cloneTimePtr(nextRun)
}

// cloneTimePtr returns a detached copy of a timestamp pointer.
func cloneTimePtr(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func (s *jobRuntimeState) snapshot() JobSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return JobSnapshot{
		ID:                  s.id,
		Running:             s.running,
		LastAttemptAt:       cloneTimePtr(s.lastAttemptAt),
		LastSuccessAt:       cloneTimePtr(s.lastSuccessAt),
		LastFailureAt:       cloneTimePtr(s.lastFailureAt),
		LastDuration:        s.lastDuration,
		LastError:           s.lastError,
		ConsecutiveFailures: s.consecutiveFailures,
		NextRunAt:           cloneTimePtr(s.nextRunAt),
	}
}

// markCancelled records that an in-progress execution stopped because its
// parent lifecycle was cancelled, typically during application shutdown.
func (s *jobRuntimeState) markCancelled(duration time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.running = false
	s.lastDuration = duration
}
