// Package plugininstance resolves persisted user selections against the
// read-only Plugin Registry assembled by startup discovery.
package plugininstance

import (
	"encoding/json"
	"fmt"
	"strings"

	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/plugin"
)

// Resolve converts enabled configured declarations to validated runtime
// Instances. It performs no process start, registry mutation, scheduling, or
// I/O.
//
// Every declaration, enabled or disabled, is checked for well-formed
// identity: non-empty ID/extensionId/capabilityId and ID uniqueness across
// the whole set. This runs before the enabled/disabled split so a disabled
// declaration can never collide with an enabled one's ID (which would make a
// future Re-enable ambiguous). A disabled declaration is otherwise skipped
// entirely: it is never checked against the current Registry/schema/Host
// support, so an installed-but-now-stale disabled Instance never blocks
// application startup. Re-enabling must run it back through this same
// function to prove it is still valid.
func Resolve(declarations []config.PluginInstanceConfig, registry *plugin.Registry) ([]plugin.Instance, error) {
	if registry == nil {
		return nil, fmt.Errorf("resolve configured plugin instances: registry must not be nil")
	}
	instances := make([]plugin.Instance, 0, len(declarations))
	seenIDs := make(map[string]int, len(declarations))
	for index, declaration := range declarations {
		field := fmt.Sprintf("plugins.instances[%d]", index)
		if strings.TrimSpace(declaration.ID) == "" {
			return nil, fmt.Errorf("%s.id must not be empty", field)
		}
		if first, exists := seenIDs[declaration.ID]; exists {
			return nil, fmt.Errorf("%s.id duplicates plugins.instances[%d].id %q", field, first, declaration.ID)
		}
		seenIDs[declaration.ID] = index
		if strings.TrimSpace(declaration.ExtensionID) == "" {
			return nil, fmt.Errorf("%s.extensionId must not be empty", field)
		}
		if strings.TrimSpace(declaration.CapabilityID) == "" {
			return nil, fmt.Errorf("%s.capabilityId must not be empty", field)
		}
		if declaration.Disabled {
			continue
		}

		capabilityID := plugin.CapabilityID(declaration.CapabilityID)
		instance := plugin.Instance{
			ID:           declaration.ID,
			ExtensionID:  declaration.ExtensionID,
			CapabilityID: capabilityID,
			Config:       append(json.RawMessage(nil), declaration.Config...),
		}
		if err := registry.ValidateInstance(instance); err != nil {
			return nil, fmt.Errorf("%s: %w", field, err)
		}
		if !plugin.SupportsCapability(capabilityID) {
			return nil, fmt.Errorf("%s: %w: %q", field, plugin.ErrCapabilityUnsupported, capabilityID)
		}
		extension, err := registry.Find(instance.ExtensionID)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", field, err)
		}
		if err := rejectPersistedSecrets(instance.Config, extension.Manifest.Configuration); err != nil {
			return nil, fmt.Errorf("%s: %w", field, err)
		}
		instances = append(instances, instance)
	}
	return instances, nil
}

func rejectPersistedSecrets(document json.RawMessage, schema plugin.PluginConfigSchema) error {
	if len(document) == 0 {
		return nil
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(document, &values); err != nil {
		return err // Registry validation normally makes this path unreachable.
	}
	for _, field := range schema.Fields {
		if field.Type == plugin.ConfigSecret {
			if _, exists := values[field.Key]; exists {
				return fmt.Errorf("secret field %q requires dedicated secret storage", field.Key)
			}
		}
	}
	return nil
}
