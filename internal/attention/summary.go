package attention

import (
	"context"
)

type TrackedItemSummary struct {
	ActionableIssueCount       int
	HighestActiveSeverity      *Severity
	HasOverdueUnmetExpectation bool
}

// WorkItemSummary is the minimal Attention-owned fact set used by product
// overviews. Counts and severity reflect the service clock and reuse
// AttentionIssue.DispositionAt rather than duplicating snooze policy.
type WorkItemSummary struct {
	WorkItemID            string
	OpenIssueCount        int
	ActionableIssueCount  int
	SnoozedIssueCount     int
	HighestActiveSeverity *Severity
	TrackedItems          map[string]TrackedItemSummary
}

func severityRank(severity Severity) int {
	switch severity {
	case SeverityCritical:
		return 3
	case SeverityWarning:
		return 2
	case SeverityInfo:
		return 1
	default:
		return 0
	}
}

// ListWorkItemSummaries reads all current OPEN issues once and groups them by
// WorkItem. A missing key is a valid zero summary, not an unavailable read.
func (s *Service) ListWorkItemSummaries(ctx context.Context) (map[string]WorkItemSummary, error) {
	issues, err := NewRepository(s.db).listOpenIssues(ctx)
	if err != nil {
		return nil, err
	}
	now := s.clock()
	summaries := make(map[string]WorkItemSummary)
	for _, issue := range issues {
		if issue.WorkItemID == nil {
			continue
		}
		wid := *issue.WorkItemID
		summary := summaries[wid]
		summary.WorkItemID = wid
		if summary.TrackedItems == nil {
			summary.TrackedItems = make(map[string]TrackedItemSummary)
		}
		summary.OpenIssueCount++
		switch issue.DispositionAt(now) {
		case DispositionNeedsAction:
			summary.ActionableIssueCount++
			if summary.HighestActiveSeverity == nil || severityRank(issue.Severity) > severityRank(*summary.HighestActiveSeverity) {
				severity := issue.Severity
				summary.HighestActiveSeverity = &severity
			}
			if issue.SubjectTrackedItemID != nil {
				item := summary.TrackedItems[*issue.SubjectTrackedItemID]
				item.ActionableIssueCount++
				if issue.Type == IssueMonitorOverdueUnmet {
					item.HasOverdueUnmetExpectation = true
				}
				if item.HighestActiveSeverity == nil || severityRank(issue.Severity) > severityRank(*item.HighestActiveSeverity) {
					severity := issue.Severity
					item.HighestActiveSeverity = &severity
				}
				summary.TrackedItems[*issue.SubjectTrackedItemID] = item
			}
		case DispositionSnoozed:
			summary.SnoozedIssueCount++
		}
		summaries[wid] = summary
	}
	return summaries, nil
}
