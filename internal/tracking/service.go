package tracking

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"lum-harbor-notifier/internal/foundation"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/identity"
	"lum-harbor-notifier/internal/workitem"
)

const entityTypeTrackedItem = "TRACKED_ITEM"

// WorkItemOperator is the narrow, transaction-aware WorkItem read Tracking needs
// to verify the owning WorkItem is current (unarchived) inside a mutation.
type WorkItemOperator interface {
	GetByID(ctx context.Context, id string) (*workitem.WorkItem, error)
	GetByIDTx(ctx context.Context, tx *sql.Tx, id string) (*workitem.WorkItem, error)
}

// WorkflowStepInput is one step of a new definition. Sequence is derived from
// slice order (1..N), so it is always unique and deterministic.
type WorkflowStepInput struct {
	Key      string
	Title    string
	Terminal bool
}

// CreateWorkflowDefinitionInput contains only caller-authored process facts.
type CreateWorkflowDefinitionInput struct {
	Name    string
	Version int
	Steps   []WorkflowStepInput
}

// CreateWorkflowInput binds a WorkflowDefinition to a WorkItem as its single Workflow.
//
// CurrentStepID is optional: when omitted the Workflow starts at the first
// (lowest sequence) step of its definition. StartedAt is optional: when omitted
// the Workflow's actual start is its creation time. StartedAt means the actual
// Workflow start, never a planned start.
type CreateWorkflowInput struct {
	WorkItemID           string
	WorkflowDefinitionID string
	CurrentStepID        *string
	StartedAt            *time.Time
}

// SetWorkflowCurrentStepInput explicitly moves a Workflow's own current step,
// forward or backward. It never touches TrackedItem states.
type SetWorkflowCurrentStepInput struct {
	WorkflowID string
	StepID     string
}

// CreateTrackedItemInput contains only product semantics. Identity, timestamps,
// Revision and initial step states are service-owned.
type CreateTrackedItemInput struct {
	WorkItemID   string
	WorkflowID   *string
	ParentItemID *string
	Title        string
	ItemType     *string
}

// SetStepStateInput is the narrow typed command for one step's state.
type SetStepStateInput struct {
	TrackedItemID  string
	WorkflowStepID string
	State          StepState
}

type UpdateTrackedItemInput struct {
	ID           string
	Title        string
	ItemType     *string
	ParentItemID *string
}

type SetTrackedItemStageStateInput struct {
	TrackedItemID  string
	WorkflowStepID string
	State          StepState
}

type TrackedItemRelationInput struct {
	FromTrackedItemID string
	ToTrackedItemID   string
}

// Service owns Tracking validation, transactions and Revision creation.
// Tracking commands are direct, human-explicit commands (origin USER).
type Service struct {
	db        *sql.DB
	workItems WorkItemOperator
}

func NewService(db *sql.DB, workItems WorkItemOperator) *Service {
	return &Service{db: db, workItems: workItems}
}

// CreateWorkflowDefinition atomically persists a definition and its ordered
// steps. Definitions are configuration: no Entity, no Revision, and no
// update/delete path exists.
func (s *Service) CreateWorkflowDefinition(ctx context.Context, input CreateWorkflowDefinitionInput) (*WorkflowDefinition, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create workflow definition transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	def, err := s.CreateWorkflowDefinitionTx(ctx, tx, input)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create workflow definition transaction: %w", err)
	}
	return def, nil
}

// CreateWorkflowDefinitionTx executes the same definition-creation use case
// within a caller-provided transaction, so a Proposal Apply can create the
// definition and its attached Workflow atomically. Definitions remain
// non-revisioned configuration; this call alone produces no Revision.
func (s *Service) CreateWorkflowDefinitionTx(ctx context.Context, tx *sql.Tx, input CreateWorkflowDefinitionInput) (*WorkflowDefinition, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return nil, fmt.Errorf("%w: workflow name must not be blank", ErrInvalidInput)
	}
	if input.Version <= 0 {
		return nil, fmt.Errorf("%w: workflow version must be positive", ErrInvalidInput)
	}
	if len(input.Steps) == 0 {
		return nil, fmt.Errorf("%w: workflow needs at least one step", ErrInvalidInput)
	}
	definitionID, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate workflow definition ID: %w", err)
	}
	def := &WorkflowDefinition{ID: definitionID, Name: name, Version: input.Version, Active: true, CreatedAt: time.Now().UTC()}
	seen := map[string]struct{}{}
	for index, in := range input.Steps {
		key, title := strings.TrimSpace(in.Key), strings.TrimSpace(in.Title)
		if key == "" || title == "" {
			return nil, fmt.Errorf("%w: step %d needs a key and a title", ErrInvalidInput, index+1)
		}
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("%w: duplicate step key %q", ErrInvalidInput, key)
		}
		seen[key] = struct{}{}
		stepID, err := identity.New()
		if err != nil {
			return nil, fmt.Errorf("generate workflow step ID: %w", err)
		}
		def.Steps = append(def.Steps, WorkflowStep{
			ID: stepID, WorkflowDefinitionID: definitionID, Key: key, Title: title,
			Sequence: index + 1, Terminal: in.Terminal,
		})
	}

	if err := NewRepository(tx).createDefinition(ctx, def); err != nil {
		return nil, err
	}
	return def, nil
}

