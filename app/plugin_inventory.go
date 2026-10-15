package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/plugin"
)

// PluginRuntimeSnapshotter is the current-lifetime process-state boundary used
// by the read-only Settings projection.
type PluginRuntimeSnapshotter interface {
	Snapshot() plugin.RuntimeSnapshot
}

type pluginInventoryRuntime interface {
	PluginRuntimeSnapshotter
	plugin.Invoker
}

// PluginInventoryReader combines immutable startup discovery with the latest
// persisted configured declarations (both enabled and disabled) and a current
// Host snapshot. It never scans or reloads config. For an enabled, running
// notification.deliver Instance it also uses that capability's narrow
// CHECK_READINESS operation so Plugin detail can show the current native
// permission; that operation may invoke the OS-owned prompt while status is
// notDetermined, but creates no Lum Harbor or platform notification.
// Declarations are the product/control-plane fact ("what the user configured,
// enabled or not"): they start as the startup-loaded set and are replaced only
// by this process's own successful desired-config saves, so Settings shows
// desired Enabled/Disabled truth before the restart that applies it. The
// current Host snapshot and this capability-specific permission status are
// the only runtime facts mixed into the projection.
type PluginInventoryReader struct {
	registry  *plugin.Registry
	runtime   pluginInventoryRuntime
	mu        sync.RWMutex
	instances []ConfiguredPluginInstanceDTO
}

type PluginInventoryDTO struct {
	Plugins []PluginInventoryItemDTO `json:"plugins"`
}

type PluginInventoryItemDTO struct {
	ID                     string                        `json:"id"`
	Name                   string                        `json:"name"`
	Version                string                        `json:"version"`
	ProtocolVersion        int                           `json:"protocolVersion"`
	Installed              bool                          `json:"installed"`
	Selected               bool                          `json:"selected"`
	Running                bool                          `json:"running"`
	RuntimeStatus          string                        `json:"runtimeStatus"`
	Capabilities           []string                      `json:"capabilities"`
	SelectableCapabilities []string                      `json:"selectableCapabilities"`
	Bindings               []PluginBindingDTO            `json:"bindings"`
	Instances              []ConfiguredPluginInstanceDTO `json:"instances"`
	ConfigSchema           []PluginConfigFieldDTO        `json:"configSchema"`
}

// PluginConfigFieldDTO projects one manifest configuration schema field.
// Secret fields are included so the frontend can detect a required secret
// and refuse to offer Instance creation for it; secret values themselves
// never appear here because the manifest schema forbids secret defaults.
type PluginConfigFieldDTO struct {
	Key         string                  `json:"key"`
	Label       string                  `json:"label"`
	Description string                  `json:"description,omitempty"`
	Type        string                  `json:"type"`
	Required    bool                    `json:"required"`
	Default     any                     `json:"default,omitempty"`
	Options     []PluginConfigOptionDTO `json:"options,omitempty"`
	Visibility  string                  `json:"visibility,omitempty"`
}

type PluginConfigOptionDTO struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type PluginBindingDTO struct {
	ID           string   `json:"id"`
	Type         string   `json:"type"`
	CapabilityID string   `json:"capabilityId"`
	Actions      []string `json:"actions"`
}

type ConfiguredPluginInstanceDTO struct {
	ID                              string                 `json:"id"`
	ExtensionID                     string                 `json:"extensionId"`
	CapabilityID                    string                 `json:"capabilityId"`
	Enabled                         bool                   `json:"enabled"`
	Config                          map[string]interface{} `json:"config,omitempty"`
	NativeNotificationAuthorization string                 `json:"nativeNotificationAuthorization,omitempty"`
}

