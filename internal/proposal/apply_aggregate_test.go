package proposal

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/database"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/reminder"
	"lum-harbor-notifier/internal/testsupport"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

type applyHarness struct {
	ctx       context.Context
	db        *sql.DB
	workItems *workitem.Service
	tracking  *tracking.Service
	planner   *planner.Service
	proposals *Service
}

// composeServices wires the production Proposal operators over db.
func composeServices(db *sql.DB) applyHarness {
	workItems := workitem.NewService(db, workitem.NewRepository(db))
	trackingService := tracking.NewService(db, workItems)
	plannerService := planner.NewService(db, planner.NewRepository(db))
	proposals := NewService(db, workItems, trackingService)
	proposals.SetPlannerOperator(plannerService)
	proposals.SetReminderOperator(reminder.NewService(db))
	return applyHarness{ctx: context.Background(), db: db, workItems: workItems, tracking: trackingService, planner: plannerService, proposals: proposals}
}

func newApplyHarness(t *testing.T) applyHarness {
	t.Helper()
	return composeServices(testsupport.OpenTestDatabase(t).DB)
}

func (h applyHarness) prepare(t *testing.T, input OperationInput) PreparedOperation {
	t.Helper()
	prepared, err := h.proposals.PrepareOperation(h.ctx, input)
	if err != nil {
		t.Fatalf("prepare %T: %v", input, err)
	}
	return prepared
}

func (h applyHarness) create(t *testing.T, sources []string, operations ...PreparedOperation) *Proposal {
	t.Helper()
	created, err := h.proposals.CreateAggregateProposal(h.ctx, CreateAggregateProposalInput{Title: "Engineering plan", SourceRevisionIDs: sources, Producer: ProducerAI, Operations: operations})
	if err != nil {
		t.Fatalf("create aggregate: %v", err)
	}
	return created
}

func (h applyHarness) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	return countRows(t, h.db, query, args...)
}

func strPtr(s string) *string { return &s }

// engineeringPlan is the flagship request: a work stream with its Workflow,
// two tracked defects, an actionable reminded Todo for the first defect, and
// an unrelated grocery reminder.
func (h applyHarness) engineeringPlan(t *testing.T, workflowName string) []PreparedOperation {
	t.Helper()
	due := time.Now().Add(96 * time.Hour).UTC().Format(time.RFC3339)
	return []PreparedOperation{
		h.prepare(t, ProposeWorkItemCreateWithWorkflowInput{Title: "Checkout stream", WorkflowName: workflowName, Stages: []string{"Triage", "Fix", "Verify"}}),
		h.prepare(t, ProposeTrackedItemOperationInput{Payload: TrackedItemOperationPayload{OperationKind: OperationTypeTrackedItemCreate, WorkItemID: strPtr("$op[0].work_item_id"), Title: strPtr("Defect A")}}),
		h.prepare(t, ProposeTrackedItemOperationInput{Payload: TrackedItemOperationPayload{OperationKind: OperationTypeTrackedItemCreate, WorkItemID: strPtr("$op[0].work_item_id"), Title: strPtr("Defect B")}}),
		h.prepare(t, ProposePlannerItemCreateInput{Title: "Fix Defect A", TrackedItemID: strPtr("$op[1].tracked_item_id"), DueAt: &due, OffsetsSeconds: []int{86400}}),
		h.prepare(t, ProposePlannerItemCreateInput{Title: "Buy groceries", DueAt: &due, OffsetsSeconds: []int{0}}),
	}
}

