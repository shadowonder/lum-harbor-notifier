package app

import (
	"strings"
	"sync"
	"testing"

	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/plugintest"
	"lum-harbor-notifier/internal/plugininstance"
)

func TestSetPluginInstanceEnabledDisablesAndRetainsIdentity(t *testing.T) {
	setupIsolatedConfigHome(t)
	application := newCreateInstanceApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})
	created, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := application.SetPluginInstanceEnabled(created.InstanceID, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.InstanceID != created.InstanceID || result.ExtensionID != "lh-plugin-base" ||
		result.CapabilityID != string(plugin.CapabilitySourceFetch) || result.Enabled || !result.RestartRequired {
		t.Fatalf("unexpected disable result: %+v", result)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins.Instances) != 1 {
		t.Fatalf("expected declaration retained, not removed: %+v", cfg.Plugins.Instances)
	}
	declaration := cfg.Plugins.Instances[0]
	if declaration.ID != created.InstanceID || declaration.ExtensionID != "lh-plugin-base" ||
		declaration.CapabilityID != string(plugin.CapabilitySourceFetch) || !declaration.Disabled {
		t.Fatalf("expected same identity with Disabled=true: %+v", declaration)
	}
}

func TestSetPluginInstanceEnabledDisabledDeclarationPersistsAcrossReload(t *testing.T) {
	setupIsolatedConfigHome(t)
	application := newCreateInstanceApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})
	created, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetPluginInstanceEnabled(created.InstanceID, false); err != nil {
		t.Fatal(err)
	}

	first, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	second, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !first.Plugins.Instances[0].Disabled || !second.Plugins.Instances[0].Disabled {
		t.Fatalf("expected disabled state stable across reload: first=%+v second=%+v",
			first.Plugins.Instances[0], second.Plugins.Instances[0])
	}
}

func TestSetPluginInstanceEnabledReEnableRestoresSameIdentityAndResolves(t *testing.T) {
	setupIsolatedConfigHome(t)
	root := t.TempDir()
	plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})
	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("discover: err=%v failures=%v", err, failures)
	}
	host := newInventoryHost(t, registry)
	reader, err := NewPluginInventoryReader(registry, nil, host)
	if err != nil {
		t.Fatal(err)
	}
	application := &App{pluginInventory: reader}

	created, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetPluginInstanceEnabled(created.InstanceID, false); err != nil {
		t.Fatal(err)
	}

	reEnableResult, err := application.SetPluginInstanceEnabled(created.InstanceID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !reEnableResult.Enabled || reEnableResult.InstanceID != created.InstanceID {
		t.Fatalf("unexpected re-enable result: %+v", reEnableResult)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins.Instances) != 1 || cfg.Plugins.Instances[0].Disabled {
		t.Fatalf("expected the same declaration re-enabled: %+v", cfg.Plugins.Instances)
	}
	if cfg.Plugins.Instances[0].ID != created.InstanceID {
		t.Fatalf("re-enable must not change instance identity: got %q want %q", cfg.Plugins.Instances[0].ID, created.InstanceID)
	}

	// Next-startup equivalent: fresh Discover + fresh Resolve must see it active again.
	registryAfterRestart, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("post-reenable discover: err=%v failures=%v", err, failures)
	}
	instances, err := plugininstance.Resolve(cfg.Plugins.Instances, registryAfterRestart)
	if err != nil {
		t.Fatal(err)
	}
	if len(instances) != 1 || instances[0].ID != created.InstanceID {
		t.Fatalf("expected re-enabled instance to resolve again: %+v", instances)
	}
}

