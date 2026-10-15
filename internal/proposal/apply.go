package proposal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/reminder"
	"lum-harbor-notifier/internal/temporal"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

// Transaction ownership for Apply:
//
//   - ApplyProposal opens the one outer transaction and dispatches to exactly
//     one coordinator: applySingularTx (zero child operations) or
//     applyAggregateTx (one or more).
//   - A coordinator is the only code that finalizes the Proposal and commits.
//   - executeOperationTx and the execute* functions below perform one typed
//     operation's authoritative precondition check and domain mutation inside
//     the caller's transaction. They never commit, roll back, or touch the
//     Proposal row; singular and aggregate Apply share them unchanged.

// operationExecution is the outcome of executing one typed operation.
type operationExecution struct {
	// stale reports that an authoritative precondition no longer holds; no
	// domain mutation was made. staleTarget is the WorkItem, if any, reported
	// with a singular STALE result.
	stale       bool
	staleTarget *workitem.WorkItem
	// appliedRevisionID is the Revision a singular Proposal of this type
	// records as applied_revision_id (unchanged legacy choice).
	appliedRevisionID string
	// results are the complete ordered (entity, Revision) results; the first
	// is the operation's single PRIMARY result.
	results []ProposalOperationResult
	// workItem is the WorkItem a singular Apply result reports, if any.
	workItem *workitem.WorkItem
	// createdTargetID is the root entity a singular root create records as the
	// Proposal's target after Apply.
	createdTargetID *string
	// outputs are the declared referenceable IDs this operation produced.
	outputs map[OutputKind]string
	// afterApplied appends domain History that is caused by the Proposal's
	// PROPOSAL_APPLIED event, once that event exists.
	afterApplied func(ctx context.Context, tx *sql.Tx, appliedEventID string) error
}

func staleExecution(target *workitem.WorkItem) *operationExecution {
	return &operationExecution{stale: true, staleTarget: target}
}

func primaryResult(entityID *string, revisionID string) ProposalOperationResult {
	return ProposalOperationResult{EntityID: entityID, RevisionID: revisionID, Role: ResultRolePrimary}
}

func secondaryResult(entityID *string, revisionID string) ProposalOperationResult {
	return ProposalOperationResult{EntityID: entityID, RevisionID: revisionID, Role: ResultRoleSecondary}
}

// executeOperationTx executes one typed operation carried by op (a singular
// Proposal, or a decoded and reference-resolved aggregate child). Truly
// unknown operation types fail closed in the Tracking family's guard.
func (s *Service) executeOperationTx(ctx context.Context, tx *sql.Tx, op *Proposal) (*operationExecution, error) {
	switch op.OperationType {
	case OperationTypeWorkItemUpdateDetails:
		return s.executeWorkItemUpdateDetailsTx(ctx, tx, op)
	case OperationTypeWorkItemCreate:
		return s.executeWorkItemCreateTx(ctx, tx, op)
	case OperationTypeWorkItemCreateWithWorkflow:
		return s.executeWorkItemCreateWithWorkflowTx(ctx, tx, op)
	case OperationTypeWorkflowSetCurrentStep:
		return s.executeWorkflowSetCurrentStepTx(ctx, tx, op)
	case OperationTypeMonitorPointCreate:
		return s.executeMonitorPointCreateTx(ctx, tx, op)
	case OperationTypeMonitorPointSetState:
		return s.executeMonitorPointSetStateTx(ctx, tx, op)
	case OperationTypePlannerItemCreate:
		return s.executePlannerItemCreateTx(ctx, tx, op)
	case OperationTypePlannerItemSetLocalNote:
		return s.executePlannerItemSetLocalNoteTx(ctx, tx, op)
	case OperationTypeWorkItemSetLifecycle:
		return s.executeWorkItemLifecycleTx(ctx, tx, op)
	case OperationTypeWorkflowCreate:
		return s.executeWorkflowCreateTx(ctx, tx, op)
	default:
		return s.executeTrackingOperationTx(ctx, tx, op)
	}
}

// --- Coordinators -----------------------------------------------------------

