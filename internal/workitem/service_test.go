package workitem

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"lum-harbor-notifier/internal/testsupport"
)

func TestServiceCreateLocalWorkItemCreatesRevisionEntityAndDomainState(t *testing.T) {
	ctx := context.Background()
	database := testsupport.OpenTestDatabase(t)
	service := NewService(database.DB, NewRepository(database.DB))
	description := "Coordinate the production release outcome"

	created, err := service.CreateLocalWorkItem(ctx, CreateLocalWorkItemInput{
		Title: "  Production Release  ", Description: &description,
	})
	if err != nil {
		t.Fatal(err)
	}
	parsedID, err := uuid.Parse(created.ID)
	if err != nil || parsedID.Version() != 7 {
		t.Fatalf("expected UUIDv7 identity, got %q (%v)", created.ID, err)
	}
	if created.Title != "Production Release" || created.Lifecycle != LifecycleActive || created.CompletedAt != nil {
		t.Fatalf("unexpected initial WorkItem: %+v", created)
	}
	if created.LastRevisionID == "" {
		t.Fatal("expected Revision identity")
	}

	loaded, err := service.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != created.ID || loaded.LastRevisionID != created.LastRevisionID {
		t.Fatalf("unstable readback: created=%+v loaded=%+v", created, loaded)
	}
	listed, err := service.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("unexpected service list: %+v", listed)
	}

	var entityType string
	if err := database.DB.QueryRowContext(ctx, "SELECT entity_type FROM entities WHERE id = ?", created.ID).Scan(&entityType); err != nil {
		t.Fatalf("load Entity: %v", err)
	}
	if entityType != "WORK_ITEM" {
		t.Fatalf("unexpected entity type %q", entityType)
	}
	var origin string
	if err := database.DB.QueryRowContext(ctx, "SELECT origin_type FROM revisions WHERE id = ?", created.LastRevisionID).Scan(&origin); err != nil {
		t.Fatalf("load Revision: %v", err)
	}
	if origin != "USER" {
		t.Fatalf("unexpected Revision origin %q", origin)
	}
}

