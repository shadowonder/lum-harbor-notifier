package app

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

func newTrackingApp(t *testing.T) (*App, *workitem.WorkItem, string) {
	t.Helper()
	application, service, db := newWorkItemApp(t)
	application.trackingService = tracking.NewService(db, service)
	item, err := service.CreateLocalWorkItem(context.Background(), workitem.CreateLocalWorkItemInput{Title: "Release"})
	if err != nil {
		t.Fatal(err)
	}
	return application, item, "unused"
}

func TestGetWorkItemTrackingDistinguishesEmptyFromUnavailableAtTheBoundary(t *testing.T) {
	application, item, _ := newTrackingApp(t)
	empty, err := application.GetWorkItemTracking(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if empty.WorkItemID != item.ID || empty.TrackedItems == nil || empty.Workflow != nil || empty.MonitorPoints == nil ||
		len(empty.TrackedItems)+len(empty.MonitorPoints) != 0 {
		t.Fatalf("a valid WorkItem must succeed with non-nil empty collections: %+v", empty)
	}
	if _, err := application.GetWorkItemTracking("unknown"); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("unknown WorkItem must be an error, got %v", err)
	}
}

func TestTrackingBoundaryEndToEndWithRealTruth(t *testing.T) {
	application, item, _ := newTrackingApp(t)
	def, err := application.CreateWorkflowDefinition(CreateWorkflowDefinitionRequestDTO{
		Name: "Release", Version: 1, Steps: []WorkflowStepRequestDTO{{Key: "plan", Title: "Plan"}, {Key: "ship", Title: "Ship", Terminal: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	active, err := application.ListActiveWorkflowDefinitions()
	if err != nil || len(active) != 1 || active[0].ID != def.ID || len(active[0].Steps) != 2 || active[0].Steps[1].Key != "ship" {
		t.Fatalf("active definitions: %+v err=%v", active, err)
	}
	wf, err := application.CreateWorkflow(CreateWorkflowRequestDTO{WorkItemID: item.ID, WorkflowDefinitionID: def.ID})
	if err != nil {
		t.Fatal(err)
	}
	root, err := application.CreateTrackedItem(CreateTrackedItemRequestDTO{WorkItemID: item.ID, Title: "API"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := application.CreateTrackedItem(CreateTrackedItemRequestDTO{WorkItemID: item.ID, Title: "Docs", ParentItemID: &root.ID})
	if err != nil {
		t.Fatal(err)
	}
	state, err := application.SetTrackedItemStepState(SetTrackedItemStepStateRequestDTO{TrackedItemID: root.ID, WorkflowStepID: def.Steps[0].ID, State: "COMPLETED"})
	if err != nil || state.State != "COMPLETED" || state.CompletedAt == nil {
		t.Fatalf("state: %+v err=%v", state, err)
	}
	lifecycle := "COMPLETED"
	wiMonitor, err := application.CreateMonitorPoint(CreateMonitorPointRequestDTO{
		WorkItemID: item.ID, Kind: "FINAL_TARGET", Title: "Ship", DueAt: "2026-10-15T17:00:00-05:00", Timezone: "America/Chicago", ExpectedWorkItemLifecycle: &lifecycle,
	})
	if err != nil {
		t.Fatal(err)
	}
	itemMonitor, err := application.CreateMonitorPoint(CreateMonitorPointRequestDTO{
		WorkItemID: item.ID, SubjectTrackedItemID: &root.ID, Kind: "CHECKPOINT", Title: "API shipped", DueAt: "2026-10-01T00:00:00Z", Timezone: "UTC", ExpectedStepID: &def.Steps[1].ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if wiMonitor.SubjectKind != "WORK_ITEM" || wiMonitor.SubjectID != item.ID || wiMonitor.ExpectationKind != "WORK_ITEM_LIFECYCLE" || *wiMonitor.ExpectedWorkItemLifecycle != "COMPLETED" ||
		wiMonitor.State != "ACTIVE" || wiMonitor.DueAt != "2026-10-15T22:00:00Z" {
		t.Fatalf("WorkItem monitor DTO: %+v", wiMonitor)
	}

	view, err := application.GetWorkItemTracking(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.TrackedItems) != 2 || view.TrackedItems[0].ID != root.ID || view.TrackedItems[1].ID != child.ID || *view.TrackedItems[1].ParentItemID != root.ID {
		t.Fatalf("hierarchy: %+v", view.TrackedItems)
	}
	p := view.TrackedItems[0].Progression
	if p == nil || p.PassedCount != 1 || p.ProgressPercent != 50 || p.CurrentStepKey == nil || *p.CurrentStepKey != "ship" || *p.CurrentState != "NOT_STARTED" || p.Complete {
		t.Fatalf("progression must be backend-derived: %+v", p)
	}
	pChild := view.TrackedItems[1].Progression
	if pChild == nil || pChild.PassedCount != 0 || len(view.TrackedItems[1].StepStates) != 2 {
		t.Fatalf("child item progression: %+v", view.TrackedItems[1])
	}
	if view.Workflow == nil || view.Workflow.ID != wf.ID || view.Workflow.Definition.ID != def.ID || len(view.MonitorPoints) != 2 || view.MonitorPoints[0].ID != itemMonitor.ID || view.MonitorPoints[1].ID != wiMonitor.ID {
		t.Fatalf("workflow/monitors: %+v", view)
	}

	done, err := application.SetMonitorPointState(SetMonitorPointStateRequestDTO{ID: itemMonitor.ID, TargetState: "SATISFIED"})
	if err != nil || done.State != "SATISFIED" || done.SatisfiedAt == nil {
		t.Fatalf("satisfy: %+v err=%v", done, err)
	}
}

func TestTrackingBoundaryRejectsUnsafeOrInvalidRequestsSafely(t *testing.T) {
	application, item, _ := newTrackingApp(t)
	lifecycle := "COMPLETED"
	if _, err := application.CreateMonitorPoint(CreateMonitorPointRequestDTO{WorkItemID: item.ID, Kind: "CHECKPOINT", Title: "t", DueAt: "tomorrow", Timezone: "UTC", ExpectedWorkItemLifecycle: &lifecycle}); err == nil {
		t.Fatal("a non-RFC3339 due time must be rejected")
	}
	if _, err := application.CreateMonitorPoint(CreateMonitorPointRequestDTO{WorkItemID: "nope", Kind: "CHECKPOINT", Title: "t", DueAt: time.Now().Format(time.RFC3339), Timezone: "UTC", ExpectedWorkItemLifecycle: &lifecycle}); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("unknown WorkItem: %v", err)
	}
	if _, err := application.SetTrackedItemStepState(SetTrackedItemStepStateRequestDTO{TrackedItemID: "x", WorkflowStepID: "y", State: "DONE"}); err == nil {
		t.Fatal("invalid state must be rejected")
	}
	if _, err := application.SetMonitorPointState(SetMonitorPointStateRequestDTO{ID: "missing", TargetState: "SATISFIED"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("got %v", err)
	}
	// Uninitialized service fails honestly, never panics.
	bare := &App{}
	if _, err := bare.GetWorkItemTracking("x"); err == nil {
		t.Fatal("uninitialized tracking must fail")
	}
}

// Requests cannot carry authority the backend owns; responses carry no
// persistence internals, no revision IDs, and no display state.
func TestTrackingDTOsCarryNoAuthorityOrPresentationFields(t *testing.T) {
	requests := []any{CreateWorkflowDefinitionRequestDTO{}, WorkflowStepRequestDTO{}, CreateWorkflowRequestDTO{}, SetWorkflowCurrentStepRequestDTO{}, CreateTrackedItemRequestDTO{}, SetTrackedItemStepStateRequestDTO{}, CreateMonitorPointRequestDTO{}, SetMonitorPointStateRequestDTO{}}
	for _, dto := range requests {
		for _, field := range jsonFields(t, dto) {
			for _, banned := range []string{"revision", "createdat", "updatedat", "satisfiedat", "completedat", "enteredat", "currentstep", "entityid", "sql", "state"} {
				allowed := (reflect.TypeOf(dto).Name() == "SetTrackedItemStepStateRequestDTO" && field == "state") ||
					(reflect.TypeOf(dto).Name() == "SetMonitorPointStateRequestDTO" && field == "targetState") ||
					// A Workflow's own current step is an explicit authoritative input, never derived by the client.
					(reflect.TypeOf(dto).Name() == "CreateWorkflowRequestDTO" && field == "currentStepId")
				if strings.Contains(strings.ToLower(field), banned) && !allowed {
					t.Fatalf("%T must not accept %q", dto, field)
				}
			}
		}
	}
	responses := []any{WorkItemTrackingDTO{}, WorkflowDTO{}, TrackedItemDTO{}, StepStateDTO{}, ProgressionDTO{}, MonitorPointDTO{}, WorkflowDefinitionDTO{}, WorkflowStepDTO{}}
	for _, dto := range responses {
		for _, field := range jsonFields(t, dto) {
			for _, banned := range []string{"revision", "color", "badge", "css", "icon", "label", "variant", "coordinate", "overdue", "risk"} {
				if strings.Contains(strings.ToLower(field), banned) {
					t.Fatalf("%T exposes %q", dto, field)
				}
			}
		}
	}
}

func jsonFields(t *testing.T, dto any) []string {
	t.Helper()
	raw, err := json.Marshal(dto)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	return names
}

func TestHumanTrackingCommandsDoNotTouchWorkItemContent(t *testing.T) {
	application, item, _ := newTrackingApp(t)
	before, _ := application.GetWorkItems()
	def, err := application.CreateWorkflowDefinition(CreateWorkflowDefinitionRequestDTO{Name: "W", Version: 1, Steps: []WorkflowStepRequestDTO{{Key: "a", Title: "A"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.CreateWorkflow(CreateWorkflowRequestDTO{WorkItemID: item.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	ti, err := application.CreateTrackedItem(CreateTrackedItemRequestDTO{WorkItemID: item.ID, Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetTrackedItemStepState(SetTrackedItemStepStateRequestDTO{TrackedItemID: ti.ID, WorkflowStepID: def.Steps[0].ID, State: "IN_PROGRESS"}); err != nil {
		t.Fatal(err)
	}
	after, _ := application.GetWorkItems()
	if len(before) != 1 || len(after) != 1 || before[0] != after[0] {
		t.Fatalf("WorkItem DTO changed: %+v vs %+v", before, after)
	}
}

// TestAssistantPathHasNoDirectTrackingMutationAccess checks both the generic
// dispatch entrypoint (assistant.go) and the capability registry that now
// owns Tracking handler wiring (propose_registry.go, since Round 31's generic
// capability cutover moved capability-specific dispatch there) - neither may
// call Tracking mutation directly, and Tracking intent must still go through
// Proposal creation.
func TestAssistantPathHasNoDirectTrackingMutationAccess(t *testing.T) {
	combined := ""
	for _, file := range []string{"assistant.go", "propose_registry.go"} {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		combined += string(source)
	}
	for _, banned := range []string{"CreateTrackedItem(", "UpdateTrackedItem(", "ArchiveTrackedItem(", "AddDependency(", "RemoveDependency(", "SetTrackedItemStageState("} {
		if strings.Contains(combined, banned) {
			t.Fatalf("the Assistant application path must not call Tracking mutation directly (%s)", banned)
		}
	}
	if !strings.Contains(combined, "ProposeTrackedItemOperation") {
		t.Fatal("Assistant must hand Tracking intent to Proposal creation")
	}
}

func TestWorkflowAuthoritativeStateIsExposedByTrackingAndOverviewAndMovesOnlyExplicitly(t *testing.T) {
	application, item, _ := newTrackingApp(t)
	def, err := application.CreateWorkflowDefinition(CreateWorkflowDefinitionRequestDTO{
		Name: "Delivery", Version: 1,
		Steps: []WorkflowStepRequestDTO{{Key: "plan", Title: "Plan"}, {Key: "verify", Title: "Verify"}, {Key: "release", Title: "Release", Terminal: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	started := "2026-09-01T09:00:00Z"
	wf, err := application.CreateWorkflow(CreateWorkflowRequestDTO{WorkItemID: item.ID, WorkflowDefinitionID: def.ID, CurrentStepID: &def.Steps[1].ID, StartedAt: &started})
	if err != nil {
		t.Fatal(err)
	}
	if wf.StartedAt != started || wf.CurrentStep.ID != def.Steps[1].ID || wf.CurrentStep.Key != "verify" || wf.CurrentStep.Title != "Verify" || wf.CurrentStep.Sequence != 2 {
		t.Fatalf("workflow DTO must expose the semantic current step and actual start: %+v", wf)
	}
	if _, err := application.CreateWorkflow(CreateWorkflowRequestDTO{WorkItemID: item.ID, WorkflowDefinitionID: def.ID, StartedAt: strPtr("not-a-time")}); err == nil {
		t.Fatal("a malformed start must be rejected")
	}

	// Item progression does not move the Workflow.
	child, err := application.CreateTrackedItem(CreateTrackedItemRequestDTO{WorkItemID: item.ID, Title: "API"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.SetTrackedItemStepState(SetTrackedItemStepStateRequestDTO{TrackedItemID: child.ID, WorkflowStepID: def.Steps[0].ID, State: "COMPLETED"}); err != nil {
		t.Fatal(err)
	}
	view, err := application.GetWorkItemTracking(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Workflow == nil || view.Workflow.CurrentStep.Key != "verify" || view.Workflow.StartedAt != started {
		t.Fatalf("tracking projection must expose Workflow state unchanged by items: %+v", view.Workflow)
	}
	overview, err := application.GetWorkItemOverview()
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, row := range overview.Items {
		if row.WorkItem.ID != item.ID {
			continue
		}
		found = true
		w := row.Tracking.Workflow
		if w == nil || w.CurrentStep.Key != "verify" || w.CurrentStep.Sequence != 2 || w.StartedAt != started || len(w.Stages) != 3 {
			t.Fatalf("overview projection must expose the semantic Workflow state: %+v", w)
		}
	}
	if !found {
		t.Fatal("work item missing from overview")
	}

	moved, err := application.SetWorkflowCurrentStep(SetWorkflowCurrentStepRequestDTO{WorkflowID: wf.ID, StepID: def.Steps[0].ID})
	if err != nil || moved.CurrentStep.Key != "plan" {
		t.Fatalf("explicit backward Workflow move failed: %+v err=%v", moved, err)
	}
	if _, err := application.SetWorkflowCurrentStep(SetWorkflowCurrentStepRequestDTO{WorkflowID: wf.ID, StepID: "nope"}); err == nil {
		t.Fatal("an unknown step must be rejected")
	}
}

func strPtr(value string) *string { return &value }
