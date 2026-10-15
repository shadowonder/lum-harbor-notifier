package planner

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"lum-harbor-notifier/internal/foundation"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/source"
	"lum-harbor-notifier/internal/temporal"
	"lum-harbor-notifier/internal/testsupport"
)

func ptr[T any](v T) *T { return &v }

func newExternalCreateInput(system, externalID string) CreateExternalPlannerItemInput {
	return CreateExternalPlannerItemInput{
		System: system, ExternalID: externalID,
		Type: ItemTypeReminder, Title: "Buy milk", Status: StatusOpen,
	}
}

// ingestOneRealSourceRevision creates a real, persisted SourceRevision
// through the actual Source Layer ingestion path, so provenance tests use a
// genuine ID rather than an arbitrary string. history_events.source_revision_id
// is a real foreign key to source_revisions(id) — an arbitrary string is
// rejected, which is exactly the invariant this exists to prove.
func ingestOneRealSourceRevision(t *testing.T, ctx context.Context, db *sql.DB, externalID string) string {
	t.Helper()
	sourceService := source.NewService(db)
	checkpoint, err := sourceService.PrepareFetch(ctx, "reminders-ext", "instance-1", "source.fetch")
	if err != nil {
		t.Fatal(err)
	}
	result, err := sourceService.IngestFetchResult(ctx, source.FetchBatch{
		ExtensionID: "reminders-ext", InstanceID: "instance-1", CapabilityID: "source.fetch", Checkpoint: checkpoint,
		Records: []source.Observation{{
			ExternalID: externalID, SourceKind: "reminder", ObservedAt: time.Now().UTC(),
			Payload: json.RawMessage(`{"title":"Buy milk"}`),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.NewlyChangedRevisionIDs) != 1 {
		t.Fatalf("expected exactly one new source revision, got %+v", result)
	}
	return result.NewlyChangedRevisionIDs[0]
}

// ---------- External Create ----------

func TestCreateExternalPlannerItemCreatesItemEntityRevisionRefAndHistory(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))

	sourceRevisionID := ingestOneRealSourceRevision(t, ctx, db.DB, "reminder-abc")
	input := newExternalCreateInput("APPLE_REMINDER:instance-1", "reminder-abc")
	input.SourceRevisionID = &sourceRevisionID
	item, err := service.CreateExternalPlannerItem(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if item.Authority != AuthorityExternal || item.Type != ItemTypeReminder || item.Title != "Buy milk" || item.Status != StatusOpen {
		t.Fatalf("unexpected created item: %+v", item)
	}

	// Entity exists.
	var entityType string
	if err := db.DB.QueryRow("SELECT entity_type FROM entities WHERE id = ?", item.ID).Scan(&entityType); err != nil {
		t.Fatalf("entity was not created: %v", err)
	}
	if entityType != "PLANNER_ITEM" {
		t.Fatalf("unexpected entity_type: %s", entityType)
	}

	// Revision is SYNC-origin.
	var originType string
	if err := db.DB.QueryRow("SELECT origin_type FROM revisions WHERE id = ?", item.LastRevisionID).Scan(&originType); err != nil {
		t.Fatalf("revision was not created: %v", err)
	}
	if originType != string(foundation.RevisionOriginSync) {
		t.Fatalf("expected SYNC revision origin, got %s", originType)
	}

	// AUTHORITATIVE external ref resolves back to the same entity.
	foundationRepo := foundation.NewRepository(db.DB)
	ref, err := foundationRepo.GetExternalRefByIdentity(ctx, "APPLE_REMINDER:instance-1", "reminder-abc")
	if err != nil {
		t.Fatal(err)
	}
	if ref.EntityID != item.ID || ref.RelationshipType != foundation.RelationshipAuthoritative {
		t.Fatalf("unexpected external ref: %+v", ref)
	}

	// History: PLANNER_ITEM_CREATED, actor SOURCE, provenance carries
	// source_revision_id.
	historyRepo := history.NewRepository(db.DB)
	event, err := historyRepo.FindLatestBySubjectAndType(ctx, item.ID, history.EventPlannerItemCreated)
	if err != nil {
		t.Fatal(err)
	}
	if event == nil {
		t.Fatal("expected a PLANNER_ITEM_CREATED history event")
	}
	if event.Actor != history.ActorSource {
		t.Fatalf("expected actor SOURCE, got %s", event.Actor)
	}
	if event.SourceRevisionID == nil || *event.SourceRevisionID != sourceRevisionID {
		t.Fatalf("expected source_revision_id provenance, got %+v", event.SourceRevisionID)
	}
	if event.RevisionID == nil || *event.RevisionID != item.LastRevisionID {
		t.Fatalf("expected history event linked to the created revision, got %+v", event.RevisionID)
	}
}

func TestCreateExternalPlannerItemRollsBackOnDuplicateExternalIdentityLeavingNoOrphans(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))

	if _, err := service.CreateExternalPlannerItem(ctx, newExternalCreateInput("APPLE_REMINDER:instance-1", "reminder-abc")); err != nil {
		t.Fatal(err)
	}

	beforeEntities := countRows(t, db.DB, "entities")
	beforeRevisions := countRows(t, db.DB, "revisions")
	beforePlannerItems := countRows(t, db.DB, "planner_items")
	beforeExternalRefs := countRows(t, db.DB, "external_refs")
	beforeHistory := countRows(t, db.DB, "history_events")

	second := newExternalCreateInput("APPLE_REMINDER:instance-1", "reminder-abc")
	second.Title = "A different title"
	_, err := service.CreateExternalPlannerItem(ctx, second)
	if !errors.Is(err, foundation.ErrExternalRefIdentityConflict) {
		t.Fatalf("expected ErrExternalRefIdentityConflict, got %v", err)
	}

	if got := countRows(t, db.DB, "entities"); got != beforeEntities {
		t.Fatalf("orphan entity created: before=%d after=%d", beforeEntities, got)
	}
	if got := countRows(t, db.DB, "revisions"); got != beforeRevisions {
		t.Fatalf("orphan revision created: before=%d after=%d", beforeRevisions, got)
	}
	if got := countRows(t, db.DB, "planner_items"); got != beforePlannerItems {
		t.Fatalf("orphan planner item created: before=%d after=%d", beforePlannerItems, got)
	}
	if got := countRows(t, db.DB, "external_refs"); got != beforeExternalRefs {
		t.Fatalf("orphan external ref created: before=%d after=%d", beforeExternalRefs, got)
	}
	if got := countRows(t, db.DB, "history_events"); got != beforeHistory {
		t.Fatalf("orphan history event created: before=%d after=%d", beforeHistory, got)
	}
}

