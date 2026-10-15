package proposal

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrInvalidOperationPayload indicates an operation payload does not conform
// to the expected versioned operation contract.
var ErrInvalidOperationPayload = errors.New("invalid operation payload")

// Status represents the complete lifecycle vocabulary for a Change Proposal.
type Status string

const (
	StatusPending  Status = "PENDING"
	StatusApplied  Status = "APPLIED"
	StatusRejected Status = "REJECTED"
	StatusStale    Status = "STALE"
)

// Valid reports whether the status is supported by the Proposal lifecycle.
func (s Status) Valid() bool {
	switch s {
	case StatusPending, StatusApplied, StatusRejected, StatusStale:
		return true
	default:
		return false
	}
}

// CanTransitionTo reports whether the lifecycle permits moving from s to target.
// A same-state request is treated as a no-op by Service rather than a state transition.
func (s Status) CanTransitionTo(target Status) bool {
	switch s {
	case StatusPending:
		return target == StatusApplied || target == StatusRejected || target == StatusStale
	default:
		// Terminal states cannot transition to any other status.
		return false
	}
}

// Operation contract constants.
const (
	OperationTypeWorkItemUpdateDetails       = "work_item.update_details"
	OperationTypeWorkItemCreate              = "work_item.create"
	OperationTypeWorkItemCreateWithWorkflow  = "work_item.create_with_workflow"
	OperationTypeTrackedItemCreate           = "tracked_item.create"
	OperationTypeTrackedItemUpdate           = "tracked_item.update"
	OperationTypeTrackedItemStageState       = "tracked_item.stage_state"
	OperationTypeTrackedItemArchive          = "tracked_item.archive"
	OperationTypeTrackedItemDependencyAdd    = "tracked_item.dependency_add"
	OperationTypeTrackedItemDependencyRemove = "tracked_item.dependency_remove"
	OperationTypePlannerItemCreate           = "planner.item.create"
	OperationTypeWorkItemSetLifecycle        = "workitem.set_lifecycle"
	OperationTypeWorkflowCreate              = "tracking.workflow_create"
	OperationTypeWorkflowSetCurrentStep      = "tracking.workflow_set_current_step"
	OperationTypeMonitorPointCreate          = "monitor_point.create"
	OperationTypeMonitorPointSetState        = "monitor_point.set_state"
	OperationVersionV1                       = "v1"
	// OperationTypeAggregate is the compatibility operation_type of an
	// aggregate parent Proposal. It is not a capability: the authoritative
	// typed operations are the parent's ordered Operations.
	OperationTypeAggregate = "proposal.aggregate"
)

// RootCreateOperationTypes is the small, explicit allowlist of operation
// types permitted to have a NULL target_entity_id (see the matching DB CHECK
// constraint in 00001_foundation.sql, which additionally admits the aggregate
// parent OperationTypeAggregate). Every other operation type must resolve a
// real, existing target entity before a Proposal can be created.
var RootCreateOperationTypes = map[string]bool{
	OperationTypePlannerItemCreate:          true,
	OperationTypeWorkItemCreate:             true,
	OperationTypeWorkItemCreateWithWorkflow: true,
}

// WorkItemPreconditions captures authoritative WorkItem state at the moment the
// Proposal was created. This foundation enables stale detection in Slice 2 before
// applying mutations.
type WorkItemPreconditions struct {
	ExpectedLastRevisionID string  `json:"expected_last_revision_id"`
	ExpectedTitle          string  `json:"expected_title"`
	ExpectedDescription    *string `json:"expected_description,omitempty"`
}

