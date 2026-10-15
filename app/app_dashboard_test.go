package app

import (
	"context"
	"database/sql"
	"reflect"
	"testing"
	"time"

	"lum-harbor-notifier/internal/calendar"
	"lum-harbor-notifier/internal/dashboardview"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/plannerview"
	"lum-harbor-notifier/internal/testsupport"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

func TestGetDashboardViewReturnsPlannerAndAuthoritativeWorkItemDTOs(t *testing.T) {
	application, plannerService, workItemService, database := newDashboardApp(t)
	ctx := context.Background()
	if _, err := plannerService.CreateLocalTodo(ctx, planner.CreateLocalTodoInput{Title: "No-date current action"}); err != nil {
		t.Fatal(err)
	}
	description := "Ship the authoritative release"
	active, err := workItemService.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: "Active release", Description: &description})
	if err != nil {
		t.Fatal(err)
	}
	paused, err := workItemService.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: "Paused migration"})
	if err != nil {
		t.Fatal(err)
	}
	closed, err := workItemService.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: "Completed rollout"})
	if err != nil {
		t.Fatal(err)
	}
	archived, err := workItemService.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: "Archived"})
	if err != nil {
		t.Fatal(err)
	}
	setDashboardLifecycle(t, workItemService, paused.ID, workitem.LifecyclePaused)
	setDashboardLifecycle(t, workItemService, closed.ID, workitem.LifecycleCompleted)
	if _, err := database.ExecContext(ctx, "UPDATE entities SET archived_at = ? WHERE id = ?", time.Now().UTC().Format(time.RFC3339Nano), archived.ID); err != nil {
		t.Fatal(err)
	}

	view, err := application.GetDashboardView(testDashboardRequest())
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Planner.CurrentOpenItems) != 1 || view.Planner.CurrentOpenItems[0].Title != "No-date current action" {
		t.Fatalf("Dashboard did not preserve Planner current-open semantics: %+v", view.Planner)
	}
	if view.WorkItems.CurrentCount != 2 || view.WorkItems.PausedCount != 1 || view.WorkItems.ClosedCount != 1 || view.WorkItems.CompletedCount != 1 {
		t.Fatalf("unexpected WorkItem summary DTO: %+v", view.WorkItems)
	}
	if len(view.WorkItems.CurrentItems) != 2 {
		t.Fatalf("closed or archived WorkItem leaked into DTO list: %+v", view.WorkItems.CurrentItems)
	}
	if len(view.WorkItems.AllCurrentItems) != 2 {
		t.Fatalf("closed or archived WorkItem leaked into all-current DTO list: %+v", view.WorkItems.AllCurrentItems)
	}
	ids := map[string]bool{}
	for _, item := range view.WorkItems.CurrentItems {
		ids[item.ID] = true
		if item.Title == "" || item.UpdatedAt == "" {
			t.Fatalf("incomplete current WorkItem DTO: %+v", item)
		}
	}
	if !ids[active.ID] || !ids[paused.ID] {
		t.Fatalf("missing current WorkItem DTOs: %+v", view.WorkItems.CurrentItems)
	}
	for _, item := range view.WorkItems.AllCurrentItems {
		if item.ID == active.ID && (item.Description == nil || *item.Description != description) {
			t.Fatalf("authoritative description missing from DTO: %+v", item)
		}
	}
}

func TestGetDashboardViewReturnsLegalEmptyWorkItemDTO(t *testing.T) {
	application, _, _, _ := newDashboardApp(t)
	view, err := application.GetDashboardView(testDashboardRequest())
	if err != nil {
		t.Fatal(err)
	}
	if view.WorkItems.CurrentCount != 0 || view.WorkItems.ClosedCount != 0 || view.WorkItems.CurrentItems == nil || len(view.WorkItems.CurrentItems) != 0 || view.WorkItems.AllCurrentItems == nil || len(view.WorkItems.AllCurrentItems) != 0 {
		t.Fatalf("unexpected empty Dashboard WorkItem DTO: %+v", view.WorkItems)
	}
}

func TestDashboardCurrentWorkItemDTOExposesOnlyProductSummaryFields(t *testing.T) {
	typeOfDTO := reflect.TypeOf(DashboardCurrentWorkItemDTO{})
	if typeOfDTO.NumField() != 7 {
		t.Fatalf("unexpected Dashboard WorkItem DTO fields: %v", typeOfDTO)
	}
	for _, required := range []string{"ID", "Title", "Description", "LifecycleState", "UpdatedAt", "Tracking", "WorkflowStartedAt"} {
		if _, exists := typeOfDTO.FieldByName(required); !exists {
			t.Fatalf("Dashboard WorkItem DTO missing %s", required)
		}
	}
	for _, forbidden := range []string{"CompletedAt", "LastRevisionID", "CreatedAt", "StartAt", "TargetAt", "Entity"} {
		if _, exists := typeOfDTO.FieldByName(forbidden); exists {
			t.Fatalf("Dashboard WorkItem DTO exposes %s", forbidden)
		}
	}
}

