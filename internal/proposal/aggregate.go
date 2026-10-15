package proposal

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"lum-harbor-notifier/internal/identity"
)

// MaxAggregateOperations is the V1 product / human-review / model-runaway
// limit on child operations in one aggregate Proposal. It is not an
// architectural maximum.
const MaxAggregateOperations = 20

// ErrInvalidAggregatePlan indicates an aggregate operation plan failed
// preflight before anything was persisted.
var ErrInvalidAggregatePlan = errors.New("invalid aggregate proposal plan")

// buildMode tells a typed operation builder whether it is preparing a
// singular Proposal or an aggregate child. Only an aggregate child may carry
// symbolic references, and only in its declared reference fields.
type buildMode int

const (
	buildSingular buildMode = iota
	buildAggregateChild
)

func (m buildMode) isReference(value *string) bool {
	return m == buildAggregateChild && value != nil && IsReference(*value)
}

// OperationInput is the closed set of typed singular operation intents: the
// Propose*Input types. The application capability registry resolves raw
// capability arguments into one of these; the Service then either persists it
// as a singular Proposal (Propose) or prepares it as an aggregate child
// (PrepareOperation) through the same builder.
type OperationInput interface {
	build(ctx context.Context, s *Service, mode buildMode) (*Proposal, Producer, error)
	propose(ctx context.Context, s *Service) (*Proposal, error)
}

func (in ProposeWorkItemUpdateInput) build(ctx context.Context, s *Service, mode buildMode) (*Proposal, Producer, error) {
	return s.buildWorkItemUpdate(ctx, in, mode)
}

// The WorkItem update keeps its checked single-transaction creation path.
func (in ProposeWorkItemUpdateInput) propose(ctx context.Context, s *Service) (*Proposal, error) {
	return s.ProposeWorkItemUpdate(ctx, in)
}

func (in ProposeTrackedItemOperationInput) build(ctx context.Context, s *Service, mode buildMode) (*Proposal, Producer, error) {
	return s.buildTrackedItemOperation(ctx, in, mode)
}

func (in ProposeTrackedItemOperationInput) propose(ctx context.Context, s *Service) (*Proposal, error) {
	return s.proposeWith(ctx, in)
}

func (in ProposePlannerItemCreateInput) build(ctx context.Context, s *Service, mode buildMode) (*Proposal, Producer, error) {
	return s.buildPlannerItemCreate(ctx, in, mode)
}

func (in ProposePlannerItemCreateInput) propose(ctx context.Context, s *Service) (*Proposal, error) {
	return s.proposeWith(ctx, in)
}

func (in ProposeWorkItemLifecycleInput) build(ctx context.Context, s *Service, mode buildMode) (*Proposal, Producer, error) {
	return s.buildWorkItemLifecycle(ctx, in, mode)
}

func (in ProposeWorkItemLifecycleInput) propose(ctx context.Context, s *Service) (*Proposal, error) {
	return s.proposeWith(ctx, in)
}

func (in ProposeWorkflowCreateInput) build(ctx context.Context, s *Service, mode buildMode) (*Proposal, Producer, error) {
	return s.buildWorkflowCreate(ctx, in, mode)
}

func (in ProposeWorkflowCreateInput) propose(ctx context.Context, s *Service) (*Proposal, error) {
	return s.proposeWith(ctx, in)
}

func (in ProposeWorkItemCreateInput) build(ctx context.Context, s *Service, mode buildMode) (*Proposal, Producer, error) {
	return s.buildWorkItemCreate(ctx, in, mode)
}

func (in ProposeWorkItemCreateInput) propose(ctx context.Context, s *Service) (*Proposal, error) {
	return s.proposeWith(ctx, in)
}

func (in ProposeWorkItemCreateWithWorkflowInput) build(ctx context.Context, s *Service, mode buildMode) (*Proposal, Producer, error) {
	return s.buildWorkItemCreateWithWorkflow(ctx, in, mode)
}