// TrackedItemOperationPayload is the closed, versioned family of Tracking
// proposal operations. Fields are interpreted only for the discriminated kind.
// Stable IDs are backend-resolved before proposal creation; no revision or
// timestamp can be supplied by AI.
type TrackedItemOperationPayload struct {
	OperationKind          string  `json:"operation_kind"`
	Version                string  `json:"version"`
	ExpectedLastRevisionID *string `json:"expected_last_revision_id,omitempty"`
	ExpectedTitle          *string `json:"expected_title,omitempty"`
	ExpectedItemType       *string `json:"expected_item_type,omitempty"`
	ExpectedParentItemID   *string `json:"expected_parent_item_id,omitempty"`
	ExpectedParentTitle    *string `json:"expected_parent_title,omitempty"`
	ExpectedWorkflowStep   *string `json:"expected_workflow_step_title,omitempty"`
	ExpectedState          *string `json:"expected_state,omitempty"`
	WorkItemID             *string `json:"work_item_id,omitempty"`
	WorkflowID             *string `json:"workflow_id,omitempty"`
	TrackedItemID          *string `json:"tracked_item_id,omitempty"`
	Title                  *string `json:"title,omitempty"`
	ItemType               *string `json:"item_type,omitempty"`
	ParentItemID           *string `json:"parent_item_id,omitempty"`
	ParentItemTitle        *string `json:"parent_item_title,omitempty"`
	WorkflowStepID         *string `json:"workflow_step_id,omitempty"`
	WorkflowStepTitle      *string `json:"workflow_step_title,omitempty"`
	State                  *string `json:"state,omitempty"`
	ToTrackedItemID        *string `json:"to_tracked_item_id,omitempty"`
	ToTrackedItemTitle     *string `json:"to_tracked_item_title,omitempty"`
}

func (p *TrackedItemOperationPayload) Validate() error {
	if p.Version != OperationVersionV1 {
		return fmt.Errorf("%w: unsupported operation version %q", ErrInvalidOperationPayload, p.Version)
	}
	required := func(value *string) bool { return value != nil && strings.TrimSpace(*value) != "" }
	switch p.OperationKind {
	case OperationTypeTrackedItemCreate:
		if !required(p.WorkItemID) || !required(p.WorkflowID) || !required(p.Title) {
			return fmt.Errorf("%w: create requires work item, workflow and title", ErrInvalidOperationPayload)
		}
	case OperationTypeTrackedItemUpdate:
		if !required(p.TrackedItemID) || !required(p.ExpectedLastRevisionID) || !required(p.Title) {
			return fmt.Errorf("%w: update requires item, base revision and title", ErrInvalidOperationPayload)
		}
	case OperationTypeTrackedItemStageState:
		if !required(p.TrackedItemID) || !required(p.ExpectedLastRevisionID) || !required(p.WorkflowStepID) || !required(p.State) {
			return fmt.Errorf("%w: stage/state requires item, base revision, step and state", ErrInvalidOperationPayload)
		}
	case OperationTypeTrackedItemArchive:
		if !required(p.TrackedItemID) || !required(p.ExpectedLastRevisionID) {
			return fmt.Errorf("%w: archive requires item and base revision", ErrInvalidOperationPayload)
		}
	case OperationTypeTrackedItemDependencyAdd, OperationTypeTrackedItemDependencyRemove:
		if !required(p.TrackedItemID) || !required(p.ExpectedLastRevisionID) || !required(p.ToTrackedItemID) {
			return fmt.Errorf("%w: dependency requires both items and base revision", ErrInvalidOperationPayload)
		}
	default:
		return fmt.Errorf("%w: unsupported Tracking operation %q", ErrInvalidOperationPayload, p.OperationKind)
	}
	return nil
}

func (p *TrackedItemOperationPayload) JSON() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	raw, err := json.Marshal(p)
	return string(raw), err
}

func ParseTrackedItemOperationPayload(data []byte) (*TrackedItemOperationPayload, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var payload TrackedItemOperationPayload
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: malformed Tracking payload: %v", ErrInvalidOperationPayload, err)
	}
	if err := payload.Validate(); err != nil {
		return nil, err
	}
	return &payload, nil
}

// WorkItemUpdateDetailsPayload is the versioned typed operation payload for
// proposing title and description updates to a WorkItem.
// Target identity is maintained by the Proposal itself (target_entity_id) to avoid
// duplicate target truth.
type WorkItemUpdateDetailsPayload struct {
	OperationKind       string                `json:"operation_kind"`
	Version             string                `json:"version"`
	Preconditions       WorkItemPreconditions `json:"preconditions"`
	ProposedTitle       string                `json:"proposed_title"`
	ProposedDescription *string               `json:"proposed_description,omitempty"`
}

// Validate checks the structural integrity, version, and required envelope
// fields of the operation payload.
func (p *WorkItemUpdateDetailsPayload) Validate() error {
	if p.OperationKind != OperationTypeWorkItemUpdateDetails {
		return fmt.Errorf("%w: invalid operation kind %q, expected %q",
			ErrInvalidOperationPayload, p.OperationKind, OperationTypeWorkItemUpdateDetails)
	}
	if p.Version != OperationVersionV1 {
		return fmt.Errorf("%w: unsupported operation version %q, expected %q",
			ErrInvalidOperationPayload, p.Version, OperationVersionV1)
	}
	if strings.TrimSpace(p.Preconditions.ExpectedLastRevisionID) == "" {
		return fmt.Errorf("%w: expected_last_revision_id is required", ErrInvalidOperationPayload)
	}
	return nil
}

