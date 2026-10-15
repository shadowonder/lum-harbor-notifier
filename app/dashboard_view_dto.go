package app

import (
	"time"

	"lum-harbor-notifier/internal/dashboardview"
)

// DashboardViewRequestDTO supplies the local-day context for Dashboard's
// existing Planner semantics.
type DashboardViewRequestDTO struct {
	RangeStart string `json:"rangeStart"`
	RangeEnd   string `json:"rangeEnd"`
	Today      string `json:"today"`
	Timezone   string `json:"timezone"`
}

// DashboardViewDTO is the single authoritative Dashboard read contract.
type DashboardViewDTO struct {
	Planner   PlannerViewDTO              `json:"planner"`
	WorkItems DashboardWorkItemSummaryDTO `json:"workItems"`
}

type DashboardWorkItemSummaryDTO struct {
	CurrentCount      int                           `json:"currentCount"`
	PausedCount       int                           `json:"pausedCount"`
	CompletedCount    int                           `json:"completedCount"`
	CancelledCount    int                           `json:"cancelledCount"`
	ClosedCount       int                           `json:"closedCount"`
	CurrentItems      []DashboardCurrentWorkItemDTO `json:"currentItems"`
	AllCurrentItems   []DashboardCurrentWorkItemDTO `json:"allCurrentItems"`
	TrackingAvailable bool                          `json:"trackingAvailable"`
}

// DashboardCurrentWorkItemDTO exposes only compact, navigable product facts.
type DashboardCurrentWorkItemDTO struct {
	ID             string                     `json:"id"`
	Title          string                     `json:"title"`
	Description    *string                    `json:"description"`
	LifecycleState string                     `json:"lifecycleState"`
	UpdatedAt      string                     `json:"updatedAt"`
	Tracking       WorkItemTrackingSummaryDTO `json:"tracking"`
	// WorkflowStartedAt is the authoritative actual Workflow start (RFC 3339) of the
	// WorkItem's single Workflow, or nil when the WorkItem has no Workflow. It is the
	// START of the Active Workflows span; the END is tracking.activeFinalTarget.
	WorkflowStartedAt *string `json:"workflowStartedAt"`
}

func dashboardViewToDTO(view *dashboardview.View) DashboardViewDTO {
	return DashboardViewDTO{
		Planner: plannerViewToDTO(view.Planner),
		WorkItems: DashboardWorkItemSummaryDTO{
			CurrentCount:      view.WorkItems.CurrentCount,
			PausedCount:       view.WorkItems.PausedCount,
			CompletedCount:    view.WorkItems.CompletedCount,
			CancelledCount:    view.WorkItems.CancelledCount,
			ClosedCount:       view.WorkItems.ClosedCount,
			CurrentItems:      dashboardCurrentWorkItemsToDTO(view.WorkItems.CurrentItems),
			AllCurrentItems:   dashboardCurrentWorkItemsToDTO(view.WorkItems.AllCurrentItems),
			TrackingAvailable: view.WorkItems.TrackingAvailable,
		},
	}
}

func dashboardCurrentWorkItemsToDTO(items []dashboardview.CurrentWorkItem) []DashboardCurrentWorkItemDTO {
	dtos := make([]DashboardCurrentWorkItemDTO, 0, len(items))
	for _, item := range items {
		var startedAt *string
		if item.Tracking.Workflow != nil {
			value := trackingTime(item.Tracking.Workflow.StartedAt)
			startedAt = &value
		}
		dtos = append(dtos, DashboardCurrentWorkItemDTO{
			WorkflowStartedAt: startedAt,
			ID:                item.ID, Title: item.Title, Description: item.Description,
			LifecycleState: string(item.Lifecycle),
			UpdatedAt:      item.UpdatedAt.UTC().Format(time.RFC3339Nano),
			Tracking:       trackingSummaryToDTO(item.Tracking),
		})
	}
	return dtos
}