func TestAggregateApplyCreatesWholePlanAtomicallyWithExactLineage(t *testing.T) {
	h := newApplyHarness(t)
	insertTestSourceRevision(t, h.db, "src-rev-apply")
	created := h.create(t, []string{"src-rev-apply"}, h.engineeringPlan(t, "Defect flow")...)
	revisionsBefore := h.count(t, `SELECT COUNT(*) FROM revisions`)

	result, err := h.proposals.ApplyProposal(h.ctx, ApplyProposalInput{ID: created.ID})
	if err != nil || result.Outcome != ApplyOutcomeApplied {
		t.Fatalf("apply: %+v %v", result, err)
	}
	applied := result.Proposal
	if applied.Status != StatusApplied || applied.TargetEntityID != nil || len(applied.Operations) != 5 {
		t.Fatalf("parent: %+v", applied)
	}

	ops := applied.Operations
	wantResults := []int{2, 1, 1, 2, 2}
	for i, op := range ops {
		if len(op.Results) != wantResults[i] {
			t.Fatalf("operation %d: %d results, want %d: %+v", i, len(op.Results), wantResults[i], op.Results)
		}
		if op.Results[0].Role != ResultRolePrimary {
			t.Fatalf("operation %d: first result must be PRIMARY", i)
		}
	}
	workItemID := *ops[0].Results[0].EntityID
	if ops[0].Results[1].EntityID != nil || ops[0].Results[1].Role != ResultRoleSecondary {
		t.Fatalf("Workflow result must be an entity-less SECONDARY: %+v", ops[0].Results[1])
	}
	if applied.AppliedRevisionID == nil || *applied.AppliedRevisionID != ops[0].Results[0].RevisionID {
		t.Fatalf("applied_revision_id must be the first child's PRIMARY result, got %v", applied.AppliedRevisionID)
	}
	todoResult, reminderResult := ops[3].Results[0], ops[3].Results[1]
	if todoResult.RevisionID != reminderResult.RevisionID || *todoResult.EntityID == *reminderResult.EntityID {
		t.Fatalf("Todo and Reminder must be two entities under one Revision: %+v %+v", todoResult, reminderResult)
	}
	for _, check := range []struct {
		entity, entityType string
	}{{workItemID, "WORK_ITEM"}, {*ops[1].Results[0].EntityID, "TRACKED_ITEM"}, {*ops[2].Results[0].EntityID, "TRACKED_ITEM"},
		{*todoResult.EntityID, "PLANNER_ITEM"}, {*reminderResult.EntityID, "TODO_REMINDER"}, {*ops[4].Results[0].EntityID, "PLANNER_ITEM"}, {*ops[4].Results[1].EntityID, "TODO_REMINDER"}} {
		if n := h.count(t, `SELECT COUNT(*) FROM entities WHERE id = ? AND entity_type = ?`, check.entity, check.entityType); n != 1 {
			t.Fatalf("result entity %s is not a %s", check.entity, check.entityType)
		}
	}

	// Exact relations: symbolic references reached the domain as the real IDs.
	wf, err := h.tracking.GetWorkflowByWorkItem(h.ctx, workItemID)
	if err != nil {
		t.Fatal(err)
	}
	if ops[0].Results[1].RevisionID != wf.LastRevisionID {
		t.Fatal("Workflow result does not name the Workflow's Revision")
	}
	defectA := *ops[1].Results[0].EntityID
	for _, defect := range []string{defectA, *ops[2].Results[0].EntityID} {
		item, err := h.tracking.GetTrackedItem(h.ctx, defect)
		if err != nil || item.WorkItemID != workItemID || item.WorkflowID != wf.ID {
			t.Fatalf("defect %s not created in the new WorkItem/Workflow: %+v %v", defect, item, err)
		}
	}
	var todoWorkItem, todoWorkflow, todoTracked sql.NullString
	if err := h.db.QueryRow(`SELECT work_item_id, workflow_id, related_entity_id FROM planner_items WHERE entity_id = ?`, *todoResult.EntityID).Scan(&todoWorkItem, &todoWorkflow, &todoTracked); err != nil {
		t.Fatal(err)
	}
	if todoWorkItem.String != workItemID || todoWorkflow.String != wf.ID || todoTracked.String != defectA {
		t.Fatalf("Todo relation: wi=%v wf=%v tracked=%v", todoWorkItem, todoWorkflow, todoTracked)
	}
	var groceryWorkItem sql.NullString
	if err := h.db.QueryRow(`SELECT work_item_id FROM planner_items WHERE entity_id = ?`, *ops[4].Results[0].EntityID).Scan(&groceryWorkItem); err != nil || groceryWorkItem.Valid {
		t.Fatalf("unrelated grocery Todo must stay standalone: %v %v", groceryWorkItem, err)
	}
	if n := h.count(t, `SELECT COUNT(*) FROM todo_reminders WHERE planner_item_id IN (?, ?) AND state = 'ACTIVE'`, *todoResult.EntityID, *ops[4].Results[0].EntityID); n != 2 {
		t.Fatalf("expected two active reminders, got %d", n)
	}

	// Source lineage: every distinct committed Revision links to the parent Source.
	distinct := map[string]bool{}
	for _, op := range ops {
		for _, r := range op.Results {
			distinct[r.RevisionID] = true
		}
	}
	if newRevisions := h.count(t, `SELECT COUNT(*) FROM revisions`) - revisionsBefore; newRevisions != len(distinct) {
		t.Fatalf("results must name every new Revision: %d new, %d in results", newRevisions, len(distinct))
	}
	for revisionID := range distinct {
		if n := h.count(t, `SELECT COUNT(*) FROM revision_sources WHERE revision_id = ? AND source_revision_id = 'src-rev-apply' AND role = 'PRIMARY_SOURCE'`, revisionID); n != 1 {
			t.Fatalf("revision %s lacks parent Source lineage", revisionID)
		}
	}
	if n := h.count(t, `SELECT COUNT(*) FROM revision_sources`); n != len(distinct) {
		t.Fatalf("expected exactly %d revision_sources rows, got %d", len(distinct), n)
	}
	if n := h.count(t, `SELECT COUNT(*) FROM change_proposal_sources`) + h.count(t, `SELECT COUNT(*) FROM source_revisions`); n != 2 {
		t.Fatalf("Apply must not manufacture Sources or child Source links (found %d rows)", n)
	}

	events, err := history.NewRepository(h.db).ListByCorrelation(h.ctx, history.ProposalCorrelation(created.ID), 50)
	if err != nil {
		t.Fatal(err)
	}
	appliedEvents := 0
	for _, ev := range events {
		if ev.Type == history.EventProposalApplied {
			appliedEvents++
			if ev.RevisionID == nil || *ev.RevisionID != *applied.AppliedRevisionID || !strings.Contains(ev.Summary, "5 operations") {
				t.Fatalf("PROPOSAL_APPLIED: %+v", ev)
			}
		}
	}
	if appliedEvents != 1 {
		t.Fatalf("expected one PROPOSAL_APPLIED event, got %d", appliedEvents)
	}

	// Repeated Apply is the existing idempotent no-op.
	revisionsAfter := h.count(t, `SELECT COUNT(*) FROM revisions`)
	again, err := h.proposals.ApplyProposal(h.ctx, ApplyProposalInput{ID: created.ID})
	if err != nil || again.Outcome != ApplyOutcomeAlreadyApplied || h.count(t, `SELECT COUNT(*) FROM revisions`) != revisionsAfter {
		t.Fatalf("repeated Apply: %+v %v", again, err)
	}
}

