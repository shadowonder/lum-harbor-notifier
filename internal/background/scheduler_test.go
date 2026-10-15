package background

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestSchedulerRegisterAddsJob verifies that a valid registration becomes
// visible through the Scheduler runtime-state boundary.
func TestSchedulerRegisterAddsJob(t *testing.T) {
	scheduler := NewScheduler()

	err := scheduler.Register(
		JobDefinition{
			ID:            "reminder-sync",
			Interval:      time.Minute,
			Timeout:       10 * time.Second,
			Enabled:       true,
			OverlapPolicy: OverlapPolicySkip,
		},
		jobFunc(func(context.Context) error {
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("register Job: %v", err)
	}

	snapshot, err := scheduler.Snapshot("reminder-sync")

	if err != nil {
		t.Fatalf("snapshot Job: %v", err)
	}
	if snapshot.ID != "reminder-sync" {
		t.Fatalf("expected Job ID reminder-sync, got %q", snapshot.ID)
	}
}

// TestSchedulerRegisterRejectsDuplicateID verifies that Job identity is unique
// within one Scheduler registry.
func TestSchedulerRegisterRejectsDuplicateID(t *testing.T) {
	scheduler := NewScheduler()

	definition := JobDefinition{
		ID:            "reminder-sync",
		Timeout:       10 * time.Second,
		Enabled:       true,
		OverlapPolicy: OverlapPolicySkip,
	}

	job := jobFunc(func(context.Context) error {
		return nil
	})

	if err := scheduler.Register(definition, job); err != nil {
		t.Fatalf("register first Job: %v", err)
	}

	err := scheduler.Register(definition, job)

	if !errors.Is(err, ErrDuplicateJob) {
		t.Fatalf("expected ErrDuplicateJob, got %v", err)
	}
}

// TestSchedulerSnapshotRejectsUnknownJob verifies that callers cannot silently
// inspect a Job that was never registered.
func TestSchedulerSnapshotRejectsUnknownJob(t *testing.T) {
	scheduler := NewScheduler()

	_, err := scheduler.Snapshot("missing-job")

	if !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("expected ErrJobNotFound, got %v", err)
	}
}

// TestSchedulerRegisterRejectsInvalidDefinition verifies that Scheduler
// enforces JobDefinition validation at the registration boundary.
func TestSchedulerRegisterRejectsInvalidDefinition(t *testing.T) {
	scheduler := NewScheduler()

	err := scheduler.Register(
		JobDefinition{
			ID:            "invalid-job",
			Timeout:       0,
			Enabled:       true,
			OverlapPolicy: OverlapPolicySkip,
		},
		jobFunc(func(context.Context) error {
			return nil
		}),
	)

	if err == nil {
		t.Fatal("expected invalid JobDefinition to be rejected")
	}
}

// TestSchedulerStartRunsEnabledJob verifies that Start launches a runner for
// an enabled periodic Job.
func TestSchedulerStartRunsEnabledJob(t *testing.T) {
	scheduler := NewScheduler()

	executed := make(chan struct{}, 1)

	err := scheduler.Register(
		JobDefinition{
			ID:            "enabled-job",
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
	if err != nil {
		t.Fatalf("register Job: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	if err := scheduler.Start(ctx); err != nil {
		cancel()
		t.Fatalf("start Scheduler: %v", err)
	}

	select {
	case <-executed:
		// Expected.
	case <-time.After(time.Second):
		cancel()
		t.Fatal("timed out waiting for enabled Job execution")
	}

	cancel()

	done := make(chan struct{})

	go func() {
		scheduler.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Expected.
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Scheduler runners to stop")
	}
}

// TestSchedulerStartDoesNotRunDisabledJob verifies that disabled Jobs remain
// registered but do not receive an active runner goroutine.
func TestSchedulerStartDoesNotRunDisabledJob(t *testing.T) {
	scheduler := NewScheduler()

	executed := make(chan struct{}, 1)

	err := scheduler.Register(
		JobDefinition{
			ID:            "disabled-job",
			Interval:      10 * time.Millisecond,
			Timeout:       time.Second,
			Enabled:       false,
			OverlapPolicy: OverlapPolicySkip,
		},
		jobFunc(func(context.Context) error {
			executed <- struct{}{}
			return nil
		}),
	)
	if err != nil {
		t.Fatalf("register Job: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := scheduler.Start(ctx); err != nil {
		t.Fatalf("start Scheduler: %v", err)
	}

	select {
	case <-executed:
		t.Fatal("disabled Job unexpectedly executed")

	case <-time.After(75 * time.Millisecond):
		// Expected: several possible intervals passed without execution.
	}

	snapshot, err := scheduler.Snapshot("disabled-job")
	if err != nil {
		t.Fatalf("snapshot disabled Job: %v", err)
	}
	if snapshot.LastAttemptAt != nil {
		t.Fatalf("expected disabled Job to have no execution history, got %v", snapshot.LastAttemptAt)
	}
}

// TestSchedulerStartRejectsSecondStart verifies that Scheduler lifecycle is
// single-start in V1.
func TestSchedulerStartRejectsSecondStart(t *testing.T) {
	scheduler := NewScheduler()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := scheduler.Start(ctx); err != nil {
		t.Fatalf("first Start: %v", err)
	}

	err := scheduler.Start(ctx)

	if !errors.Is(err, ErrSchedulerStarted) {
		t.Fatalf("expected ErrSchedulerStarted, got %v", err)
	}
}

func TestSchedulerStartContextContract(t *testing.T) {
	scheduler := NewScheduler()
	if err := scheduler.Start(nil); err == nil {
		t.Fatal("expected nil context rejection")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := scheduler.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled context error, got %v", err)
	}
	if scheduler.State() != SchedulerNew {
		t.Fatalf("rejected starts must preserve NEW, got %s", scheduler.State())
	}
}

func TestSchedulerShutdownBeforeStartIsTerminal(t *testing.T) {
	scheduler := NewScheduler()
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if scheduler.State() != SchedulerStopped {
		t.Fatalf("expected STOPPED, got %s", scheduler.State())
	}
	if err := scheduler.Start(context.Background()); !errors.Is(err, ErrSchedulerLifecycle) {
		t.Fatalf("expected terminal Start rejection, got %v", err)
	}
	if err := scheduler.Register(validTestDefinition("late", true), jobFunc(func(context.Context) error { return nil })); !errors.Is(err, ErrSchedulerLifecycle) {
		t.Fatalf("expected terminal Register rejection, got %v", err)
	}
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatalf("repeated Shutdown: %v", err)
	}
}

func TestSchedulerTriggerLifecycleMatrix(t *testing.T) {
	scheduler := NewScheduler()
	if err := scheduler.Register(validTestDefinition("enabled", true), jobFunc(func(context.Context) error { return nil })); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Register(validTestDefinition("disabled", false), jobFunc(func(context.Context) error { return nil })); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Trigger("missing"); !errors.Is(err, ErrJobNotFound) {
		t.Fatalf("unknown trigger: %v", err)
	}
	if err := scheduler.Trigger("disabled"); !errors.Is(err, ErrJobDisabled) {
		t.Fatalf("disabled trigger: %v", err)
	}
	if err := scheduler.Trigger("enabled"); !errors.Is(err, ErrSchedulerLifecycle) {
		t.Fatalf("pre-start trigger: %v", err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Register(validTestDefinition("late", true), jobFunc(func(context.Context) error { return nil })); !errors.Is(err, ErrSchedulerLifecycle) {
		t.Fatalf("started Register: %v", err)
	}
	if err := scheduler.Trigger("enabled"); err != nil {
		t.Fatalf("started trigger: %v", err)
	}
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Trigger("enabled"); !errors.Is(err, ErrSchedulerLifecycle) {
		t.Fatalf("post-stop trigger: %v", err)
	}
}

func TestSchedulerStoppingRejectsOperationsAndEventuallyCompletes(t *testing.T) {
	scheduler := NewScheduler()
	started := make(chan struct{})
	release := make(chan struct{})
	if err := scheduler.Register(validTestDefinition("blocked", true), jobFunc(func(context.Context) error {
		close(started)
		<-release
		return nil
	})); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Trigger("blocked"); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, started, "blocked Job start")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := scheduler.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected bounded Shutdown timeout, got %v", err)
	}
	if scheduler.State() != SchedulerStopping {
		t.Fatalf("expected STOPPING, got %s", scheduler.State())
	}
	if err := scheduler.Trigger("blocked"); !errors.Is(err, ErrSchedulerLifecycle) {
		t.Fatalf("stopping Trigger: %v", err)
	}
	if err := scheduler.Start(context.Background()); !errors.Is(err, ErrSchedulerLifecycle) {
		t.Fatalf("stopping Start: %v", err)
	}
	if err := scheduler.Register(validTestDefinition("late", true), jobFunc(func(context.Context) error { return nil })); !errors.Is(err, ErrSchedulerLifecycle) {
		t.Fatalf("stopping Register: %v", err)
	}
	close(release)
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatalf("terminal Shutdown: %v", err)
	}
	if scheduler.State() != SchedulerStopped {
		t.Fatalf("expected STOPPED, got %s", scheduler.State())
	}
}

func TestSchedulerConcurrentShutdownSharesCompletion(t *testing.T) {
	scheduler := NewScheduler()
	started := make(chan struct{})
	if err := scheduler.Register(validTestDefinition("cooperative", true), jobFunc(func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	})); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Trigger("cooperative"); err != nil {
		t.Fatal(err)
	}
	waitForSignal(t, started, "cooperative Job start")

	const callers = 20
	var wait sync.WaitGroup
	errorsCh := make(chan error, callers)
	for index := 0; index < callers; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errorsCh <- scheduler.Shutdown(context.Background())
		}()
	}
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Errorf("concurrent Shutdown: %v", err)
		}
	}
	if scheduler.State() != SchedulerStopped {
		t.Fatalf("expected STOPPED, got %s", scheduler.State())
	}
}