// CreateWorkflow creates the single Workflow for a WorkItem. A WorkItem can
// have at most one Workflow: creating a second one is rejected.
func (s *Service) CreateWorkflow(ctx context.Context, input CreateWorkflowInput) (*Workflow, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create workflow transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	wf, err := s.CreateWorkflowTx(ctx, tx, input)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create workflow transaction: %w", err)
	}
	return wf, nil
}

// CreateWorkflowTx executes the same Workflow-attachment use case within a
// caller-provided transaction, so a Proposal Apply can create the definition
// and attach the Workflow to the target WorkItem atomically, producing
// exactly the one Revision this call already produces on its own.
func (s *Service) CreateWorkflowTx(ctx context.Context, tx *sql.Tx, input CreateWorkflowInput) (*Workflow, error) {
	workItemID := strings.TrimSpace(input.WorkItemID)
	definitionID := strings.TrimSpace(input.WorkflowDefinitionID)
	if workItemID == "" || definitionID == "" {
		return nil, fmt.Errorf("%w: work item ID and workflow definition ID are required", ErrInvalidInput)
	}

	if _, err := s.workItems.GetByIDTx(ctx, tx, workItemID); err != nil {
		if errors.Is(err, workitem.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrWorkItemUnavailable, workItemID)
		}
		return nil, err
	}

	repo := NewRepository(tx)
	def, err := repo.GetDefinition(ctx, definitionID)
	if err != nil {
		return nil, err
	}
	if !def.Active {
		return nil, fmt.Errorf("%w: %s", ErrWorkflowInactive, def.ID)
	}

	// A Workflow always has a current step; a definition without steps cannot start one.
	if len(def.Steps) == 0 {
		return nil, fmt.Errorf("%w: workflow definition %s has no steps", ErrInvalidInput, def.ID)
	}
	current := def.Steps[0]
	if input.CurrentStepID != nil && strings.TrimSpace(*input.CurrentStepID) != "" {
		requested := strings.TrimSpace(*input.CurrentStepID)
		found := false
		for _, step := range def.Steps {
			if step.ID == requested {
				current, found = step, true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("%w: %s", ErrStepNotInWorkflow, requested)
		}
	}

	// Invariant: at most one Workflow per WorkItem.
	if existing, err := repo.GetWorkflowByWorkItem(ctx, workItemID); err == nil && existing != nil {
		return nil, fmt.Errorf("%w: work item %s already has workflow %s", ErrWorkflowAlreadyExists, workItemID, existing.ID)
	}

	workflowID, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate workflow ID: %w", err)
	}
	now := time.Now().UTC()
	startedAt := now
	if input.StartedAt != nil {
		if input.StartedAt.IsZero() {
			return nil, fmt.Errorf("%w: started at must be a real timestamp", ErrInvalidInput)
		}
		startedAt = input.StartedAt.UTC()
	}
	revisionID, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate workflow revision ID: %w", err)
	}
	summary := "Create workflow"
	if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revisionID, foundation.RevisionOriginUser, &summary, now); err != nil {
		return nil, err
	}
	wf := &Workflow{
		ID:                   workflowID,
		WorkItemID:           workItemID,
		WorkflowDefinitionID: definitionID,
		CurrentStepID:        current.ID,
		StartedAt:            startedAt,
		LastRevisionID:       revisionID,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if err := repo.createWorkflow(ctx, wf); err != nil {
		return nil, err
	}
	after := map[string]any{
		"version":                1,
		"workflow_id":            wf.ID,
		"work_item_id":           wf.WorkItemID,
		"workflow_definition_id": wf.WorkflowDefinitionID,
		"current_step_id":        wf.CurrentStepID,
		"step_title":             current.Title,
	}
	afterJSON, err := history.Snapshot(after)
	if err != nil {
		return nil, err
	}
	if _, err := history.NewRepository(tx).Append(ctx, history.Draft{
		Type: history.EventWorkflowCreated, Scope: history.ScopeTracking, Actor: history.ActorUser,
		Summary:    fmt.Sprintf("Workflow created for work item %s", wf.WorkItemID),
		WorkItemID: &wf.WorkItemID, RevisionID: &revisionID, CorrelationID: history.RevisionCorrelation(revisionID),
		AfterJSON: afterJSON, OccurredAt: now,
	}); err != nil {
		return nil, err
	}
	return wf, nil
}

