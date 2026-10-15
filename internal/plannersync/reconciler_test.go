package plannersync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"lum-harbor-notifier/internal/foundation"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/source"
	"lum-harbor-notifier/internal/testsupport"
)

const (
	testExtensionID = "reminders-eventkit-bridge"
	testInstanceID  = "instance-1"
	testCapability  = "source.fetch"
)

func intPtr(v int) *int       { return &v }
func strPtr(v string) *string { return &v }

// ---------- harness ----------

type testHarness struct {
	db         *sql.DB
	sourceSvc  *source.Service
	foundation *foundation.Repository
	plannerSvc *planner.Service
	reconciler *Reconciler
}

func newHarness(t *testing.T) *testHarness {
	t.Helper()
	database := testsupport.OpenTestDatabase(t)
	return newHarnessOnDB(t, database.DB)
}

// newHarnessOnDB builds a fresh set of Reconciler and its dependencies bound
// to an already-open database, without reusing any in-memory state from a
// previous harness. It is used to simulate an application/service restart:
// nothing about identity resolution may depend on process memory.
func newHarnessOnDB(t *testing.T, db *sql.DB) *testHarness {
	t.Helper()
	sourceSvc := source.NewService(db)
	foundationRepo := foundation.NewRepository(db)
	plannerSvc := planner.NewService(db, planner.NewRepository(db))
	reconciler, err := NewReconciler(sourceSvc, plannerSvc, testExtensionID)
	if err != nil {
		t.Fatal(err)
	}
	return &testHarness{db: db, sourceSvc: sourceSvc, foundation: foundationRepo, plannerSvc: plannerSvc, reconciler: reconciler}
}

func (h *testHarness) ingest(t *testing.T, extensionID, instanceID, externalID string, dto reminderPayloadDTO) {
	t.Helper()
	raw, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := h.sourceSvc.PrepareFetch(context.Background(), extensionID, instanceID, testCapability)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.sourceSvc.IngestFetchResult(context.Background(), source.FetchBatch{
		ExtensionID: extensionID, InstanceID: instanceID, CapabilityID: testCapability, Checkpoint: checkpoint,
		Records: []source.Observation{{
			ExternalID: externalID, SourceKind: SourceKindReminder, ObservedAt: time.Now().UTC(),
			Payload: json.RawMessage(raw),
		}},
	}); err != nil {
		t.Fatal(err)
	}
}

