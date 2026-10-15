package plugin_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/plugintest"
)

func TestPluginFixtureProcess(t *testing.T) {
	if plugintest.RunIfRequested() {
		return
	}
}

func validManifest() plugin.Manifest {
	return plugin.Manifest{
		ManifestVersion: plugin.SupportedManifestVersion,
		ID:              "company-assistant", Name: "Company Assistant", Version: "1.0.0",
		ProtocolVersion: plugin.SupportedProtocolVersion, Executable: "bin/plugin",
		Capabilities: []plugin.CapabilityDeclaration{
			{ID: plugin.CapabilityAIPreRequest},
			{ID: plugin.CapabilityAIProvider},
			{ID: plugin.CapabilityAIPostResponse},
		},
		Bindings: []plugin.BindingDeclaration{
			{ID: "pre", Type: plugin.BindingAIPre, Capability: plugin.CapabilityAIPreRequest, Actions: []string{"source.interpret"}, Order: 10},
			{ID: "provider", Type: plugin.BindingAIProvider, Capability: plugin.CapabilityAIProvider, Actions: []string{"*"}, Order: 20},
			{ID: "post", Type: plugin.BindingAIPost, Capability: plugin.CapabilityAIPostResponse, Actions: []string{"*"}, Order: 30},
		},
		Configuration: plugin.PluginConfigSchema{Version: 1},
	}
}

func TestValidManifestLoads(t *testing.T) {
	directory := t.TempDir()
	manifest := validManifest()
	data, _ := json.Marshal(manifest)
	path := filepath.Join(directory, plugin.ManifestFileName)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := plugin.LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest: %v", err)
	}
	if loaded.ID != manifest.ID || len(loaded.Capabilities) != 3 || loaded.Bindings[0].ID != "pre" {
		t.Fatalf("unexpected manifest: %+v", loaded)
	}
}

func TestManifestRejectsTrailingJSON(t *testing.T) {
	directory := t.TempDir()
	manifest := validManifest()
	data, _ := json.Marshal(manifest)
	path := filepath.Join(directory, plugin.ManifestFileName)
	if err := os.WriteFile(path, append(data, []byte(` {}`)...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.LoadManifest(path); !errors.Is(err, plugin.ErrManifestInvalid) {
		t.Fatalf("expected invalid manifest, got %v", err)
	}
}

func TestManifestValidationFailures(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*plugin.Manifest)
		want   string
	}{
		{name: "unsupported manifest", mutate: func(m *plugin.Manifest) { m.ManifestVersion = 9 }, want: "manifestVersion"},
		{name: "unsupported package format", mutate: func(m *plugin.Manifest) { m.PackageFormatVersion = 9 }, want: "packageFormatVersion"},
		{name: "negative package format", mutate: func(m *plugin.Manifest) { m.PackageFormatVersion = -1 }, want: "packageFormatVersion"},
		{name: "empty os entry", mutate: func(m *plugin.Manifest) { m.OS = []string{"darwin", " "} }, want: "os"},
		{name: "empty arch entry", mutate: func(m *plugin.Manifest) { m.Arch = []string{" "} }, want: "arch"},
		{name: "invalid id", mutate: func(m *plugin.Manifest) { m.ID = "Bad ID" }, want: "id must match"},
		{name: "empty name", mutate: func(m *plugin.Manifest) { m.Name = "" }, want: "name"},
		{name: "unsupported protocol", mutate: func(m *plugin.Manifest) { m.ProtocolVersion = 9 }, want: "protocolVersion"},
		{name: "duplicate capability", mutate: func(m *plugin.Manifest) { m.Capabilities = append(m.Capabilities, m.Capabilities[0]) }, want: "duplicate capability"},
		{name: "missing capability", mutate: func(m *plugin.Manifest) { m.Bindings[0].Capability = plugin.CapabilityID("missing") }, want: "undeclared capability"},
		{name: "empty AI actions", mutate: func(m *plugin.Manifest) { m.Bindings[0].Actions = nil }, want: "actions"},
		{name: "mixed wildcard", mutate: func(m *plugin.Manifest) { m.Bindings[0].Actions = []string{"*", "planner.proposal"} }, want: "wildcard"},
		{name: "path escape", mutate: func(m *plugin.Manifest) { m.Executable = "../../../binary" }, want: "escapes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := validManifest()
			test.mutate(&manifest)
			err := manifest.Validate(t.TempDir())
			if !errors.Is(err, plugin.ErrManifestInvalid) && !errors.Is(err, plugin.ErrUnsupportedProtocol) {
				t.Fatalf("expected classified manifest error, got %v", err)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %q in %v", test.want, err)
			}
		})
	}
}

