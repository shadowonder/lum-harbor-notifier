package plannerview

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"lum-harbor-notifier/internal/calendar"
	"lum-harbor-notifier/internal/database/codec"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/temporal"
	"lum-harbor-notifier/internal/testsupport"
)

// TestServiceGet verifies the complete Planner View read path across the
// Calendar and Planner domains.
//
// This distinction is important because browsing another month must not change
// the user's Current Open queue or Completed Today history. CurrentOpenItems
// and CompletedTodayItems always represent the real current day, while
// CalendarEvents, CalendarItems, and CompletedItems are projections over the
// visible Calendar range.
//
// The test verifies all five Planner View projections:
//
//   - CalendarEvents
//   - CalendarItems
//   - CurrentOpenItems
//   - CompletedItems
//   - CompletedTodayItems
//
// It also verifies that the same underlying domain state is projected
// differently depending on its temporal semantics. For example, a future
// PlannerItem may appear in CalendarItems while being excluded from
// CurrentOpenItems.
func TestServiceGet(t *testing.T) {
	ctx := context.Background()

	db := testsupport.OpenTestDatabase(t)

	plannerRepository := planner.NewRepository(db.DB)
	plannerService := planner.NewService(db.DB, plannerRepository)
	calendarRepository := calendar.NewRepository(db.DB)
	viewService := NewService(plannerService, calendarRepository)

	location, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatalf("load test timezone: %v", err)
	}

	// Today is deliberately outside the visible October Calendar range.
	//
	// CurrentOpenItems must still be calculated from this date rather than
	// from RangeStart or RangeEnd.
	today, err := temporal.ParseLocalDate("2026-09-15")
	if err != nil {
		t.Fatalf("parse today: %v", err)
	}

	// The visible Calendar range represents the entire month of October.
	//
	// Planner View ranges use half-open semantics:
	//
	//	[2026-10-01, 2026-11-01)
	rangeStart, err := temporal.ParseLocalDate("2026-10-01")
	if err != nil {
		t.Fatalf("parse range start: %v", err)
	}

	rangeEnd, err := temporal.ParseLocalDate("2026-11-01")
	if err != nil {
		t.Fatalf("parse range end: %v", err)
	}

	// ---------------------------------------------------------------------
	// Calendar Event inside the visible October range.
	//
	// This Event should appear only in CalendarEvents because CalendarEvent
	// and PlannerItem remain separate domain concepts.
	// ---------------------------------------------------------------------

	eventStart := time.Date(2026, 10, 15, 10, 0, 0, 0, location)
	eventEnd := time.Date(2026, 10, 15, 11, 0, 0, 0, location)
	insertTimedCalendarEvent(
		t,
		db.DB,
		"calendar-event-october",
		"October architecture review",
		eventStart,
		eventEnd,
	)

	// ---------------------------------------------------------------------
	// PlannerItem due today.
	//
	// Because Today is Sep 15, this belongs to CurrentOpenItems.
	//
	// It must NOT appear in CalendarItems because the visible Calendar is
	// October rather than September.
	// ---------------------------------------------------------------------

	todayDate, err := temporal.ParseLocalDate("2026-09-15")
	if err != nil {
		t.Fatalf("parse today PlannerItem date: %v", err)
	}

	todayItem, err := plannerService.CreateLocalTodo(
		ctx,
		planner.CreateLocalTodoInput{
			Title:   "Finish today's deployment review",
			DueDate: &todayDate,
		},
	)
	if err != nil {
		t.Fatalf("create today PlannerItem: %v", err)
	}

	// ---------------------------------------------------------------------
	// PlannerItem with no date or timestamp.
	//
	// No-date OPEN items belong to CurrentOpenItems because they remain
	// actionable now.
	//
	// They do not belong to CalendarItems because no Calendar position can
	// be derived without inventing a date.
	// ---------------------------------------------------------------------

	noDateItem, err := plannerService.CreateLocalTodo(
		ctx,
		planner.CreateLocalTodoInput{
			Title: "Review unresolved technical debt",
		},
	)
	if err != nil {
		t.Fatalf("create no-date PlannerItem: %v", err)
	}

	// ---------------------------------------------------------------------
	// Future PlannerItem inside the visible October Calendar range.
	//
	// This item demonstrates that Calendar projection and Current Open
	// projection are independent:
	//
	//	CalendarItems     -> yes
	//	CurrentOpenItems  -> no
	//
	// The item is visible on the October Calendar but is not yet part of the
	// Sep 15 current action queue.
	// ---------------------------------------------------------------------

	octoberDueDate, err := temporal.ParseLocalDate("2026-10-10")
	if err != nil {
		t.Fatalf("parse October PlannerItem date: %v", err)
	}

	octoberItem, err := plannerService.CreateLocalTodo(
		ctx,
		planner.CreateLocalTodoInput{
			Title:   "Prepare October release",
			DueDate: &octoberDueDate,
		},
	)
	if err != nil {
		t.Fatalf("create October PlannerItem: %v", err)
	}

	// ---------------------------------------------------------------------
	// Completed PlannerItems in the real current day and visible October range.
	//
	// We insert this fixture directly because CompleteLocalTodo intentionally
	// records time.Now(). That behavior is correct for production mutations,
	// but an integration test needs deterministic control over completed_at.
	//
	// Both PlannerItems have no scheduled or due date. Therefore their
	// completed_at values alone determine projection membership:
	//
	//	Sep 15 item -> CompletedTodayItems only
	//	October item -> CompletedItems only
	// ---------------------------------------------------------------------

	completedTodayAt := time.Date(2026, 9, 15, 16, 45, 0, 0, location)
	insertCompletedPlannerItem(
		t,
		db.DB,
		"planner-item-completed-today",
		"Finish September deployment review",
		completedTodayAt,
	)

	completedOctoberAt := time.Date(2026, 10, 12, 15, 30, 0, 0, location)
	insertCompletedPlannerItem(
		t,
		db.DB,
		"planner-item-completed-october",
		"Verify October production deployment",
		completedOctoberAt,
	)

	// ---------------------------------------------------------------------
	// Build the complete Planner View.
	// ---------------------------------------------------------------------

	view, err := viewService.Get(
		ctx,
		Request{
			RangeStart: rangeStart,
			RangeEnd:   rangeEnd,
			Today:      today,
			Location:   location,
		},
	)
	if err != nil {
		t.Fatalf("Get Planner View failed: %v", err)
	}

	// Convert each projection into ID membership maps.
	//
	// The test intentionally does not depend on repository ordering because
	// this test verifies application-level projection membership rather than
	// presentation order.
	calendarEventIDs := make(map[string]bool, len(view.CalendarEvents))
	for _, event := range view.CalendarEvents {
		calendarEventIDs[event.ID] = true
	}

	calendarItemIDs := make(map[string]bool, len(view.CalendarItems))
	for _, item := range view.CalendarItems {
		calendarItemIDs[item.ID] = true
	}

	currentOpenIDs := make(map[string]bool, len(view.CurrentOpenItems))
	for _, item := range view.CurrentOpenItems {
		currentOpenIDs[item.ID] = true
	}

	completedItemIDs := make(map[string]bool, len(view.CompletedItems))
	for _, item := range view.CompletedItems {
		completedItemIDs[item.ID] = true
	}

	completedTodayItemIDs := make(map[string]bool, len(view.CompletedTodayItems))
	for _, item := range view.CompletedTodayItems {
		completedTodayItemIDs[item.ID] = true
	}

	// ---------------------------------------------------------------------
	// CalendarEvents projection
	// ---------------------------------------------------------------------

	if !calendarEventIDs["calendar-event-october"] {
		t.Fatal(
			"expected October Calendar Event to appear in CalendarEvents",
		)
	}

	if len(view.CalendarEvents) != 1 {
		t.Fatalf(
			"expected 1 Calendar Event, got %d",
			len(view.CalendarEvents),
		)
	}

	// ---------------------------------------------------------------------
	// CalendarItems projection
	// ---------------------------------------------------------------------

	if !calendarItemIDs[octoberItem.ID] {
		t.Fatalf("expected October PlannerItem %q to appear in CalendarItems", octoberItem.ID)
	}

	if calendarItemIDs[todayItem.ID] {
		t.Fatalf("today PlannerItem %q must not appear in October CalendarItems", todayItem.ID)
	}

	if calendarItemIDs[noDateItem.ID] {
		t.Fatalf("no-date PlannerItem %q must not appear in CalendarItems", noDateItem.ID)
	}

	if calendarItemIDs["planner-item-completed-october"] {
		t.Fatal("completed no-date PlannerItem must not appear in CalendarItems")
	}

	if len(view.CalendarItems) != 1 {
		t.Fatalf("expected 1 Calendar PlannerItem, got %d", len(view.CalendarItems))
	}

	// ---------------------------------------------------------------------
	// CurrentOpenItems projection
	// ---------------------------------------------------------------------

	if !currentOpenIDs[todayItem.ID] {
		t.Fatalf("expected today's PlannerItem %q in CurrentOpenItems", todayItem.ID)
	}

	if !currentOpenIDs[noDateItem.ID] {
		t.Fatalf("expected no-date PlannerItem %q in CurrentOpenItems", noDateItem.ID)
	}

	if currentOpenIDs[octoberItem.ID] {
		t.Fatalf("future October PlannerItem %q must not appear in CurrentOpenItems", octoberItem.ID)
	}

	if currentOpenIDs["planner-item-completed-october"] {
		t.Fatal("completed PlannerItem must not appear in CurrentOpenItems")
	}

	if len(view.CurrentOpenItems) != 2 {
		t.Fatalf("expected 2 Current Open PlannerItems, got %d", len(view.CurrentOpenItems))
	}

	// ---------------------------------------------------------------------
	// CompletedItems projection
	// ---------------------------------------------------------------------

	if !completedItemIDs["planner-item-completed-october"] {
		t.Fatal(
			"expected October completed PlannerItem in CompletedItems",
		)
	}

	if completedItemIDs["planner-item-completed-today"] {
		t.Fatal("Sep 15 completed PlannerItem must not appear in October CompletedItems")
	}

	if completedItemIDs[todayItem.ID] {
		t.Fatalf("open PlannerItem %q must not appear in CompletedItems", todayItem.ID)
	}

	if completedItemIDs[octoberItem.ID] {
		t.Fatalf("open future PlannerItem %q must not appear in CompletedItems", octoberItem.ID)
	}

	if len(view.CompletedItems) != 1 {
		t.Fatalf("expected 1 completed PlannerItem, got %d", len(view.CompletedItems))
	}

	// ---------------------------------------------------------------------
	// CompletedTodayItems projection
	// ---------------------------------------------------------------------

	if !completedTodayItemIDs["planner-item-completed-today"] {
		t.Fatal("expected Sep 15 completed PlannerItem in CompletedTodayItems")
	}

	if completedTodayItemIDs["planner-item-completed-october"] {
		t.Fatal("October completed PlannerItem must not appear in CompletedTodayItems")
	}

	if len(view.CompletedTodayItems) != 1 {
		t.Fatalf(
			"expected 1 Completed Today PlannerItem, got %d",
			len(view.CompletedTodayItems),
		)
	}
}

