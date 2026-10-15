package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/plannersync"
	"lum-harbor-notifier/internal/plannerview"
	"lum-harbor-notifier/internal/reminder"
	"lum-harbor-notifier/internal/source"
	"lum-harbor-notifier/internal/temporal"
)

// CompleteLocalTodo completes one locally authoritative Planner Todo and
// returns the committed Planner Item state to the frontend.
//
// App owns transport validation only. Planner Service owns authority and
// lifecycle rules, the transaction boundary, Revision creation, and the
// authoritative database update. Keeping this endpoint thin prevents the
// Wails adapter from becoming a second completion implementation.
func (app *App) CompleteLocalTodo(id string) (PlannerItemDTO, error) {
	if app.plannerService == nil {
		return PlannerItemDTO{}, fmt.Errorf("planner service is not initialized")
	}

	itemID := strings.TrimSpace(id)
	if itemID == "" {
		return PlannerItemDTO{}, fmt.Errorf("planner item ID must not be empty")
	}

	// Wails normally initializes App.ctx during startup. The fallback keeps
	// this thin adapter callable from tests outside the Wails lifecycle.
	ctx := app.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	item, err := app.plannerService.CompleteLocalTodo(ctx, itemID)
	if err != nil {
		return PlannerItemDTO{}, fmt.Errorf("complete local Planner Todo %q: %w", itemID, err)
	}
	if app.trackingReevaluationTrigger != nil {
		if err := app.trackingReevaluationTrigger(); err != nil {
			// Completion is already committed; a periodic/startup cycle will drain
			// the durable request if this non-durable wake-up loses a race.
			slog.Warn("could not trigger tracking reevaluation cycle", "error", err)
		}
	}

	dto := plannerItemToDTO(*item)
	app.enrichPlannerItem(ctx, &dto)
	return dto, nil
}

// CreateLocalTodo creates a local planner todo, optionally with a work item link and reminder offsets.
func (app *App) CreateLocalTodo(request CreateLocalTodoRequestDTO) (PlannerItemDTO, error) {
	if app.plannerService == nil {
		return PlannerItemDTO{}, fmt.Errorf("planner service is not initialized")
	}

	title := strings.TrimSpace(request.Title)
	if title == "" {
		return PlannerItemDTO{}, fmt.Errorf("planner item title must not be empty")
	}

	ctx := app.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	input := planner.CreateLocalTodoInput{
		Title:          title,
		Details:        request.Details,
		WorkItemID:     request.WorkItemID,
		WorkflowID:     request.WorkflowID,
		WorkflowStepID: request.WorkflowStepID,
		TrackedItemID:  request.TrackedItemID,
	}

	if request.DueDate != nil && strings.TrimSpace(*request.DueDate) != "" {
		d, err := temporal.ParseLocalDate(strings.TrimSpace(*request.DueDate))
		if err != nil {
			return PlannerItemDTO{}, fmt.Errorf("invalid due date: %w", err)
		}
		input.DueDate = &d
	}

	if request.DueAt != nil && strings.TrimSpace(*request.DueAt) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(*request.DueAt))
		if err != nil {
			return PlannerItemDTO{}, fmt.Errorf("invalid due at: %w", err)
		}
		input.DueAt = &t
	}

	var sourceRevisionID *string
	if app.sourceService != nil {
		payloadMap := map[string]any{
			"title":   title,
			"details": request.Details,
		}
		if request.WorkItemID != nil {
			payloadMap["workItemId"] = *request.WorkItemID
		}
		if request.WorkflowID != nil {
			payloadMap["workflowId"] = *request.WorkflowID
		}
		if request.WorkflowStepID != nil {
			payloadMap["workflowStepId"] = *request.WorkflowStepID
		}
		if request.TrackedItemID != nil {
			payloadMap["trackedItemId"] = *request.TrackedItemID
		}
		instanceID := "todo-create"
		capabilityID := "planner.todo.create"
		// Scheduling from a WorkItem is a Schedule Action whether or not the
		// WorkItem has a Workflow.
		if request.WorkItemID != nil || request.WorkflowID != nil || request.WorkflowStepID != nil {
			instanceID = "schedule-action"
			capabilityID = "workflow.schedule-action"
		}
		payloadBytes, _ := json.Marshal(payloadMap)
		rev, err := app.sourceService.CaptureIntentSource(appContext(ctx), source.CaptureIntentInput{
			ExtensionID:    "core.manual",
			InstanceID:     instanceID,
			CapabilityID:   capabilityID,
			SourceKind:     "MANUAL_INTENT",
			Payload:        payloadBytes,
			ProcessingMode: source.ProcessingModeDirectMutation,
		})
		if err == nil && rev != nil {
			sourceRevisionID = &rev.ID
		}
	}

	input.SourceRevisionID = sourceRevisionID

	item, err := app.plannerService.CreateLocalTodo(ctx, input)
	if err != nil {
		if sourceRevisionID != nil && app.sourceService != nil {
			_ = app.sourceService.FailProcessing(appContext(ctx), *sourceRevisionID, err.Error())
		}
		return PlannerItemDTO{}, fmt.Errorf("create local planner todo: %w", err)
	}

	if len(request.OffsetsSeconds) > 0 && app.reminderService != nil {
		_, err := app.reminderService.SetReminder(ctx, reminder.SetReminderInput{
			PlannerItemID:        item.ID,
			OffsetsSeconds:       request.OffsetsSeconds,
			ConfiguredAtCreation: true,
		})
		if err != nil {
			return PlannerItemDTO{}, fmt.Errorf("set todo reminder: %w", err)
		}
		app.triggerTodoReminderEvaluation()
	}

	dto := plannerItemToDTO(*item)
	app.enrichPlannerItem(ctx, &dto)
	return dto, nil
}

