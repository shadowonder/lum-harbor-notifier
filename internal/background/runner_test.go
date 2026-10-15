package background

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// testJob is the smallest Job implementation needed by runner unit tests.
//
// Its behavior will be expanded later when runner execution, timeout, and
// shutdown semantics are introduced.
type testJob struct{}

func (testJob) Run(context.Context) error {
	return nil
}

// TestJobRunnerTriggerRecordsPendingSignal verifies that trigger() records one
// execution request without requiring a receiver to already be waiting.
func TestJobRunnerTriggerRecordsPendingSignal(t *testing.T) {
	runner := newJobRunner(
		JobDefinition{
			ID:            "test-job",
			Timeout:       5,
			OverlapPolicy: OverlapPolicySkip,
		},
		testJob{},
	)

	runner.trigger()

	select {
	case <-runner.triggerCh:
		// Expected.
	default:
		t.Fatal("expected trigger signal to be pending")
	}
}

// TestJobRunnerTriggerCoalescesRepeatedRequests verifies that repeated trigger
// requests do not accumulate multiple pending executions.
//
// Trigger is a wake-up signal rather than a durable event queue, so one
// pending signal is sufficient to represent any number of requests that arrive
// before the runner consumes it.
func TestJobRunnerTriggerCoalescesRepeatedRequests(t *testing.T) {
	runner := newJobRunner(
		JobDefinition{
			ID:            "test-job",
			Timeout:       5,
			OverlapPolicy: OverlapPolicySkip,
		},
		testJob{},
	)

	runner.trigger()
	runner.trigger()
	runner.trigger()

	select {
	case <-runner.triggerCh:
		// Expected first and only pending signal.
	default:
		t.Fatal("expected one pending trigger signal")
	}

	select {
	case <-runner.triggerCh:
		t.Fatal("expected repeated triggers to coalesce into one signal")
	default:
		// Expected: no second signal exists.
	}
}

// jobFunc adapts a function to the Job interface so runner tests can describe
// execution behavior without defining a dedicated Job type for every case.
type jobFunc func(context.Context) error

func (f jobFunc) Run(ctx context.Context) error {
	return f(ctx)
}

// TestJobRunnerExecuteRecordsSuccess verifies that a successful Job run
// updates the runner state with success and attempt timestamps.
func TestJobRunnerExecuteRecordsSuccess(t *testing.T) {
	runner := newJobRunner(
		JobDefinition{
			ID:            "successful-job",
			Timeout:       time.Second,
			OverlapPolicy: OverlapPolicySkip,
		},
		jobFunc(func(context.Context) error {
			return nil
		}),
	)

	runner.execute(context.Background())

	snapshot := runner.state.snapshot()

	if snapshot.Running {
		t.Fatal("expected completed Job not to be running")
	}
	if snapshot.LastAttemptAt == nil {
		t.Fatal("expected last attempt to be recorded")
	}
	if snapshot.LastSuccessAt == nil {
		t.Fatal("expected last success to be recorded")
	}
	if snapshot.LastFailureAt != nil {
		t.Fatalf("expected no failure, got %v", snapshot.LastFailureAt)
	}
	if snapshot.ConsecutiveFailures != 0 {
		t.Fatalf("expected zero consecutive failures, got %d", snapshot.ConsecutiveFailures)
	}
}

// TestJobRunnerExecuteRecordsFailure verifies that a failing Job run
// updates the runner state with the failure and increments the
// consecutive failure count.
func TestJobRunnerExecuteRecordsFailure(t *testing.T) {
	expectedErr := errors.New("rally unavailable")

	runner := newJobRunner(
		JobDefinition{
			ID:            "failing-job",
			Timeout:       time.Second,
			OverlapPolicy: OverlapPolicySkip,
		},
		jobFunc(func(context.Context) error {
			return expectedErr
		}),
	)

	runner.execute(context.Background())

	snapshot := runner.state.snapshot()

	if snapshot.Running {
		t.Fatal("expected failed Job not to be running")
	}
	if snapshot.LastFailureAt == nil {
		t.Fatal("expected last failure to be recorded")
	}
	if snapshot.LastError != expectedErr.Error() {
		t.Fatalf("expected error %q, got %q", expectedErr.Error(), snapshot.LastError)
	}
	if snapshot.ConsecutiveFailures != 1 {
		t.Fatalf("expected one consecutive failure, got %d", snapshot.ConsecutiveFailures)
	}
}