// applySingularTx applies a singular Proposal: execute its one operation, then
// finalize, link Sources, record History, and commit - the unchanged
// singular Apply sequence.
func (s *Service) applySingularTx(ctx context.Context, tx *sql.Tx, repo *Repository, prop *Proposal) (*ApplyProposalResult, error) {
	exec, err := s.executeOperationTx(ctx, tx, prop)
	if err != nil {
		return nil, err
	}
	if exec.stale {
		return s.markStaleTx(ctx, tx, repo, prop, exec.staleTarget)
	}
	if exec.createdTargetID != nil {
		if _, err := tx.ExecContext(ctx, "UPDATE change_proposals SET target_entity_id = ? WHERE id = ?", *exec.createdTargetID, prop.ID); err != nil {
			return nil, fmt.Errorf("set proposal target entity id: %w", err)
		}
	}
	now := time.Now().UTC()
	applied, err := repo.MarkProposalAppliedIfPending(ctx, prop.ID, exec.appliedRevisionID, now, now)
	if err != nil {
		return nil, err
	}
	if err := linkResultSources(ctx, repo, exec.results, prop.SourceRevisionIDs, map[string]bool{}); err != nil {
		return nil, err
	}
	revisionID := exec.appliedRevisionID
	appliedEvent, err := recordProposalTransition(ctx, tx, applied, history.EventProposalApplied, history.ActorUser, &revisionID)
	if err != nil {
		return nil, err
	}
	if exec.afterApplied != nil {
		if err := exec.afterApplied(ctx, tx, appliedEvent.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit apply proposal transaction: %w", err)
	}
	return &ApplyProposalResult{Outcome: ApplyOutcomeApplied, Proposal: applied, WorkItem: exec.workItem}, nil
}

// applyAggregateTx applies every child of an aggregate Proposal in sequence
// inside the caller's single outer transaction and commits once. Each child's
// declared references resolve to outputs of earlier children; its complete
// results are persisted and every committed Revision is linked to the
// parent's SourceRevisions. Any error returns before commit, so the deferred
// rollback discards every child mutation and the parent stays PENDING. A stale
// child discards all child mutations first, then marks the parent STALE.
func (s *Service) applyAggregateTx(ctx context.Context, tx *sql.Tx, repo *Repository, prop *Proposal) (*ApplyProposalResult, error) {
	outputs := make([]map[OutputKind]string, len(prop.Operations))
	linked := map[string]bool{}
	var primaryRevisionID string
	var afterApplied []func(ctx context.Context, tx *sql.Tx, appliedEventID string) error
	for index, child := range prop.Operations {
		op, err := decodeOperation(prop.ID, child.OperationType, child.TargetEntityID, child.PayloadJSON)
		if err != nil {
			return nil, fmt.Errorf("aggregate operation %d: %w", index, err)
		}
		if err := resolveReferences(op, index, outputs); err != nil {
			return nil, fmt.Errorf("aggregate operation %d (%s): %w", index, op.OperationType, err)
		}
		exec, err := s.executeOperationTx(ctx, tx, op)
		if err != nil {
			return nil, fmt.Errorf("apply aggregate operation %d (%s): %w", index, op.OperationType, err)
		}
		if exec.stale {
			_ = tx.Rollback()
			return s.markAggregateStale(ctx, prop.ID)
		}
		results, err := childResults(child, op.OperationType, exec)
		if err != nil {
			return nil, err
		}
		if err := repo.CreateOperationResults(ctx, results); err != nil {
			return nil, err
		}
		if err := linkResultSources(ctx, repo, results, prop.SourceRevisionIDs, linked); err != nil {
			return nil, err
		}
		outputs[index] = exec.outputs
		if index == 0 {
			// Compatibility pointer: the first child's PRIMARY result.
			primaryRevisionID = results[0].RevisionID
		}
		if exec.afterApplied != nil {
			afterApplied = append(afterApplied, exec.afterApplied)
		}
	}
	now := time.Now().UTC()
	applied, err := repo.MarkProposalAppliedIfPending(ctx, prop.ID, primaryRevisionID, now, now)
	if err != nil {
		return nil, err
	}
	appliedEvent, err := recordProposalTransition(ctx, tx, applied, history.EventProposalApplied, history.ActorUser, &primaryRevisionID)
	if err != nil {
		return nil, err
	}
	for _, record := range afterApplied {
		if err := record(ctx, tx, appliedEvent.ID); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit apply aggregate proposal transaction: %w", err)
	}
	return &ApplyProposalResult{Outcome: ApplyOutcomeApplied, Proposal: applied}, nil
}

// markAggregateStale records STALE for an aggregate in its own transaction,
// after the transaction holding any partial child mutations was rolled back.
func (s *Service) markAggregateStale(ctx context.Context, proposalID string) (*ApplyProposalResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin stale aggregate proposal transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	staleProp, err := NewRepository(tx).MarkProposalStaleIfPending(ctx, proposalID, now, now)
	if err != nil {
		return nil, err
	}
	if _, err := recordProposalTransition(ctx, tx, staleProp, history.EventProposalStale, history.ActorSystem, nil); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit stale aggregate proposal transaction: %w", err)
	}
	return &ApplyProposalResult{Outcome: ApplyOutcomeStale, Proposal: staleProp}, nil
}