// SetWorkflowCurrentStepTx moves the Workflow's own current step. It is an
// explicit human/authoritative command: the target may be earlier or later than
// the present step, and no TrackedItem state is read or changed. A move to the
// current step is a no-op (no Revision, no History).
func (s *Service) SetWorkflowCurrentStepTx(ctx context.Context, tx *sql.Tx, input SetWorkflowCurrentStepInput) (*Workflow, error) {
	workflowID := strings.TrimSpace(input.WorkflowID)
	stepID := strings.TrimSpace(input.StepID)
	if workflowID == "" || stepID == "" {
		return nil, fmt.Errorf("%w: workflow ID and step ID are required", ErrInvalidInput)
	}
	repo := NewRepository(tx)
	wf, err := repo.GetWorkflow(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	def, err := repo.GetDefinition(ctx, wf.WorkflowDefinitionID)
	if err != nil {
		return nil, err
	}
	var target, from *WorkflowStep
	for i := range def.Steps {
		if def.Steps[i].ID == stepID {
			target = &def.Steps[i]
		}
		if def.Steps[i].ID == wf.CurrentStepID {
			from = &def.Steps[i]
		}
	}
	if target == nil {
		return nil, fmt.Errorf("%w: %s", ErrStepNotInWorkflow, stepID)
	}
	if from == nil {
		return nil, fmt.Errorf("%w: workflow %s current step is not in its definition", ErrInconsistentState, wf.ID)
	}
	if target.ID == wf.CurrentStepID {
		return wf, nil
	}
	revisionID, err := identity.New()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	summary := "Move workflow current step"
	if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revisionID, foundation.RevisionOriginUser, &summary, now); err != nil {
		return nil, err
	}
	if err := repo.setWorkflowCurrentStep(ctx, wf.ID, target.ID, revisionID, now); err != nil {
		return nil, err
	}
	wf.CurrentStepID, wf.LastRevisionID, wf.UpdatedAt = target.ID, revisionID, now
	before := map[string]any{"version": 1, "workflow_step_id": from.ID, "step_key": from.Key, "step_title": from.Title, "sequence_no": from.Sequence}
	after := map[string]any{"version": 1, "workflow_step_id": target.ID, "step_key": target.Key, "step_title": target.Title, "sequence_no": target.Sequence}
	beforeJSON, err := history.Snapshot(before)
	if err != nil {
		return nil, err
	}
	afterJSON, err := history.Snapshot(after)
	if err != nil {
		return nil, err
	}
	workItemID := wf.WorkItemID
	if _, err := history.NewRepository(tx).Append(ctx, history.Draft{
		Type: history.EventWorkflowCurrentStepChanged, Scope: history.ScopeTracking, Actor: history.ActorUser,
		Summary:    fmt.Sprintf("Workflow moved from %s to %s", from.Title, target.Title),
		WorkItemID: &workItemID, RevisionID: &revisionID, CorrelationID: history.RevisionCorrelation(revisionID),
		BeforeJSON: beforeJSON, AfterJSON: afterJSON, OccurredAt: now,
	}); err != nil {
		return nil, err
	}
	return wf, nil
}

// SetWorkflowCurrentStep is the atomic public form of SetWorkflowCurrentStepTx.
func (s *Service) SetWorkflowCurrentStep(ctx context.Context, input SetWorkflowCurrentStepInput) (*Workflow, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	wf, err := s.SetWorkflowCurrentStepTx(ctx, tx, input)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return wf, nil
}

// GetWorkflowByWorkItem returns the single workflow instance for a WorkItem,
// or ErrNoWorkflow if none exists.
func (s *Service) GetWorkflowByWorkItem(ctx context.Context, workItemID string) (*Workflow, error) {
	return NewRepository(s.db).GetWorkflowByWorkItem(ctx, strings.TrimSpace(workItemID))
}

// GetWorkflowByWorkItemTx returns the single workflow instance within a caller transaction.
func (s *Service) GetWorkflowByWorkItemTx(ctx context.Context, tx *sql.Tx, workItemID string) (*Workflow, error) {
	return NewRepository(tx).GetWorkflowByWorkItem(ctx, strings.TrimSpace(workItemID))
}

// GetWorkflow returns one Workflow instance by ID.
func (s *Service) GetWorkflow(ctx context.Context, id string) (*Workflow, error) {
	return NewRepository(s.db).GetWorkflow(ctx, strings.TrimSpace(id))
}

// GetWorkflowTx returns one Workflow instance by ID within a caller transaction.
func (s *Service) GetWorkflowTx(ctx context.Context, tx *sql.Tx, id string) (*Workflow, error) {
	return NewRepository(tx).GetWorkflow(ctx, strings.TrimSpace(id))
}

// GetMonitorPoint returns one MonitorPoint by ID.
func (s *Service) GetMonitorPoint(ctx context.Context, id string) (*MonitorPoint, error) {
	return NewRepository(s.db).getMonitorPoint(ctx, strings.TrimSpace(id))
}

// GetWorkflowStep returns one WorkflowStep by ID.
func (s *Service) GetWorkflowStep(ctx context.Context, id string) (*WorkflowStep, error) {
	return NewRepository(s.db).GetStep(ctx, strings.TrimSpace(id))
}

// GetWorkflowDefinition returns a definition with its ordered steps.
func (s *Service) GetWorkflowDefinition(ctx context.Context, id string) (*WorkflowDefinition, error) {
	return NewRepository(s.db).GetDefinition(ctx, strings.TrimSpace(id))
}

