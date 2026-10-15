package app

import (
	"encoding/json"
	"strings"
	"testing"

	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/plugintest"
)

func newDeliveryOwnerApp(t *testing.T) *App {
	t.Helper()
	setupIsolatedConfigHome(t)
	return newCreateInstanceApp(t,
		plugintest.ExtensionSpec{ID: "lh-plugin-macos-notifications", Capabilities: []plugin.CapabilityID{plugin.CapabilityNotificationDeliver}},
		plugintest.ExtensionSpec{ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch}},
	)
}

func createDeliveryInstance(t *testing.T, application *App) string {
	t.Helper()
	created, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-macos-notifications", CapabilityID: string(plugin.CapabilityNotificationDeliver),
	})
	if err != nil {
		t.Fatal(err)
	}
	return created.InstanceID
}

// persistedEnabled maps every persisted declaration ID to its enabled state.
func persistedEnabled(t *testing.T) map[string]bool {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	states := make(map[string]bool, len(cfg.Plugins.Instances))
	for _, declaration := range cfg.Plugins.Instances {
		states[declaration.ID] = !declaration.Disabled
	}
	return states
}

func TestCreatePluginInstanceRejectsSecondEnabledDeliveryOwner(t *testing.T) {
	application := newDeliveryOwnerApp(t)
	first := createDeliveryInstance(t, application)

	_, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-macos-notifications", CapabilityID: string(plugin.CapabilityNotificationDeliver),
	})
	if err == nil {
		t.Fatal("expected second enabled delivery owner to be rejected")
	}
	if states := persistedEnabled(t); len(states) != 1 || !states[first] {
		t.Fatalf("expected only the first owner persisted and enabled: %+v", states)
	}
	inventory, err := application.GetPluginInventory()
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range inventory.Plugins {
		if item.ID == "lh-plugin-macos-notifications" && (len(item.Instances) != 1 || item.Instances[0].ID != first) {
			t.Fatalf("rejected create must not appear in the desired-state projection: %+v", item.Instances)
		}
	}
}

func TestSetPluginInstanceEnabledRejectsSecondEnabledDeliveryOwner(t *testing.T) {
	application := newDeliveryOwnerApp(t)
	first := createDeliveryInstance(t, application)
	if _, err := application.SetPluginInstanceEnabled(first, false); err != nil {
		t.Fatal(err)
	}
	second := createDeliveryInstance(t, application)
	if _, err := application.SetPluginInstanceEnabled(second, false); err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetPluginInstanceEnabled(first, true); err != nil {
		t.Fatal(err)
	}

	if _, err := application.SetPluginInstanceEnabled(second, true); err == nil {
		t.Fatal("expected Use Again of a second delivery owner to be rejected")
	}
	if states := persistedEnabled(t); !states[first] || states[second] {
		t.Fatalf("expected first enabled and second still disabled: %+v", states)
	}
}

func TestSetPluginInstanceEnabledAllowsDeliveryOwnerTransfer(t *testing.T) {
	application := newDeliveryOwnerApp(t)
	first := createDeliveryInstance(t, application)
	if _, err := application.SetPluginInstanceEnabled(first, false); err != nil {
		t.Fatal(err)
	}
	second := createDeliveryInstance(t, application)

	if _, err := application.SetPluginInstanceEnabled(second, false); err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetPluginInstanceEnabled(second, true); err != nil {
		t.Fatalf("expected enabling the only owner to succeed: %v", err)
	}
	if states := persistedEnabled(t); states[first] || !states[second] {
		t.Fatalf("expected exactly the second owner enabled: %+v", states)
	}
}

func TestSingleDeliveryOwnerRuleDoesNotApplyToOtherCapabilities(t *testing.T) {
	application := newDeliveryOwnerApp(t)
	for i := 0; i < 2; i++ {
		if _, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
			ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
		}); err != nil {
			t.Fatalf("expected multiple enabled source.fetch instances to stay allowed: %v", err)
		}
	}
	createDeliveryInstance(t, application)
	if states := persistedEnabled(t); len(states) != 3 {
		t.Fatalf("expected two source.fetch and one delivery instance: %+v", states)
	}
}

// A hand-edited or legacy config with two enabled owners must still be
// repairable through Stop Using; the guard only checks the mutated entry.
func TestSetPluginInstanceEnabledAllowsDisablingLegacyDuplicateDeliveryOwner(t *testing.T) {
	application := newDeliveryOwnerApp(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"legacy-a", "legacy-b"} {
		cfg.Plugins.Instances = append(cfg.Plugins.Instances, config.PluginInstanceConfig{
			ID: id, ExtensionID: "lh-plugin-macos-notifications",
			CapabilityID: string(plugin.CapabilityNotificationDeliver), Config: json.RawMessage(`{}`),
		})
	}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}

	if _, err := application.SetPluginInstanceEnabled("legacy-b", false); err != nil {
		t.Fatalf("expected disabling one duplicate owner to succeed: %v", err)
	}
	if states := persistedEnabled(t); !states["legacy-a"] || states["legacy-b"] {
		t.Fatalf("expected only legacy-a enabled: %+v", states)
	}
}

