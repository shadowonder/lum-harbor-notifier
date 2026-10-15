package app

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"lum-harbor-notifier/internal/applicationruntime"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/reminder"
	"lum-harbor-notifier/internal/trackingreevaluation"
)

type failingPhaseBPlannerOperator struct{}

func (failingPhaseBPlannerOperator) CreateLocalTodoTx(context.Context, *sql.Tx, planner.CreateLocalTodoInput) (*planner.PlannerItem, error) {
	return nil, errors.New("forced planner apply failure")
}

func createAppliedTrackingTodoForPhaseB(t *testing.T, f *round2TestFixture, externalID string) (*planner.PlannerItem, string) {
	t.Helper()
	tracked := createSourceResolvedTrackedItem(t, f, externalID, "TEST", "Phase B "+externalID)
	f.mockAI.interpretDoc = `{"version":"source.interpret.v1","summary":"An action is due.","interpretationDocument":{"candidate":{"version":1,"capability_id":"planner.item.create","arguments":{"title":"Finish ` + externalID + `","due_date":"2026-10-10","tracked_item_id":"` + tracked.ID + `"},"reason":"Current evidence requires action."}}}`
	revision := captureTrackingSource(t, f, externalID, "current evidence for "+externalID)
	interpreted, err := f.app.InterpretSourceCtx(context.Background(), InterpretSourceRequestDTO{SourceRevisionID: revision.ID})
	if err != nil || interpreted.Proposal == nil {
		t.Fatalf("create source proposal: result=%+v err=%v", interpreted, err)
	}
	if _, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: interpreted.Proposal.ID}); err != nil {
		t.Fatalf("apply source proposal: %v", err)
	}
	open, err := f.plannerService.ListOpenByTrackedItemID(context.Background(), tracked.ID)
	if err != nil || len(open) != 1 {
		t.Fatalf("load applied tracking Todo: items=%+v err=%v", open, err)
	}
	return &open[0], revision.ID
}

func TestPhaseBCompletionRequestAndNoActionReevaluation(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()
	todo, sourceRevisionID := createAppliedTrackingTodoForPhaseB(t, f, "PB-NO-ACTION")
	trackedID := *todo.RelatedEntityID
	beforeTracking, err := f.trackingService.GetWorkItemTracking(ctx, *todo.WorkItemID)
	if err != nil {
		t.Fatal(err)
	}
	workflowRevision := beforeTracking.Workflow.Workflow.LastRevisionID
	trackedRevision := beforeTracking.Items[0].Item.LastRevisionID

	var sourceStateBefore, sourceUpdatedBefore string
	if err := f.db.QueryRow(`SELECT state, updated_at FROM source_processing_state WHERE source_revision_id = ?`, sourceRevisionID).Scan(&sourceStateBefore, &sourceUpdatedBefore); err != nil {
		t.Fatal(err)
	}
	var sourceCountBefore int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM source_revisions`).Scan(&sourceCountBefore)

	if _, err := f.app.CompleteLocalTodo(todo.ID); err != nil {
		t.Fatal(err)
	}
	completed, _ := f.plannerService.GetByID(ctx, todo.ID)
	request, err := f.trackingReevaluationService.GetByCompletionRevision(ctx, completed.LastRevisionID)
	if err != nil {
		t.Fatalf("durable reevaluation request: %v", err)
	}
	if request.Status != trackingreevaluation.StatusPending || request.TrackedItemID != trackedID {
		t.Fatalf("unexpected completion request: %+v", request)
	}
	rem, err := f.app.reminderService.GetByPlannerItemID(ctx, todo.ID)
	if err != nil || rem.State != reminder.StateSatisfied {
		t.Fatalf("completion did not satisfy reminder: reminder=%+v err=%v", rem, err)
	}

	// Repeating the hook with the same completion Revision is idempotent.
	tx, _ := f.db.BeginTx(ctx, nil)
	if err := f.trackingReevaluationService.RequestForCompletionTx(ctx, tx, todo.ID, completed.LastRevisionID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var requestCount int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM tracking_reevaluation_requests WHERE completion_revision_id = ?`, completed.LastRevisionID).Scan(&requestCount)
	if requestCount != 1 {
		t.Fatalf("duplicate completion created %d requests", requestCount)
	}

	f.mockAI.interpretDoc = `{"version":"source.interpret.v1","summary":"Todo completion does not prove a business state change.","interpretationDocument":{"candidate":null}}`
	result, err := f.app.ProcessPendingTrackingReevaluations(ctx)
	if err != nil || result.Completed != 1 || result.Failed != 0 {
		t.Fatalf("no-action reevaluation: result=%+v err=%v", result, err)
	}
	request, _ = f.trackingReevaluationService.Get(ctx, request.ID)
	if request.Status != trackingreevaluation.StatusCompleted || request.AIRunID == nil || request.ProposalID != nil {
		t.Fatalf("no-action request final state: %+v", request)
	}
	var pendingProposals int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM change_proposals WHERE status = 'PENDING'`).Scan(&pendingProposals)
	if pendingProposals != 0 {
		t.Fatalf("NO ACTION created %d pending Proposals", pendingProposals)
	}
	var sourceCountAfter int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM source_revisions`).Scan(&sourceCountAfter)
	if sourceCountAfter != sourceCountBefore {
		t.Fatalf("reevaluation manufactured Source revisions: before=%d after=%d", sourceCountBefore, sourceCountAfter)
	}
	var sourceStateAfter, sourceUpdatedAfter string
	_ = f.db.QueryRow(`SELECT state, updated_at FROM source_processing_state WHERE source_revision_id = ?`, sourceRevisionID).Scan(&sourceStateAfter, &sourceUpdatedAfter)
	if sourceStateAfter != sourceStateBefore || sourceUpdatedAfter != sourceUpdatedBefore {
		t.Fatalf("reevaluation changed source_processing_state: before=%s/%s after=%s/%s", sourceStateBefore, sourceUpdatedBefore, sourceStateAfter, sourceUpdatedAfter)
	}
	afterTracking, _ := f.trackingService.GetWorkItemTracking(ctx, *todo.WorkItemID)
	if afterTracking.Workflow.Workflow.LastRevisionID != workflowRevision || afterTracking.Items[0].Item.LastRevisionID != trackedRevision {
		t.Fatal("Todo completion or reevaluation mutated Workflow/TrackedItem truth")
	}
}

