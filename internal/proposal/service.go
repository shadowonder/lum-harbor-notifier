package proposal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/identity"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/reminder"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

// Domain errors for proposal use cases.
var (
	ErrInvalidInput            = errors.New("invalid proposal input")
	ErrInvalidStatusTransition = errors.New("invalid proposal status transition")
	ErrTargetUnavailable       = errors.New("target work item unavailable")
	ErrBaseChanged             = errors.New("target work item no longer matches the expected base")
)

// WorkItemOperator defines the narrow transaction-aware contract required by Proposal
// to inspect and mutate target WorkItem authoritative truth.
type WorkItemOperator interface {
	GetByID(ctx context.Context, id string) (*workitem.WorkItem, error)
	GetByIDTx(ctx context.Context, tx *sql.Tx, id string) (*workitem.WorkItem, error)
	CreateLocalWorkItemTx(ctx context.Context, tx *sql.Tx, input workitem.CreateLocalWorkItemInput) (*workitem.WorkItem, error)
	UpdateLocalWorkItemDetailsTx(ctx context.Context, tx *sql.Tx, input workitem.UpdateLocalWorkItemDetailsInput) (*workitem.WorkItem, error)
	SetLocalWorkItemLifecycleStateTx(ctx context.Context, tx *sql.Tx, input workitem.SetLocalWorkItemLifecycleStateInput) (*workitem.WorkItem, error)
}

type TrackingOperator interface {
	GetWorkItemTracking(context.Context, string) (*tracking.WorkItemTracking, error)
	GetTrackedItem(context.Context, string) (*tracking.TrackedItem, error)
	GetTrackedItemTx(context.Context, *sql.Tx, string) (*tracking.TrackedItem, error)
	CreateTrackedItemTx(context.Context, *sql.Tx, tracking.CreateTrackedItemInput) (*tracking.TrackedItem, error)
	UpdateTrackedItemTx(context.Context, *sql.Tx, tracking.UpdateTrackedItemInput) (*tracking.TrackedItem, error)
	SetTrackedItemStageStateTx(context.Context, *sql.Tx, tracking.SetTrackedItemStageStateInput) (*tracking.TrackedItem, error)
	ArchiveTrackedItemTx(context.Context, *sql.Tx, string) (*tracking.TrackedItem, error)
	AddDependencyTx(context.Context, *sql.Tx, tracking.TrackedItemRelationInput) (*tracking.TrackedItemRelation, error)
	RemoveDependencyTx(context.Context, *sql.Tx, tracking.TrackedItemRelationInput) (string, error)
	CreateWorkflowDefinitionTx(context.Context, *sql.Tx, tracking.CreateWorkflowDefinitionInput) (*tracking.WorkflowDefinition, error)
	CreateWorkflowTx(context.Context, *sql.Tx, tracking.CreateWorkflowInput) (*tracking.Workflow, error)
	GetWorkflow(context.Context, string) (*tracking.Workflow, error)
	GetWorkflowTx(context.Context, *sql.Tx, string) (*tracking.Workflow, error)
	GetWorkflowByWorkItem(context.Context, string) (*tracking.Workflow, error)
	GetWorkflowByWorkItemTx(context.Context, *sql.Tx, string) (*tracking.Workflow, error)
	SetWorkflowCurrentStepTx(context.Context, *sql.Tx, tracking.SetWorkflowCurrentStepInput) (*tracking.Workflow, error)
	CreateMonitorPointTx(context.Context, *sql.Tx, tracking.CreateMonitorPointInput) (*tracking.MonitorPoint, error)
	GetMonitorPoint(context.Context, string) (*tracking.MonitorPoint, error)
	GetMonitorPointTx(context.Context, *sql.Tx, string) (*tracking.MonitorPoint, error)
	SetMonitorPointStateTx(context.Context, *sql.Tx, tracking.SetMonitorPointStateInput) (*tracking.MonitorPoint, error)
}

// PlannerOperator defines the narrow transaction-aware contract required by
// Proposal to create a new root-level Planner item atomically during Apply.
type PlannerOperator interface {
	CreateLocalTodoTx(ctx context.Context, tx *sql.Tx, input planner.CreateLocalTodoInput) (*planner.PlannerItem, error)
}

// ReminderOperator defines the transaction-aware contract for setting a reminder during Apply.
type ReminderOperator interface {
	SetReminderTx(ctx context.Context, tx *sql.Tx, input reminder.SetReminderInput, revisionID string, now time.Time) (*reminder.Reminder, error)
}

// ExpectedWorkItemBase is the authoritative WorkItem snapshot a producer
// (for example the AI Assistant) actually used to derive its proposed values.
// When supplied, creation succeeds only if the target still equals this base.
type ExpectedWorkItemBase struct {
	LastRevisionID string
	Title          string
	Description    *string
}

// ProposeWorkItemUpdateInput defines caller-supplied intent for proposing an
// update to a WorkItem. Target identity and proposed values are preserved as-is.
type ProposeWorkItemUpdateInput struct {
	TargetWorkItemID    string
	ProposedTitle       string
	ProposedDescription *string
	ProposalTitle       *string
	Summary             *string
	Reason              *string
	AIRunID             *string
	SourceRevisionIDs   []string
	// ExpectedBase, when non-nil, makes creation a checked operation: the
	// authoritative target read that supplies the captured preconditions is
	// verified against it in the same transaction.
	ExpectedBase *ExpectedWorkItemBase
	// Producer is explicit on production callers. Empty retains USER semantics
	// for existing direct/internal callers and test fixtures.
	Producer Producer
}

type ProposeTrackedItemOperationInput struct {
	Payload           TrackedItemOperationPayload
	ProposalTitle     string
	Summary           *string
	Reason            *string
	AIRunID           *string
	SourceRevisionIDs []string
	Producer          Producer
}

// ProposePlannerItemCreateInput defines caller-supplied intent for proposing
// a brand-new root-level Planner item. WorkItemID is optional: when a
// WorkItem is in context, it becomes the non-null target_entity_id (the same
// existing-parent pattern tracked_item.create already uses); when nil, the
// resulting Proposal has a NULL target and represents a true root create.
type ProposePlannerItemCreateInput struct {
	WorkItemID        *string
	WorkflowID        *string
	WorkflowStepID    *string
	TrackedItemID     *string
	Title             string
	Details           *string
	DueDate           *string
	DueAt             *string
	OffsetsSeconds    []int
	ProposalTitle     *string
	Summary           *string
	Reason            *string
	AIRunID           *string
	SourceRevisionIDs []string
	Producer          Producer
}

// ProposeWorkItemLifecycleInput defines caller-supplied intent for proposing
// a WorkItem lifecycle transition through the reviewable Proposal path.
type ProposeWorkItemLifecycleInput struct {
	TargetWorkItemID  string
	TargetState       string
	ProposalTitle     *string
	Summary           *string
	Reason            *string
	AIRunID           *string
	SourceRevisionIDs []string
	Producer          Producer
}

// ProposeWorkflowCreateInput defines caller-supplied intent for proposing one
// semantic action: create/attach a Workflow with the given ordered steps to
// a target WorkItem.
type ProposeWorkflowCreateInput struct {
	TargetWorkItemID  string
	DefinitionName    string
	Steps             []string
	ProposalTitle     *string
	Summary           *string
	Reason            *string
	AIRunID           *string
	SourceRevisionIDs []string
	Producer          Producer
}

// ProposeWorkItemCreateInput defines caller-supplied intent for proposing a
// brand-new WorkItem root entity.
type ProposeWorkItemCreateInput struct {
	Title             string
	Description       *string
	ProposalTitle     *string
	Summary           *string
	Reason            *string
	AIRunID           *string
	SourceRevisionIDs []string
	Producer          Producer
}

