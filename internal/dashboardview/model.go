package dashboardview

import (
	"time"

	"lum-harbor-notifier/internal/plannerview"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

// Request contains the already-defined Planner temporal context needed by the
// Dashboard. Dashboard View delegates it unchanged to Planner View.
type Request struct {
	Planner plannerview.Request
}

// View is the application-level read model for the cross-domain Dashboard.
type View struct {
	Planner   *plannerview.View
	WorkItems WorkItemSummary
}

// WorkItemSummary contains only lifecycle facts supported by authoritative
// WorkItem current state.
type WorkItemSummary struct {
	CurrentCount      int
	PausedCount       int
	CompletedCount    int
	CancelledCount    int
	ClosedCount       int
	CurrentItems      []CurrentWorkItem
	AllCurrentItems   []CurrentWorkItem
	TrackingAvailable bool
}

// CurrentWorkItem is the compact navigable WorkItem projection used by the
// Dashboard. Description is authoritative WorkItem display text; schedule,
// completion metadata, and internal revision identity remain deliberately absent.
type CurrentWorkItem struct {
	ID          string
	Title       string
	Description *string
	Lifecycle   workitem.LifecycleState
	UpdatedAt   time.Time
	Tracking    tracking.WorkItemSummary
}
