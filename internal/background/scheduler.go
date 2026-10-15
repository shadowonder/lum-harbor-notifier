package background

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
)

var (
	ErrSchedulerLifecycle = errors.New("background scheduler lifecycle does not allow operation")
	ErrSchedulerStarted   = fmt.Errorf("%w: scheduler has already started", ErrSchedulerLifecycle)
	ErrDuplicateJob       = errors.New("background job is already registered")
	ErrJobNotFound        = errors.New("background job is not registered")
	ErrJobDisabled        = errors.New("background job is disabled")
)

type SchedulerState string

const (
	SchedulerNew      SchedulerState = "NEW"
	SchedulerStarted  SchedulerState = "STARTED"
	SchedulerStopping SchedulerState = "STOPPING"
	SchedulerStopped  SchedulerState = "STOPPED"
)

// Scheduler owns a statically registered set of background Jobs. It launches
// one runner goroutine per enabled Job and owns the child context that stops
// those runners. The done channel is closed exactly once when the terminal
// STOPPED state is reached and is shared by every Shutdown caller.
type Scheduler struct {
	mu sync.RWMutex

	runners map[string]*jobRunner
	state   SchedulerState
	cancel  context.CancelFunc
	done    chan struct{}

	wg sync.WaitGroup
}

func NewScheduler() *Scheduler {
	return &Scheduler{
		runners: make(map[string]*jobRunner),
		state:   SchedulerNew,
		done:    make(chan struct{}),
	}
}

func (s *Scheduler) State() SchedulerState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

func (s *Scheduler) Register(definition JobDefinition, job Job) error {
	if isNilJob(job) {
		return fmt.Errorf("background job %q must not be nil", definition.ID)
	}
	if err := definition.Validate(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != SchedulerNew {
		return lifecycleError("register", s.state)
	}
	if _, exists := s.runners[definition.ID]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicateJob, definition.ID)
	}
	s.runners[definition.ID] = newJobRunner(definition, job)
	return nil
}

func isNilJob(job Job) bool {
	if job == nil {
		return true
	}
	value := reflect.ValueOf(job)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (s *Scheduler) Snapshot(jobID string) (JobSnapshot, error) {
	s.mu.RLock()
	runner, exists := s.runners[jobID]
	s.mu.RUnlock()
	if !exists {
		return JobSnapshot{}, fmt.Errorf("%w: %s", ErrJobNotFound, jobID)
	}
	return runner.state.snapshot(), nil
}

func (s *Scheduler) Snapshots() []JobSnapshot {
	s.mu.RLock()
	runners := make([]*jobRunner, 0, len(s.runners))
	for _, runner := range s.runners {
		runners = append(runners, runner)
	}
	s.mu.RUnlock()

	snapshots := make([]JobSnapshot, 0, len(runners))
	for _, runner := range runners {
		snapshots = append(snapshots, runner.state.snapshot())
	}
	return snapshots
}

func (s *Scheduler) Start(parentCtx context.Context) error {
	if parentCtx == nil {
		return fmt.Errorf("background scheduler start context must not be nil")
	}
	if err := parentCtx.Err(); err != nil {
		return fmt.Errorf("background scheduler start context: %w", err)
	}

	s.mu.Lock()
	if s.state != SchedulerNew {
		state := s.state
		s.mu.Unlock()
		if state == SchedulerStarted {
			return ErrSchedulerStarted
		}
		return lifecycleError("start", state)
	}
	if err := parentCtx.Err(); err != nil {
		s.mu.Unlock()
		return fmt.Errorf("background scheduler start context: %w", err)
	}

	schedulerCtx, cancel := context.WithCancel(parentCtx)
	enabledRunners := make([]*jobRunner, 0, len(s.runners))
	for _, runner := range s.runners {
		if runner.definition.Enabled {
			enabledRunners = append(enabledRunners, runner)
		}
	}

	s.wg.Add(len(enabledRunners))
	s.cancel = cancel
	s.state = SchedulerStarted
	for _, runner := range enabledRunners {
		go func(runner *jobRunner) {
			defer s.wg.Done()
			runner.run(schedulerCtx)
		}(runner)
	}
	go s.awaitTerminal(schedulerCtx)
	s.mu.Unlock()
	return nil
}

func (s *Scheduler) awaitTerminal(schedulerCtx context.Context) {
	<-schedulerCtx.Done()

	s.mu.Lock()
	if s.state == SchedulerStarted {
		s.state = SchedulerStopping
	}
	s.mu.Unlock()

	s.wg.Wait()

	s.mu.Lock()
	if s.state != SchedulerStopped {
		s.state = SchedulerStopped
		close(s.done)
	}
	s.mu.Unlock()
}

// Trigger records a non-durable wake-up for one enabled Job. Acceptance means
// the signal was offered while STARTED; shutdown may still win before the Job
// begins because triggers are deliberately not durable events.
func (s *Scheduler) Trigger(jobID string) error {
	s.mu.RLock()
	runner, exists := s.runners[jobID]
	if !exists {
		s.mu.RUnlock()
		return fmt.Errorf("%w: %s", ErrJobNotFound, jobID)
	}
	if !runner.definition.Enabled {
		s.mu.RUnlock()
		return fmt.Errorf("%w: %s", ErrJobDisabled, jobID)
	}
	if s.state != SchedulerStarted {
		state := s.state
		s.mu.RUnlock()
		return lifecycleError("trigger", state)
	}
	runner.trigger()
	s.mu.RUnlock()
	return nil
}

// Shutdown is terminal. Calling it in NEW transitions directly to STOPPED.
// Calling it in STARTED cancels Scheduler-owned runner/job contexts. Every
// caller waits on the same completion signal and may stop waiting through its
// own context without changing the eventual terminal transition.
func (s *Scheduler) Shutdown(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("background scheduler shutdown context must not be nil")
	}

	s.mu.Lock()
	terminal := false
	switch s.state {
	case SchedulerNew:
		s.state = SchedulerStopped
		close(s.done)
		terminal = true
	case SchedulerStarted:
		s.state = SchedulerStopping
		s.cancel()
	case SchedulerStopping:
		// Another caller or parent cancellation already owns the transition.
	case SchedulerStopped:
		terminal = true
	}
	done := s.done
	s.mu.Unlock()
	if terminal {
		return nil
	}

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("background scheduler shutdown: %w", ctx.Err())
	}
}

func lifecycleError(operation string, state SchedulerState) error {
	return fmt.Errorf("%w: cannot %s while %s", ErrSchedulerLifecycle, operation, state)
}
