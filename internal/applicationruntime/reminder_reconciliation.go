package applicationruntime

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"lum-harbor-notifier/internal/background"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/plannersync"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/source"
)

// ReminderReconciliationJobID is the static Scheduler identity of the M13
// Slice 4A cycle, named the same way AttentionCycleJobID is.
const ReminderReconciliationJobID = "reminder.reconciliation.cycle"

type reminderReconciliationJob struct {
	reconciler *plannersync.Reconciler
}

// Run performs one deterministic reconciliation pass over every current
// REMINDER SourceRecord produced by the real Reminders Plugin. A per-record
// failure (malformed payload, authority conflict, etc.) is aggregated into
// the returned error so Scheduler records it as a failed attempt, but never
// rolls back the records that already committed inside Reconcile — the same
// per-record isolation Slice 2B already proved. Only bounded counts and
// stable external IDs are logged, never Reminder title/notes/payload.
func (j reminderReconciliationJob) Run(ctx context.Context) error {
	result, err := j.reconciler.Reconcile(ctx)
	if err != nil {
		return fmt.Errorf("reminder reconciliation: %w", err)
	}
	if result.Scanned > 0 {
		slog.Info("reminder reconciliation cycle",
			"scanned", result.Scanned, "created", result.Created, "updated", result.Updated,
			"unchanged", result.Unchanged, "failed", result.Failed)
	}
	if result.Failed == 0 {
		return nil
	}
	for _, record := range result.Records {
		if record.Outcome == plannersync.OutcomeFailed {
			slog.Warn("reminder reconciliation record failed", "externalId", record.ExternalID, "error", record.Error)
		}
	}
	return fmt.Errorf("reminder reconciliation: %d of %d record(s) failed", result.Failed, result.Scanned)
}

// RegisterReminderReconciliation statically registers exactly one Reminder
// reconciliation Job with the existing Scheduler — never one Job per
// Instance — when at least one resolved Instance selects the real
// `reminders-eventkit-bridge` Plugin's source.fetch capability. Multiple such
// Instances still produce exactly one Job, because plannersync.Reconciler
// already scans every current REMINDER head across all Instances of that one
// producer extension in a single Reconcile call. Registration is a pure
// composition-layer decision: internal/plannersync itself never imports
// internal/background or knows Scheduler exists.
//
// It returns a nil definition (and nil error) when no such Instance is
// currently selected/resolved — an unrelated source.fetch Plugin, or a
// disabled/unresolved Reminders declaration, registers nothing, matching
// plugininstance.Resolve's own existing enablement semantics rather than
// reimplementing them here.
func RegisterReminderReconciliation(
	scheduler *background.Scheduler,
	instances []plugin.Instance,
	sourceService *source.Service,
	plannerService *planner.Service,
	interval, timeout time.Duration,
) (*background.JobDefinition, error) {
	var matchedExtensionID string
	for _, instance := range instances {
		if (instance.ExtensionID == plannersync.ReminderExtensionID || instance.ExtensionID == plannersync.LegacyReminderExtensionID) && instance.CapabilityID == plugin.CapabilitySourceFetch {
			matchedExtensionID = instance.ExtensionID
			break
		}
	}
	if matchedExtensionID == "" {
		return nil, nil
	}
	if scheduler == nil {
		return nil, fmt.Errorf("register reminder reconciliation: scheduler must not be nil")
	}

	reconciler, err := plannersync.NewReconciler(sourceService, plannerService, matchedExtensionID)
	if err != nil {
		return nil, fmt.Errorf("register reminder reconciliation: %w", err)
	}
	definition := background.JobDefinition{
		ID: ReminderReconciliationJobID, Interval: interval, Timeout: timeout, Enabled: true, OverlapPolicy: background.OverlapPolicySkip,
	}
	if err := scheduler.Register(definition, reminderReconciliationJob{reconciler: reconciler}); err != nil {
		return nil, fmt.Errorf("register reminder reconciliation: %w", err)
	}
	return &definition, nil
}