// SetTodoReminder sets or updates reminder threshold offsets for a Todo.
func (app *App) SetTodoReminder(request SetTodoReminderRequestDTO) (TodoReminderDTO, error) {
	if app.reminderService == nil {
		return TodoReminderDTO{}, fmt.Errorf("reminder service is not initialized")
	}

	plannerItemID := strings.TrimSpace(request.PlannerItemID)
	if plannerItemID == "" {
		return TodoReminderDTO{}, fmt.Errorf("planner item ID must not be empty")
	}

	ctx := app.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	rem, err := app.reminderService.SetReminder(ctx, reminder.SetReminderInput{
		PlannerItemID:  plannerItemID,
		OffsetsSeconds: request.OffsetsSeconds,
	})
	if err != nil {
		return TodoReminderDTO{}, fmt.Errorf("set todo reminder: %w", err)
	}
	app.triggerTodoReminderEvaluation()

	return TodoReminderDTO{
		ID:             rem.ID,
		PlannerItemID:  rem.PlannerItemID,
		State:          string(rem.State),
		OffsetsSeconds: rem.OffsetsSeconds,
		FiredOffsets:   rem.FiredOffsets,
		RemindMode:     string(rem.RemindMode),
	}, nil
}

func (app *App) triggerTodoReminderEvaluation() {
	if app.todoReminderTrigger == nil {
		return
	}
	// Scheduler Trigger is coalesced and non-durable. The reminder row is
	// already committed, and the periodic cycle remains the recovery path.
	if err := app.todoReminderTrigger(); err != nil {
		slog.Warn("could not trigger todo reminder cycle", "error", err)
	}
}

// RemoveTodoReminder removes an active reminder and resolves any associated open attention issues.
func (app *App) RemoveTodoReminder(plannerItemID string) error {
	if app.reminderService == nil {
		return fmt.Errorf("reminder service is not initialized")
	}

	itemID := strings.TrimSpace(plannerItemID)
	if itemID == "" {
		return fmt.Errorf("planner item ID must not be empty")
	}

	ctx := app.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	if err := app.reminderService.RemoveReminder(ctx, itemID); err != nil {
		return fmt.Errorf("remove todo reminder: %w", err)
	}

	if app.attentionService != nil {
		if err := app.attentionService.ResolveByPlannerItem(ctx, itemID); err != nil {
			return fmt.Errorf("resolve attention issues on reminder removal: %w", err)
		}
	}

	return nil
}

// GetPlannerItemHistory queries canonical history events correlated to a planner item.
func (app *App) GetPlannerItemHistory(plannerItemID string) ([]PlannerRecentChangeDTO, error) {
	if app.historyService == nil {
		return nil, fmt.Errorf("history service is not initialized")
	}

	itemID := strings.TrimSpace(plannerItemID)
	if itemID == "" {
		return nil, fmt.Errorf("planner item ID must not be empty")
	}

	ctx := app.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	page, err := app.historyService.QueryPlannerItem(ctx, itemID)
	if err != nil {
		return nil, fmt.Errorf("query planner item history: %w", err)
	}

	result := make([]PlannerRecentChangeDTO, 0, len(page.Events))
	for _, ev := range page.Events {
		result = append(result, PlannerRecentChangeDTO{
			EventID:    ev.ID,
			EventType:  string(ev.Type),
			Summary:    ev.Summary,
			OccurredAt: ev.OccurredAt.UTC().Format(time.RFC3339Nano),
			Actor:      string(ev.Actor),
		})
	}
	return result, nil
}

