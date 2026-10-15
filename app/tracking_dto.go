package app

import (
	"time"

	"lum-harbor-notifier/internal/tracking"
)

// Tracking transport models are domain-shaped and presentation-free. Revision
// IDs never cross Wails (freshness is observable through updatedAt); enum
// values are the canonical domain vocabulary.

type WorkflowStepDTO struct {
	ID       string `json:"id"`
	Key      string `json:"key"`
	Title    string `json:"title"`
	Sequence int    `json:"sequence"`
	Terminal bool   `json:"terminal"`
}

type WorkflowDefinitionDTO struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Version   int               `json:"version"`
	Active    bool              `json:"active"`
	CreatedAt string            `json:"createdAt"`
	Steps     []WorkflowStepDTO `json:"steps"`
}

type StepStateDTO struct {
	WorkflowStepID string  `json:"workflowStepId"`
	State          string  `json:"state"`
	EnteredAt      *string `json:"enteredAt"`
	CompletedAt    *string `json:"completedAt"`
}

// ProgressionDTO is the backend-derived workflow position. It is never
// persisted and must not be recomputed by the frontend.
type ProgressionDTO struct {
	TotalSteps      int     `json:"totalSteps"`
	CompletedCount  int     `json:"completedCount"`
	SkippedCount    int     `json:"skippedCount"`
	PassedCount     int     `json:"passedCount"`
	ProgressPercent int     `json:"progressPercent"`
	Complete        bool    `json:"complete"`
	CurrentStepID   *string `json:"currentStepId"`
	CurrentStepKey  *string `json:"currentStepKey"`
	CurrentState    *string `json:"currentState"`
}

type TrackedItemDTO struct {
	ID             string          `json:"id"`
	WorkItemID     string          `json:"workItemId"`
	WorkflowID     string          `json:"workflowId"`
	ParentItemID   *string         `json:"parentItemId"`
	Title          string          `json:"title"`
	ItemType       *string         `json:"itemType"`
	LifecycleState string          `json:"lifecycleState"`
	CompletedAt    *string         `json:"completedAt"`
	CreatedAt      string          `json:"createdAt"`
	UpdatedAt      string          `json:"updatedAt"`
	StepStates     []StepStateDTO  `json:"stepStates"`
	Progression    *ProgressionDTO `json:"progression"`
}

type MonitorPointDTO struct {
	ID                        string  `json:"id"`
	SubjectKind               string  `json:"subjectKind"`
	SubjectID                 string  `json:"subjectId"`
	Kind                      string  `json:"kind"`
	Title                     string  `json:"title"`
	DueAt                     string  `json:"dueAt"`
	Timezone                  string  `json:"timezone"`
	ExpectationKind           string  `json:"expectationKind"`
	ExpectedWorkItemLifecycle *string `json:"expectedWorkItemLifecycle"`
	ExpectedStepID            *string `json:"expectedStepId"`
	State                     string  `json:"state"`
	SatisfiedAt               *string `json:"satisfiedAt"`
	CreatedAt                 string  `json:"createdAt"`
	UpdatedAt                 string  `json:"updatedAt"`
}

type WorkflowDTO struct {
	ID                   string                `json:"id"`
	WorkItemID           string                `json:"workItemId"`
	WorkflowDefinitionID string                `json:"workflowDefinitionId"`
	StartedAt            string                `json:"startedAt"`
	CurrentStep          WorkflowStepDTO       `json:"currentStep"`
	CreatedAt            string                `json:"createdAt"`
	UpdatedAt            string                `json:"updatedAt"`
	Definition           WorkflowDefinitionDTO `json:"definition"`
}

type TrackedItemRelationDTO struct {
	FromTrackedItemID string `json:"fromTrackedItemId"`
	ToTrackedItemID   string `json:"toTrackedItemId"`
	RelationType      string `json:"relationType"`
}

type TrackedItemAttentionDTO struct {
	ID                   string  `json:"id"`
	SubjectTrackedItemID string  `json:"subjectTrackedItemId"`
	MonitorPointID       string  `json:"monitorPointId"`
	State                string  `json:"state"`
	Severity             string  `json:"severity"`
	Reason               string  `json:"reason"`
	RecommendedAction    *string `json:"recommendedAction"`
	SnoozedUntil         *string `json:"snoozedUntil"`
	ResolvedAt           *string `json:"resolvedAt"`
}

