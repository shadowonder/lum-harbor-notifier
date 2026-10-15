// Package history owns immutable, durable business-history records.
//
// History explains committed facts; it is never a source of current domain
// truth, an event-sourcing log, a replay mechanism, or an undo mechanism.
package history

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidEvent = errors.New("invalid history event")
	ErrNotFound     = errors.New("history event not found")
)

type EventType string

const (
	EventMonitorCreated               EventType = "MONITOR_CREATED"
	EventMonitorStateChanged          EventType = "MONITOR_STATE_CHANGED"
	EventAttentionOpened              EventType = "ATTENTION_OPENED"
	EventAttentionResolved            EventType = "ATTENTION_RESOLVED"
	EventAttentionSnoozed             EventType = "ATTENTION_SNOOZED"
	EventNotificationCreated          EventType = "NOTIFICATION_CREATED"
	EventNotificationSent             EventType = "NOTIFICATION_SENT"
	EventNotificationPresented        EventType = "NOTIFICATION_PRESENTED"
	EventNotificationDelivered        EventType = "NOTIFICATION_DELIVERED"
	EventNotificationRead             EventType = "NOTIFICATION_READ"
	EventNotificationCancelled        EventType = "NOTIFICATION_CANCELLED"
	EventProposalCreated              EventType = "PROPOSAL_CREATED"
	EventProposalApplied              EventType = "PROPOSAL_APPLIED"
	EventProposalRejected             EventType = "PROPOSAL_REJECTED"
	EventProposalStale                EventType = "PROPOSAL_STALE"
	EventWorkItemCreated              EventType = "WORK_ITEM_CREATED"
	EventWorkItemDetailsChanged       EventType = "WORK_ITEM_DETAILS_CHANGED"
	EventWorkItemLifecycleChanged     EventType = "WORK_ITEM_LIFECYCLE_CHANGED"
	EventTrackedItemCreated           EventType = "TRACKED_ITEM_CREATED"
	EventTrackedItemUpdated           EventType = "TRACKED_ITEM_UPDATED"
	EventTrackedItemStageStateChanged EventType = "TRACKED_ITEM_STAGE_STATE_CHANGED"
	EventWorkflowCreated              EventType = "WORKFLOW_CREATED"
	EventWorkflowCurrentStepChanged   EventType = "WORKFLOW_CURRENT_STEP_CHANGED"
	EventTrackedItemArchived          EventType = "TRACKED_ITEM_ARCHIVED"
	EventTrackedItemDependencyAdded   EventType = "TRACKED_ITEM_DEPENDENCY_ADDED"
	EventTrackedItemDependencyRemoved EventType = "TRACKED_ITEM_DEPENDENCY_REMOVED"
	EventPlannerItemCreated           EventType = "PLANNER_ITEM_CREATED"
	EventPlannerItemCompleted         EventType = "PLANNER_ITEM_COMPLETED"
	// EventPlannerItemExternalUpdated (M13 Slice 2A) records a deterministic
	// external-sync refresh of an already-existing EXTERNAL PlannerItem's
	// native fields (title/details/due/status) that is neither its creation
	// nor a completion transition captured well enough by the other two
	// Planner event types on its own (e.g. a title or due-date change with no
	// status change, or a reopen). Actor is always ActorSource.
	EventPlannerItemExternalUpdated EventType = "PLANNER_ITEM_EXTERNAL_UPDATED"
	// EventPlannerItemLocalNoteChanged records a change to the Lum
	// Harbor-owned local note of a Planner Item (applied through a Proposal).
	// It never describes an external-authoritative field change.
	EventPlannerItemLocalNoteChanged EventType = "PLANNER_ITEM_LOCAL_NOTE_CHANGED"
	EventPlannerItemReminderSet      EventType = "PLANNER_ITEM_REMINDER_SET"
	EventPlannerItemReminderRemoved  EventType = "PLANNER_ITEM_REMINDER_REMOVED"
	EventSourceRevisionObserved      EventType = "SOURCE_REVISION_OBSERVED"
	EventAIRunSucceeded              EventType = "AI_RUN_SUCCEEDED"
	EventAIRunFailed                 EventType = "AI_RUN_FAILED"
)

func (v EventType) Valid() bool {
	switch v {
	case EventMonitorCreated, EventMonitorStateChanged, EventAttentionOpened, EventAttentionResolved,
		EventAttentionSnoozed, EventNotificationCreated, EventNotificationSent,
		EventNotificationPresented, EventNotificationDelivered,
		EventNotificationRead,
		EventNotificationCancelled, EventProposalCreated, EventProposalApplied, EventProposalRejected, EventProposalStale,
		EventWorkItemCreated, EventWorkItemDetailsChanged, EventWorkItemLifecycleChanged,
		EventTrackedItemCreated, EventTrackedItemUpdated, EventTrackedItemStageStateChanged,
		EventWorkflowCreated, EventWorkflowCurrentStepChanged,
		EventTrackedItemArchived, EventTrackedItemDependencyAdded, EventTrackedItemDependencyRemoved,
		EventPlannerItemCreated, EventPlannerItemCompleted, EventPlannerItemExternalUpdated, EventPlannerItemLocalNoteChanged,
		EventPlannerItemReminderSet, EventPlannerItemReminderRemoved,
		EventSourceRevisionObserved,
		EventAIRunSucceeded, EventAIRunFailed:
		return true
	default:
		return false
	}
}

