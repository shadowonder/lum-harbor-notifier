package tracking

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"lum-harbor-notifier/internal/database/codec"
	dbgen "lum-harbor-notifier/internal/database/generated"
)

// Repository persists Tracking state through its supplied executor (*sql.DB
// for reads, *sql.Tx inside Service mutations). It owns no transaction,
// Revision or validation.
type Repository struct {
	queries *dbgen.Queries
}

func NewRepository(db dbgen.DBTX) *Repository {
	return &Repository{queries: dbgen.New(db)}
}

func boolToInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}

func (r *Repository) createDefinition(ctx context.Context, def *WorkflowDefinition) error {
	if err := r.queries.CreateWorkflowDefinition(ctx, dbgen.CreateWorkflowDefinitionParams{
		ID: def.ID, Name: def.Name, Version: int64(def.Version),
		Active: boolToInt(def.Active), CreatedAt: codec.FormatTime(def.CreatedAt),
	}); err != nil {
		return fmt.Errorf("create workflow definition %s: %w", def.ID, err)
	}
	for _, step := range def.Steps {
		if err := r.queries.CreateWorkflowStep(ctx, dbgen.CreateWorkflowStepParams{
			ID: step.ID, WorkflowDefinitionID: step.WorkflowDefinitionID, StepKey: step.Key,
			Title: step.Title, SequenceNo: int64(step.Sequence), Terminal: boolToInt(step.Terminal),
		}); err != nil {
			return fmt.Errorf("create workflow step %s: %w", step.ID, err)
		}
	}
	return nil
}

// GetDefinition loads a definition with its ordered steps.
func (r *Repository) GetDefinition(ctx context.Context, id string) (*WorkflowDefinition, error) {
	row, err := r.queries.GetWorkflowDefinitionByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrWorkflowNotFound, id)
		}
		return nil, fmt.Errorf("load workflow definition %s: %w", id, err)
	}
	def, err := definitionFromDB(row)
	if err != nil {
		return nil, err
	}
	steps, err := r.queries.ListWorkflowStepsByDefinition(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("load workflow steps %s: %w", id, err)
	}
	for _, s := range steps {
		def.Steps = append(def.Steps, stepFromDB(s))
	}
	return def, nil
}

func (r *Repository) getStep(ctx context.Context, id string) (*WorkflowStep, error) {
	row, err := r.queries.GetWorkflowStepByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: workflow step %s", ErrStepNotInWorkflow, id)
		}
		return nil, fmt.Errorf("load workflow step %s: %w", id, err)
	}
	step := stepFromDB(row)
	return &step, nil
}

func (r *Repository) GetStep(ctx context.Context, id string) (*WorkflowStep, error) {
	return r.getStep(ctx, id)
}

func (r *Repository) createWorkflow(ctx context.Context, w *Workflow) error {
	if err := r.queries.CreateWorkflow(ctx, dbgen.CreateWorkflowParams{
		ID: w.ID, WorkItemID: w.WorkItemID, WorkflowDefinitionID: w.WorkflowDefinitionID,
		CurrentStepID: w.CurrentStepID, StartedAt: codec.FormatTime(w.StartedAt),
		LastRevisionID: w.LastRevisionID, CreatedAt: codec.FormatTime(w.CreatedAt),
		UpdatedAt: codec.FormatTime(w.UpdatedAt),
	}); err != nil {
		return fmt.Errorf("create workflow %s: %w", w.ID, err)
	}
	return nil
}

// GetWorkflow loads one Workflow instance.
func (r *Repository) GetWorkflow(ctx context.Context, id string) (*Workflow, error) {
	row, err := r.queries.GetWorkflowByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: workflow %s", ErrWorkflowNotFound, id)
		}
		return nil, fmt.Errorf("load workflow %s: %w", id, err)
	}
	return workflowFromDB(row)
}

// GetWorkflowByWorkItem loads the single workflow instance for a WorkItem.
func (r *Repository) GetWorkflowByWorkItem(ctx context.Context, workItemID string) (*Workflow, error) {
	row, err := r.queries.GetWorkflowByWorkItem(ctx, workItemID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: for work item %s", ErrNoWorkflow, workItemID)
		}
		return nil, fmt.Errorf("load workflow for work item %s: %w", workItemID, err)
	}
	return workflowFromDB(row)
}

