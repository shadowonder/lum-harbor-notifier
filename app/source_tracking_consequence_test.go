package app

import (
	"context"
	"strings"
	"testing"

	"lum-harbor-notifier/internal/foundation"
	"lum-harbor-notifier/internal/identity"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/reminder"
	"lum-harbor-notifier/internal/source"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

func createSourceResolvedTrackedItem(t *testing.T, f *round2TestFixture, externalID, system, title string) *tracking.TrackedItem {
	t.Helper()
	ctx := context.Background()
	wi, err := f.workitemService.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: title + " Work"})
	if err != nil {
		t.Fatal(err)
	}
	def, err := f.trackingService.CreateWorkflowDefinition(ctx, tracking.CreateWorkflowDefinitionInput{
		Name: title + " Flow", Version: 1,
		Steps: []tracking.WorkflowStepInput{{Key: "work", Title: "Work"}, {Key: "done", Title: "Done", Terminal: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.trackingService.CreateWorkflow(ctx, tracking.CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	item, err := f.trackingService.CreateTrackedItem(ctx, tracking.CreateTrackedItemInput{WorkItemID: wi.ID, Title: title})
	if err != nil {
		t.Fatal(err)
	}
	refID, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := foundation.NewRepository(f.db).CreateExternalRef(ctx, refID, item.ID, system, externalID, foundation.RelationshipAuthoritative, nil); err != nil {
		t.Fatal(err)
	}
	return item
}

func captureTrackingSource(t *testing.T, f *round2TestFixture, externalID, body string) *source.SourceRevision {
	t.Helper()
	rev, err := f.sourceService.CaptureLocalSource(context.Background(), source.CaptureLocalSourceInput{
		Title: "Tracking evidence", Body: body, SourceKind: "NOTE", ExternalID: externalID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return rev
}

func TestPhaseASourceResolvedTrackingConsequenceSuppressionAndRecurrence(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()
	tracked := createSourceResolvedTrackedItem(t, f, "TRACK-42", "TEST", "Release Gate")
	f.mockAI.interpretDoc = `{"version":"source.interpret.v1","summary":"The gate needs follow-up.","interpretationDocument":{"candidate":{"version":1,"capability_id":"planner.item.create","arguments":{"title":"Follow up on release gate","due_date":"2026-10-10","tracked_item_id":"` + tracked.ID + `"},"reason":"Current evidence requires user action."}}}`

	firstSource := captureTrackingSource(t, f, "TRACK-42", "first evidence")
	first, err := f.app.InterpretSourceCtx(ctx, InterpretSourceRequestDTO{SourceRevisionID: firstSource.ID})
	if err != nil {
		t.Fatalf("interpret first source: %v", err)
	}
	if !first.HasProposal || first.Proposal == nil {
		t.Fatalf("expected tracking Todo proposal: %+v", first)
	}
	requestText := f.mockAI.lastRequests["source.interpret"].Messages[len(f.mockAI.lastRequests["source.interpret"].Messages)-1].Content
	if !strings.Contains(requestText, `"resolvedTrackedItemContext"`) || !strings.Contains(requestText, tracked.ID) || !strings.Contains(requestText, `"openTodos":[]`) {
		t.Fatalf("bounded resolved context missing from interpretation request: %s", requestText)
	}

	pending, err := f.proposalService.GetByID(ctx, first.Proposal.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pending.PlannerItemPayload == nil || pending.PlannerItemPayload.TrackedItemID == nil || *pending.PlannerItemPayload.TrackedItemID != tracked.ID {
		t.Fatalf("proposal lost typed TrackedItem identity: %+v", pending.PlannerItemPayload)
	}
	if got := pending.PlannerItemPayload.OffsetsSeconds; len(got) != 3 || got[0] != 259200 || got[1] != 172800 || got[2] != 86400 {
		t.Fatalf("tracking reminder defaults mismatch: %v", got)
	}
	if _, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: pending.ID}); err != nil {
		t.Fatal(err)
	}
	linked, err := f.plannerService.ListOpenByTrackedItemID(ctx, tracked.ID)
	if err != nil || len(linked) != 1 {
		t.Fatalf("expected one applied linked Todo: items=%+v err=%v", linked, err)
	}
	rem, err := f.app.reminderService.GetByPlannerItemID(ctx, linked[0].ID)
	if err != nil || rem.State != reminder.StateActive || len(rem.OffsetsSeconds) != 3 {
		t.Fatalf("expected active 3/2/1-day reminder: reminder=%+v err=%v", rem, err)
	}
	var proposalSourceCount, revisionSourceCount int
	if err := f.db.QueryRow("SELECT COUNT(*) FROM change_proposal_sources WHERE proposal_id = ? AND source_revision_id = ?", pending.ID, firstSource.ID).Scan(&proposalSourceCount); err != nil {
		t.Fatal(err)
	}
	if err := f.db.QueryRow("SELECT COUNT(*) FROM revision_sources WHERE revision_id = ? AND source_revision_id = ?", linked[0].LastRevisionID, firstSource.ID).Scan(&revisionSourceCount); err != nil {
		t.Fatal(err)
	}
	if proposalSourceCount != 1 || revisionSourceCount != 1 {
		t.Fatalf("Source/Proposal/Revision lineage missing: proposal=%d revision=%d", proposalSourceCount, revisionSourceCount)
	}

	secondSource := captureTrackingSource(t, f, "TRACK-42", "equivalent current evidence")
	second, err := f.app.InterpretSourceCtx(ctx, InterpretSourceRequestDTO{SourceRevisionID: secondSource.ID})
	if err != nil {
		t.Fatalf("interpret equivalent source: %v", err)
	}
	if second.HasProposal || !strings.Contains(second.Notice, "Equivalent active action") {
		t.Fatalf("equivalent active consequence was not suppressed: %+v", second)
	}
	requestText = f.mockAI.lastRequests["source.interpret"].Messages[len(f.mockAI.lastRequests["source.interpret"].Messages)-1].Content
	if !strings.Contains(requestText, linked[0].ID) || !strings.Contains(requestText, `"state":"ACTIVE"`) || !strings.Contains(requestText, "259200") {
		t.Fatalf("active Todo/reminder state missing from bounded context: %s", requestText)
	}

	if _, err := f.plannerService.CompleteLocalTodo(ctx, linked[0].ID); err != nil {
		t.Fatal(err)
	}
	thirdSource := captureTrackingSource(t, f, "TRACK-42", "new evidence after completion")
	third, err := f.app.InterpretSourceCtx(ctx, InterpretSourceRequestDTO{SourceRevisionID: thirdSource.ID})
	if err != nil {
		t.Fatalf("interpret recurrence: %v", err)
	}
	if !third.HasProposal || third.Proposal == nil {
		t.Fatalf("completed historical Todo must allow recurrence: %+v", third)
	}
	if third.Proposal.OperationType != proposal.OperationTypePlannerItemCreate {
		t.Fatalf("unexpected recurrence operation: %s", third.Proposal.OperationType)
	}
}

func TestPhaseASourceExternalIdentityAmbiguityFailsClosed(t *testing.T) {
	f := newRound2Fixture(t)
	createSourceResolvedTrackedItem(t, f, "SHARED-7", "SYSTEM-A", "First")
	createSourceResolvedTrackedItem(t, f, "SHARED-7", "SYSTEM-B", "Second")
	rev := captureTrackingSource(t, f, "SHARED-7", "ambiguous evidence")

	_, err := f.app.InterpretSourceCtx(context.Background(), InterpretSourceRequestDTO{SourceRevisionID: rev.ID})
	if err == nil || !strings.Contains(err.Error(), "ambiguous external id") {
		t.Fatalf("expected ambiguity to fail closed, got %v", err)
	}
	state, err := f.sourceService.GetProcessingState(context.Background(), rev.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.State != source.ProcessingStatusFailed {
		t.Fatalf("ambiguous source must be FAILED, got %+v", state)
	}
}

func TestPhaseASourceNoCandidateMeansNoAction(t *testing.T) {
	f := newRound2Fixture(t)
	f.mockAI.interpretDoc = `{"version":"source.interpret.v1","summary":"No current action is required.","interpretationDocument":{"candidate":null}}`
	src, err := f.app.CaptureLocalSource(LocalSourceCaptureRequestDTO{Title: "FYI", Content: "Status only"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.app.InterpretSourceCtx(context.Background(), InterpretSourceRequestDTO{SourceRevisionID: src.SourceRevisionID})
	if err != nil {
		t.Fatal(err)
	}
	if result.HasProposal {
		t.Fatalf("no-action interpretation manufactured a Proposal: %+v", result)
	}
}