// WorkItemTrackingDTO is the single semantic Tracking view of one WorkItem.
type WorkItemTrackingDTO struct {
	WorkItemID      string                    `json:"workItemId"`
	Workflow        *WorkflowDTO              `json:"workflow"`
	TrackedItems    []TrackedItemDTO          `json:"trackedItems"`
	Relations       []TrackedItemRelationDTO  `json:"relations"`
	MonitorPoints   []MonitorPointDTO         `json:"monitorPoints"`
	History         []HistoryEventDTO         `json:"history"`
	AttentionIssues []TrackedItemAttentionDTO `json:"attentionIssues"`
}

type WorkflowStepRequestDTO struct {
	Key      string `json:"key"`
	Title    string `json:"title"`
	Terminal bool   `json:"terminal"`
}

// CreateWorkflowDefinitionRequestDTO carries only process facts.
type CreateWorkflowDefinitionRequestDTO struct {
	Name    string                   `json:"name"`
	Version int                      `json:"version"`
	Steps   []WorkflowStepRequestDTO `json:"steps"`
}

// CreateWorkflowRequestDTO binds an active WorkflowDefinition to a WorkItem.
// CurrentStepID and StartedAt are optional: the first definition step and the
// creation time are used when omitted. StartedAt is an RFC 3339 actual start.
type CreateWorkflowRequestDTO struct {
	WorkItemID           string  `json:"workItemId"`
	WorkflowDefinitionID string  `json:"workflowDefinitionId"`
	CurrentStepID        *string `json:"currentStepId"`
	StartedAt            *string `json:"startedAt"`
}

// SetWorkflowCurrentStepRequestDTO explicitly moves the Workflow's own current step.
type SetWorkflowCurrentStepRequestDTO struct {
	WorkflowID string `json:"workflowId"`
	StepID     string `json:"stepId"`
}

// CreateTrackedItemRequestDTO carries only product semantics; identity,
// Revision, timestamps and initial step states are backend-owned.
type CreateTrackedItemRequestDTO struct {
	WorkItemID   string  `json:"workItemId"`
	WorkflowID   *string `json:"workflowId"`
	ParentItemID *string `json:"parentItemId"`
	Title        string  `json:"title"`
	ItemType     *string `json:"itemType"`
}

type SetTrackedItemStepStateRequestDTO struct {
	TrackedItemID  string `json:"trackedItemId"`
	WorkflowStepID string `json:"workflowStepId"`
	State          string `json:"state"`
}

// CreateMonitorPointRequestDTO: dueAt is RFC 3339. Exactly one expectation
// field must match the subject (see the Tracking contract).
type CreateMonitorPointRequestDTO struct {
	WorkItemID                string  `json:"workItemId"`
	SubjectTrackedItemID      *string `json:"subjectTrackedItemId"`
	Kind                      string  `json:"kind"`
	Title                     string  `json:"title"`
	DueAt                     string  `json:"dueAt"`
	Timezone                  string  `json:"timezone"`
	ExpectedWorkItemLifecycle *string `json:"expectedWorkItemLifecycle"`
	ExpectedStepID            *string `json:"expectedStepId"`
}

type SetMonitorPointStateRequestDTO struct {
	ID          string `json:"id"`
	TargetState string `json:"targetState"`
}

func trackingTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func workflowToDTO(def tracking.WorkflowDefinition) WorkflowDefinitionDTO {
	steps := make([]WorkflowStepDTO, 0, len(def.Steps))
	for _, s := range def.Steps {
		steps = append(steps, WorkflowStepDTO{ID: s.ID, Key: s.Key, Title: s.Title, Sequence: s.Sequence, Terminal: s.Terminal})
	}
	return WorkflowDefinitionDTO{ID: def.ID, Name: def.Name, Version: def.Version, Active: def.Active, CreatedAt: trackingTime(def.CreatedAt), Steps: steps}
}

// currentStepDTO exposes the Workflow's persisted current step as a semantic
// step (never derived from TrackedItems).
func currentStepDTO(view *tracking.WorkflowView) WorkflowStepDTO {
	step, _ := view.CurrentStep()
	return WorkflowStepDTO{ID: step.ID, Key: step.Key, Title: step.Title, Sequence: step.Sequence, Terminal: step.Terminal}
}

