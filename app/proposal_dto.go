package app

import (
	"fmt"
	"strings"
	"time"

	"lum-harbor-notifier/internal/proposal"
)

// ProposeWorkItemUpdateRequestDTO conveys user intent to suggest an update
// to a WorkItem. Preconditions, operation versioning, and payload encoding
// remain internal to the backend producer.
type ProposeWorkItemUpdateRequestDTO struct {
	TargetWorkItemID    string  `json:"targetWorkItemId"`
	ProposedTitle       string  `json:"proposedTitle"`
	ProposedDescription *string `json:"proposedDescription"`
	Reason              *string `json:"reason"`
}

// ApplyProposalRequestDTO identifies the proposal to apply.
type ApplyProposalRequestDTO struct {
	ID string `json:"id"`
}

// ApplyProposalResultDTO reports the typed product outcome of an Apply attempt.
// WorkItem is optional/nullable: when an Apply results in a STALE outcome due to
// target unavailability (or when not present), WorkItem is nil.
type ApplyProposalResultDTO struct {
	Outcome  string       `json:"outcome"`
	Proposal ProposalDTO  `json:"proposal"`
	WorkItem *WorkItemDTO `json:"workItem"`
}

// RejectProposalRequestDTO identifies the proposal to reject.
type RejectProposalRequestDTO struct {
	ID string `json:"id"`
}

// DashboardProposalSummaryDTO provides lightweight proposal awareness metrics
// specifically tailored for the Dashboard command and orientation surface.
type DashboardProposalSummaryDTO struct {
	PendingCount          int `json:"pendingCount"`
	AffectedWorkItemCount int `json:"affectedWorkItemCount"`
}

// ProposalDTO is the product-facing Change Proposal transport model.
//
// Invariant:
//   - BaseTitle and BaseDescription represent the snapshot observed when the proposal was created.
//     They are NEVER labeled as current truth.
//   - Current truth is obtained directly from the authoritative selected WorkItem in React.
//   - ProposedTitle and ProposedDescription represent the suggested changes.
//   - SourceExtensionIDs names the extension that captured each linked Source
//     (for example core.assistant or core.local) so the UI can tell an
//     Assistant turn from another Source producer. Filled by the App layer.
type ProposalDTO struct {
	ID string `json:"id"`
	// TargetWorkItemID is nil only for a pending root-create Proposal (for
	// example planner.item.create with no WorkItem context) - the one case
	// where there is no existing target entity to reference at all.
	TargetWorkItemID    *string               `json:"targetWorkItemId"`
	OperationType       string                `json:"operationType"`
	Title               string                `json:"title"`
	Summary             *string               `json:"summary"`
	Reason              *string               `json:"reason"`
	Status              string                `json:"status"`
	BaseTitle           string                `json:"baseTitle"`
	BaseDescription     *string               `json:"baseDescription"`
	ProposedTitle       string                `json:"proposedTitle"`
	ProposedDescription *string               `json:"proposedDescription"`
	CreatedAt           string                `json:"createdAt"`
	UpdatedAt           string                `json:"updatedAt"`
	ResolvedAt          *string               `json:"resolvedAt"`
	AIRunID             *string               `json:"aiRunId"`
	SourceRevisionIDs   []string              `json:"sourceRevisionIds"`
	SourceExtensionIDs  []string              `json:"sourceExtensionIds"`
	SourceProvenances   []SourceProvenanceDTO `json:"sourceProvenances"`
	Changes             []ProposalChangeDTO   `json:"changes"`
	// Operations is empty for a singular Proposal. For an aggregate Proposal
	// (operationType proposal.aggregate) it is the authoritative ordered review
	// content; the parent's own changes are then empty.
	Operations []ProposalOperationDTO `json:"operations"`
}

// ProposalOperationDTO is the review contract of one ordered child operation
// of an aggregate Proposal. The backend interprets the typed payload; the
// frontend never parses operation payload JSON.
type ProposalOperationDTO struct {
	ID            string                `json:"id"`
	SequenceNo    int                   `json:"sequenceNo"`
	OperationType string                `json:"operationType"`
	Label         string                `json:"label"`
	Relations     []ProposalRelationDTO `json:"relations"`
	Changes       []ProposalChangeDTO   `json:"changes"`
}

