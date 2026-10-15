package background

import (
	"context"
	"errors"
	"time"
)

// jobRunner owns the execution lifecycle of one registered Job.
//
// Each runner is intended to be driven by exactly one event-loop goroutine.
// That single-owner model serializes executions of the same Job without
// requiring a mutex around Job.Run itself.
//
// Different jobRunner instances may execute concurrently, which allows one
// slow Job to make progress independently from unrelated background work.
//
// A runner combines:
//   - the business operation represented by Job;
//   - the scheduling policy represented by JobDefinition;
//   - mutable diagnostic state represented by jobRuntimeState;
//   - a bounded trigger signal used to request an execution.
//
// Scheduling orchestration across multiple runners belongs to Scheduler.
// jobRunner is deliberately package-private because callers should interact
// with background work through Scheduler rather than controlling individual
// runners directly.
type jobRunner struct {
	definition JobDefinition
	job        Job
	state      *jobRuntimeState
	triggerCh  chan struct{}
}

// newJobRunner creates the runtime controller for one validated Job registration.
func newJobRunner(definition JobDefinition, job Job) *jobRunner {
	return &jobRunner{
		definition: definition,
		job:        job,
		state:      newJobRuntimeState(definition.ID),
		triggerCh:  make(chan struct{}, 1),
	}
}

// trigger requests that this runner execute its Job as soon as possible.
//
// Trigger delivery is intentionally non-blocking. The channel has capacity
// one, so at most one pending execution request is retained while the runner
// is busy or has not yet consumed a previous trigger.
//
// Multiple trigger calls therefore coalesce into one pending signal:
//
//	trigger()
//	trigger()
//	trigger()
//
// becomes:
//
//	one pending execution
//
// This is appropriate because Trigger represents "state may have changed,
// check again" rather than a durable business event. Durable facts belong in
// persistent storage rather than in this in-memory channel.
func (r *jobRunner) trigger() {
	select {
	case r.triggerCh <- struct{}{}:
		// Trigger job to run
	default:
		// There is a trigger already pending, so the request is coalesced.
	}
}

// run owns the event loop for one registered Job.
//
// The runner waits for one of three events:
//
//   - the application context is cancelled;
//   - an explicit trigger requests an execution;
//   - the Job's periodic interval expires.
//
// Job executions are serialized because this event loop calls execute
// synchronously. While execute is running, this goroutine cannot start another
// execution of the same Job. Other jobRunner instances remain independent and
// may execute concurrently.
//
// Periodic scheduling uses fixed-delay semantics. After every execution,
// regardless of whether it was started by a timer or an explicit trigger, the
// periodic delay begins again from zero.
func (r *jobRunner) run(ctx context.Context) {
	for {
		timer, timerCh := r.newPeriodicTimer()

		select {
		case <-ctx.Done():
			stopTimer(timer)
			r.state.setNextRun(nil)
			return

		case <-r.triggerCh:
			stopTimer(timer)
			r.state.setNextRun(nil)

			r.execute(ctx)

		case <-timerCh:
			r.state.setNextRun(nil)

			r.execute(ctx)
		}
	}
}

// newPeriodicTimer creates the timer for the next fixed-delay execution.
//
// A Job with Interval == 0 is trigger-only. In that case the returned channel
// is nil. A nil channel is disabled inside a Go select statement, allowing the
// same event loop to support both periodic and trigger-only Jobs without a
// separate polling loop.
func (r *jobRunner) newPeriodicTimer() (*time.Timer, <-chan time.Time) {
	if r.definition.Interval == 0 {
		r.state.setNextRun(nil)
		return nil, nil
	}

	nextRunAt := time.Now().Add(r.definition.Interval)
	r.state.setNextRun(&nextRunAt)

	timer := time.NewTimer(r.definition.Interval)

	return timer, timer.C
}

// stopTimer stops a periodic timer that is no longer needed.
//
// A trigger may arrive before the periodic timer expires. In that case the old
// timer must be stopped because fixed-delay scheduling restarts only after the
// triggered execution completes.
func stopTimer(timer *time.Timer) {
	if timer == nil {
		return
	}
	timer.Stop()
}

// execute runs one execution of the Job and records its runtime outcome.
//
// Each execution receives its own child context with the timeout declared by
// JobDefinition. The Job is expected to cooperate with cancellation by
// observing ctx.Done(), returning ctx.Err(), or passing the context to
// cancellable downstream operations such as database or RPC calls.
//
// Timeout is cooperative rather than preemptive: Go cannot forcibly terminate
// an arbitrary goroutine that ignores its context. A misbehaving Job that
// never returns can therefore outlive its timeout until that Job implementation
// is fixed.
//
// Application shutdown cancellation is treated differently from a Job failure.
// A parent-context cancellation stops the execution without incrementing
// failure diagnostics, while an execution timeout or business error is recorded
// as a failure.
func (r *jobRunner) execute(parentCtx context.Context) {
	// Avoid beginning new work when the runner lifecycle has already been
	// cancelled.
	if parentCtx.Err() != nil {
		return
	}

	executionCtx, cancel := context.WithTimeout(parentCtx, r.definition.Timeout)
	defer cancel()

	startedAt := time.Now()
	r.state.markStarted(startedAt)

	err := r.job.Run(executionCtx)

	finishedAt := time.Now()
	duration := finishedAt.Sub(startedAt)

	// Parent cancellation represents runner/application shutdown rather than a
	// failure of the Job itself.
	if parentCtx.Err() != nil {
		r.state.markCancelled(duration)
		return
	}

	// A deadline exceeded by this execution is a Job failure even if a
	// misbehaving Job happens to return nil after its context expired.
	if errors.Is(executionCtx.Err(), context.DeadlineExceeded) {
		r.state.markFailed(finishedAt, duration, context.DeadlineExceeded)
		return
	}
	if err != nil {
		r.state.markFailed(finishedAt, duration, err)
		return
	}

	r.state.markSucceeded(finishedAt, duration)
}