// TestJobRunnerExecuteRecordsTimeout verifies that a Job run exceeding its
// timeout is recorded as a failure with a deadline-exceeded error.
func TestJobRunnerExecuteRecordsTimeout(t *testing.T) {
	runner := newJobRunner(
		JobDefinition{
			ID:            "timeout-job",
			Timeout:       25 * time.Millisecond,
			OverlapPolicy: OverlapPolicySkip,
		},
		jobFunc(func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}),
	)

	runner.execute(context.Background())

	snapshot := runner.state.snapshot()

	if snapshot.Running {
		t.Fatal("expected timed-out Job not to be running")
	}
	if snapshot.LastFailureAt == nil {
		t.Fatal("expected timeout to be recorded as failure")
	}
	if snapshot.LastError != context.DeadlineExceeded.Error() {
		t.Fatalf("expected timeout error %q, got %q", context.DeadlineExceeded.Error(), snapshot.LastError)
	}
	if snapshot.ConsecutiveFailures != 1 {
		t.Fatalf("expected one consecutive failure, got %d", snapshot.ConsecutiveFailures)
	}
}

func TestJobRunnerRecordsDeadlineWhenJobReturnsNilLate(t *testing.T) {
	runner := newJobRunner(
		JobDefinition{ID: "late-nil", Timeout: 15 * time.Millisecond, OverlapPolicy: OverlapPolicySkip},
		jobFunc(func(ctx context.Context) error {
			<-ctx.Done()
			return nil
		}),
	)
	runner.execute(context.Background())
	snapshot := runner.state.snapshot()
	if snapshot.LastError != context.DeadlineExceeded.Error() || snapshot.ConsecutiveFailures != 1 {
		t.Fatalf("late nil result was not a timeout failure: %+v", snapshot)
	}
}