// ParseWorkItemUpdateDetailsPayload decodes and validates raw JSON payload bytes.
func ParseWorkItemUpdateDetailsPayload(data []byte) (*WorkItemUpdateDetailsPayload, error) {
	var payload WorkItemUpdateDetailsPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("%w: malformed JSON: %v", ErrInvalidOperationPayload, err)
	}
	if err := payload.Validate(); err != nil {
		return nil, err
	}
	return &payload, nil
}

// JSON marshals the payload into canonical JSON string representation.
func (p *WorkItemUpdateDetailsPayload) JSON() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("marshal operation payload: %w", err)
	}
	return string(b), nil
}

// PlannerItemCreatePayload is the versioned typed operation payload for
// proposing a brand-new root-level Planner item. It is the one operation
// family permitted a NULL target_entity_id: there is no pre-existing entity
// to reference when the item has no WorkItem context, and the Proposal must
// not itself create one before human Apply (Proposal-first boundary).
// WorkItemID is optional: when set (a WorkItem was in context), Apply
// resolves the same non-null target pattern tracked_item.create already
// uses; when nil, target_entity_id is NULL and Apply creates the entity for
// the first time.
type PlannerItemCreatePayload struct {
	OperationKind  string  `json:"operation_kind"`
	Version        string  `json:"version"`
	WorkItemID     *string `json:"work_item_id,omitempty"`
	WorkflowID     *string `json:"workflow_id,omitempty"`
	WorkflowStepID *string `json:"workflow_step_id,omitempty"`
	TrackedItemID  *string `json:"tracked_item_id,omitempty"`
	Title          string  `json:"title"`
	Details        *string `json:"details,omitempty"`
	DueDate        *string `json:"due_date,omitempty"` // YYYY-MM-DD
	DueAt          *string `json:"due_at,omitempty"`   // RFC3339
	OffsetsSeconds []int   `json:"offsets_seconds,omitempty"`
}

func (p *PlannerItemCreatePayload) Validate() error {
	if p.OperationKind != OperationTypePlannerItemCreate {
		return fmt.Errorf("%w: invalid operation kind %q, expected %q", ErrInvalidOperationPayload, p.OperationKind, OperationTypePlannerItemCreate)
	}
	if p.Version != OperationVersionV1 {
		return fmt.Errorf("%w: unsupported operation version %q", ErrInvalidOperationPayload, p.Version)
	}
	if strings.TrimSpace(p.Title) == "" {
		return fmt.Errorf("%w: title must not be blank", ErrInvalidOperationPayload)
	}
	if p.DueDate != nil && p.DueAt != nil {
		return fmt.Errorf("%w: due_date and due_at are mutually exclusive", ErrInvalidOperationPayload)
	}
	if p.TrackedItemID != nil && strings.TrimSpace(*p.TrackedItemID) == "" {
		return fmt.Errorf("%w: tracked_item_id must not be blank", ErrInvalidOperationPayload)
	}
	return nil
}

func (p *PlannerItemCreatePayload) JSON() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(p)
	return string(b), err
}

func ParsePlannerItemCreatePayload(data []byte) (*PlannerItemCreatePayload, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var payload PlannerItemCreatePayload
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: malformed Planner create payload: %v", ErrInvalidOperationPayload, err)
	}
	if err := payload.Validate(); err != nil {
		return nil, err
	}
	return &payload, nil
}

// WorkItemSetLifecyclePayload is the versioned typed operation payload for
// proposing a WorkItem lifecycle transition through the reviewable Proposal
// path (the direct command path remains untouched and Proposal-free).
type WorkItemSetLifecyclePayload struct {
	OperationKind          string  `json:"operation_kind"`
	Version                string  `json:"version"`
	ExpectedLifecycleState string  `json:"expected_lifecycle_state"`
	TargetState            string  `json:"target_state"`
	Reason                 *string `json:"reason,omitempty"`
}

