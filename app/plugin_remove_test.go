package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"lum-harbor-notifier/internal/appdir"
	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/plugintest"
	"lum-harbor-notifier/internal/plugininstall"
)

func newRemovePluginApp(t *testing.T, extensions ...plugintest.ExtensionSpec) (*App, string) {
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
	installer, err := plugininstall.New(root)
	if err != nil {
		t.Fatal(err)
	}
	return &App{pluginInventory: reader, pluginInstaller: installer}, root
}

func TestRemovePluginSucceedsForInstalledExtensionWithNoDeclarations(t *testing.T) {
	setupIsolatedConfigHome(t)
	application, root := newRemovePluginApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})

	result, err := application.RemovePlugin("lh-plugin-base")
	if err != nil {
		t.Fatal(err)
	}
	if result.ExtensionID != "lh-plugin-base" || !result.RestartRequired {
		t.Fatalf("unexpected result: %+v", result)
	}
	if _, statErr := os.Stat(filepath.Join(root, "lh-plugin-base")); !os.IsNotExist(statErr) {
		t.Fatalf("expected artifact removed, stat err: %v", statErr)
	}
}

func TestRemovePluginSucceedsAndRemovesOnlyDisabledDeclarations(t *testing.T) {
	setupIsolatedConfigHome(t)
	application, root := newRemovePluginApp(t, plugintest.ExtensionSpec{
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

	if _, err := application.RemovePlugin("lh-plugin-base"); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins.Instances) != 0 {
		t.Fatalf("expected disabled declaration removed: %+v", cfg.Plugins.Instances)
	}
	if _, statErr := os.Stat(filepath.Join(root, "lh-plugin-base")); !os.IsNotExist(statErr) {
		t.Fatalf("expected artifact removed: %v", statErr)
	}
}

func TestRemovePluginRejectsWhenEnabledDeclarationExists(t *testing.T) {
	setupIsolatedConfigHome(t)
	application, root := newRemovePluginApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})
	if _, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
	}); err != nil {
		t.Fatal(err)
	}

	_, err := application.RemovePlugin("lh-plugin-base")
	if err == nil || !strings.Contains(err.Error(), "stop using") {
		t.Fatalf("expected enabled-declaration rejection, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "lh-plugin-base")); statErr != nil {
		t.Fatalf("expected artifact preserved: %v", statErr)
	}
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins.Instances) != 1 {
		t.Fatalf("expected declaration preserved: %+v", cfg.Plugins.Instances)
	}
}

// TestRemovePluginRejectsWhenHostProcessStillRunningDespiteAllDisabled is the
// key regression case: Stop Using only changes next-start desired config, it
// never live-stops the Host process, so a process started earlier this
// session must still block Remove even though every declaration is disabled.
func TestRemovePluginRejectsWhenHostProcessStillRunningDespiteAllDisabled(t *testing.T) {
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
	if err := host.Start(context.Background(), "lh-plugin-base"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })

	reader, err := NewPluginInventoryReader(registry, nil, host)
	if err != nil {
		t.Fatal(err)
	}
	installer, err := plugininstall.New(root)
	if err != nil {
		t.Fatal(err)
	}
	application := &App{pluginInventory: reader, pluginInstaller: installer}

	created, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetPluginInstanceEnabled(created.InstanceID, false); err != nil {
		t.Fatal(err)
	}

	_, err = application.RemovePlugin("lh-plugin-base")
	if err == nil || !strings.Contains(err.Error(), "restart Lum Harbor") {
		t.Fatalf("expected host-still-running rejection, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "lh-plugin-base")); statErr != nil {
		t.Fatalf("expected artifact preserved: %v", statErr)
	}
}

func TestRemovePluginRemovesAllDisabledDeclarationsForSameExtension(t *testing.T) {
	setupIsolatedConfigHome(t)
	application, _ := newRemovePluginApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})
	createdA, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
	})
	if err != nil {
		t.Fatal(err)
	}
	createdB, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetPluginInstanceEnabled(createdA.InstanceID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetPluginInstanceEnabled(createdB.InstanceID, false); err != nil {
		t.Fatal(err)
	}

	if _, err := application.RemovePlugin("lh-plugin-base"); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins.Instances) != 0 {
		t.Fatalf("expected both disabled declarations removed: %+v", cfg.Plugins.Instances)
	}
}