// NewPluginInventoryReader takes the startup-loaded configured declarations
// (both enabled and disabled) rather than resolved runtime Instances, so a
// disabled declaration remains visible for the user to Re-enable after
// restart instead of disappearing from Settings entirely.
func NewPluginInventoryReader(registry *plugin.Registry, declarations []config.PluginInstanceConfig, runtime pluginInventoryRuntime) (*PluginInventoryReader, error) {
	if registry == nil {
		return nil, fmt.Errorf("plugin inventory registry must not be nil")
	}
	if runtime == nil {
		return nil, fmt.Errorf("plugin inventory runtime must not be nil")
	}
	return &PluginInventoryReader{registry: registry, instances: projectDeclarations(declarations), runtime: runtime}, nil
}

func projectDeclarations(declarations []config.PluginInstanceConfig) []ConfiguredPluginInstanceDTO {
	projectedInstances := make([]ConfiguredPluginInstanceDTO, 0, len(declarations))
	for _, declaration := range declarations {
		var cfgMap map[string]interface{}
		if len(declaration.Config) > 0 && string(declaration.Config) != "null" {
			_ = json.Unmarshal(declaration.Config, &cfgMap)
		}
		projectedInstances = append(projectedInstances, ConfiguredPluginInstanceDTO{
			ID: declaration.ID, ExtensionID: declaration.ExtensionID, CapabilityID: declaration.CapabilityID,
			Enabled: !declaration.Disabled,
			Config:  cfgMap,
		})
	}
	return projectedInstances
}

// replaceDeclarations records the desired declarations a Plugin mutation just
// persisted. It changes only the desired-state projection, never the startup
// Registry, Host, or Scheduler.
func (r *PluginInventoryReader) replaceDeclarations(declarations []config.PluginInstanceConfig) {
	projected := projectDeclarations(declarations)
	r.mu.Lock()
	r.instances = projected
	r.mu.Unlock()
}

func (r *PluginInventoryReader) Read() PluginInventoryDTO {
	runtimeByID := make(map[string]plugin.ExtensionRuntimeSnapshot)
	for _, snapshot := range r.runtime.Snapshot().Extensions {
		runtimeByID[snapshot.ID] = snapshot
	}
	r.mu.RLock()
	declarations := r.instances
	r.mu.RUnlock()
	instancesByExtension := make(map[string][]ConfiguredPluginInstanceDTO)
	for _, instance := range declarations {
		instancesByExtension[instance.ExtensionID] = append(instancesByExtension[instance.ExtensionID], instance)
	}

	extensions := r.registry.List()
	result := PluginInventoryDTO{Plugins: make([]PluginInventoryItemDTO, 0, len(extensions))}
	for _, extension := range extensions {
		manifest := extension.Manifest
		instances := instancesByExtension[manifest.ID]
		selected := false
		for _, instance := range instances {
			if instance.Enabled {
				selected = true
				break
			}
		}
		sanitizedInstances := make([]ConfiguredPluginInstanceDTO, 0, len(instances))
		for _, inst := range instances {
			instCopy := inst
			instCopy.Config = sanitizeInstanceConfig(manifest.Configuration, inst.Config)
			if inst.Enabled && inst.CapabilityID == string(plugin.CapabilityNotificationDeliver) {
				if snapshot, exists := runtimeByID[manifest.ID]; exists && snapshot.Running {
					readiness, err := plugin.NewNotificationDeliverer(r.runtime).CheckReadiness(
						context.Background(),
						plugin.Instance{ID: inst.ID, ExtensionID: inst.ExtensionID, CapabilityID: plugin.CapabilityNotificationDeliver},
					)
					if err != nil {
						instCopy.NativeNotificationAuthorization = "CHECK_FAILED"
					} else {
						instCopy.NativeNotificationAuthorization = string(readiness.AuthorizationStatus)
					}
				}
			}
			sanitizedInstances = append(sanitizedInstances, instCopy)
		}

		item := PluginInventoryItemDTO{
			ID: manifest.ID, Name: manifest.Name, Version: manifest.Version,
			ProtocolVersion: manifest.ProtocolVersion, Installed: true, Selected: selected,
			Capabilities:  make([]string, 0, len(manifest.Capabilities)),
			Bindings:      make([]PluginBindingDTO, 0, len(manifest.Bindings)),
			Instances:     sanitizedInstances,
			RuntimeStatus: "DISCOVERED",
			ConfigSchema:  projectPluginConfigSchema(manifest.Configuration),
		}
		for _, capability := range manifest.CapabilityIDs() {
			item.Capabilities = append(item.Capabilities, string(capability))
			if plugin.SupportsCapability(capability) {
				item.SelectableCapabilities = append(item.SelectableCapabilities, string(capability))
			}
		}
		for _, binding := range manifest.Bindings {
			// A SCHEDULE binding (or any binding with no declared Actions,
			// e.g. reminders-eventkit-bridge's source.fetch SCHEDULE
			// binding) has a nil binding.Actions; append([]string(nil), ...)
			// with nothing to append returns nil, not []string{}, so this
			// field must be built with make() to serialize as JSON "[]"
			// rather than "null" — the frontend calls .length on it
			// unconditionally.
			item.Bindings = append(item.Bindings, PluginBindingDTO{
				ID: binding.ID, Type: string(binding.Type), CapabilityID: string(binding.Capability),
				Actions: append(make([]string, 0, len(binding.Actions)), binding.Actions...),
			})
		}
		if snapshot, exists := runtimeByID[manifest.ID]; exists {
			item.Running = snapshot.Running
			item.RuntimeStatus = snapshot.Status
		}
		result.Plugins = append(result.Plugins, item)
	}
	return result
}

