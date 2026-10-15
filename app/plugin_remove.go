package app

import (
	"fmt"
	"strings"

	"lum-harbor-notifier/internal/config"
)

// PluginRemovalResultDTO exposes only the facts a user needs after removing
// an installed Plugin. No file path or other filesystem detail crosses Wails.
type PluginRemovalResultDTO struct {
	ExtensionID     string `json:"extensionId"`
	RestartRequired bool   `json:"restartRequired"`
}

// RemovePlugin uninstalls an installed extension's artifact and deletes its
// now-dangling disabled configured Instance declarations. It never touches
// Source/AI/domain history: this mutation depends only on config.json and the
// Plugin artifact directory, never on the database.
//
// Eligibility is strict and entirely backend-owned:
//   - every configured Instance declaration for this extension must already
//     be disabled (Stop Using, then a restart to actually stop the process,
//     is the expected path to reach this state);
//   - the current Host runtime snapshot must show the extension was never
//     started this application lifetime, or has fully stopped.
//
// The second check matters because Stop Using only changes next-start
// desired config; it never live-stops the Host process. A still-running
// process left over from this session's startup blocks Remove even when
// every declaration is already disabled, because deleting the artifact out
// from under a live process would be unsafe.
//
// This never mutates the current Registry, Host, or Scheduler: the extension
// disappears from Settings' current inventory only after the next restart.
func (a *App) RemovePlugin(extensionID string) (PluginRemovalResultDTO, error) {
	if a.pluginInventory == nil || a.pluginInstaller == nil {
		return PluginRemovalResultDTO{}, fmt.Errorf("plugin removal is not initialized")
	}
	extensionID = strings.TrimSpace(extensionID)
	if extensionID == "" {
		return PluginRemovalResultDTO{}, fmt.Errorf("select a plugin to remove")
	}
	if _, err := a.pluginInventory.registry.Find(extensionID); err != nil {
		return PluginRemovalResultDTO{}, fmt.Errorf("this plugin is not installed")
	}

	// The entire eligibility check and mutation must run under the same lock
	// Create/Toggle already use: otherwise a concurrent CreatePluginInstance
	// or SetPluginInstanceEnabled(true) could introduce a fresh enabled
	// declaration for this extension between this check and the artifact
	// removal below, leaving an enabled declaration pointing at nothing.
	a.pluginConfigMu.Lock()
	defer a.pluginConfigMu.Unlock()

	cfg, err := config.Load()
	if err != nil {
		return PluginRemovalResultDTO{}, fmt.Errorf("load runtime configuration failed")
	}

	remaining := make([]config.PluginInstanceConfig, 0, len(cfg.Plugins.Instances))
	for _, declaration := range cfg.Plugins.Instances {
		if declaration.ExtensionID != extensionID {
			remaining = append(remaining, declaration)
			continue
		}
		if !declaration.Disabled {
			return PluginRemovalResultDTO{}, fmt.Errorf("stop using every instance of this plugin and restart Lum Harbor before removing it")
		}
	}

	runtimeStatus := "DISCOVERED"
	for _, extension := range a.pluginInventory.runtime.Snapshot().Extensions {
		if extension.ID == extensionID {
			runtimeStatus = extension.Status
			break
		}
	}
	if runtimeStatus != "DISCOVERED" && runtimeStatus != "STOPPED" {
		return PluginRemovalResultDTO{}, fmt.Errorf("this plugin's process is still running from this session; restart Lum Harbor before removing it")
	}

	// Removing declarations can only shrink the candidate set, which can
	// never newly violate plugininstance.Resolve for the entries that
	// remain; re-running that validation here would be redundant.
	cfg.Plugins.Instances = remaining
	if err := config.Save(cfg); err != nil {
		return PluginRemovalResultDTO{}, fmt.Errorf("save plugin configuration failed")
	}
	a.pluginInventory.replaceDeclarations(cfg.Plugins.Instances)
	// Once the desired declarations have been durably detached, the startup
	// Registry is no longer sufficient authority for creating a new Instance
	// in this application lifetime. Mark the extension before touching the
	// artifact so a failed or partial filesystem removal cannot reopen the
	// stale-Registry path to an enabled dangling declaration.
	if a.removedExtensionIDs == nil {
		a.removedExtensionIDs = make(map[string]struct{})
	}
	a.removedExtensionIDs[extensionID] = struct{}{}
	if err := a.pluginInstaller.Remove(extensionID); err != nil {
		return PluginRemovalResultDTO{}, fmt.Errorf("plugin files could not be fully removed; try removing it again")
	}

	return PluginRemovalResultDTO{ExtensionID: extensionID, RestartRequired: true}, nil
}
