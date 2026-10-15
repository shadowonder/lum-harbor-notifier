package app

import (
	"context"
	"encoding/json"
	"fmt"

	"lum-harbor-notifier/internal/source"
	"lum-harbor-notifier/internal/workitem"
)

// GetWorkItems returns the current non-archived WorkItems in Repository-owned
// deterministic order.
func (app *App) GetWorkItems() ([]WorkItemDTO, error) {
	if app.workitemService == nil {
		return nil, fmt.Errorf("work item service is not initialized")
	}
	items, err := app.workitemService.List(appContext(app.ctx))
	if err != nil {
		return nil, fmt.Errorf("list WorkItems: %w", err)
	}
	return workItemsToDTO(items), nil
}

// GetWorkItemOverview returns the bounded cross-domain list projection. The
// existing GetWorkItems endpoint remains the domain-shaped compatibility read
// until the frontend is wired in the next slice.
func (app *App) GetWorkItemOverview() (WorkItemOverviewDTO, error) {
	if app.workItemOverviewService == nil {
		return WorkItemOverviewDTO{}, fmt.Errorf("work item overview service is not initialized")
	}
	view, err := app.workItemOverviewService.List(appContext(app.ctx))
	if err != nil {
		return WorkItemOverviewDTO{}, fmt.Errorf("get WorkItem overview: %w", err)
	}
	return workItemOverviewToDTO(view), nil
}

// CreateWorkItem delegates all identity, lifecycle, Revision, and transaction
// decisions to the WorkItem Service and returns its committed domain state.
func (app *App) CreateWorkItem(request CreateWorkItemRequestDTO) (WorkItemDTO, error) {
	if app.workitemService == nil {
		return WorkItemDTO{}, fmt.Errorf("work item service is not initialized")
	}
	var sourceRevisionID *string
	if app.sourceService != nil {
		payloadBytes, _ := json.Marshal(map[string]any{
			"title":       request.Title,
			"description": request.Description,
		})
		rev, err := app.sourceService.CaptureIntentSource(appContext(app.ctx), source.CaptureIntentInput{
			ExtensionID:    "core.manual",
			InstanceID:     "workitem-create",
			CapabilityID:   "workitem.create",
			SourceKind:     "MANUAL_INTENT",
			Payload:        payloadBytes,
			ProcessingMode: source.ProcessingModeDirectMutation,
		})
		if err == nil && rev != nil {
			sourceRevisionID = &rev.ID
		}
	}
	item, err := app.workitemService.CreateLocalWorkItem(
		appContext(app.ctx),
		workitem.CreateLocalWorkItemInput{
			Title:            request.Title,
			Description:      request.Description,
			SourceRevisionID: sourceRevisionID,
		},
	)
	if err != nil {
		if sourceRevisionID != nil && app.sourceService != nil {
			_ = app.sourceService.FailProcessing(appContext(app.ctx), *sourceRevisionID, err.Error())
		}
		return WorkItemDTO{}, fmt.Errorf("create WorkItem: %w", err)
	}
	return workItemToDTO(*item), nil
}

// SetWorkItemLifecycleState delegates transition validation, Revision
// creation, completion timestamp semantics, and transaction ownership to the
// WorkItem Service. The response is the authoritative committed state (or the
// unchanged authoritative state for a same-state no-op).
func (app *App) SetWorkItemLifecycleState(request SetWorkItemLifecycleStateRequestDTO) (WorkItemDTO, error) {
	if app.workitemService == nil {
		return WorkItemDTO{}, fmt.Errorf("work item service is not initialized")
	}
	item, err := app.workitemService.SetLocalWorkItemLifecycleState(
		appContext(app.ctx),
		workitem.SetLocalWorkItemLifecycleStateInput{
			ID: request.ID, Target: workitem.LifecycleState(request.TargetLifecycleState),
		},
	)
	if err != nil {
		return WorkItemDTO{}, fmt.Errorf("set WorkItem lifecycle: %w", err)
	}
	return workItemToDTO(*item), nil
}

func appContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
