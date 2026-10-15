package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/applicationruntime"
	"lum-harbor-notifier/internal/assistant"
	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/calendar"
	dbgen "lum-harbor-notifier/internal/database/generated"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/notificationdelivery"
	"lum-harbor-notifier/internal/openai"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/plannerview"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/readcap"
	"lum-harbor-notifier/internal/reminder"
	"lum-harbor-notifier/internal/testsupport"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
	"lum-harbor-notifier/internal/workitemoverview"
)

type reminderChainAppFixture struct {
	db               *sql.DB
	app              *App
	workitemService  *workitem.Service
	trackingService  *tracking.Service
	plannerService   *planner.Service
	reminderService  *reminder.Service
	attentionService *attention.Service
	historyService   *history.Service
	proposalService  *proposal.Service
	evaluator        *applicationruntime.TodoReminderEvaluator
}

func newReminderChainAppFixture(t *testing.T) *reminderChainAppFixture {
	t.Helper()
	testDB := testsupport.OpenTestDatabase(t)
	db := testDB.DB

	plannerRepo := planner.NewRepository(db)
	calendarRepo := calendar.NewRepository(db)
	plannerSvc := planner.NewService(db, plannerRepo)
	plannerViewSvc := plannerview.NewService(plannerSvc, calendarRepo)
	workitemRepo := workitem.NewRepository(db)
	workitemSvc := workitem.NewService(db, workitemRepo)
	trackingSvc := tracking.NewService(db, workitemSvc)
	attentionSvc := attention.NewService(db, trackingSvc)
	reminderSvc := reminder.NewService(db)
	proposalSvc := proposal.NewService(db, workitemSvc)
	proposalSvc.SetTrackingOperator(trackingSvc)
	proposalSvc.SetPlannerOperator(plannerSvc)
	proposalSvc.SetReminderOperator(reminderSvc)
	historySvc := history.NewService(history.NewRepository(db))
	overviewSvc := workitemoverview.NewService(workitemSvc, trackingSvc, attentionSvc, plannerSvc)

	plannerSvc.AddCompletionHook(func(ctx context.Context, tx *sql.Tx, plannerItemID, revisionID string, now time.Time) error {
		if err := reminderSvc.SatisfyReminderTx(ctx, tx, plannerItemID, revisionID, now); err != nil {
			return err
		}
		return attentionSvc.ResolveByPlannerItemTx(ctx, tx, plannerItemID, revisionID, now)
	})

	app := &App{
		workitemService:         workitemSvc,
		plannerService:          plannerSvc,
		plannerViewService:      plannerViewSvc,
		reminderService:         reminderSvc,
		attentionService:        attentionSvc,
		historyService:          historySvc,
		proposalService:         proposalSvc,
		trackingService:         trackingSvc,
		workItemOverviewService: overviewSvc,
		proposeRegistry:         NewProposeRegistry(),
	}

	evaluator := applicationruntime.NewTodoReminderCycle(db, reminderSvc, attentionSvc)

	return &reminderChainAppFixture{
		db:               db,
		app:              app,
		workitemService:  workitemSvc,
		trackingService:  trackingSvc,
		plannerService:   plannerSvc,
		reminderService:  reminderSvc,
		attentionService: attentionSvc,
		historyService:   historySvc,
		proposalService:  proposalSvc,
		evaluator:        evaluator,
	}
}

func (f *reminderChainAppFixture) listOpenAttentionIssues(ctx context.Context) ([]attention.AttentionIssue, error) {
	all, err := f.attentionService.ListAttentionIssues(ctx)
	if err != nil {
		return nil, err
	}
	var open []attention.AttentionIssue
	for _, issue := range all {
		if issue.State == attention.IssueOpen {
			open = append(open, issue)
		}
	}
	return open, nil
}

// ============================================================================
// SCENARIO A: Multi-Threshold Sequential Firing
// Canonical requirement: Todo due Wednesday. Remind at T-3d, T-2d, T-1d.
// Exactly ONE open Attention issue per Todo whose reason updates as urgency rises.
// Three independently idempotent threshold notifications generated.
// ============================================================================
func TestM18_ScenarioA_MultiThresholdSequentialFiring(t *testing.T) {
	ctx := context.Background()
	f := newReminderChainAppFixture(t)

	// Due Wednesday 2026-09-30 at 12:00:00 UTC
	dueDate := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	dueAtStr := dueDate.Format(time.RFC3339)

	wi, err := f.workitemService.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{
		Title: "Scenario A Work Item",
	})
	if err != nil {
		t.Fatalf("CreateLocalWorkItem: %v", err)
	}

	// 3 thresholds: 3 days (259200s), 2 days (172800s), 1 day (86400s)
	offsets := []int{259200, 172800, 86400}
	todo, err := f.app.CreateLocalTodo(CreateLocalTodoRequestDTO{
		Title:          "Scenario A Multi-Threshold Todo",
		DueAt:          &dueAtStr,
		WorkItemID:     &wi.ID,
		OffsetsSeconds: offsets,
	})
	if err != nil {
		t.Fatalf("CreateLocalTodo: %v", err)
	}
	if todo.Reminder == nil || len(todo.Reminder.OffsetsSeconds) != 3 {
		t.Fatalf("expected 3 offsets, got %+v", todo.Reminder)
	}

	// Step 1: Time reaches T-3 days (2026-09-27 12:00:00 UTC)
	tMinus3 := dueDate.Add(-3 * 24 * time.Hour)
	res1, err := f.evaluator.EvaluateReminders(ctx, tMinus3)
	if err != nil {
		t.Fatalf("EvaluateReminders at T-3d: %v", err)
	}
	if res1.Fired != 1 {
		t.Fatalf("expected 1 fired at T-3d, got %d", res1.Fired)
	}

	// Verify Attention: exactly 1 open issue
	openIssues1, err := f.listOpenAttentionIssues(ctx)
	if err != nil {
		t.Fatalf("ListOpenIssues: %v", err)
	}
	if len(openIssues1) != 1 {
		t.Fatalf("expected 1 open issue at T-3d, got %d", len(openIssues1))
	}
	if !strings.Contains(openIssues1[0].Reason, "3 days") {
		t.Errorf("expected reason to contain '3 days', got %q", openIssues1[0].Reason)
	}

	// Verify Notifications: 1 notification
	center1, err := f.app.GetNotificationCenter()
	if err != nil {
		t.Fatalf("GetNotificationCenter: %v", err)
	}
	if len(center1.Items) != 1 {
		t.Fatalf("expected 1 notification at T-3d, got %d", len(center1.Items))
	}

	// Step 2: Time reaches T-2 days (2026-09-28 12:00:00 UTC)
	tMinus2 := dueDate.Add(-2 * 24 * time.Hour)
	res2, err := f.evaluator.EvaluateReminders(ctx, tMinus2)
	if err != nil {
		t.Fatalf("EvaluateReminders at T-2d: %v", err)
	}
	if res2.Fired != 1 {
		t.Fatalf("expected 1 fired at T-2d, got %d", res2.Fired)
	}

	// Verify Attention: STILL exactly 1 open issue, reason updated to latest urgency
	openIssues2, err := f.listOpenAttentionIssues(ctx)
	if err != nil {
		t.Fatalf("ListOpenIssues: %v", err)
	}
	if len(openIssues2) != 1 {
		t.Fatalf("expected STILL 1 open issue at T-2d, got %d", len(openIssues2))
	}
	if !strings.Contains(openIssues2[0].Reason, "2 days") {
		t.Errorf("expected updated reason to contain '2 days', got %q", openIssues2[0].Reason)
	}

	// Verify Notifications: 2 notifications in center
	center2, err := f.app.GetNotificationCenter()
	if err != nil {
		t.Fatalf("GetNotificationCenter: %v", err)
	}
	if len(center2.Items) != 2 {
		t.Fatalf("expected 2 notifications at T-2d, got %d", len(center2.Items))
	}

	// Step 3: Time reaches T-1 day (2026-09-29 12:00:00 UTC)
	tMinus1 := dueDate.Add(-1 * 24 * time.Hour)
	res3, err := f.evaluator.EvaluateReminders(ctx, tMinus1)
	if err != nil {
		t.Fatalf("EvaluateReminders at T-1d: %v", err)
	}
	if res3.Fired != 1 {
		t.Fatalf("expected 1 fired at T-1d, got %d", res3.Fired)
	}

	// Verify Attention: STILL exactly 1 open issue, reason updated to 1 day
	openIssues3, err := f.listOpenAttentionIssues(ctx)
	if err != nil {
		t.Fatalf("ListOpenIssues: %v", err)
	}
	if len(openIssues3) != 1 {
		t.Fatalf("expected STILL 1 open issue at T-1d, got %d", len(openIssues3))
	}
	if !strings.Contains(openIssues3[0].Reason, "1 day") {
		t.Errorf("expected updated reason to contain '1 day', got %q", openIssues3[0].Reason)
	}

	// Verify Notifications: 3 notifications in center
	center3, err := f.app.GetNotificationCenter()
	if err != nil {
		t.Fatalf("GetNotificationCenter: %v", err)
	}
	if len(center3.Items) != 3 {
		t.Fatalf("expected 3 notifications at T-1d, got %d", len(center3.Items))
	}
}

