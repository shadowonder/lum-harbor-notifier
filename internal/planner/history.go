package planner

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"lum-harbor-notifier/internal/history"
)

type plannerCreatedSnapshot struct {
	Version       int     `json:"version"`
	ItemType      string  `json:"item_type"`
	Status        string  `json:"status"`
	Title         string  `json:"title"`
	WorkItemID    *string `json:"work_item_id,omitempty"`
	TrackedItemID *string `json:"tracked_item_id,omitempty"`
	ScheduledAt   *string `json:"scheduled_at,omitempty"`
	DueAt         *string `json:"due_at,omitempty"`
	ScheduledDate *string `json:"scheduled_date,omitempty"`
	DueDate       *string `json:"due_date,omitempty"`
	// CompletedAt is always nil for a LOCAL creation (only EXTERNAL creation
	// can arrive already completed, e.g. a Reminder first observed after the
	// user completed it outside Lum Harbor); omitempty keeps LOCAL creation
	// event payloads byte-identical to before this field existed.
	CompletedAt *string `json:"completed_at,omitempty"`
}

// plannerExternalUpdateSnapshot is the before/after shape for a deterministic
// external-sync refresh of an already-existing EXTERNAL Planner Item. It is
// deliberately separate from plannerCreatedSnapshot: item_type never changes
// after creation, so it has no place here.
type plannerExternalUpdateSnapshot struct {
	Version     int     `json:"version"`
	Title       string  `json:"title"`
	Details     *string `json:"details,omitempty"`
	DueAt       *string `json:"due_at,omitempty"`
	DueDate     *string `json:"due_date,omitempty"`
	Status      string  `json:"status"`
	CompletedAt *string `json:"completed_at,omitempty"`
}

type plannerCompletionSnapshot struct {
	Version     int     `json:"version"`
	Status      string  `json:"status"`
	CompletedAt *string `json:"completed_at"`
}

func recordPlannerCreated(ctx context.Context, tx *sql.Tx, item *PlannerItem, sourceRevisionID *string) error {
	after, err := history.Snapshot(plannerCreatedSnapshot{
		Version: 1, ItemType: string(item.Type), Status: string(item.Status), Title: item.Title,
		WorkItemID: item.WorkItemID, TrackedItemID: item.RelatedEntityID,
		ScheduledAt: plannerHistoryTime(item.ScheduledAt), DueAt: plannerHistoryTime(item.DueAt),
		ScheduledDate: plannerHistoryDate(item.ScheduledDate), DueDate: plannerHistoryDate(item.DueDate),
	})
	if err != nil {
		return err
	}
	subjectID, revisionID := item.ID, item.LastRevisionID
	var causationEventID *string
	if sourceRevisionID != nil && strings.TrimSpace(*sourceRevisionID) != "" {
		events, err := history.NewRepository(tx).ListByCorrelation(ctx, history.SourceRevisionCorrelation(*sourceRevisionID), 1)
		if err == nil && len(events) > 0 {
			causationEventID = &events[0].ID
		}
	}
	_, err = history.NewRepository(tx).Append(ctx, history.Draft{
		Type: history.EventPlannerItemCreated, Scope: history.ScopePlanner, Actor: history.ActorUser,
		Summary: "Planner item created", SubjectEntityID: &subjectID, WorkItemID: item.WorkItemID, RevisionID: &revisionID,
		SourceRevisionID: sourceRevisionID, CausationEventID: causationEventID,
		CorrelationID: history.RevisionCorrelation(revisionID), AfterJSON: after, OccurredAt: item.CreatedAt,
	})
	return err
}

// recordPlannerExternalCreated appends the same PLANNER_ITEM_CREATED event
// type LOCAL creation uses, but with actor SOURCE (an external system, not a
// user, produced this item) and an optional Source evidence link. Reusing the
// event type is deliberate (M13 Slice 2A design): creation is creation
// regardless of authority, and Actor already distinguishes who did it.
func recordPlannerExternalCreated(ctx context.Context, tx *sql.Tx, item *PlannerItem, sourceRevisionID *string) error {
	after, err := history.Snapshot(plannerCreatedSnapshot{
		Version: 1, ItemType: string(item.Type), Status: string(item.Status), Title: item.Title,
		DueAt: plannerHistoryTime(item.DueAt), DueDate: plannerHistoryDate(item.DueDate),
		CompletedAt: plannerHistoryTime(item.CompletedAt),
	})
	if err != nil {
		return err
	}
	subjectID, revisionID := item.ID, item.LastRevisionID
	var causationEventID *string
	if sourceRevisionID != nil && strings.TrimSpace(*sourceRevisionID) != "" {
		events, err := history.NewRepository(tx).ListByCorrelation(ctx, history.SourceRevisionCorrelation(*sourceRevisionID), 1)
		if err == nil && len(events) > 0 {
			causationEventID = &events[0].ID
		}
	}
	_, err = history.NewRepository(tx).Append(ctx, history.Draft{
		Type: history.EventPlannerItemCreated, Scope: history.ScopePlanner, Actor: history.ActorSource,
		Summary: "External planner item created", SubjectEntityID: &subjectID, WorkItemID: item.WorkItemID, RevisionID: &revisionID,
		SourceRevisionID: sourceRevisionID, CausationEventID: causationEventID,
		CorrelationID: history.RevisionCorrelation(revisionID),
		AfterJSON:     after, OccurredAt: item.CreatedAt,
	})
	return err
}

