package planner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"lum-harbor-notifier/internal/temporal"
	"lum-harbor-notifier/internal/testsupport"
	"testing"
	"time"
)

func TestServiceCreateLocalTodo(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)

	service := NewService(db.DB, NewRepository(db.DB))

	details := "Review backend changes before production deployment"

	scheduledAt := time.Date(2026, 9, 14, 14, 0, 0, 0, time.UTC)
	dueAt := time.Date(2026, 9, 14, 18, 0, 0, 0, time.UTC)

	input := CreateLocalTodoInput{
		Title:       " Review deployment PR ",
		Details:     &details,
		ScheduledAt: &scheduledAt,
		DueAt:       &dueAt,
	}

	created, err := service.CreateLocalTodo(ctx, input)
	if err != nil {
		t.Fatalf("CreateLocalTodo failed: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected generated Planner item ID")
	}
	if created.LastRevisionID == "" {
		t.Fatalf("expected generated Revision ID")
	}
	if created.Authority != AuthorityLocal {
		t.Fatalf("expected authority %q, but got %q", AuthorityLocal, created.Authority)
	}
	if created.Type != ItemTypeTodo {
		t.Fatalf("expected status %q, got %q", StatusOpen, created.Status)
	}
	if created.ContextStatus != ContextUnknown {
		t.Fatalf("expected context status %q, but got %q", ContextUnknown, created.ContextStatus)
	}
	if created.Title != "Review deployment PR" {
		t.Fatalf("unexpected normalized title: %q", created.Title)
	}

	// Read the Planner Item back through Repository instead of trusting
	// only the object returned by Service. This proves that the transaction
	// actually persisted the Planner row.
	repository := NewRepository(db.DB)

	loaded, err := repository.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetById after CreateLocalTodo failed: %v", err)
	}
	if loaded.ID != created.ID {
		t.Fatalf("expected persisted ID %q, got %q", created.ID, loaded.ID)
	}
	if loaded.LastRevisionID != created.LastRevisionID {
		t.Fatalf("expected revision ID %q, got %q", created.LastRevisionID, loaded.LastRevisionID)
	}
	if loaded.Title != created.Title {
		t.Fatalf("expected persisted title %q, got %q", created.Title, loaded.Title)
	}
	if loaded.ScheduledAt == nil || !loaded.ScheduledAt.Equal(scheduledAt) {
		t.Fatalf("unexpected scheduled time: %v", loaded.ScheduledAt)
	}
	if loaded.DueAt == nil || !loaded.DueAt.Equal(dueAt) {
		t.Fatalf("unexpected due time: %v", loaded.DueAt)
	}

	// Verify that Service also created the shared Entity record.
	//
	// We use direct SQL here because this test is checking transaction
	// orchestration, not Foundation Repository behavior.
	var entityType string
	err = db.DB.QueryRowContext(
		ctx,
		"SELECT entity_type from entities WHERE id = ?",
		created.ID,
	).Scan(&entityType)

	if err != nil {
		t.Fatalf("expected entity record: %v", err)
	}
	if entityType != "PLANNER_ITEM" {
		t.Fatalf("expected entity type PLANNER_ITEM, but got %q", entityType)
	}

	// Verify that the Planner Item references a real USER Revision
	// created by the same Service mutation.
	var revisionOrigin string

	err = db.DB.QueryRowContext(
		ctx,
		"SELECT origin_type FROM revisions WHERE id = ?",
		created.LastRevisionID,
	).Scan(&revisionOrigin)

	if err != nil {
		t.Fatalf("expected Revision record: %v", err)
	}
	if revisionOrigin != "USER" {
		t.Fatalf("expected USER revision, got %q", revisionOrigin)
	}
}