func TestExternalPlannerItemCreationDoesNotAffectLocalCreateBehavior(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))

	if _, err := service.CreateExternalPlannerItem(ctx, newExternalCreateInput("APPLE_REMINDER:instance-1", "reminder-abc")); err != nil {
		t.Fatal(err)
	}

	local, err := service.CreateLocalTodo(ctx, CreateLocalTodoInput{Title: "Local todo"})
	if err != nil {
		t.Fatal(err)
	}
	if local.Authority != AuthorityLocal {
		t.Fatalf("expected LOCAL authority, got %s", local.Authority)
	}

	// Completing the LOCAL item must still work normally and must not touch
	// the unrelated EXTERNAL item created above.
	completed, err := service.CompleteLocalTodo(ctx, local.ID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Status != StatusCompleted {
		t.Fatalf("expected local todo to complete, got %+v", completed)
	}
}

// ---------- External Sync (update) ----------

func createExternalFixture(t *testing.T, ctx context.Context, service *Service, system, externalID string, native ExternalPlannerItemNativeState) *PlannerItem {
	t.Helper()
	item, err := service.CreateExternalPlannerItem(ctx, CreateExternalPlannerItemInput{
		System: system, ExternalID: externalID, Type: ItemTypeReminder,
		Title: native.Title, Details: native.Details, DueAt: native.DueAt, DueDate: native.DueDate,
		Status: native.Status, CompletedAt: native.CompletedAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func TestSyncExternalPlannerItemTitleChange(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))
	item := createExternalFixture(t, ctx, service, "APPLE_REMINDER:instance-1", "r1", ExternalPlannerItemNativeState{Title: "Old title", Status: StatusOpen})

	result, err := service.SyncExternalPlannerItem(ctx, SyncExternalPlannerItemInput{
		ID:     item.ID,
		Native: ExternalPlannerItemNativeState{Title: "New title", Status: StatusOpen},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Item.Title != "New title" {
		t.Fatalf("unexpected sync result: %+v", result)
	}
	if result.Item.LastRevisionID == item.LastRevisionID {
		t.Fatal("expected a new revision id after a real change")
	}
}

func TestSyncExternalPlannerItemDetailsChange(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))
	item := createExternalFixture(t, ctx, service, "APPLE_REMINDER:instance-1", "r1", ExternalPlannerItemNativeState{Title: "Task", Status: StatusOpen})

	result, err := service.SyncExternalPlannerItem(ctx, SyncExternalPlannerItemInput{
		ID:     item.ID,
		Native: ExternalPlannerItemNativeState{Title: "Task", Details: ptr("Some notes"), Status: StatusOpen},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Item.Details == nil || *result.Item.Details != "Some notes" {
		t.Fatalf("unexpected sync result: %+v", result)
	}
}

func TestSyncExternalPlannerItemTimedDue(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))
	item := createExternalFixture(t, ctx, service, "APPLE_REMINDER:instance-1", "r1", ExternalPlannerItemNativeState{Title: "Task", Status: StatusOpen})

	due := time.Date(2027, 1, 15, 15, 30, 0, 0, time.UTC)
	result, err := service.SyncExternalPlannerItem(ctx, SyncExternalPlannerItemInput{
		ID:     item.ID,
		Native: ExternalPlannerItemNativeState{Title: "Task", DueAt: &due, Status: StatusOpen},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Item.DueAt == nil || !result.Item.DueAt.Equal(due) || result.Item.DueDate != nil {
		t.Fatalf("unexpected timed-due sync result: %+v", result.Item)
	}
}

func TestSyncExternalPlannerItemDateOnlyDue(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))
	item := createExternalFixture(t, ctx, service, "APPLE_REMINDER:instance-1", "r1", ExternalPlannerItemNativeState{Title: "Task", Status: StatusOpen})

	dueDate := temporal.LocalDate("2027-01-15")
	result, err := service.SyncExternalPlannerItem(ctx, SyncExternalPlannerItemInput{
		ID:     item.ID,
		Native: ExternalPlannerItemNativeState{Title: "Task", DueDate: &dueDate, Status: StatusOpen},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Item.DueDate == nil || *result.Item.DueDate != dueDate || result.Item.DueAt != nil {
		t.Fatalf("unexpected date-only sync result: %+v", result.Item)
	}
}

func TestSyncExternalPlannerItemDueRemoved(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))
	due := time.Date(2027, 1, 15, 15, 30, 0, 0, time.UTC)
	item := createExternalFixture(t, ctx, service, "APPLE_REMINDER:instance-1", "r1", ExternalPlannerItemNativeState{Title: "Task", DueAt: &due, Status: StatusOpen})

	result, err := service.SyncExternalPlannerItem(ctx, SyncExternalPlannerItemInput{
		ID:     item.ID,
		Native: ExternalPlannerItemNativeState{Title: "Task", Status: StatusOpen},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Item.DueAt != nil || result.Item.DueDate != nil {
		t.Fatalf("expected due to be cleared, got %+v", result.Item)
	}
}

func TestSyncExternalPlannerItemOpenToCompletedUsesSuppliedTimestamp(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))
	item := createExternalFixture(t, ctx, service, "APPLE_REMINDER:instance-1", "r1", ExternalPlannerItemNativeState{Title: "Task", Status: StatusOpen})

	completedAt := time.Date(2027, 1, 10, 9, 0, 0, 0, time.UTC)
	result, err := service.SyncExternalPlannerItem(ctx, SyncExternalPlannerItemInput{
		ID:     item.ID,
		Native: ExternalPlannerItemNativeState{Title: "Task", Status: StatusCompleted, CompletedAt: &completedAt},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Item.Status != StatusCompleted || result.Item.CompletedAt == nil || !result.Item.CompletedAt.Equal(completedAt) {
		t.Fatalf("unexpected completion sync result: %+v", result.Item)
	}
}

func TestSyncExternalPlannerItemCompletedToOpenClearsCompletedAt(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))
	completedAt := time.Date(2027, 1, 10, 9, 0, 0, 0, time.UTC)
	item := createExternalFixture(t, ctx, service, "APPLE_REMINDER:instance-1", "r1", ExternalPlannerItemNativeState{Title: "Task", Status: StatusCompleted, CompletedAt: &completedAt})

	result, err := service.SyncExternalPlannerItem(ctx, SyncExternalPlannerItemInput{
		ID:     item.ID,
		Native: ExternalPlannerItemNativeState{Title: "Task", Status: StatusOpen},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed || result.Item.Status != StatusOpen || result.Item.CompletedAt != nil {
		t.Fatalf("expected reopen to clear completed_at, got %+v", result.Item)
	}
}

func TestSyncExternalPlannerItemMultipleFieldsProduceOneRevisionAndOneHistoryEvent(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))
	item := createExternalFixture(t, ctx, service, "APPLE_REMINDER:instance-1", "r1", ExternalPlannerItemNativeState{Title: "Old", Status: StatusOpen})

	beforeRevisions := countRows(t, db.DB, "revisions")
	beforeHistory := countRows(t, db.DB, "history_events")

	completedAt := time.Date(2027, 1, 10, 9, 0, 0, 0, time.UTC)
	due := time.Date(2027, 1, 15, 15, 30, 0, 0, time.UTC)
	result, err := service.SyncExternalPlannerItem(ctx, SyncExternalPlannerItemInput{
		ID: item.ID,
		Native: ExternalPlannerItemNativeState{
			Title: "New", DueAt: &due, Status: StatusCompleted, CompletedAt: &completedAt,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("expected a real change")
	}
	if got := countRows(t, db.DB, "revisions"); got != beforeRevisions+1 {
		t.Fatalf("expected exactly one new revision for a multi-field sync, before=%d after=%d", beforeRevisions, got)
	}
	if got := countRows(t, db.DB, "history_events"); got != beforeHistory+1 {
		t.Fatalf("expected exactly one new history event for a multi-field sync, before=%d after=%d", beforeHistory, got)
	}

	historyRepo := history.NewRepository(db.DB)
	event, err := historyRepo.FindLatestBySubjectAndType(ctx, item.ID, history.EventPlannerItemExternalUpdated)
	if err != nil {
		t.Fatal(err)
	}
	if event == nil {
		t.Fatal("expected a PLANNER_ITEM_EXTERNAL_UPDATED history event")
	}
	if event.Actor != history.ActorSource {
		t.Fatalf("expected actor SOURCE, got %s", event.Actor)
	}
}

func TestSyncExternalPlannerItemIdenticalStateIsTrueNoOp(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))
	due := time.Date(2027, 1, 15, 15, 30, 0, 0, time.UTC)
	item := createExternalFixture(t, ctx, service, "APPLE_REMINDER:instance-1", "r1", ExternalPlannerItemNativeState{
		Title: "Task", Details: ptr("notes"), DueAt: &due, Status: StatusOpen,
	})

	beforeRevisions := countRows(t, db.DB, "revisions")
	beforeHistory := countRows(t, db.DB, "history_events")

	result, err := service.SyncExternalPlannerItem(ctx, SyncExternalPlannerItemInput{
		ID: item.ID,
		Native: ExternalPlannerItemNativeState{
			Title: "Task", Details: ptr("notes"), DueAt: &due, Status: StatusOpen,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatalf("expected a true no-op, got Changed=true: %+v", result.Item)
	}
	if got := countRows(t, db.DB, "revisions"); got != beforeRevisions {
		t.Fatalf("expected no new revision for an identical sync, before=%d after=%d", beforeRevisions, got)
	}
	if got := countRows(t, db.DB, "history_events"); got != beforeHistory {
		t.Fatalf("expected no new history event for an identical sync, before=%d after=%d", beforeHistory, got)
	}
	if result.Item.LastRevisionID != item.LastRevisionID {
		t.Fatalf("expected last_revision_id to stay unchanged on a no-op, before=%s after=%s", item.LastRevisionID, result.Item.LastRevisionID)
	}
}

func TestSyncExternalPlannerItemRejectsLocalItem(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))

	local, err := service.CreateLocalTodo(ctx, CreateLocalTodoInput{Title: "Local todo"})
	if err != nil {
		t.Fatal(err)
	}

	beforeRevisions := countRows(t, db.DB, "revisions")

	_, err = service.SyncExternalPlannerItem(ctx, SyncExternalPlannerItemInput{
		ID:     local.ID,
		Native: ExternalPlannerItemNativeState{Title: "Hijacked title", Status: StatusOpen},
	})
	if !errors.Is(err, ErrNotExternalAuthority) {
		t.Fatalf("expected ErrNotExternalAuthority, got %v", err)
	}
	if got := countRows(t, db.DB, "revisions"); got != beforeRevisions {
		t.Fatalf("expected no revision created when rejecting a LOCAL item, before=%d after=%d", beforeRevisions, got)
	}

	// The LOCAL item's title must be completely untouched.
	reloaded, err := NewRepository(db.DB).GetByID(ctx, local.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Title != "Local todo" {
		t.Fatalf("external sync mutated a LOCAL item's title: %+v", reloaded)
	}
}

func TestSyncExternalPlannerItemRejectsNonexistentItem(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))

	_, err := service.SyncExternalPlannerItem(ctx, SyncExternalPlannerItemInput{
		ID:     "does-not-exist",
		Native: ExternalPlannerItemNativeState{Title: "Task", Status: StatusOpen},
	})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// ---------- ReconcileExternalPlannerItem (M14 Slice 2 boundary consolidation) ----------
//
// These tests are the primary deterministic proof for external identity
// resolution and create-race recovery, moved here from
// internal/plannersync.Reconciler's own tests (M13 Slice 2B) because Planner
// now owns that decision, not the cross-domain reconciler that merely
// translates evidence into these calls.

func reconcileInput(system, externalID string, native ExternalPlannerItemNativeState) ReconcileExternalPlannerItemInput {
	return ReconcileExternalPlannerItemInput{
		System: system, ExternalID: externalID, Type: ItemTypeReminder, Native: native,
	}
}

func TestReconcileExternalPlannerItemFirstObservationCreates(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))

	sourceRevisionID := ingestOneRealSourceRevision(t, ctx, db.DB, "reminder-abc")
	input := reconcileInput("APPLE_REMINDER:instance-1", "reminder-abc", ExternalPlannerItemNativeState{Title: "Buy milk", Status: StatusOpen})
	input.SourceRevisionID = &sourceRevisionID

	result, err := service.ReconcileExternalPlannerItem(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != ReconcileOutcomeCreated {
		t.Fatalf("expected CREATED, got %s", result.Outcome)
	}
	if result.Item.Title != "Buy milk" || result.Item.Authority != AuthorityExternal {
		t.Fatalf("unexpected created item: %+v", result.Item)
	}

	foundationRepo := foundation.NewRepository(db.DB)
	ref, err := foundationRepo.GetExternalRefByIdentity(ctx, "APPLE_REMINDER:instance-1", "reminder-abc")
	if err != nil {
		t.Fatal(err)
	}
	if ref.EntityID != result.Item.ID || ref.RelationshipType != foundation.RelationshipAuthoritative {
		t.Fatalf("unexpected external ref: %+v", ref)
	}

	historyRepo := history.NewRepository(db.DB)
	event, err := historyRepo.FindLatestBySubjectAndType(ctx, result.Item.ID, history.EventPlannerItemCreated)
	if err != nil {
		t.Fatal(err)
	}
	if event == nil || event.Actor != history.ActorSource {
		t.Fatalf("expected a PLANNER_ITEM_CREATED history event with actor SOURCE, got %+v", event)
	}
}

