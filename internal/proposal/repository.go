package proposal

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"lum-harbor-notifier/internal/database/codec"
	dbgen "lum-harbor-notifier/internal/database/generated"
)

// ErrNotFound indicates no proposal exists with the requested ID.
var ErrNotFound = errors.New("proposal not found")

// Repository handles persistence mapping for change_proposals through dbgen.Queries.
// It is strictly a persistence mapper and does not enforce business lifecycle rules.
type Repository struct {
	queries *dbgen.Queries
}

// NewRepository constructs a Repository bound to the given database executor
// (*sql.DB for reads, *sql.Tx for mutations).
func NewRepository(db dbgen.DBTX) *Repository {
	return &Repository{queries: dbgen.New(db)}
}

// Create persists a new Proposal row, its Source links and, for an aggregate,
// its ordered child operations. Children are written once with the parent;
// their results are written only by Apply (CreateOperationResults).
func (r *Repository) Create(ctx context.Context, p *Proposal) error {
	if err := validateProposalShape(p); err != nil {
		return err
	}
	payloadJSON, err := marshalProposalPayload(p)
	if err != nil {
		return fmt.Errorf("serialize proposal payload: %w", err)
	}

	if err := r.queries.CreateProposal(ctx, dbgen.CreateProposalParams{
		ID:                   p.ID,
		TargetEntityID:       p.TargetEntityID,
		OperationType:        p.OperationType,
		Title:                p.Title,
		Summary:              p.Summary,
		Reason:               p.Reason,
		Status:               string(p.Status),
		OperationPayloadJson: payloadJSON,
		AppliedRevisionID:    p.AppliedRevisionID,
		AiRunID:              p.AIRunID,
		CreatedAt:            codec.FormatTime(p.CreatedAt),
		UpdatedAt:            codec.FormatTime(p.UpdatedAt),
		ResolvedAt:           codec.FormatOptionalTime(p.ResolvedAt),
	}); err != nil {
		return fmt.Errorf("create proposal %s: %w", p.ID, err)
	}
	for _, sourceRevID := range p.SourceRevisionIDs {
		role := "PRIMARY_SOURCE"
		if err := r.queries.CreateProposalSource(ctx, dbgen.CreateProposalSourceParams{
			ProposalID:       p.ID,
			SourceRevisionID: sourceRevID,
			Role:             &role,
		}); err != nil {
			return fmt.Errorf("create proposal source %s -> %s: %w", p.ID, sourceRevID, err)
		}
	}
	for _, op := range p.Operations {
		if err := r.queries.CreateProposalOperation(ctx, dbgen.CreateProposalOperationParams{
			ID:                   op.ID,
			ProposalID:           p.ID,
			SequenceNo:           int64(op.SequenceNo),
			OperationType:        op.OperationType,
			TargetEntityID:       op.TargetEntityID,
			OperationPayloadJson: op.PayloadJSON,
		}); err != nil {
			return fmt.Errorf("create proposal operation %s #%d for %s: %w", op.ID, op.SequenceNo, p.ID, err)
		}
	}
	return nil
}

// CreateOperationResults persists the complete ordered results of applied
// child operations. Aggregate Apply calls it in the same transaction that marks
// the parent APPLIED; like the rest of Repository it enforces no lifecycle rule.
func (r *Repository) CreateOperationResults(ctx context.Context, results []ProposalOperationResult) error {
	for _, result := range results {
		if !result.Role.Valid() {
			return fmt.Errorf("%w: invalid result role %q for operation %s", ErrInvalidOperationPayload, result.Role, result.ProposalOperationID)
		}
		if err := r.queries.CreateProposalOperationResult(ctx, dbgen.CreateProposalOperationResultParams{
			ProposalOperationID: result.ProposalOperationID,
			SequenceNo:          int64(result.SequenceNo),
			EntityID:            result.EntityID,
			RevisionID:          result.RevisionID,
			ResultRole:          string(result.Role),
		}); err != nil {
			return fmt.Errorf("create proposal operation result %s #%d: %w", result.ProposalOperationID, result.SequenceNo, err)
		}
	}
	return nil
}