func (h *testHarness) ingestAbsentPoll(t *testing.T, extensionID, instanceID string) {
	t.Helper()
	checkpoint, err := h.sourceSvc.PrepareFetch(context.Background(), extensionID, instanceID, testCapability)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.sourceSvc.IngestFetchResult(context.Background(), source.FetchBatch{
		ExtensionID: extensionID, InstanceID: instanceID, CapabilityID: testCapability, Checkpoint: checkpoint,
		Records: nil,
	}); err != nil {
		t.Fatal(err)
	}
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
// in the same table; a raw table-wide count would conflate "a new
// SourceRevision arrived" with "Planner truth actually changed," which is
// exactly the distinction these tests exist to prove.
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

func mustGetPlannerItem(t *testing.T, db *sql.DB, id string) *planner.PlannerItem {
	t.Helper()
	item, err := planner.NewRepository(db).GetByID(context.Background(), id)
	if err != nil {
		t.Fatalf("get planner item %s: %v", id, err)
	}
	return item
}

func mustGetExternalRef(t *testing.T, foundationRepo *foundation.Repository, system, externalID string) *foundation.ExternalRef {
	t.Helper()
	ref, err := foundationRepo.GetExternalRefByIdentity(context.Background(), system, externalID)
	if err != nil {
		t.Fatalf("get external ref (%s, %s): %v", system, externalID, err)
	}
	return ref
}

// ---------- payload builders ----------

func noDuePayload(title string) reminderPayloadDTO {
	return reminderPayloadDTO{Title: title, DueKind: string(reminderDueNone)}
}

func dateOnlyPayload(title string, year, month, day int) reminderPayloadDTO {
	return reminderPayloadDTO{
		Title: title, DueKind: string(reminderDueDateOnly),
		DueYear: intPtr(year), DueMonth: intPtr(month), DueDay: intPtr(day),
	}
}

func timedPayload(title string, year, month, day, hour, minute, second int, tz string) reminderPayloadDTO {
	return reminderPayloadDTO{
		Title: title, DueKind: string(reminderDueTimed),
		DueYear: intPtr(year), DueMonth: intPtr(month), DueDay: intPtr(day),
		DueHour: intPtr(hour), DueMinute: intPtr(minute), DueSecond: intPtr(second),
		TimeZone: strPtr(tz),
	}
}

func completedPayload(title, completedAtRFC3339 string) reminderPayloadDTO {
	return reminderPayloadDTO{
		Title: title, DueKind: string(reminderDueNone),
		Completed: true, CompletedAt: strPtr(completedAtRFC3339),
	}
}

// ============================================================
// Payload decode/validation (unit-level, no database)
// ============================================================

func TestDecodeReminderPayloadValidation(t *testing.T) {
	cases := []struct {
		name    string
		dto     reminderPayloadDTO
		wantErr bool
	}{
		{"none valid", reminderPayloadDTO{Title: "x", DueKind: "NONE"}, false},
		{"none with a date component is invalid", reminderPayloadDTO{Title: "x", DueKind: "NONE", DueYear: intPtr(2027)}, true},
		{"date-only valid", dateOnlyPayload("x", 2027, 1, 15), false},
		{"date-only missing day", reminderPayloadDTO{Title: "x", DueKind: "DATE_ONLY", DueYear: intPtr(2027), DueMonth: intPtr(1)}, true},
		{"date-only with hour is invalid", reminderPayloadDTO{Title: "x", DueKind: "DATE_ONLY", DueYear: intPtr(2027), DueMonth: intPtr(1), DueDay: intPtr(15), DueHour: intPtr(0)}, true},
		{"timed valid", timedPayload("x", 2027, 1, 15, 9, 30, 0, "America/Chicago"), false},
		{"timed missing timezone", reminderPayloadDTO{Title: "x", DueKind: "TIMED", DueYear: intPtr(2027), DueMonth: intPtr(1), DueDay: intPtr(15), DueHour: intPtr(9), DueMinute: intPtr(30), DueSecond: intPtr(0)}, true},
		{"timed unrecognized timezone", timedPayload("x", 2027, 1, 15, 9, 30, 0, "Not/AZone"), true},
		{"timed missing time components", reminderPayloadDTO{Title: "x", DueKind: "TIMED", DueYear: intPtr(2027), DueMonth: intPtr(1), DueDay: intPtr(15), TimeZone: strPtr("UTC")}, true},
		{"unrecognized due kind", reminderPayloadDTO{Title: "x", DueKind: "WEEKLY"}, true},
		{"completed without timestamp", reminderPayloadDTO{Title: "x", DueKind: "NONE", Completed: true}, true},
		{"completed with unparseable timestamp", reminderPayloadDTO{Title: "x", DueKind: "NONE", Completed: true, CompletedAt: strPtr("not-a-time")}, true},
		{"completed valid", completedPayload("x", "2027-01-15T10:00:00Z"), false},
		{"malformed JSON", reminderPayloadDTO{}, false}, // placeholder, overridden below
	}
	for _, tc := range cases {
		if tc.name == "malformed JSON" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.dto)
			if err != nil {
				t.Fatal(err)
			}
			_, err = decodeReminderPayload(raw)
			if tc.wantErr && err == nil {
				t.Fatalf("expected an error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr && !errors.Is(err, errMalformedReminderPayload) {
				t.Fatalf("expected errMalformedReminderPayload, got %v", err)
			}
		})
	}

	t.Run("malformed JSON body", func(t *testing.T) {
		_, err := decodeReminderPayload(json.RawMessage(`{"title":`))
		if !errors.Is(err, errMalformedReminderPayload) {
			t.Fatalf("expected errMalformedReminderPayload, got %v", err)
		}
	})
}