// ProposalRelationDTO names an entity an operation acts on or links to: an
// existing entity by its title, or an entity created by an earlier action of
// the same Proposal (CreatedBySequenceNo), never by raw reference syntax.
type ProposalRelationDTO struct {
	Label               string `json:"label"`
	Value               string `json:"value"`
	CreatedBySequenceNo *int   `json:"createdBySequenceNo"`
}

// SourceProvenanceDTO conveys the origin details of a linked Source observation
// for human review and presentation.
type SourceProvenanceDTO struct {
	ExtensionID   string `json:"extensionId"`
	ExtensionName string `json:"extensionName"`
	ExternalID    string `json:"externalId"`
	SourceKind    string `json:"sourceKind"`
}

type ProposalChangeDTO struct {
	Label  string  `json:"label"`
	Before *string `json:"before"`
	After  *string `json:"after"`
}

// entityTitleResolver returns the user-facing title of an existing Work Item
// or Tracked Item, or false when it cannot be resolved. Review display only.
type entityTitleResolver func(entityID string) (string, bool)

// proposalToDTO maps a Proposal without resolving existing-entity titles for
// aggregate children; proposalToReviewDTO is the same mapping with a resolver.
func proposalToDTO(p proposal.Proposal) ProposalDTO {
	return proposalToReviewDTO(p, nil)
}