// resolveReferences replaces each declared reference field of the child at
// index with the concrete ID an earlier child produced. Only the fields
// declared by referenceFields are read or written.
func resolveReferences(op *Proposal, index int, outputs []map[OutputKind]string) error {
	for _, field := range referenceFields(op) {
		ref, err := fieldReference(field)
		if err != nil {
			return err
		}
		if ref == nil {
			continue
		}
		if ref.Index >= index {
			return fmt.Errorf("%s: reference %s does not point to an earlier operation", field.name, ref)
		}
		id := outputs[ref.Index][ref.Output]
		if id == "" {
			return fmt.Errorf("%s: operation %d produced no %s", field.name, ref.Index, ref.Output)
		}
		resolved := id
		*field.slot = &resolved
	}
	return nil
}

// childResults binds an execution's results to its child operation and checks
// the result and output contract every executor must satisfy.
func childResults(child ProposalOperation, operationType string, exec *operationExecution) ([]ProposalOperationResult, error) {
	if len(exec.results) == 0 || exec.results[0].Role != ResultRolePrimary {
		return nil, fmt.Errorf("operation %d (%s) produced no primary result", child.SequenceNo, operationType)
	}
	results := make([]ProposalOperationResult, len(exec.results))
	for index, result := range exec.results {
		if index > 0 && result.Role == ResultRolePrimary {
			return nil, fmt.Errorf("operation %d (%s) produced more than one primary result", child.SequenceNo, operationType)
		}
		result.ProposalOperationID = child.ID
		result.SequenceNo = index
		results[index] = result
	}
	for _, kind := range operationOutputs[operationType] {
		if exec.outputs[kind] == "" {
			return nil, fmt.Errorf("operation %d (%s) did not produce declared output %s", child.SequenceNo, operationType, kind)
		}
	}
	return results, nil
}

// linkResultSources links each distinct result Revision to the Proposal's
// SourceRevisions once; linked carries Revisions already linked.
func linkResultSources(ctx context.Context, repo *Repository, results []ProposalOperationResult, sourceRevisionIDs []string, linked map[string]bool) error {
	if len(sourceRevisionIDs) == 0 {
		return nil
	}
	for _, result := range results {
		if linked[result.RevisionID] {
			continue
		}
		if err := repo.LinkRevisionSources(ctx, result.RevisionID, sourceRevisionIDs, "PRIMARY_SOURCE"); err != nil {
			return err
		}
		linked[result.RevisionID] = true
	}
	return nil
}

// --- Typed operation executors ----------------------------------------------

