package pluginadapter_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"lum-harbor-notifier/internal/background"
	"lum-harbor-notifier/internal/background/pluginjob"
	"lum-harbor-notifier/internal/database"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/plugintest"
	"lum-harbor-notifier/internal/source"
	"lum-harbor-notifier/internal/source/pluginadapter"
	"lum-harbor-notifier/internal/testsupport"
)

func TestPluginFixtureProcess(t *testing.T) {
	if plugintest.RunIfRequested() {
		return
	}
}

func TestDurableCursorSurvivesDatabaseReopenAndFeedsRequest(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	db, err := database.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cursor := "abc"
	service := source.NewService(db.DB)
	checkpoint, err := service.PrepareFetch(context.Background(), "dummy", "team-a", string(plugin.CapabilitySourceFetch))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.IngestFetchResult(context.Background(), source.FetchBatch{
		ExtensionID: "dummy", InstanceID: "team-a", CapabilityID: string(plugin.CapabilitySourceFetch), NextCursor: &cursor,
		Checkpoint: checkpoint,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := database.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	reopenedService := source.NewService(reopened.DB)
	state, err := reopenedService.GetSyncState(context.Background(), "dummy", "team-a", string(plugin.CapabilitySourceFetch))
	if err != nil || state.Cursor == nil || *state.Cursor != cursor || state.CheckpointVersion != 1 {
		t.Fatalf("reopened cursor: state=%+v err=%v", state, err)
	}
	adapter, err := pluginadapter.New(reopenedService)
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := adapter.Prepare(context.Background(), sourceInstance("team-a"))
	fetchCheckpoint, ok := prepared.FetchContext.(source.FetchCheckpoint)
	if err != nil || prepared.Request.Cursor == nil || *prepared.Request.Cursor != cursor || !ok || fetchCheckpoint.Version != 1 {
		t.Fatalf("durable request checkpoint: prepared=%+v err=%v", prepared, err)
	}
}

func TestSchedulerPluginJobSubprocessPersistsAndDeduplicatesSourceFacts(t *testing.T) {
	host, extension := newSourceRuntime(t)
	db := testsupport.OpenTestDatabase(t)
	service := source.NewService(db.DB)
	adapter, err := pluginadapter.New(service)
	if err != nil {
		t.Fatal(err)
	}
	instance := sourceInstance("team-a")
	interval := time.Duration(0)
	registration, err := pluginjob.BuildScheduledSourceFetch(
		extension,
		instance,
		pluginjob.ScheduleOptions{BindingID: "poll", IntervalOverride: &interval, Timeout: time.Second, Enabled: true},
		plugin.NewSourceFetcher(host),
		adapter,
		adapter,
	)
	if err != nil {
		t.Fatal(err)
	}
	scheduler := background.NewScheduler()
	if err := scheduler.Register(registration.Definition, registration.Job); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = scheduler.Shutdown(context.Background()) })

	for _, expectedCursor := range []string{"team-a-cursor-1", "team-a-cursor-2", "team-a-cursor-3"} {
		if err := scheduler.Trigger(registration.Definition.ID); err != nil {
			t.Fatal(err)
		}
		waitForCursor(t, service, "team-a", expectedCursor)
	}

	record, err := service.GetSourceRecordByIdentity(context.Background(), "dummy", "team-a", "dummy.todo", "team-a-item")
	if err != nil {
		t.Fatal(err)
	}
	revisions, err := service.ListRevisionsForRecord(context.Background(), record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(revisions) != 2 {
		t.Fatalf("X, X, Y should create two revisions, got %d", len(revisions))
	}
	if string(revisions[0].Payload) != `{"instanceId":"team-a","team":"team-a","version":"X"}` ||
		string(revisions[1].Payload) != `{"instanceId":"team-a","team":"team-a","version":"Y"}` {
		t.Fatalf("unexpected canonical revisions: %s / %s", revisions[0].Payload, revisions[1].Payload)
	}
	if !record.LastSeenAt.Equal(time.Date(2026, 9, 17, 12, 2, 0, 0, time.UTC)) {
		t.Fatalf("last_seen did not advance: %s", record.LastSeenAt)
	}
	state, err := service.GetSyncState(context.Background(), "dummy", "team-a", string(plugin.CapabilitySourceFetch))
	if err != nil || state.CheckpointVersion != 3 {
		t.Fatalf("normal Scheduler path checkpoint: state=%+v err=%v", state, err)
	}
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSourceFetchJobRejectsResultWhenCheckpointAdvancesDuringRemoteFetch(t *testing.T) {
	host, _ := newSourceRuntime(t)
	db := testsupport.OpenTestDatabase(t)
	service := source.NewService(db.DB)
	adapter, _ := pluginadapter.New(service)
	if _, err := ingestCurrent(service, source.FetchBatch{
		ExtensionID: "dummy", InstanceID: "team-a", CapabilityID: string(plugin.CapabilitySourceFetch), NextCursor: stringPointer("C0"),
	}); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	fetcher := plugin.NewSourceFetcher(&signalingInvoker{inner: host, started: started})
	instance := sourceInstance("team-a")
	instance.Config = json.RawMessage(`{"team":"team-a","delayMillis":80}`)
	job, err := pluginjob.NewSourceFetchJob(fetcher, instance, adapter, adapter)
	if err != nil {
		t.Fatal(err)
	}
	jobDone := make(chan error, 1)
	go func() { jobDone <- job.Run(context.Background()) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("remote source fetch did not start")
	}

	if _, err := ingestCurrent(service, source.FetchBatch{
		ExtensionID: "dummy", InstanceID: "team-a", CapabilityID: string(plugin.CapabilitySourceFetch), NextCursor: stringPointer("newer"),
	}); err != nil {
		t.Fatal(err)
	}
	if err := <-jobDone; !errors.Is(err, source.ErrStaleSourceFetch) {
		t.Fatalf("expected stale SourceFetchJob result, got %v", err)
	}
	state, _ := service.GetSyncState(context.Background(), "dummy", "team-a", string(plugin.CapabilitySourceFetch))
	if state.Cursor == nil || *state.Cursor != "newer" || state.CheckpointVersion != 2 {
		t.Fatalf("stale SourceFetchJob changed checkpoint: %+v", state)
	}
	var records int
	if err := db.DB.QueryRow(`SELECT COUNT(*) FROM source_records`).Scan(&records); err != nil {
		t.Fatal(err)
	}
	if records != 0 {
		t.Fatalf("stale SourceFetchJob persisted %d records", records)
	}
}

func TestAdapterKeepsInstanceIdentitiesAndCursorsIndependent(t *testing.T) {
	db := testsupport.OpenTestDatabase(t)
	service := source.NewService(db.DB)
	adapter, _ := pluginadapter.New(service)
	observedAt := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	for _, instanceID := range []string{"team-a", "team-b"} {
		cursor := instanceID + "-cursor"
		prepared, err := adapter.Prepare(context.Background(), sourceInstance(instanceID))
		if err != nil {
			t.Fatal(err)
		}
		err = adapter.Accept(context.Background(), pluginjob.SourceFetchBatch{
			ExtensionID: "dummy", InstanceID: instanceID, FetchContext: prepared.FetchContext,
			Result: plugin.SourceFetchResult{
				Records:    []plugin.SourceRecord{{ExternalID: "ITEM-1", SourceKind: "dummy.todo", ObservedAt: observedAt, Payload: json.RawMessage(`{"same":true}`)}},
				NextCursor: &cursor,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, instanceID := range []string{"team-a", "team-b"} {
		record, err := service.GetSourceRecordByIdentity(context.Background(), "dummy", instanceID, "dummy.todo", "ITEM-1")
		if err != nil || record.InstanceID != instanceID {
			t.Fatalf("record %s: %+v err=%v", instanceID, record, err)
		}
		prepared, err := adapter.Prepare(context.Background(), sourceInstance(instanceID))
		if err != nil || prepared.Request.Cursor == nil || *prepared.Request.Cursor != instanceID+"-cursor" {
			t.Fatalf("request %s: %+v err=%v", instanceID, prepared.Request, err)
		}
	}
}

func newSourceRuntime(t *testing.T) (*plugin.Host, plugin.Extension) {
	t.Helper()
	root := t.TempDir()
	plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
		ID: "dummy", Name: "Dummy Source",
		Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
		Bindings: []plugin.BindingDeclaration{{
			ID: "poll", Type: plugin.BindingSchedule, Capability: plugin.CapabilitySourceFetch, DefaultIntervalSeconds: 300,
		}},
		Schema: plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{
			{Key: "team", Label: "Team", Type: plugin.ConfigString},
			{Key: "delayMillis", Label: "Delay", Type: plugin.ConfigNumber},
		}},
	})
	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("discover fixture: err=%v failures=%v", err, failures)
	}
	host, err := plugin.NewHost(registry, plugin.HostOptions{
		StartupTimeout: 3 * time.Second, RequestTimeout: time.Second, ShutdownTimeout: 200 * time.Millisecond,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := host.Start(context.Background(), "dummy"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })
	extension, _ := registry.Find("dummy")
	return host, extension
}

func sourceInstance(id string) plugin.Instance {
	return plugin.Instance{
		ID: id, ExtensionID: "dummy", CapabilityID: plugin.CapabilitySourceFetch,
		Config: json.RawMessage(`{"team":"` + id + `"}`),
	}
}

type signalingInvoker struct {
	inner   plugin.Invoker
	started chan struct{}
}

func (i *signalingInvoker) Invoke(ctx context.Context, invocation plugin.Invocation) (json.RawMessage, error) {
	close(i.started)
	return i.inner.Invoke(ctx, invocation)
}

func ingestCurrent(service *source.Service, batch source.FetchBatch) (source.IngestionResult, error) {
	checkpoint, err := service.PrepareFetch(context.Background(), batch.ExtensionID, batch.InstanceID, batch.CapabilityID)
	if err != nil {
		return source.IngestionResult{}, err
	}
	batch.Checkpoint = checkpoint
	return service.IngestFetchResult(context.Background(), batch)
}

func stringPointer(value string) *string { return &value }

func waitForCursor(t *testing.T, service *source.Service, instanceID, expected string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		state, err := service.GetSyncState(context.Background(), "dummy", instanceID, string(plugin.CapabilitySourceFetch))
		if err == nil && state.Cursor != nil && *state.Cursor == expected {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("cursor %q was not persisted", expected)
}
