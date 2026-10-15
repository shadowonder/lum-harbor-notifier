package readcap

import (
	"context"
	"fmt"
	"strings"
	"time"

	"lum-harbor-notifier/internal/attention"
)

// AttentionReadForWorkItemInput is the validated input for CapabilityAttentionReadForWorkItem.
type AttentionReadForWorkItemInput struct {
	WorkItemID string `json:"workItemId"`
}

// AttentionIssueSummary is the bounded projection of one AttentionIssue.
type AttentionIssueSummary struct {
	ID                string                `json:"id"`
	Type              attention.IssueType   `json:"type"`
	Severity          attention.Severity    `json:"severity"`
	State             attention.IssueState  `json:"state"`
	Disposition       attention.Disposition `json:"disposition"`
	Reason            string                `json:"reason"`
	RecommendedAction *string               `json:"recommendedAction,omitempty"`
	OpenedAt          time.Time             `json:"openedAt"`
}

// AttentionReadForWorkItemOutput is the result of CapabilityAttentionReadForWorkItem.
type AttentionReadForWorkItemOutput struct {
	WorkItemID            string                  `json:"workItemId"`
	OpenIssueCount        int                     `json:"openIssueCount"`
	ActionableIssueCount  int                     `json:"actionableIssueCount"`
	SnoozedIssueCount     int                     `json:"snoozedIssueCount"`
	HighestActiveSeverity *attention.Severity     `json:"highestActiveSeverity,omitempty"`
	Issues                []AttentionIssueSummary `json:"issues"`
}

func (s *Service) attentionReadForWorkItem(ctx context.Context, input AttentionReadForWorkItemInput) (AttentionReadForWorkItemOutput, error) {
	workItemID := strings.TrimSpace(input.WorkItemID)
	if workItemID == "" {
		return AttentionReadForWorkItemOutput{}, fmt.Errorf("%w: %s requires a non-empty workItemId", ErrInvalidInput, CapabilityAttentionReadForWorkItem)
	}
	if s.attention == nil {
		return AttentionReadForWorkItemOutput{}, fmt.Errorf("%s: attention service is not configured", CapabilityAttentionReadForWorkItem)
	}

	issues, err := s.attention.ListIssuesForWorkItem(ctx, workItemID)
	if err != nil {
		return AttentionReadForWorkItemOutput{}, fmt.Errorf("%s: %w", CapabilityAttentionReadForWorkItem, err)
	}

	summaries, err := s.attention.ListWorkItemSummaries(ctx)
	if err != nil {
		return AttentionReadForWorkItemOutput{}, fmt.Errorf("%s: %w", CapabilityAttentionReadForWorkItem, err)
	}

	summary := summaries[workItemID]
	now := time.Now().UTC()

	issueSummaries := make([]AttentionIssueSummary, 0, len(issues))
	for _, issue := range issues {
		issueSummaries = append(issueSummaries, AttentionIssueSummary{
			ID:                issue.ID,
			Type:              issue.Type,
			Severity:          issue.Severity,
			State:             issue.State,
			Disposition:       issue.DispositionAt(now),
			Reason:            issue.Reason,
			RecommendedAction: issue.RecommendedAction,
			OpenedAt:          issue.OpenedAt,
		})
	}

	return AttentionReadForWorkItemOutput{
		WorkItemID:            workItemID,
		OpenIssueCount:        summary.OpenIssueCount,
		ActionableIssueCount:  summary.ActionableIssueCount,
		SnoozedIssueCount:     summary.SnoozedIssueCount,
		HighestActiveSeverity: summary.HighestActiveSeverity,
		Issues:                issueSummaries,
	}, nil
}