// ResolveWorkflowReference resolves a workflow reference string to an existing Core Workflow.
// It checks whether idOrRef is an exact Core workflows.id, or maps via foundation external_refs
// to an entity (directly a workflow or a work item owning a workflow). If unresolved, it returns
// nil, ErrWorkflowNotFound.
func (s *Service) ResolveWorkflowReference(ctx context.Context, idOrRef string) (*Workflow, error) {
	clean := strings.TrimSpace(idOrRef)
	if clean == "" {
		return nil, ErrWorkflowNotFound
	}

	// 1. Direct Core workflows(id) lookup
	wf, err := s.GetWorkflow(ctx, clean)
	if err == nil && wf != nil {
		return wf, nil
	}

	// 2. Check external_refs for an explicit mapping to an internal entity
	if s.db != nil {
		var entityID string
		err := s.db.QueryRowContext(ctx, "SELECT entity_id FROM external_refs WHERE external_id = ? LIMIT 1", clean).Scan(&entityID)
		if err == nil && entityID != "" {
			if wf, err := s.GetWorkflowByWorkItem(ctx, entityID); err == nil && wf != nil {
				return wf, nil
			}
			if wf, err := s.GetWorkflow(ctx, entityID); err == nil && wf != nil {
				return wf, nil
			}
		}
	}

	return nil, ErrWorkflowNotFound
}

// CreateTrackedItem atomically creates one USER Revision, the TrackedItem
// Entity, the TrackedItem row and one NOT_STARTED state row per step of the
// WorkItem's single Workflow's WorkflowDefinition.
func (s *Service) CreateTrackedItem(ctx context.Context, input CreateTrackedItemInput) (*TrackedItem, error) {
	workItemID := strings.TrimSpace(input.WorkItemID)
	title := strings.TrimSpace(input.Title)
	if workItemID == "" {
		return nil, fmt.Errorf("%w: work item ID must not be empty", ErrInvalidInput)
	}
	if title == "" {
		return nil, fmt.Errorf("%w: title must not be blank", ErrInvalidInput)
	}
	itemType := normalizeOptional(input.ItemType)
	parentID := normalizeOptional(input.ParentItemID)
	inputWorkflowID := normalizeOptional(input.WorkflowID)

	itemID, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate tracked item ID: %w", err)
	}
	revisionID, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate tracked item revision ID: %w", err)
	}
	now := time.Now().UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create tracked item transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	repo := NewRepository(tx)

	if _, err := s.workItems.GetByIDTx(ctx, tx, workItemID); err != nil {
		if errors.Is(err, workitem.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrWorkItemUnavailable, workItemID)
		}
		return nil, err
	}

	// Resolve the WorkItem's workflow.
	wf, err := repo.GetWorkflowByWorkItem(ctx, workItemID)
	if err != nil {
		return nil, fmt.Errorf("%w: work item %s has no workflow", ErrNoWorkflow, workItemID)
	}
	if inputWorkflowID != nil && *inputWorkflowID != wf.ID {
		return nil, fmt.Errorf("%w: workflow %s does not belong to work item %s", ErrInvalidInput, *inputWorkflowID, workItemID)
	}

	definition, err := repo.GetDefinition(ctx, wf.WorkflowDefinitionID)
	if err != nil {
		return nil, err
	}
	if !definition.Active {
		return nil, fmt.Errorf("%w: %s", ErrWorkflowInactive, definition.ID)
	}

	if parentID != nil {
		parent, err := repo.GetItem(ctx, *parentID)
		if err != nil {
			if errors.Is(err, ErrTrackedItemNotFound) {
				return nil, fmt.Errorf("%w: parent %s does not exist", ErrInvalidParent, *parentID)
			}
			return nil, err
		}
		if parent.WorkItemID != workItemID {
			return nil, fmt.Errorf("%w: parent %s belongs to another work item", ErrInvalidParent, *parentID)
		}
		if parent.WorkflowID != wf.ID {
			return nil, fmt.Errorf("%w: parent %s belongs to another workflow", ErrInvalidParent, *parentID)
		}
	}

	summary := "Create tracked item"
	if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revisionID, foundation.RevisionOriginUser, &summary, now); err != nil {
		return nil, err
	}
	if err := foundation.NewRepository(tx).CreateEntity(ctx, itemID, entityTypeTrackedItem, now); err != nil {
		return nil, err
	}
	item := &TrackedItem{
		ID: itemID, WorkItemID: workItemID, WorkflowID: wf.ID, ParentItemID: parentID,
		Title: title, ItemType: itemType, Lifecycle: ItemActive,
		LastRevisionID: revisionID, CreatedAt: now, UpdatedAt: now,
	}
	if err := repo.createItem(ctx, item); err != nil {
		return nil, err
	}
	for _, step := range definition.Steps {
		if err := repo.createStepState(ctx, StepStateRecord{
			TrackedItemID: itemID, WorkflowStepID: step.ID, State: StepNotStarted, LastRevisionID: revisionID,
		}); err != nil {
			return nil, err
		}
	}
	if err := appendTrackedItemHistory(ctx, tx, history.EventTrackedItemCreated, history.ActorUser, item, revisionID, "Tracked item created", nil, map[string]any{"version": 1, "title": item.Title, "item_type": item.ItemType, "parent_item_id": item.ParentItemID, "workflow_id": item.WorkflowID}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create tracked item transaction: %w", err)
	}
	return item, nil
}

