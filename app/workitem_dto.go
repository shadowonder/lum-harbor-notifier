package app

import (
	"time"

	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
	"lum-harbor-notifier/internal/workitemoverview"
)

// CreateWorkItemRequestDTO contains only user-authored WorkItem fields.
// Identity, Revision, lifecycle, and completion state remain backend-owned.
type CreateWorkItemRequestDTO struct {
	Title       string  `json:"title"`
	Description *string `json:"description"`
}

// SetWorkItemLifecycleStateRequestDTO exposes only WorkItem identity and the
// requested target. Completion time and mutation bookkeeping are backend-owned.
type SetWorkItemLifecycleStateRequestDTO struct {
	ID                   string `json:"id"`
	TargetLifecycleState string `json:"targetLifecycleState"`
}

// WorkItemDTO is the product-facing WorkItem transport model. Persistence
// implementation details such as last_revision_id never cross Wails.
type WorkItemDTO struct {
	ID             string  `json:"id"`
	Title          string  `json:"title"`
	Description    *string `json:"description"`
	LifecycleState string  `json:"lifecycleState"`
	CompletedAt    *string `json:"completedAt"`
	CreatedAt      string  `json:"createdAt"`
	UpdatedAt      string  `json:"updatedAt"`
}

type TrackedItemProgressionSummaryDTO struct {
	TrackedItemID        string         `json:"trackedItemId"`
	ParentItemID         *string        `json:"parentItemId"`
	WorkflowDefinitionID string         `json:"workflowDefinitionId"`
	Progression          ProgressionDTO `json:"progression"`
}

type WorkItemTrackingSummaryDTO struct {
	WorkItemID             string                             `json:"workItemId"`
	TrackedItemCount       int                                `json:"trackedItemCount"`
	WorkflowBoundItemCount int                                `json:"workflowBoundItemCount"`
	Progressions           []TrackedItemProgressionSummaryDTO `json:"progressions"`
	ActiveFinalTarget      *MonitorPointDTO                   `json:"activeFinalTarget"`
}

type WorkItemAttentionSummaryDTO struct {
	WorkItemID             string  `json:"workItemId"`
	OpenIssueCount         int     `json:"openIssueCount"`
	ActionableIssueCount   int     `json:"actionableIssueCount"`
	SnoozedIssueCount      int     `json:"snoozedIssueCount"`
	HighestActiveSeverity  *string `json:"highestActiveSeverity"`
	HasActionableAttention bool    `json:"hasActionableAttention"`
}

type WorkItemOverviewStageDTO struct {
	ID       string `json:"id"`
	Key      string `json:"key"`
	Title    string `json:"title"`
	Sequence int    `json:"sequence"`
	Terminal bool   `json:"terminal"`
}

// WorkItemOverviewWorkflowDTO carries the Workflow's own authoritative
// position: currentStep is the persisted Workflow.current_step (semantic, no
// client-side join) and startedAt is the actual Workflow start. Neither is
// derived from TrackedItems.
type WorkItemOverviewWorkflowDTO struct {
	ID           string                     `json:"id"`
	DefinitionID string                     `json:"definitionId"`
	StartedAt    string                     `json:"startedAt"`
	CurrentStep  WorkItemOverviewStageDTO   `json:"currentStep"`
	Stages       []WorkItemOverviewStageDTO `json:"stages"`
}

type WorkItemOverviewTrackedItemSignalDTO struct {
	State                      string  `json:"state"`
	HasActiveTrackingTodo      bool    `json:"hasActiveTrackingTodo"`
	GoverningTodoDueAt         *string `json:"governingTodoDueAt"`
	ActionableAttentionCount   int     `json:"actionableAttentionCount"`
	HighestAttentionSeverity   *string `json:"highestAttentionSeverity"`
	HasActiveExpectation       bool    `json:"hasActiveExpectation"`
	HasSatisfiedExpectation    bool    `json:"hasSatisfiedExpectation"`
	HasOverdueUnmetExpectation bool    `json:"hasOverdueUnmetExpectation"`
}

type WorkItemOverviewTrackedItemDTO struct {
	ID            string                               `json:"id"`
	Title         string                               `json:"title"`
	ItemType      *string                              `json:"itemType"`
	ParentItemID  *string                              `json:"parentItemId"`
	Lifecycle     string                               `json:"lifecycle"`
	CurrentStepID *string                              `json:"currentStepId"`
	CurrentState  *string                              `json:"currentState"`
	Complete      bool                                 `json:"complete"`
	Resolved      bool                                 `json:"resolved"`
	Signal        WorkItemOverviewTrackedItemSignalDTO `json:"signal"`
}

