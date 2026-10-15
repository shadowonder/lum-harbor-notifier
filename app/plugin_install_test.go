package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugininstall"
)

func writeInstallArtifact(t *testing.T, directory, id, version string) {
	t.Helper()
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := plugin.Manifest{
		ManifestVersion: plugin.SupportedManifestVersion,
		ID:              id, Name: "Test Plugin", Version: version,
		ProtocolVersion: plugin.SupportedProtocolVersion,
		Executable:      "run.sh",
		Capabilities:    []plugin.CapabilityDeclaration{{ID: plugin.CapabilitySourceFetch}},
		Configuration:   plugin.PluginConfigSchema{Version: 1},
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, plugin.ManifestFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "run.sh"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestInstallPluginPlacesValidArtifact(t *testing.T) {
	installer, err := plugininstall.New(filepath.Join(t.TempDir(), "plugins"))
	if err != nil {
		t.Fatal(err)
	}
	application := &App{pluginInstaller: installer}

	source := filepath.Join(t.TempDir(), "artifact")
	writeInstallArtifact(t, source, "new-source", "1.0.0")

	result, err := application.InstallPlugin(source)
	if err != nil {
		t.Fatal(err)
	}
	if result.ExtensionID != "new-source" || result.Version != "1.0.0" || !result.RestartRequired {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestInstallPluginLeavesCurrentInventoryUnchangedUntilRestart(t *testing.T) {
	pluginRoot := filepath.Join(t.TempDir(), "plugins")
	if err := os.MkdirAll(pluginRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	registry, failures, err := plugin.Discover(pluginRoot)
	if err != nil || len(failures) != 0 {
		t.Fatalf("discover: err=%v failures=%v", err, failures)
	}
	host := newInventoryHost(t, registry)
	reader, err := NewPluginInventoryReader(registry, nil, host)
	if err != nil {
		t.Fatal(err)
	}
	before := reader.Read()

	installer, err := plugininstall.New(pluginRoot)
	if err != nil {
		t.Fatal(err)
	}
	application := &App{pluginInstaller: installer, pluginInventory: reader}

	source := filepath.Join(t.TempDir(), "artifact")
	writeInstallArtifact(t, source, "fresh-extension", "2.0.0")
	if _, err := application.InstallPlugin(source); err != nil {
		t.Fatal(err)
	}

	after, err := application.GetPluginInventory()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Plugins) != len(before.Plugins) {
		t.Fatalf("current inventory changed after install: before=%d after=%d", len(before.Plugins), len(after.Plugins))
	}
	for _, item := range after.Plugins {
		if item.ID == "fresh-extension" {
			t.Fatalf("newly installed plugin leaked into current inventory before restart: %+v", item)
		}
	}

	registryAfterRestart, failures, err := plugin.Discover(pluginRoot)
	if err != nil || len(failures) != 0 {
		t.Fatalf("post-install discover: err=%v failures=%v", err, failures)
	}
	found := false
	for _, extension := range registryAfterRestart.List() {
		if extension.Manifest.ID == "fresh-extension" {
			found = true
		}
	}
	if !found {
		t.Fatal("installed extension not discoverable on next startup")
	}
}

func TestInstallPluginAlreadyInstalledReturnsClearError(t *testing.T) {
	pluginRoot := filepath.Join(t.TempDir(), "plugins")
	installer, err := plugininstall.New(pluginRoot)
	if err != nil {
		t.Fatal(err)
	}
	application := &App{pluginInstaller: installer}
	source := filepath.Join(t.TempDir(), "artifact")
	writeInstallArtifact(t, source, "dup-extension", "1.0.0")

	if _, err := application.InstallPlugin(source); err != nil {
		t.Fatal(err)
	}
	_, err = application.InstallPlugin(source)
	if err == nil {
		t.Fatal("expected already-installed error")
	}
	if !strings.Contains(err.Error(), "already installed") {
		t.Fatalf("expected clear already-installed error, got: %v", err)
	}
	if strings.Contains(err.Error(), pluginRoot) {
		t.Fatalf("error leaked internal plugin root path: %v", err)
	}
}

func TestInstallPluginRejectsMalformedArtifactWithoutPartialDestination(t *testing.T) {
	pluginRoot := filepath.Join(t.TempDir(), "plugins")
	installer, err := plugininstall.New(pluginRoot)
	if err != nil {
		t.Fatal(err)
	}
	application := &App{pluginInstaller: installer}

	source := filepath.Join(t.TempDir(), "broken-artifact")
	if err := os.MkdirAll(source, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err = application.InstallPlugin(source)
	if err == nil {
		t.Fatal("expected malformed artifact error")
	}
	if strings.Contains(err.Error(), source) || strings.Contains(err.Error(), pluginRoot) {
		t.Fatalf("error leaked filesystem path: %v", err)
	}
	entries, statErr := os.ReadDir(pluginRoot)
	if statErr != nil && !os.IsNotExist(statErr) {
		t.Fatal(statErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no partial destination, found: %v", entries)
	}
}

func TestInstallPluginResultDoesNotExposeDestinationPath(t *testing.T) {
	pluginRoot := filepath.Join(t.TempDir(), "plugins")
	installer, err := plugininstall.New(pluginRoot)
	if err != nil {
		t.Fatal(err)
	}
	application := &App{pluginInstaller: installer}
	source := filepath.Join(t.TempDir(), "artifact")
	writeInstallArtifact(t, source, "path-safe-extension", "1.0.0")

	result, err := application.InstallPlugin(source)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{pluginRoot, "destination", "Destination", "executable", "Executable"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("install result exposed %q: %s", forbidden, encoded)
		}
	}
}

func TestInstallPluginRequiresInitializedInstaller(t *testing.T) {
	application := &App{}
	if _, err := application.InstallPlugin("/tmp/whatever"); err == nil {
		t.Fatal("expected uninitialized installer error")
	}
}

func TestInstallPluginRejectsEmptySourceDirectory(t *testing.T) {
	installer, err := plugininstall.New(filepath.Join(t.TempDir(), "plugins"))
	if err != nil {
		t.Fatal(err)
	}
	application := &App{pluginInstaller: installer}
	if _, err := application.InstallPlugin("   "); err == nil {
		t.Fatal("expected empty source directory rejection")
	}
}

func TestSelectPluginArtifactDirectoryRequiresReadyWindow(t *testing.T) {
	application := &App{}
	if _, err := application.SelectPluginArtifactDirectory(); err == nil {
		t.Fatal("expected window not ready error")
	}
}
