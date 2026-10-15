package app

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

// GetWorkItemTracking returns the whole semantic Tracking view of one
// WorkItem in one call (TrackedItems, pinned workflows with ordered steps,
// authoritative step states, derived progression, MonitorPoints). A valid
// WorkItem without tracking data succeeds with empty collections; an unknown or
// archived WorkItem is an error.
func (app *App) GetWorkItemTracking(workItemID string) (WorkItemTrackingDTO, error) {
	if app.trackingService == nil {
		return WorkItemTrackingDTO{}, fmt.Errorf("tracking service is not initialized")
	}
	view, err := app.trackingService.GetWorkItemTracking(appContext(app.ctx), workItemID)
	if err != nil {
		return WorkItemTrackingDTO{}, trackingTransportError("load work item tracking", err)
	}
	dto := workItemTrackingToDTO(*view)
	if app.historyService != nil {
		page, historyErr := app.historyService.QueryWorkItem(appContext(app.ctx), workItemID)
		if historyErr != nil {
			return WorkItemTrackingDTO{}, fmt.Errorf("load tracking history: %w", historyErr)
		}
		dto.History, historyErr = historyEventsToDTO(page.Events)
		if historyErr != nil {
			return WorkItemTrackingDTO{}, fmt.Errorf("map tracking history: %w", historyErr)
		}
	}
	if app.attentionService != nil {
		issues, attentionErr := app.attentionService.ListAttentionIssues(appContext(app.ctx))
		if attentionErr != nil {
			return WorkItemTrackingDTO{}, fmt.Errorf("load tracking attention: %w", attentionErr)
		}
		for _, issue := range issues {
			if issue.WorkItemID == nil || *issue.WorkItemID != workItemID || issue.SubjectTrackedItemID == nil || issue.MonitorPointID == nil {
				continue
			}
			dto.AttentionIssues = append(dto.AttentionIssues, TrackedItemAttentionDTO{ID: issue.ID, SubjectTrackedItemID: *issue.SubjectTrackedItemID, MonitorPointID: *issue.MonitorPointID, State: string(issue.State), Severity: string(issue.Severity), Reason: issue.Reason, RecommendedAction: issue.RecommendedAction, SnoozedUntil: formatDTOTime(issue.SnoozedUntil), ResolvedAt: formatDTOTime(issue.ResolvedAt)})
		}
	}
	return dto, nil
}

// ListActiveWorkflowDefinitions lists definitions a new TrackedItem can bind to.
func (app *App) ListActiveWorkflowDefinitions() ([]WorkflowDefinitionDTO, error) {
	if app.trackingService == nil {
		return nil, fmt.Errorf("tracking service is not initialized")
	}
	defs, err := app.trackingService.ListActiveWorkflowDefinitions(appContext(app.ctx))
	if err != nil {
		return nil, trackingTransportError("list workflow definitions", err)
	}
	result := make([]WorkflowDefinitionDTO, 0, len(defs))
	for _, def := range defs {
		result = append(result, workflowToDTO(def))
	}
	return result, nil
}

// CreateWorkflowDefinition creates a versioned, immutable process definition.
func (app *App) CreateWorkflowDefinition(request CreateWorkflowDefinitionRequestDTO) (WorkflowDefinitionDTO, error) {
	if app.trackingService == nil {
		return WorkflowDefinitionDTO{}, fmt.Errorf("tracking service is not initialized")
	}
	steps := make([]tracking.WorkflowStepInput, 0, len(request.Steps))
	for _, s := range request.Steps {
		steps = append(steps, tracking.WorkflowStepInput{Key: s.Key, Title: s.Title, Terminal: s.Terminal})
	}
	def, err := app.trackingService.CreateWorkflowDefinition(appContext(app.ctx), tracking.CreateWorkflowDefinitionInput{
		Name: request.Name, Version: request.Version, Steps: steps,
	})
	if err != nil {
		return WorkflowDefinitionDTO{}, trackingTransportError("create workflow definition", err)
	}
	return workflowToDTO(*def), nil
}

