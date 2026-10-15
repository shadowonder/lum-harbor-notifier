package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// NotificationUrgency is the platform-neutral urgency fact a delivery
// capability receives. It is deliberately not a platform enum - it never
// names a macOS interruption level, a sound, or any other presentation
// detail; a Plugin translates it into its own platform-specific mechanism.
type NotificationUrgency string

const (
	// NotificationUrgencyNormal covers the product's Normal/Warning class.
	NotificationUrgencyNormal NotificationUrgency = "NORMAL"
	// NotificationUrgencyCritical covers the product's Critical/Severe
	// class, which a delivery capability must present with a noticeably
	// stronger real user experience than Normal.
	NotificationUrgencyCritical NotificationUrgency = "CRITICAL"
)

func (u NotificationUrgency) valid() bool {
	return u == NotificationUrgencyNormal || u == NotificationUrgencyCritical
}

// NotificationDeliveryRequest is the notification.deliver typed input. It
// carries only what an external delivery implementation actually needs -
// never a raw DB record, Attention internals, a macOS enum, or a
// platform-specific field. Optional Planner/WorkItem identity exists only so
// a platform click can return to the product's existing navigation paths.
//
// DeliveryID is a stable, deterministic identity for one Lum Harbor external
// delivery, derived from durable Notification identity by the delivery
// boundary (never a per-attempt random value). The semantic contract is
// "ensure delivery for this DeliveryID", not "fire another notification":
// a Plugin invoked twice with the same DeliveryID for the same logical
// delivery must not produce two distinct user-visible effects. The real
// platform-side mechanics of honoring that are a Slice 2 concern; Slice 1
// only makes the contract explicit and proves it against a fake capability.
type NotificationDeliveryRequest struct {
	DeliveryID    string              `json:"deliveryId"`
	Title         string              `json:"title"`
	Message       string              `json:"message"`
	Urgency       NotificationUrgency `json:"urgency"`
	PlannerItemID *string             `json:"plannerItemId,omitempty"`
	WorkItemID    *string             `json:"workItemId,omitempty"`
}

// NotificationDeliveryResult is the notification.deliver typed result. A
// delivery capability that could not ensure delivery must return an RPC
// error (Plugin Runtime Contract V1 §7) rather than a "successful" result
// carrying a failure flag - there is nothing else to say on success.
type NotificationDeliveryResult struct{}

type NotificationAuthorizationStatus string

const (
	NotificationAuthorizationNotDetermined NotificationAuthorizationStatus = "NOT_DETERMINED"
	NotificationAuthorizationDenied        NotificationAuthorizationStatus = "DENIED"
	NotificationAuthorizationAuthorized    NotificationAuthorizationStatus = "AUTHORIZED"
	NotificationAuthorizationProvisional   NotificationAuthorizationStatus = "PROVISIONAL"
	NotificationAuthorizationEphemeral     NotificationAuthorizationStatus = "EPHEMERAL"
	NotificationAuthorizationUnknown       NotificationAuthorizationStatus = "UNKNOWN"
)

func (s NotificationAuthorizationStatus) valid() bool {
	switch s {
	case NotificationAuthorizationNotDetermined, NotificationAuthorizationDenied,
		NotificationAuthorizationAuthorized, NotificationAuthorizationProvisional,
		NotificationAuthorizationEphemeral, NotificationAuthorizationUnknown:
		return true
	default:
		return false
	}
}

// NotificationReadinessResult is the plugin-specific startup projection for
// macOS notification permission. It is deliberately part of the existing
// notification.deliver seam rather than a generic plugin-health protocol.
type NotificationReadinessResult struct {
	AuthorizationStatus NotificationAuthorizationStatus `json:"authorizationStatus"`
}

type notificationDeliveryInvocation struct {
	Operation     string              `json:"operation"`
	DeliveryID    string              `json:"deliveryId,omitempty"`
	Title         string              `json:"title,omitempty"`
	Message       string              `json:"message,omitempty"`
	Urgency       NotificationUrgency `json:"urgency,omitempty"`
	PlannerItemID *string             `json:"plannerItemId,omitempty"`
	WorkItemID    *string             `json:"workItemId,omitempty"`
}

// NotificationDeliverer is the notification.deliver typed adapter, mirroring
// ReadQuerier's serialize/invoke/decode shape exactly (Plugin Runtime
// Contract V1 §9: the Host, not the manifest, owns payload meaning).
type NotificationDeliverer struct {
	invoker Invoker
}

