package attention

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"lum-harbor-notifier/internal/database"
	"lum-harbor-notifier/internal/foundation"
	"lum-harbor-notifier/internal/testsupport"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

var due = time.Date(2026, 10, 15, 22, 0, 0, 0, time.UTC)
var afterDue = due.Add(2 * time.Hour)

type fixture struct {
	ctx       context.Context
	db        *sql.DB
	workItems *workitem.Service
	tracking  *tracking.Service
	attention *Service
	clock     time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := testsupport.OpenTestDatabase(t)
	workItems := workitem.NewService(db.DB, workitem.NewRepository(db.DB))
	trackingService := tracking.NewService(db.DB, workItems)
	f := &fixture{ctx: context.Background(), db: db.DB, workItems: workItems, tracking: trackingService, attention: NewService(db.DB, trackingService), clock: afterDue}
	f.attention.clock = func() time.Time { return f.clock }
	return f
}

func (f *fixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := f.db.QueryRowContext(f.ctx, query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (f *fixture) workItem(t *testing.T) *workitem.WorkItem {
	t.Helper()
	item, err := f.workItems.CreateLocalWorkItem(f.ctx, workitem.CreateLocalWorkItemInput{Title: "Release"})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func (f *fixture) workItemMonitor(t *testing.T, wi *workitem.WorkItem, when time.Time) *tracking.MonitorPoint {
	t.Helper()
	completed := workitem.LifecycleCompleted
	m, err := f.tracking.CreateMonitorPoint(f.ctx, tracking.CreateMonitorPointInput{
		WorkItemID: wi.ID, Kind: tracking.MonitorFinalTarget, Title: "Ship it", DueAt: when, Timezone: "America/Chicago", ExpectedWorkItemLifecycle: &completed,
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

type stepFixture struct {
	wi      *workitem.WorkItem
	item    *tracking.TrackedItem
	def     *tracking.WorkflowDefinition
	monitor *tracking.MonitorPoint
}

// stepMonitor builds a real WorkItem → workflow → TrackedItem → monitor that
// expects the "build" step (second of three) to be COMPLETED.
func (f *fixture) stepMonitor(t *testing.T, when time.Time) stepFixture {
	t.Helper()
	wi := f.workItem(t)
	def, err := f.tracking.CreateWorkflowDefinition(f.ctx, tracking.CreateWorkflowDefinitionInput{Name: "Flow", Version: 1, Steps: []tracking.WorkflowStepInput{{Key: "plan", Title: "Plan"}, {Key: "build", Title: "Build"}, {Key: "done", Title: "Done", Terminal: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.tracking.CreateWorkflow(f.ctx, tracking.CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	item, err := f.tracking.CreateTrackedItem(f.ctx, tracking.CreateTrackedItemInput{WorkItemID: wi.ID, Title: "API"})
	if err != nil {
		t.Fatal(err)
	}
	m, err := f.tracking.CreateMonitorPoint(f.ctx, tracking.CreateMonitorPointInput{WorkItemID: wi.ID, SubjectTrackedItemID: &item.ID, Kind: tracking.MonitorCheckpoint, Title: "Build done", DueAt: when, Timezone: "UTC", ExpectedStepID: &def.Steps[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	return stepFixture{wi: wi, item: item, def: def, monitor: m}
}

func (f *fixture) setStep(t *testing.T, s stepFixture, index int, state tracking.StepState) {
	t.Helper()
	if _, err := f.tracking.SetTrackedItemStepState(f.ctx, tracking.SetStepStateInput{TrackedItemID: s.item.ID, WorkflowStepID: s.def.Steps[index].ID, State: state}); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) reconcile(t *testing.T, now time.Time) ReconcileResult {
	t.Helper()
	r, err := f.attention.Reconcile(f.ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (f *fixture) dispatch(t *testing.T, now time.Time) DispatchResult {
	t.Helper()
	r, err := f.attention.DispatchPendingNotifications(f.ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (f *fixture) issues(t *testing.T) []AttentionIssue {
	t.Helper()
	list, err := f.attention.ListAttentionIssues(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func (f *fixture) notifications(t *testing.T) []NotificationView {
	t.Helper()
	list, err := f.attention.ListNotificationViews(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestNotYetDueCreatesNothing(t *testing.T) {
	f := newFixture(t)
	f.workItemMonitor(t, f.workItem(t), due)
	r := f.reconcile(t, due.Add(-time.Minute))
	if r.Opened != 0 || r.Evaluated != 0 || len(f.issues(t)) != 0 || len(f.notifications(t)) != 0 {
		t.Fatalf("a not-yet-due monitor must produce no Attention: %+v", r)
	}
}

func TestOverdueUnmetWorkItemOpensOneIssueAndOneInitialNotification(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t)
	m := f.workItemMonitor(t, wi, due)
	revisions := f.count(t, "SELECT COUNT(*) FROM revisions")

	r := f.reconcile(t, afterDue)
	if r.Opened != 1 || r.Evaluated != 1 || r.Resolved != 0 {
		t.Fatalf("unexpected result: %+v", r)
	}
	issues, views := f.issues(t), f.notifications(t)
	if len(issues) != 1 || len(views) != 1 {
		t.Fatalf("expected 1 issue and 1 notification, got %d/%d", len(issues), len(views))
	}
	issue, n := issues[0], views[0].Notification
	if issue.Type != IssueMonitorOverdueUnmet || issue.Severity != SeverityWarning || issue.State != IssueOpen ||
		issue.WorkItemID == nil || *issue.WorkItemID != wi.ID || issue.MonitorPointID == nil || *issue.MonitorPointID != m.ID || issue.SubjectTrackedItemID != nil ||
		issue.DedupeKey != "monitor-overdue-unmet:"+m.ID || issue.SnoozedUntil != nil || issue.ResolvedAt != nil || issue.RecommendedAction == nil {
		t.Fatalf("unexpected issue: %+v", issue)
	}
	if !strings.Contains(issue.Reason, "Ship it") || !strings.Contains(issue.Reason, "ACTIVE") || !strings.Contains(issue.Reason, "COMPLETED") || !strings.Contains(issue.Reason, "Oct 15, 2026 5:00 PM CDT") {
		t.Fatalf("reason must explain what was due, what is, what was expected, in the monitor zone: %q", issue.Reason)
	}
	if n.AttentionIssueID != issue.ID || n.State != DeliveryPending || n.Channel != ChannelInApp || n.ReadAt != nil || n.DismissedAt != nil || n.SentAt != nil ||
		n.Title != "Ship it" || n.WorkItemID == nil || *n.WorkItemID != wi.ID || n.DedupeKey != "attention-opened:"+issue.ID {
		t.Fatalf("unexpected notification: %+v", n)
	}
	if views[0].Issue.ID != issue.ID || views[0].Issue.DispositionAt(afterDue) != DispositionNeedsAction {
		t.Fatalf("view must link the issue: %+v", views[0])
	}
	// Exactly one shared SYSTEM Revision for the open, two Entities.
	if f.count(t, "SELECT COUNT(*) FROM revisions") != revisions+1 {
		t.Fatal("open must create exactly one Revision")
	}
	var origin string
	if err := f.db.QueryRow("SELECT origin_type FROM revisions WHERE id = ?", issue.LastRevisionID).Scan(&origin); err != nil || origin != "SYSTEM" {
		t.Fatalf("origin %q err=%v", origin, err)
	}
	if n.LastRevisionID != issue.LastRevisionID {
		t.Fatal("issue and initial notification share one logical mutation")
	}
	var openedSummary string
	if err := f.db.QueryRow("SELECT summary FROM history_events WHERE event_type = 'ATTENTION_OPENED' AND subject_entity_id = ?", issue.ID).Scan(&openedSummary); err != nil || openedSummary != "Attention opened because target is overdue" {
		t.Fatalf("monitor ATTENTION_OPENED summary %q err=%v", openedSummary, err)
	}
	var types string
	if err := f.db.QueryRow("SELECT group_concat(entity_type) FROM (SELECT entity_type FROM entities WHERE entity_type IN ('ATTENTION_ISSUE','NOTIFICATION') ORDER BY 1)").Scan(&types); err != nil || types != "ATTENTION_ISSUE,NOTIFICATION" {
		t.Fatalf("entities: %q err=%v", types, err)
	}
}

func TestOverdueUnmetDeadlineMonitorOpensCriticalIssueAndNotification(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t)
	completed := workitem.LifecycleCompleted
	_, err := f.tracking.CreateMonitorPoint(f.ctx, tracking.CreateMonitorPointInput{
		WorkItemID: wi.ID, Kind: tracking.MonitorDeadline, Title: "Hard Deadline", DueAt: due, Timezone: "America/Chicago", ExpectedWorkItemLifecycle: &completed,
	})
	if err != nil {
		t.Fatal(err)
	}

	r := f.reconcile(t, afterDue)
	if r.Opened != 1 || r.Evaluated != 1 || r.Resolved != 0 {
		t.Fatalf("unexpected result: %+v", r)
	}
	issues := f.issues(t)
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if issues[0].Severity != SeverityCritical {
		t.Fatalf("expected SeverityCritical for DEADLINE monitor, got %v", issues[0].Severity)
	}
}

func TestReasonFactsAreVersionedStructuredAndSnapshotSurvivesLaterChanges(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t)
	m := f.workItemMonitor(t, wi, due)
	f.reconcile(t, afterDue)
	issue := f.issues(t)[0]
	snapshotBefore := f.notifications(t)[0].Notification.ReasonSnapshotJSON

	var facts ReasonFacts
	if err := json.Unmarshal([]byte(issue.ReasonJSON), &facts); err != nil {
		t.Fatal(err)
	}
	if facts.Version != 1 || facts.IssueType != "MONITOR_OVERDUE_UNMET" || facts.AttentionIssueID != issue.ID || facts.MonitorPointID != m.ID ||
		facts.Subject.Kind != "WORK_ITEM" || facts.Subject.ID != wi.ID || facts.Expected.Kind != "WORK_ITEM_LIFECYCLE" ||
		*facts.Expected.WorkItemLifecycle != "COMPLETED" || *facts.Actual.WorkItemLifecycle != "ACTIVE" || facts.DueAt != "2026-10-15T22:00:00Z" {
		t.Fatalf("unexpected reason facts: %+v", facts)
	}
	if snapshotBefore != issue.ReasonJSON {
		t.Fatal("the initial notification snapshot must equal the reason facts at creation")
	}
	// Truth changes afterwards; the historical snapshot must not.
	if _, err := f.workItems.SetLocalWorkItemLifecycleState(f.ctx, workitem.SetLocalWorkItemLifecycleStateInput{ID: wi.ID, Target: workitem.LifecycleCompleted}); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t, afterDue)
	if f.notifications(t)[0].Notification.ReasonSnapshotJSON != snapshotBefore {
		t.Fatal("the reason snapshot must survive later truth changes and resolution")
	}
	for _, banned := range []string{"color", "badge", "icon", "css"} {
		if strings.Contains(strings.ToLower(snapshotBefore), banned) {
			t.Fatalf("snapshot must carry no presentation state: %s", banned)
		}
	}
}

func TestRepeatedReconcileIsIdempotent(t *testing.T) {
	f := newFixture(t)
	f.workItemMonitor(t, f.workItem(t), due)
	f.reconcile(t, afterDue)
	revisions, entities := f.count(t, "SELECT COUNT(*) FROM revisions"), f.count(t, "SELECT COUNT(*) FROM entities")
	for i := 0; i < 3; i++ {
		r := f.reconcile(t, afterDue.Add(time.Duration(i)*time.Hour))
		if r.Opened != 0 || r.Resolved != 0 {
			t.Fatalf("repeat must change nothing: %+v", r)
		}
	}
	if len(f.issues(t)) != 1 || len(f.notifications(t)) != 1 ||
		f.count(t, "SELECT COUNT(*) FROM revisions") != revisions || f.count(t, "SELECT COUNT(*) FROM entities") != entities {
		t.Fatal("idempotent reconcile created duplicate Attention/Notification/Revision/Entity")
	}
}

func TestWorkItemLifecycleClearsConditionAndResolvesButKeepsNotification(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t)
	f.workItemMonitor(t, wi, due)
	f.reconcile(t, afterDue)
	f.dispatch(t, afterDue)
	n := f.notifications(t)[0].Notification
	if _, err := f.attention.MarkNotificationRead(f.ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.workItems.SetLocalWorkItemLifecycleState(f.ctx, workitem.SetLocalWorkItemLifecycleStateInput{ID: wi.ID, Target: workitem.LifecycleCompleted}); err != nil {
		t.Fatal(err)
	}
	revisions := f.count(t, "SELECT COUNT(*) FROM revisions")

	r := f.reconcile(t, afterDue)
	if r.Resolved != 1 || r.Opened != 0 {
		t.Fatalf("unexpected: %+v", r)
	}
	issue := f.issues(t)[0]
	if issue.State != IssueResolved || issue.ResolvedAt == nil || issue.SnoozedUntil != nil || issue.DispositionAt(afterDue) != DispositionResolved {
		t.Fatalf("issue must be RESOLVED: %+v", issue)
	}
	views := f.notifications(t)
	if len(views) != 1 || views[0].Notification.ID != n.ID || views[0].Notification.ReadAt == nil {
		t.Fatalf("the Notification must be retained with its read state: %+v", views)
	}
	if f.count(t, "SELECT COUNT(*) FROM revisions") != revisions+1 {
		t.Fatal("resolve must create exactly one Revision")
	}
	var origin string
	if err := f.db.QueryRow("SELECT origin_type FROM revisions WHERE id = ?", issue.LastRevisionID).Scan(&origin); err != nil || origin != "SYSTEM" {
		t.Fatalf("origin %q err=%v", origin, err)
	}
	r = f.reconcile(t, afterDue)
	if r.Resolved != 0 || f.count(t, "SELECT COUNT(*) FROM revisions") != revisions+1 {
		t.Fatal("reconciling a resolved condition again must be a no-op")
	}
}

func TestRecurrenceAfterResolutionOpensANewIssue(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t)
	f.workItemMonitor(t, wi, due)
	f.reconcile(t, afterDue)
	for _, target := range []workitem.LifecycleState{workitem.LifecycleCompleted, workitem.LifecycleActive} {
		if _, err := f.workItems.SetLocalWorkItemLifecycleState(f.ctx, workitem.SetLocalWorkItemLifecycleStateInput{ID: wi.ID, Target: target}); err != nil {
			t.Fatal(err)
		}
		f.reconcile(t, afterDue)
	}
	issues := f.issues(t)
	open, resolved := 0, 0
	for _, i := range issues {
		if i.State == IssueOpen {
			open++
		} else {
			resolved++
		}
	}
	if open != 1 || resolved != 1 || len(f.notifications(t)) != 2 {
		t.Fatalf("a recurrence is a new logical issue with its own initial notification: open=%d resolved=%d", open, resolved)
	}
}

func TestTrackedItemStepExpectation(t *testing.T) {
	f := newFixture(t)
	s := f.stepMonitor(t, due)
	f.reconcile(t, afterDue)
	issue := f.issues(t)[0]
	if issue.State != IssueOpen || issue.SubjectTrackedItemID == nil || *issue.SubjectTrackedItemID != s.item.ID || issue.WorkItemID == nil || *issue.WorkItemID != s.wi.ID {
		t.Fatalf("unexpected issue: %+v", issue)
	}
	if !strings.Contains(issue.Reason, "Build") || !strings.Contains(issue.Reason, "API") || !strings.Contains(issue.Reason, "NOT_STARTED") {
		t.Fatalf("reason: %q", issue.Reason)
	}

	f.setStep(t, s, 0, tracking.StepCompleted) // a different step
	f.reconcile(t, afterDue)
	if f.issues(t)[0].State != IssueOpen {
		t.Fatal("completing a different step must not resolve the issue")
	}
	f.setStep(t, s, 1, tracking.StepSkipped)
	f.reconcile(t, afterDue)
	if f.issues(t)[0].State != IssueOpen {
		t.Fatal("SKIPPED must not satisfy a step-COMPLETED expectation")
	}
	f.setStep(t, s, 1, tracking.StepCompleted)
	r := f.reconcile(t, afterDue)
	if r.Resolved != 1 || f.issues(t)[0].State != IssueResolved {
		t.Fatal("completing the exact expected step must resolve the issue")
	}
	if len(f.notifications(t)) != 1 {
		t.Fatal("the notification must be retained")
	}
}

func TestMonitorTerminalStatesResolveOpenIssues(t *testing.T) {
	for _, target := range []tracking.MonitorState{tracking.MonitorSatisfied, tracking.MonitorCancelled} {
		f := newFixture(t)
		m := f.workItemMonitor(t, f.workItem(t), due)
		f.reconcile(t, afterDue)
		f.dispatch(t, afterDue)
		n := f.notifications(t)[0].Notification
		if _, err := f.attention.MarkNotificationRead(f.ctx, n.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := f.tracking.SetMonitorPointState(f.ctx, tracking.SetMonitorPointStateInput{ID: m.ID, Target: target}); err != nil {
			t.Fatal(err)
		}
		r := f.reconcile(t, afterDue)
		if r.Resolved != 1 || f.issues(t)[0].State != IssueResolved {
			t.Fatalf("%s must resolve the issue: %+v", target, r)
		}
		kept := f.notifications(t)
		if len(kept) != 1 || kept[0].Notification.ReadAt == nil {
			t.Fatal("Notification must be retained with its read state")
		}
	}
}

func TestSnoozeIsOrthogonalToLifecycleAndNotifications(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t)
	m := f.workItemMonitor(t, wi, due)
	f.reconcile(t, afterDue)
	issue := f.issues(t)[0]
	before := f.notifications(t)[0].Notification
	revisions := f.count(t, "SELECT COUNT(*) FROM revisions")
	until := afterDue.Add(3 * time.Hour)

	snoozed, err := f.attention.SnoozeAttentionIssue(f.ctx, SnoozeInput{ID: issue.ID, Until: until})
	if err != nil {
		t.Fatal(err)
	}
	if snoozed.State != IssueOpen || snoozed.SnoozedUntil == nil || !snoozed.SnoozedUntil.Equal(until) || snoozed.DispositionAt(afterDue) != DispositionSnoozed {
		t.Fatalf("unexpected: %+v", snoozed)
	}
	if f.count(t, "SELECT COUNT(*) FROM revisions") != revisions+1 {
		t.Fatal("snooze must create exactly one Revision")
	}
	var origin string
	if err := f.db.QueryRow("SELECT origin_type FROM revisions WHERE id = ?", snoozed.LastRevisionID).Scan(&origin); err != nil || origin != "USER" {
		t.Fatalf("origin %q err=%v", origin, err)
	}
	after := f.notifications(t)[0].Notification
	if after.ReadAt != nil || after.LastRevisionID != before.LastRevisionID || after.State != DeliveryPending {
		t.Fatal("snooze must not touch the Notification")
	}
	mp, _ := f.tracking.GetWorkItemTracking(f.ctx, wi.ID)
	got, _ := f.workItems.GetByID(f.ctx, wi.ID)
	if mp.MonitorPoints[0].State != tracking.MonitorActive || mp.MonitorPoints[0].LastRevisionID != m.LastRevisionID || got.LastRevisionID != wi.LastRevisionID {
		t.Fatal("snooze must not touch Monitor or WorkItem")
	}

	// Same value is a no-op; reconcile preserves the snooze and adds nothing.
	if _, err := f.attention.SnoozeAttentionIssue(f.ctx, SnoozeInput{ID: issue.ID, Until: until}); err != nil || f.count(t, "SELECT COUNT(*) FROM revisions") != revisions+1 {
		t.Fatalf("same snooze must be a no-op: %v", err)
	}
	f.reconcile(t, afterDue)
	kept := f.issues(t)[0]
	if kept.SnoozedUntil == nil || !kept.SnoozedUntil.Equal(until) || len(f.notifications(t)) != 1 {
		t.Fatal("reconcile must preserve the snooze and create no second notification")
	}

	// Expiry needs no state rewrite: the derived disposition simply changes.
	if d := kept.DispositionAt(until.Add(time.Second)); d != DispositionNeedsAction {
		t.Fatalf("after expiry the OPEN issue needs action again, got %s", d)
	}
	var state string
	if err := f.db.QueryRow("SELECT state FROM attention_issues WHERE entity_id = ?", issue.ID).Scan(&state); err != nil || state != "OPEN" {
		t.Fatalf("state must remain OPEN, got %q", state)
	}
}

func TestSnoozeValidation(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t)
	f.workItemMonitor(t, wi, due)
	f.reconcile(t, afterDue)
	issue := f.issues(t)[0]
	if _, err := f.attention.SnoozeAttentionIssue(f.ctx, SnoozeInput{ID: issue.ID, Until: afterDue.Add(-time.Minute)}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a past time must be rejected: %v", err)
	}
	if _, err := f.attention.SnoozeAttentionIssue(f.ctx, SnoozeInput{ID: issue.ID}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a zero time must be rejected: %v", err)
	}
	if _, err := f.attention.SnoozeAttentionIssue(f.ctx, SnoozeInput{ID: " ", Until: afterDue.Add(time.Hour)}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("blank id: %v", err)
	}
	if _, err := f.attention.SnoozeAttentionIssue(f.ctx, SnoozeInput{ID: "missing", Until: afterDue.Add(time.Hour)}); !errors.Is(err, ErrIssueNotFound) {
		t.Fatalf("unknown id: %v", err)
	}
	if _, err := f.workItems.SetLocalWorkItemLifecycleState(f.ctx, workitem.SetLocalWorkItemLifecycleStateInput{ID: wi.ID, Target: workitem.LifecycleCompleted}); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t, afterDue)
	if _, err := f.attention.SnoozeAttentionIssue(f.ctx, SnoozeInput{ID: issue.ID, Until: afterDue.Add(time.Hour)}); !errors.Is(err, ErrIssueNotOpen) {
		t.Fatalf("a resolved issue cannot be snoozed: %v", err)
	}
}

func TestResolvingClearsSnooze(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t)
	f.workItemMonitor(t, wi, due)
	f.reconcile(t, afterDue)
	if _, err := f.attention.SnoozeAttentionIssue(f.ctx, SnoozeInput{ID: f.issues(t)[0].ID, Until: afterDue.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.workItems.SetLocalWorkItemLifecycleState(f.ctx, workitem.SetLocalWorkItemLifecycleStateInput{ID: wi.ID, Target: workitem.LifecycleCompleted}); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t, afterDue)
	if issue := f.issues(t)[0]; issue.State != IssueResolved || issue.SnoozedUntil != nil {
		t.Fatalf("resolution must clear the snooze: %+v", issue)
	}
}

func TestMarkNotificationReadIsOrthogonalAndIdempotent(t *testing.T) {
	f := newFixture(t)
	f.workItemMonitor(t, f.workItem(t), due)
	f.reconcile(t, afterDue)
	pending := f.notifications(t)[0].Notification
	if _, err := f.attention.MarkNotificationRead(f.ctx, pending.ID); !errors.Is(err, ErrNotDelivered) {
		t.Fatalf("a PENDING notification is not user-readable: %v", err)
	}
	f.dispatch(t, afterDue)
	n := f.notifications(t)[0].Notification
	issueBefore := f.issues(t)[0]
	revisions := f.count(t, "SELECT COUNT(*) FROM revisions")
	f.clock = afterDue.Add(30 * time.Minute)

	read, err := f.attention.MarkNotificationRead(f.ctx, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if read.ReadAt == nil || !read.ReadAt.Equal(f.clock) || read.State != DeliverySent || read.DismissedAt != nil || read.LastRevisionID == n.LastRevisionID {
		t.Fatalf("unexpected: %+v", read)
	}
	if f.count(t, "SELECT COUNT(*) FROM revisions") != revisions+1 {
		t.Fatal("first read must create exactly one Revision")
	}
	var origin string
	if err := f.db.QueryRow("SELECT origin_type FROM revisions WHERE id = ?", read.LastRevisionID).Scan(&origin); err != nil || origin != "USER" {
		t.Fatalf("origin %q err=%v", origin, err)
	}
	f.clock = f.clock.Add(time.Hour)
	again, err := f.attention.MarkNotificationRead(f.ctx, n.ID)
	if err != nil || !again.ReadAt.Equal(*read.ReadAt) || again.LastRevisionID != read.LastRevisionID || f.count(t, "SELECT COUNT(*) FROM revisions") != revisions+1 {
		t.Fatalf("second read must be a no-op preserving read_at: %+v err=%v", again, err)
	}
	issueAfter := f.issues(t)[0]
	if issueAfter.State != IssueOpen || issueAfter.LastRevisionID != issueBefore.LastRevisionID || issueAfter.SnoozedUntil != nil {
		t.Fatal("reading a notification must not resolve, snooze or touch the issue")
	}
	if _, err := f.attention.MarkNotificationRead(f.ctx, "missing"); !errors.Is(err, ErrNotificationMissing) {
		t.Fatalf("got %v", err)
	}
	if _, err := f.attention.MarkNotificationRead(f.ctx, " "); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("got %v", err)
	}
}

func TestOpenIsAtomicWhenTheNotificationInsertFails(t *testing.T) {
	f := newFixture(t)
	f.workItemMonitor(t, f.workItem(t), due)
	if _, err := f.db.Exec(`CREATE TRIGGER fail_notification_insert BEFORE INSERT ON notifications BEGIN SELECT RAISE(ABORT, 'injected notification failure'); END`); err != nil {
		t.Fatal(err)
	}
	revisions, entities := f.count(t, "SELECT COUNT(*) FROM revisions"), f.count(t, "SELECT COUNT(*) FROM entities")
	if _, err := f.attention.Reconcile(f.ctx, afterDue); err == nil {
		t.Fatal("the injected failure must surface")
	}
	if f.count(t, "SELECT COUNT(*) FROM attention_issues") != 0 || f.count(t, "SELECT COUNT(*) FROM notifications") != 0 ||
		f.count(t, "SELECT COUNT(*) FROM revisions") != revisions || f.count(t, "SELECT COUNT(*) FROM entities") != entities {
		t.Fatal("a half-created Attention/Notification chain must never commit")
	}
	if _, err := f.db.Exec("DROP TRIGGER fail_notification_insert"); err != nil {
		t.Fatal(err)
	}
	if r := f.reconcile(t, afterDue); r.Opened != 1 {
		t.Fatalf("after the fault clears the chain must be created: %+v", r)
	}
}

func TestConcurrentReconcileCannotDuplicateTheLogicalIssueOrInitialNotification(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 3; i++ {
		f.workItemMonitor(t, f.workItem(t), due.Add(time.Duration(i)*time.Minute))
	}
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := f.attention.Reconcile(f.ctx, afterDue); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent reconcile must converge without surfacing conflicts: %v", err)
	}
	if got := f.count(t, "SELECT COUNT(*) FROM attention_issues WHERE state='OPEN'"); got != 3 {
		t.Fatalf("expected exactly 3 open issues, got %d", got)
	}
	if got := f.count(t, "SELECT COUNT(*) FROM notifications"); got != 3 {
		t.Fatalf("expected exactly 3 initial notifications, got %d", got)
	}
}

func TestSchemaBackstopsDuplicateOpenIssueAndInitialNotification(t *testing.T) {
	f := newFixture(t)
	f.workItemMonitor(t, f.workItem(t), due)
	f.reconcile(t, afterDue)
	issue := f.issues(t)[0]
	n := f.notifications(t)[0].Notification
	if _, err := f.db.Exec(`INSERT INTO entities (id, entity_type, created_at) VALUES ('dupA','ATTENTION_ISSUE','x'), ('dupN','NOTIFICATION','x')`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Exec(`INSERT INTO attention_issues SELECT 'dupA', work_item_id, subject_tracked_item_id, monitor_point_id, issue_type, severity, state, reason, reason_json, recommended_action, opened_at, resolved_at, snoozed_until, dedupe_key, last_revision_id, updated_at FROM attention_issues WHERE entity_id = ?`, issue.ID); err == nil {
		t.Fatal("the schema must reject a second OPEN issue with the same dedupe key")
	}
	if _, err := f.db.Exec(`INSERT INTO notifications SELECT 'dupN', attention_issue_id, work_item_id, subject_tracked_item_id, title, message, reason_snapshot_json, state, channel, sent_at, read_at, dismissed_at, dedupe_key, last_revision_id, created_at, updated_at FROM notifications WHERE entity_id = ?`, n.ID); err == nil {
		t.Fatal("the schema must reject a second initial notification for one issue")
	}
}

func TestNotificationViewsDeriveTheFiveProductViews(t *testing.T) {
	f := newFixture(t)
	wi1, wi2, wi3 := f.workItem(t), f.workItem(t), f.workItem(t)
	f.workItemMonitor(t, wi1, due)
	f.workItemMonitor(t, wi2, due.Add(time.Minute))
	f.workItemMonitor(t, wi3, due.Add(2*time.Minute))
	f.reconcile(t, afterDue)
	f.dispatch(t, afterDue)
	byWI := map[string]NotificationView{}
	for _, v := range f.notifications(t) {
		if v.Notification.WorkItemID != nil {
			byWI[*v.Notification.WorkItemID] = v
		}
	}
	if _, err := f.attention.SnoozeAttentionIssue(f.ctx, SnoozeInput{ID: byWI[wi2.ID].Issue.ID, Until: afterDue.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.attention.MarkNotificationRead(f.ctx, byWI[wi1.ID].Notification.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.workItems.SetLocalWorkItemLifecycleState(f.ctx, workitem.SetLocalWorkItemLifecycleStateInput{ID: wi3.ID, Target: workitem.LifecycleCompleted}); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t, afterDue)
	got := map[string]struct {
		d    Disposition
		read bool
	}{}
	for _, v := range f.notifications(t) {
		if v.Notification.WorkItemID != nil {
			got[*v.Notification.WorkItemID] = struct {
				d    Disposition
				read bool
			}{v.Issue.DispositionAt(afterDue), v.Notification.ReadAt != nil}
		}
	}
	if g := got[wi1.ID]; g.d != DispositionNeedsAction || !g.read {
		t.Fatalf("wi1 needs action and is read: %+v", g)
	}
	if g := got[wi2.ID]; g.d != DispositionSnoozed || g.read {
		t.Fatalf("wi2 snoozed and unread: %+v", g)
	}
	if g := got[wi3.ID]; g.d != DispositionResolved || g.read {
		t.Fatalf("wi3 resolved and unread: %+v", g)
	}
}

func TestAttentionTruthSurvivesReopeningTheFileBackedDatabase(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t)
	f.workItemMonitor(t, wi, due)
	f.reconcile(t, afterDue)
	f.dispatch(t, afterDue)
	issue, n := f.issues(t)[0], f.notifications(t)[0].Notification
	until := afterDue.Add(4 * time.Hour)
	if _, err := f.attention.SnoozeAttentionIssue(f.ctx, SnoozeInput{ID: issue.ID, Until: until}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.attention.MarkNotificationRead(f.ctx, n.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.Open(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.DB.Close() })
	workItems := workitem.NewService(reopened.DB, workitem.NewRepository(reopened.DB))
	service := NewService(reopened.DB, tracking.NewService(reopened.DB, workItems))
	views, err := service.ListNotificationViews(f.ctx)
	if err != nil || len(views) != 1 {
		t.Fatalf("views: %+v err=%v", views, err)
	}
	got := views[0]
	if got.Issue.State != IssueOpen || got.Issue.SnoozedUntil == nil || !got.Issue.SnoozedUntil.Equal(until) || got.Notification.ReadAt == nil ||
		got.Notification.ReasonSnapshotJSON != n.ReasonSnapshotJSON || got.Issue.ID != issue.ID {
		t.Fatalf("truth changed across reopen: %+v", got)
	}
	revisions := f.count2(t, reopened.DB, "SELECT COUNT(*) FROM revisions")
	r, err := service.Reconcile(f.ctx, afterDue)
	if err != nil || r.Opened != 0 || r.Resolved != 0 || f.count2(t, reopened.DB, "SELECT COUNT(*) FROM revisions") != revisions {
		t.Fatalf("reconcile after reopen must be idempotent: %+v err=%v", r, err)
	}
}

func (f *fixture) count2(t *testing.T, db *sql.DB, query string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestM10TruthIsConsumedNotDuplicated(t *testing.T) {
	f := newFixture(t)
	for _, table := range []string{"attention_issues", "notifications"} {
		rows, err := f.db.Query("SELECT name FROM pragma_table_info(?)", table)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var name string
			_ = rows.Scan(&name)
			for _, banned := range []string{"current_step", "target_date", "due_at", "expected", "step_state", "lifecycle", "next_notification", "score", "confidence", "rule_json"} {
				if strings.Contains(name, banned) {
					t.Fatalf("%s.%s duplicates M10 truth or adds out-of-scope fields", table, name)
				}
			}
		}
		_ = rows.Close()
	}
	for _, table := range []string{"tracked_items", "monitor_points", "work_items"} {
		rows, err := f.db.Query("SELECT name FROM pragma_table_info(?)", table)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var name string
			_ = rows.Scan(&name)
			if strings.Contains(name, "attention") || strings.Contains(name, "notif") {
				t.Fatalf("%s.%s adds an attention/notification flag to M10 truth", table, name)
			}
		}
		_ = rows.Close()
	}
}

func TestOpenTodoReminderAttentionTx(t *testing.T) {
	f := newFixture(t)
	now := time.Date(2026, 9, 25, 10, 0, 0, 0, time.UTC)
	dueAt := time.Date(2026, 9, 25, 10, 15, 0, 0, time.UTC)

	// Create planner item entity in DB
	plannerItemID := "planner-test-123"
	tx, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()

	found := foundation.NewRepository(tx)
	revID := "rev-init"
	sum := "Init"
	if _, err := found.CreateRevision(f.ctx, revID, foundation.RevisionOriginUser, &sum, now); err != nil {
		t.Fatal(err)
	}
	if err := found.CreateEntity(f.ctx, plannerItemID, "PLANNER_ITEM", now); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(f.ctx, `INSERT INTO planner_items (
		entity_id, authority_type, item_type, title, status, context_status, due_at, last_revision_id, created_at, updated_at
	) VALUES (?, 'LOCAL', 'TODO', 'Submit quarterly report', 'OPEN', 'COMPLETE', ?, ?, ?, ?)`,
		plannerItemID, dueAt.Format(time.RFC3339Nano), revID, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// Now open attention via OpenTodoReminderAttentionTx
	tx2, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx2.Rollback() }()

	input := OpenTodoReminderAttentionInput{
		PlannerItemID: plannerItemID,
		Title:         "Submit quarterly report",
		OffsetSeconds: 900,
		DueAt:         dueAt,
	}
	issue, notif, err := f.attention.OpenTodoReminderAttentionTx(f.ctx, tx2, input, now)
	if err != nil {
		t.Fatalf("OpenTodoReminderAttentionTx failed: %v", err)
	}
	if issue.Type != IssueTodoReminderDue {
		t.Fatalf("expected IssueTodoReminderDue, got %v", issue.Type)
	}
	if notif.State != DeliveryPending {
		t.Fatalf("expected DeliveryPending, got %v", notif.State)
	}
	if notif.PlannerItemID == nil || *notif.PlannerItemID != plannerItemID {
		t.Fatalf("expected planner item id %s, got %v", plannerItemID, notif.PlannerItemID)
	}
	if err := tx2.Commit(); err != nil {
		t.Fatal(err)
	}

	// History explains the rule that opened it: a reminder, not an overdue target.
	var openedSummary string
	if err := f.db.QueryRowContext(f.ctx, `SELECT summary FROM history_events WHERE event_type = 'ATTENTION_OPENED' AND subject_entity_id = ?`, issue.ID).Scan(&openedSummary); err != nil {
		t.Fatal(err)
	}
	if openedSummary != "Attention opened because a Todo reminder is due" {
		t.Fatalf("unexpected ATTENTION_OPENED summary for Todo reminder: %q", openedSummary)
	}

	// Dispatch pending notification
	disp, err := f.attention.DispatchPendingNotifications(f.ctx, now)
	if err != nil {
		t.Fatalf("DispatchPendingNotifications failed: %v", err)
	}
	if disp.Sent != 1 {
		t.Fatalf("expected 1 sent notification, got %d", disp.Sent)
	}

	// Complete / resolve by planner item
	tx3, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx3.Rollback() }()

	revResolve := "rev-resolve"
	sumResolve := "Resolve"
	if _, err := foundation.NewRepository(tx3).CreateRevision(f.ctx, revResolve, foundation.RevisionOriginUser, &sumResolve, now); err != nil {
		t.Fatal(err)
	}
	if err := f.attention.ResolveByPlannerItemTx(f.ctx, tx3, plannerItemID, revResolve, now); err != nil {
		t.Fatalf("ResolveByPlannerItemTx failed: %v", err)
	}
	if err := tx3.Commit(); err != nil {
		t.Fatal(err)
	}

	// Verify issue is resolved
	loadedIssue, err := f.attention.GetAttentionIssue(f.ctx, issue.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loadedIssue.State != IssueResolved {
		t.Fatalf("expected IssueResolved, got %v", loadedIssue.State)
	}
}

func TestPackageBoundariesKeepAIProposalAndAssistantOut(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range parsed.Imports {
			for _, banned := range []string{"internal/assistant", "internal/proposal", "internal/ai\"", "internal/openai", "internal/plugin", "internal/sourceprocessing"} {
				if strings.Contains(spec.Path.Value, banned) {
					t.Fatalf("%s imports %s: Attention/Notification must be deterministic and AI-free", file, banned)
				}
			}
		}
	}
	for _, dir := range []string{"../assistant", "../proposal", "../ai"} {
		sources, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, file := range sources {
			parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, spec := range parsed.Imports {
				if strings.Contains(spec.Path.Value, "internal/attention") {
					t.Fatalf("%s must not reach Attention mutation", file)
				}
			}
		}
	}
}