func (p *WorkItemSetLifecyclePayload) Validate() error {
	if p.OperationKind != OperationTypeWorkItemSetLifecycle {
		return fmt.Errorf("%w: invalid operation kind %q, expected %q", ErrInvalidOperationPayload, p.OperationKind, OperationTypeWorkItemSetLifecycle)
	}
	if p.Version != OperationVersionV1 {
		return fmt.Errorf("%w: unsupported operation version %q", ErrInvalidOperationPayload, p.Version)
	}
	if strings.TrimSpace(p.TargetState) == "" {
		return fmt.Errorf("%w: target_state must not be blank", ErrInvalidOperationPayload)
	}
	return nil
}

func (p *WorkItemSetLifecyclePayload) JSON() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(p)
	return string(b), err
}

func ParseWorkItemSetLifecyclePayload(data []byte) (*WorkItemSetLifecyclePayload, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var payload WorkItemSetLifecyclePayload
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: malformed lifecycle payload: %v", ErrInvalidOperationPayload, err)
	}
	if err := payload.Validate(); err != nil {
		return nil, err
	}
	return &payload, nil
}

// WorkflowCreatePayload is the versioned typed operation payload for
// proposing one semantic user action: create/attach a Workflow (with its
// ordered step definition) to a target WorkItem. Definitions are
// non-revisioned configuration; only the Workflow attachment itself produces
// a Revision, exactly mirroring tracking.Service.CreateWorkflow's existing
// non-Proposal shape.
type WorkflowCreatePayload struct {
	OperationKind          string   `json:"operation_kind"`
	Version                string   `json:"version"`
	ExpectedLastRevisionID *string  `json:"expected_last_revision_id,omitempty"`
	DefinitionName         string   `json:"definition_name"`
	Steps                  []string `json:"steps"`
}

func (p *WorkflowCreatePayload) Validate() error {
	if p.OperationKind != OperationTypeWorkflowCreate {
		return fmt.Errorf("%w: invalid operation kind %q, expected %q", ErrInvalidOperationPayload, p.OperationKind, OperationTypeWorkflowCreate)
	}
	if p.Version != OperationVersionV1 {
		return fmt.Errorf("%w: unsupported operation version %q", ErrInvalidOperationPayload, p.Version)
	}
	if strings.TrimSpace(p.DefinitionName) == "" {
		return fmt.Errorf("%w: definition_name must not be blank", ErrInvalidOperationPayload)
	}
	if len(p.Steps) == 0 {
		return fmt.Errorf("%w: steps must not be empty", ErrInvalidOperationPayload)
	}
	seen := map[string]bool{}
	for _, step := range p.Steps {
		trimmed := strings.TrimSpace(step)
		if trimmed == "" {
			return fmt.Errorf("%w: step titles must not be blank", ErrInvalidOperationPayload)
		}
		key := strings.ToLower(trimmed)
		if seen[key] {
			return fmt.Errorf("%w: duplicate step title %q", ErrInvalidOperationPayload, trimmed)
		}
		seen[key] = true
	}
	return nil
}

func (p *WorkflowCreatePayload) JSON() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(p)
	return string(b), err
}

func ParseWorkflowCreatePayload(data []byte) (*WorkflowCreatePayload, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var payload WorkflowCreatePayload
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: malformed workflow create payload: %v", ErrInvalidOperationPayload, err)
	}
	if err := payload.Validate(); err != nil {
		return nil, err
	}
	return &payload, nil
}

// WorkItemCreatePayload is the versioned typed operation payload for
// proposing the creation of a brand-new WorkItem root entity. Like
// planner.item.create, it is permitted a NULL target_entity_id because no
// WorkItem entity exists until human Apply.
type WorkItemCreatePayload struct {
	OperationKind string  `json:"operation_kind"`
	Version       string  `json:"version"`
	Title         string  `json:"title"`
	Description   *string `json:"description,omitempty"`
}

func (p *WorkItemCreatePayload) Validate() error {
	if p.OperationKind != OperationTypeWorkItemCreate {
		return fmt.Errorf("%w: invalid operation kind %q, expected %q", ErrInvalidOperationPayload, p.OperationKind, OperationTypeWorkItemCreate)
	}
	if p.Version != OperationVersionV1 {
		return fmt.Errorf("%w: unsupported operation version %q", ErrInvalidOperationPayload, p.Version)
	}
	if strings.TrimSpace(p.Title) == "" {
		return fmt.Errorf("%w: title must not be blank", ErrInvalidOperationPayload)
	}
	return nil
}

