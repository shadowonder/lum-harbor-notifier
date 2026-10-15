package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugininstance"
)

// UpdatePluginInstanceConfigRequestDTO carries the instance ID and the
// updated non-secret configuration values. The frontend never supplies
// an extension ID, config file path, or secret values.
type UpdatePluginInstanceConfigRequestDTO struct {
	InstanceID string                 `json:"instanceId"`
	Config     map[string]interface{} `json:"config,omitempty"`
}

// UpdatePluginInstanceConfig updates the persisted configuration for an
// existing Plugin Instance declaration without changing its ID, extension,
// capability, or enabled state. It revalidates the candidate configuration
// against the extension's manifest schema and rejects any secret fields.
//
// This never mutates the current running process or live instance in place:
// desired-state changes take effect on the next application restart.
func (a *App) UpdatePluginInstanceConfig(request UpdatePluginInstanceConfigRequestDTO) (PluginInstanceMutationResultDTO, error) {
	if a.pluginInventory == nil {
		return PluginInstanceMutationResultDTO{}, fmt.Errorf("plugin inventory is not initialized")
	}
	instanceID := strings.TrimSpace(request.InstanceID)
	if instanceID == "" {
		return PluginInstanceMutationResultDTO{}, fmt.Errorf("select a plugin instance")
	}

	configDocument := json.RawMessage(`{}`)
	if request.Config != nil {
		encoded, err := json.Marshal(request.Config)
		if err != nil {
			return PluginInstanceMutationResultDTO{}, fmt.Errorf("plugin configuration must be a JSON object")
		}
		configDocument = encoded
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

	target := cfg.Plugins.Instances[targetIndex]
	extension, err := a.pluginInventory.registry.Find(target.ExtensionID)
	if err != nil {
		return PluginInstanceMutationResultDTO{}, fmt.Errorf("selected plugin is not installed")
	}

	// Reject secret fields explicitly
	for _, field := range extension.Manifest.Configuration.Fields {
		if field.Type == plugin.ConfigSecret {
			if _, exists := request.Config[field.Key]; exists {
				return PluginInstanceMutationResultDTO{}, fmt.Errorf("secret field %q requires dedicated secret storage", field.Key)
			}
		}
	}

	// Validate against manifest schema
	if err := extension.Manifest.Configuration.ValidateDocument(configDocument); err != nil {
		return PluginInstanceMutationResultDTO{}, err
	}

	candidate := append([]config.PluginInstanceConfig(nil), cfg.Plugins.Instances...)
	candidate[targetIndex].Config = configDocument

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
		ExtensionID:     target.ExtensionID,
		CapabilityID:    target.CapabilityID,
		Enabled:         !target.Disabled,
		RestartRequired: true,
	}, nil
}