func TestReconcileExternalPlannerItemExistingChangedItemUpdatesSameEntity(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))

	created, err := service.ReconcileExternalPlannerItem(ctx, reconcileInput("APPLE_REMINDER:instance-1", "reminder-abc", ExternalPlannerItemNativeState{Title: "Old title", Status: StatusOpen}))
	if err != nil {
		t.Fatal(err)
	}

	updated, err := service.ReconcileExternalPlannerItem(ctx, reconcileInput("APPLE_REMINDER:instance-1", "reminder-abc", ExternalPlannerItemNativeState{Title: "New title", Status: StatusOpen}))
	if err != nil {
		t.Fatal(err)
	}
	if updated.Outcome != ReconcileOutcomeUpdated {
		t.Fatalf("expected UPDATED, got %s", updated.Outcome)
	}
	if updated.Item.ID != created.Item.ID {
		t.Fatalf("expected the same Planner entity, got %s vs %s", created.Item.ID, updated.Item.ID)
	}
	if updated.Item.Title != "New title" {
		t.Fatalf("expected the title to be refreshed, got %+v", updated.Item)
	}
	if got := countRows(t, db.DB, "planner_items"); got != 1 {
		t.Fatalf("expected exactly one planner item, got %d", got)
	}
}

func TestReconcileExternalPlannerItemTrueNoOpProducesNoRevisionOrHistory(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))

	native := ExternalPlannerItemNativeState{Title: "Task", Status: StatusOpen}
	if _, err := service.ReconcileExternalPlannerItem(ctx, reconcileInput("APPLE_REMINDER:instance-1", "reminder-abc", native)); err != nil {
		t.Fatal(err)
	}

	beforeRevisions := countRows(t, db.DB, "revisions")
	beforeHistory := countRows(t, db.DB, "history_events")

	result, err := service.ReconcileExternalPlannerItem(ctx, reconcileInput("APPLE_REMINDER:instance-1", "reminder-abc", native))
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != ReconcileOutcomeUnchanged {
		t.Fatalf("expected UNCHANGED, got %s", result.Outcome)
	}
	if got := countRows(t, db.DB, "revisions"); got != beforeRevisions {
		t.Fatalf("expected no new revision for a true no-op, before=%d after=%d", beforeRevisions, got)
	}
	if got := countRows(t, db.DB, "history_events"); got != beforeHistory {
		t.Fatalf("expected no new history event for a true no-op, before=%d after=%d", beforeHistory, got)
	}
}

