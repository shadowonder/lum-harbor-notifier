// Package tracking owns Tracking Core V1 authoritative truth: WorkflowDefinition,
// WorkflowStep, TrackedItem and TrackedItemStepState.
//
// Contract highlights (see docs/contracts/tracking-core-contract-v1.md):
//   - A TrackedItem's position is never persisted; it is derived
//     (ComputeProgression). The overall Workflow position (workflows.current_step_id)
//     and workflows.started_at ARE persisted, independent of any TrackedItem, and
//     TrackedItem changes never mutate them.
//   - WorkflowDefinitions are immutable once created; a changed process is a new
//     (name, version).
//   - Tracking commands are human-explicit direct commands. Nothing here is
//     reachable from the AI Assistant.
package tracking

import (
	"errors"
	"time"
)

var (
	ErrInvalidInput          = errors.New("invalid tracking input")
	ErrTrackedItemNotFound   = errors.New("tracked item not found")
	ErrWorkflowNotFound      = errors.New("workflow definition not found")
	ErrWorkflowInactive      = errors.New("workflow definition is not active")
	ErrWorkItemUnavailable   = errors.New("work item unavailable")
	ErrInvalidParent         = errors.New("invalid parent tracked item")
	ErrNoWorkflow            = errors.New("tracked item has no workflow")
	ErrWorkflowAlreadyExists = errors.New("work item already has a workflow")
	ErrStepNotInWorkflow     = errors.New("workflow step does not belong to the tracked item's workflow")
	ErrInconsistentState     = errors.New("tracked item step states are inconsistent with its workflow")
	ErrRelationExists        = errors.New("tracked item dependency already exists")
	ErrRelationNotFound      = errors.New("tracked item dependency not found")
	ErrDependencyCycle       = errors.New("tracked item dependency would create a cycle")
)

// StepState is the semantic workflow step state vocabulary. It is independent
// of WorkItem and TrackedItem lifecycle.
type StepState string

const (
	StepNotStarted StepState = "NOT_STARTED"
	StepInProgress StepState = "IN_PROGRESS"
	StepCompleted  StepState = "COMPLETED"
	StepWaiting    StepState = "WAITING"
	StepBlocked    StepState = "BLOCKED"
	StepFailed     StepState = "FAILED"
	StepUnknown    StepState = "UNKNOWN"
	StepSkipped    StepState = "SKIPPED"
)

// Valid reports whether the state belongs to the V1 vocabulary.
func (s StepState) Valid() bool {
	switch s {
	case StepNotStarted, StepInProgress, StepCompleted, StepWaiting,
		StepBlocked, StepFailed, StepUnknown, StepSkipped:
		return true
	default:
		return false
	}
}

// ItemLifecycle is the TrackedItem lifecycle. Archival is Entity-level.
type ItemLifecycle string

const (
	ItemActive    ItemLifecycle = "ACTIVE"
	ItemCompleted ItemLifecycle = "COMPLETED"
	ItemCancelled ItemLifecycle = "CANCELLED"
)

func (l ItemLifecycle) Valid() bool {
	return l == ItemActive || l == ItemCompleted || l == ItemCancelled
}

// Workflow is the single process instance owned by at most one WorkItem (0..1).
// It uses exactly one WorkflowDefinition.
//
// CurrentStepID is the Workflow's own authoritative position: a real step of
// its WorkflowDefinition (enforced by a composite foreign key). It is never
// inferred from TrackedItems and may be moved backward by an explicit
// mutation. StartedAt is the actual Workflow start (defaults to CreatedAt).
type Workflow struct {
	ID                   string
	WorkItemID           string
	WorkflowDefinitionID string
	CurrentStepID        string
	StartedAt            time.Time
	LastRevisionID       string
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// WorkflowStep is one immutable, ordered step of a WorkflowDefinition.
type WorkflowStep struct {
	ID                   string
	WorkflowDefinitionID string
	Key                  string
	Title                string
	Sequence             int
	Terminal             bool
}

// WorkflowDefinition is a reusable, versioned, immutable process definition.
type WorkflowDefinition struct {
	ID        string
	Name      string
	Version   int
	Active    bool
	CreatedAt time.Time
	Steps     []WorkflowStep // ordered by Sequence
}

// StepByID resolves a step by ID from the definition's ordered steps.
func (d WorkflowDefinition) StepByID(id string) (WorkflowStep, bool) {
	for _, step := range d.Steps {
		if step.ID == id {
			return step, true
		}
	}
	return WorkflowStep{}, false
}

// WorkflowView binds the Workflow instance to its authoritative WorkflowDefinition.
type WorkflowView struct {
	Workflow   Workflow
	Definition WorkflowDefinition
}

// CurrentStep resolves the Workflow's persisted current step from its
// definition. It never inspects TrackedItems.
func (v WorkflowView) CurrentStep() (WorkflowStep, bool) {
	for _, step := range v.Definition.Steps {
		if step.ID == v.Workflow.CurrentStepID {
			return step, true
		}
	}
	return WorkflowStep{}, false
}

// TrackedItem is one tracked unit inside a WorkItem and its single Workflow.
// There is intentionally no current-step field: current stage position is
// deterministically derived from step states.
type TrackedItem struct {
	ID             string
	WorkItemID     string
	WorkflowID     string
	ParentItemID   *string
	Title          string
	ItemType       *string
	Lifecycle      ItemLifecycle
	CompletedAt    *time.Time
	LastRevisionID string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// StepStateRecord is the authoritative state of one workflow step for one
// TrackedItem.
type StepStateRecord struct {
	TrackedItemID  string
	WorkflowStepID string
	State          StepState
	EnteredAt      *time.Time
	CompletedAt    *time.Time
	LastRevisionID string
}

// RelationType is deliberately bounded. DEPENDS_ON is stored; BLOCKS is the
// inverse read projection and is never persisted as a duplicate row.
type RelationType string

const RelationDependsOn RelationType = "DEPENDS_ON"

type TrackedItemRelation struct {
	WorkflowID        string
	FromTrackedItemID string
	ToTrackedItemID   string
	Type              RelationType
	LastRevisionID    string
	CreatedAt         time.Time
}

// TrackedItemView is the machine-consumable read shape for one TrackedItem:
// authoritative facts plus the derived (never persisted) progression.
type TrackedItemView struct {
	Item        TrackedItem
	StepStates  []StepStateRecord // ordered by workflow step sequence
	Progression *Progression      // nil when the item has no workflow
}

// WorkItemTracking is the bounded, semantic Tracking read model of one
// WorkItem. It contains no presentation concerns.
type WorkItemTracking struct {
	WorkItemID string
	Workflow   *WorkflowView     // nil when the WorkItem has no workflow
	Items      []TrackedItemView // deterministic: creation order, then ID
	Relations  []TrackedItemRelation
	// MonitorPoints are authoritative expectations (any state), ordered by due
	// time, creation time, then ID. No evaluation result is included.
	MonitorPoints []MonitorPoint
}