func TestSchedulerParentCancellationCompletesLifecycle(t *testing.T) {
	scheduler := NewScheduler()
	parent, cancel := context.WithCancel(context.Background())
	if err := scheduler.Start(parent); err != nil {
		t.Fatal(err)
	}
	cancel()
	waitForSchedulerState(t, scheduler, SchedulerStopped)
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown after parent cancellation: %v", err)
	}
}

func TestSchedulerSnapshotsAreDetachedAndComplete(t *testing.T) {
	scheduler := NewScheduler()
	for index := 0; index < 3; index++ {
		id := fmt.Sprintf("job-%d", index)
		if err := scheduler.Register(validTestDefinition(id, false), jobFunc(func(context.Context) error { return nil })); err != nil {
			t.Fatal(err)
		}
	}
	snapshots := scheduler.Snapshots()
	if len(snapshots) != 3 {
		t.Fatalf("expected 3 snapshots, got %d", len(snapshots))
	}
	snapshots[0].ID = "mutated"
	fresh := scheduler.Snapshots()
	for _, snapshot := range fresh {
		if snapshot.ID == "mutated" {
			t.Fatal("snapshot mutation reached Scheduler state")
		}
	}
}

func validTestDefinition(id string, enabled bool) JobDefinition {
	return JobDefinition{
		ID: id, Timeout: time.Second, Enabled: enabled, OverlapPolicy: OverlapPolicySkip,
	}
}

func waitForSchedulerState(t *testing.T, scheduler *Scheduler, expected SchedulerState) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if scheduler.State() == expected {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("expected Scheduler state %s, got %s", expected, scheduler.State())
}

func waitForSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}
