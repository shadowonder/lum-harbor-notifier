package attention

import (
	"errors"
	"sync"
	"testing"
	"time"

	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

func (f *fixture) revisionCount(t *testing.T) int {
	return f.count(t, "SELECT COUNT(*) FROM revisions")
}

func (f *fixture) onlyNotification(t *testing.T) Notification {
	t.Helper()
	views := f.notifications(t)
	if len(views) != 1 {
		t.Fatalf("expected one notification, got %d", len(views))
	}
	return views[0].Notification
}

func TestDispatchSendsAnOpenUnsnoozedNotificationOnce(t *testing.T) {
	f := newFixture(t)
	f.workItemMonitor(t, f.workItem(t), due)
	f.reconcile(t, afterDue)
	if n := f.onlyNotification(t); n.State != DeliveryPending || n.SentAt != nil {
		t.Fatalf("a freshly opened notification is PENDING: %+v", n)
	}
	revisions := f.revisionCount(t)

	r := f.dispatch(t, afterDue)
	if r.Sent != 1 || r.Cancelled != 0 || r.Deferred != 0 {
		t.Fatalf("unexpected: %+v", r)
	}
	n := f.onlyNotification(t)
	if n.State != DeliverySent || n.SentAt == nil || !n.SentAt.Equal(afterDue) || n.ReadAt != nil || n.Channel != ChannelInApp {
		t.Fatalf("unexpected: %+v", n)
	}
	if f.revisionCount(t) != revisions+1 {
		t.Fatal("PENDING→SENT must create exactly one Revision")
	}
	var origin string
	if err := f.db.QueryRow("SELECT origin_type FROM revisions WHERE id = ?", n.LastRevisionID).Scan(&origin); err != nil || origin != "SYSTEM" {
		t.Fatalf("origin %q err=%v", origin, err)
	}
	if issue := f.issues(t)[0]; issue.State != IssueOpen {
		t.Fatal("delivery must not touch the AttentionIssue")
	}
	// Repeat: no churn.
	if r := f.dispatch(t, afterDue.Add(time.Hour)); r != (DispatchResult{}) || f.revisionCount(t) != revisions+1 {
		t.Fatalf("repeated dispatch must be a no-op: %+v", r)
	}
	if again := f.onlyNotification(t); !again.SentAt.Equal(*n.SentAt) || again.LastRevisionID != n.LastRevisionID {
		t.Fatal("sent_at and revision must not change")
	}
}

func TestActiveSnoozeBlocksFirstDeliveryAndExpiryAllowsIt(t *testing.T) {
	f := newFixture(t)
	f.workItemMonitor(t, f.workItem(t), due)
	f.reconcile(t, afterDue)
	until := afterDue.Add(3 * time.Hour)
	if _, err := f.attention.SnoozeAttentionIssue(f.ctx, SnoozeInput{ID: f.issues(t)[0].ID, Until: until}); err != nil {
		t.Fatal(err)
	}
	revisions := f.revisionCount(t)

	r := f.dispatch(t, afterDue.Add(time.Hour))
	if r.Deferred != 1 || r.Sent != 0 || f.onlyNotification(t).State != DeliveryPending || f.revisionCount(t) != revisions {
		t.Fatalf("an actively snoozed issue must keep its notification PENDING with no churn: %+v", r)
	}
	// Exactly at expiry (snoozed_until <= now) the same PENDING notification is sent.
	r = f.dispatch(t, until)
	if r.Sent != 1 {
		t.Fatalf("snooze expiry must allow delivery: %+v", r)
	}
	if n := f.onlyNotification(t); n.State != DeliverySent || !n.SentAt.Equal(until) {
		t.Fatalf("unexpected: %+v", n)
	}
	if len(f.notifications(t)) != 1 || len(f.issues(t)) != 1 {
		t.Fatal("no second notification or issue may appear")
	}
}

func TestResolvedBeforeDeliveryIsCancelledNeverSent(t *testing.T) {
	scenarios := map[string]func(*testing.T, *fixture, *workitem.WorkItem, *tracking.MonitorPoint){
		"expectation becomes met": func(t *testing.T, f *fixture, wi *workitem.WorkItem, _ *tracking.MonitorPoint) {
			if _, err := f.workItems.SetLocalWorkItemLifecycleState(f.ctx, workitem.SetLocalWorkItemLifecycleStateInput{ID: wi.ID, Target: workitem.LifecycleCompleted}); err != nil {
				t.Fatal(err)
			}
		},
		"monitor satisfied": func(t *testing.T, f *fixture, _ *workitem.WorkItem, m *tracking.MonitorPoint) {
			if _, err := f.tracking.SetMonitorPointState(f.ctx, tracking.SetMonitorPointStateInput{ID: m.ID, Target: tracking.MonitorSatisfied}); err != nil {
				t.Fatal(err)
			}
		},
		"monitor cancelled": func(t *testing.T, f *fixture, _ *workitem.WorkItem, m *tracking.MonitorPoint) {
			if _, err := f.tracking.SetMonitorPointState(f.ctx, tracking.SetMonitorPointStateInput{ID: m.ID, Target: tracking.MonitorCancelled}); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, change := range scenarios {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			wi := f.workItem(t)
			m := f.workItemMonitor(t, wi, due)
			f.reconcile(t, afterDue) // OPEN + PENDING
			change(t, f, wi, m)
			// One production cycle: Reconcile first, then Dispatch.
			f.reconcile(t, afterDue)
			r := f.dispatch(t, afterDue)
			if r.Cancelled != 1 || r.Sent != 0 {
				t.Fatalf("an obsolete pending notification must be CANCELLED, not SENT: %+v", r)
			}
			n := f.onlyNotification(t)
			if n.State != DeliveryCancelled || n.SentAt != nil || n.ReasonSnapshotJSON == "" {
				t.Fatalf("unexpected: %+v", n)
			}
			if f.issues(t)[0].State != IssueResolved {
				t.Fatal("the issue must be RESOLVED")
			}
			var origin string
			if err := f.db.QueryRow("SELECT origin_type FROM revisions WHERE id = ?", n.LastRevisionID).Scan(&origin); err != nil || origin != "SYSTEM" {
				t.Fatalf("origin %q err=%v", origin, err)
			}
			// CANCELLED never resurrects, even if the condition recurs.
			revisions := f.revisionCount(t)
			f.dispatch(t, afterDue.Add(time.Hour))
			if f.onlyNotification(t).State != DeliveryCancelled || f.revisionCount(t) != revisions {
				t.Fatal("CANCELLED must be terminal and dispatch a no-op")
			}
			if _, err := f.attention.MarkNotificationRead(f.ctx, n.ID); !errors.Is(err, ErrNotDelivered) {
				t.Fatalf("a CANCELLED notification is not user-readable: %v", err)
			}
		})
	}
}

func TestSentNotificationIsUnchangedByLaterResolution(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t)
	f.workItemMonitor(t, wi, due)
	f.reconcile(t, afterDue)
	f.dispatch(t, afterDue)
	sent := f.onlyNotification(t)
	if _, err := f.attention.MarkNotificationRead(f.ctx, sent.ID); err != nil {
		t.Fatal(err)
	}
	read := f.onlyNotification(t)
	if _, err := f.workItems.SetLocalWorkItemLifecycleState(f.ctx, workitem.SetLocalWorkItemLifecycleStateInput{ID: wi.ID, Target: workitem.LifecycleCompleted}); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t, afterDue)
	f.dispatch(t, afterDue)
	after := f.onlyNotification(t)
	if after.State != DeliverySent || after.ReadAt == nil || !after.ReadAt.Equal(*read.ReadAt) || after.LastRevisionID != read.LastRevisionID ||
		after.Message != read.Message || after.ReasonSnapshotJSON != read.ReasonSnapshotJSON || after.SentAt == nil || !after.SentAt.Equal(*read.SentAt) {
		t.Fatalf("a SENT notification must survive resolution untouched: %+v", after)
	}
	if f.issues(t)[0].State != IssueResolved {
		t.Fatal("the issue must be RESOLVED")
	}
}

