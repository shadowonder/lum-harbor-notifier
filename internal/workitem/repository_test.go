package workitem

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"lum-harbor-notifier/internal/database/codec"
	"lum-harbor-notifier/internal/testsupport"
)

func insertFoundationFixture(t *testing.T, db sqlExecutor, id string, createdAt time.Time, archived bool) string {
	t.Helper()
	ctx := context.Background()
	revisionID := "revision-" + id
	var archivedAt *string
	if archived {
		value := codec.FormatTime(createdAt)
		archivedAt = &value
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO revisions (id, origin_type, summary, applied_at)
		VALUES (?, 'USER', 'work item repository fixture', ?)
	`, revisionID, codec.FormatTime(createdAt)); err != nil {
		t.Fatalf("insert revision fixture: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO entities (id, entity_type, created_at, archived_at)
		VALUES (?, 'WORK_ITEM', ?, ?)
	`, id, codec.FormatTime(createdAt), archivedAt); err != nil {
		t.Fatalf("insert entity fixture: %v", err)
	}
	return revisionID
}

type sqlExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func TestRepositoryCreateGetAndOptionalDescriptionRoundTrip(t *testing.T) {
	ctx := context.Background()
	database := testsupport.OpenTestDatabase(t)
	repository := NewRepository(database.DB)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)

	for _, test := range []struct {
		name        string
		id          string
		description *string
	}{
		{name: "nil", id: "work-item-nil-description"},
		{name: "value", id: "work-item-value-description", description: stringPointer("Long-term release outcome")},
	} {
		t.Run(test.name, func(t *testing.T) {
			revisionID := insertFoundationFixture(t, database.DB, test.id, now, false)
			item := &WorkItem{
				ID: test.id, Title: "Production Release", Description: test.description,
				Lifecycle: LifecycleActive, LastRevisionID: revisionID,
				CreatedAt: now, UpdatedAt: now,
			}
			if err := repository.Create(ctx, item); err != nil {
				t.Fatalf("Create: %v", err)
			}
			loaded, err := repository.GetByID(ctx, item.ID)
			if err != nil {
				t.Fatalf("GetByID: %v", err)
			}
			if loaded.ID != item.ID || loaded.Title != item.Title || loaded.Lifecycle != LifecycleActive {
				t.Fatalf("unexpected round-trip: %+v", loaded)
			}
			if !equalOptionalString(loaded.Description, test.description) {
				t.Fatalf("description mismatch: got %v want %v", loaded.Description, test.description)
			}
		})
	}
}

func TestRepositoryMapsCompletedLifecycle(t *testing.T) {
	ctx := context.Background()
	database := testsupport.OpenTestDatabase(t)
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	revisionID := insertFoundationFixture(t, database.DB, "work-item-completed", now, false)
	if _, err := database.DB.ExecContext(ctx, `
		INSERT INTO work_items (
			entity_id, title, lifecycle_state, completed_at,
			last_revision_id, created_at, updated_at
		) VALUES (?, ?, 'COMPLETED', ?, ?, ?, ?)
	`, "work-item-completed", "Completed release", codec.FormatTime(now), revisionID, codec.FormatTime(now), codec.FormatTime(now)); err != nil {
		t.Fatal(err)
	}

	loaded, err := NewRepository(database.DB).GetByID(ctx, "work-item-completed")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Lifecycle != LifecycleCompleted || loaded.CompletedAt == nil || !loaded.CompletedAt.Equal(now) {
		t.Fatalf("unexpected completed mapping: %+v", loaded)
	}
}