// ============================================================
// Constructor validation
// ============================================================

func TestNewReconcilerRejectsMissingDependenciesOrExtensionID(t *testing.T) {
	h := newHarness(t)
	if _, err := NewReconciler(nil, h.plannerSvc, testExtensionID); err == nil {
		t.Fatal("expected error for nil source service")
	}
	if _, err := NewReconciler(h.sourceSvc, nil, testExtensionID); err == nil {
		t.Fatal("expected error for nil planner service")
	}
	if _, err := NewReconciler(h.sourceSvc, h.plannerSvc, "  "); err == nil {
		t.Fatal("expected error for blank extension ID")
	}
}

// ============================================================
// Create
// ============================================================

func TestReconcileCreatesOneExternalPlannerItemForNewReminder(t *testing.T) {
	h := newHarness(t)
	h.ingest(t, testExtensionID, testInstanceID, "item-1", noDuePayload("Buy milk"))

	result, err := h.reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Scanned != 1 || result.Created != 1 || result.Failed != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}

	system := ReminderExternalSystem(testInstanceID)
	ref := mustGetExternalRef(t, h.foundation, system, "item-1")
	if ref.RelationshipType != foundation.RelationshipAuthoritative {
		t.Fatalf("expected AUTHORITATIVE ref, got %s", ref.RelationshipType)
	}

	item := mustGetPlannerItem(t, h.db, ref.EntityID)
	if item.Authority != planner.AuthorityExternal || item.Type != planner.ItemTypeReminder {
		t.Fatalf("unexpected planner item: %+v", item)
	}
	if item.Title != "Buy milk" || item.Status != planner.StatusOpen || item.DueAt != nil || item.DueDate != nil {
		t.Fatalf("unexpected native state: %+v", item)
	}

	var eventType, actor string
	if err := h.db.QueryRow(
		"SELECT event_type, actor_type FROM history_events WHERE subject_entity_id = ?", ref.EntityID,
	).Scan(&eventType, &actor); err != nil {
		t.Fatal(err)
	}
	if eventType != "PLANNER_ITEM_CREATED" || actor != "SOURCE" {
		t.Fatalf("unexpected history event: type=%s actor=%s", eventType, actor)
	}
}

func TestReconcileOutOfScopeExtensionIsIgnored(t *testing.T) {
	h := newHarness(t)
	h.ingest(t, "some-other-extension", testInstanceID, "item-1", noDuePayload("Not ours"))

	result, err := h.reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Scanned != 0 {
		t.Fatalf("expected 0 scanned candidates out of producer scope, got %+v", result)
	}
	if countRows(t, h.db, "planner_items") != 0 {
		t.Fatal("expected no planner item created for an out-of-scope extension")
	}
}

// ============================================================
// Identity: no duplication, instance namespace, restart
// ============================================================

func TestReconcileRepeatedRunsDoNotDuplicate(t *testing.T) {
	h := newHarness(t)
	h.ingest(t, testExtensionID, testInstanceID, "item-1", noDuePayload("Buy milk"))

	if _, err := h.reconciler.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		result, err := h.reconciler.Reconcile(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if result.Scanned != 1 || result.Created != 0 || result.Unchanged != 1 {
			t.Fatalf("run %d: expected a true no-op, got %+v", i, result)
		}
	}
	if countRows(t, h.db, "planner_items") != 1 || countRows(t, h.db, "external_refs") != 1 {
		t.Fatalf("expected exactly one planner item and one external ref, got %d/%d",
			countRows(t, h.db, "planner_items"), countRows(t, h.db, "external_refs"))
	}
}