func NewNotificationDeliverer(invoker Invoker) *NotificationDeliverer {
	return &NotificationDeliverer{invoker: invoker}
}

func (d *NotificationDeliverer) Deliver(ctx context.Context, instance Instance, request NotificationDeliveryRequest) (NotificationDeliveryResult, error) {
	if d == nil || d.invoker == nil {
		return NotificationDeliveryResult{}, fmt.Errorf("notification deliverer invoker must not be nil")
	}
	if instance.CapabilityID != CapabilityNotificationDeliver {
		return NotificationDeliveryResult{}, fmt.Errorf("%w: instance %q requires %q", ErrCapabilityUnsupported, instance.ID, CapabilityNotificationDeliver)
	}
	deliveryID := strings.TrimSpace(request.DeliveryID)
	if deliveryID == "" {
		return NotificationDeliveryResult{}, fmt.Errorf("notification.deliver request must have a non-empty DeliveryID")
	}
	if strings.TrimSpace(request.Title) == "" {
		return NotificationDeliveryResult{}, fmt.Errorf("notification.deliver request must have a non-empty Title")
	}
	if strings.TrimSpace(request.Message) == "" {
		return NotificationDeliveryResult{}, fmt.Errorf("notification.deliver request must have a non-empty Message")
	}
	if !request.Urgency.valid() {
		return NotificationDeliveryResult{}, fmt.Errorf("notification.deliver request has unsupported Urgency %q", request.Urgency)
	}
	input, err := json.Marshal(notificationDeliveryInvocation{
		Operation: "DELIVER", DeliveryID: deliveryID, Title: request.Title, Message: request.Message,
		Urgency: request.Urgency, PlannerItemID: request.PlannerItemID,
		WorkItemID: request.WorkItemID,
	})
	if err != nil {
		return NotificationDeliveryResult{}, fmt.Errorf("encode notification.deliver request: %w", err)
	}
	output, err := d.invoker.Invoke(ctx, Invocation{
		ExtensionID:  instance.ExtensionID,
		CapabilityID: CapabilityNotificationDeliver,
		Instance:     &instance,
		Input:        input,
	})
	if err != nil {
		return NotificationDeliveryResult{}, err
	}
	var result NotificationDeliveryResult
	if err := json.Unmarshal(output, &result); err != nil {
		return NotificationDeliveryResult{}, fmt.Errorf("decode notification.deliver result for instance %q: %w", instance.ID, err)
	}
	return result, nil
}

// CheckReadiness asks the selected notification.deliver implementation to
// resolve its native authorization state. A macOS plugin may prompt when the
// state is notDetermined; this operation never creates or delivers a
// notification.
func (d *NotificationDeliverer) CheckReadiness(ctx context.Context, instance Instance) (NotificationReadinessResult, error) {
	if d == nil || d.invoker == nil {
		return NotificationReadinessResult{}, fmt.Errorf("notification deliverer invoker must not be nil")
	}
	if instance.CapabilityID != CapabilityNotificationDeliver {
		return NotificationReadinessResult{}, fmt.Errorf("%w: instance %q requires %q", ErrCapabilityUnsupported, instance.ID, CapabilityNotificationDeliver)
	}
	input, err := json.Marshal(notificationDeliveryInvocation{Operation: "CHECK_READINESS"})
	if err != nil {
		return NotificationReadinessResult{}, fmt.Errorf("encode notification.deliver readiness request: %w", err)
	}
	output, err := d.invoker.Invoke(ctx, Invocation{
		ExtensionID: instance.ExtensionID, CapabilityID: CapabilityNotificationDeliver,
		Instance: &instance, Input: input,
	})
	if err != nil {
		return NotificationReadinessResult{}, err
	}
	var result NotificationReadinessResult
	if err := json.Unmarshal(output, &result); err != nil {
		return NotificationReadinessResult{}, fmt.Errorf("decode notification.deliver readiness result for instance %q: %w", instance.ID, err)
	}
	if !result.AuthorizationStatus.valid() {
		return NotificationReadinessResult{}, fmt.Errorf("notification.deliver readiness result for instance %q has unsupported authorization status %q", instance.ID, result.AuthorizationStatus)
	}
	return result, nil
}