// TestJobRunnerRunExecutesTriggeredJob verifies the complete trigger path:
//
//	trigger()
//	    -> trigger channel
//	    -> runner event loop
//	    -> Job.Run
//
// The test uses channels for synchronization rather than arbitrary sleeps so
// it waits for actual lifecycle events instead of relying on machine timing.
func TestJobRunnerRunExecutesTriggeredJob(t *testing.T) {
	executed := make(chan struct{}, 1)

	runner := newJobRunner(
		JobDefinition{
			ID:            "triggered-job",
			Interval:      0,
			Timeout:       time.Second,
			Enabled:       true,
			OverlapPolicy: OverlapPolicySkip,
		},
		jobFunc(func(context.Context) error {
			executed <- struct{}{}
			return nil
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)
		runner.run(ctx)
	}()

	runner.trigger()

	select {
	case <-executed:
		// Expected.
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for triggered Job execution")
	}

	cancel()

	select {
	case <-done:
		// Expected.
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runner shutdown")
	}

	snapshot := runner.state.snapshot()

	if snapshot.LastSuccessAt == nil {
		t.Fatal("expected triggered execution to record success")
	}

	if snapshot.Running {
		t.Fatal("expected runner not to be running after shutdown")
	}
}

// TestJobRunnerRunExecutesPeriodicJob verifies that a positive Interval causes
// the runner to execute its Job without requiring an explicit trigger.
//
// The test observes the Job itself rather than sleeping for a guessed amount
// of time. The outer timeout exists only to prevent a broken runner from
// hanging the test suite forever.
func TestJobRunnerRunExecutesPeriodicJob(t *testing.T) {
	executed := make(chan struct{}, 1)

	runner := newJobRunner(
		JobDefinition{
			ID:            "periodic-job",
			Interval:      25 * time.Millisecond,
			Timeout:       time.Second,
			Enabled:       true,
			OverlapPolicy: OverlapPolicySkip,
		},
		jobFunc(func(context.Context) error {
			executed <- struct{}{}
			return nil
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)
		runner.run(ctx)
	}()

	select {
	case <-executed:
		// Expected.
	case <-time.After(time.Second):
		cancel()
		t.Fatal("timed out waiting for periodic Job execution")
	}

	cancel()

	select {
	case <-done:
		// Expected.
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runner shutdown")
	}
}

// TestJobRunnerRunCancelsActiveJobOnShutdown verifies that cancelling the
// runner lifecycle context propagates into the currently executing Job.
//
// Application shutdown is lifecycle cancellation rather than a Job failure, so
// the execution must stop without incrementing failure diagnostics.
func TestJobRunnerRunCancelsActiveJobOnShutdown(t *testing.T) {
	started := make(chan struct{}, 1)

	runner := newJobRunner(
		JobDefinition{
			ID:            "shutdown-job",
			Interval:      0,
			Timeout:       time.Minute,
			Enabled:       true,
			OverlapPolicy: OverlapPolicySkip,
		},
		jobFunc(func(ctx context.Context) error {
			started <- struct{}{}

			<-ctx.Done()

			return ctx.Err()
		}),
	)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)
		runner.run(ctx)
	}()

	runner.trigger()

	// Do not cancel until the Job has definitely entered Run().
	select {
	case <-started:
		// Expected.
	case <-time.After(time.Second):
		cancel()
		t.Fatal("timed out waiting for Job to start")
	}

	// This simulates application/Scheduler shutdown.
	cancel()

	select {
	case <-done:
		// Expected.
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for active Job to stop")
	}

	snapshot := runner.state.snapshot()

	if snapshot.Running {
		t.Fatal("expected cancelled Job not to remain running")
	}
	if snapshot.LastFailureAt != nil {
		t.Fatalf("expected shutdown cancellation not to record failure, got %v", snapshot.LastFailureAt)
	}
	if snapshot.LastError != "" {
		t.Fatalf("expected shutdown cancellation not to record error, got %q", snapshot.LastError)
	}
	if snapshot.ConsecutiveFailures != 0 {
		t.Fatalf("expected shutdown cancellation not to increment failures, got %d", snapshot.ConsecutiveFailures)
	}
}

func TestJobRunnerTriggerDuringRunCoalescesOneFollowUpWithoutOverlap(t *testing.T) {
	firstStarted := make(chan struct{})
	secondStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var calls atomic.Int32
	var active atomic.Int32
	var maxActive atomic.Int32

	runner := newJobRunner(
		JobDefinition{ID: "coalesced", Timeout: time.Second, Enabled: true, OverlapPolicy: OverlapPolicySkip},
		jobFunc(func(context.Context) error {
			current := active.Add(1)
			defer active.Add(-1)
			for {
				maximum := maxActive.Load()
				if current <= maximum || maxActive.CompareAndSwap(maximum, current) {
					break
				}
			}
			call := calls.Add(1)
			if call == 1 {
				close(firstStarted)
				<-releaseFirst
			} else if call == 2 {
				close(secondStarted)
			}
			return nil
		}),
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runner.run(ctx)
		close(done)
	}()

	runner.trigger()
	waitForSignal(t, firstStarted, "first coalesced execution")
	for index := 0; index < 20; index++ {
		runner.trigger()
	}
	close(releaseFirst)
	waitForSignal(t, secondStarted, "coalesced follow-up")

	time.Sleep(20 * time.Millisecond)
	if calls.Load() != 2 {
		t.Fatalf("expected exactly two executions, got %d", calls.Load())
	}
	if maxActive.Load() != 1 {
		t.Fatalf("same Job overlapped: max active %d", maxActive.Load())
	}
	cancel()
	waitForSignal(t, done, "coalesced runner stop")
}

