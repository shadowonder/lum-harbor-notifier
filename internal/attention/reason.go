package attention

import (
	"encoding/json"
	"fmt"
	"time"

	"lum-harbor-notifier/internal/tracking"
)

// ReasonVersion versions the bounded structured reason facts stored on the
// AttentionIssue (reason_json) and snapshotted into each Notification.
const ReasonVersion = 1

type reasonSubject struct {
	Kind  string  `json:"kind"`
	ID    string  `json:"id"`
	Title *string `json:"title"`
}

type reasonExpected struct {
	Kind              string  `json:"kind"`
	WorkItemLifecycle *string `json:"work_item_lifecycle"`
	StepID            *string `json:"step_id"`
	StepTitle         *string `json:"step_title"`
}

type reasonActual struct {
	WorkItemLifecycle *string `json:"work_item_lifecycle"`
	StepState         *string `json:"step_state"`
}

// ReasonFacts is the versioned, closed, machine-readable explanation. It holds
// identities and the facts that justified the issue at creation time, never a
// copy of any aggregate or presentation state.
type ReasonFacts struct {
	Version          int            `json:"version"`
	IssueType        string         `json:"issue_type"`
	AttentionIssueID string         `json:"attention_issue_id"`
	MonitorPointID   string         `json:"monitor_point_id"`
	MonitorKind      string         `json:"monitor_kind"`
	MonitorTitle     string         `json:"monitor_title"`
	DueAt            string         `json:"due_at"`
	Timezone         string         `json:"timezone"`
	Subject          reasonSubject  `json:"subject"`
	Expected         reasonExpected `json:"expected"`
	Actual           reasonActual   `json:"actual"`
}

func stringPtr[T ~string](v *T) *string {
	if v == nil {
		return nil
	}
	s := string(*v)
	return &s
}

func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func buildReasonFacts(issueID string, m tracking.MonitorPoint, ev tracking.ExpectationEvaluation) ReasonFacts {
	return ReasonFacts{
		Version:          ReasonVersion,
		IssueType:        string(IssueMonitorOverdueUnmet),
		AttentionIssueID: issueID,
		MonitorPointID:   m.ID,
		MonitorKind:      string(m.Kind),
		MonitorTitle:     m.Title,
		DueAt:            m.DueAt.UTC().Format(time.RFC3339),
		Timezone:         m.Timezone,
		Subject:          reasonSubject{Kind: string(m.Subject.Kind), ID: m.Subject.ID, Title: optionalString(ev.SubjectTitle)},
		Expected: reasonExpected{
			Kind: string(m.Expected.Kind), WorkItemLifecycle: stringPtr(m.Expected.WorkItemState),
			StepID: m.Expected.StepID, StepTitle: optionalString(ev.StepTitle),
		},
		Actual: reasonActual{WorkItemLifecycle: stringPtr(ev.ActualWorkItemLifecycle), StepState: stringPtr(ev.ActualStepState)},
	}
}

func (f ReasonFacts) JSON() (string, error) {
	encoded, err := json.Marshal(f)
	if err != nil {
		return "", fmt.Errorf("marshal reason facts: %w", err)
	}
	return string(encoded), nil
}

// reasonText is the deterministic human-readable explanation (never AI text).
func reasonText(m tracking.MonitorPoint, ev tracking.ExpectationEvaluation) string {
	due := formatDue(m)
	if m.Expected.Kind == tracking.ExpectStepCompleted {
		state := "unknown"
		if ev.ActualStepState != nil {
			state = string(*ev.ActualStepState)
		}
		return fmt.Sprintf("“%s” was due %s. Step “%s” of “%s” is %s; expected COMPLETED.", m.Title, due, ev.StepTitle, ev.SubjectTitle, state)
	}
	actual, expected := "unknown", "unknown"
	if ev.ActualWorkItemLifecycle != nil {
		actual = string(*ev.ActualWorkItemLifecycle)
	}
	if m.Expected.WorkItemState != nil {
		expected = string(*m.Expected.WorkItemState)
	}
	return fmt.Sprintf("“%s” was due %s. The Work Item is still %s; expected %s.", m.Title, due, actual, expected)
}

func formatDue(m tracking.MonitorPoint) string {
	location, err := time.LoadLocation(m.Timezone)
	if err != nil {
		location = time.UTC
	}
	return m.DueAt.In(location).Format("Jan 2, 2006 3:04 PM MST")
}

func recommendedAction(m tracking.MonitorPoint) string {
	if m.Expected.Kind == tracking.ExpectStepCompleted {
		return "Complete the expected step, or mark the target satisfied or cancelled."
	}
	return "Bring the Work Item to the expected state, or mark the target satisfied or cancelled."
}

func dedupeKey(m tracking.MonitorPoint) string {
	return "monitor-overdue-unmet:" + m.ID
}

func initialNotificationKey(issueID string) string {
	return "attention-opened:" + issueID
}
