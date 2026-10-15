package dashboardview

import (
	"context"
	"fmt"
	"sort"

	"lum-harbor-notifier/internal/plannerview"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

const currentWorkItemDisplayLimit = 4

// PlannerReader preserves Planner View as the authority for Dashboard today,
// current-open, Calendar, and completion projections.
type PlannerReader interface {
	Get(context.Context, plannerview.Request) (*plannerview.View, error)
}

// WorkItemReader supplies authoritative non-archived WorkItem current state.
type WorkItemReader interface {
	List(context.Context) ([]workitem.WorkItem, error)
}

type TrackingSummaryReader interface {
	ListWorkItemSummaries(context.Context) (map[string]tracking.WorkItemSummary, error)
}

// Service composes existing application/domain read authorities into one
// Dashboard-specific projection. It owns no mutation behavior.
type Service struct {
	planner   PlannerReader
	workItems WorkItemReader
	tracking  TrackingSummaryReader
}

func NewService(planner PlannerReader, workItems WorkItemReader, trackingReader TrackingSummaryReader) *Service {
	return &Service{planner: planner, workItems: workItems, tracking: trackingReader}
}

// Get returns one Dashboard read result. Planner and WorkItems are primary;
// Tracking is independently available so its failure never hides WorkItems.
func (s *Service) Get(ctx context.Context, request Request) (*View, error) {
	planner, err := s.planner.Get(ctx, request.Planner)
	if err != nil {
		return nil, fmt.Errorf("load dashboard planner projection: %w", err)
	}
	items, err := s.workItems.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("load dashboard WorkItem projection: %w", err)
	}
	trackingSummaries, trackingErr := s.tracking.ListWorkItemSummaries(ctx)
	return &View{Planner: planner, WorkItems: summarizeWorkItems(items, trackingSummaries, trackingErr == nil)}, nil
}

func summarizeWorkItems(items []workitem.WorkItem, trackingSummaries map[string]tracking.WorkItemSummary, trackingAvailable bool) WorkItemSummary {
	summary := WorkItemSummary{
		CurrentItems:      make([]CurrentWorkItem, 0),
		AllCurrentItems:   make([]CurrentWorkItem, 0),
		TrackingAvailable: trackingAvailable,
	}
	for _, item := range items {
		switch item.Lifecycle {
		case workitem.LifecycleActive:
			summary.CurrentCount++
			summary.AllCurrentItems = append(summary.AllCurrentItems, currentWorkItem(item, trackingSummaries, trackingAvailable))
		case workitem.LifecyclePaused:
			summary.CurrentCount++
			summary.PausedCount++
			summary.AllCurrentItems = append(summary.AllCurrentItems, currentWorkItem(item, trackingSummaries, trackingAvailable))
		case workitem.LifecycleCompleted:
			summary.CompletedCount++
			summary.ClosedCount++
		case workitem.LifecycleCancelled:
			summary.CancelledCount++
			summary.ClosedCount++
		}
	}

	// Recent current work is a bounded, explainable display policy—not a
	// priority, urgency, risk, or health ranking.
	sort.SliceStable(summary.AllCurrentItems, func(i, j int) bool {
		left := summary.AllCurrentItems[i]
		right := summary.AllCurrentItems[j]
		if !left.UpdatedAt.Equal(right.UpdatedAt) {
			return left.UpdatedAt.After(right.UpdatedAt)
		}
		return left.ID < right.ID
	})
	visibleCount := min(len(summary.AllCurrentItems), currentWorkItemDisplayLimit)
	summary.CurrentItems = append(summary.CurrentItems, summary.AllCurrentItems[:visibleCount]...)
	return summary
}

func currentWorkItem(item workitem.WorkItem, summaries map[string]tracking.WorkItemSummary, available bool) CurrentWorkItem {
	trackingSummary := tracking.WorkItemSummary{WorkItemID: item.ID, Progressions: []tracking.TrackedItemProgression{}}
	if available {
		if existing, ok := summaries[item.ID]; ok {
			trackingSummary = existing
		}
	}
	return CurrentWorkItem{
		ID: item.ID, Title: item.Title, Description: item.Description,
		Lifecycle: item.Lifecycle, UpdatedAt: item.UpdatedAt,
		Tracking: trackingSummary,
	}
}