func TestAggregateApplyMidPlanFailureRollsBackEveryChild(t *testing.T) {
	h := newApplyHarness(t)
	insertTestSourceRevision(t, h.db, "src-rev-fail")
	plan := []PreparedOperation{
		h.prepare(t, ProposeWorkItemCreateWithWorkflowInput{Title: "Stream A", WorkflowName: "Shared name", Stages: []string{"Plan", "Ship"}}),
		h.prepare(t, ProposeTrackedItemOperationInput{Payload: TrackedItemOperationPayload{OperationKind: OperationTypeTrackedItemCreate, WorkItemID: strPtr("$op[0].work_item_id"), Title: strPtr("Defect")}}),
		h.prepare(t, ProposePlannerItemCreateInput{Title: "Buy groceries", OffsetsSeconds: nil}),
		// Passes preflight, then fails in the domain: WorkflowDefinition
		// (name, version) is unique and child 0 already used this name.
		h.prepare(t, ProposeWorkItemCreateWithWorkflowInput{Title: "Stream B", WorkflowName: "Shared name", Stages: []string{"Plan", "Ship"}}),
	}
	created := h.create(t, []string{"src-rev-fail"}, plan...)
	before := domainCounts(t, h.db)
	historyBefore := h.count(t, `SELECT COUNT(*) FROM history_events`)

	if _, err := h.proposals.ApplyProposal(h.ctx, ApplyProposalInput{ID: created.ID}); err == nil || !strings.Contains(err.Error(), "aggregate operation 3") {
		t.Fatalf("expected the fourth child to fail Apply: %v", err)
	}
	assertDomainCounts(t, h.db, before)
	if n := h.count(t, `SELECT COUNT(*) FROM proposal_operation_results`); n != 0 {
		t.Fatalf("rolled-back Apply left %d result rows", n)
	}
	if n := h.count(t, `SELECT COUNT(*) FROM history_events`); n != historyBefore {
		t.Fatalf("rolled-back Apply left History (%d -> %d)", historyBefore, n)
	}
	reloaded, err := h.proposals.GetByID(h.ctx, created.ID)
	if err != nil || reloaded.Status != StatusPending || reloaded.AppliedRevisionID != nil || len(reloaded.Operations) != 4 {
		t.Fatalf("a non-stale failure must leave the parent PENDING and intact: %+v %v", reloaded, err)
	}
}