// ProposeWorkItemCreateWithWorkflowInput defines caller-supplied intent for proposing
// a brand-new WorkItem along with its initial Workflow stages in a single Proposal.
type ProposeWorkItemCreateWithWorkflowInput struct {
	Title             string
	Description       *string
	WorkflowName      string
	Stages            []string
	ProposalTitle     *string
	Summary           *string
	Reason            *string
	AIRunID           *string
	SourceRevisionIDs []string
	Producer          Producer
}

// ProposeMonitorPointCreateInput defines caller-supplied intent for proposing
// an authoritative MonitorPoint expectation for a WorkItem or TrackedItem.
type ProposeMonitorPointCreateInput struct {
	WorkItemID                string
	SubjectTrackedItemID      *string
	SubjectTrackedItemTitle   *string
	Kind                      string
	Title                     string
	DueAt                     time.Time
	Timezone                  string
	ExpectedWorkItemLifecycle *string
	ExpectedStepID            *string
	ExpectedStepTitle         *string
	ProposalTitle             *string
	Summary                   *string
	Reason                    *string
	AIRunID                   *string
	SourceRevisionIDs         []string
	Producer                  Producer
}

// ProposeMonitorPointSetStateInput defines caller-supplied intent for proposing
// a state transition on an existing MonitorPoint.
type ProposeMonitorPointSetStateInput struct {
	MonitorPointID    string
	TargetState       string
	ProposalTitle     *string
	Summary           *string
	Reason            *string
	AIRunID           *string
	SourceRevisionIDs []string
	Producer          Producer
}

// ProposeWorkflowSetCurrentStepInput defines caller-supplied intent for
// proposing moving a Workflow's authoritative current step.
type ProposeWorkflowSetCurrentStepInput struct {
	WorkItemID        string
	WorkflowID        string
	TargetStepID      string
	TargetStepTitle   string
	ProposalTitle     *string
	Summary           *string
	Reason            *string
	AIRunID           *string
	SourceRevisionIDs []string
	Producer          Producer
}

// RejectProposalInput identifies the proposal to reject.
type RejectProposalInput struct {
	ID string
}

// ApplyOutcome represents the typed result outcome of an Apply attempt.
type ApplyOutcome string

const (
	ApplyOutcomeApplied        ApplyOutcome = "APPLIED"
	ApplyOutcomeStale          ApplyOutcome = "STALE"
	ApplyOutcomeAlreadyApplied ApplyOutcome = "ALREADY_APPLIED"
)

// ApplyProposalInput identifies the proposal to apply.
type ApplyProposalInput struct {
	ID string
}

// ApplyProposalResult is the product-level outcome of an Apply attempt.
type ApplyProposalResult struct {
	Outcome  ApplyOutcome
	Proposal *Proposal
	WorkItem *workitem.WorkItem
}

// ProposalSummary provides high-level aggregation of pending proposals for
// command/orientation surfaces like the Dashboard. It contains counts only and
// deliberately omits detailed proposal payloads.
type ProposalSummary struct {
	PendingCount          int
	AffectedWorkItemCount int
}

// Service owns Proposal lifecycle, validation, and transaction boundaries.
// It follows the transaction-bound repository pattern: reads construct a
// repository over *sql.DB; mutations explicitly construct the repository
// over the active *sql.Tx.
type Service struct {
	db               *sql.DB
	workItemOperator WorkItemOperator
	trackingOperator TrackingOperator
	plannerOperator  PlannerOperator
	reminderOperator ReminderOperator
}

// NewService constructs a Service with its required database and domain operator.
func NewService(db *sql.DB, workItemOperator WorkItemOperator, trackingOperator ...TrackingOperator) *Service {
	service := &Service{
		db:               db,
		workItemOperator: workItemOperator,
	}
	if len(trackingOperator) > 0 {
		service.trackingOperator = trackingOperator[0]
	}
	return service
}

func (s *Service) SetTrackingOperator(operator TrackingOperator) { s.trackingOperator = operator }

func (s *Service) SetPlannerOperator(operator PlannerOperator) { s.plannerOperator = operator }

func (s *Service) SetReminderOperator(operator ReminderOperator) { s.reminderOperator = operator }

// ProposeWorkItemUpdate deterministically creates a new PENDING Proposal
// targeting an authoritative WorkItem. It captures exact preconditions
// (LastRevisionID, Title, Description) without mutating the target WorkItem or
// creating a domain Revision.
func (s *Service) ProposeWorkItemUpdate(ctx context.Context, input ProposeWorkItemUpdateInput) (*Proposal, error) {
	targetID, producer, err := validateWorkItemUpdateInput(input)
	if err != nil {
		return nil, err
	}

	// The authoritative read, the optional expected-base check, and the insert
	// share one transaction, so the captured preconditions can never describe
	// a different WorkItem state than the one that was verified.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create proposal transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Unknown or archived items return an error.
	target, err := s.workItemOperator.GetByIDTx(ctx, tx, targetID)
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrTargetUnavailable, targetID, err)
	}
	proposal, err := newWorkItemUpdateProposal(target, input)
	if err != nil {
		return nil, err
	}

	repo := NewRepository(tx)
	if err := repo.Create(ctx, proposal); err != nil {
		return nil, err
	}
	if err := recordProposalCreated(ctx, tx, proposal, producer); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create proposal transaction: %w", err)
	}

	return proposal, nil
}

// buildWorkItemUpdate is the non-persisting counterpart of
// ProposeWorkItemUpdate. Its read is not transactional; an aggregate child is
// protected by the same captured preconditions, re-checked at Apply.
func (s *Service) buildWorkItemUpdate(ctx context.Context, input ProposeWorkItemUpdateInput, _ buildMode) (*Proposal, Producer, error) {
	targetID, producer, err := validateWorkItemUpdateInput(input)
	if err != nil {
		return nil, "", err
	}
	target, err := s.workItemOperator.GetByID(ctx, targetID)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %s: %v", ErrTargetUnavailable, targetID, err)
	}
	proposal, err := newWorkItemUpdateProposal(target, input)
	if err != nil {
		return nil, "", err
	}
	return proposal, producer, nil
}

func validateWorkItemUpdateInput(input ProposeWorkItemUpdateInput) (string, Producer, error) {
	targetID := strings.TrimSpace(input.TargetWorkItemID)
	if targetID == "" {
		return "", "", fmt.Errorf("%w: target work item ID must not be empty", ErrInvalidInput)
	}
	if strings.TrimSpace(input.ProposedTitle) == "" && input.ProposedDescription == nil {
		return "", "", fmt.Errorf("%w: proposed title or proposed description must be provided", ErrInvalidInput)
	}
	producer := input.Producer
	if producer == "" {
		producer = ProducerUser
	}
	if !producer.valid() {
		return "", "", fmt.Errorf("%w: unsupported Proposal producer %q", ErrInvalidInput, input.Producer)
	}
	return targetID, producer, nil
}