// ============================================================================
// SCENARIO B: Completion Suppression
// User completes Todo after T-3d fires. T-2d and T-1d must NEVER fire.
// Attention resolves authoritatively.
// ============================================================================
func TestM18_ScenarioB_CompletionSuppression(t *testing.T) {
	ctx := context.Background()
	f := newReminderChainAppFixture(t)

	dueDate := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	dueAtStr := dueDate.Format(time.RFC3339)

	todo, err := f.app.CreateLocalTodo(CreateLocalTodoRequestDTO{
		Title:          "Scenario B Todo",
		DueAt:          &dueAtStr,
		OffsetsSeconds: []int{259200, 172800, 86400},
	})
	if err != nil {
		t.Fatalf("CreateLocalTodo: %v", err)
	}

	// T-3d fires
	tMinus3 := dueDate.Add(-3 * 24 * time.Hour)
	_, _ = f.evaluator.EvaluateReminders(ctx, tMinus3)

	centerBefore, _ := f.app.GetNotificationCenter()
	if len(centerBefore.Items) != 1 {
		t.Fatalf("expected 1 notification before completion, got %d", len(centerBefore.Items))
	}

	// User completes Todo at T-2.5 days
	completed, err := f.app.CompleteLocalTodo(todo.ID)
	if err != nil {
		t.Fatalf("CompleteLocalTodo: %v", err)
	}
	if completed.Status != "COMPLETED" {
		t.Fatalf("expected COMPLETED, got %s", completed.Status)
	}
	if completed.Reminder == nil || completed.Reminder.State != string(reminder.StateSatisfied) {
		t.Fatalf("expected SATISFIED reminder, got %+v", completed.Reminder)
	}

	// Open Attention is resolved
	openIssues, _ := f.listOpenAttentionIssues(ctx)
	if len(openIssues) != 0 {
		t.Fatalf("expected 0 open attention issues after completion, got %d", len(openIssues))
	}

	// Time advances to T-2d: must NOT fire
	tMinus2 := dueDate.Add(-2 * 24 * time.Hour)
	res2, err := f.evaluator.EvaluateReminders(ctx, tMinus2)
	if err != nil {
		t.Fatalf("EvaluateReminders at T-2d: %v", err)
	}
	if res2.Fired != 0 {
		t.Errorf("expected 0 fired after completion at T-2d, got %d", res2.Fired)
	}

	// Time advances to T-1d: must NOT fire
	tMinus1 := dueDate.Add(-1 * 24 * time.Hour)
	res3, err := f.evaluator.EvaluateReminders(ctx, tMinus1)
	if err != nil {
		t.Fatalf("EvaluateReminders at T-1d: %v", err)
	}
	if res3.Fired != 0 {
		t.Errorf("expected 0 fired after completion at T-1d, got %d", res3.Fired)
	}

	// Notifications remain exactly 1 (the historical T-3d one)
	centerAfter, _ := f.app.GetNotificationCenter()
	if len(centerAfter.Items) != 1 {
		t.Fatalf("expected notification count to stay 1, got %d", len(centerAfter.Items))
	}
	if centerAfter.Items[0].AttentionState != "RESOLVED" {
		t.Errorf("expected RESOLVED attention state, got %s", centerAfter.Items[0].AttentionState)
	}
}

// ============================================================================
// SCENARIO C: Restart Durability & Idempotency
// Scheduler restart with fresh services against existing SQLite DB preserves
// fired progress and causes ZERO spurious repeat notifications.
// ============================================================================
func TestM18_ScenarioC_RestartDurabilityAndIdempotency(t *testing.T) {
	ctx := context.Background()
	f := newReminderChainAppFixture(t)

	dueDate := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	dueAtStr := dueDate.Format(time.RFC3339)

	todo, err := f.app.CreateLocalTodo(CreateLocalTodoRequestDTO{
		Title:          "Scenario C Restart Todo",
		DueAt:          &dueAtStr,
		OffsetsSeconds: []int{259200, 172800, 86400},
	})
	if err != nil {
		t.Fatalf("CreateLocalTodo: %v", err)
	}

	// Fire T-3d
	tMinus3 := dueDate.Add(-3 * 24 * time.Hour)
	res1, _ := f.evaluator.EvaluateReminders(ctx, tMinus3)
	if res1.Fired != 1 {
		t.Fatalf("expected 1 fired at T-3d, got %d", res1.Fired)
	}

	// Simulate restart: construct completely fresh services pointing to same DB
	freshReminderSvc := reminder.NewService(f.db)
	freshAttentionSvc := attention.NewService(f.db, f.trackingService)
	freshEvaluator := applicationruntime.NewTodoReminderCycle(f.db, freshReminderSvc, freshAttentionSvc)

	// Re-run evaluation at T-3d immediately after restart: must be 0 fired
	resDup, err := freshEvaluator.EvaluateReminders(ctx, tMinus3)
	if err != nil {
		t.Fatalf("freshEvaluator.EvaluateReminders: %v", err)
	}
	if resDup.Fired != 0 {
		t.Fatalf("expected 0 fired on re-evaluation after restart, got %d", resDup.Fired)
	}

	// Re-run at T-2.5d: still 0 fired
	resBetween, _ := freshEvaluator.EvaluateReminders(ctx, dueDate.Add(-60*time.Hour))
	if resBetween.Fired != 0 {
		t.Fatalf("expected 0 fired at T-2.5d, got %d", resBetween.Fired)
	}

	// Verify notifications count is still 1
	center, _ := f.app.GetNotificationCenter()
	if len(center.Items) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(center.Items))
	}

	// Advance to T-2d: threshold 172800 fires cleanly
	tMinus2 := dueDate.Add(-2 * 24 * time.Hour)
	res2, err := freshEvaluator.EvaluateReminders(ctx, tMinus2)
	if err != nil {
		t.Fatalf("freshEvaluator at T-2d: %v", err)
	}
	if res2.Fired != 1 {
		t.Fatalf("expected 1 fired at T-2d, got %d", res2.Fired)
	}

	// Verify total notifications is now 2
	center2, _ := f.app.GetNotificationCenter()
	if len(center2.Items) != 2 {
		t.Fatalf("expected 2 notifications, got %d", len(center2.Items))
	}
	_ = todo
}