func TestReconcileExternalPlannerItemFirstObservationAlreadyCompleted(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))

	completedAt := time.Date(2027, 1, 10, 9, 0, 0, 0, time.UTC)
	result, err := service.ReconcileExternalPlannerItem(ctx, reconcileInput("APPLE_REMINDER:instance-1", "reminder-abc", ExternalPlannerItemNativeState{
		Title: "Task", Status: StatusCompleted, CompletedAt: &completedAt,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != ReconcileOutcomeCreated {
		t.Fatalf("expected CREATED, got %s", result.Outcome)
	}
	if result.Item.Status != StatusCompleted || result.Item.CompletedAt == nil || !result.Item.CompletedAt.Equal(completedAt) {
		t.Fatalf("expected the item to be created already completed, got %+v", result.Item)
	}
}

// TestReconcileExternalPlannerItemRejectsLocalItem proves that an
// (accidentally or maliciously) misconfigured external_ref pointing at a
// LOCAL PlannerItem is still rejected through this new seam, exactly as
// SyncExternalPlannerItem already guarantees — ReconcileExternalPlannerItem
// adds no new authority bypass.
func TestReconcileExternalPlannerItemRejectsLocalItem(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))

	local, err := service.CreateLocalTodo(ctx, CreateLocalTodoInput{Title: "Local todo"})
	if err != nil {
		t.Fatal(err)
	}

	foundationRepo := foundation.NewRepository(db.DB)
	if _, err := foundationRepo.CreateExternalRef(
		ctx, "manual-ref-id", local.ID, "APPLE_REMINDER:instance-1", "reminder-abc", foundation.RelationshipAuthoritative, nil,
	); err != nil {
		t.Fatal(err)
	}

	beforeRevisions := countRows(t, db.DB, "revisions")
	_, err = service.ReconcileExternalPlannerItem(ctx, reconcileInput("APPLE_REMINDER:instance-1", "reminder-abc", ExternalPlannerItemNativeState{Title: "Hijacked", Status: StatusOpen}))
	if !errors.Is(err, ErrNotExternalAuthority) {
		t.Fatalf("expected ErrNotExternalAuthority, got %v", err)
	}
	if got := countRows(t, db.DB, "revisions"); got != beforeRevisions {
		t.Fatalf("expected no revision created when rejecting a LOCAL item, before=%d after=%d", beforeRevisions, got)
	}
}

