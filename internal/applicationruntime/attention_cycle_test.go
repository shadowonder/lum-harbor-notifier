package applicationruntime

import (
	"context"
	"database/sql"
	"errors"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/background"
	"lum-harbor-notifier/internal/database"
	"lum-harbor-notifier/internal/testsupport"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

type recordingCycle struct {
	calls        []string
	reconcileErr error
	dispatchErr  error
}

func (c *recordingCycle) Reconcile(context.Context, time.Time) (attention.ReconcileResult, error) {
	c.calls = append(c.calls, "reconcile")
	return attention.ReconcileResult{}, c.reconcileErr
}

func (c *recordingCycle) DispatchPendingNotifications(context.Context, time.Time) (attention.DispatchResult, error) {
	c.calls = append(c.calls, "dispatch")
	return attention.DispatchResult{}, c.dispatchErr
}

func fixedClock(at time.Time) func() time.Time { return func() time.Time { return at } }

func TestAttentionCycleReconcilesBeforeDispatchingAndStopsOnReconcileFailure(t *testing.T) {
	cycle := &recordingCycle{}
	job := attentionCycleJob{cycle: cycle, clock: fixedClock(time.Now())}
	if err := job.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Join(cycle.calls, ",") != "reconcile,dispatch" {
		t.Fatalf("Reconcile must run before Dispatch: %v", cycle.calls)
	}

	failing := &recordingCycle{reconcileErr: errors.New("boom")}
	err := attentionCycleJob{cycle: failing, clock: fixedClock(time.Now())}.Run(context.Background())
	if err == nil || !errors.Is(err, failing.reconcileErr) || strings.Join(failing.calls, ",") != "reconcile" {
		t.Fatalf("a Reconcile failure must fail the job and skip Dispatch (never send unaligned notifications): err=%v calls=%v", err, failing.calls)
	}

	dispatchFails := &recordingCycle{dispatchErr: errors.New("dispatch boom")}
	if err := (attentionCycleJob{cycle: dispatchFails, clock: fixedClock(time.Now())}).Run(context.Background()); !errors.Is(err, dispatchFails.dispatchErr) {
		t.Fatalf("a Dispatch failure must be returned, not swallowed: %v", err)
	}
}

func TestAttentionCycleFailuresReachSchedulerMetadata(t *testing.T) {
	scheduler := background.NewScheduler()
	cycle := &recordingCycle{reconcileErr: errors.New("reconcile failed")}
	definition, err := registerAttentionCycle(scheduler, cycle, 10*time.Millisecond, time.Second, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "consecutive failures recorded", func() bool {
		snapshot, err := scheduler.Snapshot(definition.ID)
		return err == nil && snapshot.ConsecutiveFailures >= 2 && strings.Contains(snapshot.LastError, "reconcile failed")
	})
	shutdown(t, scheduler)
}

func TestAttentionCycleRegistrationValidation(t *testing.T) {
	if _, err := RegisterAttentionCycle(nil, &recordingCycle{}, time.Minute, time.Second); err == nil {
		t.Fatal("a nil scheduler must be rejected")
	}
	if _, err := RegisterAttentionCycle(background.NewScheduler(), nil, time.Minute, time.Second); err == nil {
		t.Fatal("a nil cycle must be rejected")
	}
	scheduler := background.NewScheduler()
	if _, err := RegisterAttentionCycle(scheduler, &recordingCycle{}, time.Minute, time.Second); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterAttentionCycle(scheduler, &recordingCycle{}, time.Minute, time.Second); err == nil {
		t.Fatal("the cycle is a single static job; a duplicate registration must be rejected")
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := RegisterAttentionCycle(scheduler, &recordingCycle{}, time.Minute, time.Second); err == nil {
		t.Fatal("registration after Start must be rejected")
	}
	shutdown(t, scheduler)
}

func waitFor(t *testing.T, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func shutdown(t *testing.T, scheduler *background.Scheduler) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := scheduler.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Shutdown(ctx); err != nil {
		t.Fatalf("repeated shutdown must stay unchanged: %v", err)
	}
}

type services struct {
	db        *sql.DB
	workItems *workitem.Service
	tracking  *tracking.Service
	attention *attention.Service
}

func newServices(db *sql.DB) services {
	workItems := workitem.NewService(db, workitem.NewRepository(db))
	trackingService := tracking.NewService(db, workItems)
	return services{db: db, workItems: workItems, tracking: trackingService, attention: attention.NewService(db, trackingService)}
}

var (
	monitorDue = time.Date(2026, 10, 15, 22, 0, 0, 0, time.UTC)
	cycleNow   = monitorDue.Add(2 * time.Hour)
)

func (s services) overdueWorkItem(t *testing.T, title string) *workitem.WorkItem {
	t.Helper()
	ctx := context.Background()
	wi, err := s.workItems.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: title})
	if err != nil {
		t.Fatal(err)
	}
	completed := workitem.LifecycleCompleted
	if _, err := s.tracking.CreateMonitorPoint(ctx, tracking.CreateMonitorPointInput{
		WorkItemID: wi.ID, Kind: tracking.MonitorFinalTarget, Title: title + " target", DueAt: monitorDue, Timezone: "UTC", ExpectedWorkItemLifecycle: &completed,
	}); err != nil {
		t.Fatal(err)
	}
	return wi
}

