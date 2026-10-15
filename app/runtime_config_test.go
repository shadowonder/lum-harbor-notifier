package app

import (
	"path/filepath"
	"testing"

	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/platform"

	"github.com/wailsapp/wails/v2/pkg/options"
)

func TestNewRuntimeConfigDTOCarriesPlatformAndDashboardLimitUnchanged(t *testing.T) {
	preferences := config.DefaultConfig().Notifications.InApp
	dto := NewRuntimeConfigDTO(config.DefaultConfig().General, 8, platform.Windows, preferences)
	if dto.General.CloseWindowBehavior != config.CloseWindowBehaviorBackground {
		t.Fatalf("CloseWindowBehavior = %q, want %q", dto.General.CloseWindowBehavior, config.CloseWindowBehaviorBackground)
	}
	if dto.Dashboard.TodoDisplayLimit != 8 {
		t.Fatalf("TodoDisplayLimit = %d, want 8", dto.Dashboard.TodoDisplayLimit)
	}
	if dto.Platform != platform.Windows {
		t.Fatalf("Platform = %q, want %q", dto.Platform, platform.Windows)
	}
	if dto.Notifications.InApp.AutoDismiss || dto.Notifications.InApp.AutoDismissSeconds != 600 || dto.Notifications.InApp.Placement != config.NotificationPlacementBottomRight {
		t.Fatalf("unexpected notification settings: %+v", dto.Notifications.InApp)
	}
}

func TestGetRuntimeConfigReturnsTheExactStartupSnapshot(t *testing.T) {
	snapshot := NewRuntimeConfigDTO(config.DefaultConfig().General, 6, platform.MacOS, config.DefaultConfig().Notifications.InApp)
	a := &App{runtimeConfig: snapshot}
	if got := a.GetRuntimeConfig(); got != snapshot {
		t.Fatalf("GetRuntimeConfig() = %+v, want the exact startup snapshot %+v", got, snapshot)
	}
}

func TestSaveInAppNotificationSettingsPersistsOnlyPresentationPreferences(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))

	seed := config.DefaultConfig()
	seed.Dashboard.TodoDisplayLimit = 9
	if err := config.Save(seed); err != nil {
		t.Fatal(err)
	}

	a := &App{runtimeConfig: NewRuntimeConfigDTO(seed.General, seed.Dashboard.TodoDisplayLimit, platform.MacOS, seed.Notifications.InApp)}
	got, err := a.SaveInAppNotificationSettings(SaveInAppNotificationSettingsRequestDTO{
		AutoDismiss: true, AutoDismissSeconds: 30, Placement: config.NotificationPlacementTopLeft,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got.AutoDismiss || got.AutoDismissSeconds != 30 || got.Placement != config.NotificationPlacementTopLeft {
		t.Fatalf("unexpected saved projection: %+v", got)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Notifications.InApp != (config.InAppNotificationConfig{AutoDismiss: true, AutoDismissSeconds: 30, Placement: config.NotificationPlacementTopLeft}) {
		t.Fatalf("preferences were not persisted: %+v", loaded.Notifications.InApp)
	}
	if loaded.Dashboard.TodoDisplayLimit != 9 {
		t.Fatalf("unrelated configuration changed: %+v", loaded.Dashboard)
	}
}

func TestSaveInAppNotificationSettingsRejectsUnsupportedValues(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))

	a := &App{}
	_, err := a.SaveInAppNotificationSettings(SaveInAppNotificationSettingsRequestDTO{
		AutoDismiss: true, AutoDismissSeconds: 1, Placement: config.NotificationPlacementBottomRight,
	})
	if err == nil {
		t.Fatal("expected invalid duration to be rejected")
	}
}

func TestSaveGeneralSettingsPersistsCloseWindowBehavior(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))

	seed := config.DefaultConfig()
	seed.General.CloseWindowBehavior = config.CloseWindowBehaviorBackground
	if err := config.Save(seed); err != nil {
		t.Fatal(err)
	}

	a := &App{runtimeConfig: NewRuntimeConfigDTO(seed.General, seed.Dashboard.TodoDisplayLimit, platform.MacOS, seed.Notifications.InApp)}
	got, err := a.SaveGeneralSettings(SaveGeneralSettingsRequestDTO{
		CloseWindowBehavior: config.CloseWindowBehaviorQuit,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.CloseWindowBehavior != config.CloseWindowBehaviorQuit {
		t.Fatalf("got closeWindowBehavior = %q, want %q", got.CloseWindowBehavior, config.CloseWindowBehaviorQuit)
	}
	if a.runtimeConfig.General.CloseWindowBehavior != config.CloseWindowBehaviorQuit {
		t.Fatalf("runtime snapshot was not updated: %+v", a.runtimeConfig.General)
	}

	loaded, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.General.CloseWindowBehavior != config.CloseWindowBehaviorQuit {
		t.Fatalf("persisted config = %q, want %q", loaded.General.CloseWindowBehavior, config.CloseWindowBehaviorQuit)
	}
}

func TestSaveGeneralSettingsUpdatesRuntimeAndAppOptions(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))

	seed := config.DefaultConfig()
	if err := config.Save(seed); err != nil {
		t.Fatal(err)
	}

	opts := &options.App{HideWindowOnClose: true}
	a := &App{runtimeConfig: NewRuntimeConfigDTO(seed.General, seed.Dashboard.TodoDisplayLimit, platform.MacOS, seed.Notifications.InApp)}
	a.SetAppOptions(opts)

	// Change to quit mode: HideWindowOnClose should become false
	if _, err := a.SaveGeneralSettings(SaveGeneralSettingsRequestDTO{CloseWindowBehavior: config.CloseWindowBehaviorQuit}); err != nil {
		t.Fatal(err)
	}
	if opts.HideWindowOnClose {
		t.Fatal("expected appOptions.HideWindowOnClose to be false in quit mode")
	}

	// Change back to background mode: HideWindowOnClose should become true
	if _, err := a.SaveGeneralSettings(SaveGeneralSettingsRequestDTO{CloseWindowBehavior: config.CloseWindowBehaviorBackground}); err != nil {
		t.Fatal(err)
	}
	if !opts.HideWindowOnClose {
		t.Fatal("expected appOptions.HideWindowOnClose to be true in background mode")
	}
}

func TestSaveGeneralSettingsRejectsInvalidValues(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))

	a := &App{}
	_, err := a.SaveGeneralSettings(SaveGeneralSettingsRequestDTO{
		CloseWindowBehavior: "minimize_to_system_tray",
	})
	if err == nil {
		t.Fatal("expected invalid closeWindowBehavior to be rejected")
	}
}
