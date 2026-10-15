//go:build plugincompat

package plugincompat_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"lum-harbor-notifier/internal/appdir"
	"lum-harbor-notifier/internal/applicationruntime"
	"lum-harbor-notifier/internal/background"
	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/database"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugininstall"
	"lum-harbor-notifier/internal/plugininstance"
	"lum-harbor-notifier/internal/source"
	"lum-harbor-notifier/internal/source/pluginadapter"
	"lum-harbor-notifier/internal/testsupport"
)

const (
	extensionID                = "lh-plugin-base"
	artifactEnvironment        = "LUM_HARBOR_PLUGIN_COMPAT_ARTIFACT"
	windowsArtifactEnvironment = "LUM_HARBOR_PLUGIN_COMPAT_WINDOWS_ARTIFACT"
)

// TestActualHostValidatesStandaloneWindowsArtifactWithoutExecuting proves
// that a real Windows-built artifact (plugin.json declaring
// "executable": "lh-plugin-base.exe", plus the actual PE binary) installs
// and discovers correctly on any host, including this one, since Install
// and Discover only validate manifest/filesystem structure and never
// execute the plugin binary. Actually launching the process (handshake,
// capability invocation) requires a genuine Windows host and is out of
// scope here — see the Slice 3 report's Native Windows Plugin Evidence
// section for that remaining gap.
func TestActualHostValidatesStandaloneWindowsArtifactWithoutExecuting(t *testing.T) {
	configured := strings.TrimSpace(os.Getenv(windowsArtifactEnvironment))
	if configured == "" {
		t.Skipf("Windows plugin artifact not supplied; set %s to an absolute artifact directory", windowsArtifactEnvironment)
	}
	if !filepath.IsAbs(configured) {
		t.Fatalf("%s must be an absolute artifact directory, got %q", windowsArtifactEnvironment, configured)
	}
	artifactDirectory, err := filepath.EvalSymlinks(filepath.Clean(configured))
	if err != nil {
		t.Fatalf("resolve Windows plugin artifact directory %q: %v", configured, err)
	}

	pluginRoot := t.TempDir()
	installer, err := plugininstall.New(pluginRoot)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := installer.Install(artifactDirectory)
	if err != nil {
		t.Fatalf("install supplied Windows artifact: %v", err)
	}
	if installed.ExtensionID != extensionID || !installed.RestartRequired {
		t.Fatalf("unexpected Windows installation result: %+v", installed)
	}

	registry, failures, err := plugin.Discover(pluginRoot)
	if err != nil {
		t.Fatalf("discover staged Windows artifact: %v", err)
	}
	if len(failures) != 0 {
		t.Fatalf("Windows discovery failures: %+v", failures)
	}
	extensions := registry.List()
	if len(extensions) != 1 {
		t.Fatalf("discovered Windows extensions: got %d want 1", len(extensions))
	}
	extension := extensions[0]
	if extension.Manifest.Executable != "lh-plugin-base.exe" {
		t.Fatalf("expected the Windows artifact manifest to declare the .exe entry, got %q", extension.Manifest.Executable)
	}
	if filepath.Base(extension.ExecutablePath) != "lh-plugin-base.exe" {
		t.Fatalf("resolved Windows executable path: got %q", extension.ExecutablePath)
	}
	if _, err := registry.ResolveCapability(extensionID, plugin.CapabilitySourceFetch); err != nil {
		t.Fatalf("resolve source.fetch on Windows artifact: %v", err)
	}
}