func newDashboardApp(t *testing.T) (*App, *planner.Service, *workitem.Service, *sql.DB) {
	t.Helper()
	database := testsupport.OpenTestDatabase(t)
	plannerService := planner.NewService(database.DB, planner.NewRepository(database.DB))
	plannerViewService := plannerview.NewService(plannerService, calendar.NewRepository(database.DB))
	workItemService := workitem.NewService(database.DB, workitem.NewRepository(database.DB))
	trackingService := tracking.NewService(database.DB, workItemService)
	dashboardViewService := dashboardview.NewService(plannerViewService, workItemService, trackingService)
	return &App{dashboardViewService: dashboardViewService}, plannerService, workItemService, database.DB
}

func setDashboardLifecycle(t *testing.T, service *workitem.Service, id string, target workitem.LifecycleState) {
	t.Helper()
	if _, err := service.SetLocalWorkItemLifecycleState(context.Background(), workitem.SetLocalWorkItemLifecycleStateInput{ID: id, Target: target}); err != nil {
		t.Fatal(err)
	}
}

func testDashboardRequest() DashboardViewRequestDTO {
	return DashboardViewRequestDTO{
		RangeStart: "2026-09-18",
		RangeEnd:   "2026-09-19",
		Today:      "2026-09-18",
		Timezone:   "America/Chicago",
	}
}

func TestDashboardExposesAuthoritativeWorkflowStartAndActiveFinalTargetWithoutNPlusOne(t *testing.T) {
	application, _, workItemService, database := newDashboardApp(t)
	ctx := context.Background()
	trackingService := tracking.NewService(database, workItemService)
	def, err := trackingService.CreateWorkflowDefinition(ctx, tracking.CreateWorkflowDefinitionInput{Name: "Flow", Version: 1, Steps: []tracking.WorkflowStepInput{{Key: "a", Title: "A"}, {Key: "b", Title: "B", Terminal: true}}})
	if err != nil {
		t.Fatal(err)
	}
	withTarget, _ := workItemService.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: "With target"})
	noTarget, _ := workItemService.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: "No target"})
	noWorkflow, _ := workItemService.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: "No workflow"})
	started := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	for _, id := range []string{withTarget.ID, noTarget.ID} {
		if _, err := trackingService.CreateWorkflow(ctx, tracking.CreateWorkflowInput{WorkItemID: id, WorkflowDefinitionID: def.ID, StartedAt: &started}); err != nil {
			t.Fatal(err)
		}
	}
	lifecycle := workitem.LifecycleCompleted
	target := time.Date(2026, 10, 15, 17, 0, 0, 0, time.UTC)
	if _, err := trackingService.CreateMonitorPoint(ctx, tracking.CreateMonitorPointInput{WorkItemID: withTarget.ID, Kind: tracking.MonitorFinalTarget, Title: "Launch", DueAt: target, Timezone: "UTC", ExpectedWorkItemLifecycle: &lifecycle}); err != nil {
		t.Fatal(err)
	}
	// A satisfied/cancelled FINAL_TARGET must never be the current end.
	old, err := trackingService.CreateMonitorPoint(ctx, tracking.CreateMonitorPointInput{WorkItemID: noTarget.ID, Kind: tracking.MonitorFinalTarget, Title: "Old", DueAt: target, Timezone: "UTC", ExpectedWorkItemLifecycle: &lifecycle})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := trackingService.SetMonitorPointState(ctx, tracking.SetMonitorPointStateInput{ID: old.ID, Target: tracking.MonitorCancelled}); err != nil {
		t.Fatal(err)
	}

	view, err := application.GetDashboardView(testDashboardRequest())
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]DashboardCurrentWorkItemDTO{}
	for _, item := range view.WorkItems.AllCurrentItems {
		byID[item.ID] = item
	}
	a := byID[withTarget.ID]
	if a.WorkflowStartedAt == nil || *a.WorkflowStartedAt != "2026-08-20T09:00:00Z" || a.Tracking.ActiveFinalTarget == nil || a.Tracking.ActiveFinalTarget.DueAt != "2026-10-15T17:00:00Z" {
		t.Fatalf("span start/end must be Workflow.startedAt and the ACTIVE FINAL_TARGET: %+v", a)
	}
	b := byID[noTarget.ID]
	if b.WorkflowStartedAt == nil || b.Tracking.ActiveFinalTarget != nil {
		t.Fatalf("a cancelled FINAL_TARGET must not be an end: %+v", b)
	}
	if c := byID[noWorkflow.ID]; c.WorkflowStartedAt != nil || c.Tracking.ActiveFinalTarget != nil {
		t.Fatalf("a WorkItem without a Workflow has no span: %+v", c)
	}
}