func TestServiceCreateLocalTodoDateOnly(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)

	service := NewService(db.DB, NewRepository(db.DB))

	scheduledDate, err := temporal.ParseLocalDate("2026-09-18")
	if err != nil {
		t.Fatalf("parse scheduled date: %v", err)
	}
	dueDate, err := temporal.ParseLocalDate("2026-09-19")
	if err != nil {
		t.Fatalf("parse due date: %v", err)
	}

	input := CreateLocalTodoInput{
		Title:         "Pay property tax",
		ScheduledDate: &scheduledDate,
		DueDate:       &dueDate,
	}

	created, err := service.CreateLocalTodo(ctx, input)
	if err != nil {
		t.Fatalf("CreateLocalTodo failed: %v", err)
	}
	if created.ScheduledAt != nil {
		t.Fatalf("expected ScheduledAt nil, got %v", created.ScheduledAt)
	}
	if created.DueAt != nil {
		t.Fatalf("expected DueAt nil, got %v", created.DueAt)
	}
	if created.ScheduledDate == nil {
		t.Fatal("expected ScheduledDate, got nil")
	}
	if *created.ScheduledDate != scheduledDate {
		t.Fatalf("expected ScheduledDate %q, got %q", scheduledDate, *created.ScheduledDate)
	}
	if created.DueDate == nil {
		t.Fatal("expected DueDate, got nil")
	}
	if *created.DueDate != dueDate {
		t.Fatalf("expected DueDate %q, got %q", dueDate, *created.DueDate)
	}

	// Read through Repository to prove the date-only values were actually
	// persisted and did not get converted into artificial timestamps.
	repository := NewRepository(db.DB)

	loaded, err := repository.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID after CreateLocalTodo failed: %v", err)
	}
	if loaded.ScheduledAt != nil {
		t.Fatalf("expected persisted ScheduledAt nil, got %v", loaded.ScheduledAt)
	}
	if loaded.DueAt != nil {
		t.Fatalf("expected persisted DueAt nil, got %v", loaded.DueAt)
	}
	if loaded.ScheduledDate == nil || *loaded.ScheduledDate != scheduledDate {
		t.Fatalf("unexpected persisted ScheduledDate: %v", loaded.ScheduledDate)
	}
	if loaded.DueDate == nil || *loaded.DueDate != dueDate {
		t.Fatalf("unexpected persisted DueDate: %v", loaded.DueDate)
	}
}

// TestServiceCreateLocalTodoRollsBack verifies that CreateLocalTodo is
// atomic.
//
// The test intentionally forces planner_items INSERT to fail after the
// Revision and Entity have already been created inside the transaction.
//
// The expected behavior is:
//
//	Revision INSERT     succeeds
//	Entity INSERT       succeeds
//	PlannerItem INSERT  fails
//	       ↓
//	    ROLLBACK
//
// No partial Foundation or Planner records may remain.
func TestServiceCreateLocalTodoRollback(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)

	beforeEntities := countRows(t, db.DB, "entities")
	beforeRevision := countRows(t, db.DB, "revisions")
	beforePlannerItems := countRows(t, db.DB, "planner_items")

	_, err := db.DB.ExecContext(ctx, `
		CREATE TRIGGER fail_planner_item_insert
		BEFORE INSERT ON planner_items
		BEGIN
			SELECT RAISE(ABORT, 'forced planner insert failure');
		END;
	`)
	if err != nil {
		t.Fatalf("failed to create test trigger: %v", err)
	}
	service := NewService(db.DB, NewRepository(db.DB))

	_, err = service.CreateLocalTodo(
		ctx,
		CreateLocalTodoInput{
			Title: "This creation must fail",
		},
	)

	if err == nil {
		t.Fatal("expected CreateLocalTodo to fail")
	}

	// The Planner failure must roll back the entire logical mutation,
	// including Foundation records created earlier in the transaction.
	afterEntities := countRows(t, db.DB, "entities")
	afterRevisions := countRows(t, db.DB, "revisions")
	afterPlannerItems := countRows(t, db.DB, "planner_items")

	if afterEntities != beforeEntities {
		t.Fatalf("expected entities count %d after rollback, got %d", beforeEntities, afterEntities)
	}
	if afterRevisions != beforeRevision {
		t.Fatalf("expected revisions count %d after rollback, got %d", beforeRevision, afterRevisions)
	}
	if afterPlannerItems != beforePlannerItems {
		t.Fatalf("expected planner_items count %d after rollback, got %d", beforePlannerItems, afterPlannerItems)
	}
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()

	query := fmt.Sprintf("SELECT count(*) FROM %s", table)

	var count int
	if err := db.QueryRow(query).Scan(&count); err != nil {
		t.Fatalf("failed to count rows in %s: %v", table, err)
	}

	return count
}

// TestServiceListByRangeRejectsInvalidExactRange verifies that Planner rejects
// an empty exact-time window before executing the Calendar range query.
func TestServiceListByRangeRejectsInvalidExactRange(t *testing.T) {
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))

	start := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	startDate, err := temporal.ParseLocalDate("2026-09-14")
	if err != nil {
		t.Fatalf("parse range start date: %v", err)
	}
	endDate, err := temporal.ParseLocalDate("2026-09-15")
	if err != nil {
		t.Fatalf("parse range end date: %v", err)
	}

	_, err = service.ListByRange(
		context.Background(),
		start,
		start,
		startDate,
		endDate,
	)

	if !errors.Is(err, ErrInvalidTimeRange) {
		t.Fatalf("expected ErrInvalidTimeRange, got %v", err)
	}
}

