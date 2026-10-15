package proposal

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/testsupport"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

func newFullService(t *testing.T) (context.Context, *sql.DB, *workitem.Service, *tracking.Service, *planner.Service, *Service) {
	t.Helper()
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	wiService := workitem.NewService(db.DB, workitem.NewRepository(db.DB))
	trackingService := tracking.NewService(db.DB, wiService)
	plannerService := planner.NewService(db.DB, planner.NewRepository(db.DB))
	proposals := NewService(db.DB, wiService, trackingService)
	proposals.SetPlannerOperator(plannerService)
	return ctx, db.DB, wiService, trackingService, plannerService, proposals
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// --- planner.item.create ---------------------------------------------------

func TestPlannerRootCreateProposalHasNullTargetAndZeroTruthUntilApply(t *testing.T) {
	ctx, db, _, _, _, proposals := newFullService(t)
	revisionsBefore := countRows(t, db, "SELECT COUNT(*) FROM revisions")
	entitiesBefore := countRows(t, db, "SELECT COUNT(*) FROM entities")

	created, err := proposals.ProposePlannerItemCreate(ctx, ProposePlannerItemCreateInput{
		Title: "Submit homework", Producer: ProducerAI,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.TargetEntityID != nil {
		t.Fatalf("expected NULL target for a true root-create Proposal, got %v", *created.TargetEntityID)
	}
	if countRows(t, db, "SELECT COUNT(*) FROM revisions") != revisionsBefore || countRows(t, db, "SELECT COUNT(*) FROM entities") != entitiesBefore {
		t.Fatal("creating the Proposal must create zero Planner domain truth")
	}
	if countRows(t, db, "SELECT COUNT(*) FROM planner_items") != 0 {
		t.Fatal("no planner_items row may exist before Apply")
	}

	result, err := proposals.ApplyProposal(ctx, ApplyProposalInput{ID: created.ID})
	if err != nil || result.Outcome != ApplyOutcomeApplied {
		t.Fatalf("apply: %+v err=%v", result, err)
	}
	if countRows(t, db, "SELECT COUNT(*) FROM planner_items") != 1 {
		t.Fatal("Apply must atomically create exactly one PlannerItem")
	}
	if countRows(t, db, "SELECT COUNT(*) FROM revisions") != revisionsBefore+1 {
		t.Fatal("Apply must create exactly one Revision")
	}
}

func TestPlannerRootCreateRejectLeavesZeroTruth(t *testing.T) {
	ctx, db, _, _, _, proposals := newFullService(t)
	created, err := proposals.ProposePlannerItemCreate(ctx, ProposePlannerItemCreateInput{Title: "Reject me", Producer: ProducerAI})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := proposals.RejectProposal(ctx, RejectProposalInput{ID: created.ID}); err != nil {
		t.Fatal(err)
	}
	if countRows(t, db, "SELECT COUNT(*) FROM planner_items") != 0 || countRows(t, db, "SELECT COUNT(*) FROM entities WHERE entity_type = 'PLANNER_ITEM'") != 0 {
		t.Fatal("Reject must leave zero Planner truth")
	}
}

func TestPlannerCreateWithWorkItemContextUsesExistingTargetPattern(t *testing.T) {
	ctx, _, wiService, _, _, proposals := newFullService(t)
	wi := createTestWorkItem(t, ctx, wiService, "Related Work", nil)
	created, err := proposals.ProposePlannerItemCreate(ctx, ProposePlannerItemCreateInput{
		Title: "Follow up", WorkItemID: &wi.ID, Producer: ProducerAI,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.TargetEntityID == nil || *created.TargetEntityID != wi.ID {
		t.Fatalf("expected non-null target = parent WorkItem ID, got %v", created.TargetEntityID)
	}
}

func TestPlannerTrackedItemCreateProposalRoundTrip(t *testing.T) {
	ctx, _, wiService, trackingService, plannerService, proposals := newFullService(t)
	wi := createTestWorkItem(t, ctx, wiService, "Tracked Todo", nil)
	def, err := trackingService.CreateWorkflowDefinition(ctx, tracking.CreateWorkflowDefinitionInput{
		Name: "Tracked Todo Flow", Version: 1,
		Steps: []tracking.WorkflowStepInput{{Key: "work", Title: "Work"}, {Key: "done", Title: "Done", Terminal: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	wf, err := trackingService.CreateWorkflow(ctx, tracking.CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID})
	if err != nil {
		t.Fatal(err)
	}
	tracked, err := trackingService.CreateTrackedItem(ctx, tracking.CreateTrackedItemInput{WorkItemID: wi.ID, Title: "Exact tracked item"})
	if err != nil {
		t.Fatal(err)
	}
	due := "2026-10-08"
	created, err := proposals.ProposePlannerItemCreate(ctx, ProposePlannerItemCreateInput{
		Title: "Act on exact tracked item", TrackedItemID: &tracked.ID, DueDate: &due, Producer: ProducerAI,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.TargetEntityID == nil || *created.TargetEntityID != wi.ID || created.PlannerItemPayload.TrackedItemID == nil || *created.PlannerItemPayload.TrackedItemID != tracked.ID || created.PlannerItemPayload.WorkflowID == nil || *created.PlannerItemPayload.WorkflowID != wf.ID {
		t.Fatalf("proposal did not preserve typed relation/provenance: %+v", created)
	}
	if _, err := proposals.ApplyProposal(ctx, ApplyProposalInput{ID: created.ID}); err != nil {
		t.Fatal(err)
	}
	items, err := plannerService.SearchByTitle(ctx, "Act on exact tracked item", 10)
	if err != nil || len(items) != 1 || items[0].RelatedEntityID == nil || *items[0].RelatedEntityID != tracked.ID {
		t.Fatalf("applied Todo lost relation: items=%+v err=%v", items, err)
	}

	wrong := wi.ID
	if _, err := proposals.ProposePlannerItemCreate(ctx, ProposePlannerItemCreateInput{Title: "Wrong type", TrackedItemID: &wrong, Producer: ProducerAI}); err == nil {
		t.Fatal("non-TrackedItem entity must be rejected before Proposal creation")
	}
}

func TestPlannerCreateMutuallyExclusiveDueFields(t *testing.T) {
	ctx, _, _, _, _, proposals := newFullService(t)
	dueDate, dueAt := "2026-09-25", "2026-09-25T15:00:00Z"
	_, err := proposals.ProposePlannerItemCreate(ctx, ProposePlannerItemCreateInput{
		Title: "Ambiguous", DueDate: &dueDate, DueAt: &dueAt, Producer: ProducerAI,
	})
	if err == nil {
		t.Fatal("expected rejection when both due_date and due_at are set - never guess which one wins")
	}
}

func TestNullTargetRejectedForNonRootCreateOperationTypes(t *testing.T) {
	ctx, db, wiService, _, _, proposals := newFullService(t)
	wi := createTestWorkItem(t, ctx, wiService, "Guard Target", nil)
	proposalID := "prop-null-target-guard"
	_, err := db.ExecContext(ctx, `INSERT INTO change_proposals (id, target_entity_id, operation_type, title, status, operation_payload_json, created_at, updated_at) VALUES (?, NULL, ?, 'x', 'PENDING', '{}', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z')`,
		proposalID, OperationTypeWorkItemUpdateDetails)
	if err == nil {
		t.Fatal("DB CHECK must reject a NULL target for any operation type other than the root-create allowlist")
	}
	_ = wi
	_ = proposals
}

// --- workitem.set_lifecycle -------------------------------------------------

func TestWorkItemLifecycleProposalValidTransitionAppliesAtomically(t *testing.T) {
	ctx, db, wiService, _, _, proposals := newFullService(t)
	wi := createTestWorkItem(t, ctx, wiService, "Payments Modernization Program", nil)
	revisionsBefore := countRows(t, db, "SELECT COUNT(*) FROM revisions")

	created, err := proposals.ProposeWorkItemLifecycle(ctx, ProposeWorkItemLifecycleInput{
		TargetWorkItemID: wi.ID, TargetState: "PAUSED", Producer: ProducerAI,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.TargetEntityID == nil || *created.TargetEntityID != wi.ID {
		t.Fatalf("expected target = work item ID, got %v", created.TargetEntityID)
	}
	fresh, err := wiService.GetByID(ctx, wi.ID)
	if err != nil || fresh.Lifecycle != workitem.LifecycleActive {
		t.Fatal("Proposal creation must not mutate the WorkItem")
	}

	result, err := proposals.ApplyProposal(ctx, ApplyProposalInput{ID: created.ID})
	if err != nil || result.Outcome != ApplyOutcomeApplied {
		t.Fatalf("apply: %+v err=%v", result, err)
	}
	after, err := wiService.GetByID(ctx, wi.ID)
	if err != nil || after.Lifecycle != workitem.LifecyclePaused {
		t.Fatalf("expected PAUSED after apply, got %+v err=%v", after, err)
	}
	if countRows(t, db, "SELECT COUNT(*) FROM revisions") != revisionsBefore+1 {
		t.Fatal("expected exactly one Revision from Apply")
	}
}

func TestWorkItemLifecycleProposalInvalidTransitionRejectedAtPropose(t *testing.T) {
	ctx, _, wiService, _, _, proposals := newFullService(t)
	wi := createTestWorkItem(t, ctx, wiService, "Terminal Item", nil)
	if _, err := wiService.SetLocalWorkItemLifecycleState(ctx, workitem.SetLocalWorkItemLifecycleStateInput{ID: wi.ID, Target: workitem.LifecycleCompleted}); err != nil {
		t.Fatal(err)
	}
	// COMPLETED can only transition back to ACTIVE, never directly to PAUSED.
	if _, err := proposals.ProposeWorkItemLifecycle(ctx, ProposeWorkItemLifecycleInput{
		TargetWorkItemID: wi.ID, TargetState: "PAUSED", Producer: ProducerAI,
	}); err == nil {
		t.Fatal("expected domain CanTransitionTo rejection, not silently accepted")
	}
}

func TestWorkItemLifecycleProposalGoesStaleWhenStateChangedBeforeApply(t *testing.T) {
	ctx, _, wiService, _, _, proposals := newFullService(t)
	wi := createTestWorkItem(t, ctx, wiService, "Racing Item", nil)
	created, err := proposals.ProposeWorkItemLifecycle(ctx, ProposeWorkItemLifecycleInput{
		TargetWorkItemID: wi.ID, TargetState: "PAUSED", Producer: ProducerAI,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wiService.SetLocalWorkItemLifecycleState(ctx, workitem.SetLocalWorkItemLifecycleStateInput{ID: wi.ID, Target: workitem.LifecycleCancelled}); err != nil {
		t.Fatal(err)
	}
	result, err := proposals.ApplyProposal(ctx, ApplyProposalInput{ID: created.ID})
	if err != nil || result.Outcome != ApplyOutcomeStale {
		t.Fatalf("expected STALE, got %+v err=%v", result, err)
	}
}

// --- tracking.workflow_create -----------------------------------------------

func TestWorkflowCreateProposalAppliesDefinitionAndWorkflowAtomically(t *testing.T) {
	ctx, db, wiService, trackingService, _, proposals := newFullService(t)
	wi := createTestWorkItem(t, ctx, wiService, "New Project", nil)
	revisionsBefore := countRows(t, db, "SELECT COUNT(*) FROM revisions")

	created, err := proposals.ProposeWorkflowCreate(ctx, ProposeWorkflowCreateInput{
		TargetWorkItemID: wi.ID, DefinitionName: "Delivery", Steps: []string{"Development", "Review", "Production"}, Producer: ProducerAI,
	})
	if err != nil {
		t.Fatal(err)
	}
	if view, _ := trackingService.GetWorkItemTracking(ctx, wi.ID); view.Workflow != nil {
		t.Fatal("Proposal creation must not attach a Workflow before Apply")
	}

	result, err := proposals.ApplyProposal(ctx, ApplyProposalInput{ID: created.ID})
	if err != nil || result.Outcome != ApplyOutcomeApplied {
		t.Fatalf("apply: %+v err=%v", result, err)
	}
	view, err := trackingService.GetWorkItemTracking(ctx, wi.ID)
	if err != nil || view.Workflow == nil || len(view.Workflow.Definition.Steps) != 3 {
		t.Fatalf("expected a 3-step workflow attached, got %+v err=%v", view.Workflow, err)
	}
	// Exactly one Revision: the Workflow attachment. WorkflowDefinition
	// creation is non-revisioned configuration, matching CreateWorkflow's
	// existing non-Proposal shape.
	if countRows(t, db, "SELECT COUNT(*) FROM revisions") != revisionsBefore+1 {
		t.Fatal("expected exactly one Revision from the compound Apply")
	}
}

func TestWorkflowCreateProposalRejectedWhenWorkflowAlreadyExists(t *testing.T) {
	ctx, _, wiService, trackingService, _, proposals := newFullService(t)
	wi := createTestWorkItem(t, ctx, wiService, "Already Has Workflow", nil)
	def, err := trackingService.CreateWorkflowDefinition(ctx, tracking.CreateWorkflowDefinitionInput{Name: "Existing", Version: 1, Steps: []tracking.WorkflowStepInput{{Key: "only", Title: "Only Step"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := trackingService.CreateWorkflow(ctx, tracking.CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := proposals.ProposeWorkflowCreate(ctx, ProposeWorkflowCreateInput{
		TargetWorkItemID: wi.ID, DefinitionName: "Second", Steps: []string{"A", "B"}, Producer: ProducerAI,
	}); err == nil {
		t.Fatal("expected rejection: at-most-one-Workflow-per-WorkItem invariant")
	}
}

func TestWorkflowCreateProposalRejectsDuplicateStepTitles(t *testing.T) {
	ctx, _, wiService, _, _, proposals := newFullService(t)
	wi := createTestWorkItem(t, ctx, wiService, "Dup Steps", nil)
	if _, err := proposals.ProposeWorkflowCreate(ctx, ProposeWorkflowCreateInput{
		TargetWorkItemID: wi.ID, DefinitionName: "Dup", Steps: []string{"Review", "review"}, Producer: ProducerAI,
	}); err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate step title rejection, got %v", err)
	}
}

// --- work_item.create_with_workflow -----------------------------------------

func TestWorkItemCreateWithWorkflowProposalLifecycle(t *testing.T) {
	ctx, db, wiService, trackingService, _, proposals := newFullService(t)
	revisionsBefore := countRows(t, db, "SELECT COUNT(*) FROM revisions")
	entitiesBefore := countRows(t, db, "SELECT COUNT(*) FROM entities")

	desc := "Feature description"
	stages := []string{"define", "develop", "test", "stage", "adoption", "production"}
	prop, err := proposals.ProposeWorkItemCreateWithWorkflow(ctx, ProposeWorkItemCreateWithWorkflowInput{
		Title:        "F13456752",
		Description:  &desc,
		WorkflowName: "Deploy Pipeline",
		Stages:       stages,
		Producer:     ProducerAI,
	})
	if err != nil {
		t.Fatalf("expected successful proposal creation: %v", err)
	}

	if prop.TargetEntityID != nil {
		t.Fatalf("expected NULL target_entity_id before apply, got %s", *prop.TargetEntityID)
	}
	if countRows(t, db, "SELECT COUNT(*) FROM entities") != entitiesBefore {
		t.Fatal("proposal creation must not create entities before apply")
	}

	applyRes, err := proposals.ApplyProposal(ctx, ApplyProposalInput{ID: prop.ID})
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
	if applyRes.Outcome != ApplyOutcomeApplied {
		t.Fatalf("expected ApplyOutcomeApplied, got %v", applyRes.Outcome)
	}
	if applyRes.WorkItem == nil || applyRes.WorkItem.Title != "F13456752" {
		t.Fatalf("unexpected applied work item: %+v", applyRes.WorkItem)
	}

	var targetEntityID sql.NullString
	if err := db.QueryRow("SELECT target_entity_id FROM change_proposals WHERE id = ?", prop.ID).Scan(&targetEntityID); err != nil {
		t.Fatal(err)
	}
	if !targetEntityID.Valid || targetEntityID.String != applyRes.WorkItem.ID {
		t.Fatalf("expected target_entity_id to be %s, got %v", applyRes.WorkItem.ID, targetEntityID)
	}

	wi, err := wiService.GetByID(ctx, applyRes.WorkItem.ID)
	if err != nil {
		t.Fatalf("expected work item to exist: %v", err)
	}
	if wi.Title != "F13456752" {
		t.Fatalf("unexpected title: %s", wi.Title)
	}

	view, err := trackingService.GetWorkItemTracking(ctx, wi.ID)
	if err != nil || view.Workflow == nil {
		t.Fatalf("expected attached workflow, got %+v err=%v", view, err)
	}
	if len(view.Workflow.Definition.Steps) != 6 {
		t.Fatalf("expected 6 steps, got %d", len(view.Workflow.Definition.Steps))
	}

	if countRows(t, db, "SELECT COUNT(*) FROM revisions") != revisionsBefore+2 {
		t.Fatalf("expected 2 revisions created, got %d", countRows(t, db, "SELECT COUNT(*) FROM revisions")-revisionsBefore)
	}
}