// SetTrackedItemStepState sets one step's semantic state. A same-state request
// is a no-op (no Revision, no timestamp change). A real change creates exactly
// one USER Revision, updates the step state, and advances the TrackedItem's
// last_revision_id/updated_at so the item's tracking freshness is observable
// from the item itself. Title, lifecycle and the owning WorkItem are untouched.
//
// Timestamp rules (deterministic, service-owned): NOT_STARTED clears
// entered_at/completed_at; any other state keeps an existing entered_at or sets
// it to now; completed_at is set to now only when entering COMPLETED and is
// cleared otherwise.
func (s *Service) SetTrackedItemStepState(ctx context.Context, input SetStepStateInput) (*StepStateRecord, error) {
	itemID := strings.TrimSpace(input.TrackedItemID)
	stepID := strings.TrimSpace(input.WorkflowStepID)
	if itemID == "" || stepID == "" {
		return nil, fmt.Errorf("%w: tracked item ID and workflow step ID are required", ErrInvalidInput)
	}
	if !input.State.Valid() {
		return nil, fmt.Errorf("%w: unsupported step state %q", ErrInvalidInput, input.State)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin set step state transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	repo := NewRepository(tx)

	item, err := repo.GetItem(ctx, itemID)
	if err != nil {
		return nil, err
	}
	workflow, err := repo.GetWorkflow(ctx, item.WorkflowID)
	if err != nil {
		return nil, err
	}
	step, err := repo.getStep(ctx, stepID)
	if err != nil {
		return nil, err
	}
	if step.WorkflowDefinitionID != workflow.WorkflowDefinitionID {
		return nil, fmt.Errorf("%w: step %s, item %s", ErrStepNotInWorkflow, stepID, itemID)
	}
	current, err := repo.getStepState(ctx, itemID, stepID)
	if err != nil {
		return nil, err
	}
	if current.State == input.State {
		return current, nil
	}

	revisionID, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate step state revision ID: %w", err)
	}
	now := time.Now().UTC()
	summary := fmt.Sprintf("Set tracked item step state to %s", input.State)
	if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revisionID, foundation.RevisionOriginUser, &summary, now); err != nil {
		return nil, err
	}
	next := StepStateRecord{TrackedItemID: itemID, WorkflowStepID: stepID, State: input.State, LastRevisionID: revisionID}
	if input.State != StepNotStarted {
		if current.EnteredAt != nil {
			next.EnteredAt = current.EnteredAt
		} else {
			entered := now
			next.EnteredAt = &entered
		}
	}
	if input.State == StepCompleted {
		completed := now
		next.CompletedAt = &completed
	}
	if err := repo.updateStepState(ctx, next); err != nil {
		return nil, err
	}
	if err := repo.touchItem(ctx, itemID, revisionID, now); err != nil {
		return nil, err
	}
	before, _ := history.Snapshot(map[string]any{"version": 1, "workflow_step_id": stepID, "state": current.State})
	after, _ := history.Snapshot(map[string]any{"version": 1, "workflow_step_id": stepID, "state": next.State})
	if _, err := history.NewRepository(tx).Append(ctx, history.Draft{Type: history.EventTrackedItemStageStateChanged, Scope: history.ScopeTracking, Actor: history.ActorUser, Summary: fmt.Sprintf("%s changed to %s", item.Title, next.State), SubjectEntityID: &item.ID, WorkItemID: &item.WorkItemID, RevisionID: &revisionID, CorrelationID: history.RevisionCorrelation(revisionID), BeforeJSON: before, AfterJSON: after, OccurredAt: now}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit set step state transaction: %w", err)
	}
	return &next, nil
}

// UpdateTrackedItem changes the bounded editable metadata. It is retained as
// a deterministic domain capability for Proposal Apply and tests; product UI
// does not expose a direct edit surface.
func (s *Service) UpdateTrackedItem(ctx context.Context, input UpdateTrackedItemInput) (*TrackedItem, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin update tracked item transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	item, err := s.UpdateTrackedItemTx(ctx, tx, input)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit update tracked item transaction: %w", err)
	}
	return item, nil
}

