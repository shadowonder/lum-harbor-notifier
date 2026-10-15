package readadapter_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/plugintest"
	"lum-harbor-notifier/internal/plugin/readadapter"
	"lum-harbor-notifier/internal/readcap"
)

func readQuerySchema() plugin.PluginConfigSchema {
	return plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{
		{Key: "label", Label: "Label", Type: plugin.ConfigString},
		{Key: "delayMillis", Label: "Delay", Type: plugin.ConfigNumber},
	}}
}

func newReadQueryExtension(t *testing.T, root, id string) {
	t.Helper()
	plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
		ID: id, Name: "Read Fixture", Mode: "normal",
		CapabilityDeclarations: []plugin.CapabilityDeclaration{{
			ID:              plugin.CapabilityReadQuery,
			Description:     "Answers a bounded status lookup.",
			InputSemantics:  `{"query": string}`,
			ResultSemantics: `{"summary": string, "facts": [{"name": string, "value": string}]}`,
		}},
		Schema: readQuerySchema(),
	})
}

func newHost(t *testing.T, root string) *plugin.Host {
	t.Helper()
	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("Discover: err=%v failures=%v", err, failures)
	}
	host, err := plugin.NewHost(registry, plugin.HostOptions{
		StartupTimeout: 3 * time.Second, RequestTimeout: 500 * time.Millisecond,
		ShutdownTimeout: 150 * time.Millisecond,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	return host
}

func TestUnifiedDiscoveryListsCoreAndPluginDescriptors(t *testing.T) {
	root := t.TempDir()
	newReadQueryExtension(t, root, "read-plugin")
	registry, _, err := plugin.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	host := newHost(t, root)
	if err := host.Start(context.Background(), "read-plugin"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })

	instances := []plugin.Instance{{ID: "read-plugin-instance-1", ExtensionID: "read-plugin", CapabilityID: plugin.CapabilityReadQuery, Config: json.RawMessage(`{}`)}}
	source, err := readadapter.New(host, registry, instances)
	if err != nil {
		t.Fatal(err)
	}

	rf := newReadcapFixture(t, source)
	descriptors := rf.readcap.ListDescriptors()
	wantCoreIDs := []string{
		readcap.CapabilityWorkItemRead,
		readcap.CapabilityResolveExternal,
		readcap.CapabilitySearch,
		readcap.CapabilityAttentionReadForWorkItem,
		readcap.CapabilityHistoryReadForWorkItem,
		readcap.CapabilityWorkItemList,
	}
	if len(descriptors) != len(wantCoreIDs)+1 {
		t.Fatalf("expected %d CORE + 1 PLUGIN descriptor, got %d: %+v", len(wantCoreIDs), len(descriptors), descriptors)
	}
	for i, want := range wantCoreIDs {
		if descriptors[i].ID != want || descriptors[i].Origin != readcap.OriginCore {
			t.Fatalf("descriptor %d: expected CORE %q, got %+v", i, want, descriptors[i])
		}
	}
	pluginDescriptor := descriptors[len(wantCoreIDs)]
	if pluginDescriptor.Origin != readcap.OriginPlugin {
		t.Fatalf("expected PLUGIN origin, got %+v", pluginDescriptor)
	}
	if pluginDescriptor.ID != "plugin:read-plugin:read-plugin-instance-1" {
		t.Fatalf("unexpected plugin descriptor ID: %q", pluginDescriptor.ID)
	}
	if pluginDescriptor.Description == "" || pluginDescriptor.InputSemantics == "" || pluginDescriptor.ResultSemantics == "" {
		t.Fatalf("expected manifest semantic metadata to be carried through: %+v", pluginDescriptor)
	}

	// Defensive copy, same as the CORE-only guarantee.
	descriptors[0].ID = "mutated"
	if again := rf.readcap.ListDescriptors(); again[0].ID == "mutated" {
		t.Fatal("mutating a returned Descriptor slice affected internal state")
	}
}