func workflowViewToDTO(view *tracking.WorkflowView) *WorkflowDTO {
	if view == nil {
		return nil
	}
	return &WorkflowDTO{
		ID:                   view.Workflow.ID,
		WorkItemID:           view.Workflow.WorkItemID,
		WorkflowDefinitionID: view.Workflow.WorkflowDefinitionID,
		StartedAt:            trackingTime(view.Workflow.StartedAt),
		CurrentStep:          currentStepDTO(view),
		CreatedAt:            trackingTime(view.Workflow.CreatedAt),
		UpdatedAt:            trackingTime(view.Workflow.UpdatedAt),
		Definition:           workflowToDTO(view.Definition),
	}
}

func stepStateToDTO(s tracking.StepStateRecord) StepStateDTO {
	return StepStateDTO{WorkflowStepID: s.WorkflowStepID, State: string(s.State), EnteredAt: formatDTOTime(s.EnteredAt), CompletedAt: formatDTOTime(s.CompletedAt)}
}

func progressionToDTO(p *tracking.Progression) *ProgressionDTO {
	if p == nil {
		return nil
	}
	dto := &ProgressionDTO{
		TotalSteps: p.TotalSteps, CompletedCount: p.CompletedCount, SkippedCount: p.SkippedCount,
		PassedCount: p.PassedCount, ProgressPercent: p.ProgressPercent, Complete: p.Complete,
		CurrentStepID: p.CurrentStepID, CurrentStepKey: p.CurrentStepKey,
	}
	if p.CurrentState != nil {
		state := string(*p.CurrentState)
		dto.CurrentState = &state
	}
	return dto
}

func trackedItemToDTO(view tracking.TrackedItemView) TrackedItemDTO {
	states := make([]StepStateDTO, 0, len(view.StepStates))
	for _, s := range view.StepStates {
		states = append(states, stepStateToDTO(s))
	}
	i := view.Item
	return TrackedItemDTO{
		ID: i.ID, WorkItemID: i.WorkItemID, WorkflowID: i.WorkflowID,
		ParentItemID: i.ParentItemID, Title: i.Title, ItemType: i.ItemType,
		LifecycleState: string(i.Lifecycle),
		CompletedAt:    formatDTOTime(i.CompletedAt), CreatedAt: trackingTime(i.CreatedAt), UpdatedAt: trackingTime(i.UpdatedAt),
		StepStates: states, Progression: progressionToDTO(view.Progression),
	}
}

func monitorPointToDTO(m tracking.MonitorPoint) MonitorPointDTO {
	dto := MonitorPointDTO{
		ID: m.ID, SubjectKind: string(m.Subject.Kind), SubjectID: m.Subject.ID, Kind: string(m.Kind), Title: m.Title,
		DueAt: trackingTime(m.DueAt), Timezone: m.Timezone, ExpectationKind: string(m.Expected.Kind),
		ExpectedStepID: m.Expected.StepID, State: string(m.State), SatisfiedAt: formatDTOTime(m.SatisfiedAt),
		CreatedAt: trackingTime(m.CreatedAt), UpdatedAt: trackingTime(m.UpdatedAt),
	}
	if m.Expected.WorkItemState != nil {
		lifecycle := string(*m.Expected.WorkItemState)
		dto.ExpectedWorkItemLifecycle = &lifecycle
	}
	return dto
}

func workItemTrackingToDTO(view tracking.WorkItemTracking) WorkItemTrackingDTO {
	dto := WorkItemTrackingDTO{
		WorkItemID:      view.WorkItemID,
		Workflow:        workflowViewToDTO(view.Workflow),
		TrackedItems:    make([]TrackedItemDTO, 0, len(view.Items)),
		Relations:       make([]TrackedItemRelationDTO, 0, len(view.Relations)),
		MonitorPoints:   make([]MonitorPointDTO, 0, len(view.MonitorPoints)),
		History:         []HistoryEventDTO{},
		AttentionIssues: []TrackedItemAttentionDTO{},
	}
	for _, item := range view.Items {
		dto.TrackedItems = append(dto.TrackedItems, trackedItemToDTO(item))
	}
	for _, m := range view.MonitorPoints {
		dto.MonitorPoints = append(dto.MonitorPoints, monitorPointToDTO(m))
	}
	for _, relation := range view.Relations {
		dto.Relations = append(dto.Relations, TrackedItemRelationDTO{FromTrackedItemID: relation.FromTrackedItemID, ToTrackedItemID: relation.ToTrackedItemID, RelationType: string(relation.Type)})
	}
	return dto
}