// executeWorkItemUpdateDetailsTx re-reads the target WorkItem in the same
// transaction (avoiding pool deadlock with MaxOpenConns=1) and validates the
// captured preconditions: ExpectedLastRevisionID primarily, title and
// description supplementarily. A missing/archived or changed target is stale.
func (s *Service) executeWorkItemUpdateDetailsTx(ctx context.Context, tx *sql.Tx, op *Proposal) (*operationExecution, error) {
	target, err := s.workItemOperator.GetByIDTx(ctx, tx, targetOrEmpty(op))
	if err != nil {
		if errors.Is(err, workitem.ErrNotFound) {
			return staleExecution(nil), nil
		}
		return nil, err
	}
	preconditions := op.Payload.Preconditions
	if preconditions.ExpectedLastRevisionID != target.LastRevisionID ||
		preconditions.ExpectedTitle != target.Title ||
		!optionalStringEqual(preconditions.ExpectedDescription, target.Description) {
		return staleExecution(target), nil
	}
	updated, err := s.workItemOperator.UpdateLocalWorkItemDetailsTx(ctx, tx, workitem.UpdateLocalWorkItemDetailsInput{
		ID:          targetOrEmpty(op),
		Title:       op.Payload.ProposedTitle,
		Description: op.Payload.ProposedDescription,
	})
	if err != nil {
		return nil, fmt.Errorf("apply work item mutation: %w", err)
	}
	proposalID := op.ID
	return &operationExecution{
		appliedRevisionID: updated.LastRevisionID,
		results:           []ProposalOperationResult{primaryResult(&updated.ID, updated.LastRevisionID)},
		workItem:          updated,
		afterApplied: func(ctx context.Context, tx *sql.Tx, appliedEventID string) error {
			correlationID := history.ProposalCorrelation(proposalID)
			return workitem.RecordDetailsChangedHistoryTx(ctx, tx, target, updated, &workitem.DetailsHistoryProvenance{
				ProposalID: &proposalID, CorrelationID: &correlationID, CausationEventID: &appliedEventID,
			})
		},
	}, nil
}

func (s *Service) executeTrackingOperationTx(ctx context.Context, tx *sql.Tx, op *Proposal) (*operationExecution, error) {
	if s.trackingOperator == nil || op.TrackingPayload == nil {
		return nil, fmt.Errorf("%w: Tracking operation unavailable", ErrInvalidOperationPayload)
	}
	payload := op.TrackingPayload
	if payload.OperationKind != OperationTypeTrackedItemCreate {
		item, err := s.trackingOperator.GetTrackedItemTx(ctx, tx, targetOrEmpty(op))
		if err != nil || payload.ExpectedLastRevisionID == nil || item.LastRevisionID != *payload.ExpectedLastRevisionID {
			return staleExecution(nil), nil
		}
	}
	exec := &operationExecution{}
	switch payload.OperationKind {
	case OperationTypeTrackedItemCreate:
		item, err := s.trackingOperator.CreateTrackedItemTx(ctx, tx, tracking.CreateTrackedItemInput{WorkItemID: value(payload.WorkItemID), WorkflowID: payload.WorkflowID, ParentItemID: payload.ParentItemID, Title: value(payload.Title), ItemType: payload.ItemType})
		if err != nil {
			return nil, err
		}
		exec.appliedRevisionID = item.LastRevisionID
		exec.results = []ProposalOperationResult{primaryResult(&item.ID, item.LastRevisionID)}
		exec.outputs = map[OutputKind]string{OutputTrackedItemID: item.ID}
	case OperationTypeTrackedItemUpdate:
		item, err := s.trackingOperator.UpdateTrackedItemTx(ctx, tx, tracking.UpdateTrackedItemInput{ID: value(payload.TrackedItemID), Title: value(payload.Title), ItemType: payload.ItemType, ParentItemID: payload.ParentItemID})
		if err != nil {
			return nil, err
		}
		exec.appliedRevisionID = item.LastRevisionID
		exec.results = []ProposalOperationResult{primaryResult(&item.ID, item.LastRevisionID)}
	case OperationTypeTrackedItemStageState:
		item, err := s.trackingOperator.SetTrackedItemStageStateTx(ctx, tx, tracking.SetTrackedItemStageStateInput{TrackedItemID: value(payload.TrackedItemID), WorkflowStepID: value(payload.WorkflowStepID), State: tracking.StepState(value(payload.State))})
		if err != nil {
			return nil, err
		}
		exec.appliedRevisionID = item.LastRevisionID
		exec.results = []ProposalOperationResult{primaryResult(&item.ID, item.LastRevisionID)}
	case OperationTypeTrackedItemArchive:
		item, err := s.trackingOperator.ArchiveTrackedItemTx(ctx, tx, value(payload.TrackedItemID))
		if err != nil {
			return nil, err
		}
		exec.appliedRevisionID = item.LastRevisionID
		exec.results = []ProposalOperationResult{primaryResult(&item.ID, item.LastRevisionID)}
	case OperationTypeTrackedItemDependencyAdd:
		relation, err := s.trackingOperator.AddDependencyTx(ctx, tx, tracking.TrackedItemRelationInput{FromTrackedItemID: value(payload.TrackedItemID), ToTrackedItemID: value(payload.ToTrackedItemID)})
		if err != nil {
			return nil, err
		}
		// A relation is not a Foundation Entity.
		exec.appliedRevisionID = relation.LastRevisionID
		exec.results = []ProposalOperationResult{primaryResult(nil, relation.LastRevisionID)}
	case OperationTypeTrackedItemDependencyRemove:
		revisionID, err := s.trackingOperator.RemoveDependencyTx(ctx, tx, tracking.TrackedItemRelationInput{FromTrackedItemID: value(payload.TrackedItemID), ToTrackedItemID: value(payload.ToTrackedItemID)})
		if err != nil {
			return nil, err
		}
		exec.appliedRevisionID = revisionID
		exec.results = []ProposalOperationResult{primaryResult(nil, revisionID)}
	default:
		return nil, fmt.Errorf("%w: unsupported operation %q", ErrInvalidOperationPayload, payload.OperationKind)
	}
	return exec, nil
}

