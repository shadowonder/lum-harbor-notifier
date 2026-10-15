package app

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

var (
	notifDue  = time.Date(2026, 10, 15, 22, 0, 0, 0, time.UTC)
	notifNow  = notifDue.Add(2 * time.Hour)
	farFuture = time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
)

type centerFixture struct {
	app       *App
	attention *attention.Service
	tracking  *tracking.Service
	workItems *workitem.Service
	wi        map[string]*workitem.WorkItem
	monitors  map[string]*tracking.MonitorPoint
	notif     map[string]string // scenario -> notification ID
	issue     map[string]string // scenario -> issue ID
}

func newCenterFixture(t *testing.T) (*centerFixture, func(string) int) {
	t.Helper()
	application, workItems, db := newWorkItemApp(t)
	trackingService := tracking.NewService(db, workItems)
	attentionService := attention.NewService(db, trackingService)
	application.trackingService, application.attentionService = trackingService, attentionService
	f := &centerFixture{app: application, attention: attentionService, tracking: trackingService, workItems: workItems,
		wi: map[string]*workitem.WorkItem{}, monitors: map[string]*tracking.MonitorPoint{}, notif: map[string]string{}, issue: map[string]string{}}
	count := func(query string) int {
		var n int
		if err := db.QueryRow(query).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	ctx := context.Background()
	completed := workitem.LifecycleCompleted
	for _, name := range []string{"sentUnread", "sentRead", "sentSnoozed", "sentResolved", "pending", "cancelled"} {
		wi, err := workItems.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: "WI " + name})
		if err != nil {
			t.Fatal(err)
		}
		m, err := trackingService.CreateMonitorPoint(ctx, tracking.CreateMonitorPointInput{WorkItemID: wi.ID, Kind: tracking.MonitorFinalTarget, Title: name + " target", DueAt: notifDue, Timezone: "UTC", ExpectedWorkItemLifecycle: &completed})
		if err != nil {
			t.Fatal(err)
		}
		f.wi[name], f.monitors[name] = wi, m
	}
	must := func(_ any, err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(attentionService.Reconcile(ctx, notifNow))
	issues, err := attentionService.ListAttentionIssues(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for name, wi := range f.wi {
		for _, issue := range issues {
			if issue.WorkItemID != nil && *issue.WorkItemID == wi.ID {
				f.issue[name] = issue.ID
			}
		}
	}
	must(attentionService.SnoozeAttentionIssue(ctx, attention.SnoozeInput{ID: f.issue["pending"], Until: farFuture}))
	must(workItems.SetLocalWorkItemLifecycleState(ctx, workitem.SetLocalWorkItemLifecycleStateInput{ID: f.wi["cancelled"].ID, Target: workitem.LifecycleCompleted}))
	must(attentionService.Reconcile(ctx, notifNow))
	must(attentionService.DispatchPendingNotifications(ctx, notifNow))
	views, err := attentionService.ListNotificationViews(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for name, wi := range f.wi {
		for _, v := range views {
			if v.Notification.WorkItemID != nil && *v.Notification.WorkItemID == wi.ID {
				f.notif[name] = v.Notification.ID
			}
		}
	}
	must(attentionService.MarkNotificationRead(ctx, f.notif["sentRead"]))
	must(attentionService.SnoozeAttentionIssue(ctx, attention.SnoozeInput{ID: f.issue["sentSnoozed"], Until: farFuture}))
	must(workItems.SetLocalWorkItemLifecycleState(ctx, workitem.SetLocalWorkItemLifecycleStateInput{ID: f.wi["sentResolved"].ID, Target: workitem.LifecycleCompleted}))
	must(attentionService.Reconcile(ctx, notifNow))
	return f, count
}

func TestNotificationCenterProjectsOnlyDeliveredNotificationsWithRealTruth(t *testing.T) {
	f, _ := newCenterFixture(t)
	center, err := f.app.GetNotificationCenter()
	if err != nil {
		t.Fatal(err)
	}
	if center.At == "" || len(center.Items) != 4 {
		t.Fatalf("PENDING and CANCELLED must be excluded; expected 4 SENT items, got %d", len(center.Items))
	}
	byWI := map[string]NotificationCenterItemDTO{}
	for _, item := range center.Items {
		if item.WorkItemID != nil {
			byWI[*item.WorkItemID] = item
		}
		if item.DeliveryState != "SENT" || item.SentAt == nil || item.IssueType != "MONITOR_OVERDUE_UNMET" || item.Severity != "WARNING" {
			t.Fatalf("every center item is a delivered notification: %+v", item)
		}
	}
	for _, hidden := range []string{"pending", "cancelled"} {
		if _, ok := byWI[f.wi[hidden].ID]; ok {
			t.Fatalf("%s must not be in the center", hidden)
		}
	}
	unread := byWI[f.wi["sentUnread"].ID]
	if unread.ReadAt != nil || unread.AttentionState != "OPEN" || unread.Disposition != "NEEDS_ACTION" || unread.SnoozedUntil != nil ||
		unread.NotificationID != f.notif["sentUnread"] || unread.AttentionIssueID != f.issue["sentUnread"] ||
		unread.WorkItemTitle == nil || *unread.WorkItemTitle != "WI sentUnread" || unread.Title != "sentUnread target" || unread.RecommendedAction == nil {
		t.Fatalf("unread: %+v", unread)
	}
	if read := byWI[f.wi["sentRead"].ID]; read.ReadAt == nil || read.Disposition != "NEEDS_ACTION" {
		t.Fatalf("read_at must be preserved: %+v", read)
	}
	if s := byWI[f.wi["sentSnoozed"].ID]; s.Disposition != "SNOOZED" || s.SnoozedUntil == nil || s.AttentionState != "OPEN" {
		t.Fatalf("snoozed: %+v", s)
	}
	if r := byWI[f.wi["sentResolved"].ID]; r.Disposition != "RESOLVED" || r.AttentionState != "RESOLVED" || r.ResolvedAt == nil || r.DeliveryState != "SENT" {
		t.Fatalf("resolved keeps its SENT communication: %+v", r)
	}
	if unread.Reason.Version != 1 || unread.Reason.MonitorPointID != f.monitors["sentUnread"].ID || unread.Reason.SubjectKind != "WORK_ITEM" ||
		unread.Reason.ExpectedKind != "WORK_ITEM_LIFECYCLE" || unread.Reason.ExpectedWorkItemLifecycle == nil || *unread.Reason.ExpectedWorkItemLifecycle != "COMPLETED" ||
		unread.Reason.ActualWorkItemLifecycle == nil || *unread.Reason.ActualWorkItemLifecycle != "ACTIVE" {
		t.Fatalf("reason facts: %+v", unread.Reason)
	}
}

func TestNotificationCenterDTOCarriesNoPresentationOrInternalFields(t *testing.T) {
	raw, _ := json.Marshal(NotificationCenterItemDTO{})
	var fields map[string]any
	_ = json.Unmarshal(raw, &fields)
	for name := range fields {
		for _, banned := range []string{"revision", "color", "badge", "icon", "css", "tab", "label", "variant", "dedupe", "channel"} {
			if strings.Contains(strings.ToLower(name), banned) {
				t.Fatalf("center item exposes %q", name)
			}
		}
	}
	// The five product views derive from one collection: the required facts exist.
	for _, required := range []string{"readAt", "attentionState", "snoozedUntil", "disposition", "workItemId", "subjectTrackedItemId"} {
		if _, ok := fields[required]; !ok {
			t.Fatalf("missing %q", required)
		}
	}
}

func TestMarkNotificationReadBoundary(t *testing.T) {
	f, count := newCenterFixture(t)
	revisions := count("SELECT COUNT(*) FROM revisions")
	result, err := f.app.MarkNotificationRead(MarkNotificationReadRequestDTO{NotificationID: f.notif["sentUnread"]})
	if err != nil || result.NotificationID != f.notif["sentUnread"] || result.ReadAt == "" {
		t.Fatalf("unexpected: %+v err=%v", result, err)
	}
	again, err := f.app.MarkNotificationRead(MarkNotificationReadRequestDTO{NotificationID: f.notif["sentUnread"]})
	if err != nil || again.ReadAt != result.ReadAt || count("SELECT COUNT(*) FROM revisions") != revisions+1 {
		t.Fatalf("repeat must be a no-op preserving read_at: %+v err=%v", again, err)
	}
	// The issue must be untouched by reading.
	issue, _ := f.attention.GetAttentionIssue(context.Background(), f.issue["sentUnread"])
	if issue.State != attention.IssueOpen || issue.SnoozedUntil != nil {
		t.Fatal("reading must not resolve or snooze the issue")
	}
	// PENDING and CANCELLED are not readable.
	views, _ := f.attention.ListNotificationViews(context.Background())
	byIssue := map[string]attention.Notification{}
	for _, v := range views {
		if v.Notification.WorkItemID != nil {
			byIssue[*v.Notification.WorkItemID] = v.Notification
		}
	}
	for _, name := range []string{"pending", "cancelled"} {
		if _, err := f.app.MarkNotificationRead(MarkNotificationReadRequestDTO{NotificationID: byIssue[f.wi[name].ID].ID}); err == nil || !strings.Contains(err.Error(), "not been delivered") {
			t.Fatalf("%s notification must not be readable: %v", name, err)
		}
	}
	if _, err := f.app.MarkNotificationRead(MarkNotificationReadRequestDTO{NotificationID: "missing"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("got %v", err)
	}
	// Unread projection is unaffected for PENDING/CANCELLED.
	center, _ := f.app.GetNotificationCenter()
	for _, item := range center.Items {
		if item.NotificationID == byIssue[f.wi["pending"].ID].ID || item.NotificationID == byIssue[f.wi["cancelled"].ID].ID {
			t.Fatal("PENDING/CANCELLED must never appear as unread items")
		}
	}
}

func TestSnoozeAttentionIssueBoundary(t *testing.T) {
	f, count := newCenterFixture(t)
	until := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	result, err := f.app.SnoozeAttentionIssue(SnoozeAttentionIssueRequestDTO{AttentionIssueID: f.issue["sentUnread"], SnoozedUntil: until.Format(time.RFC3339)})
	if err != nil || result.AttentionIssueID != f.issue["sentUnread"] || result.SnoozedUntil == "" {
		t.Fatalf("unexpected: %+v err=%v", result, err)
	}
	center, _ := f.app.GetNotificationCenter()
	for _, item := range center.Items {
		if item.AttentionIssueID == f.issue["sentUnread"] && (item.Disposition != "SNOOZED" || item.AttentionState != "OPEN" || item.ReadAt != nil) {
			t.Fatalf("snooze must only change snoozed_until: %+v", item)
		}
	}
	bad := map[string]SnoozeAttentionIssueRequestDTO{
		"past":           {AttentionIssueID: f.issue["sentSnoozed"], SnoozedUntil: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)},
		"resolved":       {AttentionIssueID: f.issue["sentResolved"], SnoozedUntil: farFuture.Format(time.RFC3339)},
		"unknown":        {AttentionIssueID: "missing", SnoozedUntil: farFuture.Format(time.RFC3339)},
		"not a time":     {AttentionIssueID: f.issue["sentUnread"], SnoozedUntil: "tomorrow"},
		"blank issue id": {AttentionIssueID: " ", SnoozedUntil: farFuture.Format(time.RFC3339)},
	}
	revisions := count("SELECT COUNT(*) FROM revisions")
	for name, request := range bad {
		if _, err := f.app.SnoozeAttentionIssue(request); err == nil {
			t.Fatalf("%s must be rejected", name)
		}
	}
	if count("SELECT COUNT(*) FROM revisions") != revisions {
		t.Fatal("rejected snoozes must create no Revision")
	}
	// Frontend-owned authority fields do not exist on the request.
	raw, _ := json.Marshal(SnoozeAttentionIssueRequestDTO{})
	for _, banned := range []string{"revision", "now", "state", "updatedAt", "reason", "notification"} {
		if strings.Contains(strings.ToLower(string(raw)), banned) {
			t.Fatalf("snooze request must not accept %q", banned)
		}
	}
	if count("SELECT COUNT(*) FROM change_proposals") != 0 {
		t.Fatal("Notification commands must not create Proposals")
	}
}

func TestNotificationBoundaryFailsHonestlyWhenUninitializedAndIsAIAndAssistantFree(t *testing.T) {
	bare := &App{}
	if _, err := bare.GetNotificationCenter(); err == nil {
		t.Fatal("uninitialized center must fail")
	}
	if _, err := bare.MarkNotificationRead(MarkNotificationReadRequestDTO{NotificationID: "x"}); err == nil {
		t.Fatal("uninitialized mark read must fail")
	}
	for _, file := range []string{"assistant.go"} {
		src, _ := os.ReadFile(file)
		for _, banned := range []string{"attentionService", "internal/attention", "MarkNotificationRead", "SnoozeAttentionIssue", "Reconcile", "DispatchPending"} {
			if strings.Contains(string(src), banned) {
				t.Fatalf("the Assistant path must not reach Attention/Notification (%s)", banned)
			}
		}
	}
	for _, file := range []string{"app_notification.go", "notification_dto.go"} {
		src, _ := os.ReadFile(file)
		for _, banned := range []string{"internal/ai\"", "aiExecutor", "internal/proposal", "Reconcile(", "DispatchPending"} {
			if strings.Contains(string(src), banned) {
				t.Fatalf("%s must not depend on AI/Proposal or expose background commands: %s", file, banned)
			}
		}
	}
}