func TestAggregateApplyStaleChildRollsBackAndMarksParentStale(t *testing.T) {
	h := newApplyHarness(t)
	wi, err := h.workItems.CreateLocalWorkItem(h.ctx, workitem.CreateLocalWorkItemInput{Title: "Existing"})
	if err != nil {
		t.Fatal(err)
	}
	def, err := h.tracking.CreateWorkflowDefinition(h.ctx, tracking.CreateWorkflowDefinitionInput{Name: "Existing flow", Version: 1, Steps: []tracking.WorkflowStepInput{{Key: "plan", Title: "Plan"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.tracking.CreateWorkflow(h.ctx, tracking.CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	item, err := h.tracking.CreateTrackedItem(h.ctx, tracking.CreateTrackedItemInput{WorkItemID: wi.ID, Title: "API"})
	if err != nil {
		t.Fatal(err)
	}
	created := h.create(t, nil,
		h.prepare(t, ProposeWorkItemCreateInput{Title: "New stream"}),
		h.prepare(t, ProposePlannerItemCreateInput{Title: "Buy groceries"}),
		h.prepare(t, ProposeTrackedItemOperationInput{Payload: TrackedItemOperationPayload{OperationKind: OperationTypeTrackedItemUpdate, TrackedItemID: &item.ID, Title: strPtr("API v2")}}),
	)
	// Authoritative truth changes after review began.
	if _, err := h.tracking.UpdateTrackedItem(h.ctx, tracking.UpdateTrackedItemInput{ID: item.ID, Title: "API renamed elsewhere"}); err != nil {
		t.Fatal(err)
	}
	before := domainCounts(t, h.db)

	result, err := h.proposals.ApplyProposal(h.ctx, ApplyProposalInput{ID: created.ID})
	if err != nil || result.Outcome != ApplyOutcomeStale || result.Proposal.Status != StatusStale {
		t.Fatalf("stale child must make the aggregate STALE: %+v %v", result, err)
	}
	assertDomainCounts(t, h.db, before)
	if n := h.count(t, `SELECT COUNT(*) FROM proposal_operation_results`); n != 0 {
		t.Fatalf("stale Apply left %d result rows", n)
	}
	if n := h.count(t, `SELECT COUNT(*) FROM history_events WHERE proposal_id = ? AND event_type = 'PROPOSAL_STALE'`, created.ID); n != 1 {
		t.Fatalf("expected one PROPOSAL_STALE event, got %d", n)
	}
	if _, err := h.proposals.ApplyProposal(h.ctx, ApplyProposalInput{ID: created.ID}); !errors.Is(err, ErrInvalidStatusTransition) {
		t.Fatalf("a STALE aggregate cannot be applied: %v", err)
	}
}

func TestRejectAggregateKeepsChildrenAndBlocksApply(t *testing.T) {
	h := newApplyHarness(t)
	insertTestSourceRevision(t, h.db, "src-rev-reject")
	created := h.create(t, []string{"src-rev-reject"}, h.engineeringPlan(t, "Reject flow")...)
	before := domainCounts(t, h.db)

	rejected, err := h.proposals.RejectProposal(h.ctx, RejectProposalInput{ID: created.ID})
	if err != nil || rejected.Status != StatusRejected || len(rejected.Operations) != 5 {
		t.Fatalf("reject: %+v %v", rejected, err)
	}
	if len(rejected.SourceRevisionIDs) != 1 || rejected.SourceRevisionIDs[0] != "src-rev-reject" {
		t.Fatalf("parent Source links changed: %v", rejected.SourceRevisionIDs)
	}
	if _, err := h.proposals.ApplyProposal(h.ctx, ApplyProposalInput{ID: created.ID}); !errors.Is(err, ErrInvalidStatusTransition) {
		t.Fatalf("a rejected aggregate cannot be applied: %v", err)
	}
	assertDomainCounts(t, h.db, before)
	if n := h.count(t, `SELECT COUNT(*) FROM proposal_operations WHERE proposal_id = ?`, created.ID); n != 5 {
		t.Fatalf("children must remain durable review evidence, found %d", n)
	}
}

func TestPendingAggregateSurvivesRestartAndApplies(t *testing.T) {
	ctx := context.Background()
	first := testsupport.OpenTestDatabase(t)
	h := composeServices(first.DB)
	insertTestSourceRevision(t, h.db, "src-rev-restart")
	created := h.create(t, []string{"src-rev-restart"}, h.engineeringPlan(t, "Restart flow")...)
	if err := first.DB.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := database.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.DB.Close() }()
	h2 := composeServices(reopened.DB)
	loaded, err := h2.proposals.GetByID(ctx, created.ID)
	if err != nil || loaded.Status != StatusPending || len(loaded.Operations) != 5 {
		t.Fatalf("reload after restart: %+v %v", loaded, err)
	}
	for i, op := range loaded.Operations {
		if op.PayloadJSON != created.Operations[i].PayloadJSON {
			t.Fatalf("child %d payload changed across restart", i)
		}
	}
	result, err := h2.proposals.ApplyProposal(ctx, ApplyProposalInput{ID: created.ID})
	if err != nil || result.Outcome != ApplyOutcomeApplied {
		t.Fatalf("apply after restart: %+v %v", result, err)
	}
	if n := countRows(t, reopened.DB, `SELECT COUNT(*) FROM proposal_operation_results`); n != 8 {
		t.Fatalf("expected 8 durable result rows, got %d", n)
	}
}

func TestSchedulesTodoReminderCoversSingularAndAggregate(t *testing.T) {
	h := newApplyHarness(t)
	due := time.Now().Add(48 * time.Hour).UTC().Format(time.RFC3339)
	singular, err := h.proposals.ProposePlannerItemCreate(h.ctx, ProposePlannerItemCreateInput{Title: "Reminded", DueAt: &due, OffsetsSeconds: []int{3600}})
	if err != nil {
		t.Fatal(err)
	}
	plain, err := h.proposals.ProposePlannerItemCreate(h.ctx, ProposePlannerItemCreateInput{Title: "Unreminded"})
	if err != nil {
		t.Fatal(err)
	}
	reminded := h.create(t, nil, h.prepare(t, ProposeWorkItemCreateInput{Title: "W"}), h.prepare(t, ProposePlannerItemCreateInput{Title: "R", DueAt: &due, OffsetsSeconds: []int{0}}))
	unreminded := h.create(t, nil, h.prepare(t, ProposeWorkItemCreateInput{Title: "W2"}), h.prepare(t, ProposePlannerItemCreateInput{Title: "U"}))
	if !singular.SchedulesTodoReminder() || plain.SchedulesTodoReminder() || !reminded.SchedulesTodoReminder() || unreminded.SchedulesTodoReminder() {
		t.Fatal("SchedulesTodoReminder misclassified a Proposal")
	}
}

// TestAggregateUpdateDetailsHistoryIsCausedByParentApply proves domain History
// that singular Apply writes after PROPOSAL_APPLIED keeps that causation, and
// the parent correlation, when the operation is an aggregate child.
func TestAggregateUpdateDetailsHistoryIsCausedByParentApply(t *testing.T) {
	h := newApplyHarness(t)
	wi, err := h.workItems.CreateLocalWorkItem(h.ctx, workitem.CreateLocalWorkItemInput{Title: "Old title"})
	if err != nil {
		t.Fatal(err)
	}
	created := h.create(t, nil,
		h.prepare(t, ProposeWorkItemUpdateInput{TargetWorkItemID: wi.ID, ProposedTitle: "New title"}),
		h.prepare(t, ProposePlannerItemCreateInput{Title: "Follow up", WorkItemID: &wi.ID}),
	)
	if result, err := h.proposals.ApplyProposal(h.ctx, ApplyProposalInput{ID: created.ID}); err != nil || result.Outcome != ApplyOutcomeApplied {
		t.Fatalf("apply: %+v %v", result, err)
	}
	updated, err := h.workItems.GetByID(h.ctx, wi.ID)
	if err != nil || updated.Title != "New title" {
		t.Fatalf("details not applied: %+v %v", updated, err)
	}
	events, err := history.NewRepository(h.db).ListByCorrelation(h.ctx, history.ProposalCorrelation(created.ID), 50)
	if err != nil {
		t.Fatal(err)
	}
	byType := map[history.EventType]history.Event{}
	for _, ev := range events {
		byType[ev.Type] = ev
	}
	applied, changed := byType[history.EventProposalApplied], byType[history.EventWorkItemDetailsChanged]
	if applied.ID == "" || changed.ID == "" || changed.CausationEventID == nil || *changed.CausationEventID != applied.ID ||
		changed.ProposalID == nil || *changed.ProposalID != created.ID {
		t.Fatalf("details History must be caused by the parent PROPOSAL_APPLIED: applied=%+v changed=%+v", applied, changed)
	}
}