func (r *Repository) createItem(ctx context.Context, item *TrackedItem) error {
	if err := r.queries.CreateTrackedItem(ctx, dbgen.CreateTrackedItemParams{
		EntityID: item.ID, WorkItemID: item.WorkItemID, WorkflowID: item.WorkflowID,
		ParentItemID: item.ParentItemID, Title: item.Title, ItemType: item.ItemType,
		LifecycleState: string(item.Lifecycle), CompletedAt: codec.FormatOptionalTime(item.CompletedAt),
		LastRevisionID: item.LastRevisionID, CreatedAt: codec.FormatTime(item.CreatedAt),
		UpdatedAt: codec.FormatTime(item.UpdatedAt),
	}); err != nil {
		return fmt.Errorf("create tracked item %s: %w", item.ID, err)
	}
	return nil
}

// GetItem returns one current (entity-unarchived) TrackedItem.
func (r *Repository) GetItem(ctx context.Context, id string) (*TrackedItem, error) {
	row, err := r.queries.GetTrackedItemByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrTrackedItemNotFound, id)
		}
		return nil, fmt.Errorf("load tracked item %s: %w", id, err)
	}
	return itemFromDB(row)
}

func (r *Repository) touchItem(ctx context.Context, id, revisionID string, at time.Time) error {
	if err := r.queries.TouchTrackedItem(ctx, dbgen.TouchTrackedItemParams{
		LastRevisionID: revisionID, UpdatedAt: codec.FormatTime(at), EntityID: id,
	}); err != nil {
		return fmt.Errorf("touch tracked item %s: %w", id, err)
	}
	return nil
}

func (r *Repository) updateItem(ctx context.Context, item *TrackedItem) error {
	if err := r.queries.UpdateTrackedItemDetails(ctx, dbgen.UpdateTrackedItemDetailsParams{
		Title: item.Title, ItemType: item.ItemType, ParentItemID: item.ParentItemID,
		LastRevisionID: item.LastRevisionID, UpdatedAt: codec.FormatTime(item.UpdatedAt), EntityID: item.ID,
	}); err != nil {
		return fmt.Errorf("update tracked item %s: %w", item.ID, err)
	}
	return nil
}

func (r *Repository) archiveItem(ctx context.Context, id string, at time.Time) error {
	archivedAt := codec.FormatTime(at)
	if err := r.queries.ArchiveTrackedItemEntity(ctx, dbgen.ArchiveTrackedItemEntityParams{
		ArchivedAt: &archivedAt, EntityID: id,
	}); err != nil {
		return fmt.Errorf("archive tracked item %s: %w", id, err)
	}
	return nil
}

func (r *Repository) createRelation(ctx context.Context, relation TrackedItemRelation) error {
	if err := r.queries.CreateTrackedItemRelation(ctx, dbgen.CreateTrackedItemRelationParams{
		WorkflowID: relation.WorkflowID, FromTrackedItemID: relation.FromTrackedItemID,
		ToTrackedItemID: relation.ToTrackedItemID, LastRevisionID: relation.LastRevisionID,
		CreatedAt: codec.FormatTime(relation.CreatedAt),
	}); err != nil {
		return fmt.Errorf("create tracked item dependency: %w", err)
	}
	return nil
}

func (r *Repository) deleteRelation(ctx context.Context, fromID, toID string) (bool, error) {
	count, err := r.queries.DeleteTrackedItemRelation(ctx, dbgen.DeleteTrackedItemRelationParams{
		FromTrackedItemID: fromID, ToTrackedItemID: toID,
	})
	if err != nil {
		return false, fmt.Errorf("delete tracked item dependency: %w", err)
	}
	return count > 0, nil
}

func (r *Repository) listRelations(ctx context.Context, workflowID string) ([]TrackedItemRelation, error) {
	rows, err := r.queries.ListTrackedItemRelationsByWorkflow(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("list tracked item dependencies: %w", err)
	}
	result := make([]TrackedItemRelation, 0, len(rows))
	for _, row := range rows {
		created, err := codec.ParseTime(row.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse tracked item dependency created_at: %w", err)
		}
		result = append(result, TrackedItemRelation{WorkflowID: row.WorkflowID, FromTrackedItemID: row.FromTrackedItemID, ToTrackedItemID: row.ToTrackedItemID, Type: RelationType(row.RelationType), LastRevisionID: row.LastRevisionID, CreatedAt: created})
	}
	return result, nil
}

func (r *Repository) createStepState(ctx context.Context, s StepStateRecord) error {
	if err := r.queries.CreateTrackedItemStepState(ctx, dbgen.CreateTrackedItemStepStateParams{
		TrackedItemID: s.TrackedItemID, WorkflowStepID: s.WorkflowStepID, State: string(s.State),
		EnteredAt: codec.FormatOptionalTime(s.EnteredAt), CompletedAt: codec.FormatOptionalTime(s.CompletedAt),
		LastRevisionID: s.LastRevisionID,
	}); err != nil {
		return fmt.Errorf("create step state %s/%s: %w", s.TrackedItemID, s.WorkflowStepID, err)
	}
	return nil
}

