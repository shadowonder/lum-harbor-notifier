// Package attention owns M11 Attention & Notification durable truth.
//
// AttentionIssue (a persistent unresolved problem) and Notification (a durable
// communication) are distinct. Attention has only an OPEN/RESOLVED lifecycle;
// snooze is an orthogonal timestamp whose expiry needs no state rewrite.
// Notification read/dismiss are interaction timestamps, never Attention
// resolution. Attention is derived deterministically from Tracking truth; no AI
// participates. See docs/contracts/attention-notification-foundation-v1.md.
package attention

import (
	"errors"
	"time"
)

var (
	ErrInvalidInput        = errors.New("invalid attention input")
	ErrIssueNotFound       = errors.New("attention issue not found")
	ErrNotificationMissing = errors.New("notification not found")
	ErrIssueNotOpen        = errors.New("attention issue is not open")
	// ErrNotDelivered: only a SENT (delivered) Notification is user-readable.
	ErrNotDelivered = errors.New("notification has not been delivered")
	// errConflict: a conditional delivery transition lost a race; the caller
	// rolls back and re-evaluates.
	errConflict = errors.New("notification state changed concurrently")
)

type IssueType string

const (
	// IssueMonitorOverdueUnmet: an ACTIVE MonitorPoint is due and its typed
	// expectation is not satisfied.
	IssueMonitorOverdueUnmet IssueType = "MONITOR_OVERDUE_UNMET"
	// IssueTodoReminderDue: a Todo reminder threshold has been reached.
	IssueTodoReminderDue IssueType = "TODO_REMINDER_DUE"
)

type Severity string

const (
	SeverityInfo     Severity = "INFO"
	SeverityWarning  Severity = "WARNING"
	SeverityCritical Severity = "CRITICAL"
)

// IssueState is the authoritative Attention lifecycle. There is no SNOOZED state.
type IssueState string

const (
	IssueOpen     IssueState = "OPEN"
	IssueResolved IssueState = "RESOLVED"
)

// Disposition is the derived handling status of an issue at a given time.
type Disposition string

const (
	DispositionNeedsAction Disposition = "NEEDS_ACTION"
	DispositionSnoozed     Disposition = "SNOOZED"
	DispositionResolved    Disposition = "RESOLVED"
)

// DeliveryState is the Notification delivery lifecycle (not read/snooze/resolution).
type DeliveryState string

const (
	// DeliveryPending: durably created but NOT yet delivered to the in-app
	// Notification Center. Never product-visible.
	DeliveryPending DeliveryState = "PENDING"
	// DeliverySent: delivered by the deterministic IN_APP dispatcher; the only
	// product-visible state. Later Attention resolution never changes it.
	DeliverySent DeliveryState = "SENT"
	// DeliveryCancelled: became obsolete before first delivery because its
	// AttentionIssue resolved. Never sent, never product-visible; a delivery
	// terminal state only (not dismissal, suppression or Attention resolution).
	DeliveryCancelled DeliveryState = "CANCELLED"
)

type Channel string

const ChannelInApp Channel = "IN_APP"

// AttentionIssue is a persistent, unresolved problem.
type AttentionIssue struct {
	ID                   string
	WorkItemID           *string
	SubjectTrackedItemID *string // nil: the WorkItem itself is the subject
	MonitorPointID       *string
	PlannerItemID        *string
	Type                 IssueType
	Severity             Severity
	State                IssueState
	Reason               string
	ReasonJSON           string
	RecommendedAction    *string
	OpenedAt             time.Time
	ResolvedAt           *time.Time
	SnoozedUntil         *time.Time
	DedupeKey            string
	LastRevisionID       string
	UpdatedAt            time.Time
}

// DispositionAt derives Needs Action / Snoozed / Resolved. Snooze expiry is
// implicit: once snoozed_until <= now an OPEN issue needs action again.
func (i AttentionIssue) DispositionAt(now time.Time) Disposition {
	if i.State == IssueResolved {
		return DispositionResolved
	}
	if i.SnoozedUntil != nil && i.SnoozedUntil.After(now) {
		return DispositionSnoozed
	}
	return DispositionNeedsAction
}

// Notification is a durable user-facing communication.
type Notification struct {
	ID                   string
	AttentionIssueID     string
	WorkItemID           *string
	SubjectTrackedItemID *string
	PlannerItemID        *string
	Title                string
	Message              string
	ReasonSnapshotJSON   string
	State                DeliveryState
	Channel              Channel
	SentAt               *time.Time
	PresentedAt          *time.Time
	ReadAt               *time.Time
	DismissedAt          *time.Time
	DedupeKey            string
	LastRevisionID       string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// NotificationView pairs a Notification with its linked AttentionIssue so
// callers can derive Needs Action / Unread / Snoozed / Resolved without
// re-reading. It carries no presentation fields.
type NotificationView struct {
	Notification Notification
	Issue        AttentionIssue
}

// CenterItem is one product-visible (SENT) Notification with its linked issue
// and the disposition derived at the service clock.
type CenterItem struct {
	View        NotificationView
	Disposition Disposition
}

// NotificationCenter is the bounded, semantic, presentation-free projection of
// every delivered Notification, newest first. At is the service clock instant
// used to derive dispositions (so callers never own time policy).
type NotificationCenter struct {
	At    time.Time
	Items []CenterItem
}

// DispatchResult summarizes one deterministic IN_APP dispatch.
type DispatchResult struct {
	Sent      int // PENDING → SENT
	Cancelled int // PENDING → CANCELLED (issue resolved before delivery)
	Deferred  int // left PENDING (issue actively snoozed)
}

// ReconcileResult summarizes one deterministic reconciliation.
type ReconcileResult struct {
	Evaluated int // due ACTIVE monitors evaluated
	Opened    int
	Resolved  int
}
