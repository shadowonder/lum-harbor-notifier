package app

import (
	"encoding/json"
	"time"

	"lum-harbor-notifier/internal/attention"
)

// Notification Center transport models are domain-shaped and presentation-free
// (no tabs, colors, icons). Enum values are the canonical domain vocabulary;
// timestamps are RFC 3339 UTC. Revision IDs never cross Wails.

// NotificationReasonDTO is the bounded, versioned explainability snapshot taken
// when the Notification was created; it never changes with later truth.
type NotificationReasonDTO struct {
	Version                   int     `json:"version"`
	MonitorPointID            string  `json:"monitorPointId"`
	MonitorKind               string  `json:"monitorKind"`
	MonitorTitle              string  `json:"monitorTitle"`
	DueAt                     string  `json:"dueAt"`
	Timezone                  string  `json:"timezone"`
	SubjectKind               string  `json:"subjectKind"`
	SubjectID                 string  `json:"subjectId"`
	SubjectTitle              *string `json:"subjectTitle"`
	ExpectedKind              string  `json:"expectedKind"`
	ExpectedWorkItemLifecycle *string `json:"expectedWorkItemLifecycle"`
	ExpectedStepID            *string `json:"expectedStepId"`
	ExpectedStepTitle         *string `json:"expectedStepTitle"`
	ActualWorkItemLifecycle   *string `json:"actualWorkItemLifecycle"`
	ActualStepState           *string `json:"actualStepState"`
}

// NotificationCenterItemDTO is one delivered (SENT) Notification with its
// linked Attention truth. Disposition is derived by the backend clock from
// snoozedUntil; the frontend must not own time policy.
type NotificationCenterItemDTO struct {
	NotificationID       string                `json:"notificationId"`
	AttentionIssueID     string                `json:"attentionIssueId"`
	WorkItemID           *string               `json:"workItemId"`
	WorkItemTitle        *string               `json:"workItemTitle"`
	SubjectTrackedItemID *string               `json:"subjectTrackedItemId"`
	PlannerItemID        *string               `json:"plannerItemId,omitempty"`
	Title                string                `json:"title"`
	Message              string                `json:"message"`
	Severity             string                `json:"severity"`
	IssueType            string                `json:"issueType"`
	RecommendedAction    *string               `json:"recommendedAction"`
	DeliveryState        string                `json:"deliveryState"`
	SentAt               *string               `json:"sentAt"`
	ReadAt               *string               `json:"readAt"`
	DismissedAt          *string               `json:"dismissedAt"`
	AttentionState       string                `json:"attentionState"`
	SnoozedUntil         *string               `json:"snoozedUntil"`
	ResolvedAt           *string               `json:"resolvedAt"`
	Disposition          string                `json:"disposition"`
	CreatedAt            string                `json:"createdAt"`
	UpdatedAt            string                `json:"updatedAt"`
	Reason               NotificationReasonDTO `json:"reason"`
}

// NotificationCenterDTO is one bounded collection; the five product views
// (All, Needs Action, Unread, Snoozed, Resolved) are derived from it.
type NotificationCenterDTO struct {
	// At is the backend clock instant used to derive dispositions.
	At    string                      `json:"at"`
	Items []NotificationCenterItemDTO `json:"items"`
}

// MarkNotificationReadRequestDTO identifies the delivered Notification to read.
type MarkNotificationReadRequestDTO struct {
	NotificationID string `json:"notificationId"`
}

// NotificationReadResultDTO is deliberately small; callers reload the center.
type NotificationReadResultDTO struct {
	NotificationID string `json:"notificationId"`
	ReadAt         string `json:"readAt"`
}

// SnoozeAttentionIssueRequestDTO carries only the issue and the requested
// instant (RFC 3339). Validation uses the backend clock.
type SnoozeAttentionIssueRequestDTO struct {
	AttentionIssueID string `json:"attentionIssueId"`
	SnoozedUntil     string `json:"snoozedUntil"`
}

type SnoozeResultDTO struct {
	AttentionIssueID string `json:"attentionIssueId"`
	SnoozedUntil     string `json:"snoozedUntil"`
}

func optionalTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339Nano)
	return &s
}

func notificationCenterToDTO(center attention.NotificationCenter, workItemTitles map[string]string) (NotificationCenterDTO, error) {
	dto := NotificationCenterDTO{At: center.At.UTC().Format(time.RFC3339Nano), Items: make([]NotificationCenterItemDTO, 0, len(center.Items))}
	for _, item := range center.Items {
		n, issue := item.View.Notification, item.View.Issue
		var reasonDTO NotificationReasonDTO
		var facts attention.ReasonFacts
		if err := json.Unmarshal([]byte(n.ReasonSnapshotJSON), &facts); err == nil {
			reasonDTO = NotificationReasonDTO{
				Version: facts.Version, MonitorPointID: facts.MonitorPointID, MonitorKind: facts.MonitorKind,
				MonitorTitle: facts.MonitorTitle, DueAt: facts.DueAt, Timezone: facts.Timezone,
				SubjectKind: facts.Subject.Kind, SubjectID: facts.Subject.ID, SubjectTitle: facts.Subject.Title,
				ExpectedKind: facts.Expected.Kind, ExpectedWorkItemLifecycle: facts.Expected.WorkItemLifecycle,
				ExpectedStepID: facts.Expected.StepID, ExpectedStepTitle: facts.Expected.StepTitle,
				ActualWorkItemLifecycle: facts.Actual.WorkItemLifecycle, ActualStepState: facts.Actual.StepState,
			}
		}
		if issue.Type == attention.IssueTodoReminderDue {
			var raw map[string]any
			if err := json.Unmarshal([]byte(n.ReasonSnapshotJSON), &raw); err == nil {
				if dueAt, ok := raw["due_at"].(string); ok {
					reasonDTO.DueAt = dueAt
				}
				if title, ok := raw["title"].(string); ok {
					reasonDTO.SubjectTitle = &title
				}
			}
			reasonDTO.SubjectKind = "PLANNER_ITEM"
		}
		var workItemTitle *string
		if n.WorkItemID != nil {
			if title, ok := workItemTitles[*n.WorkItemID]; ok {
				workItemTitle = &title
			}
		}
		dto.Items = append(dto.Items, NotificationCenterItemDTO{
			NotificationID: n.ID, AttentionIssueID: issue.ID, WorkItemID: n.WorkItemID, WorkItemTitle: workItemTitle,
			SubjectTrackedItemID: n.SubjectTrackedItemID, PlannerItemID: n.PlannerItemID, Title: n.Title, Message: n.Message,
			Severity: string(issue.Severity), IssueType: string(issue.Type), RecommendedAction: issue.RecommendedAction,
			DeliveryState: string(n.State), SentAt: optionalTime(n.SentAt), ReadAt: optionalTime(n.ReadAt),
			DismissedAt: optionalTime(n.DismissedAt), AttentionState: string(issue.State),
			SnoozedUntil: optionalTime(issue.SnoozedUntil), ResolvedAt: optionalTime(issue.ResolvedAt),
			Disposition: string(item.Disposition),
			CreatedAt:   n.CreatedAt.UTC().Format(time.RFC3339Nano), UpdatedAt: n.UpdatedAt.UTC().Format(time.RFC3339Nano),
			Reason: reasonDTO,
		})
	}
	return dto, nil
}
