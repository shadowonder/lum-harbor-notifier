package applicationruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/appdir"
	"lum-harbor-notifier/internal/background"
	"lum-harbor-notifier/internal/background/pluginjob"
	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/database"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/plugintest"
	"lum-harbor-notifier/internal/plugininstance"
	"lum-harbor-notifier/internal/source"
	"lum-harbor-notifier/internal/source/pluginadapter"
	"lum-harbor-notifier/internal/testsupport"
)

type sourceWorkloadInvoker struct{}

func (sourceWorkloadInvoker) Invoke(context.Context, plugin.Invocation) (json.RawMessage, error) {
	return json.RawMessage(`{"records":[]}`), nil
}

type sourceWorkloadSink struct{}

func (sourceWorkloadSink) Accept(context.Context, pluginjob.SourceFetchBatch) error { return nil }

func TestRegisterSourceWorkloadsSelectsOnlySourceInstances(t *testing.T) {
	registry := sourceWorkloadRegistry(t, true)
	requestSource := pluginjob.SourceFetchRequestSourceFunc(func(context.Context, plugin.Instance) (pluginjob.PreparedSourceFetch, error) {
		return pluginjob.PreparedSourceFetch{}, nil
	})
	tests := []struct {
		name      string
		instances []plugin.Instance
		wantIDs   []string
	}{
		{name: "no selected source instance"},
		{name: "selected non-source capability", instances: []plugin.Instance{{
			ID: "provider", ExtensionID: "ai-extension", CapabilityID: plugin.CapabilityAIProvider,
		}}},
		{name: "one selected source instance", instances: []plugin.Instance{
			sourceWorkloadInstance("team-a"),
		}, wantIDs: []string{"source:source-extension:team-a"}},
		{name: "two source instances share extension but not Job identity", instances: []plugin.Instance{
			sourceWorkloadInstance("team-a"), sourceWorkloadInstance("team-b"),
		}, wantIDs: []string{"source:source-extension:team-a", "source:source-extension:team-b"}},
		{name: "installed source extension is unselected"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scheduler := background.NewScheduler()
			definitions, err := RegisterSourceWorkloads(
				scheduler,
				registry,
				test.instances,
				plugin.NewSourceFetcher(sourceWorkloadInvoker{}),
				requestSource,
				sourceWorkloadSink{},
				5*time.Second,
			)
			if err != nil {
				t.Fatal(err)
			}
			if len(definitions) != len(test.wantIDs) || len(scheduler.Snapshots()) != len(test.wantIDs) {
				t.Fatalf("definitions=%d snapshots=%d want=%d", len(definitions), len(scheduler.Snapshots()), len(test.wantIDs))
			}
			for index, definition := range definitions {
				if definition.ID != test.wantIDs[index] || definition.Interval != 300*time.Second || definition.Timeout != 5*time.Second || !definition.Enabled {
					t.Fatalf("definition[%d]=%+v want ID=%q with production schedule", index, definition, test.wantIDs[index])
				}
			}
		})
	}
}

func TestRegisterSourceWorkloadsRejectsMissingScheduleBeforeSchedulerStart(t *testing.T) {
	scheduler := background.NewScheduler()
	registry := sourceWorkloadRegistry(t, false)
	requestSource := pluginjob.SourceFetchRequestSourceFunc(func(context.Context, plugin.Instance) (pluginjob.PreparedSourceFetch, error) {
		return pluginjob.PreparedSourceFetch{}, nil
	})
	_, err := RegisterSourceWorkloads(
		scheduler,
		registry,
		[]plugin.Instance{sourceWorkloadInstance("team-a")},
		plugin.NewSourceFetcher(sourceWorkloadInvoker{}),
		requestSource,
		sourceWorkloadSink{},
		time.Second,
	)
	if err == nil || !strings.Contains(err.Error(), "no source.fetch SCHEDULE binding") {
		t.Fatalf("expected missing schedule startup error, got %v", err)
	}
	if scheduler.State() != background.SchedulerNew || len(scheduler.Snapshots()) != 0 {
		t.Fatalf("invalid workload mutated Scheduler: state=%s jobs=%d", scheduler.State(), len(scheduler.Snapshots()))
	}
}

