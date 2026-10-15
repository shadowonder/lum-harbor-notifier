package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/proposalcap"
)

// aggregateReviewFixturePath is the ProposalDTO the frontend review tests
// render. TestAggregateProposalReviewDTOMatchesFrontendFixture keeps it equal
// to what the production Wails path returns; set LUM_UPDATE_GOLDEN=1 to
// rewrite it after an intentional DTO change.
const aggregateReviewFixturePath = "../frontend/tests/fixtures/aggregate-proposal-review.json"

func createAggregate(t *testing.T, application *App, title string, calls ...proposalcap.Call) *proposal.Proposal {
	t.Helper()
	ctx := context.Background()
	plan, err := application.proposeRegistry.PrepareAggregatePlan(ctx, application, nil, calls)
	if err != nil {
		t.Fatalf("prepare plan: %v", err)
	}
	summary := "Two engineering defects tracked under a new work stream, one actionable review, and a personal errand."
	reason := "You asked to set up Infrastructure Tech with its delivery workflow, track the sharding and cache defects, make the sharding review actionable, and remind you to buy groceries."
	created, err := application.proposalService.CreateAggregateProposal(ctx, proposal.CreateAggregateProposalInput{
		Title: title, Summary: &summary, Reason: &reason, Producer: proposal.ProducerAI, Operations: plan,
	})
	if err != nil {
		t.Fatalf("create aggregate: %v", err)
	}
	return created
}

func infrastructurePlan() []proposalcap.Call {
	return []proposalcap.Call{
		call(proposal.OperationTypeWorkItemCreateWithWorkflow, `{"title":"Infrastructure Tech","workflow_name":"Standard Delivery","stages":["Design","Dev","Release"]}`),
		call(proposal.OperationTypeTrackedItemCreate, `{"work_item_id":"$op[0].work_item_id","title":"Database Sharding"}`),
		call(proposal.OperationTypeTrackedItemCreate, `{"work_item_id":"$op[0].work_item_id","title":"Cache Invalidation"}`),
		call(proposal.OperationTypePlannerItemCreate, `{"title":"Review Sharding Design","tracked_item_id":"$op[1].tracked_item_id","due_date":"2030-01-15","due_time":"09:00","timezone":"UTC"}`),
		call(proposal.OperationTypePlannerItemCreate, `{"title":"Buy groceries","due_date":"2030-01-14","due_time":"17:00","timezone":"UTC","remind":true}`),
	}
}

func pendingDTO(t *testing.T, application *App, id string) ProposalDTO {
	t.Helper()
	dtos, err := application.GetPendingProposals()
	if err != nil {
		t.Fatal(err)
	}
	for _, dto := range dtos {
		if dto.ID == id {
			return dto
		}
	}
	t.Fatalf("proposal %s not pending", id)
	return ProposalDTO{}
}

// normalizeForFixture replaces generated identities and instants so the
// fixture is deterministic; review content is untouched.
func normalizeForFixture(dto ProposalDTO) ProposalDTO {
	dto.ID = "aggregate-proposal-fixture"
	dto.CreatedAt, dto.UpdatedAt = "2030-01-01T08:00:00Z", "2030-01-01T08:00:00Z"
	for i := range dto.Operations {
		dto.Operations[i].ID = fmt.Sprintf("operation-%d", i)
	}
	return dto
}

func relationsOf(op ProposalOperationDTO) []string {
	out := make([]string, 0, len(op.Relations))
	for _, r := range op.Relations {
		seq := "existing"
		if r.CreatedBySequenceNo != nil {
			seq = fmt.Sprintf("action %d", *r.CreatedBySequenceNo+1)
		}
		out = append(out, fmt.Sprintf("%s=%s (%s)", r.Label, r.Value, seq))
	}
	return out
}

func changesOf(op ProposalOperationDTO) map[string]string {
	out := map[string]string{}
	for _, c := range op.Changes {
		if c.After != nil {
			out[c.Label] = *c.After
		}
	}
	return out
}