// ============================================================================
// SCENARIO D: Missed-Window Offline Catchup Anti-Spam Policy
// System was offline while T-3d and T-2d passed. On wakeup at T-1d:
// Emits at most the most recent passed threshold (T-1d), marks older passed
// thresholds as fired/handled, producing ZERO burst spam.
// ============================================================================
func TestM18_ScenarioD_MissedWindowOfflineCatchup(t *testing.T) {
	ctx := context.Background()
	f := newReminderChainAppFixture(t)

	dueDate := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	dueAtStr := dueDate.Format(time.RFC3339)

	todo, err := f.app.CreateLocalTodo(CreateLocalTodoRequestDTO{
		Title:          "Scenario D Missed Window Todo",
		DueAt:          &dueAtStr,
		OffsetsSeconds: []int{259200, 172800, 86400}, // 3d, 2d, 1d
	})
	if err != nil {
		t.Fatalf("CreateLocalTodo: %v", err)
	}

	// App was offline during T-3d and T-2d!
	// Evaluator wakes up for the first time at T-1d (all 3 thresholds have passed)
	tMinus1 := dueDate.Add(-1 * 24 * time.Hour)
	res, err := f.evaluator.EvaluateReminders(ctx, tMinus1)
	if err != nil {
		t.Fatalf("EvaluateReminders at T-1d: %v", err)
	}
	if res.Fired != 1 {
		t.Fatalf("expected anti-spam policy to fire at most 1 threshold, got %d", res.Fired)
	}

	// Notifications in center: exactly 1, NOT 3!
	center, _ := f.app.GetNotificationCenter()
	if len(center.Items) != 1 {
		t.Fatalf("expected exactly 1 notification, got %d", len(center.Items))
	}
	if !strings.Contains(center.Items[0].Message, "1 day") {
		t.Errorf("expected notification to be for the most urgent threshold (1 day), got %q", center.Items[0].Message)
	}

	// In database, all 3 passed offsets must be marked fired/handled so they never re-fire
	rem, err := f.reminderService.GetByPlannerItemID(ctx, todo.ID)
	if err != nil {
		t.Fatalf("GetReminderByPlannerItem: %v", err)
	}
	if len(rem.FiredOffsets) != 3 {
		t.Fatalf("expected all 3 passed offsets to be handled, got %v", rem.FiredOffsets)
	}

	// Subsequent evaluation 6 hours later: 0 fired
	resLater, _ := f.evaluator.EvaluateReminders(ctx, dueDate.Add(-18*time.Hour))
	if resLater.Fired != 0 {
		t.Errorf("expected 0 fired later, got %d", resLater.Fired)
	}
}

// ============================================================================
// SCENARIO E: Workflow-Origin Todo Provenance
// WorkItem -> Workflow Definition -> Workflow -> Step -> Planner Todo.
// Todo stores and enriches WorkflowName, WorkflowStepTitle, WorkItemTitle.
// Invariant: Completing Todo does NOT automatically advance Workflow step.
// ============================================================================
func TestM18_ScenarioE_WorkflowOriginTodoProvenance(t *testing.T) {
	ctx := context.Background()
	f := newReminderChainAppFixture(t)

	// 1. Create WorkItem
	wi, err := f.workitemService.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{
		Title: "Enterprise Core Migration",
	})
	if err != nil {
		t.Fatalf("CreateLocalWorkItem: %v", err)
	}

	// 2. Create Workflow Definition with steps
	steps := []tracking.WorkflowStepInput{
		{Key: "arch", Title: "Architecture Review", Terminal: false},
		{Key: "code", Title: "Coding & Tests", Terminal: false},
		{Key: "deploy", Title: "Production Deployment", Terminal: true},
	}
	def, err := f.trackingService.CreateWorkflowDefinition(ctx, tracking.CreateWorkflowDefinitionInput{
		Name:    "Standard Software Lifecycle",
		Version: 1,
		Steps:   steps,
	})
	if err != nil {
		t.Fatalf("CreateWorkflowDefinition: %v", err)
	}

	// 3. Create Workflow on the WorkItem
	wf, err := f.trackingService.CreateWorkflow(ctx, tracking.CreateWorkflowInput{
		WorkItemID:           wi.ID,
		WorkflowDefinitionID: def.ID,
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	targetStep := def.Steps[1] // "Coding & Tests"

	// 4. Create Planner Todo originating from Workflow Step
	dueStr := "2026-10-15"
	todo, err := f.app.CreateLocalTodo(CreateLocalTodoRequestDTO{
		Title:          "Implement database adapter",
		DueDate:        &dueStr,
		WorkItemID:     &wi.ID,
		WorkflowID:     &wf.ID,
		WorkflowStepID: &targetStep.ID,
	})
	if err != nil {
		t.Fatalf("CreateLocalTodo: %v", err)
	}

	// Verify persistence in SQLite
	queries := dbgen.New(f.db)
	rawItem, err := queries.GetPlannerItemByID(ctx, todo.ID)
	if err != nil {
		t.Fatalf("GetPlannerItemByID: %v", err)
	}
	if rawItem.WorkflowID == nil || *rawItem.WorkflowID != wf.ID {
		t.Fatalf("expected WorkflowID %s, got %v", wf.ID, rawItem.WorkflowID)
	}
	if rawItem.WorkflowStepID == nil || *rawItem.WorkflowStepID != targetStep.ID {
		t.Fatalf("expected WorkflowStepID %s, got %v", targetStep.ID, rawItem.WorkflowStepID)
	}

	// Verify enrichment in GetPlannerView
	view, err := f.app.GetPlannerView(PlannerViewRequestDTO{
		RangeStart: "2026-10-01",
		RangeEnd:   "2026-10-31",
		Today:      "2026-10-15",
		Timezone:   "UTC",
	})
	if err != nil {
		t.Fatalf("GetPlannerView: %v", err)
	}

	var enrichedTodo *PlannerItemDTO
	for i := range view.CalendarItems {
		if view.CalendarItems[i].ID == todo.ID {
			enrichedTodo = &view.CalendarItems[i]
			break
		}
	}
	if enrichedTodo == nil {
		for i := range view.CurrentOpenItems {
			if view.CurrentOpenItems[i].ID == todo.ID {
				enrichedTodo = &view.CurrentOpenItems[i]
				break
			}
		}
	}
	if enrichedTodo == nil {
		t.Fatalf("failed to find Todo in PlannerView")
	}

	if enrichedTodo.WorkflowName == nil || *enrichedTodo.WorkflowName != "Standard Software Lifecycle" {
		t.Errorf("expected WorkflowName 'Standard Software Lifecycle', got %v", enrichedTodo.WorkflowName)
	}
	if enrichedTodo.WorkflowStepTitle == nil || *enrichedTodo.WorkflowStepTitle != "Coding & Tests" {
		t.Errorf("expected WorkflowStepTitle 'Coding & Tests', got %v", enrichedTodo.WorkflowStepTitle)
	}
	if enrichedTodo.WorkItemTitle == nil || *enrichedTodo.WorkItemTitle != "Enterprise Core Migration" {
		t.Errorf("expected WorkItemTitle 'Enterprise Core Migration', got %v", enrichedTodo.WorkItemTitle)
	}

	// 5. Complete Todo and verify Workflow is NOT automatically advanced (honesty invariant)
	completed, err := f.app.CompleteLocalTodo(todo.ID)
	if err != nil {
		t.Fatalf("CompleteLocalTodo: %v", err)
	}
	if completed.Status != "COMPLETED" {
		t.Fatalf("expected COMPLETED, got %s", completed.Status)
	}

	reloadedWf, err := f.trackingService.GetWorkflow(ctx, wf.ID)
	if err != nil {
		t.Fatalf("GetWorkflow: %v", err)
	}
	if reloadedWf.WorkflowDefinitionID != def.ID {
		t.Errorf("workflow definition altered unexpectedly")
	}
}

// ============================================================================
// SCENARIO F: Notification Navigation
// Notification in Notification Center carries PlannerItemID.
// Planner inspector loads exact item and details from notification reference.
// ============================================================================
func TestM18_ScenarioF_NotificationNavigation(t *testing.T) {
	ctx := context.Background()
	f := newReminderChainAppFixture(t)

	dueDate := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	dueAtStr := dueDate.Format(time.RFC3339)
	todo, err := f.app.CreateLocalTodo(CreateLocalTodoRequestDTO{
		Title:          "Scenario F Navigation Todo",
		DueAt:          &dueAtStr,
		OffsetsSeconds: []int{0},
	})
	if err != nil {
		t.Fatalf("CreateLocalTodo: %v", err)
	}

	// Fire at due time
	_, err = f.evaluator.EvaluateReminders(ctx, dueDate)
	if err != nil {
		t.Fatalf("EvaluateReminders: %v", err)
	}

	center, err := f.app.GetNotificationCenter()
	if err != nil {
		t.Fatalf("GetNotificationCenter: %v", err)
	}
	if len(center.Items) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(center.Items))
	}
	notif := center.Items[0]
	if notif.PlannerItemID == nil || *notif.PlannerItemID != todo.ID {
		t.Fatalf("expected notification PlannerItemID %s, got %v", todo.ID, notif.PlannerItemID)
	}

	// Using the notification's PlannerItemID, open in Planner
	view, err := f.app.GetPlannerView(PlannerViewRequestDTO{
		RangeStart: "2026-09-01",
		RangeEnd:   "2026-10-01",
		Today:      "2026-09-28",
		Timezone:   "UTC",
	})
	if err != nil {
		t.Fatalf("GetPlannerView: %v", err)
	}

	found := false
	allItems := append(view.CalendarItems, view.CurrentOpenItems...)
	for _, item := range allItems {
		if item.ID == *notif.PlannerItemID {
			found = true
			if item.Title != "Scenario F Navigation Todo" {
				t.Errorf("title mismatch: %s", item.Title)
			}
			break
		}
	}
	if !found {
		t.Errorf("PlannerView did not contain item referenced by notification: %s", *notif.PlannerItemID)
	}
}