// newWorkItemUpdateProposal verifies the optional expected base against the
// authoritative target and captures it as the optimistic preconditions.
func newWorkItemUpdateProposal(target *workitem.WorkItem, input ProposeWorkItemUpdateInput) (*Proposal, error) {
	if base := input.ExpectedBase; base != nil {
		if base.LastRevisionID != target.LastRevisionID ||
			base.Title != target.Title ||
			!optionalStringEqual(base.Description, target.Description) {
			return nil, fmt.Errorf("%w: %s", ErrBaseChanged, target.ID)
		}
	}

	proposedTitle := strings.TrimSpace(input.ProposedTitle)
	if proposedTitle == "" {
		proposedTitle = target.Title
	}
	proposedDescription := input.ProposedDescription
	if proposedDescription == nil && input.ExpectedBase != nil {
		proposedDescription = target.Description
	}

	// Capture optimistic preconditions for Slice 2 stale protection.
	preconditions := WorkItemPreconditions{
		ExpectedLastRevisionID: target.LastRevisionID,
		ExpectedTitle:          target.Title,
		ExpectedDescription:    target.Description,
	}

	// Build typed versioned operation payload.
	payload := WorkItemUpdateDetailsPayload{
		OperationKind:       OperationTypeWorkItemUpdateDetails,
		Version:             OperationVersionV1,
		Preconditions:       preconditions,
		ProposedTitle:       proposedTitle,
		ProposedDescription: proposedDescription,
	}
	if err := payload.Validate(); err != nil {
		return nil, err
	}

	proposalID, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate proposal ID: %w", err)
	}

	// Title for the proposal itself
	proposalTitle := fmt.Sprintf("Update work item: %s", target.Title)
	if input.ProposalTitle != nil && strings.TrimSpace(*input.ProposalTitle) != "" {
		proposalTitle = strings.TrimSpace(*input.ProposalTitle)
	}

	now := time.Now().UTC()
	return &Proposal{
		ID:                proposalID,
		TargetEntityID:    ptrString(target.ID),
		OperationType:     OperationTypeWorkItemUpdateDetails,
		Title:             proposalTitle,
		Summary:           input.Summary,
		Reason:            input.Reason,
		Status:            StatusPending,
		Payload:           payload,
		AppliedRevisionID: nil,
		AIRunID:           input.AIRunID,
		SourceRevisionIDs: input.SourceRevisionIDs,
		CreatedAt:         now,
		UpdatedAt:         now,
		ResolvedAt:        nil,
	}, nil
}

// ProposeTrackedItemOperation creates a checked, typed Tracking Proposal.
// It reads current truth but never mutates Tracking or creates a Revision.
func (s *Service) ProposeTrackedItemOperation(ctx context.Context, input ProposeTrackedItemOperationInput) (*Proposal, error) {
	return s.proposeWith(ctx, input)
}

