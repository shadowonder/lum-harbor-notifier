package applicationruntime

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/background"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/reminder"
	"lum-harbor-notifier/internal/testsupport"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

type reminderCycleFixture struct {
	ctx       context.Context
	db        *sql.DB
	planner   *planner.Service
	reminder  *reminder.Service
	attention *attention.Service
	evaluator *TodoReminderEvaluator
}

func newReminderCycleFixture(t *testing.T) *reminderCycleFixture {
	t.Helper()
	testDB := testsupport.OpenTestDatabase(t)
	db := testDB.DB

	plannerRepo := planner.NewRepository(db)
	plannerService := planner.NewService(db, plannerRepo)
	workItems := workitem.NewService(db, workitem.NewRepository(db))
	trackingService := tracking.NewService(db, workItems)
	attentionService := attention.NewService(db, trackingService)
	reminderService := reminder.NewService(db)

	evaluator := NewTodoReminderCycle(db, reminderService, attentionService)

	return &reminderCycleFixture{
		ctx:       context.Background(),
		db:        db,
		planner:   plannerService,
		reminder:  reminderService,
		attention: attentionService,
		evaluator: evaluator,
	}
}

func TestTodoReminderCycleThresholdTriggeringAndIdempotency(t *testing.T) {
	f := newReminderCycleFixture(t)
	now := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	dueAt := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)

	// Create local Todo due at 10:00
	todo, err := f.planner.CreateLocalTodo(f.ctx, planner.CreateLocalTodoInput{
		Title: "Prepare release notes",
		DueAt: &dueAt,
	})
	if err != nil {
		t.Fatalf("CreateLocalTodo failed: %v", err)
	}

	// Set reminder with offsets [900, 0] (15 minutes before, and at due time)
	rem, err := f.reminder.SetReminder(f.ctx, reminder.SetReminderInput{
		PlannerItemID:  todo.ID,
		OffsetsSeconds: []int{900, 0},
	})
	if err != nil {
		t.Fatalf("SetReminder failed: %v", err)
	}
	if rem.State != reminder.StateActive {
		t.Fatalf("expected reminder to be ACTIVE, got %v", rem.State)
	}

	// 1. Clock at 09:30 (30m before due) -> neither 900s (09:45) nor 0s (10:00) reached.
	t30 := now.Add(30 * time.Minute)
	res30, err := f.evaluator.EvaluateReminders(f.ctx, t30)
	if err != nil {
		t.Fatalf("EvaluateReminders at 09:30 failed: %v", err)
	}
	if res30.Evaluated != 1 || res30.Fired != 0 {
		t.Fatalf("expected 1 evaluated, 0 fired at 09:30, got %+v", res30)
	}

	issues, err := f.attention.ListAttentionIssues(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 0 {
		t.Fatalf("expected 0 issues at 09:30, got %d", len(issues))
	}

	// 2. Clock at 09:45 (15m before due) -> 900s threshold reached!
	t45 := now.Add(45 * time.Minute)
	res45, err := f.evaluator.EvaluateReminders(f.ctx, t45)
	if err != nil {
		t.Fatalf("EvaluateReminders at 09:45 failed: %v", err)
	}
	if res45.Fired != 1 {
		t.Fatalf("expected 1 fired at 09:45, got %+v", res45)
	}

	issues, err = f.attention.ListAttentionIssues(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue at 09:45, got %d", len(issues))
	}
	if issues[0].Type != attention.IssueTodoReminderDue {
		t.Fatalf("expected IssueTodoReminderDue, got %v", issues[0].Type)
	}
	if issues[0].PlannerItemID == nil || *issues[0].PlannerItemID != todo.ID {
		t.Fatalf("expected planner_item_id %s, got %v", todo.ID, issues[0].PlannerItemID)
	}

	// Notifications must be dispatched to SENT
	notifViews, err := f.attention.ListNotificationViews(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(notifViews) != 1 {
		t.Fatalf("expected 1 notification at 09:45, got %d", len(notifViews))
	}
	if notifViews[0].Notification.State != attention.DeliverySent {
		t.Fatalf("expected notification to be SENT, got %v", notifViews[0].Notification.State)
	}
	if notifViews[0].Notification.PlannerItemID == nil || *notifViews[0].Notification.PlannerItemID != todo.ID {
		t.Fatalf("expected planner_item_id %s on notification, got %v", todo.ID, notifViews[0].Notification.PlannerItemID)
	}

	// Check reminder fired_offsets
	updatedRem, err := f.reminder.GetByPlannerItemID(f.ctx, todo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(updatedRem.FiredOffsets) != 1 || updatedRem.FiredOffsets[0] != 900 {
		t.Fatalf("expected fired offsets [900], got %v", updatedRem.FiredOffsets)
	}

	// 3. Restart-safe idempotency: run again at 09:50 -> 0 new fired.
	t50 := now.Add(50 * time.Minute)
	res50, err := f.evaluator.EvaluateReminders(f.ctx, t50)
	if err != nil {
		t.Fatalf("EvaluateReminders at 09:50 failed: %v", err)
	}
	if res50.Fired != 0 {
		t.Fatalf("expected 0 fired at 09:50, got %+v", res50)
	}
	issues, _ = f.attention.ListAttentionIssues(f.ctx)
	if len(issues) != 1 {
		t.Fatalf("expected still 1 issue at 09:50, got %d", len(issues))
	}

	// 4. Clock at 10:00 (due time) -> 0s threshold reached!
	t100 := now.Add(60 * time.Minute)
	res100, err := f.evaluator.EvaluateReminders(f.ctx, t100)
	if err != nil {
		t.Fatalf("EvaluateReminders at 10:00 failed: %v", err)
	}
	if res100.Fired != 1 {
		t.Fatalf("expected 1 fired at 10:00, got %+v", res100)
	}

	issues, _ = f.attention.ListAttentionIssues(f.ctx)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue at 10:00 (updated to latest urgency), got %d", len(issues))
	}
	if !strings.Contains(issues[0].Reason, "due now") {
		t.Fatalf("expected issue reason to be updated to 'due now', got %q", issues[0].Reason)
	}
	notifViews, _ = f.attention.ListNotificationViews(f.ctx)
	if len(notifViews) != 2 {
		t.Fatalf("expected 2 notifications at 10:00, got %d", len(notifViews))
	}

	updatedRem, _ = f.reminder.GetByPlannerItemID(f.ctx, todo.ID)
	if len(updatedRem.FiredOffsets) != 2 {
		t.Fatalf("expected 2 fired offsets, got %v", updatedRem.FiredOffsets)
	}

	// 5. Run again at 10:05 -> 0 new fired.
	t105 := now.Add(65 * time.Minute)
	res105, err := f.evaluator.EvaluateReminders(f.ctx, t105)
	if err != nil {
		t.Fatal(err)
	}
	if res105.Fired != 0 {
		t.Fatalf("expected 0 fired at 10:05, got %+v", res105)
	}
}

func TestTodoReminderCycleCompletionCutoff(t *testing.T) {
	f := newReminderCycleFixture(t)
	now := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	dueAt := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)

	// Wire completion hook
	f.planner.AddCompletionHook(func(ctx context.Context, tx *sql.Tx, plannerItemID, revisionID string, hookNow time.Time) error {
		if err := f.reminder.SatisfyReminderTx(ctx, tx, plannerItemID, revisionID, hookNow); err != nil {
			return err
		}
		return f.attention.ResolveByPlannerItemTx(ctx, tx, plannerItemID, revisionID, hookNow)
	})

	// Create local Todo due at 10:00
	todo, err := f.planner.CreateLocalTodo(f.ctx, planner.CreateLocalTodoInput{
		Title: "Urgent client email",
		DueAt: &dueAt,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Set reminder at due time
	_, err = f.reminder.SetReminder(f.ctx, reminder.SetReminderInput{
		PlannerItemID:  todo.ID,
		OffsetsSeconds: []int{0},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Trigger reminder at 10:00
	res, err := f.evaluator.EvaluateReminders(f.ctx, dueAt)
	if err != nil {
		t.Fatal(err)
	}
	if res.Fired != 1 {
		t.Fatalf("expected 1 fired, got %+v", res)
	}

	// Verify open issue exists
	issues, err := f.attention.ListAttentionIssues(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].State != attention.IssueOpen {
		t.Fatalf("expected 1 OPEN issue, got %v", issues)
	}

	// User completes Todo!
	completed, err := f.planner.CompleteLocalTodo(f.ctx, todo.ID)
	if err != nil {
		t.Fatalf("CompleteLocalTodo failed: %v", err)
	}
	if completed.Status != planner.StatusCompleted {
		t.Fatalf("expected status COMPLETED, got %v", completed.Status)
	}

	// Verify reminder is now SATISFIED
	rem, err := f.reminder.GetByPlannerItemID(f.ctx, todo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rem.State != reminder.StateSatisfied {
		t.Fatalf("expected reminder state SATISFIED, got %v", rem.State)
	}

	// Verify attention issue is now RESOLVED
	issue, err := f.attention.GetAttentionIssue(f.ctx, issues[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if issue.State != attention.IssueResolved {
		t.Fatalf("expected attention issue to be RESOLVED, got %v", issue.State)
	}

	// Verify notification remains SENT
	notifViews, err := f.attention.ListNotificationViews(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(notifViews) != 1 || notifViews[0].Notification.State != attention.DeliverySent {
		t.Fatalf("expected notification to remain SENT, got %v", notifViews)
	}

	// Evaluation at 10:30 sees 0 active reminders!
	resLater, err := f.evaluator.EvaluateReminders(f.ctx, now.Add(90*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if resLater.Evaluated != 0 || resLater.Fired != 0 {
		t.Fatalf("expected 0 evaluated and 0 fired after completion, got %+v", resLater)
	}
}

func TestTodoReminderCycleReconciliationSelfHealsCompletedTodo(t *testing.T) {
	f := newReminderCycleFixture(t)
	dueAt := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)

	// Create local Todo without completion hook (simulating out-of-band completion)
	todo, err := f.planner.CreateLocalTodo(f.ctx, planner.CreateLocalTodoInput{
		Title: "Review pull request",
		DueAt: &dueAt,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = f.reminder.SetReminder(f.ctx, reminder.SetReminderInput{
		PlannerItemID:  todo.ID,
		OffsetsSeconds: []int{0},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Complete the Todo without triggering reminder hook
	_, err = f.planner.CompleteLocalTodo(f.ctx, todo.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Reminder was still ACTIVE
	remBefore, _ := f.reminder.GetByPlannerItemID(f.ctx, todo.ID)
	if remBefore.State != reminder.StateActive {
		t.Fatalf("expected ACTIVE before cycle runs, got %v", remBefore.State)
	}

	// Cycle runs: must detect completion and satisfy reminder!
	res, err := f.evaluator.EvaluateReminders(f.ctx, dueAt)
	if err != nil {
		t.Fatal(err)
	}
	if res.Satisfied != 1 || res.Fired != 0 {
		t.Fatalf("expected 1 satisfied, 0 fired, got %+v", res)
	}

	remAfter, _ := f.reminder.GetByPlannerItemID(f.ctx, todo.ID)
	if remAfter.State != reminder.StateSatisfied {
		t.Fatalf("expected SATISFIED after cycle runs, got %v", remAfter.State)
	}
}

func TestRegisterTodoReminderCycle(t *testing.T) {
	scheduler := background.NewScheduler()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = scheduler.Shutdown(ctx)
	}()

	f := newReminderCycleFixture(t)

	// Nil scheduler returns error
	if _, err := RegisterTodoReminderCycle(nil, f.evaluator, time.Minute, time.Second); err == nil {
		t.Fatal("expected error with nil scheduler")
	}

	// Nil cycle returns error
	if _, err := RegisterTodoReminderCycle(scheduler, nil, time.Minute, time.Second); err == nil {
		t.Fatal("expected error with nil cycle")
	}

	// Valid registration
	def, err := RegisterTodoReminderCycle(scheduler, f.evaluator, 30*time.Second, 15*time.Second)
	if err != nil {
		t.Fatalf("RegisterTodoReminderCycle failed: %v", err)
	}
	if def.ID != TodoReminderCycleJobID {
		t.Fatalf("expected ID %s, got %s", TodoReminderCycleJobID, def.ID)
	}
	if def.Interval != 30*time.Second || def.Timeout != 15*time.Second {
		t.Fatalf("unexpected interval/timeout: %v, %v", def.Interval, def.Timeout)
	}

	// Duplicate registration fails
	if _, err := RegisterTodoReminderCycle(scheduler, f.evaluator, 30*time.Second, 15*time.Second); err == nil {
		t.Fatal("expected duplicate registration to fail")
	}
}

func TestTodoReminderCycleMissedWindowAntiSpamPolicy(t *testing.T) {
	f := newReminderCycleFixture(t)

	dueAt := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	todo, err := f.planner.CreateLocalTodo(f.ctx, planner.CreateLocalTodoInput{
		Title: "Multi-threshold report",
		DueAt: &dueAt,
	})
	if err != nil {
		t.Fatalf("CreateLocalTodo failed: %v", err)
	}

	// Policy: 3 days (259200s), 2 days (172800s), 1 day (86400s) before due.
	offsets := []int{259200, 172800, 86400}
	_, err = f.reminder.SetReminder(f.ctx, reminder.SetReminderInput{
		PlannerItemID:  todo.ID,
		OffsetsSeconds: offsets,
	})
	if err != nil {
		t.Fatalf("SetReminder failed: %v", err)
	}

	// App was offline/asleep. Wakes up at T-12h: all 3 thresholds (3d, 2d, 1d) passed!
	wakeTime := dueAt.Add(-12 * time.Hour)
	res, err := f.evaluator.EvaluateReminders(f.ctx, wakeTime)
	if err != nil {
		t.Fatalf("EvaluateReminders failed: %v", err)
	}

	// Anti-spam policy: only 1 notification fired (for most recent passed threshold: 1 day).
	if res.Fired != 1 {
		t.Fatalf("expected exactly 1 notification fired (anti-spam suppressed 2 older ones), got %d", res.Fired)
	}

	notifs, err := f.attention.ListNotificationViews(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(notifs) != 1 {
		t.Fatalf("expected exactly 1 notification view, got %d", len(notifs))
	}
	if !strings.Contains(notifs[0].Notification.Message, "due in 1 day") {
		t.Fatalf("expected notification message for most recent threshold ('due in 1 day'), got %q", notifs[0].Notification.Message)
	}

	// But all 3 passed offsets must be marked fired in repository so older missed thresholds never fire.
	rem, err := f.reminder.GetByPlannerItemID(f.ctx, todo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rem.FiredOffsets) != 3 {
		t.Fatalf("expected all 3 passed offsets recorded in FiredOffsets, got %v", rem.FiredOffsets)
	}

	// Subsequent cycle at T-6h: 0 new fired!
	res2, err := f.evaluator.EvaluateReminders(f.ctx, dueAt.Add(-6*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if res2.Fired != 0 {
		t.Fatalf("expected 0 fired on subsequent cycle, got %d", res2.Fired)
	}
}