func TestAggregateProposalReviewDTOMatchesFrontendFixture(t *testing.T) {
	application := newRegistryTestApp(t)
	created := createAggregate(t, application, "Set up Infrastructure Tech and remind me about groceries", infrastructurePlan()...)
	dto := pendingDTO(t, application, created.ID)

	if dto.OperationType != proposal.OperationTypeAggregate || dto.TargetWorkItemID != nil || len(dto.Changes) != 0 || len(dto.Operations) != 5 {
		t.Fatalf("aggregate parent DTO: type=%s target=%v changes=%d ops=%d", dto.OperationType, dto.TargetWorkItemID, len(dto.Changes), len(dto.Operations))
	}
	wantLabels := []string{"Create Work Item & Workflow", "Create Tracked Item", "Create Tracked Item", "Create Todo", "Create Todo"}
	wantRelations := [][]string{
		{},
		{"Work Item=Created in action 1 — Infrastructure Tech (action 1)"},
		{"Work Item=Created in action 1 — Infrastructure Tech (action 1)"},
		{"Work Item=Created in action 1 — Infrastructure Tech (action 1)", "Tracked Item=Created in action 2 — Database Sharding (action 2)"},
		{},
	}
	for i, op := range dto.Operations {
		if op.SequenceNo != i || op.Label != wantLabels[i] || op.OperationType != created.Operations[i].OperationType {
			t.Fatalf("operation %d: %+v", i, op)
		}
		if got := relationsOf(op); strings.Join(got, "|") != strings.Join(wantRelations[i], "|") {
			t.Fatalf("operation %d relations: %v, want %v", i, got, wantRelations[i])
		}
	}
	stream := changesOf(dto.Operations[0])
	if stream["Title"] != "Infrastructure Tech" || stream["Workflow"] != "Standard Delivery" || stream["Stages"] != "Design, Dev, Release" {
		t.Fatalf("stream changes: %v", stream)
	}
	if got := changesOf(dto.Operations[3]); got["Title"] != "Review Sharding Design" || got["Due"] != "2030-01-15T09:00:00Z" || got["Reminder"] != "3 days before due, 2 days before due, 1 day before due" {
		t.Fatalf("linked Todo changes: %v", got)
	}
	if got := changesOf(dto.Operations[4]); got["Title"] != "Buy groceries" || got["Reminder"] != "At due time" {
		t.Fatalf("grocery Todo changes: %v", got)
	}
	raw, err := json.MarshalIndent(normalizeForFixture(dto), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "$op[") || strings.Contains(string(raw), "operation_kind") {
		t.Fatalf("review DTO leaks reference syntax or payload JSON:\n%s", raw)
	}
	raw = append(raw, '\n')
	if os.Getenv("LUM_UPDATE_GOLDEN") == "1" {
		if err := os.WriteFile(aggregateReviewFixturePath, raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	golden, err := os.ReadFile(aggregateReviewFixturePath)
	if err != nil {
		t.Fatalf("read frontend fixture (run with LUM_UPDATE_GOLDEN=1 to create): %v", err)
	}
	if string(golden) != string(raw) {
		t.Fatalf("frontend review fixture is out of date with the Wails ProposalDTO; rerun with LUM_UPDATE_GOLDEN=1.\nwant:\n%s\ngot:\n%s", golden, raw)
	}
}

func TestAggregateReviewDTOKeepsLiteralTextAndNamesExistingEntities(t *testing.T) {
	application := newRegistryTestApp(t)
	f := newAggregateFixture(t, application)
	created := createAggregate(t, application, "Mixed plan",
		call(proposal.OperationTypeWorkItemCreateWithWorkflow, `{"title":"Stream","workflow_name":"Stream flow","stages":["Plan","Ship"]}`),
		call(proposal.OperationTypeTrackedItemCreate, `{"work_item_id":"$op[0].work_item_id","title":"Review $op[0].work_item_id"}`),
		call(proposal.OperationTypeWorkItemSetLifecycle, fmt.Sprintf(`{"work_item_id":%q,"target_state":"PAUSED"}`, f.workItemID)),
		call(proposal.OperationTypeTrackedItemStageState, fmt.Sprintf(`{"work_item_id":%q,"target_item_title":"API","stage_title":"Build","state":"IN_PROGRESS"}`, f.workItemID)),
		call(proposal.OperationTypePlannerItemCreate, fmt.Sprintf(`{"title":"Follow up","work_item_id":%q}`, f.workItemID)),
	)
	dto := pendingDTO(t, application, created.ID)
	if got := changesOf(dto.Operations[1])["Tracked Item"]; got != "Review $op[0].work_item_id" {
		t.Fatalf("a literal title must stay literal: %q", got)
	}
	want := [][]string{
		{},
		{"Work Item=Created in action 1 — Stream (action 1)"},
		{"Work Item=Payments (existing)"},
		{"Work Item=Payments (existing)", "Tracked Item=API (existing)"},
		{"Work Item=Payments (existing)"},
	}
	for i, op := range dto.Operations {
		if got := relationsOf(op); strings.Join(got, "|") != strings.Join(want[i], "|") {
			t.Fatalf("operation %d relations: %v, want %v", i, got, want[i])
		}
	}
	if got := changesOf(dto.Operations[2])["Lifecycle"]; got != "PAUSED" {
		t.Fatalf("lifecycle change: %v", dto.Operations[2].Changes)
	}
}

// TestAggregateUpdateDetailsCardNamesItsWorkItem covers the C3 audit
// correction: an aggregate Work Item update names the Work Item it changes.
func TestAggregateUpdateDetailsCardNamesItsWorkItem(t *testing.T) {
	application := newRegistryTestApp(t)
	f := newAggregateFixture(t, application)
	created := createAggregate(t, application, "Rename and follow up",
		call(proposal.OperationTypeWorkItemUpdateDetails, fmt.Sprintf(`{"work_item_id":%q,"proposed_title":"Payments v2"}`, f.workItemID)),
		call(proposal.OperationTypePlannerItemCreate, fmt.Sprintf(`{"title":"Announce rename","work_item_id":%q}`, f.workItemID)),
	)
	dto := pendingDTO(t, application, created.ID)
	if got := relationsOf(dto.Operations[0]); strings.Join(got, "|") != "Work Item=Payments (existing)" {
		t.Fatalf("update card relations: %v", got)
	}
	if got := changesOf(dto.Operations[0])["Title"]; got != "Payments v2" {
		t.Fatalf("update card changes: %v", dto.Operations[0].Changes)
	}
}

func TestSingularProposalDTOHasNoOperations(t *testing.T) {
	application := newRegistryTestApp(t)
	created, err := application.proposeRegistry.Execute(context.Background(), application, proposal.OperationTypePlannerItemCreate, nil, json.RawMessage(`{"title":"Plain todo","due_date":"2030-01-14"}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	dto := pendingDTO(t, application, created.ID)
	if dto.Operations == nil || len(dto.Operations) != 0 {
		t.Fatalf("singular DTO must carry an empty operations list: %#v", dto.Operations)
	}
	if got := changesOf(ProposalOperationDTO{Changes: dto.Changes}); len(dto.Changes) != 2 || got["Title"] != "Plain todo" || got["Due"] != "2030-01-14" {
		t.Fatalf("singular planner changes changed: %+v", dto.Changes)
	}
	if dto.Changes[0].Before != nil {
		t.Fatal("singular create change must keep a nil before value")
	}
	raw, err := json.Marshal(dto)
	if err != nil || !strings.Contains(string(raw), `"operations":[]`) {
		t.Fatalf("singular transport must serialize an empty operations array: %s %v", raw, err)
	}
	if strings.Contains(string(raw), "Reminder") || strings.Contains(string(raw), "relations") {
		t.Fatalf("aggregate-only review rows leaked into a singular DTO: %s", raw)
	}
}

func TestApplyAndRejectAggregateThroughWailsMethods(t *testing.T) {
	application := newRegistryTestApp(t)
	applied := createAggregate(t, application, "Apply me", infrastructurePlan()...)
	result, err := application.ApplyProposal(ApplyProposalRequestDTO{ID: applied.ID})
	if err != nil || result.Outcome != string(proposal.ApplyOutcomeApplied) || result.Proposal.Status != "APPLIED" || len(result.Proposal.Operations) != 5 || result.WorkItem != nil {
		t.Fatalf("aggregate Apply result: %+v %v", result, err)
	}
	if result.Proposal.TargetWorkItemID != nil {
		t.Fatal("an aggregate result must not invent a single navigation target")
	}

	rejected := createAggregate(t, application, "Reject me",
		call(proposal.OperationTypeWorkItemCreate, `{"title":"Never created"}`),
		call(proposal.OperationTypePlannerItemCreate, `{"title":"Never scheduled"}`),
	)
	dto, err := application.RejectProposal(RejectProposalRequestDTO{ID: rejected.ID})
	if err != nil || dto.Status != "REJECTED" || len(dto.Operations) != 2 {
		t.Fatalf("aggregate Reject: %+v %v", dto, err)
	}
	if _, err := application.ApplyProposal(ApplyProposalRequestDTO{ID: rejected.ID}); err == nil {
		t.Fatal("a rejected aggregate must not apply")
	}
}
