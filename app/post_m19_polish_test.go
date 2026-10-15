package app

import (
	"context"
	"strings"
	"testing"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/assistant"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/readcap"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

// --- R4-01: Description-Only and Semantic WorkItem Updates -----------------

func TestR401_DescriptionOnlyWorkItemUpdate(t *testing.T) {
	candidate := `{"version":1,"capability_id":"work_item.update_details","arguments":{"proposed_description":"Updated final acceptance criteria"},"reason":"clarification"}`
	h := newMutationHarness(t, candidate)

	result, err := h.app.AskAssistant(AskAssistantRequestDTO{
		Message:    "Update the description of Real Persisted Item to: Updated final acceptance criteria",
		Surface:    "WORK_ITEMS",
		WorkItemID: h.item.ID,
	})
	if err != nil {
		t.Fatalf("unexpected AskAssistant error: %v", err)
	}
	if result.Kind != "MUTATION_INTENT" || result.ProposalOutcome != AssistantProposalCreated || result.ProposalID == "" {
		t.Fatalf("expected Proposal created, got %+v", result)
	}

	// Verify proposal preserved the authoritative current title
	created, err := h.app.proposalService.GetByID(context.Background(), result.ProposalID)
	if err != nil {
		t.Fatal(err)
	}
	if created.OperationType != proposal.OperationTypeWorkItemUpdateDetails {
		t.Fatalf("unexpected operation type: %s", created.OperationType)
	}
	payload := created.Payload
	if payload.ProposedTitle != "Real Persisted Item" {
		t.Fatalf("expected preserved title 'Real Persisted Item', got %q", payload.ProposedTitle)
	}
	if payload.ProposedDescription == nil || *payload.ProposedDescription != "Updated final acceptance criteria" {
		t.Fatalf("expected updated description, got %+v", payload.ProposedDescription)
	}
	if payload.Preconditions.ExpectedTitle != "Real Persisted Item" {
		t.Fatalf("expected precondition title 'Real Persisted Item', got %q", payload.Preconditions.ExpectedTitle)
	}
	if payload.Preconditions.ExpectedDescription == nil || *payload.Preconditions.ExpectedDescription != "Original description" {
		t.Fatalf("expected precondition description 'Original description', got %+v", payload.Preconditions.ExpectedDescription)
	}

	// Apply the Proposal
	applied, err := h.app.ApplyProposal(ApplyProposalRequestDTO{ID: result.ProposalID})
	if err != nil {
		t.Fatalf("unexpected apply error: %v", err)
	}
	if applied.Outcome != "APPLIED" || applied.WorkItem == nil {
		t.Fatalf("expected applied result, got %+v", applied)
	}
	if applied.WorkItem.Title != "Real Persisted Item" {
		t.Fatalf("title should remain 'Real Persisted Item', got %q", applied.WorkItem.Title)
	}
	if applied.WorkItem.Description == nil || *applied.WorkItem.Description != "Updated final acceptance criteria" {
		t.Fatalf("description was not updated: %+v", applied.WorkItem.Description)
	}

	// Authoritative verification
	after, err := h.service.GetByID(context.Background(), h.item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Title != "Real Persisted Item" || after.Description == nil || *after.Description != "Updated final acceptance criteria" {
		t.Fatalf("authoritative work item state mismatch: %+v", after)
	}
}

func TestR401_TitleOnlyWorkItemUpdate(t *testing.T) {
	candidate := `{"version":1,"capability_id":"work_item.update_details","arguments":{"proposed_title":"Renamed Title"},"reason":"rename"}`
	h := newMutationHarness(t, candidate)

	result, err := h.app.AskAssistant(AskAssistantRequestDTO{
		Message:    "Rename the work item to: Renamed Title",
		Surface:    "WORK_ITEMS",
		WorkItemID: h.item.ID,
	})
	if err != nil {
		t.Fatalf("unexpected AskAssistant error: %v", err)
	}
	if result.Kind != "MUTATION_INTENT" || result.ProposalOutcome != AssistantProposalCreated || result.ProposalID == "" {
		t.Fatalf("expected Proposal created, got %+v", result)
	}

	created, err := h.app.proposalService.GetByID(context.Background(), result.ProposalID)
	if err != nil {
		t.Fatal(err)
	}
	payload := created.Payload
	if payload.ProposedTitle != "Renamed Title" {
		t.Fatalf("expected proposed title 'Renamed Title', got %q", payload.ProposedTitle)
	}
	if payload.ProposedDescription == nil || *payload.ProposedDescription != "Original description" {
		t.Fatalf("expected preserved description, got %+v", payload.ProposedDescription)
	}

	applied, err := h.app.ApplyProposal(ApplyProposalRequestDTO{ID: result.ProposalID})
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}
	if applied.Outcome != "APPLIED" || applied.WorkItem.Title != "Renamed Title" || applied.WorkItem.Description == nil || *applied.WorkItem.Description != "Original description" {
		t.Fatalf("unexpected applied work item: %+v", applied.WorkItem)
	}
}

func TestR401_NeitherChangedProducesNoProposal(t *testing.T) {
	candidate := `{"version":1,"capability_id":"work_item.update_details","arguments":{},"reason":"same"}`
	h := newMutationHarness(t, candidate)

	result, err := h.app.AskAssistant(AskAssistantRequestDTO{
		Message:    "Do nothing",
		Surface:    "WORK_ITEMS",
		WorkItemID: h.item.ID,
	})
	if err != nil {
		t.Fatalf("unexpected AskAssistant error: %v", err)
	}
	if result.ProposalOutcome != AssistantProposalNoChange || result.ProposalID != "" {
		t.Fatalf("expected AssistantProposalNoChange, got %+v", result)
	}

	// Repeating identical values also produces no change
	h.executor.outputs[ai.ActionAssistantAsk] = `{"version":1,"kind":"MUTATION_CANDIDATE","text":"Nothing to change.","candidate":{"version":1,"capability_id":"work_item.update_details","arguments":{"proposed_title":"Real Persisted Item","proposed_description":"Original description"},"reason":"same"}}`
	result2, err := h.app.AskAssistant(AskAssistantRequestDTO{
		Message:    "Keep same",
		Surface:    "WORK_ITEMS",
		WorkItemID: h.item.ID,
	})
	if err != nil {
		t.Fatalf("unexpected AskAssistant error: %v", err)
	}
	if result2.ProposalOutcome != AssistantProposalNoChange || result2.ProposalID != "" {
		t.Fatalf("expected AssistantProposalNoChange for identical values, got %+v", result2)
	}
}

func TestR401_BoundedValidationExplanationSurfaced(t *testing.T) {
	candidate := `{"version":1,"capability_id":"work_item.update_details","arguments":{"proposed_title":"   "},"reason":"blank"}`
	h := newMutationHarness(t, candidate)

	_, err := h.app.AskAssistant(AskAssistantRequestDTO{
		Message:    "Set blank title",
		Surface:    "WORK_ITEMS",
		WorkItemID: h.item.ID,
	})
	if err == nil {
		t.Fatal("expected validation error")
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "proposed_title must not be blank") {
		t.Fatalf("expected bounded error message containing 'proposed_title must not be blank', got %q", errMsg)
	}
	if !strings.HasPrefix(errMsg, "The Assistant could not create the Proposal:") {
		t.Fatalf("expected prefix 'The Assistant could not create the Proposal:', got %q", errMsg)
	}
	if !strings.HasSuffix(errMsg, "Nothing was changed.") {
		t.Fatalf("expected suffix 'Nothing was changed.', got %q", errMsg)
	}
}

// --- R4-02: TrackedItem Read Context and Progression -----------------------

func TestR402_TrackedItemProgressionReadContext(t *testing.T) {
	application, service, db := newWorkItemApp(t)
	item, err := service.CreateLocalWorkItem(context.Background(), workitem.CreateLocalWorkItemInput{Title: "Payment Gateway"})
	if err != nil {
		t.Fatal(err)
	}

	trackingSvc := tracking.NewService(db, service)
	application.trackingService = trackingSvc

	def, err := trackingSvc.CreateWorkflowDefinition(context.Background(), tracking.CreateWorkflowDefinitionInput{
		Name:    "payment-workflow",
		Version: 1,
		Steps: []tracking.WorkflowStepInput{
			{Key: "specs", Title: "Specification", Terminal: false},
			{Key: "dev", Title: "Development", Terminal: false},
			{Key: "qa", Title: "Quality Assurance", Terminal: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	wf, err := trackingSvc.CreateWorkflow(context.Background(), tracking.CreateWorkflowInput{
		WorkItemID:           item.ID,
		WorkflowDefinitionID: def.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create 3 tracked items
	itemDev, err := trackingSvc.CreateTrackedItem(context.Background(), tracking.CreateTrackedItemInput{
		WorkItemID: item.ID, WorkflowID: &wf.ID, Title: "Stripe Connector",
	})
	if err != nil {
		t.Fatal(err)
	}
	itemNotStarted, err := trackingSvc.CreateTrackedItem(context.Background(), tracking.CreateTrackedItemInput{
		WorkItemID: item.ID, WorkflowID: &wf.ID, Title: "PayPal Connector",
	})
	if err != nil {
		t.Fatal(err)
	}
	itemDone, err := trackingSvc.CreateTrackedItem(context.Background(), tracking.CreateTrackedItemInput{
		WorkItemID: item.ID, WorkflowID: &wf.ID, Title: "Core Engine",
	})
	if err != nil {
		t.Fatal(err)
	}

	// itemDev: Specification = COMPLETED, Development = IN_PROGRESS
	if _, err := trackingSvc.SetTrackedItemStepState(context.Background(), tracking.SetStepStateInput{
		TrackedItemID:  itemDev.ID,
		WorkflowStepID: def.Steps[0].ID,
		State:          tracking.StepCompleted,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := trackingSvc.SetTrackedItemStepState(context.Background(), tracking.SetStepStateInput{
		TrackedItemID:  itemDev.ID,
		WorkflowStepID: def.Steps[1].ID,
		State:          tracking.StepInProgress,
	}); err != nil {
		t.Fatal(err)
	}

	// itemDone: Specification = COMPLETED, Development = COMPLETED, QA = COMPLETED
	for _, s := range def.Steps {
		if _, err := trackingSvc.SetTrackedItemStepState(context.Background(), tracking.SetStepStateInput{
			TrackedItemID:  itemDone.ID,
			WorkflowStepID: s.ID,
			State:          tracking.StepCompleted,
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Verify reader projection
	reader := assistantWorkItemReader{service: service, tracking: trackingSvc}
	snapshot, err := reader.ReadWorkItem(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Tracking == nil {
		t.Fatal("expected non-nil tracking snapshot")
	}

	// Workflow current step remains the Workflow's own position (Specification)
	if snapshot.Tracking.CurrentStep == nil || snapshot.Tracking.CurrentStep.Title != "Specification" {
		t.Fatalf("expected workflow current step Specification, got %+v", snapshot.Tracking.CurrentStep)
	}

	byTitle := make(map[string]assistant.TrackedItemSnapshot, len(snapshot.Tracking.Items))
	for _, it := range snapshot.Tracking.Items {
		byTitle[it.Title] = it
	}

	// In progress item
	devSnap := byTitle["Stripe Connector"]
	if devSnap.State != "IN_PROGRESS" || devSnap.Complete {
		t.Fatalf("expected Stripe Connector to be IN_PROGRESS, got %+v", devSnap)
	}
	if devSnap.CurrentStepTitle == nil || *devSnap.CurrentStepTitle != "Development" {
		t.Fatalf("expected Stripe Connector current step Development, got %+v", devSnap.CurrentStepTitle)
	}

	// Not started item
	nsSnap := byTitle["PayPal Connector"]
	if nsSnap.ID != itemNotStarted.ID || nsSnap.State != "NOT_STARTED" || nsSnap.Complete {
		t.Fatalf("expected PayPal Connector to be NOT_STARTED, got %+v", nsSnap)
	}
	if nsSnap.CurrentStepTitle == nil || *nsSnap.CurrentStepTitle != "Specification" {
		t.Fatalf("expected PayPal Connector current step Specification, got %+v", nsSnap.CurrentStepTitle)
	}

	// Completed item
	doneSnap := byTitle["Core Engine"]
	if doneSnap.State != "COMPLETED" || !doneSnap.Complete || doneSnap.CurrentStepTitle != nil {
		t.Fatalf("expected Core Engine to be COMPLETED, got %+v", doneSnap)
	}

	// Now verify AskAssistant query
	executor := &scriptedAIExecutor{
		outputs: map[ai.Action]string{
			ai.ActionRuntimeCapabilityDecision: `{"version":1,"decision":"FINAL","calls":[]}`,
			ai.ActionAssistantAsk:              `{"version":1,"kind":"ANSWER","text":"Stripe Connector is currently in progress (stage: Development).","candidate":null}`,
		},
	}
	plannerSvc := planner.NewService(db, planner.NewRepository(db))
	readcapSvc, err := readcap.NewService(service, trackingSvc, plannerSvc, db, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := assistant.NewNativeRuntimeAdapter(executor, readcapSvc, NewProposeRegistry())
	if err != nil {
		t.Fatal(err)
	}
	application.aiExecutor = executor
	application.assistantRuntime = engine

	result, err := application.AskAssistant(AskAssistantRequestDTO{
		Message:    "Which tracked items are in progress?",
		Surface:    "WORK_ITEMS",
		WorkItemID: item.ID,
	})
	if err != nil {
		t.Fatalf("unexpected AskAssistant error: %v", err)
	}
	if result.Kind != "ANSWER" || !strings.Contains(result.Text, "Stripe Connector") {
		t.Fatalf("unexpected assistant answer: %+v", result)
	}

	// Verify that the prompt sent to the model actually contained the tracked item's state and current step
	if len(executor.requests) < 2 {
		t.Fatalf("expected at least 2 model requests, got %d", len(executor.requests))
	}
	prompt2Req := executor.requests[1]
	foundContext := false
	for _, m := range prompt2Req.Messages {
		if strings.Contains(m.Content, `"state": "IN_PROGRESS"`) && strings.Contains(m.Content, `"current_step_title": "Development"`) {
			foundContext = true
			break
		}
	}
	if !foundContext {
		t.Fatal("Prompt 2 request did not contain the derived state and step title in context")
	}

	// Verify no mutations or revisions were created
	events, err := history.NewRepository(db).ListByWorkItem(context.Background(), item.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Actor == history.ActorAI {
			t.Fatalf("read-only query should not create AI history event: %+v", ev)
		}
	}
}
