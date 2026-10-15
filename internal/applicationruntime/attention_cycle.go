package applicationruntime

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/background"
)

// AttentionCycleJobID is the static Scheduler identity of the M11 cycle.
const AttentionCycleJobID = "attention.notification.cycle"

// AttentionCycle is the narrow deterministic capability the cycle drives.
type AttentionCycle interface {
	Reconcile(ctx context.Context, now time.Time) (attention.ReconcileResult, error)
	DispatchPendingNotifications(ctx context.Context, now time.Time) (attention.DispatchResult, error)
}

type attentionCycleJob struct {
	cycle AttentionCycle
	clock func() time.Time
}

// Run performs one deterministic cycle: first align Attention with M10 truth
// (Reconcile), then — only if that succeeded — deliver PENDING notifications
// (Dispatch). Reconcile-before-Dispatch guarantees an obsolete pending
// communication is cancelled instead of sent. Each step owns its own
// transaction; a Dispatch failure leaves committed Reconcile results and the
// PENDING rows for the next cycle. Errors are returned unchanged so the
// Scheduler records them (attempt, failure, consecutive failures).
func (j attentionCycleJob) Run(ctx context.Context) error {
	now := j.clock().UTC()
	reconciled, err := j.cycle.Reconcile(ctx, now)
	if err != nil {
		return fmt.Errorf("attention reconcile: %w", err)
	}
	dispatched, err := j.cycle.DispatchPendingNotifications(ctx, now)
	if err != nil {
		return fmt.Errorf("notification dispatch: %w", err)
	}
	if reconciled.Opened+reconciled.Resolved+dispatched.Sent+dispatched.Cancelled > 0 {
		slog.Info("attention notification cycle",
			"opened", reconciled.Opened, "resolved", reconciled.Resolved,
			"sent", dispatched.Sent, "cancelled", dispatched.Cancelled, "deferred", dispatched.Deferred)
	}
	return nil
}

// RegisterAttentionCycle statically registers the single M11 job with the
// existing Scheduler (before Start, alongside source workloads). It uses the
// Scheduler's fixed-delay interval; no new timer framework exists.
func RegisterAttentionCycle(scheduler *background.Scheduler, cycle AttentionCycle, interval, timeout time.Duration) (background.JobDefinition, error) {
	return registerAttentionCycle(scheduler, cycle, interval, timeout, time.Now)
}

func registerAttentionCycle(scheduler *background.Scheduler, cycle AttentionCycle, interval, timeout time.Duration, clock func() time.Time) (background.JobDefinition, error) {
	if scheduler == nil {
		return background.JobDefinition{}, fmt.Errorf("register attention cycle: scheduler must not be nil")
	}
	if cycle == nil {
		return background.JobDefinition{}, fmt.Errorf("register attention cycle: cycle must not be nil")
	}
	definition := background.JobDefinition{
		ID: AttentionCycleJobID, Interval: interval, Timeout: timeout, Enabled: true, OverlapPolicy: background.OverlapPolicySkip,
	}
	if err := scheduler.Register(definition, attentionCycleJob{cycle: cycle, clock: clock}); err != nil {
		return background.JobDefinition{}, fmt.Errorf("register attention cycle: %w", err)
	}
	return definition, nil
}