func TestUnifiedDiscoveryExcludesNonReadCapabilities(t *testing.T) {
	root := t.TempDir()
	plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
		ID: "source-plugin", Mode: "normal",
		Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
	})
	registry, _, err := plugin.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	host := newHost(t, root)
	instances := []plugin.Instance{{ID: "source-instance", ExtensionID: "source-plugin", CapabilityID: plugin.CapabilitySourceFetch, Config: json.RawMessage(`{}`)}}
	source, err := readadapter.New(host, registry, instances)
	if err != nil {
		t.Fatal(err)
	}
	if len(source.ListDescriptors()) != 0 {
		t.Fatalf("expected zero descriptors for a non-read-query instance, got %+v", source.ListDescriptors())
	}
}

func TestUnifiedExecutionRunsRealPluginSubprocess(t *testing.T) {
	root := t.TempDir()
	newReadQueryExtension(t, root, "read-plugin")
	registry, _, err := plugin.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	host := newHost(t, root)
	if err := host.Start(context.Background(), "read-plugin"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })

	instances := []plugin.Instance{{ID: "instance-1", ExtensionID: "read-plugin", CapabilityID: plugin.CapabilityReadQuery, Config: json.RawMessage(`{"label":"Rally"}`)}}
	source, err := readadapter.New(host, registry, instances)
	if err != nil {
		t.Fatal(err)
	}
	rf := newReadcapFixture(t, source)

	descriptorID := "plugin:read-plugin:instance-1"
	result, err := rf.readcap.Execute(context.Background(), readcap.Request{
		CapabilityID: descriptorID,
		Input:        []byte(`{"query":"feature status"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	output, ok := result.Output.(readcap.PluginQueryOutput)
	if !ok {
		t.Fatalf("expected readcap.PluginQueryOutput, got %T", result.Output)
	}
	if !strings.Contains(output.Summary, "Rally") || !strings.Contains(output.Summary, "feature status") || !strings.Contains(output.Summary, "instance-1") {
		t.Fatalf("unexpected summary: %q", output.Summary)
	}
	if len(output.Facts) != 2 {
		t.Fatalf("expected 2 facts, got %+v", output.Facts)
	}
}

func TestUnifiedExecutionMultiInstanceIdentityAndConfigAreDistinct(t *testing.T) {
	root := t.TempDir()
	newReadQueryExtension(t, root, "read-plugin")
	registry, _, err := plugin.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	host := newHost(t, root)
	if err := host.Start(context.Background(), "read-plugin"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })

	instances := []plugin.Instance{
		{ID: "instance-a", ExtensionID: "read-plugin", CapabilityID: plugin.CapabilityReadQuery, Config: json.RawMessage(`{"label":"Alpha"}`)},
		{ID: "instance-b", ExtensionID: "read-plugin", CapabilityID: plugin.CapabilityReadQuery, Config: json.RawMessage(`{"label":"Bravo"}`)},
	}
	source, err := readadapter.New(host, registry, instances)
	if err != nil {
		t.Fatal(err)
	}
	rf := newReadcapFixture(t, source)

	descriptors := rf.readcap.ListDescriptors()
	pluginDescriptors := descriptors[len(descriptors)-2:]
	if pluginDescriptors[0].ID == pluginDescriptors[1].ID {
		t.Fatalf("expected two distinct descriptor IDs, got %+v", pluginDescriptors)
	}
	if pluginDescriptors[0].ID != "plugin:read-plugin:instance-a" || pluginDescriptors[1].ID != "plugin:read-plugin:instance-b" {
		t.Fatalf("unexpected descriptor IDs: %+v", pluginDescriptors)
	}

	resultA, err := rf.readcap.Execute(context.Background(), readcap.Request{CapabilityID: "plugin:read-plugin:instance-a", Input: []byte(`{"query":"status"}`)})
	if err != nil {
		t.Fatal(err)
	}
	resultB, err := rf.readcap.Execute(context.Background(), readcap.Request{CapabilityID: "plugin:read-plugin:instance-b", Input: []byte(`{"query":"status"}`)})
	if err != nil {
		t.Fatal(err)
	}
	outputA := resultA.Output.(readcap.PluginQueryOutput)
	outputB := resultB.Output.(readcap.PluginQueryOutput)
	if !strings.Contains(outputA.Summary, "Alpha") || !strings.Contains(outputA.Summary, "instance-a") {
		t.Fatalf("instance A result did not reflect its own identity/config: %q", outputA.Summary)
	}
	if !strings.Contains(outputB.Summary, "Bravo") || !strings.Contains(outputB.Summary, "instance-b") {
		t.Fatalf("instance B result did not reflect its own identity/config: %q", outputB.Summary)
	}
}

func TestUnifiedExecutionUnknownDescriptorIsRejected(t *testing.T) {
	root := t.TempDir()
	newReadQueryExtension(t, root, "read-plugin")
	registry, _, err := plugin.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	host := newHost(t, root)
	instances := []plugin.Instance{{ID: "instance-1", ExtensionID: "read-plugin", CapabilityID: plugin.CapabilityReadQuery, Config: json.RawMessage(`{}`)}}
	source, err := readadapter.New(host, registry, instances)
	if err != nil {
		t.Fatal(err)
	}
	rf := newReadcapFixture(t, source)

	_, err = rf.readcap.Execute(context.Background(), readcap.Request{CapabilityID: "plugin:read-plugin:no-such-instance", Input: []byte(`{"query":"x"}`)})
	if !errors.Is(err, readcap.ErrUnknownCapability) {
		t.Fatalf("expected ErrUnknownCapability, got %v", err)
	}
}

func TestUnifiedExecutionRejectsInvalidPluginOutput(t *testing.T) {
	root := t.TempDir()
	plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
		ID: "read-plugin", Mode: "normal",
		CapabilityDeclarations: []plugin.CapabilityDeclaration{{
			ID: plugin.CapabilityReadQuery, Description: "test fixture",
			InputSemantics: "test", ResultSemantics: "test",
		}},
		Schema: plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{
			{Key: "readQueryBehavior", Label: "Behavior", Type: plugin.ConfigString},
		}},
	})
	registry, _, err := plugin.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	host := newHost(t, root)
	if err := host.Start(context.Background(), "read-plugin"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })

	instances := []plugin.Instance{{
		ID: "instance-1", ExtensionID: "read-plugin", CapabilityID: plugin.CapabilityReadQuery,
		Config: json.RawMessage(fmt.Sprintf(`{"readQueryBehavior":%q}`, "too_many_facts")),
	}}
	source, err := readadapter.New(host, registry, instances)
	if err != nil {
		t.Fatal(err)
	}
	rf := newReadcapFixture(t, source)

	_, err = rf.readcap.Execute(context.Background(), readcap.Request{CapabilityID: "plugin:read-plugin:instance-1", Input: []byte(`{"query":"x"}`)})
	if err == nil {
		t.Fatal("expected the oversized fact list to be rejected")
	}
}

func TestUnifiedExecutionInvalidSerializedInputIsRejectedBeforeInvocation(t *testing.T) {
	root := t.TempDir()
	newReadQueryExtension(t, root, "read-plugin")
	registry, _, err := plugin.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	host := newHost(t, root)
	// Deliberately never started: if Execute reached the Plugin, this would
	// fail with a "not started" error, not ErrInvalidInput.
	instances := []plugin.Instance{{ID: "instance-1", ExtensionID: "read-plugin", CapabilityID: plugin.CapabilityReadQuery, Config: json.RawMessage(`{}`)}}
	source, err := readadapter.New(host, registry, instances)
	if err != nil {
		t.Fatal(err)
	}
	rf := newReadcapFixture(t, source)

	_, err = rf.readcap.Execute(context.Background(), readcap.Request{CapabilityID: "plugin:read-plugin:instance-1", Input: []byte(`not-json`)})
	if !errors.Is(err, readcap.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}
