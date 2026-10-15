package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"lum-harbor-notifier/internal/appdir"
)

// TestDefaultConfig protects every runtime section required before config.json
// exists, including the unchanged logging defaults.
func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.General.CloseWindowBehavior != CloseWindowBehaviorBackground {
		t.Fatalf("expected close window behavior to default to background, got %+v", cfg.General)
	}
	if cfg.Logging.Level != "info" || !cfg.Logging.Console || !cfg.Logging.File {
		t.Fatalf("unexpected logging defaults: %+v", cfg.Logging)
	}
	if cfg.Logging.FileName != "lum-harbor.log" || cfg.Logging.MaxSizeMB != 1 ||
		cfg.Logging.MaxBackups != 5 || cfg.Logging.MaxAgeDays != 7 || cfg.Logging.Compress {
		t.Fatalf("unexpected logging file defaults: %+v", cfg.Logging)
	}
	if cfg.Dashboard.TodoDisplayLimit != 6 {
		t.Fatalf("expected dashboard todo limit 6, got %d", cfg.Dashboard.TodoDisplayLimit)
	}
	if cfg.Notifications.InApp.AutoDismiss || cfg.Notifications.InApp.AutoDismissSeconds != 600 || cfg.Notifications.InApp.Placement != NotificationPlacementBottomRight {
		t.Fatalf("unexpected in-app notification defaults: %+v", cfg.Notifications.InApp)
	}
	if cfg.Plugins.Directory != "plugins" || cfg.Plugins.StartupTimeoutSeconds != 10 ||
		cfg.Plugins.RequestTimeoutSeconds != 30 {
		t.Fatalf("unexpected plugin defaults: %+v", cfg.Plugins)
	}
	if len(cfg.Plugins.Instances) != 0 {
		t.Fatalf("expected no configured plugin instances, got %+v", cfg.Plugins.Instances)
	}
	if cfg.Background.ShutdownTimeoutSeconds != 10 {
		t.Fatalf("unexpected background defaults: %+v", cfg.Background)
	}
	if cfg.AI.BuiltInOpenAI.Endpoint != "" || cfg.AI.BuiltInOpenAI.Model != "" || cfg.AI.BuiltInOpenAI.RequestTimeoutSeconds != 30 {
		t.Fatalf("unexpected AI defaults: %+v", cfg.AI)
	}
	if !cfg.AI.BuiltInOpenAI.UseSystemCredentialManager {
		t.Fatalf("expected system credential manager to default to true, got %+v", cfg.AI.BuiltInOpenAI)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config must be valid: %v", err)
	}
}