// GetByID loads a single Proposal by its ID.
func (r *Repository) GetByID(ctx context.Context, id string) (*Proposal, error) {
	row, err := r.queries.GetProposalByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return nil, fmt.Errorf("load proposal %s: %w", id, err)
	}
	p, err := proposalFromDB(row)
	if err != nil {
		return nil, err
	}
	sources, err := r.queries.ListProposalSources(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("load proposal sources for %s: %w", id, err)
	}
	for _, s := range sources {
		p.SourceRevisionIDs = append(p.SourceRevisionIDs, s.SourceRevisionID)
	}
	if err := r.loadOperations(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// FindPendingBySourceRevision finds a pending proposal linked to the given source revision.
func (r *Repository) FindPendingBySourceRevision(ctx context.Context, sourceRevisionID string) (*Proposal, error) {
	row, err := r.queries.FindPendingProposalBySourceRevision(ctx, sourceRevisionID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("find pending proposal for source revision %s: %w", sourceRevisionID, err)
	}
	p, err := proposalFromDB(row)
	if err != nil {
		return nil, err
	}
	sources, err := r.queries.ListProposalSources(ctx, p.ID)
	if err == nil {
		for _, s := range sources {
			p.SourceRevisionIDs = append(p.SourceRevisionIDs, s.SourceRevisionID)
		}
	}
	if err := r.loadOperations(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// ListPending loads all proposals currently in PENDING status in deterministic
// order (created_at ASC, id ASC).
func (r *Repository) ListPending(ctx context.Context) ([]Proposal, error) {
	rows, err := r.queries.ListPendingProposals(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pending proposals: %w", err)
	}
	proposals := make([]Proposal, 0, len(rows))
	for _, row := range rows {
		p, err := proposalFromDB(row)
		if err != nil {
			return nil, fmt.Errorf("convert proposal %s: %w", row.ID, err)
		}
		sources, err := r.queries.ListProposalSources(ctx, p.ID)
		if err == nil {
			for _, s := range sources {
				p.SourceRevisionIDs = append(p.SourceRevisionIDs, s.SourceRevisionID)
			}
		}
		if err := r.loadOperations(ctx, p); err != nil {
			return nil, err
		}
		proposals = append(proposals, *p)
	}
	return proposals, nil
}

// TransitionStatusCAS performs a conditional compare-and-swap update of the
// proposal status. If the current status in DB does not match fromStatus,
// sql.ErrNoRows is returned.
func (r *Repository) TransitionStatusCAS(
	ctx context.Context,
	id string,
	fromStatus Status,
	toStatus Status,
	resolvedAt *time.Time,
	updatedAt time.Time,
) (*Proposal, error) {
	row, err := r.queries.TransitionProposalStatus(ctx, dbgen.TransitionProposalStatusParams{
		ToStatus:   string(toStatus),
		ResolvedAt: codec.FormatOptionalTime(resolvedAt),
		UpdatedAt:  codec.FormatTime(updatedAt),
		ID:         id,
		FromStatus: string(fromStatus),
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("transition proposal %s status from %s to %s: %w", id, fromStatus, toStatus, err)
	}
	p, err := proposalFromDB(row)
	if err != nil {
		return nil, err
	}
	sources, err := r.queries.ListProposalSources(ctx, id)
	if err == nil {
		for _, s := range sources {
			p.SourceRevisionIDs = append(p.SourceRevisionIDs, s.SourceRevisionID)
		}
	}
	if err := r.loadOperations(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// MarkProposalAppliedIfPending conditionally updates a PENDING proposal to APPLIED
// and associates it with the committed revision ID. If current status is not PENDING,
// sql.ErrNoRows is returned.
func (r *Repository) MarkProposalAppliedIfPending(
	ctx context.Context,
	id string,
	appliedRevisionID string,
	resolvedAt time.Time,
	updatedAt time.Time,
) (*Proposal, error) {
	row, err := r.queries.MarkProposalAppliedIfPending(ctx, dbgen.MarkProposalAppliedIfPendingParams{
		AppliedRevisionID: &appliedRevisionID,
		ResolvedAt:        codec.FormatOptionalTime(&resolvedAt),
		UpdatedAt:         codec.FormatTime(updatedAt),
		ID:                id,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("mark proposal %s applied: %w", id, err)
	}
	p, err := proposalFromDB(row)
	if err != nil {
		return nil, err
	}
	sources, err := r.queries.ListProposalSources(ctx, id)
	if err == nil {
		for _, s := range sources {
			p.SourceRevisionIDs = append(p.SourceRevisionIDs, s.SourceRevisionID)
		}
	}
	if err := r.loadOperations(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// MarkProposalStaleIfPending conditionally updates a PENDING proposal to STALE.
// If current status is not PENDING, sql.ErrNoRows is returned.
func (r *Repository) MarkProposalStaleIfPending(
	ctx context.Context,
	id string,
	resolvedAt time.Time,
	updatedAt time.Time,
) (*Proposal, error) {
	row, err := r.queries.MarkProposalStaleIfPending(ctx, dbgen.MarkProposalStaleIfPendingParams{
		ResolvedAt: codec.FormatOptionalTime(&resolvedAt),
		UpdatedAt:  codec.FormatTime(updatedAt),
		ID:         id,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, sql.ErrNoRows
		}
		return nil, fmt.Errorf("mark proposal %s stale: %w", id, err)
	}
	p, err := proposalFromDB(row)
	if err != nil {
		return nil, err
	}
	sources, err := r.queries.ListProposalSources(ctx, id)
	if err == nil {
		for _, s := range sources {
			p.SourceRevisionIDs = append(p.SourceRevisionIDs, s.SourceRevisionID)
		}
	}
	if err := r.loadOperations(ctx, p); err != nil {
		return nil, err
	}
	return p, nil
}

// LinkRevisionSources persists links between a committed domain Revision and exact Source revisions.
func (r *Repository) LinkRevisionSources(ctx context.Context, revisionID string, sourceRevisionIDs []string, role string) error {
	for _, sourceRevID := range sourceRevisionIDs {
		rRole := role
		if err := r.queries.CreateRevisionSource(ctx, dbgen.CreateRevisionSourceParams{
			RevisionID:       revisionID,
			SourceRevisionID: sourceRevID,
			Role:             &rRole,
		}); err != nil {
			return fmt.Errorf("create revision source %s -> %s: %w", revisionID, sourceRevID, err)
		}
	}
	return nil
}

// ListRevisionSources retrieves source revision IDs linked to a domain Revision.
func (r *Repository) ListRevisionSources(ctx context.Context, revisionID string) ([]string, error) {
	rows, err := r.queries.ListRevisionSources(ctx, revisionID)
	if err != nil {
		return nil, fmt.Errorf("list revision sources for revision %s: %w", revisionID, err)
	}
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.SourceRevisionID)
	}
	return out, nil
}

// marshalProposalPayload serializes whichever typed payload family is set on
// p, dispatching on OperationType. Exactly one payload field is ever set by a
// Propose* constructor.
func marshalProposalPayload(p *Proposal) (string, error) {
	switch {
	case p.OperationType == OperationTypeAggregate:
		return aggregateCompatibilityPayloadJSON()
	case p.WorkItemCreatePayload != nil:
		return p.WorkItemCreatePayload.JSON()
	case p.WorkItemCreateWithWorkflowPayload != nil:
		return p.WorkItemCreateWithWorkflowPayload.JSON()
	case p.MonitorPointCreatePayload != nil:
		return p.MonitorPointCreatePayload.JSON()
	case p.MonitorPointSetStatePayload != nil:
		return p.MonitorPointSetStatePayload.JSON()
	case p.WorkflowSetCurrentStepPayload != nil:
		return p.WorkflowSetCurrentStepPayload.JSON()
	case p.PlannerLocalNotePayload != nil:
		return p.PlannerLocalNotePayload.JSON()
	case p.TrackingPayload != nil:
		return p.TrackingPayload.JSON()
	case p.PlannerItemPayload != nil:
		return p.PlannerItemPayload.JSON()
	case p.WorkItemLifecyclePayload != nil:
		return p.WorkItemLifecyclePayload.JSON()
	case p.WorkflowCreatePayload != nil:
		return p.WorkflowCreatePayload.JSON()
	default:
		return p.Payload.JSON()
	}
}

func proposalFromDB(row dbgen.ChangeProposal) (*Proposal, error) {
	status := Status(row.Status)
	if !status.Valid() {
		return nil, fmt.Errorf("invalid proposal status %q in database", row.Status)
	}
	createdAt, err := codec.ParseTime(row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	updatedAt, err := codec.ParseTime(row.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}
	resolvedAt, err := codec.ParseOptionalTime(row.ResolvedAt)
	if err != nil {
		return nil, fmt.Errorf("parse resolved_at: %w", err)
	}
	proposal := &Proposal{
		ID:                row.ID,
		TargetEntityID:    row.TargetEntityID,
		OperationType:     row.OperationType,
		Title:             row.Title,
		Summary:           row.Summary,
		Reason:            row.Reason,
		Status:            status,
		AppliedRevisionID: row.AppliedRevisionID,
		AIRunID:           row.AiRunID,
		CreatedAt:         createdAt,
		UpdatedAt:         updatedAt,
		ResolvedAt:        resolvedAt,
	}
	if err := decodeOperationPayload(proposal, row.OperationPayloadJson); err != nil {
		return nil, err
	}
	return proposal, nil
}

// validateProposalShape keeps the aggregate discriminator and the child rows in
// agreement before anything is written: an aggregate parent has at least one
// child, a singular Proposal has none, and results never exist before Apply.
func validateProposalShape(p *Proposal) error {
	aggregate := p.OperationType == OperationTypeAggregate
	if aggregate && len(p.Operations) == 0 {
		return fmt.Errorf("%w: aggregate proposal %s requires at least one operation", ErrInvalidOperationPayload, p.ID)
	}
	if !aggregate && len(p.Operations) > 0 {
		return fmt.Errorf("%w: %s proposal %s cannot carry child operations", ErrInvalidOperationPayload, p.OperationType, p.ID)
	}
	for _, op := range p.Operations {
		if op.ProposalID != "" && op.ProposalID != p.ID {
			return fmt.Errorf("%w: operation %s belongs to proposal %s, not %s", ErrInvalidOperationPayload, op.ID, op.ProposalID, p.ID)
		}
		if len(op.Results) > 0 {
			return fmt.Errorf("%w: operation %s cannot be created with results", ErrInvalidOperationPayload, op.ID)
		}
	}
	return nil
}

// loadOperations attaches an aggregate Proposal's ordered child operations and
// their ordered results. A singular Proposal has no child rows and keeps nil
// Operations, exactly as it loaded before aggregates existed. A discriminator
// that disagrees with the child rows fails closed rather than guessing.
func (r *Repository) loadOperations(ctx context.Context, p *Proposal) error {
	rows, err := r.queries.ListProposalOperations(ctx, p.ID)
	if err != nil {
		return fmt.Errorf("load proposal operations for %s: %w", p.ID, err)
	}
	aggregate := p.OperationType == OperationTypeAggregate
	if len(rows) == 0 {
		if aggregate {
			return fmt.Errorf("%w: aggregate proposal %s has no operations", ErrInvalidOperationPayload, p.ID)
		}
		return nil
	}
	if !aggregate {
		return fmt.Errorf("%w: %s proposal %s has child operations", ErrInvalidOperationPayload, p.OperationType, p.ID)
	}
	operations := make([]ProposalOperation, 0, len(rows))
	byID := make(map[string]int, len(rows))
	for _, row := range rows {
		byID[row.ID] = len(operations)
		operations = append(operations, ProposalOperation{
			ID:             row.ID,
			ProposalID:     row.ProposalID,
			SequenceNo:     int(row.SequenceNo),
			OperationType:  row.OperationType,
			TargetEntityID: row.TargetEntityID,
			PayloadJSON:    row.OperationPayloadJson,
		})
	}
	results, err := r.queries.ListProposalOperationResults(ctx, p.ID)
	if err != nil {
		return fmt.Errorf("load proposal operation results for %s: %w", p.ID, err)
	}
	for _, row := range results {
		role := ResultRole(row.ResultRole)
		if !role.Valid() {
			return fmt.Errorf("invalid proposal operation result role %q in database", row.ResultRole)
		}
		index := byID[row.ProposalOperationID]
		operations[index].Results = append(operations[index].Results, ProposalOperationResult{
			ProposalOperationID: row.ProposalOperationID,
			SequenceNo:          int(row.SequenceNo),
			EntityID:            row.EntityID,
			RevisionID:          row.RevisionID,
			Role:                role,
		})
	}
	p.Operations = operations
	return nil
}

// decodeOperationPayload strictly parses payloadJSON into the typed payload
// family of p.OperationType. Unknown types fall through to the Tracking family,
// whose discriminator check fails closed.
func decodeOperationPayload(p *Proposal, payloadJSON string) error {
	switch p.OperationType {
	case OperationTypeAggregate:
		if err := parseAggregateCompatibilityPayload([]byte(payloadJSON)); err != nil {
			return fmt.Errorf("parse aggregate payload for proposal %s: %w", p.ID, err)
		}
	case OperationTypeWorkItemUpdateDetails:
		payload, err := ParseWorkItemUpdateDetailsPayload([]byte(payloadJSON))
		if err != nil {
			return fmt.Errorf("parse operation payload for proposal %s: %w", p.ID, err)
		}
		if p.OperationType != payload.OperationKind {
			return fmt.Errorf("%w: operation discriminator mismatch", ErrInvalidOperationPayload)
		}
		p.Payload = *payload
	case OperationTypeWorkItemCreate:
		payload, err := ParseWorkItemCreatePayload([]byte(payloadJSON))
		if err != nil {
			return fmt.Errorf("parse work item create payload for proposal %s: %w", p.ID, err)
		}
		if p.OperationType != payload.OperationKind {
			return fmt.Errorf("%w: operation discriminator mismatch", ErrInvalidOperationPayload)
		}
		p.WorkItemCreatePayload = payload
	case OperationTypeWorkItemCreateWithWorkflow:
		payload, err := ParseWorkItemCreateWithWorkflowPayload([]byte(payloadJSON))
		if err != nil {
			return fmt.Errorf("parse work item create with workflow payload for proposal %s: %w", p.ID, err)
		}
		if p.OperationType != payload.OperationKind {
			return fmt.Errorf("%w: operation discriminator mismatch", ErrInvalidOperationPayload)
		}
		p.WorkItemCreateWithWorkflowPayload = payload
	case OperationTypeMonitorPointCreate:
		payload, err := ParseMonitorPointCreatePayload([]byte(payloadJSON))
		if err != nil {
			return fmt.Errorf("parse monitor point create payload for proposal %s: %w", p.ID, err)
		}
		if p.OperationType != payload.OperationKind {
			return fmt.Errorf("%w: operation discriminator mismatch", ErrInvalidOperationPayload)
		}
		p.MonitorPointCreatePayload = payload
	case OperationTypeMonitorPointSetState:
		payload, err := ParseMonitorPointSetStatePayload([]byte(payloadJSON))
		if err != nil {
			return fmt.Errorf("parse monitor point set state payload for proposal %s: %w", p.ID, err)
		}
		if p.OperationType != payload.OperationKind {
			return fmt.Errorf("%w: operation discriminator mismatch", ErrInvalidOperationPayload)
		}
		p.MonitorPointSetStatePayload = payload
	case OperationTypeWorkflowSetCurrentStep:
		payload, err := ParseWorkflowSetCurrentStepPayload([]byte(payloadJSON))
		if err != nil {
			return fmt.Errorf("parse workflow set current step payload for proposal %s: %w", p.ID, err)
		}
		if p.OperationType != payload.OperationKind {
			return fmt.Errorf("%w: operation discriminator mismatch", ErrInvalidOperationPayload)
		}
		p.WorkflowSetCurrentStepPayload = payload
	case OperationTypePlannerItemSetLocalNote:
		payload, err := ParsePlannerItemSetLocalNotePayload([]byte(payloadJSON))
		if err != nil {
			return fmt.Errorf("parse planner local note payload for proposal %s: %w", p.ID, err)
		}
		if p.OperationType != payload.OperationKind {
			return fmt.Errorf("%w: operation discriminator mismatch", ErrInvalidOperationPayload)
		}
		p.PlannerLocalNotePayload = payload
	case OperationTypePlannerItemCreate:
		payload, err := ParsePlannerItemCreatePayload([]byte(payloadJSON))
		if err != nil {
			return fmt.Errorf("parse Planner create payload for proposal %s: %w", p.ID, err)
		}
		if p.OperationType != payload.OperationKind {
			return fmt.Errorf("%w: operation discriminator mismatch", ErrInvalidOperationPayload)
		}
		p.PlannerItemPayload = payload
	case OperationTypeWorkItemSetLifecycle:
		payload, err := ParseWorkItemSetLifecyclePayload([]byte(payloadJSON))
		if err != nil {
			return fmt.Errorf("parse lifecycle payload for proposal %s: %w", p.ID, err)
		}
		if p.OperationType != payload.OperationKind {
			return fmt.Errorf("%w: operation discriminator mismatch", ErrInvalidOperationPayload)
		}
		p.WorkItemLifecyclePayload = payload
	case OperationTypeWorkflowCreate:
		payload, err := ParseWorkflowCreatePayload([]byte(payloadJSON))
		if err != nil {
			return fmt.Errorf("parse workflow create payload for proposal %s: %w", p.ID, err)
		}
		if p.OperationType != payload.OperationKind {
			return fmt.Errorf("%w: operation discriminator mismatch", ErrInvalidOperationPayload)
		}
		p.WorkflowCreatePayload = payload
	default:
		payload, err := ParseTrackedItemOperationPayload([]byte(payloadJSON))
		if err != nil {
			return fmt.Errorf("parse Tracking payload for proposal %s: %w", p.ID, err)
		}
		if p.OperationType != payload.OperationKind {
			return fmt.Errorf("%w: operation discriminator mismatch", ErrInvalidOperationPayload)
		}
		p.TrackingPayload = payload
	}
	return nil
}

// decodeOperation decodes one aggregate child into a transient singular
// operation carrier, so preflight and Apply use exactly the typed payloads and
// validation a singular Proposal of that type uses. proposalID is the parent's.
func decodeOperation(proposalID, operationType string, targetEntityID *string, payloadJSON string) (*Proposal, error) {
	if !isSingularOperationType(operationType) {
		return nil, fmt.Errorf("%w: %q is not a singular operation type", ErrInvalidOperationPayload, operationType)
	}
	op := &Proposal{ID: proposalID, OperationType: operationType, TargetEntityID: cloneString(targetEntityID)}
	if err := decodeOperationPayload(op, payloadJSON); err != nil {
		return nil, err
	}
	return op, nil
}

// isSingularOperationType reports membership in the closed typed operation
// set. The aggregate parent type is deliberately not a member: aggregates do
// not nest.
func isSingularOperationType(operationType string) bool {
	switch operationType {
	case OperationTypeWorkItemUpdateDetails, OperationTypeWorkItemCreate, OperationTypeWorkItemCreateWithWorkflow,
		OperationTypeTrackedItemCreate, OperationTypeTrackedItemUpdate, OperationTypeTrackedItemStageState,
		OperationTypeTrackedItemArchive, OperationTypeTrackedItemDependencyAdd, OperationTypeTrackedItemDependencyRemove,
		OperationTypePlannerItemCreate, OperationTypeWorkItemSetLifecycle, OperationTypeWorkflowCreate,
		OperationTypeWorkflowSetCurrentStep, OperationTypeMonitorPointCreate, OperationTypeMonitorPointSetState,
		OperationTypePlannerItemSetLocalNote:
		return true
	default:
		return false
	}
}
