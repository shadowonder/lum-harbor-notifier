package readcap_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/readcap"
	"lum-harbor-notifier/internal/testsupport"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

// mustEncode marshals v to the json.RawMessage Request.Input now expects,
// mirroring exactly what a future M16 caller would send on the wire — a
// plain JSON object, never a concrete Go input type.
func mustEncode(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type fixture struct {
	ctx       context.Context
	db        *sql.DB
	workItems *workitem.Service
	tracking  *tracking.Service
	planner   *planner.Service
	attention *attention.Service
	history   *history.Service
	readcap   *readcap.Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	database := testsupport.OpenTestDatabase(t)
	workItems := workitem.NewService(database.DB, workitem.NewRepository(database.DB))
	trackingSvc := tracking.NewService(database.DB, workItems)
	plannerSvc := planner.NewService(database.DB, planner.NewRepository(database.DB))
	attentionSvc := attention.NewService(database.DB, trackingSvc)
	historySvc := history.NewService(history.NewRepository(database.DB))

	service, err := readcap.NewService(workItems, trackingSvc, plannerSvc, database.DB, nil, attentionSvc, historySvc)
	if err != nil {
		t.Fatal(err)
	}

	return &fixture{
		ctx: context.Background(), db: database.DB,
		workItems: workItems, tracking: trackingSvc, planner: plannerSvc,
		attention: attentionSvc, history: historySvc, readcap: service,
	}
}

func (f *fixture) workItem(t *testing.T, title string) *workitem.WorkItem {
	t.Helper()
	item, err := f.workItems.CreateLocalWorkItem(f.ctx, workitem.CreateLocalWorkItemInput{Title: title})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func (f *fixture) workflowDefinition(t *testing.T, name string, keys ...string) *tracking.WorkflowDefinition {
	t.Helper()
	steps := make([]tracking.WorkflowStepInput, len(keys))
	for i, key := range keys {
		steps[i] = tracking.WorkflowStepInput{Key: key, Title: key, Terminal: i == len(keys)-1}
	}
	def, err := f.tracking.CreateWorkflowDefinition(f.ctx, tracking.CreateWorkflowDefinitionInput{Name: name, Version: 1, Steps: steps})
	if err != nil {
		t.Fatal(err)
	}
	return def
}

func (f *fixture) workflow(t *testing.T, workItemID, definitionID string) *tracking.Workflow {
	t.Helper()
	wf, err := f.tracking.CreateWorkflow(f.ctx, tracking.CreateWorkflowInput{
		WorkItemID: workItemID, WorkflowDefinitionID: definitionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return wf
}

func (f *fixture) trackedItem(t *testing.T, workItemID, workflowID, title string) *tracking.TrackedItem {
	t.Helper()
	item, err := f.tracking.CreateTrackedItem(f.ctx, tracking.CreateTrackedItemInput{
		WorkItemID: workItemID, WorkflowID: &workflowID, Title: title,
	})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func (f *fixture) localTodo(t *testing.T, title string) *planner.PlannerItem {
	t.Helper()
	item, err := f.planner.CreateLocalTodo(f.ctx, planner.CreateLocalTodoInput{Title: title})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func (f *fixture) externalPlannerItem(t *testing.T, system, externalID, title string) *planner.PlannerItem {
	t.Helper()
	item, err := f.planner.CreateExternalPlannerItem(f.ctx, planner.CreateExternalPlannerItemInput{
		System: system, ExternalID: externalID,
		Type: planner.ItemTypeReminder, Title: title, Status: planner.StatusOpen,
	})
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func (f *fixture) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := f.db.QueryRowContext(f.ctx, query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
