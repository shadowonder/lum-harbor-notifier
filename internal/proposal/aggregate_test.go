package proposal

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"lum-harbor-notifier/internal/history"
)

func TestParseReferenceGrammar(t *testing.T) {
	valid := map[string]Reference{
		"$op[0].work_item_id":     {Index: 0, Output: OutputWorkItemID},
		"$op[7].workflow_id":      {Index: 7, Output: OutputWorkflowID},
		"$op[19].tracked_item_id": {Index: 19, Output: OutputTrackedItemID},
		"$op[3].planner_item_id":  {Index: 3, Output: OutputPlannerItemID},
	}
	for raw, want := range valid {
		got, err := ParseReference(raw)
		if err != nil || got != want || got.String() != raw {
			t.Fatalf("%q: got %+v %v", raw, got, err)
		}
	}
	for _, raw := range []string{"$op[01].work_item_id", "$op[-1].work_item_id", "$op[0].title", "$op[0]", "$op[0].work_item_id.extra",
		"$op[100].work_item_id", "$op[0].work_item_id ", "$op [0].work_item_id", "$op[0].WORK_ITEM_ID"} {
		if _, err := ParseReference(raw); !errors.Is(err, ErrInvalidAggregatePlan) {
			t.Fatalf("%q must be rejected, got %v", raw, err)
		}
	}
	for _, literal := range []string{"Review $op[0].work_item_id", " $op[0].work_item_id", "op[0].work_item_id", "01a1-uuid"} {
		if IsReference(literal) {
			t.Fatalf("%q must be literal data", literal)
		}
	}
}

// prepareStreamPlan prepares WorkItem+Workflow, a TrackedItem in it, and an
// unrelated standalone Todo through the service-level preparation seam.
func prepareStreamPlan(t *testing.T, ctx context.Context, proposals *Service) []PreparedOperation {
	t.Helper()
	stream, err := proposals.PrepareOperation(ctx, ProposeWorkItemCreateWithWorkflowInput{Title: "Payments", WorkflowName: "Delivery", Stages: []string{"Plan", "Ship"}})
	if err != nil {
		t.Fatal(err)
	}
	ref, title := "$op[0].work_item_id", "Ledger"
	tracked, err := proposals.PrepareOperation(ctx, ProposeTrackedItemOperationInput{Payload: TrackedItemOperationPayload{OperationKind: OperationTypeTrackedItemCreate, WorkItemID: &ref, Title: &title}})
	if err != nil {
		t.Fatal(err)
	}
	groceries, err := proposals.PrepareOperation(ctx, ProposePlannerItemCreateInput{Title: "Buy groceries"})
	if err != nil {
		t.Fatal(err)
	}
	return []PreparedOperation{stream, tracked, groceries}
}

