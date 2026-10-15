package app

import (
	"lum-harbor-notifier/internal/calendar"
	"lum-harbor-notifier/internal/plannerview"
)

// PlannerViewRequestDTO is the frontend request used to load all data required
// for one visible Planner Calendar range.
//
// RangeStart and RangeEnd use YYYY-MM-DD and form a half-open range:
//
//	[RangeStart, RangeEnd)
//
// Today is intentionally independent from the visible range. A user may browse
// October while Current Open and Completed Today must still represent the real
// current day.
//
// Timezone must contain an IANA timezone identifier such as:
//
//	America/Chicago
//
// The backend uses that timezone to translate calendar-day boundaries into
// exact instants without assuming that every local day contains exactly
// twenty-four hours.
type PlannerViewRequestDTO struct {
	RangeStart string `json:"rangeStart"`
	RangeEnd   string `json:"rangeEnd"`
	Today      string `json:"today"`
	Timezone   string `json:"timezone"`
}

// PlannerViewDTO is the complete page-level read model returned to the
// Planner frontend.
//
// Each collection is a projection over authoritative backend state:
//
//   - CalendarEvents: things that happen in the visible Calendar range.
//   - CalendarItems: Planner actions positioned in the visible Calendar range.
//   - CurrentOpenItems: past-due, today, and no-date actions requiring attention.
//   - CompletedItems: actions completed inside the visible Calendar range.
//   - CompletedTodayItems: actions completed on Today, independently of the
//     visible Calendar range.
//
// A PlannerItem may intentionally appear in more than one collection. For
// example, an open task due today may appear in both CalendarItems and
// CurrentOpenItems. This does not represent duplicated persisted state.
type PlannerViewDTO struct {
	CalendarEvents   []CalendarEventDTO `json:"calendarEvents"`
	CalendarItems    []PlannerItemDTO   `json:"calendarItems"`
	CurrentOpenItems []PlannerItemDTO   `json:"currentOpenItems"`
	CompletedItems   []PlannerItemDTO   `json:"completedItems"`
	// CompletedTodayItems is anchored to request Today rather than the visible
	// range so the Open rail remains accurate while another period is visible.
	CompletedTodayItems []PlannerItemDTO `json:"completedTodayItems"`
}

// CalendarEventDTO is the frontend representation of one concrete Calendar
// Event occurrence.
//
// Timed events use StartAt and EndAt.
// All-day events use StartDate and EndDate.
//
// EndDate remains exclusive. For example, a one-day all-day event on Sep 18
// is represented as:
//
//	StartDate = 2026-09-18
//	EndDate   = 2026-09-19
//
// This preserves the Calendar domain semantics through the transport layer.
type CalendarEventDTO struct {
	ID        string `json:"id"`
	Authority string `json:"authority"`

	Title    string  `json:"title"`
	Details  *string `json:"details"`
	Location *string `json:"location"`

	StartAt *string `json:"startAt"`
	EndAt   *string `json:"endAt"`

	StartDate *string `json:"startDate"`
	EndDate   *string `json:"endDate"`

	IsAllDay bool    `json:"isAllDay"`
	Timezone *string `json:"timezone"`
}

// plannerViewToDTO converts the application-level Planner View into the stable
// transport contract consumed by the Wails frontend.
//
// This function performs representation conversion only. Projection
// membership has already been decided by plannerview.Service.
func plannerViewToDTO(view *plannerview.View) PlannerViewDTO {

	calendarEvents := make([]CalendarEventDTO, 0, len(view.CalendarEvents))

	for _, event := range view.CalendarEvents {
		calendarEvents = append(calendarEvents, calendarEventToDTO(event))
	}

	return PlannerViewDTO{
		CalendarEvents: calendarEvents,

		CalendarItems: plannerItemsToDTO(view.CalendarItems),

		CurrentOpenItems: plannerItemsToDTO(view.CurrentOpenItems),

		CompletedItems: plannerItemsToDTO(view.CompletedItems),

		CompletedTodayItems: plannerItemsToDTO(view.CompletedTodayItems),
	}
}

// calendarEventToDTO converts a Calendar domain Event into its frontend
// representation.
//
// Timestamp and date-only fields reuse the transport helpers defined for
// PlannerItemDTO because both domains share the same transport semantics:
// exact instants use RFC3339 and LocalDate values use YYYY-MM-DD.
func calendarEventToDTO(event calendar.Event) CalendarEventDTO {

	return CalendarEventDTO{
		ID:        event.ID,
		Authority: string(event.Authority),

		Title:    event.Title,
		Details:  event.Details,
		Location: event.Location,

		StartAt: formatDTOTime(event.StartAt),
		EndAt:   formatDTOTime(event.EndAt),

		StartDate: formatDTODate(event.StartDate),
		EndDate:   formatDTODate(event.EndDate),

		IsAllDay: event.IsAllDay,
		Timezone: event.Timezone,
	}
}