func proposalToReviewDTO(p proposal.Proposal, resolve entityTitleResolver) ProposalDTO {
	dto := ProposalDTO{
		ID:                p.ID,
		TargetWorkItemID:  p.TargetEntityID,
		OperationType:     p.OperationType,
		Title:             p.Title,
		Summary:           p.Summary,
		Reason:            p.Reason,
		Status:            string(p.Status),
		CreatedAt:         p.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:         p.UpdatedAt.UTC().Format(time.RFC3339Nano),
		ResolvedAt:        formatDTOTime(p.ResolvedAt),
		AIRunID:           p.AIRunID,
		SourceRevisionIDs: p.SourceRevisionIDs,
		SourceProvenances: []SourceProvenanceDTO{},
		Changes:           []ProposalChangeDTO{},
		Operations:        aggregateOperationsToDTO(p.Operations, resolve),
	}
	add := func(label string, before, after *string) {
		dto.Changes = append(dto.Changes, ProposalChangeDTO{Label: label, Before: before, After: after})
	}

	switch p.OperationType {
	case proposal.OperationTypeWorkItemUpdateDetails:
		dto.BaseTitle = p.Payload.Preconditions.ExpectedTitle
		dto.BaseDescription = p.Payload.Preconditions.ExpectedDescription
		dto.ProposedTitle = p.Payload.ProposedTitle
		dto.ProposedDescription = p.Payload.ProposedDescription
		add("Title", &p.Payload.Preconditions.ExpectedTitle, &p.Payload.ProposedTitle)
		add("Description", p.Payload.Preconditions.ExpectedDescription, p.Payload.ProposedDescription)

	case proposal.OperationTypePlannerItemCreate:
		if payload := p.PlannerItemPayload; payload != nil {
			add("Title", nil, &payload.Title)
			if payload.Details != nil {
				add("Notes", nil, payload.Details)
			}
			if payload.DueDate != nil {
				add("Due", nil, payload.DueDate)
			} else if payload.DueAt != nil {
				add("Due", nil, payload.DueAt)
			}
		}

	case proposal.OperationTypePlannerItemSetLocalNote:
		if payload := p.PlannerLocalNotePayload; payload != nil {
			// targetWorkItemId is a WorkItem navigation hint, never the
			// Planner Item's own ID.
			dto.TargetWorkItemID = payload.WorkItemID
			add("Planner Item", nil, &payload.PlannerItemTitle)
			add("Lum Harbor note", payload.ExpectedLocalNote, payload.LocalNote)
		}

	case proposal.OperationTypeWorkItemSetLifecycle:
		if payload := p.WorkItemLifecyclePayload; payload != nil {
			add("Lifecycle", &payload.ExpectedLifecycleState, &payload.TargetState)
		}

	case proposal.OperationTypeWorkflowCreate:
		if payload := p.WorkflowCreatePayload; payload != nil {
			add("Definition", nil, &payload.DefinitionName)
			steps := strings.Join(payload.Steps, ", ")
			add("Steps", nil, &steps)
		}

	case proposal.OperationTypeWorkItemCreate:
		if payload := p.WorkItemCreatePayload; payload != nil {
			dto.ProposedTitle = payload.Title
			dto.ProposedDescription = payload.Description
			add("Title", nil, &payload.Title)
			if payload.Description != nil {
				add("Description", nil, payload.Description)
			}
		}

	case proposal.OperationTypeWorkItemCreateWithWorkflow:
		if payload := p.WorkItemCreateWithWorkflowPayload; payload != nil {
			dto.ProposedTitle = payload.Title
			dto.ProposedDescription = payload.Description
			add("Title", nil, &payload.Title)
			if payload.Description != nil {
				add("Description", nil, payload.Description)
			}
			add("Workflow", nil, &payload.WorkflowName)
			stages := strings.Join(payload.Stages, ", ")
			add("Stages", nil, &stages)
		}

	case proposal.OperationTypeWorkflowSetCurrentStep:
		if payload := p.WorkflowSetCurrentStepPayload; payload != nil {
			dto.TargetWorkItemID = &payload.WorkItemID
			add("Workflow Step", payload.ExpectedCurrentStepTitle, &payload.TargetStepTitle)
		}

	case proposal.OperationTypeMonitorPointCreate:
		if payload := p.MonitorPointCreatePayload; payload != nil {
			dto.TargetWorkItemID = &payload.WorkItemID
			add("Monitor Target", nil, &payload.Title)
			kindStr := payload.Kind
			add("Kind", nil, &kindStr)
			if payload.SubjectTrackedItemTitle != nil {
				add("Subject", nil, payload.SubjectTrackedItemTitle)
			}
			if payload.ExpectedStepTitle != nil {
				add("Expected Step", nil, payload.ExpectedStepTitle)
			}
			if payload.ExpectedWorkItemLifecycle != nil {
				add("Expected Lifecycle", nil, payload.ExpectedWorkItemLifecycle)
			}
			dueStr := payload.DueAt + " (" + payload.Timezone + ")"
			add("Due", nil, &dueStr)
		}

	case proposal.OperationTypeMonitorPointSetState:
		if payload := p.MonitorPointSetStatePayload; payload != nil {
			dto.TargetWorkItemID = &payload.WorkItemID
			add("Monitor Target", nil, &payload.MonitorPointTitle)
			add("State", &payload.ExpectedState, &payload.TargetState)
		}

	default:
		if payload := p.TrackingPayload; payload != nil {
			// TargetEntityID for non-create Tracking operations is the
			// TrackedItem's own ID, not its parent WorkItem's ID; the DTO's
			// targetWorkItemId is always meant to be a WorkItem ID (used for
			// frontend navigation), so it is overridden here from the payload.
			if payload.WorkItemID != nil {
				dto.TargetWorkItemID = payload.WorkItemID
			}
			switch payload.OperationKind {
			case proposal.OperationTypeTrackedItemCreate:
				add("Tracked Item", nil, payload.Title)
				add("Type", nil, payload.ItemType)
				if payload.ParentItemTitle != nil {
					add("Parent", nil, payload.ParentItemTitle)
				}
			case proposal.OperationTypeTrackedItemUpdate:
				add("Title", payload.ExpectedTitle, payload.Title)
				add("Type", payload.ExpectedItemType, payload.ItemType)
				add("Parent", payload.ExpectedParentTitle, payload.ParentItemTitle)
			case proposal.OperationTypeTrackedItemStageState:
				add("Stage", payload.ExpectedWorkflowStep, payload.WorkflowStepTitle)
				add("State", payload.ExpectedState, payload.State)
			case proposal.OperationTypeTrackedItemArchive:
				before := "Active"
				value := "Archived"
				add("Availability", &before, &value)
			case proposal.OperationTypeTrackedItemDependencyAdd:
				add("Depends on", nil, payload.ToTrackedItemTitle)
			case proposal.OperationTypeTrackedItemDependencyRemove:
				add("Remove dependency", payload.ToTrackedItemTitle, nil)
			}
		}
	}
	return dto
}

