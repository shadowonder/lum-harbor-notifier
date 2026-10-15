package app

import (
	"fmt"
	"strings"

	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/plugininstance"
)

// PluginInstanceMutationResultDTO exposes only the facts a user needs after
// changing a configured Instance's enabled/disabled desired state. No file
// path or other filesystem/runtime detail crosses Wails.
type PluginInstanceMutationResultDTO struct {
	InstanceID      string `json:"instanceId"`
	ExtensionID     string `json:"extensionId"`
	CapabilityID    string `json:"capabilityId"`
	Enabled         bool   `json:"enabled"`
	RestartRequired bool   `json:"restartRequired"`
}

// SetPluginInstanceEnabled persists a configured Instance's desired
// enabled/disabled state without changing its ID, extension, capability, or
// configuration. Disabling ("Stop Using") always keeps the declaration, so a
// later Re-enable ("Use Again") can restore the exact same Instance identity
// and durable state (e.g. a Source checkpoint) rather than recreating it.
//
// Re-enabling revalidates the full candidate declaration set through the
// same plugininstance.Resolve production startup already relies on, so a
// Plugin that became invalid while disabled (removed, capability no longer
// supported, schema no longer satisfied) cannot silently re-enter runtime
// resolution; disabling itself only needs the candidate set to still resolve
// for every other declaration, since a disabled entry is never checked
// against current Registry/schema state.
//
// This never mutates the current Registry, Host, or Scheduler: the change
// takes effect only on the next application start.
func (a *App) SetPluginInstanceEnabled(instanceID string, enabled bool) (PluginInstanceMutationResultDTO, error) {
	if a.pluginInventory == nil {
		return PluginInstanceMutationResultDTO{}, fmt.Errorf("plugin inventory is not initialized")
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return PluginInstanceMutationResultDTO{}, fmt.Errorf("select a plugin instance")
	}

	a.pluginConfigMu.Lock()
	defer a.pluginConfigMu.Unlock()

	cfg, err := config.Load()
	if err != nil {
		return PluginInstanceMutationResultDTO{}, fmt.Errorf("load runtime configuration failed")
	}

	targetIndex := -1
	for index, declaration := range cfg.Plugins.Instances {
		if declaration.ID == instanceID {
			targetIndex = index
			break
		}
	}
	if targetIndex == -1 {
		return PluginInstanceMutationResultDTO{}, fmt.Errorf("plugin instance not found")
	}
	// No removedExtensionIDs check is needed here: RemovePlugin always deletes
	// every declaration for an extension in the same pluginConfigMu-guarded
	// critical section that records it as removed, so a declaration for a
	// removed extension can never still be found by ID above.

	candidate := append([]config.PluginInstanceConfig(nil), cfg.Plugins.Instances...)
	candidate[targetIndex].Disabled = !enabled

	if _, err := plugininstance.Resolve(candidate, a.pluginInventory.registry); err != nil {
		return PluginInstanceMutationResultDTO{}, classifyInstanceMutationError(err, targetIndex)
	}
	if err := checkSingleDeliveryOwner(candidate, targetIndex); err != nil {
		return PluginInstanceMutationResultDTO{}, err
	}
	if err := checkSingleAIProviderOwner(candidate, targetIndex); err != nil {
		return PluginInstanceMutationResultDTO{}, err
	}

	cfg.Plugins.Instances = candidate
	if err := config.Save(cfg); err != nil {
		return PluginInstanceMutationResultDTO{}, fmt.Errorf("save plugin instance configuration failed")
	}
	a.pluginInventory.replaceDeclarations(cfg.Plugins.Instances)

	return PluginInstanceMutationResultDTO{
		InstanceID:      instanceID,
		ExtensionID:     candidate[targetIndex].ExtensionID,
		CapabilityID:    candidate[targetIndex].CapabilityID,
		Enabled:         enabled,
		RestartRequired: true,
	}, nil
}