func (p *WorkItemCreatePayload) JSON() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(p)
	return string(b), err
}

func ParseWorkItemCreatePayload(data []byte) (*WorkItemCreatePayload, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var payload WorkItemCreatePayload
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: malformed work item create payload: %v", ErrInvalidOperationPayload, err)
	}
	if err := payload.Validate(); err != nil {
		return nil, err
	}
	return &payload, nil
}

// WorkItemCreateWithWorkflowPayload is the versioned typed operation payload for
// proposing the atomic creation of a new WorkItem together with its Workflow stages.
type WorkItemCreateWithWorkflowPayload struct {
	OperationKind string   `json:"operation_kind"`
	Version       string   `json:"version"`
	Title         string   `json:"title"`
	Description   *string  `json:"description,omitempty"`
	WorkflowName  string   `json:"workflow_name"`
	Stages        []string `json:"stages"`
}

func (p *WorkItemCreateWithWorkflowPayload) Validate() error {
	if p.OperationKind != OperationTypeWorkItemCreateWithWorkflow {
		return fmt.Errorf("%w: invalid operation kind %q, expected %q", ErrInvalidOperationPayload, p.OperationKind, OperationTypeWorkItemCreateWithWorkflow)
	}
	if p.Version != OperationVersionV1 {
		return fmt.Errorf("%w: unsupported operation version %q", ErrInvalidOperationPayload, p.Version)
	}
	if strings.TrimSpace(p.Title) == "" {
		return fmt.Errorf("%w: title must not be blank", ErrInvalidOperationPayload)
	}
	if strings.TrimSpace(p.WorkflowName) == "" {
		return fmt.Errorf("%w: workflow name must not be blank", ErrInvalidOperationPayload)
	}
	if len(p.Stages) == 0 {
		return fmt.Errorf("%w: workflow needs at least one stage", ErrInvalidOperationPayload)
	}
	for i, s := range p.Stages {
		if strings.TrimSpace(s) == "" {
			return fmt.Errorf("%w: stage %d must not be blank", ErrInvalidOperationPayload, i+1)
		}
	}
	return nil
}

func (p *WorkItemCreateWithWorkflowPayload) JSON() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(p)
	return string(b), err
}

func ParseWorkItemCreateWithWorkflowPayload(data []byte) (*WorkItemCreateWithWorkflowPayload, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var payload WorkItemCreateWithWorkflowPayload
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: malformed work item create with workflow payload: %v", ErrInvalidOperationPayload, err)
	}
	if err := payload.Validate(); err != nil {
		return nil, err
	}
	return &payload, nil
}

// MonitorPointCreatePayload is the versioned typed operation payload for
// proposing the creation of an authoritative MonitorPoint expectation.
type MonitorPointCreatePayload struct {
	OperationKind                 string  `json:"operation_kind"`
	Version                       string  `json:"version"`
	WorkItemID                    string  `json:"work_item_id"`
	ExpectedWorkItemRevisionID    *string `json:"expected_work_item_revision_id,omitempty"`
	SubjectTrackedItemID          *string `json:"subject_tracked_item_id,omitempty"`
	SubjectTrackedItemTitle       *string `json:"subject_tracked_item_title,omitempty"`
	ExpectedTrackedItemRevisionID *string `json:"expected_tracked_item_revision_id,omitempty"`
	Kind                          string  `json:"kind"`
	Title                         string  `json:"title"`
	DueAt                         string  `json:"due_at"`   // RFC3339 in UTC
	Timezone                      string  `json:"timezone"` // IANA timezone string
	ExpectedWorkItemLifecycle     *string `json:"expected_work_item_lifecycle,omitempty"`
	ExpectedStepID                *string `json:"expected_step_id,omitempty"`
	ExpectedStepTitle             *string `json:"expected_step_title,omitempty"`
}

