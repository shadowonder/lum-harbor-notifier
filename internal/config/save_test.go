package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSaveFileRoundTripsThroughLoadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := DefaultConfig()
	cfg.Plugins.Instances = []PluginInstanceConfig{
		{ID: "team-a", ExtensionID: "lh-plugin-base", CapabilityID: "source.fetch", Config: json.RawMessage(`{"limit":3}`)},
	}

	if err := saveFile(path, cfg); err != nil {
		t.Fatal(err)
	}
	reloaded, err := loadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// MarshalIndent re-formats whitespace inside the nested instance Config
	// document (still the same JSON value), so instance config is compared
	// semantically while the rest of the struct is compared exactly.
	var wantConfig, gotConfig map[string]any
	if err := json.Unmarshal(cfg.Plugins.Instances[0].Config, &wantConfig); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(reloaded.Plugins.Instances[0].Config, &gotConfig); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotConfig, wantConfig) {
		t.Fatalf("reloaded instance config mismatch: got %+v want %+v", gotConfig, wantConfig)
	}
	cfg.Plugins.Instances[0].Config = nil
	reloaded.Plugins.Instances[0].Config = nil
	if !reflect.DeepEqual(reloaded, cfg) {
		t.Fatalf("reloaded config does not match saved config: got %+v want %+v", reloaded, cfg)
	}
}

func TestSaveFilePreservesUnrelatedSections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := DefaultConfig()
	original.Logging.Level = "debug"
	original.Dashboard.TodoDisplayLimit = 12
	original.Background.ShutdownTimeoutSeconds = 25
	if err := saveFile(path, original); err != nil {
		t.Fatal(err)
	}

	loaded, err := loadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	loaded.Plugins.Instances = []PluginInstanceConfig{
		{ID: "team-a", ExtensionID: "lh-plugin-base", CapabilityID: "source.fetch"},
	}
	if err := saveFile(path, loaded); err != nil {
		t.Fatal(err)
	}

	reloaded, err := loadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Logging.Level != "debug" || reloaded.Dashboard.TodoDisplayLimit != 12 || reloaded.Background.ShutdownTimeoutSeconds != 25 {
		t.Fatalf("unrelated config sections were not preserved: %+v", reloaded)
	}
	if len(reloaded.Plugins.Instances) != 1 || reloaded.Plugins.Instances[0].ID != "team-a" {
		t.Fatalf("plugin instance mutation was not persisted: %+v", reloaded.Plugins.Instances)
	}
}

func TestSaveFileRejectsInvalidConfigWithoutWritingDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	invalid := DefaultConfig()
	invalid.Dashboard.TodoDisplayLimit = 0

	err := saveFile(path, invalid)
	if err == nil || !strings.Contains(err.Error(), "dashboard.todoDisplayLimit") {
		t.Fatalf("expected validation error, got %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("expected no destination file after validation failure, stat err: %v", statErr)
	}
}

func TestSaveFileLeavesNoTemporaryFilesBehind(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "config.json")

	invalid := DefaultConfig()
	invalid.Plugins.Directory = ""
	if err := saveFile(path, invalid); err == nil {
		t.Fatal("expected validation failure")
	}
	if err := saveFile(path, DefaultConfig()); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".config-") {
			t.Fatalf("temporary config file remained: %s", entry.Name())
		}
	}
	if len(entries) != 1 || entries[0].Name() != "config.json" {
		t.Fatalf("expected exactly one published config.json, found: %v", entries)
	}
}

func TestSaveFileOverwritesExistingDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveFile(path, DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	updated := DefaultConfig()
	updated.Dashboard.TodoDisplayLimit = 9
	if err := saveFile(path, updated); err != nil {
		t.Fatal(err)
	}
	reloaded, err := loadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Dashboard.TodoDisplayLimit != 9 {
		t.Fatalf("expected overwritten config, got %+v", reloaded)
	}
}

func TestSaveRoundTripsThroughLoadAtProductionPath(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Plugins.Instances = []PluginInstanceConfig{
		{ID: "team-a", ExtensionID: "lh-plugin-base", CapabilityID: "source.fetch"},
	}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}

	reloaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reloaded, cfg) {
		t.Fatalf("Load after Save mismatch: got %+v want %+v", reloaded, cfg)
	}
}

func TestSaveFileConfigPermissionsAreOwnerOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := saveFile(path, DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("expected owner-only config permissions, got %v", perm)
	}
}