// buildTrackedItemOperation validates input and constructs the pending Proposal without
// persisting it. mode admits symbolic references only for aggregate children.
func (s *Service) buildTrackedItemOperation(ctx context.Context, input ProposeTrackedItemOperationInput, mode buildMode) (*Proposal, Producer, error) {
	if s.trackingOperator == nil {
		return nil, "", fmt.Errorf("%w: Tracking proposal capability unavailable", ErrInvalidInput)
	}
	payload := input.Payload
	payload.Version = OperationVersionV1
	producer := input.Producer
	if producer == "" {
		producer = ProducerUser
	}
	if !producer.valid() {
		return nil, "", ErrInvalidInput
	}
	var targetID string
	if payload.OperationKind == OperationTypeTrackedItemCreate {
		if payload.WorkItemID == nil {
			return nil, "", fmt.Errorf("%w: work item is required", ErrInvalidOperationPayload)
		}
		if mode.isReference(payload.WorkItemID) {
			// A WorkItem created earlier in the same aggregate: its Workflow is
			// the companion output of that same operation, and nothing about it
			// can be read yet. The Proposal target stays NULL.
			if payload.ParentItemID != nil {
				return nil, "", fmt.Errorf("%w: a parent tracked item cannot be resolved in a work item created by the same proposal", ErrInvalidOperationPayload)
			}
			workflowRef, err := companionReference(*payload.WorkItemID, OutputWorkflowID)
			if err != nil {
				return nil, "", err
			}
			payload.WorkflowID = &workflowRef
		} else {
			view, err := s.trackingOperator.GetWorkItemTracking(ctx, *payload.WorkItemID)
			if err != nil {
				return nil, "", fmt.Errorf("%w: %v", ErrTargetUnavailable, err)
			}
			if view.Workflow == nil {
				return nil, "", fmt.Errorf("%w: target has no workflow", ErrTargetUnavailable)
			}
			workflowID := view.Workflow.Workflow.ID
			payload.WorkflowID = &workflowID
			if payload.ParentItemID != nil {
				if parent, parentErr := s.trackingOperator.GetTrackedItem(ctx, *payload.ParentItemID); parentErr == nil {
					parentTitle := parent.Title
					payload.ParentItemTitle = &parentTitle
				}
			}
			targetID = view.WorkItemID
		}
	} else {
		if payload.TrackedItemID == nil {
			return nil, "", fmt.Errorf("%w: tracked item is required", ErrInvalidOperationPayload)
		}
		item, err := s.trackingOperator.GetTrackedItem(ctx, *payload.TrackedItemID)
		if err != nil {
			return nil, "", fmt.Errorf("%w: %v", ErrTargetUnavailable, err)
		}
		workItemID, workflowID, revisionID := item.WorkItemID, item.WorkflowID, item.LastRevisionID
		payload.WorkItemID, payload.WorkflowID, payload.ExpectedLastRevisionID = &workItemID, &workflowID, &revisionID
		currentTitle := item.Title
		payload.ExpectedTitle, payload.ExpectedItemType, payload.ExpectedParentItemID = &currentTitle, item.ItemType, item.ParentItemID
		if item.ParentItemID != nil {
			if parent, parentErr := s.trackingOperator.GetTrackedItem(ctx, *item.ParentItemID); parentErr == nil {
				parentTitle := parent.Title
				payload.ExpectedParentTitle = &parentTitle
			}
		}
		if payload.ParentItemID != nil {
			if parent, parentErr := s.trackingOperator.GetTrackedItem(ctx, *payload.ParentItemID); parentErr == nil {
				parentTitle := parent.Title
				payload.ParentItemTitle = &parentTitle
			}
		}
		if payload.ToTrackedItemID != nil {
			if related, relatedErr := s.trackingOperator.GetTrackedItem(ctx, *payload.ToTrackedItemID); relatedErr == nil {
				relatedTitle := related.Title
				payload.ToTrackedItemTitle = &relatedTitle
			}
		}
		if view, viewErr := s.trackingOperator.GetWorkItemTracking(ctx, workItemID); viewErr == nil {
			for _, step := range view.Workflow.Definition.Steps {
				if payload.WorkflowStepID != nil && step.ID == *payload.WorkflowStepID {
					stepTitle := step.Title
					payload.WorkflowStepTitle = &stepTitle
				}
			}
			for _, itemView := range view.Items {
				if itemView.Item.ID == item.ID && itemView.Progression != nil {
					if itemView.Progression.CurrentState != nil {
						state := string(*itemView.Progression.CurrentState)
						payload.ExpectedState = &state
					}
					for _, step := range view.Workflow.Definition.Steps {
						if itemView.Progression.CurrentStepID != nil && step.ID == *itemView.Progression.CurrentStepID {
							stepTitle := step.Title
							payload.ExpectedWorkflowStep = &stepTitle
						}
					}
				}
			}
		}
		targetID = item.ID
	}
	if err := payload.Validate(); err != nil {
		return nil, "", err
	}
	id, err := identity.New()
	if err != nil {
		return nil, "", err
	}
	now := time.Now().UTC()
	title := strings.TrimSpace(input.ProposalTitle)
	if title == "" {
		title = trackingProposalTitle(payload.OperationKind)
	}
	proposal := &Proposal{
		ID:                id,
		TargetEntityID:    optionalTarget(targetID),
		OperationType:     payload.OperationKind,
		Title:             title,
		Summary:           input.Summary,
		Reason:            input.Reason,
		Status:            StatusPending,
		TrackingPayload:   &payload,
		AIRunID:           input.AIRunID,
		SourceRevisionIDs: input.SourceRevisionIDs,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	return proposal, producer, nil
}

// ProposePlannerItemCreate creates a checked, typed Planner Proposal. It
// never creates the Foundation Entity, Revision, or planner_items row that
// domain truth would require - those exist only after human Apply. When
// input.WorkItemID identifies an existing WorkItem, that WorkItem's ID
// becomes the non-null target (mirroring tracked_item.create's existing
// parent-target pattern); otherwise the Proposal is created with a NULL
// target, which the database CHECK constraint permits only for this
// operation type.
func (s *Service) ProposePlannerItemCreate(ctx context.Context, input ProposePlannerItemCreateInput) (*Proposal, error) {
	return s.proposeWith(ctx, input)
}

// buildPlannerItemCreate validates input and constructs the pending Proposal without
// persisting it. mode admits symbolic references only for aggregate children.
func (s *Service) buildPlannerItemCreate(ctx context.Context, input ProposePlannerItemCreateInput, mode buildMode) (*Proposal, Producer, error) {
	if s.plannerOperator == nil {
		return nil, "", fmt.Errorf("%w: Planner proposal capability unavailable", ErrInvalidInput)
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, "", fmt.Errorf("%w: title must not be blank", ErrInvalidInput)
	}
	producer := input.Producer
	if producer == "" {
		producer = ProducerUser
	}
	if !producer.valid() {
		return nil, "", fmt.Errorf("%w: unsupported Proposal producer %q", ErrInvalidInput, input.Producer)
	}
	// A TrackedItem created earlier in the same aggregate cannot be read yet;
	// preflight derives its WorkItem/Workflow provenance from that operation.
	if input.TrackedItemID != nil && strings.TrimSpace(*input.TrackedItemID) != "" && !mode.isReference(input.TrackedItemID) {
		if s.trackingOperator == nil {
			return nil, "", fmt.Errorf("%w: Tracking proposal capability unavailable", ErrInvalidInput)
		}
		trackedItemID := strings.TrimSpace(*input.TrackedItemID)
		trackedItem, err := s.trackingOperator.GetTrackedItem(ctx, trackedItemID)
		if err != nil {
			return nil, "", fmt.Errorf("%w: tracked item %s: %v", ErrTargetUnavailable, trackedItemID, err)
		}
		input.TrackedItemID = &trackedItemID
		if input.WorkItemID == nil || strings.TrimSpace(*input.WorkItemID) == "" {
			input.WorkItemID = &trackedItem.WorkItemID
		} else if strings.TrimSpace(*input.WorkItemID) != trackedItem.WorkItemID {
			return nil, "", fmt.Errorf("%w: tracked item %s belongs to work item %s", ErrTargetUnavailable, trackedItemID, trackedItem.WorkItemID)
		}
		if input.WorkflowID == nil || strings.TrimSpace(*input.WorkflowID) == "" {
			input.WorkflowID = &trackedItem.WorkflowID
		} else if strings.TrimSpace(*input.WorkflowID) != trackedItem.WorkflowID {
			return nil, "", fmt.Errorf("%w: tracked item %s belongs to workflow %s", ErrTargetUnavailable, trackedItemID, trackedItem.WorkflowID)
		}
	}

	var targetID, workItemID *string
	if mode.isReference(input.WorkItemID) {
		// A WorkItem created earlier in the same aggregate is not a concrete
		// target yet; the reference stays in the payload and the target NULL.
		workItemID = input.WorkItemID
	} else if input.WorkItemID != nil && strings.TrimSpace(*input.WorkItemID) != "" {
		trimmed := strings.TrimSpace(*input.WorkItemID)
		if _, err := s.workItemOperator.GetByID(ctx, trimmed); err != nil {
			return nil, "", fmt.Errorf("%w: %s: %v", ErrTargetUnavailable, trimmed, err)
		}
		targetID = &trimmed
		workItemID = targetID
	}

	payload := PlannerItemCreatePayload{
		OperationKind:  OperationTypePlannerItemCreate,
		Version:        OperationVersionV1,
		WorkItemID:     workItemID,
		WorkflowID:     input.WorkflowID,
		WorkflowStepID: input.WorkflowStepID,
		TrackedItemID:  input.TrackedItemID,
		Title:          title,
		Details:        input.Details,
		DueDate:        input.DueDate,
		DueAt:          input.DueAt,
		OffsetsSeconds: input.OffsetsSeconds,
	}
	if err := payload.Validate(); err != nil {
		return nil, "", err
	}

	id, err := identity.New()
	if err != nil {
		return nil, "", fmt.Errorf("generate proposal ID: %w", err)
	}
	proposalTitle := fmt.Sprintf("Create planner item: %s", title)
	if input.ProposalTitle != nil && strings.TrimSpace(*input.ProposalTitle) != "" {
		proposalTitle = strings.TrimSpace(*input.ProposalTitle)
	}
	now := time.Now().UTC()
	proposal := &Proposal{
		ID:                 id,
		TargetEntityID:     targetID,
		OperationType:      OperationTypePlannerItemCreate,
		Title:              proposalTitle,
		Summary:            input.Summary,
		Reason:             input.Reason,
		Status:             StatusPending,
		PlannerItemPayload: &payload,
		AIRunID:            input.AIRunID,
		SourceRevisionIDs:  input.SourceRevisionIDs,
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	return proposal, producer, nil
}

// ProposeWorkItemLifecycle creates a checked, typed WorkItem lifecycle
// transition Proposal. It captures the current authoritative lifecycle state
// as a precondition (analogous to ExpectedLastRevisionID elsewhere) without
// mutating the WorkItem or creating a Revision.
func (s *Service) ProposeWorkItemLifecycle(ctx context.Context, input ProposeWorkItemLifecycleInput) (*Proposal, error) {
	return s.proposeWith(ctx, input)
}

// buildWorkItemLifecycle validates input and constructs the pending Proposal without
// persisting it. mode admits symbolic references only for aggregate children.
func (s *Service) buildWorkItemLifecycle(ctx context.Context, input ProposeWorkItemLifecycleInput, mode buildMode) (*Proposal, Producer, error) {
	targetID := strings.TrimSpace(input.TargetWorkItemID)
	if targetID == "" {
		return nil, "", fmt.Errorf("%w: target work item ID must not be empty", ErrInvalidInput)
	}
	targetState := strings.TrimSpace(input.TargetState)
	if targetState == "" {
		return nil, "", fmt.Errorf("%w: target state must not be empty", ErrInvalidInput)
	}
	producer := input.Producer
	if producer == "" {
		producer = ProducerUser
	}
	if !producer.valid() {
		return nil, "", fmt.Errorf("%w: unsupported Proposal producer %q", ErrInvalidInput, input.Producer)
	}

	target, err := s.workItemOperator.GetByID(ctx, targetID)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %s: %v", ErrTargetUnavailable, targetID, err)
	}
	if !workitem.LifecycleState(target.Lifecycle).CanTransitionTo(workitem.LifecycleState(targetState)) {
		return nil, "", fmt.Errorf("%w: %s to %s", ErrInvalidOperationPayload, target.Lifecycle, targetState)
	}

	payload := WorkItemSetLifecyclePayload{
		OperationKind: OperationTypeWorkItemSetLifecycle, Version: OperationVersionV1,
		ExpectedLifecycleState: string(target.Lifecycle), TargetState: targetState, Reason: input.Reason,
	}
	if err := payload.Validate(); err != nil {
		return nil, "", err
	}

	id, err := identity.New()
	if err != nil {
		return nil, "", fmt.Errorf("generate proposal ID: %w", err)
	}
	proposalTitle := fmt.Sprintf("Change work item lifecycle: %s -> %s", target.Title, targetState)
	if input.ProposalTitle != nil && strings.TrimSpace(*input.ProposalTitle) != "" {
		proposalTitle = strings.TrimSpace(*input.ProposalTitle)
	}
	now := time.Now().UTC()
	proposal := &Proposal{
		ID:                       id,
		TargetEntityID:           ptrString(target.ID),
		OperationType:            OperationTypeWorkItemSetLifecycle,
		Title:                    proposalTitle,
		Summary:                  input.Summary,
		Reason:                   input.Reason,
		Status:                   StatusPending,
		WorkItemLifecyclePayload: &payload,
		AIRunID:                  input.AIRunID,
		SourceRevisionIDs:        input.SourceRevisionIDs,
		CreatedAt:                now,
		UpdatedAt:                now,
	}

	return proposal, producer, nil
}

// ProposeWorkflowCreate creates a checked, typed Proposal representing one
// semantic action: create/attach a Workflow (with its ordered step
// definition) to the target WorkItem. It never creates the
// WorkflowDefinition or Workflow itself - both are created only atomically
// during Apply.
func (s *Service) ProposeWorkflowCreate(ctx context.Context, input ProposeWorkflowCreateInput) (*Proposal, error) {
	return s.proposeWith(ctx, input)
}

// buildWorkflowCreate validates input and constructs the pending Proposal without
// persisting it. mode admits symbolic references only for aggregate children.
func (s *Service) buildWorkflowCreate(ctx context.Context, input ProposeWorkflowCreateInput, mode buildMode) (*Proposal, Producer, error) {
	if s.trackingOperator == nil {
		return nil, "", fmt.Errorf("%w: Workflow proposal capability unavailable", ErrInvalidInput)
	}
	targetID := strings.TrimSpace(input.TargetWorkItemID)
	if targetID == "" {
		return nil, "", fmt.Errorf("%w: target work item ID must not be empty", ErrInvalidInput)
	}
	definitionName := strings.TrimSpace(input.DefinitionName)
	if definitionName == "" {
		return nil, "", fmt.Errorf("%w: definition name must not be empty", ErrInvalidInput)
	}
	producer := input.Producer
	if producer == "" {
		producer = ProducerUser
	}
	if !producer.valid() {
		return nil, "", fmt.Errorf("%w: unsupported Proposal producer %q", ErrInvalidInput, input.Producer)
	}

	target, err := s.workItemOperator.GetByID(ctx, targetID)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %s: %v", ErrTargetUnavailable, targetID, err)
	}
	// At-most-one-Workflow-per-WorkItem is authoritatively enforced again at
	// Apply time inside CreateWorkflowTx; this is an early, honest rejection
	// so the user is not asked to review a Proposal that cannot possibly
	// apply.
	if view, viewErr := s.trackingOperator.GetWorkItemTracking(ctx, target.ID); viewErr == nil && view.Workflow != nil {
		return nil, "", fmt.Errorf("%w: work item %s already has a workflow", ErrInvalidOperationPayload, target.ID)
	}

	payload := WorkflowCreatePayload{
		OperationKind: OperationTypeWorkflowCreate, Version: OperationVersionV1,
		ExpectedLastRevisionID: &target.LastRevisionID, DefinitionName: definitionName, Steps: input.Steps,
	}
	if err := payload.Validate(); err != nil {
		return nil, "", err
	}

	id, err := identity.New()
	if err != nil {
		return nil, "", fmt.Errorf("generate proposal ID: %w", err)
	}
	proposalTitle := fmt.Sprintf("Create workflow for: %s", target.Title)
	if input.ProposalTitle != nil && strings.TrimSpace(*input.ProposalTitle) != "" {
		proposalTitle = strings.TrimSpace(*input.ProposalTitle)
	}
	now := time.Now().UTC()
	proposal := &Proposal{
		ID:                    id,
		TargetEntityID:        ptrString(target.ID),
		OperationType:         OperationTypeWorkflowCreate,
		Title:                 proposalTitle,
		Summary:               input.Summary,
		Reason:                input.Reason,
		Status:                StatusPending,
		WorkflowCreatePayload: &payload,
		AIRunID:               input.AIRunID,
		SourceRevisionIDs:     input.SourceRevisionIDs,
		CreatedAt:             now,
		UpdatedAt:             now,
	}

	return proposal, producer, nil
}

