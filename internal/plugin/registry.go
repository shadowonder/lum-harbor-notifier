package plugin

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type Extension struct {
	Manifest       Manifest
	Directory      string
	ExecutablePath string
}

type Instance struct {
	ID           string
	ExtensionID  string
	CapabilityID CapabilityID
	Config       json.RawMessage
}

type Registry struct {
	extensions map[string]Extension
}

func NewRegistry(extensions []Extension) (*Registry, error) {
	registry := &Registry{extensions: make(map[string]Extension, len(extensions))}
	for _, extension := range extensions {
		if _, exists := registry.extensions[extension.Manifest.ID]; exists {
			return nil, fmt.Errorf("%w: duplicate extension id %q", ErrManifestInvalid, extension.Manifest.ID)
		}
		registry.extensions[extension.Manifest.ID] = cloneExtension(extension)
	}
	return registry, nil
}

func (r *Registry) List() []Extension {
	values := make([]Extension, 0, len(r.extensions))
	for _, extension := range r.extensions {
		values = append(values, cloneExtension(extension))
	}
	sort.Slice(values, func(i, j int) bool { return values[i].Manifest.ID < values[j].Manifest.ID })
	return values
}

func (r *Registry) Find(id string) (Extension, error) {
	extension, exists := r.extensions[id]
	if !exists {
		return Extension{}, fmt.Errorf("%w: %s", ErrExtensionNotFound, id)
	}
	return cloneExtension(extension), nil
}

func (r *Registry) ResolveCapability(extensionID string, capabilityID CapabilityID) (CapabilityDeclaration, error) {
	extension, err := r.Find(extensionID)
	if err != nil {
		return CapabilityDeclaration{}, err
	}
	for _, capability := range extension.Manifest.Capabilities {
		if capability.ID == capabilityID {
			return capability, nil
		}
	}
	return CapabilityDeclaration{}, fmt.Errorf("%w: extension %q capability %q", ErrCapabilityUnsupported, extensionID, capabilityID)
}

func (r *Registry) ValidateInstance(instance Instance) error {
	if strings.TrimSpace(instance.ID) == "" {
		return fmt.Errorf("%w: id must not be empty", ErrInstanceNotFound)
	}
	extension, err := r.Find(instance.ExtensionID)
	if err != nil {
		return err
	}
	if _, err := r.ResolveCapability(instance.ExtensionID, instance.CapabilityID); err != nil {
		return err
	}
	if err := extension.Manifest.Configuration.ValidateDocument(instance.Config); err != nil {
		return fmt.Errorf("instance %q configuration invalid: %w", instance.ID, err)
	}
	return nil
}

func cloneExtension(extension Extension) Extension {
	cloned := extension
	cloned.Manifest.Capabilities = append([]CapabilityDeclaration(nil), extension.Manifest.Capabilities...)
	cloned.Manifest.Bindings = append([]BindingDeclaration(nil), extension.Manifest.Bindings...)
	cloned.Manifest.Configuration.Fields = append([]PluginConfigField(nil), extension.Manifest.Configuration.Fields...)
	for index := range cloned.Manifest.Configuration.Fields {
		field := &cloned.Manifest.Configuration.Fields[index]
		field.Default = append(json.RawMessage(nil), field.Default...)
		field.Options = append([]ConfigOption(nil), field.Options...)
	}
	return cloned
}

func resolvedExecutablePath(extensionDir, executable string) (string, error) {
	path := filepath.Join(extensionDir, filepath.Clean(executable))
	directoryReal, err := filepath.EvalSymlinks(extensionDir)
	if err != nil {
		return "", fmt.Errorf("resolve extension directory: %w", err)
	}
	executableReal, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", fmt.Errorf("resolve executable: %w", err)
	}
	relative, err := filepath.Rel(directoryReal, executableReal)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("executable escapes the extension directory")
	}
	return executableReal, nil
}
