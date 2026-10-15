package proposal

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

// insertTestSourceRevision creates one immutable SourceRevision for parent
// Source-link assertions.
func insertTestSourceRevision(t *testing.T, db *sql.DB, id string) {
	t.Helper()
	now := "2026-10-03T12:00:00Z"
	recordID, hash := "rec-"+id, strings.Repeat("b", 64)
	if _, err := db.Exec(`INSERT INTO source_records (id, extension_id, instance_id, source_kind, external_id, current_revision_id, current_content_hash, first_seen_at, last_seen_at, created_at, updated_at)
		VALUES (?, 'core.assistant', 'default', 'USER_INTENT', ?, NULL, NULL, ?, ?, ?, ?)`, recordID, "ext-"+id, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO source_revisions (id, source_record_id, content_hash, payload_json, observed_at, created_at) VALUES (?, ?, ?, '{"text":"plan"}', ?, ?)`, id, recordID, hash, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE source_records SET current_revision_id = ?, current_content_hash = ? WHERE id = ?`, id, hash, recordID); err != nil {
		t.Fatal(err)
	}
}

// createInTx persists p through the transaction-bound Repository, as every
// production writer does.
func createInTx(t *testing.T, ctx context.Context, db *sql.DB, p *Proposal) error {
	t.Helper()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := NewRepository(tx).Create(ctx, p); err != nil {
		return err
	}
	return tx.Commit()
}

func newAggregate(id string, operations ...ProposalOperation) *Proposal {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	title := "Engineering plan"
	return &Proposal{
		ID: id, OperationType: OperationTypeAggregate, Title: title, Status: StatusPending,
		Operations: operations, CreatedAt: now, UpdatedAt: now,
	}
}

const symbolicChildPayload = `{"operation_kind":"tracked_item.create","version":"v1","work_item_id":"$op[0].work_item_id","workflow_id":"$op[0].workflow_id","title":"Title mentioning $op[0].work_item_id literally"}`