func TestJobRunnerTriggerResetsFullFixedDelay(t *testing.T) {
	const interval = 120 * time.Millisecond
	executed := make(chan time.Time, 3)
	runner := newJobRunner(
		JobDefinition{ID: "fixed-delay", Interval: interval, Timeout: time.Second, Enabled: true, OverlapPolicy: OverlapPolicySkip},
		jobFunc(func(context.Context) error {
			executed <- time.Now()
			return nil
		}),
	)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runner.run(ctx)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	runner.trigger()
	first := waitForTime(t, executed, "triggered fixed-delay execution")
	select {
	case next := <-executed:
		t.Fatalf("periodic execution ran before full delay: %v", next.Sub(first))
	case <-time.After(70 * time.Millisecond):
	}
	second := waitForTime(t, executed, "periodic execution after reset")
	if delay := second.Sub(first); delay < 100*time.Millisecond {
		t.Fatalf("expected approximately full fixed delay, got %v", delay)
	}
	cancel()
	waitForSignal(t, done, "fixed-delay runner stop")
}

func TestDifferentJobRunnersExecuteConcurrently(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	makeRunner := func(id string) *jobRunner {
		return newJobRunner(
			JobDefinition{ID: id, Timeout: time.Second, Enabled: true, OverlapPolicy: OverlapPolicySkip},
			jobFunc(func(context.Context) error {
				started <- id
				<-release
				return nil
			}),
		)
	}
	first := makeRunner("first")
	second := makeRunner("second")
	ctx, cancel := context.WithCancel(context.Background())
	var wait sync.WaitGroup
	for _, runner := range []*jobRunner{first, second} {
		wait.Add(1)
		go func(runner *jobRunner) {
			defer wait.Done()
			runner.run(ctx)
		}(runner)
		runner.trigger()
	}
	waitForString(t, started, "first concurrent runner")
	waitForString(t, started, "second concurrent runner")
	close(release)
	cancel()
	wait.Wait()
}

func TestPriorFailureIsPreservedAcrossShutdownCancelledAttempt(t *testing.T) {
	secondStarted := make(chan struct{})
	var calls atomic.Int32
	runner := newJobRunner(
		JobDefinition{ID: "failure-then-cancel", Timeout: time.Second, OverlapPolicy: OverlapPolicySkip},
		jobFunc(func(ctx context.Context) error {
			if calls.Add(1) == 1 {
				return errors.New("prior failure")
			}
			close(secondStarted)
			<-ctx.Done()
			return ctx.Err()
		}),
	)
	runner.execute(context.Background())
	failed := runner.state.snapshot()
	time.Sleep(time.Millisecond)

	parent, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runner.execute(parent)
		close(done)
	}()
	waitForSignal(t, secondStarted, "cancelled attempt start")
	cancel()
	waitForSignal(t, done, "cancelled attempt completion")

	snapshot := runner.state.snapshot()
	if snapshot.Running || snapshot.LastError != "prior failure" || snapshot.ConsecutiveFailures != 1 {
		t.Fatalf("failure history not preserved: %+v", snapshot)
	}
	if snapshot.LastFailureAt == nil || !snapshot.LastFailureAt.Equal(*failed.LastFailureAt) {
		t.Fatalf("last failure changed across cancellation: before=%v after=%v", failed.LastFailureAt, snapshot.LastFailureAt)
	}
	if snapshot.LastAttemptAt == nil || failed.LastAttemptAt == nil || !snapshot.LastAttemptAt.After(*failed.LastAttemptAt) {
		t.Fatalf("latest attempt was not updated: before=%v after=%v", failed.LastAttemptAt, snapshot.LastAttemptAt)
	}
}

func waitForTime(t *testing.T, values <-chan time.Time, description string) time.Time {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
		return time.Time{}
	}
}

func waitForString(t *testing.T, values <-chan string, description string) string {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
		return ""
	}
}