type WorkItemOverviewStateDistributionDTO struct {
	NotStarted int `json:"notStarted"`
	InProgress int `json:"inProgress"`
	Waiting    int `json:"waiting"`
	Blocked    int `json:"blocked"`
	Failed     int `json:"failed"`
	Unknown    int `json:"unknown"`
	Skipped    int `json:"skipped"`
	Completed  int `json:"completed"`
}

type WorkItemOverviewTrackingDTO struct {
	TrackedItemCount  int                                  `json:"trackedItemCount"`
	Workflow          *WorkItemOverviewWorkflowDTO         `json:"workflow"`
	Items             []WorkItemOverviewTrackedItemDTO     `json:"items"`
	StateDistribution WorkItemOverviewStateDistributionDTO `json:"stateDistribution"`
	ActiveFinalTarget *MonitorPointDTO                     `json:"activeFinalTarget"`
}

type WorkItemOverviewAttentionDTO struct {
	OpenIssueCount        int     `json:"openIssueCount"`
	ActionableIssueCount  int     `json:"actionableIssueCount"`
	SnoozedIssueCount     int     `json:"snoozedIssueCount"`
	HighestActiveSeverity *string `json:"highestActiveSeverity"`
}

type WorkItemOverviewItemDTO struct {
	WorkItem  WorkItemDTO                  `json:"workItem"`
	Tracking  WorkItemOverviewTrackingDTO  `json:"tracking"`
	Attention WorkItemOverviewAttentionDTO `json:"attention"`
}

func overviewTrackingToDTO(summary tracking.WorkItemSummary, signals map[string]workitemoverview.TrackedItemSignal) WorkItemOverviewTrackingDTO {
	dto := WorkItemOverviewTrackingDTO{
		TrackedItemCount: summary.TrackedItemCount,
		Items:            make([]WorkItemOverviewTrackedItemDTO, 0, len(summary.Items)),
		StateDistribution: WorkItemOverviewStateDistributionDTO{
			NotStarted: summary.StateDistribution.NotStarted, InProgress: summary.StateDistribution.InProgress,
			Waiting: summary.StateDistribution.Waiting, Blocked: summary.StateDistribution.Blocked,
			Failed: summary.StateDistribution.Failed, Unknown: summary.StateDistribution.Unknown,
			Skipped: summary.StateDistribution.Skipped, Completed: summary.StateDistribution.Completed,
		},
	}
	if summary.Workflow != nil {
		workflow := WorkItemOverviewWorkflowDTO{
			ID: summary.Workflow.ID, DefinitionID: summary.Workflow.Definition.ID,
			StartedAt:   trackingTime(summary.Workflow.StartedAt),
			CurrentStep: WorkItemOverviewStageDTO{ID: summary.Workflow.CurrentStep.ID, Key: summary.Workflow.CurrentStep.Key, Title: summary.Workflow.CurrentStep.Title, Sequence: summary.Workflow.CurrentStep.Sequence, Terminal: summary.Workflow.CurrentStep.Terminal},
			Stages:      make([]WorkItemOverviewStageDTO, 0, len(summary.Workflow.Definition.Steps)),
		}
		for _, step := range summary.Workflow.Definition.Steps {
			workflow.Stages = append(workflow.Stages, WorkItemOverviewStageDTO{ID: step.ID, Key: step.Key, Title: step.Title, Sequence: step.Sequence, Terminal: step.Terminal})
		}
		dto.Workflow = &workflow
	}
	for _, item := range summary.Items {
		var state *string
		if item.Progression.CurrentState != nil {
			value := string(*item.Progression.CurrentState)
			state = &value
		}
		signal := signals[item.ID]
		signalDTO := WorkItemOverviewTrackedItemSignalDTO{
			State: string(signal.State), ActionableAttentionCount: signal.ActionableAttentionCount,
			HasActiveTrackingTodo: signal.HasActiveTrackingTodo,
			HasActiveExpectation:  signal.HasActiveExpectation, HasSatisfiedExpectation: signal.HasSatisfiedExpectation,
			HasOverdueUnmetExpectation: signal.HasOverdueUnmetExpectation,
		}
		if signal.GoverningTodoDueAt != nil {
			value := signal.GoverningTodoDueAt.UTC().Format(time.RFC3339Nano)
			signalDTO.GoverningTodoDueAt = &value
		}
		if signal.HighestAttentionSeverity != nil {
			value := string(*signal.HighestAttentionSeverity)
			signalDTO.HighestAttentionSeverity = &value
		}
		dto.Items = append(dto.Items, WorkItemOverviewTrackedItemDTO{
			ID: item.ID, Title: item.Title, ItemType: item.ItemType, ParentItemID: item.ParentItemID,
			Lifecycle: string(item.Lifecycle), CurrentStepID: item.Progression.CurrentStepID,
			CurrentState: state, Complete: item.Progression.Complete,
			Resolved: item.Progression.Complete || item.Lifecycle == tracking.ItemCompleted || item.Lifecycle == tracking.ItemCancelled,
			Signal:   signalDTO,
		})
	}
	if summary.ActiveFinalTarget != nil {
		target := monitorPointToDTO(*summary.ActiveFinalTarget)
		dto.ActiveFinalTarget = &target
	}
	return dto
}