func TestProductionSourceWorkloadsPersistIndependentInstancesAndCursor(t *testing.T) {
	root := t.TempDir()
	plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
		ID: "source-extension", Name: "Production Source Fixture",
		Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
		Bindings: []plugin.BindingDeclaration{{
			ID: "poll", Type: plugin.BindingSchedule, Capability: plugin.CapabilitySourceFetch, DefaultIntervalSeconds: 300,
		}},
		Schema: plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{{
			Key: "team", Label: "Team", Type: plugin.ConfigString,
		}}},
	})
	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("discover fixture: err=%v failures=%v", err, failures)
	}
	host, err := plugin.NewHost(registry, plugin.HostOptions{
		StartupTimeout: 5 * time.Second, RequestTimeout: 5 * time.Second, ShutdownTimeout: 5 * time.Second,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	instances := []plugin.Instance{sourceWorkloadInstance("team-a"), sourceWorkloadInstance("team-b")}
	database := testsupport.OpenTestDatabase(t)
	sourceService := source.NewService(database.DB)
	adapter, err := pluginadapter.New(sourceService)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := background.NewScheduler()
	definitions, err := RegisterSourceWorkloads(
		scheduler, registry, instances, plugin.NewSourceFetcher(host), adapter, adapter, time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, scheduler, host, instances, 3*time.Second)
	t.Cleanup(func() { _ = runtime.Shutdown() })
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if snapshot := host.Snapshot(); len(snapshot.Extensions) != 1 || !snapshot.Extensions[0].Running {
		t.Fatalf("shared extension process state: %+v", snapshot)
	}
	if len(definitions) != 2 || definitions[0].ID == definitions[1].ID {
		t.Fatalf("per-instance workload definitions: %+v", definitions)
	}

	for _, definition := range definitions {
		if err := scheduler.Trigger(definition.ID); err != nil {
			t.Fatal(err)
		}
	}
	for _, instanceID := range []string{"team-a", "team-b"} {
		state := waitForSourceCheckpoint(t, sourceService, "source-extension", instanceID, 1)
		if state.Cursor == nil || *state.Cursor != instanceID+"-cursor-1" {
			t.Fatalf("first cursor for %s: %+v", instanceID, state)
		}
		record, err := sourceService.GetSourceRecordByIdentity(context.Background(), "source-extension", instanceID, "dummy.todo", instanceID+"-item")
		if err != nil {
			t.Fatal(err)
		}
		revisions, err := sourceService.ListRevisionsForRecord(context.Background(), record.ID)
		if err != nil || len(revisions) != 1 {
			t.Fatalf("first durable revision for %s: count=%d err=%v", instanceID, len(revisions), err)
		}
	}

	if err := scheduler.Trigger(definitions[0].ID); err != nil {
		t.Fatal(err)
	}
	stateA := waitForSourceCheckpoint(t, sourceService, "source-extension", "team-a", 2)
	if stateA.Cursor == nil || *stateA.Cursor != "team-a-cursor-2" {
		t.Fatalf("second durable cursor: %+v", stateA)
	}
	recordA, err := sourceService.GetSourceRecordByIdentity(context.Background(), "source-extension", "team-a", "dummy.todo", "team-a-item")
	if err != nil {
		t.Fatal(err)
	}
	revisionsA, err := sourceService.ListRevisionsForRecord(context.Background(), recordA.ID)
	if err != nil || len(revisionsA) != 1 {
		t.Fatalf("unchanged second fetch created a revision: count=%d err=%v", len(revisionsA), err)
	}
	stateB := waitForSourceCheckpoint(t, sourceService, "source-extension", "team-b", 1)
	if stateB.CheckpointVersion != 1 {
		t.Fatalf("team-b checkpoint changed with team-a: %+v", stateB)
	}

	if err := runtime.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if scheduler.State() != background.SchedulerStopped || host.Snapshot().Extensions[0].Running {
		t.Fatalf("ordered shutdown state: scheduler=%s host=%+v", scheduler.State(), host.Snapshot())
	}
}

func TestProductionSourceWorkloadRecoversAcrossApplicationLifetimes(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(tempHome, ".config"))
	pluginRoot := t.TempDir()
	plugintest.WriteExtension(t, pluginRoot, plugintest.ExtensionSpec{
		ID: "source-extension", Name: "Restart Source Fixture",
		Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
		Bindings: []plugin.BindingDeclaration{{
			ID: "poll", Type: plugin.BindingSchedule, Capability: plugin.CapabilitySourceFetch, DefaultIntervalSeconds: 300,
		}},
		Schema: plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{{
			Key: "team", Label: "Team", Type: plugin.ConfigString,
		}}},
	})
	wantConfig := config.DefaultConfig()
	wantConfig.Plugins.Directory = pluginRoot
	wantConfig.Plugins.StartupTimeoutSeconds = 3
	wantConfig.Plugins.RequestTimeoutSeconds = 3
	wantConfig.Background.ShutdownTimeoutSeconds = 3
	wantConfig.Plugins.Instances = []config.PluginInstanceConfig{{
		ID: "team-a", ExtensionID: "source-extension", CapabilityID: string(plugin.CapabilitySourceFetch),
		Config: json.RawMessage(`{"team":"team-a"}`),
	}}
	writeRecoveryConfig(t, wantConfig)

	first := startRecoveryLifetime(t)
	if !reflect.DeepEqual(first.config.Plugins.Instances, wantConfig.Plugins.Instances) {
		t.Fatalf("first lifetime selected instance: %+v", first.config.Plugins.Instances)
	}
	if err := first.scheduler.Trigger(first.definitions[0].ID); err != nil {
		t.Fatal(err)
	}
	firstState := waitForSourceCheckpoint(t, first.source, "source-extension", "team-a", 1)
	if firstState.Cursor == nil || *firstState.Cursor != "team-a-cursor-1" || firstState.CheckpointVersion != 1 {
		t.Fatalf("first lifetime checkpoint: %+v", firstState)
	}
	firstRecord, err := first.source.GetSourceRecordByIdentity(context.Background(), "source-extension", "team-a", "dummy.todo", "team-a-item")
	if err != nil {
		t.Fatal(err)
	}
	firstRevisions, err := first.source.ListRevisionsForRecord(context.Background(), firstRecord.ID)
	if err != nil || len(firstRevisions) != 1 {
		t.Fatalf("first lifetime revisions: count=%d err=%v", len(firstRevisions), err)
	}
	assertRecoveryTableCount(t, first.database.DB, "source_records", 1)
	assertRecoveryTableCount(t, first.database.DB, "source_revisions", 1)
	firstJobID := first.definitions[0].ID
	firstHost := first.host
	firstRegistry := first.registry
	firstScheduler := first.scheduler
	first.close(t)
	if firstHost.Snapshot().Extensions[0].Running {
		t.Fatal("first lifetime Plugin process remained running after shutdown")
	}

	second := startRecoveryLifetime(t)
	if firstHost == second.host || firstRegistry == second.registry || firstScheduler == second.scheduler {
		t.Fatal("second lifetime reused process-local runtime objects")
	}
	if !reflect.DeepEqual(second.config.Plugins.Instances, wantConfig.Plugins.Instances) || !reflect.DeepEqual(second.instances, first.instances) {
		t.Fatalf("reloaded selection changed: config=%+v instances=%+v", second.config.Plugins.Instances, second.instances)
	}
	if second.definitions[0].ID != firstJobID {
		t.Fatalf("reconstructed Job ID: got %q want %q", second.definitions[0].ID, firstJobID)
	}
	if snapshot := second.host.Snapshot(); len(snapshot.Extensions) != 1 || !snapshot.Extensions[0].Running {
		t.Fatalf("new Plugin process did not reach RUNNING: %+v", snapshot)
	}
	if err := second.scheduler.Trigger(second.definitions[0].ID); err != nil {
		t.Fatal(err)
	}
	secondState := waitForSourceCheckpoint(t, second.source, "source-extension", "team-a", 2)
	if secondState.Cursor == nil || *secondState.Cursor != "team-a-cursor-2" || secondState.CheckpointVersion != 2 {
		t.Fatalf("restarted workload did not resume first cursor: %+v", secondState)
	}
	secondRecord, err := second.source.GetSourceRecordByIdentity(context.Background(), "source-extension", "team-a", "dummy.todo", "team-a-item")
	if err != nil {
		t.Fatal(err)
	}
	secondRevisions, err := second.source.ListRevisionsForRecord(context.Background(), secondRecord.ID)
	if err != nil {
		t.Fatal(err)
	}
	if secondRecord.ID != firstRecord.ID || secondRecord.CurrentRevisionID != firstRecord.CurrentRevisionID || len(secondRevisions) != 1 {
		t.Fatalf("restart changed durable identity/head/history: first=%+v second=%+v revisions=%d", firstRecord, secondRecord, len(secondRevisions))
	}
	assertRecoveryTableCount(t, second.database.DB, "source_records", 1)
	assertRecoveryTableCount(t, second.database.DB, "source_revisions", 1)
	second.close(t)
}

