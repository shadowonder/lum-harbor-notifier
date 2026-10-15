package readcap

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"lum-harbor-notifier/internal/workitem"
)

// WorkItemReadInput is the validated input for CapabilityWorkItemRead.
type WorkItemReadInput struct {
	// WorkItemID is a stable WorkItem entity ID. Required, non-empty after
	// trimming. Wire key: "workItemId".
	WorkItemID string `json:"workItemId"`
}

// WorkItemFacts is the authoritative WorkItem content this capability
// exposes. It is a readcap-owned shape, not the internal workitem.WorkItem
// type, so this package's result contract does not change if that internal
// type does.
type WorkItemFacts struct {
	ID             string
	Title          string
	Description    *string
	Lifecycle      string
	LastRevisionID string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CompletedAt    *time.Time
}

// WorkflowStepFacts is one step of the WorkItem's Workflow definition.
type WorkflowStepFacts struct {
	ID       string
	Key      string
	Title    string
	Sequence int
	Terminal bool
}

// TrackedItemFacts is one TrackedItem belonging to the WorkItem's Workflow.
type TrackedItemFacts struct {
	ID               string  `json:"id"`
	Title            string  `json:"title"`
	ItemType         *string `json:"item_type,omitempty"`
	ParentItemID     *string `json:"parent_item_id,omitempty"`
	ParentItemTitle  *string `json:"parent_item_title,omitempty"`
	Lifecycle        string  `json:"lifecycle"`
	State            string  `json:"state"`
	CurrentStepID    *string `json:"current_step_id,omitempty"`
	CurrentStepTitle *string `json:"current_step_title,omitempty"`
	Complete         bool    `json:"complete"`
}

// MonitorPointFacts is one MonitorPoint attached to the WorkItem or its TrackedItems.
type MonitorPointFacts struct {
	ID          string
	Kind        string
	Title       string
	State       string
	DueAt       time.Time
	Timezone    string
	SubjectKind string
	SubjectID   string
}

// TrackingRelationFacts is one dependency relation between two TrackedItems under the same Workflow.
type TrackingRelationFacts struct {
	FromTrackedItemID string `json:"from_tracked_item_id"`
	ToTrackedItemID   string `json:"to_tracked_item_id"`
	Type              string `json:"type"`
}

// TrackingFacts is the bounded Tracking summary of one WorkItem's single
// Workflow. It is scoped to exactly that WorkItem's own Workflow and
// TrackedItems — never a cross-WorkItem read.
type TrackingFacts struct {
	WorkflowID    string
	StartedAt     time.Time
	CurrentStep   *WorkflowStepFacts // nil only if the current step is not among the definition's own steps (should not happen; defensive)
	Steps         []WorkflowStepFacts
	Items         []TrackedItemFacts
	Relations     []TrackingRelationFacts
	MonitorPoints []MonitorPointFacts
}

// WorkItemReadOutput is the result of CapabilityWorkItemRead.
type WorkItemReadOutput struct {
	WorkItem WorkItemFacts
	// Tracking is nil when the WorkItem has no Workflow.
	Tracking *TrackingFacts
}

func (s *Service) readWorkItem(ctx context.Context, input WorkItemReadInput) (WorkItemReadOutput, error) {
	id := strings.TrimSpace(input.WorkItemID)
	if id == "" {
		return WorkItemReadOutput{}, fmt.Errorf("%w: %s requires a non-empty WorkItemID", ErrInvalidInput, CapabilityWorkItemRead)
	}

	item, err := s.workItems.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, workitem.ErrNotFound) {
			return WorkItemReadOutput{}, fmt.Errorf("%w: work item %q", ErrEntityNotFound, id)
		}
		return WorkItemReadOutput{}, fmt.Errorf("%s: %w", CapabilityWorkItemRead, err)
	}

	output := WorkItemReadOutput{WorkItem: WorkItemFacts{
		ID:             item.ID,
		Title:          item.Title,
		Description:    item.Description,
		Lifecycle:      string(item.Lifecycle),
		LastRevisionID: item.LastRevisionID,
		CreatedAt:      item.CreatedAt,
		UpdatedAt:      item.UpdatedAt,
		CompletedAt:    item.CompletedAt,
	}}

	view, err := s.tracking.GetWorkItemTracking(ctx, id)
	if err != nil {
		return WorkItemReadOutput{}, fmt.Errorf("%s: bounded tracking read: %w", CapabilityWorkItemRead, err)
	}

	if view.Workflow != nil {
		facts := &TrackingFacts{
			WorkflowID: view.Workflow.Workflow.ID,
			StartedAt:  view.Workflow.Workflow.StartedAt,
		}
		if step, ok := view.Workflow.CurrentStep(); ok {
			facts.CurrentStep = &WorkflowStepFacts{
				ID: step.ID, Key: step.Key, Title: step.Title,
				Sequence: step.Sequence, Terminal: step.Terminal,
			}
		}
		for _, step := range view.Workflow.Definition.Steps {
			facts.Steps = append(facts.Steps, WorkflowStepFacts{
				ID: step.ID, Key: step.Key, Title: step.Title,
				Sequence: step.Sequence, Terminal: step.Terminal,
			})
		}
		itemTitles := make(map[string]string, len(view.Items))
		for _, viewItem := range view.Items {
			itemTitles[viewItem.Item.ID] = viewItem.Item.Title
		}
		for _, viewItem := range view.Items {
			ti := viewItem.Item
			itemFact := TrackedItemFacts{
				ID:           ti.ID,
				Title:        ti.Title,
				ItemType:     ti.ItemType,
				ParentItemID: ti.ParentItemID,
				Lifecycle:    string(ti.Lifecycle),
				State:        "NOT_STARTED",
			}
			if ti.ParentItemID != nil {
				if parentTitle, ok := itemTitles[*ti.ParentItemID]; ok {
					itemFact.ParentItemTitle = &parentTitle
				}
			}
			if p := viewItem.Progression; p != nil {
				itemFact.Complete = p.Complete
				itemFact.State = p.State()
				if !p.Complete && p.CurrentStepID != nil {
					itemFact.CurrentStepID = p.CurrentStepID
					if step, ok := view.Workflow.Definition.StepByID(*p.CurrentStepID); ok {
						itemFact.CurrentStepTitle = &step.Title
					}
				}
			}
			facts.Items = append(facts.Items, itemFact)
		}
		for _, rel := range view.Relations {
			facts.Relations = append(facts.Relations, TrackingRelationFacts{
				FromTrackedItemID: rel.FromTrackedItemID,
				ToTrackedItemID:   rel.ToTrackedItemID,
				Type:              string(rel.Type),
			})
		}
		for _, mp := range view.MonitorPoints {
			facts.MonitorPoints = append(facts.MonitorPoints, MonitorPointFacts{
				ID:          mp.ID,
				Kind:        string(mp.Kind),
				Title:       mp.Title,
				State:       string(mp.State),
				DueAt:       mp.DueAt,
				Timezone:    mp.Timezone,
				SubjectKind: string(mp.Subject.Kind),
				SubjectID:   mp.Subject.ID,
			})
		}
		output.Tracking = facts
	}

	return output, nil
}
