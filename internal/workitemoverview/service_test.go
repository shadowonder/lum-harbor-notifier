package workitemoverview

import (
	"context"
	"errors"
	"testing"
	"time"

	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

type workItemReaderStub struct {
	items []workitem.WorkItem
	err   error
}

func (s workItemReaderStub) List(context.Context) ([]workitem.WorkItem, error) { return s.items, s.err }

type trackingReaderStub struct {
	summaries map[string]tracking.WorkItemSummary
	err       error
}

func (s trackingReaderStub) ListWorkItemSummaries(context.Context) (map[string]tracking.WorkItemSummary, error) {
	return s.summaries, s.err
}

type attentionReaderStub struct {
	summaries map[string]attention.WorkItemSummary
	err       error
}

type trackingTodoReaderStub struct {
	targets []planner.TrackingTodoDueTarget
	err     error
}

func (s trackingTodoReaderStub) ListOpenTrackingTodoDueTargets(context.Context) ([]planner.TrackingTodoDueTarget, error) {
	return s.targets, s.err
}

func (s attentionReaderStub) ListWorkItemSummaries(context.Context) (map[string]attention.WorkItemSummary, error) {
	return s.summaries, s.err
}

func TestListComposesByStableIdentityAndCreatesValidEmptySummaries(t *testing.T) {
	items := []workitem.WorkItem{{ID: "one", Title: "One"}, {ID: "two", Title: "Two"}}
	view, err := NewService(
		workItemReaderStub{items: items},
		trackingReaderStub{summaries: map[string]tracking.WorkItemSummary{"one": {WorkItemID: "one", TrackedItemCount: 2}, "not-listed": {WorkItemID: "not-listed", TrackedItemCount: 99}}},
		attentionReaderStub{summaries: map[string]attention.WorkItemSummary{"two": {WorkItemID: "two", ActionableIssueCount: 1}}},
		trackingTodoReaderStub{},
	).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !view.TrackingAvailable || !view.AttentionAvailable || len(view.Items) != 2 {
		t.Fatalf("unexpected view: %+v", view)
	}
	if view.Items[0].Tracking.TrackedItemCount != 2 || view.Items[0].Attention.WorkItemID != "one" || view.Items[0].Attention.ActionableIssueCount != 0 {
		t.Fatalf("first WorkItem was contaminated: %+v", view.Items[0])
	}
	if view.Items[1].Tracking.WorkItemID != "two" || view.Items[1].Tracking.TrackedItemCount != 0 || view.Items[1].Attention.ActionableIssueCount != 1 {
		t.Fatalf("second WorkItem was contaminated: %+v", view.Items[1])
	}
}

func TestListKeepsWorkItemsWhenIndependentSummariesFail(t *testing.T) {
	boom := errors.New("unavailable")
	view, err := NewService(
		workItemReaderStub{items: []workitem.WorkItem{{ID: "one"}}},
		trackingReaderStub{err: boom},
		attentionReaderStub{err: boom},
		trackingTodoReaderStub{err: boom},
	).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if view.TrackingAvailable || view.AttentionAvailable || len(view.Items) != 1 {
		t.Fatalf("partial failure semantics were lost: %+v", view)
	}
	if view.Items[0].Tracking.WorkItemID != "one" || view.Items[0].Attention.WorkItemID != "one" {
		t.Fatalf("unavailable summaries lost stable identity: %+v", view.Items[0])
	}
}

func TestListFailsWhenPrimaryWorkItemReadFails(t *testing.T) {
	boom := errors.New("primary failed")
	_, err := NewService(workItemReaderStub{err: boom}, trackingReaderStub{}, attentionReaderStub{}, trackingTodoReaderStub{}).List(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("expected primary error, got %v", err)
	}
}

func TestListDerivesTrackingSignalFromEarliestOpenTodoDueTargetOnly(t *testing.T) {
	now := time.Date(2026, time.October, 3, 12, 0, 0, 0, time.UTC)
	warning := attention.SeverityWarning
	critical := attention.SeverityCritical
	blocked := tracking.StepBlocked
	failed := tracking.StepFailed
	items := []tracking.OverviewTrackedItem{
		{ID: "story", ItemType: ptrString("STORY"), Progression: tracking.Progression{CurrentState: &blocked}},
		{ID: "task", ItemType: ptrString("TASK"), Progression: tracking.Progression{CurrentState: &failed}},
		{ID: "defect", ItemType: ptrString("DEFECT")},
		{ID: "gate", ItemType: ptrString("GATE"), Monitoring: tracking.OverviewItemMonitoring{HasSatisfiedExpectation: true}},
		{ID: "active-monitor", Monitoring: tracking.OverviewItemMonitoring{HasActiveExpectation: true}},
		{ID: "overdue", ItemType: ptrString("TASK")},
		{ID: "green"},
		{ID: "yellow"},
		{ID: "yellow-between"},
		{ID: "red"},
		{ID: "red-soon"},
		{ID: "red-overdue"},
	}
	service := NewService(
		workItemReaderStub{items: []workitem.WorkItem{{ID: "one"}}},
		trackingReaderStub{summaries: map[string]tracking.WorkItemSummary{"one": {WorkItemID: "one", Items: items}}},
		attentionReaderStub{summaries: map[string]attention.WorkItemSummary{"one": {
			WorkItemID: "one",
			TrackedItems: map[string]attention.TrackedItemSummary{
				"task":    {ActionableIssueCount: 1, HighestActiveSeverity: &warning},
				"defect":  {ActionableIssueCount: 1, HighestActiveSeverity: &critical},
				"overdue": {ActionableIssueCount: 1, HighestActiveSeverity: &warning, HasOverdueUnmetExpectation: true},
			},
		}}},
		trackingTodoReaderStub{targets: []planner.TrackingTodoDueTarget{
			{PlannerItemID: "green-later", TrackedItemID: "green", WorkItemID: "one", DueAt: now.Add(5 * 24 * time.Hour)},
			{PlannerItemID: "green-earliest", TrackedItemID: "green", WorkItemID: "one", DueAt: now.Add(73 * time.Hour)},
			{PlannerItemID: "yellow", TrackedItemID: "yellow", WorkItemID: "one", DueAt: now.Add(72 * time.Hour)},
			{PlannerItemID: "yellow-between", TrackedItemID: "yellow-between", WorkItemID: "one", DueAt: now.Add(48 * time.Hour)},
			{PlannerItemID: "red", TrackedItemID: "red", WorkItemID: "one", DueAt: now.Add(24 * time.Hour)},
			{PlannerItemID: "red-soon", TrackedItemID: "red-soon", WorkItemID: "one", DueAt: now.Add(23 * time.Hour)},
			{PlannerItemID: "red-overdue", TrackedItemID: "red-overdue", WorkItemID: "one", DueAt: now.Add(-time.Minute)},
		}},
	)
	service.SetClock(func() time.Time { return now })
	view, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	signals := view.Items[0].TrackedItemSignals
	if signals["story"].State != SignalNeutral {
		t.Fatalf("BLOCKED execution state without monitoring/attention must remain neutral: %+v", signals["story"])
	}
	if signals["task"].State != SignalNeutral || signals["defect"].State != SignalNeutral {
		t.Fatalf("Attention must not govern tracking health: %+v", signals)
	}
	if signals["gate"].State != SignalNeutral {
		t.Fatalf("MonitorPoint satisfaction must not govern tracking health: %+v", signals["gate"])
	}
	if signals["active-monitor"].State != SignalNeutral {
		t.Fatalf("active monitoring without positive/problem truth must remain neutral: %+v", signals["active-monitor"])
	}
	if signals["overdue"].State != SignalNeutral {
		t.Fatalf("unlinked overdue Attention must not govern tracking health: %+v", signals["overdue"])
	}
	if signals["green"].State != SignalSatisfied || signals["yellow"].State != SignalWatch || signals["yellow-between"].State != SignalWatch || signals["red"].State != SignalProblem || signals["red-soon"].State != SignalProblem || signals["red-overdue"].State != SignalProblem {
		t.Fatalf("Todo due thresholds are wrong: %+v", signals)
	}
	if !signals["green"].HasActiveTrackingTodo || signals["green"].GoverningTodoDueAt == nil || !signals["green"].GoverningTodoDueAt.Equal(now.Add(73*time.Hour)) {
		t.Fatalf("earliest open Todo must govern: %+v", signals["green"])
	}
}

func ptrString(value string) *string { return &value }
