package plugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

const (
	ManifestFileName              = "plugin.json"
	SupportedManifestVersion      = 1
	SupportedProtocolVersion      = 1
	SupportedPackageFormatVersion = 1
	// maxCapabilityMetadataRunes bounds read.query's required semantic
	// metadata fields (M15 Closeout). It exists only so a future AI/tool-
	// calling caller (M16) never sees an unbounded usage-text blob; it is
	// not a general manifest text limit and does not apply to any other
	// capability's (optional, unbounded) metadata.
	maxCapabilityMetadataRunes = 500
)

type CapabilityID string

const (
	CapabilitySourceFetch  CapabilityID = "source.fetch"
	CapabilityAIPreRequest CapabilityID = "ai.pre_request"
	CapabilityAIProvider   CapabilityID = "ai.provider"
	// CapabilityAIRuntime is the high-level resumable reasoning contract. An
	// implementation receives the full AI-visible catalog and may request
	// Host-executed capabilities before returning a canonical final result.
	CapabilityAIRuntime      CapabilityID = "ai.runtime"
	CapabilityAIPostResponse CapabilityID = "ai.post_response"
	// CapabilityReadQuery is the M15 Slice 2 Host-owned typed read contract:
	// a bounded, opaque query string in, a bounded summary/facts result out.
	// See ReadQueryRequest/ReadQueryResult in read_query.go.
	CapabilityReadQuery CapabilityID = "read.query"
	// CapabilityNotificationDeliver is the M17 Slice 1 Host-owned typed
	// external-delivery contract: a platform-neutral delivery request in
	// (stable delivery ID, title, message, urgency), success or an error
	// out. See NotificationDeliveryRequest/NotificationDeliveryResult in
	// notification_deliver.go.
	CapabilityNotificationDeliver CapabilityID = "notification.deliver"
)

type BindingType string

const (
	BindingAIPre      BindingType = "AI_PRE"
	BindingAIProvider BindingType = "AI_PROVIDER"
	BindingAIRuntime  BindingType = "AI_RUNTIME"
	BindingAIPost     BindingType = "AI_POST"
	BindingSchedule   BindingType = "SCHEDULE"
)

type Manifest struct {
	PackageFormatVersion int                     `json:"packageFormatVersion,omitempty"`
	OS                   []string                `json:"os,omitempty"`
	Arch                 []string                `json:"arch,omitempty"`
	ManifestVersion      int                     `json:"manifestVersion"`
	ID                   string                  `json:"id"`
	Name                 string                  `json:"name"`
	Version              string                  `json:"version"`
	ProtocolVersion      int                     `json:"protocolVersion"`
	Executable           string                  `json:"executable"`
	Capabilities         []CapabilityDeclaration `json:"capabilities"`
	Bindings             []BindingDeclaration    `json:"bindings,omitempty"`
	Configuration        PluginConfigSchema      `json:"configuration"`
}

type CapabilityDeclaration struct {
	ID CapabilityID `json:"id"`
	// Description, InputSemantics, and ResultSemantics are optional,
	// free-text usage metadata for a future AI/tool-calling caller (M16) —
	// what this capability does, what input it expects, and what result it
	// returns. They are declaration-time documentation only, exactly like
	// PluginConfigField.Description; the host, never the manifest, still
	// owns the actual typed request/result contract and its validation
	// (M15 Slice 2, Plugin Runtime Contract V1 §3/§9).
	Description     string `json:"description,omitempty"`
	InputSemantics  string `json:"inputSemantics,omitempty"`
	ResultSemantics string `json:"resultSemantics,omitempty"`
}

type BindingDeclaration struct {
	ID                     string       `json:"id"`
	Type                   BindingType  `json:"type"`
	Capability             CapabilityID `json:"capability"`
	Actions                []string     `json:"actions,omitempty"`
	Order                  int          `json:"order,omitempty"`
	DefaultIntervalSeconds int          `json:"defaultIntervalSeconds,omitempty"`
}

var pluginIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:[._-][a-z0-9]+)*$`)

// IsValidPluginID reports whether id conforms to Lum Harbor's plugin ID format.
func IsValidPluginID(id string) bool {
	return pluginIDPattern.MatchString(id)
}

func LoadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("%w: read %q: %v", ErrManifestInvalid, path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("%w: decode %q: %v", ErrManifestInvalid, path, err)
	}
	if err := ensureDecoderEOF(decoder); err != nil {
		return Manifest{}, fmt.Errorf("%w: manifest %q: %v", ErrManifestInvalid, path, err)
	}
	directory := filepath.Dir(path)
	if err := manifest.Validate(directory); err != nil {
		return Manifest{}, err
	}
	manifest.sortBindings()
	return manifest, nil
}

func (m Manifest) Validate(extensionDir string) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("%w: extension %q: %s", ErrManifestInvalid, m.ID, fmt.Sprintf(format, args...))
	}
	if m.PackageFormatVersion < 0 || m.PackageFormatVersion > SupportedPackageFormatVersion {
		return fail("unsupported packageFormatVersion %d", m.PackageFormatVersion)
	}
	for _, osName := range m.OS {
		if strings.TrimSpace(osName) == "" {
			return fail("os must not contain empty values")
		}
	}
	for _, archName := range m.Arch {
		if strings.TrimSpace(archName) == "" {
			return fail("arch must not contain empty values")
		}
	}
	if m.ManifestVersion != SupportedManifestVersion {
		return fail("unsupported manifestVersion %d", m.ManifestVersion)
	}
	if !pluginIDPattern.MatchString(m.ID) {
		return fail("id must match %s", pluginIDPattern.String())
	}
	if strings.TrimSpace(m.Name) == "" {
		return fail("name must not be empty")
	}
	if strings.TrimSpace(m.Version) == "" {
		return fail("version must not be empty")
	}
	if m.ProtocolVersion != SupportedProtocolVersion {
		return fmt.Errorf("%w: extension %q protocolVersion %d", ErrUnsupportedProtocol, m.ID, m.ProtocolVersion)
	}
	if err := validateExecutablePath(extensionDir, m.Executable); err != nil {
		return fail("%v", err)
	}
	declared := make(map[CapabilityID]struct{}, len(m.Capabilities))
	for _, capability := range m.Capabilities {
		if strings.TrimSpace(string(capability.ID)) == "" {
			return fail("capability id must not be empty")
		}
		if _, exists := declared[capability.ID]; exists {
			return fail("duplicate capability %q", capability.ID)
		}
		declared[capability.ID] = struct{}{}
		if capability.ID == CapabilityReadQuery {
			if err := validateReadQueryCapabilityMetadata(capability); err != nil {
				return fail("capability %q: %v", capability.ID, err)
			}
		}
	}
	if len(declared) == 0 {
		return fail("at least one capability is required")
	}
	seenBindings := make(map[string]struct{}, len(m.Bindings))
	for _, binding := range m.Bindings {
		if strings.TrimSpace(binding.ID) == "" {
			return fail("binding id must not be empty")
		}
		if _, exists := seenBindings[binding.ID]; exists {
			return fail("duplicate binding %q", binding.ID)
		}
		seenBindings[binding.ID] = struct{}{}
		if _, exists := declared[binding.Capability]; !exists {
			return fail("binding %q references undeclared capability %q", binding.ID, binding.Capability)
		}
		if binding.Order < 0 {
			return fail("binding %q order must not be negative", binding.ID)
		}
		if err := validateBindingDeclaration(binding); err != nil {
			return fail("binding %q: %v", binding.ID, err)
		}
	}
	if err := m.Configuration.Validate(); err != nil {
		return fail("configuration: %v", err)
	}
	return nil
}

// validateReadQueryCapabilityMetadata requires read.query's Description,
// InputSemantics, and ResultSemantics to be present and bounded (M15
// Closeout). Unlike every other capability's optional metadata, read.query
// is discovered and exposed to a future AI/tool-calling caller (M16) purely
// from this text — a Descriptor with no semantic content would be useless
// (or actively misleading) to that caller, so it is required here, not left
// optional like PluginConfigField.Description.
func validateReadQueryCapabilityMetadata(capability CapabilityDeclaration) error {
	fields := []struct {
		name  string
		value string
	}{
		{"description", capability.Description},
		{"inputSemantics", capability.InputSemantics},
		{"resultSemantics", capability.ResultSemantics},
	}
	for _, field := range fields {
		trimmed := strings.TrimSpace(field.value)
		if trimmed == "" {
			return fmt.Errorf("%s must not be empty", field.name)
		}
		if utf8.RuneCountInString(trimmed) > maxCapabilityMetadataRunes {
			return fmt.Errorf("%s exceeds %d characters", field.name, maxCapabilityMetadataRunes)
		}
	}
	return nil
}

func validateBindingDeclaration(binding BindingDeclaration) error {
	expected := map[BindingType]CapabilityID{
		BindingAIPre:      CapabilityAIPreRequest,
		BindingAIProvider: CapabilityAIProvider,
		BindingAIRuntime:  CapabilityAIRuntime,
		BindingAIPost:     CapabilityAIPostResponse,
	}
	switch binding.Type {
	case BindingAIPre, BindingAIProvider, BindingAIPost, BindingAIRuntime:
		if binding.Capability != expected[binding.Type] {
			return fmt.Errorf("type %s requires capability %s", binding.Type, expected[binding.Type])
		}
		if len(binding.Actions) == 0 {
			return fmt.Errorf("AI actions must not be empty")
		}
		seen := make(map[string]struct{}, len(binding.Actions))
		for _, action := range binding.Actions {
			if strings.TrimSpace(action) == "" {
				return fmt.Errorf("AI action must not be empty")
			}
			if _, exists := seen[action]; exists {
				return fmt.Errorf("duplicate AI action %q", action)
			}
			seen[action] = struct{}{}
		}
		if _, all := seen["*"]; all && len(seen) != 1 {
			return fmt.Errorf("wildcard action must be the only action")
		}
		if binding.DefaultIntervalSeconds != 0 {
			return fmt.Errorf("AI binding cannot declare a schedule interval")
		}
	case BindingSchedule:
		if len(binding.Actions) != 0 {
			return fmt.Errorf("schedule binding cannot declare AI actions")
		}
		if binding.DefaultIntervalSeconds <= 0 {
			return fmt.Errorf("schedule interval must be greater than zero")
		}
	default:
		return fmt.Errorf("unsupported type %q", binding.Type)
	}
	return nil
}

func validateExecutablePath(extensionDir, executable string) error {
	if strings.TrimSpace(executable) == "" {
		return fmt.Errorf("executable must not be empty")
	}
	if filepath.IsAbs(executable) {
		return fmt.Errorf("executable must be relative to the extension directory")
	}
	cleaned := filepath.Clean(executable)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return fmt.Errorf("executable escapes the extension directory")
	}
	joined := filepath.Join(extensionDir, cleaned)
	relative, err := filepath.Rel(extensionDir, joined)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("executable escapes the extension directory")
	}
	return nil
}

func (m *Manifest) sortBindings() {
	sort.SliceStable(m.Bindings, func(i, j int) bool {
		if m.Bindings[i].Order == m.Bindings[j].Order {
			return m.Bindings[i].ID < m.Bindings[j].ID
		}
		return m.Bindings[i].Order < m.Bindings[j].Order
	})
}

func (m Manifest) CapabilityIDs() []CapabilityID {
	values := make([]CapabilityID, 0, len(m.Capabilities))
	for _, capability := range m.Capabilities {
		values = append(values, capability.ID)
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	return values
}
