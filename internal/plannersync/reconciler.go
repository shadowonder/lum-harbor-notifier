package plannersync

import (
	"context"
	"fmt"
	"strings"

	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/source"
)

// SourceKindReminder is the real M13 structured source_kind produced by a
// future Reminders Plugin's source.fetch capability. Test fixtures may
// synthesize SourceRecords of this kind without a real Plugin, but the kind
// itself is the production contract, not a throwaway test kind.
const SourceKindReminder = "REMINDER"

// ReminderExtensionID is the standardized production `lh-plugin-apple-reminders`
// Plugin's manifest extension ID — the producer scope a production Reminder
// Reconciler is constructed for. Exported so the composition layer that decides
// whether to register a reconciliation Job never duplicates this literal.
const ReminderExtensionID = "lh-plugin-apple-reminders"

// LegacyReminderExtensionID is the pre-standardization extension ID for the Apple
// Reminders plugin, preserved for backwards compatibility.
const LegacyReminderExtensionID = "reminders-eventkit-bridge"

// RecordOutcome classifies what happened to one candidate SourceRecord
// during a Reconcile run.
type RecordOutcome string

const (
	OutcomeCreated   RecordOutcome = "CREATED"
	OutcomeUpdated   RecordOutcome = "UPDATED"
	OutcomeUnchanged RecordOutcome = "UNCHANGED"
	OutcomeFailed    RecordOutcome = "FAILED"
)

// RecordResult reports what happened to exactly one candidate SourceRecord.
type RecordResult struct {
	SourceRecordID string
	ExtensionID    string
	InstanceID     string
	ExternalID     string
	Outcome        RecordOutcome
	Error          error
}

// Result is the bounded outcome of one Reconcile call. Scanned counts only
// records that were in this reconciler's producer scope (matching extension
// and source_kind); Created+Updated+Unchanged+Failed always sums to Scanned.
type Result struct {
	Scanned   int
	Created   int
	Updated   int
	Unchanged int
	Failed    int
	Records   []RecordResult
}

// FullySuccessful reports whether every scanned candidate reconciled without
// error. A caller uses this to decide whether to surface a partial-failure
// warning; the successfully reconciled records are never rolled back because
// one other record failed.
func (r Result) FullySuccessful() bool {
	return r.Failed == 0
}

// Reconciler is the deterministic cross-domain application responsibility
// that turns durable REMINDER Source Layer evidence into authoritative
// EXTERNAL PlannerItems. It owns none of Planner, Foundation, History, or
// external_refs persistence itself — it only reads Source current heads,
// decodes/validates/maps the Reminder payload, and invokes Planner's single
// owned external-identity-reconciliation command (planner.Service.
// ReconcileExternalPlannerItem, M14 Slice 2). It never opens a transaction,
// never writes SQL, and — as of M14 Slice 2 — never resolves external
// identity itself; that decision (create vs. update, and create-race
// recovery) belongs entirely to Planner, the owner of the mutation it
// produces.
type Reconciler struct {
	source      *source.Service
	planner     *planner.Service
	extensionID string
}

// NewReconciler constructs a Reminder reconciler bound to one expected
// producer extension ID (the future real Reminders Plugin's extension
// identifier, or a fixed test extension ID before that Plugin exists). A
// SourceRecord whose ExtensionID does not match this value is out of scope
// and is never touched, even if its source_kind is REMINDER — this is the
// reconciler's explicit producer-scope boundary (a REMINDER-kind record from
// an unrelated/unexpected producer is not assumed to be an Apple Reminders
// EventKit payload).
func NewReconciler(
	sourceService *source.Service,
	plannerService *planner.Service,
	extensionID string,
) (*Reconciler, error) {
	if sourceService == nil || plannerService == nil {
		return nil, fmt.Errorf("reminder reconciler requires non-nil source and planner dependencies")
	}
	if strings.TrimSpace(extensionID) == "" {
		return nil, fmt.Errorf("reminder reconciler requires a non-empty expected producer extension ID")
	}
	return &Reconciler{
		source: sourceService, planner: plannerService,
		extensionID: extensionID,
	}, nil
}

// Reconcile scans every current REMINDER SourceRecord head in this
// reconciler's producer scope and deterministically creates or refreshes its
// EXTERNAL PlannerItem. It never deletes, archives, or completes a
// PlannerItem merely because a record is absent from any particular batch —
// it only ever reacts to Source Layer heads that are actually currently
// persisted (see the Source Layer contract's "no deletion/tombstone"
// invariant). A malformed or failing candidate is reported in Result and
// does not prevent any other candidate in the same call from committing.
func (rc *Reconciler) Reconcile(ctx context.Context) (Result, error) {
	heads, err := rc.source.ListCurrentHeadsBySourceKind(ctx, SourceKindReminder)
	if err != nil {
		return Result{}, fmt.Errorf("list current %s heads: %w", SourceKindReminder, err)
	}

	result := Result{}
	for _, head := range heads {
		if head.ExtensionID != rc.extensionID {
			continue
		}
		result.Scanned++

		record := RecordResult{
			SourceRecordID: head.RecordID, ExtensionID: head.ExtensionID,
			InstanceID: head.InstanceID, ExternalID: head.ExternalID,
		}
		outcome, err := rc.reconcileOne(ctx, head)
		record.Outcome = outcome
		if err != nil {
			record.Error = err
			result.Failed++
		} else {
			switch outcome {
			case OutcomeCreated:
				result.Created++
			case OutcomeUpdated:
				result.Updated++
			case OutcomeUnchanged:
				result.Unchanged++
			}
		}
		result.Records = append(result.Records, record)
	}
	return result, nil
}

// reconcileOne decodes and validates one candidate's Reminder payload, maps
// it to Planner-native terms, and hands the entire identity-resolution and
// create-vs-update decision to Planner's own owned seam (M14 Slice 2). This
// package no longer resolves external identity, decides create vs. sync, or
// recovers from a create race itself — Planner does, because Planner is the
// authority that owns the mutation and the external_refs binding it
// produces.
func (rc *Reconciler) reconcileOne(ctx context.Context, head source.CurrentSourceHead) (RecordOutcome, error) {
	native, err := decodeReminderPayload(head.Payload)
	if err != nil {
		return OutcomeFailed, fmt.Errorf("external id %q: %w", head.ExternalID, err)
	}

	sourceRevisionID := head.CurrentRevisionID
	result, err := rc.planner.ReconcileExternalPlannerItem(ctx, planner.ReconcileExternalPlannerItemInput{
		System: ReminderExternalSystem(head.InstanceID), ExternalID: head.ExternalID,
		Type: planner.ItemTypeReminder,
		Native: planner.ExternalPlannerItemNativeState{
			Title: native.Title, Details: native.Details,
			DueAt: native.DueAt, DueDate: native.DueDate,
			Status: native.Status, CompletedAt: native.CompletedAt,
		},
		SourceRevisionID: &sourceRevisionID,
	})
	if err != nil {
		return OutcomeFailed, fmt.Errorf("reconcile external planner item for %q: %w", head.ExternalID, err)
	}
	switch result.Outcome {
	case planner.ReconcileOutcomeCreated:
		return OutcomeCreated, nil
	case planner.ReconcileOutcomeUpdated:
		return OutcomeUpdated, nil
	default:
		return OutcomeUnchanged, nil
	}
}
