package plugininstance_test

import (
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/appdir"
	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugininstance"
)

func TestResolveEmptyConfiguration(t *testing.T) {
	instances, err := plugininstance.Resolve(nil, registryWithCapabilities(t, plugin.CapabilitySourceFetch))
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 0 {
		t.Fatalf("resolved instances: %+v", instances)
	}
}

func TestResolveValidIndependentInstancesWithoutStartingHost(t *testing.T) {
	registry := registryWithCapabilities(t, plugin.CapabilitySourceFetch)
	host, err := plugin.NewHost(registry, plugin.HostOptions{
		StartupTimeout: time.Second, RequestTimeout: time.Second, ShutdownTimeout: time.Second,
		Logger: slog.New(slog.NewTextHandler(os.Stderr, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	before := host.Snapshot()
	declarations := []config.PluginInstanceConfig{
		{ID: "team-a", ExtensionID: "company-assistant", CapabilityID: "source.fetch", Config: json.RawMessage(`{"endpoint":"alpha","limit":3,"enabled":true}`)},
		{ID: "team-b", ExtensionID: "company-assistant", CapabilityID: "source.fetch", Config: json.RawMessage(`{"endpoint":"beta","limit":8,"enabled":false}`)},
	}
	instances, err := plugininstance.Resolve(declarations, registry)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 2 || instances[0].ID != "team-a" || instances[1].ID != "team-b" {
		t.Fatalf("resolved instances: %+v", instances)
	}
	declarations[0].Config[0] = '['
	if !json.Valid(instances[0].Config) || instances[0].Config[0] != '{' {
		t.Fatal("resolved instance config aliases declaration bytes")
	}
	after := host.Snapshot()
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("resolution changed Host snapshot: before=%+v after=%+v", before, after)
	}
	for _, extension := range after.Extensions {
		if extension.Running {
			t.Fatalf("resolution started extension: %+v", extension)
		}
	}
}

func TestResolveExcludesDisabledInstancesFromRuntimeResult(t *testing.T) {
	registry := registryWithCapabilities(t, plugin.CapabilitySourceFetch)
	declarations := []config.PluginInstanceConfig{
		{ID: "team-a", ExtensionID: "company-assistant", CapabilityID: "source.fetch", Config: json.RawMessage(`{"endpoint":"alpha"}`)},
		{ID: "team-b", ExtensionID: "company-assistant", CapabilityID: "source.fetch", Config: json.RawMessage(`{"endpoint":"beta"}`), Disabled: true},
	}
	instances, err := plugininstance.Resolve(declarations, registry)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 || instances[0].ID != "team-a" {
		t.Fatalf("expected only the enabled instance resolved, got %+v", instances)
	}
}

func TestResolveDisabledInstanceNeverBlocksStartupOnStaleRuntimeRequirements(t *testing.T) {
	registry := registryWithCapabilities(t, plugin.CapabilitySourceFetch)
	declarations := []config.PluginInstanceConfig{
		// A disabled declaration referencing a since-removed extension and an
		// invalid config document must not fail resolution: it is structurally
		// well-formed (non-empty id/extensionId/capabilityId) but is never
		// checked against the current Registry/schema while disabled.
		{ID: "stale", ExtensionID: "removed-extension", CapabilityID: "source.fetch", Config: json.RawMessage(`{"endpoint":false}`), Disabled: true},
	}
	instances, err := plugininstance.Resolve(declarations, registry)
	if err != nil {
		t.Fatalf("disabled instance with stale runtime requirements must not fail resolution: %v", err)
	}
	if len(instances) != 0 {
		t.Fatalf("expected no runtime instance for a disabled declaration, got %+v", instances)
	}
}

func TestResolveRejectsDuplicateIDAcrossEnabledAndDisabledDeclarations(t *testing.T) {
	registry := registryWithCapabilities(t, plugin.CapabilitySourceFetch)
	declarations := []config.PluginInstanceConfig{
		{ID: "shared-id", ExtensionID: "company-assistant", CapabilityID: "source.fetch", Config: json.RawMessage(`{"endpoint":"alpha"}`)},
		{ID: "shared-id", ExtensionID: "company-assistant", CapabilityID: "source.fetch", Config: json.RawMessage(`{"endpoint":"beta"}`), Disabled: true},
	}
	_, err := plugininstance.Resolve(declarations, registry)
	if err == nil || !strings.Contains(err.Error(), "duplicates") {
		t.Fatalf("expected duplicate ID rejection across enabled/disabled declarations, got %v", err)
	}
}

func TestResolveStillRejectsMalformedDisabledDeclaration(t *testing.T) {
	registry := registryWithCapabilities(t, plugin.CapabilitySourceFetch)
	tests := []struct {
		name         string
		declarations []config.PluginInstanceConfig
		contains     string
	}{
		{
			name:         "empty id",
			declarations: []config.PluginInstanceConfig{{ExtensionID: "company-assistant", CapabilityID: "source.fetch", Disabled: true}},
			contains:     ".id must not be empty",
		},
		{
			name:         "empty extension id",
			declarations: []config.PluginInstanceConfig{{ID: "disabled-source", CapabilityID: "source.fetch", Disabled: true}},
			contains:     ".extensionId must not be empty",
		},
		{
			name:         "empty capability id",
			declarations: []config.PluginInstanceConfig{{ID: "disabled-source", ExtensionID: "company-assistant", Disabled: true}},
			contains:     ".capabilityId must not be empty",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := plugininstance.Resolve(test.declarations, registry)
			if err == nil || !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("expected %q, got %v", test.contains, err)
			}
		})
	}
}

func TestResolveFailuresAreFieldQualified(t *testing.T) {
	tests := []struct {
		name         string
		registry     *plugin.Registry
		declarations []config.PluginInstanceConfig
		classified   error
		contains     string
	}{
		{
			name: "missing extension", registry: registryWithCapabilities(t, plugin.CapabilitySourceFetch),
			declarations: []config.PluginInstanceConfig{{ID: "source", ExtensionID: "missing", CapabilityID: "source.fetch", Config: json.RawMessage(`{"endpoint":"alpha"}`)}},
			classified:   plugin.ErrExtensionNotFound,
		},
		{
			name: "undeclared capability", registry: registryWithCapabilities(t, plugin.CapabilitySourceFetch),
			declarations: []config.PluginInstanceConfig{{ID: "source", ExtensionID: "company-assistant", CapabilityID: "ai.provider", Config: json.RawMessage(`{"endpoint":"alpha"}`)}},
			classified:   plugin.ErrCapabilityUnsupported,
		},
		{
			name: "host unsupported capability", registry: registryWithCapabilities(t, plugin.CapabilityID("future.capability")),
			declarations: []config.PluginInstanceConfig{{ID: "source", ExtensionID: "company-assistant", CapabilityID: "future.capability", Config: json.RawMessage(`{"endpoint":"alpha"}`)}},
			classified:   plugin.ErrCapabilityUnsupported,
		},
		{
			name: "duplicate id", registry: registryWithCapabilities(t, plugin.CapabilitySourceFetch),
			declarations: []config.PluginInstanceConfig{
				{ID: "source", ExtensionID: "company-assistant", CapabilityID: "source.fetch", Config: json.RawMessage(`{"endpoint":"alpha"}`)},
				{ID: "source", ExtensionID: "company-assistant", CapabilityID: "source.fetch", Config: json.RawMessage(`{"endpoint":"beta"}`)},
			},
			contains: "duplicates",
		},
		{
			name: "empty id", registry: registryWithCapabilities(t, plugin.CapabilitySourceFetch),
			declarations: []config.PluginInstanceConfig{{ExtensionID: "company-assistant", CapabilityID: "source.fetch", Config: json.RawMessage(`{"endpoint":"alpha"}`)}},
			contains:     ".id must not be empty",
		},
		{
			name: "invalid plugin config", registry: registryWithCapabilities(t, plugin.CapabilitySourceFetch),
			declarations: []config.PluginInstanceConfig{{ID: "source", ExtensionID: "company-assistant", CapabilityID: "source.fetch", Config: json.RawMessage(`{"endpoint":false}`)}},
			contains:     "configuration invalid",
		},
		{
			name: "persisted secret", registry: registryWithCapabilities(t, plugin.CapabilitySourceFetch),
			declarations: []config.PluginInstanceConfig{{ID: "source", ExtensionID: "company-assistant", CapabilityID: "source.fetch", Config: json.RawMessage(`{"endpoint":"alpha","token":"sensitive"}`)}},
			contains:     "requires dedicated secret storage",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := plugininstance.Resolve(test.declarations, test.registry)
			if err == nil || !strings.Contains(err.Error(), "plugins.instances[") {
				t.Fatalf("expected field-qualified error, got %v", err)
			}
			if test.classified != nil && !errors.Is(err, test.classified) {
				t.Fatalf("expected %v, got %v", test.classified, err)
			}
			if test.contains != "" && !strings.Contains(err.Error(), test.contains) {
				t.Fatalf("expected %q in %v", test.contains, err)
			}
		})
	}
}