func TestDispatchIsConcurrencySafe(t *testing.T) {
	f := newFixture(t)
	for i := 0; i < 4; i++ {
		f.workItemMonitor(t, f.workItem(t), due.Add(time.Duration(i)*time.Minute))
	}
	f.reconcile(t, afterDue)
	revisions := f.revisionCount(t)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	total := make(chan int, 12)
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := f.attention.DispatchPendingNotifications(f.ctx, afterDue)
			if err != nil {
				errs <- err
				return
			}
			total <- r.Sent
		}()
	}
	wg.Wait()
	close(errs)
	close(total)
	for err := range errs {
		t.Fatalf("concurrent dispatch must converge: %v", err)
	}
	sum := 0
	for n := range total {
		sum += n
	}
	if sum != 4 || f.count(t, "SELECT COUNT(*) FROM notifications WHERE state='SENT'") != 4 || f.revisionCount(t) != revisions+4 {
		t.Fatalf("each notification must be sent exactly once with one Revision: sent=%d revisions=%d", sum, f.revisionCount(t)-revisions)
	}
}

func TestDispatchConditionalTransitionLosesTheRaceSafely(t *testing.T) {
	f := newFixture(t)
	f.workItemMonitor(t, f.workItem(t), due)
	f.reconcile(t, afterDue)
	n := f.onlyNotification(t)
	f.dispatch(t, afterDue)
	tx, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := NewRepository(tx).markSent(f.ctx, n.ID, n.LastRevisionID, afterDue); !errors.Is(err, errConflict) {
		t.Fatalf("a transition on a non-PENDING row must lose the CAS, got %v", err)
	}
	if err := NewRepository(tx).markCancelled(f.ctx, n.ID, n.LastRevisionID, afterDue); !errors.Is(err, errConflict) {
		t.Fatalf("got %v", err)
	}
}