func (p *MonitorPointCreatePayload) Validate() error {
	if p.OperationKind != OperationTypeMonitorPointCreate {
		return fmt.Errorf("%w: invalid operation kind %q, expected %q", ErrInvalidOperationPayload, p.OperationKind, OperationTypeMonitorPointCreate)
	}
	if p.Version != OperationVersionV1 {
		return fmt.Errorf("%w: unsupported operation version %q", ErrInvalidOperationPayload, p.Version)
	}
	if strings.TrimSpace(p.WorkItemID) == "" {
		return fmt.Errorf("%w: work_item_id must not be blank", ErrInvalidOperationPayload)
	}
	if strings.TrimSpace(p.Title) == "" {
		return fmt.Errorf("%w: title must not be blank", ErrInvalidOperationPayload)
	}
	if strings.TrimSpace(p.Kind) == "" {
		return fmt.Errorf("%w: kind must not be blank", ErrInvalidOperationPayload)
	}
	if strings.TrimSpace(p.DueAt) == "" {
		return fmt.Errorf("%w: due_at must not be blank", ErrInvalidOperationPayload)
	}
	if _, err := time.Parse(time.RFC3339, p.DueAt); err != nil {
		return fmt.Errorf("%w: invalid due_at timestamp %q: %v", ErrInvalidOperationPayload, p.DueAt, err)
	}
	if strings.TrimSpace(p.Timezone) == "" {
		return fmt.Errorf("%w: timezone must not be blank", ErrInvalidOperationPayload)
	}
	if p.SubjectTrackedItemID == nil {
		if p.ExpectedWorkItemLifecycle == nil || p.ExpectedStepID != nil {
			return fmt.Errorf("%w: a work item subject requires expected_work_item_lifecycle and no expected_step_id", ErrInvalidOperationPayload)
		}
	} else {
		if p.ExpectedStepID == nil || p.ExpectedWorkItemLifecycle != nil {
			return fmt.Errorf("%w: a tracked item subject requires expected_step_id and no expected_work_item_lifecycle", ErrInvalidOperationPayload)
		}
	}
	return nil
}

func (p *MonitorPointCreatePayload) JSON() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(p)
	return string(b), err
}

func ParseMonitorPointCreatePayload(data []byte) (*MonitorPointCreatePayload, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var payload MonitorPointCreatePayload
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: malformed monitor point create payload: %v", ErrInvalidOperationPayload, err)
	}
	if err := payload.Validate(); err != nil {
		return nil, err
	}
	return &payload, nil
}

// MonitorPointSetStatePayload is the versioned typed operation payload for
// proposing a MonitorPoint state transition (ACTIVE -> SATISFIED / CANCELLED).
type MonitorPointSetStatePayload struct {
	OperationKind          string `json:"operation_kind"`
	Version                string `json:"version"`
	MonitorPointID         string `json:"monitor_point_id"`
	WorkItemID             string `json:"work_item_id"`
	MonitorPointTitle      string `json:"monitor_point_title"`
	ExpectedLastRevisionID string `json:"expected_last_revision_id"`
	ExpectedState          string `json:"expected_state"`
	TargetState            string `json:"target_state"`
}

func (p *MonitorPointSetStatePayload) Validate() error {
	if p.OperationKind != OperationTypeMonitorPointSetState {
		return fmt.Errorf("%w: invalid operation kind %q, expected %q", ErrInvalidOperationPayload, p.OperationKind, OperationTypeMonitorPointSetState)
	}
	if p.Version != OperationVersionV1 {
		return fmt.Errorf("%w: unsupported operation version %q", ErrInvalidOperationPayload, p.Version)
	}
	if strings.TrimSpace(p.MonitorPointID) == "" {
		return fmt.Errorf("%w: monitor_point_id must not be blank", ErrInvalidOperationPayload)
	}
	if strings.TrimSpace(p.WorkItemID) == "" {
		return fmt.Errorf("%w: work_item_id must not be blank", ErrInvalidOperationPayload)
	}
	if strings.TrimSpace(p.ExpectedLastRevisionID) == "" {
		return fmt.Errorf("%w: expected_last_revision_id must not be blank", ErrInvalidOperationPayload)
	}
	if p.ExpectedState != "ACTIVE" {
		return fmt.Errorf("%w: expected_state must be ACTIVE", ErrInvalidOperationPayload)
	}
	if p.TargetState != "SATISFIED" && p.TargetState != "CANCELLED" {
		return fmt.Errorf("%w: target_state must be SATISFIED or CANCELLED, got %q", ErrInvalidOperationPayload, p.TargetState)
	}
	return nil
}

func (p *MonitorPointSetStatePayload) JSON() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(p)
	return string(b), err
}

func ParseMonitorPointSetStatePayload(data []byte) (*MonitorPointSetStatePayload, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var payload MonitorPointSetStatePayload
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: malformed monitor point set state payload: %v", ErrInvalidOperationPayload, err)
	}
	if err := payload.Validate(); err != nil {
		return nil, err
	}
	return &payload, nil
}

