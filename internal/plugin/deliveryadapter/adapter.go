// Package deliveryadapter bridges the platform-neutral notificationdelivery
// domain package to the typed Plugin Runtime notification.deliver capability.
//
// Following the architectural precedent set by internal/plugin/readadapter,
// internal/notificationdelivery never imports internal/plugin; this adapter
// package is the only place that imports both. It owns translating the
// platform-neutral DeliveryRequest into the typed Plugin capability request,
// invoking the Plugin through the typed deliverer, and returning the result.
package deliveryadapter

import (
	"context"
	"fmt"

	"lum-harbor-notifier/internal/notificationdelivery"
	"lum-harbor-notifier/internal/plugin"
)

// Adapter adapts a resolved plugin.Instance selecting notification.deliver
// to the notificationdelivery.Deliverer interface.
type Adapter struct {
	deliverer *plugin.NotificationDeliverer
	instance  plugin.Instance
}

// New constructs an Adapter for the specified resolved Instance. The instance
// must select CapabilityNotificationDeliver.
func New(invoker plugin.Invoker, instance plugin.Instance) (*Adapter, error) {
	if invoker == nil {
		return nil, fmt.Errorf("deliveryadapter: invoker must not be nil")
	}
	if instance.CapabilityID != plugin.CapabilityNotificationDeliver {
		return nil, fmt.Errorf("deliveryadapter: instance %q requires capability %q, got %q", instance.ID, plugin.CapabilityNotificationDeliver, instance.CapabilityID)
	}
	return &Adapter{
		deliverer: plugin.NewNotificationDeliverer(invoker),
		instance:  instance,
	}, nil
}

// Deliver translates the domain DeliveryRequest into the typed Plugin request,
// invokes the capability, and maps the result.
func (a *Adapter) Deliver(ctx context.Context, request notificationdelivery.DeliveryRequest) (notificationdelivery.DeliveryResult, error) {
	if a == nil || a.deliverer == nil {
		return notificationdelivery.DeliveryResult{}, fmt.Errorf("deliveryadapter: adapter not initialized")
	}

	urgency := plugin.NotificationUrgencyNormal
	if request.Urgency == notificationdelivery.UrgencyCritical {
		urgency = plugin.NotificationUrgencyCritical
	}

	pluginReq := plugin.NotificationDeliveryRequest{
		DeliveryID: request.DeliveryID, Title: request.Title, Message: request.Message,
		Urgency: urgency, PlannerItemID: request.PlannerItemID,
		WorkItemID: request.WorkItemID,
	}

	_, err := a.deliverer.Deliver(ctx, a.instance, pluginReq)
	if err != nil {
		return notificationdelivery.DeliveryResult{}, err
	}
	return notificationdelivery.DeliveryResult{}, nil
}