// TestServiceListByRange verifies that an exact-time PlannerItem created
// through the Service can be read through the current Calendar range use case.
func TestServiceListByRange(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))

	start := time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	startDate, err := temporal.ParseLocalDate("2026-09-14")
	if err != nil {
		t.Fatalf("parse range start date: %v", err)
	}
	endDate, err := temporal.ParseLocalDate("2026-09-15")
	if err != nil {
		t.Fatalf("parse range end date: %v", err)
	}

	scheduledAt := start.Add(10 * time.Hour)

	created, err := service.CreateLocalTodo(
		ctx,
		CreateLocalTodoInput{
			Title:       "Planner range test",
			ScheduledAt: &scheduledAt,
		},
	)
	if err != nil {
		t.Fatalf("CreateLocalTodo failed: %v", err)
	}

	items, err := service.ListByRange(
		ctx,
		start,
		end,
		startDate,
		endDate,
	)

	if err != nil {
		t.Fatalf("ListByRange failed: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 planner item, got %d", len(items))
	}
	if items[0].ID != created.ID {
		t.Fatalf("expected item %q, got %q", created.ID, items[0].ID)
	}
}

// TestServiceCompleteLocalTodo verifies the normal LOCAL Todo completion flow.
//
// The completion must update completed_at and last_revision_id and persist
// the new state to SQLite.
func TestServiceCompleteLocalTodo(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))

	created, err := service.CreateLocalTodo(
		ctx,
		CreateLocalTodoInput{
			Title: "Complete service test",
		},
	)
	if err != nil {
		t.Fatalf("CreateLocalTodo failed: %v", err)
	}

	originalRevisionID := created.LastRevisionID

	completed, err := service.CompleteLocalTodo(
		ctx,
		created.ID,
	)
	if err != nil {
		t.Fatalf("CompleteLocalTodo failed: %v", err)
	}
	if completed.Status != StatusCompleted {
		t.Fatalf("expected status %q, got %q", StatusCompleted, completed.Status)
	}
	if completed.CompletedAt == nil {
		t.Fatal("expected completed_at to be set")
	}
	if completed.LastRevisionID == originalRevisionID {
		t.Fatalf("expected completion to create a new revision, still got %q", originalRevisionID)
	}

	// Reload through Repository so the test verifies committed database state
	// rather than only trusting the object returned by Service.
	repository := NewRepository(db.DB)

	loaded, err := repository.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID after completion failed: %v", err)
	}
	if loaded.Status != StatusCompleted {
		t.Fatalf("expected persisted status %q, got %q", StatusCompleted, loaded.Status)
	}
	if loaded.CompletedAt == nil {
		t.Fatal("expected persisted completed_at to be set")
	}
	if loaded.LastRevisionID != completed.LastRevisionID {
		t.Fatalf("expected persisted revision %q, got %q", completed.LastRevisionID, loaded.LastRevisionID)
	}
}

// TestServiceCompleteLocalTodoIsIdempotent verifies that completing an
// already-completed LOCAL Todo does not create another logical mutation.
//
// No meaningless Revision should be created.
func TestServiceCompleteLocalTodoIsIdempotent(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))

	created, err := service.CreateLocalTodo(
		ctx,
		CreateLocalTodoInput{
			Title: "Idempotent completion test",
		},
	)
	if err != nil {
		t.Fatalf("CreateLocalTodo failed: %v", err)
	}

	firstCompletion, err := service.CompleteLocalTodo(
		ctx,
		created.ID,
	)
	if err != nil {
		t.Fatalf("first CompleteLocalTodo failed: %v", err)
	}

	revisionsAfterFirstCompletion := countRows(t, db.DB, "revisions")

	secondCompletion, err := service.CompleteLocalTodo(ctx, created.ID)
	if err != nil {
		t.Fatalf("second CompleteLocalTodo failed: %v", err)
	}

	revisionsAfterSecondCompletion := countRows(t, db.DB, "revisions")

	if secondCompletion.LastRevisionID != firstCompletion.LastRevisionID {
		t.Fatalf(
			"expected idempotent completion to keep revision %q, got %q",
			firstCompletion.LastRevisionID,
			secondCompletion.LastRevisionID,
		)
	}

	if revisionsAfterSecondCompletion != revisionsAfterFirstCompletion {
		t.Fatalf(
			"expected revision count to remain %d, got %d",
			revisionsAfterFirstCompletion,
			revisionsAfterSecondCompletion,
		)
	}

	if firstCompletion.CompletedAt == nil ||
		secondCompletion.CompletedAt == nil {
		t.Fatal("expected completed_at to be set")
	}

	if !secondCompletion.CompletedAt.Equal(
		*firstCompletion.CompletedAt,
	) {
		t.Fatalf(
			"expected completed_at to remain %v, got %v",
			firstCompletion.CompletedAt,
			secondCompletion.CompletedAt,
		)
	}
}