func TestAggregateProposalPersistenceRoundTrip(t *testing.T) {
	ctx, db, wiService, _, _, _ := newFullService(t)
	existing := createTestWorkItem(t, ctx, wiService, "Groceries home", nil)
	insertTestSourceRevision(t, db, "src-rev-agg")

	createWithWorkflow := `{"operation_kind":"work_item.create_with_workflow","version":"v1","title":"Payments","workflow_name":"Delivery","stages":["Plan","Ship"]}`
	lifecycle := `{"operation_kind":"workitem.set_lifecycle","version":"v1","target_state":"PAUSED"}`
	// Supplied out of order: persistence order is SequenceNo, not slice order.
	p := newAggregate("prop-agg-roundtrip",
		ProposalOperation{ID: "op-c", SequenceNo: 2, OperationType: OperationTypeWorkItemSetLifecycle, TargetEntityID: &existing.ID, PayloadJSON: lifecycle},
		ProposalOperation{ID: "op-a", SequenceNo: 0, OperationType: OperationTypeWorkItemCreateWithWorkflow, PayloadJSON: createWithWorkflow},
		ProposalOperation{ID: "op-b", SequenceNo: 1, OperationType: OperationTypeTrackedItemCreate, PayloadJSON: symbolicChildPayload},
	)
	p.SourceRevisionIDs = []string{"src-rev-agg"}
	if err := createInTx(t, ctx, db, p); err != nil {
		t.Fatalf("create aggregate: %v", err)
	}

	var rawType, rawPayload string
	var rawTarget, rawApplied *string
	if err := db.QueryRow(`SELECT operation_type, operation_payload_json, target_entity_id, applied_revision_id FROM change_proposals WHERE id = ?`, p.ID).Scan(&rawType, &rawPayload, &rawTarget, &rawApplied); err != nil {
		t.Fatal(err)
	}
	if rawType != OperationTypeAggregate || rawPayload != `{"operation_kind":"proposal.aggregate","version":"v1"}` || rawTarget != nil || rawApplied != nil {
		t.Fatalf("parent singular columns must hold only compatibility values: %q %q %v %v", rawType, rawPayload, rawTarget, rawApplied)
	}

	repo := NewRepository(db)
	loaded, err := repo.GetByID(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	pending, err := repo.ListPending(ctx)
	if err != nil || len(pending) != 1 {
		t.Fatalf("list pending: %d %v", len(pending), err)
	}
	bySource, err := repo.FindPendingBySourceRevision(ctx, "src-rev-agg")
	if err != nil || bySource == nil {
		t.Fatalf("find by source: %v %v", bySource, err)
	}
	for name, got := range map[string]*Proposal{"GetByID": loaded, "ListPending": &pending[0], "FindPendingBySourceRevision": bySource} {
		if !got.IsAggregate() || len(got.Operations) != 3 {
			t.Fatalf("%s: expected aggregate with 3 operations, got %+v", name, got.Operations)
		}
		if got.TargetEntityID != nil || got.AppliedRevisionID != nil || got.Status != StatusPending {
			t.Fatalf("%s: unexpected parent fields %+v", name, got)
		}
		if len(got.SourceRevisionIDs) != 1 || got.SourceRevisionIDs[0] != "src-rev-agg" {
			t.Fatalf("%s: parent Source link changed: %v", name, got.SourceRevisionIDs)
		}
		want := []struct {
			id, opType, payload string
			target              *string
		}{
			{"op-a", OperationTypeWorkItemCreateWithWorkflow, createWithWorkflow, nil},
			{"op-b", OperationTypeTrackedItemCreate, symbolicChildPayload, nil},
			{"op-c", OperationTypeWorkItemSetLifecycle, lifecycle, &existing.ID},
		}
		for i, op := range got.Operations {
			if op.SequenceNo != i || op.ID != want[i].id || op.ProposalID != p.ID || op.OperationType != want[i].opType {
				t.Fatalf("%s: operation %d out of order or wrong: %+v", name, i, op)
			}
			// Verbatim persistence: no reference scanning or rewriting.
			if op.PayloadJSON != want[i].payload {
				t.Fatalf("%s: payload %d not byte-identical: %s", name, i, op.PayloadJSON)
			}
			if !optionalStringEqual(op.TargetEntityID, want[i].target) {
				t.Fatalf("%s: target %d = %v", name, i, op.TargetEntityID)
			}
			if op.Results != nil {
				t.Fatalf("%s: pending operation must have no results", name)
			}
		}
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM change_proposal_sources WHERE proposal_id = ?`, p.ID); n != 1 {
		t.Fatalf("expected exactly one parent-level Source link, got %d", n)
	}
}

func TestAggregateProposalResultsRoundTrip(t *testing.T) {
	ctx, db, wiService, _, _, _ := newFullService(t)
	wi := createTestWorkItem(t, ctx, wiService, "Payments", nil)
	now := "2026-10-03T12:00:00Z"
	for _, entity := range []string{"todo-1", "reminder-1"} {
		if _, err := db.Exec(`INSERT INTO entities (id, entity_type, created_at) VALUES (?, 'PLANNER_ITEM', ?)`, entity, now); err != nil {
			t.Fatal(err)
		}
	}
	for _, rev := range []string{"rev-workflow", "rev-todo"} {
		if _, err := db.Exec(`INSERT INTO revisions (id, origin_type, applied_at) VALUES (?, 'USER', ?)`, rev, now); err != nil {
			t.Fatal(err)
		}
	}
	p := newAggregate("prop-agg-results",
		ProposalOperation{ID: "op-0", SequenceNo: 0, OperationType: OperationTypeWorkItemCreateWithWorkflow, PayloadJSON: `{}`},
		ProposalOperation{ID: "op-1", SequenceNo: 1, OperationType: OperationTypePlannerItemCreate, PayloadJSON: `{}`},
	)
	if err := createInTx(t, ctx, db, p); err != nil {
		t.Fatal(err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	repo := NewRepository(tx)
	todo, reminderEntity := "todo-1", "reminder-1"
	results := []ProposalOperationResult{
		// One child producing two Revisions; the Workflow Revision has no Entity.
		{ProposalOperationID: "op-0", SequenceNo: 0, EntityID: &wi.ID, RevisionID: wi.LastRevisionID, Role: ResultRolePrimary},
		{ProposalOperationID: "op-0", SequenceNo: 1, RevisionID: "rev-workflow", Role: ResultRoleSecondary},
		// One Revision covering two Entities.
		{ProposalOperationID: "op-1", SequenceNo: 0, EntityID: &todo, RevisionID: "rev-todo", Role: ResultRolePrimary},
		{ProposalOperationID: "op-1", SequenceNo: 1, EntityID: &reminderEntity, RevisionID: "rev-todo", Role: ResultRoleSecondary},
	}
	if err := repo.CreateOperationResults(ctx, results); err != nil {
		t.Fatal(err)
	}
	stamp := time.Date(2026, 10, 3, 13, 0, 0, 0, time.UTC)
	applied, err := repo.MarkProposalAppliedIfPending(ctx, p.ID, wi.LastRevisionID, stamp, stamp)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if applied.Status != StatusApplied || applied.AppliedRevisionID == nil || *applied.AppliedRevisionID != wi.LastRevisionID || len(applied.Operations) != 2 {
		t.Fatalf("applied aggregate: %+v", applied)
	}

	loaded, err := NewRepository(db).GetByID(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	var got []ProposalOperationResult
	for _, op := range loaded.Operations {
		for _, result := range op.Results {
			if result.ProposalOperationID != op.ID {
				t.Fatalf("result attached to wrong operation: %+v under %s", result, op.ID)
			}
			got = append(got, result)
		}
	}
	if len(got) != len(results) {
		t.Fatalf("expected %d results, got %d", len(results), len(got))
	}
	for i := range results {
		w, g := results[i], got[i]
		if g.ProposalOperationID != w.ProposalOperationID || g.SequenceNo != w.SequenceNo || g.RevisionID != w.RevisionID || g.Role != w.Role || !optionalStringEqual(g.EntityID, w.EntityID) {
			t.Fatalf("result %d: want %+v got %+v", i, w, g)
		}
	}

	if err := NewRepository(db).CreateOperationResults(ctx, []ProposalOperationResult{{ProposalOperationID: "op-1", SequenceNo: 2, RevisionID: "rev-todo", Role: "WORK_ITEM"}}); !errors.Is(err, ErrInvalidOperationPayload) {
		t.Fatalf("a domain type is not a result role: %v", err)
	}
}

func TestAggregateProposalCreateRejectsInconsistentShape(t *testing.T) {
	ctx, db, _, _, _, _ := newFullService(t)
	child := func(id string, seq int) ProposalOperation {
		return ProposalOperation{ID: id, SequenceNo: seq, OperationType: OperationTypeWorkItemCreate, PayloadJSON: `{}`}
	}

	empty := newAggregate("prop-empty")
	if err := createInTx(t, ctx, db, empty); !errors.Is(err, ErrInvalidOperationPayload) {
		t.Fatalf("aggregate without operations: %v", err)
	}
	singularWithChild := newAggregate("prop-singular-child", child("op-s", 0))
	singularWithChild.OperationType = OperationTypeWorkItemCreate
	singularWithChild.WorkItemCreatePayload = &WorkItemCreatePayload{OperationKind: OperationTypeWorkItemCreate, Version: OperationVersionV1, Title: "A"}
	if err := createInTx(t, ctx, db, singularWithChild); !errors.Is(err, ErrInvalidOperationPayload) {
		t.Fatalf("singular with child operation: %v", err)
	}
	withResults := child("op-r", 0)
	withResults.Results = []ProposalOperationResult{{RevisionID: "rev", Role: ResultRolePrimary}}
	if err := createInTx(t, ctx, db, newAggregate("prop-with-results", withResults)); !errors.Is(err, ErrInvalidOperationPayload) {
		t.Fatalf("operation created with results: %v", err)
	}
	foreign := child("op-f", 0)
	foreign.ProposalID = "another-proposal"
	if err := createInTx(t, ctx, db, newAggregate("prop-foreign", foreign)); !errors.Is(err, ErrInvalidOperationPayload) {
		t.Fatalf("operation of another parent: %v", err)
	}
	// A DB-level child failure rolls back the whole parent write.
	if err := createInTx(t, ctx, db, newAggregate("prop-dup", child("op-d1", 0), child("op-d2", 0))); err == nil {
		t.Fatal("duplicate sequence must fail")
	}
	invalidJSON := child("op-j", 0)
	invalidJSON.PayloadJSON = `{not json`
	if err := createInTx(t, ctx, db, newAggregate("prop-json", invalidJSON)); err == nil {
		t.Fatal("malformed child payload must fail")
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM change_proposals`); n != 0 {
		t.Fatalf("rejected shapes must leave no parent rows, found %d", n)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM proposal_operations`); n != 0 {
		t.Fatalf("rejected shapes must leave no child rows, found %d", n)
	}
}

func TestProposalLoadFailsClosedWhenDiscriminatorDisagreesWithChildren(t *testing.T) {
	ctx, db, wiService, _, _, proposals := newFullService(t)
	if _, err := db.Exec(`INSERT INTO change_proposals (id, target_entity_id, operation_type, title, status, operation_payload_json, created_at, updated_at)
		VALUES ('prop-orphan-agg', NULL, 'proposal.aggregate', 'Plan', 'PENDING', '{"operation_kind":"proposal.aggregate","version":"v1"}', '2026-10-03T12:00:00Z', '2026-10-03T12:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRepository(db).GetByID(ctx, "prop-orphan-agg"); !errors.Is(err, ErrInvalidOperationPayload) {
		t.Fatalf("aggregate parent without children must not load as singular: %v", err)
	}

	wi := createTestWorkItem(t, ctx, wiService, "Target", nil)
	singular, err := proposals.ProposeWorkItemLifecycle(ctx, ProposeWorkItemLifecycleInput{TargetWorkItemID: wi.ID, TargetState: "PAUSED"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO proposal_operations (id, proposal_id, sequence_no, operation_type, operation_payload_json) VALUES ('op-stray', ?, 0, 'work_item.create', '{}')`, singular.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRepository(db).GetByID(ctx, singular.ID); !errors.Is(err, ErrInvalidOperationPayload) {
		t.Fatalf("singular parent with child rows must not load: %v", err)
	}
}

// TestLegacySingularProposalsUnchangedByAggregatePersistence proves singular
// Proposals carry zero child rows, reload with nil Operations, and still
// apply/reject/stale through the existing singular paths without writing any
// aggregate result rows.
func TestLegacySingularProposalsUnchangedByAggregatePersistence(t *testing.T) {
	ctx, db, wiService, trackingService, _, proposals := newFullService(t)
	insertTestSourceRevision(t, db, "src-rev-legacy")
	sources := []string{"src-rev-legacy"}
	existing := createTestWorkItem(t, ctx, wiService, "Existing", nil)
	def, err := trackingService.CreateWorkflowDefinition(ctx, tracking.CreateWorkflowDefinitionInput{Name: "Existing process", Version: 1, Steps: []tracking.WorkflowStepInput{{Key: "plan", Title: "Plan"}}})
	if err != nil {
		t.Fatal(err)
	}
	wf, err := trackingService.CreateWorkflow(ctx, tracking.CreateWorkflowInput{WorkItemID: existing.ID, WorkflowDefinitionID: def.ID})
	if err != nil {
		t.Fatal(err)
	}

	withWorkflow, err := proposals.ProposeWorkItemCreateWithWorkflow(ctx, ProposeWorkItemCreateWithWorkflowInput{Title: "Payments", WorkflowName: "Delivery", Stages: []string{"Plan", "Ship"}, SourceRevisionIDs: sources, Producer: ProducerAI})
	if err != nil {
		t.Fatal(err)
	}
	workItemCreate, err := proposals.ProposeWorkItemCreate(ctx, ProposeWorkItemCreateInput{Title: "Standalone", Producer: ProducerAI})
	if err != nil {
		t.Fatal(err)
	}
	plannerCreate, err := proposals.ProposePlannerItemCreate(ctx, ProposePlannerItemCreateInput{Title: "Buy groceries", Producer: ProducerAI})
	if err != nil {
		t.Fatal(err)
	}
	update, err := proposals.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{TargetWorkItemID: existing.ID, ProposedTitle: "Renamed"})
	if err != nil {
		t.Fatal(err)
	}
	trackedTitle := "API"
	trackedCreate, err := proposals.ProposeTrackedItemOperation(ctx, ProposeTrackedItemOperationInput{Payload: TrackedItemOperationPayload{OperationKind: OperationTypeTrackedItemCreate, WorkItemID: &existing.ID, WorkflowID: &wf.ID, Title: &trackedTitle}})
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := proposals.ProposeWorkItemLifecycle(ctx, ProposeWorkItemLifecycleInput{TargetWorkItemID: existing.ID, TargetState: "PAUSED"})
	if err != nil {
		t.Fatal(err)
	}

	all := []*Proposal{withWorkflow, workItemCreate, plannerCreate, update, trackedCreate, lifecycle}
	rawPayloads := map[string]string{}
	for _, p := range all {
		var raw string
		if err := db.QueryRow(`SELECT operation_payload_json FROM change_proposals WHERE id = ?`, p.ID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		rawPayloads[p.ID] = raw
		loaded, err := proposals.GetByID(ctx, p.ID)
		if err != nil {
			t.Fatalf("%s reload: %v", p.OperationType, err)
		}
		if loaded.IsAggregate() || loaded.Operations != nil {
			t.Fatalf("%s must load as singular with nil Operations: %+v", p.OperationType, loaded.Operations)
		}
		if loaded.OperationType != p.OperationType || !optionalStringEqual(loaded.TargetEntityID, p.TargetEntityID) {
			t.Fatalf("%s reload changed parent fields: %+v", p.OperationType, loaded)
		}
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM proposal_operations`); n != 0 {
		t.Fatalf("singular Proposals must not write child rows, found %d", n)
	}
	if pending, err := proposals.ListPending(ctx); err != nil || len(pending) != len(all) {
		t.Fatalf("list pending: %d %v", len(pending), err)
	}

	// Apply work_item.create_with_workflow through the unchanged singular path.
	result, err := proposals.ApplyProposal(ctx, ApplyProposalInput{ID: withWorkflow.ID})
	if err != nil || result.Outcome != ApplyOutcomeApplied || result.WorkItem == nil {
		t.Fatalf("apply create_with_workflow: %+v %v", result, err)
	}
	createdWF, err := trackingService.GetWorkflowByWorkItem(ctx, result.WorkItem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Proposal.AppliedRevisionID == nil || *result.Proposal.AppliedRevisionID != createdWF.LastRevisionID {
		t.Fatalf("singular create_with_workflow must keep the Workflow Revision as applied_revision_id: %v", result.Proposal.AppliedRevisionID)
	}
	for _, rev := range []string{result.WorkItem.LastRevisionID, createdWF.LastRevisionID} {
		if n := countRows(t, db, `SELECT COUNT(*) FROM revision_sources WHERE revision_id = ? AND source_revision_id = 'src-rev-legacy'`, rev); n != 1 {
			t.Fatalf("revision %s lost its Source lineage", rev)
		}
	}

	if r, err := proposals.ApplyProposal(ctx, ApplyProposalInput{ID: workItemCreate.ID}); err != nil || r.Outcome != ApplyOutcomeApplied {
		t.Fatalf("apply work_item.create: %+v %v", r, err)
	}
	if r, err := proposals.ApplyProposal(ctx, ApplyProposalInput{ID: plannerCreate.ID}); err != nil || r.Outcome != ApplyOutcomeApplied {
		t.Fatalf("apply planner.item.create: %+v %v", r, err)
	}
	if r, err := proposals.ApplyProposal(ctx, ApplyProposalInput{ID: trackedCreate.ID}); err != nil || r.Outcome != ApplyOutcomeApplied {
		t.Fatalf("apply tracked_item.create: %+v %v", r, err)
	}
	if rejected, err := proposals.RejectProposal(ctx, RejectProposalInput{ID: lifecycle.ID}); err != nil || rejected.Status != StatusRejected {
		t.Fatalf("reject singular: %+v %v", rejected, err)
	}
	if _, err := wiService.UpdateLocalWorkItemDetails(ctx, workitem.UpdateLocalWorkItemDetailsInput{ID: existing.ID, Title: "Changed elsewhere"}); err != nil {
		t.Fatal(err)
	}
	if r, err := proposals.ApplyProposal(ctx, ApplyProposalInput{ID: update.ID}); err != nil || r.Outcome != ApplyOutcomeStale {
		t.Fatalf("stale singular: %+v %v", r, err)
	}

	for _, p := range all {
		var raw string
		if err := db.QueryRow(`SELECT operation_payload_json FROM change_proposals WHERE id = ?`, p.ID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		if raw != rawPayloads[p.ID] {
			t.Fatalf("%s parent payload rewritten: %s", p.OperationType, raw)
		}
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM proposal_operations`) + countRows(t, db, `SELECT COUNT(*) FROM proposal_operation_results`); n != 0 {
		t.Fatalf("singular Apply/Reject/Stale must not write aggregate rows, found %d", n)
	}
}
