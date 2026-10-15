package background

import (
	"errors"
	"testing"
	"time"
)

// TestJobRuntimeStateInitialSnapshot verifies that a newly registered Job has
// no fabricated execution history.
//
// A Job that has never run must be distinguishable from a Job that previously
// ran at some zero or sentinel timestamp.
func TestJobRuntimeStateInitialSnapshot(t *testing.T) {
	state := newJobRuntimeState("reminder-sync")

	snapshot := state.snapshot()

	if snapshot.ID != "reminder-sync" {
		t.Fatalf("expected ID reminder-sync, got %q", snapshot.ID)
	}
	if snapshot.Running {
		t.Fatal("new Job must not be running")
	}
	if snapshot.LastAttemptAt != nil {
		t.Fatalf("expected no last attempt, got %v", snapshot.LastAttemptAt)
	}
	if snapshot.LastSuccessAt != nil {
		t.Fatalf("expected no last success, got %v", snapshot.LastSuccessAt)
	}
	if snapshot.LastFailureAt != nil {
		t.Fatalf("expected no last failure, got %v", snapshot.LastFailureAt)
	}
	if snapshot.LastDuration != 0 {
		t.Fatalf("expected zero duration, got %v", snapshot.LastDuration)
	}
	if snapshot.LastError != "" {
		t.Fatalf("expected empty last error, got %q", snapshot.LastError)
	}
	if snapshot.ConsecutiveFailures != 0 {
		t.Fatalf("expected zero consecutive failures, got %d", snapshot.ConsecutiveFailures)
	}
	if snapshot.NextRunAt != nil {
		t.Fatalf("expected no next run, got %v", snapshot.NextRunAt)
	}
}

// TestJobRuntimeStateSuccessfulExecution verifies the normal lifecycle of one
// successful Job execution.
func TestJobRuntimeStateSuccessfulExecution(t *testing.T) {
	state := newJobRuntimeState("reminder-sync")

	startedAt := time.Date(2026, time.September, 16, 10, 0, 0, 0, time.UTC)

	finishedAt := startedAt.Add(2 * time.Second)

	state.markStarted(startedAt)

	runningSnapshot := state.snapshot()

	if !runningSnapshot.Running {
		t.Fatal("expected Job to be running after markStarted")
	}

	assertTimePointerEqual(t, runningSnapshot.LastAttemptAt, startedAt, "last attempt")

	state.markSucceeded(finishedAt, finishedAt.Sub(startedAt))

	completedSnapshot := state.snapshot()

	if completedSnapshot.Running {
		t.Fatal("expected Job not to be running after success")
	}

	assertTimePointerEqual(t, completedSnapshot.LastSuccessAt, finishedAt, "last success")

	if completedSnapshot.LastDuration != 2*time.Second {
		t.Fatalf("expected duration 2s, got %v", completedSnapshot.LastDuration)
	}
	if completedSnapshot.LastError != "" {
		t.Fatalf("expected successful execution to clear last error, got %q", completedSnapshot.LastError)
	}
	if completedSnapshot.ConsecutiveFailures != 0 {
		t.Fatalf("expected successful execution to reset failure count, got %d", completedSnapshot.ConsecutiveFailures)
	}
}

// TestJobRuntimeStateFailureHistory verifies that consecutive failures are
// accumulated and that a later success resets the active failure condition
// without deleting historical failure timing information.
func TestJobRuntimeStateFailureHistory(t *testing.T) {
	state := newJobRuntimeState("rally-sync")

	firstStart := time.Date(2026, time.September, 16, 10, 0, 0, 0, time.UTC)
	firstFinish := firstStart.Add(time.Second)

	state.markStarted(firstStart)
	state.markFailed(firstFinish, time.Second, errors.New("first failure"))

	firstFailureSnapshot := state.snapshot()

	if firstFailureSnapshot.ConsecutiveFailures != 1 {
		t.Fatalf("expected 1 consecutive failure, got %d", firstFailureSnapshot.ConsecutiveFailures)
	}
	if firstFailureSnapshot.LastError != "first failure" {
		t.Fatalf("expected first failure error, got %q", firstFailureSnapshot.LastError)
	}

	assertTimePointerEqual(t, firstFailureSnapshot.LastFailureAt, firstFinish, "first failure time")

	secondStart := firstFinish.Add(time.Minute)
	secondFinish := secondStart.Add(2 * time.Second)

	state.markStarted(secondStart)
	state.markFailed(secondFinish, 2*time.Second, errors.New("second failure"))

	secondFailureSnapshot := state.snapshot()

	if secondFailureSnapshot.ConsecutiveFailures != 2 {
		t.Fatalf("expected 2 consecutive failures, got %d", secondFailureSnapshot.ConsecutiveFailures)
	}
	if secondFailureSnapshot.LastError != "second failure" {
		t.Fatalf("expected second failure error, got %q", secondFailureSnapshot.LastError)
	}

	assertTimePointerEqual(t, secondFailureSnapshot.LastFailureAt, secondFinish, "second failure time")

	successStart := secondFinish.Add(time.Minute)
	successFinish := successStart.Add(500 * time.Millisecond)

	state.markStarted(successStart)
	state.markSucceeded(successFinish, 500*time.Millisecond)

	successSnapshot := state.snapshot()

	if successSnapshot.ConsecutiveFailures != 0 {
		t.Fatalf("expected success to reset consecutive failures, got %d", successSnapshot.ConsecutiveFailures)
	}
	if successSnapshot.LastError != "" {
		t.Fatalf("expected success to clear last error, got %q", successSnapshot.LastError)
	}

	// Historical failure timing remains useful even after recovery.
	assertTimePointerEqual(t, successSnapshot.LastFailureAt, secondFinish, "preserved last failure time")
	assertTimePointerEqual(t, successSnapshot.LastSuccessAt, successFinish, "last success time")
}