func (in ProposeWorkItemCreateWithWorkflowInput) propose(ctx context.Context, s *Service) (*Proposal, error) {
	return s.proposeWith(ctx, in)
}

func (in ProposeMonitorPointCreateInput) build(ctx context.Context, s *Service, mode buildMode) (*Proposal, Producer, error) {
	return s.buildMonitorPointCreate(ctx, in, mode)
}

func (in ProposeMonitorPointCreateInput) propose(ctx context.Context, s *Service) (*Proposal, error) {
	return s.proposeWith(ctx, in)
}

func (in ProposeMonitorPointSetStateInput) build(ctx context.Context, s *Service, mode buildMode) (*Proposal, Producer, error) {
	return s.buildMonitorPointSetState(ctx, in, mode)
}

func (in ProposeMonitorPointSetStateInput) propose(ctx context.Context, s *Service) (*Proposal, error) {
	return s.proposeWith(ctx, in)
}

func (in ProposeWorkflowSetCurrentStepInput) build(ctx context.Context, s *Service, mode buildMode) (*Proposal, Producer, error) {
	return s.buildWorkflowSetCurrentStep(ctx, in, mode)
}

func (in ProposeWorkflowSetCurrentStepInput) propose(ctx context.Context, s *Service) (*Proposal, error) {
	return s.proposeWith(ctx, in)
}

// Propose creates one singular Proposal from a typed intent. It is exactly the
// matching Propose* method; aggregate persistence is never involved.
func (s *Service) Propose(ctx context.Context, input OperationInput) (*Proposal, error) {
	return input.propose(ctx, s)
}

// proposeWith builds a singular Proposal and persists it with its creation
// History in one transaction.
func (s *Service) proposeWith(ctx context.Context, input OperationInput) (*Proposal, error) {
	proposal, producer, err := input.build(ctx, s, buildSingular)
	if err != nil {
		return nil, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := NewRepository(tx).Create(ctx, proposal); err != nil {
		return nil, err
	}
	if err := recordProposalCreated(ctx, tx, proposal, producer); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return proposal, nil
}

// --- Symbolic references ---------------------------------------------------

// OutputKind names one entity ID a child operation produces on Apply that a
// later child may reference.
type OutputKind string

const (
	OutputWorkItemID    OutputKind = "work_item_id"
	OutputWorkflowID    OutputKind = "workflow_id"
	OutputTrackedItemID OutputKind = "tracked_item_id"
	OutputPlannerItemID OutputKind = "planner_item_id"
)

// operationOutputs declares, per operation type, the outputs its Apply
// produces. Operations not listed produce no referenceable output.
var operationOutputs = map[string][]OutputKind{
	OperationTypeWorkItemCreate:             {OutputWorkItemID},
	OperationTypeWorkItemCreateWithWorkflow: {OutputWorkItemID, OutputWorkflowID},
	OperationTypeWorkflowCreate:             {OutputWorkflowID},
	OperationTypeTrackedItemCreate:          {OutputTrackedItemID},
	OperationTypePlannerItemCreate:          {OutputPlannerItemID},
}

func operationProduces(operationType string, kind OutputKind) bool {
	for _, produced := range operationOutputs[operationType] {
		if produced == kind {
			return true
		}
	}
	return false
}

// Reference is one parsed symbolic reference "$op[<index>].<output>" to an
// output of an earlier child of the same aggregate Proposal.
type Reference struct {
	Index  int
	Output OutputKind
}

func (r Reference) String() string { return fmt.Sprintf("$op[%d].%s", r.Index, r.Output) }

var referencePattern = regexp.MustCompile(`^\$op\[(0|[1-9][0-9]?)\]\.([a-z_]+)$`)

// IsReference reports whether a value claims to be a symbolic reference. It is
// consulted only for fields declared to accept references; every other field
// is literal data and is never inspected.
func IsReference(value string) bool { return strings.HasPrefix(value, "$op") }

// ParseReference parses the whole value as one reference. There is no
// expression, interpolation, or nesting syntax.
func ParseReference(value string) (Reference, error) {
	match := referencePattern.FindStringSubmatch(value)
	if match == nil {
		return Reference{}, fmt.Errorf("%w: malformed symbolic reference %q", ErrInvalidAggregatePlan, value)
	}
	index, err := strconv.Atoi(match[1])
	if err != nil {
		return Reference{}, fmt.Errorf("%w: malformed symbolic reference %q", ErrInvalidAggregatePlan, value)
	}
	output := OutputKind(match[2])
	switch output {
	case OutputWorkItemID, OutputWorkflowID, OutputTrackedItemID, OutputPlannerItemID:
	default:
		return Reference{}, fmt.Errorf("%w: unknown reference output %q", ErrInvalidAggregatePlan, match[2])
	}
	return Reference{Index: index, Output: output}, nil
}

// referenceField is one typed payload field declared to accept a reference.
type referenceField struct {
	name    string
	accepts OutputKind
	slot    **string
}

// referenceFields is the single declaration of which typed payload fields may
// hold a symbolic reference, and to which output kind. Preflight validates and
// Apply resolves exactly these fields and nothing else.
func referenceFields(op *Proposal) []referenceField {
	switch op.OperationType {
	case OperationTypeTrackedItemCreate:
		if p := op.TrackingPayload; p != nil {
			return []referenceField{
				{name: "work_item_id", accepts: OutputWorkItemID, slot: &p.WorkItemID},
				{name: "workflow_id", accepts: OutputWorkflowID, slot: &p.WorkflowID},
			}
		}
	case OperationTypePlannerItemCreate:
		if p := op.PlannerItemPayload; p != nil {
			return []referenceField{
				{name: "work_item_id", accepts: OutputWorkItemID, slot: &p.WorkItemID},
				{name: "workflow_id", accepts: OutputWorkflowID, slot: &p.WorkflowID},
				{name: "tracked_item_id", accepts: OutputTrackedItemID, slot: &p.TrackedItemID},
			}
		}
	}
	return nil
}

// fieldReference returns the parsed reference held by a declared field, or
// nil when the field is absent or concrete.
func fieldReference(field referenceField) (*Reference, error) {
	if *field.slot == nil || !IsReference(**field.slot) {
		return nil, nil
	}
	ref, err := ParseReference(**field.slot)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", field.name, err)
	}
	return &ref, nil
}

