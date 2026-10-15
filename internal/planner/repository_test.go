package planner

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"lum-harbor-notifier/internal/database/codec"
	"lum-harbor-notifier/internal/identity"
	"lum-harbor-notifier/internal/temporal"
	"lum-harbor-notifier/internal/testsupport"
)

// TestRepositoryGetByID verifies the full Planner persistence read path:
//
// Direct SQL is used only to prepare test data because Create() has not
// been implemented yet. The behavior under test is Repository.GetByID().
func TestRepositoryGetByID(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)

	// A Planner Item depends on an Entity and a Revision, so create
	// those prerequisite records first.
	_, err := db.DB.ExecContext(ctx, `
		INSERT INTO entities (
			id,
			entity_type,
			created_at
		)
		VALUES (?, ?, ?)
	`,
		"planner-1",
		"PLANNER_ITEM",
		"2026-09-13T20:00:00Z",
	)
	if err != nil {
		t.Fatalf("failed to insert test entity: %v", err)
	}

	_, err = db.DB.ExecContext(ctx, `
		INSERT INTO revisions (
			id,
			revision_no,
			origin_type,
			summary,
			applied_at
		)
		VALUES (?, ?, ?, ?, ?)
	`,
		"revision-1",
		1,
		"USER",
		"create planner test item",
		"2026-09-13T20:00:00Z",
	)
	if err != nil {
		t.Fatalf("failed to insert test revision: %v", err)
	}

	// Insert a row containing several different persistence representations:
	//
	// This gives Repository.GetByID() a meaningful mapping test.
	_, err = db.DB.ExecContext(ctx, `
		INSERT INTO planner_items (
			entity_id,
			authority_type,
			item_type,
			title,
			details,
			status,
			scheduled_at,
			due_at,
			context_status,
			missing_context_json,
			last_revision_id,
			created_at,
			updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		"planner-1",
		"LOCAL",
		"TODO",
		"Review A's PR",
		"Check the backend changes before production release",
		"OPEN",
		"2026-09-14T14:00:00Z",
		"2026-09-14T18:00:00Z",
		"INCOMPLETE",
		`["expectedOutcome"]`,
		"revision-1",
		"2026-09-13T20:00:00Z",
		"2026-09-13T20:00:00Z",
	)
	if err != nil {
		t.Fatalf("failed to insert planner item: %v", err)
	}

	repository := NewRepository(db.DB)

	item, err := repository.GetByID(ctx, "planner-1")
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if item.ID != "planner-1" {
		t.Fatalf("expected ID %q, got %q", "planner-1", item.ID)
	}

	if item.Authority != AuthorityLocal {
		t.Fatalf(
			"expected authority %q, got %q",
			AuthorityLocal,
			item.Authority,
		)
	}

	if item.Type != ItemTypeTodo {
		t.Fatalf(
			"expected item type %q, got %q",
			ItemTypeTodo,
			item.Type,
		)
	}

	if item.Title != "Review A's PR" {
		t.Fatalf("unexpected title: %q", item.Title)
	}

	if len(item.MissingContext) != 1 ||
		item.MissingContext[0] != "expectedOutcome" {
		t.Fatalf(
			"unexpected missing context: %#v",
			item.MissingContext,
		)
	}
}

// TestRepositoryGetByIDNotFound verifies that database/sql's
// sql.ErrNoRows does not leak outside the Repository.
func TestRepositoryGetByIDNotFound(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)

	repository := NewRepository(db.DB)

	_, err := repository.GetByID(ctx, "does-not-exist")
	if err == nil {
		t.Fatal("expected GetByID to return an error")
	}

	if !errors.Is(err, ErrNotFound) {
		t.Fatalf(
			"expected ErrNotFound, got %v",
			err,
		)
	}
}

// TestRepositoryCreateAndGetByID verifies the complete Planner
// Entity and Revision rows are inserted directly as test prerequisites.
// Creating those records becomes Service responsibility in the next phase.
func TestRepositoryCreateAndGetByID(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)

	_, err := db.DB.ExecContext(ctx, `
		INSERT INTO entities (
			id,
			entity_type,
			created_at
		)
		VALUES (?, ?, ?)
	`,
		"planner-create-1",
		"PLANNER_ITEM",
		"2026-09-13T21:00:00Z",
	)
	if err != nil {
		t.Fatalf("failed to create test entity: %v", err)
	}

	_, err = db.DB.ExecContext(ctx, `
		INSERT INTO revisions (
			id,
			revision_no,
			origin_type,
			summary,
			applied_at
		)
		VALUES (?, ?, ?, ?, ?)
	`,
		"revision-create-1",
		1,
		"USER",
		"create test planner item",
		"2026-09-13T21:00:00Z",
	)
	if err != nil {
		t.Fatalf("failed to create test revision: %v", err)
	}

	scheduledAt := time.Date(
		2026, 9, 14,
		9, 0, 0, 0,
		time.UTC,
	)

	dueAt := time.Date(
		2026, 9, 14,
		17, 0, 0, 0,
		time.UTC,
	)

	now := time.Date(
		2026, 9, 13,
		21, 0, 0, 0,
		time.UTC,
	)

	repository := NewRepository(db.DB)

	item := &PlannerItem{
		ID:        "planner-create-1",
		Authority: AuthorityLocal,

		Type:   ItemTypeTodo,
		Title:  "Review A's PR",
		Status: StatusOpen,

		ScheduledAt: &scheduledAt,
		DueAt:       &dueAt,

		ContextStatus:  ContextIncomplete,
		MissingContext: []string{"expectedOutcome"},

		LastRevisionID: "revision-create-1",

		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := repository.Create(ctx, item); err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	loaded, err := repository.GetByID(ctx, item.ID)
	if err != nil {
		t.Fatalf("GetByID failed after Create: %v", err)
	}

	if loaded.ID != item.ID {
		t.Fatalf(
			"expected ID %q, got %q",
			item.ID,
			loaded.ID,
		)
	}

	if loaded.Title != item.Title {
		t.Fatalf(
			"expected title %q, got %q",
			item.Title,
			loaded.Title,
		)
	}

	if loaded.Authority != AuthorityLocal {
		t.Fatalf(
			"expected authority %q, got %q",
			AuthorityLocal,
			loaded.Authority,
		)
	}

	if len(loaded.MissingContext) != 1 ||
		loaded.MissingContext[0] != "expectedOutcome" {
		t.Fatalf(
			"unexpected missing context: %#v",
			loaded.MissingContext,
		)
	}
}

// insertPlannerFixture inserts the minimum Foundation + Planner records
// required by Repository and Service integration tests.
//
// Direct SQL is intentional here: this test is exercising the Repository
// read query, not PlannerService creation behavior.
func insertPlannerFixture(
	t *testing.T,
	db *sql.DB,
	id string,
	scheduledAt *time.Time,
	dueAt *time.Time,
	archived bool,
) {
	t.Helper()

	ctx := context.Background()

	now := time.Date(2026, 9, 13, 20, 0, 0, 0, time.UTC)

	revisionID := "revision-" + id

	var archivedAt *string
	if archived {
		value := codec.FormatTime(now)
		archivedAt = &value
	}

	_, err := db.ExecContext(
		ctx,
		`
		INSERT INTO revisions (
			id,
			origin_type,
			summary,
			applied_at
		)
		VALUES (?, ?, ?, ?)
		`,
		revisionID,
		"USER",
		"planner range test fixture",
		codec.FormatTime(now),
	)
	if err != nil {
		t.Fatalf("insert revision %s: %v", revisionID, err)
	}

	_, err = db.ExecContext(
		ctx,
		`
		INSERT INTO entities (
			id,
			entity_type,
			created_at,
			archived_at
		)
		VALUES (?, ?, ?, ?)
		`,
		id,
		"PLANNER_ITEM",
		codec.FormatTime(now),
		archivedAt,
	)
	if err != nil {
		t.Fatalf("insert entity %s: %v", id, err)
	}

	_, err = db.ExecContext(
		ctx,
		`
		INSERT INTO planner_items (
			entity_id,
			authority_type,
			item_type,
			title,
			status,
			scheduled_at,
			due_at,
			context_status,
			last_revision_id,
			created_at,
			updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`,
		id,
		"LOCAL",
		"TODO",
		id,
		"OPEN",
		codec.FormatOptionalTime(scheduledAt),
		codec.FormatOptionalTime(dueAt),
		"UNKNOWN",
		revisionID,
		codec.FormatTime(now),
		codec.FormatTime(now),
	)
	if err != nil {
		t.Fatalf("insert planner item %s: %v", id, err)
	}
}

func TestRepositoryComplete(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)

	// Prepare the existing OPEN Planner Item using the same fixture
	// strategy as the other Repository integration tests.
	insertPlannerFixture(
		t,
		db.DB,
		"complete-test",
		nil,
		nil,
		false,
	)

	// Complete() requires a valid Revision because last_revision_id has
	// a foreign-key constraint.
	newRevisionID, err := identity.New()
	if err != nil {
		t.Fatalf("failed to generate completion revision ID: %v", err)
	}

	_, err = db.DB.ExecContext(
		ctx,
		`
		INSERT INTO revisions (
			id,
			origin_type,
			summary,
			applied_at
		)
		VALUES (?, ?, ?, ?)
		`,
		newRevisionID,
		"USER",
		"complete planner item",
		codec.FormatTime(time.Now().UTC()),
	)
	if err != nil {
		t.Fatalf("failed to insert completion revision: %v", err)
	}

	repository := NewRepository(db.DB)

	completedAt := time.Date(2026, 9, 14, 21, 30, 0, 0, time.UTC)

	item, err := repository.Complete(ctx, "complete-test", completedAt, newRevisionID)
	if err != nil {
		t.Fatalf("Complete failed: %v", err)
	}

	if item.Status != StatusCompleted {
		t.Fatalf("expected status %q, got %q", StatusCompleted, item.Status)
	}

	if item.CompletedAt == nil ||
		!item.CompletedAt.Equal(completedAt) {
		t.Fatalf("unexpected completed_at: %v", item.CompletedAt)
	}

	if item.LastRevisionID != newRevisionID {
		t.Fatalf("expected revision %q, got %q", newRevisionID, item.LastRevisionID)
	}
}

func TestRepositoryCreateAndGetByIDDateOnly(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)

	now := time.Date(2026, 9, 15, 14, 0, 0, 0, time.UTC)

	entityID := "planner-date-only"
	revisionID := "revision-date-only"

	_, err := db.DB.ExecContext(
		ctx,
		`
		INSERT INTO revisions (
			id,
			origin_type,
			summary,
			applied_at
		)
		VALUES (?, ?, ?, ?)
		`,
		revisionID,
		"USER",
		"create date-only planner item",
		codec.FormatTime(now),
	)
	if err != nil {
		t.Fatalf("insert revision: %v", err)
	}

	_, err = db.DB.ExecContext(
		ctx,
		`
		INSERT INTO entities (
			id,
			entity_type,
			created_at
		)
		VALUES (?, ?, ?)
		`,
		entityID,
		"PLANNER_ITEM",
		codec.FormatTime(now),
	)
	if err != nil {
		t.Fatalf("insert entity: %v", err)
	}
	scheduledDate, err := temporal.ParseLocalDate("2026-09-18")
	if err != nil {
		t.Fatalf("parse scheduled date: %v", err)
	}
	dueDate, err := temporal.ParseLocalDate("2026-09-19")
	if err != nil {
		t.Fatalf("parse due date: %v", err)
	}

	repository := NewRepository(db.DB)

	item := &PlannerItem{
		ID:        entityID,
		Authority: AuthorityLocal,

		Type:   ItemTypeTodo,
		Title:  "Pay property tax",
		Status: StatusOpen,

		ScheduledDate: &scheduledDate,
		DueDate:       &dueDate,

		ContextStatus: ContextUnknown,

		LastRevisionID: revisionID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := repository.Create(ctx, item); err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	loaded, err := repository.GetByID(ctx, entityID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	// Date-only must remain date-only. Repository must never invent
	// midnight timestamps for calendar-date values.
	if loaded.ScheduledAt != nil {
		t.Fatalf("expected ScheduledAt nil, got %v", loaded.ScheduledAt)
	}
	if loaded.DueAt != nil {
		t.Fatalf("expected DueAt nil, got %v", loaded.DueAt)
	}
	if loaded.ScheduledDate == nil {
		t.Fatal("expected ScheduledDate, got nil")
	}
	if *loaded.ScheduledDate != scheduledDate {
		t.Fatalf("expected ScheduledDate %q, got %q", scheduledDate, *loaded.ScheduledDate)
	}
	if loaded.DueDate == nil {
		t.Fatal("expected DueDate, got nil")
	}
	if *loaded.DueDate != dueDate {
		t.Fatalf("expected DueDate %q, got %q", dueDate, *loaded.DueDate)
	}
}

// TestRepositoryListByRange verifies that ListByRange includes todos whose
// scheduled/due timestamp or date falls within the half-open range
// [start, end), and excludes items outside it (no date, or exactly at the
// range end).
func TestRepositoryListByRange(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)

	service := NewService(db.DB, NewRepository(db.DB))
	repository := NewRepository(db.DB)

	location, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	// Visible calendar range:
	//
	// [2026-09-15 00:00, 2026-09-16 00:00)
	rangeStartAt := time.Date(2026, 9, 15, 0, 0, 0, 0, location)

	rangeEndAt := time.Date(2026, 9, 16, 0, 0, 0, 0, location)

	rangeStartDate, err := temporal.ParseLocalDate("2026-09-15")
	if err != nil {
		t.Fatalf("parse range start date: %v", err)
	}

	rangeEndDate, err := temporal.ParseLocalDate("2026-09-16")
	if err != nil {
		t.Fatalf("parse range end date: %v", err)
	}

	// ------------------------------------------------------------
	// 1. scheduled_at exactly at range start -> included
	//
	// This locks the inclusive start boundary of the half-open range.
	// ------------------------------------------------------------

	scheduledAt := rangeStartAt

	scheduledExact, err := service.CreateLocalTodo(
		ctx,
		CreateLocalTodoInput{
			Title:       "Scheduled exact item",
			ScheduledAt: &scheduledAt,
		},
	)
	if err != nil {
		t.Fatalf("create scheduled exact item: %v", err)
	}

	// ------------------------------------------------------------
	// 2. due_at inside range -> included
	// ------------------------------------------------------------

	dueAt := time.Date(2026, 9, 15, 17, 0, 0, 0, location)

	dueExact, err := service.CreateLocalTodo(
		ctx,
		CreateLocalTodoInput{
			Title: "Due exact item",
			DueAt: &dueAt,
		},
	)
	if err != nil {
		t.Fatalf("create due exact item: %v", err)
	}

	// ------------------------------------------------------------
	// 3. scheduled_date inside range -> included
	// ------------------------------------------------------------

	scheduledDate, err := temporal.ParseLocalDate("2026-09-15")
	if err != nil {
		t.Fatalf("parse scheduled date: %v", err)
	}

	scheduledDateOnly, err := service.CreateLocalTodo(
		ctx,
		CreateLocalTodoInput{
			Title:         "Scheduled date-only item",
			ScheduledDate: &scheduledDate,
		},
	)
	if err != nil {
		t.Fatalf("create scheduled date-only item: %v", err)
	}

	// ------------------------------------------------------------
	// 4. due_date inside range -> included
	// ------------------------------------------------------------

	dueDate, err := temporal.ParseLocalDate("2026-09-15")
	if err != nil {
		t.Fatalf("parse due date: %v", err)
	}

	dueDateOnly, err := service.CreateLocalTodo(
		ctx,
		CreateLocalTodoInput{
			Title:   "Due date-only item",
			DueDate: &dueDate,
		},
	)
	if err != nil {
		t.Fatalf("create due date-only item: %v", err)
	}

	// ------------------------------------------------------------
	// 5. no-date item -> excluded
	//
	// It belongs to CurrentOpen, but not to the Calendar projection.
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
	// 6. archived exact item inside the range -> excluded
	// ------------------------------------------------------------

	archivedAt := rangeStartAt.Add(12 * time.Hour)
	insertPlannerFixture(
		t,
		db.DB,
		"archived-range-item",
		&archivedAt,
		nil,
		true,
	)

	// ------------------------------------------------------------
	// 7. exact timestamp exactly at range end -> excluded
	//
	// The visible range is half-open: [start, end).
	// ------------------------------------------------------------

	exactAtRangeEnd := rangeEndAt

	exactFuture, err := service.CreateLocalTodo(
		ctx,
		CreateLocalTodoInput{
			Title:       "Exact item at range end",
			ScheduledAt: &exactAtRangeEnd,
		},
	)
	if err != nil {
		t.Fatalf("create exact range-end item: %v", err)
	}

	// ------------------------------------------------------------
	// 8. date exactly at range end -> excluded
	// ------------------------------------------------------------

	dateAtRangeEnd, err := temporal.ParseLocalDate("2026-09-16")
	if err != nil {
		t.Fatalf("parse range-end date: %v", err)
	}

	dateFuture, err := service.CreateLocalTodo(
		ctx,
		CreateLocalTodoInput{
			Title:         "Date-only item at range end",
			ScheduledDate: &dateAtRangeEnd,
		},
	)
	if err != nil {
		t.Fatalf("create date range-end item: %v", err)
	}

	// ------------------------------------------------------------
	// Query Calendar-visible Planner Items
	// ------------------------------------------------------------

	items, err := repository.ListByRange(
		ctx,
		rangeStartAt,
		rangeEndAt,
		rangeStartDate,
		rangeEndDate,
	)
	if err != nil {
		t.Fatalf("ListByRange failed: %v", err)
	}

	// This test verifies membership, not SQL ordering.
	returned := make(map[string]bool, len(items))

	for _, item := range items {
		returned[item.ID] = true
	}

	expected := []string{scheduledExact.ID, dueExact.ID, scheduledDateOnly.ID, dueDateOnly.ID}

	for _, id := range expected {
		if !returned[id] {
			t.Fatalf("expected planner item %q to be returned", id)
		}
	}

	if returned[noDate.ID] {
		t.Fatalf("no-date item %q must not appear in calendar range", noDate.ID)
	}

	if returned["archived-range-item"] {
		t.Fatal("archived PlannerItem must not appear in calendar range")
	}

	if returned[exactFuture.ID] {
		t.Fatalf("exact item at range end %q must not be returned", exactFuture.ID)
	}

	if returned[dateFuture.ID] {
		t.Fatalf("date-only item at range end %q must not be returned", dateFuture.ID)
	}

	if len(items) != len(expected) {
		t.Fatalf("expected %d planner items, got %d", len(expected), len(items))
	}
}