// executePlannerItemCreateTx creates the Foundation Entity, Revision, and
// PlannerItem - and, when configured, its Reminder under the same Revision.
// Like tracked_item.create, a create has no prior state to go stale against.
func (s *Service) executePlannerItemCreateTx(ctx context.Context, tx *sql.Tx, op *Proposal) (*operationExecution, error) {
	if s.plannerOperator == nil || op.PlannerItemPayload == nil {
		return nil, fmt.Errorf("%w: Planner operation unavailable", ErrInvalidOperationPayload)
	}
	payload := op.PlannerItemPayload

	workItemID := op.TargetEntityID
	if workItemID == nil {
		workItemID = payload.WorkItemID
	}
	input := planner.CreateLocalTodoInput{
		Title:          payload.Title,
		Details:        payload.Details,
		WorkItemID:     workItemID,
		WorkflowID:     payload.WorkflowID,
		WorkflowStepID: payload.WorkflowStepID,
		TrackedItemID:  payload.TrackedItemID,
	}
	if payload.DueDate != nil {
		d, err := temporal.ParseLocalDate(*payload.DueDate)
		if err != nil {
			return nil, fmt.Errorf("%w: malformed due_date: %v", ErrInvalidOperationPayload, err)
		}
		input.DueDate = &d
	}
	if payload.DueAt != nil {
		t, err := time.Parse(time.RFC3339, *payload.DueAt)
		if err != nil {
			return nil, fmt.Errorf("%w: malformed due_at: %v", ErrInvalidOperationPayload, err)
		}
		input.DueAt = &t
	}

	item, err := s.plannerOperator.CreateLocalTodoTx(ctx, tx, input)
	if err != nil {
		if errors.Is(err, planner.ErrInvalidWorkflow) || errors.Is(err, planner.ErrInvalidWorkflowStep) || errors.Is(err, planner.ErrInvalidWorkItem) || errors.Is(err, planner.ErrInvalidTrackedItem) {
			return nil, fmt.Errorf("%w: %w", ErrInvalidOperationPayload, err)
		}
		return nil, fmt.Errorf("apply planner item create: %w", err)
	}
	exec := &operationExecution{
		appliedRevisionID: item.LastRevisionID,
		results:           []ProposalOperationResult{primaryResult(&item.ID, item.LastRevisionID)},
		outputs:           map[OutputKind]string{OutputPlannerItemID: item.ID},
	}
	if s.reminderOperator != nil && len(payload.OffsetsSeconds) > 0 {
		configured, err := s.reminderOperator.SetReminderTx(ctx, tx, reminder.SetReminderInput{
			PlannerItemID:        item.ID,
			OffsetsSeconds:       payload.OffsetsSeconds,
			ConfiguredAtCreation: true,
		}, item.LastRevisionID, time.Now().UTC())
		if err != nil {
			return nil, fmt.Errorf("apply planner item create reminder: %w", err)
		}
		// The Reminder entity shares the Todo's Revision.
		exec.results = append(exec.results, secondaryResult(&configured.ID, configured.LastRevisionID))
	}
	return exec, nil
}