// ProposeWorkItemCreate creates a checked, typed Proposal for creating a
// brand-new WorkItem root entity. Like planner.item.create, it has a NULL
// TargetEntityID because no WorkItem entity exists until human Apply.
func (s *Service) ProposeWorkItemCreate(ctx context.Context, input ProposeWorkItemCreateInput) (*Proposal, error) {
	return s.proposeWith(ctx, input)
}

// buildWorkItemCreate validates input and constructs the pending Proposal without
// persisting it. mode admits symbolic references only for aggregate children.
func (s *Service) buildWorkItemCreate(ctx context.Context, input ProposeWorkItemCreateInput, mode buildMode) (*Proposal, Producer, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, "", fmt.Errorf("%w: title must not be blank", ErrInvalidInput)
	}
	producer := input.Producer
	if producer == "" {
		producer = ProducerUser
	}
	if !producer.valid() {
		return nil, "", fmt.Errorf("%w: unsupported Proposal producer %q", ErrInvalidInput, input.Producer)
	}

	payload := WorkItemCreatePayload{
		OperationKind: OperationTypeWorkItemCreate,
		Version:       OperationVersionV1,
		Title:         title,
		Description:   normalizeOptional(input.Description),
	}
	if err := payload.Validate(); err != nil {
		return nil, "", err
	}

	id, err := identity.New()
	if err != nil {
		return nil, "", fmt.Errorf("generate proposal ID: %w", err)
	}
	proposalTitle := fmt.Sprintf("Create work item: %s", title)
	if input.ProposalTitle != nil && strings.TrimSpace(*input.ProposalTitle) != "" {
		proposalTitle = strings.TrimSpace(*input.ProposalTitle)
	}
	now := time.Now().UTC()
	proposal := &Proposal{
		ID:                    id,
		TargetEntityID:        nil,
		OperationType:         OperationTypeWorkItemCreate,
		Title:                 proposalTitle,
		Summary:               input.Summary,
		Reason:                input.Reason,
		Status:                StatusPending,
		WorkItemCreatePayload: &payload,
		AIRunID:               input.AIRunID,
		SourceRevisionIDs:     input.SourceRevisionIDs,
		CreatedAt:             now,
		UpdatedAt:             now,
	}

	return proposal, producer, nil
}

// ProposeWorkItemCreateWithWorkflow creates a checked, typed Proposal for atomically creating
// a WorkItem along with its initial Workflow stages.
func (s *Service) ProposeWorkItemCreateWithWorkflow(ctx context.Context, input ProposeWorkItemCreateWithWorkflowInput) (*Proposal, error) {
	return s.proposeWith(ctx, input)
}