// companionReference derives the reference to another output of the same
// earlier child, e.g. the Workflow created together with a referenced WorkItem.
func companionReference(value string, output OutputKind) (string, error) {
	ref, err := ParseReference(value)
	if err != nil {
		return "", err
	}
	return Reference{Index: ref.Index, Output: output}.String(), nil
}

// --- Preparation and preflight ---------------------------------------------

// PreparedOperation is one validated typed operation ready to become a child
// of an aggregate Proposal. It is produced only by Service.PrepareOperation,
// so every concrete ID in it passed the same checks as singular creation.
type PreparedOperation struct {
	operationType  string
	targetEntityID *string
	payloadJSON    string
	title          string
}

func (p PreparedOperation) OperationType() string   { return p.operationType }
func (p PreparedOperation) TargetEntityID() *string { return p.targetEntityID }
func (p PreparedOperation) PayloadJSON() string     { return p.payloadJSON }

// Title is the singular Proposal title this operation would carry; preview
// metadata only, not persisted on the child.
func (p PreparedOperation) Title() string { return p.title }

// PrepareOperation validates and normalizes one typed intent as an aggregate
// child through the same builder singular creation uses. Symbolic references
// are admitted only in declared reference fields. It reads current truth but
// opens no transaction, persists nothing, and mutates nothing.
func (s *Service) PrepareOperation(ctx context.Context, input OperationInput) (PreparedOperation, error) {
	draft, _, err := input.build(ctx, s, buildAggregateChild)
	if err != nil {
		return PreparedOperation{}, err
	}
	payloadJSON, err := marshalProposalPayload(draft)
	if err != nil {
		return PreparedOperation{}, err
	}
	return PreparedOperation{
		operationType:  draft.OperationType,
		targetEntityID: draft.TargetEntityID,
		payloadJSON:    payloadJSON,
		title:          draft.Title,
	}, nil
}