// executeWorkItemLifecycleTx re-validates the captured ExpectedLifecycleState
// against the authoritative WorkItem; SetLocalWorkItemLifecycleStateTx owns
// the transition graph, which is never duplicated here.
func (s *Service) executeWorkItemLifecycleTx(ctx context.Context, tx *sql.Tx, op *Proposal) (*operationExecution, error) {
	if op.WorkItemLifecyclePayload == nil {
		return nil, fmt.Errorf("%w: lifecycle operation unavailable", ErrInvalidOperationPayload)
	}
	payload := op.WorkItemLifecyclePayload
	target, err := s.workItemOperator.GetByIDTx(ctx, tx, targetOrEmpty(op))
	if err != nil {
		return staleExecution(nil), nil
	}
	if string(target.Lifecycle) != payload.ExpectedLifecycleState {
		return staleExecution(target), nil
	}
	updated, err := s.workItemOperator.SetLocalWorkItemLifecycleStateTx(ctx, tx, workitem.SetLocalWorkItemLifecycleStateInput{
		ID: target.ID, Target: workitem.LifecycleState(payload.TargetState),
	})
	if err != nil {
		return nil, fmt.Errorf("apply work item lifecycle: %w", err)
	}
	return &operationExecution{
		appliedRevisionID: updated.LastRevisionID,
		results:           []ProposalOperationResult{primaryResult(&updated.ID, updated.LastRevisionID)},
		workItem:          updated,
	}, nil
}

// executeWorkflowCreateTx creates the non-revisioned WorkflowDefinition and
// attaches the Workflow, producing the one Revision CreateWorkflowTx produces
// on its own; CreateWorkflowTx re-enforces at most one Workflow per WorkItem.
func (s *Service) executeWorkflowCreateTx(ctx context.Context, tx *sql.Tx, op *Proposal) (*operationExecution, error) {
	if s.trackingOperator == nil || op.WorkflowCreatePayload == nil {
		return nil, fmt.Errorf("%w: workflow operation unavailable", ErrInvalidOperationPayload)
	}
	payload := op.WorkflowCreatePayload
	target, err := s.workItemOperator.GetByIDTx(ctx, tx, targetOrEmpty(op))
	if err != nil {
		return staleExecution(nil), nil
	}
	if payload.ExpectedLastRevisionID != nil && target.LastRevisionID != *payload.ExpectedLastRevisionID {
		return staleExecution(target), nil
	}
	wf, err := s.createWorkflowTx(ctx, tx, target.ID, payload.DefinitionName, payload.Steps, "apply workflow create")
	if err != nil {
		return nil, err
	}
	// A Workflow is not a Foundation Entity.
	return &operationExecution{
		appliedRevisionID: wf.LastRevisionID,
		results:           []ProposalOperationResult{primaryResult(nil, wf.LastRevisionID)},
		outputs:           map[OutputKind]string{OutputWorkflowID: wf.ID},
	}, nil
}

func (s *Service) executeWorkItemCreateTx(ctx context.Context, tx *sql.Tx, op *Proposal) (*operationExecution, error) {
	if op.WorkItemCreatePayload == nil {
		return nil, fmt.Errorf("%w: work item create operation unavailable", ErrInvalidOperationPayload)
	}
	wi, err := s.workItemOperator.CreateLocalWorkItemTx(ctx, tx, workitem.CreateLocalWorkItemInput{
		Title:       op.WorkItemCreatePayload.Title,
		Description: op.WorkItemCreatePayload.Description,
	})
	if err != nil {
		return nil, fmt.Errorf("apply work item create: %w", err)
	}
	return &operationExecution{
		appliedRevisionID: wi.LastRevisionID,
		results:           []ProposalOperationResult{primaryResult(&wi.ID, wi.LastRevisionID)},
		workItem:          wi,
		createdTargetID:   &wi.ID,
		outputs:           map[OutputKind]string{OutputWorkItemID: wi.ID},
	}, nil
}

