package attention

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"lum-harbor-notifier/internal/history"
)

type attentionOpenedSnapshot struct {
	Version        int     `json:"version"`
	IssueType      string  `json:"issue_type"`
	Severity       string  `json:"severity"`
	State          string  `json:"state"`
	MonitorPointID *string `json:"monitor_point_id,omitempty"`
	PlannerItemID  *string `json:"planner_item_id,omitempty"`
	ReasonVersion  int     `json:"reason_version"`
}

type attentionStateSnapshot struct {
	Version      int     `json:"version"`
	State        string  `json:"state"`
	SnoozedUntil *string `json:"snoozed_until"`
	ResolvedAt   *string `json:"resolved_at,omitempty"`
}

type notificationStateSnapshot struct {
	Version          int     `json:"version"`
	State            string  `json:"state"`
	Channel          string  `json:"channel,omitempty"`
	AttentionIssueID string  `json:"attention_issue_id,omitempty"`
	SentAt           *string `json:"sent_at"`
	ReadAt           *string `json:"read_at"`
}

func instant(value *time.Time) *string {
	if value == nil {
		return nil
	}
	formatted := value.UTC().Format(time.RFC3339Nano)
	return &formatted
}

func issueCorrelation(issue AttentionIssue) string {
	if issue.MonitorPointID != nil && *issue.MonitorPointID != "" {
		return history.MonitorCorrelation(*issue.MonitorPointID)
	}
	if issue.PlannerItemID != nil && *issue.PlannerItemID != "" {
		return history.PlannerCorrelation(*issue.PlannerItemID)
	}
	return "attention:" + issue.ID
}

// attentionOpenedSummary explains why Attention opened in terms of the rule
// that opened it: a Todo reminder threshold is not an overdue target.
func attentionOpenedSummary(issueType IssueType) string {
	if issueType == IssueTodoReminderDue {
		return "Attention opened because a Todo reminder is due"
	}
	return "Attention opened because target is overdue"
}

func recordAttentionOpened(ctx context.Context, tx *sql.Tx, issue *AttentionIssue, occurredAt time.Time) (*history.Event, error) {
	after, err := history.Snapshot(attentionOpenedSnapshot{
		Version:        1,
		IssueType:      string(issue.Type),
		Severity:       string(issue.Severity),
		State:          string(issue.State),
		MonitorPointID: issue.MonitorPointID,
		PlannerItemID:  issue.PlannerItemID,
		ReasonVersion:  1,
	})
	if err != nil {
		return nil, err
	}
	historyRepo := history.NewRepository(tx)
	var causeID *string
	if issue.PlannerItemID != nil && *issue.PlannerItemID != "" {
		// A Planner-item issue opens because that item's configured reminder
		// threshold was reached; without a reminder, the item itself is the cause.
		for _, eventType := range []history.EventType{history.EventPlannerItemReminderSet, history.EventPlannerItemCreated} {
			cause, err := historyRepo.FindLatestBySubjectAndType(ctx, *issue.PlannerItemID, eventType)
			if err != nil {
				return nil, err
			}
			if cause != nil {
				causeID = &cause.ID
				break
			}
		}
	}
	revisionID, subjectID := issue.LastRevisionID, issue.ID
	return historyRepo.Append(ctx, history.Draft{
		Type:             history.EventAttentionOpened,
		Scope:            history.ScopeAttention,
		Actor:            history.ActorRule,
		Summary:          attentionOpenedSummary(issue.Type),
		SubjectEntityID:  &subjectID,
		WorkItemID:       issue.WorkItemID,
		RevisionID:       &revisionID,
		CorrelationID:    issueCorrelation(*issue),
		CausationEventID: causeID,
		AfterJSON:        after,
		OccurredAt:       occurredAt,
	})
}

func recordNotificationCreated(ctx context.Context, tx *sql.Tx, notification *Notification, correlationID, causeEventID string, occurredAt time.Time) error {
	after, err := history.Snapshot(notificationStateSnapshot{Version: 1, State: string(notification.State), Channel: string(notification.Channel), AttentionIssueID: notification.AttentionIssueID})
	if err != nil {
		return err
	}
	revisionID, subjectID := notification.LastRevisionID, notification.ID
	var causationPtr *string
	if trimmed := strings.TrimSpace(causeEventID); trimmed != "" {
		causationPtr = &trimmed
	}
	_, err = history.NewRepository(tx).Append(ctx, history.Draft{
		Type:             history.EventNotificationCreated,
		Scope:            history.ScopeNotification,
		Actor:            history.ActorSystem,
		Summary:          "Initial in-app notification created",
		SubjectEntityID:  &subjectID,
		WorkItemID:       notification.WorkItemID,
		RevisionID:       &revisionID,
		CorrelationID:    correlationID,
		CausationEventID: causationPtr,
		AfterJSON:        after,
		OccurredAt:       occurredAt,
	})
	return err
}

