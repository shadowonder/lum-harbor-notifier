package app

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"testing"
	"time"

	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/testsupport"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
	"lum-harbor-notifier/internal/workitemoverview"
)

func newWorkItemApp(t *testing.T) (*App, *workitem.Service, *sql.DB) {
	t.Helper()
	database := testsupport.OpenTestDatabase(t)
	repository := workitem.NewRepository(database.DB)
	service := workitem.NewService(database.DB, repository)
	trackingService := tracking.NewService(database.DB, service)
	attentionService := attention.NewService(database.DB, trackingService)
	plannerService := planner.NewService(database.DB, planner.NewRepository(database.DB))
	overviewService := workitemoverview.NewService(service, trackingService, attentionService, plannerService)
	return &App{workitemService: service, workItemOverviewService: overviewService, proposeRegistry: NewProposeRegistry()}, service, database.DB
}

func TestGetWorkItemsReturnsRealDTOsAndExcludesArchived(t *testing.T) {
	application, service, database := newWorkItemApp(t)
	ctx := context.Background()
	visible, err := service.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: "Production Release"})
	if err != nil {
		t.Fatal(err)
	}
	archived, err := service.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: "Archived Objective"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, "UPDATE entities SET archived_at = ? WHERE id = ?", time.Now().UTC().Format(time.RFC3339Nano), archived.ID); err != nil {
		t.Fatal(err)
	}

	items, err := application.GetWorkItems()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != visible.ID || items[0].Title != visible.Title {
		t.Fatalf("unexpected WorkItem DTO list: %+v", items)
	}
	if items[0].LifecycleState != "ACTIVE" || items[0].CreatedAt == "" || items[0].UpdatedAt == "" {
		t.Fatalf("missing DTO state: %+v", items[0])
	}
}

func TestWorkItemDTOMapsProductFields(t *testing.T) {
	created := time.Date(2026, 9, 18, 12, 0, 0, 123, time.UTC)
	updated := created.Add(time.Hour)
	completed := updated.Add(time.Hour)
	description := "Release outcome"
	dto := workItemToDTO(workitem.WorkItem{
		ID: "work-item-1", Title: "Production Release", Description: &description,
		Lifecycle: workitem.LifecycleCompleted, CompletedAt: &completed,
		LastRevisionID: "must-not-cross-wails", CreatedAt: created, UpdatedAt: updated,
	})
	if dto.ID != "work-item-1" || dto.Title != "Production Release" || dto.Description == nil || *dto.Description != description {
		t.Fatalf("unexpected DTO identity/content: %+v", dto)
	}
	if dto.LifecycleState != "COMPLETED" || dto.CompletedAt == nil || *dto.CompletedAt != completed.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected lifecycle mapping: %+v", dto)
	}
	if dto.CreatedAt != created.Format(time.RFC3339Nano) || dto.UpdatedAt != updated.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected timestamp mapping: %+v", dto)
	}
	if _, exposed := reflect.TypeOf(dto).FieldByName("LastRevisionID"); exposed {
		t.Fatal("WorkItemDTO must not expose persistence Revision identity")
	}
}

func TestGetWorkItemOverviewDistinguishesValidEmptySummaries(t *testing.T) {
	application, service, _ := newWorkItemApp(t)
	item, err := service.CreateLocalWorkItem(context.Background(), workitem.CreateLocalWorkItemInput{Title: "Overview"})
	if err != nil {
		t.Fatal(err)
	}
	view, err := application.GetWorkItemOverview()
	if err != nil {
		t.Fatal(err)
	}
	if !view.TrackingAvailable || !view.AttentionAvailable || len(view.Items) != 1 {
		t.Fatalf("unexpected overview availability: %+v", view)
	}
	got := view.Items[0]
	if got.WorkItem.ID != item.ID || got.Tracking.TrackedItemCount != 0 || got.Tracking.Workflow != nil || len(got.Tracking.Items) != 0 || got.Attention.ActionableIssueCount != 0 {
		t.Fatalf("valid empty summaries were not mapped: %+v", got)
	}
}

func TestOverviewFillProjectionUsesResolvedDimensionOnly(t *testing.T) {
	summary := tracking.WorkItemSummary{Items: []tracking.OverviewTrackedItem{
		{ID: "active", Lifecycle: tracking.ItemActive},
		{ID: "complete-progression", Lifecycle: tracking.ItemActive, Progression: tracking.Progression{Complete: true}},
		{ID: "cancelled", Lifecycle: tracking.ItemCancelled},
	}}
	dto := overviewTrackingToDTO(summary, map[string]workitemoverview.TrackedItemSignal{
		"active":               {State: workitemoverview.SignalNeutral},
		"complete-progression": {State: workitemoverview.SignalNeutral},
		"cancelled":            {State: workitemoverview.SignalNeutral},
	})
	if dto.Items[0].Resolved || !dto.Items[1].Resolved || !dto.Items[2].Resolved {
		t.Fatalf("resolved fill projection is wrong: %+v", dto.Items)
	}
}