// TestReconcileExternalPlannerItemRecoversFromConcurrentCreateRace
// deterministically reproduces the create race originally proven in M13
// Slice 2B inside internal/plannersync.Reconciler (a lookup miss followed by
// another caller creating the same external identity before this call's own
// create attempt lands), using the Service-private beforeExternalCreate test
// hook rather than a real second goroutine or a distributed lock. It proves
// this method recovers by re-resolving identity and syncing instead of
// creating a second PlannerItem for the same external object — this is now
// the primary ownership-bearing test for that guarantee.
func TestReconcileExternalPlannerItemRecoversFromConcurrentCreateRace(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))

	raced := false
	service.beforeExternalCreate = func() {
		raced = true
		if _, err := service.CreateExternalPlannerItem(ctx, CreateExternalPlannerItemInput{
			System: "APPLE_REMINDER:instance-1", ExternalID: "reminder-abc",
			Type: ItemTypeReminder, Title: "Buy milk", Status: StatusOpen,
		}); err != nil {
			t.Fatalf("simulated concurrent create failed: %v", err)
		}
	}

	result, err := service.ReconcileExternalPlannerItem(ctx, reconcileInput("APPLE_REMINDER:instance-1", "reminder-abc", ExternalPlannerItemNativeState{Title: "Buy milk", Status: StatusOpen}))
	if err != nil {
		t.Fatal(err)
	}
	if !raced {
		t.Fatal("expected the injected race hook to run")
	}
	if result.Outcome != ReconcileOutcomeUnchanged {
		t.Fatalf("expected the losing caller to fall back to a no-op sync (identical native state), got %s", result.Outcome)
	}
	if got := countRows(t, db.DB, "planner_items"); got != 1 {
		t.Fatalf("expected exactly one planner item, got %d", got)
	}
	if got := countRows(t, db.DB, "external_refs"); got != 1 {
		t.Fatalf("expected exactly one external ref, got %d", got)
	}
}