// buildWorkItemCreateWithWorkflow validates input and constructs the pending Proposal without
// persisting it. mode admits symbolic references only for aggregate children.
func (s *Service) buildWorkItemCreateWithWorkflow(ctx context.Context, input ProposeWorkItemCreateWithWorkflowInput, mode buildMode) (*Proposal, Producer, error) {
	if s.trackingOperator == nil {
		return nil, "", fmt.Errorf("%w: Tracking proposal capability unavailable", ErrInvalidInput)
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, "", fmt.Errorf("%w: title must not be blank", ErrInvalidInput)
	}
	workflowName := strings.TrimSpace(input.WorkflowName)
	if workflowName == "" {
		return nil, "", fmt.Errorf("%w: workflow name must not be blank", ErrInvalidInput)
	}
	if len(input.Stages) == 0 {
		return nil, "", fmt.Errorf("%w: workflow needs at least one stage", ErrInvalidInput)
	}
	stages := make([]string, len(input.Stages))
	for i, st := range input.Stages {
		stTrimmed := strings.TrimSpace(st)
		if stTrimmed == "" {
			return nil, "", fmt.Errorf("%w: stage %d must not be blank", ErrInvalidInput, i+1)
		}
		stages[i] = stTrimmed
	}

	producer := input.Producer
	if producer == "" {
		producer = ProducerUser
	}
	if !producer.valid() {
		return nil, "", fmt.Errorf("%w: unsupported Proposal producer %q", ErrInvalidInput, input.Producer)
	}

	payload := WorkItemCreateWithWorkflowPayload{
		OperationKind: OperationTypeWorkItemCreateWithWorkflow,
		Version:       OperationVersionV1,
		Title:         title,
		Description:   normalizeOptional(input.Description),
		WorkflowName:  workflowName,
		Stages:        stages,
	}
	if err := payload.Validate(); err != nil {
		return nil, "", err
	}

	id, err := identity.New()
	if err != nil {
		return nil, "", fmt.Errorf("generate proposal ID: %w", err)
	}
	proposalTitle := fmt.Sprintf("Create work item with workflow: %s", title)
	if input.ProposalTitle != nil && strings.TrimSpace(*input.ProposalTitle) != "" {
		proposalTitle = strings.TrimSpace(*input.ProposalTitle)
	}
	now := time.Now().UTC()
	proposal := &Proposal{
		ID:                                id,
		TargetEntityID:                    nil,
		OperationType:                     OperationTypeWorkItemCreateWithWorkflow,
		Title:                             proposalTitle,
		Summary:                           input.Summary,
		Reason:                            input.Reason,
		Status:                            StatusPending,
		WorkItemCreateWithWorkflowPayload: &payload,
		AIRunID:                           input.AIRunID,
		SourceRevisionIDs:                 input.SourceRevisionIDs,
		CreatedAt:                         now,
		UpdatedAt:                         now,
	}

	return proposal, producer, nil
}

// ProposeMonitorPointCreate creates a checked, typed Proposal for creating an
// authoritative MonitorPoint expectation for a WorkItem or same-WorkItem TrackedItem.
func (s *Service) ProposeMonitorPointCreate(ctx context.Context, input ProposeMonitorPointCreateInput) (*Proposal, error) {
	return s.proposeWith(ctx, input)
}

// buildMonitorPointCreate validates input and constructs the pending Proposal without
// persisting it. mode admits symbolic references only for aggregate children.
func (s *Service) buildMonitorPointCreate(ctx context.Context, input ProposeMonitorPointCreateInput, mode buildMode) (*Proposal, Producer, error) {
	if s.trackingOperator == nil {
		return nil, "", fmt.Errorf("%w: Tracking proposal capability unavailable", ErrInvalidInput)
	}
	workItemID := strings.TrimSpace(input.WorkItemID)
	if workItemID == "" {
		return nil, "", fmt.Errorf("%w: work item ID must not be empty", ErrInvalidInput)
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, "", fmt.Errorf("%w: title must not be blank", ErrInvalidInput)
	}
	if input.DueAt.IsZero() {
		return nil, "", fmt.Errorf("%w: due time is required", ErrInvalidInput)
	}
	timezone := strings.TrimSpace(input.Timezone)
	if timezone == "" {
		return nil, "", fmt.Errorf("%w: timezone is required", ErrInvalidInput)
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return nil, "", fmt.Errorf("%w: unknown timezone %q", ErrInvalidInput, timezone)
	}
	producer := input.Producer
	if producer == "" {
		producer = ProducerUser
	}
	if !producer.valid() {
		return nil, "", fmt.Errorf("%w: unsupported Proposal producer %q", ErrInvalidInput, input.Producer)
	}

	targetWI, err := s.workItemOperator.GetByID(ctx, workItemID)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %s: %v", ErrTargetUnavailable, workItemID, err)
	}

	payload := MonitorPointCreatePayload{
		OperationKind:              OperationTypeMonitorPointCreate,
		Version:                    OperationVersionV1,
		WorkItemID:                 workItemID,
		ExpectedWorkItemRevisionID: &targetWI.LastRevisionID,
		Kind:                       input.Kind,
		Title:                      title,
		DueAt:                      input.DueAt.UTC().Format(time.RFC3339),
		Timezone:                   timezone,
		ExpectedWorkItemLifecycle:  input.ExpectedWorkItemLifecycle,
		ExpectedStepID:             input.ExpectedStepID,
		ExpectedStepTitle:          input.ExpectedStepTitle,
	}

	if input.SubjectTrackedItemID != nil {
		subjectID := strings.TrimSpace(*input.SubjectTrackedItemID)
		item, err := s.trackingOperator.GetTrackedItem(ctx, subjectID)
		if err != nil {
			return nil, "", fmt.Errorf("%w: subject tracked item %s: %v", ErrTargetUnavailable, subjectID, err)
		}
		if item.WorkItemID != workItemID {
			return nil, "", fmt.Errorf("%w: tracked item %s belongs to a different work item", ErrInvalidInput, subjectID)
		}
		payload.SubjectTrackedItemID = &item.ID
		payload.ExpectedTrackedItemRevisionID = &item.LastRevisionID
		if input.SubjectTrackedItemTitle != nil {
			payload.SubjectTrackedItemTitle = input.SubjectTrackedItemTitle
		} else {
			payload.SubjectTrackedItemTitle = &item.Title
		}
	}

	if err := payload.Validate(); err != nil {
		return nil, "", err
	}

	id, err := identity.New()
	if err != nil {
		return nil, "", fmt.Errorf("generate proposal ID: %w", err)
	}
	proposalTitle := fmt.Sprintf("Create monitor point: %s", title)
	if input.ProposalTitle != nil && strings.TrimSpace(*input.ProposalTitle) != "" {
		proposalTitle = strings.TrimSpace(*input.ProposalTitle)
	}
	now := time.Now().UTC()
	proposal := &Proposal{
		ID:                        id,
		TargetEntityID:            ptrString(workItemID),
		OperationType:             OperationTypeMonitorPointCreate,
		Title:                     proposalTitle,
		Summary:                   input.Summary,
		Reason:                    input.Reason,
		Status:                    StatusPending,
		MonitorPointCreatePayload: &payload,
		AIRunID:                   input.AIRunID,
		SourceRevisionIDs:         input.SourceRevisionIDs,
		CreatedAt:                 now,
		UpdatedAt:                 now,
	}

	return proposal, producer, nil
}

// ProposeMonitorPointSetState creates a checked, typed Proposal for transitioning
// a MonitorPoint's state (ACTIVE -> SATISFIED / CANCELLED).
func (s *Service) ProposeMonitorPointSetState(ctx context.Context, input ProposeMonitorPointSetStateInput) (*Proposal, error) {
	return s.proposeWith(ctx, input)
}

