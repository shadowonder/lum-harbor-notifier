package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/applicationruntime"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/reminder"
	"lum-harbor-notifier/internal/source"
	"lum-harbor-notifier/internal/trackingreevaluation"
	"lum-harbor-notifier/internal/workitem"
	"lum-harbor-notifier/internal/workitemoverview"
)

// planOperation is one operation of a scripted Prompt 2 plan; arguments are
// marshaled into the JSON string the strict schema requires.
type planOperation struct {
	capability string
	arguments  map[string]any
}

// prompt2PlanOutput renders the exact Prompt 2 output envelope a model returns
// for one multi-operation Proposal.
func prompt2PlanOutput(t *testing.T, title string, ops ...planOperation) string {
	t.Helper()
	operations := make([]map[string]any, 0, len(ops))
	for i, op := range ops {
		args, err := json.Marshal(op.arguments)
		if err != nil {
			t.Fatal(err)
		}
		operations = append(operations, map[string]any{"sequence_no": i, "capability_id": op.capability, "arguments": string(args), "reason": nil})
	}
	out, err := json.Marshal(map[string]any{
		"version": 1, "kind": "MUTATION_CANDIDATE", "text": "I prepared one plan for your review.", "candidate": nil,
		"plan": map[string]any{
			"title": title, "summary": "Sets up both engineering work streams, tracks their defects, makes one defect actionable, and adds a personal errand.",
			"reason": "You asked for these changes in one request.", "operations": operations,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func flagshipOperations(linkedDue time.Time) []planOperation {
	return []planOperation{
		{"work_item.create_with_workflow", map[string]any{"title": "transactionDedup", "workflow_name": "transactionDedup Delivery", "stages": []string{"Design", "Dev", "QA", "Release"}}},
		{"tracked_item.create", map[string]any{"work_item_id": "$op[0].work_item_id", "title": "Duplicate Message"}},
		{"tracked_item.create", map[string]any{"work_item_id": "$op[0].work_item_id", "title": "Replay Race"}},
		{"planner.item.create", map[string]any{"title": "Fix Duplicate Message", "tracked_item_id": "$op[1].tracked_item_id", "due_date": linkedDue.Format("2006-01-02"), "due_time": linkedDue.Format("15:04"), "timezone": "UTC"}},
		{"work_item.create_with_workflow", map[string]any{"title": "Infrastructure Tech", "workflow_name": "Infrastructure Delivery", "stages": []string{"Design", "Build", "Release"}}},
		{"tracked_item.create", map[string]any{"work_item_id": "$op[4].work_item_id", "title": "Database Sharding"}},
		{"planner.item.create", map[string]any{"title": "Buy groceries", "due_date": "2030-01-14", "due_time": "17:00", "timezone": "UTC", "remind": true}},
	}
}

const flagshipMessage = "Create two engineering work streams. For transactionDedup create a workflow with Design, Dev, QA, Release. Track defects Duplicate Message and Replay Race. Make Duplicate Message actionable. Create another infrastructure work stream with Design, Build, Release and track Database Sharding. Also remind me to buy groceries at 5 PM."

func (f *round2TestFixture) count(query string, args ...any) int {
	f.t.Helper()
	var n int
	if err := f.db.QueryRow(query, args...).Scan(&n); err != nil {
		f.t.Fatalf("%s: %v", query, err)
	}
	return n
}

func (f *round2TestFixture) str(query string, args ...any) string {
	f.t.Helper()
	var v *string
	if err := f.db.QueryRow(query, args...).Scan(&v); err != nil {
		f.t.Fatalf("%s: %v", query, err)
	}
	if v == nil {
		return ""
	}
	return *v
}

// TestC4FlagshipAssistantAggregateFullChain drives one natural-language turn
// through the production Assistant path (scripted model output only), reviews
// it through the Wails DTO, applies it, and follows the resulting tracking Todo
// through Phase A health/reminders and Phase B completion reevaluation.
func TestC4FlagshipAssistantAggregateFullChain(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()
	now := time.Now().UTC()
	linkedDue := now.Add(30 * time.Hour).Truncate(time.Minute)
	f.mockAI.assistantAsk = prompt2PlanOutput(t, "Engineering streams and groceries", flagshipOperations(linkedDue)...)

	// 1. One turn -> one Source -> one AI Run -> one aggregate Proposal.
	result, err := f.app.AskAssistant(AskAssistantRequestDTO{Message: flagshipMessage, Surface: "dashboard"})
	if err != nil {
		t.Fatalf("AskAssistant: %v", err)
	}
	if result.Kind != "MUTATION_INTENT" || result.ProposalOutcome != AssistantProposalCreated || result.ProposalID == "" || result.ChangeApplied {
		t.Fatalf("assistant result: %+v", result)
	}
	if !strings.Contains(result.Text, "7 actions") {
		t.Fatalf("assistant response should describe the whole plan: %q", result.Text)
	}
	if n := f.count(`SELECT COUNT(*) FROM source_revisions`); n != 1 {
		t.Fatalf("expected exactly one Source revision (the turn), got %d", n)
	}
	turnRevision := f.str(`SELECT id FROM source_revisions`)
	if n := f.count(`SELECT COUNT(*) FROM ai_runs WHERE action = 'assistant.ask'`); n != 1 {
		t.Fatalf("expected exactly one Assistant AI Run, got %d", n)
	}
	runID := f.str(`SELECT id FROM ai_runs WHERE action = 'assistant.ask'`)
	if got := f.str(`SELECT status FROM ai_runs WHERE id = ?`, runID); got != "SUCCEEDED" {
		t.Fatalf("AI Run status %s", got)
	}
	if got := f.str(`SELECT source_revision_id FROM ai_run_sources WHERE ai_run_id = ?`, runID); got != turnRevision {
		t.Fatal("AI Run is not linked to the turn Source")
	}
	if n := f.count(`SELECT COUNT(*) FROM change_proposals`); n != 1 {
		t.Fatalf("one request must create one parent Proposal, got %d", n)
	}
	parent, err := f.proposalService.GetByID(ctx, result.ProposalID)
	if err != nil {
		t.Fatal(err)
	}
	if parent.OperationType != proposal.OperationTypeAggregate || len(parent.Operations) != 7 || parent.AIRunID == nil || *parent.AIRunID != runID ||
		len(parent.SourceRevisionIDs) != 1 || parent.SourceRevisionIDs[0] != turnRevision || parent.Title != "Engineering streams and groceries" {
		t.Fatalf("aggregate parent: %+v", parent)
	}
	if got := f.str(`SELECT state FROM source_processing_state WHERE source_revision_id = ?`, turnRevision); got != "COMPLETED" {
		t.Fatalf("turn Source processing state %s", got)
	}
	if got := f.str(`SELECT proposal_id FROM source_processing_state WHERE source_revision_id = ?`, turnRevision); got != parent.ID {
		t.Fatalf("turn Source processing must reference the aggregate Proposal, got %s", got)
	}
	if n := f.count(`SELECT COUNT(*) FROM work_items`); n != 0 {
		t.Fatal("creating the Proposal must not mutate domain truth")
	}

	// 2. Review: the Wails DTO shows all seven actions in human terms.
	pending, err := f.app.GetPendingProposals()
	if err != nil || len(pending) != 1 || len(pending[0].Operations) != 7 {
		t.Fatalf("pending review DTO: %d %v", len(pending), err)
	}
	raw, _ := json.Marshal(pending[0])
	if strings.Contains(string(raw), "$op[") {
		t.Fatalf("review DTO exposes reference syntax: %s", raw)
	}
	if rel := relationsOf(pending[0].Operations[3]); strings.Join(rel, "|") != "Work Item=Created in action 1 — transactionDedup (action 1)|Tracked Item=Created in action 2 — Duplicate Message (action 2)" {
		t.Fatalf("linked Todo relations: %v", rel)
	}

	// 3. Apply all, atomically.
	revisionsBefore := f.count(`SELECT COUNT(*) FROM revisions`)
	applied, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: parent.ID})
	if err != nil || applied.Outcome != string(proposal.ApplyOutcomeApplied) {
		t.Fatalf("apply: %+v %v", applied, err)
	}
	if n := f.count(`SELECT COUNT(*) FROM work_items`); n != 2 {
		t.Fatalf("expected 2 WorkItems, got %d", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM workflows`); n != 2 {
		t.Fatalf("expected 2 Workflows, got %d", n)
	}
	if n := f.count(`SELECT COUNT(DISTINCT name) FROM workflow_definitions`); n != 2 {
		t.Fatalf("expected 2 distinct workflow definitions, got %d", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM tracked_items`); n != 3 {
		t.Fatalf("expected 3 TrackedItems, got %d", n)
	}
	dedupID := f.str(`SELECT entity_id FROM work_items WHERE title = 'transactionDedup'`)
	infraID := f.str(`SELECT entity_id FROM work_items WHERE title = 'Infrastructure Tech'`)
	dedupWorkflow := f.str(`SELECT id FROM workflows WHERE work_item_id = ?`, dedupID)
	duplicateID := f.str(`SELECT entity_id FROM tracked_items WHERE title = 'Duplicate Message'`)
	for title, workItemID := range map[string]string{"Duplicate Message": dedupID, "Replay Race": dedupID, "Database Sharding": infraID} {
		if got := f.str(`SELECT work_item_id FROM tracked_items WHERE title = ?`, title); got != workItemID {
			t.Fatalf("%s belongs to %s, want %s", title, got, workItemID)
		}
	}
	linkedTodo := f.str(`SELECT entity_id FROM planner_items WHERE title = 'Fix Duplicate Message'`)
	if f.str(`SELECT related_entity_id FROM planner_items WHERE entity_id = ?`, linkedTodo) != duplicateID ||
		f.str(`SELECT work_item_id FROM planner_items WHERE entity_id = ?`, linkedTodo) != dedupID ||
		f.str(`SELECT workflow_id FROM planner_items WHERE entity_id = ?`, linkedTodo) != dedupWorkflow {
		t.Fatal("linked Todo relation is not exact")
	}
	grocery := f.str(`SELECT entity_id FROM planner_items WHERE title = 'Buy groceries'`)
	if f.str(`SELECT work_item_id FROM planner_items WHERE entity_id = ?`, grocery) != "" || f.str(`SELECT related_entity_id FROM planner_items WHERE entity_id = ?`, grocery) != "" {
		t.Fatal("grocery Todo must stay unrelated")
	}
	if n := f.count(`SELECT COUNT(*) FROM todo_reminders WHERE planner_item_id IN (?, ?) AND state = 'ACTIVE'`, linkedTodo, grocery); n != 2 {
		t.Fatalf("expected two active reminders, got %d", n)
	}
	linkedReminder, err := f.app.reminderService.GetByPlannerItemID(ctx, linkedTodo)
	if err != nil || fmt.Sprint(linkedReminder.OffsetsSeconds) != fmt.Sprint([]int{259200, 172800, 86400}) {
		t.Fatalf("linked Todo reminder must use the 3d/2d/1d tracking thresholds: %+v %v", linkedReminder, err)
	}

	// 4. Lineage: every new Revision is named by results and linked to the turn Source; no new Sources.
	results := f.count(`SELECT COUNT(DISTINCT r.revision_id) FROM proposal_operation_results r JOIN proposal_operations o ON o.id = r.proposal_operation_id WHERE o.proposal_id = ?`, parent.ID)
	if newRevisions := f.count(`SELECT COUNT(*) FROM revisions`) - revisionsBefore; newRevisions != results {
		t.Fatalf("results must name every new Revision: %d new, %d named", newRevisions, results)
	}
	if n := f.count(`SELECT COUNT(DISTINCT r.revision_id) FROM proposal_operation_results r JOIN proposal_operations o ON o.id = r.proposal_operation_id
		JOIN revision_sources rs ON rs.revision_id = r.revision_id AND rs.source_revision_id = ? WHERE o.proposal_id = ?`, turnRevision, parent.ID); n != results {
		t.Fatalf("only %d of %d result Revisions link to the turn Source", n, results)
	}
	if n := f.count(`SELECT COUNT(*) FROM source_revisions`); n != 1 {
		t.Fatalf("Apply manufactured Sources: %d", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM change_proposal_sources`); n != 1 {
		t.Fatalf("expected one parent-level Source link, got %d", n)
	}

	// 5. Phase A: health comes from the open linked Todo; the reminder fires
	//    through the existing evaluator into Attention and Notification.
	overview, err := f.app.workItemOverviewService.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var signal *workitemoverview.TrackedItemSignal
	var replaySignal workitemoverview.SignalState
	for _, item := range overview.Items {
		if s, ok := item.TrackedItemSignals[duplicateID]; ok {
			signal = &s
		}
		for id, s := range item.TrackedItemSignals {
			if id != duplicateID && item.WorkItem.ID == dedupID {
				replaySignal = s.State
			}
		}
	}
	if signal == nil || signal.State != workitemoverview.SignalWatch || !signal.HasActiveTrackingTodo {
		t.Fatalf("Duplicate Message health must be WATCH from its Todo due in 30h: %+v", signal)
	}
	if replaySignal != workitemoverview.SignalNeutral {
		t.Fatalf("Replay Race has no Todo and must be NEUTRAL, got %s", replaySignal)
	}
	evaluator := applicationruntime.NewTodoReminderCycle(f.db, f.app.reminderService, f.app.attentionService)
	fired, err := evaluator.EvaluateReminders(ctx, time.Now())
	if err != nil || fired.Fired < 1 {
		t.Fatalf("1-day threshold must fire: %+v %v", fired, err)
	}
	if n := f.count(`SELECT COUNT(*) FROM attention_issues WHERE planner_item_id = ? AND issue_type = 'TODO_REMINDER_DUE' AND state = 'OPEN'`, linkedTodo); n != 1 {
		t.Fatalf("expected one open reminder Attention, got %d", n)
	}
	if n := f.count(`SELECT COUNT(*) FROM notifications WHERE planner_item_id = ?`, linkedTodo); n < 1 {
		t.Fatal("reminder Attention produced no Notification")
	}

	// 6. Phase B: completion -> durable request -> reevaluation -> one follow-up Proposal.
	if _, err := f.app.CompleteLocalTodo(linkedTodo); err != nil {
		t.Fatal(err)
	}
	completed, _ := f.plannerService.GetByID(ctx, linkedTodo)
	request, err := f.trackingReevaluationService.GetByCompletionRevision(ctx, completed.LastRevisionID)
	if err != nil || request.TrackedItemID != duplicateID || request.Status != trackingreevaluation.StatusPending {
		t.Fatalf("completion request: %+v %v", request, err)
	}
	if rem, _ := f.app.reminderService.GetByPlannerItemID(ctx, linkedTodo); rem.State != reminder.StateSatisfied {
		t.Fatal("completion must satisfy the reminder")
	}
	if n := f.count(`SELECT COUNT(*) FROM attention_issues WHERE planner_item_id = ? AND state = 'OPEN'`, linkedTodo); n != 0 {
		t.Fatal("completion must resolve the reminder Attention")
	}
	f.mockAI.interpretDoc = `{"version":"source.interpret.v1","summary":"Verify the fix next.","interpretationDocument":{"candidate":{"version":1,"capability_id":"planner.item.create","arguments":{"title":"Verify Duplicate Message fix","due_date":"2030-02-01","tracked_item_id":"` + duplicateID + `"},"reason":"The original request implies verification."},"plan":null}}`
	cycle, err := f.app.ProcessPendingTrackingReevaluations(ctx)
	if err != nil || cycle.Completed != 1 || cycle.Failed != 0 {
		t.Fatalf("reevaluation cycle: %+v %v", cycle, err)
	}
	request, _ = f.trackingReevaluationService.Get(ctx, request.ID)
	if request.Status != trackingreevaluation.StatusCompleted || request.ProposalID == nil || request.AIRunID == nil {
		t.Fatalf("reevaluation request: %+v", request)
	}
	followUp, err := f.proposalService.GetByID(ctx, *request.ProposalID)
	if err != nil || followUp.IsAggregate() || followUp.OperationType != proposal.OperationTypePlannerItemCreate || *followUp.PlannerItemPayload.TrackedItemID != duplicateID {
		t.Fatalf("follow-up must be one singular linked Todo Proposal: %+v %v", followUp, err)
	}
	if len(followUp.SourceRevisionIDs) == 0 || followUp.SourceRevisionIDs[0] != turnRevision {
		t.Fatalf("follow-up evidence must be the original turn Source: %v", followUp.SourceRevisionIDs)
	}
	if n := f.count(`SELECT COUNT(*) FROM source_revisions`); n != 1 {
		t.Fatal("reevaluation manufactured a Source")
	}
}

func TestC4AssistantSingleOperationPlanAndCandidateStaySingular(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()
	f.mockAI.assistantAsk = prompt2PlanOutput(t, "One todo", planOperation{"planner.item.create", map[string]any{"title": "Single planned todo"}})
	result, err := f.app.AskAssistant(AskAssistantRequestDTO{Message: "Add a todo", Surface: "planner"})
	if err != nil || result.ProposalOutcome != AssistantProposalCreated {
		t.Fatalf("single-operation plan: %+v %v", result, err)
	}
	created, _ := f.proposalService.GetByID(ctx, result.ProposalID)
	if created.IsAggregate() || created.OperationType != proposal.OperationTypePlannerItemCreate {
		t.Fatalf("a one-operation plan must be a singular Proposal: %+v", created)
	}

	f.mockAI.assistantAsk = newMockAIService().assistantAsk // legacy singular candidate without a plan field
	result, err = f.app.AskAssistant(AskAssistantRequestDTO{Message: "Review the budget", Surface: "planner"})
	if err != nil || result.ProposalOutcome != AssistantProposalCreated {
		t.Fatalf("singular candidate: %+v %v", result, err)
	}
	created, _ = f.proposalService.GetByID(ctx, result.ProposalID)
	if created.IsAggregate() || created.OperationType != proposal.OperationTypePlannerItemCreate || created.AIRunID == nil || len(created.SourceRevisionIDs) != 1 {
		t.Fatalf("singular candidate behavior changed: %+v", created)
	}
	if n := f.count(`SELECT COUNT(*) FROM proposal_operations`); n != 0 {
		t.Fatalf("singular Proposals must have no child rows, found %d", n)
	}

	f.mockAI.assistantAsk = `{"version":1,"kind":"ANSWER","text":"Lum Harbor tracks work.","candidate":null,"plan":null}`
	result, err = f.app.AskAssistant(AskAssistantRequestDTO{Message: "What can you do?", Surface: "dashboard"})
	if err != nil || result.Kind != "ANSWER" || result.ProposalID != "" {
		t.Fatalf("answer: %+v %v", result, err)
	}
	if n := f.count(`SELECT COUNT(*) FROM change_proposals`); n != 2 {
		t.Fatalf("an answer must not create a Proposal (%d total)", n)
	}
}

func TestC4AssistantPlanFailsClosed(t *testing.T) {
	many := make([]planOperation, 21)
	for i := range many {
		many[i] = planOperation{"planner.item.create", map[string]any{"title": fmt.Sprintf("Todo %d", i)}}
	}
	stream := planOperation{"work_item.create_with_workflow", map[string]any{"title": "Stream", "workflow_name": "Stream flow", "stages": []string{"Plan", "Ship"}}}
	sameName := planOperation{"work_item.create_with_workflow", map[string]any{"title": "Stream B", "workflow_name": "Stream flow", "stages": []string{"Plan", "Ship"}}}
	cases := map[string]struct {
		output      func(t *testing.T) string
		wantKind    string // empty: AskAssistant returns an error
		wantErrPart string
	}{
		"more than 20 operations": {output: func(t *testing.T) string { return prompt2PlanOutput(t, "Too many", many...) }, wantErrPart: "21 operations, expected 1 to 20"},
		"unknown capability": {output: func(t *testing.T) string {
			return prompt2PlanOutput(t, "Unknown", stream, planOperation{"not.a.capability", map[string]any{}})
		}, wantKind: "UNSUPPORTED"},
		"invalid reference output": {output: func(t *testing.T) string {
			return prompt2PlanOutput(t, "Bad ref", stream, planOperation{"planner.item.create", map[string]any{"title": "A", "tracked_item_id": "$op[0].work_item_id"}})
		}, wantErrPart: "does not supply a tracked_item_id"},
		"forward reference": {output: func(t *testing.T) string {
			return prompt2PlanOutput(t, "Forward", planOperation{"tracked_item.create", map[string]any{"work_item_id": "$op[1].work_item_id", "title": "A"}}, stream)
		}, wantErrPart: "must point to an earlier operation"},
		"reference to an operation without that output": {output: func(t *testing.T) string {
			return prompt2PlanOutput(t, "No workflow", planOperation{"work_item.create", map[string]any{"title": "Bare"}}, planOperation{"tracked_item.create", map[string]any{"work_item_id": "$op[0].work_item_id", "title": "A"}})
		}, wantErrPart: "does not produce workflow_id"},
		"malformed arguments string": {output: func(t *testing.T) string {
			return `{"version":1,"kind":"MUTATION_CANDIDATE","text":"x","candidate":null,"plan":{"title":"P","summary":"S","reason":null,"operations":[{"sequence_no":0,"capability_id":"planner.item.create","arguments":"{not json","reason":null},{"sequence_no":1,"capability_id":"planner.item.create","arguments":"{}","reason":null}]}}`
		}, wantErrPart: "arguments must be a JSON object"},
		"empty operations": {output: func(t *testing.T) string {
			return `{"version":1,"kind":"MUTATION_CANDIDATE","text":"x","candidate":null,"plan":{"title":"P","summary":"S","reason":null,"operations":[]}}`
		}, wantErrPart: "unusable response"},
		"candidate and plan together": {output: func(t *testing.T) string {
			return `{"version":1,"kind":"MUTATION_CANDIDATE","text":"x","candidate":{"version":1,"capability_id":"planner.item.create","arguments":"{\"title\":\"A\"}","reason":null},"plan":{"title":"P","summary":"S","reason":null,"operations":[{"sequence_no":0,"capability_id":"planner.item.create","arguments":"{}","reason":null}]}}`
		}, wantErrPart: "unusable response"},
		"duplicate workflow name collides only at Apply": {output: func(t *testing.T) string { return prompt2PlanOutput(t, "Same names", stream, sameName) }},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newRound2Fixture(t)
			f.mockAI.assistantAsk = tc.output(t)
			result, err := f.app.AskAssistant(AskAssistantRequestDTO{Message: "Do several things", Surface: "dashboard"})
			if name == "duplicate workflow name collides only at Apply" {
				// Preflight cannot see definition names; Apply fails safely and rolls back everything.
				if err != nil || result.ProposalID == "" {
					t.Fatalf("plan should persist for review: %+v %v", result, err)
				}
				if _, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: result.ProposalID}); err == nil {
					t.Fatal("duplicate workflow definition names must fail Apply")
				}
				if n := f.count(`SELECT COUNT(*) FROM work_items`) + f.count(`SELECT COUNT(*) FROM workflows`) + f.count(`SELECT COUNT(*) FROM workflow_definitions`); n != 0 {
					t.Fatalf("failed Apply left %d domain rows", n)
				}
				if got := f.str(`SELECT status FROM change_proposals WHERE id = ?`, result.ProposalID); got != "PENDING" {
					t.Fatalf("failed Apply must leave the parent PENDING, got %s", got)
				}
				return
			}
			if tc.wantKind != "" {
				if err != nil || result.Kind != tc.wantKind || result.ProposalID != "" {
					t.Fatalf("want %s without Proposal: %+v %v", tc.wantKind, result, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantErrPart) || !strings.Contains(err.Error(), "Nothing was changed") {
				t.Fatalf("want a safe error containing %q, got result=%+v err=%v", tc.wantErrPart, result, err)
			}
			if n := f.count(`SELECT COUNT(*) FROM change_proposals`) + f.count(`SELECT COUNT(*) FROM proposal_operations`) + f.count(`SELECT COUNT(*) FROM work_items`) + f.count(`SELECT COUNT(*) FROM planner_items`); n != 0 {
				t.Fatalf("a refused plan left %d rows", n)
			}
			if tc.wantKind == "" {
				if got := f.str(`SELECT state FROM source_processing_state`); got != "FAILED" {
					t.Fatalf("turn Source processing must be FAILED, got %s", got)
				}
			}
		})
	}
}

func interpretPlanDoc(t *testing.T, ops ...planOperation) string {
	t.Helper()
	operations := make([]map[string]any, 0, len(ops))
	for i, op := range ops {
		operations = append(operations, map[string]any{"sequence_no": i, "capability_id": op.capability, "arguments": op.arguments, "reason": "Stated in the source."})
	}
	out, err := json.Marshal(map[string]any{
		"version": "source.interpret.v1", "summary": "A deployment defect has two follow-ups.",
		"interpretationDocument": map[string]any{"candidate": nil, "plan": map[string]any{
			"title": "Deployment defect follow-ups", "summary": "Two actionable follow-ups from the deployment defect report.", "reason": "The source names two separate actions.", "operations": operations,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func captureNote(t *testing.T, f *round2TestFixture, externalID, body string) string {
	t.Helper()
	rev, err := f.sourceService.CaptureLocalSource(context.Background(), source.CaptureLocalSourceInput{Title: "Deployment defect", Body: body, SourceKind: "NOTE", ExternalID: externalID})
	if err != nil {
		t.Fatal(err)
	}
	return rev.ID
}

func TestC4SourceInterpretAggregateNoActionAndSingular(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()
	payments, err := f.workitemService.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: "Payments"})
	if err != nil {
		t.Fatal(err)
	}

	// Multi-action source -> one aggregate Proposal linked to that exact Source and interpret run.
	revID := captureNote(t, f, "DEPLOY-1", "A deployment defect has two actionable follow-ups: investigate duplicate retries by tomorrow and verify reconciliation by Friday.")
	f.mockAI.interpretDoc = interpretPlanDoc(t,
		planOperation{"planner.item.create", map[string]any{"title": "Investigate duplicate retries", "due_date": "2030-01-10", "work_item_id": payments.ID}},
		planOperation{"planner.item.create", map[string]any{"title": "Verify reconciliation", "due_date": "2030-01-11"}},
	)
	sourcesBefore := f.count(`SELECT COUNT(*) FROM source_revisions`)
	interpreted, err := f.app.InterpretSourceCtx(ctx, InterpretSourceRequestDTO{SourceRevisionID: revID})
	if err != nil || !interpreted.HasProposal || interpreted.Proposal == nil {
		t.Fatalf("source plan: %+v %v", interpreted, err)
	}
	dto := interpreted.Proposal
	if dto.OperationType != proposal.OperationTypeAggregate || len(dto.Operations) != 2 {
		t.Fatalf("source plan must create one aggregate: %+v", dto)
	}
	// C3 correction: the Source path returns the resolver-backed review DTO.
	if rel := relationsOf(dto.Operations[0]); strings.Join(rel, "|") != "Work Item=Payments (existing)" {
		t.Fatalf("source aggregate DTO must name existing entities: %v", rel)
	}
	created, _ := f.proposalService.GetByID(ctx, dto.ID)
	if created.AIRunID == nil || *created.AIRunID != interpreted.InterpretRunID || len(created.SourceRevisionIDs) != 1 || created.SourceRevisionIDs[0] != revID {
		t.Fatalf("source aggregate lineage: run=%v sources=%v", created.AIRunID, created.SourceRevisionIDs)
	}
	if got := f.str(`SELECT source_revision_id FROM ai_run_sources WHERE ai_run_id = ?`, interpreted.InterpretRunID); got != revID {
		t.Fatal("interpret run is not linked to the interpreted Source")
	}
	if got := f.str(`SELECT proposal_id FROM source_processing_state WHERE source_revision_id = ?`, revID); got != created.ID {
		t.Fatal("source processing must complete with the aggregate Proposal")
	}
	if n := f.count(`SELECT COUNT(*) FROM source_revisions`); n != sourcesBefore {
		t.Fatal("interpretation manufactured child Sources")
	}
	if _, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: created.ID}); err != nil {
		t.Fatal(err)
	}
	if n := f.count(`SELECT COUNT(*) FROM planner_items WHERE title IN ('Investigate duplicate retries', 'Verify reconciliation')`); n != 2 {
		t.Fatalf("expected both follow-ups applied, got %d", n)
	}

	// NO ACTION stays no action.
	quiet := captureNote(t, f, "DEPLOY-2", "Deployment completed without issues.")
	f.mockAI.interpretDoc = `{"version":"source.interpret.v1","summary":"Informational only.","interpretationDocument":{"candidate":null,"plan":null}}`
	none, err := f.app.InterpretSourceCtx(ctx, InterpretSourceRequestDTO{SourceRevisionID: quiet})
	if err != nil || none.HasProposal {
		t.Fatalf("NO ACTION: %+v %v", none, err)
	}
	if got := f.str(`SELECT state FROM source_processing_state WHERE source_revision_id = ?`, quiet); got != "COMPLETED" {
		t.Fatalf("NO ACTION state %s", got)
	}

	// One operation (candidate, or a one-operation plan) stays singular.
	single := captureNote(t, f, "DEPLOY-3", "Rotate the deployment key by Monday.")
	f.mockAI.interpretDoc = interpretPlanDoc(t, planOperation{"planner.item.create", map[string]any{"title": "Rotate the deployment key", "due_date": "2030-01-12"}})
	one, err := f.app.InterpretSourceCtx(ctx, InterpretSourceRequestDTO{SourceRevisionID: single})
	if err != nil || one.Proposal == nil || one.Proposal.OperationType != proposal.OperationTypePlannerItemCreate || len(one.Proposal.Operations) != 0 {
		t.Fatalf("one-operation plan must be singular: %+v %v", one, err)
	}
}

func TestC4SourceInterpretPlanRefusedForResolvedTrackedItemAndInReevaluation(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()

	// Phase A: a Source resolved to an existing TrackedItem keeps one bounded consequence.
	tracked := createSourceResolvedTrackedItem(t, f, "C4-RESOLVED", "TEST", "Resolved")
	revision := captureTrackingSource(t, f, "C4-RESOLVED", "two follow-ups for the resolved item")
	f.mockAI.interpretDoc = interpretPlanDoc(t,
		planOperation{"planner.item.create", map[string]any{"title": "A", "due_date": "2030-01-10", "tracked_item_id": tracked.ID}},
		planOperation{"planner.item.create", map[string]any{"title": "B", "due_date": "2030-01-11", "tracked_item_id": tracked.ID}},
	)
	if _, err := f.app.InterpretSourceCtx(ctx, InterpretSourceRequestDTO{SourceRevisionID: revision.ID}); err == nil || !strings.Contains(err.Error(), "accepts at most one action") {
		t.Fatalf("resolved Source plan must be refused: %v", err)
	}
	if got := f.str(`SELECT state FROM source_processing_state WHERE source_revision_id = ?`, revision.ID); got != "FAILED" {
		t.Fatalf("refused plan state %s", got)
	}
	if n := f.count(`SELECT COUNT(*) FROM change_proposals`); n != 0 {
		t.Fatalf("refused plan left %d Proposals", n)
	}

	// Phase B: reevaluation remains NO ACTION or one typed Proposal.
	todo, _ := createAppliedTrackingTodoForPhaseB(t, f, "C4-REEVAL")
	if _, err := f.app.CompleteLocalTodo(todo.ID); err != nil {
		t.Fatal(err)
	}
	proposalsBefore := f.count(`SELECT COUNT(*) FROM change_proposals`)
	f.mockAI.interpretDoc = interpretPlanDoc(t,
		planOperation{"planner.item.create", map[string]any{"title": "Next A", "due_date": "2030-01-10", "tracked_item_id": *todo.RelatedEntityID}},
		planOperation{"planner.item.create", map[string]any{"title": "Next B", "due_date": "2030-01-11", "tracked_item_id": *todo.RelatedEntityID}},
	)
	cycle, err := f.app.ProcessPendingTrackingReevaluations(ctx)
	if err != nil || cycle.Completed != 1 || cycle.Failed != 0 {
		t.Fatalf("reevaluation: %+v %v", cycle, err)
	}
	if n := f.count(`SELECT COUNT(*) FROM change_proposals`); n != proposalsBefore {
		t.Fatalf("reevaluation must not create an aggregate (%d -> %d Proposals)", proposalsBefore, n)
	}
	if n := f.count(`SELECT COUNT(*) FROM tracking_reevaluation_requests WHERE state = 'COMPLETED' AND proposal_id IS NULL`); n != 1 {
		t.Fatal("reevaluation with a plan must complete as NO ACTION")
	}
	if !strings.Contains(fmt.Sprint(f.mockAI.lastRequests), "always set plan to null") {
		t.Fatal("reevaluation context must instruct the model never to return a plan")
	}
}

// TestC4ReferenceArgumentsDeclaredExactlyWhereAccepted keeps the AI-facing
// contract equal to the backend: exactly the capability arguments that accept
// a symbolic reference say so in their descriptors.
func TestC4ReferenceArgumentsDeclaredExactlyWhereAccepted(t *testing.T) {
	declared := []string{}
	for _, descriptor := range NewProposeRegistry().ListDescriptors() {
		for name, arg := range descriptor.Arguments {
			if strings.Contains(arg.Description, "Accepts an earlier-operation reference") {
				declared = append(declared, descriptor.ID+"."+name)
			}
		}
	}
	sort.Strings(declared)
	want := []string{"planner.item.create.tracked_item_id", "planner.item.create.work_item_id", "planner.item.create.workflow_id", "tracked_item.create.work_item_id"}
	if strings.Join(declared, ",") != strings.Join(want, ",") {
		t.Fatalf("reference-accepting arguments: %v, want %v", declared, want)
	}
}