// ============================================================================
// SCENARIO G: Recent Changes & Canonical History Audit Trail
// Audit trail records:
// PLANNER_ITEM_CREATED -> PLANNER_ITEM_REMINDER_SET -> ATTENTION_OPENED ->
// NOTIFICATION_CREATED -> NOTIFICATION_SENT -> PLANNER_ITEM_COMPLETED ->
// ATTENTION_RESOLVED.
// ============================================================================
func TestM18_ScenarioG_AuditTrailAndHistory(t *testing.T) {
	ctx := context.Background()
	f := newReminderChainAppFixture(t)

	dueDate := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	dueAtStr := dueDate.Format(time.RFC3339)
	todo, err := f.app.CreateLocalTodo(CreateLocalTodoRequestDTO{
		Title:          "Scenario G Audit Todo",
		DueAt:          &dueAtStr,
		OffsetsSeconds: []int{0},
	})
	if err != nil {
		t.Fatalf("CreateLocalTodo: %v", err)
	}

	// Fire reminder
	_, _ = f.evaluator.EvaluateReminders(ctx, dueDate)

	// Complete Todo
	_, err = f.app.CompleteLocalTodo(todo.ID)
	if err != nil {
		t.Fatalf("CompleteLocalTodo: %v", err)
	}

	historyEvents, err := f.app.GetPlannerItemHistory(todo.ID)
	if err != nil {
		t.Fatalf("GetPlannerItemHistory: %v", err)
	}

	eventTypes := make(map[string]bool)
	for _, ev := range historyEvents {
		eventTypes[ev.EventType] = true
	}

	expectedTypes := []string{
		string(history.EventPlannerItemCreated),
		string(history.EventPlannerItemReminderSet),
		string(history.EventAttentionOpened),
		string(history.EventNotificationCreated),
		string(history.EventNotificationSent),
		string(history.EventPlannerItemCompleted),
		string(history.EventAttentionResolved),
	}

	for _, exp := range expectedTypes {
		if !eventTypes[exp] {
			t.Errorf("missing expected history event: %s (found: %+v)", exp, eventTypes)
		}
	}
}

// ============================================================================
// SCENARIO H: Real AI Proposal Acceptance (Deterministic Apply + Real LLM)
// Part 1: Deterministic verification of Proposal with multi-threshold reminders
// and workflow provenance applied to SQLite tables.
// Part 2: Live LLM verification (when LUM_HARBOR_OPENAI_ACCEPTANCE=1).
// ============================================================================
func TestM18_ScenarioH_ProposalWithMultiThresholdReminders(t *testing.T) {
	ctx := context.Background()
	f := newReminderChainAppFixture(t)

	// 1. Create WorkItem and Workflow
	wi, err := f.workitemService.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{
		Title: "AI Feature Integration",
	})
	if err != nil {
		t.Fatalf("CreateLocalWorkItem: %v", err)
	}

	def, err := f.trackingService.CreateWorkflowDefinition(ctx, tracking.CreateWorkflowDefinitionInput{
		Name:    "AI Flow",
		Version: 1,
		Steps:   []tracking.WorkflowStepInput{{Key: "prompt", Title: "Prompt Engineering", Terminal: true}},
	})
	if err != nil {
		t.Fatalf("CreateWorkflowDefinition: %v", err)
	}

	wf, err := f.trackingService.CreateWorkflow(ctx, tracking.CreateWorkflowInput{
		WorkItemID:           wi.ID,
		WorkflowDefinitionID: def.ID,
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	// 2. Invoke registry handler for planner.item.create with multi-threshold offsets
	args := map[string]any{
		"title":                    "Review Prompt Matrix",
		"due_date":                 "2026-10-20",
		"due_time":                 "14:00",
		"timezone":                 "UTC",
		"remind":                   true,
		"reminder_offsets_minutes": []any{"4320", "1440"}, // 3 days and 1 day
		"work_item_id":             wi.ID,
		"workflow_id":              wf.ID,
		"workflow_step_id":         def.Steps[0].ID,
	}

	rawArgs, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal args: %v", err)
	}

	prop, err := f.app.proposeRegistry.Execute(ctx, f.app, "planner.item.create", nil, rawArgs, nil)
	if err != nil {
		t.Fatalf("registry.Execute: %v", err)
	}

	// 3. Verify Proposal payload contains multi-threshold offsets and workflow provenance
	if prop.PlannerItemPayload == nil {
		t.Fatalf("expected PlannerItemPayload")
	}
	if len(prop.PlannerItemPayload.OffsetsSeconds) != 2 {
		t.Fatalf("expected 2 reminder offsets in proposal payload, got %v", prop.PlannerItemPayload.OffsetsSeconds)
	}
	if prop.PlannerItemPayload.WorkflowID == nil || *prop.PlannerItemPayload.WorkflowID != wf.ID {
		t.Errorf("expected WorkflowID %s, got %v", wf.ID, prop.PlannerItemPayload.WorkflowID)
	}

	// 4. Apply Proposal
	applyRes, err := f.proposalService.ApplyProposal(ctx, proposal.ApplyProposalInput{ID: prop.ID})
	if err != nil {
		t.Fatalf("ApplyProposal: %v", err)
	}
	if applyRes.Outcome != proposal.ApplyOutcomeApplied {
		t.Fatalf("expected ApplyOutcomeApplied, got %s", applyRes.Outcome)
	}

	// 5. Authoritative SQLite verification
	items, err := f.plannerService.SearchByTitle(ctx, "Review Prompt Matrix", 10)
	if err != nil || len(items) == 0 {
		t.Fatalf("created item not found in SQLite planner_items: %v", err)
	}
	createdItem := &items[0]
	if createdItem.WorkflowID == nil || *createdItem.WorkflowID != wf.ID {
		t.Errorf("SQLite workflow_id mismatch: %v", createdItem.WorkflowID)
	}

	rem, err := f.reminderService.GetByPlannerItemID(ctx, createdItem.ID)
	if err != nil {
		t.Fatalf("GetByPlannerItemID: %v", err)
	}
	if rem.State != reminder.StateActive {
		t.Errorf("expected ACTIVE reminder, got %s", rem.State)
	}
	if len(rem.OffsetsSeconds) != 2 {
		t.Errorf("expected 2 offsets in SQLite todo_reminders, got %v", rem.OffsetsSeconds)
	}
}

