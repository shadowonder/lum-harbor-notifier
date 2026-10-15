package planner

import (
	"lum-harbor-notifier/internal/temporal"
	"time"
)

// PlannerItem is Lum Harbor's domain representation of one actionable
// item shown in the Planner.
//
// This struct represents business state, not a raw database row.
// Repository code is responsible for translating between SQLite values
// and this domain model.
//
// Optional database timestamps are represented as *time.Time:
//
//	nil       → no value exists
//	non-nil   → a real timestamp exists
type PlannerItem struct {
	ID              string
	Authority       AuthorityType
	WorkItemID      *string
	WorkflowID      *string
	WorkflowStepID  *string
	RelatedEntityID *string
	MonitorPointID  *string
	Type            ItemType
	Title           string
	Details         *string
	Status          Status

	ScheduledAt   *time.Time          // Exact date-time placement
	DueAt         *time.Time          // Exact date-time placement
	ScheduledDate *temporal.LocalDate // Date-only placement
	DueDate       *temporal.LocalDate // Date-only placement

	ContextStatus  ContextStatus
	MissingContext []string

	LastRevisionID string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CompletedAt    *time.Time

	// LocalNote is Lum Harbor-owned context attached to the item. For an
	// EXTERNAL item every other content field above is owned by the external
	// system and refreshed by sync; LocalNote is never written by sync and
	// changes only through SetExternalPlannerItemLocalNoteTx.
	LocalNote *string
}

type ItemType string

const (
	ItemTypeTodo       ItemType = "TODO"
	ItemTypeCheckPoint ItemType = "CHECKPOINT"
	ItemTypeFollowUp   ItemType = "FOLLOW_UP"
	ItemTypeReminder   ItemType = "REMINDER"
)

type ContextStatus string

const (
	ContextComplete   ContextStatus = "COMPLETE"
	ContextIncomplete ContextStatus = "INCOMPLETE"
	ContextUnknown    ContextStatus = "UNKNOWN"
)

type Status string

const (
	StatusOpen      Status = "OPEN"
	StatusCompleted Status = "COMPLETED"
)

type AuthorityType string

const (
	AuthorityLocal    AuthorityType = "LOCAL"
	AuthorityExternal AuthorityType = "EXTERNAL"
)

// Valid reports whether the authority value is supported by Lum Harbor.
func (a AuthorityType) Valid() bool {
	switch a {
	case AuthorityLocal, AuthorityExternal:
		return true
	default:
		return false
	}
}

// Valid reports whether the ItemType is one of the values currently
// supported by the Planner domain.
//
// Repository and Service code can use this before persisting externally
// supplied values.
func (t ItemType) Valid() bool {
	switch t {
	case ItemTypeTodo,
		ItemTypeCheckPoint,
		ItemTypeFollowUp,
		ItemTypeReminder:
		return true
	default:
		return false
	}
}

// Valid reports whether the Planner lifecycle status is supported.
func (s Status) Valid() bool {
	switch s {
	case StatusOpen, StatusCompleted:
		return true
	default:
		return false
	}
}

// Valid reports whether the context state is part of the supported
// Planner domain vocabulary.
func (s ContextStatus) Valid() bool {
	switch s {
	case ContextComplete,
		ContextIncomplete,
		ContextUnknown:
		return true
	default:
		return false
	}
}