func TestReconcileTitleChangeUsesSamePlannerItem(t *testing.T) {
	h := newHarness(t)
	h.ingest(t, testExtensionID, testInstanceID, "item-1", noDuePayload("Buy milk"))
	if _, err := h.reconciler.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	system := ReminderExternalSystem(testInstanceID)
	originalEntityID := mustGetExternalRef(t, h.foundation, system, "item-1").EntityID

	h.ingest(t, testExtensionID, testInstanceID, "item-1", noDuePayload("Buy oat milk"))
	result, err := h.reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Updated != 1 || result.Created != 0 {
		t.Fatalf("expected an update to the same item, got %+v", result)
	}

	newEntityID := mustGetExternalRef(t, h.foundation, system, "item-1").EntityID
	if newEntityID != originalEntityID {
		t.Fatalf("expected the same PlannerItem entity, got %s vs %s", originalEntityID, newEntityID)
	}
	item := mustGetPlannerItem(t, h.db, originalEntityID)
	if item.Title != "Buy oat milk" {
		t.Fatalf("expected updated title, got %q", item.Title)
	}
	if countRows(t, h.db, "planner_items") != 1 {
		t.Fatal("expected no second PlannerItem to be created")
	}
}

func TestReconcileServiceReconstructionStillResolvesIdentity(t *testing.T) {
	h := newHarness(t)
	h.ingest(t, testExtensionID, testInstanceID, "item-1", noDuePayload("Buy milk"))
	if _, err := h.reconciler.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Simulate an application/service restart: brand-new Service/Repository
	// objects bound to the same database, none of which share any in-memory
	// state with the harness above.
	restarted := newHarnessOnDB(t, h.db)
	result, err := restarted.reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 0 || result.Unchanged != 1 {
		t.Fatalf("expected identity to resolve through external_refs after reconstruction, got %+v", result)
	}
	if countRows(t, h.db, "planner_items") != 1 {
		t.Fatal("expected no duplicate planner item after reconstruction")
	}
}

func TestReconcileSameExternalIDDifferentInstanceDoesNotCollide(t *testing.T) {
	h := newHarness(t)
	h.ingest(t, testExtensionID, "instance-1", "item-1", noDuePayload("Instance one's milk"))
	h.ingest(t, testExtensionID, "instance-2", "item-1", noDuePayload("Instance two's milk"))

	result, err := h.reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Scanned != 2 || result.Created != 2 {
		t.Fatalf("expected two independent creates, got %+v", result)
	}

	refOne := mustGetExternalRef(t, h.foundation, ReminderExternalSystem("instance-1"), "item-1")
	refTwo := mustGetExternalRef(t, h.foundation, ReminderExternalSystem("instance-2"), "item-1")
	if refOne.EntityID == refTwo.EntityID {
		t.Fatal("expected distinct entities for the same external ID under different instances")
	}
	itemOne := mustGetPlannerItem(t, h.db, refOne.EntityID)
	itemTwo := mustGetPlannerItem(t, h.db, refTwo.EntityID)
	if itemOne.Title == itemTwo.Title {
		t.Fatal("expected the two independently-created items to keep their own distinct titles")
	}
}

// ============================================================
// Update: due shapes, details, completion
// ============================================================