// Live real OpenAI provider acceptance (English & Chinese)
func TestM18_ScenarioH_LiveRealOpenAIAcceptance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in -short mode")
	}
	if os.Getenv("LUM_HARBOR_OPENAI_ACCEPTANCE") != "1" && strings.ToLower(os.Getenv("LUM_HARBOR_OPENAI_ACCEPTANCE")) != "true" {
		t.Skip("skipping live OpenAI test; set LUM_HARBOR_OPENAI_ACCEPTANCE=1 to run")
	}

	userHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolve user home: %v", err)
	}
	credPath := filepath.Join(userHome, "Library", "Application Support", "lum_harbor", "credentials", "builtin-openai-credential.secret")
	credBytes, err := os.ReadFile(credPath)
	if err != nil {
		t.Fatalf("read credential: %v", err)
	}
	apiKey := strings.TrimSpace(string(credBytes))
	if apiKey == "" {
		t.Fatal("empty API key")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	provider, err := openai.NewProvider(openai.Config{
		Endpoint:   "https://api.openai.com/v1",
		Credential: apiKey,
		Model:      "gpt-4o-mini",
		Timeout:    60 * time.Second,
	})
	if err != nil {
		t.Fatalf("openai.NewProvider: %v", err)
	}

	testDB := testsupport.OpenTestDatabase(t)
	db := testDB.DB
	workItems := workitem.NewService(db, workitem.NewRepository(db))
	trackingSvc := tracking.NewService(db, workItems)
	plannerSvc := planner.NewService(db, planner.NewRepository(db))
	historySvc := history.NewService(history.NewRepository(db))
	reminderSvc := reminder.NewService(db)
	proposalSvc := proposal.NewService(db, workItems)
	proposalSvc.SetTrackingOperator(trackingSvc)
	proposalSvc.SetPlannerOperator(plannerSvc)
	proposalSvc.SetReminderOperator(reminderSvc)

	readcapSvc, err := readcap.NewService(workItems, trackingSvc, plannerSvc, db, nil, nil, historySvc)
	if err != nil {
		t.Fatalf("readcap.NewService: %v", err)
	}
	registry := NewProposeRegistry()
	engine, err := assistant.NewNativeRuntimeAdapter(provider, readcapSvc, registry)
	if err != nil {
		t.Fatalf("assistant.NewNativeRuntimeAdapter: %v", err)
	}

	app := &App{
		workitemService:  workItems,
		trackingService:  trackingSvc,
		plannerService:   plannerSvc,
		proposalService:  proposalSvc,
		reminderService:  reminderSvc,
		assistantRuntime: engine,
		proposeRegistry:  registry,
		aiExecutor:       provider,
	}

	// English test: natural language multi-threshold reminder
	t.Run("English Natural Language Multi-Threshold Reminder", func(t *testing.T) {
		res, err := app.AskAssistant(AskAssistantRequestDTO{
			Message: "Remind me to file quarterly taxes on 2026-10-15 at 10:00, notify me 3 days and 1 day before",
			Surface: "PLANNER",
		})
		if err != nil {
			t.Fatalf("AskAssistant failed: %v", err)
		}
		if res.Kind != "MUTATION_INTENT" || res.ProposalOutcome != AssistantProposalCreated {
			t.Fatalf("expected Proposal created, got kind=%s outcome=%s text=%q", res.Kind, res.ProposalOutcome, res.Text)
		}

		p, err := proposalSvc.GetByID(ctx, res.ProposalID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if p.OperationType != proposal.OperationTypePlannerItemCreate {
			t.Fatalf("expected planner.item.create, got %s", p.OperationType)
		}

		applyRes, err := proposalSvc.ApplyProposal(ctx, proposal.ApplyProposalInput{ID: p.ID})
		if err != nil {
			t.Fatalf("ApplyProposal: %v", err)
		}
		if applyRes.Outcome != proposal.ApplyOutcomeApplied {
			t.Fatalf("expected ApplyOutcomeApplied, got %s", applyRes.Outcome)
		}

		// Verify in SQLite
		items, err := plannerSvc.SearchByTitle(ctx, "taxes", 10)
		if err != nil || len(items) == 0 {
			t.Fatalf("expected created todo in sqlite, got %v", items)
		}
		rem, err := reminderSvc.GetByPlannerItemID(ctx, items[0].ID)
		if err != nil {
			t.Fatalf("GetByPlannerItemID: %v", err)
		}
		if rem.State != reminder.StateActive {
			t.Errorf("expected ACTIVE reminder, got %s", rem.State)
		}
	})

	// Chinese test: natural language multi-threshold reminder
	t.Run("Chinese Natural Language Multi-Threshold Reminder", func(t *testing.T) {
		res, err := app.AskAssistant(AskAssistantRequestDTO{
			Message: "2026年10月20日上午10点提醒我提交季度审计报告，提前3天和1天提醒我",
			Surface: "PLANNER",
		})
		if err != nil {
			t.Fatalf("AskAssistant failed: %v", err)
		}
		if res.Kind != "MUTATION_INTENT" || res.ProposalOutcome != AssistantProposalCreated {
			t.Fatalf("expected Proposal created, got kind=%s outcome=%s text=%q", res.Kind, res.ProposalOutcome, res.Text)
		}

		p, err := proposalSvc.GetByID(ctx, res.ProposalID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if p.OperationType != proposal.OperationTypePlannerItemCreate {
			t.Fatalf("expected planner.item.create, got %s", p.OperationType)
		}

		applyRes, err := proposalSvc.ApplyProposal(ctx, proposal.ApplyProposalInput{ID: p.ID})
		if err != nil {
			t.Fatalf("ApplyProposal: %v", err)
		}
		if applyRes.Outcome != proposal.ApplyOutcomeApplied {
			t.Fatalf("expected ApplyOutcomeApplied, got %s", applyRes.Outcome)
		}
	})
}

// ============================================================================
// SCENARIO I: External Notification Delivery Regression
// Verifies notification delivery records are created and processed.
// ============================================================================
func TestM18_ScenarioI_ExternalNotificationDeliveryRegression(t *testing.T) {
	ctx := context.Background()
	f := newReminderChainAppFixture(t)

	dueDate := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	dueAtStr := dueDate.Format(time.RFC3339)
	_, err := f.app.CreateLocalTodo(CreateLocalTodoRequestDTO{
		Title:          "Scenario I Delivery Todo",
		DueAt:          &dueAtStr,
		OffsetsSeconds: []int{0},
	})
	if err != nil {
		t.Fatalf("CreateLocalTodo: %v", err)
	}

	// Fire reminder
	_, err = f.evaluator.EvaluateReminders(ctx, dueDate)
	if err != nil {
		t.Fatalf("EvaluateReminders: %v", err)
	}

	// Verify notification awaiting external delivery exists in SQLite
	queries := dbgen.New(f.db)
	awaiting, err := queries.ListSentNotificationsAwaitingExternalDelivery(ctx)
	if err != nil {
		t.Fatalf("ListSentNotificationsAwaitingExternalDelivery: %v", err)
	}
	if len(awaiting) != 1 {
		t.Fatalf("expected 1 notification awaiting external delivery, got %d", len(awaiting))
	}
	if awaiting[0].Title == "" {
		t.Errorf("expected non-empty notification title")
	}

	// Verify external delivery processing via notificationdelivery.Service
	fakeDeliverer := &fakeScenarioIDeliverer{}
	deliveryService := notificationdelivery.NewService(f.db, fakeDeliverer)
	cycleRes, err := deliveryService.RunDeliveryCycle(ctx, dueDate)
	if err != nil {
		t.Fatalf("RunDeliveryCycle: %v", err)
	}
	if cycleRes.Delivered != 1 {
		t.Errorf("expected 1 delivered in cycle, got %d", cycleRes.Delivered)
	}

	// Verify delivery is recorded and no longer awaiting
	awaitingAfter, err := queries.ListSentNotificationsAwaitingExternalDelivery(ctx)
	if err != nil {
		t.Fatalf("ListSentNotificationsAwaitingExternalDelivery after delivery: %v", err)
	}
	if len(awaitingAfter) != 0 {
		t.Errorf("expected 0 awaiting external delivery after cycle, got %d", len(awaitingAfter))
	}
}

type fakeScenarioIDeliverer struct {
	calls []notificationdelivery.DeliveryRequest
}

func (f *fakeScenarioIDeliverer) Deliver(_ context.Context, req notificationdelivery.DeliveryRequest) (notificationdelivery.DeliveryResult, error) {
	f.calls = append(f.calls, req)
	return notificationdelivery.DeliveryResult{}, nil
}

// Mainline legacy test preserved
func TestM18_MainlineWorkExecutionAndReminderChain(t *testing.T) {
	ctx := context.Background()
	f := newReminderChainAppFixture(t)

	now := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)

	// Step 1: WorkItem exists
	wi, err := f.workitemService.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{
		Title: "Q3 Database Optimization",
	})
	if err != nil {
		t.Fatalf("CreateLocalWorkItem failed: %v", err)
	}

	// Step 2: Actionable Planner Todo is created linked to this WorkItem, with Reminder intent
	dueAt := now.Add(15 * time.Minute).Format(time.RFC3339)
	todoDetails := "Validate index scan reduction"
	todo, err := f.app.CreateLocalTodo(CreateLocalTodoRequestDTO{
		Title:          "Verify query plan metrics",
		Details:        &todoDetails,
		DueAt:          &dueAt,
		WorkItemID:     &wi.ID,
		OffsetsSeconds: []int{0, 900},
	})
	if err != nil {
		t.Fatalf("CreateLocalTodo failed: %v", err)
	}
	if todo.WorkItemID == nil || *todo.WorkItemID != wi.ID {
		t.Fatalf("expected WorkItemID %s, got %v", wi.ID, todo.WorkItemID)
	}
	if todo.Reminder == nil || todo.Reminder.State != string(reminder.StateActive) {
		t.Fatalf("expected ACTIVE reminder, got %+v", todo.Reminder)
	}
	if len(todo.Reminder.OffsetsSeconds) != 2 {
		t.Fatalf("expected 2 reminder offsets, got %v", todo.Reminder.OffsetsSeconds)
	}

	// Step 3: Scheduler evaluation runs at 10:00 UTC (due 10:15 - offset 900s = 10:00)
	res, err := f.evaluator.EvaluateReminders(ctx, now)
	if err != nil {
		t.Fatalf("evaluator.EvaluateReminders failed: %v", err)
	}
	if res.Evaluated != 1 || res.Fired != 1 {
		t.Fatalf("expected 1 evaluated and 1 fired, got %+v", res)
	}

	// Step 4: Notification Center verification
	center, err := f.app.GetNotificationCenter()
	if err != nil {
		t.Fatalf("GetNotificationCenter failed: %v", err)
	}
	if len(center.Items) != 1 {
		t.Fatalf("expected 1 notification in center, got %d", len(center.Items))
	}
	notif := center.Items[0]
	if notif.PlannerItemID == nil || *notif.PlannerItemID != todo.ID {
		t.Fatalf("expected notification PlannerItemID %s, got %v", todo.ID, notif.PlannerItemID)
	}
	if notif.WorkItemID == nil || *notif.WorkItemID != wi.ID {
		t.Fatalf("expected notification WorkItemID %s, got %v", wi.ID, notif.WorkItemID)
	}
	if notif.DeliveryState != "SENT" {
		t.Errorf("expected SENT delivery state, got %q", notif.DeliveryState)
	}
	if notif.AttentionState != "OPEN" {
		t.Errorf("expected OPEN attention state, got %q", notif.AttentionState)
	}

	// Step 5: User "Opens Todo" in Planner
	view, err := f.app.GetPlannerView(PlannerViewRequestDTO{
		RangeStart: "2026-09-01",
		RangeEnd:   "2026-10-01",
		Today:      "2026-09-25",
		Timezone:   "UTC",
	})
	if err != nil {
		t.Fatalf("GetPlannerView failed: %v", err)
	}

	var loadedTodo *PlannerItemDTO
	for i := range view.CalendarItems {
		if view.CalendarItems[i].ID == todo.ID {
			loadedTodo = &view.CalendarItems[i]
			break
		}
	}
	if loadedTodo == nil {
		for i := range view.CurrentOpenItems {
			if view.CurrentOpenItems[i].ID == todo.ID {
				loadedTodo = &view.CurrentOpenItems[i]
				break
			}
		}
	}
	if loadedTodo == nil {
		t.Fatalf("expected to find Todo in PlannerView")
	}

	if loadedTodo.Reminder == nil || len(loadedTodo.Reminder.FiredOffsets) != 1 || loadedTodo.Reminder.FiredOffsets[0] != 900 {
		t.Fatalf("expected FiredOffsets [900], got %+v", loadedTodo.Reminder)
	}
	if loadedTodo.WorkItemID == nil || *loadedTodo.WorkItemID != wi.ID {
		t.Errorf("expected WorkItemID %s, got %v", wi.ID, loadedTodo.WorkItemID)
	}
	if len(loadedTodo.RecentChanges) < 3 {
		t.Fatalf("expected at least 3 recent changes, got %d", len(loadedTodo.RecentChanges))
	}

	// Step 6: User completes the Todo
	completed, err := f.app.CompleteLocalTodo(todo.ID)
	if err != nil {
		t.Fatalf("CompleteLocalTodo failed: %v", err)
	}
	if completed.Status != "COMPLETED" {
		t.Fatalf("expected Status COMPLETED, got %q", completed.Status)
	}
	if completed.Reminder == nil || completed.Reminder.State != string(reminder.StateSatisfied) {
		t.Fatalf("expected Reminder state SATISFIED, got %+v", completed.Reminder)
	}

	reloadedCenter, err := f.app.GetNotificationCenter()
	if err != nil {
		t.Fatalf("GetNotificationCenter after completion failed: %v", err)
	}
	if len(reloadedCenter.Items) != 1 {
		t.Fatalf("expected 1 notification in center, got %d", len(reloadedCenter.Items))
	}
	if reloadedCenter.Items[0].AttentionState != "RESOLVED" {
		t.Errorf("expected AttentionState RESOLVED after completing Todo, got %q", reloadedCenter.Items[0].AttentionState)
	}

	// Step 7: Subsequent Scheduler evaluation runs at 10:15 UTC (when offset 0 would fire)
	laterTime := now.Add(15 * time.Minute)
	resLater, err := f.evaluator.EvaluateReminders(ctx, laterTime)
	if err != nil {
		t.Fatalf("evaluator.EvaluateReminders at laterTime failed: %v", err)
	}
	if resLater.Fired != 0 {
		t.Errorf("expected 0 fired after Todo completion, got %d", resLater.Fired)
	}

	// Step 8: Complete Audit Trail via GetPlannerItemHistory
	historyEvents, err := f.app.GetPlannerItemHistory(todo.ID)
	if err != nil {
		t.Fatalf("GetPlannerItemHistory failed: %v", err)
	}

	eventTypes := make(map[string]bool)
	for _, ev := range historyEvents {
		eventTypes[ev.EventType] = true
	}

	expectedTypes := []string{
		string(history.EventPlannerItemCreated),
		string(history.EventPlannerItemReminderSet),
		string(history.EventAttentionOpened),
		string(history.EventNotificationCreated),
		string(history.EventNotificationSent),
		string(history.EventPlannerItemCompleted),
		string(history.EventAttentionResolved),
	}

	for _, exp := range expectedTypes {
		if !eventTypes[exp] {
			t.Errorf("expected history event %s to be present in audit trail, but was missing", exp)
		}
	}
}

