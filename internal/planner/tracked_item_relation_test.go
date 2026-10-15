package planner

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/temporal"
	"lum-harbor-notifier/internal/testsupport"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

type trackingTodoFixture struct {
	ctx      context.Context
	planner  *Service
	tracking *tracking.Service
	work     *workitem.Service
}

func newTrackingTodoFixture(t *testing.T) *trackingTodoFixture {
	t.Helper()
	db := testsupport.OpenTestDatabase(t).DB
	work := workitem.NewService(db, workitem.NewRepository(db))
	return &trackingTodoFixture{
		ctx:      context.Background(),
		planner:  NewService(db, NewRepository(db)),
		tracking: tracking.NewService(db, work),
		work:     work,
	}
}

func (f *trackingTodoFixture) trackedItem(t *testing.T, name string) (*workitem.WorkItem, *tracking.WorkflowDefinition, *tracking.Workflow, *tracking.TrackedItem) {
	t.Helper()
	wi, err := f.work.CreateLocalWorkItem(f.ctx, workitem.CreateLocalWorkItemInput{Title: name})
	if err != nil {
		t.Fatal(err)
	}
	def, err := f.tracking.CreateWorkflowDefinition(f.ctx, tracking.CreateWorkflowDefinitionInput{
		Name: name + " flow", Version: 1,
		Steps: []tracking.WorkflowStepInput{{Key: "work", Title: "Work"}, {Key: "done", Title: "Done", Terminal: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	wf, err := f.tracking.CreateWorkflow(f.ctx, tracking.CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID})
	if err != nil {
		t.Fatal(err)
	}
	item, err := f.tracking.CreateTrackedItem(f.ctx, tracking.CreateTrackedItemInput{WorkItemID: wi.ID, Title: name + " tracked"})
	if err != nil {
		t.Fatal(err)
	}
	return wi, def, wf, item
}

func TestTrackingTodoRelationValidationCardinalityAndCompletionInvariant(t *testing.T) {
	f := newTrackingTodoFixture(t)
	wi, def, wf, tracked := f.trackedItem(t, "Primary")
	otherWI, otherDef, _, _ := f.trackedItem(t, "Other")

	created, err := f.planner.CreateLocalTodo(f.ctx, CreateLocalTodoInput{
		Title: "Act on tracked item", WorkItemID: &wi.ID, WorkflowID: &wf.ID,
		WorkflowStepID: &def.Steps[0].ID, TrackedItemID: &tracked.ID,
	})
	if err != nil {
		t.Fatalf("valid relation: %v", err)
	}
	if created.RelatedEntityID == nil || *created.RelatedEntityID != tracked.ID {
		t.Fatalf("typed relation was not stored through related_entity_id: %+v", created)
	}

	second, err := f.planner.CreateLocalTodo(f.ctx, CreateLocalTodoInput{
		Title: "Second action", WorkItemID: &wi.ID, WorkflowID: &wf.ID, TrackedItemID: &tracked.ID,
	})
	if err != nil || second.RelatedEntityID == nil || *second.RelatedEntityID != tracked.ID {
		t.Fatalf("one TrackedItem must permit multiple Todos: item=%+v err=%v", second, err)
	}
	if _, err := f.planner.CreateLocalTodo(f.ctx, CreateLocalTodoInput{Title: "Unrelated"}); err != nil {
		t.Fatalf("unrelated Todo may omit tracked item: %v", err)
	}

	for name, input := range map[string]CreateLocalTodoInput{
		"wrong entity type": {Title: "Wrong", WorkItemID: &wi.ID, TrackedItemID: &wi.ID},
		"missing entity":    {Title: "Missing", WorkItemID: &wi.ID, TrackedItemID: ptrTestString("does-not-exist")},
		"cross work item":   {Title: "Cross", WorkItemID: &otherWI.ID, TrackedItemID: &tracked.ID},
		"wrong workflow step": {Title: "Step", WorkItemID: &wi.ID, WorkflowID: &wf.ID,
			WorkflowStepID: &otherDef.Steps[0].ID, TrackedItemID: &tracked.ID},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := f.planner.CreateLocalTodo(f.ctx, input); !errors.Is(err, ErrInvalidTrackedItem) {
				t.Fatalf("expected ErrInvalidTrackedItem, got %v", err)
			}
		})
	}

	before, err := f.tracking.GetTrackedItem(f.ctx, tracked.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.planner.CompleteLocalTodo(f.ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	after, err := f.tracking.GetTrackedItem(f.ctx, tracked.ID)
	if err != nil {
		t.Fatal(err)
	}
	if before.LastRevisionID != after.LastRevisionID || before.Lifecycle != after.Lifecycle || before.UpdatedAt != after.UpdatedAt {
		t.Fatalf("Todo completion mutated TrackedItem: before=%+v after=%+v", before, after)
	}
}

func TestTrackingTodoHistoryUsesTypedTrackedItemIdentity(t *testing.T) {
	f := newTrackingTodoFixture(t)
	wi, _, wf, tracked := f.trackedItem(t, "History")
	created, err := f.planner.CreateLocalTodo(f.ctx, CreateLocalTodoInput{
		Title: "History relation", WorkItemID: &wi.ID, WorkflowID: &wf.ID, TrackedItemID: &tracked.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	var afterJSON string
	if err := f.planner.db.QueryRowContext(f.ctx, "SELECT after_json FROM history_events WHERE subject_entity_id = ? AND event_type = 'PLANNER_ITEM_CREATED'", created.ID).Scan(&afterJSON); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(afterJSON, `"tracked_item_id":"`+tracked.ID+`"`) || strings.Contains(afterJSON, "related_entity_id") {
		t.Fatalf("history must expose typed tracked item identity: %s", afterJSON)
	}
}

func TestListOpenTrackingTodoDueTargetsFiltersAndNormalizes(t *testing.T) {
	f := newTrackingTodoFixture(t)
	wi, _, wf, tracked := f.trackedItem(t, "Health")
	dueAt := time.Date(2026, time.October, 8, 9, 30, 0, 0, time.UTC)
	dueDate, err := temporal.ParseLocalDate("2026-10-09")
	if err != nil {
		t.Fatal(err)
	}

	completed, err := f.planner.CreateLocalTodo(f.ctx, CreateLocalTodoInput{
		Title: "Completed", WorkItemID: &wi.ID, WorkflowID: &wf.ID, TrackedItemID: &tracked.ID, DueAt: &dueAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.planner.CompleteLocalTodo(f.ctx, completed.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.planner.CreateLocalTodo(f.ctx, CreateLocalTodoInput{Title: "Unrelated", DueAt: &dueAt}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.planner.CreateLocalTodo(f.ctx, CreateLocalTodoInput{
		Title: "No due", WorkItemID: &wi.ID, WorkflowID: &wf.ID, TrackedItemID: &tracked.ID,
	}); err != nil {
		t.Fatal(err)
	}
	exact, err := f.planner.CreateLocalTodo(f.ctx, CreateLocalTodoInput{
		Title: "Exact", WorkItemID: &wi.ID, WorkflowID: &wf.ID, TrackedItemID: &tracked.ID, DueAt: &dueAt,
	})
	if err != nil {
		t.Fatal(err)
	}
	dateOnly, err := f.planner.CreateLocalTodo(f.ctx, CreateLocalTodoInput{
		Title: "Date", WorkItemID: &wi.ID, WorkflowID: &wf.ID, TrackedItemID: &tracked.ID, DueDate: &dueDate,
	})
	if err != nil {
		t.Fatal(err)
	}

	targets, err := f.planner.ListOpenTrackingTodoDueTargets(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 2 {
		t.Fatalf("expected only two open linked actionable targets, got %+v", targets)
	}
	byID := map[string]TrackingTodoDueTarget{targets[0].PlannerItemID: targets[0], targets[1].PlannerItemID: targets[1]}
	if !byID[exact.ID].DueAt.Equal(dueAt) {
		t.Fatalf("exact due target changed: %+v", byID[exact.ID])
	}
	wantDateDue := time.Date(2026, time.October, 9, 23, 59, 59, 0, time.UTC)
	if !byID[dateOnly.ID].DueAt.Equal(wantDateDue) {
		t.Fatalf("date-only target mismatch: got %s want %s", byID[dateOnly.ID].DueAt, wantDateDue)
	}
}

func ptrTestString(value string) *string { return &value }