func assertRecoveryTableCount(t *testing.T, db *sql.DB, table string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&got); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if got != want {
		t.Fatalf("%s count: got %d want %d", table, got, want)
	}
}

type recoveryLifetime struct {
	config      config.Config
	registry    *plugin.Registry
	instances   []plugin.Instance
	host        *plugin.Host
	scheduler   *background.Scheduler
	source      *source.Service
	database    *database.Database
	runtime     *Runtime
	definitions []background.JobDefinition
	closed      bool
}

func startRecoveryLifetime(t *testing.T) *recoveryLifetime {
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
		t.Fatalf("open application database: %v", err)
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
	definitions, err := RegisterSourceWorkloads(
		scheduler, registry, instances, plugin.NewSourceFetcher(host), adapter, adapter,
		time.Duration(cfg.Plugins.RequestTimeoutSeconds)*time.Second,
	)
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	runtime, err := New(scheduler, host, instances, time.Duration(cfg.Background.ShutdownTimeoutSeconds)*time.Second)
	if err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	lifetime := &recoveryLifetime{
		config: cfg, registry: registry, instances: instances, host: host, scheduler: scheduler,
		source: sourceService, database: db, runtime: runtime, definitions: definitions,
	}
	t.Cleanup(func() { lifetime.closeBestEffort() })
	if err := runtime.Start(context.Background()); err != nil {
		lifetime.closeBestEffort()
		t.Fatalf("start reconstructed application runtime: %v", err)
	}
	if len(definitions) != 1 {
		lifetime.closeBestEffort()
		t.Fatalf("reconstructed source definitions: %+v", definitions)
	}
	return lifetime
}