type Scope string

const (
	ScopeTracking     Scope = "TRACKING"
	ScopeAttention    Scope = "ATTENTION"
	ScopeNotification Scope = "NOTIFICATION"
	ScopeProposal     Scope = "PROPOSAL"
	ScopeWorkItem     Scope = "WORK_ITEM"
	ScopePlanner      Scope = "PLANNER"
	ScopeSource       Scope = "SOURCE"
	ScopeAI           Scope = "AI"
)

func (v Scope) Valid() bool {
	return v == ScopeTracking || v == ScopeAttention || v == ScopeNotification || v == ScopeProposal || v == ScopeWorkItem || v == ScopePlanner || v == ScopeSource || v == ScopeAI
}

type ActorType string

const (
	ActorUser   ActorType = "USER"
	ActorSystem ActorType = "SYSTEM"
	ActorRule   ActorType = "RULE"
	ActorAI     ActorType = "AI"
	ActorSource ActorType = "SOURCE"
)

func (v ActorType) Valid() bool {
	return v == ActorUser || v == ActorSystem || v == ActorRule || v == ActorAI || v == ActorSource
}

type Event struct {
	ID               string
	Type             EventType
	Scope            Scope
	Actor            ActorType
	Summary          string
	SubjectEntityID  *string
	WorkItemID       *string
	RevisionID       *string
	ProposalID       *string
	AIRunID          *string
	SourceRevisionID *string
	CorrelationID    string
	CausationEventID *string
	BeforeJSON       *string
	AfterJSON        *string
	OccurredAt       time.Time
	RecordedAt       time.Time
}

// Draft contains the business-owned facts for an event. Append owns event
// identity and recorded_at; the mutation owner supplies occurred_at.
type Draft struct {
	Type             EventType
	Scope            Scope
	Actor            ActorType
	Summary          string
	SubjectEntityID  *string
	WorkItemID       *string
	RevisionID       *string
	ProposalID       *string
	AIRunID          *string
	SourceRevisionID *string
	CorrelationID    string
	CausationEventID *string
	BeforeJSON       *string
	AfterJSON        *string
	OccurredAt       time.Time
}

func MonitorCorrelation(monitorPointID string) string {
	return "monitor:" + strings.TrimSpace(monitorPointID)
}

func PlannerCorrelation(plannerItemID string) string {
	return "planner:" + strings.TrimSpace(plannerItemID)
}

func ProposalCorrelation(proposalID string) string {
	return "proposal:" + strings.TrimSpace(proposalID)
}

func RevisionCorrelation(revisionID string) string {
	return "revision:" + strings.TrimSpace(revisionID)
}

func SourceRevisionCorrelation(sourceRevisionID string) string {
	return "source-revision:" + strings.TrimSpace(sourceRevisionID)
}

func ProcessingCorrelation(processingID string) string {
	return "processing:" + strings.TrimSpace(processingID)
}

// Snapshot serializes one bounded semantic object. Scalars, arrays and null
// are rejected; semantic versioning remains the owning domain's responsibility.
func Snapshot(value any) (*string, error) {
	if value == nil {
		return nil, nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("%w: encode snapshot: %v", ErrInvalidEvent, err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil || object == nil {
		return nil, fmt.Errorf("%w: snapshot must be a JSON object", ErrInvalidEvent)
	}
	encoded := string(raw)
	return &encoded, nil
}

func validateDraft(d Draft) error {
	if !d.Type.Valid() || !d.Scope.Valid() || !d.Actor.Valid() {
		return fmt.Errorf("%w: unsupported type, scope, or actor", ErrInvalidEvent)
	}
	if strings.TrimSpace(d.Summary) == "" || len(d.Summary) > 512 {
		return fmt.Errorf("%w: summary must be nonblank and at most 512 bytes", ErrInvalidEvent)
	}
	if strings.TrimSpace(d.CorrelationID) == "" {
		return fmt.Errorf("%w: correlation ID is required", ErrInvalidEvent)
	}
	if d.OccurredAt.IsZero() {
		return fmt.Errorf("%w: occurred_at is required", ErrInvalidEvent)
	}
	for name, raw := range map[string]*string{"before": d.BeforeJSON, "after": d.AfterJSON} {
		if raw == nil {
			continue
		}
		var object map[string]json.RawMessage
		if err := json.Unmarshal([]byte(*raw), &object); err != nil || object == nil {
			return fmt.Errorf("%w: %s snapshot must be a valid JSON object", ErrInvalidEvent, name)
		}
	}
	return nil
}

// CausalChain represents a connected causal lineage of History events,
// from upstream root cause to downstream consequences.
type CausalChain struct {
	TargetEvent Event
	RootEvent   *Event
	Events      []Event
}