func recordAttentionResolved(ctx context.Context, tx *sql.Tx, issue AttentionIssue, revisionID string, occurredAt time.Time) error {
	before, err := history.Snapshot(attentionStateSnapshot{Version: 1, State: string(IssueOpen), SnoozedUntil: instant(issue.SnoozedUntil)})
	if err != nil {
		return err
	}
	resolvedAt := occurredAt.UTC().Format(time.RFC3339Nano)
	after, err := history.Snapshot(attentionStateSnapshot{Version: 1, State: string(IssueResolved), SnoozedUntil: nil, ResolvedAt: &resolvedAt})
	if err != nil {
		return err
	}
	historyRepo := history.NewRepository(tx)
	corrID := issueCorrelation(issue)
	var causeID *string
	if issue.MonitorPointID != nil && *issue.MonitorPointID != "" {
		cause, err := historyRepo.FindLatestBySubjectAndType(ctx, *issue.MonitorPointID, history.EventMonitorStateChanged)
		if err != nil {
			return err
		}
		if cause != nil && cause.CorrelationID == corrID {
			causeID = &cause.ID
		}
	}
	if issue.PlannerItemID != nil && *issue.PlannerItemID != "" {
		// Completing the Planner item resolves its issue in the same Revision.
		cause, err := historyRepo.FindLatestBySubjectAndType(ctx, *issue.PlannerItemID, history.EventPlannerItemCompleted)
		if err != nil {
			return err
		}
		if cause != nil && cause.RevisionID != nil && *cause.RevisionID == revisionID {
			causeID = &cause.ID
		}
	}
	subjectID := issue.ID
	_, err = historyRepo.Append(ctx, history.Draft{
		Type:             history.EventAttentionResolved,
		Scope:            history.ScopeAttention,
		Actor:            history.ActorRule,
		Summary:          "Attention resolved because the target no longer needs attention",
		SubjectEntityID:  &subjectID,
		WorkItemID:       issue.WorkItemID,
		RevisionID:       &revisionID,
		CorrelationID:    corrID,
		CausationEventID: causeID,
		BeforeJSON:       before,
		AfterJSON:        after,
		OccurredAt:       occurredAt,
	})
	return err
}

func recordAttentionSnoozed(ctx context.Context, tx *sql.Tx, issue *AttentionIssue, until time.Time, revisionID string, occurredAt time.Time) error {
	before, err := history.Snapshot(attentionStateSnapshot{Version: 1, State: string(issue.State), SnoozedUntil: instant(issue.SnoozedUntil)})
	if err != nil {
		return err
	}
	after, err := history.Snapshot(attentionStateSnapshot{Version: 1, State: string(issue.State), SnoozedUntil: instant(&until)})
	if err != nil {
		return err
	}
	subjectID := issue.ID
	_, err = history.NewRepository(tx).Append(ctx, history.Draft{
		Type:            history.EventAttentionSnoozed,
		Scope:           history.ScopeAttention,
		Actor:           history.ActorUser,
		Summary:         "Attention snoozed until " + until.UTC().Format(time.RFC3339),
		SubjectEntityID: &subjectID,
		WorkItemID:      issue.WorkItemID,
		RevisionID:      &revisionID,
		CorrelationID:   issueCorrelation(*issue),
		BeforeJSON:      before,
		AfterJSON:       after,
		OccurredAt:      occurredAt,
	})
	return err
}

func recordNotificationRead(ctx context.Context, tx *sql.Tx, notification *Notification, revisionID string, occurredAt time.Time, issue AttentionIssue) error {
	before, err := history.Snapshot(notificationStateSnapshot{Version: 1, State: string(notification.State), ReadAt: nil})
	if err != nil {
		return err
	}
	after, err := history.Snapshot(notificationStateSnapshot{Version: 1, State: string(notification.State), ReadAt: instant(&occurredAt)})
	if err != nil {
		return err
	}
	subjectID := notification.ID
	_, err = history.NewRepository(tx).Append(ctx, history.Draft{
		Type:            history.EventNotificationRead,
		Scope:           history.ScopeNotification,
		Actor:           history.ActorUser,
		Summary:         "Notification marked read",
		SubjectEntityID: &subjectID,
		WorkItemID:      notification.WorkItemID,
		RevisionID:      &revisionID,
		CorrelationID:   issueCorrelation(issue),
		BeforeJSON:      before,
		AfterJSON:       after,
		OccurredAt:      occurredAt,
	})
	return err
}

