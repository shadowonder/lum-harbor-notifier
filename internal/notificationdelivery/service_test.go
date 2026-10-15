package notificationdelivery_test

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"

	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/database"
	"lum-harbor-notifier/internal/notificationdelivery"
	"lum-harbor-notifier/internal/testsupport"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

var (
	monitorDue = time.Date(2026, 10, 15, 22, 0, 0, 0, time.UTC)
	cycleNow   = monitorDue.Add(2 * time.Hour)
)

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

// realSentNotification produces a real, production-composed SENT
// Notification: a real WorkItem, a real overdue MonitorPoint, then the real
// Attention Reconcile -> DispatchPendingNotifications sequence - exactly the
// chain internal/attention's own cycle tests use, never raw SQL.
func (s services) realSentNotification(t *testing.T, title string) (notificationID, workItemID string) {
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
	if _, err := s.attention.Reconcile(ctx, cycleNow); err != nil {
		t.Fatal(err)
	}
	if _, err := s.attention.DispatchPendingNotifications(ctx, cycleNow); err != nil {
		t.Fatal(err)
	}
	var id string
	row := s.db.QueryRow(`SELECT n.entity_id FROM notifications n INNER JOIN attention_issues a ON a.entity_id = n.attention_issue_id INNER JOIN monitor_points mp ON mp.entity_id = a.monitor_point_id INNER JOIN work_items w ON w.entity_id = mp.work_item_id WHERE w.entity_id = ? AND n.state = 'SENT'`, wi.ID)
	if err := row.Scan(&id); err != nil {
		t.Fatalf("expected a real SENT notification to exist: %v", err)
	}
	return id, wi.ID
}

func notificationState(t *testing.T, db *sql.DB, notificationID string) (state string, sentAt sql.NullString) {
	t.Helper()
	row := db.QueryRow(`SELECT state, sent_at FROM notifications WHERE entity_id = ?`, notificationID)
	if err := row.Scan(&state, &sentAt); err != nil {
		t.Fatal(err)
	}
	return state, sentAt
}

func attentionIssueState(t *testing.T, db *sql.DB, notificationID string) string {
	t.Helper()
	var state string
	row := db.QueryRow(`SELECT a.state FROM attention_issues a INNER JOIN notifications n ON n.attention_issue_id = a.entity_id WHERE n.entity_id = ?`, notificationID)
	if err := row.Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

func hasDeliveryMarker(t *testing.T, db *sql.DB, notificationID string) bool {
	t.Helper()
	var deliveredAt string
	err := db.QueryRow(`SELECT delivered_at FROM notification_external_deliveries WHERE notification_id = ?`, notificationID).Scan(&deliveredAt)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		t.Fatal(err)
	}
	return true
}

// fakeDeliverer is a fake/test Plugin-equivalent capability implementation.
// It never imports internal/plugin - it satisfies notificationdelivery.Deliverer
// directly, exactly like recordingCycle in applicationruntime's own tests
// satisfies AttentionCycle directly.
type fakeDeliverer struct {
	mu      sync.Mutex
	calls   []notificationdelivery.DeliveryRequest
	failFor map[string]bool
}

func (f *fakeDeliverer) Deliver(_ context.Context, request notificationdelivery.DeliveryRequest) (notificationdelivery.DeliveryResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, request)
	if f.failFor[request.DeliveryID] {
		return notificationdelivery.DeliveryResult{}, errors.New("fixture: delivery capability unavailable")
	}
	return notificationdelivery.DeliveryResult{}, nil
}

func (f *fakeDeliverer) callCountFor(deliveryID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	count := 0
	for _, call := range f.calls {
		if call.DeliveryID == deliveryID {
			count++
		}
	}
	return count
}

// failOnceExecDBTX wraps a real dbgen.DBTX and fails the next N ExecContext
// calls, simulating the crash window between a confirmed successful
// external side effect and the local durable commit that would record it.
type failOnceExecDBTX struct {
	inner interface {
		ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
		PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
		QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
		QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	}
	remaining int
}

func (f *failOnceExecDBTX) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if f.remaining > 0 {
		f.remaining--
		return nil, errors.New("fixture: simulated crash before the durable marker commits")
	}
	return f.inner.ExecContext(ctx, query, args...)
}
func (f *failOnceExecDBTX) PrepareContext(ctx context.Context, query string) (*sql.Stmt, error) {
	return f.inner.PrepareContext(ctx, query)
}
func (f *failOnceExecDBTX) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return f.inner.QueryContext(ctx, query, args...)
}
func (f *failOnceExecDBTX) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return f.inner.QueryRowContext(ctx, query, args...)
}