// operationReviewLabels is the one bounded mapping from operation type to the
// human action name shown for an aggregate child.
var operationReviewLabels = map[string]string{
	proposal.OperationTypeWorkItemUpdateDetails:       "Update Work Item",
	proposal.OperationTypeWorkItemCreate:              "Create Work Item",
	proposal.OperationTypeWorkItemCreateWithWorkflow:  "Create Work Item & Workflow",
	proposal.OperationTypeWorkItemSetLifecycle:        "Change Work Item Lifecycle",
	proposal.OperationTypeWorkflowCreate:              "Create Workflow",
	proposal.OperationTypeWorkflowSetCurrentStep:      "Move Workflow Step",
	proposal.OperationTypeTrackedItemCreate:           "Create Tracked Item",
	proposal.OperationTypeTrackedItemUpdate:           "Update Tracked Item",
	proposal.OperationTypeTrackedItemStageState:       "Set Tracked Item Stage",
	proposal.OperationTypeTrackedItemArchive:          "Archive Tracked Item",
	proposal.OperationTypeTrackedItemDependencyAdd:    "Add Dependency",
	proposal.OperationTypeTrackedItemDependencyRemove: "Remove Dependency",
	proposal.OperationTypePlannerItemCreate:           "Create Todo",
	proposal.OperationTypePlannerItemSetLocalNote:     "Update Lum Harbor Note",
	proposal.OperationTypeMonitorPointCreate:          "Create Monitor Point",
	proposal.OperationTypeMonitorPointSetState:        "Update Monitor Point",
}

// aggregateOperationsToDTO maps aggregate children in sequence order. Each
// child's changes come from the same typed mapping a singular Proposal of
// that type uses; relations name linked entities in human terms.
func aggregateOperationsToDTO(children []proposal.ProposalOperation, resolve entityTitleResolver) []ProposalOperationDTO {
	operations := make([]ProposalOperationDTO, 0, len(children))
	decoded := make([]*proposal.Proposal, len(children))
	for index, child := range children {
		label, ok := operationReviewLabels[child.OperationType]
		if !ok {
			label = child.OperationType
		}
		dto := ProposalOperationDTO{
			ID: child.ID, SequenceNo: child.SequenceNo, OperationType: child.OperationType, Label: label,
			Relations: []ProposalRelationDTO{}, Changes: []ProposalChangeDTO{},
		}
		if op, err := child.TypedOperation(); err == nil {
			decoded[index] = op
			dto.Relations = operationRelations(op, decoded[:index], resolve)
			for _, change := range proposalToDTO(*op).Changes {
				// An action card lists what the action sets; a row with
				// neither a prior nor a proposed value carries nothing.
				if change.Before != nil || change.After != nil {
					dto.Changes = append(dto.Changes, change)
				}
			}
			if reminder := reminderChange(op); reminder != nil {
				dto.Changes = append(dto.Changes, *reminder)
			}
		}
		operations = append(operations, dto)
	}
	return operations
}