// insertTimedCalendarEvent creates a deterministic timed Calendar Event
// fixture directly in SQLite.
//
// Planner View does not currently expose a Calendar mutation use case, so the
// test should not introduce a production Calendar Create API merely to prepare
// test data.
//
// The fixture creates the shared Entity and Revision records required by the
// CalendarEvent foreign-key relationships before inserting the Calendar row.
func insertTimedCalendarEvent(
	t *testing.T,
	db *sql.DB,
	id string,
	title string,
	startAt time.Time,
	endAt time.Time,
) {
	t.Helper()

	createdAt := startAt.Add(-24 * time.Hour)
	revisionID := id + "-revision"

	_, err := db.Exec(
		`
		INSERT INTO entities (
			id,
			entity_type,
			created_at
		)
		VALUES (?, 'CALENDAR_EVENT', ?)
		`,
		id,
		codec.FormatTime(createdAt),
	)
	if err != nil {
		t.Fatalf("insert Calendar Event entity %q: %v", id, err)
	}

	_, err = db.Exec(
		`
		INSERT INTO revisions (
			id,
			origin_type,
			summary,
			applied_at
		)
		VALUES (?, 'SYNC', ?, ?)
		`,
		revisionID,
		"Calendar integration test fixture",
		codec.FormatTime(createdAt),
	)
	if err != nil {
		t.Fatalf("insert Calendar Event revision %q: %v", revisionID, err)
	}

	_, err = db.Exec(
		`
		INSERT INTO calendar_events (
			entity_id,
			authority_type,
			title,
			start_at,
			end_at,
			is_all_day,
			timezone,
			last_revision_id,
			created_at,
			updated_at
		)
		VALUES (?, 'EXTERNAL', ?, ?, ?, 0, ?, ?, ?, ?)
		`,
		id,
		title,
		codec.FormatTime(startAt),
		codec.FormatTime(endAt),
		startAt.Location().String(),
		revisionID,
		codec.FormatTime(createdAt),
		codec.FormatTime(createdAt),
	)
	if err != nil {
		t.Fatalf("insert timed Calendar Event %q: %v", id, err)
	}
}