// buildMonitorPointSetState validates input and constructs the pending Proposal without
// persisting it. mode admits symbolic references only for aggregate children.
func (s *Service) buildMonitorPointSetState(ctx context.Context, input ProposeMonitorPointSetStateInput, mode buildMode) (*Proposal, Producer, error) {
	if s.trackingOperator == nil {
		return nil, "", fmt.Errorf("%w: Tracking proposal capability unavailable", ErrInvalidInput)
	}
	monitorID := strings.TrimSpace(input.MonitorPointID)
	if monitorID == "" {
		return nil, "", fmt.Errorf("%w: monitor point ID must not be empty", ErrInvalidInput)
	}
	targetState := strings.TrimSpace(input.TargetState)
	if targetState != string(tracking.MonitorSatisfied) && targetState != string(tracking.MonitorCancelled) {
		return nil, "", fmt.Errorf("%w: target state must be SATISFIED or CANCELLED, got %q", ErrInvalidInput, targetState)
	}
	producer := input.Producer
	if producer == "" {
		producer = ProducerUser
	}
	if !producer.valid() {
		return nil, "", fmt.Errorf("%w: unsupported Proposal producer %q", ErrInvalidInput, input.Producer)
	}

	current, err := s.trackingOperator.GetMonitorPoint(ctx, monitorID)
	if err != nil {
		return nil, "", fmt.Errorf("%w: %s: %v", ErrTargetUnavailable, monitorID, err)
	}
	if current.State != tracking.MonitorActive {
		return nil, "", fmt.Errorf("%w: cannot transition monitor point from terminal state %s", ErrInvalidOperationPayload, current.State)
	}

	payload := MonitorPointSetStatePayload{
		OperationKind:          OperationTypeMonitorPointSetState,
		Version:                OperationVersionV1,
		MonitorPointID:         current.ID,
		WorkItemID:             current.WorkItemID,
		MonitorPointTitle:      current.Title,
		ExpectedLastRevisionID: current.LastRevisionID,
		ExpectedState:          string(current.State),
		TargetState:            targetState,
	}
	if err := payload.Validate(); err != nil {
		return nil, "", err
	}

	id, err := identity.New()
	if err != nil {
		return nil, "", fmt.Errorf("generate proposal ID: %w", err)
	}
	proposalTitle := fmt.Sprintf("Set monitor point state: %s -> %s", current.Title, targetState)
	if input.ProposalTitle != nil && strings.TrimSpace(*input.ProposalTitle) != "" {
		proposalTitle = strings.TrimSpace(*input.ProposalTitle)
	}
	now := time.Now().UTC()
	proposal := &Proposal{
		ID:                          id,
		TargetEntityID:              ptrString(current.ID),
		OperationType:               OperationTypeMonitorPointSetState,
		Title:                       proposalTitle,
		Summary:                     input.Summary,
		Reason:                      input.Reason,
		Status:                      StatusPending,
		MonitorPointSetStatePayload: &payload,
		AIRunID:                     input.AIRunID,
		SourceRevisionIDs:           input.SourceRevisionIDs,
		CreatedAt:                   now,
		UpdatedAt:                   now,
	}

	return proposal, producer, nil
}

// ProposeWorkflowSetCurrentStep creates a checked, typed Proposal for moving
// a Workflow's authoritative current step.
func (s *Service) ProposeWorkflowSetCurrentStep(ctx context.Context, input ProposeWorkflowSetCurrentStepInput) (*Proposal, error) {
	return s.proposeWith(ctx, input)
}

// buildWorkflowSetCurrentStep validates input and constructs the pending Proposal without
// persisting it. mode admits symbolic references only for aggregate children.
func (s *Service) buildWorkflowSetCurrentStep(ctx context.Context, input ProposeWorkflowSetCurrentStepInput, mode buildMode) (*Proposal, Producer, error) {
	if s.trackingOperator == nil {
		return nil, "", fmt.Errorf("%w: Tracking proposal capability unavailable", ErrInvalidInput)
	}
	workItemID := strings.TrimSpace(input.WorkItemID)
	workflowID := strings.TrimSpace(input.WorkflowID)
	targetStepID := strings.TrimSpace(input.TargetStepID)
	if workItemID == "" || workflowID == "" || targetStepID == "" {
		return nil, "", fmt.Errorf("%w: work item, workflow, and target step ID are required", ErrInvalidInput)
	}
	producer := input.Producer
	if producer == "" {
		producer = ProducerUser
	}
	if !producer.valid() {
		return nil, "", fmt.Errorf("%w: unsupported Proposal producer %q", ErrInvalidInput, input.Producer)
	}

	wf, err := s.trackingOperator.GetWorkflow(ctx, workflowID)
	if err != nil {
		return nil, "", fmt.Errorf("%w: workflow %s: %v", ErrTargetUnavailable, workflowID, err)
	}
	if wf.WorkItemID != workItemID {
		return nil, "", fmt.Errorf("%w: workflow %s does not belong to work item %s", ErrInvalidInput, workflowID, workItemID)
	}

	targetTitle := input.TargetStepTitle
	var currentTitle *string
	if trackingView, err := s.trackingOperator.GetWorkItemTracking(ctx, workItemID); err == nil && trackingView != nil && trackingView.Workflow != nil {
		for _, step := range trackingView.Workflow.Definition.Steps {
			if step.ID == targetStepID && targetTitle == "" {
				targetTitle = step.Title
			}
			if step.ID == wf.CurrentStepID {
				stepTitle := step.Title
				currentTitle = &stepTitle
			}
		}
	}
	if targetTitle == "" {
		targetTitle = targetStepID
	}

	payload := WorkflowSetCurrentStepPayload{
		OperationKind:              OperationTypeWorkflowSetCurrentStep,
		Version:                    OperationVersionV1,
		WorkItemID:                 workItemID,
		WorkflowID:                 workflowID,
		TargetStepID:               targetStepID,
		TargetStepTitle:            targetTitle,
		ExpectedWorkflowRevisionID: wf.LastRevisionID,
		ExpectedCurrentStepID:      wf.CurrentStepID,
		ExpectedCurrentStepTitle:   currentTitle,
	}
	if err := payload.Validate(); err != nil {
		return nil, "", err
	}

	id, err := identity.New()
	if err != nil {
		return nil, "", fmt.Errorf("generate proposal ID: %w", err)
	}
	proposalTitle := fmt.Sprintf("Move workflow step to: %s", targetTitle)
	if input.ProposalTitle != nil && strings.TrimSpace(*input.ProposalTitle) != "" {
		proposalTitle = strings.TrimSpace(*input.ProposalTitle)
	}
	now := time.Now().UTC()
	proposal := &Proposal{
		ID:                            id,
		TargetEntityID:                ptrString(workItemID),
		OperationType:                 OperationTypeWorkflowSetCurrentStep,
		Title:                         proposalTitle,
		Summary:                       input.Summary,
		Reason:                        input.Reason,
		Status:                        StatusPending,
		WorkflowSetCurrentStepPayload: &payload,
		AIRunID:                       input.AIRunID,
		SourceRevisionIDs:             input.SourceRevisionIDs,
		CreatedAt:                     now,
		UpdatedAt:                     now,
	}

	return proposal, producer, nil
}

func trackingProposalTitle(kind string) string {
	switch kind {
	case OperationTypeTrackedItemCreate:
		return "Create tracked item"
	case OperationTypeTrackedItemUpdate:
		return "Update tracked item"
	case OperationTypeTrackedItemStageState:
		return "Move tracked item"
	case OperationTypeTrackedItemArchive:
		return "Remove tracked item"
	case OperationTypeTrackedItemDependencyAdd:
		return "Add tracked item dependency"
	case OperationTypeTrackedItemDependencyRemove:
		return "Remove tracked item dependency"
	default:
		return "Change tracked item"
	}
}

// GetByID loads a Proposal by ID using a repository bound to *sql.DB.
func (s *Service) GetByID(ctx context.Context, id string) (*Proposal, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("%w: ID must not be empty", ErrInvalidInput)
	}
	repo := NewRepository(s.db)
	return repo.GetByID(ctx, id)
}

// ListPending loads all pending proposals in deterministic creation order.
func (s *Service) ListPending(ctx context.Context) ([]Proposal, error) {
	repo := NewRepository(s.db)
	return repo.ListPending(ctx)
}

