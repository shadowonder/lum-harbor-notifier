package plugin_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/plugintest"
)

func sourceSchema() plugin.PluginConfigSchema {
	return plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{
		{Key: "team", Label: "Team", Type: plugin.ConfigString},
		{Key: "remoteError", Label: "Remote error", Type: plugin.ConfigBoolean},
	}}
}

func newFixtureHost(t *testing.T, mode string, capabilities []plugin.CapabilityID, bindings []plugin.BindingDeclaration) (*plugin.Host, plugin.Extension) {
	t.Helper()
	root := t.TempDir()
	plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
		ID: "fixture", Name: "Fixture", Mode: mode, Capabilities: capabilities,
		Bindings: bindings, Schema: sourceSchema(),
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
	extension, _ := registry.Find("fixture")
	return host, extension
}

func TestHandshakeSuccessInvokeRemoteErrorAndSnapshot(t *testing.T) {
	host, _ := newFixtureHost(t, "normal", []plugin.CapabilityID{plugin.CapabilitySourceFetch}, nil)
	if err := host.Start(context.Background(), "fixture"); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
	instance := plugin.Instance{ID: "team-a", ExtensionID: "fixture", CapabilityID: plugin.CapabilitySourceFetch, Config: json.RawMessage(`{"team":"a"}`)}
	result, err := plugin.NewSourceFetcher(host).Fetch(context.Background(), instance, plugin.SourceFetchRequest{})
	if err != nil || len(result.Records) != 1 || result.Records[0].ExternalID != "a-item" {
		t.Fatalf("Fetch: result=%+v err=%v", result, err)
	}
	errorInstance := instance
	errorInstance.Config = json.RawMessage(`{"remoteError":true}`)
	_, err = plugin.NewSourceFetcher(host).Fetch(context.Background(), errorInstance, plugin.SourceFetchRequest{})
	if !errors.Is(err, plugin.ErrRemote) {
		t.Fatalf("expected remote error, got %v", err)
	}
	if _, err := host.Invoke(context.Background(), plugin.Invocation{ExtensionID: "fixture", CapabilityID: "unknown", Input: json.RawMessage(`{}`)}); !errors.Is(err, plugin.ErrCapabilityUnsupported) {
		t.Fatalf("expected unsupported capability, got %v", err)
	}
	snapshot := host.Snapshot()
	if len(snapshot.Extensions) != 1 || !snapshot.Extensions[0].Running || snapshot.Extensions[0].ID != "fixture" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	encoded, _ := json.Marshal(snapshot)
	for _, forbidden := range []string{"secret", "pending", "ExecutablePath", "stdin"} {
		if stringContains(string(encoded), forbidden) {
			t.Fatalf("snapshot leaked %q: %s", forbidden, encoded)
		}
	}
}

func TestHandshakeMismatchesFailStart(t *testing.T) {
	for _, mode := range []string{"handshake_id", "handshake_version", "handshake_protocol", "handshake_capabilities"} {
		t.Run(mode, func(t *testing.T) {
			host, _ := newFixtureHost(t, mode, []plugin.CapabilityID{plugin.CapabilitySourceFetch}, nil)
			if err := host.Start(context.Background(), "fixture"); !errors.Is(err, plugin.ErrHandshake) {
				t.Fatalf("expected handshake error, got %v", err)
			}
		})
	}
}

