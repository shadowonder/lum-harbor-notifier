package deliveryadapter_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/notificationdelivery"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/deliveryadapter"
	"lum-harbor-notifier/internal/plugininstall"
	"lum-harbor-notifier/internal/testsupport"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

func findBridgeDist(t *testing.T) string {
	t.Helper()
	// Relative from internal/plugin/deliveryadapter
	candidates := []string{
		"../../../../lh-plugin-macos-notifications/dist/lh-plugin-macos-notifications.lhplugin",
		"../../../lh-plugin-macos-notifications/dist/lh-plugin-macos-notifications.lhplugin",
		"../../lh-plugin-macos-notifications/dist/lh-plugin-macos-notifications.lhplugin",
		"../../../../lh-plugin-macos-notifications/dist/lh-plugin-macos-notifications",
		"../../../lh-plugin-macos-notifications/dist/lh-plugin-macos-notifications",
		"../../lh-plugin-macos-notifications/dist/lh-plugin-macos-notifications",
	}
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err == nil {
			if strings.HasSuffix(c, ".lhplugin") {
				if _, err := os.Stat(abs); err == nil {
					return abs
				}
			} else {
				if _, err := os.Stat(filepath.Join(abs, "plugin.json")); err == nil {
					return abs
				}
			}
		}
	}
	t.Skip("lh-plugin-macos-notifications artifact not found; run make package in lh-plugin-macos-notifications first")
	return ""
}

