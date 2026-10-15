package workitem

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"lum-harbor-notifier/internal/history"
)

type createdHistorySnapshot struct {
	Version   int    `json:"version"`
	Title     string `json:"title"`
	Lifecycle string `json:"lifecycle"`
}

type detailsHistorySnapshot struct {
	Version     int     `json:"version"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
}

type lifecycleHistorySnapshot struct {
	Version     int     `json:"version"`
	Lifecycle   string  `json:"lifecycle"`
	CompletedAt *string `json:"completed_at"`
}

// DetailsHistoryProvenance supplies the upstream Proposal process when the
// details mutation is coordinated by Proposal Apply. Nil fields mean a direct
// WorkItem operation correlated to its own Revision.
type DetailsHistoryProvenance struct {
	ProposalID       *string
	CorrelationID    *string
	CausationEventID *string
}

func recordCreatedHistory(ctx context.Context, tx *sql.Tx, item *WorkItem, sourceRevisionID *string) error {
	after, err := history.Snapshot(createdHistorySnapshot{Version: 1, Title: item.Title, Lifecycle: string(item.Lifecycle)})
	if err != nil {
		return err
	}
	subjectID, workItemID, revisionID := item.ID, item.ID, item.LastRevisionID
	var causationEventID *string
	if sourceRevisionID != nil && strings.TrimSpace(*sourceRevisionID) != "" {
		events, err := history.NewRepository(tx).ListByCorrelation(ctx, history.SourceRevisionCorrelation(*sourceRevisionID), 1)
		if err == nil && len(events) > 0 {
			causationEventID = &events[0].ID
		}
	}
	_, err = history.NewRepository(tx).Append(ctx, history.Draft{
		Type: history.EventWorkItemCreated, Scope: history.ScopeWorkItem, Actor: history.ActorUser,
		Summary: "Work Item created", SubjectEntityID: &subjectID, WorkItemID: &workItemID, RevisionID: &revisionID,
		SourceRevisionID: sourceRevisionID, CausationEventID: causationEventID,
		CorrelationID: history.RevisionCorrelation(revisionID), AfterJSON: after, OccurredAt: item.CreatedAt,
	})
	return err
}

// RecordDetailsChangedHistoryTx records the WorkItem-owned semantic change in
// the caller's transaction. Proposal Apply calls it only after its APPLIED fact
// exists, allowing a real direct causation edge without mutating History later.
func RecordDetailsChangedHistoryTx(ctx context.Context, tx *sql.Tx, before, after *WorkItem, provenance *DetailsHistoryProvenance) error {
	if before == nil || after == nil || before.ID != after.ID || after.LastRevisionID == "" {
		return fmt.Errorf("invalid Work Item details history input")
	}
	beforeJSON, err := history.Snapshot(detailsHistorySnapshot{Version: 1, Title: before.Title, Description: before.Description})
	if err != nil {
		return err
	}
	afterJSON, err := history.Snapshot(detailsHistorySnapshot{Version: 1, Title: after.Title, Description: after.Description})
	if err != nil {
		return err
	}
	correlationID := history.RevisionCorrelation(after.LastRevisionID)
	var proposalID, causeID *string
	if provenance != nil {
		proposalID, causeID = provenance.ProposalID, provenance.CausationEventID
		if provenance.CorrelationID != nil {
			correlationID = *provenance.CorrelationID
		}
	}
	subjectID, workItemID, revisionID := after.ID, after.ID, after.LastRevisionID
	_, err = history.NewRepository(tx).Append(ctx, history.Draft{
		Type: history.EventWorkItemDetailsChanged, Scope: history.ScopeWorkItem, Actor: history.ActorUser,
		Summary: "Work Item title or description changed", SubjectEntityID: &subjectID, WorkItemID: &workItemID,
		RevisionID: &revisionID, ProposalID: proposalID, CorrelationID: correlationID, CausationEventID: causeID,
		BeforeJSON: beforeJSON, AfterJSON: afterJSON, OccurredAt: after.UpdatedAt,
	})
	return err
}

func recordLifecycleChangedHistory(ctx context.Context, tx *sql.Tx, before, after *WorkItem) error {
	beforeJSON, err := history.Snapshot(lifecycleHistorySnapshot{Version: 1, Lifecycle: string(before.Lifecycle), CompletedAt: formatHistoryTime(before.CompletedAt)})
	if err != nil {
		return err
	}
	afterJSON, err := history.Snapshot(lifecycleHistorySnapshot{Version: 1, Lifecycle: string(after.Lifecycle), CompletedAt: formatHistoryTime(after.CompletedAt)})
	if err != nil {
		return err
	}
	subjectID, workItemID, revisionID := after.ID, after.ID, after.LastRevisionID
	_, err = history.NewRepository(tx).Append(ctx, history.Draft{
		Type: history.EventWorkItemLifecycleChanged, Scope: history.ScopeWorkItem, Actor: history.ActorUser,
		Summary: fmt.Sprintf("Work Item lifecycle changed to %s", after.Lifecycle), SubjectEntityID: &subjectID,
		WorkItemID: &workItemID, RevisionID: &revisionID, CorrelationID: history.RevisionCorrelation(revisionID),
		BeforeJSON: beforeJSON, AfterJSON: afterJSON, OccurredAt: after.UpdatedAt,
	})
	return err
}

func formatHistoryTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339Nano)
	return &formatted
}