func TestPluginConfigSchemaAndDocumentValidation(t *testing.T) {
	schema := plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{
		{Key: "endpoint", Label: "Endpoint", Type: plugin.ConfigString, Required: true, Visibility: plugin.VisibilityPrimary},
		{Key: "timeout", Label: "Timeout", Type: plugin.ConfigNumber},
		{Key: "enabled", Label: "Enabled", Type: plugin.ConfigBoolean},
		{Key: "mode", Label: "Mode", Type: plugin.ConfigEnum, Options: []plugin.ConfigOption{{Label: "Safe", Value: "safe"}}},
		{Key: "token", Label: "Token", Type: plugin.ConfigSecret, Visibility: plugin.VisibilityExpert},
	}}
	if err := schema.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if err := schema.ValidateDocument(json.RawMessage(`{"endpoint":"local","timeout":3,"enabled":true,"mode":"safe","token":"secret"}`)); err != nil {
		t.Fatalf("ValidateDocument: %v", err)
	}
	for _, document := range []string{`{}`, `{"endpoint":false}`, `{"endpoint":"ok","mode":"bad"}`, `{"endpoint":"ok","extra":1}`, `{"endpoint":"ok"} {}`} {
		if err := schema.ValidateDocument(json.RawMessage(document)); err == nil {
			t.Fatalf("expected %s to fail", document)
		} else if strings.Contains(err.Error(), "secret") {
			t.Fatalf("error leaked secret value: %v", err)
		}
	}
	duplicate := schema
	duplicate.Fields = append(duplicate.Fields, duplicate.Fields[0])
	if err := duplicate.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate key failure, got %v", err)
	}
}

func TestSecretFieldManifestDefaultContract(t *testing.T) {
	valid := plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{
		{Key: "token", Label: "Token", Type: plugin.ConfigSecret, Required: true, Visibility: plugin.VisibilityExpert},
	}}
	if err := valid.Validate(); err != nil {
		t.Fatalf("secret without default must be valid: %v", err)
	}

	const secretValue = "super-sensitive-token"
	invalid := valid
	invalid.Fields = append([]plugin.PluginConfigField(nil), valid.Fields...)
	invalid.Fields[0].Default, _ = json.Marshal(secretValue)
	err := invalid.Validate()
	if err == nil || !strings.Contains(err.Error(), "field \"token\"") || !strings.Contains(err.Error(), "cannot define") {
		t.Fatalf("expected field-qualified secret default error, got %v", err)
	}
	if strings.Contains(err.Error(), secretValue) {
		t.Fatalf("secret validation error leaked value: %v", err)
	}
	manifest := validManifest()
	manifest.Configuration = invalid
	err = manifest.Validate(t.TempDir())
	if !errors.Is(err, plugin.ErrManifestInvalid) || strings.Contains(err.Error(), secretValue) {
		t.Fatalf("expected redacted invalid manifest error, got %v", err)
	}

	defaultEndpoint, _ := json.Marshal("https://example.test")
	nonSecret := plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{
		{Key: "endpoint", Label: "Endpoint", Type: plugin.ConfigString, Default: defaultEndpoint},
	}}
	if err := nonSecret.Validate(); err != nil {
		t.Fatalf("non-secret default behavior changed: %v", err)
	}
}

func TestDiscoveryRejectsExecutableSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	extensionDirectory := filepath.Join(root, "escaped")
	if err := os.MkdirAll(extensionDirectory, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.sh")
	if err := os.WriteFile(outside, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(extensionDirectory, "plugin.sh")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	manifest := validManifest()
	manifest.ID = "escaped"
	manifest.Executable = "plugin.sh"
	data, _ := json.Marshal(manifest)
	if err := os.WriteFile(filepath.Join(extensionDirectory, plugin.ManifestFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	registry, failures, err := plugin.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.List()) != 0 || len(failures) != 1 || !strings.Contains(failures[0].Err.Error(), "escapes") {
		t.Fatalf("expected symlink escape rejection, registry=%+v failures=%+v", registry.List(), failures)
	}
}

func TestDiscoveryKeepsValidExtensionsWhenAnotherManifestIsBad(t *testing.T) {
	root := t.TempDir()
	plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
		ID: "valid-source", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})
	bad := filepath.Join(root, "bad")
	if err := os.MkdirAll(bad, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, plugin.ManifestFileName), []byte(`{"manifestVersion":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	registry, failures, err := plugin.Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(registry.List()) != 1 || len(failures) != 1 {
		t.Fatalf("unexpected discovery: extensions=%d failures=%d", len(registry.List()), len(failures))
	}
	if _, err := registry.Find("valid-source"); err != nil {
		t.Fatalf("Find: %v", err)
	}
}

func TestUnknownCapabilityIsDiscoverable(t *testing.T) {
	root := t.TempDir()
	plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
		ID: "future-extension", Capabilities: []plugin.CapabilityID{"future.capability"},
	})
	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("Discover: err=%v failures=%v", err, failures)
	}
	extension, err := registry.Find("future-extension")
	if err != nil || len(extension.Manifest.Capabilities) != 1 || extension.Manifest.Capabilities[0].ID != "future.capability" {
		t.Fatalf("unknown capability was not preserved: extension=%+v err=%v", extension, err)
	}
}

func validReadQueryCapability() plugin.CapabilityDeclaration {
	return plugin.CapabilityDeclaration{
		ID:              plugin.CapabilityReadQuery,
		Description:     "Answers a bounded status lookup.",
		InputSemantics:  `{"query": string}`,
		ResultSemantics: `{"summary": string, "facts": [...]}`,
	}
}

func manifestWithReadQuery(capability plugin.CapabilityDeclaration) plugin.Manifest {
	m := validManifest()
	m.Capabilities = append(m.Capabilities, capability)
	return m
}

// TestReadQuerySemanticMetadataIsRequiredAndBounded guards the M15 Closeout
// fix: a read.query capability declaration must carry non-empty, bounded
// Description/InputSemantics/ResultSemantics, so a future AI/tool-calling
// caller (M16) can never see a PLUGIN Descriptor with no usable semantics.
// This requirement is scoped to read.query only — every other capability's
// metadata remains optional and unbounded, exactly like
// PluginConfigField.Description.
func TestReadQuerySemanticMetadataIsRequiredAndBounded(t *testing.T) {
	oversized := strings.Repeat("x", 501)

	tests := []struct {
		name    string
		mutate  func(*plugin.CapabilityDeclaration)
		wantErr bool
		want    string
	}{
		{name: "complete metadata is valid", mutate: func(c *plugin.CapabilityDeclaration) {}, wantErr: false},
		{name: "missing description", mutate: func(c *plugin.CapabilityDeclaration) { c.Description = "" }, wantErr: true, want: "description must not be empty"},
		{name: "whitespace-only description", mutate: func(c *plugin.CapabilityDeclaration) { c.Description = "   " }, wantErr: true, want: "description must not be empty"},
		{name: "missing input semantics", mutate: func(c *plugin.CapabilityDeclaration) { c.InputSemantics = "" }, wantErr: true, want: "inputSemantics must not be empty"},
		{name: "missing result semantics", mutate: func(c *plugin.CapabilityDeclaration) { c.ResultSemantics = "" }, wantErr: true, want: "resultSemantics must not be empty"},
		{name: "oversized description", mutate: func(c *plugin.CapabilityDeclaration) { c.Description = oversized }, wantErr: true, want: "description exceeds 500 characters"},
		{name: "oversized input semantics", mutate: func(c *plugin.CapabilityDeclaration) { c.InputSemantics = oversized }, wantErr: true, want: "inputSemantics exceeds 500 characters"},
		{name: "oversized result semantics", mutate: func(c *plugin.CapabilityDeclaration) { c.ResultSemantics = oversized }, wantErr: true, want: "resultSemantics exceeds 500 characters"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			capability := validReadQueryCapability()
			test.mutate(&capability)
			manifest := manifestWithReadQuery(capability)
			err := manifest.Validate(t.TempDir())
			if test.wantErr {
				if !errors.Is(err, plugin.ErrManifestInvalid) {
					t.Fatalf("expected ErrManifestInvalid, got %v", err)
				}
				if !strings.Contains(err.Error(), test.want) {
					t.Fatalf("expected %q in %v", test.want, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("expected valid manifest, got %v", err)
			}
		})
	}
}

// TestNonReadQueryCapabilitiesAreNotRequiredToHaveSemanticMetadata confirms
// the read.query-only scope of the new requirement: source.fetch and every
// AI capability (and validManifest's own baseline AI-only manifest) remain
// valid with zero metadata, exactly as before this Closeout round.
func TestNonReadQueryCapabilitiesAreNotRequiredToHaveSemanticMetadata(t *testing.T) {
	if err := validManifest().Validate(t.TempDir()); err != nil {
		t.Fatalf("expected the existing AI-only manifest to remain valid with no capability metadata: %v", err)
	}

	sourceFetchManifest := validManifest()
	sourceFetchManifest.Capabilities = append(sourceFetchManifest.Capabilities, plugin.CapabilityDeclaration{ID: plugin.CapabilitySourceFetch})
	if err := sourceFetchManifest.Validate(t.TempDir()); err != nil {
		t.Fatalf("expected a bare source.fetch declaration (no metadata) to remain valid: %v", err)
	}
}
