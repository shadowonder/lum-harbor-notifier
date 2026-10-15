package app

import (
	"encoding/json"
	"strings"
	"testing"

	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/plugintest"
)

func TestUpdatePluginInstanceConfigSuccess(t *testing.T) {
	setupIsolatedConfigHome(t)
	application := newCreateInstanceApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
		Schema: plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{
			{Key: "endpoint", Label: "Endpoint", Type: plugin.ConfigString, Required: true},
			{Key: "limit", Label: "Limit", Type: plugin.ConfigNumber},
		}},
	})

	created, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
		Config: map[string]interface{}{"endpoint": "http://original.test", "limit": 10},
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := application.UpdatePluginInstanceConfig(UpdatePluginInstanceConfigRequestDTO{
		InstanceID: created.InstanceID,
		Config:     map[string]interface{}{"endpoint": "http://updated.test", "limit": 25},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.InstanceID != created.InstanceID || result.ExtensionID != "lh-plugin-base" ||
		result.CapabilityID != string(plugin.CapabilitySourceFetch) || !result.Enabled || !result.RestartRequired {
		t.Fatalf("unexpected update result: %+v", result)
	}

	// Verify persistence in config.json
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins.Instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(cfg.Plugins.Instances))
	}
	var stored map[string]interface{}
	if err := json.Unmarshal(cfg.Plugins.Instances[0].Config, &stored); err != nil {
		t.Fatal(err)
	}
	if stored["endpoint"] != "http://updated.test" || stored["limit"] != float64(25) {
		t.Fatalf("unexpected stored config: %+v", stored)
	}

	// Verify inventory read returns updated config
	inventory, err := application.GetPluginInventory()
	if err != nil {
		t.Fatal(err)
	}
	item := inventory.Plugins[0]
	if len(item.Instances) != 1 || item.Instances[0].Config["endpoint"] != "http://updated.test" {
		t.Fatalf("inventory projection missing updated config: %+v", item.Instances)
	}
}

func TestUpdatePluginInstanceConfigRejectsInvalidSchema(t *testing.T) {
	setupIsolatedConfigHome(t)
	application := newCreateInstanceApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
		Schema: plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{
			{Key: "limit", Label: "Limit", Type: plugin.ConfigNumber, Required: true},
		}},
	})

	created, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
		Config: map[string]interface{}{"limit": 10},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = application.UpdatePluginInstanceConfig(UpdatePluginInstanceConfigRequestDTO{
		InstanceID: created.InstanceID,
		Config:     map[string]interface{}{"limit": "not-a-number"},
	})
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("expected schema validation failure, got: %v", err)
	}
}

func TestUpdatePluginInstanceConfigRejectsSecretField(t *testing.T) {
	setupIsolatedConfigHome(t)
	application := newCreateInstanceApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
		Schema: plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{
			{Key: "token", Label: "Token", Type: plugin.ConfigSecret},
		}},
	})

	created, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = application.UpdatePluginInstanceConfig(UpdatePluginInstanceConfigRequestDTO{
		InstanceID: created.InstanceID,
		Config:     map[string]interface{}{"token": "secret-value"},
	})
	if err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("expected secret field rejection, got: %v", err)
	}
}

func TestUpdatePluginInstanceConfigRejectsUnknownInstance(t *testing.T) {
	setupIsolatedConfigHome(t)
	application := newCreateInstanceApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})

	_, err := application.UpdatePluginInstanceConfig(UpdatePluginInstanceConfigRequestDTO{
		InstanceID: "non-existent",
		Config:     map[string]interface{}{},
	})
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found error, got: %v", err)
	}
}
