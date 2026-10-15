package applicationruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"lum-harbor-notifier/internal/background"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/plugintest"
	"lum-harbor-notifier/internal/testsupport"
)

type hostFunc func(context.Context) error

func (f hostFunc) Start(context.Context, string) error { return nil }
func (f hostFunc) Shutdown(ctx context.Context) error  { return f(ctx) }

type recordingHost struct {
	mu            sync.Mutex
	starts        []string
	running       map[string]bool
	failExtension string
	shutdownCalls int
	scheduler     *background.Scheduler
}

func (h *recordingHost) Start(_ context.Context, extensionID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.scheduler != nil && h.scheduler.State() != background.SchedulerNew {
		return errors.New("extension started after scheduler")
	}
	h.starts = append(h.starts, extensionID)
	if extensionID == h.failExtension {
		return errors.New("fixture startup failure")
	}
	if h.running == nil {
		h.running = make(map[string]bool)
	}
	h.running[extensionID] = true
	return nil
}

func (h *recordingHost) Shutdown(context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.shutdownCalls++
	for extensionID := range h.running {
		delete(h.running, extensionID)
	}
	return nil
}

func (h *recordingHost) state() ([]string, int, int) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.starts...), len(h.running), h.shutdownCalls
}

type jobFunc func(context.Context) error

func (f jobFunc) Run(ctx context.Context) error { return f(ctx) }

func TestSelectedInstancesStartOnlyUniqueRequiredExtensions(t *testing.T) {
	tests := []struct {
		name      string
		instances []plugin.Instance
		want      []string
	}{
		{name: "empty selection"},
		{name: "one selected instance", instances: []plugin.Instance{{ID: "one", ExtensionID: "extension-a"}}, want: []string{"extension-a"}},
		{name: "shared extension starts once", instances: []plugin.Instance{
			{ID: "one", ExtensionID: "extension-a"},
			{ID: "two", ExtensionID: "extension-a"},
		}, want: []string{"extension-a"}},
		{name: "different extensions preserve first seen order", instances: []plugin.Instance{
			{ID: "one", ExtensionID: "extension-b"},
			{ID: "two", ExtensionID: "extension-a"},
			{ID: "three", ExtensionID: "extension-b"},
		}, want: []string{"extension-b", "extension-a"}},
		{name: "installed but unselected extension is not started", instances: []plugin.Instance{
			{ID: "selected", ExtensionID: "extension-a"},
		}, want: []string{"extension-a"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			scheduler := background.NewScheduler()
			host := &recordingHost{scheduler: scheduler}
			runtime := newTestRuntime(t, scheduler, host, test.instances, time.Second)
			if err := runtime.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			starts, _, _ := host.state()
			if !reflect.DeepEqual(starts, test.want) {
				t.Fatalf("started extensions: got %v want %v", starts, test.want)
			}
			if scheduler.State() != background.SchedulerStarted {
				t.Fatalf("scheduler state after plugin startup: %s", scheduler.State())
			}
			if err := runtime.Shutdown(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSelectedExtensionStartupFailureIsFailFastAndRollsBack(t *testing.T) {
	for _, test := range []struct {
		name      string
		instances []plugin.Instance
		fail      string
		wantCalls []string
	}{
		{name: "first selected extension fails", instances: []plugin.Instance{{ID: "one", ExtensionID: "extension-a"}}, fail: "extension-a", wantCalls: []string{"extension-a"}},
		{name: "partial startup is cleaned up", instances: []plugin.Instance{
			{ID: "one", ExtensionID: "extension-a"},
			{ID: "two", ExtensionID: "extension-b"},
		}, fail: "extension-b", wantCalls: []string{"extension-a", "extension-b"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			scheduler := background.NewScheduler()
			host := &recordingHost{failExtension: test.fail, scheduler: scheduler}
			runtime := newTestRuntime(t, scheduler, host, test.instances, time.Second)
			err := runtime.Start(context.Background())
			if err == nil || !bytes.Contains([]byte(err.Error()), []byte(test.fail)) {
				t.Fatalf("expected startup error naming %q, got %v", test.fail, err)
			}
			starts, running, shutdownCalls := host.state()
			if !reflect.DeepEqual(starts, test.wantCalls) || running != 0 || shutdownCalls != 1 {
				t.Fatalf("failed startup calls=%v running=%d shutdown_calls=%d", starts, running, shutdownCalls)
			}
			if scheduler.State() != background.SchedulerStopped {
				t.Fatalf("scheduler advanced after plugin failure: %s", scheduler.State())
			}
			if err := runtime.Wait(); err != nil {
				t.Fatalf("wait after cleaned failed start: %v", err)
			}
		})
	}
}

func TestStartingExtensionsDoesNotMutateInstanceConfiguration(t *testing.T) {
	config := json.RawMessage(`{"team":"alpha"}`)
	instances := []plugin.Instance{{
		ID: "team-alpha", ExtensionID: "extension-a", CapabilityID: plugin.CapabilitySourceFetch, Config: config,
	}}
	wantConfig := append(json.RawMessage(nil), instances[0].Config...)
	scheduler := background.NewScheduler()
	runtime := newTestRuntime(t, scheduler, &recordingHost{}, instances, time.Second)
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(instances[0].Config, wantConfig) {
		t.Fatalf("instance configuration mutated: got %s want %s", instances[0].Config, wantConfig)
	}
}

func TestRealHostPartialStartupLeavesNoProcessRunning(t *testing.T) {
	root := t.TempDir()
	for _, spec := range []plugintest.ExtensionSpec{
		{ID: "extension-a", Mode: "normal", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch}},
		{ID: "extension-b", Mode: "handshake_id", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch}},
	} {
		plugintest.WriteExtension(t, root, spec)
	}
	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("discover fixtures: err=%v failures=%v", err, failures)
	}
	host, err := plugin.NewHost(registry, plugin.HostOptions{
		StartupTimeout: 3 * time.Second, RequestTimeout: time.Second, ShutdownTimeout: time.Second,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, background.NewScheduler(), host, []plugin.Instance{
		{ID: "one", ExtensionID: "extension-a"},
		{ID: "two", ExtensionID: "extension-b"},
	}, time.Second)
	if err := runtime.Start(context.Background()); !errors.Is(err, plugin.ErrHandshake) {
		t.Fatalf("expected handshake startup failure, got %v", err)
	}
	for _, extension := range host.Snapshot().Extensions {
		if extension.Running {
			t.Fatalf("extension remained running after partial-start rollback: %+v", extension)
		}
	}
}

func TestRealHostLeavesDiscoveredButUnselectedExtensionStopped(t *testing.T) {
	root := t.TempDir()
	for _, extensionID := range []string{"extension-a", "extension-b"} {
		plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
			ID: extensionID, Mode: "normal", Capabilities: []plugin.CapabilityID{plugin.CapabilitySourceFetch},
		})
	}
	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("discover fixtures: err=%v failures=%v", err, failures)
	}
	host, err := plugin.NewHost(registry, plugin.HostOptions{
		StartupTimeout: 3 * time.Second, RequestTimeout: time.Second, ShutdownTimeout: time.Second,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := newTestRuntime(t, background.NewScheduler(), host, []plugin.Instance{
		{ID: "selected", ExtensionID: "extension-a"},
	}, time.Second)
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = runtime.Shutdown() })
	for _, extension := range host.Snapshot().Extensions {
		wantRunning := extension.ID == "extension-a"
		if extension.Running != wantRunning {
			t.Fatalf("unexpected selected startup state: %+v", extension)
		}
	}
}