// TestServiceCompleteLocalTodoNotFound verifies that Service preserves
// Planner's not-found semantics when the requested item does not exist.
//
// No Revision should be created for a mutation that never had a valid
// target.
func TestServiceCompleteLocalTodoNotFound(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))

	beforeRevisions := countRows(t, db.DB, "revisions")

	_, err := service.CompleteLocalTodo(ctx, "does-not-exist")

	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	afterRevisions := countRows(t, db.DB, "revisions")

	if afterRevisions != beforeRevisions {
		t.Fatalf("expected no new revisions, before=%d after=%d", beforeRevisions, afterRevisions)
	}
}

// TestServiceCompleteLocalTodoRejectsExternalAuthority verifies that
// native state owned by an external source cannot be mutated through
// the LOCAL completion use case.
func TestServiceCompleteLocalTodoRejectsExternalAuthority(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)

	insertPlannerFixture(
		t,
		db.DB,
		"external-complete-test",
		nil,
		nil,
		false,
	)

	_, err := db.DB.ExecContext(
		ctx,
		`
		UPDATE planner_items
		SET authority_type = 'EXTERNAL'
		WHERE entity_id = ?
		`,
		"external-complete-test",
	)
	if err != nil {
		t.Fatalf("failed to mark fixture externally authoritative: %v", err)
	}

	beforeRevisions := countRows(t, db.DB, "revisions")

	service := NewService(db.DB, NewRepository(db.DB))

	_, err = service.CompleteLocalTodo(
		ctx,
		"external-complete-test",
	)

	if !errors.Is(err, ErrExternalAuthority) {
		t.Fatalf("expected ErrExternalAuthority, got %v", err)
	}

	afterRevisions := countRows(t, db.DB, "revisions")

	if afterRevisions != beforeRevisions {
		t.Fatalf(
			"expected no revision for rejected mutation, before=%d after=%d",
			beforeRevisions,
			afterRevisions,
		)
	}

	repository := NewRepository(db.DB)

	item, err := repository.GetByID(
		ctx,
		"external-complete-test",
	)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if item.Status != StatusOpen {
		t.Fatalf("expected external item to remain %q, got %q", StatusOpen, item.Status)
	}
}

// TestServiceCompleteLocalTodoRollsBack verifies atomicity when the
// Planner UPDATE fails after the new Revision has already been inserted.
func TestServiceCompleteLocalTodoRollsBack(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))

	created, err := service.CreateLocalTodo(
		ctx,
		CreateLocalTodoInput{
			Title: "Completion rollback test",
		},
	)
	if err != nil {
		t.Fatalf("CreateLocalTodo failed: %v", err)
	}

	originalRevisionID := created.LastRevisionID
	beforeRevisions := countRows(t, db.DB, "revisions")

	// Fail only the transition to COMPLETED.
	//
	// The Service should have enough time to create the new Revision
	// inside the transaction before this trigger aborts the Planner UPDATE.
	_, err = db.DB.ExecContext(
		ctx,
		`
		CREATE TRIGGER fail_complete_planner_item
		BEFORE UPDATE OF status ON planner_items
		WHEN NEW.status = 'COMPLETED'
		BEGIN
			SELECT RAISE(
				ABORT,
				'forced planner completion failure'
			);
		END;
		`,
	)
	if err != nil {
		t.Fatalf(
			"failed to create completion failure trigger: %v",
			err,
		)
	}

	_, err = service.CompleteLocalTodo(ctx, created.ID)
	if err == nil {
		t.Fatal("expected CompleteLocalTodo to fail")
	}

	afterRevisions := countRows(t, db.DB, "revisions")

	if afterRevisions != beforeRevisions {
		t.Fatalf(
			"expected failed completion revision to roll back, before=%d after=%d",
			beforeRevisions,
			afterRevisions,
		)
	}

	repository := NewRepository(db.DB)

	loaded, err := repository.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetByID after rollback failed: %v", err)
	}
	if loaded.Status != StatusOpen {
		t.Fatalf("expected Planner Item to remain %q after rollback, got %q", StatusOpen, loaded.Status)
	}
	if loaded.CompletedAt != nil {
		t.Fatalf("expected completed_at to remain nil, got %v", loaded.CompletedAt)
	}
	if loaded.LastRevisionID != originalRevisionID {
		t.Fatalf("expected original revision %q after rollback, got %q", originalRevisionID, loaded.LastRevisionID)
	}
}