func TestServiceUpdateLocalWorkItemDetailsCreatesRevisionAndPreservesState(t *testing.T) {
	ctx := context.Background()
	database := testsupport.OpenTestDatabase(t)
	repository := NewRepository(database.DB)
	service := NewService(database.DB, repository)
	createdAt := time.Date(2026, 9, 18, 10, 0, 0, 0, time.UTC)
	completedAt := createdAt.Add(time.Hour)
	originalRevisionID := insertFoundationFixture(t, database.DB, "work-item-service-update", createdAt, false)
	if err := repository.Create(ctx, &WorkItem{
		ID: "work-item-service-update", Title: "Original", Description: stringPointer("Original description"),
		Lifecycle: LifecycleCompleted, CompletedAt: &completedAt,
		LastRevisionID: originalRevisionID, CreatedAt: createdAt, UpdatedAt: createdAt,
	}); err != nil {
		t.Fatal(err)
	}

	description := "  Updated description  "
	updated, err := service.UpdateLocalWorkItemDetails(ctx, UpdateLocalWorkItemDetailsInput{
		ID: "work-item-service-update", Title: "  Updated title  ", Description: &description,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ID != "work-item-service-update" || updated.Title != "Updated title" || updated.Description == nil || *updated.Description != "Updated description" {
		t.Fatalf("unexpected updated identity/details: %+v", updated)
	}
	if updated.Lifecycle != LifecycleCompleted || updated.CompletedAt == nil || !updated.CompletedAt.Equal(completedAt) || !updated.CreatedAt.Equal(createdAt) {
		t.Fatalf("update changed non-editable state: %+v", updated)
	}
	if updated.LastRevisionID == originalRevisionID || !updated.UpdatedAt.After(createdAt) {
		t.Fatalf("update did not advance mutation bookkeeping: %+v", updated)
	}

	var origin, summary string
	if err := database.DB.QueryRowContext(
		ctx,
		"SELECT origin_type, summary FROM revisions WHERE id = ?",
		updated.LastRevisionID,
	).Scan(&origin, &summary); err != nil {
		t.Fatal(err)
	}
	if origin != "USER" || summary != "Update local work item" {
		t.Fatalf("unexpected update Revision: origin=%q summary=%q", origin, summary)
	}
	loaded, err := service.GetByID(ctx, updated.ID)
	if err != nil || loaded.Title != updated.Title || loaded.LastRevisionID != updated.LastRevisionID {
		t.Fatalf("GetByID did not return authoritative update: item=%+v err=%v", loaded, err)
	}
	listed, err := service.List(ctx)
	if err != nil || len(listed) != 1 || listed[0].Title != updated.Title {
		t.Fatalf("List did not return authoritative update: items=%+v err=%v", listed, err)
	}
}

func TestServiceCreateAndUpdateNormalizeEmptyDescriptionToNil(t *testing.T) {
	ctx := context.Background()
	database := testsupport.OpenTestDatabase(t)
	service := NewService(database.DB, NewRepository(database.DB))
	empty := "  \t "
	createdEmpty, err := service.CreateLocalWorkItem(ctx, CreateLocalWorkItemInput{
		Title: "No description", Description: &empty,
	})
	if err != nil {
		t.Fatal(err)
	}
	if createdEmpty.Description != nil {
		t.Fatalf("create should normalize empty description to nil: %+v", createdEmpty)
	}

	value := "Has a description"
	createdValue, err := service.CreateLocalWorkItem(ctx, CreateLocalWorkItemInput{
		Title: "Clear description", Description: &value,
	})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := service.UpdateLocalWorkItemDetails(ctx, UpdateLocalWorkItemDetailsInput{
		ID: createdValue.ID, Title: createdValue.Title, Description: &empty,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Description != nil {
		t.Fatalf("update should normalize empty description to nil: %+v", updated)
	}
}

func TestServiceUpdateRejectsBlankTitleUnknownAndArchived(t *testing.T) {
	ctx := context.Background()
	database := testsupport.OpenTestDatabase(t)
	service := NewService(database.DB, NewRepository(database.DB))
	created, err := service.CreateLocalWorkItem(ctx, CreateLocalWorkItemInput{Title: "Original"})
	if err != nil {
		t.Fatal(err)
	}
	revisionsBefore := rowCount(t, database.DB, "revisions")
	if _, err := service.UpdateLocalWorkItemDetails(ctx, UpdateLocalWorkItemDetailsInput{
		ID: created.ID, Title: " \t ",
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected blank-title ErrInvalidInput, got %v", err)
	}
	if _, err := service.UpdateLocalWorkItemDetails(ctx, UpdateLocalWorkItemDetailsInput{
		ID: "missing", Title: "Valid",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected unknown WorkItem ErrNotFound, got %v", err)
	}
	if _, err := database.DB.ExecContext(
		ctx,
		"UPDATE entities SET archived_at = ? WHERE id = ?",
		time.Now().UTC().Format(time.RFC3339Nano),
		created.ID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateLocalWorkItemDetails(ctx, UpdateLocalWorkItemDetailsInput{
		ID: created.ID, Title: "Archived update",
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected archived WorkItem ErrNotFound, got %v", err)
	}
	if rowCount(t, database.DB, "revisions") != revisionsBefore {
		t.Fatal("rejected updates created Revisions")
	}
}

func TestServiceUpdateRollsBackRevisionAndMutationOnPersistenceFailure(t *testing.T) {
	ctx := context.Background()
	database := testsupport.OpenTestDatabase(t)
	service := NewService(database.DB, NewRepository(database.DB))
	created, err := service.CreateLocalWorkItem(ctx, CreateLocalWorkItemInput{Title: "Original"})
	if err != nil {
		t.Fatal(err)
	}
	revisionsBefore := rowCount(t, database.DB, "revisions")
	if _, err := database.DB.ExecContext(ctx, `
		CREATE TRIGGER fail_work_item_update
		BEFORE UPDATE ON work_items
		BEGIN
			SELECT RAISE(ABORT, 'forced work item update failure');
		END;
	`); err != nil {
		t.Fatal(err)
	}
	if _, err := service.UpdateLocalWorkItemDetails(ctx, UpdateLocalWorkItemDetailsInput{
		ID: created.ID, Title: "Must roll back",
	}); err == nil {
		t.Fatal("expected forced update failure")
	}
	if rowCount(t, database.DB, "revisions") != revisionsBefore {
		t.Fatal("failed update left a Revision behind")
	}
	loaded, err := service.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Title != created.Title || loaded.LastRevisionID != created.LastRevisionID || !loaded.UpdatedAt.Equal(created.UpdatedAt) {
		t.Fatalf("failed update changed WorkItem: before=%+v after=%+v", created, loaded)
	}
}

func TestServiceLifecycleAllowsV1Transitions(t *testing.T) {
	allowed := []struct {
		name string
		from LifecycleState
		to   LifecycleState
	}{
		{name: "active to paused", from: LifecycleActive, to: LifecyclePaused},
		{name: "paused to active", from: LifecyclePaused, to: LifecycleActive},
		{name: "active to completed", from: LifecycleActive, to: LifecycleCompleted},
		{name: "paused to completed", from: LifecyclePaused, to: LifecycleCompleted},
		{name: "active to cancelled", from: LifecycleActive, to: LifecycleCancelled},
		{name: "paused to cancelled", from: LifecyclePaused, to: LifecycleCancelled},
		{name: "completed to active", from: LifecycleCompleted, to: LifecycleActive},
		{name: "cancelled to active", from: LifecycleCancelled, to: LifecycleActive},
	}

	for index, test := range allowed {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			database := testsupport.OpenTestDatabase(t)
			repository := NewRepository(database.DB)
			service := NewService(database.DB, repository)
			createdAt := time.Date(2020, 9, 18, 10, index, 0, 0, time.UTC)
			original := createLifecycleFixture(t, repository, database.DB, "lifecycle-"+test.name, test.from, createdAt, false)
			revisionsBefore := rowCount(t, database.DB, "revisions")

			updated, err := service.SetLocalWorkItemLifecycleState(ctx, SetLocalWorkItemLifecycleStateInput{
				ID: original.ID, Target: test.to,
			})
			if err != nil {
				t.Fatal(err)
			}
			if updated.Lifecycle != test.to {
				t.Fatalf("unexpected target lifecycle: %+v", updated)
			}
			if test.to == LifecycleCompleted {
				if updated.CompletedAt == nil || !updated.CompletedAt.Equal(updated.UpdatedAt) {
					t.Fatalf("completion timestamp must equal transition timestamp: %+v", updated)
				}
			} else if updated.CompletedAt != nil {
				t.Fatalf("non-completed lifecycle retained completedAt: %+v", updated)
			}
			if updated.LastRevisionID == original.LastRevisionID || !updated.UpdatedAt.After(original.UpdatedAt) {
				t.Fatalf("transition did not advance mutation bookkeeping: before=%+v after=%+v", original, updated)
			}
			if updated.ID != original.ID || updated.Title != original.Title || !equalOptionalString(updated.Description, original.Description) || !updated.CreatedAt.Equal(original.CreatedAt) {
				t.Fatalf("transition changed preserved fields: before=%+v after=%+v", original, updated)
			}
			if rowCount(t, database.DB, "revisions") != revisionsBefore+1 {
				t.Fatal("real transition did not create exactly one Revision")
			}
			var origin, summary string
			if err := database.DB.QueryRowContext(ctx, "SELECT origin_type, summary FROM revisions WHERE id = ?", updated.LastRevisionID).Scan(&origin, &summary); err != nil {
				t.Fatal(err)
			}
			wantSummary := "Change local work item lifecycle to " + string(test.to)
			if origin != "USER" || summary != wantSummary {
				t.Fatalf("unexpected lifecycle Revision: origin=%q summary=%q", origin, summary)
			}
			loaded, err := service.GetByID(ctx, original.ID)
			if err != nil || loaded.Lifecycle != updated.Lifecycle || loaded.LastRevisionID != updated.LastRevisionID {
				t.Fatalf("authoritative lifecycle readback failed: item=%+v err=%v", loaded, err)
			}
		})
	}
}

func TestServiceLifecycleSameStateIsNoOp(t *testing.T) {
	ctx := context.Background()
	database := testsupport.OpenTestDatabase(t)
	repository := NewRepository(database.DB)
	service := NewService(database.DB, repository)
	original := createLifecycleFixture(
		t,
		repository,
		database.DB,
		"lifecycle-no-op",
		LifecycleCompleted,
		time.Date(2020, 9, 18, 10, 0, 0, 0, time.UTC),
		false,
	)
	revisionsBefore := rowCount(t, database.DB, "revisions")

	result, err := service.SetLocalWorkItemLifecycleState(ctx, SetLocalWorkItemLifecycleStateInput{
		ID: original.ID, Target: LifecycleCompleted,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.LastRevisionID != original.LastRevisionID || !result.UpdatedAt.Equal(original.UpdatedAt) || result.CompletedAt == nil || !result.CompletedAt.Equal(*original.CompletedAt) {
		t.Fatalf("same-state request was not an exact no-op: before=%+v after=%+v", original, result)
	}
	if rowCount(t, database.DB, "revisions") != revisionsBefore {
		t.Fatal("same-state request created a Revision")
	}
}

func TestServiceLifecycleRejectsInvalidUnknownAndArchived(t *testing.T) {
	ctx := context.Background()
	database := testsupport.OpenTestDatabase(t)
	repository := NewRepository(database.DB)
	service := NewService(database.DB, repository)
	original := createLifecycleFixture(
		t,
		repository,
		database.DB,
		"lifecycle-invalid",
		LifecycleCompleted,
		time.Date(2020, 9, 18, 10, 0, 0, 0, time.UTC),
		false,
	)
	revisionsBefore := rowCount(t, database.DB, "revisions")

	if _, err := service.SetLocalWorkItemLifecycleState(ctx, SetLocalWorkItemLifecycleStateInput{
		ID: original.ID, Target: LifecyclePaused,
	}); !errors.Is(err, ErrInvalidLifecycleTransition) {
		t.Fatalf("expected invalid transition error, got %v", err)
	}
	if _, err := service.SetLocalWorkItemLifecycleState(ctx, SetLocalWorkItemLifecycleStateInput{
		ID: original.ID, Target: LifecycleState("UNKNOWN"),
	}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected invalid target input error, got %v", err)
	}
	if _, err := service.SetLocalWorkItemLifecycleState(ctx, SetLocalWorkItemLifecycleStateInput{
		ID: "missing", Target: LifecycleActive,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected unknown WorkItem error, got %v", err)
	}
	if _, err := database.DB.ExecContext(ctx, "UPDATE entities SET archived_at = ? WHERE id = ?", time.Now().UTC().Format(time.RFC3339Nano), original.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.SetLocalWorkItemLifecycleState(ctx, SetLocalWorkItemLifecycleStateInput{
		ID: original.ID, Target: LifecycleActive,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected archived WorkItem error, got %v", err)
	}
	if rowCount(t, database.DB, "revisions") != revisionsBefore {
		t.Fatal("rejected lifecycle requests created Revisions")
	}
}

func TestServiceLifecycleRollsBackRevisionAndStateOnPersistenceFailure(t *testing.T) {
	ctx := context.Background()
	database := testsupport.OpenTestDatabase(t)
	repository := NewRepository(database.DB)
	service := NewService(database.DB, repository)
	original := createLifecycleFixture(
		t,
		repository,
		database.DB,
		"lifecycle-rollback",
		LifecycleActive,
		time.Date(2020, 9, 18, 10, 0, 0, 0, time.UTC),
		false,
	)
	revisionsBefore := rowCount(t, database.DB, "revisions")
	if _, err := database.DB.ExecContext(ctx, `
		CREATE TRIGGER fail_work_item_lifecycle_update
		BEFORE UPDATE ON work_items
		BEGIN
			SELECT RAISE(ABORT, 'forced lifecycle update failure');
		END;
	`); err != nil {
		t.Fatal(err)
	}

	if _, err := service.SetLocalWorkItemLifecycleState(ctx, SetLocalWorkItemLifecycleStateInput{
		ID: original.ID, Target: LifecycleCompleted,
	}); err == nil {
		t.Fatal("expected forced lifecycle persistence failure")
	}
	if rowCount(t, database.DB, "revisions") != revisionsBefore {
		t.Fatal("failed lifecycle transition left a Revision behind")
	}
	loaded, err := service.GetByID(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Lifecycle != original.Lifecycle || loaded.CompletedAt != nil || loaded.LastRevisionID != original.LastRevisionID || !loaded.UpdatedAt.Equal(original.UpdatedAt) {
		t.Fatalf("failed lifecycle transition changed WorkItem: before=%+v after=%+v", original, loaded)
	}
}

func createLifecycleFixture(
	t *testing.T,
	repository *Repository,
	db *sql.DB,
	id string,
	lifecycle LifecycleState,
	createdAt time.Time,
	archived bool,
) *WorkItem {
	t.Helper()
	revisionID := insertFoundationFixture(t, db, id, createdAt, archived)
	description := "Preserved description"
	var completedAt *time.Time
	if lifecycle == LifecycleCompleted {
		value := createdAt.Add(time.Hour)
		completedAt = &value
	}
	item := &WorkItem{
		ID: id, Title: "Preserved title", Description: &description,
		Lifecycle: lifecycle, CompletedAt: completedAt, LastRevisionID: revisionID,
		CreatedAt: createdAt, UpdatedAt: createdAt.Add(2 * time.Hour),
	}
	if err := repository.Create(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	return item
}

func TestServiceRejectsBlankTitleWithoutPersistence(t *testing.T) {
	database := testsupport.OpenTestDatabase(t)
	service := NewService(database.DB, NewRepository(database.DB))
	beforeEntities := rowCount(t, database.DB, "entities")
	beforeRevisions := rowCount(t, database.DB, "revisions")
	beforeWorkItems := rowCount(t, database.DB, "work_items")

	_, err := service.CreateLocalWorkItem(context.Background(), CreateLocalWorkItemInput{Title: " \t "})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
	if rowCount(t, database.DB, "entities") != beforeEntities ||
		rowCount(t, database.DB, "revisions") != beforeRevisions ||
		rowCount(t, database.DB, "work_items") != beforeWorkItems {
		t.Fatal("invalid input created persistence state")
	}
}

func TestServiceCreateLocalWorkItemRollsBackOnWorkItemFailure(t *testing.T) {
	ctx := context.Background()
	database := testsupport.OpenTestDatabase(t)
	beforeEntities := rowCount(t, database.DB, "entities")
	beforeRevisions := rowCount(t, database.DB, "revisions")
	beforeWorkItems := rowCount(t, database.DB, "work_items")
	if _, err := database.DB.ExecContext(ctx, `
		CREATE TRIGGER fail_work_item_insert
		BEFORE INSERT ON work_items
		BEGIN
			SELECT RAISE(ABORT, 'forced work item insert failure');
		END;
	`); err != nil {
		t.Fatal(err)
	}

	service := NewService(database.DB, NewRepository(database.DB))
	if _, err := service.CreateLocalWorkItem(ctx, CreateLocalWorkItemInput{Title: "Must roll back"}); err == nil {
		t.Fatal("expected forced persistence failure")
	}
	if rowCount(t, database.DB, "entities") != beforeEntities ||
		rowCount(t, database.DB, "revisions") != beforeRevisions ||
		rowCount(t, database.DB, "work_items") != beforeWorkItems {
		t.Fatal("failed WorkItem create left partial transaction state")
	}
}

func rowCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var count int
	if err := db.QueryRow("SELECT count(*) FROM " + table).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return count
}