func TestSetPluginInstanceEnabledReEnableFailsForStaleDeclarationAndStaysDisabled(t *testing.T) {
	setupIsolatedConfigHome(t)
	root := t.TempDir()
	plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})
	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("discover: err=%v failures=%v", err, failures)
	}
	host := newInventoryHost(t, registry)
	reader, err := NewPluginInventoryReader(registry, nil, host)
	if err != nil {
		t.Fatal(err)
	}
	application := &App{pluginInventory: reader}

	created, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetPluginInstanceEnabled(created.InstanceID, false); err != nil {
		t.Fatal(err)
	}

	// Simulate the extension having been removed from the plugin root while
	// this Instance was disabled: the same App still holds the old Registry
	// (as a real running process would until its next restart), so re-enable
	// must be validated against it and fail rather than silently succeeding.
	emptyRegistry, err := plugin.NewRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	staleReader, err := NewPluginInventoryReader(emptyRegistry, nil, host)
	if err != nil {
		t.Fatal(err)
	}
	staleApplication := &App{pluginInventory: staleReader}

	_, err = staleApplication.SetPluginInstanceEnabled(created.InstanceID, true)
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("expected re-enable to fail for a since-removed extension, got %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins.Instances) != 1 || !cfg.Plugins.Instances[0].Disabled {
		t.Fatalf("expected declaration to remain disabled after a failed re-enable: %+v", cfg.Plugins.Instances)
	}
}

func TestSetPluginInstanceEnabledPreservesUnrelatedConfigSections(t *testing.T) {
	setupIsolatedConfigHome(t)
	seeded := config.DefaultConfig()
	seeded.Dashboard.TodoDisplayLimit = 13
	if err := config.Save(seeded); err != nil {
		t.Fatal(err)
	}

	application := newCreateInstanceApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})
	created, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetPluginInstanceEnabled(created.InstanceID, false); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Dashboard.TodoDisplayLimit != 13 {
		t.Fatalf("unrelated config section was not preserved: %+v", cfg.Dashboard)
	}
}

func TestSetPluginInstanceEnabledRejectsUnknownInstance(t *testing.T) {
	setupIsolatedConfigHome(t)
	application := newCreateInstanceApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})
	if _, err := application.SetPluginInstanceEnabled("does-not-exist", false); err == nil {
		t.Fatal("expected unknown instance rejection")
	}
}

func TestSetPluginInstanceEnabledRejectsEmptyInstanceID(t *testing.T) {
	setupIsolatedConfigHome(t)
	application := newCreateInstanceApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})
	if _, err := application.SetPluginInstanceEnabled("   ", false); err == nil {
		t.Fatal("expected empty instance id rejection")
	}
}

func TestSetPluginInstanceEnabledRequiresInitializedInventory(t *testing.T) {
	application := &App{}
	if _, err := application.SetPluginInstanceEnabled("any", false); err == nil {
		t.Fatal("expected uninitialized inventory error")
	}
}

// TestPluginConfigMutationsShareOneMutexAcrossCreateAndToggle proves create
// and enable/disable mutations serialize through the same pluginConfigMu:
// running many of each concurrently against one extension must never lose a
// declaration or corrupt config.json.
func TestPluginConfigMutationsShareOneMutexAcrossCreateAndToggle(t *testing.T) {
	setupIsolatedConfigHome(t)
	application := newCreateInstanceApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})

	seed, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
	})
	if err != nil {
		t.Fatal(err)
	}

	const additionalCreations = 4
	var wg sync.WaitGroup
	for i := 0; i < additionalCreations; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
				ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
			}); err != nil {
				t.Errorf("unexpected concurrent create error: %v", err)
			}
		}()
	}
	wg.Add(2)
	go func() {
		defer wg.Done()
		if _, err := application.SetPluginInstanceEnabled(seed.InstanceID, false); err != nil {
			t.Errorf("unexpected concurrent disable error: %v", err)
		}
	}()
	go func() {
		defer wg.Done()
		if _, err := application.SetPluginInstanceEnabled(seed.InstanceID, true); err != nil {
			t.Errorf("unexpected concurrent enable error: %v", err)
		}
	}()
	wg.Wait()

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins.Instances) != additionalCreations+1 {
		t.Fatalf("expected %d persisted declarations with no lost updates, got %d: %+v",
			additionalCreations+1, len(cfg.Plugins.Instances), cfg.Plugins.Instances)
	}
	seenIDs := make(map[string]struct{}, len(cfg.Plugins.Instances))
	for _, declaration := range cfg.Plugins.Instances {
		if _, exists := seenIDs[declaration.ID]; exists {
			t.Fatalf("duplicate instance id persisted: %s", declaration.ID)
		}
		seenIDs[declaration.ID] = struct{}{}
	}
}
