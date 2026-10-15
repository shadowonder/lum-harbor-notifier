package calendar

import (
	"context"
	"testing"
	"time"

	"lum-harbor-notifier/internal/database/codec"
	dbgen "lum-harbor-notifier/internal/database/generated"
	"lum-harbor-notifier/internal/temporal"
	"lum-harbor-notifier/internal/testsupport"
)

func TestRepositoryListByRange(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)

	repository := NewRepository(db.DB)

	location, err := time.LoadLocation("America/Chicago")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	rangeStart := time.Date(2026, 9, 15, 0, 0, 0, 0, location)

	rangeEnd := time.Date(2026, 9, 16, 0, 0, 0, 0, location)

	rangeStartDate, err := temporal.ParseLocalDate("2026-09-15")
	if err != nil {
		t.Fatalf("parse range start date: %v", err)
	}

	rangeEndDate, err := temporal.ParseLocalDate("2026-09-16")
	if err != nil {
		t.Fatalf("parse range end date: %v", err)
	}

	// Starts before the visible range but overlaps into it.
	//
	// Sep 14 23:00 -> Sep 15 01:00
	//
	// Must be returned.
	overlapStart := time.Date(2026, 9, 14, 23, 0, 0, 0, location)

	overlapEnd := time.Date(2026, 9, 15, 1, 0, 0, 0, location)

	insertTimedCalendarEvent(t, db.DB, "event-overlap", "Overlapping deployment", overlapStart, overlapEnd)

	// Fully inside the visible range.
	insideStart := time.Date(2026, 9, 15, 10, 0, 0, 0, location)

	insideEnd := time.Date(2026, 9, 15, 11, 0, 0, 0, location)

	insertTimedCalendarEvent(t, db.DB, "event-inside", "Team meeting", insideStart, insideEnd)

	// One-day all-day event:
	//
	// [Sep 15, Sep 16)
	//
	// Must be returned.
	insertAllDayCalendarEvent(t, db.DB, "event-all-day", "Release day", "2026-09-15", "2026-09-16")

	// This event ends exactly when the requested range begins:
	//
	// [Sep 14, Sep 15)
	//
	// Because EndDate is exclusive, it must NOT appear on Sep 15.
	insertAllDayCalendarEvent(t, db.DB, "event-before", "Previous day", "2026-09-14", "2026-09-15")

	// Completely in the future.
	insertAllDayCalendarEvent(t, db.DB, "event-future", "Future event", "2026-09-16", "2026-09-17")

	events, err := repository.ListByRange(ctx, rangeStart, rangeEnd, rangeStartDate, rangeEndDate)
	if err != nil {
		t.Fatalf("ListByRange failed: %v", err)
	}

	returned := make(map[string]bool, len(events))

	for _, event := range events {
		returned[event.ID] = true
	}

	expected := []string{"event-overlap", "event-inside", "event-all-day"}

	for _, id := range expected {
		if !returned[id] {
			t.Fatalf("expected calendar event %q to be returned", id)
		}
	}
	if returned["event-before"] {
		t.Fatal("all-day event ending at range start must not be returned")
	}
	if returned["event-future"] {
		t.Fatal("future event outside the range must not be returned")
	}
	if len(events) != len(expected) {
		t.Fatalf("expected %d events, got %d", len(expected), len(events))
	}
}

func insertTimedCalendarEvent(
	t *testing.T,
	db dbgen.DBTX,
	id string,
	title string,
	start time.Time,
	end time.Time,
) {
	t.Helper()

	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

	revisionID := id + "-revision"

	if _, err := db.ExecContext(
		context.Background(),
		`
		INSERT INTO entities (
			id,
			entity_type,
			created_at
		)
		VALUES (?, 'CALENDAR_EVENT', ?)
		`,
		id,
		codec.FormatTime(now),
	); err != nil {
		t.Fatalf("insert calendar entity %s: %v", id, err)
	}

	if _, err := db.ExecContext(
		context.Background(),
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
		"Calendar test fixture",
		codec.FormatTime(now),
	); err != nil {
		t.Fatalf("insert calendar revision %s: %v", id, err)
	}

	if _, err := db.ExecContext(
		context.Background(),
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
		codec.FormatTime(start),
		codec.FormatTime(end),
		start.Location().String(),
		revisionID,
		codec.FormatTime(now),
		codec.FormatTime(now),
	); err != nil {
		t.Fatalf("insert timed calendar event %s: %v", id, err)
	}
}

func insertAllDayCalendarEvent(
	t *testing.T,
	db dbgen.DBTX,
	id string,
	title string,
	startDate string,
	endDate string,
) {
	t.Helper()

	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)

	revisionID := id + "-revision"

	if _, err := db.ExecContext(
		context.Background(),
		`
		INSERT INTO entities (
			id,
			entity_type,
			created_at
		)
		VALUES (?, 'CALENDAR_EVENT', ?)
		`,
		id,
		codec.FormatTime(now),
	); err != nil {
		t.Fatalf("insert calendar entity %s: %v", id, err)
	}

	if _, err := db.ExecContext(
		context.Background(),
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
		"Calendar test fixture",
		codec.FormatTime(now),
	); err != nil {
		t.Fatalf("insert calendar revision %s: %v", id, err)
	}

	if _, err := db.ExecContext(
		context.Background(),
		`
		INSERT INTO calendar_events (
			entity_id,
			authority_type,
			title,
			start_date,
			end_date,
			is_all_day,
			last_revision_id,
			created_at,
			updated_at
		)
		VALUES (?, 'EXTERNAL', ?, ?, ?, 1, ?, ?, ?)
		`,
		id,
		title,
		startDate,
		endDate,
		revisionID,
		codec.FormatTime(now),
		codec.FormatTime(now),
	); err != nil {
		t.Fatalf("insert all-day calendar event %s: %v", id, err)
	}
}