func TestRunDeliveryCycleDeliversEligibleNotificationAndSkipsItNextTime(t *testing.T) {
	db := testsupport.OpenTestDatabase(t)
	s := newServices(db.DB)
	notificationID, workItemID := s.realSentNotification(t, "Ship it")

	fake := &fakeDeliverer{}
	svc := notificationdelivery.NewService(db.DB, fake)

	result, err := svc.RunDeliveryCycle(context.Background(), cycleNow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Attempted != 1 || result.Delivered != 1 || result.Failed != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !hasDeliveryMarker(t, db.DB, notificationID) {
		t.Fatal("expected a durable delivery-success marker")
	}
	if fake.callCountFor(notificationID) != 1 {
		t.Fatalf("expected exactly one Deliver call, got %d", fake.callCountFor(notificationID))
	}
	if len(fake.calls) != 1 || fake.calls[0].WorkItemID == nil || *fake.calls[0].WorkItemID != workItemID {
		t.Fatalf("expected delivery target WorkItem %q, got %+v", workItemID, fake.calls)
	}

	// Next cycle: the same Notification must not be attempted again.
	result, err = svc.RunDeliveryCycle(context.Background(), cycleNow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Attempted != 0 {
		t.Fatalf("expected an already-delivered Notification to be skipped, got %+v", result)
	}
	if fake.callCountFor(notificationID) != 1 {
		t.Fatal("expected no additional Deliver call for an already-delivered Notification")
	}
}

func TestRunDeliveryCyclePluginFailureLeavesCoreTruthUntouchedAndRetriesWithSameDeliveryID(t *testing.T) {
	db := testsupport.OpenTestDatabase(t)
	s := newServices(db.DB)
	notificationID, _ := s.realSentNotification(t, "Ship it")
	beforeState, beforeSentAt := notificationState(t, db.DB, notificationID)
	beforeIssueState := attentionIssueState(t, db.DB, notificationID)

	fake := &fakeDeliverer{failFor: map[string]bool{notificationID: true}}
	svc := notificationdelivery.NewService(db.DB, fake)

	result, err := svc.RunDeliveryCycle(context.Background(), cycleNow)
	if err == nil {
		t.Fatal("expected the cycle to report the delivery failure")
	}
	if result.Attempted != 1 || result.Delivered != 0 || result.Failed != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if hasDeliveryMarker(t, db.DB, notificationID) {
		t.Fatal("a failed delivery must never record a success marker")
	}
	afterState, afterSentAt := notificationState(t, db.DB, notificationID)
	if afterState != beforeState || afterSentAt != beforeSentAt {
		t.Fatalf("Notification truth must be untouched by delivery failure: before=(%s,%v) after=(%s,%v)", beforeState, beforeSentAt, afterState, afterSentAt)
	}
	if attentionIssueState(t, db.DB, notificationID) != beforeIssueState {
		t.Fatal("AttentionIssue truth must be untouched by delivery failure")
	}

	// Next cycle: the Plugin recovers. The retry must use the SAME DeliveryID.
	fake.failFor[notificationID] = false
	result, err = svc.RunDeliveryCycle(context.Background(), cycleNow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Delivered != 1 {
		t.Fatalf("expected the retry to succeed: %+v", result)
	}
	if fake.callCountFor(notificationID) != 2 {
		t.Fatalf("expected exactly 2 Deliver calls (fail then succeed), got %d", fake.callCountFor(notificationID))
	}
	if !hasDeliveryMarker(t, db.DB, notificationID) {
		t.Fatal("expected a durable delivery-success marker after the successful retry")
	}
}

// TestRunDeliveryCycleSurvivesCrashWindowBetweenPluginSuccessAndCoreMarkerWrite
// is the required crash-window test: it proves the boundary contract - not
// exactly-once external delivery from the database alone, but that a
// crash between a confirmed successful external side effect and the local
// durable commit is safe, because the retry uses the identical DeliveryID
// and Core still converges on exactly one durable success marker.
func TestRunDeliveryCycleSurvivesCrashWindowBetweenPluginSuccessAndCoreMarkerWrite(t *testing.T) {
	db := testsupport.OpenTestDatabase(t)
	s := newServices(db.DB)
	notificationID, _ := s.realSentNotification(t, "Ship it")
	fake := &fakeDeliverer{}

	// Attempt 1: the fake Plugin (standing in for the real macOS side
	// effect) succeeds, but the wrapped DBTX fails the marker INSERT -
	// simulating a crash/commit failure strictly after the external side
	// effect already happened.
	faulty := &failOnceExecDBTX{inner: db.DB, remaining: 1}
	crashingSvc := notificationdelivery.NewService(faulty, fake)
	result, err := crashingSvc.RunDeliveryCycle(context.Background(), cycleNow)
	if err == nil {
		t.Fatal("expected the simulated marker-write crash to surface as a failure")
	}
	if result.Delivered != 0 || result.Failed != 1 {
		t.Fatalf("unexpected result during the simulated crash: %+v", result)
	}
	if hasDeliveryMarker(t, db.DB, notificationID) {
		t.Fatal("no marker should exist after the simulated crash")
	}
	if fake.callCountFor(notificationID) != 1 {
		t.Fatalf("expected exactly one real external side effect so far, got %d", fake.callCountFor(notificationID))
	}

	// Attempt 2: a reconstructed Service (the next job run / restart) finds
	// the same candidate still eligible and retries with the SAME
	// DeliveryID.
	recoveredSvc := notificationdelivery.NewService(db.DB, fake)
	result, err = recoveredSvc.RunDeliveryCycle(context.Background(), cycleNow)
	if err != nil {
		t.Fatalf("unexpected error on retry: %v", err)
	}
	if result.Delivered != 1 {
		t.Fatalf("expected the retry to succeed and record the marker: %+v", result)
	}
	if fake.callCountFor(notificationID) != 2 {
		t.Fatalf("expected the retry to reuse the SAME DeliveryID (2 total calls), got %d", fake.callCountFor(notificationID))
	}
	for _, call := range fake.calls {
		if call.DeliveryID != notificationID {
			t.Fatalf("every call must use the stable DeliveryID %q, got %q", notificationID, call.DeliveryID)
		}
	}
	if !hasDeliveryMarker(t, db.DB, notificationID) {
		t.Fatal("expected a durable delivery-success marker after recovery")
	}

	// Attempt 3: later runs must skip it - Core converges on exactly one
	// success marker despite the Plugin having been invoked twice.
	result, err = recoveredSvc.RunDeliveryCycle(context.Background(), cycleNow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Attempted != 0 {
		t.Fatalf("expected the delivered Notification to be skipped, got %+v", result)
	}
}

func TestRunDeliveryCycleIsolatesOneFailureFromOtherCandidates(t *testing.T) {
	db := testsupport.OpenTestDatabase(t)
	s := newServices(db.DB)
	failingID, _ := s.realSentNotification(t, "Fails")
	succeedingID, _ := s.realSentNotification(t, "Succeeds")

	fake := &fakeDeliverer{failFor: map[string]bool{failingID: true}}
	svc := notificationdelivery.NewService(db.DB, fake)

	result, err := svc.RunDeliveryCycle(context.Background(), cycleNow)
	if err == nil {
		t.Fatal("expected the cycle to report the one failure")
	}
	if result.Attempted != 2 || result.Delivered != 1 || result.Failed != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if hasDeliveryMarker(t, db.DB, failingID) {
		t.Fatal("the failing candidate must not have a success marker")
	}
	if !hasDeliveryMarker(t, db.DB, succeedingID) {
		t.Fatal("the succeeding candidate must retain its success marker despite the other candidate's failure")
	}
}

func TestRunDeliveryCycleSkipsAlreadyDeliveredNotificationAfterRestart(t *testing.T) {
	db := testsupport.OpenTestDatabase(t)
	s := newServices(db.DB)
	notificationID, _ := s.realSentNotification(t, "Ship it")

	fake := &fakeDeliverer{}
	svc := notificationdelivery.NewService(db.DB, fake)
	if _, err := svc.RunDeliveryCycle(context.Background(), cycleNow); err != nil {
		t.Fatal(err)
	}
	if !hasDeliveryMarker(t, db.DB, notificationID) {
		t.Fatal("expected a durable delivery-success marker before restart")
	}
	if err := db.DB.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := database.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.DB.Close() })

	restartedFake := &fakeDeliverer{}
	restartedSvc := notificationdelivery.NewService(reopened.DB, restartedFake)
	result, err := restartedSvc.RunDeliveryCycle(context.Background(), cycleNow)
	if err != nil {
		t.Fatal(err)
	}
	if result.Attempted != 0 {
		t.Fatalf("expected the already-delivered Notification to survive restart and be skipped, got %+v", result)
	}
	if len(restartedFake.calls) != 0 {
		t.Fatal("expected no external delivery to be re-attempted after restart")
	}
}
