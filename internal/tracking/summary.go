package tracking

import (
	"context"
	"fmt"
	"time"

	dbgen "lum-harbor-notifier/internal/database/generated"
)

// WorkItemSummary is the small Tracking-owned fact set used by cross-domain
// overview projections. Progressions remain per TrackedItem: Tracking does not
// invent a single WorkItem-wide current stage.
type WorkItemSummary struct {
	WorkItemID       string
	TrackedItemCount int
	// WorkflowBoundItemCount is retained for the existing Dashboard compact
	// projection; the Work Items Overview no longer transports this wording.
	WorkflowBoundItemCount int
	Workflow               *OverviewWorkflow
	Items                  []OverviewTrackedItem
	StateDistribution      OverviewStateDistribution
	// Progressions remains the compact Dashboard input. The Work Items Overview
	// uses Items and Workflow instead of exposing the old progress-bar contract.
	Progressions      []TrackedItemProgression
	ActiveFinalTarget *MonitorPoint
}

// OverviewWorkflow carries the Workflow's own persisted position and start.
// CurrentStep is the authoritative Workflow.current_step, never derived from
// TrackedItems.
type OverviewWorkflow struct {
	ID          string
	Definition  WorkflowDefinition
	StartedAt   time.Time
	CurrentStep WorkflowStep
}

type OverviewTrackedItem struct {
	ID           string
	Title        string
	ItemType     *string
	ParentItemID *string
	Lifecycle    ItemLifecycle
	Progression  Progression
	Monitoring   OverviewItemMonitoring
}

// OverviewItemMonitoring is bounded Tracking truth for Overview signal
// composition. ACTIVE means monitoring context exists; SATISFIED is the only
// positive expectation fact. CANCELLED monitors do not imply either.
type OverviewItemMonitoring struct {
	HasActiveExpectation    bool
	HasSatisfiedExpectation bool
}

type OverviewStateDistribution struct {
	NotStarted int
	InProgress int
	Waiting    int
	Blocked    int
	Failed     int
	Unknown    int
	Skipped    int
	Completed  int
}

// TrackedItemProgression identifies the workflow-bound item whose position was
// derived by ComputeProgression. Parent identity is retained without flattening
// the hierarchy into a presentation model.
type TrackedItemProgression struct {
	TrackedItemID        string
	ParentItemID         *string
	WorkflowDefinitionID string
	Progression          Progression
}

type workItemSummaryRows struct {
	workflows   []dbgen.Workflow
	definitions []dbgen.WorkflowDefinition
	items       []dbgen.TrackedItem
	steps       []dbgen.WorkflowStep
	states      []dbgen.TrackedItemStepState
	monitors    []dbgen.MonitorPoint
}

// loadWorkItemSummaryRows performs fixed queries independent of the
// number of WorkItems. Rows are limited to non-archived WorkItems and entities.
func (r *Repository) loadWorkItemSummaryRows(ctx context.Context) (workItemSummaryRows, error) {
	wfs, err := r.queries.ListWorkflowsForWorkItemOverview(ctx)
	if err != nil {
		return workItemSummaryRows{}, fmt.Errorf("list overview workflows: %w", err)
	}
	definitions, err := r.queries.ListWorkflowDefinitionsForOverview(ctx)
	if err != nil {
		return workItemSummaryRows{}, fmt.Errorf("list overview workflow definitions: %w", err)
	}
	items, err := r.queries.ListTrackedItemsForWorkItemOverview(ctx)
	if err != nil {
		return workItemSummaryRows{}, fmt.Errorf("list overview tracked items: %w", err)
	}
	steps, err := r.queries.ListWorkflowStepsForWorkItemOverview(ctx)
	if err != nil {
		return workItemSummaryRows{}, fmt.Errorf("list overview workflow steps: %w", err)
	}
	states, err := r.queries.ListStepStatesForWorkItemOverview(ctx)
	if err != nil {
		return workItemSummaryRows{}, fmt.Errorf("list overview step states: %w", err)
	}
	monitors, err := r.queries.ListMonitorPointsForOverview(ctx)
	if err != nil {
		return workItemSummaryRows{}, fmt.Errorf("list overview monitor points: %w", err)
	}
	return workItemSummaryRows{workflows: wfs, definitions: definitions, items: items, steps: steps, states: states, monitors: monitors}, nil
}