func TestServiceListCurrentOpen(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))

	location, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatalf("load timezone: %v", err)
	}
	today, err := temporal.ParseLocalDate("2026-09-15")
	if err != nil {
		t.Fatalf("parse today: %v", err)
	}

	// ------------------------------------------------------------
	// 1. Past exact-time item -> included
	// ------------------------------------------------------------
	pastTime := time.Date(2026, 9, 14, 15, 0, 0, 0, location)
	pastExact, err := service.CreateLocalTodo(
		ctx,
		CreateLocalTodoInput{
			Title: "Past exact item",
			DueAt: &pastTime,
		},
	)
	if err != nil {
		t.Fatalf("create past exact item: %v", err)
	}

	// ------------------------------------------------------------
	// 2. Today exact-time item, later in the day -> included
	//
	// This is important: CurrentOpen means "belongs to today",
	// not merely "scheduled before the current clock time".
	// ------------------------------------------------------------
	todayEvening := time.Date(2026, 9, 15, 20, 0, 0, 0, location)
	todayExact, err := service.CreateLocalTodo(
		ctx,
		CreateLocalTodoInput{
			Title: "Today evening item",
			DueAt: &todayEvening,
		},
	)
	if err != nil {
		t.Fatalf("create today exact item: %v", err)
	}

	// ------------------------------------------------------------
	// 3. Past date-only item -> included
	// ------------------------------------------------------------
	pastDate, err := temporal.ParseLocalDate("2026-09-14")
	if err != nil {
		t.Fatalf("parse past date: %v", err)
	}

	pastDateOnly, err := service.CreateLocalTodo(
		ctx,
		CreateLocalTodoInput{
			Title:   "Past date-only item",
			DueDate: &pastDate,
		},
	)
	if err != nil {
		t.Fatalf("create past date-only item: %v", err)
	}

	// ------------------------------------------------------------
	// 4. No-date item -> included
	// ------------------------------------------------------------
	noDate, err := service.CreateLocalTodo(
		ctx,
		CreateLocalTodoInput{
			Title: "No-date item",
		},
	)
	if err != nil {
		t.Fatalf("create no-date item: %v", err)
	}

	// ------------------------------------------------------------
	// 5. Future date-only item -> excluded
	// ------------------------------------------------------------
	futureDate, err := temporal.ParseLocalDate("2026-09-16")
	if err != nil {
		t.Fatalf("parse future date: %v", err)
	}

	future, err := service.CreateLocalTodo(
		ctx,
		CreateLocalTodoInput{
			Title:   "Future item",
			DueDate: &futureDate,
		},
	)
	if err != nil {
		t.Fatalf("create future item: %v", err)
	}

	// ------------------------------------------------------------
	// 6. Current item that has already been completed -> excluded
	// ------------------------------------------------------------
	completedCandidate, err := service.CreateLocalTodo(
		ctx,
		CreateLocalTodoInput{
			Title: "Completed item",
		},
	)
	if err != nil {
		t.Fatalf("create completion candidate: %v", err)
	}

	if _, err := service.CompleteLocalTodo(ctx, completedCandidate.ID); err != nil {
		t.Fatalf("complete item: %v", err)
	}

	// ------------------------------------------------------------
	// Query Current Open
	// ------------------------------------------------------------
	items, err := service.ListCurrentOpen(ctx, today, location)
	if err != nil {
		t.Fatalf("ListCurrentOpen failed: %v", err)
	}

	// Do not depend on SQL ordering here. This test is about membership.
	returned := make(map[string]bool, len(items))
	for _, item := range items {
		returned[item.ID] = true
	}

	expected := []string{pastExact.ID, todayExact.ID, pastDateOnly.ID, noDate.ID}

	for _, id := range expected {
		if !returned[id] {
			t.Fatalf("expected current-open item %q to be returned", id)
		}
	}
	if returned[future.ID] {
		t.Fatalf("future item %q must not be returned", future.ID)
	}
	if returned[completedCandidate.ID] {
		t.Fatalf("completed item %q must not be returned", completedCandidate.ID)
	}
	if len(items) != len(expected) {
		t.Fatalf("expected %d current-open items, got %d", len(expected), len(items))
	}
}
