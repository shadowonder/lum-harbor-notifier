package applicationruntime

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/background"
	"lum-harbor-notifier/internal/config"
	"lum-harbor-notifier/internal/foundation"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/plannersync"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugininstance"
	"lum-harbor-notifier/internal/source"
	"lum-harbor-notifier/internal/testsupport"
)

const reminderTestCapability = string(plugin.CapabilitySourceFetch)

// ============================================================
// Registration (§12.A)
// ============================================================

func TestRegisterReminderReconciliationOnlyWhenARealReminderInstanceIsSelected(t *testing.T) {
	tests := []struct {
		name      string
		instances []plugin.Instance
		wantJob   bool
	}{
		{name: "no instances at all"},
		{name: "unrelated source.fetch plugin only", instances: []plugin.Instance{
			{ID: "team-a", ExtensionID: "some-other-source", CapabilityID: plugin.CapabilitySourceFetch},
		}},
		{name: "reminders extension but wrong capability", instances: []plugin.Instance{
			{ID: "x", ExtensionID: plannersync.ReminderExtensionID, CapabilityID: plugin.CapabilityAIProvider},
		}},
		{name: "one active reminders instance", instances: []plugin.Instance{
			{ID: "instance-1", ExtensionID: plannersync.ReminderExtensionID, CapabilityID: plugin.CapabilitySourceFetch},
		}, wantJob: true},
		{name: "multiple active reminders instances still one job", instances: []plugin.Instance{
			{ID: "instance-1", ExtensionID: plannersync.ReminderExtensionID, CapabilityID: plugin.CapabilitySourceFetch},
			{ID: "instance-2", ExtensionID: plannersync.ReminderExtensionID, CapabilityID: plugin.CapabilitySourceFetch},
			{ID: "team-a", ExtensionID: "some-other-source", CapabilityID: plugin.CapabilitySourceFetch},
		}, wantJob: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			db := testsupport.OpenTestDatabase(t)
			scheduler := background.NewScheduler()
			definition, err := RegisterReminderReconciliation(
				scheduler, test.instances,
				source.NewService(db.DB), planner.NewService(db.DB, planner.NewRepository(db.DB)),
				time.Hour, 5*time.Second,
			)
			if err != nil {
				t.Fatal(err)
			}
			gotJob := definition != nil
			if gotJob != test.wantJob {
				t.Fatalf("job registered=%v want=%v", gotJob, test.wantJob)
			}
			wantSnapshots := 0
			if test.wantJob {
				wantSnapshots = 1
				if definition.ID != ReminderReconciliationJobID {
					t.Fatalf("job ID: got %q want %q", definition.ID, ReminderReconciliationJobID)
				}
			}
			if len(scheduler.Snapshots()) != wantSnapshots {
				t.Fatalf("scheduler snapshots: got %d want %d", len(scheduler.Snapshots()), wantSnapshots)
			}
		})
	}
}

