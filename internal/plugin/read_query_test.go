package plugin_test

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
)

func readQuerySchema() plugin.PluginConfigSchema {
	return plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{
		{Key: "label", Label: "Label", Type: plugin.ConfigString},
		{Key: "remoteError", Label: "Remote error", Type: plugin.ConfigBoolean},
		{Key: "delayMillis", Label: "Delay", Type: plugin.ConfigNumber},
		{Key: "readQueryBehavior", Label: "Behavior", Type: plugin.ConfigString},
	}}
}

func newReadQueryFixtureHost(t *testing.T) (*plugin.Host, plugin.Extension) {
	t.Helper()
	root := t.TempDir()
	plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
		ID: "read-fixture", Name: "Read Fixture", Mode: "normal",
		CapabilityDeclarations: []plugin.CapabilityDeclaration{{
			ID: plugin.CapabilityReadQuery, Description: "test fixture",
			InputSemantics: "test", ResultSemantics: "test",
		}},
		Schema: readQuerySchema(),
	})
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
	extension, _ := registry.Find("read-fixture")
	return host, extension
}

func TestReadQuerierHappyPathAgainstFixtureSubprocess(t *testing.T) {
	host, _ := newReadQueryFixtureHost(t)
	if err := host.Start(context.Background(), "read-fixture"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })

	instance := plugin.Instance{ID: "instance-1", ExtensionID: "read-fixture", CapabilityID: plugin.CapabilityReadQuery, Config: json.RawMessage(`{}`)}
	result, err := plugin.NewReadQuerier(host).Query(context.Background(), instance, plugin.ReadQueryRequest{Query: "feature status"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Summary, "feature status") || !strings.Contains(result.Summary, "instance-1") {
		t.Fatalf("unexpected summary: %q", result.Summary)
	}
	if len(result.Facts) != 2 {
		t.Fatalf("expected 2 facts, got %+v", result.Facts)
	}
}

func TestReadQuerierMultiInstanceConfigAndIdentity(t *testing.T) {
	host, _ := newReadQueryFixtureHost(t)
	if err := host.Start(context.Background(), "read-fixture"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })

	querier := plugin.NewReadQuerier(host)
	instanceA := plugin.Instance{ID: "instance-a", ExtensionID: "read-fixture", CapabilityID: plugin.CapabilityReadQuery, Config: json.RawMessage(`{"label":"Alpha"}`)}
	instanceB := plugin.Instance{ID: "instance-b", ExtensionID: "read-fixture", CapabilityID: plugin.CapabilityReadQuery, Config: json.RawMessage(`{"label":"Bravo"}`)}

	resultA, err := querier.Query(context.Background(), instanceA, plugin.ReadQueryRequest{Query: "status"})
	if err != nil {
		t.Fatal(err)
	}
	resultB, err := querier.Query(context.Background(), instanceB, plugin.ReadQueryRequest{Query: "status"})
	if err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(resultA.Summary, "Alpha:") || !strings.Contains(resultA.Summary, "instance-a") {
		t.Fatalf("instance A did not receive its own config/identity: %q", resultA.Summary)
	}
	if !strings.HasPrefix(resultB.Summary, "Bravo:") || !strings.Contains(resultB.Summary, "instance-b") {
		t.Fatalf("instance B did not receive its own config/identity: %q", resultB.Summary)
	}
	if resultA.Summary == resultB.Summary {
		t.Fatalf("two distinct instances produced identical results: %q", resultA.Summary)
	}
}

func TestReadQuerierRejectsWrongCapabilityInstance(t *testing.T) {
	host, _ := newReadQueryFixtureHost(t)
	instance := plugin.Instance{ID: "instance-1", ExtensionID: "read-fixture", CapabilityID: plugin.CapabilitySourceFetch, Config: json.RawMessage(`{}`)}
	_, err := plugin.NewReadQuerier(host).Query(context.Background(), instance, plugin.ReadQueryRequest{Query: "x"})
	if !errors.Is(err, plugin.ErrCapabilityUnsupported) {
		t.Fatalf("expected ErrCapabilityUnsupported, got %v", err)
	}
}

func TestReadQuerierRejectsEmptyQuery(t *testing.T) {
	host, _ := newReadQueryFixtureHost(t)
	instance := plugin.Instance{ID: "instance-1", ExtensionID: "read-fixture", CapabilityID: plugin.CapabilityReadQuery, Config: json.RawMessage(`{}`)}
	_, err := plugin.NewReadQuerier(host).Query(context.Background(), instance, plugin.ReadQueryRequest{Query: "   "})
	if err == nil {
		t.Fatal("expected an error for an empty query")
	}
}

func TestReadQuerierRejectsOversizedAndInvalidOutput(t *testing.T) {
	cases := []struct {
		name     string
		behavior string
	}{
		{"oversized summary", "oversized_summary"},
		{"too many facts", "too_many_facts"},
		{"invalid fact name", "invalid_fact_name"},
		{"malformed payload", "malformed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host, _ := newReadQueryFixtureHost(t)
			if err := host.Start(context.Background(), "read-fixture"); err != nil {
				t.Fatalf("Start: %v", err)
			}
			t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
			instance := plugin.Instance{
				ID: "instance-1", ExtensionID: "read-fixture", CapabilityID: plugin.CapabilityReadQuery,
				Config: json.RawMessage(fmt.Sprintf(`{"readQueryBehavior":%q}`, tc.behavior)),
			}
			_, err := plugin.NewReadQuerier(host).Query(context.Background(), instance, plugin.ReadQueryRequest{Query: "x"})
			if err == nil {
				t.Fatalf("expected %s to be rejected", tc.name)
			}
		})
	}
}

func TestReadQuerierRespectsHostRequestTimeout(t *testing.T) {
	host, _ := newReadQueryFixtureHost(t)
	if err := host.Start(context.Background(), "read-fixture"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })

	// newReadQueryFixtureHost's RequestTimeout is 500ms; ask the fixture to
	// delay well beyond that. No new cancellation/timeout logic was added to
	// ReadQuerier or read.query specifically — this proves it inherits
	// Host.Invoke's existing deadline behavior unmodified.
	instance := plugin.Instance{
		ID: "instance-1", ExtensionID: "read-fixture", CapabilityID: plugin.CapabilityReadQuery,
		Config: json.RawMessage(`{"delayMillis":2000}`),
	}
	start := time.Now()
	_, err := plugin.NewReadQuerier(host).Query(context.Background(), instance, plugin.ReadQueryRequest{Query: "x"})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("expected the call to fail near the host request timeout, took %s", elapsed)
	}
}

func TestReadQuerierProcessRemoteErrorIsNeverASuccessfulResult(t *testing.T) {
	host, _ := newReadQueryFixtureHost(t)
	if err := host.Start(context.Background(), "read-fixture"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })

	instance := plugin.Instance{
		ID: "instance-1", ExtensionID: "read-fixture", CapabilityID: plugin.CapabilityReadQuery,
		Config: json.RawMessage(`{"remoteError":true}`),
	}
	_, err := plugin.NewReadQuerier(host).Query(context.Background(), instance, plugin.ReadQueryRequest{Query: "x"})
	if !errors.Is(err, plugin.ErrRemote) {
		t.Fatalf("expected a remote error, got %v", err)
	}
}