// CreateWorkflow creates the single Workflow for a WorkItem.
func (app *App) CreateWorkflow(request CreateWorkflowRequestDTO) (WorkflowDTO, error) {
	if app.trackingService == nil {
		return WorkflowDTO{}, fmt.Errorf("tracking service is not initialized")
	}
	input := tracking.CreateWorkflowInput{
		WorkItemID:           request.WorkItemID,
		WorkflowDefinitionID: request.WorkflowDefinitionID,
		CurrentStepID:        request.CurrentStepID,
	}
	if request.StartedAt != nil && strings.TrimSpace(*request.StartedAt) != "" {
		startedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(*request.StartedAt))
		if err != nil {
			return WorkflowDTO{}, fmt.Errorf("workflow start must be an RFC 3339 timestamp")
		}
		input.StartedAt = &startedAt
	}
	wf, err := app.trackingService.CreateWorkflow(appContext(app.ctx), input)
	if err != nil {
		return WorkflowDTO{}, trackingTransportError("create workflow", err)
	}
	def, err := app.trackingService.GetWorkflowDefinition(appContext(app.ctx), wf.WorkflowDefinitionID)
	if err != nil {
		return WorkflowDTO{}, trackingTransportError("load workflow definition", err)
	}
	view := &tracking.WorkflowView{
		Workflow:   *wf,
		Definition: *def,
	}
	return *workflowViewToDTO(view), nil
}

// SetWorkflowCurrentStep explicitly moves the Workflow's own current step
// (forward or backward). It never changes any TrackedItem state.
func (app *App) SetWorkflowCurrentStep(request SetWorkflowCurrentStepRequestDTO) (WorkflowDTO, error) {
	if app.trackingService == nil {
		return WorkflowDTO{}, fmt.Errorf("tracking service is not initialized")
	}
	wf, err := app.trackingService.SetWorkflowCurrentStep(appContext(app.ctx), tracking.SetWorkflowCurrentStepInput{
		WorkflowID: request.WorkflowID, StepID: request.StepID,
	})
	if err != nil {
		return WorkflowDTO{}, trackingTransportError("set workflow current step", err)
	}
	def, err := app.trackingService.GetWorkflowDefinition(appContext(app.ctx), wf.WorkflowDefinitionID)
	if err != nil {
		return WorkflowDTO{}, trackingTransportError("load workflow definition", err)
	}
	return *workflowViewToDTO(&tracking.WorkflowView{Workflow: *wf, Definition: *def}), nil
}

// CreateTrackedItem creates a TrackedItem (and its NOT_STARTED step states
// for the WorkItem's single workflow). The returned DTO has no step states;
// callers re-read GetWorkItemTracking for the authoritative view.
func (app *App) CreateTrackedItem(request CreateTrackedItemRequestDTO) (TrackedItemDTO, error) {
	if app.trackingService == nil {
		return TrackedItemDTO{}, fmt.Errorf("tracking service is not initialized")
	}
	item, err := app.trackingService.CreateTrackedItem(appContext(app.ctx), tracking.CreateTrackedItemInput{
		WorkItemID: request.WorkItemID, WorkflowID: request.WorkflowID,
		ParentItemID: request.ParentItemID, Title: request.Title,
		ItemType: request.ItemType,
	})
	if err != nil {
		return TrackedItemDTO{}, trackingTransportError("create tracked item", err)
	}
	return trackedItemToDTO(tracking.TrackedItemView{Item: *item}), nil
}

// SetTrackedItemStepState sets one step's semantic state.
func (app *App) SetTrackedItemStepState(request SetTrackedItemStepStateRequestDTO) (StepStateDTO, error) {
	if app.trackingService == nil {
		return StepStateDTO{}, fmt.Errorf("tracking service is not initialized")
	}
	state, err := app.trackingService.SetTrackedItemStepState(appContext(app.ctx), tracking.SetStepStateInput{
		TrackedItemID: request.TrackedItemID, WorkflowStepID: request.WorkflowStepID, State: tracking.StepState(request.State),
	})
	if err != nil {
		return StepStateDTO{}, trackingTransportError("set step state", err)
	}
	return stepStateToDTO(*state), nil
}

