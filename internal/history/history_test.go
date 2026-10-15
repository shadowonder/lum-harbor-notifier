package history_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/database"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

type fixture struct {
	ctx       context.Context
	db        *sql.DB
	workItems *workitem.Service
	tracking  *tracking.Service
	attention *attention.Service
}

func openFixture(t *testing.T) *fixture {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	db, err := database.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DB.Close() })
	return services(db.DB)
}

func services(db *sql.DB) *fixture {
	workItems := workitem.NewService(db, workitem.NewRepository(db))
	trackingService := tracking.NewService(db, workItems)
	return &fixture{ctx: context.Background(), db: db, workItems: workItems, tracking: trackingService, attention: attention.NewService(db, trackingService)}
}

func (f *fixture) workItem(t *testing.T) *workitem.WorkItem {
	t.Helper()
	item, err := f.workItems.CreateLocalWorkItem(f.ctx, workitem.CreateLocalWorkItemInput{Title: "History release"})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func (f *fixture) monitor(t *testing.T, item *workitem.WorkItem, kind tracking.MonitorKind, due time.Time) *tracking.MonitorPoint {
	t.Helper()
	expected := workitem.LifecycleCompleted
	point, err := f.tracking.CreateMonitorPoint(f.ctx, tracking.CreateMonitorPointInput{
		WorkItemID: item.ID, Kind: kind, Title: "Ship release", DueAt: due, Timezone: "UTC", ExpectedWorkItemLifecycle: &expected,
	})
	if err != nil {
		t.Fatal(err)
	}
	return point
}

func TestRealMonitorAttentionNotificationCausalChain(t *testing.T) {
	f := openFixture(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	item := f.workItem(t)
	point := f.monitor(t, item, tracking.MonitorFinalTarget, now.Add(-time.Hour))
	now = time.Now().UTC().Truncate(time.Microsecond)

	if result, err := f.attention.Reconcile(f.ctx, now); err != nil || result.Opened != 1 {
		t.Fatalf("open attention: result=%+v err=%v", result, err)
	}
	issues, _ := f.attention.ListAttentionIssues(f.ctx)
	notifications, _ := f.attention.ListNotificationViews(f.ctx)
	issue, notification := issues[0], notifications[0].Notification
	if result, err := f.attention.DispatchPendingNotifications(f.ctx, time.Now().UTC()); err != nil || result.Sent != 1 {
		t.Fatalf("dispatch: result=%+v err=%v", result, err)
	}
	if _, err := f.attention.MarkNotificationRead(f.ctx, notification.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.attention.SnoozeAttentionIssue(f.ctx, attention.SnoozeInput{ID: issue.ID, Until: time.Now().UTC().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	stateEventCount := count(t, f.db, "SELECT COUNT(*) FROM history_events")
	if _, err := f.tracking.SetMonitorPointState(f.ctx, tracking.SetMonitorPointStateInput{ID: point.ID, Target: tracking.MonitorSatisfied}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tracking.SetMonitorPointState(f.ctx, tracking.SetMonitorPointStateInput{ID: point.ID, Target: tracking.MonitorSatisfied}); err != nil {
		t.Fatal(err)
	}
	if got := count(t, f.db, "SELECT COUNT(*) FROM history_events"); got != stateEventCount+1 {
		t.Fatalf("same-state monitor request created history: before=%d after=%d", stateEventCount, got)
	}
	if result, err := f.attention.Reconcile(f.ctx, now.Add(2*time.Minute)); err != nil || result.Resolved != 1 {
		t.Fatalf("resolve attention: result=%+v err=%v", result, err)
	}

	repo := history.NewRepository(f.db)
	correlation := history.MonitorCorrelation(point.ID)
	events, err := repo.ListByCorrelation(f.ctx, correlation, 100)
	if err != nil {
		t.Fatal(err)
	}
	want := []history.EventType{
		history.EventMonitorCreated, history.EventAttentionOpened, history.EventNotificationCreated,
		history.EventNotificationSent, history.EventNotificationRead, history.EventAttentionSnoozed,
		history.EventMonitorStateChanged, history.EventAttentionResolved,
	}
	if len(events) != len(want) {
		t.Fatalf("got %d events, want %d: %+v", len(events), len(want), events)
	}
	byType := map[history.EventType]history.Event{}
	for index, event := range events {
		if index > 0 && (event.OccurredAt.Before(events[index-1].OccurredAt) || (event.OccurredAt.Equal(events[index-1].OccurredAt) && event.ID < events[index-1].ID)) {
			t.Fatalf("trace is not deterministically ordered at %d", index)
		}
		byType[event.Type] = event
		if event.CorrelationID != correlation || event.WorkItemID == nil || *event.WorkItemID != item.ID || event.SubjectEntityID == nil || event.RevisionID == nil {
			t.Fatalf("missing durable links on %+v", event)
		}
		if event.BeforeJSON != nil {
			assertVersionedObject(t, *event.BeforeJSON)
		}
		if event.AfterJSON != nil {
			assertVersionedObject(t, *event.AfterJSON)
		}
	}
	for _, eventType := range want {
		if _, ok := byType[eventType]; !ok {
			t.Errorf("missing event %s", eventType)
		}
	}
	if byType[history.EventMonitorCreated].Actor != history.ActorUser || byType[history.EventAttentionOpened].Actor != history.ActorRule || byType[history.EventNotificationSent].Actor != history.ActorSystem {
		t.Fatal("actor vocabulary does not match business ownership")
	}
	opened, created, sent := byType[history.EventAttentionOpened], byType[history.EventNotificationCreated], byType[history.EventNotificationSent]
	if opened.RevisionID == nil || created.RevisionID == nil || *opened.RevisionID != *created.RevisionID {
		t.Fatal("attention open and notification creation must share one revision")
	}
	if created.CausationEventID == nil || *created.CausationEventID != opened.ID {
		t.Fatal("notification creation must be directly caused by attention open")
	}
	if sent.CausationEventID == nil || *sent.CausationEventID != created.ID {
		t.Fatal("notification delivery must be directly caused by notification creation")
	}
	changed, resolved := byType[history.EventMonitorStateChanged], byType[history.EventAttentionResolved]
	if resolved.CausationEventID == nil || *resolved.CausationEventID != changed.ID {
		t.Fatal("monitor terminal transition must directly cause deterministic attention resolution")
	}
	if _, err := repo.GetByID(f.ctx, sent.ID); err != nil {
		t.Fatal(err)
	}
	if recent, err := repo.ListRecent(f.ctx, 3); err != nil || len(recent) != 3 {
		t.Fatalf("bounded recent read: len=%d err=%v", len(recent), err)
	}
	if workItemEvents, err := repo.ListByWorkItem(f.ctx, item.ID, 100); err != nil || len(workItemEvents) != 9 {
		t.Fatalf("work-item read: len=%d err=%v", len(workItemEvents), err)
	}

	beforeNoOp := count(t, f.db, "SELECT COUNT(*) FROM history_events")
	if _, err := f.attention.Reconcile(f.ctx, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.attention.DispatchPendingNotifications(f.ctx, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.attention.MarkNotificationRead(f.ctx, notification.ID); err != nil {
		t.Fatal(err)
	}
	if got := count(t, f.db, "SELECT COUNT(*) FROM history_events"); got != beforeNoOp {
		t.Fatalf("no-op cycle created history: before=%d after=%d", beforeNoOp, got)
	}
}

func TestCorrelationIsolationForTwoMonitorsOnOneWorkItem(t *testing.T) {
	f := openFixture(t)
	now := time.Now().UTC()
	item := f.workItem(t)
	first := f.monitor(t, item, tracking.MonitorCheckpoint, now.Add(-2*time.Hour))
	second := f.monitor(t, item, tracking.MonitorDeadline, now.Add(-time.Hour))
	if _, err := f.attention.Reconcile(f.ctx, now); err != nil {
		t.Fatal(err)
	}
	for _, point := range []*tracking.MonitorPoint{first, second} {
		events, err := history.NewRepository(f.db).ListByCorrelation(f.ctx, history.MonitorCorrelation(point.ID), 20)
		if err != nil || len(events) != 3 {
			t.Fatalf("trace %s: len=%d err=%v", point.ID, len(events), err)
		}
		for _, event := range events {
			if event.CorrelationID != history.MonitorCorrelation(point.ID) {
				t.Fatalf("cross-monitor event leakage: %+v", event)
			}
		}
	}
	if history.MonitorCorrelation(first.ID) == history.MonitorCorrelation(second.ID) {
		t.Fatal("monitor correlations must be distinct")
	}
}

func TestHistoryInsertFailureRollsBackBusinessMutations(t *testing.T) {
	t.Run("monitor state", func(t *testing.T) {
		f := openFixture(t)
		item := f.workItem(t)
		point := f.monitor(t, item, tracking.MonitorCheckpoint, time.Now().Add(time.Hour))
		beforeRevisions := count(t, f.db, "SELECT COUNT(*) FROM revisions")
		failEvent(t, f.db, history.EventMonitorStateChanged)
		if _, err := f.tracking.SetMonitorPointState(f.ctx, tracking.SetMonitorPointStateInput{ID: point.ID, Target: tracking.MonitorSatisfied}); err == nil {
			t.Fatal("expected history failure")
		}
		assertScalar(t, f.db, "SELECT state FROM monitor_points WHERE entity_id = ?", point.ID, "ACTIVE")
		if count(t, f.db, "SELECT COUNT(*) FROM revisions") != beforeRevisions {
			t.Fatal("revision committed despite history failure")
		}
	})

	t.Run("attention open and notification create", func(t *testing.T) {
		f := openFixture(t)
		now := time.Now().UTC()
		item := f.workItem(t)
		f.monitor(t, item, tracking.MonitorCheckpoint, now.Add(-time.Hour))
		beforeRevisions := count(t, f.db, "SELECT COUNT(*) FROM revisions")
		failEvent(t, f.db, history.EventAttentionOpened)
		if _, err := f.attention.Reconcile(f.ctx, now); err == nil {
			t.Fatal("expected history failure")
		}
		if count(t, f.db, "SELECT COUNT(*) FROM attention_issues") != 0 || count(t, f.db, "SELECT COUNT(*) FROM notifications") != 0 {
			t.Fatal("attention mutation committed despite history failure")
		}
		if count(t, f.db, "SELECT COUNT(*) FROM revisions") != beforeRevisions {
			t.Fatal("revision committed despite history failure")
		}
	})

	t.Run("notification dispatch", func(t *testing.T) {
		f := openFixture(t)
		now := time.Now().UTC()
		item := f.workItem(t)
		f.monitor(t, item, tracking.MonitorCheckpoint, now.Add(-time.Hour))
		if _, err := f.attention.Reconcile(f.ctx, now); err != nil {
			t.Fatal(err)
		}
		notifications, _ := f.attention.ListNotificationViews(f.ctx)
		beforeRevisions := count(t, f.db, "SELECT COUNT(*) FROM revisions")
		failEvent(t, f.db, history.EventNotificationSent)
		if _, err := f.attention.DispatchPendingNotifications(f.ctx, now.Add(time.Minute)); err == nil {
			t.Fatal("expected history failure")
		}
		assertScalar(t, f.db, "SELECT state FROM notifications WHERE entity_id = ?", notifications[0].Notification.ID, "PENDING")
		if count(t, f.db, "SELECT COUNT(*) FROM revisions") != beforeRevisions {
			t.Fatal("revision committed despite history failure")
		}
	})

	t.Run("attention snooze", func(t *testing.T) {
		f := openFixture(t)
		now := time.Now().UTC()
		item := f.workItem(t)
		f.monitor(t, item, tracking.MonitorCheckpoint, now.Add(-time.Hour))
		if _, err := f.attention.Reconcile(f.ctx, now); err != nil {
			t.Fatal(err)
		}
		issues, _ := f.attention.ListAttentionIssues(f.ctx)
		beforeRevisions := count(t, f.db, "SELECT COUNT(*) FROM revisions")
		failEvent(t, f.db, history.EventAttentionSnoozed)
		if _, err := f.attention.SnoozeAttentionIssue(f.ctx, attention.SnoozeInput{ID: issues[0].ID, Until: time.Now().Add(time.Hour)}); err == nil {
			t.Fatal("expected history failure")
		}
		var snoozed *string
		if err := f.db.QueryRow("SELECT snoozed_until FROM attention_issues WHERE entity_id = ?", issues[0].ID).Scan(&snoozed); err != nil {
			t.Fatal(err)
		}
		if snoozed != nil {
			t.Fatal("snooze committed despite history failure")
		}
		if count(t, f.db, "SELECT COUNT(*) FROM revisions") != beforeRevisions {
			t.Fatal("revision committed despite history failure")
		}
	})
}

func TestHistoryIsImmutableAndRejectsNonObjectSnapshots(t *testing.T) {
	f := openFixture(t)
	item := f.workItem(t)
	point := f.monitor(t, item, tracking.MonitorCheckpoint, time.Now().Add(time.Hour))
	events, err := history.NewRepository(f.db).ListByCorrelation(f.ctx, history.MonitorCorrelation(point.ID), 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%d err=%v", len(events), err)
	}
	if _, err := f.db.Exec("UPDATE history_events SET summary = 'rewritten' WHERE id = ?", events[0].ID); err == nil {
		t.Fatal("history update must fail")
	}
	if _, err := f.db.Exec("DELETE FROM history_events WHERE id = ?", events[0].ID); err == nil {
		t.Fatal("history delete must fail")
	}
	if _, err := history.Snapshot([]string{"not", "an", "object"}); err == nil {
		t.Fatal("array snapshot must fail")
	}
	if id, err := uuid.Parse(events[0].ID); err != nil || id.Version() != 7 {
		t.Fatalf("event ID is not UUIDv7: %s", events[0].ID)
	}
}

func TestHistorySurvivesDatabaseRestart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	opened, err := database.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	f := services(opened.DB)
	now := time.Now().UTC()
	item := f.workItem(t)
	point := f.monitor(t, item, tracking.MonitorCheckpoint, now.Add(-time.Hour))
	now = time.Now().UTC()
	if _, err := f.attention.Reconcile(f.ctx, now); err != nil {
		t.Fatal(err)
	}
	if _, err := f.attention.DispatchPendingNotifications(f.ctx, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	before, err := history.NewRepository(opened.DB).ListByCorrelation(f.ctx, history.MonitorCorrelation(point.ID), 20)
	if err != nil {
		t.Fatal(err)
	}
	if err := opened.DB.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := database.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.DB.Close()
	after, err := history.NewRepository(reopened.DB).ListByCorrelation(context.Background(), history.MonitorCorrelation(point.ID), 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) || len(after) != 4 {
		t.Fatalf("restart lost history: before=%d after=%d", len(before), len(after))
	}
	byType := map[history.EventType]history.Event{}
	for _, event := range after {
		byType[event.Type] = event
	}
	if byType[history.EventNotificationCreated].CausationEventID == nil || byType[history.EventNotificationSent].CausationEventID == nil {
		t.Fatal("restart lost causation")
	}
	sentEvent := byType[history.EventNotificationSent]
	if notification, err := attention.NewService(reopened.DB, tracking.NewService(reopened.DB, workitem.NewService(reopened.DB, workitem.NewRepository(reopened.DB)))).GetNotification(context.Background(), *sentEvent.SubjectEntityID); err != nil || notification.State != attention.DeliverySent {
		t.Fatalf("current truth after restart: notification=%+v err=%v", notification, err)
	}
}

func failEvent(t *testing.T, db *sql.DB, eventType history.EventType) {
	t.Helper()
	statement := fmt.Sprintf(`CREATE TRIGGER fail_history_insert BEFORE INSERT ON history_events WHEN NEW.event_type = '%s' BEGIN SELECT RAISE(ABORT, 'injected history failure'); END`, eventType)
	if _, err := db.Exec(statement); err != nil {
		t.Fatal(err)
	}
}

func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var value int
	if err := db.QueryRow(query, args...).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}

func assertScalar(t *testing.T, db *sql.DB, query string, arg any, want string) {
	t.Helper()
	var got string
	if err := db.QueryRow(query, arg).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func assertVersionedObject(t *testing.T, raw string) {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal([]byte(raw), &object); err != nil {
		t.Fatal(err)
	}
	if object["version"] != float64(1) {
		t.Fatalf("snapshot is not version 1: %s", raw)
	}
}
