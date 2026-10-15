package attention

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"lum-harbor-notifier/internal/database/codec"
	dbgen "lum-harbor-notifier/internal/database/generated"
)

// Repository persists Attention/Notification state through its executor. It
// owns no transaction, Revision or business rule.
type Repository struct {
	queries *dbgen.Queries
}

func NewRepository(db dbgen.DBTX) *Repository { return &Repository{queries: dbgen.New(db)} }

func (r *Repository) createIssue(ctx context.Context, a *AttentionIssue) error {
	if err := r.queries.CreateAttentionIssue(ctx, dbgen.CreateAttentionIssueParams{
		EntityID: a.ID, WorkItemID: a.WorkItemID, SubjectTrackedItemID: a.SubjectTrackedItemID,
		MonitorPointID: a.MonitorPointID, PlannerItemID: a.PlannerItemID, IssueType: string(a.Type), Severity: string(a.Severity), State: string(a.State),
		Reason: a.Reason, ReasonJson: a.ReasonJSON, RecommendedAction: a.RecommendedAction,
		OpenedAt: codec.FormatTime(a.OpenedAt), ResolvedAt: codec.FormatOptionalTime(a.ResolvedAt),
		SnoozedUntil: codec.FormatOptionalTime(a.SnoozedUntil), DedupeKey: a.DedupeKey,
		LastRevisionID: a.LastRevisionID, UpdatedAt: codec.FormatTime(a.UpdatedAt),
	}); err != nil {
		return fmt.Errorf("create attention issue %s: %w", a.ID, err)
	}
	return nil
}

func (r *Repository) getIssue(ctx context.Context, id string) (*AttentionIssue, error) {
	row, err := r.queries.GetAttentionIssueByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrIssueNotFound, id)
		}
		return nil, fmt.Errorf("load attention issue %s: %w", id, err)
	}
	return issueFromDB(row)
}

func (r *Repository) listOpenIssues(ctx context.Context) ([]AttentionIssue, error) {
	rows, err := r.queries.ListOpenAttentionIssues(ctx)
	if err != nil {
		return nil, fmt.Errorf("list open attention issues: %w", err)
	}
	return issuesFromDB(rows)
}

func (r *Repository) listOpenIssuesByPlannerItem(ctx context.Context, plannerItemID string) ([]AttentionIssue, error) {
	rows, err := r.queries.ListOpenAttentionIssuesByPlannerItem(ctx, &plannerItemID)
	if err != nil {
		return nil, fmt.Errorf("list open attention issues for planner item %s: %w", plannerItemID, err)
	}
	result := make([]AttentionIssue, 0, len(rows))
	for _, row := range rows {
		issue, err := issueFromDB(dbgen.AttentionIssue(row))
		if err != nil {
			return nil, err
		}
		result = append(result, *issue)
	}
	return result, nil
}

// ListIssues returns every current AttentionIssue, newest first.
func (r *Repository) ListIssues(ctx context.Context) ([]AttentionIssue, error) {
	rows, err := r.queries.ListAttentionIssues(ctx)
	if err != nil {
		return nil, fmt.Errorf("list attention issues: %w", err)
	}
	return issuesFromDB(rows)
}

func (r *Repository) resolveIssue(ctx context.Context, id, revisionID string, at time.Time) error {
	if err := r.queries.ResolveAttentionIssue(ctx, dbgen.ResolveAttentionIssueParams{
		ResolvedAt: codec.FormatOptionalTime(&at), LastRevisionID: revisionID, UpdatedAt: codec.FormatTime(at), EntityID: id,
	}); err != nil {
		return fmt.Errorf("resolve attention issue %s: %w", id, err)
	}
	return nil
}

func (r *Repository) resolveByPlannerItem(ctx context.Context, plannerItemID, revisionID string, at time.Time) error {
	if err := r.queries.ResolveAttentionIssuesByPlannerItem(ctx, dbgen.ResolveAttentionIssuesByPlannerItemParams{
		ResolvedAt: codec.FormatOptionalTime(&at), LastRevisionID: revisionID, UpdatedAt: codec.FormatTime(at), PlannerItemID: &plannerItemID,
	}); err != nil {
		return fmt.Errorf("resolve attention issues for planner item %s: %w", plannerItemID, err)
	}
	return nil
}

func (r *Repository) snoozeIssue(ctx context.Context, id string, until time.Time, revisionID string, at time.Time) error {
	if err := r.queries.SnoozeAttentionIssue(ctx, dbgen.SnoozeAttentionIssueParams{
		SnoozedUntil: codec.FormatOptionalTime(&until), LastRevisionID: revisionID, UpdatedAt: codec.FormatTime(at), EntityID: id,
	}); err != nil {
		return fmt.Errorf("snooze attention issue %s: %w", id, err)
	}
	return nil
}

