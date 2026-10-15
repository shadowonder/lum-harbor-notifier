package app

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/plugintest"
	"lum-harbor-notifier/internal/plugininstance"
)

// setupIsolatedConfigHome points config.Load/config.Save at a throwaway
// per-test HOME so tests never touch the real user configuration directory.
func setupIsolatedConfigHome(t *testing.T) {
	t.Helper()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))
}

func newCreateInstanceApp(t *testing.T, extensions ...plugintest.ExtensionSpec) *App {
	t.Helper()
	root := t.TempDir()
	for _, spec := range extensions {
		plugintest.WriteExtension(t, root, spec)
	}
	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("discover fixtures: err=%v failures=%v", err, failures)
	}
	host := newInventoryHost(t, registry)
	reader, err := NewPluginInventoryReader(registry, nil, host)
	if err != nil {
		t.Fatal(err)
	}
	return &App{pluginInventory: reader}
}

func assertNoPersistedInstances(t *testing.T) {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins.Instances) != 0 {
		t.Fatalf("expected config unchanged, found instances: %+v", cfg.Plugins.Instances)
	}
}

func TestCreatePluginInstancePersistsValidDeclaration(t *testing.T) {
	setupIsolatedConfigHome(t)
	application := newCreateInstanceApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})

	result, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ExtensionID != "lh-plugin-base" || result.CapabilityID != string(plugin.CapabilitySourceFetch) || !result.RestartRequired {
		t.Fatalf("unexpected result: %+v", result)
	}
	if !strings.HasPrefix(result.InstanceID, "lh-plugin-base-") {
		t.Fatalf("expected stable extension-derived instance id, got %q", result.InstanceID)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins.Instances) != 1 || cfg.Plugins.Instances[0].ID != result.InstanceID {
		t.Fatalf("declaration not persisted: %+v", cfg.Plugins.Instances)
	}
}

// TestCreatePluginInstanceDeclarationResolvesOnNextLifetimeDiscovery proves the
// persisted declaration survives a simulated restart: fresh Discover + fresh
// config.Load + the same plugininstance.Resolve production startup uses.
func TestCreatePluginInstanceDeclarationResolvesOnNextLifetimeDiscovery(t *testing.T) {
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

	result, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
	})
	if err != nil {
		t.Fatal(err)
	}

	registryAfterRestart, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("post-create discover: err=%v failures=%v", err, failures)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	instances, err := plugininstance.Resolve(cfg.Plugins.Instances, registryAfterRestart)
	if err != nil {
		t.Fatalf("resolve persisted declaration on next lifetime: %v", err)
	}
	if len(instances) != 1 || instances[0].ID != result.InstanceID || instances[0].ExtensionID != "lh-plugin-base" {
		t.Fatalf("resolved instance mismatch: %+v", instances)
	}
}

func TestCreatePluginInstanceRejectsUnknownExtension(t *testing.T) {
	setupIsolatedConfigHome(t)
	application := newCreateInstanceApp(t)

	_, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "does-not-exist", CapabilityID: string(plugin.CapabilitySourceFetch),
	})
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("expected unknown extension rejection, got %v", err)
	}
	assertNoPersistedInstances(t)
}

func TestCreatePluginInstanceRejectsUndeclaredCapability(t *testing.T) {
	setupIsolatedConfigHome(t)
	application := newCreateInstanceApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})

	_, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: "custom.undeclared",
	})
	if err == nil || !strings.Contains(err.Error(), "capability") {
		t.Fatalf("expected undeclared capability rejection, got %v", err)
	}
	assertNoPersistedInstances(t)
}

func TestCreatePluginInstanceRejectsHostUnsupportedDeclaredCapability(t *testing.T) {
	setupIsolatedConfigHome(t)
	application := newCreateInstanceApp(t, plugintest.ExtensionSpec{
		ID:           "multi-capability",
		Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch, plugin.CapabilityID("custom.unsupported")},
	})

	_, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "multi-capability", CapabilityID: "custom.unsupported",
	})
	if err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("expected host-unsupported capability rejection, got %v", err)
	}
	assertNoPersistedInstances(t)
}

func TestCreatePluginInstanceRejectsInvalidNonSecretConfig(t *testing.T) {
	setupIsolatedConfigHome(t)
	application := newCreateInstanceApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
		Schema: plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{
			{Key: "limit", Label: "Limit", Type: plugin.ConfigNumber, Required: true},
		}},
	})

	_, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
		Config: map[string]interface{}{"limit": "not-a-number"},
	})
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("expected invalid config rejection, got %v", err)
	}
	assertNoPersistedInstances(t)
}

func TestCreatePluginInstanceRejectsSecretConfigValue(t *testing.T) {
	setupIsolatedConfigHome(t)
	application := newCreateInstanceApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
		Schema: plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{
			{Key: "token", Label: "Token", Type: plugin.ConfigSecret},
		}},
	})

	_, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
		Config: map[string]interface{}{"token": "super-secret-value"},
	})
	if err == nil || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("expected secret rejection, got %v", err)
	}
	assertNoPersistedInstances(t)
}