func TestCreateAggregateProposalPersistsParentChildrenSourcesAndHistoryAtomically(t *testing.T) {
	ctx, db, _, _, _, proposals := newFullService(t)
	insertTestSourceRevision(t, db, "src-rev-c2b")
	plan := prepareStreamPlan(t, ctx, proposals)
	domainBefore := domainCounts(t, db)

	created, err := proposals.CreateAggregateProposal(ctx, CreateAggregateProposalInput{
		Title: "Payments stream plan", SourceRevisionIDs: []string{"src-rev-c2b"}, Producer: ProducerAI,
		Operations: plan,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.OperationType != OperationTypeAggregate || created.TargetEntityID != nil || created.Status != StatusPending || len(created.Operations) != 3 {
		t.Fatalf("unexpected aggregate parent: %+v", created)
	}

	loaded, err := proposals.GetByID(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i, op := range loaded.Operations {
		if op.SequenceNo != i || op.OperationType != plan[i].OperationType() || op.PayloadJSON != plan[i].PayloadJSON() || op.ProposalID != created.ID {
			t.Fatalf("child %d not persisted as prepared: %+v", i, op)
		}
	}
	if !strings.Contains(loaded.Operations[1].PayloadJSON, `"work_item_id":"$op[0].work_item_id"`) || loaded.Operations[1].TargetEntityID != nil {
		t.Fatalf("symbolic child persisted incorrectly: %+v", loaded.Operations[1])
	}
	var rawPayload string
	if err := db.QueryRow(`SELECT operation_payload_json FROM change_proposals WHERE id = ?`, created.ID).Scan(&rawPayload); err != nil || rawPayload != `{"operation_kind":"proposal.aggregate","version":"v1"}` {
		t.Fatalf("parent payload must stay the compatibility manifest: %q %v", rawPayload, err)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM change_proposal_sources WHERE proposal_id = ? AND source_revision_id = 'src-rev-c2b'`, created.ID); n != 1 {
		t.Fatalf("expected one parent-level Source link, got %d", n)
	}
	if n := countRows(t, db, `SELECT COUNT(*) FROM change_proposal_sources`); n != 1 {
		t.Fatalf("children must not get Source links, found %d links", n)
	}

	events, err := history.NewRepository(db).ListByCorrelation(ctx, history.ProposalCorrelation(created.ID), 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("expected exactly one creation History event: %d %v", len(events), err)
	}
	ev := events[0]
	if ev.Type != history.EventProposalCreated || ev.Actor != history.ActorAI || ev.ProposalID == nil || *ev.ProposalID != created.ID ||
		ev.SourceRevisionID == nil || *ev.SourceRevisionID != "src-rev-c2b" || ev.SubjectEntityID != nil || !strings.Contains(ev.Summary, "3 operations") {
		t.Fatalf("unexpected creation History: %+v", ev)
	}
	assertDomainCounts(t, db, domainBefore)
}

func TestCreateAggregateProposalFailureLeavesNoRows(t *testing.T) {
	ctx, db, _, _, _, proposals := newFullService(t)
	plan := prepareStreamPlan(t, ctx, proposals)
	persisted := func() int {
		return countRows(t, db, `SELECT COUNT(*) FROM change_proposals`) + countRows(t, db, `SELECT COUNT(*) FROM proposal_operations`) +
			countRows(t, db, `SELECT COUNT(*) FROM change_proposal_sources`) + countRows(t, db, `SELECT COUNT(*) FROM history_events WHERE proposal_id IS NOT NULL`)
	}

	if _, err := proposals.CreateAggregateProposal(ctx, CreateAggregateProposalInput{Title: "  ", Operations: plan}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("blank title: %v", err)
	}
	if _, err := proposals.CreateAggregateProposal(ctx, CreateAggregateProposalInput{Title: "Backwards", Operations: []PreparedOperation{plan[1], plan[0]}}); !errors.Is(err, ErrInvalidAggregatePlan) {
		t.Fatalf("forward reference must fail preflight: %v", err)
	}
	if _, err := proposals.CreateAggregateProposal(ctx, CreateAggregateProposalInput{Title: "Empty"}); !errors.Is(err, ErrInvalidAggregatePlan) {
		t.Fatalf("empty plan: %v", err)
	}
	// A child whose concrete target vanished fails inside the creation
	// transaction (FK), after the parent row was already written in it.
	missing := "entity-that-does-not-exist"
	vanished := plan[2]
	vanished.targetEntityID = &missing
	insertTestSourceRevision(t, db, "src-rev-fail")
	if _, err := proposals.CreateAggregateProposal(ctx, CreateAggregateProposalInput{Title: "Vanished target", SourceRevisionIDs: []string{"src-rev-fail"}, Operations: []PreparedOperation{plan[0], vanished}}); err == nil {
		t.Fatal("child persistence failure must fail creation")
	}
	if n := persisted(); n != 0 {
		t.Fatalf("failed creation left %d Proposal rows behind", n)
	}
}

// domainCounts captures domain truth that Proposal creation must never touch.
func domainCounts(t *testing.T, db *sql.DB) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, table := range []string{"entities", "revisions", "revision_sources", "work_items", "workflows", "workflow_definitions", "tracked_items", "planner_items", "todo_reminders", "monitor_points"} {
		out[table] = countRows(t, db, "SELECT COUNT(*) FROM "+table)
	}
	return out
}

func assertDomainCounts(t *testing.T, db *sql.DB, want map[string]int) {
	t.Helper()
	for table, n := range domainCounts(t, db) {
		if want[table] != n {
			t.Fatalf("%s changed from %d to %d", table, want[table], n)
		}
	}
}