// recordPlannerExternalUpdated appends exactly one PLANNER_ITEM_EXTERNAL_UPDATED
// event per sync refresh, regardless of how many native fields changed (title,
// due shape, and status/completion can all change together and still produce
// one Revision and one History event — never one event per field).
func recordPlannerExternalUpdated(ctx context.Context, tx *sql.Tx, before, after *PlannerItem, sourceRevisionID *string) error {
	beforeJSON, err := history.Snapshot(plannerExternalUpdateSnapshot{
		Version: 1, Title: before.Title, Details: before.Details,
		DueAt: plannerHistoryTime(before.DueAt), DueDate: plannerHistoryDate(before.DueDate),
		Status: string(before.Status), CompletedAt: plannerHistoryTime(before.CompletedAt),
	})
	if err != nil {
		return err
	}
	afterJSON, err := history.Snapshot(plannerExternalUpdateSnapshot{
		Version: 1, Title: after.Title, Details: after.Details,
		DueAt: plannerHistoryTime(after.DueAt), DueDate: plannerHistoryDate(after.DueDate),
		Status: string(after.Status), CompletedAt: plannerHistoryTime(after.CompletedAt),
	})
	if err != nil {
		return err
	}
	subjectID, revisionID := after.ID, after.LastRevisionID
	var causationEventID *string
	if sourceRevisionID != nil && strings.TrimSpace(*sourceRevisionID) != "" {
		events, err := history.NewRepository(tx).ListByCorrelation(ctx, history.SourceRevisionCorrelation(*sourceRevisionID), 1)
		if err == nil && len(events) > 0 {
			causationEventID = &events[0].ID
		}
	}
	_, err = history.NewRepository(tx).Append(ctx, history.Draft{
		Type: history.EventPlannerItemExternalUpdated, Scope: history.ScopePlanner, Actor: history.ActorSource,
		Summary: "External planner item synced", SubjectEntityID: &subjectID, WorkItemID: after.WorkItemID, RevisionID: &revisionID,
		SourceRevisionID: sourceRevisionID, CausationEventID: causationEventID,
		CorrelationID: history.RevisionCorrelation(revisionID),
		BeforeJSON:    beforeJSON, AfterJSON: afterJSON, OccurredAt: after.UpdatedAt,
	})
	return err
}

func recordPlannerCompleted(ctx context.Context, tx *sql.Tx, before, after *PlannerItem) error {
	beforeJSON, err := history.Snapshot(plannerCompletionSnapshot{Version: 1, Status: string(before.Status), CompletedAt: plannerHistoryTime(before.CompletedAt)})
	if err != nil {
		return err
	}
	afterJSON, err := history.Snapshot(plannerCompletionSnapshot{Version: 1, Status: string(after.Status), CompletedAt: plannerHistoryTime(after.CompletedAt)})
	if err != nil {
		return err
	}
	subjectID, revisionID := after.ID, after.LastRevisionID
	_, err = history.NewRepository(tx).Append(ctx, history.Draft{
		Type: history.EventPlannerItemCompleted, Scope: history.ScopePlanner, Actor: history.ActorUser,
		Summary: "Planner item completed", SubjectEntityID: &subjectID, WorkItemID: after.WorkItemID, RevisionID: &revisionID,
		CorrelationID: history.RevisionCorrelation(revisionID), BeforeJSON: beforeJSON, AfterJSON: afterJSON, OccurredAt: after.UpdatedAt,
	})
	return err
}

func plannerHistoryTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339Nano)
	return &formatted
}

func plannerHistoryDate[T ~string](value *T) *string {
	if value == nil {
		return nil
	}
	formatted := string(*value)
	return &formatted
}
