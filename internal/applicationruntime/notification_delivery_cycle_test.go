package applicationruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"lum-harbor-notifier/internal/background"
	"lum-harbor-notifier/internal/notificationdelivery"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/testsupport"
)

type recordingDeliveryCycle struct {
	calls  int
	err    error
	result notificationdelivery.CycleResult
}

func (c *recordingDeliveryCycle) RunDeliveryCycle(context.Context, time.Time) (notificationdelivery.CycleResult, error) {
	c.calls++
	return c.result, c.err
}

func TestNotificationDeliveryCycleJobPropagatesCycleError(t *testing.T) {
	cycle := &recordingDeliveryCycle{err: errors.New("boom")}
	job := notificationDeliveryCycleJob{cycle: cycle, clock: fixedClock(time.Now())}
	if err := job.Run(context.Background()); err == nil {
		t.Fatal("expected the job to propagate the cycle's error")
	}
	if cycle.calls != 1 {
		t.Fatalf("expected exactly one cycle call, got %d", cycle.calls)
	}
}

func TestNotificationDeliveryCycleJobSucceedsWithNoCandidates(t *testing.T) {
	cycle := &recordingDeliveryCycle{}
	job := notificationDeliveryCycleJob{cycle: cycle, clock: fixedClock(time.Now())}
	if err := job.Run(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNotificationDeliveryCycleRegistrationValidation(t *testing.T) {
	scheduler := background.NewScheduler()
	if _, err := registerNotificationDeliveryCycle(nil, &recordingDeliveryCycle{}, time.Second, time.Second, fixedClock(time.Now())); err == nil {
		t.Fatal("expected a nil scheduler to be rejected")
	}
	if _, err := registerNotificationDeliveryCycle(scheduler, nil, time.Second, time.Second, fixedClock(time.Now())); err == nil {
		t.Fatal("expected a nil cycle to be rejected")
	}
}

func TestNotificationDeliveryCycleJobIDIsDistinctFromAttentionCycle(t *testing.T) {
	if NotificationDeliveryCycleJobID == AttentionCycleJobID {
		t.Fatal("the delivery cycle must be a separate Scheduler job from the attention cycle")
	}
}

// fakeDeliverer is a fake/test capability implementation satisfying
// notificationdelivery.Deliverer directly - no Plugin subprocess, exactly
// like recordingCycle satisfies AttentionCycle directly elsewhere in this
// package's own tests.
type fakeDeliverer struct {
	mu    sync.Mutex
	calls []notificationdelivery.DeliveryRequest
}

func (f *fakeDeliverer) Deliver(_ context.Context, request notificationdelivery.DeliveryRequest) (notificationdelivery.DeliveryResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, request)
	return notificationdelivery.DeliveryResult{}, nil
}

func (f *fakeDeliverer) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

// TestRealSchedulerRunsBothCyclesIndependentlyAndDeliversRealNotification
// proves the production composition shape end to end (short of a real
// Plugin): the real attention cycle produces a real SENT Notification from
// a real WorkItem/MonitorPoint, and the separately-registered delivery
// cycle picks it up and durably records success - with both jobs sharing
// one real Scheduler and neither blocking the other.
func TestRealSchedulerRunsBothCyclesIndependentlyAndDeliversRealNotification(t *testing.T) {
	db := testsupport.OpenTestDatabase(t)
	s := newServices(db.DB)
	wi := s.overdueWorkItem(t, "Ship it")

	scheduler := background.NewScheduler()
	if _, err := registerAttentionCycle(scheduler, s.attention, 10*time.Millisecond, 5*time.Second, fixedClock(cycleNow)); err != nil {
		t.Fatal(err)
	}
	fake := &fakeDeliverer{}
	deliveryService := notificationdelivery.NewService(db.DB, fake)
	if _, err := registerNotificationDeliveryCycle(scheduler, deliveryService, 10*time.Millisecond, 5*time.Second, fixedClock(cycleNow)); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { shutdown(t, scheduler) })

	var notificationID string
	waitFor(t, "the attention cycle to produce a real SENT notification and the delivery cycle to deliver it", func() bool {
		row := db.DB.QueryRow(`SELECT n.entity_id FROM notifications n INNER JOIN attention_issues a ON a.entity_id = n.attention_issue_id INNER JOIN monitor_points mp ON mp.entity_id = a.monitor_point_id WHERE mp.work_item_id = ? AND n.state = 'SENT'`, wi.ID)
		if err := row.Scan(&notificationID); err != nil {
			return false
		}
		var deliveredAt sql.NullString
		_ = db.DB.QueryRow(`SELECT delivered_at FROM notification_external_deliveries WHERE notification_id = ?`, notificationID).Scan(&deliveredAt)
		return deliveredAt.Valid
	})

	if fake.callCount() != 1 {
		t.Fatalf("expected exactly one real delivery call, got %d", fake.callCount())
	}
	if fake.calls[0].DeliveryID != notificationID {
		t.Fatalf("expected the DeliveryID to be the real Notification's stable identity, got %q want %q", fake.calls[0].DeliveryID, notificationID)
	}
}

type fakePluginInvoker struct{}

