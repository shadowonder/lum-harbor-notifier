package reminder

import (
	"errors"
	"time"
)

var (
	ErrNotFound     = errors.New("todo reminder not found")
	ErrInvalidInput = errors.New("invalid todo reminder input")
	ErrUndatedTodo  = errors.New("cannot set due-relative reminder on undated todo")
)

type State string

const (
	StateActive    State = "ACTIVE"
	StateSatisfied State = "SATISFIED"
	StateCancelled State = "CANCELLED"
)

func (s State) Valid() bool {
	return s == StateActive || s == StateSatisfied || s == StateCancelled
}

type RemindMode string

const (
	RemindModeBeforeDue RemindMode = "BEFORE_DUE"
	RemindModeExactTime RemindMode = "EXACT_TIME"
)

func (m RemindMode) Valid() bool {
	return m == RemindModeBeforeDue || m == RemindModeExactTime
}

type Reminder struct {
	ID             string
	PlannerItemID  string
	State          State
	OffsetsSeconds []int
	FiredOffsets   []int
	RemindMode     RemindMode
	LastRevisionID string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	SatisfiedAt    *time.Time
}

type SetReminderInput struct {
	PlannerItemID  string
	OffsetsSeconds []int
	RemindMode     RemindMode
	// ConfiguredAtCreation marks a reminder set by the same request that
	// created the Planner item, so its History names that creation as cause.
	ConfiguredAtCreation bool
}