func TestPhaseBCompletionRequestScopeRollbackAndRestart(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()
	todo, _ := createAppliedTrackingTodoForPhaseB(t, f, "PB-RESTART")
	if _, err := f.app.CompleteLocalTodo(todo.ID); err != nil {
		t.Fatal(err)
	}
	completed, _ := f.plannerService.GetByID(ctx, todo.ID)

	// A normal unrelated Todo never creates a tracking reevaluation request.
	unrelated, err := f.plannerService.CreateLocalTodo(ctx, planner.CreateLocalTodoInput{Title: "Buy groceries"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.app.CompleteLocalTodo(unrelated.ID); err != nil {
		t.Fatal(err)
	}
	var total int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM tracking_reevaluation_requests`).Scan(&total)
	if total != 1 {
		t.Fatalf("unrelated completion changed request count: %d", total)
	}

	// A later hook failure rolls back both completion and the request insert.
	rollbackTodo, err := f.plannerService.CreateLocalTodo(ctx, planner.CreateLocalTodoInput{
		Title: "Rollback", WorkItemID: todo.WorkItemID, WorkflowID: todo.WorkflowID,
		WorkflowStepID: todo.WorkflowStepID, TrackedItemID: todo.RelatedEntityID,
	})
	if err != nil {
		t.Fatal(err)
	}
	f.plannerService.AddCompletionHook(func(context.Context, *sql.Tx, string, string, time.Time) error { return errors.New("forced rollback") })
	if _, err := f.plannerService.CompleteLocalTodo(ctx, rollbackTodo.ID); err == nil {
		t.Fatal("expected forced rollback")
	}
	loaded, _ := f.plannerService.GetByID(ctx, rollbackTodo.ID)
	if loaded.Status != planner.StatusOpen {
		t.Fatalf("rollback Todo status=%s", loaded.Status)
	}
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM tracking_reevaluation_requests`).Scan(&total)
	if total != 1 {
		t.Fatalf("rolled-back completion left request: %d", total)
	}

	// The committed request remains after closing and reopening the real file DB.
	requestBeforeRestart, err := f.trackingReevaluationService.GetByCompletionRevision(ctx, completed.LastRevisionID)
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := f.trackingReevaluationService.Claim(ctx, requestBeforeRestart.ID); err != nil || !claimed {
		t.Fatalf("claim before simulated crash: claimed=%v err=%v", claimed, err)
	}
	f.restart()
	if recovered, err := f.trackingReevaluationService.RecoverInterrupted(ctx); err != nil || recovered != 1 {
		t.Fatalf("startup recovery: recovered=%d err=%v", recovered, err)
	}
	reloaded, err := f.trackingReevaluationService.GetByCompletionRevision(ctx, completed.LastRevisionID)
	if err != nil || reloaded.Status != trackingreevaluation.StatusPending {
		t.Fatalf("request did not survive restart: request=%+v err=%v", reloaded, err)
	}
}

func TestPhaseBReevaluationCreatesOneNextTodoProposal(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()
	todo, sourceRevisionID := createAppliedTrackingTodoForPhaseB(t, f, "PB-NEXT")
	trackedID := *todo.RelatedEntityID
	if _, err := f.app.CompleteLocalTodo(todo.ID); err != nil {
		t.Fatal(err)
	}
	completed, _ := f.plannerService.GetByID(ctx, todo.ID)
	f.mockAI.interpretDoc = `{"version":"source.interpret.v1","summary":"Deployment is the next evidence-backed action.","interpretationDocument":{"candidate":{"version":1,"capability_id":"planner.item.create","arguments":{"title":"Deploy after freeze","due_date":"2026-10-12","tracked_item_id":"` + trackedID + `"},"reason":"Current source evidence identifies the next action."}}}`
	result, err := f.app.ProcessPendingTrackingReevaluations(ctx)
	if err != nil || result.Completed != 1 {
		t.Fatalf("next-action reevaluation: %+v err=%v", result, err)
	}
	request, _ := f.trackingReevaluationService.GetByCompletionRevision(ctx, completed.LastRevisionID)
	if request.ProposalID == nil || request.AIRunID == nil {
		t.Fatalf("request missing result lineage: %+v", request)
	}
	created, err := f.proposalService.GetByID(ctx, *request.ProposalID)
	if err != nil {
		t.Fatal(err)
	}
	if created.OperationType != proposal.OperationTypePlannerItemCreate || created.PlannerItemPayload == nil || created.PlannerItemPayload.TrackedItemID == nil || *created.PlannerItemPayload.TrackedItemID != trackedID {
		t.Fatalf("unexpected next-action Proposal: %+v", created)
	}
	foundEvidence := false
	for _, id := range created.SourceRevisionIDs {
		if id == sourceRevisionID {
			foundEvidence = true
		}
	}
	if !foundEvidence {
		t.Fatalf("Proposal lineage missing existing Source %s: %v", sourceRevisionID, created.SourceRevisionIDs)
	}
	if _, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: created.ID}); err != nil {
		t.Fatal(err)
	}
	open, _ := f.plannerService.ListOpenByTrackedItemID(ctx, trackedID)
	if len(open) != 1 || open[0].Title != "Deploy after freeze" {
		t.Fatalf("new OPEN linked Todo missing: %+v", open)
	}
	original, _ := f.plannerService.GetByID(ctx, todo.ID)
	if original.Status != planner.StatusCompleted {
		t.Fatalf("historical Todo was changed: %+v", original)
	}
}