// PreflightAggregatePlan validates a whole ordered plan without touching the
// database: operation count, closed non-aggregate operation types, typed
// payloads, reference syntax/direction/output type, derivable intra-plan
// relations, and intra-plan state conflicts that would make Apply stale. It
// returns the plan with derived companion references filled in. It never
// replaces authoritative Apply-time revalidation.
func PreflightAggregatePlan(operations []PreparedOperation) ([]PreparedOperation, error) {
	if len(operations) == 0 || len(operations) > MaxAggregateOperations {
		return nil, fmt.Errorf("%w: %d operations, expected 1 to %d", ErrInvalidAggregatePlan, len(operations), MaxAggregateOperations)
	}
	plan := make([]*Proposal, 0, len(operations))
	for index, operation := range operations {
		op, err := decodeOperation("", operation.operationType, operation.targetEntityID, operation.payloadJSON)
		if err != nil {
			return nil, planError(index, operation.operationType, err)
		}
		if err := validateReferences(op, index, plan); err != nil {
			return nil, planError(index, op.OperationType, err)
		}
		if err := deriveCompanions(op, plan); err != nil {
			return nil, planError(index, op.OperationType, err)
		}
		if err := validateReferences(op, index, plan); err != nil {
			return nil, planError(index, op.OperationType, err)
		}
		plan = append(plan, op)
	}
	if err := checkStateConflicts(plan); err != nil {
		return nil, err
	}
	prepared := make([]PreparedOperation, len(plan))
	for index, op := range plan {
		payloadJSON, err := marshalProposalPayload(op)
		if err != nil {
			return nil, fmt.Errorf("%w: operation %d: %v", ErrInvalidAggregatePlan, index, err)
		}
		prepared[index] = PreparedOperation{
			operationType:  op.OperationType,
			targetEntityID: op.TargetEntityID,
			payloadJSON:    payloadJSON,
			title:          operations[index].title,
		}
	}
	return prepared, nil
}

// planError attributes a preflight failure to one operation, wrapping
// ErrInvalidAggregatePlan exactly once.
func planError(index int, operationType string, err error) error {
	if errors.Is(err, ErrInvalidAggregatePlan) {
		return fmt.Errorf("operation %d (%s): %w", index, operationType, err)
	}
	return fmt.Errorf("%w: operation %d (%s): %v", ErrInvalidAggregatePlan, index, operationType, err)
}

// validateReferences checks every declared reference field of op at position
// index: whole-value syntax, strictly backward direction, the field's accepted
// output kind, and that the referenced earlier operation declares that output.
func validateReferences(op *Proposal, index int, earlier []*Proposal) error {
	for _, field := range referenceFields(op) {
		ref, err := fieldReference(field)
		if err != nil {
			return err
		}
		if ref == nil {
			continue
		}
		if ref.Index >= index {
			return fmt.Errorf("%s: reference %s must point to an earlier operation", field.name, ref)
		}
		if ref.Output != field.accepts {
			return fmt.Errorf("%s: reference %s does not supply a %s", field.name, ref, field.accepts)
		}
		if !operationProduces(earlier[ref.Index].OperationType, ref.Output) {
			return fmt.Errorf("%s: operation %d (%s) does not produce %s", field.name, ref.Index, earlier[ref.Index].OperationType, ref.Output)
		}
	}
	if p := op.TrackingPayload; op.OperationType == OperationTypeTrackedItemCreate && p != nil && IsReference(value(p.WorkItemID)) {
		expected, err := companionReference(value(p.WorkItemID), OutputWorkflowID)
		if err != nil {
			return err
		}
		if value(p.WorkflowID) != expected {
			return fmt.Errorf("workflow_id must be %s for a work item created in this proposal", expected)
		}
	}
	return nil
}