// projectPluginConfigSchema maps a manifest configuration schema to its
// frontend-safe projection. It never includes secret values: the manifest
// schema already forbids a default on a secret field.
func projectPluginConfigSchema(schema plugin.PluginConfigSchema) []PluginConfigFieldDTO {
	fields := make([]PluginConfigFieldDTO, 0, len(schema.Fields))
	for _, field := range schema.Fields {
		dto := PluginConfigFieldDTO{
			Key: field.Key, Label: field.Label, Description: field.Description,
			Type: string(field.Type), Required: field.Required, Visibility: string(field.Visibility),
		}
		if len(field.Default) > 0 {
			var decoded any
			if err := json.Unmarshal(field.Default, &decoded); err == nil {
				dto.Default = decoded
			}
		}
		for _, option := range field.Options {
			dto.Options = append(dto.Options, PluginConfigOptionDTO{Label: option.Label, Value: option.Value})
		}
		fields = append(fields, dto)
	}
	return fields
}

// ExtensionName returns the human-readable name of an extension from the startup registry,
// or empty string if not found.
func (r *PluginInventoryReader) ExtensionName(id string) string {
	if r == nil || r.registry == nil {
		return ""
	}
	ext, err := r.registry.Find(id)
	if err != nil {
		return ""
	}
	return ext.Manifest.Name
}

// sanitizeInstanceConfig filters out any secret fields or fields not declared in the manifest schema,
// preventing secrets or undeclared fields from ever being exposed to the frontend inventory projection.
func sanitizeInstanceConfig(schema plugin.PluginConfigSchema, rawConfig map[string]interface{}) map[string]interface{} {
	if len(rawConfig) == 0 {
		return nil
	}
	publicKeys := make(map[string]bool)
	for _, field := range schema.Fields {
		if field.Type != plugin.ConfigSecret {
			publicKeys[field.Key] = true
		}
	}
	if len(publicKeys) == 0 {
		return nil
	}
	sanitized := make(map[string]interface{})
	for k, v := range rawConfig {
		if publicKeys[k] {
			sanitized[k] = v
		}
	}
	if len(sanitized) == 0 {
		return nil
	}
	return sanitized
}

// GetPluginInventory exposes a frontend-safe, read-only startup/runtime view.
func (a *App) GetPluginInventory() (PluginInventoryDTO, error) {
	if a.pluginInventory == nil {
		return PluginInventoryDTO{}, fmt.Errorf("plugin inventory is not initialized")
	}
	return a.pluginInventory.Read(), nil
}