func TestPhaseBReevaluationFailureIsDurableAndDuplicateCycleSafe(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()
	todo, sourceRevisionID := createAppliedTrackingTodoForPhaseB(t, f, "PB-FAIL")
	if _, err := f.app.CompleteLocalTodo(todo.ID); err != nil {
		t.Fatal(err)
	}
	completed, _ := f.plannerService.GetByID(ctx, todo.ID)
	var sourceStateBefore, sourceUpdatedBefore string
	_ = f.db.QueryRow(`SELECT state, updated_at FROM source_processing_state WHERE source_revision_id = ?`, sourceRevisionID).Scan(&sourceStateBefore, &sourceUpdatedBefore)
	f.mockAI.cleanOutput = `not-json`
	result, err := f.app.ProcessPendingTrackingReevaluations(ctx)
	if err != nil || result.Failed != 1 {
		t.Fatalf("failure cycle result=%+v err=%v", result, err)
	}
	request, _ := f.trackingReevaluationService.GetByCompletionRevision(ctx, completed.LastRevisionID)
	if request.Status != trackingreevaluation.StatusFailed || request.LastError == nil || *request.LastError == "" {
		t.Fatalf("failed request is not diagnosable: %+v", request)
	}
	second, err := f.app.ProcessPendingTrackingReevaluations(ctx)
	if err != nil || second.Scanned != 0 {
		t.Fatalf("duplicate cycle reprocessed terminal request: %+v err=%v", second, err)
	}
	var sourceStateAfter, sourceUpdatedAfter string
	_ = f.db.QueryRow(`SELECT state, updated_at FROM source_processing_state WHERE source_revision_id = ?`, sourceRevisionID).Scan(&sourceStateAfter, &sourceUpdatedAfter)
	if sourceStateAfter != sourceStateBefore || sourceUpdatedAfter != sourceUpdatedBefore {
		t.Fatal("failed reevaluation modified original source processing lifecycle")
	}
	if retried, err := f.trackingReevaluationService.RetryFailed(ctx, request.ID); err != nil || !retried {
		t.Fatalf("failed request is not explicitly retryable: retried=%v err=%v", retried, err)
	}
}