func TestRealNotificationMacOSBridgePluginProtocolAndDelivery(t *testing.T) {
	distArtifact := findBridgeDist(t)

	// Create a temporary plugin root and install the artifact
	pluginRoot := t.TempDir()
	installer, err := plugininstall.New(pluginRoot)
	if err != nil {
		t.Fatalf("New installer: %v", err)
	}

	result, err := installer.Install(distArtifact)
	if err != nil {
		t.Fatalf("Install bridge: %v", err)
	}
	if result.ExtensionID != "lh-plugin-macos-notifications" {
		t.Fatalf("expected extension ID 'lh-plugin-macos-notifications', got %q", result.ExtensionID)
	}

	// Discover extensions
	registry, failures, err := plugin.Discover(pluginRoot)
	if err != nil || len(failures) != 0 {
		t.Fatalf("Discover: err=%v failures=%v", err, failures)
	}

	ext, err := registry.Find("lh-plugin-macos-notifications")
	if err != nil {
		t.Fatalf("Find extension: %v", err)
	}
	if len(ext.Manifest.Capabilities) != 1 || ext.Manifest.Capabilities[0].ID != plugin.CapabilityNotificationDeliver {
		t.Fatalf("expected capability notification.deliver, got %v", ext.Manifest.Capabilities)
	}

	// Construct Host
	host, err := plugin.NewHost(registry, plugin.HostOptions{
		StartupTimeout:  5 * time.Second,
		RequestTimeout:  5 * time.Second,
		ShutdownTimeout: 2 * time.Second,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}

	// Start extension process (exercises system.handshake over stdio)
	ctx := context.Background()
	if err := host.Start(ctx, "lh-plugin-macos-notifications"); err != nil {
		t.Fatalf("host.Start failed: %v", err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })

	instance := plugin.Instance{
		ID:           "test-bridge-instance-1",
		ExtensionID:  "lh-plugin-macos-notifications",
		CapabilityID: plugin.CapabilityNotificationDeliver,
	}
	readiness, err := plugin.NewNotificationDeliverer(host).CheckReadiness(ctx, instance)
	if err != nil {
		if strings.Contains(err.Error(), "authorization_check_failed") {
			t.Skipf("macOS notification readiness is unavailable in this test environment: %v", err)
		}
		t.Fatalf("CheckReadiness: %v", err)
	}
	if readiness.AuthorizationStatus == plugin.NotificationAuthorizationDenied {
		t.Skip("macOS notification permission is denied for the test bridge bundle")
	}
	if readiness.AuthorizationStatus != plugin.NotificationAuthorizationAuthorized &&
		readiness.AuthorizationStatus != plugin.NotificationAuthorizationProvisional &&
		readiness.AuthorizationStatus != plugin.NotificationAuthorizationEphemeral {
		t.Fatalf("unexpected native notification readiness: %+v", readiness)
	}

	// Construct delivery adapter
	adapter, err := deliveryadapter.New(host, instance)
	if err != nil {
		t.Fatalf("New adapter: %v", err)
	}

	deliveryID := "real-macos-test-" + time.Now().Format("20060102150405.000000")

	// 1. Deliver NORMAL notification
	_, err = adapter.Deliver(ctx, notificationdelivery.DeliveryRequest{
		DeliveryID: deliveryID,
		Title:      "Lum Harbor Test",
		Message:    "Slice 2 automated delivery verification",
		Urgency:    notificationdelivery.UrgencyNormal,
	})
	if err != nil {
		t.Fatalf("Deliver NORMAL: %v", err)
	}

	// 2. Deliver the SAME DeliveryID again -> duplicate suppression in bridge
	_, err = adapter.Deliver(ctx, notificationdelivery.DeliveryRequest{
		DeliveryID: deliveryID,
		Title:      "Lum Harbor Test",
		Message:    "Slice 2 automated delivery verification (duplicate)",
		Urgency:    notificationdelivery.UrgencyNormal,
	})
	if err != nil {
		t.Fatalf("Deliver duplicate: %v", err)
	}

	// 3. Deliver CRITICAL notification
	criticalID := "real-macos-critical-" + time.Now().Format("20060102150405.000000")
	_, err = adapter.Deliver(ctx, notificationdelivery.DeliveryRequest{
		DeliveryID: criticalID,
		Title:      "Lum Harbor Attention",
		Message:    "Overdue work item requires immediate review",
		Urgency:    notificationdelivery.UrgencyCritical,
	})
	if err != nil {
		t.Fatalf("Deliver CRITICAL: %v", err)
	}

	// 4. Test within notificationdelivery.Service with real database
	testDB := testsupport.OpenTestDatabase(t)
	db := testDB.DB

	// Seed a real SENT notification using standard domain services
	workItems := workitem.NewService(db, workitem.NewRepository(db))
	trackingService := tracking.NewService(db, workItems)
	attentionService := attention.NewService(db, trackingService)

	monitorDue := time.Date(2026, 10, 15, 22, 0, 0, 0, time.UTC)
	cycleNow := monitorDue.Add(2 * time.Hour)
	wi, err := workItems.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: "Ship M17 Integration"})
	if err != nil {
		t.Fatalf("CreateLocalWorkItem: %v", err)
	}
	completed := workitem.LifecycleCompleted
	if _, err := trackingService.CreateMonitorPoint(ctx, tracking.CreateMonitorPointInput{
		WorkItemID:                wi.ID,
		Kind:                      tracking.MonitorFinalTarget,
		Title:                     "Ship M17 Target",
		DueAt:                     monitorDue,
		Timezone:                  "UTC",
		ExpectedWorkItemLifecycle: &completed,
	}); err != nil {
		t.Fatalf("CreateMonitorPoint: %v", err)
	}
	if _, err := attentionService.Reconcile(ctx, cycleNow); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if _, err := attentionService.DispatchPendingNotifications(ctx, cycleNow); err != nil {
		t.Fatalf("DispatchPendingNotifications: %v", err)
	}

	var notifID string
	err = db.QueryRow(`SELECT n.entity_id FROM notifications n WHERE n.state = 'SENT' LIMIT 1`).Scan(&notifID)
	if err != nil {
		t.Fatalf("query SENT notification: %v", err)
	}

	service := notificationdelivery.NewService(db, adapter)

	// Run first cycle -> delivers and marks
	cycleResult, err := service.RunDeliveryCycle(ctx, cycleNow)
	if err != nil {
		t.Fatalf("RunDeliveryCycle 1 failed: %v", err)
	}
	if cycleResult.Attempted != 1 || cycleResult.Delivered != 1 || cycleResult.Failed != 0 {
		t.Fatalf("expected 1 attempted, 1 delivered, 0 failed, got %+v", cycleResult)
	}

	// Verify marker is durably stored in notification_external_deliveries
	var deliveredAt string
	err = db.QueryRow(`SELECT delivered_at FROM notification_external_deliveries WHERE notification_id = ?`, notifID).Scan(&deliveredAt)
	if err != nil {
		t.Fatalf("query marker: %v", err)
	}
	if deliveredAt == "" {
		t.Fatal("expected non-empty delivered_at")
	}

	// Run second cycle -> skips already-delivered candidate
	cycleResult2, err := service.RunDeliveryCycle(ctx, cycleNow)
	if err != nil {
		t.Fatalf("RunDeliveryCycle 2 failed: %v", err)
	}
	if cycleResult2.Attempted != 0 || cycleResult2.Delivered != 0 {
		t.Fatalf("expected 0 attempted on second run, got %+v", cycleResult2)
	}
}