func TestRemovePluginPreservesDeclarationsForOtherExtensions(t *testing.T) {
	setupIsolatedConfigHome(t)
	application, root := newRemovePluginApp(t,
		plugintest.ExtensionSpec{ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch}},
		plugintest.ExtensionSpec{ID: "other-extension", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch}},
	)
	target, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
	})
	if err != nil {
		t.Fatal(err)
	}
	other, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "other-extension", CapabilityID: string(plugin.CapabilitySourceFetch),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetPluginInstanceEnabled(target.InstanceID, false); err != nil {
		t.Fatal(err)
	}

	if _, err := application.RemovePlugin("lh-plugin-base"); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins.Instances) != 1 || cfg.Plugins.Instances[0].ID != other.InstanceID {
		t.Fatalf("expected only the other extension's declaration preserved: %+v", cfg.Plugins.Instances)
	}
	if _, statErr := os.Stat(filepath.Join(root, "other-extension")); statErr != nil {
		t.Fatalf("expected other extension's artifact preserved: %v", statErr)
	}
}

func TestRemovePluginPreservesUnrelatedConfigSections(t *testing.T) {
	setupIsolatedConfigHome(t)
	seeded := config.DefaultConfig()
	seeded.Dashboard.TodoDisplayLimit = 14
	if err := config.Save(seeded); err != nil {
		t.Fatal(err)
	}

	application, _ := newRemovePluginApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})
	if _, err := application.RemovePlugin("lh-plugin-base"); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Dashboard.TodoDisplayLimit != 14 {
		t.Fatalf("unrelated config section was not preserved: %+v", cfg.Dashboard)
	}
}

func TestRemovePluginRejectsUnknownExtension(t *testing.T) {
	setupIsolatedConfigHome(t)
	application, root := newRemovePluginApp(t)

	_, err := application.RemovePlugin("does-not-exist")
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("expected unknown extension rejection, got %v", err)
	}
	entries, statErr := os.ReadDir(root)
	if statErr != nil && !os.IsNotExist(statErr) {
		t.Fatal(statErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no filesystem mutation for unknown extension, found: %v", entries)
	}
}

func TestRemovePluginNotFoundByFreshDiscoverAfterSuccess(t *testing.T) {
	setupIsolatedConfigHome(t)
	application, root := newRemovePluginApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})
	if _, err := application.RemovePlugin("lh-plugin-base"); err != nil {
		t.Fatal(err)
	}

	registryAfterRestart, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("post-remove discover: err=%v failures=%v", err, failures)
	}
	for _, extension := range registryAfterRestart.List() {
		if extension.Manifest.ID == "lh-plugin-base" {
			t.Fatalf("expected removed extension absent from a fresh Discover: %+v", extension)
		}
	}
}

func TestRemovePluginLeavesCurrentStartupRegistryAndInventoryUnchanged(t *testing.T) {
	setupIsolatedConfigHome(t)
	application, _ := newRemovePluginApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})
	before, err := application.GetPluginInventory()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.RemovePlugin("lh-plugin-base"); err != nil {
		t.Fatal(err)
	}
	after, err := application.GetPluginInventory()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Plugins) != len(before.Plugins) || after.Plugins[0].ID != "lh-plugin-base" {
		t.Fatalf("expected current startup inventory unchanged by remove: before=%+v after=%+v", before.Plugins, after.Plugins)
	}
}

// TestRemovePluginArtifactFailurePreventsCreateFromStaleRegistry proves that
// once config detachment commits, a later artifact-removal error cannot let
// the immutable startup Registry authorize a new enabled declaration. The
// artifact may be absent, intact, or partially removed; only session safety
// is asserted here.
func TestRemovePluginArtifactFailurePreventsCreateFromStaleRegistry(t *testing.T) {
	setupIsolatedConfigHome(t)
	application, root := newRemovePluginApp(t,
		plugintest.ExtensionSpec{ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch}},
		plugintest.ExtensionSpec{ID: "other-extension", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch}},
	)
	created, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetPluginInstanceEnabled(created.InstanceID, false); err != nil {
		t.Fatal(err)
	}

	// Simulate the artifact vanishing out from under a still-valid startup
	// Registry entry (e.g. manual filesystem tampering between startup and
	// this Remove call), so the installer's own removal step fails.
	if err := os.RemoveAll(filepath.Join(root, "lh-plugin-base")); err != nil {
		t.Fatal(err)
	}

	_, err = application.RemovePlugin("lh-plugin-base")
	if err == nil {
		t.Fatal("expected artifact removal failure to surface as an error")
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins.Instances) != 0 {
		t.Fatalf("expected declarations already removed despite artifact-removal failure: %+v", cfg.Plugins.Instances)
	}

	if _, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
	}); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("expected failed-remove extension to stay unavailable this session, got %v", err)
	}
	if _, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "other-extension", CapabilityID: string(plugin.CapabilitySourceFetch),
	}); err != nil {
		t.Fatalf("unrelated extension should remain creatable: %v", err)
	}

	cfg, err = config.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, declaration := range cfg.Plugins.Instances {
		if declaration.ExtensionID == "lh-plugin-base" && !declaration.Disabled {
			t.Fatalf("enabled declaration recreated from stale Registry: %+v", cfg.Plugins.Instances)
		}
	}
	if len(cfg.Plugins.Instances) != 1 || cfg.Plugins.Instances[0].ExtensionID != "other-extension" {
		t.Fatalf("expected only unrelated extension declaration: %+v", cfg.Plugins.Instances)
	}
}

