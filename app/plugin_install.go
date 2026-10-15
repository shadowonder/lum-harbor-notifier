package app

import (
	"errors"
	"fmt"
	"strings"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugininstall"
)

// PluginInstaller places or removes artifact directories in the
// application-owned plugin root. It is implemented by *plugininstall.Installer;
// App depends on this narrow interface rather than the concrete installer.
type PluginInstaller interface {
	Install(source string) (plugininstall.Result, error)
	Remove(extensionID string) error
}

// PluginInstallResultDTO exposes only product-facing installation facts.
// Destination paths, executable paths, and other filesystem detail never
// cross the Wails boundary.
type PluginInstallResultDTO struct {
	ExtensionID     string `json:"extensionId"`
	Version         string `json:"version"`
	RestartRequired bool   `json:"restartRequired"`
}

// SelectPluginArtifactFile opens a native file picker for the user
// to choose an already-packaged Plugin archive (*.lhplugin). An empty result means
// the user cancelled; callers must not treat that as an error.
func (a *App) SelectPluginArtifactFile() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("application window is not ready")
	}
	selected, err := wailsruntime.OpenFileDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Select Plugin Package (*.lhplugin)",
		Filters: []wailsruntime.FileFilter{
			{
				DisplayName: "Lum Harbor Plugin (*.lhplugin)",
				Pattern:     "*.lhplugin",
			},
		},
	})
	if err != nil {
		return "", fmt.Errorf("open plugin package picker: %w", err)
	}
	return selected, nil
}

// SelectPluginArtifactDirectory opens a native directory picker for the user
// to choose an already-built Plugin artifact folder. An empty result means
// the user cancelled; callers must not treat that as an error.
func (a *App) SelectPluginArtifactDirectory() (string, error) {
	if a.ctx == nil {
		return "", fmt.Errorf("application window is not ready")
	}
	selected, err := wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title: "Select Plugin Artifact Folder",
	})
	if err != nil {
		return "", fmt.Errorf("open plugin artifact picker: %w", err)
	}
	return selected, nil
}

// InstallPlugin validates and places a user-selected plugin package (*.lhplugin)
// or unpacked artifact directory through the existing plugininstall boundary. It never
// mutates the current Registry, Host, or Scheduler; the installed Plugin becomes
// discoverable on the next application start only.
func (a *App) InstallPlugin(source string) (PluginInstallResultDTO, error) {
	if a.pluginInstaller == nil {
		return PluginInstallResultDTO{}, fmt.Errorf("plugin installation is not initialized")
	}
	source = strings.TrimSpace(source)
	if source == "" {
		return PluginInstallResultDTO{}, fmt.Errorf("select a plugin package or artifact folder")
	}
	result, err := a.pluginInstaller.Install(source)
	if err != nil {
		return PluginInstallResultDTO{}, classifyPluginInstallError(err)
	}
	return PluginInstallResultDTO{
		ExtensionID:     result.ExtensionID,
		Version:         result.Version,
		RestartRequired: result.RestartRequired,
	}, nil
}

// classifyPluginInstallError maps internal installer errors to a small set of
// product-facing messages without leaking absolute filesystem paths.
func classifyPluginInstallError(err error) error {
	switch {
	case errors.Is(err, plugininstall.ErrAlreadyInstalled):
		return fmt.Errorf("this plugin is already installed")
	case errors.Is(err, plugininstall.ErrIncompatiblePlatform):
		return fmt.Errorf("plugin is not compatible with current operating system or architecture")
	case errors.Is(err, plugininstall.ErrInvalidPackage):
		return fmt.Errorf("selected file is not a valid plugin package")
	case errors.Is(err, plugin.ErrManifestInvalid):
		return fmt.Errorf("selected artifact is not a valid plugin")
	case errors.Is(err, plugin.ErrUnsupportedProtocol):
		return fmt.Errorf("plugin protocol version is not supported")
	default:
		return fmt.Errorf("plugin installation failed")
	}
}
