package workitem

import "time"

// WorkItem is Lum Harbor's authoritative representation of one long-term
// outcome or objective. It is a domain model, not a generated database row.
type WorkItem struct {
	ID             string
	Title          string
	Description    *string
	Lifecycle      LifecycleState
	CompletedAt    *time.Time
	LastRevisionID string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// LifecycleState is the minimal V1 WorkItem lifecycle vocabulary.
type LifecycleState string

const (
	LifecycleActive    LifecycleState = "ACTIVE"
	LifecyclePaused    LifecycleState = "PAUSED"
	LifecycleCompleted LifecycleState = "COMPLETED"
	LifecycleCancelled LifecycleState = "CANCELLED"
)

// Valid reports whether the lifecycle value is supported by WorkItem V1.
func (s LifecycleState) Valid() bool {
	switch s {
	case LifecycleActive, LifecyclePaused, LifecycleCompleted, LifecycleCancelled:
		return true
	default:
		return false
	}
}

// CanTransitionTo reports whether V1 permits a real lifecycle change. A
// same-state request is handled separately as a no-op by the Service.
func (s LifecycleState) CanTransitionTo(target LifecycleState) bool {
	switch s {
	case LifecycleActive:
		return target == LifecyclePaused || target == LifecycleCompleted || target == LifecycleCancelled
	case LifecyclePaused:
		return target == LifecycleActive || target == LifecycleCompleted || target == LifecycleCancelled
	case LifecycleCompleted, LifecycleCancelled:
		return target == LifecycleActive
	default:
		return false
	}
}