func TestRegisterReminderReconciliationDisabledInstanceNeverReachesRegistration(t *testing.T) {
	// A disabled declaration never survives plugininstance.Resolve's own
	// enablement semantics — this test proves the registration function
	// need not (and does not) reimplement that logic itself: by the time a
	// disabled Reminders declaration reaches RegisterReminderReconciliation's
	// instances slice, it is already gone.
	extension := plugin.Extension{Manifest: plugin.Manifest{
		ManifestVersion: plugin.SupportedManifestVersion, ID: plannersync.ReminderExtensionID,
		Name: "Reminders", Version: "0.1.0", ProtocolVersion: plugin.SupportedProtocolVersion,
		Capabilities: []plugin.CapabilityDeclaration{{ID: plugin.CapabilitySourceFetch}},
	}}
	registry, err := plugin.NewRegistry([]plugin.Extension{extension})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := plugininstance.Resolve([]config.PluginInstanceConfig{
		{ID: "instance-1", ExtensionID: plannersync.ReminderExtensionID, CapabilityID: reminderTestCapability, Disabled: true},
	}, registry)
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 0 {
		t.Fatalf("expected a disabled declaration to resolve to zero instances, got %+v", resolved)
	}

	db := testsupport.OpenTestDatabase(t)
	scheduler := background.NewScheduler()
	definition, err := RegisterReminderReconciliation(
		scheduler, resolved,
		source.NewService(db.DB), planner.NewService(db.DB, planner.NewRepository(db.DB)),
		time.Hour, 5*time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	if definition != nil {
		t.Fatalf("expected no job for a disabled Reminders instance, got %+v", definition)
	}
}

// ============================================================
// Composition harness (real Scheduler + real SQLite + synthetic REMINDER evidence)
// ============================================================

type reminderCompositionHarness struct {
	db             *sql.DB
	sourceService  *source.Service
	foundationRepo *foundation.Repository
	plannerService *planner.Service
	scheduler      *background.Scheduler
	definition     *background.JobDefinition
}

// newReminderCompositionHarness registers the Job with a one-hour interval so
// no periodic tick can race the test; every execution in these tests is
// driven explicitly via scheduler.Trigger, exactly as the instruction
// requires (never a real 60-second wait, and never a production-facing
// interval override — production's own hardcoded 60s in main.go is untouched).
func newReminderCompositionHarness(t *testing.T, instances []plugin.Instance) *reminderCompositionHarness {
	t.Helper()
	db := testsupport.OpenTestDatabase(t)
	sourceService := source.NewService(db.DB)
	foundationRepo := foundation.NewRepository(db.DB)
	plannerService := planner.NewService(db.DB, planner.NewRepository(db.DB))
	scheduler := background.NewScheduler()
	definition, err := RegisterReminderReconciliation(scheduler, instances, sourceService, plannerService, time.Hour, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if definition == nil {
		t.Fatal("expected a registered reminder reconciliation job")
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { shutdown(t, scheduler) })
	return &reminderCompositionHarness{
		db: db.DB, sourceService: sourceService, foundationRepo: foundationRepo, plannerService: plannerService,
		scheduler: scheduler, definition: definition,
	}
}

func (h *reminderCompositionHarness) ingest(t *testing.T, instanceID, externalID, payload string) {
	t.Helper()
	ctx := context.Background()
	checkpoint, err := h.sourceService.PrepareFetch(ctx, plannersync.ReminderExtensionID, instanceID, reminderTestCapability)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.sourceService.IngestFetchResult(ctx, source.FetchBatch{
		ExtensionID: plannersync.ReminderExtensionID, InstanceID: instanceID, CapabilityID: reminderTestCapability,
		Checkpoint: checkpoint,
		Records: []source.Observation{{
			ExternalID: externalID, SourceKind: plannersync.SourceKindReminder, ObservedAt: time.Now().UTC(),
			Payload: json.RawMessage(payload),
		}},
	}); err != nil {
		t.Fatal(err)
	}
}

// triggerAndWaitForRun triggers the job and waits for a genuinely new
// completed attempt: LastAttemptAt strictly after whatever it was
// immediately before this call (nil counts as "before the beginning of
// time"), and the run no longer in flight.
func (h *reminderCompositionHarness) triggerAndWaitForRun(t *testing.T) {
	t.Helper()
	before, err := h.scheduler.Snapshot(h.definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.scheduler.Trigger(h.definition.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "reminder reconciliation run", func() bool {
		snapshot, err := h.scheduler.Snapshot(h.definition.ID)
		if err != nil || snapshot.Running || snapshot.LastAttemptAt == nil {
			return false
		}
		return before.LastAttemptAt == nil || snapshot.LastAttemptAt.After(*before.LastAttemptAt)
	})
}

func mustExternalRef(t *testing.T, repo *foundation.Repository, instanceID, externalID string) *foundation.ExternalRef {
	t.Helper()
	system := plannersync.ReminderExternalSystem(instanceID)
	ref, err := repo.GetExternalRefByIdentity(context.Background(), system, externalID)
	if err != nil {
		t.Fatalf("get external ref (%s, %s): %v", system, externalID, err)
	}
	return ref
}

func mustPlannerItem(t *testing.T, db *sql.DB, id string) *planner.PlannerItem {
	t.Helper()
	item, err := planner.NewRepository(db).GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("get planner item %s: %v", id, err)
	}
	return item
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(fmt.Sprintf("SELECT count(*) FROM %s", table)).Scan(&count); err != nil {
		t.Fatalf("count rows in %s: %v", table, err)
	}
	return count
}

// countPlannerHistoryEvents scopes a history_events count to one Planner
// subject entity. history_events is shared with Source ingestion, which
// appends its own SOURCE_REVISION_OBSERVED event for every new SourceRevision
// in the same table (the exact same real finding Slice 2B's own tests made) —
// a raw table-wide count would conflate "a new SourceRevision arrived" with
// "Planner truth actually changed."
func countPlannerHistoryEvents(t *testing.T, db *sql.DB, entityID string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(
		"SELECT count(*) FROM history_events WHERE scope = 'PLANNER' AND subject_entity_id = ?", entityID,
	).Scan(&count); err != nil {
		t.Fatalf("count planner history events for %s: %v", entityID, err)
	}
	return count
}

func noDueReminderPayload(title string) string {
	return fmt.Sprintf(`{"title":%q,"completed":false,"dueKind":"NONE"}`, title)
}

func timedReminderPayload(title string, year, month, day, hour, minute, second int, timeZone string) string {
	return fmt.Sprintf(
		`{"title":%q,"completed":false,"dueKind":"TIMED","dueYear":%d,"dueMonth":%d,"dueDay":%d,"dueHour":%d,"dueMinute":%d,"dueSecond":%d,"timeZone":%q}`,
		title, year, month, day, hour, minute, second, timeZone,
	)
}

func noDueReminderPayloadWithList(title, list string) string {
	return fmt.Sprintf(`{"title":%q,"completed":false,"dueKind":"NONE","list":%q}`, title, list)
}

const malformedReminderPayload = `{"title":"Bad reminder","completed":false,"dueKind":"WEEKLY"}`

// ============================================================
// B. Automatic initial reconciliation
// ============================================================

func TestAutomaticInitialReconciliationCreatesExternalPlannerItem(t *testing.T) {
	db := testsupport.OpenTestDatabase(t)
	sourceService := source.NewService(db.DB)
	foundationRepo := foundation.NewRepository(db.DB)
	plannerService := planner.NewService(db.DB, planner.NewRepository(db.DB))

	// Persist real REMINDER Source evidence BEFORE the Scheduler ever starts,
	// simulating a prior session's already-durable Source truth.
	checkpoint, err := sourceService.PrepareFetch(context.Background(), plannersync.ReminderExtensionID, "instance-1", reminderTestCapability)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sourceService.IngestFetchResult(context.Background(), source.FetchBatch{
		ExtensionID: plannersync.ReminderExtensionID, InstanceID: "instance-1", CapabilityID: reminderTestCapability,
		Checkpoint: checkpoint,
		Records: []source.Observation{{
			ExternalID: "item-1", SourceKind: plannersync.SourceKindReminder, ObservedAt: time.Now().UTC(),
			Payload: json.RawMessage(noDueReminderPayload("Buy milk")),
		}},
	}); err != nil {
		t.Fatal(err)
	}

	scheduler := background.NewScheduler()
	instances := []plugin.Instance{{ID: "instance-1", ExtensionID: plannersync.ReminderExtensionID, CapabilityID: plugin.CapabilitySourceFetch}}
	definition, err := RegisterReminderReconciliation(scheduler, instances, sourceService, plannerService, time.Hour, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if definition == nil {
		t.Fatal("expected a registered job")
	}
	if err := scheduler.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { shutdown(t, scheduler) })

	// Mirror main.go's own "trigger an initial run right after Start" pattern
	// exactly — this is the startup-reconciliation behavior under test.
	if err := scheduler.Trigger(definition.ID); err != nil {
		t.Fatal(err)
	}

	waitFor(t, "initial automatic reconciliation to create the planner item", func() bool {
		_, err := foundationRepo.GetExternalRefByIdentity(context.Background(), plannersync.ReminderExternalSystem("instance-1"), "item-1")
		return err == nil
	})

	ref := mustExternalRef(t, foundationRepo, "instance-1", "item-1")
	item := mustPlannerItem(t, db.DB, ref.EntityID)
	if item.Title != "Buy milk" || item.Authority != planner.AuthorityExternal {
		t.Fatalf("unexpected planner item created by automatic reconciliation: %+v", item)
	}
}

// ============================================================
// C. Scheduled reconciliation (update-in-place)
// ============================================================

func TestScheduledReconciliationUpdatesSamePlannerEntity(t *testing.T) {
	h := newReminderCompositionHarness(t, []plugin.Instance{
		{ID: "instance-1", ExtensionID: plannersync.ReminderExtensionID, CapabilityID: plugin.CapabilitySourceFetch},
	})
	h.ingest(t, "instance-1", "item-1", noDueReminderPayload("Buy milk"))
	h.triggerAndWaitForRun(t)
	originalEntityID := mustExternalRef(t, h.foundationRepo, "instance-1", "item-1").EntityID

	h.ingest(t, "instance-1", "item-1", timedReminderPayload("Buy oat milk", 2027, 1, 15, 9, 30, 0, "UTC"))
	h.triggerAndWaitForRun(t)

	updatedEntityID := mustExternalRef(t, h.foundationRepo, "instance-1", "item-1").EntityID
	if updatedEntityID != originalEntityID {
		t.Fatalf("expected the same Planner entity, got %s vs %s", originalEntityID, updatedEntityID)
	}
	item := mustPlannerItem(t, h.db, updatedEntityID)
	if item.Title != "Buy oat milk" || item.DueAt == nil {
		t.Fatalf("expected the update to take effect on the same entity, got %+v", item)
	}
	if countRows(t, h.db, "planner_items") != 1 {
		t.Fatal("expected exactly one planner item, no duplicate created")
	}
}

// ============================================================
// D. True no-op
// ============================================================

func TestUnchangedSourceTruthProducesNoNewPlannerRevisionOrHistory(t *testing.T) {
	h := newReminderCompositionHarness(t, []plugin.Instance{
		{ID: "instance-1", ExtensionID: plannersync.ReminderExtensionID, CapabilityID: plugin.CapabilitySourceFetch},
	})
	h.ingest(t, "instance-1", "item-1", noDueReminderPayloadWithList("Buy milk", "Groceries"))
	h.triggerAndWaitForRun(t)
	entityID := mustExternalRef(t, h.foundationRepo, "instance-1", "item-1").EntityID
	beforeRevisions := countRows(t, h.db, "revisions")
	beforeHistory := countPlannerHistoryEvents(t, h.db, entityID)

	// A genuinely new SourceRevision (list changed) that maps to the same
	// Planner-native state must still be a true Planner no-op.
	h.ingest(t, "instance-1", "item-1", noDueReminderPayloadWithList("Buy milk", "Errands"))
	h.triggerAndWaitForRun(t)

	item := mustPlannerItem(t, h.db, entityID)
	if item.Title != "Buy milk" {
		t.Fatalf("unexpected mutation: %+v", item)
	}
	if got := countRows(t, h.db, "revisions"); got != beforeRevisions {
		t.Fatalf("expected no new Planner revision, before=%d after=%d", beforeRevisions, got)
	}
	if got := countPlannerHistoryEvents(t, h.db, entityID); got != beforeHistory {
		t.Fatalf("expected no new Planner history event, before=%d after=%d", beforeHistory, got)
	}
}

// ============================================================
// E. Multiple Instances, one Job
// ============================================================

func TestOneJobReconcilesMultipleReminderInstances(t *testing.T) {
	h := newReminderCompositionHarness(t, []plugin.Instance{
		{ID: "instance-1", ExtensionID: plannersync.ReminderExtensionID, CapabilityID: plugin.CapabilitySourceFetch},
		{ID: "instance-2", ExtensionID: plannersync.ReminderExtensionID, CapabilityID: plugin.CapabilitySourceFetch},
	})
	h.ingest(t, "instance-1", "item-1", noDueReminderPayload("Instance one's task"))
	h.ingest(t, "instance-2", "item-1", noDueReminderPayload("Instance two's task"))
	h.triggerAndWaitForRun(t)

	refOne := mustExternalRef(t, h.foundationRepo, "instance-1", "item-1")
	refTwo := mustExternalRef(t, h.foundationRepo, "instance-2", "item-1")
	if refOne.EntityID == refTwo.EntityID {
		t.Fatal("expected distinct entities for the same external ID under different instances")
	}
	itemOne := mustPlannerItem(t, h.db, refOne.EntityID)
	itemTwo := mustPlannerItem(t, h.db, refTwo.EntityID)
	if itemOne.Title != "Instance one's task" || itemTwo.Title != "Instance two's task" {
		t.Fatalf("unexpected items from the one shared job: %+v / %+v", itemOne, itemTwo)
	}
	if len(h.scheduler.Snapshots()) != 1 {
		t.Fatalf("expected exactly one reconciliation job for both instances, got %d", len(h.scheduler.Snapshots()))
	}
}

// ============================================================
// F. Partial failure
// ============================================================

func TestPartialFailureCommitsValidRecordAndReportsFailure(t *testing.T) {
	h := newReminderCompositionHarness(t, []plugin.Instance{
		{ID: "instance-1", ExtensionID: plannersync.ReminderExtensionID, CapabilityID: plugin.CapabilitySourceFetch},
	})
	h.ingest(t, "instance-1", "item-good", noDueReminderPayload("Good task"))
	h.ingest(t, "instance-1", "item-bad", malformedReminderPayload)
	h.triggerAndWaitForRun(t)

	ref := mustExternalRef(t, h.foundationRepo, "instance-1", "item-good")
	item := mustPlannerItem(t, h.db, ref.EntityID)
	if item.Title != "Good task" {
		t.Fatalf("expected the valid record to commit, got %+v", item)
	}
	if _, err := h.foundationRepo.GetExternalRefByIdentity(context.Background(), plannersync.ReminderExternalSystem("instance-1"), "item-bad"); err == nil {
		t.Fatal("expected no PlannerItem for the malformed record")
	}

	snapshot, err := h.scheduler.Snapshot(h.definition.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.LastError == "" || !strings.Contains(snapshot.LastError, "1 of 2") {
		t.Fatalf("expected the Scheduler to record a partial-failure summary, got %+v", snapshot)
	}
	if snapshot.ConsecutiveFailures == 0 {
		t.Fatal("expected the Scheduler to count this attempt as a failure despite the committed valid record")
	}
}

// ============================================================
// G. Cancellation / shutdown
// ============================================================

func TestReminderReconciliationJobShutsDownCleanly(t *testing.T) {
	h := newReminderCompositionHarness(t, []plugin.Instance{
		{ID: "instance-1", ExtensionID: plannersync.ReminderExtensionID, CapabilityID: plugin.CapabilitySourceFetch},
	})
	h.ingest(t, "instance-1", "item-1", noDueReminderPayload("Buy milk"))
	h.triggerAndWaitForRun(t)
	// t.Cleanup registered by newReminderCompositionHarness performs the
	// actual shutdown-and-verify (shutdown() asserts a clean, idempotent
	// Shutdown within a bounded timeout, and that a repeated Shutdown call
	// stays unchanged) — this test exists to name the scenario explicitly
	// and to prove shutdown succeeds even with at least one real completed
	// reconciliation run in the job's history.
}
