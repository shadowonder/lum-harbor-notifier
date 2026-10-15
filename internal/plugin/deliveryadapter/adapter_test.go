package deliveryadapter_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"lum-harbor-notifier/internal/notificationdelivery"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/deliveryadapter"
)

type fakeInvoker struct {
	calls []plugin.Invocation
	err   error
}

func (f *fakeInvoker) Invoke(ctx context.Context, inv plugin.Invocation) (json.RawMessage, error) {
	f.calls = append(f.calls, inv)
	if f.err != nil {
		return nil, f.err
	}
	return json.Marshal(plugin.NotificationDeliveryResult{})
}

func TestNewValidation(t *testing.T) {
	validInstance := plugin.Instance{
		ID:           "delivery-inst-1",
		ExtensionID:  "lh-plugin-macos-notifications",
		CapabilityID: plugin.CapabilityNotificationDeliver,
	}

	if _, err := deliveryadapter.New(nil, validInstance); err == nil {
		t.Fatal("expected error for nil invoker")
	}

	wrongInstance := plugin.Instance{
		ID:           "wrong-inst",
		ExtensionID:  "other",
		CapabilityID: plugin.CapabilitySourceFetch,
	}
	if _, err := deliveryadapter.New(&fakeInvoker{}, wrongInstance); err == nil {
		t.Fatal("expected error for wrong capability instance")
	}
}

func TestDeliverMapping(t *testing.T) {
	instance := plugin.Instance{
		ID:           "delivery-inst-1",
		ExtensionID:  "lh-plugin-macos-notifications",
		CapabilityID: plugin.CapabilityNotificationDeliver,
	}

	tests := []struct {
		name            string
		req             notificationdelivery.DeliveryRequest
		expectedUrgency plugin.NotificationUrgency
	}{
		{
			name: "Normal urgency",
			req: notificationdelivery.DeliveryRequest{
				DeliveryID:    "deliv-1",
				Title:         "Test Title",
				Message:       "Test Message",
				Urgency:       notificationdelivery.UrgencyNormal,
				PlannerItemID: stringPointer("todo-1"),
			},
			expectedUrgency: plugin.NotificationUrgencyNormal,
		},
		{
			name: "Critical urgency",
			req: notificationdelivery.DeliveryRequest{
				DeliveryID: "deliv-2",
				Title:      "Critical Alert",
				Message:    "Something is on fire",
				Urgency:    notificationdelivery.UrgencyCritical,
			},
			expectedUrgency: plugin.NotificationUrgencyCritical,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			invoker := &fakeInvoker{}
			adapter, err := deliveryadapter.New(invoker, instance)
			if err != nil {
				t.Fatalf("unexpected New error: %v", err)
			}

			_, err = adapter.Deliver(context.Background(), tc.req)
			if err != nil {
				t.Fatalf("unexpected Deliver error: %v", err)
			}

			if len(invoker.calls) != 1 {
				t.Fatalf("expected 1 call, got %d", len(invoker.calls))
			}

			call := invoker.calls[0]
			if call.CapabilityID != plugin.CapabilityNotificationDeliver {
				t.Errorf("expected capability %q, got %q", plugin.CapabilityNotificationDeliver, call.CapabilityID)
			}
			if call.ExtensionID != instance.ExtensionID {
				t.Errorf("expected extensionID %q, got %q", instance.ExtensionID, call.ExtensionID)
			}

			var payload plugin.NotificationDeliveryRequest
			if err := json.Unmarshal(call.Input, &payload); err != nil {
				t.Fatalf("failed to unmarshal call input: %v", err)
			}

			if payload.DeliveryID != tc.req.DeliveryID {
				t.Errorf("expected DeliveryID %q, got %q", tc.req.DeliveryID, payload.DeliveryID)
			}
			if payload.Title != tc.req.Title {
				t.Errorf("expected Title %q, got %q", tc.req.Title, payload.Title)
			}
			if payload.Message != tc.req.Message {
				t.Errorf("expected Message %q, got %q", tc.req.Message, payload.Message)
			}
			if payload.Urgency != tc.expectedUrgency {
				t.Errorf("expected Urgency %q, got %q", tc.expectedUrgency, payload.Urgency)
			}
			if tc.req.PlannerItemID != nil && (payload.PlannerItemID == nil || *payload.PlannerItemID != *tc.req.PlannerItemID) {
				t.Errorf("expected PlannerItemID %q, got %v", *tc.req.PlannerItemID, payload.PlannerItemID)
			}
		})
	}
}

func stringPointer(value string) *string { return &value }

func TestDeliverErrorPropagation(t *testing.T) {
	instance := plugin.Instance{
		ID:           "delivery-inst-1",
		ExtensionID:  "lh-plugin-macos-notifications",
		CapabilityID: plugin.CapabilityNotificationDeliver,
	}

	expectedErr := errors.New("platform RPC timeout")
	invoker := &fakeInvoker{err: expectedErr}
	adapter, err := deliveryadapter.New(invoker, instance)
	if err != nil {
		t.Fatalf("unexpected New error: %v", err)
	}

	req := notificationdelivery.DeliveryRequest{
		DeliveryID: "deliv-err",
		Title:      "Error Test",
		Message:    "Should fail",
		Urgency:    notificationdelivery.UrgencyNormal,
	}

	_, err = adapter.Deliver(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected error %v, got %v", expectedErr, err)
	}
}