// GetPlannerView loads the complete read model required to render the
// Planner page.
//
// RangeStart and RangeEnd describe the visible Calendar date range using
// half-open semantics:
//
//	[RangeStart, RangeEnd)
//
// Today is intentionally independent from the visible Calendar range.
// A user may browse another month while CurrentOpenItems and
// CompletedTodayItems must continue to represent the user's actual current
// day.
//
// Timezone must be an IANA timezone identifier such as America/Chicago.
// It is used when date-only boundaries need to be translated into exact
// timestamp boundaries for database queries.
//
// This method belongs to the Wails adapter layer. It parses frontend transport
// values into strongly typed application values, delegates page-level
// projection behavior to plannerview.Service, and converts the result back
// into frontend DTOs.
func (app *App) GetPlannerView(
	request PlannerViewRequestDTO,
) (PlannerViewDTO, error) {

	if app.plannerViewService == nil {
		return PlannerViewDTO{}, fmt.Errorf(
			"planner view service is not initialized",
		)
	}

	// Wails normally initializes App.ctx during application startup.
	//
	// Falling back to a background context also allows this adapter method to
	// remain usable from tests that do not execute the Wails lifecycle.
	ctx := app.ctx
	if ctx == nil {
		ctx = context.Background()
	}

	plannerRequest, err := parsePlannerViewRequest(
		request.RangeStart,
		request.RangeEnd,
		request.Today,
		request.Timezone,
	)
	if err != nil {
		return PlannerViewDTO{}, err
	}

	// PlannerView Service owns the page-level aggregation across the Calendar
	// and Planner domains. App does not query repositories directly.
	view, err := app.plannerViewService.Get(
		ctx,
		plannerRequest,
	)
	if err != nil {
		return PlannerViewDTO{}, fmt.Errorf(
			"get Planner View: %w",
			err,
		)
	}

	dto := plannerViewToDTO(view)
	dto.CalendarItems = app.enrichPlannerItems(ctx, dto.CalendarItems)
	dto.CurrentOpenItems = app.enrichPlannerItems(ctx, dto.CurrentOpenItems)
	dto.CompletedItems = app.enrichPlannerItems(ctx, dto.CompletedItems)
	dto.CompletedTodayItems = app.enrichPlannerItems(ctx, dto.CompletedTodayItems)
	return dto, nil
}

func (app *App) enrichPlannerItem(ctx context.Context, dto *PlannerItemDTO) {
	if dto == nil {
		return
	}
	if dto.Authority == string(planner.AuthorityExternal) {
		dto.ExternalOrigin = app.plannerExternalOrigin(ctx, dto.ID)
	}
	if app.reminderService != nil {
		rem, err := app.reminderService.GetByPlannerItemID(ctx, dto.ID)
		if err == nil && rem != nil && rem.State != reminder.StateCancelled {
			dto.Reminder = &TodoReminderDTO{
				ID:             rem.ID,
				PlannerItemID:  rem.PlannerItemID,
				State:          string(rem.State),
				OffsetsSeconds: rem.OffsetsSeconds,
				FiredOffsets:   rem.FiredOffsets,
				RemindMode:     string(rem.RemindMode),
			}
		}
	}
	if app.historyService != nil {
		page, err := app.historyService.QueryPlannerItem(ctx, dto.ID)
		if err == nil && len(page.Events) > 0 {
			changes := make([]PlannerRecentChangeDTO, 0, len(page.Events))
			for _, ev := range page.Events {
				changes = append(changes, PlannerRecentChangeDTO{
					EventID:    ev.ID,
					EventType:  string(ev.Type),
					Summary:    ev.Summary,
					OccurredAt: ev.OccurredAt.UTC().Format(time.RFC3339Nano),
					Actor:      string(ev.Actor),
				})
			}
			dto.RecentChanges = changes
		}
	}
	if app.workitemService != nil && dto.WorkItemID != nil && *dto.WorkItemID != "" {
		wi, err := app.workitemService.GetByID(ctx, *dto.WorkItemID)
		if err == nil && wi != nil {
			dto.WorkItemTitle = &wi.Title
		}
	}
	if app.trackingService != nil {
		if dto.TrackedItemID != nil && *dto.TrackedItemID != "" {
			item, err := app.trackingService.GetTrackedItem(ctx, *dto.TrackedItemID)
			if err == nil && item != nil {
				dto.TrackedItemTitle = &item.Title
			}
		}
		if dto.WorkflowID != nil && *dto.WorkflowID != "" {
			wf, err := app.trackingService.GetWorkflow(ctx, *dto.WorkflowID)
			if err == nil && wf != nil {
				def, err := app.trackingService.GetWorkflowDefinition(ctx, wf.WorkflowDefinitionID)
				if err == nil && def != nil {
					dto.WorkflowName = &def.Name
				}
			}
		}
		if dto.WorkflowStepID != nil && *dto.WorkflowStepID != "" {
			step, err := app.trackingService.GetWorkflowStep(ctx, *dto.WorkflowStepID)
			if err == nil && step != nil {
				dto.WorkflowStepTitle = &step.Title
			}
		}
	}
}