func reconcileOnce(t *testing.T, h *testHarness) Result {
	t.Helper()
	result, err := h.reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestReconcileDueShapeAndCompletionTransitions(t *testing.T) {
	t.Run("no due to date-only", func(t *testing.T) {
		h := newHarness(t)
		h.ingest(t, testExtensionID, testInstanceID, "item-1", noDuePayload("Task"))
		reconcileOnce(t, h)
		h.ingest(t, testExtensionID, testInstanceID, "item-1", dateOnlyPayload("Task", 2027, 1, 15))
		result := reconcileOnce(t, h)
		if result.Updated != 1 {
			t.Fatalf("expected an update, got %+v", result)
		}
		ref := mustGetExternalRef(t, h.foundation, ReminderExternalSystem(testInstanceID), "item-1")
		item := mustGetPlannerItem(t, h.db, ref.EntityID)
		if item.DueDate == nil || string(*item.DueDate) != "2027-01-15" || item.DueAt != nil {
			t.Fatalf("expected date-only due 2027-01-15, got %+v", item)
		}
	})

	t.Run("date-only to timed", func(t *testing.T) {
		h := newHarness(t)
		h.ingest(t, testExtensionID, testInstanceID, "item-1", dateOnlyPayload("Task", 2027, 1, 15))
		reconcileOnce(t, h)
		h.ingest(t, testExtensionID, testInstanceID, "item-1", timedPayload("Task", 2027, 1, 15, 9, 30, 0, "America/Chicago"))
		result := reconcileOnce(t, h)
		if result.Updated != 1 {
			t.Fatalf("expected an update, got %+v", result)
		}
		ref := mustGetExternalRef(t, h.foundation, ReminderExternalSystem(testInstanceID), "item-1")
		item := mustGetPlannerItem(t, h.db, ref.EntityID)
		if item.DueDate != nil || item.DueAt == nil {
			t.Fatalf("expected exact timed due, got %+v", item)
		}
		wantUTC := time.Date(2027, 1, 15, 9, 30, 0, 0, time.FixedZone("", -6*3600))
		if !item.DueAt.Equal(wantUTC) {
			t.Fatalf("expected due at %v (America/Chicago 09:30), got %v", wantUTC, item.DueAt)
		}
	})

	t.Run("due removed", func(t *testing.T) {
		h := newHarness(t)
		h.ingest(t, testExtensionID, testInstanceID, "item-1", dateOnlyPayload("Task", 2027, 1, 15))
		reconcileOnce(t, h)
		h.ingest(t, testExtensionID, testInstanceID, "item-1", noDuePayload("Task"))
		result := reconcileOnce(t, h)
		if result.Updated != 1 {
			t.Fatalf("expected an update, got %+v", result)
		}
		ref := mustGetExternalRef(t, h.foundation, ReminderExternalSystem(testInstanceID), "item-1")
		item := mustGetPlannerItem(t, h.db, ref.EntityID)
		if item.DueDate != nil || item.DueAt != nil {
			t.Fatalf("expected no due date/time, got %+v", item)
		}
	})

	t.Run("open to completed with real timestamp", func(t *testing.T) {
		h := newHarness(t)
		h.ingest(t, testExtensionID, testInstanceID, "item-1", noDuePayload("Task"))
		reconcileOnce(t, h)
		h.ingest(t, testExtensionID, testInstanceID, "item-1", completedPayload("Task", "2027-01-15T10:00:00Z"))
		result := reconcileOnce(t, h)
		if result.Updated != 1 {
			t.Fatalf("expected an update, got %+v", result)
		}
		ref := mustGetExternalRef(t, h.foundation, ReminderExternalSystem(testInstanceID), "item-1")
		item := mustGetPlannerItem(t, h.db, ref.EntityID)
		if item.Status != planner.StatusCompleted || item.CompletedAt == nil {
			t.Fatalf("expected COMPLETED with a real timestamp, got %+v", item)
		}
		if !item.CompletedAt.Equal(time.Date(2027, 1, 15, 10, 0, 0, 0, time.UTC)) {
			t.Fatalf("unexpected completion timestamp: %v", item.CompletedAt)
		}
	})

	t.Run("completed back to open clears completion", func(t *testing.T) {
		h := newHarness(t)
		h.ingest(t, testExtensionID, testInstanceID, "item-1", completedPayload("Task", "2027-01-15T10:00:00Z"))
		reconcileOnce(t, h)
		h.ingest(t, testExtensionID, testInstanceID, "item-1", noDuePayload("Task"))
		result := reconcileOnce(t, h)
		if result.Updated != 1 {
			t.Fatalf("expected an update, got %+v", result)
		}
		ref := mustGetExternalRef(t, h.foundation, ReminderExternalSystem(testInstanceID), "item-1")
		item := mustGetPlannerItem(t, h.db, ref.EntityID)
		if item.Status != planner.StatusOpen || item.CompletedAt != nil {
			t.Fatalf("expected reopened OPEN item with no completion timestamp, got %+v", item)
		}
	})

	t.Run("multiple field change is one revision and one history event", func(t *testing.T) {
		h := newHarness(t)
		h.ingest(t, testExtensionID, testInstanceID, "item-1", noDuePayload("Task"))
		reconcileOnce(t, h)
		entityID := mustGetExternalRef(t, h.foundation, ReminderExternalSystem(testInstanceID), "item-1").EntityID
		beforeRevisions := countRows(t, h.db, "revisions")
		beforeHistory := countPlannerHistoryEvents(t, h.db, entityID)

		h.ingest(t, testExtensionID, testInstanceID, "item-1", timedPayload("Renamed task", 2027, 1, 15, 9, 30, 0, "UTC"))
		result := reconcileOnce(t, h)
		if result.Updated != 1 {
			t.Fatalf("expected an update, got %+v", result)
		}
		if got := countRows(t, h.db, "revisions"); got != beforeRevisions+1 {
			t.Fatalf("expected exactly one new revision, before=%d after=%d", beforeRevisions, got)
		}
		if got := countPlannerHistoryEvents(t, h.db, entityID); got != beforeHistory+1 {
			t.Fatalf("expected exactly one new planner history event, before=%d after=%d", beforeHistory, got)
		}
	})
}

// ============================================================
// No-op
// ============================================================

func TestReconcileRepeatedIdenticalHeadProducesNoRevisionOrHistory(t *testing.T) {
	h := newHarness(t)
	h.ingest(t, testExtensionID, testInstanceID, "item-1", noDuePayload("Task"))
	reconcileOnce(t, h)

	beforeRevisions := countRows(t, h.db, "revisions")
	beforeHistory := countRows(t, h.db, "history_events")

	result := reconcileOnce(t, h)
	if result.Unchanged != 1 || result.Updated != 0 {
		t.Fatalf("expected a true no-op, got %+v", result)
	}
	if got := countRows(t, h.db, "revisions"); got != beforeRevisions {
		t.Fatalf("expected no new revision, before=%d after=%d", beforeRevisions, got)
	}
	if got := countRows(t, h.db, "history_events"); got != beforeHistory {
		t.Fatalf("expected no new history event, before=%d after=%d", beforeHistory, got)
	}
}

// TestReconcileNewSourceRevisionMappingToSameNativeStateIsPlannerNoOp covers
// the M13 invariant that a real new SourceRevision (a genuinely different
// canonical payload, hence a genuinely new source_revisions row) does not by
// itself force a Planner mutation: only a change to a field Planner actually
// consumes does. "list" is decoded by the payload contract but never mapped
// into Planner native state.
func TestReconcileNewSourceRevisionMappingToSameNativeStateIsPlannerNoOp(t *testing.T) {
	h := newHarness(t)
	first := noDuePayload("Task")
	first.List = strPtr("Groceries")
	h.ingest(t, testExtensionID, testInstanceID, "item-1", first)
	reconcileOnce(t, h)

	entityID := mustGetExternalRef(t, h.foundation, ReminderExternalSystem(testInstanceID), "item-1").EntityID
	beforeSourceRevisions := countRows(t, h.db, "source_revisions")
	beforePlannerRevisions := countRows(t, h.db, "revisions")
	beforeHistory := countPlannerHistoryEvents(t, h.db, entityID)

	second := noDuePayload("Task")
	second.List = strPtr("Errands")
	h.ingest(t, testExtensionID, testInstanceID, "item-1", second)
	if got := countRows(t, h.db, "source_revisions"); got != beforeSourceRevisions+1 {
		t.Fatalf("expected a genuinely new SourceRevision from the changed payload, before=%d after=%d", beforeSourceRevisions, got)
	}

	result := reconcileOnce(t, h)
	if result.Unchanged != 1 || result.Updated != 0 {
		t.Fatalf("expected the Planner-irrelevant field change to be a Planner no-op, got %+v", result)
	}
	if got := countRows(t, h.db, "revisions"); got != beforePlannerRevisions {
		t.Fatalf("expected no new Planner revision, before=%d after=%d", beforePlannerRevisions, got)
	}
	if got := countPlannerHistoryEvents(t, h.db, entityID); got != beforeHistory {
		t.Fatalf("expected no new Planner history event, before=%d after=%d", beforeHistory, got)
	}
}

// ============================================================
// Failure isolation
// ============================================================

func TestReconcileMalformedRecordDoesNotBlockValidRecord(t *testing.T) {
	h := newHarness(t)
	h.ingest(t, testExtensionID, testInstanceID, "item-good", noDuePayload("Good task"))
	h.ingest(t, testExtensionID, testInstanceID, "item-bad", reminderPayloadDTO{Title: "Bad task", DueKind: "WEEKLY"})

	result, err := h.reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Scanned != 2 || result.Created != 1 || result.Failed != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if result.FullySuccessful() {
		t.Fatal("expected FullySuccessful() to be false when a record failed")
	}

	var goodOutcome, badOutcome RecordOutcome
	for _, r := range result.Records {
		switch r.ExternalID {
		case "item-good":
			goodOutcome = r.Outcome
			if r.Error != nil {
				t.Fatalf("expected the good record to succeed, got error: %v", r.Error)
			}
		case "item-bad":
			badOutcome = r.Outcome
			if r.Error == nil {
				t.Fatal("expected the bad record to carry an error")
			}
		}
	}
	if goodOutcome != OutcomeCreated {
		t.Fatalf("expected the good record to be CREATED, got %s", goodOutcome)
	}
	if badOutcome != OutcomeFailed {
		t.Fatalf("expected the bad record to be FAILED, got %s", badOutcome)
	}
	if countRows(t, h.db, "planner_items") != 1 {
		t.Fatal("expected exactly one PlannerItem from the one valid record")
	}
}

func TestReconcileInvalidTimedTimezoneFailsThatRecordOnly(t *testing.T) {
	h := newHarness(t)
	h.ingest(t, testExtensionID, testInstanceID, "item-1", timedPayload("Task", 2027, 1, 15, 9, 0, 0, "Not/AZone"))

	result, err := h.reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Failed != 1 || result.Created != 0 {
		t.Fatalf("expected the record to fail without creating a PlannerItem, got %+v", result)
	}
	if countRows(t, h.db, "planner_items") != 0 {
		t.Fatal("expected no PlannerItem to be created for an invalid timezone")
	}
}

func TestReconcileCompletedWithoutTimestampFailsThatRecordOnly(t *testing.T) {
	h := newHarness(t)
	bad := noDuePayload("Task")
	bad.Completed = true // CompletedAt intentionally left nil
	h.ingest(t, testExtensionID, testInstanceID, "item-1", bad)

	result, err := h.reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Failed != 1 || result.Created != 0 {
		t.Fatalf("expected the record to fail without creating a PlannerItem, got %+v", result)
	}
	if countRows(t, h.db, "planner_items") != 0 {
		t.Fatal("expected no PlannerItem to be created for a completed record with no timestamp")
	}
}

// ============================================================
// Absence semantics
// ============================================================

func TestReconcileAbsentSubsequentPollDoesNotMutateExistingPlannerTruth(t *testing.T) {
	h := newHarness(t)
	h.ingest(t, testExtensionID, testInstanceID, "item-1", noDuePayload("Task"))
	reconcileOnce(t, h)

	ref := mustGetExternalRef(t, h.foundation, ReminderExternalSystem(testInstanceID), "item-1")
	before := mustGetPlannerItem(t, h.db, ref.EntityID)

	// A successful zero-record fetch: the plugin observed nothing new (e.g. a
	// permission hiccup, or simply nothing changed), which per the Source
	// Layer contract must never be interpreted as deletion.
	h.ingestAbsentPoll(t, testExtensionID, testInstanceID)

	result := reconcileOnce(t, h)
	if result.Scanned != 1 || result.Unchanged != 1 {
		t.Fatalf("expected the still-persisted head to reconcile as a no-op, got %+v", result)
	}

	after := mustGetPlannerItem(t, h.db, ref.EntityID)
	if after.Status != before.Status || after.Title != before.Title || after.LastRevisionID != before.LastRevisionID {
		t.Fatalf("expected PlannerItem truth unchanged by an absent poll: before=%+v after=%+v", before, after)
	}
	if countRows(t, h.db, "planner_items") != 1 {
		t.Fatal("expected the PlannerItem to still exist")
	}
}

// ============================================================
// Authority invariant
// ============================================================

// TestReconcileNeverMutatesALocalPlannerItemThroughAnExternalRefAccident
// proves that even if an AUTHORITATIVE external_ref accidentally pointed at
// a LOCAL PlannerItem (a state the reconciler's own code never produces,
// since CreateExternalPlannerItem always creates a fresh EXTERNAL entity),
// Slice 2A's authority check still rejects the mutation, so the reconciler
// can never silently overwrite a LOCAL item's user-owned truth.
func TestReconcileNeverMutatesALocalPlannerItemThroughAnExternalRefAccident(t *testing.T) {
	h := newHarness(t)
	local, err := h.plannerSvc.CreateLocalTodo(context.Background(), planner.CreateLocalTodoInput{Title: "My local todo"})
	if err != nil {
		t.Fatal(err)
	}

	system := ReminderExternalSystem(testInstanceID)
	if _, err := h.foundation.CreateExternalRef(
		context.Background(), "manual-ref-id", local.ID, system, "item-1", foundation.RelationshipAuthoritative, nil,
	); err != nil {
		t.Fatal(err)
	}

	// Ingest before capturing "before" counts: SourceRevision creation writes
	// its own unrelated SOURCE-scope history event, which must not be
	// conflated with whether the rejected Planner sync attempt wrote anything.
	h.ingest(t, testExtensionID, testInstanceID, "item-1", noDuePayload("Attempted overwrite"))
	beforeRevisions := countRows(t, h.db, "revisions")
	beforeHistory := countPlannerHistoryEvents(t, h.db, local.ID)

	result, err := h.reconciler.Reconcile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Failed != 1 {
		t.Fatalf("expected the sync attempt against a LOCAL item to fail, got %+v", result)
	}
	if !errors.Is(result.Records[0].Error, planner.ErrNotExternalAuthority) {
		t.Fatalf("expected ErrNotExternalAuthority, got %v", result.Records[0].Error)
	}

	reloaded := mustGetPlannerItem(t, h.db, local.ID)
	if reloaded.Title != "My local todo" || reloaded.Authority != planner.AuthorityLocal {
		t.Fatalf("expected the LOCAL item to be untouched, got %+v", reloaded)
	}
	if got := countRows(t, h.db, "revisions"); got != beforeRevisions {
		t.Fatalf("expected no new revision from the rejected sync attempt, before=%d after=%d", beforeRevisions, got)
	}
	if got := countPlannerHistoryEvents(t, h.db, local.ID); got != beforeHistory {
		t.Fatalf("expected no new history event from the rejected sync attempt, before=%d after=%d", beforeHistory, got)
	}
}

// ============================================================
// Create race / idempotency
// ============================================================
//
// The deterministic create-race recovery guarantee (a lookup miss followed
// by a competing create, recovered by re-resolving identity and syncing
// rather than creating a duplicate) moved to internal/planner along with the
// identity-resolution logic it depends on, in M14 Slice 2. See
// planner.TestReconcileExternalPlannerItemRecoversFromConcurrentCreateRace
// for the primary deterministic proof. This package retains no hook capable
// of injecting that race (Reconciler no longer resolves identity itself), so
// it is correctly out of scope for a plannersync-level test.

func TestDescribeExternalSystem(t *testing.T) {
	described, ok := DescribeExternalSystem(ReminderExternalSystem("inst-7"))
	if !ok || described.Label != "Apple Reminders" || described.InstanceID != "inst-7" || described.SourceKind != SourceKindReminder ||
		len(described.ExtensionIDs) == 0 || described.ExtensionIDs[0] != ReminderExtensionID {
		t.Fatalf("unexpected description: %+v ok=%v", described, ok)
	}
	for _, foreign := range []string{"", "APPLE_REMINDER", "APPLE_REMINDER:", "GOOGLE_TASKS:inst-7"} {
		if _, ok := DescribeExternalSystem(foreign); ok {
			t.Fatalf("namespace %q must not be described", foreign)
		}
	}
}
