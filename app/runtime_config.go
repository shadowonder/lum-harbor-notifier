package app

import (
	"fmt"

	"lum-harbor-notifier/internal/config"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// RuntimeConfigDTO is the intentionally narrow Wails configuration contract.
// Backend-only Plugin, Background, and Logging settings never cross the bridge.
type RuntimeConfigDTO struct {
	General       RuntimeGeneralConfigDTO       `json:"general"`
	Dashboard     RuntimeDashboardConfigDTO     `json:"dashboard"`
	Notifications RuntimeNotificationsConfigDTO `json:"notifications"`
	// Platform is one of "MACOS", "WINDOWS", or "OTHER" (see internal/platform).
	// It exists so the frontend never has to guess the OS from browser/renderer
	// heuristics (navigator.platform/userAgent) — e.g. to decide whether to show
	// a macOS-specific "Use system credential manager" option in a future Slice.
	// Deliberately just this one string: no architecture, kernel version,
	// hostname, username, machine ID, or OS build number.
	Platform string `json:"platform"`
}

// RuntimeGeneralConfigDTO contains General desktop shell preferences.
type RuntimeGeneralConfigDTO struct {
	CloseWindowBehavior string `json:"closeWindowBehavior"`
}

type SaveGeneralSettingsRequestDTO struct {
	CloseWindowBehavior string `json:"closeWindowBehavior"`
}

// RuntimeDashboardConfigDTO contains startup-validated Dashboard presentation
// settings consumed by the frontend application shell.
type RuntimeDashboardConfigDTO struct {
	TodoDisplayLimit int `json:"todoDisplayLimit"`
}

type RuntimeNotificationsConfigDTO struct {
	InApp InAppNotificationSettingsDTO `json:"inApp"`
}

// InAppNotificationSettingsDTO is the frontend-safe presentation preference
// contract. It contains no durable Notification or delivery state.
type InAppNotificationSettingsDTO struct {
	AutoDismiss        bool   `json:"autoDismiss"`
	AutoDismissSeconds int    `json:"autoDismissSeconds"`
	Placement          string `json:"placement"`
}

type SaveInAppNotificationSettingsRequestDTO struct {
	AutoDismiss        bool   `json:"autoDismiss"`
	AutoDismissSeconds int    `json:"autoDismissSeconds"`
	Placement          string `json:"placement"`
}

// NewRuntimeConfigDTO constructs the immutable frontend projection from the
// already validated startup value. It performs no filesystem access.
func NewRuntimeConfigDTO(
	generalConfig config.GeneralConfig,
	todoDisplayLimit int,
	currentPlatform string,
	notificationConfig config.InAppNotificationConfig,
) RuntimeConfigDTO {
	return RuntimeConfigDTO{
		General: RuntimeGeneralConfigDTO{
			CloseWindowBehavior: generalConfig.CloseWindowBehavior,
		},
		Dashboard: RuntimeDashboardConfigDTO{TodoDisplayLimit: todoDisplayLimit},
		Notifications: RuntimeNotificationsConfigDTO{InApp: newInAppNotificationSettingsDTO(
			notificationConfig,
		)},
		Platform: currentPlatform,
	}
}

func newInAppNotificationSettingsDTO(value config.InAppNotificationConfig) InAppNotificationSettingsDTO {
	return InAppNotificationSettingsDTO{
		AutoDismiss:        value.AutoDismiss,
		AutoDismissSeconds: value.AutoDismissSeconds,
		Placement:          value.Placement,
	}
}

// GetRuntimeConfig returns the startup snapshot held by App. Runtime config is
// restart-required in V1 and is never reloaded by a Wails request.
func (a *App) GetRuntimeConfig() RuntimeConfigDTO {
	return a.runtimeConfig
}

// SaveInAppNotificationSettings persists presentation-only desired state.
// The caller applies the returned value to the current frontend session; no
// Notification, Attention, History, SQLite, or Plugin boundary is involved.
func (a *App) SaveInAppNotificationSettings(
	request SaveInAppNotificationSettingsRequestDTO,
) (InAppNotificationSettingsDTO, error) {
	a.notificationConfigMu.Lock()
	defer a.notificationConfigMu.Unlock()

	cfg, err := config.Load()
	if err != nil {
		return InAppNotificationSettingsDTO{}, fmt.Errorf("load current runtime configuration: %w", err)
	}
	cfg.Notifications.InApp = config.InAppNotificationConfig{
		AutoDismiss:        request.AutoDismiss,
		AutoDismissSeconds: request.AutoDismissSeconds,
		Placement:          request.Placement,
	}
	if err := config.Save(cfg); err != nil {
		return InAppNotificationSettingsDTO{}, fmt.Errorf("save in-app notification settings: %w", err)
	}
	return newInAppNotificationSettingsDTO(cfg.Notifications.InApp), nil
}

// SaveGeneralSettings persists General desktop preferences.
func (a *App) SaveGeneralSettings(
	request SaveGeneralSettingsRequestDTO,
) (RuntimeGeneralConfigDTO, error) {
	a.generalConfigMu.Lock()
	defer a.generalConfigMu.Unlock()

	cfg, err := config.Load()
	if err != nil {
		return RuntimeGeneralConfigDTO{}, fmt.Errorf("load current runtime configuration: %w", err)
	}
	cfg.General.CloseWindowBehavior = request.CloseWindowBehavior
	if err := config.Save(cfg); err != nil {
		return RuntimeGeneralConfigDTO{}, fmt.Errorf("save general settings: %w", err)
	}
	a.runtimeConfig.General.CloseWindowBehavior = cfg.General.CloseWindowBehavior
	a.applyCloseWindowBehavior(cfg.General.CloseWindowBehavior)
	return RuntimeGeneralConfigDTO{
		CloseWindowBehavior: cfg.General.CloseWindowBehavior,
	}, nil
}

func (a *App) applyCloseWindowBehavior(behavior string) {
	shouldHide := behavior == config.CloseWindowBehaviorBackground
	a.appOptionsMu.Lock()
	if a.appOptions != nil {
		a.appOptions.HideWindowOnClose = shouldHide
	}
	a.appOptionsMu.Unlock()
	setNativeHideOnClose(shouldHide)
}

// ShowMainWindow makes the main application window visible and focused.
func (a *App) ShowMainWindow() {
	if a.ctx != nil {
		runtime.Show(a.ctx)
		runtime.WindowShow(a.ctx)
		runtime.WindowUnminimise(a.ctx)
	}
}

// OpenSettingsFromTray opens the main window and instructs the frontend
// to display the settings surface.
func (a *App) OpenSettingsFromTray() {
	a.ShowMainWindow()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "lumharbor:navigate-page", "settings")
	}
}

// QuitFromTray performs application shutdown through the standard runtime path.
func (a *App) QuitFromTray() {
	if a.ctx != nil {
		runtime.Quit(a.ctx)
	}
}
