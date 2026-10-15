package readcap_test

import (
	"errors"
	"testing"

	"lum-harbor-notifier/internal/readcap"
	"lum-harbor-notifier/internal/tracking"
)

func TestWorkItemReadReturnsAuthoritativeFactsAndBoundedTracking(t *testing.T) {
	f := newFixture(t)
	item := f.workItem(t, "Ship the release")
	def := f.workflowDefinition(t, "release-process", "draft", "review", "done")
	wf := f.workflow(t, item.ID, def.ID)
	tracked := f.trackedItem(t, item.ID, wf.ID, "Write changelog")

	result, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityWorkItemRead,
		Input:        mustEncode(t, readcap.WorkItemReadInput{WorkItemID: item.ID}),
	})
	if err != nil {
		t.Fatal(err)
	}

	output, ok := result.Output.(readcap.WorkItemReadOutput)
	if !ok {
		t.Fatalf("expected WorkItemReadOutput, got %T", result.Output)
	}

	if output.WorkItem.ID != item.ID || output.WorkItem.Title != "Ship the release" {
		t.Fatalf("unexpected WorkItem facts: %+v", output.WorkItem)
	}
	if output.WorkItem.Lifecycle != string(item.Lifecycle) {
		t.Fatalf("expected lifecycle %q, got %q", item.Lifecycle, output.WorkItem.Lifecycle)
	}

	if output.Tracking == nil {
		t.Fatal("expected non-nil Tracking for a WorkItem with a Workflow")
	}
	if output.Tracking.WorkflowID != wf.ID {
		t.Fatalf("expected workflow ID %q, got %q", wf.ID, output.Tracking.WorkflowID)
	}
	if output.Tracking.CurrentStep == nil || output.Tracking.CurrentStep.Key != "draft" {
		t.Fatalf("expected current step 'draft', got %+v", output.Tracking.CurrentStep)
	}
	if len(output.Tracking.Items) != 1 || output.Tracking.Items[0].ID != tracked.ID {
		t.Fatalf("expected exactly the one tracked item, got %+v", output.Tracking.Items)
	}
}
func TestWorkItemReadWithoutWorkflowReturnsNilTracking(t *testing.T) {
	f := newFixture(t)
	item := f.workItem(t, "Standalone item")

	result, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityWorkItemRead,
		Input:        mustEncode(t, readcap.WorkItemReadInput{WorkItemID: item.ID}),
	})
	if err != nil {
		t.Fatal(err)
	}

	output := result.Output.(readcap.WorkItemReadOutput)
	if output.Tracking != nil {
		t.Fatalf("expected nil Tracking for a WorkItem with no Workflow, got %+v", output.Tracking)
	}
}

func TestWorkItemReadMissingWorkItemIsEntityNotFound(t *testing.T) {
	f := newFixture(t)

	_, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityWorkItemRead,
		Input:        mustEncode(t, readcap.WorkItemReadInput{WorkItemID: "00000000-0000-0000-0000-000000000000"}),
	})
	if !errors.Is(err, readcap.ErrEntityNotFound) {
		t.Fatalf("expected ErrEntityNotFound, got %v", err)
	}
}

func TestWorkItemReadRejectsEmptyWorkItemID(t *testing.T) {
	f := newFixture(t)

	_, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityWorkItemRead,
		Input:        mustEncode(t, readcap.WorkItemReadInput{WorkItemID: "   "}),
	})
	if !errors.Is(err, readcap.ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput, got %v", err)
	}
}

func TestWorkItemReadCreatesNoRevisionOrHistory(t *testing.T) {
	f := newFixture(t)
	item := f.workItem(t, "Read-only check")

	before := f.count(t, "SELECT COUNT(*) FROM revisions")
	beforeHistory := f.count(t, "SELECT COUNT(*) FROM history_events")

	if _, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityWorkItemRead,
		Input:        mustEncode(t, readcap.WorkItemReadInput{WorkItemID: item.ID}),
	}); err != nil {
		t.Fatal(err)
	}

	if got := f.count(t, "SELECT COUNT(*) FROM revisions"); got != before {
		t.Fatalf("expected no new revisions, before=%d after=%d", before, got)
	}
	if got := f.count(t, "SELECT COUNT(*) FROM history_events"); got != beforeHistory {
		t.Fatalf("expected no new history events, before=%d after=%d", beforeHistory, got)
	}
}

