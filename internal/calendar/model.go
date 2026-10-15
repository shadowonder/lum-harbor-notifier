package calendar

import (
	"time"

	"lum-harbor-notifier/internal/temporal"
)

// Event represents one concrete calendar occurrence.
//
// An Event describes something that happens in time. It intentionally
// does not have PlannerItem lifecycle such as OPEN or COMPLETED.
type Event struct {
	ID        string
	Authority AuthorityType

	Title    string
	Details  *string
	Location *string

	StartAt *time.Time
	EndAt   *time.Time

	StartDate *temporal.LocalDate
	EndDate   *temporal.LocalDate

	IsAllDay bool
	Timezone *string

	LastRevisionID string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// AuthorityType identifies who owns the Event's native state.
type AuthorityType string

const (
	AuthorityLocal    AuthorityType = "LOCAL"
	AuthorityExternal AuthorityType = "EXTERNAL"
)

// Valid reports whether the authority value is supported.
func (a AuthorityType) Valid() bool {
	switch a {
	case AuthorityLocal, AuthorityExternal:
		return true
	default:
		return false
	}
}
