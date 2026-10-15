package plannerview

import (
	"context"
	"errors"
	"fmt"
	"time"

	"lum-harbor-notifier/internal/calendar"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/temporal"
)

// Service builds the Planner page read model by coordinating multiple
// application domains.
//
// PlannerItem and CalendarEvent remain independent domain models.
// This service is the application boundary where those models are combined
// into the projections needed by one UI page.
type Service struct {
	plannerService     *planner.Service
	calendarRepository *calendar.Repository
}

// NewService creates the Planner View application service from long-lived
// domain dependencies assembled by the process composition root.
//
// Planner View coordinates read use cases only. It neither owns a database
// connection nor constructs repositories, which keeps persistence ownership
// in the domain layer and makes dependency lifetimes visible to callers.
func NewService(
	plannerService *planner.Service,
	calendarRepository *calendar.Repository,
) *Service {
	return &Service{
		plannerService:     plannerService,
		calendarRepository: calendarRepository,
	}
}

// Get builds all data required to render the Planner page for one visible
// Calendar range.
//
// The method deliberately returns multiple projections instead of merging
// CalendarEvent and PlannerItem into a common artificial type:
//
//   - CalendarEvents represents things that happen.
//   - CalendarItems represents actions placed on the visible Calendar.
//   - CurrentOpenItems represents the current action queue.
//   - CompletedItems represents actions completed within the visible range.
//   - CompletedTodayItems represents actions completed on Request.Today,
//     independently of the visible range.
//
// A PlannerItem may appear in multiple projections. This is intentional and
// does not create duplicate authoritative state.
func (s *Service) Get(ctx context.Context, request Request) (*View, error) {

	if err := validateRequest(request); err != nil {
		return nil, err
	}

	rangeStartAt, err := startOfLocalDate(request.RangeStart, request.Location)
	if err != nil {
		return nil, fmt.Errorf("resolve planner view range start: %w", err)
	}

	rangeEndAt, err := startOfLocalDate(request.RangeEnd, request.Location)
	if err != nil {
		return nil, fmt.Errorf("resolve planner view range end: %w", err)
	}

	// Calendar Events use interval-overlap semantics because an Event has
	// duration and may begin before the visible range while continuing into it.
	calendarEvents, err := s.calendarRepository.ListByRange(
		ctx,
		rangeStartAt,
		rangeEndAt,
		request.RangeStart,
		request.RangeEnd,
	)
	if err != nil {
		return nil, fmt.Errorf("load planner view calendar events: %w", err)
	}

	// Calendar PlannerItems use scheduled/due anchor semantics rather than
	// Event overlap semantics. Planner.Service and Repository own that
	// distinction.
	calendarItems, err := s.plannerService.ListByRange(
		ctx,
		rangeStartAt,
		rangeEndAt,
		request.RangeStart,
		request.RangeEnd,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"load planner view calendar items: %w",
			err,
		)
	}

	currentOpenItems, err := s.plannerService.ListCurrentOpen(ctx, request.Today, request.Location)
	if err != nil {
		return nil, fmt.Errorf("load planner view current open items: %w", err)
	}

	todayStartAt, err := startOfLocalDate(request.Today, request.Location)
	if err != nil {
		return nil, fmt.Errorf("resolve planner view current date: %w", err)
	}

	// AddDate advances by one local calendar day. A fixed 24-hour duration
	// would produce the wrong boundary when the request timezone enters or
	// leaves daylight saving time.
	tomorrowStartAt := todayStartAt.AddDate(0, 0, 1)

	completedTodayItems, err := s.plannerService.ListCompletedByRange(
		ctx,
		todayStartAt,
		tomorrowStartAt,
	)
	if err != nil {
		return nil, fmt.Errorf("load planner view completed today items: %w", err)
	}

	completedItems, err := s.plannerService.ListCompletedByRange(ctx, rangeStartAt, rangeEndAt)
	if err != nil {
		return nil, fmt.Errorf("load planner view completed items: %w", err)
	}

	return &View{
		CalendarEvents:      calendarEvents,
		CalendarItems:       calendarItems,
		CurrentOpenItems:    currentOpenItems,
		CompletedItems:      completedItems,
		CompletedTodayItems: completedTodayItems,
	}, nil
}

// validateRequest verifies the temporal invariants required to build a
// Planner View.
//
// Today does not need to fall inside the visible range because the user may
// browse historical or future Calendar periods while CurrentOpen and
// CompletedToday continue to represent the actual current day.
func validateRequest(request Request) error {

	if request.Location == nil {
		return errors.New("planner view location must not be nil")
	}
	if !request.RangeStart.Valid() {
		return fmt.Errorf("invalid planner view range start %q", request.RangeStart)
	}
	if !request.RangeEnd.Valid() {
		return fmt.Errorf("invalid planner view range end %q", request.RangeEnd)
	}
	if string(request.RangeEnd) <= string(request.RangeStart) {
		return errors.New("planner view range end must be after range start")
	}
	if !request.Today.Valid() {
		return fmt.Errorf("invalid planner view current date %q", request.Today)
	}

	return nil
}

// startOfLocalDate resolves a timezone-free LocalDate into the exact instant
// representing midnight at the beginning of that date in the supplied
// location.
//
// LocalDate itself intentionally carries no timezone. The timezone is applied
// here because Planner View is the layer that knows which user's local
// Calendar is being rendered.
func startOfLocalDate(date temporal.LocalDate, location *time.Location) (time.Time, error) {

	if location == nil {
		return time.Time{}, errors.New("location must not be nil")
	}
	if !date.Valid() {
		return time.Time{}, fmt.Errorf("invalid local date %q", date)
	}

	value, err := time.ParseInLocation(temporal.LocalDateLayout, string(date), location)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse local date %q in location %q: %w", date, location.String(), err)
	}

	return value, nil
}
