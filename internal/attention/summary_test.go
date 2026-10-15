package attention

import (
	"testing"
	"time"

	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

func TestListWorkItemSummariesUsesDispositionAndSeveritySemantics(t *testing.T) {
	f := newFixture(t)
	primary := f.workItem(t)
	zero := f.workItem(t)
	resolvedWorkItem := f.workItem(t)

	monitors := []*tracking.MonitorPoint{
		createSummaryMonitor(t, f, primary, tracking.MonitorFinalTarget, "Final"),
		createSummaryMonitor(t, f, primary, tracking.MonitorCheckpoint, "Checkpoint"),
		createSummaryMonitor(t, f, primary, tracking.MonitorReview, "Review"),
		createSummaryMonitor(t, f, primary, tracking.MonitorCheckpoint, "Checkpoint two"),
		createSummaryMonitor(t, f, primary, tracking.MonitorReview, "Review two"),
	}
	resolvedMonitor := createSummaryMonitor(t, f, resolvedWorkItem, tracking.MonitorFinalTarget, "Resolved")
	f.reconcile(t, f.clock)

	issues := f.issues(t)
	byMonitor := make(map[string]AttentionIssue, len(issues))
	for _, issue := range issues {
		if issue.MonitorPointID != nil {
			byMonitor[*issue.MonitorPointID] = issue
		}
	}
	if len(byMonitor) != 6 {
		t.Fatalf("expected six issues, got %d", len(byMonitor))
	}
	for index, monitor := range monitors {
		severity := []Severity{SeverityCritical, SeverityInfo, SeverityWarning, SeverityInfo, SeverityWarning}[index]
		if _, err := f.db.ExecContext(f.ctx, "UPDATE attention_issues SET severity = ? WHERE entity_id = ?", string(severity), byMonitor[monitor.ID].ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := f.attention.SnoozeAttentionIssue(f.ctx, SnoozeInput{ID: byMonitor[monitors[1].ID].ID, Until: f.clock.Add(2 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.workItems.SetLocalWorkItemLifecycleState(f.ctx, workitem.SetLocalWorkItemLifecycleStateInput{ID: resolvedWorkItem.ID, Target: workitem.LifecycleCompleted}); err != nil {
		t.Fatal(err)
	}
	f.reconcile(t, f.clock.Add(time.Minute))

	summaries, err := f.attention.ListWorkItemSummaries(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := summaries[primary.ID]
	if got.WorkItemID != primary.ID || got.OpenIssueCount != 5 || got.ActionableIssueCount != 4 || got.SnoozedIssueCount != 1 {
		t.Fatalf("unexpected Attention summary: %+v", got)
	}
	if got.HighestActiveSeverity == nil || *got.HighestActiveSeverity != SeverityCritical {
		t.Fatalf("highest actionable severity is wrong: %+v", got.HighestActiveSeverity)
	}
	if _, exists := summaries[zero.ID]; exists {
		t.Fatal("a no-Attention WorkItem should be represented by the composer as a valid zero, not fabricated in the domain map")
	}
	if _, exists := summaries[resolvedWorkItem.ID]; exists {
		t.Fatalf("resolved issue for monitor %s remained actionable", resolvedMonitor.ID)
	}

	f.clock = f.clock.Add(3 * time.Hour)
	summaries, err = f.attention.ListWorkItemSummaries(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	got = summaries[primary.ID]
	if got.ActionableIssueCount != 5 || got.SnoozedIssueCount != 0 {
		t.Fatalf("expired snooze did not return to NEEDS_ACTION: %+v", got)
	}
}

func TestListWorkItemSummariesProjectsItemScopedActionableSignal(t *testing.T) {
	f := newFixture(t)
	step := f.stepMonitor(t, due)
	f.reconcile(t, f.clock)
	issue := f.issues(t)[0]
	if _, err := f.db.ExecContext(f.ctx, "UPDATE attention_issues SET severity = ? WHERE entity_id = ?", string(SeverityWarning), issue.ID); err != nil {
		t.Fatal(err)
	}

	summaries, err := f.attention.ListWorkItemSummaries(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	item := summaries[step.wi.ID].TrackedItems[step.item.ID]
	if item.ActionableIssueCount != 1 || item.HighestActiveSeverity == nil || *item.HighestActiveSeverity != SeverityWarning || !item.HasOverdueUnmetExpectation {
		t.Fatalf("item-scoped actionable signal is wrong: %+v", item)
	}
}

func createSummaryMonitor(t *testing.T, f *fixture, item *workitem.WorkItem, kind tracking.MonitorKind, title string) *tracking.MonitorPoint {
	t.Helper()
	expected := workitem.LifecycleCompleted
	monitor, err := f.tracking.CreateMonitorPoint(f.ctx, tracking.CreateMonitorPointInput{
		WorkItemID: item.ID, Kind: kind, Title: title, DueAt: due,
		Timezone: "UTC", ExpectedWorkItemLifecycle: &expected,
	})
	if err != nil {
		t.Fatal(err)
	}
	return monitor
}