func recordNotificationSent(ctx context.Context, tx *sql.Tx, notification Notification, issue AttentionIssue, revisionID string, occurredAt time.Time) error {
	before, err := history.Snapshot(notificationStateSnapshot{Version: 1, State: string(DeliveryPending)})
	if err != nil {
		return err
	}
	after, err := history.Snapshot(notificationStateSnapshot{Version: 1, State: string(DeliverySent), SentAt: instant(&occurredAt)})
	if err != nil {
		return err
	}
	historyRepo := history.NewRepository(tx)
	cause, err := historyRepo.FindLatestBySubjectAndType(ctx, notification.ID, history.EventNotificationCreated)
	if err != nil {
		return err
	}
	var causeID *string
	if cause != nil {
		causeID = &cause.ID
	}
	subjectID := notification.ID
	_, err = historyRepo.Append(ctx, history.Draft{
		Type:             history.EventNotificationSent,
		Scope:            history.ScopeNotification,
		Actor:            history.ActorSystem,
		Summary:          "Notification delivered in-app",
		SubjectEntityID:  &subjectID,
		WorkItemID:       notification.WorkItemID,
		RevisionID:       &revisionID,
		CorrelationID:    issueCorrelation(issue),
		CausationEventID: causeID,
		BeforeJSON:       before,
		AfterJSON:        after,
		OccurredAt:       occurredAt,
	})
	return err
}

func recordNotificationCancelled(ctx context.Context, tx *sql.Tx, notification Notification, issue AttentionIssue, revisionID string, occurredAt time.Time) error {
	before, err := history.Snapshot(notificationStateSnapshot{Version: 1, State: string(DeliveryPending), SentAt: nil})
	if err != nil {
		return err
	}
	after, err := history.Snapshot(notificationStateSnapshot{Version: 1, State: string(DeliveryCancelled), SentAt: nil})
	if err != nil {
		return err
	}
	historyRepo := history.NewRepository(tx)
	cause, err := historyRepo.FindLatestBySubjectAndType(ctx, issue.ID, history.EventAttentionResolved)
	if err != nil {
		return err
	}
	corrID := issueCorrelation(issue)
	var causeID *string
	if cause != nil && cause.CorrelationID == corrID {
		causeID = &cause.ID
	}
	subjectID := notification.ID
	_, err = historyRepo.Append(ctx, history.Draft{
		Type:             history.EventNotificationCancelled,
		Scope:            history.ScopeNotification,
		Actor:            history.ActorSystem,
		Summary:          "Pending notification cancelled because its attention issue was resolved",
		SubjectEntityID:  &subjectID,
		WorkItemID:       notification.WorkItemID,
		RevisionID:       &revisionID,
		CorrelationID:    corrID,
		CausationEventID: causeID,
		BeforeJSON:       before,
		AfterJSON:        after,
		OccurredAt:       occurredAt,
	})
	return err
}

func recordNotificationPresented(ctx context.Context, tx *sql.Tx, notification *Notification, revisionID string, occurredAt time.Time, issue AttentionIssue) error {
	before, err := history.Snapshot(notificationStateSnapshot{Version: 1, State: string(notification.State), SentAt: instant(notification.SentAt)})
	if err != nil {
		return err
	}
	after, err := history.Snapshot(notificationStateSnapshot{Version: 1, State: string(notification.State), SentAt: instant(notification.SentAt)})
	if err != nil {
		return err
	}
	historyRepo := history.NewRepository(tx)
	cause, _ := historyRepo.FindLatestBySubjectAndType(ctx, notification.ID, history.EventNotificationSent)
	var causeID *string
	if cause != nil {
		causeID = &cause.ID
	}
	subjectID := notification.ID
	_, err = historyRepo.Append(ctx, history.Draft{
		Type:             history.EventNotificationPresented,
		Scope:            history.ScopeNotification,
		Actor:            history.ActorSystem,
		Summary:          "Notification presented in application UI",
		SubjectEntityID:  &subjectID,
		WorkItemID:       notification.WorkItemID,
		RevisionID:       &revisionID,
		CorrelationID:    issueCorrelation(issue),
		CausationEventID: causeID,
		BeforeJSON:       before,
		AfterJSON:        after,
		OccurredAt:       occurredAt,
	})
	return err
}
