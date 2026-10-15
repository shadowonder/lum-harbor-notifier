package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/proposalcap"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

// aggregateFixture is existing truth for preparing every capability: a Work
// Item with a three-step Workflow, two Tracked Items and one Monitor Point,
// plus a bare Work Item without a Workflow.
type aggregateFixture struct {
	workItemID     string
	bareWorkItemID string
	workflowID     string
	apiItemID      string
	monitorPointID string
}

func newAggregateFixture(t *testing.T, application *App) aggregateFixture {
	t.Helper()
	ctx := context.Background()
	wi, err := application.workitemService.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: "Payments"})
	if err != nil {
		t.Fatal(err)
	}
	bare, err := application.workitemService.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: "Bare"})
	if err != nil {
		t.Fatal(err)
	}
	def, err := application.trackingService.CreateWorkflowDefinition(ctx, tracking.CreateWorkflowDefinitionInput{Name: "Fixture flow", Version: 1, Steps: []tracking.WorkflowStepInput{{Key: "plan", Title: "Plan"}, {Key: "build", Title: "Build"}, {Key: "ship", Title: "Ship", Terminal: true}}})
	if err != nil {
		t.Fatal(err)
	}
	wf, err := application.trackingService.CreateWorkflow(ctx, tracking.CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID})
	if err != nil {
		t.Fatal(err)
	}
	api, err := application.trackingService.CreateTrackedItem(ctx, tracking.CreateTrackedItemInput{WorkItemID: wi.ID, Title: "API"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.trackingService.CreateTrackedItem(ctx, tracking.CreateTrackedItemInput{WorkItemID: wi.ID, Title: "Schema"}); err != nil {
		t.Fatal(err)
	}
	completed := workitem.LifecycleCompleted
	mp, err := application.trackingService.CreateMonitorPoint(ctx, tracking.CreateMonitorPointInput{WorkItemID: wi.ID, Kind: tracking.MonitorKind("DEADLINE"), Title: "Launch", DueAt: time.Now().Add(72 * time.Hour), Timezone: "UTC", ExpectedWorkItemLifecycle: &completed})
	if err != nil {
		t.Fatal(err)
	}
	return aggregateFixture{workItemID: wi.ID, bareWorkItemID: bare.ID, workflowID: wf.ID, apiItemID: api.ID, monitorPointID: mp.ID}
}

// truthSnapshot counts every table a preparation or a Proposal could touch.
func truthSnapshot(t *testing.T, db *sql.DB) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, table := range []string{"entities", "revisions", "revision_sources", "work_items", "workflows", "workflow_definitions", "tracked_items", "tracked_item_relations", "planner_items", "todo_reminders", "monitor_points", "change_proposals", "change_proposal_sources", "proposal_operations", "proposal_operation_results", "history_events"} {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		out[table] = n
	}
	return out
}

func assertSnapshotUnchanged(t *testing.T, before, after map[string]int) {
	t.Helper()
	for table, n := range before {
		if after[table] != n {
			t.Fatalf("%s changed from %d to %d", table, n, after[table])
		}
	}
}

func call(capability, args string) proposalcap.Call {
	return proposalcap.Call{CapabilityID: capability, Arguments: json.RawMessage(args)}
}