func TestRepositoryGetByIDNotFound(t *testing.T) {
	database := testsupport.OpenTestDatabase(t)
	_, err := NewRepository(database.DB).GetByID(context.Background(), "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRepositoryUpdateDetails(t *testing.T) {
	ctx := context.Background()
	database := testsupport.OpenTestDatabase(t)
	repository := NewRepository(database.DB)
	createdAt := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	originalRevisionID := insertFoundationFixture(t, database.DB, "work-item-update", createdAt, false)
	if err := repository.Create(ctx, &WorkItem{
		ID: "work-item-update", Title: "Original", Lifecycle: LifecycleActive,
		LastRevisionID: originalRevisionID, CreatedAt: createdAt, UpdatedAt: createdAt,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.ExecContext(ctx, `
		INSERT INTO revisions (id, origin_type, summary, applied_at)
		VALUES ('revision-work-item-update-details', 'USER', 'update fixture', ?)
	`, codec.FormatTime(updatedAt)); err != nil {
		t.Fatal(err)
	}
	description := "Updated description"
	updated, err := repository.UpdateDetails(
		ctx,
		"work-item-update",
		"Updated title",
		&description,
		"revision-work-item-update-details",
		updatedAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "Updated title" || updated.Description == nil || *updated.Description != description {
		t.Fatalf("unexpected updated details: %+v", updated)
	}
	if updated.LastRevisionID != "revision-work-item-update-details" || !updated.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("unexpected update bookkeeping: %+v", updated)
	}
	if updated.ID != "work-item-update" || updated.Lifecycle != LifecycleActive || !updated.CreatedAt.Equal(createdAt) {
		t.Fatalf("repository changed immutable/basic state: %+v", updated)
	}
}

func TestRepositoryUpdateLifecycle(t *testing.T) {
	ctx := context.Background()
	database := testsupport.OpenTestDatabase(t)
	repository := NewRepository(database.DB)
	createdAt := time.Date(2020, 9, 18, 12, 0, 0, 0, time.UTC)
	updatedAt := createdAt.Add(time.Hour)
	originalRevisionID := insertFoundationFixture(t, database.DB, "work-item-lifecycle", createdAt, false)
	description := "Preserved description"
	if err := repository.Create(ctx, &WorkItem{
		ID: "work-item-lifecycle", Title: "Preserved title", Description: &description,
		Lifecycle: LifecycleActive, LastRevisionID: originalRevisionID,
		CreatedAt: createdAt, UpdatedAt: createdAt,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.DB.ExecContext(ctx, `
		INSERT INTO revisions (id, origin_type, summary, applied_at)
		VALUES ('revision-work-item-lifecycle-update', 'USER', 'lifecycle fixture', ?)
	`, codec.FormatTime(updatedAt)); err != nil {
		t.Fatal(err)
	}

	updated, err := repository.UpdateLifecycle(
		ctx,
		"work-item-lifecycle",
		LifecycleCompleted,
		&updatedAt,
		"revision-work-item-lifecycle-update",
		updatedAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Lifecycle != LifecycleCompleted || updated.CompletedAt == nil || !updated.CompletedAt.Equal(updatedAt) {
		t.Fatalf("unexpected lifecycle state: %+v", updated)
	}
	if updated.LastRevisionID != "revision-work-item-lifecycle-update" || !updated.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("unexpected lifecycle bookkeeping: %+v", updated)
	}
	if updated.ID != "work-item-lifecycle" || updated.Title != "Preserved title" || updated.Description == nil || *updated.Description != description || !updated.CreatedAt.Equal(createdAt) {
		t.Fatalf("lifecycle update changed preserved state: %+v", updated)
	}
}

func TestRepositoryListIsDeterministicAndExcludesArchivedEntities(t *testing.T) {
	ctx := context.Background()
	database := testsupport.OpenTestDatabase(t)
	repository := NewRepository(database.DB)
	early := time.Date(2026, 9, 18, 11, 0, 0, 0, time.UTC)
	late := early.Add(time.Hour)

	for _, fixture := range []struct {
		id       string
		created  time.Time
		archived bool
	}{
		{id: "work-item-b", created: late},
		{id: "work-item-a", created: late},
		{id: "work-item-first", created: early},
		{id: "work-item-archived", created: early, archived: true},
	} {
		revisionID := insertFoundationFixture(t, database.DB, fixture.id, fixture.created, fixture.archived)
		if err := repository.Create(ctx, &WorkItem{
			ID: fixture.id, Title: fixture.id, Lifecycle: LifecycleActive,
			LastRevisionID: revisionID, CreatedAt: fixture.created, UpdatedAt: fixture.created,
		}); err != nil {
			t.Fatal(err)
		}
	}

	items, err := repository.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 || items[0].ID != "work-item-first" || items[1].ID != "work-item-a" || items[2].ID != "work-item-b" {
		t.Fatalf("unexpected deterministic list: %+v", items)
	}
	if _, err := repository.GetByID(ctx, "work-item-archived"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("archived WorkItem should not be current-readable: %v", err)
	}
}

func TestTransactionBoundRepository(t *testing.T) {
	ctx := context.Background()
	database := testsupport.OpenTestDatabase(t)
	tx, err := database.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	revisionID := insertFoundationFixture(t, tx, "work-item-tx", now, false)
	txRepository := NewRepository(tx)
	if err := txRepository.Create(ctx, &WorkItem{
		ID: "work-item-tx", Title: "Transactional", Lifecycle: LifecycleActive,
		LastRevisionID: revisionID, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := txRepository.GetByID(ctx, "work-item-tx"); err != nil {
		t.Fatalf("read through transaction-bound repository: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRepository(database.DB).GetByID(ctx, "work-item-tx"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rolled-back WorkItem should not persist: %v", err)
	}
}

func stringPointer(value string) *string { return &value }

func equalOptionalString(left, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