// deriveCompanions makes a Planner Todo linked to a TrackedItem created earlier
// in the plan carry that TrackedItem's WorkItem and Workflow provenance, the
// same derivation singular creation performs from an existing TrackedItem.
func deriveCompanions(op *Proposal, earlier []*Proposal) error {
	p := op.PlannerItemPayload
	if op.OperationType != OperationTypePlannerItemCreate || p == nil || !IsReference(value(p.TrackedItemID)) {
		return nil
	}
	ref, err := ParseReference(*p.TrackedItemID)
	if err != nil || ref.Index >= len(earlier) || earlier[ref.Index].TrackingPayload == nil {
		return nil // validateReferences reports the precise problem
	}
	tracked := earlier[ref.Index].TrackingPayload
	if p.WorkItemID == nil {
		p.WorkItemID = cloneString(tracked.WorkItemID)
	} else if value(p.WorkItemID) != value(tracked.WorkItemID) {
		return fmt.Errorf("tracked item from operation %d belongs to work item %s", ref.Index, value(tracked.WorkItemID))
	}
	if p.WorkflowID == nil {
		p.WorkflowID = cloneString(tracked.WorkflowID)
	} else if value(p.WorkflowID) != value(tracked.WorkflowID) {
		return fmt.Errorf("tracked item from operation %d belongs to workflow %s", ref.Index, value(tracked.WorkflowID))
	}
	return nil
}

// stateFact is one aspect of existing authoritative truth an operation either
// checks at Apply (its stale precondition) or changes.
type stateFact struct {
	entityID string
	facet    string
}

const (
	facetRevision  = "revision"
	facetLifecycle = "lifecycle"
	facetWorkflow  = "workflow"
	facetLocalNote = "local_note"
)

// operationStateFacts mirrors the per-operation Apply precondition checks and
// the existing-entity changes each Apply makes. Creations of new entities and
// references to them involve no prior state and contribute nothing.
func operationStateFacts(op *Proposal) (checks, changes []stateFact) {
	target := value(op.TargetEntityID)
	fact := func(id, facet string) stateFact { return stateFact{entityID: id, facet: facet} }
	switch op.OperationType {
	case OperationTypeWorkItemUpdateDetails:
		return []stateFact{fact(target, facetRevision)}, []stateFact{fact(target, facetRevision)}
	case OperationTypeWorkItemSetLifecycle:
		return []stateFact{fact(target, facetLifecycle)}, []stateFact{fact(target, facetRevision), fact(target, facetLifecycle)}
	case OperationTypeWorkflowCreate:
		checks = []stateFact{fact(target, facetWorkflow)}
		if op.WorkflowCreatePayload != nil && op.WorkflowCreatePayload.ExpectedLastRevisionID != nil {
			checks = append(checks, fact(target, facetRevision))
		}
		return checks, []stateFact{fact(target, facetWorkflow)}
	case OperationTypeWorkflowSetCurrentStep:
		if p := op.WorkflowSetCurrentStepPayload; p != nil {
			return []stateFact{fact(p.WorkflowID, facetRevision)}, []stateFact{fact(p.WorkflowID, facetRevision)}
		}
	case OperationTypeTrackedItemUpdate, OperationTypeTrackedItemStageState, OperationTypeTrackedItemArchive:
		return []stateFact{fact(target, facetRevision)}, []stateFact{fact(target, facetRevision)}
	case OperationTypeTrackedItemDependencyAdd, OperationTypeTrackedItemDependencyRemove:
		return []stateFact{fact(target, facetRevision)}, nil
	case OperationTypeMonitorPointCreate:
		if p := op.MonitorPointCreatePayload; p != nil {
			if p.ExpectedWorkItemRevisionID != nil {
				checks = append(checks, fact(p.WorkItemID, facetRevision))
			}
			if p.SubjectTrackedItemID != nil && p.ExpectedTrackedItemRevisionID != nil {
				checks = append(checks, fact(*p.SubjectTrackedItemID, facetRevision))
			}
		}
		return checks, nil
	case OperationTypeMonitorPointSetState:
		return []stateFact{fact(target, facetRevision)}, []stateFact{fact(target, facetRevision)}
	case OperationTypePlannerItemSetLocalNote:
		// The stale guard is the local note itself, not the item revision.
		return []stateFact{fact(target, facetLocalNote)}, []stateFact{fact(target, facetRevision), fact(target, facetLocalNote)}
	}
	return nil, nil
}