func newAIProviderApp(t *testing.T) *App {
	t.Helper()
	setupIsolatedConfigHome(t)
	return newCreateInstanceApp(t,
		plugintest.ExtensionSpec{ID: "lh-plugin-ai-provider", Capabilities: []plugin.CapabilityID{plugin.CapabilityAIProvider}},
		plugintest.ExtensionSpec{ID: "lh-plugin-ai-provider-2", Capabilities: []plugin.CapabilityID{plugin.CapabilityAIProvider}},
	)
}

func createAIProviderInstance(t *testing.T, application *App, extensionID string) string {
	t.Helper()
	created, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: extensionID, CapabilityID: string(plugin.CapabilityAIProvider),
	})
	if err != nil {
		t.Fatal(err)
	}
	return created.InstanceID
}

func TestCreatePluginInstanceRejectsSecondEnabledAIProviderOwner(t *testing.T) {
	application := newAIProviderApp(t)
	first := createAIProviderInstance(t, application, "lh-plugin-ai-provider")

	_, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-ai-provider-2", CapabilityID: string(plugin.CapabilityAIProvider),
	})
	if err == nil {
		t.Fatal("expected second enabled AI provider owner to be rejected")
	}
	if !strings.Contains(err.Error(), "only one AI provider instance can be in use") {
		t.Fatalf("unexpected error message: %v", err)
	}
	if states := persistedEnabled(t); len(states) != 1 || !states[first] {
		t.Fatalf("expected only the first AI provider persisted and enabled: %+v", states)
	}
}

func TestSetPluginInstanceEnabledRejectsSecondEnabledAIProviderOwner(t *testing.T) {
	application := newAIProviderApp(t)
	first := createAIProviderInstance(t, application, "lh-plugin-ai-provider")
	if _, err := application.SetPluginInstanceEnabled(first, false); err != nil {
		t.Fatal(err)
	}
	second := createAIProviderInstance(t, application, "lh-plugin-ai-provider-2")
	if _, err := application.SetPluginInstanceEnabled(second, false); err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetPluginInstanceEnabled(first, true); err != nil {
		t.Fatal(err)
	}

	if _, err := application.SetPluginInstanceEnabled(second, true); err == nil {
		t.Fatal("expected Use Again of a second AI provider owner to be rejected")
	}
	if states := persistedEnabled(t); !states[first] || states[second] {
		t.Fatalf("expected first enabled and second still disabled: %+v", states)
	}
}

func TestSetPluginInstanceEnabledAllowsAIProviderOwnerTransfer(t *testing.T) {
	application := newAIProviderApp(t)
	first := createAIProviderInstance(t, application, "lh-plugin-ai-provider")
	if _, err := application.SetPluginInstanceEnabled(first, false); err != nil {
		t.Fatal(err)
	}
	second := createAIProviderInstance(t, application, "lh-plugin-ai-provider-2")
	if _, err := application.SetPluginInstanceEnabled(second, false); err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetPluginInstanceEnabled(second, true); err != nil {
		t.Fatalf("expected enabling second AI provider after first is disabled to succeed: %v", err)
	}
	if states := persistedEnabled(t); states[first] || !states[second] {
		t.Fatalf("expected only second AI provider enabled: %+v", states)
	}
}

func TestSetPluginInstanceEnabledAllowsDisablingLegacyDuplicateAIProviderOwner(t *testing.T) {
	application := newAIProviderApp(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"legacy-ai-a", "legacy-ai-b"} {
		cfg.Plugins.Instances = append(cfg.Plugins.Instances, config.PluginInstanceConfig{
			ID: id, ExtensionID: "lh-plugin-ai-provider",
			CapabilityID: string(plugin.CapabilityAIProvider), Config: json.RawMessage(`{}`),
		})
	}
	if err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}

	if _, err := application.SetPluginInstanceEnabled("legacy-ai-b", false); err != nil {
		t.Fatalf("expected disabling one duplicate AI provider to succeed: %v", err)
	}
	if states := persistedEnabled(t); !states["legacy-ai-a"] || states["legacy-ai-b"] {
		t.Fatalf("expected only legacy-ai-a enabled: %+v", states)
	}
}