// executeWorkItemCreateWithWorkflowTx creates the WorkItem and its Workflow
// together: two Revisions, of which only the WorkItem's has an Entity. The
// singular applied_revision_id stays the Workflow Revision (legacy choice).
func (s *Service) executeWorkItemCreateWithWorkflowTx(ctx context.Context, tx *sql.Tx, op *Proposal) (*operationExecution, error) {
	if s.trackingOperator == nil || op.WorkItemCreateWithWorkflowPayload == nil {
		return nil, fmt.Errorf("%w: work item create with workflow operation unavailable", ErrInvalidOperationPayload)
	}
	payload := op.WorkItemCreateWithWorkflowPayload
	wi, err := s.workItemOperator.CreateLocalWorkItemTx(ctx, tx, workitem.CreateLocalWorkItemInput{
		Title:       payload.Title,
		Description: payload.Description,
	})
	if err != nil {
		return nil, fmt.Errorf("apply work item create with workflow: create work item: %w", err)
	}
	wf, err := s.createWorkflowTx(ctx, tx, wi.ID, payload.WorkflowName, payload.Stages, "apply work item create with workflow")
	if err != nil {
		return nil, err
	}
	return &operationExecution{
		appliedRevisionID: wf.LastRevisionID,
		results: []ProposalOperationResult{
			primaryResult(&wi.ID, wi.LastRevisionID),
			secondaryResult(nil, wf.LastRevisionID),
		},
		workItem:        wi,
		createdTargetID: &wi.ID,
		outputs:         map[OutputKind]string{OutputWorkItemID: wi.ID, OutputWorkflowID: wf.ID},
	}, nil
}