// operationRelations names the Work Item and Tracked Item an operation acts on
// or links to. The Workflow is implied by its Work Item (one Workflow per Work
// Item) and is not listed separately.
func operationRelations(op *proposal.Proposal, earlier []*proposal.Proposal, resolve entityTitleResolver) []ProposalRelationDTO {
	relations := []ProposalRelationDTO{}
	add := func(label string, id *string, knownTitle *string) {
		if id == nil || strings.TrimSpace(*id) == "" {
			return
		}
		if proposal.IsReference(*id) {
			relations = append(relations, referenceRelation(label, *id, earlier))
			return
		}
		if knownTitle != nil && *knownTitle != "" {
			relations = append(relations, ProposalRelationDTO{Label: label, Value: *knownTitle})
		} else if resolve != nil {
			if title, ok := resolve(*id); ok {
				relations = append(relations, ProposalRelationDTO{Label: label, Value: title})
			}
		}
	}
	switch op.OperationType {
	case proposal.OperationTypeWorkItemUpdateDetails, proposal.OperationTypeWorkItemSetLifecycle, proposal.OperationTypeWorkflowCreate:
		add("Work Item", op.TargetEntityID, nil)
	case proposal.OperationTypeWorkflowSetCurrentStep:
		if p := op.WorkflowSetCurrentStepPayload; p != nil {
			add("Work Item", &p.WorkItemID, nil)
		}
	case proposal.OperationTypeMonitorPointCreate:
		if p := op.MonitorPointCreatePayload; p != nil {
			add("Work Item", &p.WorkItemID, nil)
		}
	case proposal.OperationTypeMonitorPointSetState:
		if p := op.MonitorPointSetStatePayload; p != nil {
			add("Work Item", &p.WorkItemID, nil)
		}
	case proposal.OperationTypePlannerItemCreate:
		if p := op.PlannerItemPayload; p != nil {
			add("Work Item", p.WorkItemID, nil)
			add("Tracked Item", p.TrackedItemID, nil)
		}
	case proposal.OperationTypePlannerItemSetLocalNote:
		if p := op.PlannerLocalNotePayload; p != nil {
			add("Planner Item", &p.PlannerItemID, &p.PlannerItemTitle)
		}
	default:
		if p := op.TrackingPayload; p != nil {
			add("Work Item", p.WorkItemID, nil)
			if p.OperationKind != proposal.OperationTypeTrackedItemCreate {
				add("Tracked Item", p.TrackedItemID, p.ExpectedTitle)
			}
		}
	}
	return relations
}

// referenceRelation renders a symbolic reference as the earlier action that
// creates the entity, named from that action's own payload when available.
func referenceRelation(label, value string, earlier []*proposal.Proposal) ProposalRelationDTO {
	ref, err := proposal.ParseReference(value)
	if err != nil || ref.Index >= len(earlier) {
		return ProposalRelationDTO{Label: label, Value: "Result of an earlier action"}
	}
	sequenceNo := ref.Index
	relation := ProposalRelationDTO{Label: label, Value: fmt.Sprintf("Result of action %d", ref.Index+1), CreatedBySequenceNo: &sequenceNo}
	if name := createdEntityName(earlier[ref.Index], ref.Output); name != "" {
		relation.Value = fmt.Sprintf("Created in action %d — %s", ref.Index+1, name)
	}
	return relation
}

func createdEntityName(op *proposal.Proposal, output proposal.OutputKind) string {
	if op == nil {
		return ""
	}
	switch output {
	case proposal.OutputWorkItemID:
		if op.WorkItemCreatePayload != nil {
			return op.WorkItemCreatePayload.Title
		}
		if op.WorkItemCreateWithWorkflowPayload != nil {
			return op.WorkItemCreateWithWorkflowPayload.Title
		}
	case proposal.OutputTrackedItemID:
		if op.TrackingPayload != nil && op.TrackingPayload.Title != nil {
			return *op.TrackingPayload.Title
		}
	}
	return ""
}

// reminderChange describes a Todo's reminder thresholds, which a singular
// Planner review does not list but an aggregate action card does.
func reminderChange(op *proposal.Proposal) *ProposalChangeDTO {
	if op.PlannerItemPayload == nil || len(op.PlannerItemPayload.OffsetsSeconds) == 0 {
		return nil
	}
	parts := make([]string, 0, len(op.PlannerItemPayload.OffsetsSeconds))
	for _, seconds := range op.PlannerItemPayload.OffsetsSeconds {
		parts = append(parts, reminderOffsetText(seconds))
	}
	value := strings.Join(parts, ", ")
	return &ProposalChangeDTO{Label: "Reminder", After: &value}
}

func reminderOffsetText(seconds int) string {
	plural := func(n int, unit string) string {
		if n == 1 {
			return fmt.Sprintf("1 %s before due", unit)
		}
		return fmt.Sprintf("%d %ss before due", n, unit)
	}
	switch {
	case seconds <= 0:
		return "At due time"
	case seconds%86400 == 0:
		return plural(seconds/86400, "day")
	case seconds%3600 == 0:
		return plural(seconds/3600, "hour")
	case seconds%60 == 0:
		return plural(seconds/60, "minute")
	default:
		return plural(seconds, "second")
	}
}