// CreateTrackedItemTx is the transaction-aware Apply seam. It performs the
// same validation and writes as CreateTrackedItem without owning commit.
func (s *Service) CreateTrackedItemTx(ctx context.Context, tx *sql.Tx, input CreateTrackedItemInput) (*TrackedItem, error) {
	workItemID, title := strings.TrimSpace(input.WorkItemID), strings.TrimSpace(input.Title)
	if workItemID == "" || title == "" {
		return nil, fmt.Errorf("%w: work item ID and title are required", ErrInvalidInput)
	}
	repo := NewRepository(tx)
	if _, err := s.workItems.GetByIDTx(ctx, tx, workItemID); err != nil {
		return nil, fmt.Errorf("%w: %s", ErrWorkItemUnavailable, workItemID)
	}
	wf, err := repo.GetWorkflowByWorkItem(ctx, workItemID)
	if err != nil {
		return nil, err
	}
	if supplied := normalizeOptional(input.WorkflowID); supplied != nil && *supplied != wf.ID {
		return nil, fmt.Errorf("%w: workflow does not belong to work item", ErrInvalidInput)
	}
	def, err := repo.GetDefinition(ctx, wf.WorkflowDefinitionID)
	if err != nil {
		return nil, err
	}
	if !def.Active {
		return nil, ErrWorkflowInactive
	}
	parentID := normalizeOptional(input.ParentItemID)
	if parentID != nil {
		parent, err := repo.GetItem(ctx, *parentID)
		if err != nil || parent.WorkItemID != workItemID || parent.WorkflowID != wf.ID {
			return nil, fmt.Errorf("%w: parent must be available in the same workflow", ErrInvalidParent)
		}
	}
	itemID, err := identity.New()
	if err != nil {
		return nil, err
	}
	revisionID, err := identity.New()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	summary := "Create tracked item"
	if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revisionID, foundation.RevisionOriginUser, &summary, now); err != nil {
		return nil, err
	}
	if err := foundation.NewRepository(tx).CreateEntity(ctx, itemID, entityTypeTrackedItem, now); err != nil {
		return nil, err
	}
	item := &TrackedItem{ID: itemID, WorkItemID: workItemID, WorkflowID: wf.ID, ParentItemID: parentID, Title: title, ItemType: normalizeOptional(input.ItemType), Lifecycle: ItemActive, LastRevisionID: revisionID, CreatedAt: now, UpdatedAt: now}
	if err := repo.createItem(ctx, item); err != nil {
		return nil, err
	}
	for _, step := range def.Steps {
		if err := repo.createStepState(ctx, StepStateRecord{TrackedItemID: itemID, WorkflowStepID: step.ID, State: StepNotStarted, LastRevisionID: revisionID}); err != nil {
			return nil, err
		}
	}
	if err := appendTrackedItemHistory(ctx, tx, history.EventTrackedItemCreated, history.ActorUser, item, revisionID, "Tracked item created", nil, map[string]any{"version": 1, "title": item.Title, "item_type": item.ItemType, "parent_item_id": item.ParentItemID, "workflow_id": item.WorkflowID}); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) UpdateTrackedItemTx(ctx context.Context, tx *sql.Tx, input UpdateTrackedItemInput) (*TrackedItem, error) {
	id, title := strings.TrimSpace(input.ID), strings.TrimSpace(input.Title)
	if id == "" || title == "" {
		return nil, fmt.Errorf("%w: tracked item ID and title are required", ErrInvalidInput)
	}
	repo := NewRepository(tx)
	current, err := repo.GetItem(ctx, id)
	if err != nil {
		return nil, err
	}
	itemType, parentID := normalizeOptional(input.ItemType), normalizeOptional(input.ParentItemID)
	if parentID != nil {
		if *parentID == id {
			return nil, fmt.Errorf("%w: item cannot parent itself", ErrInvalidParent)
		}
		parent, err := repo.GetItem(ctx, *parentID)
		if err != nil || parent.WorkItemID != current.WorkItemID || parent.WorkflowID != current.WorkflowID {
			return nil, fmt.Errorf("%w: parent must be an available item in the same workflow", ErrInvalidParent)
		}
		cursor := parent
		for cursor.ParentItemID != nil {
			if *cursor.ParentItemID == id {
				return nil, fmt.Errorf("%w: parent would create a hierarchy cycle", ErrInvalidParent)
			}
			cursor, err = repo.GetItem(ctx, *cursor.ParentItemID)
			if err != nil {
				return nil, err
			}
		}
	}
	if current.Title == title && optionalStringEqual(current.ItemType, itemType) && optionalStringEqual(current.ParentItemID, parentID) {
		return current, nil
	}
	revisionID, err := identity.New()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	summary := "Update tracked item"
	if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revisionID, foundation.RevisionOriginUser, &summary, now); err != nil {
		return nil, err
	}
	next := *current
	next.Title, next.ItemType, next.ParentItemID, next.LastRevisionID, next.UpdatedAt = title, itemType, parentID, revisionID, now
	if err := repo.updateItem(ctx, &next); err != nil {
		return nil, err
	}
	before := map[string]any{"version": 1, "title": current.Title, "item_type": current.ItemType, "parent_item_id": current.ParentItemID}
	after := map[string]any{"version": 1, "title": next.Title, "item_type": next.ItemType, "parent_item_id": next.ParentItemID}
	if err := appendTrackedItemHistory(ctx, tx, history.EventTrackedItemUpdated, history.ActorUser, &next, revisionID, "Tracked item updated", before, after); err != nil {
		return nil, err
	}
	return &next, nil
}