func TestPhaseBPostApplyTriggerRunsExistingReminderEvaluatorWithoutDuplicate(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()
	tracked := createSourceResolvedTrackedItem(t, f, "PB-DUE", "TEST", "Immediate Due")
	due := time.Now().UTC().Add(30 * time.Minute).Format(time.RFC3339)
	prop, err := f.proposalService.ProposePlannerItemCreate(ctx, proposal.ProposePlannerItemCreateInput{
		WorkItemID: &tracked.WorkItemID, WorkflowID: &tracked.WorkflowID, TrackedItemID: &tracked.ID,
		Title: "Threshold already due", DueAt: &due, OffsetsSeconds: []int{3600, 0}, Producer: proposal.ProducerAI,
	})
	if err != nil {
		t.Fatal(err)
	}
	failedProp, err := f.proposalService.ProposePlannerItemCreate(ctx, proposal.ProposePlannerItemCreateInput{
		WorkItemID: &tracked.WorkItemID, WorkflowID: &tracked.WorkflowID, TrackedItemID: &tracked.ID,
		Title: "Rolled-back reminder", DueAt: &due, OffsetsSeconds: []int{3600, 0}, Producer: proposal.ProducerAI,
	})
	if err != nil {
		t.Fatal(err)
	}
	evaluator := applicationruntime.NewTodoReminderCycle(f.db, f.app.reminderService, f.app.attentionService)
	triggered := 0
	f.app.todoReminderTrigger = func() error {
		triggered++
		_, err := evaluator.EvaluateReminders(ctx, time.Now().UTC())
		return err
	}
	f.proposalService.SetPlannerOperator(failingPhaseBPlannerOperator{})
	if _, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: failedProp.ID}); err == nil {
		t.Fatal("expected forced Apply failure")
	}
	f.proposalService.SetPlannerOperator(f.plannerService)
	if triggered != 0 {
		t.Fatalf("failed Apply triggered reminder evaluator: %d", triggered)
	}
	var rolledBackItems int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM planner_items WHERE title = 'Rolled-back reminder'`).Scan(&rolledBackItems)
	if rolledBackItems != 0 {
		t.Fatalf("failed Apply committed %d planner items", rolledBackItems)
	}
	if _, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: prop.ID}); err != nil {
		t.Fatal(err)
	}
	if triggered != 1 {
		t.Fatalf("post-commit reminder trigger count=%d", triggered)
	}
	var notifications int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM notifications`).Scan(&notifications)
	if notifications != 1 {
		t.Fatalf("immediate evaluation notifications=%d", notifications)
	}
	if _, err := evaluator.EvaluateReminders(ctx, time.Now().UTC().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	var after int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM notifications`).Scan(&after)
	if after != notifications {
		t.Fatalf("later cycle duplicated notification: before=%d after=%d", notifications, after)
	}
	var reminderPlannerItemID string
	if err := f.db.QueryRow(`SELECT planner_item_id FROM todo_reminders`).Scan(&reminderPlannerItemID); err != nil {
		t.Fatal(err)
	}
	rem, err := f.app.reminderService.GetByPlannerItemID(ctx, reminderPlannerItemID)
	if err != nil || len(rem.FiredOffsets) != 1 || rem.FiredOffsets[0] != 3600 || rem.State != reminder.StateActive {
		t.Fatalf("future threshold did not remain armed: reminder=%+v err=%v", rem, err)
	}
	if _, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: prop.ID}); err != nil {
		t.Fatal(err)
	}
	if triggered != 1 {
		t.Fatalf("idempotent Apply retriggered evaluator: %d", triggered)
	}
}