func (r *Repository) getStepState(ctx context.Context, itemID, stepID string) (*StepStateRecord, error) {
	row, err := r.queries.GetTrackedItemStepState(ctx, dbgen.GetTrackedItemStepStateParams{
		TrackedItemID: itemID, WorkflowStepID: stepID,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: no state for step %s of item %s", ErrInconsistentState, stepID, itemID)
		}
		return nil, fmt.Errorf("load step state %s/%s: %w", itemID, stepID, err)
	}
	return stepStateFromDB(dbgen.TrackedItemStepState(row))
}

func (r *Repository) updateStepState(ctx context.Context, s StepStateRecord) error {
	if err := r.queries.UpdateTrackedItemStepState(ctx, dbgen.UpdateTrackedItemStepStateParams{
		State: string(s.State), EnteredAt: codec.FormatOptionalTime(s.EnteredAt),
		CompletedAt: codec.FormatOptionalTime(s.CompletedAt), LastRevisionID: s.LastRevisionID,
		TrackedItemID: s.TrackedItemID, WorkflowStepID: s.WorkflowStepID,
	}); err != nil {
		return fmt.Errorf("update step state %s/%s: %w", s.TrackedItemID, s.WorkflowStepID, err)
	}
	return nil
}

// LoadWorkItemTracking reads everything needed for one WorkItem's Tracking
// read model in a fixed number of queries (no N+1).
func (r *Repository) LoadWorkItemTracking(ctx context.Context, workItemID string) (*WorkItemTracking, error) {
	monitors, err := r.listMonitorPoints(ctx, workItemID)
	if err != nil {
		return nil, err
	}

	result := &WorkItemTracking{
		WorkItemID:    workItemID,
		Workflow:      nil,
		Items:         []TrackedItemView{},
		MonitorPoints: monitors,
	}

	wfRow, err := r.queries.GetWorkflowByWorkItem(ctx, workItemID)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("load workflow for work item: %w", err)
		}
		// Valid empty workflow state (WorkItem has no workflow yet).
		return result, nil
	}

	wf, err := workflowFromDB(wfRow)
	if err != nil {
		return nil, err
	}
	def, err := r.GetDefinition(ctx, wf.WorkflowDefinitionID)
	if err != nil {
		return nil, fmt.Errorf("load workflow definition for work item: %w", err)
	}
	result.Workflow = &WorkflowView{
		Workflow:   *wf,
		Definition: *def,
	}
	relations, err := r.listRelations(ctx, wf.ID)
	if err != nil {
		return nil, err
	}
	result.Relations = relations

	itemRows, err := r.queries.ListTrackedItemsByWorkItem(ctx, workItemID)
	if err != nil {
		return nil, fmt.Errorf("list tracked items: %w", err)
	}
	stateRows, err := r.queries.ListStepStatesByWorkItem(ctx, workItemID)
	if err != nil {
		return nil, fmt.Errorf("list step states: %w", err)
	}

	statesByItem := map[string][]StepStateRecord{}
	for _, row := range stateRows {
		record, err := stepStateFromDB(dbgen.TrackedItemStepState(row))
		if err != nil {
			return nil, err
		}
		statesByItem[record.TrackedItemID] = append(statesByItem[record.TrackedItemID], *record)
	}
	for _, row := range itemRows {
		item, err := itemFromDB(row)
		if err != nil {
			return nil, err
		}
		view := TrackedItemView{Item: *item, StepStates: statesByItem[item.ID]}
		if view.StepStates == nil {
			view.StepStates = []StepStateRecord{}
		}
		progression, err := ComputeProgression(def.Steps, view.StepStates)
		if err != nil {
			return nil, fmt.Errorf("progression for item %s: %w", item.ID, err)
		}
		view.Progression = &progression
		result.Items = append(result.Items, view)
	}
	return result, nil
}

func definitionFromDB(row dbgen.WorkflowDefinition) (*WorkflowDefinition, error) {
	created, err := codec.ParseTime(row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse workflow definition %s created_at: %w", row.ID, err)
	}
	return &WorkflowDefinition{
		ID: row.ID, Name: row.Name, Version: int(row.Version), Active: row.Active == 1, CreatedAt: created,
	}, nil
}