// ListWorkItemSummaries returns Tracking facts keyed by WorkItem ID. A missing
// key means a valid empty Tracking summary; callers own which WorkItem IDs are
// in their product projection.
func (s *Service) ListWorkItemSummaries(ctx context.Context) (map[string]WorkItemSummary, error) {
	rows, err := NewRepository(s.db).loadWorkItemSummaryRows(ctx)
	if err != nil {
		return nil, err
	}
	workflowsByWorkItem := make(map[string]dbgen.Workflow, len(rows.workflows))
	for _, wf := range rows.workflows {
		workflowsByWorkItem[wf.WorkItemID] = wf
	}
	stepsByDefinition := make(map[string][]WorkflowStep)
	for _, row := range rows.steps {
		step := stepFromDB(row)
		stepsByDefinition[step.WorkflowDefinitionID] = append(stepsByDefinition[step.WorkflowDefinitionID], step)
	}
	definitionsByID := make(map[string]WorkflowDefinition, len(rows.definitions))
	for _, row := range rows.definitions {
		definition, err := definitionFromDB(row)
		if err != nil {
			return nil, err
		}
		definition.Steps = stepsByDefinition[definition.ID]
		definitionsByID[definition.ID] = *definition
	}
	statesByItem := make(map[string][]StepStateRecord)
	for _, row := range rows.states {
		state, err := stepStateFromDB(row)
		if err != nil {
			return nil, err
		}
		statesByItem[state.TrackedItemID] = append(statesByItem[state.TrackedItemID], *state)
	}
	monitoringByItem := make(map[string]OverviewItemMonitoring)
	var workItemTargets []MonitorPoint
	for _, row := range rows.monitors {
		point, err := monitorFromDB(row)
		if err != nil {
			return nil, err
		}
		if point.Subject.Kind == SubjectWorkItem {
			if point.Kind == MonitorFinalTarget && point.State == MonitorActive {
				workItemTargets = append(workItemTargets, *point)
			}
			continue
		}
		monitoring := monitoringByItem[point.Subject.ID]
		switch point.State {
		case MonitorActive:
			monitoring.HasActiveExpectation = true
		case MonitorSatisfied:
			monitoring.HasSatisfiedExpectation = true
		}
		monitoringByItem[point.Subject.ID] = monitoring
	}

	summaries := make(map[string]WorkItemSummary)
	for workItemID, row := range workflowsByWorkItem {
		definition, ok := definitionsByID[row.WorkflowDefinitionID]
		if !ok {
			return nil, fmt.Errorf("%w: missing workflow definition %s", ErrInconsistentState, row.WorkflowDefinitionID)
		}
		wf, err := workflowFromDB(row)
		if err != nil {
			return nil, err
		}
		view := WorkflowView{Workflow: *wf, Definition: definition}
		current, ok := view.CurrentStep()
		if !ok {
			return nil, fmt.Errorf("%w: workflow %s current step %s is not in its definition", ErrInconsistentState, row.ID, row.CurrentStepID)
		}
		summaries[workItemID] = WorkItemSummary{
			WorkItemID: workItemID,
			Workflow:   &OverviewWorkflow{ID: row.ID, Definition: definition, StartedAt: wf.StartedAt, CurrentStep: current},
			Items:      []OverviewTrackedItem{}, Progressions: []TrackedItemProgression{},
		}
	}
	for _, row := range rows.items {
		item, err := itemFromDB(row)
		if err != nil {
			return nil, err
		}
		summary := summaries[item.WorkItemID]
		summary.WorkItemID = item.WorkItemID
		summary.TrackedItemCount++
		if wf, ok := workflowsByWorkItem[item.WorkItemID]; ok {
			summary.WorkflowBoundItemCount++
			progression, err := ComputeProgression(stepsByDefinition[wf.WorkflowDefinitionID], statesByItem[item.ID])
			if err != nil {
				return nil, fmt.Errorf("overview progression for item %s: %w", item.ID, err)
			}
			summary.Progressions = append(summary.Progressions, TrackedItemProgression{
				TrackedItemID: item.ID, ParentItemID: item.ParentItemID,
				WorkflowDefinitionID: wf.WorkflowDefinitionID, Progression: progression,
			})
			summary.Items = append(summary.Items, OverviewTrackedItem{
				ID: item.ID, Title: item.Title, ItemType: item.ItemType,
				ParentItemID: item.ParentItemID, Lifecycle: item.Lifecycle, Progression: progression,
				Monitoring: monitoringByItem[item.ID],
			})
			incrementOverviewState(&summary.StateDistribution, progression)
		}
		summaries[item.WorkItemID] = summary
	}
	for _, point := range workItemTargets {
		summary := summaries[point.WorkItemID]
		summary.WorkItemID = point.WorkItemID
		// The domain invariant permits at most one active FINAL_TARGET for the
		// WorkItem subject. Fail closed if persisted data violates it.
		if summary.ActiveFinalTarget != nil {
			return nil, fmt.Errorf("%w: multiple active WorkItem final targets for %s", ErrInconsistentState, point.WorkItemID)
		}
		target := point
		summary.ActiveFinalTarget = &target
		summaries[point.WorkItemID] = summary
	}
	return summaries, nil
}

func incrementOverviewState(distribution *OverviewStateDistribution, progression Progression) {
	if progression.Complete {
		distribution.Completed++
		return
	}
	if progression.CurrentState == nil {
		distribution.Unknown++
		return
	}
	switch *progression.CurrentState {
	case StepNotStarted:
		distribution.NotStarted++
	case StepInProgress:
		distribution.InProgress++
	case StepWaiting:
		distribution.Waiting++
	case StepBlocked:
		distribution.Blocked++
	case StepFailed:
		distribution.Failed++
	case StepUnknown:
		distribution.Unknown++
	case StepSkipped:
		distribution.Skipped++
	case StepCompleted:
		distribution.Completed++
	}
}