func (l *recoveryLifetime) close(t *testing.T) {
	t.Helper()
	if l.closed {
		return
	}
	if err := l.runtime.Shutdown(); err != nil {
		t.Fatalf("shutdown application lifetime: %v", err)
	}
	if err := l.runtime.Wait(); err != nil {
		t.Fatalf("wait for application lifetime: %v", err)
	}
	if err := l.database.Close(); err != nil {
		t.Fatalf("close application database: %v", err)
	}
	l.closed = true
}

func (l *recoveryLifetime) closeBestEffort() {
	if l == nil || l.closed {
		return
	}
	_ = l.runtime.Shutdown()
	_ = l.runtime.Wait()
	_ = l.database.Close()
	l.closed = true
}

func writeRecoveryConfig(t *testing.T, cfg config.Config) {
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

func sourceWorkloadRegistry(t *testing.T, withSchedule bool) *plugin.Registry {
	t.Helper()
	extension := plugin.Extension{Manifest: plugin.Manifest{
		ManifestVersion: plugin.SupportedManifestVersion,
		ID:              "source-extension", Name: "Source Extension", Version: "1.0.0",
		ProtocolVersion: plugin.SupportedProtocolVersion,
		Capabilities:    []plugin.CapabilityDeclaration{{ID: plugin.CapabilitySourceFetch}},
		Configuration: plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{{
			Key: "team", Label: "Team", Type: plugin.ConfigString,
		}}},
	}}
	if withSchedule {
		extension.Manifest.Bindings = []plugin.BindingDeclaration{{
			ID: "poll", Type: plugin.BindingSchedule, Capability: plugin.CapabilitySourceFetch, DefaultIntervalSeconds: 300,
		}}
	}
	aiExtension := plugin.Extension{Manifest: plugin.Manifest{
		ManifestVersion: plugin.SupportedManifestVersion,
		ID:              "ai-extension", Name: "AI Extension", Version: "1.0.0",
		ProtocolVersion: plugin.SupportedProtocolVersion,
		Capabilities:    []plugin.CapabilityDeclaration{{ID: plugin.CapabilityAIProvider}},
		Configuration:   plugin.PluginConfigSchema{Version: 1},
	}}
	registry, err := plugin.NewRegistry([]plugin.Extension{extension, aiExtension})
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func sourceWorkloadInstance(instanceID string) plugin.Instance {
	return plugin.Instance{
		ID: instanceID, ExtensionID: "source-extension", CapabilityID: plugin.CapabilitySourceFetch,
		Config: json.RawMessage(`{"team":"` + instanceID + `"}`),
	}
}

func waitForSourceCheckpoint(t *testing.T, service *source.Service, extensionID, instanceID string, version int64) *source.SyncState {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state, err := service.GetSyncState(context.Background(), extensionID, instanceID, string(plugin.CapabilitySourceFetch))
		if err == nil && state.CheckpointVersion >= version {
			return state
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("checkpoint %d for %s/%s was not persisted", version, extensionID, instanceID)
	return nil
}