// TestReconcileExternalPlannerItemSameExternalIDUnderDifferentSystemIsIdentitySafe
// proves that the (System, ExternalID) composite identity — not ExternalID
// alone — remains the real identity key through this new seam, exactly as
// M13 Slice 2B originally proved.
func TestReconcileExternalPlannerItemSameExternalIDUnderDifferentSystemIsIdentitySafe(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))

	one, err := service.ReconcileExternalPlannerItem(ctx, reconcileInput("APPLE_REMINDER:instance-1", "item-1", ExternalPlannerItemNativeState{Title: "Instance one's task", Status: StatusOpen}))
	if err != nil {
		t.Fatal(err)
	}
	two, err := service.ReconcileExternalPlannerItem(ctx, reconcileInput("APPLE_REMINDER:instance-2", "item-1", ExternalPlannerItemNativeState{Title: "Instance two's task", Status: StatusOpen}))
	if err != nil {
		t.Fatal(err)
	}
	if one.Item.ID == two.Item.ID {
		t.Fatal("expected distinct entities for the same external ID under different systems")
	}
	if got := countRows(t, db.DB, "planner_items"); got != 2 {
		t.Fatalf("expected exactly two planner items, got %d", got)
	}
}

// ---------- Validation ----------

func TestValidateExternalNativeStateRejectsBothDueAtAndDueDate(t *testing.T) {
	due := time.Now().UTC()
	dueDate := temporal.LocalDate("2027-01-15")
	if _, err := validateExternalNativeState("Task", &due, &dueDate, StatusOpen, nil); err == nil {
		t.Fatal("expected an error for both due_at and due_date set")
	}
}

