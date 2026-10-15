package dashboardview

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"lum-harbor-notifier/internal/plannerview"
	"lum-harbor-notifier/internal/temporal"
	"lum-harbor-notifier/internal/testsupport"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

type recordingPlannerReader struct {
	request plannerview.Request
	view    *plannerview.View
}

func (r *recordingPlannerReader) Get(_ context.Context, request plannerview.Request) (*plannerview.View, error) {
	r.request = request
	return r.view, nil
}

type fixedWorkItemReader struct {
	items []workitem.WorkItem
}

func (r fixedWorkItemReader) List(context.Context) ([]workitem.WorkItem, error) {
	return r.items, nil
}

type fixedTrackingSummaryReader struct {
	summaries map[string]tracking.WorkItemSummary
	err       error
}

func (r fixedTrackingSummaryReader) ListWorkItemSummaries(context.Context) (map[string]tracking.WorkItemSummary, error) {
	return r.summaries, r.err
}

func TestServiceGetUsesRealWorkItemsAndPreservesPlannerProjection(t *testing.T) {
	ctx := context.Background()
	database := testsupport.OpenTestDatabase(t)
	workItems := workitem.NewService(database.DB, workitem.NewRepository(database.DB))
	plannerProjection := &plannerview.View{}
	planner := &recordingPlannerReader{view: plannerProjection}
	request := testRequest(t)

	description := "Authoritative active description"
	active, err := workItems.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: "Active", Description: &description})
	if err != nil {
		t.Fatal(err)
	}
	paused := createWorkItem(t, workItems, "Paused")
	completed := createWorkItem(t, workItems, "Completed")
	cancelled := createWorkItem(t, workItems, "Cancelled")
	archived := createWorkItem(t, workItems, "Archived")
	setLifecycle(t, workItems, paused.ID, workitem.LifecyclePaused)
	setLifecycle(t, workItems, completed.ID, workitem.LifecycleCompleted)
	setLifecycle(t, workItems, cancelled.ID, workitem.LifecycleCancelled)
	archiveEntity(t, database.DB, archived.ID)

	view, err := NewService(planner, workItems, fixedTrackingSummaryReader{summaries: map[string]tracking.WorkItemSummary{}}).Get(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if view.Planner != plannerProjection || !reflect.DeepEqual(planner.request, request.Planner) {
		t.Fatalf("Dashboard did not delegate Planner semantics unchanged: request=%+v", planner.request)
	}
	if view.WorkItems.CurrentCount != 2 || view.WorkItems.PausedCount != 1 {
		t.Fatalf("unexpected current summary: %+v", view.WorkItems)
	}
	if view.WorkItems.ClosedCount != 2 || view.WorkItems.CompletedCount != 1 || view.WorkItems.CancelledCount != 1 {
		t.Fatalf("unexpected closed summary: %+v", view.WorkItems)
	}
	if len(view.WorkItems.CurrentItems) != 2 {
		t.Fatalf("closed or archived WorkItem leaked into current list: %+v", view.WorkItems.CurrentItems)
	}
	if len(view.WorkItems.AllCurrentItems) != 2 {
		t.Fatalf("closed or archived WorkItem leaked into all-current list: %+v", view.WorkItems.AllCurrentItems)
	}
	for _, item := range view.WorkItems.CurrentItems {
		if item.ID != active.ID && item.ID != paused.ID {
			t.Fatalf("unexpected current WorkItem: %+v", item)
		}
	}
	for _, item := range view.WorkItems.AllCurrentItems {
		if item.ID == active.ID && (item.Description == nil || *item.Description != description) {
			t.Fatalf("authoritative description was not projected: %+v", item)
		}
	}
}

func TestWorkItemSummaryUsesRecentCurrentDeterministicBoundedPolicy(t *testing.T) {
	base := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	items := []workitem.WorkItem{
		{ID: "old", Title: "Old", Lifecycle: workitem.LifecycleActive, UpdatedAt: base},
		{ID: "same-b", Title: "Same B", Lifecycle: workitem.LifecyclePaused, UpdatedAt: base.Add(2 * time.Hour)},
		{ID: "same-a", Title: "Same A", Lifecycle: workitem.LifecycleActive, UpdatedAt: base.Add(2 * time.Hour)},
		{ID: "middle", Title: "Middle", Lifecycle: workitem.LifecycleActive, UpdatedAt: base.Add(time.Hour)},
		{ID: "newest", Title: "Newest", Lifecycle: workitem.LifecyclePaused, UpdatedAt: base.Add(3 * time.Hour)},
		{ID: "closed", Title: "Closed", Lifecycle: workitem.LifecycleCompleted, UpdatedAt: base.Add(4 * time.Hour)},
	}
	planner := &recordingPlannerReader{view: &plannerview.View{}}
	view, err := NewService(planner, fixedWorkItemReader{items: items}, fixedTrackingSummaryReader{summaries: map[string]tracking.WorkItemSummary{}}).Get(context.Background(), testRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(view.WorkItems.CurrentItems))
	for _, item := range view.WorkItems.CurrentItems {
		got = append(got, item.ID)
	}
	want := []string{"newest", "same-a", "same-b", "middle"}
	if !reflect.DeepEqual(got, want) || view.WorkItems.CurrentCount != 5 {
		t.Fatalf("unexpected bounded current selection: got=%v count=%d want=%v", got, view.WorkItems.CurrentCount, want)
	}
	allGot := make([]string, 0, len(view.WorkItems.AllCurrentItems))
	for _, item := range view.WorkItems.AllCurrentItems {
		allGot = append(allGot, item.ID)
	}
	allWant := []string{"newest", "same-a", "same-b", "middle", "old"}
	if !reflect.DeepEqual(allGot, allWant) {
		t.Fatalf("unexpected complete current selection: got=%v want=%v", allGot, allWant)
	}
	if !reflect.DeepEqual(view.WorkItems.CurrentItems, view.WorkItems.AllCurrentItems[:currentWorkItemDisplayLimit]) {
		t.Fatalf("bounded CurrentItems must remain the prefix of AllCurrentItems")
	}
}

