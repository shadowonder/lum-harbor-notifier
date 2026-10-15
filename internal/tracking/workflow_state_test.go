package tracking

import (
	"errors"
	"testing"
	"time"

	"lum-harbor-notifier/internal/database"
	"lum-harbor-notifier/internal/workitem"
)

func (f *fixture) workflowFor(t *testing.T, workItemID string, def *WorkflowDefinition, mutate func(*CreateWorkflowInput)) *Workflow {
	t.Helper()
	input := CreateWorkflowInput{WorkItemID: workItemID, WorkflowDefinitionID: def.ID}
	if mutate != nil {
		mutate(&input)
	}
	wf, err := f.tracking.CreateWorkflow(f.ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	return wf
}

func TestWorkflowDefaultsToFirstDefinitionStepAndCreatedAtStart(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Defaults")
	def := f.workflow(t, "Delivery", 1, "discover", "design", "ship")
	wf := f.workflowFor(t, wi.ID, def, nil)
	if wf.CurrentStepID != def.Steps[0].ID {
		t.Fatalf("omitted current step must be the first ordered definition step, got %s", wf.CurrentStepID)
	}
	if !wf.StartedAt.Equal(wf.CreatedAt) || wf.StartedAt.IsZero() {
		t.Fatalf("omitted startedAt must equal createdAt: started=%v created=%v", wf.StartedAt, wf.CreatedAt)
	}
	if wf.LastRevisionID == "" || !wf.UpdatedAt.Equal(wf.CreatedAt) {
		t.Fatalf("a Workflow owns a creation Revision and updated_at: %+v", wf)
	}
}

func TestWorkflowExplicitStartAndCurrentStepArePersistedUnchanged(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Explicit")
	def := f.workflow(t, "Course", 1, "read", "lab", "test", "reflect")
	started := time.Date(2026, 9, 1, 9, 30, 0, 0, time.UTC)
	wf := f.workflowFor(t, wi.ID, def, func(in *CreateWorkflowInput) {
		in.StartedAt = &started
		in.CurrentStepID = &def.Steps[2].ID
	})
	if !wf.StartedAt.Equal(started) || wf.CurrentStepID != def.Steps[2].ID {
		t.Fatalf("explicit values changed: %+v", wf)
	}
	if wf.StartedAt.Equal(wf.CreatedAt) {
		t.Fatal("an explicit actual start must not be replaced by createdAt")
	}
	loaded, err := f.tracking.GetWorkflowByWorkItem(f.ctx, wi.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.StartedAt.Equal(started) || loaded.CurrentStepID != def.Steps[2].ID {
		t.Fatalf("persisted values differ: %+v", loaded)
	}
}

func TestWorkflowRejectsForeignStepAndStepLessDefinitionWithoutOrphans(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Rejects")
	def := f.workflow(t, "A", 1, "a1", "a2")
	other := f.workflow(t, "B", 1, "b1")
	revisions := f.count(t, "SELECT COUNT(*) FROM revisions")
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID, CurrentStepID: &other.Steps[0].ID}); !errors.Is(err, ErrStepNotInWorkflow) {
		t.Fatalf("a step of another definition must be rejected, got %v", err)
	}
	// A definition without steps (only reachable by direct insert) cannot start a Workflow.
	if _, err := f.db.ExecContext(f.ctx, `INSERT INTO workflow_definitions (id, name, version, active, created_at) VALUES ('empty-def', 'Empty', 1, 1, '2026-09-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: "empty-def"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("a stepless definition must fail, got %v", err)
	}
	if f.count(t, "SELECT COUNT(*) FROM workflows") != 0 || f.count(t, "SELECT COUNT(*) FROM revisions") != revisions {
		t.Fatal("rejected creation must leave no Workflow and no Revision")
	}
}

func TestSetWorkflowCurrentStepForwardBackwardAndNoOp(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Moves")
	def := f.workflow(t, "Delivery", 1, "plan", "build", "verify", "release")
	wf := f.workflowFor(t, wi.ID, def, nil)
	events := func() int {
		return f.count(t, "SELECT COUNT(*) FROM history_events WHERE event_type = 'WORKFLOW_CURRENT_STEP_CHANGED'")
	}
	revisions := f.count(t, "SELECT COUNT(*) FROM revisions")

	same, err := f.tracking.SetWorkflowCurrentStep(f.ctx, SetWorkflowCurrentStepInput{WorkflowID: wf.ID, StepID: def.Steps[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	if same.LastRevisionID != wf.LastRevisionID || events() != 0 || f.count(t, "SELECT COUNT(*) FROM revisions") != revisions {
		t.Fatal("a same-step mutation must be a no-op: no Revision, no History")
	}

	forward, err := f.tracking.SetWorkflowCurrentStep(f.ctx, SetWorkflowCurrentStepInput{WorkflowID: wf.ID, StepID: def.Steps[3].ID})
	if err != nil {
		t.Fatal(err)
	}
	if forward.CurrentStepID != def.Steps[3].ID || forward.LastRevisionID == wf.LastRevisionID || events() != 1 || f.count(t, "SELECT COUNT(*) FROM revisions") != revisions+1 {
		t.Fatalf("forward move must create exactly one Revision and one History event: %+v", forward)
	}
	if f.count(t, "SELECT COUNT(*) FROM history_events WHERE event_type = 'WORKFLOW_CURRENT_STEP_CHANGED' AND revision_id = ? AND work_item_id = ? AND scope = 'TRACKING'", forward.LastRevisionID, wi.ID) != 1 {
		t.Fatal("the History event must link the WorkItem and the change Revision")
	}

	// The Workflow may explicitly regress: no monotonic sequence invariant.
	backward, err := f.tracking.SetWorkflowCurrentStep(f.ctx, SetWorkflowCurrentStepInput{WorkflowID: wf.ID, StepID: def.Steps[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	if backward.CurrentStepID != def.Steps[1].ID || events() != 2 {
		t.Fatalf("explicit backward move must succeed: %+v", backward)
	}

	other := f.workflow(t, "Other", 1, "x")
	if _, err := f.tracking.SetWorkflowCurrentStep(f.ctx, SetWorkflowCurrentStepInput{WorkflowID: wf.ID, StepID: other.Steps[0].ID}); !errors.Is(err, ErrStepNotInWorkflow) {
		t.Fatalf("a step from another definition must be rejected, got %v", err)
	}
	if _, err := f.tracking.SetWorkflowCurrentStep(f.ctx, SetWorkflowCurrentStepInput{WorkflowID: "missing", StepID: def.Steps[0].ID}); !errors.Is(err, ErrWorkflowNotFound) {
		t.Fatalf("an unknown workflow must be rejected, got %v", err)
	}
	if events() != 2 {
		t.Fatal("rejected mutations must not write History")
	}
}

// Primary invariant: TrackedItem progression never mutates Workflow.currentStep.
func TestTrackedItemProgressionNeverMutatesWorkflowCurrentStep(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Decoupled")
	def := f.workflow(t, "Delivery", 1, "implement", "verify", "release")
	release := def.Steps[2]
	wf := f.workflowFor(t, wi.ID, def, func(in *CreateWorkflowInput) { in.CurrentStepID = &release.ID })

	a, _ := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "A"})
	b, _ := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "B"})
	c, _ := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "C"})
	move := func(id string, step WorkflowStep, state StepState) {
		t.Helper()
		if _, err := f.tracking.SetTrackedItemStageState(f.ctx, SetTrackedItemStageStateInput{TrackedItemID: id, WorkflowStepID: step.ID, State: state}); err != nil {
			t.Fatal(err)
		}
	}
	move(a.ID, def.Steps[2], StepInProgress) // A at RELEASE
	move(b.ID, def.Steps[0], StepBlocked)    // B at IMPLEMENT / BLOCKED
	move(c.ID, def.Steps[1], StepWaiting)    // C at VERIFY / WAITING
	// Now items regress, complete, and move again.
	move(b.ID, def.Steps[1], StepInProgress)
	move(c.ID, def.Steps[0], StepFailed)
	move(a.ID, def.Steps[2], StepCompleted)
	move(c.ID, def.Steps[2], StepCompleted)

	loaded, err := f.tracking.GetWorkflowByWorkItem(f.ctx, wi.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.CurrentStepID != release.ID || loaded.LastRevisionID != wf.LastRevisionID || !loaded.UpdatedAt.Equal(wf.UpdatedAt) {
		t.Fatalf("TrackedItem changes must not touch the Workflow: %+v", loaded)
	}
	if f.count(t, "SELECT COUNT(*) FROM history_events WHERE event_type = 'WORKFLOW_CURRENT_STEP_CHANGED'") != 0 {
		t.Fatal("item progression must not write Workflow-step History")
	}
	// Item positions stay independently derived from their own step states.
	tracking, err := f.tracking.GetWorkItemTracking(f.ctx, wi.ID)
	if err != nil {
		t.Fatal(err)
	}
	positions := map[string]string{}
	for _, view := range tracking.Items {
		if view.Progression != nil && view.Progression.CurrentStepKey != nil {
			positions[view.Item.Title] = *view.Progression.CurrentStepKey
		}
	}
	if positions["B"] != "verify" {
		t.Fatalf("item B must keep its own derived position, got %v", positions)
	}
	if step, ok := tracking.Workflow.CurrentStep(); !ok || step.ID != release.ID {
		t.Fatalf("Workflow current step must stay RELEASE, got %+v", step)
	}
}

func TestWorkflowStateIsExposedByTrackingAndOverviewProjectionsAndSurvivesReopen(t *testing.T) {
	f := newFixture(t)
	wi := f.workItem(t, "Projected")
	def := f.workflow(t, "Course", 1, "read", "lab", "test", "reflect")
	started := time.Date(2026, 9, 7, 9, 0, 0, 0, time.UTC)
	f.workflowFor(t, wi.ID, def, func(in *CreateWorkflowInput) {
		in.StartedAt = &started
		in.CurrentStepID = &def.Steps[1].ID
	})
	if _, err := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: wi.ID, Title: "Lab 1"}); err != nil {
		t.Fatal(err)
	}
	// A second WorkItem without a Workflow stays a valid empty summary.
	empty := f.workItem(t, "No workflow")

	check := func(svc *Service, label string) {
		t.Helper()
		tracking, err := svc.GetWorkItemTracking(f.ctx, wi.ID)
		if err != nil {
			t.Fatal(err)
		}
		step, ok := tracking.Workflow.CurrentStep()
		if !ok || step.Key != "lab" || step.Title != "LAB" || step.Sequence != 2 || step.Terminal || !tracking.Workflow.Workflow.StartedAt.Equal(started) {
			t.Fatalf("%s: tracking projection lost Workflow state: %+v %+v", label, tracking.Workflow.Workflow, step)
		}
		summaries, err := svc.ListWorkItemSummaries(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		overview := summaries[wi.ID].Workflow
		if overview == nil || overview.CurrentStep.Key != "lab" || !overview.StartedAt.Equal(started) {
			t.Fatalf("%s: overview projection lost Workflow state: %+v", label, overview)
		}
		if summaries[empty.ID].Workflow != nil {
			t.Fatalf("%s: a WorkItem without a Workflow has no Workflow state", label)
		}
	}
	check(f.tracking, "live")

	if err := f.db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.Open(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.DB.Close() })
	check(NewService(reopened.DB, workitem.NewService(reopened.DB, workitem.NewRepository(reopened.DB))), "reopened")
}

func TestWorkflowHasNoLifecycleColumnsAndNoItemDerivedPosition(t *testing.T) {
	f := newFixture(t)
	rows, err := f.db.Query("SELECT name FROM pragma_table_info('workflows')")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	got := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		got[name] = true
	}
	for _, want := range []string{"id", "work_item_id", "workflow_definition_id", "current_step_id", "started_at", "last_revision_id", "created_at", "updated_at"} {
		if !got[want] {
			t.Fatalf("workflows is missing %s", want)
		}
	}
	for _, banned := range []string{"lifecycle_state", "completed_at", "state"} {
		if got[banned] {
			t.Fatalf("workflows must not carry %s in this slice", banned)
		}
	}
}
