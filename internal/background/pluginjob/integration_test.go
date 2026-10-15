package pluginjob_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"lum-harbor-notifier/internal/background"
	"lum-harbor-notifier/internal/background/pluginjob"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/plugintest"
)

func TestPluginFixtureProcess(t *testing.T) {
	if plugintest.RunIfRequested() {
		return
	}
}

type observedBatch struct {
	at    time.Time
	batch pluginjob.SourceFetchBatch
}

type channelSink struct {
	events chan observedBatch
}

func (s *channelSink) Accept(ctx context.Context, batch pluginjob.SourceFetchBatch) error {
	select {
	case s.events <- observedBatch{at: time.Now(), batch: batch}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
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
			{Key: "remoteError", Label: "Remote error", Type: plugin.ConfigBoolean},
		}},
	})
	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("Discover: err=%v failures=%v", err, failures)
	}
	host, err := plugin.NewHost(registry, plugin.HostOptions{
		StartupTimeout: 5 * time.Second, RequestTimeout: time.Second, ShutdownTimeout: 200 * time.Millisecond,
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

func scheduledSource(
	t *testing.T,
	host *plugin.Host,
	extension plugin.Extension,
	instanceID string,
	config string,
	interval time.Duration,
	timeout time.Duration,
	sink pluginjob.SourceFetchSink,
) pluginjob.ScheduledSourceFetch {
	t.Helper()
	instance := plugin.Instance{
		ID: instanceID, ExtensionID: extension.Manifest.ID,
		CapabilityID: plugin.CapabilitySourceFetch, Config: json.RawMessage(config),
	}
	registration, err := pluginjob.BuildScheduledSourceFetch(
		extension, instance,
		pluginjob.ScheduleOptions{BindingID: "poll", IntervalOverride: &interval, Timeout: timeout, Enabled: true},
		plugin.NewSourceFetcher(host), nil, sink,
	)
	if err != nil {
		t.Fatal(err)
	}
	return registration
}

func TestRecurringSourceFetchThroughRealPluginProcess(t *testing.T) {
	host, extension := newSourceRuntime(t)
	sink := &channelSink{events: make(chan observedBatch, 4)}
	registration := scheduledSource(t, host, extension, "team-a", `{"team":"team-a"}`, 35*time.Millisecond, time.Second, sink)
	scheduler := background.NewScheduler()
	if err := scheduler.Register(registration.Definition, registration.Job); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	first := waitBatch(t, sink.events, "first recurring fetch")
	second := waitBatch(t, sink.events, "second recurring fetch")
	if first.batch.InstanceID != "team-a" || second.batch.Result.Records[0].ExternalID != "team-a-item" {
		t.Fatalf("unexpected recurring batches: first=%+v second=%+v", first, second)
	}
	if second.at.Sub(first.at) < 25*time.Millisecond {
		t.Fatalf("recurring fetch did not preserve fixed delay: %v", second.at.Sub(first.at))
	}
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestManualTriggerRunsEarlyAndRestartsPeriodicDelay(t *testing.T) {
	host, extension := newSourceRuntime(t)
	sink := &channelSink{events: make(chan observedBatch, 4)}
	registration := scheduledSource(t, host, extension, "team-a", `{"team":"team-a"}`, 180*time.Millisecond, time.Second, sink)
	scheduler := background.NewScheduler()
	if err := scheduler.Register(registration.Definition, registration.Job); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Trigger(registration.Definition.ID); err != nil {
		t.Fatal(err)
	}
	first := waitBatch(t, sink.events, "manually triggered fetch")
	select {
	case second := <-sink.events:
		t.Fatalf("periodic fetch arrived before reset delay: %v", second.at.Sub(first.at))
	case <-time.After(100 * time.Millisecond):
	}
	second := waitBatch(t, sink.events, "periodic fetch after trigger")
	if second.at.Sub(first.at) < 150*time.Millisecond {
		t.Fatalf("periodic delay was not restarted: %v", second.at.Sub(first.at))
	}
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestTriggerDuringSourceFetchCoalescesOneFollowUp(t *testing.T) {
	host, extension := newSourceRuntime(t)
	sink := &channelSink{events: make(chan observedBatch, 4)}
	registration := scheduledSource(t, host, extension, "team-a", `{"team":"team-a","delayMillis":80}`, 0, time.Second, sink)
	scheduler := background.NewScheduler()
	if err := scheduler.Register(registration.Definition, registration.Job); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Trigger(registration.Definition.ID); err != nil {
		t.Fatal(err)
	}
	waitForRunning(t, scheduler, registration.Definition.ID)
	for index := 0; index < 20; index++ {
		if err := scheduler.Trigger(registration.Definition.ID); err != nil {
			t.Fatal(err)
		}
	}
	waitBatch(t, sink.events, "blocked source fetch")
	waitBatch(t, sink.events, "single source follow-up")
	select {
	case extra := <-sink.events:
		t.Fatalf("trigger burst produced extra fetch: %+v", extra)
	case <-time.After(120 * time.Millisecond):
	}
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSourceInstancesRunAsIndependentConcurrentJobs(t *testing.T) {
	host, extension := newSourceRuntime(t)
	sink := &channelSink{events: make(chan observedBatch, 4)}
	scheduler := background.NewScheduler()
	var registrations []pluginjob.ScheduledSourceFetch
	for _, team := range []string{"team-a", "team-b"} {
		registration := scheduledSource(t, host, extension, team, `{"team":"`+team+`","delayMillis":100}`, 0, time.Second, sink)
		registrations = append(registrations, registration)
		if err := scheduler.Register(registration.Definition, registration.Job); err != nil {
			t.Fatal(err)
		}
	}
	if registrations[0].Definition.ID == registrations[1].Definition.ID {
		t.Fatal("different instances produced the same Job ID")
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, registration := range registrations {
		if err := scheduler.Trigger(registration.Definition.ID); err != nil {
			t.Fatal(err)
		}
	}
	waitForBothRunning(t, scheduler, registrations[0].Definition.ID, registrations[1].Definition.ID)
	seen := map[string]string{}
	for range registrations {
		event := waitBatch(t, sink.events, "concurrent instance fetch")
		seen[event.batch.InstanceID] = event.batch.Result.Records[0].ExternalID
	}
	if seen["team-a"] != "team-a-item" || seen["team-b"] != "team-b-item" {
		t.Fatalf("instance results crossed: %v", seen)
	}
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestSourceRemoteErrorAndSchedulerTimeoutState(t *testing.T) {
	t.Run("remote error", func(t *testing.T) {
		host, extension := newSourceRuntime(t)
		sink := &channelSink{events: make(chan observedBatch, 1)}
		registration := scheduledSource(t, host, extension, "remote", `{"team":"remote","remoteError":true}`, 0, time.Second, sink)
		scheduler := background.NewScheduler()
		if err := scheduler.Register(registration.Definition, registration.Job); err != nil {
			t.Fatal(err)
		}
		if err := scheduler.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := scheduler.Trigger(registration.Definition.ID); err != nil {
			t.Fatal(err)
		}
		snapshot := waitForFailure(t, scheduler, registration.Definition.ID)
		if !contains(snapshot.LastError, "plugin remote capability error") {
			t.Fatalf("unexpected remote failure: %+v", snapshot)
		}
		if err := scheduler.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("scheduler timeout", func(t *testing.T) {
		host, extension := newSourceRuntime(t)
		sink := &channelSink{events: make(chan observedBatch, 1)}
		registration := scheduledSource(t, host, extension, "slow", `{"team":"slow","delayMillis":80}`, 0, 15*time.Millisecond, sink)
		scheduler := background.NewScheduler()
		if err := scheduler.Register(registration.Definition, registration.Job); err != nil {
			t.Fatal(err)
		}
		if err := scheduler.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		if err := scheduler.Trigger(registration.Definition.ID); err != nil {
			t.Fatal(err)
		}
		snapshot := waitForFailure(t, scheduler, registration.Definition.ID)
		if snapshot.LastError != context.DeadlineExceeded.Error() {
			t.Fatalf("expected scheduler timeout state, got %+v", snapshot)
		}
		time.Sleep(100 * time.Millisecond)
		if !host.Snapshot().Extensions[0].Running {
			t.Fatal("request timeout killed healthy plugin process")
		}
		instance := plugin.Instance{ID: "slow", ExtensionID: "dummy", CapabilityID: plugin.CapabilitySourceFetch, Config: json.RawMessage(`{"team":"slow","delayMillis":80}`)}
		if _, err := plugin.NewSourceFetcher(host).Fetch(context.Background(), instance, plugin.SourceFetchRequest{}); err != nil {
			t.Fatalf("later source invocation failed: %v", err)
		}
		if err := scheduler.Shutdown(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
}

func TestSchedulerShutdownCancelsRunningSourceFetch(t *testing.T) {
	host, extension := newSourceRuntime(t)
	sink := &channelSink{events: make(chan observedBatch, 1)}
	registration := scheduledSource(t, host, extension, "shutdown", `{"team":"shutdown","delayMillis":80}`, 0, time.Second, sink)
	scheduler := background.NewScheduler()
	if err := scheduler.Register(registration.Definition, registration.Job); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Trigger(registration.Definition.ID); err != nil {
		t.Fatal(err)
	}
	waitForRunning(t, scheduler, registration.Definition.ID)
	if err := scheduler.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if scheduler.State() != background.SchedulerStopped {
		t.Fatalf("expected terminal Scheduler, got %s", scheduler.State())
	}
	snapshot, _ := scheduler.Snapshot(registration.Definition.ID)
	if snapshot.Running || snapshot.ConsecutiveFailures != 0 {
		t.Fatalf("shutdown cancellation recorded as failure: %+v", snapshot)
	}
	time.Sleep(100 * time.Millisecond)
	if !host.Snapshot().Extensions[0].Running {
		t.Fatal("Scheduler shutdown also stopped Plugin Host")
	}
}

func waitBatch(t *testing.T, events <-chan observedBatch, description string) observedBatch {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
		return observedBatch{}
	}
}

func waitForRunning(t *testing.T, scheduler *background.Scheduler, id string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := scheduler.Snapshot(id)
		if err == nil && snapshot.Running {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("Job %q did not enter Running", id)
}

func waitForBothRunning(t *testing.T, scheduler *background.Scheduler, first, second string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		firstSnapshot, _ := scheduler.Snapshot(first)
		secondSnapshot, _ := scheduler.Snapshot(second)
		if firstSnapshot.Running && secondSnapshot.Running {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("Jobs were not concurrently running: first=%+v second=%+v", mustSnapshot(scheduler, first), mustSnapshot(scheduler, second))
}

func waitForFailure(t *testing.T, scheduler *background.Scheduler, id string) background.JobSnapshot {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err := scheduler.Snapshot(id)
		if err == nil && snapshot.LastFailureAt != nil {
			return snapshot
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("Job %q did not record failure", id)
	return background.JobSnapshot{}
}

func mustSnapshot(scheduler *background.Scheduler, id string) background.JobSnapshot {
	snapshot, _ := scheduler.Snapshot(id)
	return snapshot
}

func contains(value, part string) bool {
	for index := 0; index+len(part) <= len(value); index++ {
		if value[index:index+len(part)] == part {
			return true
		}
	}
	return false
}