func (r *Repository) updateIssueReason(ctx context.Context, id, reason, reasonJSON, revisionID string, at time.Time) error {
	if err := r.queries.UpdateAttentionIssueReason(ctx, dbgen.UpdateAttentionIssueReasonParams{
		EntityID:       id,
		Reason:         reason,
		ReasonJson:     reasonJSON,
		LastRevisionID: revisionID,
		UpdatedAt:      codec.FormatTime(at),
	}); err != nil {
		return fmt.Errorf("update attention issue reason %s: %w", id, err)
	}
	return nil
}

func (r *Repository) createNotification(ctx context.Context, n *Notification) error {
	if err := r.queries.CreateNotification(ctx, dbgen.CreateNotificationParams{
		EntityID: n.ID, AttentionIssueID: n.AttentionIssueID, WorkItemID: n.WorkItemID,
		SubjectTrackedItemID: n.SubjectTrackedItemID, PlannerItemID: n.PlannerItemID, Title: n.Title, Message: n.Message,
		ReasonSnapshotJson: n.ReasonSnapshotJSON, State: string(n.State), Channel: string(n.Channel),
		SentAt: codec.FormatOptionalTime(n.SentAt), PresentedAt: codec.FormatOptionalTime(n.PresentedAt), ReadAt: codec.FormatOptionalTime(n.ReadAt),
		DismissedAt: codec.FormatOptionalTime(n.DismissedAt), DedupeKey: n.DedupeKey,
		LastRevisionID: n.LastRevisionID, CreatedAt: codec.FormatTime(n.CreatedAt), UpdatedAt: codec.FormatTime(n.UpdatedAt),
	}); err != nil {
		return fmt.Errorf("create notification %s: %w", n.ID, err)
	}
	return nil
}

func (r *Repository) getNotification(ctx context.Context, id string) (*Notification, error) {
	row, err := r.queries.GetNotificationByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %s", ErrNotificationMissing, id)
		}
		return nil, fmt.Errorf("load notification %s: %w", id, err)
	}
	return notificationFromDB(row)
}

// ListNotifications returns every current Notification, newest first.
func (r *Repository) ListNotifications(ctx context.Context) ([]Notification, error) {
	rows, err := r.queries.ListNotifications(ctx)
	if err != nil {
		return nil, fmt.Errorf("list notifications: %w", err)
	}
	result := make([]Notification, 0, len(rows))
	for _, row := range rows {
		n, err := notificationFromDB(row)
		if err != nil {
			return nil, err
		}
		result = append(result, *n)
	}
	return result, nil
}

func (r *Repository) listNotificationsByState(ctx context.Context, state DeliveryState) ([]Notification, error) {
	rows, err := r.queries.ListNotificationsByState(ctx, string(state))
	if err != nil {
		return nil, fmt.Errorf("list %s notifications: %w", state, err)
	}
	result := make([]Notification, 0, len(rows))
	for _, row := range rows {
		n, err := notificationFromDB(row)
		if err != nil {
			return nil, err
		}
		result = append(result, *n)
	}
	return result, nil
}

// markSent is a conditional PENDING→SENT transition; a lost race returns errConflict.
func (r *Repository) markSent(ctx context.Context, id, revisionID string, at time.Time) error {
	rows, err := r.queries.MarkNotificationSent(ctx, dbgen.MarkNotificationSentParams{
		SentAt: codec.FormatOptionalTime(&at), LastRevisionID: revisionID, UpdatedAt: codec.FormatTime(at), EntityID: id,
	})
	if err != nil {
		return fmt.Errorf("mark notification %s sent: %w", id, err)
	}
	if rows != 1 {
		return errConflict
	}
	return nil
}

// markCancelled is a conditional PENDING→CANCELLED transition.
func (r *Repository) markCancelled(ctx context.Context, id, revisionID string, at time.Time) error {
	rows, err := r.queries.MarkNotificationCancelled(ctx, dbgen.MarkNotificationCancelledParams{
		LastRevisionID: revisionID, UpdatedAt: codec.FormatTime(at), EntityID: id,
	})
	if err != nil {
		return fmt.Errorf("mark notification %s cancelled: %w", id, err)
	}
	if rows != 1 {
		return errConflict
	}
	return nil
}