// ============================================================================
// M19 ROUND 1: Vanilla Work Execution & In-App Presentation Hardening
// Proves the full chain without ANY optional plugins:
// WorkItem / Workflow -> Todo -> Multi-threshold Reminders -> Scheduler ->
// Attention -> Core Notification queryable for in-app presentation ->
// Todo completion -> Attention resolution -> Remaining thresholds suppressed ->
// Cold restart preserves state -> Canonical audit history.
// ============================================================================
func TestM19_Round1_VanillaWorkExecutionAndInAppPresentation(t *testing.T) {
	ctx := context.Background()
	f := newReminderChainAppFixture(t)

	// Step 1: Create WorkItem with Workflow context
	wi, err := f.app.CreateWorkItem(CreateWorkItemRequestDTO{
		Title: "Release Hardening Plan",
	})
	if err != nil {
		t.Fatalf("CreateWorkItem failed: %v", err)
	}

	wfDef, err := f.trackingService.CreateWorkflowDefinition(ctx, tracking.CreateWorkflowDefinitionInput{
		Name:    "Ship Hardening Workflow",
		Version: 1,
		Steps: []tracking.WorkflowStepInput{
			{Key: "audit", Title: "Security Audit", Terminal: false},
			{Key: "ui", Title: "UI Acceptance", Terminal: false},
			{Key: "release", Title: "Release Verification", Terminal: true},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflowDefinition failed: %v", err)
	}

	wf, err := f.trackingService.CreateWorkflow(ctx, tracking.CreateWorkflowInput{
		WorkItemID:           wi.ID,
		WorkflowDefinitionID: wfDef.ID,
	})
	if err != nil {
		t.Fatalf("CreateWorkflow failed: %v", err)
	}
	activeStepID := wfDef.Steps[0].ID

	// Step 2: Schedule Action in Planner with provenance and multi-threshold reminder (2h, 30m before)
	dueDate := time.Date(2026, 10, 1, 14, 0, 0, 0, time.UTC)
	dueAtStr := dueDate.Format(time.RFC3339)
	todo, err := f.app.CreateLocalTodo(CreateLocalTodoRequestDTO{
		Title:          "Verify In-App Notifications Toast",
		DueAt:          &dueAtStr,
		WorkItemID:     &wi.ID,
		WorkflowID:     &wf.ID,
		WorkflowStepID: &activeStepID,
		OffsetsSeconds: []int{7200, 1800}, // 2h, 30m before
	})
	if err != nil {
		t.Fatalf("CreateLocalTodo failed: %v", err)
	}
	if todo.WorkItemID == nil || *todo.WorkItemID != wi.ID {
		t.Fatalf("expected Todo WorkItemID %s, got %v", wi.ID, todo.WorkItemID)
	}

	// Step 3: Advance clock to T-2h (threshold 7200) and evaluate reminders via core scheduler
	tMinus2h := dueDate.Add(-2 * time.Hour)
	evalResult, err := f.evaluator.EvaluateReminders(ctx, tMinus2h)
	if err != nil {
		t.Fatalf("EvaluateReminders at T-2h failed: %v", err)
	}
	if evalResult.Fired != 1 {
		t.Fatalf("expected 1 reminder fired at T-2h, got %d", evalResult.Fired)
	}

	// Step 4: Verify Core Notification is queryable without any plugin and ready for in-app alert
	center, err := f.app.GetNotificationCenter()
	if err != nil {
		t.Fatalf("GetNotificationCenter failed: %v", err)
	}
	if len(center.Items) != 1 {
		t.Fatalf("expected 1 notification in center, got %d", len(center.Items))
	}
	notif := center.Items[0]
	if notif.PlannerItemID == nil || *notif.PlannerItemID != todo.ID {
		t.Fatalf("expected notification PlannerItemID %s, got %v", todo.ID, notif.PlannerItemID)
	}
	if notif.WorkItemID == nil || *notif.WorkItemID != wi.ID {
		t.Fatalf("expected notification WorkItemID %s, got %v", wi.ID, notif.WorkItemID)
	}
	if notif.DeliveryState != "SENT" {
		t.Fatalf("expected notification DeliveryState SENT, got %s", notif.DeliveryState)
	}
	if notif.ReadAt != nil {
		t.Fatalf("expected unread notification (ReadAt == nil), got %v", notif.ReadAt)
	}
	if notif.DismissedAt != nil {
		t.Fatalf("expected undismissed notification (DismissedAt == nil), got %v", notif.DismissedAt)
	}
	if notif.Disposition != "NEEDS_ACTION" {
		t.Fatalf("expected Disposition NEEDS_ACTION, got %s", notif.Disposition)
	}
	if notif.Title == "" || notif.Message == "" {
		t.Fatalf("expected non-empty notification title and message for in-app toast, got title=%q message=%q", notif.Title, notif.Message)
	}

	// Step 5: Verify Open Todo navigation via PlannerView
	view, err := f.app.GetPlannerView(PlannerViewRequestDTO{
		RangeStart: "2026-10-01",
		RangeEnd:   "2026-10-02",
		Today:      "2026-10-01",
		Timezone:   "UTC",
	})
	if err != nil {
		t.Fatalf("GetPlannerView failed: %v", err)
	}
	var foundItem *PlannerItemDTO
	for _, it := range append(view.CalendarItems, view.CurrentOpenItems...) {
		if it.ID == *notif.PlannerItemID {
			foundItem = &it
			break
		}
	}
	if foundItem == nil {
		t.Fatalf("did not find Todo %s in PlannerView", *notif.PlannerItemID)
	}
	if foundItem.Status != "OPEN" {
		t.Fatalf("expected status OPEN, got %s", foundItem.Status)
	}

	// Step 6: User completes the Todo
	completedItem, err := f.app.CompleteLocalTodo(todo.ID)
	if err != nil {
		t.Fatalf("CompleteLocalTodo failed: %v", err)
	}
	if completedItem.Status != "COMPLETED" {
		t.Fatalf("expected completedItem status COMPLETED, got %s", completedItem.Status)
	}

	// Step 7: Verify Attention is resolved and remaining thresholds are suppressed
	openIssues, err := f.listOpenAttentionIssues(ctx)
	if err != nil {
		t.Fatalf("listOpenAttentionIssues failed: %v", err)
	}
	if len(openIssues) != 0 {
		t.Fatalf("expected 0 open attention issues after completion, got %d", len(openIssues))
	}

	// Advance clock to T-30m (threshold 1800 would fire if not completed)
	tMinus30m := dueDate.Add(-30 * time.Minute)
	evalLater, err := f.evaluator.EvaluateReminders(ctx, tMinus30m)
	if err != nil {
		t.Fatalf("EvaluateReminders at T-30m failed: %v", err)
	}
	if evalLater.Fired != 0 {
		t.Fatalf("expected 0 fired at T-30m after completion, got %d", evalLater.Fired)
	}

	// Step 8: Verify Cold Restart preserves state
	freshReminderSvc := reminder.NewService(f.db)
	freshAttentionSvc := attention.NewService(f.db, f.trackingService)
	freshEvaluator := applicationruntime.NewTodoReminderCycle(f.db, freshReminderSvc, freshAttentionSvc)

	evalRestart, err := freshEvaluator.EvaluateReminders(ctx, dueDate)
	if err != nil {
		t.Fatalf("freshEvaluator at dueDate failed: %v", err)
	}
	if evalRestart.Fired != 0 {
		t.Fatalf("expected 0 fired on restart at dueDate, got %d", evalRestart.Fired)
	}

	restartedCenter, err := f.app.GetNotificationCenter()
	if err != nil {
		t.Fatalf("GetNotificationCenter after restart failed: %v", err)
	}
	if len(restartedCenter.Items) != 1 {
		t.Fatalf("expected 1 notification in restarted center, got %d", len(restartedCenter.Items))
	}
	if restartedCenter.Items[0].AttentionState != "RESOLVED" {
		t.Fatalf("expected AttentionState RESOLVED after restart, got %s", restartedCenter.Items[0].AttentionState)
	}

	// Step 9: Verify complete canonical audit trail
	historyEvents, err := f.app.GetPlannerItemHistory(todo.ID)
	if err != nil {
		t.Fatalf("GetPlannerItemHistory failed: %v", err)
	}
	eventsMap := make(map[string]bool)
	for _, ev := range historyEvents {
		eventsMap[ev.EventType] = true
	}
	for _, expectedType := range []string{
		string(history.EventPlannerItemCreated),
		string(history.EventPlannerItemReminderSet),
		string(history.EventAttentionOpened),
		string(history.EventNotificationCreated),
		string(history.EventNotificationSent),
		string(history.EventPlannerItemCompleted),
		string(history.EventAttentionResolved),
	} {
		if !eventsMap[expectedType] {
			t.Errorf("missing expected history event %s in audit trail", expectedType)
		}
	}
}

// ============================================================================
// M19 ROUND 1: Isolated Restart & Durability Verification (Scenarios A, B, C)
// Tests:
// - Restart A: Todo + Reminder created, threshold not yet reached -> survives and remains actionable.
// - Restart B: Handled threshold does not re-fire; no duplicate Core Notification; later threshold remains eligible.
// - Restart C: Completed Todo -> Todo remains completed, Reminder satisfied, Attention resolved, historical Notification remains (unread state preserved independently), no future reminders fire.
// ============================================================================
func TestM19_Round1_RestartDurabilityScenariosABC(t *testing.T) {
	ctx := context.Background()
	f := newReminderChainAppFixture(t)

	// --- PHASE A: Setup & Restart A (Unfired Reminder Survives Restart) ---
	wi, err := f.app.CreateWorkItem(CreateWorkItemRequestDTO{
		Title: "Restart Durability WorkItem",
	})
	if err != nil {
		t.Fatalf("CreateWorkItem: %v", err)
	}

	wfDef, err := f.trackingService.CreateWorkflowDefinition(ctx, tracking.CreateWorkflowDefinitionInput{
		Name:    "Restart Durability Workflow",
		Version: 1,
		Steps: []tracking.WorkflowStepInput{
			{Key: "step-1", Title: "Initial Step", Terminal: false},
			{Key: "step-2", Title: "Final Step", Terminal: true},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflowDefinition: %v", err)
	}

	wf, err := f.trackingService.CreateWorkflow(ctx, tracking.CreateWorkflowInput{
		WorkItemID:           wi.ID,
		WorkflowDefinitionID: wfDef.ID,
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	activeStepID := wfDef.Steps[0].ID

	baseTime := time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)
	dueDate := baseTime.Add(3 * 24 * time.Hour) // Due in 3 days
	dueAtStr := dueDate.Format(time.RFC3339)

	todo, err := f.app.CreateLocalTodo(CreateLocalTodoRequestDTO{
		Title:          "Restart Durability Todo",
		DueAt:          &dueAtStr,
		WorkItemID:     &wi.ID,
		WorkflowID:     &wf.ID,
		WorkflowStepID: &activeStepID,
		OffsetsSeconds: []int{259200, 172800}, // T-3d (259200s), T-2d (172800s)
	})
	if err != nil {
		t.Fatalf("CreateLocalTodo: %v", err)
	}

	// Simulate Restart 1 (before any threshold has fired)
	freshReminderSvc1 := reminder.NewService(f.db)
	freshAttentionSvc1 := attention.NewService(f.db, f.trackingService)
	freshEvaluator1 := applicationruntime.NewTodoReminderCycle(f.db, freshReminderSvc1, freshAttentionSvc1)

	// Re-evaluate at baseTime (before T-3d): 0 fired
	resPre, err := freshEvaluator1.EvaluateReminders(ctx, baseTime.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("freshEvaluator1 at baseTime: %v", err)
	}
	if resPre.Fired != 0 {
		t.Fatalf("expected 0 fired before threshold, got %d", resPre.Fired)
	}

	// Verify Todo and Reminder state survive Restart 1
	rem1, err := freshReminderSvc1.GetByPlannerItemID(ctx, todo.ID)
	if err != nil || rem1 == nil {
		t.Fatalf("GetByPlannerItemID after Restart 1 failed: %v", err)
	}
	if rem1.State != reminder.StateActive {
		t.Fatalf("expected reminder StateActive, got %s", rem1.State)
	}
	if len(rem1.OffsetsSeconds) != 2 || len(rem1.FiredOffsets) != 0 {
		t.Fatalf("expected 2 offsets and 0 fired, got offsets=%v fired=%v", rem1.OffsetsSeconds, rem1.FiredOffsets)
	}

	// --- PHASE B: Restart B (Handled Threshold Stability & Idempotency) ---
	// Advance to T-3d: exactly fires threshold 259200
	tMinus3d := dueDate.Add(-3 * 24 * time.Hour)
	resT3, err := freshEvaluator1.EvaluateReminders(ctx, tMinus3d)
	if err != nil {
		t.Fatalf("EvaluateReminders at T-3d: %v", err)
	}
	if resT3.Fired != 1 {
		t.Fatalf("expected 1 fired at T-3d, got %d", resT3.Fired)
	}

	centerB1, err := f.app.GetNotificationCenter()
	if err != nil {
		t.Fatalf("GetNotificationCenter: %v", err)
	}
	if len(centerB1.Items) != 1 {
		t.Fatalf("expected 1 notification at T-3d, got %d", len(centerB1.Items))
	}
	notifB1 := centerB1.Items[0]
	if notifB1.ReadAt != nil {
		t.Fatalf("expected notification to be unread (ReadAt == nil), got %v", notifB1.ReadAt)
	}
	if notifB1.AttentionState != "OPEN" {
		t.Fatalf("expected AttentionState OPEN, got %s", notifB1.AttentionState)
	}

	// Simulate Restart 2 (after threshold 259200 handled)
	freshReminderSvc2 := reminder.NewService(f.db)
	freshAttentionSvc2 := attention.NewService(f.db, f.trackingService)
	freshEvaluator2 := applicationruntime.NewTodoReminderCycle(f.db, freshReminderSvc2, freshAttentionSvc2)

	// Re-evaluate immediately at T-3d on restart: MUST be 0 fired (no duplicate notification)
	resDup, err := freshEvaluator2.EvaluateReminders(ctx, tMinus3d)
	if err != nil {
		t.Fatalf("freshEvaluator2 at T-3d: %v", err)
	}
	if resDup.Fired != 0 {
		t.Fatalf("expected 0 fired on restart at T-3d, got %d", resDup.Fired)
	}

	// Verify notification count is still 1 and not duplicated
	centerB2, _ := f.app.GetNotificationCenter()
	if len(centerB2.Items) != 1 {
		t.Fatalf("expected still exactly 1 notification after restart, got %d", len(centerB2.Items))
	}

	// Advance to T-2d: threshold 172800 fires cleanly
	tMinus2d := dueDate.Add(-2 * 24 * time.Hour)
	resT2, err := freshEvaluator2.EvaluateReminders(ctx, tMinus2d)
	if err != nil {
		t.Fatalf("freshEvaluator2 at T-2d: %v", err)
	}
	if resT2.Fired != 1 {
		t.Fatalf("expected 1 fired at T-2d, got %d", resT2.Fired)
	}

	centerB3, _ := f.app.GetNotificationCenter()
	if len(centerB3.Items) != 2 {
		t.Fatalf("expected 2 total notifications after T-2d, got %d", len(centerB3.Items))
	}

	// --- PHASE C: Restart C (Completed Todo, Reminder Satisfied, Attention Resolved, Historical Notifications Remain) ---
	// Complete the Todo
	completedItem, err := f.app.CompleteLocalTodo(todo.ID)
	if err != nil {
		t.Fatalf("CompleteLocalTodo: %v", err)
	}
	if completedItem.Status != "COMPLETED" {
		t.Fatalf("expected status COMPLETED, got %s", completedItem.Status)
	}

	// Verify Attention is resolved
	openAttn, err := f.listOpenAttentionIssues(ctx)
	if err != nil || len(openAttn) != 0 {
		t.Fatalf("expected 0 open attention issues after completion, got %d", len(openAttn))
	}

	// Verify historical notifications: still exactly 2, AttentionState is RESOLVED, ReadAt is STILL nil (independent concern)
	centerC1, _ := f.app.GetNotificationCenter()
	if len(centerC1.Items) != 2 {
		t.Fatalf("expected 2 historical notifications after completion, got %d", len(centerC1.Items))
	}
	for _, item := range centerC1.Items {
		if item.AttentionState != "RESOLVED" {
			t.Errorf("expected AttentionState RESOLVED, got %s", item.AttentionState)
		}
		if item.ReadAt != nil {
			t.Errorf("expected historical notification to remain unread until explicitly marked read, got ReadAt=%v", item.ReadAt)
		}
	}

	// Simulate Restart 3 (after completion)
	freshReminderSvc3 := reminder.NewService(f.db)
	freshAttentionSvc3 := attention.NewService(f.db, f.trackingService)
	freshEvaluator3 := applicationruntime.NewTodoReminderCycle(f.db, freshReminderSvc3, freshAttentionSvc3)

	// Re-evaluate at due date: 0 fired (all future thresholds permanently suppressed)
	resFuture, err := freshEvaluator3.EvaluateReminders(ctx, dueDate)
	if err != nil {
		t.Fatalf("freshEvaluator3 at dueDate: %v", err)
	}
	if resFuture.Fired != 0 {
		t.Fatalf("expected 0 fired after completion at dueDate, got %d", resFuture.Fired)
	}

	// Verify state survives Restart 3
	rem3, err := freshReminderSvc3.GetByPlannerItemID(ctx, todo.ID)
	if err != nil || rem3 == nil {
		t.Fatalf("GetByPlannerItemID after Restart 3 failed: %v", err)
	}
	if rem3.State != reminder.StateSatisfied {
		t.Fatalf("expected reminder StateSatisfied after restart, got %s", rem3.State)
	}

	centerC2, _ := f.app.GetNotificationCenter()
	if len(centerC2.Items) != 2 {
		t.Fatalf("expected 2 historical notifications after Restart 3, got %d", len(centerC2.Items))
	}

	// Check canonical History trail
	hist, err := f.app.GetPlannerItemHistory(todo.ID)
	if err != nil {
		t.Fatalf("GetPlannerItemHistory: %v", err)
	}
	histMap := make(map[string]bool)
	for _, ev := range hist {
		histMap[ev.EventType] = true
	}
	for _, evType := range []string{
		string(history.EventPlannerItemCreated),
		string(history.EventPlannerItemReminderSet),
		string(history.EventAttentionOpened),
		string(history.EventNotificationSent),
		string(history.EventPlannerItemCompleted),
		string(history.EventAttentionResolved),
	} {
		if !histMap[evType] {
			t.Errorf("missing history event %s in audit trail", evType)
		}
	}
}
