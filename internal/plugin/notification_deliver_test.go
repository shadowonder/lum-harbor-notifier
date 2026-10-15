package plugin_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/plugintest"
)

type readinessInvoker struct {
	input  json.RawMessage
	result json.RawMessage
}

func (f *readinessInvoker) Invoke(_ context.Context, invocation plugin.Invocation) (json.RawMessage, error) {
	f.input = append(json.RawMessage(nil), invocation.Input...)
	return f.result, nil
}

func notificationDeliverSchema() plugin.PluginConfigSchema {
	return plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{
		{Key: "remoteError", Label: "Remote error", Type: plugin.ConfigBoolean},
	}}
}

func newNotificationDeliverFixtureHost(t *testing.T) (*plugin.Host, plugin.Extension) {
	t.Helper()
	root := t.TempDir()
	plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
		ID: "notification-deliver-fixture", Name: "Notification Deliver Fixture", Mode: "normal",
		CapabilityDeclarations: []plugin.CapabilityDeclaration{{ID: plugin.CapabilityNotificationDeliver}},
		Schema:                 notificationDeliverSchema(),
	})
	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("Discover: err=%v failures=%v", err, failures)
	}
	host, err := plugin.NewHost(registry, plugin.HostOptions{
		StartupTimeout: 3 * time.Second, RequestTimeout: 500 * time.Millisecond,
		ShutdownTimeout: 150 * time.Millisecond,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	extension, _ := registry.Find("notification-deliver-fixture")
	return host, extension
}

func TestNotificationDelivererHappyPathAgainstFixtureSubprocess(t *testing.T) {
	host, _ := newNotificationDeliverFixtureHost(t)
	if err := host.Start(context.Background(), "notification-deliver-fixture"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })

	instance := plugin.Instance{ID: "instance-1", ExtensionID: "notification-deliver-fixture", CapabilityID: plugin.CapabilityNotificationDeliver, Config: json.RawMessage(`{}`)}
	_, err := plugin.NewNotificationDeliverer(host).Deliver(context.Background(), instance, plugin.NotificationDeliveryRequest{
		DeliveryID: "notification-1", Title: "Ship it", Message: "due now", Urgency: plugin.NotificationUrgencyNormal,
	})
	if err != nil {
		t.Fatalf("expected a successful delivery, got: %v", err)
	}
}

func TestNotificationDelivererCriticalUrgencyAgainstFixtureSubprocess(t *testing.T) {
	host, _ := newNotificationDeliverFixtureHost(t)
	if err := host.Start(context.Background(), "notification-deliver-fixture"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })

	instance := plugin.Instance{ID: "instance-1", ExtensionID: "notification-deliver-fixture", CapabilityID: plugin.CapabilityNotificationDeliver, Config: json.RawMessage(`{}`)}
	_, err := plugin.NewNotificationDeliverer(host).Deliver(context.Background(), instance, plugin.NotificationDeliveryRequest{
		DeliveryID: "notification-2", Title: "Ship it", Message: "overdue", Urgency: plugin.NotificationUrgencyCritical,
	})
	if err != nil {
		t.Fatalf("expected a successful delivery, got: %v", err)
	}
}

func TestNotificationDelivererProcessRemoteErrorIsNeverASuccessfulResult(t *testing.T) {
	host, _ := newNotificationDeliverFixtureHost(t)
	if err := host.Start(context.Background(), "notification-deliver-fixture"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })

	instance := plugin.Instance{ID: "instance-1", ExtensionID: "notification-deliver-fixture", CapabilityID: plugin.CapabilityNotificationDeliver, Config: json.RawMessage(`{"remoteError":true}`)}
	_, err := plugin.NewNotificationDeliverer(host).Deliver(context.Background(), instance, plugin.NotificationDeliveryRequest{
		DeliveryID: "notification-3", Title: "Ship it", Message: "due now", Urgency: plugin.NotificationUrgencyNormal,
	})
	if err == nil {
		t.Fatal("expected a remote Plugin error to surface as an error, never a successful result")
	}
}