func TestRemovePluginConfigSaveFailureDoesNotMarkExtensionUnavailable(t *testing.T) {
	setupIsolatedConfigHome(t)
	application, root := newRemovePluginApp(t, plugintest.ExtensionSpec{
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

	configDirectory, err := appdir.ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(configDirectory, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(configDirectory, 0o700) })

	if _, err := application.RemovePlugin("lh-plugin-base"); err == nil || !strings.Contains(err.Error(), "save plugin configuration") {
		t.Fatalf("expected config-save failure, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "lh-plugin-base")); statErr != nil {
		t.Fatalf("artifact should remain when config save fails: %v", statErr)
	}
	if err := os.Chmod(configDirectory, 0o700); err != nil {
		t.Fatal(err)
	}

	if _, err := application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
		ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
	}); err != nil {
		t.Fatalf("extension was incorrectly marked unavailable after config-save failure: %v", err)
	}
}

// TestRemovePluginConcurrentWithCreateInstanceNeverLeavesEnabledDeclarationForRemovedExtension
// proves the shared pluginConfigMu plus the removedExtensionIDs guard prevent
// the one truly forbidden outcome: an enabled declaration persisted for an
// extension whose artifact has already been deleted.
func TestRemovePluginConcurrentWithCreateInstanceNeverLeavesEnabledDeclarationForRemovedExtension(t *testing.T) {
	setupIsolatedConfigHome(t)
	application, root := newRemovePluginApp(t, plugintest.ExtensionSpec{
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

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = application.RemovePlugin("lh-plugin-base")
	}()
	go func() {
		defer wg.Done()
		_, _ = application.CreatePluginInstance(CreatePluginInstanceRequestDTO{
			ExtensionID: "lh-plugin-base", CapabilityID: string(plugin.CapabilitySourceFetch),
		})
	}()
	wg.Wait()

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	_, artifactErr := os.Stat(filepath.Join(root, "lh-plugin-base"))
	artifactMissing := os.IsNotExist(artifactErr)
	hasEnabledDeclaration := false
	for _, declaration := range cfg.Plugins.Instances {
		if declaration.ExtensionID == "lh-plugin-base" && !declaration.Disabled {
			hasEnabledDeclaration = true
		}
	}
	if artifactMissing && hasEnabledDeclaration {
		t.Fatalf("forbidden state: enabled declaration pointing at removed artifact: %+v", cfg.Plugins.Instances)
	}
}

func TestRemovePluginRejectsEmptyExtensionID(t *testing.T) {
	setupIsolatedConfigHome(t)
	application, _ := newRemovePluginApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})
	if _, err := application.RemovePlugin("   "); err == nil {
		t.Fatal("expected empty extension id rejection")
	}
}

func TestRemovePluginRequiresInitializedDependencies(t *testing.T) {
	application := &App{}
	if _, err := application.RemovePlugin("lh-plugin-base"); err == nil {
		t.Fatal("expected uninitialized dependency error")
	}
}

// TestRemovePluginHasNoDatabaseDependency documents and proves, at the
// dependency-graph level, that Remove never touches Source/AI/domain
// history: App is constructed with every database-backed service left at its
// zero value, and RemovePlugin still succeeds.
func TestRemovePluginHasNoDatabaseDependency(t *testing.T) {
	setupIsolatedConfigHome(t)
	application, _ := newRemovePluginApp(t, plugintest.ExtensionSpec{
		ID: "lh-plugin-base", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})
	if application.plannerService != nil || application.plannerViewService != nil {
		t.Fatal("test setup unexpectedly wired a database-backed service")
	}
	if _, err := application.RemovePlugin("lh-plugin-base"); err != nil {
		t.Fatal(err)
	}
}