// checkStateConflicts rejects a plan in which an operation's Apply
// precondition depends on existing truth an earlier operation of the same plan
// changes; such a plan could only ever become STALE.
func checkStateConflicts(plan []*Proposal) error {
	changedBy := map[stateFact]int{}
	for index, op := range plan {
		checks, changes := operationStateFacts(op)
		for _, check := range checks {
			if earlier, ok := changedBy[check]; ok && check.entityID != "" {
				return fmt.Errorf("%w: operation %d (%s) depends on the current %s of %s, which operation %d changes; propose them separately",
					ErrInvalidAggregatePlan, index, op.OperationType, check.facet, check.entityID, earlier)
			}
		}
		for _, change := range changes {
			if _, ok := changedBy[change]; !ok {
				changedBy[change] = index
			}
		}
	}
	return nil
}

// --- Aggregate creation -----------------------------------------------------

// CreateAggregateProposalInput is the parent-level intent of one aggregate
// Proposal: review metadata, provenance, and its prepared ordered operations.
type CreateAggregateProposalInput struct {
	Title             string
	Summary           *string
	Reason            *string
	AIRunID           *string
	SourceRevisionIDs []string
	Producer          Producer
	Operations        []PreparedOperation
}

// CreateAggregateProposal preflights the plan and persists one pending parent
// Proposal, its ordered children, its parent-level Source links, and its
// creation History in one transaction. It never mutates domain truth.
func (s *Service) CreateAggregateProposal(ctx context.Context, input CreateAggregateProposalInput) (*Proposal, error) {
	title := strings.TrimSpace(input.Title)
	if title == "" {
		return nil, fmt.Errorf("%w: aggregate proposal title must not be blank", ErrInvalidInput)
	}
	producer := input.Producer
	if producer == "" {
		producer = ProducerUser
	}
	if !producer.valid() {
		return nil, fmt.Errorf("%w: unsupported Proposal producer %q", ErrInvalidInput, input.Producer)
	}
	operations, err := PreflightAggregatePlan(input.Operations)
	if err != nil {
		return nil, err
	}
	proposalID, err := identity.New()
	if err != nil {
		return nil, fmt.Errorf("generate proposal ID: %w", err)
	}
	children := make([]ProposalOperation, 0, len(operations))
	for index, operation := range operations {
		childID, err := identity.New()
		if err != nil {
			return nil, fmt.Errorf("generate proposal operation ID: %w", err)
		}
		children = append(children, ProposalOperation{
			ID: childID, ProposalID: proposalID, SequenceNo: index,
			OperationType: operation.operationType, TargetEntityID: operation.targetEntityID, PayloadJSON: operation.payloadJSON,
		})
	}
	now := time.Now().UTC()
	proposal := &Proposal{
		ID:                proposalID,
		OperationType:     OperationTypeAggregate,
		Title:             title,
		Summary:           input.Summary,
		Reason:            input.Reason,
		Status:            StatusPending,
		AIRunID:           input.AIRunID,
		SourceRevisionIDs: input.SourceRevisionIDs,
		Operations:        children,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin create aggregate proposal transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := NewRepository(tx).Create(ctx, proposal); err != nil {
		return nil, err
	}
	if err := recordProposalCreated(ctx, tx, proposal, producer); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit create aggregate proposal transaction: %w", err)
	}
	return proposal, nil
}

func cloneString(input *string) *string {
	if input == nil {
		return nil
	}
	copied := *input
	return &copied
}