// WorkflowSetCurrentStepPayload is the versioned typed operation payload for
// proposing moving a Workflow instance's own current step.
type WorkflowSetCurrentStepPayload struct {
	OperationKind              string  `json:"operation_kind"`
	Version                    string  `json:"version"`
	WorkItemID                 string  `json:"work_item_id"`
	WorkflowID                 string  `json:"workflow_id"`
	TargetStepID               string  `json:"target_step_id"`
	TargetStepTitle            string  `json:"target_step_title"`
	ExpectedWorkflowRevisionID string  `json:"expected_workflow_revision_id"`
	ExpectedCurrentStepID      string  `json:"expected_current_step_id"`
	ExpectedCurrentStepTitle   *string `json:"expected_current_step_title,omitempty"`
}

func (p *WorkflowSetCurrentStepPayload) Validate() error {
	if p.OperationKind != OperationTypeWorkflowSetCurrentStep {
		return fmt.Errorf("%w: invalid operation kind %q, expected %q", ErrInvalidOperationPayload, p.OperationKind, OperationTypeWorkflowSetCurrentStep)
	}
	if p.Version != OperationVersionV1 {
		return fmt.Errorf("%w: unsupported operation version %q", ErrInvalidOperationPayload, p.Version)
	}
	if strings.TrimSpace(p.WorkItemID) == "" {
		return fmt.Errorf("%w: work_item_id must not be blank", ErrInvalidOperationPayload)
	}
	if strings.TrimSpace(p.WorkflowID) == "" {
		return fmt.Errorf("%w: workflow_id must not be blank", ErrInvalidOperationPayload)
	}
	if strings.TrimSpace(p.TargetStepID) == "" {
		return fmt.Errorf("%w: target_step_id must not be blank", ErrInvalidOperationPayload)
	}
	if strings.TrimSpace(p.ExpectedWorkflowRevisionID) == "" {
		return fmt.Errorf("%w: expected_workflow_revision_id must not be blank", ErrInvalidOperationPayload)
	}
	if strings.TrimSpace(p.ExpectedCurrentStepID) == "" {
		return fmt.Errorf("%w: expected_current_step_id must not be blank", ErrInvalidOperationPayload)
	}
	return nil
}

func (p *WorkflowSetCurrentStepPayload) JSON() (string, error) {
	if err := p.Validate(); err != nil {
		return "", err
	}
	b, err := json.Marshal(p)
	return string(b), err
}

func ParseWorkflowSetCurrentStepPayload(data []byte) (*WorkflowSetCurrentStepPayload, error) {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var payload WorkflowSetCurrentStepPayload
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("%w: malformed workflow set current step payload: %v", ErrInvalidOperationPayload, err)
	}
	if err := payload.Validate(); err != nil {
		return nil, err
	}
	return &payload, nil
}

// Proposal is Lum Harbor's authoritative representation of a suggested change
// targeting domain state. Suggested Change != Current Truth.
type Proposal struct {
	ID string
	// TargetEntityID is nil only for a pending root-create Proposal (see
	// RootCreateOperationTypes) - never merely omitted for convenience.
	TargetEntityID                    *string
	OperationType                     string
	Title                             string
	Summary                           *string
	Reason                            *string
	Status                            Status
	Payload                           WorkItemUpdateDetailsPayload
	TrackingPayload                   *TrackedItemOperationPayload
	PlannerItemPayload                *PlannerItemCreatePayload
	WorkItemLifecyclePayload          *WorkItemSetLifecyclePayload
	WorkflowCreatePayload             *WorkflowCreatePayload
	WorkItemCreatePayload             *WorkItemCreatePayload
	WorkItemCreateWithWorkflowPayload *WorkItemCreateWithWorkflowPayload
	MonitorPointCreatePayload         *MonitorPointCreatePayload
	MonitorPointSetStatePayload       *MonitorPointSetStatePayload
	WorkflowSetCurrentStepPayload     *WorkflowSetCurrentStepPayload
	PlannerLocalNotePayload           *PlannerItemSetLocalNotePayload
	// AppliedRevisionID is the single applied Revision of a singular Proposal.
	// For an aggregate it is only a compatibility primary pointer; the complete
	// result set is the Results of its Operations.
	AppliedRevisionID *string
	AIRunID           *string
	SourceRevisionIDs []string
	// Operations is empty for a legacy singular Proposal and holds the ordered
	// typed child operations (by SequenceNo) of an aggregate Proposal.
	Operations []ProposalOperation
	CreatedAt  time.Time
	UpdatedAt  time.Time
	ResolvedAt *time.Time
}

