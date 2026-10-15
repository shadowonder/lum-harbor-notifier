package tracking

import (
	"testing"
	"time"

	"lum-harbor-notifier/internal/workitem"
)

func TestListWorkItemSummariesUsesDerivedProgressionAndIsolatesTargets(t *testing.T) {
	f := newFixture(t)
	empty := f.workItem(t, "Empty")
	primary := f.workItem(t, "Primary")
	other := f.workItem(t, "Other")
	closed := f.workItem(t, "Closed")
	archived := f.workItem(t, "Archived")
	if _, err := f.workItems.SetLocalWorkItemLifecycleState(f.ctx, workitem.SetLocalWorkItemLifecycleStateInput{ID: closed.ID, Target: workitem.LifecycleCompleted}); err != nil {
		t.Fatal(err)
	}

	flowA := f.workflow(t, "Flow A", 1, "plan", "ship")
	flowB := f.workflow(t, "Flow B", 1, "draft", "review", "publish")
	_ = flowB
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: primary.ID, WorkflowDefinitionID: flowA.ID}); err != nil {
		t.Fatal(err)
	}
	parent, err := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: primary.ID, Title: "Parent"})
	if err != nil {
		t.Fatal(err)
	}
	child, err := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: primary.ID, ParentItemID: &parent.ID, Title: "Child"})
	if err != nil {
		t.Fatal(err)
	}
	sibling, err := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: primary.ID, Title: "Sibling"})
	if err != nil {
		t.Fatal(err)
	}
	waiting, err := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: primary.ID, Title: "Waiting"})
	if err != nil {
		t.Fatal(err)
	}
	failed, err := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: primary.ID, Title: "Failed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.tracking.AddDependency(f.ctx, TrackedItemRelationInput{FromTrackedItemID: child.ID, ToTrackedItemID: parent.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: other.ID, WorkflowDefinitionID: flowA.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: other.ID, Title: "Other item"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: closed.ID, WorkflowDefinitionID: flowA.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: closed.ID, Title: "Closed item"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tracking.CreateWorkflow(f.ctx, CreateWorkflowInput{WorkItemID: archived.ID, WorkflowDefinitionID: flowA.ID}); err != nil {
		t.Fatal(err)
	}
	archivedItem, err := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: archived.ID, Title: "Archived item"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.tracking.SetTrackedItemStepState(f.ctx, SetStepStateInput{TrackedItemID: parent.ID, WorkflowStepID: flowA.Steps[0].ID, State: StepCompleted}); err != nil {
		t.Fatal(err)
	}
	for _, change := range []struct {
		itemID, stepID string
		state          StepState
	}{
		{child.ID, flowA.Steps[0].ID, StepBlocked},
		{sibling.ID, flowA.Steps[0].ID, StepCompleted}, {sibling.ID, flowA.Steps[1].ID, StepCompleted},
		{waiting.ID, flowA.Steps[0].ID, StepWaiting}, {failed.ID, flowA.Steps[0].ID, StepFailed},
	} {
		if _, err := f.tracking.SetTrackedItemStepState(f.ctx, SetStepStateInput{TrackedItemID: change.itemID, WorkflowStepID: change.stepID, State: change.state}); err != nil {
			t.Fatal(err)
		}
	}

	due := time.Date(2026, 12, 1, 18, 0, 0, 0, time.UTC)
	expected := workitem.LifecycleCompleted
	active, err := f.tracking.CreateMonitorPoint(f.ctx, CreateMonitorPointInput{WorkItemID: primary.ID, Kind: MonitorFinalTarget, Title: "Primary target", DueAt: due, Timezone: "UTC", ExpectedWorkItemLifecycle: &expected})
	if err != nil {
		t.Fatal(err)
	}
	trackedTarget, err := f.tracking.CreateMonitorPoint(f.ctx, CreateMonitorPointInput{WorkItemID: other.ID, SubjectTrackedItemID: ptr(otherTrackedItemID(t, f, other.ID, flowA.ID)), Kind: MonitorFinalTarget, Title: "Tracked only", DueAt: due.Add(time.Hour), Timezone: "UTC", ExpectedStepID: &flowA.Steps[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	if trackedTarget.Subject.Kind != SubjectTrackedItem {
		t.Fatal("fixture did not create a tracked-item target")
	}
	if _, err := f.tracking.CreateMonitorPoint(f.ctx, CreateMonitorPointInput{WorkItemID: primary.ID, SubjectTrackedItemID: &child.ID, Kind: MonitorCheckpoint, Title: "Child ship", DueAt: due, Timezone: "UTC", ExpectedStepID: &flowA.Steps[1].ID}); err != nil {
		t.Fatal(err)
	}
	satisfiedMonitor, err := f.tracking.CreateMonitorPoint(f.ctx, CreateMonitorPointInput{WorkItemID: primary.ID, SubjectTrackedItemID: &sibling.ID, Kind: MonitorCheckpoint, Title: "Sibling ship", DueAt: due, Timezone: "UTC", ExpectedStepID: &flowA.Steps[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.tracking.SetMonitorPointState(f.ctx, SetMonitorPointStateInput{ID: satisfiedMonitor.ID, Target: MonitorSatisfied}); err != nil {
		t.Fatal(err)
	}
	oldSatisfied, err := f.tracking.CreateMonitorPoint(f.ctx, CreateMonitorPointInput{WorkItemID: empty.ID, Kind: MonitorFinalTarget, Title: "Satisfied", DueAt: due, Timezone: "UTC", ExpectedWorkItemLifecycle: &expected})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.tracking.SetMonitorPointState(f.ctx, SetMonitorPointStateInput{ID: oldSatisfied.ID, Target: MonitorSatisfied}); err != nil {
		t.Fatal(err)
	}
	oldCancelled, err := f.tracking.CreateMonitorPoint(f.ctx, CreateMonitorPointInput{WorkItemID: empty.ID, Kind: MonitorFinalTarget, Title: "Cancelled", DueAt: due, Timezone: "UTC", ExpectedWorkItemLifecycle: &expected})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.tracking.SetMonitorPointState(f.ctx, SetMonitorPointStateInput{ID: oldCancelled.ID, Target: MonitorCancelled}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.ExecContext(f.ctx, "UPDATE entities SET archived_at = ? WHERE id IN (?, ?)", due.Format(time.RFC3339Nano), archived.ID, archivedItem.ID); err != nil {
		t.Fatal(err)
	}

	summaries, err := f.tracking.ListWorkItemSummaries(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	got := summaries[primary.ID]
	if got.WorkItemID != primary.ID || got.TrackedItemCount != 5 || got.WorkflowBoundItemCount != 5 || len(got.Progressions) != 5 {
		t.Fatalf("unexpected primary summary: %+v", got)
	}
	if got.Workflow == nil || got.Workflow.Definition.Name != "Flow A" || len(got.Workflow.Definition.Steps) != 2 || len(got.Items) != 5 {
		t.Fatalf("overview workflow shape is incomplete: %+v", got)
	}
	if got.Items[0].Title != "Parent" || got.StateDistribution.NotStarted != 1 || got.StateDistribution.Blocked != 1 || got.StateDistribution.Waiting != 1 || got.StateDistribution.Failed != 1 || got.StateDistribution.Completed != 1 {
		t.Fatalf("overview item/state projection is wrong: %+v", got)
	}
	if !got.Items[1].Monitoring.HasActiveExpectation || got.Items[1].Monitoring.HasSatisfiedExpectation {
		t.Fatalf("active item monitoring context is wrong: %+v", got.Items[1].Monitoring)
	}
	if !got.Items[2].Monitoring.HasSatisfiedExpectation || got.Items[2].Monitoring.HasActiveExpectation {
		t.Fatalf("satisfied item monitoring context is wrong: %+v", got.Items[2].Monitoring)
	}
	if got.Progressions[0].TrackedItemID != parent.ID || got.Progressions[0].Progression.ProgressPercent != 50 || got.Progressions[0].Progression.CurrentStepKey == nil || *got.Progressions[0].Progression.CurrentStepKey != "ship" {
		t.Fatalf("progression was not derived by Tracking semantics: %+v", got.Progressions[0])
	}
	if got.Progressions[1].TrackedItemID != child.ID || got.Progressions[1].ParentItemID == nil || *got.Progressions[1].ParentItemID != parent.ID {
		t.Fatalf("parent/child identity was not preserved: %+v", got.Progressions[1])
	}
	if got.ActiveFinalTarget == nil || got.ActiveFinalTarget.ID != active.ID || !got.ActiveFinalTarget.DueAt.Equal(due) {
		t.Fatalf("active WorkItem target missing: %+v", got.ActiveFinalTarget)
	}
	if summaries[other.ID].ActiveFinalTarget != nil {
		t.Fatal("tracked-item FINAL_TARGET was promoted to WorkItem target")
	}
	if summaries[empty.ID].ActiveFinalTarget != nil {
		t.Fatal("satisfied/cancelled FINAL_TARGET became current")
	}
	if summaries[closed.ID].TrackedItemCount != 1 {
		t.Fatal("non-archived closed WorkItem must retain overview facts")
	}
	if _, exists := summaries[archived.ID]; exists {
		t.Fatal("archived WorkItem leaked into summaries")
	}
}

func otherTrackedItemID(t *testing.T, f *fixture, workItemID, workflowID string) string {
	t.Helper()
	item, err := f.tracking.CreateTrackedItem(f.ctx, CreateTrackedItemInput{WorkItemID: workItemID, Title: "Target subject"})
	if err != nil {
		t.Fatal(err)
	}
	return item.ID
}

func ptr(value string) *string { return &value }