func overviewAttentionToDTO(summary attention.WorkItemSummary) WorkItemOverviewAttentionDTO {
	dto := WorkItemOverviewAttentionDTO{
		OpenIssueCount: summary.OpenIssueCount, ActionableIssueCount: summary.ActionableIssueCount,
		SnoozedIssueCount: summary.SnoozedIssueCount,
	}
	if summary.HighestActiveSeverity != nil {
		value := string(*summary.HighestActiveSeverity)
		dto.HighestActiveSeverity = &value
	}
	return dto
}

type WorkItemOverviewDTO struct {
	Items              []WorkItemOverviewItemDTO `json:"items"`
	TrackingAvailable  bool                      `json:"trackingAvailable"`
	AttentionAvailable bool                      `json:"attentionAvailable"`
}

func workItemToDTO(item workitem.WorkItem) WorkItemDTO {
	return WorkItemDTO{
		ID:             item.ID,
		Title:          item.Title,
		Description:    item.Description,
		LifecycleState: string(item.Lifecycle),
		CompletedAt:    formatDTOTime(item.CompletedAt),
		CreatedAt:      item.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:      item.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func workItemsToDTO(items []workitem.WorkItem) []WorkItemDTO {
	result := make([]WorkItemDTO, 0, len(items))
	for _, item := range items {
		result = append(result, workItemToDTO(item))
	}
	return result
}

func trackingSummaryToDTO(summary tracking.WorkItemSummary) WorkItemTrackingSummaryDTO {
	dto := WorkItemTrackingSummaryDTO{
		WorkItemID: summary.WorkItemID, TrackedItemCount: summary.TrackedItemCount,
		WorkflowBoundItemCount: summary.WorkflowBoundItemCount,
		Progressions:           make([]TrackedItemProgressionSummaryDTO, 0, len(summary.Progressions)),
	}
	for _, item := range summary.Progressions {
		progression := progressionToDTO(&item.Progression)
		dto.Progressions = append(dto.Progressions, TrackedItemProgressionSummaryDTO{
			TrackedItemID: item.TrackedItemID, ParentItemID: item.ParentItemID,
			WorkflowDefinitionID: item.WorkflowDefinitionID, Progression: *progression,
		})
	}
	if summary.ActiveFinalTarget != nil {
		target := monitorPointToDTO(*summary.ActiveFinalTarget)
		dto.ActiveFinalTarget = &target
	}
	return dto
}

func attentionSummaryToDTO(summary attention.WorkItemSummary) WorkItemAttentionSummaryDTO {
	dto := WorkItemAttentionSummaryDTO{
		WorkItemID: summary.WorkItemID, OpenIssueCount: summary.OpenIssueCount,
		ActionableIssueCount: summary.ActionableIssueCount, SnoozedIssueCount: summary.SnoozedIssueCount,
		HasActionableAttention: summary.ActionableIssueCount > 0,
	}
	if summary.HighestActiveSeverity != nil {
		severity := string(*summary.HighestActiveSeverity)
		dto.HighestActiveSeverity = &severity
	}
	return dto
}

func workItemOverviewToDTO(view *workitemoverview.View) WorkItemOverviewDTO {
	dto := WorkItemOverviewDTO{
		Items:             make([]WorkItemOverviewItemDTO, 0, len(view.Items)),
		TrackingAvailable: view.TrackingAvailable, AttentionAvailable: view.AttentionAvailable,
	}
	for _, item := range view.Items {
		dto.Items = append(dto.Items, WorkItemOverviewItemDTO{
			WorkItem: workItemToDTO(item.WorkItem), Tracking: overviewTrackingToDTO(item.Tracking, item.TrackedItemSignals),
			Attention: overviewAttentionToDTO(item.Attention),
		})
	}
	return dto
}