func TestWorkItemReadExposesDerivedProgressionPerTrackedItem(t *testing.T) {
	f := newFixture(t)
	item := f.workItem(t, "Platform Overhaul")
	def := f.workflowDefinition(t, "platform-lifecycle", "Design", "Implementation", "Verification")
	wf := f.workflow(t, item.ID, def.ID)

	tArch := f.trackedItem(t, item.ID, wf.ID, "Architecture")
	tBackend := f.trackedItem(t, item.ID, wf.ID, "Backend Service")
	tTests := f.trackedItem(t, item.ID, wf.ID, "Test Suite")

	// Complete all steps for Architecture
	for _, s := range def.Steps {
		if _, err := f.tracking.SetTrackedItemStepState(f.ctx, tracking.SetStepStateInput{
			TrackedItemID:  tArch.ID,
			WorkflowStepID: s.ID,
			State:          tracking.StepCompleted,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Move Backend Service to IN_PROGRESS on Implementation (step 1 completed, step 2 in_progress)
	if _, err := f.tracking.SetTrackedItemStepState(f.ctx, tracking.SetStepStateInput{
		TrackedItemID:  tBackend.ID,
		WorkflowStepID: def.Steps[0].ID,
		State:          tracking.StepCompleted,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.tracking.SetTrackedItemStepState(f.ctx, tracking.SetStepStateInput{
		TrackedItemID:  tBackend.ID,
		WorkflowStepID: def.Steps[1].ID,
		State:          tracking.StepInProgress,
	}); err != nil {
		t.Fatal(err)
	}

	// Read via readcap
	result, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityWorkItemRead,
		Input:        mustEncode(t, readcap.WorkItemReadInput{WorkItemID: item.ID}),
	})
	if err != nil {
		t.Fatal(err)
	}
	output := result.Output.(readcap.WorkItemReadOutput)
	if output.Tracking == nil {
		t.Fatal("expected non-nil tracking facts")
	}

	// Workflow's own current step must remain "Design" (independent of item progression)
	if output.Tracking.CurrentStep == nil || output.Tracking.CurrentStep.Key != "Design" {
		t.Fatalf("expected workflow current step Design, got %+v", output.Tracking.CurrentStep)
	}

	byTitle := make(map[string]readcap.TrackedItemFacts, len(output.Tracking.Items))
	for _, it := range output.Tracking.Items {
		byTitle[it.Title] = it
	}

	if byTitle["Architecture"].ID != tArch.ID || byTitle["Backend Service"].ID != tBackend.ID || byTitle["Test Suite"].ID != tTests.ID {
		t.Fatalf("unexpected tracked item IDs in facts")
	}

	arch := byTitle["Architecture"]
	if arch.State != "COMPLETED" || !arch.Complete || arch.CurrentStepTitle != nil {
		t.Fatalf("expected Architecture to be COMPLETED, got %+v", arch)
	}

	backend := byTitle["Backend Service"]
	if backend.State != "IN_PROGRESS" || backend.Complete || backend.CurrentStepTitle == nil || *backend.CurrentStepTitle != "Implementation" {
		t.Fatalf("expected Backend Service to be IN_PROGRESS at Implementation, got %+v", backend)
	}

	tests := byTitle["Test Suite"]
	if tests.State != "NOT_STARTED" || tests.Complete || tests.CurrentStepTitle == nil || *tests.CurrentStepTitle != "Design" {
		t.Fatalf("expected Test Suite to be NOT_STARTED at Design, got %+v", tests)
	}
}

func TestWorkItemReadIncludesTrackedItemRelations(t *testing.T) {
	f := newFixture(t)
	item := f.workItem(t, "Feature With Dependencies")
	def := f.workflowDefinition(t, "feature-process", "Design", "Implementation")
	wf := f.workflow(t, item.ID, def.ID)

	t1 := f.trackedItem(t, item.ID, wf.ID, "Task 1")
	t2 := f.trackedItem(t, item.ID, wf.ID, "Task 2")

	if _, err := f.tracking.AddDependency(f.ctx, tracking.TrackedItemRelationInput{
		FromTrackedItemID: t2.ID,
		ToTrackedItemID:   t1.ID,
	}); err != nil {
		t.Fatal(err)
	}

	result, err := f.readcap.Execute(f.ctx, readcap.Request{
		CapabilityID: readcap.CapabilityWorkItemRead,
		Input:        mustEncode(t, readcap.WorkItemReadInput{WorkItemID: item.ID}),
	})
	if err != nil {
		t.Fatal(err)
	}
	output := result.Output.(readcap.WorkItemReadOutput)
	if output.Tracking == nil {
		t.Fatal("expected non-nil tracking facts")
	}
	if len(output.Tracking.Relations) != 1 {
		t.Fatalf("expected 1 relation, got %d", len(output.Tracking.Relations))
	}
	rel := output.Tracking.Relations[0]
	if rel.FromTrackedItemID != t2.ID || rel.ToTrackedItemID != t1.ID {
		t.Fatalf("unexpected relation: %+v", rel)
	}
	if rel.Type != "DEPENDS_ON" {
		t.Fatalf("expected relation type DEPENDS_ON, got %s", rel.Type)
	}
}
