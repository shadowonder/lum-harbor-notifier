package plannerview

import (
	"time"

	"lum-harbor-notifier/internal/calendar"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/temporal"
)

// Request describes the temporal context required to build one Planner page.
//
// RangeStart and RangeEnd describe the visible Calendar range using
// half-open date semantics:
//
//	[RangeStart, RangeEnd)
//
// For example, a one-day Sep 15 range is:
//
//	RangeStart = 2026-09-15
//	RangeEnd   = 2026-09-16
//
// Today is intentionally independent from the visible range. The user may
// browse another week or month while the Open rail must still represent the
// real current action queue.
//
// Location gives date-only values their local calendar context when the
// application needs to derive exact timestamp boundaries such as local
// midnight.
type Request struct {
	RangeStart temporal.LocalDate
	RangeEnd   temporal.LocalDate

	Today temporal.LocalDate

	Location *time.Location
}

// View is the application-level read model consumed by the Planner page.
//
// The same PlannerItem may intentionally appear in more than one projection.
// For example, a task due today may appear in both CalendarItems and
// CurrentOpenItems. These slices are projections over the same domain truth,
// not separate persisted copies.
type View struct {
	CalendarEvents []calendar.Event

	CalendarItems []planner.PlannerItem

	CurrentOpenItems []planner.PlannerItem

	// CompletedItems contains completions within the visible Calendar range.
	// It supports selected-day projections while the user browses that range.
	CompletedItems []planner.PlannerItem

	// CompletedTodayItems contains completions for Request.Today in the
	// request's local timezone, independently of the visible Calendar range.
	// It keeps the Open rail's Completed Today section accurate while the user
	// browses a historical or future month.
	CompletedTodayItems []planner.PlannerItem
}