func preparedPayload(t *testing.T, op proposal.PreparedOperation) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(op.PayloadJSON()), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPrepareOperationCoversEveryRegisteredCapabilityWithoutMutation(t *testing.T) {
	application, db := newRegistryTestAppWithDB(t)
	f := newAggregateFixture(t, application)
	ctx := context.Background()
	synced, err := application.plannerService.CreateExternalPlannerItem(ctx, planner.CreateExternalPlannerItemInput{
		System: "APPLE_REMINDER:fixture", ExternalID: "EK-fixture", Type: planner.ItemTypeReminder, Title: "Synced reminder", Status: planner.StatusOpen,
	})
	if err != nil {
		t.Fatal(err)
	}
	calls := map[string]string{
		proposal.OperationTypePlannerItemSetLocalNote:     fmt.Sprintf(`{"planner_item_id":%q,"local_note":"Gate code 1234"}`, synced.ID),
		proposal.OperationTypeWorkItemUpdateDetails:       fmt.Sprintf(`{"work_item_id":%q,"proposed_title":"Payments v2"}`, f.workItemID),
		proposal.OperationTypeTrackedItemCreate:           fmt.Sprintf(`{"work_item_id":%q,"title":"Ledger"}`, f.workItemID),
		proposal.OperationTypeTrackedItemUpdate:           fmt.Sprintf(`{"work_item_id":%q,"target_item_title":"API","proposed_title":"API v2"}`, f.workItemID),
		proposal.OperationTypeTrackedItemStageState:       fmt.Sprintf(`{"work_item_id":%q,"target_item_title":"API","stage_title":"Build","state":"IN_PROGRESS"}`, f.workItemID),
		proposal.OperationTypeTrackedItemArchive:          fmt.Sprintf(`{"work_item_id":%q,"target_item_title":"Schema"}`, f.workItemID),
		proposal.OperationTypeTrackedItemDependencyAdd:    fmt.Sprintf(`{"work_item_id":%q,"target_item_title":"API","related_item_title":"Schema"}`, f.workItemID),
		proposal.OperationTypeTrackedItemDependencyRemove: fmt.Sprintf(`{"work_item_id":%q,"target_item_title":"API","related_item_title":"Schema"}`, f.workItemID),
		proposal.OperationTypePlannerItemCreate:           `{"title":"Buy groceries","due_date":"2026-12-01","remind":true}`,
		proposal.OperationTypeWorkItemSetLifecycle:        fmt.Sprintf(`{"work_item_id":%q,"target_state":"PAUSED"}`, f.workItemID),
		proposal.OperationTypeWorkflowCreate:              fmt.Sprintf(`{"work_item_id":%q,"definition_name":"Bare flow","steps":["A","B"]}`, f.bareWorkItemID),
		proposal.OperationTypeWorkItemCreate:              `{"title":"Standalone"}`,
		proposal.OperationTypeWorkItemCreateWithWorkflow:  `{"title":"Stream","workflow_name":"Stream flow","stages":["Plan","Ship"]}`,
		proposal.OperationTypeMonitorPointCreate:          fmt.Sprintf(`{"work_item_id":%q,"title":"Checkpoint","due_date":"2026-12-01"}`, f.workItemID),
		proposal.OperationTypeMonitorPointSetState:        fmt.Sprintf(`{"monitor_point_id":%q,"target_state":"SATISFIED"}`, f.monitorPointID),
		proposal.OperationTypeWorkflowSetCurrentStep:      fmt.Sprintf(`{"work_item_id":%q,"step_title":"Build"}`, f.workItemID),
	}
	descriptors := application.proposeRegistry.ListDescriptors()
	if len(descriptors) != len(calls) {
		t.Fatalf("registry has %d capabilities, test covers %d", len(descriptors), len(calls))
	}
	before := truthSnapshot(t, db)
	for _, descriptor := range descriptors {
		args, ok := calls[descriptor.ID]
		if !ok {
			t.Fatalf("capability %s not covered", descriptor.ID)
		}
		prepared, err := application.proposeRegistry.PrepareOperation(ctx, application, descriptor.ID, nil, json.RawMessage(args))
		if err != nil {
			t.Fatalf("%s: prepare: %v", descriptor.ID, err)
		}
		if prepared.OperationType() != descriptor.ID || !json.Valid([]byte(prepared.PayloadJSON())) || prepared.Title() == "" {
			t.Fatalf("%s: unexpected prepared operation %+v", descriptor.ID, prepared)
		}
		if _, err := proposal.PreflightAggregatePlan([]proposal.PreparedOperation{prepared}); err != nil {
			t.Fatalf("%s: single-operation preflight: %v", descriptor.ID, err)
		}
	}
	assertSnapshotUnchanged(t, before, truthSnapshot(t, db))
}

