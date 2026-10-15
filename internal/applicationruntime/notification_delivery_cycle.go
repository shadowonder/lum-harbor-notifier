package applicationruntime

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"lum-harbor-notifier/internal/background"
	dbgen "lum-harbor-notifier/internal/database/generated"
	"lum-harbor-notifier/internal/notificationdelivery"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/deliveryadapter"
)

// NotificationDeliveryCycleJobID is the static Scheduler identity of the M17
// Slice 1 external delivery cycle. It is deliberately separate from
// AttentionCycleJobID: the attention cycle is pure-DB, fast, and
// deterministic (Reconcile/Dispatch are plain SQL transactions with no
// external I/O), and coupling a Plugin RPC round-trip to that same cadence
// would risk delaying unrelated Attention/Notification reconciliation if an
// external delivery capability is slow or unhealthy.
const NotificationDeliveryCycleJobID = "notification.delivery.cycle"

// NotificationDeliveryCycle is the narrow deterministic capability the
// cycle drives. *notificationdelivery.Service satisfies it directly.
type NotificationDeliveryCycle interface {
	RunDeliveryCycle(ctx context.Context, now time.Time) (notificationdelivery.CycleResult, error)
}

type notificationDeliveryCycleJob struct {
	cycle NotificationDeliveryCycle
	clock func() time.Time
}

// Run attempts external delivery for every currently-eligible SENT
// Notification. A per-candidate failure is aggregated into the returned
// error (Scheduler records attempt/failure/consecutive-failure metadata),
// mirroring reminderReconciliationJob's own per-record failure isolation:
// candidates that already succeeded in this same run keep their durable
// success marker regardless of this job's overall return value.
func (j notificationDeliveryCycleJob) Run(ctx context.Context) error {
	now := j.clock().UTC()
	result, err := j.cycle.RunDeliveryCycle(ctx, now)
	if result.Attempted > 0 {
		slog.Info("notification delivery cycle",
			"attempted", result.Attempted, "delivered", result.Delivered, "failed", result.Failed)
	}
	if err != nil {
		return fmt.Errorf("notification delivery cycle: %w", err)
	}
	return nil
}

// RegisterNotificationDeliveryCycle statically registers the M17 Slice 1
// job with the existing Scheduler (before Start, alongside source and
// attention workloads). Production conditional registration - only when a
// real external delivery Plugin Instance is selected, mirroring
// RegisterReminderReconciliation's own selected-Instance pattern - is a
// Slice 2 concern; this function only establishes the job itself.
func RegisterNotificationDeliveryCycle(scheduler *background.Scheduler, cycle NotificationDeliveryCycle, interval, timeout time.Duration) (background.JobDefinition, error) {
	return registerNotificationDeliveryCycle(scheduler, cycle, interval, timeout, time.Now)
}

func registerNotificationDeliveryCycle(scheduler *background.Scheduler, cycle NotificationDeliveryCycle, interval, timeout time.Duration, clock func() time.Time) (background.JobDefinition, error) {
	if scheduler == nil {
		return background.JobDefinition{}, fmt.Errorf("register notification delivery cycle: scheduler must not be nil")
	}
	if cycle == nil {
		return background.JobDefinition{}, fmt.Errorf("register notification delivery cycle: cycle must not be nil")
	}
	definition := background.JobDefinition{
		ID: NotificationDeliveryCycleJobID, Interval: interval, Timeout: timeout, Enabled: true, OverlapPolicy: background.OverlapPolicySkip,
	}
	if err := scheduler.Register(definition, notificationDeliveryCycleJob{cycle: cycle, clock: clock}); err != nil {
		return background.JobDefinition{}, fmt.Errorf("register notification delivery cycle: %w", err)
	}
	return definition, nil
}

// RegisterProductionNotificationDelivery conditionally registers the
// NotificationDeliveryCycle Job with the Scheduler when a resolved Instance
// selects the notification.deliver capability. It mirrors
// RegisterReminderReconciliation's selected-Instance precedent:
//   - If no delivery Instance is selected: returns nil, nil (no job registered, no error).
//   - If a delivery Instance is selected: constructs the deliveryadapter and
//     notificationdelivery.Service, registers the JobDefinition, and returns &definition.
//   - If more than one delivery Instance is selected: returns an explicit
//     configuration error rather than silently choosing one external side-
//     effect owner.
func RegisterProductionNotificationDelivery(
	scheduler *background.Scheduler,
	instances []plugin.Instance,
	invoker plugin.Invoker,
	db dbgen.DBTX,
	interval, timeout time.Duration,
) (*background.JobDefinition, error) {
	deliveryInstance, err := selectedNotificationDeliveryInstance(instances)
	if err != nil {
		return nil, fmt.Errorf("register notification delivery: %w", err)
	}
	if deliveryInstance == nil {
		return nil, nil
	}
	if scheduler == nil {
		return nil, fmt.Errorf("register notification delivery: scheduler must not be nil")
	}
	if invoker == nil {
		return nil, fmt.Errorf("register notification delivery: invoker must not be nil")
	}
	if db == nil {
		return nil, fmt.Errorf("register notification delivery: db must not be nil")
	}

	adapter, err := deliveryadapter.New(invoker, *deliveryInstance)
	if err != nil {
		return nil, fmt.Errorf("register notification delivery: construct adapter: %w", err)
	}

	service := notificationdelivery.NewService(db, adapter)
	def, err := RegisterNotificationDeliveryCycle(scheduler, service, interval, timeout)
	if err != nil {
		return nil, err
	}
	return &def, nil
}

// CheckProductionNotificationDeliveryReadiness probes the one selected
// notification.deliver instance after the Host has started it. The operation
// is plugin-specific and presentation-only: it may resolve native permission,
// but it never creates a Notification or invokes delivery.
func CheckProductionNotificationDeliveryReadiness(
	ctx context.Context,
	instances []plugin.Instance,
	invoker plugin.Invoker,
) (*plugin.NotificationReadinessResult, error) {
	selected, err := selectedNotificationDeliveryInstance(instances)
	if err != nil {
		return nil, fmt.Errorf("check notification delivery readiness: %w", err)
	}
	if selected == nil {
		return nil, nil
	}
	if invoker == nil {
		return nil, fmt.Errorf("check notification delivery readiness: invoker must not be nil")
	}
	result, err := plugin.NewNotificationDeliverer(invoker).CheckReadiness(ctx, *selected)
	if err != nil {
		return nil, fmt.Errorf("check notification delivery readiness for instance %q: %w", selected.ID, err)
	}
	return &result, nil
}

func selectedNotificationDeliveryInstance(instances []plugin.Instance) (*plugin.Instance, error) {
	var selected *plugin.Instance
	for _, instance := range instances {
		if instance.CapabilityID != plugin.CapabilityNotificationDeliver {
			continue
		}
		if selected != nil {
			return nil, fmt.Errorf("multiple enabled %q instances (%q and %q)", plugin.CapabilityNotificationDeliver, selected.ID, instance.ID)
		}
		copy := instance
		selected = &copy
	}
	return selected, nil
}