// SchedulesTodoReminder reports whether applying the Proposal creates a
// Planner Todo with reminder thresholds, as a singular Proposal or as any
// aggregate child, so the caller can wake reminder evaluation after commit.
func (p *Proposal) SchedulesTodoReminder() bool {
	if p.OperationType == OperationTypePlannerItemCreate && p.PlannerItemPayload != nil && len(p.PlannerItemPayload.OffsetsSeconds) > 0 {
		return true
	}
	for _, child := range p.Operations {
		if child.OperationType != OperationTypePlannerItemCreate {
			continue
		}
		if payload, err := ParsePlannerItemCreatePayload([]byte(child.PayloadJSON)); err == nil && len(payload.OffsetsSeconds) > 0 {
			return true
		}
	}
	return false
}

// IsAggregate reports whether the Proposal is an aggregate parent. Presence of
// child operations, not a mode column, distinguishes it from a singular one.
func (p *Proposal) IsAggregate() bool { return len(p.Operations) > 0 }

// ProposalOperation is one ordered typed child operation of an aggregate
// Proposal. OperationType is one closed singular Proposal operation type and
// PayloadJSON its versioned payload, persisted verbatim; typed preparation and
// validation of children belong to the capability registry, not persistence.
type ProposalOperation struct {
	ID         string
	ProposalID string
	SequenceNo int
	// OperationType is never OperationTypeAggregate: aggregates do not nest.
	OperationType string
	// TargetEntityID is a concrete existing Entity, or nil for a root create or
	// a target produced by an earlier child of the same Proposal.
	TargetEntityID *string
	PayloadJSON    string
	// Results is empty until the parent Proposal is applied.
	Results []ProposalOperationResult
}

// TypedOperation decodes the child into a transient singular operation that
// carries its typed payload, through the same decoder preflight and Apply use.
// It is read-only: references stay unresolved and nothing is persisted.
func (op ProposalOperation) TypedOperation() (*Proposal, error) {
	return decodeOperation(op.ProposalID, op.OperationType, op.TargetEntityID, op.PayloadJSON)
}

// ResultRole marks which result row represents its child operation.
type ResultRole string

const (
	ResultRolePrimary   ResultRole = "PRIMARY"
	ResultRoleSecondary ResultRole = "SECONDARY"
)

// Valid reports whether the role is supported.
func (r ResultRole) Valid() bool { return r == ResultRolePrimary || r == ResultRoleSecondary }

// ProposalOperationResult names one Foundation Revision a child operation
// committed and, when that Revision created or changed a Foundation Entity,
// which Entity. A Revision is a mutation batch rather than an entity version,
// so EntityID is optional (a Workflow has no Entity) and one Revision may appear
// in several rows (a Todo and its Reminder). The domain type of EntityID is
// entities.entity_type; it is deliberately not repeated here.
type ProposalOperationResult struct {
	ProposalOperationID string
	SequenceNo          int
	EntityID            *string
	RevisionID          string
	Role                ResultRole
}

// aggregateCompatibilityPayload is the minimal operation_payload_json of an
// aggregate parent. It only satisfies the singular column's discriminator
// convention and never carries child operations.
type aggregateCompatibilityPayload struct {
	OperationKind string `json:"operation_kind"`
	Version       string `json:"version"`
}

func aggregateCompatibilityPayloadJSON() (string, error) {
	b, err := json.Marshal(aggregateCompatibilityPayload{OperationKind: OperationTypeAggregate, Version: OperationVersionV1})
	return string(b), err
}

func parseAggregateCompatibilityPayload(data []byte) error {
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var payload aggregateCompatibilityPayload
	if err := decoder.Decode(&payload); err != nil {
		return fmt.Errorf("%w: malformed aggregate payload: %v", ErrInvalidOperationPayload, err)
	}
	if payload.OperationKind != OperationTypeAggregate {
		return fmt.Errorf("%w: invalid operation kind %q, expected %q", ErrInvalidOperationPayload, payload.OperationKind, OperationTypeAggregate)
	}
	if payload.Version != OperationVersionV1 {
		return fmt.Errorf("%w: unsupported operation version %q", ErrInvalidOperationPayload, payload.Version)
	}
	return nil
}