// createWorkflowTx creates a version-1 WorkflowDefinition from ordered stage
// titles and attaches its Workflow to workItemID.
func (s *Service) createWorkflowTx(ctx context.Context, tx *sql.Tx, workItemID, name string, stages []string, errorContext string) (*tracking.Workflow, error) {
	steps := make([]tracking.WorkflowStepInput, 0, len(stages))
	for index, title := range stages {
		steps = append(steps, tracking.WorkflowStepInput{Key: fmt.Sprintf("step_%d", index+1), Title: title})
	}
	def, err := s.trackingOperator.CreateWorkflowDefinitionTx(ctx, tx, tracking.CreateWorkflowDefinitionInput{
		Name: name, Version: 1, Steps: steps,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: create definition: %w", errorContext, err)
	}
	wf, err := s.trackingOperator.CreateWorkflowTx(ctx, tx, tracking.CreateWorkflowInput{
		WorkItemID: workItemID, WorkflowDefinitionID: def.ID,
	})
	if err != nil {
		return nil, fmt.Errorf("%s: attach workflow: %w", errorContext, err)
	}
	return wf, nil
}

func (s *Service) executeWorkflowSetCurrentStepTx(ctx context.Context, tx *sql.Tx, op *Proposal) (*operationExecution, error) {
	if s.trackingOperator == nil || op.WorkflowSetCurrentStepPayload == nil {
		return nil, fmt.Errorf("%w: workflow set current step operation unavailable", ErrInvalidOperationPayload)
	}
	payload := op.WorkflowSetCurrentStepPayload
	wf, err := s.trackingOperator.GetWorkflowTx(ctx, tx, payload.WorkflowID)
	if err != nil {
		return staleExecution(nil), nil
	}
	if wf.LastRevisionID != payload.ExpectedWorkflowRevisionID || wf.CurrentStepID != payload.ExpectedCurrentStepID {
		return staleExecution(nil), nil
	}
	updated, err := s.trackingOperator.SetWorkflowCurrentStepTx(ctx, tx, tracking.SetWorkflowCurrentStepInput{
		WorkflowID: payload.WorkflowID,
		StepID:     payload.TargetStepID,
	})
	if err != nil {
		return nil, fmt.Errorf("apply workflow set current step: %w", err)
	}
	return &operationExecution{
		appliedRevisionID: updated.LastRevisionID,
		results:           []ProposalOperationResult{primaryResult(nil, updated.LastRevisionID)},
	}, nil
}

func (s *Service) executeMonitorPointCreateTx(ctx context.Context, tx *sql.Tx, op *Proposal) (*operationExecution, error) {
	if s.trackingOperator == nil || op.MonitorPointCreatePayload == nil {
		return nil, fmt.Errorf("%w: monitor point create operation unavailable", ErrInvalidOperationPayload)
	}
	payload := op.MonitorPointCreatePayload
	targetWI, err := s.workItemOperator.GetByIDTx(ctx, tx, payload.WorkItemID)
	if err != nil {
		return staleExecution(nil), nil
	}
	if payload.ExpectedWorkItemRevisionID != nil && targetWI.LastRevisionID != *payload.ExpectedWorkItemRevisionID {
		return staleExecution(targetWI), nil
	}
	if payload.SubjectTrackedItemID != nil {
		ti, err := s.trackingOperator.GetTrackedItemTx(ctx, tx, *payload.SubjectTrackedItemID)
		if err != nil {
			return staleExecution(targetWI), nil
		}
		if payload.ExpectedTrackedItemRevisionID != nil && ti.LastRevisionID != *payload.ExpectedTrackedItemRevisionID {
			return staleExecution(targetWI), nil
		}
	}
	dueAt, err := time.Parse(time.RFC3339, payload.DueAt)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid due_at timestamp: %v", ErrInvalidOperationPayload, err)
	}
	var expectedLifecycle *workitem.LifecycleState
	if payload.ExpectedWorkItemLifecycle != nil {
		l := workitem.LifecycleState(*payload.ExpectedWorkItemLifecycle)
		expectedLifecycle = &l
	}
	point, err := s.trackingOperator.CreateMonitorPointTx(ctx, tx, tracking.CreateMonitorPointInput{
		WorkItemID:                payload.WorkItemID,
		SubjectTrackedItemID:      payload.SubjectTrackedItemID,
		Kind:                      tracking.MonitorKind(payload.Kind),
		Title:                     payload.Title,
		DueAt:                     dueAt,
		Timezone:                  payload.Timezone,
		ExpectedWorkItemLifecycle: expectedLifecycle,
		ExpectedStepID:            payload.ExpectedStepID,
	})
	if err != nil {
		return nil, fmt.Errorf("apply monitor point create: %w", err)
	}
	return &operationExecution{
		appliedRevisionID: point.LastRevisionID,
		results:           []ProposalOperationResult{primaryResult(&point.ID, point.LastRevisionID)},
	}, nil
}

func (s *Service) executeMonitorPointSetStateTx(ctx context.Context, tx *sql.Tx, op *Proposal) (*operationExecution, error) {
	if s.trackingOperator == nil || op.MonitorPointSetStatePayload == nil {
		return nil, fmt.Errorf("%w: monitor point set state operation unavailable", ErrInvalidOperationPayload)
	}
	payload := op.MonitorPointSetStatePayload
	point, err := s.trackingOperator.GetMonitorPointTx(ctx, tx, payload.MonitorPointID)
	if err != nil {
		return staleExecution(nil), nil
	}
	if point.LastRevisionID != payload.ExpectedLastRevisionID || string(point.State) != payload.ExpectedState {
		return staleExecution(nil), nil
	}
	updated, err := s.trackingOperator.SetMonitorPointStateTx(ctx, tx, tracking.SetMonitorPointStateInput{
		ID:     payload.MonitorPointID,
		Target: tracking.MonitorState(payload.TargetState),
	})
	if err != nil {
		return nil, fmt.Errorf("apply monitor point set state: %w", err)
	}
	return &operationExecution{
		appliedRevisionID: updated.LastRevisionID,
		results:           []ProposalOperationResult{primaryResult(&updated.ID, updated.LastRevisionID)},
	}, nil
}