func TestLoadInAppNotificationPreferencesAndPreserveDefaults(t *testing.T) {
	writeRuntimeConfig(t, `{"notifications":{"inApp":{"autoDismiss":true,"autoDismissSeconds":30,"placement":"TOP_CENTER"}}}`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := InAppNotificationConfig{AutoDismiss: true, AutoDismissSeconds: 30, Placement: NotificationPlacementTopCenter}
	if cfg.Notifications.InApp != want {
		t.Fatalf("notification preferences = %+v, want %+v", cfg.Notifications.InApp, want)
	}
	if cfg.Dashboard != DefaultConfig().Dashboard || cfg.Background != DefaultConfig().Background || cfg.General != DefaultConfig().General {
		t.Fatalf("notification override changed unrelated defaults: %+v", cfg)
	}
}

func TestLoadGeneralCloseWindowBehaviorAndPreserveDefaults(t *testing.T) {
	writeRuntimeConfig(t, `{"general":{"closeWindowBehavior":"quit"}}`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.General.CloseWindowBehavior != CloseWindowBehaviorQuit {
		t.Fatalf("general.closeWindowBehavior = %q, want %q", cfg.General.CloseWindowBehavior, CloseWindowBehaviorQuit)
	}
	if cfg.Dashboard != DefaultConfig().Dashboard || cfg.Background != DefaultConfig().Background || cfg.Notifications != DefaultConfig().Notifications {
		t.Fatalf("general override changed unrelated defaults: %+v", cfg)
	}
}

func TestLoadGeneralCloseWindowBehaviorSnakeCase(t *testing.T) {
	writeRuntimeConfig(t, `{"general":{"close_window_behavior":"quit"}}`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.General.CloseWindowBehavior != CloseWindowBehaviorQuit {
		t.Fatalf("general.close_window_behavior = %q, want %q", cfg.General.CloseWindowBehavior, CloseWindowBehaviorQuit)
	}
}

func TestLoadGeneralCloseWindowBehaviorMissingDefaultsToBackground(t *testing.T) {
	writeRuntimeConfig(t, `{"dashboard":{"todoDisplayLimit":5}}`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.General.CloseWindowBehavior != CloseWindowBehaviorBackground {
		t.Fatalf("missing general.closeWindowBehavior must default to background, got %q", cfg.General.CloseWindowBehavior)
	}
}

func TestValidateGeneralCloseWindowBehaviorRejectsInvalid(t *testing.T) {
	cfg := DefaultConfig()
	cfg.General.CloseWindowBehavior = "minimize_to_dock"
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected invalid general.closeWindowBehavior to be rejected")
	}
}

func TestLoadLegacyInAppNotificationMinutesMigratesToSeconds(t *testing.T) {
	writeRuntimeConfig(t, `{"notifications":{"inApp":{"autoDismiss":true,"autoDismissMinutes":15,"placement":"BOTTOM_LEFT"}}}`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := InAppNotificationConfig{AutoDismiss: true, AutoDismissSeconds: 900, Placement: NotificationPlacementBottomLeft}
	if cfg.Notifications.InApp != want {
		t.Fatalf("legacy notification preferences = %+v, want %+v", cfg.Notifications.InApp, want)
	}
}

func TestLoadPartialInAppNotificationOverridePreservesNestedDefaults(t *testing.T) {
	writeRuntimeConfig(t, `{"notifications":{"inApp":{"placement":"TOP_LEFT"}}}`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Notifications.InApp.AutoDismiss || cfg.Notifications.InApp.AutoDismissSeconds != 600 || cfg.Notifications.InApp.Placement != NotificationPlacementTopLeft {
		t.Fatalf("unexpected partially overridden preferences: %+v", cfg.Notifications.InApp)
	}
}

func TestLoadRejectsInvalidInAppNotificationPreferences(t *testing.T) {
	for _, value := range []string{
		`{"notifications":{"inApp":{"autoDismissSeconds":6}}}`,
		`{"notifications":{"inApp":{"autoDismissMinutes":6}}}`,
		`{"notifications":{"inApp":{"placement":"MIDDLE"}}}`,
	} {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadFile(path); err == nil || !strings.Contains(err.Error(), "notifications.inApp") {
			t.Fatalf("expected notification validation error for %s, got %v", value, err)
		}
	}
}

func TestLoadBuiltInOpenAIConfigAndPreserveUnrelatedDefaults(t *testing.T) {
	writeRuntimeConfig(t, `{"ai":{"builtInOpenAI":{"endpoint":"http://127.0.0.1:11434/v1","model":"local-model","requestTimeoutSeconds":45}}}`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AI.BuiltInOpenAI.Configured() || cfg.AI.BuiltInOpenAI.Model != "local-model" || cfg.AI.BuiltInOpenAI.RequestTimeoutSeconds != 45 {
		t.Fatalf("unexpected AI config: %+v", cfg.AI)
	}
	defaults := DefaultConfig()
	if cfg.Logging != defaults.Logging || !reflect.DeepEqual(cfg.Plugins, defaults.Plugins) || cfg.Background != defaults.Background || cfg.Dashboard != defaults.Dashboard {
		t.Fatalf("AI override changed unrelated sections: %+v", cfg)
	}
}

func TestBuiltInOpenAIConfigRoundTripContainsNoCredential(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))
	cfg := DefaultConfig()
	cfg.AI.BuiltInOpenAI = BuiltInOpenAIConfig{Endpoint: "https://api.openai.com/v1", Model: "gpt-test", RequestTimeoutSeconds: 20}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.AI != cfg.AI {
		t.Fatalf("round trip mismatch: got %+v want %+v", loaded.AI, cfg.AI)
	}
	path, _ := appdir.ConfigDir()
	data, err := os.ReadFile(filepath.Join(path, configFileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(data)), "api_key") || strings.Contains(string(data), "secret-value") {
		t.Fatalf("config file contains credential material: %s", data)
	}
}

// TestLoadOmittedUseSystemCredentialManagerDefaultsToTrue protects backward
// compatibility for existing macOS users: a config.json written before this
// field existed must keep resolving to the real system Keychain, never
// silently switch them to the weaker application-managed fallback store.
func TestLoadOmittedUseSystemCredentialManagerDefaultsToTrue(t *testing.T) {
	writeRuntimeConfig(t, `{"ai":{"builtInOpenAI":{"endpoint":"https://api.openai.com/v1","model":"gpt-test","requestTimeoutSeconds":30}}}`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.AI.BuiltInOpenAI.UseSystemCredentialManager {
		t.Fatalf("expected omitted useSystemCredentialManager to default to true, got %+v", cfg.AI.BuiltInOpenAI)
	}
}

// TestLoadExplicitUseSystemCredentialManagerFalseSurvivesRoundTrip protects
// the pointer-based override pattern: an explicit false must not be
// indistinguishable from an omitted field.
func TestLoadExplicitUseSystemCredentialManagerFalseSurvivesRoundTrip(t *testing.T) {
	writeRuntimeConfig(t, `{"ai":{"builtInOpenAI":{"endpoint":"https://api.openai.com/v1","model":"gpt-test","requestTimeoutSeconds":30,"useSystemCredentialManager":false}}}`)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.BuiltInOpenAI.UseSystemCredentialManager {
		t.Fatalf("expected explicit false to be preserved, got %+v", cfg.AI.BuiltInOpenAI)
	}
	defaults := DefaultConfig()
	if cfg.Logging != defaults.Logging || !reflect.DeepEqual(cfg.Plugins, defaults.Plugins) || cfg.Background != defaults.Background || cfg.Dashboard != defaults.Dashboard {
		t.Fatalf("credential store mode override changed unrelated sections: %+v", cfg)
	}

	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	reloaded, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.AI.BuiltInOpenAI.UseSystemCredentialManager {
		t.Fatalf("expected explicit false to survive a save/load round trip, got %+v", reloaded.AI.BuiltInOpenAI)
	}
}

func TestLoadRejectsInvalidBuiltInOpenAIConfig(t *testing.T) {
	for _, value := range []string{
		`{"ai":{"builtInOpenAI":{"endpoint":"not-a-url","model":"model"}}}`,
		`{"ai":{"builtInOpenAI":{"endpoint":"https://user:secret@example.com/v1","model":"model"}}}`,
		`{"ai":{"builtInOpenAI":{"endpoint":"https://api.openai.com/v1"}}}`,
		`{"ai":{"builtInOpenAI":{"endpoint":"https://api.openai.com/v1","model":" model "}}}`,
		`{"ai":{"builtInOpenAI":{"requestTimeoutSeconds":0}}}`,
	} {
		path := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadFile(path); err == nil || !strings.Contains(err.Error(), "ai.builtInOpenAI") {
			t.Fatalf("expected AI validation error for %s, got %v", value, err)
		}
	}
}

// TestLoadMissingFileReturnsDefaults verifies absence remains a normal first-run
// state rather than a startup error.
func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected missing config to use defaults: %v", err)
	}
	if !reflect.DeepEqual(cfg, DefaultConfig()) {
		t.Fatalf("expected defaults for missing config, got %+v", cfg)
	}
}

// TestLoadPartialTopLevelOverride proves an isolated Dashboard override cannot
// zero unrelated nested defaults.
func TestLoadPartialTopLevelOverride(t *testing.T) {
	writeRuntimeConfig(t, `{"dashboard":{"todoDisplayLimit":8}}`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load partial dashboard config: %v", err)
	}
	defaults := DefaultConfig()
	if cfg.Dashboard.TodoDisplayLimit != 8 {
		t.Fatalf("expected dashboard todo limit 8, got %d", cfg.Dashboard.TodoDisplayLimit)
	}
	if cfg.Logging != defaults.Logging || !reflect.DeepEqual(cfg.Plugins, defaults.Plugins) ||
		cfg.Background != defaults.Background {
		t.Fatalf("top-level partial override changed unrelated defaults: %+v", cfg)
	}
}

func TestLoadPluginInstancesAndReloadIndependentConfig(t *testing.T) {
	writeRuntimeConfig(t, `{"plugins":{"instances":[{"id":"team-source","extensionId":"company-assistant","capabilityId":"source.fetch","config":{"endpoint":"local","limit":3,"enabled":true}}]}}`)

	first, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Plugins.Instances) != 1 {
		t.Fatalf("configured instances: %+v", first.Plugins.Instances)
	}
	instance := first.Plugins.Instances[0]
	if instance.ID != "team-source" || instance.ExtensionID != "company-assistant" || instance.CapabilityID != "source.fetch" || !json.Valid(instance.Config) {
		t.Fatalf("configured instance: %+v", instance)
	}
	first.Plugins.Instances[0].Config[0] = '['
	second, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(second.Plugins.Instances[0].Config) || second.Plugins.Instances[0].Config[0] != '{' {
		t.Fatalf("reloaded config aliased prior mutable bytes: %s", second.Plugins.Instances[0].Config)
	}
}

func TestLoadExplicitEmptyPluginInstances(t *testing.T) {
	writeRuntimeConfig(t, `{"plugins":{"instances":[]}}`)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Plugins.Instances) != 0 {
		t.Fatalf("expected explicit empty selection, got %+v", cfg.Plugins.Instances)
	}
}

func TestLoadRejectsUnknownPluginInstanceField(t *testing.T) {
	writeRuntimeConfig(t, `{"plugins":{"instances":[{"id":"source","extensionId":"ext","capabilityId":"source.fetch","unexpected":true}]}}`)
	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("expected unknown instance field error, got %v", err)
	}
}