func TestValidateExternalNativeStateRejectsCompletedWithoutTimestamp(t *testing.T) {
	if _, err := validateExternalNativeState("Task", nil, nil, StatusCompleted, nil); err == nil {
		t.Fatal("expected an error for COMPLETED status without a completion timestamp")
	}
}

func TestValidateExternalNativeStateRejectsOpenWithTimestamp(t *testing.T) {
	completedAt := time.Now().UTC()
	if _, err := validateExternalNativeState("Task", nil, nil, StatusOpen, &completedAt); err == nil {
		t.Fatal("expected an error for OPEN status with a completion timestamp")
	}
}

func TestValidateExternalNativeStateRejectsEmptyTitle(t *testing.T) {
	if _, err := validateExternalNativeState("   ", nil, nil, StatusOpen, nil); err == nil {
		t.Fatal("expected an error for a blank title")
	}
}

// TestValidateCreateLocalTodoStillRejectsBothScheduledFields proves the
// validateExactOrDateOnly extraction preserved LOCAL creation's existing
// scheduled_at/scheduled_date mutual-exclusion behavior.
func TestValidateCreateLocalTodoStillRejectsBothScheduledFields(t *testing.T) {
	scheduledAt := time.Now().UTC()
	scheduledDate := temporal.LocalDate("2027-01-15")
	err := validateCreateLocalTodo(CreateLocalTodoInput{
		Title: "Task", ScheduledAt: &scheduledAt, ScheduledDate: &scheduledDate,
	})
	if err == nil {
		t.Fatal("expected an error for both scheduled_at and scheduled_date set")
	}
}