func TestCreatePluginInstanceRejectsRequiredSecretSchema(t *testing.T) {
	setupIsolatedConfigHome(t)
	application := newCreateInstanceApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
		Schema: plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{
			{Key: "token", Label: "Token", Type: plugin.ConfigSecret, Required: true},
		}},
	})

	_, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
	})
	if err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("expected required secret field to block creation, got %v", err)
	}
	assertNoPersistedInstances(t)
}

func TestCreatePluginInstancePreservesUnrelatedConfigSections(t *testing.T) {
	setupIsolatedConfigHome(t)
	seeded := config.DefaultConfig()
	seeded.Dashboard.TodoDisplayLimit = 11
	if err := config.Save(seeded); err != nil {
		t.Fatal(err)
	}

	application := newCreateInstanceApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})
	if _, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
	}); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Dashboard.TodoDisplayLimit != 11 {
		t.Fatalf("unrelated config section was not preserved: %+v", cfg.Dashboard)
	}
}

func TestCreatePluginInstanceFailureLeavesConfigIntact(t *testing.T) {
	setupIsolatedConfigHome(t)
	seeded := config.DefaultConfig()
	seeded.Plugins.Instances = []config.PluginInstanceConfig{
		{ID: "existing", ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch)},
	}
	if err := config.Save(seeded); err != nil {
		t.Fatal(err)
	}

	application := newCreateInstanceApp(t)
	if _, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "does-not-exist", CapabilityID: string(plugin.CapabilitySourceFetch),
	}); err == nil {
		t.Fatal("expected rejection")
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins.Instances) != 1 || cfg.Plugins.Instances[0].ID != "existing" {
		t.Fatalf("original config corrupted after failed create attempt: %+v", cfg.Plugins.Instances)
	}
}

// TestCreatePluginInstanceReportsPreExistingBrokenDeclarationHonestly proves a
// stale declaration left over from outside this flow (e.g. config.json edited
// externally, or an extension removed by other means) is never misreported as
// a problem with the user's current, unrelated submission.
func TestCreatePluginInstanceReportsPreExistingBrokenDeclarationHonestly(t *testing.T) {
	setupIsolatedConfigHome(t)
	seeded := config.DefaultConfig()
	seeded.Plugins.Instances = []config.PluginInstanceConfig{
		{ID: "stale", ExtensionID: "removed-extension", CapabilityID: string(plugin.CapabilitySourceFetch)},
	}
	if err := config.Save(seeded); err != nil {
		t.Fatal(err)
	}

	application := newCreateInstanceApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})
	_, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
	})
	if err == nil || !strings.Contains(err.Error(), "a different plugin instance's configuration is invalid") {
		t.Fatalf("expected honest pre-existing-declaration error, got %v", err)
	}
}

func TestCreatePluginInstanceRejectsEmptyInput(t *testing.T) {
	setupIsolatedConfigHome(t)
	application := newCreateInstanceApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})

	if _, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		CapabilityID: string(plugin.CapabilitySourceFetch),
	}); err == nil {
		t.Fatal("expected empty extension id rejection")
	}
	if _, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base",
	}); err == nil {
		t.Fatal("expected empty capability id rejection")
	}
	assertNoPersistedInstances(t)
}

func TestCreatePluginInstanceRequiresInitializedInventory(t *testing.T) {
	setupIsolatedConfigHome(t)
	application := &App{}
	if _, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
	}); err == nil {
		t.Fatal("expected uninitialized inventory error")
	}
}

// TestCreatePluginInstanceConcurrentCreationsNeverCollideOrLoseDeclarations
// proves the load-validate-save sequence is safe against near-simultaneous
// submissions: every generated instance ID is distinct and every declaration
// survives the final save (no last-writer-wins data loss).
func TestCreatePluginInstanceConcurrentCreationsNeverCollideOrLoseDeclarations(t *testing.T) {
	setupIsolatedConfigHome(t)
	application := newCreateInstanceApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})

	const attempts = 5
	results := make(chan PluginInstanceCreationResultDTO, attempts)
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
				ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
			})
			if err != nil {
				t.Errorf("unexpected concurrent creation error: %v", err)
				return
			}
			results <- result
		}()
	}
	wg.Wait()
	close(results)

	seen := make(map[string]struct{})
	for result := range results {
		if _, exists := seen[result.InstanceID]; exists {
			t.Fatalf("duplicate instance id generated: %s", result.InstanceID)
		}
		seen[result.InstanceID] = struct{}{}
	}
	if len(seen) != attempts {
		t.Fatalf("expected %d distinct instances, got %d", attempts, len(seen))
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins.Instances) != attempts {
		t.Fatalf("expected %d persisted declarations with no lost updates, got %d: %+v", attempts, len(cfg.Plugins.Instances), cfg.Plugins.Instances)
	}
}