func TestNotificationCenterContainsOnlySentNotificationsWithDerivedDisposition(t *testing.T) {
	f := newFixture(t)
	wiSent, wiRead, wiSnoozed, wiResolved, wiPending, wiCancelled := f.workItem(t), f.workItem(t), f.workItem(t), f.workItem(t), f.workItem(t), f.workItem(t)
	monitors := map[string]*tracking.MonitorPoint{}
	for i, wi := range []*workitem.WorkItem{wiSent, wiRead, wiSnoozed, wiResolved, wiCancelled} {
		monitors[wi.ID] = f.workItemMonitor(t, wi, due.Add(time.Duration(i)*time.Minute))
	}
	f.reconcile(t, afterDue)
	// Cancel one before delivery.
	if _, err := f.tracking.SetMonitorPointState(f.ctx, tracking.SetMonitorPointStateInput{ID: monitors[wiCancelled.ID].ID, Target: tracking.MonitorCancelled}); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t, afterDue)
	// Pending stays pending: snooze it before dispatch (blocked), created last.
	f.workItemMonitor(t, wiPending, due.Add(10*time.Minute))
	f.reconcile(t, afterDue)
	var pendingIssue AttentionIssue
	for _, i := range f.issues(t) {
		if i.WorkItemID != nil && *i.WorkItemID == wiPending.ID {
			pendingIssue = i
		}
	}
	if _, err := f.attention.SnoozeAttentionIssue(f.ctx, SnoozeInput{ID: pendingIssue.ID, Until: afterDue.Add(24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	f.dispatch(t, afterDue)

	byWI := map[string]NotificationView{}
	for _, v := range f.notifications(t) {
		if v.Notification.WorkItemID != nil {
			byWI[*v.Notification.WorkItemID] = v
		}
	}
	if _, err := f.attention.MarkNotificationRead(f.ctx, byWI[wiRead.ID].Notification.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.attention.SnoozeAttentionIssue(f.ctx, SnoozeInput{ID: byWI[wiSnoozed.ID].Issue.ID, Until: afterDue.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.workItems.SetLocalWorkItemLifecycleState(f.ctx, workitem.SetLocalWorkItemLifecycleStateInput{ID: wiResolved.ID, Target: workitem.LifecycleCompleted}); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t, afterDue)

	center, err := f.attention.NotificationCenter(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !center.At.Equal(afterDue) {
		t.Fatalf("dispositions are derived at the service clock: %v", center.At)
	}
	got := map[string]CenterItem{}
	for _, item := range center.Items {
		if item.View.Notification.WorkItemID != nil {
			got[*item.View.Notification.WorkItemID] = item
		}
	}
	if len(got) != 4 {
		t.Fatalf("only the 4 SENT notifications belong in the center (PENDING and CANCELLED excluded), got %d", len(got))
	}
	if _, ok := got[wiPending.ID]; ok {
		t.Fatal("PENDING must not be in the center")
	}
	if _, ok := got[wiCancelled.ID]; ok {
		t.Fatal("CANCELLED must not be in the center")
	}
	if g := got[wiSent.ID]; g.Disposition != DispositionNeedsAction || g.View.Notification.ReadAt != nil {
		t.Fatalf("sent unread needs action: %+v", g)
	}
	if g := got[wiRead.ID]; g.Disposition != DispositionNeedsAction || g.View.Notification.ReadAt == nil {
		t.Fatalf("read notification keeps its read_at: %+v", g)
	}
	if g := got[wiSnoozed.ID]; g.Disposition != DispositionSnoozed || g.View.Issue.SnoozedUntil == nil {
		t.Fatalf("snoozed: %+v", g)
	}
	if g := got[wiResolved.ID]; g.Disposition != DispositionResolved || g.View.Notification.State != DeliverySent {
		t.Fatalf("resolved keeps its SENT communication: %+v", g)
	}
	// Snooze expiry flips the derived disposition with no state write.
	f.clock = afterDue.Add(2 * time.Hour)
	later, _ := f.attention.NotificationCenter(f.ctx)
	for _, item := range later.Items {
		if item.View.Notification.WorkItemID != nil && *item.View.Notification.WorkItemID == wiSnoozed.ID && item.Disposition != DispositionNeedsAction {
			t.Fatal("an expired snooze must need action again")
		}
	}
}