// CreateMonitorPoint creates an authoritative expectation. Nothing evaluates
// it automatically.
func (app *App) CreateMonitorPoint(request CreateMonitorPointRequestDTO) (MonitorPointDTO, error) {
	if app.trackingService == nil {
		return MonitorPointDTO{}, fmt.Errorf("tracking service is not initialized")
	}
	dueAt, err := time.Parse(time.RFC3339, strings.TrimSpace(request.DueAt))
	if err != nil {
		return MonitorPointDTO{}, fmt.Errorf("due time must be an RFC 3339 timestamp")
	}
	input := tracking.CreateMonitorPointInput{
		WorkItemID: request.WorkItemID, SubjectTrackedItemID: request.SubjectTrackedItemID,
		Kind: tracking.MonitorKind(request.Kind), Title: request.Title, DueAt: dueAt, Timezone: request.Timezone,
		ExpectedStepID: request.ExpectedStepID,
	}
	if request.ExpectedWorkItemLifecycle != nil {
		lifecycle := workitem.LifecycleState(*request.ExpectedWorkItemLifecycle)
		input.ExpectedWorkItemLifecycle = &lifecycle
	}
	point, err := app.trackingService.CreateMonitorPoint(appContext(app.ctx), input)
	if err != nil {
		return MonitorPointDTO{}, trackingTransportError("create monitor point", err)
	}
	return monitorPointToDTO(*point), nil
}

// SetMonitorPointState records an explicit ACTIVE→SATISFIED or ACTIVE→CANCELLED
// decision.
func (app *App) SetMonitorPointState(request SetMonitorPointStateRequestDTO) (MonitorPointDTO, error) {
	if app.trackingService == nil {
		return MonitorPointDTO{}, fmt.Errorf("tracking service is not initialized")
	}
	point, err := app.trackingService.SetMonitorPointState(appContext(app.ctx), tracking.SetMonitorPointStateInput{
		ID: request.ID, Target: tracking.MonitorState(request.TargetState),
	})
	if err != nil {
		return MonitorPointDTO{}, trackingTransportError("set monitor point state", err)
	}
	return monitorPointToDTO(*point), nil
}

// trackingTransportError maps domain errors to safe, user-presentable messages.
// Unexpected failures are logged and never leak internals.
func trackingTransportError(operation string, err error) error {
	switch {
	case errors.Is(err, tracking.ErrWorkItemUnavailable):
		return fmt.Errorf("the work item is not available")
	case errors.Is(err, tracking.ErrTrackedItemNotFound):
		return fmt.Errorf("the tracked item was not found")
	case errors.Is(err, tracking.ErrMonitorPointNotFound):
		return fmt.Errorf("the monitor point was not found")
	case errors.Is(err, tracking.ErrWorkflowNotFound):
		return fmt.Errorf("the workflow definition was not found")
	case errors.Is(err, tracking.ErrWorkflowInactive):
		return fmt.Errorf("the workflow definition is not active")
	case errors.Is(err, tracking.ErrInvalidParent):
		return fmt.Errorf("the parent or subject must be an existing tracked item of the same work item")
	case errors.Is(err, tracking.ErrNoWorkflow):
		return fmt.Errorf("the tracked item has no workflow")
	case errors.Is(err, tracking.ErrStepNotInWorkflow):
		return fmt.Errorf("the step does not belong to the tracked item's workflow")
	case errors.Is(err, tracking.ErrFinalTargetExists):
		return fmt.Errorf("the subject already has an active final target")
	case errors.Is(err, tracking.ErrInvalidMonitorState):
		return fmt.Errorf("that monitor point state change is not allowed")
	case errors.Is(err, tracking.ErrInvalidInput):
		return err
	}
	slog.Error("tracking operation failed", "operation", operation, "error", err)
	return fmt.Errorf("%s failed", operation)
}