func TestCreateWorkItemReturnsAuthoritativeCommittedDTO(t *testing.T) {
	application, service, _ := newWorkItemApp(t)
	description := "Coordinate release readiness"
	dto, err := application.CreateWorkItem(CreateWorkItemRequestDTO{
		Title: "  Production Release  ", Description: &description,
	})
	if err != nil {
		t.Fatal(err)
	}
	if dto.ID == "" || dto.Title != "Production Release" || dto.LifecycleState != "ACTIVE" || dto.CompletedAt != nil {
		t.Fatalf("unexpected authoritative created DTO: %+v", dto)
	}
	loaded, err := service.GetByID(context.Background(), dto.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != dto.ID || loaded.Title != dto.Title || string(loaded.Lifecycle) != dto.LifecycleState {
		t.Fatalf("DTO does not match committed domain state: dto=%+v domain=%+v", dto, loaded)
	}
}

func TestCreateWorkItemRejectsInvalidTitle(t *testing.T) {
	application, service, _ := newWorkItemApp(t)
	_, err := application.CreateWorkItem(CreateWorkItemRequestDTO{Title: "  \t "})
	if err == nil || !errors.Is(err, workitem.ErrInvalidInput) {
		t.Fatalf("expected WorkItem invalid-input error, got %v", err)
	}
	items, listErr := service.List(context.Background())
	if listErr != nil {
		t.Fatal(listErr)
	}
	if len(items) != 0 {
		t.Fatalf("invalid create persisted WorkItems: %+v", items)
	}
}

func TestCreateWorkItemRequestCannotControlBackendOwnedFields(t *testing.T) {
	typeOfRequest := reflect.TypeOf(CreateWorkItemRequestDTO{})
	for _, forbidden := range []string{"ID", "LastRevisionID", "LifecycleState", "CompletedAt"} {
		if _, exists := typeOfRequest.FieldByName(forbidden); exists {
			t.Fatalf("request exposes backend-owned field %s", forbidden)
		}
	}
}

func TestSetWorkItemLifecycleStateReturnsAuthoritativeCommittedDTO(t *testing.T) {
	application, service, _ := newWorkItemApp(t)
	created, err := service.CreateLocalWorkItem(context.Background(), workitem.CreateLocalWorkItemInput{Title: "Release"})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := application.SetWorkItemLifecycleState(SetWorkItemLifecycleStateRequestDTO{
		ID: created.ID, TargetLifecycleState: "COMPLETED",
	})
	if err != nil {
		t.Fatal(err)
	}
	if completed.ID != created.ID || completed.LifecycleState != "COMPLETED" || completed.CompletedAt == nil || completed.UpdatedAt == created.UpdatedAt.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected authoritative lifecycle DTO: %+v", completed)
	}
	loaded, err := service.GetByID(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(loaded.Lifecycle) != completed.LifecycleState || loaded.CompletedAt == nil || loaded.CompletedAt.Format(time.RFC3339Nano) != *completed.CompletedAt {
		t.Fatalf("DTO does not match committed lifecycle state: dto=%+v domain=%+v", completed, loaded)
	}
}

func TestSetWorkItemLifecycleStateRejectsInvalidTransition(t *testing.T) {
	application, service, _ := newWorkItemApp(t)
	created, err := service.CreateLocalWorkItem(context.Background(), workitem.CreateLocalWorkItemInput{Title: "Release"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetWorkItemLifecycleState(SetWorkItemLifecycleStateRequestDTO{
		ID: created.ID, TargetLifecycleState: "COMPLETED",
	}); err != nil {
		t.Fatal(err)
	}
	_, err = application.SetWorkItemLifecycleState(SetWorkItemLifecycleStateRequestDTO{
		ID: created.ID, TargetLifecycleState: "PAUSED",
	})
	if err == nil || !errors.Is(err, workitem.ErrInvalidLifecycleTransition) {
		t.Fatalf("expected safe transition error propagation, got %v", err)
	}
}

func TestSetWorkItemLifecycleStateRequestContainsOnlyTargetFields(t *testing.T) {
	typeOfRequest := reflect.TypeOf(SetWorkItemLifecycleStateRequestDTO{})
	if typeOfRequest.NumField() != 2 {
		t.Fatalf("unexpected lifecycle request fields: %v", typeOfRequest)
	}
	for _, required := range []string{"ID", "TargetLifecycleState"} {
		if _, exists := typeOfRequest.FieldByName(required); !exists {
			t.Fatalf("lifecycle request missing %s", required)
		}
	}
	for _, forbidden := range []string{"CompletedAt", "LastRevisionID", "UpdatedAt"} {
		if _, exists := typeOfRequest.FieldByName(forbidden); exists {
			t.Fatalf("lifecycle request exposes backend-owned field %s", forbidden)
		}
	}
}
