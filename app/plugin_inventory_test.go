package app

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/plugintest"
)

type notificationInventoryRuntime struct {
	extensionID string
	result      json.RawMessage
	err         error
	calls       int
}

func (r *notificationInventoryRuntime) Snapshot() plugin.RuntimeSnapshot {
	return plugin.RuntimeSnapshot{Extensions: []plugin.ExtensionRuntimeSnapshot{{
		ID: r.extensionID, Status: "RUNNING", Running: true,
	}}}
}

func (r *notificationInventoryRuntime) Invoke(_ context.Context, invocation plugin.Invocation) (json.RawMessage, error) {
	r.calls++
	if invocation.CapabilityID != plugin.CapabilityNotificationDeliver {
		return nil, errors.New("unexpected capability")
	}
	return r.result, r.err
}

func TestPluginFixtureProcess(t *testing.T) {
	if plugintest.RunIfRequested() {
		return
	}
}

func TestPluginInventoryEmptyRegistry(t *testing.T) {
	registry, err := plugin.NewRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	host := newInventoryHost(t, registry)
	reader, err := NewPluginInventoryReader(registry, nil, host)
	if err != nil {
		t.Fatal(err)
	}
	if inventory := reader.Read(); inventory.Plugins == nil || len(inventory.Plugins) != 0 {
		t.Fatalf("empty inventory: %+v", inventory)
	}
}