func TestStartFailsWhenPluginOwnedDirectoriesCannotBeProvisioned(t *testing.T) {
	for _, blockedDirectory := range []string{"plugin-data", "plugin-cache"} {
		t.Run(blockedDirectory, func(t *testing.T) {
			appRoot := t.TempDir()
			pluginRoot := filepath.Join(appRoot, "plugins")
			plugintest.WriteExtension(t, pluginRoot, plugintest.ExtensionSpec{
				ID: "fixture", Name: "Fixture", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
				Schema: sourceSchema(),
			})
			if err := os.WriteFile(filepath.Join(appRoot, blockedDirectory), []byte("not a directory"), 0o600); err != nil {
				t.Fatal(err)
			}
			registry, failures, err := plugin.Discover(pluginRoot)
			if err != nil || len(failures) != 0 {
				t.Fatalf("Discover: err=%v failures=%v", err, failures)
			}
			host, err := plugin.NewHost(registry, plugin.HostOptions{
				StartupTimeout: time.Second, RequestTimeout: time.Second, ShutdownTimeout: time.Second,
				Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := host.Start(context.Background(), "fixture"); !errors.Is(err, plugin.ErrProcessStart) {
				t.Fatalf("expected process-start failure for blocked %s, got %v", blockedDirectory, err)
			}
			if snapshot := host.Snapshot().Extensions[0]; snapshot.Running || snapshot.Status != "DISCOVERED" {
				t.Fatalf("failed provisioning must not start the process: %+v", snapshot)
			}
		})
	}
}

func TestStartCreatesPluginOwnedDirectoriesWithPrivatePermissions(t *testing.T) {
	appRoot := t.TempDir()
	pluginRoot := filepath.Join(appRoot, "plugins")
	plugintest.WriteExtension(t, pluginRoot, plugintest.ExtensionSpec{
		ID: "fixture", Name: "Fixture", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
		Schema: sourceSchema(),
	})
	registry, failures, err := plugin.Discover(pluginRoot)
	if err != nil || len(failures) != 0 {
		t.Fatalf("Discover: err=%v failures=%v", err, failures)
	}
	host, err := plugin.NewHost(registry, plugin.HostOptions{
		StartupTimeout: time.Second, RequestTimeout: time.Second, ShutdownTimeout: time.Second,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(context.Background(), "fixture"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
	for _, owned := range []string{"plugin-data", "plugin-cache"} {
		path := filepath.Join(appRoot, owned, "fixture")
		info, err := os.Lstat(path)
		if err != nil {
			t.Fatalf("inspect %s: %v", path, err)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
			t.Fatalf("unexpected %s ownership mode: %v", owned, info.Mode())
		}
	}
}

func TestStartRejectsSymlinkPluginOwnedDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not reliably available to unprivileged Windows tests")
	}
	appRoot := t.TempDir()
	pluginRoot := filepath.Join(appRoot, "plugins")
	plugintest.WriteExtension(t, pluginRoot, plugintest.ExtensionSpec{
		ID: "fixture", Name: "Fixture", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
		Schema: sourceSchema(),
	})
	dataRoot := filepath.Join(appRoot, "plugin-data")
	if err := os.MkdirAll(dataRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(dataRoot, "fixture")); err != nil {
		t.Fatal(err)
	}
	registry, failures, err := plugin.Discover(pluginRoot)
	if err != nil || len(failures) != 0 {
		t.Fatalf("Discover: err=%v failures=%v", err, failures)
	}
	host, err := plugin.NewHost(registry, plugin.HostOptions{
		StartupTimeout: time.Second, RequestTimeout: time.Second, ShutdownTimeout: time.Second,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(context.Background(), "fixture"); !errors.Is(err, plugin.ErrProcessStart) {
		t.Fatalf("expected process-start failure for symlink data directory, got %v", err)
	}
}

func TestConcurrentOutOfOrderCorrelationAndMultiInstanceIsolation(t *testing.T) {
	host, _ := newFixtureHost(t, "normal", []plugin.CapabilityID{plugin.CapabilitySourceFetch}, nil)
	if err := host.Start(context.Background(), "fixture"); err != nil {
		t.Fatal(err)
	}
	defer host.Shutdown(context.Background())
	const count = 30
	var wg sync.WaitGroup
	errorsCh := make(chan error, count)
	for index := 0; index < count; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			team := fmt.Sprintf("team-%d", index)
			instance := plugin.Instance{ID: team, ExtensionID: "fixture", CapabilityID: plugin.CapabilitySourceFetch, Config: json.RawMessage(fmt.Sprintf(`{"team":%q}`, team))}
			input := json.RawMessage(fmt.Sprintf(`{"delayMillis":%d}`, (count-index)%7*4))
			output, err := host.Invoke(context.Background(), plugin.Invocation{ExtensionID: "fixture", CapabilityID: plugin.CapabilitySourceFetch, Instance: &instance, Input: input})
			if err != nil {
				errorsCh <- err
				return
			}
			var result plugin.SourceFetchResult
			if err := json.Unmarshal(output, &result); err != nil || result.Records[0].ExternalID != team+"-item" {
				errorsCh <- fmt.Errorf("team %s got %s: %v", team, output, err)
			}
		}(index)
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Error(err)
	}
}

func TestCancellationAndLateResponseDoNotCorruptNextCall(t *testing.T) {
	host, _ := newFixtureHost(t, "normal", []plugin.CapabilityID{plugin.CapabilitySourceFetch}, nil)
	if err := host.Start(context.Background(), "fixture"); err != nil {
		t.Fatal(err)
	}
	defer host.Shutdown(context.Background())
	instance := plugin.Instance{ID: "slow", ExtensionID: "fixture", CapabilityID: plugin.CapabilitySourceFetch, Config: json.RawMessage(`{"team":"slow"}`)}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	_, err := host.Invoke(ctx, plugin.Invocation{ExtensionID: "fixture", CapabilityID: plugin.CapabilitySourceFetch, Instance: &instance, Input: json.RawMessage(`{"delayMillis":80}`)})
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, plugin.ErrRPCCancelled) {
		t.Fatalf("expected contextual timeout, got %v", err)
	}
	fast := instance
	fast.ID = "fast"
	fast.Config = json.RawMessage(`{"team":"fast"}`)
	result, err := plugin.NewSourceFetcher(host).Fetch(context.Background(), fast, plugin.SourceFetchRequest{})
	if err != nil || result.Records[0].ExternalID != "fast-item" {
		t.Fatalf("fast call corrupted: %+v %v", result, err)
	}
	time.Sleep(100 * time.Millisecond)
	if !host.Snapshot().Extensions[0].Running {
		t.Fatalf("late response made process unhealthy: %+v", host.Snapshot())
	}
}

func TestProtocolCorruptionAndUnexpectedExitFailRuntime(t *testing.T) {
	for _, mode := range []string{"malformed", "unknown_id", "both", "duplicate", "exit"} {
		t.Run(mode, func(t *testing.T) {
			host, _ := newFixtureHost(t, mode, []plugin.CapabilityID{plugin.CapabilitySourceFetch}, nil)
			if err := host.Start(context.Background(), "fixture"); err != nil {
				t.Fatal(err)
			}
			instance := plugin.Instance{ID: "one", ExtensionID: "fixture", CapabilityID: plugin.CapabilitySourceFetch, Config: json.RawMessage(`{"team":"one"}`)}
			_, callErr := plugin.NewSourceFetcher(host).Fetch(context.Background(), instance, plugin.SourceFetchRequest{})
			if mode != "duplicate" && callErr == nil {
				t.Fatalf("mode %s did not fail its pending request", mode)
			}
			deadline := time.Now().Add(time.Second)
			for host.Snapshot().Extensions[0].Status == "RUNNING" && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if host.Snapshot().Extensions[0].Status == "RUNNING" {
				t.Fatalf("mode %s did not make runtime unhealthy", mode)
			}
			_ = host.Shutdown(context.Background())
		})
	}
}

func TestGracefulAndForcedShutdownAreBounded(t *testing.T) {
	for _, mode := range []string{"normal", "hang_shutdown"} {
		t.Run(mode, func(t *testing.T) {
			host, _ := newFixtureHost(t, mode, []plugin.CapabilityID{plugin.CapabilitySourceFetch}, nil)
			if err := host.Start(context.Background(), "fixture"); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			if err := host.Shutdown(context.Background()); err != nil {
				t.Fatalf("Shutdown: %v", err)
			}
			if time.Since(started) > time.Second {
				t.Fatalf("shutdown was not bounded: %v", time.Since(started))
			}
			if status := host.Snapshot().Extensions[0].Status; status != "STOPPED" {
				t.Fatalf("expected stopped runtime, got %s", status)
			}
			if _, err := host.Invoke(context.Background(), plugin.Invocation{ExtensionID: "fixture", CapabilityID: plugin.CapabilitySourceFetch, Input: json.RawMessage(`{}`)}); !errors.Is(err, plugin.ErrHostShuttingDown) {
				t.Fatalf("expected shutdown error, got %v", err)
			}
		})
	}
}

func TestShutdownReleasesPendingCall(t *testing.T) {
	host, _ := newFixtureHost(t, "normal", []plugin.CapabilityID{plugin.CapabilitySourceFetch}, nil)
	if err := host.Start(context.Background(), "fixture"); err != nil {
		t.Fatal(err)
	}
	instance := plugin.Instance{ID: "pending", ExtensionID: "fixture", CapabilityID: plugin.CapabilitySourceFetch, Config: json.RawMessage(`{"team":"pending"}`)}
	result := make(chan error, 1)
	go func() {
		_, err := host.Invoke(context.Background(), plugin.Invocation{
			ExtensionID: "fixture", CapabilityID: plugin.CapabilitySourceFetch, Instance: &instance,
			Input: json.RawMessage(`{"delayMillis":50}`),
		})
		result <- err
	}()
	time.Sleep(20 * time.Millisecond)
	if err := host.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, plugin.ErrHostShuttingDown) {
			t.Fatalf("expected pending caller to receive shutdown error, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("pending caller was not released")
	}
	if status := host.Snapshot().Extensions[0].Status; status != "STOPPED" {
		t.Fatalf("late pending response corrupted graceful shutdown: status=%s", status)
	}
}

func stringContains(value, part string) bool {
	return len(part) > 0 && (value == part || len(value) >= len(part) && contains(value, part))
}

func contains(value, part string) bool {
	for index := 0; index+len(part) <= len(value); index++ {
		if value[index:index+len(part)] == part {
			return true
		}
	}
	return false
}