func stateByWorkItem(t *testing.T, db *sql.DB) map[string]string {
	t.Helper()
	rows, err := db.Query("SELECT work_item_id, state FROM notifications")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	states := map[string]string{}
	for rows.Next() {
		var wi, state string
		if err := rows.Scan(&wi, &state); err != nil {
			t.Fatal(err)
		}
		states[wi] = state
	}
	return states
}

func count(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRealSchedulerRunsTheCycleAndDrainsPendingNotificationsAlongsideOtherJobs(t *testing.T) {
	db := testsupport.OpenTestDatabase(t)
	s := newServices(db.DB)
	wi := s.overdueWorkItem(t, "Release")

	scheduler := background.NewScheduler()
	var otherRuns atomic.Int32
	other := background.JobDefinition{ID: "other.static.job", Interval: 10 * time.Millisecond, Timeout: time.Second, Enabled: true, OverlapPolicy: background.OverlapPolicySkip}
	if err := scheduler.Register(other, jobFunc(func(context.Context) error { otherRuns.Add(1); return nil })); err != nil {
		t.Fatal(err)
	}
	definition, err := registerAttentionCycle(scheduler, s.attention, 10*time.Millisecond, 5*time.Second, fixedClock(cycleNow))
	if err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "PENDING notification delivered by the production cycle", func() bool {
		return stateByWorkItem(t, db.DB)[wi.ID] == "SENT"
	})
	waitFor(t, "the other static job runs alongside", func() bool { return otherRuns.Load() > 0 })
	shutdown(t, scheduler)

	if count(t, db.DB, "SELECT COUNT(*) FROM attention_issues") != 1 || count(t, db.DB, "SELECT COUNT(*) FROM notifications") != 1 {
		t.Fatal("repeated cycles must not duplicate Attention or Notifications")
	}
	snapshot, err := scheduler.Snapshot(definition.ID)
	if err != nil || snapshot.ConsecutiveFailures != 0 || snapshot.LastSuccessAt == nil {
		t.Fatalf("the cycle must succeed under the real Scheduler: %+v err=%v", snapshot, err)
	}
	// The database stays usable after cooperative shutdown.
	if count(t, db.DB, "SELECT COUNT(*) FROM notifications WHERE state='SENT'") != 1 {
		t.Fatal("database must remain usable after shutdown")
	}
}

func TestPendingStateIsRecoveredAcrossRestartByANewSchedulerLifetime(t *testing.T) {
	db := testsupport.OpenTestDatabase(t)
	s := newServices(db.DB)
	ctx := context.Background()
	send := s.overdueWorkItem(t, "Send")
	snoozed := s.overdueWorkItem(t, "Snoozed")
	cancel := s.overdueWorkItem(t, "Cancel")

	// Lifetime 1: Reconcile only — all three notifications are durably PENDING.
	if _, err := s.attention.Reconcile(ctx, cycleNow); err != nil {
		t.Fatal(err)
	}
	issues, err := s.attention.ListAttentionIssues(ctx)
	if err != nil || len(issues) != 3 {
		t.Fatalf("issues: %d err=%v", len(issues), err)
	}
	for _, issue := range issues {
		if issue.WorkItemID != nil && *issue.WorkItemID == snoozed.ID {
			// The service clock is the real time (before the fixed cycle time), so a far-future snooze is valid and still active at cycleNow.
			if _, err := s.attention.SnoozeAttentionIssue(ctx, attention.SnoozeInput{ID: issue.ID, Until: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := s.workItems.SetLocalWorkItemLifecycleState(ctx, workitem.SetLocalWorkItemLifecycleStateInput{ID: cancel.ID, Target: workitem.LifecycleCompleted}); err != nil {
		t.Fatal(err)
	}
	for wi, state := range stateByWorkItem(t, db.DB) {
		if state != "PENDING" {
			t.Fatalf("%s must be PENDING before restart, got %s", wi, state)
		}
	}
	if err := db.DB.Close(); err != nil {
		t.Fatal(err)
	}

	// Lifetime 2: reopen the same file-backed database with a brand-new Scheduler.
	reopened, err := database.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.DB.Close() })
	s2 := newServices(reopened.DB)
	scheduler := background.NewScheduler()
	if _, err := registerAttentionCycle(scheduler, s2.attention, 10*time.Millisecond, 5*time.Second, fixedClock(cycleNow)); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Start(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "restart drains pending state", func() bool {
		states := stateByWorkItem(t, reopened.DB)
		return states[send.ID] == "SENT" && states[cancel.ID] == "CANCELLED"
	})
	shutdown(t, scheduler)

	states := stateByWorkItem(t, reopened.DB)
	if states[send.ID] != "SENT" || states[cancel.ID] != "CANCELLED" || states[snoozed.ID] != "PENDING" {
		t.Fatalf("unexpected recovery result: %+v", states)
	}
	if count(t, reopened.DB, "SELECT COUNT(*) FROM attention_issues") != 3 || count(t, reopened.DB, "SELECT COUNT(*) FROM notifications") != 3 {
		t.Fatal("a restart must not duplicate Attention or Notifications")
	}
}

func TestAttentionCycleIsAIFree(t *testing.T) {
	src, _ := filepath.Glob("attention_cycle.go")
	for _, file := range src {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range parsed.Imports {
			for _, banned := range []string{"internal/ai\"", "internal/assistant", "internal/proposal", "internal/openai", "internal/plugin", "internal/sourceprocessing"} {
				if strings.Contains(spec.Path.Value, banned) {
					t.Fatalf("the Attention cycle must be AI-independent: imports %s", banned)
				}
			}
		}
	}
}