// TestLoadPartialNestedOverride protects pointer-based merge semantics within a
// nested section, especially bool and numeric defaults.
func TestLoadPartialNestedOverride(t *testing.T) {
	writeRuntimeConfig(t, `{"logging":{"level":"debug"}}`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load partial logging config: %v", err)
	}
	defaults := DefaultConfig()
	if cfg.Logging.Level != "debug" {
		t.Fatalf("expected debug logging, got %q", cfg.Logging.Level)
	}
	cfg.Logging.Level = defaults.Logging.Level
	if cfg.Logging != defaults.Logging {
		t.Fatalf("nested partial override changed logging defaults: %+v", cfg.Logging)
	}
}

func TestLoadRejectsMalformedJSON(t *testing.T) {
	writeRuntimeConfig(t, `{"dashboard":`)

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "decode runtime config") {
		t.Fatalf("expected contextual malformed JSON error, got %v", err)
	}
}

func TestLoadRejectsInvalidDashboardLimit(t *testing.T) {
	for _, limit := range []int{0, 21} {
		t.Run(strconv.Itoa(limit), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			contents := `{"dashboard":{"todoDisplayLimit":` + strconv.Itoa(limit) + `}}`
			if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := loadFile(path)
			if err == nil || !strings.Contains(err.Error(), "dashboard.todoDisplayLimit") {
				t.Fatalf("expected dashboard limit validation error, got %v", err)
			}
		})
	}
}

