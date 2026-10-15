// Package notificationdelivery is the M17 delivery boundary. It owns
// finding externally-undelivered eligible Notifications, invoking a typed
// external delivery capability, and recording durable delivery-success
// truth - and nothing else. It never evaluates Attention, never creates a
// Notification, never classifies severity, never mutates WorkItem/Tracking
// truth, never resolves Attention, and never performs IN_APP dispatch; all
// of that remains internal/attention's exclusive ownership.
//
// This package never imports internal/plugin. A thin adapter package (added
// once a real delivery Plugin Instance exists) implements the narrow
// Deliverer interface below by wrapping the typed Plugin capability,
// mirroring internal/plugin/readadapter's relationship to
// internal/readcap.PluginReadSource.
package notificationdelivery

import "context"

// Urgency is the platform-neutral urgency fact passed to a delivery
// capability. It is never a platform enum.
type Urgency string

const (
	// UrgencyNormal covers the product's Normal/Warning class.
	UrgencyNormal Urgency = "NORMAL"
	// UrgencyCritical covers the product's Critical/Severe class.
	UrgencyCritical Urgency = "CRITICAL"
)

// DeliveryRequest is the platform-neutral delivery intent this package
// builds from an already-real SENT Notification and its linked
// AttentionIssue's severity. DeliveryID is stable Notification identity
// (never a per-attempt random value): the same external delivery is always
// requested with the same DeliveryID, so a delivery capability can treat
// repeated invocations as "ensure delivery for this ID" rather than "fire
// another notification".
type DeliveryRequest struct {
	DeliveryID    string
	Title         string
	Message       string
	Urgency       Urgency
	PlannerItemID *string
	WorkItemID    *string
}

// DeliveryResult is intentionally empty. Success is the absence of an
// error; there is nothing else for a delivery capability to report in V1.
type DeliveryResult struct{}

// Deliverer is the only capability this package depends on. It never
// exposes retry, persistence, or idempotency to its implementer - those
// remain this package's own responsibility (Service), not the Plugin's.
type Deliverer interface {
	Deliver(ctx context.Context, request DeliveryRequest) (DeliveryResult, error)
}

// mapUrgency translates an AttentionIssue severity fact into the
// platform-neutral two-bucket product distinction (Normal/Warning-class vs
// Critical/Severe-class). It is the only place Core severity crosses into
// this package's vocabulary.
func mapUrgency(severity string) Urgency {
	if severity == "CRITICAL" {
		return UrgencyCritical
	}
	return UrgencyNormal
}