func TestServiceGetReturnsLegalEmptyWorkItemProjection(t *testing.T) {
	view, err := NewService(
		&recordingPlannerReader{view: &plannerview.View{}},
		fixedWorkItemReader{},
		fixedTrackingSummaryReader{summaries: map[string]tracking.WorkItemSummary{}},
	).Get(context.Background(), testRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if view.WorkItems.CurrentCount != 0 || view.WorkItems.ClosedCount != 0 || view.WorkItems.CurrentItems == nil || len(view.WorkItems.CurrentItems) != 0 || view.WorkItems.AllCurrentItems == nil || len(view.WorkItems.AllCurrentItems) != 0 {
		t.Fatalf("unexpected empty WorkItem projection: %+v", view.WorkItems)
	}
}

func TestCurrentWorkItemProjectionContainsNoScheduleFields(t *testing.T) {
	typeOfProjection := reflect.TypeOf(CurrentWorkItem{})
	for _, forbidden := range []string{"CreatedAt", "StartAt", "TargetAt", "DueAt", "PreviousTargetAt"} {
		if _, exists := typeOfProjection.FieldByName(forbidden); exists {
			t.Fatalf("Dashboard current WorkItem projection exposes schedule-like field %s", forbidden)
		}
	}
	if _, exists := typeOfProjection.FieldByName("Tracking"); !exists {
		t.Fatal("Dashboard current WorkItem projection is missing Tracking truth")
	}
}

func TestDashboardProjectsAuthoritativeTargetAndTrackingAvailability(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)
	target := &tracking.MonitorPoint{ID: "target", WorkItemID: "one", Subject: tracking.MonitorSubject{Kind: tracking.SubjectWorkItem, ID: "one"}, Kind: tracking.MonitorFinalTarget, State: tracking.MonitorActive, DueAt: now}
	service := NewService(
		&recordingPlannerReader{view: &plannerview.View{}},
		fixedWorkItemReader{items: []workitem.WorkItem{{ID: "one", Title: "One", Lifecycle: workitem.LifecycleActive}}},
		fixedTrackingSummaryReader{summaries: map[string]tracking.WorkItemSummary{"one": {WorkItemID: "one", TrackedItemCount: 2, ActiveFinalTarget: target}}},
	)
	view, err := service.Get(context.Background(), testRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if !view.WorkItems.TrackingAvailable || len(view.WorkItems.CurrentItems) != 1 || view.WorkItems.CurrentItems[0].Tracking.ActiveFinalTarget == nil || !view.WorkItems.CurrentItems[0].Tracking.ActiveFinalTarget.DueAt.Equal(now) {
		t.Fatalf("authoritative target was not projected: %+v", view.WorkItems)
	}

	service.tracking = fixedTrackingSummaryReader{err: errors.New("tracking unavailable")}
	view, err = service.Get(context.Background(), testRequest(t))
	if err != nil {
		t.Fatal(err)
	}
	if view.WorkItems.TrackingAvailable || len(view.WorkItems.CurrentItems) != 1 || view.WorkItems.CurrentItems[0].Tracking.WorkItemID != "one" {
		t.Fatalf("Tracking failure hid WorkItem identity or looked empty: %+v", view.WorkItems)
	}
}

func testRequest(t *testing.T) Request {
	t.Helper()
	today, err := temporal.ParseLocalDate("2026-09-18")
	if err != nil {
		t.Fatal(err)
	}
	tomorrow, err := temporal.ParseLocalDate("2026-09-19")
	if err != nil {
		t.Fatal(err)
	}
	return Request{Planner: plannerview.Request{
		RangeStart: today, RangeEnd: tomorrow, Today: today, Location: time.UTC,
	}}
}

func createWorkItem(t *testing.T, service *workitem.Service, title string) *workitem.WorkItem {
	t.Helper()
	item, err := service.CreateLocalWorkItem(context.Background(), workitem.CreateLocalWorkItemInput{Title: title})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func setLifecycle(t *testing.T, service *workitem.Service, id string, target workitem.LifecycleState) {
	t.Helper()
	if _, err := service.SetLocalWorkItemLifecycleState(context.Background(), workitem.SetLocalWorkItemLifecycleStateInput{ID: id, Target: target}); err != nil {
		t.Fatal(err)
	}
}

func archiveEntity(t *testing.T, database *sql.DB, id string) {
	t.Helper()
	if _, err := database.ExecContext(
		context.Background(),
		"UPDATE entities SET archived_at = ? WHERE id = ?",
		time.Now().UTC().Format(time.RFC3339Nano),
		id,
	); err != nil {
		t.Fatal(err)
	}
}