func TestPluginFixtureProcess(t *testing.T) {
	if plugintest.RunIfRequested() {
		return
	}
}

func TestZeroJobRuntimeStartsAndShutsDown(t *testing.T) {
	scheduler := background.NewScheduler()
	var hostCalls atomic.Int32
	runtime := newTestRuntime(t, scheduler, hostFunc(func(context.Context) error {
		hostCalls.Add(1)
		if scheduler.State() != background.SchedulerStopped {
			t.Fatalf("host stopped while scheduler was %s", scheduler.State())
		}
		return nil
	}), nil, time.Second)
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if scheduler.State() != background.SchedulerStarted {
		t.Fatalf("scheduler state: got %s want STARTED", scheduler.State())
	}
	if err := runtime.Shutdown(); err != nil {
		t.Fatal(err)
	}
	if scheduler.State() != background.SchedulerStopped || hostCalls.Load() != 1 {
		t.Fatalf("shutdown state=%s host_calls=%d", scheduler.State(), hostCalls.Load())
	}
}

func TestShutdownCancelsRunningJobBeforeHostAndKeepsDatabaseAlive(t *testing.T) {
	database := testsupport.OpenTestDatabase(t)
	db := database.DB
	if _, err := db.Exec(`CREATE TABLE lifecycle_probe (value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	scheduler := background.NewScheduler()
	started := make(chan struct{})
	jobExited := make(chan struct{})
	var eventsMu sync.Mutex
	var events []string
	job := jobFunc(func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		if _, err := db.ExecContext(context.Background(), `INSERT INTO lifecycle_probe (value) VALUES ('job-exit')`); err != nil {
			return err
		}
		eventsMu.Lock()
		events = append(events, "job-exit")
		eventsMu.Unlock()
		close(jobExited)
		return nil
	})
	registerTriggerJob(t, scheduler, "db-job", job)
	var hostCalls atomic.Int32
	host := hostFunc(func(context.Context) error {
		hostCalls.Add(1)
		if scheduler.State() != background.SchedulerStopped {
			t.Errorf("host shutdown before Scheduler STOPPED: %s", scheduler.State())
		}
		select {
		case <-jobExited:
		default:
			t.Error("host shutdown before running Job exited")
		}
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM lifecycle_probe`).Scan(&count); err != nil || count != 1 {
			t.Errorf("database unavailable before host shutdown: count=%d err=%v", count, err)
		}
		eventsMu.Lock()
		events = append(events, "host-shutdown")
		eventsMu.Unlock()
		return nil
	})
	runtime := newTestRuntime(t, scheduler, host, nil, time.Second)
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Trigger("db-job"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("job did not start")
	}
	if err := runtime.Shutdown(); err != nil {
		t.Fatal(err)
	}
	eventsMu.Lock()
	defer eventsMu.Unlock()
	if len(events) != 2 || events[0] != "job-exit" || events[1] != "host-shutdown" || hostCalls.Load() != 1 {
		t.Fatalf("shutdown events=%v host_calls=%d", events, hostCalls.Load())
	}
}

