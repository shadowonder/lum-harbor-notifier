//go:build plugincompat

package plugincompat_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugininstall"
	"lum-harbor-notifier/internal/plugininstance"
	"lum-harbor-notifier/internal/source"
	"lum-harbor-notifier/internal/testsupport"
)

const (
	remindersExtensionID         = "lh-plugin-apple-reminders"
	remindersInstanceID          = "reminders-compat-instance"
	remindersSourceKind          = "REMINDER"
	remindersArtifactEnvironment = "LUM_HARBOR_REMINDERS_PLUGIN_COMPAT_ARTIFACT"
)

// TestActualRemindersPluginProtocolAndSourceFetch is M13 Slice 3A's
// compatibility test: it proves a real, built reminders-eventkit-bridge
// Plugin artifact (built via `make -C ../reminders-eventkit-bridge build`)
// installs/discovers/handshakes exactly like any other Lum Harbor Plugin,
// that its source.fetch capability returns real EventKit Reminders through
// the existing Host/SourceFetcher types, that every record's payload
// conforms to M13 Slice 2B's structured Reminder contract
// (internal/plannersync/payload.go), and — as an optional, explicitly
// allowed bounded acceptance (M13 Slice 3A instruction §13) — that the real
// result can be ingested by the real Source Layer. It never calls
// internal/plannersync.Reconciler, never touches Planner, and registers no
// Scheduler job: it stops at the source.fetch/Source Layer boundary.
//
// This is a real macOS-only, TCC-gated test: it requires the developer's
// machine to have already granted Reminders access to this binary (see the
// Slice 3A report's Permission/TCC Findings) and is skipped everywhere else,
// exactly like TestActualHostValidatesStandaloneWindowsArtifactWithoutExecuting
// is skipped without a supplied Windows artifact.
func TestActualRemindersPluginProtocolAndSourceFetch(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Apple Reminders / EventKit is macOS-only")
	}
	configured := strings.TrimSpace(os.Getenv(remindersArtifactEnvironment))
	if configured == "" {
		t.Skipf("real Reminders plugin artifact not supplied; set %s to an absolute artifact directory", remindersArtifactEnvironment)
	}
	if !filepath.IsAbs(configured) {
		t.Fatalf("%s must be an absolute artifact directory, got %q", remindersArtifactEnvironment, configured)
	}
	artifactDirectory, err := filepath.EvalSymlinks(filepath.Clean(configured))
	if err != nil {
		t.Fatalf("resolve Reminders plugin artifact directory %q: %v", configured, err)
	}

	pluginRoot := t.TempDir()
	installer, err := plugininstall.New(pluginRoot)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := installer.Install(artifactDirectory)
	if err != nil {
		t.Fatalf("install real Reminders plugin artifact: %v", err)
	}
	if installed.ExtensionID != remindersExtensionID || !installed.RestartRequired {
		t.Fatalf("unexpected Reminders installation result: %+v", installed)
	}

	registry, failures, err := plugin.Discover(pluginRoot)
	if err != nil {
		t.Fatalf("discover staged Reminders artifact: %v", err)
	}
	if len(failures) != 0 {
		t.Fatalf("Reminders discovery failures: %+v", failures)
	}
	extensions := registry.List()
	if len(extensions) != 1 || extensions[0].Manifest.ID != remindersExtensionID {
		t.Fatalf("discovered Reminders extensions: %+v", extensions)
	}
	if _, err := registry.ResolveCapability(remindersExtensionID, plugin.CapabilitySourceFetch); err != nil {
		t.Fatalf("resolve source.fetch on Reminders artifact: %v", err)
	}

	instances, err := plugininstance.Resolve([]config.PluginInstanceConfig{{
		ID: remindersInstanceID, ExtensionID: remindersExtensionID,
		CapabilityID: string(plugin.CapabilitySourceFetch), Config: json.RawMessage(`{}`),
	}}, registry)
	if err != nil {
		t.Fatalf("resolve configured real Reminders instance: %v", err)
	}
	if len(instances) != 1 {
		t.Fatalf("resolved Reminders instances: %+v", instances)
	}

	var logs strings.Builder
	host, err := plugin.NewHost(registry, plugin.HostOptions{
		// EventKit's first access check and a real fetchReminders(matching:)
		// call are both slower than the synthetic lh-plugin-base fixture;
		// generous but bounded timeouts avoid a flaky false failure on a
		// real developer machine without masking a genuine hang.
		StartupTimeout: 10 * time.Second, RequestTimeout: 30 * time.Second, ShutdownTimeout: 5 * time.Second,
		Logger: slog.New(slog.NewTextHandler(&logs, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}

	startCtx, cancelStart := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStart()
	if err := host.Start(startCtx, remindersExtensionID); err != nil {
		t.Fatalf("start real Reminders plugin process (handshake): %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancelShutdown()
		_ = host.Shutdown(shutdownCtx)
	})

	snapshot := host.Snapshot()
	if len(snapshot.Extensions) != 1 {
		t.Fatalf("runtime snapshot extensions: %+v", snapshot)
	}
	runtimeExtension := snapshot.Extensions[0]
	if runtimeExtension.ID != remindersExtensionID || runtimeExtension.Status != "RUNNING" || !runtimeExtension.Running ||
		runtimeExtension.ProtocolVersion != plugin.SupportedProtocolVersion || len(runtimeExtension.Capabilities) != 1 ||
		runtimeExtension.Capabilities[0] != plugin.CapabilitySourceFetch || runtimeExtension.LastError != "" {
		t.Fatalf("unexpected real Reminders runtime snapshot after handshake: %+v", runtimeExtension)
	}

	fetcher := plugin.NewSourceFetcher(host)
	fetchCtx, cancelFetch := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancelFetch()
	result, err := fetcher.Fetch(fetchCtx, instances[0], plugin.SourceFetchRequest{})
	if err != nil {
		t.Fatalf("real Reminders source.fetch: %v", err)
	}
	t.Logf("real Reminders source.fetch returned %d record(s) (structural evidence only; no titles/content logged)", len(result.Records))

	seenExternalIDs := make(map[string]bool, len(result.Records))
	for _, record := range result.Records {
		if record.SourceKind != remindersSourceKind {
			t.Fatalf("record sourceKind: got %q want %q", record.SourceKind, remindersSourceKind)
		}
		if strings.TrimSpace(record.ExternalID) == "" {
			t.Fatal("record externalId must not be empty")
		}
		if seenExternalIDs[record.ExternalID] {
			t.Fatalf("duplicate externalId %q within one fetch", record.ExternalID)
		}
		seenExternalIDs[record.ExternalID] = true
		if record.ObservedAt.IsZero() {
			t.Fatal("record observedAt must not be zero")
		}
		assertReminderPayloadConformsToSlice2BContract(t, record.Payload)
	}

	// A second, independent fetch through the same running process proves
	// stable identity is not an artifact of a single in-memory fetch. It is
	// intentionally allowed to observe a different reminder set (the
	// developer's real data may change between fetches); only the identity
	// of records present in both is checked.
	secondCtx, cancelSecond := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancelSecond()
	second, err := fetcher.Fetch(secondCtx, instances[0], plugin.SourceFetchRequest{})
	if err != nil {
		t.Fatalf("second real Reminders source.fetch: %v", err)
	}
	secondExternalIDs := make(map[string]bool, len(second.Records))
	for _, record := range second.Records {
		secondExternalIDs[record.ExternalID] = true
	}
	stableCount := 0
	for id := range seenExternalIDs {
		if secondExternalIDs[id] {
			stableCount++
		}
	}
	if len(result.Records) > 0 && stableCount == 0 {
		t.Fatalf("expected at least one externalId to remain stable across two fetches on the same running process; first=%d second=%d", len(result.Records), len(second.Records))
	}

	if err := host.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown real Reminders plugin: %v", err)
	}
	logOutput := logs.String()
	if !strings.Contains(logOutput, remindersExtensionID+": started") || !strings.Contains(logOutput, remindersExtensionID+": shutdown accepted") {
		t.Fatalf("Host did not capture expected Reminders plugin stderr diagnostics: %s", logOutput)
	}

	// Optional Source Ingestion acceptance (M13 Slice 3A instruction §13):
	// prove the real Plugin's real result can be ingested by the real
	// Source Layer, without touching Planner/plannersync/Scheduler.
	assertRealResultIngestsThroughSourceLayer(t, result)
}

// assertReminderPayloadConformsToSlice2BContract independently checks the
// wire JSON shape against M13 Slice 2B's structured Reminder contract
// (title/completed/completedAt/dueKind + due components/timeZone). It does
// not call internal/plannersync's unexported decoder — this is a
// black-box wire-contract check, appropriate for a compatibility test that
// must remain valid even if that package's internals change shape without
// changing the wire contract.
func assertReminderPayloadConformsToSlice2BContract(t *testing.T, raw json.RawMessage) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("payload is not a valid JSON object: %v", err)
	}
	if _, ok := payload["title"].(string); !ok {
		t.Fatalf("payload missing string title: %v", payload)
	}
	completed, ok := payload["completed"].(bool)
	if !ok {
		t.Fatalf("payload missing bool completed: %v", payload)
	}
	if completed {
		completedAt, ok := payload["completedAt"].(string)
		if !ok {
			t.Fatalf("completed=true payload missing string completedAt: %v", payload)
		}
		if _, err := time.Parse(time.RFC3339Nano, completedAt); err != nil {
			t.Fatalf("completedAt %q does not parse as RFC3339Nano: %v", completedAt, err)
		}
	} else if _, present := payload["completedAt"]; present {
		t.Fatalf("completed=false payload must not include completedAt: %v", payload)
	}

	dueKind, ok := payload["dueKind"].(string)
	if !ok {
		t.Fatalf("payload missing string dueKind: %v", payload)
	}
	dueComponentKeys := []string{"dueYear", "dueMonth", "dueDay", "dueHour", "dueMinute", "dueSecond", "timeZone"}
	present := func(key string) bool { _, ok := payload[key]; return ok }
	switch dueKind {
	case "NONE":
		for _, key := range dueComponentKeys {
			if present(key) {
				t.Fatalf("dueKind NONE must not include %q: %v", key, payload)
			}
		}
	case "DATE_ONLY":
		for _, key := range []string{"dueYear", "dueMonth", "dueDay"} {
			if !present(key) {
				t.Fatalf("dueKind DATE_ONLY missing %q: %v", key, payload)
			}
		}
		for _, key := range []string{"dueHour", "dueMinute", "dueSecond"} {
			if present(key) {
				t.Fatalf("dueKind DATE_ONLY must not include %q: %v", key, payload)
			}
		}
	case "TIMED":
		for _, key := range []string{"dueYear", "dueMonth", "dueDay", "dueHour", "dueMinute", "dueSecond", "timeZone"} {
			if !present(key) {
				t.Fatalf("dueKind TIMED missing %q: %v", key, payload)
			}
		}
		if tz, _ := payload["timeZone"].(string); strings.TrimSpace(tz) == "" {
			t.Fatalf("dueKind TIMED has an empty timeZone: %v", payload)
		}
	default:
		t.Fatalf("unrecognized dueKind %q: %v", dueKind, payload)
	}
}