func TestActualHostConsumesStandaloneArtifact(t *testing.T) {
	artifactDirectory := compatibilityArtifact(t)
	pluginRoot := t.TempDir()
	installer, err := plugininstall.New(pluginRoot)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := installer.Install(artifactDirectory)
	if err != nil {
		t.Fatalf("install supplied artifact: %v", err)
	}
	stagedDirectory := filepath.Join(pluginRoot, extensionID)
	if installed.ExtensionID != extensionID || installed.Destination != stagedDirectory || !installed.RestartRequired {
		t.Fatalf("unexpected installation result: %+v", installed)
	}

	registry, failures, err := plugin.Discover(pluginRoot)
	if err != nil {
		t.Fatalf("discover staged artifact: %v", err)
	}
	if len(failures) != 0 {
		t.Fatalf("discovery failures: %+v", failures)
	}
	extensions := registry.List()
	if len(extensions) != 1 {
		t.Fatalf("discovered extensions: got %d want 1", len(extensions))
	}
	extension := extensions[0]
	if extension.Manifest.ID != extensionID || extension.Manifest.ManifestVersion != plugin.SupportedManifestVersion || extension.Manifest.ProtocolVersion != plugin.SupportedProtocolVersion {
		t.Fatalf("discovered identity/version mismatch: %+v", extension.Manifest)
	}
	if len(extension.Manifest.Bindings) != 1 || extension.Manifest.Bindings[0].Type != plugin.BindingSchedule || extension.Manifest.Bindings[0].Capability != plugin.CapabilitySourceFetch || extension.Manifest.Bindings[0].DefaultIntervalSeconds != 300 {
		t.Fatalf("discovered production schedule mismatch: %+v", extension.Manifest.Bindings)
	}
	if _, err := registry.ResolveCapability(extensionID, plugin.CapabilitySourceFetch); err != nil {
		t.Fatalf("resolve source.fetch: %v", err)
	}
	instances, err := plugininstance.Resolve([]config.PluginInstanceConfig{{
		ID: "compatibility-instance", ExtensionID: extensionID,
		CapabilityID: string(plugin.CapabilitySourceFetch), Config: json.RawMessage(`{}`),
	}}, registry)
	if err != nil {
		t.Fatalf("resolve configured real-plugin instance: %v", err)
	}
	if len(instances) != 1 || instances[0].ID != "compatibility-instance" || instances[0].ExtensionID != extensionID {
		t.Fatalf("resolved configured instance: %+v", instances)
	}
	// The executable filename is manifest truth (extension.Manifest.Executable,
	// e.g. "lh-plugin-base" on Unix or "lh-plugin-base.exe" on Windows), not
	// necessarily the bare extension ID.
	wantExecutable, err := filepath.EvalSymlinks(filepath.Join(stagedDirectory, extension.Manifest.Executable))
	if err != nil {
		t.Fatal(err)
	}
	if extension.ExecutablePath != wantExecutable {
		t.Fatalf("executable path: got %q want %q", extension.ExecutablePath, wantExecutable)
	}

	var logs bytes.Buffer
	host, err := plugin.NewHost(registry, plugin.HostOptions{
		StartupTimeout:  3 * time.Second,
		RequestTimeout:  2 * time.Second,
		ShutdownTimeout: 2 * time.Second,
		Logger:          slog.New(slog.NewTextHandler(&logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	database := testsupport.OpenTestDatabase(t)
	sourceService := source.NewService(database.DB)
	sourceAdapter, err := pluginadapter.New(sourceService)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := background.NewScheduler()
	definitions, err := applicationruntime.RegisterSourceWorkloads(
		scheduler, registry, instances, plugin.NewSourceFetcher(host), sourceAdapter, sourceAdapter, 2*time.Second,
	)
	if err != nil {
		t.Fatalf("register real artifact production workload: %v", err)
	}
	if len(definitions) != 1 || definitions[0].Interval != 300*time.Second {
		t.Fatalf("real artifact production definitions: %+v", definitions)
	}
	applicationRuntime, err := applicationruntime.New(scheduler, host, instances, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = applicationRuntime.Shutdown()
		_ = applicationRuntime.Wait()
	})

	startContext, cancelStart := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancelStart()
	if err := applicationRuntime.Start(startContext); err != nil {
		t.Fatalf("start selected real artifact through application runtime: %v", err)
	}
	assertRuntimeSnapshot(t, host.Snapshot(), "RUNNING", true)
	if err := scheduler.Trigger(definitions[0].ID); err != nil {
		t.Fatal(err)
	}
	firstState := waitForSyncState(t, sourceService, 1)
	if firstState.Cursor == nil || *firstState.Cursor != "v1" || firstState.CheckpointVersion != 1 {
		t.Fatalf("first production cursor: %+v", firstState)
	}
	record, err := sourceService.GetSourceRecordByIdentity(context.Background(), extensionID, "compatibility-instance", "lm.base.item", "base-item-1")
	if err != nil {
		t.Fatal(err)
	}
	revisions, err := sourceService.ListRevisionsForRecord(context.Background(), record.ID)
	if err != nil || len(revisions) != 1 {
		t.Fatalf("first production source revision: count=%d err=%v", len(revisions), err)
	}
	if err := scheduler.Trigger(definitions[0].ID); err != nil {
		t.Fatal(err)
	}
	secondState := waitForSyncState(t, sourceService, 2)
	if secondState.Cursor == nil || *secondState.Cursor != "v1" {
		t.Fatalf("second production cursor: %+v", secondState)
	}
	revisions, err = sourceService.ListRevisionsForRecord(context.Background(), record.ID)
	if err != nil || len(revisions) != 1 {
		t.Fatalf("cursor continuation created duplicate revision: count=%d err=%v", len(revisions), err)
	}

	instance := instances[0]
	fetcher := plugin.NewSourceFetcher(host)
	first := fetch(t, fetcher, instance, plugin.SourceFetchRequest{})
	assertInitialResult(t, first)

	cursor := "v1"
	second := fetch(t, fetcher, instance, plugin.SourceFetchRequest{Cursor: &cursor})
	assertCursorResult(t, second)

	assertConcurrentCorrelation(t, fetcher, instance, cursor)

	// M15 Slice 2: the same real extension process also answers read.query,
	// through the exact same unmodified plugin.Host started above for
	// source.fetch — proving the Host-owned typed contract is real against a
	// genuine subprocess, not just the internal/plugin test-fixture harness.
	readInstance := plugin.Instance{ID: "compatibility-read-instance", ExtensionID: extensionID, CapabilityID: plugin.CapabilityReadQuery, Config: json.RawMessage(`{}`)}
	readResult, err := plugin.NewReadQuerier(host).Query(context.Background(), readInstance, plugin.ReadQueryRequest{Query: "feature status"})
	if err != nil {
		t.Fatalf("real read.query invocation: %v", err)
	}
	if !strings.Contains(readResult.Summary, "feature status") || len(readResult.Facts) != 2 {
		t.Fatalf("real read.query result: %+v", readResult)
	}

	if err := applicationRuntime.Shutdown(); err != nil {
		t.Fatalf("shutdown application runtime: %v", err)
	}
	if err := applicationRuntime.Wait(); err != nil {
		t.Fatalf("wait for application runtime shutdown: %v", err)
	}
	assertRuntimeSnapshot(t, host.Snapshot(), "STOPPED", false)
	logOutput := logs.String()
	if !strings.Contains(logOutput, "lh-plugin-base: started") || !strings.Contains(logOutput, "lh-plugin-base: shutdown accepted") {
		t.Fatalf("Host did not capture expected plugin stderr diagnostics: %s", logOutput)
	}
}

func TestActualProductionWorkloadRecoversAcrossApplicationLifetimes(t *testing.T) {
	artifactDirectory := compatibilityArtifact(t)
	pluginRoot := t.TempDir()
	installer, err := plugininstall.New(pluginRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := installer.Install(artifactDirectory); err != nil {
		t.Fatalf("install supplied artifact: %v", err)
	}
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))
	wantConfig := config.DefaultConfig()
	wantConfig.Plugins.Directory = pluginRoot
	wantConfig.Plugins.StartupTimeoutSeconds = 3
	wantConfig.Plugins.RequestTimeoutSeconds = 3
	wantConfig.Background.ShutdownTimeoutSeconds = 3
	wantConfig.Plugins.Instances = []config.PluginInstanceConfig{{
		ID: "compatibility-instance", ExtensionID: extensionID,
		CapabilityID: string(plugin.CapabilitySourceFetch), Config: json.RawMessage(`{}`),
	}}
	writeCompatibilityConfig(t, wantConfig)

	first := startCompatibilityLifetime(t)
	if !reflect.DeepEqual(first.config.Plugins.Instances, wantConfig.Plugins.Instances) {
		t.Fatalf("first selected instance: %+v", first.config.Plugins.Instances)
	}
	if err := first.scheduler.Trigger(first.definitions[0].ID); err != nil {
		t.Fatal(err)
	}
	firstState := waitForSyncState(t, first.source, 1)
	if firstState.Cursor == nil || *firstState.Cursor != "v1" {
		t.Fatalf("first lifetime cursor: %+v", firstState)
	}
	firstRecord, err := first.source.GetSourceRecordByIdentity(context.Background(), extensionID, "compatibility-instance", "lm.base.item", "base-item-1")
	if err != nil {
		t.Fatal(err)
	}
	firstRevisions, err := first.source.ListRevisionsForRecord(context.Background(), firstRecord.ID)
	if err != nil || len(firstRevisions) != 1 {
		t.Fatalf("first lifetime revisions: count=%d err=%v", len(firstRevisions), err)
	}
	assertCompatibilityTableCount(t, first.database.DB, "source_records", 1)
	assertCompatibilityTableCount(t, first.database.DB, "source_revisions", 1)
	firstJobID := first.definitions[0].ID
	firstHost := first.host
	firstRegistry := first.registry
	firstScheduler := first.scheduler
	first.close(t)
	assertRuntimeSnapshot(t, firstHost.Snapshot(), "STOPPED", false)

	second := startCompatibilityLifetime(t)
	if firstHost == second.host || firstRegistry == second.registry || firstScheduler == second.scheduler {
		t.Fatal("second lifetime reused process-local runtime objects")
	}
	if !reflect.DeepEqual(second.config.Plugins.Instances, wantConfig.Plugins.Instances) || !reflect.DeepEqual(second.instances, first.instances) {
		t.Fatalf("reloaded selection changed: config=%+v instances=%+v", second.config.Plugins.Instances, second.instances)
	}
	if second.definitions[0].ID != firstJobID {
		t.Fatalf("reconstructed Job ID: got %q want %q", second.definitions[0].ID, firstJobID)
	}
	assertRuntimeSnapshot(t, second.host.Snapshot(), "RUNNING", true)
	if err := second.scheduler.Trigger(second.definitions[0].ID); err != nil {
		t.Fatal(err)
	}
	secondState := waitForSyncState(t, second.source, 2)
	if secondState.Cursor == nil || *secondState.Cursor != "v1" || secondState.CheckpointVersion != 2 {
		t.Fatalf("restarted durable checkpoint: %+v", secondState)
	}
	secondRecord, err := second.source.GetSourceRecordByIdentity(context.Background(), extensionID, "compatibility-instance", "lm.base.item", "base-item-1")
	if err != nil {
		t.Fatal(err)
	}
	secondRevisions, err := second.source.ListRevisionsForRecord(context.Background(), secondRecord.ID)
	if err != nil {
		t.Fatal(err)
	}
	if secondRecord.ID != firstRecord.ID || secondRecord.CurrentRevisionID != firstRecord.CurrentRevisionID || len(secondRevisions) != 1 {
		t.Fatalf("restart duplicated or replaced durable Source state: first=%+v second=%+v revisions=%d", firstRecord, secondRecord, len(secondRevisions))
	}
	assertCompatibilityTableCount(t, second.database.DB, "source_records", 1)
	assertCompatibilityTableCount(t, second.database.DB, "source_revisions", 1)
	second.close(t)
}

func assertCompatibilityTableCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s count: got %d want %d", table, got, want)
	}
}