// plannerExternalOrigin resolves read-only provenance for one EXTERNAL item:
// its AUTHORITATIVE external identity names the external system and Plugin
// Instance, and the matching SourceRecord names the producing extension.
// Unresolvable parts are omitted; provenance is a presentation aid, never a
// gate.
func (app *App) plannerExternalOrigin(ctx context.Context, itemID string) *PlannerExternalOriginDTO {
	if app.plannerService == nil {
		return nil
	}
	ref, err := app.plannerService.GetExternalIdentity(ctx, itemID)
	if err != nil || ref == nil {
		return nil
	}
	described, ok := plannersync.DescribeExternalSystem(ref.System)
	if !ok {
		return &PlannerExternalOriginDTO{SystemLabel: ref.System}
	}
	instanceID := described.InstanceID
	origin := &PlannerExternalOriginDTO{SystemLabel: described.Label, InstanceID: &instanceID}
	if app.sourceService == nil {
		return origin
	}
	for _, extensionID := range described.ExtensionIDs {
		record, err := app.sourceService.GetSourceRecordByIdentity(ctx, extensionID, described.InstanceID, described.SourceKind, ref.ExternalID)
		if err != nil || record == nil {
			continue
		}
		extID := record.ExtensionID
		origin.ExtensionID = &extID
		if app.pluginInventory != nil {
			if name := app.pluginInventory.ExtensionName(extID); name != "" {
				origin.ProviderName = &name
			}
		}
		break
	}
	return origin
}

func (app *App) enrichPlannerItems(ctx context.Context, dtos []PlannerItemDTO) []PlannerItemDTO {
	for i := range dtos {
		app.enrichPlannerItem(ctx, &dtos[i])
	}
	return dtos
}

func parsePlannerViewRequest(rangeStartValue, rangeEndValue, todayValue, timezoneValue string) (plannerview.Request, error) {
	// Date-only Planner and Calendar semantics stay timezone-free until the
	// application projection applies the caller's actual IANA location.
	rangeStart, err := temporal.ParseLocalDate(strings.TrimSpace(rangeStartValue))
	if err != nil {
		return plannerview.Request{}, fmt.Errorf("invalid Planner View range start %q: %w", rangeStartValue, err)
	}
	rangeEnd, err := temporal.ParseLocalDate(strings.TrimSpace(rangeEndValue))
	if err != nil {
		return plannerview.Request{}, fmt.Errorf("invalid Planner View range end %q: %w", rangeEndValue, err)
	}
	today, err := temporal.ParseLocalDate(strings.TrimSpace(todayValue))
	if err != nil {
		return plannerview.Request{}, fmt.Errorf("invalid Planner View current date %q: %w", todayValue, err)
	}
	timezone := strings.TrimSpace(timezoneValue)
	if timezone == "" {
		return plannerview.Request{}, fmt.Errorf("Planner View timezone must not be empty")
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return plannerview.Request{}, fmt.Errorf("load Planner View timezone %q: %w", timezone, err)
	}
	return plannerview.Request{
		RangeStart: rangeStart,
		RangeEnd:   rangeEnd,
		Today:      today,
		Location:   location,
	}, nil
}