func TestNotificationDelivererRejectsWrongCapabilityInstance(t *testing.T) {
	host, _ := newNotificationDeliverFixtureHost(t)
	instance := plugin.Instance{ID: "instance-1", ExtensionID: "notification-deliver-fixture", CapabilityID: plugin.CapabilityReadQuery}
	_, err := plugin.NewNotificationDeliverer(host).Deliver(context.Background(), instance, plugin.NotificationDeliveryRequest{
		DeliveryID: "notification-1", Title: "t", Message: "m", Urgency: plugin.NotificationUrgencyNormal,
	})
	if !errors.Is(err, plugin.ErrCapabilityUnsupported) {
		t.Fatalf("expected ErrCapabilityUnsupported, got %v", err)
	}
}

func TestNotificationDelivererRejectsInvalidRequests(t *testing.T) {
	host, _ := newNotificationDeliverFixtureHost(t)
	instance := plugin.Instance{ID: "instance-1", ExtensionID: "notification-deliver-fixture", CapabilityID: plugin.CapabilityNotificationDeliver}
	deliverer := plugin.NewNotificationDeliverer(host)

	cases := map[string]plugin.NotificationDeliveryRequest{
		"empty delivery ID": {DeliveryID: "  ", Title: "t", Message: "m", Urgency: plugin.NotificationUrgencyNormal},
		"empty title":       {DeliveryID: "id", Title: " ", Message: "m", Urgency: plugin.NotificationUrgencyNormal},
		"empty message":     {DeliveryID: "id", Title: "t", Message: " ", Urgency: plugin.NotificationUrgencyNormal},
		"unknown urgency":   {DeliveryID: "id", Title: "t", Message: "m", Urgency: "SUPER_URGENT"},
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := deliverer.Deliver(context.Background(), instance, request); err == nil {
				t.Fatalf("%s: expected a validation error", name)
			}
		})
	}
}

func TestNotificationDelivererChecksReadinessWithoutDeliveryPayload(t *testing.T) {
	invoker := &readinessInvoker{result: json.RawMessage(`{"authorizationStatus":"DENIED"}`)}
	instance := plugin.Instance{ID: "native", ExtensionID: "lh-plugin-macos-notifications", CapabilityID: plugin.CapabilityNotificationDeliver}

	result, err := plugin.NewNotificationDeliverer(invoker).CheckReadiness(context.Background(), instance)
	if err != nil {
		t.Fatal(err)
	}
	if result.AuthorizationStatus != plugin.NotificationAuthorizationDenied {
		t.Fatalf("AuthorizationStatus = %q, want DENIED", result.AuthorizationStatus)
	}
	var input map[string]any
	if err := json.Unmarshal(invoker.input, &input); err != nil {
		t.Fatal(err)
	}
	if input["operation"] != "CHECK_READINESS" {
		t.Fatalf("unexpected readiness input: %s", invoker.input)
	}
	if _, exists := input["deliveryId"]; exists {
		t.Fatalf("readiness must not carry a delivery identity: %s", invoker.input)
	}
}

func TestNotificationDelivererRejectsUnknownReadinessStatus(t *testing.T) {
	invoker := &readinessInvoker{result: json.RawMessage(`{"authorizationStatus":"MAYBE"}`)}
	instance := plugin.Instance{ID: "native", ExtensionID: "lh-plugin-macos-notifications", CapabilityID: plugin.CapabilityNotificationDeliver}
	if _, err := plugin.NewNotificationDeliverer(invoker).CheckReadiness(context.Background(), instance); err == nil {
		t.Fatal("expected an unsupported authorization status to fail closed")
	}
}

func TestNotificationDelivererPreservesExplicitUnknownReadiness(t *testing.T) {
	invoker := &readinessInvoker{result: json.RawMessage(`{"authorizationStatus":"UNKNOWN"}`)}
	instance := plugin.Instance{ID: "native", ExtensionID: "lh-plugin-macos-notifications", CapabilityID: plugin.CapabilityNotificationDeliver}
	result, err := plugin.NewNotificationDeliverer(invoker).CheckReadiness(context.Background(), instance)
	if err != nil {
		t.Fatal(err)
	}
	if result.AuthorizationStatus != plugin.NotificationAuthorizationUnknown {
		t.Fatalf("AuthorizationStatus = %q, want UNKNOWN", result.AuthorizationStatus)
	}
}