func stepFromDB(row dbgen.WorkflowStep) WorkflowStep {
	return WorkflowStep{
		ID: row.ID, WorkflowDefinitionID: row.WorkflowDefinitionID, Key: row.StepKey,
		Title: row.Title, Sequence: int(row.SequenceNo), Terminal: row.Terminal == 1,
	}
}

func itemFromDB(row dbgen.TrackedItem) (*TrackedItem, error) {
	lifecycle := ItemLifecycle(row.LifecycleState)
	if !lifecycle.Valid() {
		return nil, fmt.Errorf("tracked item %s has invalid lifecycle %q", row.EntityID, row.LifecycleState)
	}
	created, err := codec.ParseTime(row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse tracked item %s created_at: %w", row.EntityID, err)
	}
	updated, err := codec.ParseTime(row.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse tracked item %s updated_at: %w", row.EntityID, err)
	}
	completed, err := codec.ParseOptionalTime(row.CompletedAt)
	if err != nil {
		return nil, fmt.Errorf("parse tracked item %s completed_at: %w", row.EntityID, err)
	}
	return &TrackedItem{
		ID: row.EntityID, WorkItemID: row.WorkItemID, WorkflowID: row.WorkflowID,
		ParentItemID: row.ParentItemID, Title: row.Title,
		ItemType: row.ItemType, Lifecycle: lifecycle,
		CompletedAt: completed, LastRevisionID: row.LastRevisionID, CreatedAt: created, UpdatedAt: updated,
	}, nil
}

func workflowFromDB(row dbgen.Workflow) (*Workflow, error) {
	created, err := codec.ParseTime(row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse workflow %s created_at: %w", row.ID, err)
	}
	updated, err := codec.ParseTime(row.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse workflow %s updated_at: %w", row.ID, err)
	}
	started, err := codec.ParseTime(row.StartedAt)
	if err != nil {
		return nil, fmt.Errorf("parse workflow %s started_at: %w", row.ID, err)
	}
	return &Workflow{
		ID: row.ID, WorkItemID: row.WorkItemID, WorkflowDefinitionID: row.WorkflowDefinitionID,
		CurrentStepID: row.CurrentStepID, StartedAt: started, LastRevisionID: row.LastRevisionID,
		CreatedAt: created, UpdatedAt: updated,
	}, nil
}

func (r *Repository) setWorkflowCurrentStep(ctx context.Context, workflowID, stepID, revisionID string, updatedAt time.Time) error {
	if err := r.queries.SetWorkflowCurrentStep(ctx, dbgen.SetWorkflowCurrentStepParams{
		CurrentStepID: stepID, LastRevisionID: revisionID, UpdatedAt: codec.FormatTime(updatedAt), ID: workflowID,
	}); err != nil {
		return fmt.Errorf("set workflow %s current step: %w", workflowID, err)
	}
	return nil
}

func stepStateFromDB(row dbgen.TrackedItemStepState) (*StepStateRecord, error) {
	state := StepState(row.State)
	if !state.Valid() {
		return nil, fmt.Errorf("step state %s/%s has invalid state %q", row.TrackedItemID, row.WorkflowStepID, row.State)
	}
	entered, err := codec.ParseOptionalTime(row.EnteredAt)
	if err != nil {
		return nil, fmt.Errorf("parse step state entered_at: %w", err)
	}
	completed, err := codec.ParseOptionalTime(row.CompletedAt)
	if err != nil {
		return nil, fmt.Errorf("parse step state completed_at: %w", err)
	}
	return &StepStateRecord{
		TrackedItemID: row.TrackedItemID, WorkflowStepID: row.WorkflowStepID, State: state,
		EnteredAt: entered, CompletedAt: completed, LastRevisionID: row.LastRevisionID,
	}, nil
}

// ListActiveDefinitions returns active definitions with ordered steps in two
// queries.
func (r *Repository) ListActiveDefinitions(ctx context.Context) ([]WorkflowDefinition, error) {
	defRows, err := r.queries.ListActiveWorkflowDefinitions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active workflow definitions: %w", err)
	}
	stepRows, err := r.queries.ListStepsForActiveWorkflowDefinitions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list active workflow steps: %w", err)
	}
	result := make([]WorkflowDefinition, 0, len(defRows))
	index := map[string]int{}
	for _, row := range defRows {
		def, err := definitionFromDB(row)
		if err != nil {
			return nil, err
		}
		index[def.ID] = len(result)
		result = append(result, *def)
	}
	for _, row := range stepRows {
		step := stepFromDB(row)
		if i, ok := index[step.WorkflowDefinitionID]; ok {
			result[i].Steps = append(result[i].Steps, step)
		}
	}
	return result, nil
}