func TestLoadRejectsInvalidPluginConfig(t *testing.T) {
	tests := []struct {
		name      string
		json      string
		fieldName string
	}{
		{"empty directory", `{"plugins":{"directory":""}}`, "plugins.directory"},
		{"startup timeout", `{"plugins":{"startupTimeoutSeconds":0}}`, "plugins.startupTimeoutSeconds"},
		{"request timeout", `{"plugins":{"requestTimeoutSeconds":0}}`, "plugins.requestTimeoutSeconds"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			if err := os.WriteFile(path, []byte(test.json), 0600); err != nil {
				t.Fatalf("write config: %v", err)
			}
			_, err := loadFile(path)
			if err == nil || !strings.Contains(err.Error(), test.fieldName) {
				t.Fatalf("expected %s validation error, got %v", test.fieldName, err)
			}
		})
	}
}

func TestLoadRejectsInvalidBackgroundTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"background":{"shutdownTimeoutSeconds":0}}`), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	_, err := loadFile(path)
	if err == nil || !strings.Contains(err.Error(), "background.shutdownTimeoutSeconds") {
		t.Fatalf("expected background timeout validation error, got %v", err)
	}
}

// writeRuntimeConfig installs one test config at the production path beneath an
// isolated HOME, exercising the same path resolution used during startup.
func writeRuntimeConfig(t *testing.T, contents string) {
	t.Helper()
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))
	directory, err := appdir.ConfigDir()
	if err != nil {
		t.Fatalf("resolve test config directory: %v", err)
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatalf("create test config directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, configFileName), []byte(contents), 0600); err != nil {
		t.Fatalf("write test runtime config: %v", err)
	}
}