// SetTrackedItemStageStateTx moves an item to one authoritative stage by
// completing earlier steps, setting the selected step state, and resetting
// later steps. All changed rows share one Revision and History event.
func (s *Service) SetTrackedItemStageStateTx(ctx context.Context, tx *sql.Tx, input SetTrackedItemStageStateInput) (*TrackedItem, error) {
	if !input.State.Valid() {
		return nil, fmt.Errorf("%w: unsupported step state %q", ErrInvalidInput, input.State)
	}
	repo := NewRepository(tx)
	item, err := repo.GetItem(ctx, strings.TrimSpace(input.TrackedItemID))
	if err != nil {
		return nil, err
	}
	wf, err := repo.GetWorkflow(ctx, item.WorkflowID)
	if err != nil {
		return nil, err
	}
	def, err := repo.GetDefinition(ctx, wf.WorkflowDefinitionID)
	if err != nil {
		return nil, err
	}
	target := -1
	for i, step := range def.Steps {
		if step.ID == strings.TrimSpace(input.WorkflowStepID) {
			target = i
		}
	}
	if target < 0 {
		return nil, fmt.Errorf("%w: %s", ErrStepNotInWorkflow, input.WorkflowStepID)
	}
	states := make([]*StepStateRecord, len(def.Steps))
	changed := false
	for i, step := range def.Steps {
		current, err := repo.getStepState(ctx, item.ID, step.ID)
		if err != nil {
			return nil, err
		}
		desired := StepNotStarted
		if i < target {
			desired = StepCompleted
		} else if i == target {
			desired = input.State
		}
		copy := *current
		copy.State = desired
		if current.State != desired {
			changed = true
		}
		states[i] = &copy
	}
	if !changed {
		return item, nil
	}
	revisionID, err := identity.New()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	summary := "Move tracked item stage/state"
	if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revisionID, foundation.RevisionOriginUser, &summary, now); err != nil {
		return nil, err
	}
	for i, next := range states {
		current, _ := repo.getStepState(ctx, item.ID, next.WorkflowStepID)
		if current.State == next.State {
			continue
		}
		next.LastRevisionID = revisionID
		next.EnteredAt, next.CompletedAt = nil, nil
		if next.State != StepNotStarted {
			entered := now
			next.EnteredAt = &entered
		}
		if next.State == StepCompleted {
			completed := now
			next.CompletedAt = &completed
		}
		if err := repo.updateStepState(ctx, *next); err != nil {
			return nil, err
		}
		_ = i
	}
	if err := repo.touchItem(ctx, item.ID, revisionID, now); err != nil {
		return nil, err
	}
	item.LastRevisionID, item.UpdatedAt = revisionID, now
	before := map[string]any{"version": 1}
	after := map[string]any{"version": 1, "workflow_step_id": def.Steps[target].ID, "step_title": def.Steps[target].Title, "state": input.State}
	if err := appendTrackedItemHistory(ctx, tx, history.EventTrackedItemStageStateChanged, history.ActorUser, item, revisionID, fmt.Sprintf("%s moved to %s (%s)", item.Title, def.Steps[target].Title, input.State), before, after); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) SetTrackedItemStageState(ctx context.Context, input SetTrackedItemStageStateInput) (*TrackedItem, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	item, err := s.SetTrackedItemStageStateTx(ctx, tx, input)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) ArchiveTrackedItemTx(ctx context.Context, tx *sql.Tx, id string) (*TrackedItem, error) {
	repo := NewRepository(tx)
	item, err := repo.GetItem(ctx, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	revisionID, err := identity.New()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	summary := "Archive tracked item"
	if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revisionID, foundation.RevisionOriginUser, &summary, now); err != nil {
		return nil, err
	}
	if err := appendTrackedItemHistory(ctx, tx, history.EventTrackedItemArchived, history.ActorUser, item, revisionID, fmt.Sprintf("%s archived", item.Title), map[string]any{"version": 1, "archived": false}, map[string]any{"version": 1, "archived": true}); err != nil {
		return nil, err
	}
	if err := repo.archiveItem(ctx, item.ID, now); err != nil {
		return nil, err
	}
	item.LastRevisionID, item.UpdatedAt = revisionID, now
	return item, nil
}

func (s *Service) ArchiveTrackedItem(ctx context.Context, id string) (*TrackedItem, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	item, err := s.ArchiveTrackedItemTx(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return item, nil
}

func (s *Service) AddDependencyTx(ctx context.Context, tx *sql.Tx, input TrackedItemRelationInput) (*TrackedItemRelation, error) {
	repo := NewRepository(tx)
	from, err := repo.GetItem(ctx, strings.TrimSpace(input.FromTrackedItemID))
	if err != nil {
		return nil, err
	}
	to, err := repo.GetItem(ctx, strings.TrimSpace(input.ToTrackedItemID))
	if err != nil {
		return nil, err
	}
	if from.ID == to.ID {
		return nil, fmt.Errorf("%w: self dependency", ErrInvalidInput)
	}
	if from.WorkflowID != to.WorkflowID {
		return nil, fmt.Errorf("%w: dependency endpoints must share one workflow", ErrInvalidInput)
	}
	relations, err := repo.listRelations(ctx, from.WorkflowID)
	if err != nil {
		return nil, err
	}
	for _, relation := range relations {
		if relation.FromTrackedItemID == from.ID && relation.ToTrackedItemID == to.ID {
			return nil, fmt.Errorf("%w: %s depends on %s", ErrRelationExists, from.ID, to.ID)
		}
	}
	if dependencyReachable(relations, to.ID, from.ID) {
		return nil, fmt.Errorf("%w: %s -> %s", ErrDependencyCycle, from.ID, to.ID)
	}
	revisionID, err := identity.New()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	summary := "Add tracked item dependency"
	if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revisionID, foundation.RevisionOriginUser, &summary, now); err != nil {
		return nil, err
	}
	relation := &TrackedItemRelation{WorkflowID: from.WorkflowID, FromTrackedItemID: from.ID, ToTrackedItemID: to.ID, Type: RelationDependsOn, LastRevisionID: revisionID, CreatedAt: now}
	if err := repo.createRelation(ctx, *relation); err != nil {
		return nil, err
	}
	if err := appendTrackedItemHistory(ctx, tx, history.EventTrackedItemDependencyAdded, history.ActorUser, from, revisionID, fmt.Sprintf("Dependency added: %s depends on %s", from.Title, to.Title), nil, map[string]any{"version": 1, "depends_on_id": to.ID, "depends_on_title": to.Title}); err != nil {
		return nil, err
	}
	return relation, nil
}