// assertRealResultIngestsThroughSourceLayer feeds the real Plugin's actual
// SourceFetchResult through the real Source Layer (source.Service), the same
// production path production/pluginadapter code uses, without engaging the
// Scheduler, application composition, or any Planner/plannersync code. It
// tolerates zero records (a developer machine may have no Reminders).
func assertRealResultIngestsThroughSourceLayer(t *testing.T, result plugin.SourceFetchResult) {
	t.Helper()
	db := testsupport.OpenTestDatabase(t)
	sourceService := source.NewService(db.DB)

	checkpoint, err := sourceService.PrepareFetch(context.Background(), remindersExtensionID, remindersInstanceID, string(plugin.CapabilitySourceFetch))
	if err != nil {
		t.Fatalf("prepare real Reminders source fetch checkpoint: %v", err)
	}
	batch := source.FetchBatch{
		ExtensionID: remindersExtensionID, InstanceID: remindersInstanceID,
		CapabilityID: string(plugin.CapabilitySourceFetch), Checkpoint: checkpoint,
	}
	for _, record := range result.Records {
		batch.Records = append(batch.Records, source.Observation{
			ExternalID: record.ExternalID, SourceKind: record.SourceKind,
			ObservedAt: record.ObservedAt, SourceUpdatedAt: record.SourceUpdatedAt,
			Payload: record.Payload,
		})
	}
	ingestion, err := sourceService.IngestFetchResult(context.Background(), batch)
	if err != nil {
		t.Fatalf("ingest real Reminders fetch result through the real Source Layer: %v", err)
	}
	totalProcessed := ingestion.CreatedCount + ingestion.UpdatedCount + ingestion.UnchangedCount
	if totalProcessed != len(result.Records) {
		t.Fatalf("source ingestion accounted for %d of %d real records: %+v", totalProcessed, len(result.Records), ingestion)
	}
	t.Logf("real Source Layer ingestion: created=%d updated=%d unchanged=%d", ingestion.CreatedCount, ingestion.UpdatedCount, ingestion.UnchangedCount)

	heads, err := sourceService.ListCurrentHeadsBySourceKind(context.Background(), remindersSourceKind)
	if err != nil {
		t.Fatalf("list ingested Reminders current heads: %v", err)
	}
	if len(heads) != len(result.Records) {
		t.Fatalf("current REMINDER heads after ingestion: got %d want %d", len(heads), len(result.Records))
	}
}
