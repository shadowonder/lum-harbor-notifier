package calendar

import (
	"context"
	"fmt"
	"lum-harbor-notifier/internal/database/codec"
	dbgen "lum-harbor-notifier/internal/database/generated"
	"lum-harbor-notifier/internal/temporal"
	"time"
)

const databaseTimeFormat = "2006-01-02T15:04:05.000000000Z07:00"

// Repository provides Calendar read persistence through the executor supplied
// at construction time. The current application uses a process-scoped
// Repository bound to *sql.DB because Calendar exposes read use cases only.
type Repository struct {
	queries *dbgen.Queries
}

// NewRepository binds Calendar persistence operations to db. The caller owns
// the executor and therefore controls whether this Repository is process- or
// transaction-scoped.
func NewRepository(db dbgen.DBTX) *Repository {
	return &Repository{
		queries: dbgen.New(db),
	}
}

// ListByRange returns active Calendar Events that overlap the requested
// visible calendar range.
func (r *Repository) ListByRange(
	ctx context.Context,
	startAt time.Time,
	endAt time.Time,
	startDate temporal.LocalDate,
	endDate temporal.LocalDate,
) ([]Event, error) {

	if !endAt.After(startAt) {
		return nil, fmt.Errorf("calendar range end must be after start")
	}

	if !startDate.Valid() {
		return nil, fmt.Errorf("invalid calendar range start date %q", startDate)
	}

	if !endDate.Valid() {
		return nil, fmt.Errorf("invalid calendar range end date %q", endDate)
	}

	if string(endDate) <= string(startDate) {
		return nil, fmt.Errorf("calendar date range end must be after start")
	}

	startAtValue := codec.FormatTime(startAt)
	endAtValue := codec.FormatTime(endAt)

	startDateValue := string(startDate)
	endDateValue := string(endDate)

	rows, err := r.queries.ListCalendarEventsByRange(
		ctx,
		dbgen.ListCalendarEventsByRangeParams{
			RangeEndAt:     &endAtValue,
			RangeStartAt:   &startAtValue,
			RangeEndDate:   &endDateValue,
			RangeStartDate: &startDateValue,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list calendar events by range: %w", err)
	}

	events := make([]Event, 0, len(rows))

	for _, row := range rows {
		event, err := eventFromDB(row)
		if err != nil {
			return nil, fmt.Errorf(
				"convert calendar event %s: %w",
				row.EntityID,
				err,
			)
		}

		events = append(events, *event)
	}

	return events, nil
}

// eventFromDB converts sqlc's CalendarEvent persistence model into
// Lum Harbor's Calendar domain model.
func eventFromDB(dbEvent dbgen.CalendarEvent) (*Event, error) {

	authority := AuthorityType(dbEvent.AuthorityType)
	if !authority.Valid() {
		return nil, fmt.Errorf("invalid calendar authority %q", dbEvent.AuthorityType)
	}

	createdAt, err := codec.ParseTime(dbEvent.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}

	updatedAt, err := codec.ParseTime(dbEvent.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}

	startAt, err := codec.ParseOptionalTime(dbEvent.StartAt)
	if err != nil {
		return nil, fmt.Errorf("parse start_at: %w", err)
	}

	endAt, err := codec.ParseOptionalTime(dbEvent.EndAt)
	if err != nil {
		return nil, fmt.Errorf("parse end_at: %w", err)
	}

	startDate, err := codec.ParseOptionalLocalDate(dbEvent.StartDate)
	if err != nil {
		return nil, fmt.Errorf("parse start_date: %w", err)
	}

	endDate, err := codec.ParseOptionalLocalDate(dbEvent.EndDate)
	if err != nil {
		return nil, fmt.Errorf("parse end_date: %w", err)
	}

	switch dbEvent.IsAllDay {
	case 0:
		if startAt == nil || endAt == nil {
			return nil, fmt.Errorf("timed calendar event requires start_at and end_at")
		}

		if startDate != nil || endDate != nil {
			return nil, fmt.Errorf("timed calendar event must not contain date-only boundaries")
		}

	case 1:
		if startDate == nil || endDate == nil {
			return nil, fmt.Errorf("all-day calendar event requires start_date and end_date")
		}

		if startAt != nil || endAt != nil {
			return nil, fmt.Errorf("all-day calendar event must not contain exact timestamps")
		}

	default:
		return nil, fmt.Errorf("invalid is_all_day value %d", dbEvent.IsAllDay)
	}

	return &Event{
		ID:        dbEvent.EntityID,
		Authority: authority,

		Title:    dbEvent.Title,
		Details:  dbEvent.Details,
		Location: dbEvent.Location,

		StartAt: startAt,
		EndAt:   endAt,

		StartDate: startDate,
		EndDate:   endDate,

		IsAllDay: dbEvent.IsAllDay == 1,
		Timezone: dbEvent.Timezone,

		LastRevisionID: dbEvent.LastRevisionID,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}, nil
}