func (r *Repository) markRead(ctx context.Context, id, revisionID string, at time.Time) error {
	if err := r.queries.MarkNotificationRead(ctx, dbgen.MarkNotificationReadParams{
		ReadAt: codec.FormatOptionalTime(&at), LastRevisionID: revisionID, UpdatedAt: codec.FormatTime(at), EntityID: id,
	}); err != nil {
		return fmt.Errorf("mark notification %s read: %w", id, err)
	}
	return nil
}

func issuesFromDB(rows []dbgen.AttentionIssue) ([]AttentionIssue, error) {
	result := make([]AttentionIssue, 0, len(rows))
	for _, row := range rows {
		issue, err := issueFromDB(row)
		if err != nil {
			return nil, err
		}
		result = append(result, *issue)
	}
	return result, nil
}

func issueFromDB(row dbgen.AttentionIssue) (*AttentionIssue, error) {
	opened, err := codec.ParseTime(row.OpenedAt)
	if err != nil {
		return nil, fmt.Errorf("parse attention issue %s opened_at: %w", row.EntityID, err)
	}
	updated, err := codec.ParseTime(row.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse attention issue %s updated_at: %w", row.EntityID, err)
	}
	resolved, err := codec.ParseOptionalTime(row.ResolvedAt)
	if err != nil {
		return nil, fmt.Errorf("parse attention issue %s resolved_at: %w", row.EntityID, err)
	}
	snoozed, err := codec.ParseOptionalTime(row.SnoozedUntil)
	if err != nil {
		return nil, fmt.Errorf("parse attention issue %s snoozed_until: %w", row.EntityID, err)
	}
	state := IssueState(row.State)
	if state != IssueOpen && state != IssueResolved {
		return nil, fmt.Errorf("attention issue %s has invalid state %q", row.EntityID, row.State)
	}
	return &AttentionIssue{
		ID: row.EntityID, WorkItemID: row.WorkItemID, SubjectTrackedItemID: row.SubjectTrackedItemID,
		MonitorPointID: row.MonitorPointID, PlannerItemID: row.PlannerItemID, Type: IssueType(row.IssueType), Severity: Severity(row.Severity), State: state,
		Reason: row.Reason, ReasonJSON: row.ReasonJson, RecommendedAction: row.RecommendedAction,
		OpenedAt: opened, ResolvedAt: resolved, SnoozedUntil: snoozed, DedupeKey: row.DedupeKey,
		LastRevisionID: row.LastRevisionID, UpdatedAt: updated,
	}, nil
}

func notificationFromDB(row dbgen.Notification) (*Notification, error) {
	created, err := codec.ParseTime(row.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse notification %s created_at: %w", row.EntityID, err)
	}
	updated, err := codec.ParseTime(row.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("parse notification %s updated_at: %w", row.EntityID, err)
	}
	sent, err := codec.ParseOptionalTime(row.SentAt)
	if err != nil {
		return nil, fmt.Errorf("parse notification %s sent_at: %w", row.EntityID, err)
	}
	presented, err := codec.ParseOptionalTime(row.PresentedAt)
	if err != nil {
		return nil, fmt.Errorf("parse notification %s presented_at: %w", row.EntityID, err)
	}
	read, err := codec.ParseOptionalTime(row.ReadAt)
	if err != nil {
		return nil, fmt.Errorf("parse notification %s read_at: %w", row.EntityID, err)
	}
	dismissed, err := codec.ParseOptionalTime(row.DismissedAt)
	if err != nil {
		return nil, fmt.Errorf("parse notification %s dismissed_at: %w", row.EntityID, err)
	}
	return &Notification{
		ID: row.EntityID, AttentionIssueID: row.AttentionIssueID, WorkItemID: row.WorkItemID,
		SubjectTrackedItemID: row.SubjectTrackedItemID, PlannerItemID: row.PlannerItemID, Title: row.Title, Message: row.Message,
		ReasonSnapshotJSON: row.ReasonSnapshotJson, State: DeliveryState(row.State), Channel: Channel(row.Channel),
		SentAt: sent, PresentedAt: presented, ReadAt: read, DismissedAt: dismissed, DedupeKey: row.DedupeKey,
		LastRevisionID: row.LastRevisionID, CreatedAt: created, UpdatedAt: updated,
	}, nil
}

func (r *Repository) markPresented(ctx context.Context, id, revisionID string, at time.Time) (bool, error) {
	formatted := codec.FormatTime(at)
	rows, err := r.queries.MarkNotificationPresented(ctx, dbgen.MarkNotificationPresentedParams{
		PresentedAt:    &formatted,
		LastRevisionID: revisionID,
		UpdatedAt:      formatted,
		EntityID:       id,
	})
	if err != nil {
		return false, fmt.Errorf("mark notification presented %s: %w", id, err)
	}
	return rows > 0, nil
}