func TestShutdownTimeoutIsHonestAndSafeDrainPreservesOrdering(t *testing.T) {
	scheduler := background.NewScheduler()
	started := make(chan struct{})
	release := make(chan struct{})
	registerTriggerJob(t, scheduler, "uncooperative", jobFunc(func(context.Context) error {
		close(started)
		<-release
		return nil
	}))
	var hostCalls atomic.Int32
	host := hostFunc(func(context.Context) error {
		hostCalls.Add(1)
		if scheduler.State() != background.SchedulerStopped {
			t.Errorf("host shutdown before scheduler stopped: %s", scheduler.State())
		}
		return nil
	})
	runtime := newTestRuntime(t, scheduler, host, nil, 25*time.Millisecond)
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := scheduler.Trigger("uncooperative"); err != nil {
		t.Fatal(err)
	}
	<-started
	startedAt := time.Now()
	err := runtime.Shutdown()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected bounded deadline error, got %v", err)
	}
	if time.Since(startedAt) > 500*time.Millisecond {
		t.Fatalf("bounded shutdown took too long: %v", time.Since(startedAt))
	}
	if hostCalls.Load() != 0 || scheduler.State() != background.SchedulerStopping {
		t.Fatalf("unsafe timeout state=%s host_calls=%d", scheduler.State(), hostCalls.Load())
	}
	close(release)
	if err := runtime.Wait(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected honest timeout retained after drain, got %v", err)
	}
	if scheduler.State() != background.SchedulerStopped || hostCalls.Load() != 1 {
		t.Fatalf("safe drain state=%s host_calls=%d", scheduler.State(), hostCalls.Load())
	}
}

func TestRepeatedShutdownSharesOneSequence(t *testing.T) {
	scheduler := background.NewScheduler()
	var hostCalls atomic.Int32
	runtime := newTestRuntime(t, scheduler, hostFunc(func(context.Context) error {
		hostCalls.Add(1)
		return nil
	}), nil, time.Second)
	if err := runtime.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	errorsCh := make(chan error, 5)
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errorsCh <- runtime.Shutdown()
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		if err != nil {
			t.Error(err)
		}
	}
	if err := runtime.Wait(); err != nil {
		t.Fatal(err)
	}
	if hostCalls.Load() != 1 {
		t.Fatalf("host shutdown calls: got %d want 1", hostCalls.Load())
	}
}

func TestStartFailureCleansSchedulerAndHost(t *testing.T) {
	scheduler := background.NewScheduler()
	var hostCalls atomic.Int32
	runtime := newTestRuntime(t, scheduler, hostFunc(func(context.Context) error {
		hostCalls.Add(1)
		if scheduler.State() != background.SchedulerStopped {
			t.Errorf("failed-start host cleanup before Scheduler STOPPED: %s", scheduler.State())
		}
		return nil
	}), nil, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runtime.Start(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled start, got %v", err)
	}
	if scheduler.State() != background.SchedulerStopped || hostCalls.Load() != 1 {
		t.Fatalf("failed-start cleanup state=%s host_calls=%d", scheduler.State(), hostCalls.Load())
	}
	if err := runtime.Wait(); err != nil {
		t.Fatalf("wait after clean failed start: %v", err)
	}
}

func newTestRuntime(t *testing.T, scheduler *background.Scheduler, host PluginHost, instances []plugin.Instance, timeout time.Duration) *Runtime {
	t.Helper()
	runtime, err := New(scheduler, host, instances, timeout)
	if err != nil {
		t.Fatal(err)
	}
	return runtime
}

func registerTriggerJob(t *testing.T, scheduler *background.Scheduler, id string, job background.Job) {
	t.Helper()
	if err := scheduler.Register(background.JobDefinition{
		ID: id, Interval: 0, Timeout: 10 * time.Second, Enabled: true, OverlapPolicy: background.OverlapPolicySkip,
	}, job); err != nil {
		t.Fatal(err)
	}
}