// FindPendingBySourceRevision returns a pending proposal associated with the source revision, or nil if none exists.
func (s *Service) FindPendingBySourceRevision(ctx context.Context, sourceRevisionID string) (*Proposal, error) {
	sourceRevisionID = strings.TrimSpace(sourceRevisionID)
	if sourceRevisionID == "" {
		return nil, fmt.Errorf("%w: source revision ID must not be empty", ErrInvalidInput)
	}
	repo := NewRepository(s.db)
	return repo.FindPendingBySourceRevision(ctx, sourceRevisionID)
}

// GetSummary returns aggregate pending proposal counts and affected target entity counts.
func (s *Service) GetSummary(ctx context.Context) (ProposalSummary, error) {
	proposals, err := s.ListPending(ctx)
	if err != nil {
		return ProposalSummary{}, err
	}
	affected := make(map[string]struct{})
	for _, p := range proposals {
		// A root-create Proposal (nil target) affects zero existing entities.
		if p.TargetEntityID != nil {
			affected[*p.TargetEntityID] = struct{}{}
		}
	}
	return ProposalSummary{
		PendingCount:          len(proposals),
		AffectedWorkItemCount: len(affected),
	}, nil
}

// RejectProposal transitions a PENDING proposal to REJECTED via conditional CAS.
// Same-state Reject on an already REJECTED proposal is an idempotent no-op.
// Rejecting a proposal in APPLIED or STALE status returns ErrInvalidStatusTransition.
// WorkItem domain state and revisions are never modified.
func (s *Service) RejectProposal(ctx context.Context, input RejectProposalInput) (*Proposal, error) {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		return nil, fmt.Errorf("%w: ID must not be empty", ErrInvalidInput)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin reject proposal transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	repo := NewRepository(tx)
	now := time.Now().UTC()

	// Attempt conditional CAS: PENDING -> REJECTED
	updated, err := repo.TransitionStatusCAS(ctx, id, StatusPending, StatusRejected, &now, now)
	if err == nil {
		if _, err := recordProposalTransition(ctx, tx, updated, history.EventProposalRejected, history.ActorUser, nil); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, fmt.Errorf("commit reject proposal transaction: %w", err)
		}
		return updated, nil
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	// CAS did not match a PENDING row. Inspect current state to provide deterministic semantics.
	current, getErr := repo.GetByID(ctx, id)
	if getErr != nil {
		return nil, getErr
	}

	if current.Status == StatusRejected {
		// Idempotent no-op: already rejected, return current state without changes
		return current, nil
	}

	// Disallowed transition from terminal states (APPLIED, STALE)
	return nil, fmt.Errorf(
		"%w: cannot reject proposal in status %s",
		ErrInvalidStatusTransition,
		current.Status,
	)
}

// ApplyProposal executes the atomic Proposal Apply use case or transitions
// to STALE if preconditions are violated.
//
// Invariants enforced:
//  1. Proposal outer tx coordinates the entire sequence.
//  2. Proposal is read in tx.
//  3. WorkItem authoritative state is read through the SAME tx via GetByIDTx
//     to avoid pool deadlock with MaxOpenConns=1.
//  4. Preconditions are validated against authoritative target truth.
//     - Primary guard: ExpectedLastRevisionID.
//     - Supplementary: ExpectedTitle, ExpectedDescription.
//     - If target disappeared, was archived, or preconditions mismatch:
//     CAS PENDING -> STALE, commit tx, return (Outcome: ApplyOutcomeStale, err: nil).
//  5. If preconditions match:
//     - Authoritative WorkItem mutation executed via UpdateLocalWorkItemDetailsTx
//     (creates exactly 1 Foundation Revision with origin USER).
//     - CAS PENDING -> APPLIED with applied_revision_id = updatedWI.LastRevisionID.
//     - Commit tx, return (Outcome: ApplyOutcomeApplied, err: nil).
//  6. Already APPLIED proposals are idempotent no-ops:
//     return (Outcome: ApplyOutcomeAlreadyApplied, err: nil) without new revisions.
//  7. Any failure during execution cleanly rolls back the entire transaction.
//  8. An aggregate Proposal applies all children in this one transaction and
//     commits once (applyAggregateTx); see apply.go for transaction ownership.
func (s *Service) ApplyProposal(ctx context.Context, input ApplyProposalInput) (*ApplyProposalResult, error) {
	id := strings.TrimSpace(input.ID)
	if id == "" {
		return nil, fmt.Errorf("%w: ID must not be empty", ErrInvalidInput)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin apply proposal transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	repo := NewRepository(tx)

	// 1. Reload durable Proposal in transaction.
	prop, err := repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// 2. Check proposal lifecycle status.
	switch prop.Status {
	case StatusApplied:
		// Idempotent no-op: already applied, perform no second mutation or revision.
		_ = tx.Rollback()
		if prop.OperationType == OperationTypeWorkItemUpdateDetails {
			wi, err := s.workItemOperator.GetByID(ctx, targetOrEmpty(prop))
			if err != nil {
				return nil, fmt.Errorf("load applied work item: %w", err)
			}
			return &ApplyProposalResult{
				Outcome:  ApplyOutcomeAlreadyApplied,
				Proposal: prop,
				WorkItem: wi,
			}, nil
		}
		return &ApplyProposalResult{
			Outcome:  ApplyOutcomeAlreadyApplied,
			Proposal: prop,
		}, nil

	case StatusRejected, StatusStale:
		return nil, fmt.Errorf("%w: cannot apply proposal with status %s", ErrInvalidStatusTransition, prop.Status)

	case StatusPending:
		// Eligible to evaluate preconditions and apply.

	default:
		return nil, fmt.Errorf("%w: unsupported proposal status %q", ErrInvalidStatusTransition, prop.Status)
	}

	// A Proposal with child operations is an aggregate; every other Proposal
	// keeps the unchanged singular Apply path.
	if prop.IsAggregate() {
		return s.applyAggregateTx(ctx, tx, repo, prop)
	}
	return s.applySingularTx(ctx, tx, repo, prop)
}

// markStaleTx is the singular CAS-to-STALE path, used once an operation's
// authoritative target disappeared or no longer matches its precondition.
func (s *Service) markStaleTx(ctx context.Context, tx *sql.Tx, repo *Repository, prop *Proposal, target *workitem.WorkItem) (*ApplyProposalResult, error) {
	now := time.Now().UTC()
	staleProp, casErr := repo.MarkProposalStaleIfPending(ctx, prop.ID, now, now)
	if casErr != nil {
		return nil, casErr
	}
	if _, err := recordProposalTransition(ctx, tx, staleProp, history.EventProposalStale, history.ActorSystem, nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit stale proposal transaction: %w", err)
	}
	return &ApplyProposalResult{Outcome: ApplyOutcomeStale, Proposal: staleProp, WorkItem: target}, nil
}

func value(input *string) string {
	if input == nil {
		return ""
	}
	return *input
}

func normalizeOptional(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// optionalTarget is ptrString for a target that may not exist yet: an empty ID
// (an entity created earlier in the same aggregate) yields a NULL target.
func optionalTarget(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

// ptrString returns a pointer to a copy of s, for constructing the nullable
// Proposal.TargetEntityID field from an always-present existing-entity ID.
func ptrString(s string) *string { return &s }

// targetOrEmpty safely dereferences a possibly-nil TargetEntityID. Callers
// use it only where the operation family guarantees a non-nil target (every
// family except RootCreateOperationTypes); it returns "" defensively rather
// than panicking if that invariant is ever violated.
func targetOrEmpty(p *Proposal) string {
	if p == nil || p.TargetEntityID == nil {
		return ""
	}
	return *p.TargetEntityID
}

func optionalStringEqual(a, b *string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
