package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/identity"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugininstance"
)

// CreatePluginInstanceRequestDTO carries only the product facts a user
// supplies when configuring an installed Plugin for use: which capability to
// use and the plugin-specific non-secret configuration values the manifest
// schema requires. The frontend never supplies an instance ID, config file
// path, plugin root, or any other PluginsConfig section.
type CreatePluginInstanceRequestDTO struct {
	ExtensionID  string                 `json:"extensionId"`
	CapabilityID string                 `json:"capabilityId"`
	Config       map[string]interface{} `json:"config,omitempty"`
}

// PluginInstanceCreationResultDTO exposes only the facts a user needs after
// configuring a Plugin for use. The generated instance ID, config file path,
// and any other filesystem/runtime detail never cross Wails.
type PluginInstanceCreationResultDTO struct {
	InstanceID      string `json:"instanceId"`
	ExtensionID     string `json:"extensionId"`
	CapabilityID    string `json:"capabilityId"`
	RestartRequired bool   `json:"restartRequired"`
}

// CreatePluginInstance persists a new configured Plugin Instance declaration
// against the latest desired Runtime Config, reusing the same validation
// authority (plugininstance.Resolve against the startup Registry) that
// production startup already relies on. It never mutates the current
// Registry, Host, or Scheduler, and never creates a live process or Job; the
// Instance is resolved and started only on the next application start.
func (a *App) CreatePluginInstance(request CreatePluginInstanceRequestDTO) (PluginInstanceCreationResultDTO, error) {
	if a.pluginInventory == nil {
		return PluginInstanceCreationResultDTO{}, fmt.Errorf("plugin inventory is not initialized")
	}
	extensionID := strings.TrimSpace(request.ExtensionID)
	if extensionID == "" {
		return PluginInstanceCreationResultDTO{}, fmt.Errorf("select a plugin to configure")
	}
	capabilityID := strings.TrimSpace(request.CapabilityID)
	if capabilityID == "" {
		return PluginInstanceCreationResultDTO{}, fmt.Errorf("select a capability to configure")
	}
	// A nil map must become "{}", not the JSON literal null: ValidateDocument
	// treats a non-empty "null" document as an invalid (non-object) document.
	configDocument := json.RawMessage(`{}`)
	if request.Config != nil {
		encoded, err := json.Marshal(request.Config)
		if err != nil {
			return PluginInstanceCreationResultDTO{}, fmt.Errorf("plugin configuration must be a JSON object")
		}
		configDocument = encoded
	}

	// Two near-simultaneous submissions must not read the same desired config,
	// each validate independently, and have the second Save silently discard
	// the first declaration.
	a.pluginConfigMu.Lock()
	defer a.pluginConfigMu.Unlock()

	if _, removed := a.removedExtensionIDs[extensionID]; removed {
		return PluginInstanceCreationResultDTO{}, fmt.Errorf("this plugin is not installed")
	}

	cfg, err := config.Load()
	if err != nil {
		return PluginInstanceCreationResultDTO{}, fmt.Errorf("load runtime configuration failed")
	}

	instanceID, err := generatePluginInstanceID(extensionID, cfg.Plugins.Instances)
	if err != nil {
		return PluginInstanceCreationResultDTO{}, err
	}
	declaration := config.PluginInstanceConfig{
		ID:           instanceID,
		ExtensionID:  extensionID,
		CapabilityID: capabilityID,
		Config:       configDocument,
	}
	existingCount := len(cfg.Plugins.Instances)
	candidate := append(append([]config.PluginInstanceConfig(nil), cfg.Plugins.Instances...), declaration)

	if _, err := plugininstance.Resolve(candidate, a.pluginInventory.registry); err != nil {
		return PluginInstanceCreationResultDTO{}, classifyInstanceMutationError(err, existingCount)
	}
	if err := checkSingleDeliveryOwner(candidate, existingCount); err != nil {
		return PluginInstanceCreationResultDTO{}, err
	}
	if err := checkSingleAIProviderOwner(candidate, existingCount); err != nil {
		return PluginInstanceCreationResultDTO{}, err
	}

	cfg.Plugins.Instances = candidate
	if err := config.Save(cfg); err != nil {
		return PluginInstanceCreationResultDTO{}, fmt.Errorf("save plugin instance configuration failed")
	}
	a.pluginInventory.replaceDeclarations(cfg.Plugins.Instances)

	return PluginInstanceCreationResultDTO{
		InstanceID:      instanceID,
		ExtensionID:     extensionID,
		CapabilityID:    capabilityID,
		RestartRequired: true,
	}, nil
}

// generatePluginInstanceID produces a stable, unique instance ID without
// requiring the user to understand runtime instance identity. Collisions
// against a UUIDv7 suffix are not expected; the bounded retry only protects
// against pathological test fixtures reusing identical IDs.
func generatePluginInstanceID(extensionID string, existing []config.PluginInstanceConfig) (string, error) {
	existingIDs := make(map[string]struct{}, len(existing))
	for _, instance := range existing {
		existingIDs[instance.ID] = struct{}{}
	}
	for attempt := 0; attempt < 5; attempt++ {
		suffix, err := identity.New()
		if err != nil {
			return "", fmt.Errorf("generate plugin instance identifier failed")
		}
		candidate := fmt.Sprintf("%s-%s", extensionID, suffix)
		if _, exists := existingIDs[candidate]; !exists {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("generate a unique plugin instance identifier failed")
}

// classifyInstanceMutationError maps a plugininstance.Resolve failure, raised
// while validating a candidate declaration list for some Plugin-instance
// mutation (create, enable, or disable), to a safe, product-facing message.
// A failure attributed to an index other than the declaration this mutation
// actually targeted means a different, pre-existing declaration is already
// broken (e.g. its extension was removed outside this flow); that must never
// be misreported as a problem with the user's current action.
func classifyInstanceMutationError(err error, targetDeclarationIndex int) error {
	expectedPrefix := fmt.Sprintf("plugins.instances[%d]: ", targetDeclarationIndex)
	message := err.Error()
	if !strings.HasPrefix(message, expectedPrefix) {
		return fmt.Errorf("a different plugin instance's configuration is invalid; fix it first")
	}
	switch {
	case errors.Is(err, plugin.ErrExtensionNotFound):
		return fmt.Errorf("selected plugin is not installed")
	case errors.Is(err, plugin.ErrCapabilityUnsupported):
		return fmt.Errorf("selected capability is not available for this plugin")
	case errors.Is(err, plugin.ErrInstanceNotFound):
		return fmt.Errorf("plugin instance identity is invalid")
	default:
		return fmt.Errorf("%s", strings.TrimPrefix(message, expectedPrefix))
	}
}