// insertCompletedPlannerItem creates a deterministic completed PlannerItem
// fixture directly in SQLite.
//
// CompleteLocalTodo uses the real current clock because completion is a
// production mutation. This helper exists so application-level read tests can
// place completed_at at a precise historical or future test instant without
// introducing clock manipulation into production code.
//
// The PlannerItem intentionally has no scheduled or due value. This isolates
// CompletedItems behavior from CalendarItems behavior and proves that
// completion projection is based on completed_at.
func insertCompletedPlannerItem(
	t *testing.T,
	db *sql.DB,
	id string,
	title string,
	completedAt time.Time,
) {
	t.Helper()

	createdAt := completedAt.Add(-48 * time.Hour)
	revisionID := id + "-revision"

	_, err := db.Exec(
		`
		INSERT INTO entities (
			id,
			entity_type,
			created_at
		)
		VALUES (?, 'PLANNER_ITEM', ?)
		`,
		id,
		codec.FormatTime(createdAt),
	)
	if err != nil {
		t.Fatalf("insert completed PlannerItem entity %q: %v", id, err)
	}

	_, err = db.Exec(
		`
		INSERT INTO revisions (
			id,
			origin_type,
			summary,
			applied_at
		)
		VALUES (?, 'USER', ?, ?)
		`,
		revisionID,
		"Planner integration test completion fixture",
		codec.FormatTime(completedAt),
	)
	if err != nil {
		t.Fatalf("insert completed PlannerItem revision %q: %v", revisionID, err)
	}

	_, err = db.Exec(
		`
		INSERT INTO planner_items (
			entity_id,
			authority_type,
			item_type,
			title,
			status,
			context_status,
			last_revision_id,
			created_at,
			updated_at,
			completed_at
		)
		VALUES (
			?,
			'LOCAL',
			'TODO',
			?,
			'COMPLETED',
			'UNKNOWN',
			?,
			?,
			?,
			?
		)
		`,
		id,
		title,
		revisionID,
		codec.FormatTime(createdAt),
		codec.FormatTime(completedAt),
		codec.FormatTime(completedAt),
	)
	if err != nil {
		t.Fatalf("insert completed PlannerItem %q: %v", id, err)
	}
}