func (s *Service) AddDependency(ctx context.Context, input TrackedItemRelationInput) (*TrackedItemRelation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	relation, err := s.AddDependencyTx(ctx, tx, input)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return relation, nil
}

func (s *Service) RemoveDependencyTx(ctx context.Context, tx *sql.Tx, input TrackedItemRelationInput) (string, error) {
	repo := NewRepository(tx)
	from, err := repo.GetItem(ctx, strings.TrimSpace(input.FromTrackedItemID))
	if err != nil {
		return "", err
	}
	to, err := repo.GetItem(ctx, strings.TrimSpace(input.ToTrackedItemID))
	if err != nil {
		return "", err
	}
	if from.WorkflowID != to.WorkflowID {
		return "", fmt.Errorf("%w: dependency endpoints must share one workflow", ErrInvalidInput)
	}
	revisionID, err := identity.New()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	summary := "Remove tracked item dependency"
	if _, err := foundation.NewRepository(tx).CreateRevision(ctx, revisionID, foundation.RevisionOriginUser, &summary, now); err != nil {
		return "", err
	}
	removed, err := repo.deleteRelation(ctx, from.ID, to.ID)
	if err != nil {
		return "", err
	}
	if !removed {
		return "", fmt.Errorf("%w: %s depends on %s", ErrRelationNotFound, from.ID, to.ID)
	}
	if err := appendTrackedItemHistory(ctx, tx, history.EventTrackedItemDependencyRemoved, history.ActorUser, from, revisionID, fmt.Sprintf("Dependency removed: %s no longer depends on %s", from.Title, to.Title), map[string]any{"version": 1, "depends_on_id": to.ID, "depends_on_title": to.Title}, nil); err != nil {
		return "", err
	}
	return revisionID, nil
}

func (s *Service) RemoveDependency(ctx context.Context, input TrackedItemRelationInput) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := s.RemoveDependencyTx(ctx, tx, input); err != nil {
		return err
	}
	return tx.Commit()
}

func dependencyReachable(relations []TrackedItemRelation, start, target string) bool {
	edges := map[string][]string{}
	for _, relation := range relations {
		edges[relation.FromTrackedItemID] = append(edges[relation.FromTrackedItemID], relation.ToTrackedItemID)
	}
	seen := map[string]bool{}
	var visit func(string) bool
	visit = func(id string) bool {
		if id == target {
			return true
		}
		if seen[id] {
			return false
		}
		seen[id] = true
		for _, next := range edges[id] {
			if visit(next) {
				return true
			}
		}
		return false
	}
	return visit(start)
}

func appendTrackedItemHistory(ctx context.Context, tx *sql.Tx, eventType history.EventType, actor history.ActorType, item *TrackedItem, revisionID, summary string, beforeValue, afterValue any) error {
	before, err := history.Snapshot(beforeValue)
	if err != nil {
		return err
	}
	after, err := history.Snapshot(afterValue)
	if err != nil {
		return err
	}
	_, err = history.NewRepository(tx).Append(ctx, history.Draft{Type: eventType, Scope: history.ScopeTracking, Actor: actor, Summary: summary, SubjectEntityID: &item.ID, WorkItemID: &item.WorkItemID, RevisionID: &revisionID, CorrelationID: history.RevisionCorrelation(revisionID), BeforeJSON: before, AfterJSON: after, OccurredAt: time.Now().UTC()})
	return err
}

func optionalStringEqual(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// GetTrackedItem returns one current TrackedItem.
func (s *Service) GetTrackedItem(ctx context.Context, id string) (*TrackedItem, error) {
	return NewRepository(s.db).GetItem(ctx, strings.TrimSpace(id))
}

func (s *Service) GetTrackedItemTx(ctx context.Context, tx *sql.Tx, id string) (*TrackedItem, error) {
	return NewRepository(tx).GetItem(ctx, strings.TrimSpace(id))
}

// GetWorkItemTracking returns the semantic Tracking read model of one
// WorkItem: TrackedItems (hierarchy identity via ParentItemID), the pinned
// WorkflowDefinitions with ordered steps, authoritative step states, the
// derived progression and MonitorPoints. Read-only; nothing derived is
// persisted. An unknown or archived WorkItem is ErrWorkItemUnavailable; a valid
// WorkItem without tracking data is a valid empty model.
func (s *Service) GetWorkItemTracking(ctx context.Context, workItemID string) (*WorkItemTracking, error) {
	id := strings.TrimSpace(workItemID)
	if id == "" {
		return nil, fmt.Errorf("%w: work item ID must not be empty", ErrInvalidInput)
	}
	if _, err := s.workItems.GetByID(ctx, id); err != nil {
		if errors.Is(err, workitem.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrWorkItemUnavailable, id)
		}
		return nil, err
	}
	return NewRepository(s.db).LoadWorkItemTracking(ctx, id)
}

// ListActiveWorkflowDefinitions returns every active definition with its
// ordered steps, so a caller can bind a new TrackedItem to an existing process.
func (s *Service) ListActiveWorkflowDefinitions(ctx context.Context) ([]WorkflowDefinition, error) {
	return NewRepository(s.db).ListActiveDefinitions(ctx)
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