func TestPluginInventoryProjectsInstalledSelectedRunningAndInstances(t *testing.T) {
	root := t.TempDir()
	plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
		ID: "selected-source", Name: "Selected Source", Version: "2.3.4",
		Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
		Bindings: []plugin.BindingDeclaration{{
			ID: "poll", Type: plugin.BindingSchedule, Capability: plugin.CapabilitySourceFetch,
			DefaultIntervalSeconds: 300,
		}},
	})
	plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
		ID: "unselected-ai", Name: "Unselected AI",
		Capabilities: []plugin.CapabilityID{plugin.CapabilityAIProvider},
	})
	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("discover fixtures: err=%v failures=%v", err, failures)
	}
	host := newInventoryHost(t, registry)
	if err := host.Start(context.Background(), "selected-source"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
	declarations := []config.PluginInstanceConfig{
		{ID: "team-a", ExtensionID: "selected-source", CapabilityID: "source.fetch", Config: json.RawMessage(`{"token":"super-secret-a"}`)},
		{ID: "team-b", ExtensionID: "selected-source", CapabilityID: "source.fetch", Config: json.RawMessage(`{"token":"super-secret-b"}`)},
	}
	reader, err := NewPluginInventoryReader(registry, declarations, host)
	if err != nil {
		t.Fatal(err)
	}
	inventory := reader.Read()
	if len(inventory.Plugins) != 2 {
		t.Fatalf("plugins: %+v", inventory.Plugins)
	}
	selected := inventory.Plugins[0]
	if selected.ID != "selected-source" || !selected.Installed || !selected.Selected || !selected.Running || selected.RuntimeStatus != "RUNNING" {
		t.Fatalf("selected extension facts: %+v", selected)
	}
	if selected.Version != "2.3.4" || selected.ProtocolVersion != plugin.SupportedProtocolVersion || len(selected.Capabilities) != 1 || selected.Capabilities[0] != string(plugin.CapabilitySourceFetch) {
		t.Fatalf("selected extension metadata: %+v", selected)
	}
	if len(selected.Instances) != 2 || selected.Instances[0].ID != "team-a" || selected.Instances[1].ID != "team-b" {
		t.Fatalf("configured instances: %+v", selected.Instances)
	}
	if !selected.Instances[0].Enabled || !selected.Instances[1].Enabled {
		t.Fatalf("expected declarations without Disabled to project as enabled: %+v", selected.Instances)
	}
	unselected := inventory.Plugins[1]
	if unselected.ID != "unselected-ai" || !unselected.Installed || unselected.Selected || unselected.Running || unselected.RuntimeStatus != "DISCOVERED" || len(unselected.Instances) != 0 {
		t.Fatalf("unselected extension facts: %+v", unselected)
	}
	// A SCHEDULE binding declares no Actions (manifest.go's
	// validateBindingDeclaration requires it), so the source manifest.Actions
	// is nil. Regression coverage for the real bug this fixture exposed:
	// the frontend calls binding.actions.length unconditionally, so this
	// field must serialize as JSON "[]", never "null".
	if len(selected.Bindings) != 1 || selected.Bindings[0].Actions == nil || len(selected.Bindings[0].Actions) != 0 {
		t.Fatalf("selected extension SCHEDULE binding actions: %+v", selected.Bindings)
	}
	encoded, err := json.Marshal(inventory)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"actions":null`) {
		t.Fatalf("inventory serialized a null actions field, which crashes the frontend's binding.actions.length: %s", encoded)
	}
	for _, forbidden := range []string{"super-secret", "token", "Config", "ExecutablePath"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("inventory exposed %q: %s", forbidden, encoded)
		}
	}
}

func TestPluginInventoryDisabledInstanceRemainsVisibleAndUnselected(t *testing.T) {
	root := t.TempDir()
	plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
		ID: "only-disabled", Name: "Only Disabled", Version: "1.0.0",
		Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})
	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("discover fixtures: err=%v failures=%v", err, failures)
	}
	host := newInventoryHost(t, registry)
	declarations := []config.PluginInstanceConfig{
		{ID: "stopped-team", ExtensionID: "only-disabled", CapabilityID: "source.fetch", Disabled: true},
	}
	reader, err := NewPluginInventoryReader(registry, declarations, host)
	if err != nil {
		t.Fatal(err)
	}
	inventory := reader.Read()
	if len(inventory.Plugins) != 1 {
		t.Fatalf("plugins: %+v", inventory.Plugins)
	}
	item := inventory.Plugins[0]
	if item.Selected {
		t.Fatalf("expected extension with only disabled instances to be unselected: %+v", item)
	}
	if len(item.Instances) != 1 || item.Instances[0].ID != "stopped-team" || item.Instances[0].Enabled {
		t.Fatalf("expected the disabled declaration to remain visible with Enabled=false: %+v", item.Instances)
	}
}

func TestPluginInventoryExtensionSelectedWhenAtLeastOneInstanceEnabled(t *testing.T) {
	root := t.TempDir()
	plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
		ID: "mixed-instances", Name: "Mixed Instances", Version: "1.0.0",
		Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})
	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("discover fixtures: err=%v failures=%v", err, failures)
	}
	host := newInventoryHost(t, registry)
	if err := host.Start(context.Background(), "mixed-instances"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
	declarations := []config.PluginInstanceConfig{
		{ID: "enabled-team", ExtensionID: "mixed-instances", CapabilityID: "source.fetch"},
		{ID: "disabled-team", ExtensionID: "mixed-instances", CapabilityID: "source.fetch", Disabled: true},
	}
	reader, err := NewPluginInventoryReader(registry, declarations, host)
	if err != nil {
		t.Fatal(err)
	}
	inventory := reader.Read()
	if len(inventory.Plugins) != 1 {
		t.Fatalf("plugins: %+v", inventory.Plugins)
	}
	item := inventory.Plugins[0]
	if !item.Selected {
		t.Fatalf("expected extension with at least one enabled instance to be selected: %+v", item)
	}
	if !item.Running {
		t.Fatalf("expected extension process running fact from Host snapshot: %+v", item)
	}
	if len(item.Instances) != 2 {
		t.Fatalf("expected both declarations to remain visible: %+v", item.Instances)
	}
	byID := map[string]ConfiguredPluginInstanceDTO{}
	for _, instance := range item.Instances {
		byID[instance.ID] = instance
	}
	if !byID["enabled-team"].Enabled {
		t.Fatalf("expected enabled-team to be enabled: %+v", byID["enabled-team"])
	}
	if byID["disabled-team"].Enabled {
		t.Fatalf("expected disabled-team to remain disabled: %+v", byID["disabled-team"])
	}
	// The DTO intentionally exposes no per-instance running state: Host is a
	// per-extension process, not a per-instance one. Only Enabled/Disabled is
	// an instance fact; Running is an extension-level fact.
}

func TestPluginInventoryProjectsConfigSchemaAndSelectableCapabilities(t *testing.T) {
	root := t.TempDir()
	plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
		ID: "configurable-source", Name: "Configurable Source", Version: "1.0.0",
		Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch, plugin.CapabilityID("custom.unsupported")},
		Schema: plugin.PluginConfigSchema{
			Version: 1,
			Fields: []plugin.PluginConfigField{
				{Key: "endpoint", Label: "Endpoint", Type: plugin.ConfigString, Required: true, Default: json.RawMessage(`"https://example.test"`)},
				{Key: "token", Label: "Token", Type: plugin.ConfigSecret, Required: true},
			},
		},
	})
	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("discover fixtures: err=%v failures=%v", err, failures)
	}
	host := newInventoryHost(t, registry)
	reader, err := NewPluginInventoryReader(registry, nil, host)
	if err != nil {
		t.Fatal(err)
	}
	inventory := reader.Read()
	if len(inventory.Plugins) != 1 {
		t.Fatalf("plugins: %+v", inventory.Plugins)
	}
	item := inventory.Plugins[0]

	if len(item.SelectableCapabilities) != 1 || item.SelectableCapabilities[0] != string(plugin.CapabilitySourceFetch) {
		t.Fatalf("expected only host-supported capability selectable, got %+v", item.SelectableCapabilities)
	}
	if len(item.Capabilities) != 2 {
		t.Fatalf("expected both declared capabilities still listed, got %+v", item.Capabilities)
	}

	if len(item.ConfigSchema) != 2 {
		t.Fatalf("expected both schema fields projected, got %+v", item.ConfigSchema)
	}
	var endpoint, token *PluginConfigFieldDTO
	for index := range item.ConfigSchema {
		field := &item.ConfigSchema[index]
		switch field.Key {
		case "endpoint":
			endpoint = field
		case "token":
			token = field
		}
	}
	if endpoint == nil || endpoint.Type != "string" || !endpoint.Required || endpoint.Default != "https://example.test" {
		t.Fatalf("endpoint field projection: %+v", endpoint)
	}
	if token == nil || token.Type != "secret" || !token.Required || token.Default != nil {
		t.Fatalf("token field projection: %+v", token)
	}

	encoded, err := json.Marshal(inventory)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "example.test") == false {
		t.Fatalf("expected non-secret default to be visible in schema projection: %s", encoded)
	}
}

func TestPluginInventoryProjectsNativeNotificationPermissionOnEnabledRunningInstance(t *testing.T) {
	root := t.TempDir()
	plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
		ID: "lh-plugin-macos-notifications", Name: "Apple macOS Notification Bridge",
		Capabilities: []plugin.CapabilityID{plugin.CapabilityNotificationDeliver},
	})
	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("discover fixture: err=%v failures=%v", err, failures)
	}
	declarations := []config.PluginInstanceConfig{{
		ID: "native", ExtensionID: "lh-plugin-macos-notifications", CapabilityID: string(plugin.CapabilityNotificationDeliver),
	}}

	for _, tc := range []struct {
		name   string
		result json.RawMessage
		err    error
		want   string
	}{
		{name: "denied", result: json.RawMessage(`{"authorizationStatus":"DENIED"}`), want: "DENIED"},
		{name: "authorized", result: json.RawMessage(`{"authorizationStatus":"AUTHORIZED"}`), want: "AUTHORIZED"},
		{name: "unknown", result: json.RawMessage(`{"authorizationStatus":"UNKNOWN"}`), want: "UNKNOWN"},
		{name: "check failure", err: errors.New("authorization_check_failed"), want: "CHECK_FAILED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := &notificationInventoryRuntime{
				extensionID: "lh-plugin-macos-notifications", result: tc.result, err: tc.err,
			}
			reader, err := NewPluginInventoryReader(registry, declarations, runtime)
			if err != nil {
				t.Fatal(err)
			}
			inventory := reader.Read()
			got := inventory.Plugins[0].Instances[0].NativeNotificationAuthorization
			if got != tc.want {
				t.Fatalf("authorization status = %q, want %q", got, tc.want)
			}
			if runtime.calls != 1 {
				t.Fatalf("readiness calls = %d, want 1", runtime.calls)
			}
		})
	}
}

func TestPluginInventoryDoesNotCheckNativePermissionForDisabledInstance(t *testing.T) {
	root := t.TempDir()
	plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
		ID: "lh-plugin-macos-notifications", Name: "Apple macOS Notification Bridge",
		Capabilities: []plugin.CapabilityID{plugin.CapabilityNotificationDeliver},
	})
	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("discover fixture: err=%v failures=%v", err, failures)
	}
	runtime := &notificationInventoryRuntime{
		extensionID: "lh-plugin-macos-notifications", result: json.RawMessage(`{"authorizationStatus":"DENIED"}`),
	}
	reader, err := NewPluginInventoryReader(registry, []config.PluginInstanceConfig{{
		ID: "native", ExtensionID: "lh-plugin-macos-notifications",
		CapabilityID: string(plugin.CapabilityNotificationDeliver), Disabled: true,
	}}, runtime)
	if err != nil {
		t.Fatal(err)
	}
	instance := reader.Read().Plugins[0].Instances[0]
	if instance.NativeNotificationAuthorization != "" || runtime.calls != 0 {
		t.Fatalf("disabled instance unexpectedly checked permission: instance=%+v calls=%d", instance, runtime.calls)
	}
}

func TestPluginInventoryConstructionRequiresTruthSources(t *testing.T) {
	registry, _ := plugin.NewRegistry(nil)
	if _, err := NewPluginInventoryReader(nil, nil, nil); err == nil {
		t.Fatal("expected nil Registry rejection")
	}
	if _, err := NewPluginInventoryReader(registry, nil, nil); err == nil {
		t.Fatal("expected nil runtime rejection")
	}
	application := &App{}
	if _, err := application.GetPluginInventory(); err == nil {
		t.Fatal("expected uninitialized inventory error")
	}
}

func newInventoryHost(t *testing.T, registry *plugin.Registry) *plugin.Host {
	t.Helper()
	host, err := plugin.NewHost(registry, plugin.HostOptions{
		StartupTimeout: 5 * time.Second, RequestTimeout: time.Second, ShutdownTimeout: time.Second,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	return host
}

// TestPluginInventoryProjectsDesiredDeclarationsBeforeRestart proves Settings
// shows desired Enabled/Disabled truth immediately after a Plugin mutation
// while process state stays current-lifetime (M19 R3 GUI acceptance defect:
// the projection was frozen at startup, so Add Instance / Stop Using / Use
// Again were invisible until restart).
func TestPluginInventoryProjectsDesiredDeclarationsBeforeRestart(t *testing.T) {
	setupIsolatedConfigHome(t)
	application, _ := newRemovePluginApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})
	readOnly := func() PluginInventoryItemDTO {
		t.Helper()
		inventory, err := application.GetPluginInventory()
		if err != nil {
			t.Fatal(err)
		}
		if len(inventory.Plugins) != 1 {
			t.Fatalf("expected one discovered plugin: %+v", inventory.Plugins)
		}
		return inventory.Plugins[0]
	}

	created, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
	})
	if err != nil {
		t.Fatal(err)
	}
	item := readOnly()
	if len(item.Instances) != 1 || item.Instances[0].ID != created.InstanceID || !item.Instances[0].Enabled ||
		!item.Selected || item.Running {
		t.Fatalf("expected desired Enabled instance with process stopped after create: %+v", item)
	}

	if _, err := application.SetPluginInstanceEnabled(created.InstanceID, false); err != nil {
		t.Fatal(err)
	}
	item = readOnly()
	if len(item.Instances) != 1 || item.Instances[0].ID != created.InstanceID || item.Instances[0].Enabled || item.Selected {
		t.Fatalf("expected same instance Disabled after Stop Using: %+v", item)
	}

	if _, err := application.SetPluginInstanceEnabled(created.InstanceID, true); err != nil {
		t.Fatal(err)
	}
	item = readOnly()
	if len(item.Instances) != 1 || item.Instances[0].ID != created.InstanceID || !item.Instances[0].Enabled {
		t.Fatalf("expected same instance Enabled after Use Again: %+v", item)
	}

	if _, err := application.SetPluginInstanceEnabled(created.InstanceID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := application.RemovePlugin("lh-plugin-base"); err != nil {
		t.Fatal(err)
	}
	item = readOnly()
	if item.ID != "lh-plugin-base" || len(item.Instances) != 0 || item.Selected {
		t.Fatalf("expected stale startup Registry entry with no declarations after Remove: %+v", item)
	}
}
