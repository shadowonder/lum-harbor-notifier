package app

import (
	"time"

	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/temporal"
)

// PlannerItemDTO is the frontend-facing representation of a Planner Item.
//
// The DTO exposes only information required by the Planner UI. Persistence
// bookkeeping such as last_revision_id and created_at intentionally remains
// inside the backend.
//
// Exact timestamps and date-only values remain separate:
//
//   - ScheduledAt / DueAt represent exact instants.
//   - ScheduledDate / DueDate represent calendar dates without a timezone.
//
// A date-only value must never be converted into an artificial midnight
// timestamp merely for transport to the frontend.
type PlannerItemDTO struct {
	ID        string  `json:"id"`
	Type      string  `json:"type"`
	Title     string  `json:"title"`
	Details   *string `json:"details"`
	Status    string  `json:"status"`
	Authority string  `json:"authority"`

	// Exact timestamp scheduling values.
	//
	// These values are serialized as RFC3339 timestamps and represent
	// real instants in time.
	ScheduledAt *string `json:"scheduledAt"`
	DueAt       *string `json:"dueAt"`

	// Date-only scheduling values.
	//
	// These values remain YYYY-MM-DD strings and intentionally contain
	// no time-of-day or timezone semantics.
	ScheduledDate *string `json:"scheduledDate"`
	DueDate       *string `json:"dueDate"`

	// CompletedAt is an exact instant because completion is an action that
	// occurs at a specific point in time.
	CompletedAt *string `json:"completedAt"`

	ContextStatus  string   `json:"contextStatus"`
	MissingContext []string `json:"missingContext"`

	WorkItemID        *string                  `json:"workItemId,omitempty"`
	WorkflowID        *string                  `json:"workflowId,omitempty"`
	WorkflowStepID    *string                  `json:"workflowStepId,omitempty"`
	WorkItemTitle     *string                  `json:"workItemTitle,omitempty"`
	WorkflowName      *string                  `json:"workflowName,omitempty"`
	WorkflowStepTitle *string                  `json:"workflowStepTitle,omitempty"`
	TrackedItemID     *string                  `json:"trackedItemId,omitempty"`
	TrackedItemTitle  *string                  `json:"trackedItemTitle,omitempty"`
	Reminder          *TodoReminderDTO         `json:"reminder,omitempty"`
	RecentChanges     []PlannerRecentChangeDTO `json:"recentChanges,omitempty"`

	// LocalNote is Lum Harbor-owned context. For an EXTERNAL item it sits
	// beside, and never replaces, the external system's own Details.
	LocalNote *string `json:"localNote"`
	// ExternalOrigin is read-only provenance for an EXTERNAL item; nil for
	// LOCAL items or when the origin cannot be resolved.
	ExternalOrigin *PlannerExternalOriginDTO `json:"externalOrigin,omitempty"`
}

// PlannerExternalOriginDTO names which external system owns an EXTERNAL
// Planner Item and which Plugin provided it. SystemLabel comes from the
// external identity namespace; ProviderName/ExtensionID come from the
// SourceRecord that produced it and the Plugin manifest.
type PlannerExternalOriginDTO struct {
	SystemLabel  string  `json:"systemLabel"`
	ProviderName *string `json:"providerName,omitempty"`
	ExtensionID  *string `json:"extensionId,omitempty"`
	InstanceID   *string `json:"instanceId,omitempty"`
}

type TodoReminderDTO struct {
	ID             string `json:"id"`
	PlannerItemID  string `json:"plannerItemId"`
	State          string `json:"state"`
	OffsetsSeconds []int  `json:"offsetsSeconds"`
	FiredOffsets   []int  `json:"firedOffsets"`
	RemindMode     string `json:"remindMode"`
}

type PlannerRecentChangeDTO struct {
	EventID    string `json:"eventId"`
	EventType  string `json:"eventType"`
	Summary    string `json:"summary"`
	OccurredAt string `json:"occurredAt"`
	Actor      string `json:"actor"`
}

type CreateLocalTodoRequestDTO struct {
	Title          string  `json:"title"`
	Details        *string `json:"details,omitempty"`
	DueDate        *string `json:"dueDate,omitempty"`
	DueAt          *string `json:"dueAt,omitempty"`
	WorkItemID     *string `json:"workItemId,omitempty"`
	WorkflowID     *string `json:"workflowId,omitempty"`
	WorkflowStepID *string `json:"workflowStepId,omitempty"`
	TrackedItemID  *string `json:"trackedItemId,omitempty"`
	OffsetsSeconds []int   `json:"offsetsSeconds,omitempty"`
}

type SetTodoReminderRequestDTO struct {
	PlannerItemID  string `json:"plannerItemId"`
	OffsetsSeconds []int  `json:"offsetsSeconds"`
}

// plannerItemToDTO converts Planner domain state into the stable transport
// model exposed through Wails.
//
// Domain timestamps are serialized explicitly rather than relying on Wails
// or JSON reflection to choose a time representation. This keeps the frontend
// contract predictable.
//
// LocalDate values remain YYYY-MM-DD strings so the transport layer preserves
// the semantic distinction between a calendar date and an exact timestamp.
func plannerItemToDTO(
	item planner.PlannerItem,
) PlannerItemDTO {

	return PlannerItemDTO{
		ID:        item.ID,
		Type:      string(item.Type),
		Title:     item.Title,
		Details:   item.Details,
		Status:    string(item.Status),
		Authority: string(item.Authority),

		ScheduledAt: formatDTOTime(item.ScheduledAt),
		DueAt:       formatDTOTime(item.DueAt),

		ScheduledDate: formatDTODate(item.ScheduledDate),
		DueDate:       formatDTODate(item.DueDate),

		CompletedAt: formatDTOTime(item.CompletedAt),

		ContextStatus:  string(item.ContextStatus),
		MissingContext: item.MissingContext,
		WorkItemID:     item.WorkItemID,
		WorkflowID:     item.WorkflowID,
		WorkflowStepID: item.WorkflowStepID,
		TrackedItemID:  item.RelatedEntityID,
		LocalNote:      item.LocalNote,
	}
}

// plannerItemsToDTO converts a PlannerItem collection into its frontend
// representation while preserving the collection's existing ordering.
//
// Projection membership is decided before this conversion. This function
// performs transport mapping only and must not filter, reorder, or otherwise
// reinterpret Planner domain state.
func plannerItemsToDTO(
	items []planner.PlannerItem,
) []PlannerItemDTO {

	result := make([]PlannerItemDTO, 0, len(items))

	for _, item := range items {
		result = append(
			result,
			plannerItemToDTO(item),
		)
	}

	return result
}

// formatDTOTime converts an optional domain timestamp into the RFC3339Nano
// representation used by the Wails transport contract.
//
// Timestamps are normalized to UTC before transport so the exact instant is
// unambiguous. The frontend may later display that instant in the user's
// selected timezone.
func formatDTOTime(value *time.Time) *string {
	if value == nil {
		return nil
	}

	formatted := value.UTC().Format(time.RFC3339Nano)
	return &formatted
}

// formatDTODate converts an optional LocalDate into the YYYY-MM-DD transport
// representation expected by the frontend.
//
// No timezone conversion is performed because LocalDate deliberately has no
// timezone or time-of-day semantics.
func formatDTODate(
	value *temporal.LocalDate,
) *string {

	if value == nil {
		return nil
	}

	formatted := string(*value)
	return &formatted
}