// TestJobRuntimeStateNextRun verifies periodic scheduling metadata, including
// the valid nil state used by trigger-only Jobs or by stopped runners.
func TestJobRuntimeStateNextRun(t *testing.T) {
	state := newJobRuntimeState("notification-evaluation")

	nextRunAt := time.Date(2026, time.September, 16, 10, 5, 0, 0, time.UTC)

	state.setNextRun(&nextRunAt)

	snapshot := state.snapshot()

	assertTimePointerEqual(t, snapshot.NextRunAt, nextRunAt, "next run")

	state.setNextRun(nil)

	snapshot = state.snapshot()

	if snapshot.NextRunAt != nil {
		t.Fatalf("expected next run to be cleared, got %v", snapshot.NextRunAt)
	}
}

// TestJobRuntimeStateSnapshotIsDetached verifies that callers cannot mutate
// Scheduler-owned timestamp state through pointers returned by snapshot().
func TestJobRuntimeStateSnapshotIsDetached(t *testing.T) {
	state := newJobRuntimeState("source-interpretation")

	startedAt := time.Date(2026, time.September, 16, 11, 0, 0, 0, time.UTC)

	state.markStarted(startedAt)

	snapshot := state.snapshot()

	if snapshot.LastAttemptAt == nil {
		t.Fatal("expected last attempt to be populated")
	}

	// Deliberately mutate the caller-owned snapshot.
	*snapshot.LastAttemptAt = startedAt.Add(24 * time.Hour)

	freshSnapshot := state.snapshot()

	// Internal state must still contain the original timestamp.
	assertTimePointerEqual(t, freshSnapshot.LastAttemptAt, startedAt, "detached last attempt")
}

func TestJobRuntimeStateSnapshotDetachesEveryTimestamp(t *testing.T) {
	state := newJobRuntimeState("all-timestamps")
	attempt := time.Date(2026, time.September, 17, 10, 0, 0, 0, time.UTC)
	failure := attempt.Add(time.Second)
	success := failure.Add(time.Second)
	next := success.Add(time.Minute)
	state.markStarted(attempt)
	state.markFailed(failure, time.Second, errors.New("failure"))
	state.markStarted(success)
	state.markSucceeded(success, 0)
	state.setNextRun(&next)

	snapshot := state.snapshot()
	for _, value := range []*time.Time{snapshot.LastAttemptAt, snapshot.LastSuccessAt, snapshot.LastFailureAt, snapshot.NextRunAt} {
		if value == nil {
			t.Fatal("expected populated timestamp")
		}
		*value = value.Add(24 * time.Hour)
	}
	fresh := state.snapshot()
	assertTimePointerEqual(t, fresh.LastAttemptAt, success, "last attempt")
	assertTimePointerEqual(t, fresh.LastSuccessAt, success, "last success")
	assertTimePointerEqual(t, fresh.LastFailureAt, failure, "last failure")
	assertTimePointerEqual(t, fresh.NextRunAt, next, "next run")
}

// assertTimePointerEqual keeps timestamp assertions readable while preserving
// the important distinction between nil and an actual time value.
func assertTimePointerEqual(t *testing.T, actual *time.Time, expected time.Time, name string) {
	t.Helper()
	if actual == nil {
		t.Fatalf("expected %s to be %v, got nil", name, expected)
	}
	if !actual.Equal(expected) {
		t.Fatalf("expected %s to be %v, got %v", name, expected, *actual)
	}
}