func (f *fakePluginInvoker) Invoke(ctx context.Context, inv plugin.Invocation) (json.RawMessage, error) {
	return json.RawMessage("{}"), nil
}

type readinessFakeInvoker struct {
	input json.RawMessage
}

func (f *readinessFakeInvoker) Invoke(_ context.Context, invocation plugin.Invocation) (json.RawMessage, error) {
	f.input = append(json.RawMessage(nil), invocation.Input...)
	return json.RawMessage(`{"authorizationStatus":"DENIED"}`), nil
}

func TestCheckProductionNotificationDeliveryReadinessUsesSelectedInstanceOnly(t *testing.T) {
	invoker := &readinessFakeInvoker{}
	instances := []plugin.Instance{
		{ID: "source", ExtensionID: "source", CapabilityID: plugin.CapabilitySourceFetch},
		{ID: "native", ExtensionID: "lh-plugin-macos-notifications", CapabilityID: plugin.CapabilityNotificationDeliver},
	}
	result, err := CheckProductionNotificationDeliveryReadiness(context.Background(), instances, invoker)
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.AuthorizationStatus != plugin.NotificationAuthorizationDenied {
		t.Fatalf("unexpected readiness result: %+v", result)
	}
	if !strings.Contains(string(invoker.input), `"operation":"CHECK_READINESS"`) {
		t.Fatalf("unexpected readiness payload: %s", invoker.input)
	}

	result, err = CheckProductionNotificationDeliveryReadiness(context.Background(), instances[:1], invoker)
	if err != nil || result != nil {
		t.Fatalf("no selected delivery instance should be a no-op: result=%+v err=%v", result, err)
	}
}

func TestRegisterProductionNotificationDeliveryConditional(t *testing.T) {
	db := testsupport.OpenTestDatabase(t)
	scheduler := background.NewScheduler()
	invoker := &fakePluginInvoker{}

	// Case 1: No delivery instance in slice -> returns nil, nil
	unrelatedInstances := []plugin.Instance{
		{ID: "src-1", ExtensionID: "ext-1", CapabilityID: plugin.CapabilitySourceFetch},
	}
	def, err := RegisterProductionNotificationDelivery(scheduler, unrelatedInstances, invoker, db.DB, time.Minute, 30*time.Second)
	if err != nil {
		t.Fatalf("unexpected error when no delivery instance present: %v", err)
	}
	if def != nil {
		t.Fatalf("expected nil definition when no delivery instance present, got %v", def)
	}

	// Case 2: Delivery instance present -> registers job
	deliveryInstances := []plugin.Instance{
		{ID: "deliv-1", ExtensionID: "lh-plugin-macos-notifications", CapabilityID: plugin.CapabilityNotificationDeliver},
	}
	def, err = RegisterProductionNotificationDelivery(scheduler, deliveryInstances, invoker, db.DB, time.Minute, 30*time.Second)
	if err != nil {
		t.Fatalf("unexpected error when delivery instance present: %v", err)
	}
	if def == nil {
		t.Fatal("expected non-nil definition when delivery instance present")
	}
	if def.ID != NotificationDeliveryCycleJobID {
		t.Fatalf("expected Job ID %q, got %q", NotificationDeliveryCycleJobID, def.ID)
	}

	// Case 3: Validation errors
	if _, err := RegisterProductionNotificationDelivery(nil, deliveryInstances, invoker, db.DB, time.Minute, 30*time.Second); err == nil {
		t.Fatal("expected error for nil scheduler")
	}
	if _, err := RegisterProductionNotificationDelivery(scheduler, deliveryInstances, nil, db.DB, time.Minute, 30*time.Second); err == nil {
		t.Fatal("expected error for nil invoker")
	}
	if _, err := RegisterProductionNotificationDelivery(scheduler, deliveryInstances, invoker, nil, time.Minute, 30*time.Second); err == nil {
		t.Fatal("expected error for nil db")
	}
}

func TestRegisterProductionNotificationDeliveryRejectsAmbiguousSelection(t *testing.T) {
	db := testsupport.OpenTestDatabase(t)
	scheduler := background.NewScheduler()
	instances := []plugin.Instance{
		{ID: "delivery-primary", ExtensionID: "delivery-one", CapabilityID: plugin.CapabilityNotificationDeliver},
		{ID: "delivery-secondary", ExtensionID: "delivery-two", CapabilityID: plugin.CapabilityNotificationDeliver},
	}

	definition, err := RegisterProductionNotificationDelivery(
		scheduler,
		instances,
		&fakePluginInvoker{},
		db.DB,
		time.Minute,
		30*time.Second,
	)
	if err == nil {
		t.Fatal("expected multiple enabled delivery instances to be rejected")
	}
	if definition != nil {
		t.Fatalf("ambiguous delivery selection must not register a job: %+v", definition)
	}
	if !strings.Contains(err.Error(), "multiple enabled") ||
		!strings.Contains(err.Error(), "delivery-primary") ||
		!strings.Contains(err.Error(), "delivery-secondary") {
		t.Fatalf("unexpected ambiguity error: %v", err)
	}
	if _, snapshotErr := scheduler.Snapshot(NotificationDeliveryCycleJobID); snapshotErr == nil {
		t.Fatal("ambiguous delivery selection registered a caller despite the error")
	}
}