type compatibilityLifetime struct {
	config      config.Config
	registry    *plugin.Registry
	instances   []plugin.Instance
	host        *plugin.Host
	scheduler   *background.Scheduler
	source      *source.Service
	database    *database.Database
	runtime     *applicationruntime.Runtime
	definitions []background.JobDefinition
	closed      bool
}

func startCompatibilityLifetime(t *testing.T) *compatibilityLifetime {
	t.Helper()
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("reload runtime config: %v", err)
	}
	registry, failures, err := plugin.Discover(cfg.Plugins.Directory)
	if err != nil || len(failures) != 0 {
		t.Fatalf("rediscover installed artifact: err=%v failures=%v", err, failures)
	}
	instances, err := plugininstance.Resolve(cfg.Plugins.Instances, registry)
	if err != nil {
		t.Fatalf("resolve reloaded selected instance: %v", err)
	}
	db, err := database.Open(context.Background())
	if err != nil {
		t.Fatalf("reopen application database: %v", err)
	}
	host, err := plugin.NewHost(registry, plugin.HostOptions{
		StartupTimeout:  time.Duration(cfg.Plugins.StartupTimeoutSeconds) * time.Second,
		RequestTimeout:  time.Duration(cfg.Plugins.RequestTimeoutSeconds) * time.Second,
		ShutdownTimeout: time.Duration(cfg.Background.ShutdownTimeoutSeconds) * time.Second,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	sourceService := source.NewService(db.DB)
	adapter, err := pluginadapter.New(sourceService)
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	scheduler := background.NewScheduler()
	definitions, err := applicationruntime.RegisterSourceWorkloads(
		scheduler, registry, instances, plugin.NewSourceFetcher(host), adapter, adapter,
		time.Duration(cfg.Plugins.RequestTimeoutSeconds)*time.Second,
	)
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	runtime, err := applicationruntime.New(scheduler, host, instances, time.Duration(cfg.Background.ShutdownTimeoutSeconds)*time.Second)
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	lifetime := &compatibilityLifetime{
		config: cfg, registry: registry, instances: instances, host: host, scheduler: scheduler,
		source: sourceService, database: db, runtime: runtime, definitions: definitions,
	}
	t.Cleanup(func() { lifetime.closeBestEffort() })
	if err := runtime.Start(context.Background()); err != nil {
		lifetime.closeBestEffort()
		t.Fatalf("start reconstructed real-plugin runtime: %v", err)
	}
	if len(definitions) != 1 {
		lifetime.closeBestEffort()
		t.Fatalf("reconstructed real-plugin definitions: %+v", definitions)
	}
	return lifetime
}

func (l *compatibilityLifetime) close(t *testing.T) {
	t.Helper()
	if l.closed {
		return
	}
	if err := l.runtime.Shutdown(); err != nil {
		t.Fatalf("shutdown real-plugin lifetime: %v", err)
	}
	if err := l.runtime.Wait(); err != nil {
		t.Fatalf("wait for real-plugin lifetime: %v", err)
	}
	if err := l.database.Close(); err != nil {
		t.Fatalf("close real-plugin database: %v", err)
	}
	l.closed = true
}

func (l *compatibilityLifetime) closeBestEffort() {
	if l == nil || l.closed {
		return
	}
	_ = l.runtime.Shutdown()
	_ = l.runtime.Wait()
	_ = l.database.Close()
	l.closed = true
}

func writeCompatibilityConfig(t *testing.T, cfg config.Config) {
	t.Helper()
	directory, err := appdir.ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "config.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func waitForSyncState(t *testing.T, service *source.Service, version int64) *source.SyncState {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		state, err := service.GetSyncState(context.Background(), extensionID, "compatibility-instance", string(plugin.CapabilitySourceFetch))
		if err == nil && state.CheckpointVersion >= version {
			return state
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("production source checkpoint %d was not persisted", version)
	return nil
}

func assertConcurrentCorrelation(t *testing.T, fetcher *plugin.SourceFetcher, instance plugin.Instance, cursor string) {
	t.Helper()
	start := make(chan struct{})
	errorsCh := make(chan error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		<-start
		result, err := fetcher.Fetch(context.Background(), instance, plugin.SourceFetchRequest{})
		if err != nil {
			errorsCh <- err
			return
		}
		if len(result.Records) != 1 || result.NextCursor == nil || *result.NextCursor != cursor {
			errorsCh <- fmt.Errorf("initial correlated result: %+v", result)
		}
	}()
	go func() {
		defer wait.Done()
		<-start
		result, err := fetcher.Fetch(context.Background(), instance, plugin.SourceFetchRequest{Cursor: &cursor})
		if err != nil {
			errorsCh <- err
			return
		}
		if len(result.Records) != 0 || result.NextCursor == nil || *result.NextCursor != cursor {
			errorsCh <- fmt.Errorf("cursor correlated result: %+v", result)
		}
	}()
	close(start)
	wait.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Error(err)
	}
}

func fetch(t *testing.T, fetcher *plugin.SourceFetcher, instance plugin.Instance, request plugin.SourceFetchRequest) plugin.SourceFetchResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := fetcher.Fetch(ctx, instance, request)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertInitialResult(t *testing.T, result plugin.SourceFetchResult) {
	t.Helper()
	if len(result.Records) != 1 || result.Records[0].ExternalID != "base-item-1" || result.Records[0].SourceKind != "lm.base.item" {
		t.Fatalf("initial source.fetch result: %+v", result)
	}
	wantObservedAt := time.Date(2026, time.September, 17, 0, 0, 0, 0, time.UTC)
	if !result.Records[0].ObservedAt.Equal(wantObservedAt) || !json.Valid(result.Records[0].Payload) {
		t.Fatalf("initial record timestamp/payload: %+v", result.Records[0])
	}
	if result.NextCursor == nil || *result.NextCursor != "v1" {
		t.Fatalf("initial cursor: %+v", result.NextCursor)
	}
}

func assertCursorResult(t *testing.T, result plugin.SourceFetchResult) {
	t.Helper()
	if len(result.Records) != 0 || result.NextCursor == nil || *result.NextCursor != "v1" {
		t.Fatalf("cursor source.fetch result: %+v", result)
	}
}

func assertRuntimeSnapshot(t *testing.T, snapshot plugin.RuntimeSnapshot, status string, running bool) {
	t.Helper()
	if len(snapshot.Extensions) != 1 {
		t.Fatalf("runtime snapshot extensions: %+v", snapshot)
	}
	extension := snapshot.Extensions[0]
	// lh-plugin-base declares both source.fetch (M2-M3) and read.query (M15
	// Slice 2); CapabilityIDs() sorts alphabetically, so read.query precedes
	// source.fetch.
	wantCapabilities := []plugin.CapabilityID{plugin.CapabilityReadQuery, plugin.CapabilitySourceFetch}
	if extension.ID != extensionID || extension.Status != status || extension.Running != running || extension.ProtocolVersion != plugin.SupportedProtocolVersion || !reflect.DeepEqual(extension.Capabilities, wantCapabilities) || extension.LastError != "" {
		t.Fatalf("runtime snapshot: %+v", extension)
	}
}

func compatibilityArtifact(t *testing.T) string {
	t.Helper()
	configured := strings.TrimSpace(os.Getenv(artifactEnvironment))
	if configured == "" {
		t.Skipf("plugin artifact not supplied; set %s to an absolute artifact directory", artifactEnvironment)
	}
	if !filepath.IsAbs(configured) {
		t.Fatalf("%s must be an absolute artifact directory, got %q", artifactEnvironment, configured)
	}
	artifactDirectory, err := filepath.EvalSymlinks(filepath.Clean(configured))
	if err != nil {
		t.Fatalf("resolve plugin artifact directory %q: %v", configured, err)
	}
	info, err := os.Stat(artifactDirectory)
	if err != nil {
		t.Fatalf("stat plugin artifact directory %q: %v", artifactDirectory, err)
	}
	if !info.IsDir() {
		t.Fatalf("plugin artifact path %q is not a directory", artifactDirectory)
	}
	manifestPath := filepath.Join(artifactDirectory, plugin.ManifestFileName)
	manifestEntry, err := os.Stat(manifestPath)
	if err != nil {
		t.Fatalf("stat plugin artifact file %q: %v", manifestPath, err)
	}
	if !manifestEntry.Mode().IsRegular() {
		t.Fatalf("plugin artifact file %q is not regular", manifestPath)
	}
	// The artifact's actual executable filename is manifest truth, not a
	// hardcoded assumption: a Windows-built artifact's manifest declares
	// "<name>.exe", not the bare extension ID this harness happens to share
	// with the Unix artifact name. Loading through plugin.LoadExtension
	// reuses the same containment/existence validation production discovery
	// applies, rather than duplicating it here.
	extension, err := plugin.LoadExtension(artifactDirectory)
	if err != nil {
		t.Fatalf("load supplied plugin artifact %q: %v", artifactDirectory, err)
	}
	executableEntry, err := os.Stat(extension.ExecutablePath)
	if err != nil {
		t.Fatalf("stat plugin artifact executable %q: %v", extension.ExecutablePath, err)
	}
	if !executableEntry.Mode().IsRegular() {
		t.Fatalf("plugin artifact executable %q is not regular", extension.ExecutablePath)
	}
	// POSIX executable-bit enforcement is Unix/macOS-only; Windows reports
	// no such bit for any regular file (see internal/plugininstall's
	// requireExecutablePermission), so this harness must not apply a Unix
	// invariant to a Windows-built artifact.
	if runtime.GOOS != "windows" && executableEntry.Mode().Perm()&0o111 == 0 {
		t.Fatalf("plugin artifact executable %q has no executable bit", extension.ExecutablePath)
	}
	return artifactDirectory
}