func TestConfiguredInstanceSurvivesConfigReloadAndRediscovery(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	configDirectory, err := appdir.ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(configDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	contents := `{"plugins":{"instances":[{"id":"stable-source","extensionId":"company-assistant","capabilityId":"source.fetch","config":{"endpoint":"persistent","limit":5,"enabled":true}}]}}`
	if err := os.WriteFile(filepath.Join(configDirectory, "config.json"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	pluginRoot := t.TempDir()
	writeDiscoveredArtifact(t, pluginRoot)

	first := loadDiscoverResolve(t, pluginRoot)
	second := loadDiscoverResolve(t, pluginRoot)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("instance changed across reload: first=%+v second=%+v", first, second)
	}
	if len(second) != 1 || second[0].ID != "stable-source" || second[0].ExtensionID != "company-assistant" || second[0].CapabilityID != plugin.CapabilitySourceFetch {
		t.Fatalf("stable instance identity: %+v", second)
	}
}

func registryWithCapabilities(t *testing.T, capabilities ...plugin.CapabilityID) *plugin.Registry {
	t.Helper()
	declarations := make([]plugin.CapabilityDeclaration, len(capabilities))
	for index, capability := range capabilities {
		declarations[index] = plugin.CapabilityDeclaration{ID: capability}
	}
	registry, err := plugin.NewRegistry([]plugin.Extension{{Manifest: plugin.Manifest{
		ID: "company-assistant", Name: "Company Assistant", Version: "1.0.0",
		ManifestVersion: plugin.SupportedManifestVersion, ProtocolVersion: plugin.SupportedProtocolVersion,
		Capabilities: declarations,
		Configuration: plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{
			{Key: "endpoint", Label: "Endpoint", Type: plugin.ConfigString, Required: true},
			{Key: "limit", Label: "Limit", Type: plugin.ConfigNumber},
			{Key: "enabled", Label: "Enabled", Type: plugin.ConfigBoolean},
			{Key: "token", Label: "Token", Type: plugin.ConfigSecret},
		}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func writeDiscoveredArtifact(t *testing.T, root string) {
	t.Helper()
	directory := filepath.Join(root, "company-assistant")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := plugin.Manifest{
		ManifestVersion: plugin.SupportedManifestVersion,
		ID:              "company-assistant", Name: "Company Assistant", Version: "1.0.0",
		ProtocolVersion: plugin.SupportedProtocolVersion, Executable: "assistant",
		Capabilities: []plugin.CapabilityDeclaration{{ID: plugin.CapabilitySourceFetch}},
		Configuration: plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{
			{Key: "endpoint", Label: "Endpoint", Type: plugin.ConfigString, Required: true},
			{Key: "limit", Label: "Limit", Type: plugin.ConfigNumber},
			{Key: "enabled", Label: "Enabled", Type: plugin.ConfigBoolean},
		}},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, plugin.ManifestFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "assistant"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func loadDiscoverResolve(t *testing.T, pluginRoot string) []plugin.Instance {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	registry, failures, err := plugin.Discover(pluginRoot)
	if err != nil || len(failures) != 0 {
		t.Fatalf("discover: failures=%v err=%v", failures, err)
	}
	instances, err := plugininstance.Resolve(cfg.Plugins.Instances, registry)
	if err != nil {
		t.Fatal(err)
	}
	return instances
}