func TestSingularExecuteNeverAdmitsSymbolicReferences(t *testing.T) {
	application := newRegistryTestApp(t)
	ctx := context.Background()
	if _, err := application.proposeRegistry.Execute(ctx, application, proposal.OperationTypeTrackedItemCreate, nil, json.RawMessage(`{"work_item_id":"$op[0].work_item_id","title":"API"}`), nil); err == nil {
		t.Fatal("a singular Proposal must treat a reference-shaped ID as a literal (unknown) Work Item")
	}
	// Planner keeps its existing behavior: an unresolvable work_item_id is dropped.
	created, err := application.proposeRegistry.Execute(ctx, application, proposal.OperationTypePlannerItemCreate, nil, json.RawMessage(`{"title":"Plain todo","work_item_id":"$op[0].work_item_id"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if created.IsAggregate() || created.PlannerItemPayload.WorkItemID != nil || created.TargetEntityID != nil {
		t.Fatalf("singular planner create changed behavior: %+v", created.PlannerItemPayload)
	}
}

func TestPrepareAggregatePlanReferenceContract(t *testing.T) {
	application, db := newRegistryTestAppWithDB(t)
	f := newAggregateFixture(t, application)
	ctx := context.Background()
	stream := call(proposal.OperationTypeWorkItemCreateWithWorkflow, `{"title":"Stream","workflow_name":"Stream flow","stages":["Plan","Ship"]}`)

	t.Run("backward reference in declared fields with derived companions", func(t *testing.T) {
		before := truthSnapshot(t, db)
		plan, err := application.proposeRegistry.PrepareAggregatePlan(ctx, application, nil, []proposalcap.Call{
			stream,
			call(proposal.OperationTypeTrackedItemCreate, `{"work_item_id":"$op[0].work_item_id","title":"$op[0].work_item_id"}`),
			call(proposal.OperationTypePlannerItemCreate, `{"title":"Review $op[0].work_item_id","tracked_item_id":"$op[1].tracked_item_id","due_date":"2026-12-01"}`),
		})
		if err != nil {
			t.Fatal(err)
		}
		tracked := preparedPayload(t, plan[1])
		if tracked["work_item_id"] != "$op[0].work_item_id" || tracked["workflow_id"] != "$op[0].workflow_id" || plan[1].TargetEntityID() != nil {
			t.Fatalf("tracked child: %v target=%v", tracked, plan[1].TargetEntityID())
		}
		if tracked["title"] != "$op[0].work_item_id" {
			t.Fatalf("a title is literal text even when it looks like a reference: %v", tracked["title"])
		}
		todo := preparedPayload(t, plan[2])
		if todo["title"] != "Review $op[0].work_item_id" || todo["tracked_item_id"] != "$op[1].tracked_item_id" {
			t.Fatalf("planner child: %v", todo)
		}
		if todo["work_item_id"] != "$op[0].work_item_id" || todo["workflow_id"] != "$op[0].workflow_id" {
			t.Fatalf("planner child must carry the referenced TrackedItem's provenance: %v", todo)
		}
		if offsets, _ := todo["offsets_seconds"].([]any); len(offsets) != 3 {
			t.Fatalf("tracking-linked due Todo keeps the default reminder thresholds: %v", todo["offsets_seconds"])
		}
		assertSnapshotUnchanged(t, before, truthSnapshot(t, db))
	})

	rejected := map[string][]proposalcap.Call{
		"reference in a non-declared ID field":      {stream, call(proposal.OperationTypeWorkItemSetLifecycle, `{"work_item_id":"$op[0].work_item_id","target_state":"PAUSED"}`)},
		"reference in a non-declared tracked field": {stream, call(proposal.OperationTypeTrackedItemUpdate, `{"work_item_id":"$op[0].work_item_id","target_item_title":"API","proposed_title":"X"}`)},
		"malformed reference":                       {stream, call(proposal.OperationTypeTrackedItemCreate, `{"work_item_id":"$op[x].work_item_id","title":"A"}`)},
		"expression is not a reference":             {stream, call(proposal.OperationTypePlannerItemCreate, `{"title":"A","work_item_id":"$op[0].work_item_id + 1"}`)},
		"forward reference":                         {call(proposal.OperationTypeTrackedItemCreate, `{"work_item_id":"$op[1].work_item_id","title":"A"}`), stream},
		"self reference":                            {call(proposal.OperationTypePlannerItemCreate, `{"title":"A","work_item_id":"$op[0].work_item_id"}`)},
		"incompatible output type":                  {stream, call(proposal.OperationTypePlannerItemCreate, `{"title":"A","tracked_item_id":"$op[0].work_item_id"}`)},
		"referenced operation lacks output":         {call(proposal.OperationTypeWorkItemCreate, `{"title":"No flow"}`), call(proposal.OperationTypeTrackedItemCreate, `{"work_item_id":"$op[0].work_item_id","title":"A"}`)},
		"parent title in new work item":             {stream, call(proposal.OperationTypeTrackedItemCreate, `{"work_item_id":"$op[0].work_item_id","title":"A","parent_item_title":"B"}`)},
		"nested aggregate":                          {call(proposal.OperationTypeAggregate, `{}`)},
		"unknown capability":                        {call("not.a.capability", `{}`)},
		"invalid arguments":                         {call(proposal.OperationTypeWorkItemCreate, `{"title":42}`)},
		"unknown concrete target":                   {call(proposal.OperationTypeTrackedItemCreate, `{"work_item_id":"missing-work-item","title":"A"}`)},
		"unknown concrete tracked item":             {call(proposal.OperationTypePlannerItemCreate, `{"title":"A","tracked_item_id":"missing-tracked-item"}`)},
		"zero operations":                           {},
		"state conflict within plan": {
			call(proposal.OperationTypeWorkItemSetLifecycle, fmt.Sprintf(`{"work_item_id":%q,"target_state":"PAUSED"}`, f.workItemID)),
			call(proposal.OperationTypeWorkItemUpdateDetails, fmt.Sprintf(`{"work_item_id":%q,"proposed_title":"Renamed"}`, f.workItemID)),
		},
		"tracked item state conflict within plan": {
			call(proposal.OperationTypeTrackedItemUpdate, fmt.Sprintf(`{"work_item_id":%q,"target_item_title":"API","proposed_title":"API v2"}`, f.workItemID)),
			call(proposal.OperationTypeTrackedItemStageState, fmt.Sprintf(`{"work_item_id":%q,"target_item_title":"API","stage_title":"Build","state":"BLOCKED"}`, f.workItemID)),
		},
		"second workflow for one work item": {
			call(proposal.OperationTypeWorkflowCreate, fmt.Sprintf(`{"work_item_id":%q,"definition_name":"One","steps":["A"]}`, f.bareWorkItemID)),
			call(proposal.OperationTypeWorkflowCreate, fmt.Sprintf(`{"work_item_id":%q,"definition_name":"Two","steps":["A"]}`, f.bareWorkItemID)),
		},
	}
	for name, calls := range rejected {
		t.Run("rejects "+name, func(t *testing.T) {
			before := truthSnapshot(t, db)
			_, err := application.proposeRegistry.PrepareAggregatePlan(ctx, application, nil, calls)
			if err == nil {
				t.Fatal("expected preflight rejection")
			}
			t.Log(err)
			assertSnapshotUnchanged(t, before, truthSnapshot(t, db))
		})
	}

	t.Run("a non-conflicting change order is accepted", func(t *testing.T) {
		if _, err := application.proposeRegistry.PrepareAggregatePlan(ctx, application, nil, []proposalcap.Call{
			call(proposal.OperationTypeWorkItemUpdateDetails, fmt.Sprintf(`{"work_item_id":%q,"proposed_title":"Renamed"}`, f.workItemID)),
			call(proposal.OperationTypeWorkItemSetLifecycle, fmt.Sprintf(`{"work_item_id":%q,"target_state":"PAUSED"}`, f.workItemID)),
			call(proposal.OperationTypeTrackedItemDependencyAdd, fmt.Sprintf(`{"work_item_id":%q,"target_item_title":"API","related_item_title":"Schema"}`, f.workItemID)),
			call(proposal.OperationTypeTrackedItemStageState, fmt.Sprintf(`{"work_item_id":%q,"target_item_title":"API","stage_title":"Build","state":"IN_PROGRESS"}`, f.workItemID)),
		}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestPrepareAggregatePlanOperationLimit(t *testing.T) {
	application := newRegistryTestApp(t)
	ctx := context.Background()
	plan := func(n int) []proposalcap.Call {
		calls := make([]proposalcap.Call, n)
		for i := range calls {
			calls[i] = call(proposal.OperationTypePlannerItemCreate, fmt.Sprintf(`{"title":"Todo %d"}`, i))
		}
		return calls
	}
	if prepared, err := application.proposeRegistry.PrepareAggregatePlan(ctx, application, nil, plan(proposal.MaxAggregateOperations)); err != nil || len(prepared) != 20 {
		t.Fatalf("20 operations must be accepted: %d %v", len(prepared), err)
	}
	if _, err := application.proposeRegistry.PrepareAggregatePlan(ctx, application, nil, plan(proposal.MaxAggregateOperations+1)); !errors.Is(err, proposal.ErrInvalidAggregatePlan) {
		t.Fatalf("21 operations must be rejected: %v", err)
	}
	if _, err := application.proposeRegistry.PrepareAggregatePlan(ctx, application, nil, nil); !errors.Is(err, proposal.ErrInvalidAggregatePlan) || !strings.Contains(err.Error(), "0 operations") {
		t.Fatalf("zero operations must be rejected: %v", err)
	}
}
