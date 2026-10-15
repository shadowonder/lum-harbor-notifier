package app

import (
	"context"
	"strings"
	"testing"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/assistant"
	"lum-harbor-notifier/internal/foundation"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/readcap"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

func setupDecisionPolicyApp(t *testing.T, executor *scriptedAIExecutor) (*App, *workitem.Service, *tracking.Service, *proposal.Service, *foundation.Repository) {
	t.Helper()
	application, service, db := newWorkItemApp(t)
	trackingService := tracking.NewService(db, service)
	plannerService := planner.NewService(db, planner.NewRepository(db))
	readcapService, err := readcap.NewService(service, trackingService, plannerService, db, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	proposalService := proposal.NewService(db, service, trackingService)
	application.proposalService = proposalService
	application.trackingService = trackingService

	registry := NewProposeRegistry()
	engine, err := assistant.NewNativeRuntimeAdapter(executor, readcapService, registry)
	if err != nil {
		t.Fatal(err)
	}
	application.aiExecutor = executor
	application.assistantRuntime = engine
	application.proposeRegistry = registry

	identityRepo := foundation.NewRepository(db)
	return application, service, trackingService, proposalService, identityRepo
}

// CASE 1 — NEW ENTITY WITH IDENTIFIER-LIKE NAME
// Input: "Create a new feature WorkItem called F13456752 and give it a deployment workflow with stages define, develop, test, stage, adoption, production."
// Expected:
// Prompt 1: NO_READ
// Prompt 2: MUTATION_CANDIDATE work_item.create_with_workflow
// No external resolution required.
func TestDecisionPolicy_Case1_NewEntityWithIdentifierLikeName(t *testing.T) {
	candidateJSON := `{"version":1,"capability_id":"work_item.create_with_workflow","arguments":{"title":"F13456752","workflow_name":"deployment","stages":["define","develop","test","stage","adoption","production"]},"reason":"Creating new feature with deployment workflow"}`

	executor := &scriptedAIExecutor{
		actionOutputs: map[ai.Action][]string{
			ai.ActionRuntimeCapabilityDecision: {
				`{"version":1,"decision":"FINAL","calls":[]}`,
			},
			ai.ActionAssistantAsk: {
				`{"version":1,"kind":"MUTATION_CANDIDATE","text":"Propose creating F13456752 with deployment workflow","candidate":` + candidateJSON + `}`,
			},
		},
	}

	app, _, _, proposalService, _ := setupDecisionPolicyApp(t, executor)

	msg := "Create a new feature WorkItem called F13456752 and give it a deployment workflow with stages define, develop, test, stage, adoption, production."
	result, err := app.AskAssistant(AskAssistantRequestDTO{
		Message:    msg,
		Surface:    "WORK_ITEMS",
		WorkItemID: "",
	})
	if err != nil {
		t.Fatalf("AskAssistant failed: %v", err)
	}

	if result.Kind != "MUTATION_INTENT" {
		t.Fatalf("expected MUTATION_INTENT, got %s", result.Kind)
	}
	if result.ProposalOutcome != "CREATED" || result.ProposalID == "" {
		t.Fatalf("expected proposal CREATED, got %+v", result)
	}

	// Verify Prompt 1 received semantic guidance that intent outranks identifier shape
	if len(executor.requests) < 2 {
		t.Fatalf("expected at least 2 requests, got %d", len(executor.requests))
	}
	p1Content := executor.requests[0].Messages[0].Content
	if !strings.Contains(p1Content, "Intent outranks identifier shape") {
		t.Errorf("Prompt 1 missing intent outranks identifier shape rule")
	}

	// Verify the created proposal in DB
	prop, err := proposalService.GetByID(context.Background(), result.ProposalID)
	if err != nil {
		t.Fatalf("get proposal failed: %v", err)
	}
	if prop.OperationType != proposal.OperationTypeWorkItemCreateWithWorkflow {
		t.Fatalf("unexpected operation type: %s", prop.OperationType)
	}
	if prop.TargetEntityID != nil {
		t.Fatalf("target_entity_id must be null for root create, got %s", *prop.TargetEntityID)
	}
	if prop.WorkItemCreateWithWorkflowPayload == nil || prop.WorkItemCreateWithWorkflowPayload.Title != "F13456752" {
		t.Fatalf("unexpected payload: %+v", prop.WorkItemCreateWithWorkflowPayload)
	}

	// Apply proposal and verify atomic creation
	applyRes, err := app.ApplyProposal(ApplyProposalRequestDTO{ID: result.ProposalID})
	if err != nil {
		t.Fatalf("ApplyProposal failed: %v", err)
	}
	if applyRes.Outcome != "APPLIED" || applyRes.WorkItem == nil || applyRes.WorkItem.Title != "F13456752" {
		t.Fatalf("unexpected apply outcome: %+v", applyRes)
	}
}

// CASE 2 — EXISTING INTERNAL ENTITY
// Input: "Create a deployment workflow for existing WorkItem F13456752."
// Expected:
// Assistant should perform appropriate authoritative resolution/search/read.
// It must NOT blindly create a new WorkItem.
func TestDecisionPolicy_Case2_ExistingInternalEntity(t *testing.T) {
	executor := &scriptedAIExecutor{}
	app, service, trackingService, proposalService, _ := setupDecisionPolicyApp(t, executor)

	// Seed existing WorkItem with title "F13456752"
	existingWI, err := service.CreateLocalWorkItem(context.Background(), workitem.CreateLocalWorkItemInput{
		Title: "F13456752",
	})
	if err != nil {
		t.Fatal(err)
	}

	candidateJSON := `{"version":1,"capability_id":"tracking.workflow_create","arguments":{"work_item_id":"` + existingWI.ID + `","definition_name":"deployment","steps":["define","develop","test","stage","adoption","production"]},"reason":"Attach workflow to existing WorkItem"}`

	executor.actionOutputs = map[ai.Action][]string{
		ai.ActionRuntimeCapabilityDecision: {
			// Step 1: Prompt 1 searches for the existing WorkItem
			`{"version":1,"decision":"CALL","calls":[{"capability_id":"entity.search","arguments_json":"{\"query\":\"F13456752\"}"}]}`,
			// Step 2: Prompt 1 sees resolved candidate, decides no more reads needed
			`{"version":1,"decision":"FINAL","calls":[]}`,
		},
		ai.ActionAssistantAsk: {
			// Step 3: Prompt 2 synthesizes tracking.workflow_create on the EXISTING WorkItem
			`{"version":1,"kind":"MUTATION_CANDIDATE","text":"Attaching deployment workflow to existing WorkItem F13456752","candidate":` + candidateJSON + `}`,
		},
	}

	msg := "Create a deployment workflow for existing WorkItem F13456752."
	result, err := app.AskAssistant(AskAssistantRequestDTO{
		Message:    msg,
		Surface:    "WORK_ITEMS",
		WorkItemID: "",
	})
	if err != nil {
		t.Fatalf("AskAssistant failed: %v", err)
	}

	if result.Kind != "MUTATION_INTENT" {
		t.Fatalf("expected MUTATION_INTENT, got %s", result.Kind)
	}
	if result.ProposalOutcome != "CREATED" || result.ProposalID == "" {
		t.Fatalf("expected proposal CREATED, got %+v", result)
	}

	// Verify proposal targets the existing WorkItem, NOT creating a new one
	prop, err := proposalService.GetByID(context.Background(), result.ProposalID)
	if err != nil {
		t.Fatalf("get proposal failed: %v", err)
	}
	if prop.OperationType != proposal.OperationTypeWorkflowCreate {
		t.Fatalf("expected tracking.workflow_create, got %s", prop.OperationType)
	}
	if prop.TargetEntityID == nil || *prop.TargetEntityID != existingWI.ID {
		t.Fatalf("expected target_entity_id %s, got %v", existingWI.ID, prop.TargetEntityID)
	}

	// Apply proposal and verify workflow is attached to existing WorkItem
	applyRes, err := app.ApplyProposal(ApplyProposalRequestDTO{ID: result.ProposalID})
	if err != nil {
		t.Fatalf("ApplyProposal failed: %v", err)
	}
	if applyRes.Outcome != "APPLIED" {
		t.Fatalf("expected APPLIED, got %s", applyRes.Outcome)
	}

	trackingView, err := trackingService.GetWorkItemTracking(context.Background(), existingWI.ID)
	if err != nil || trackingView.Workflow == nil {
		t.Fatalf("expected attached workflow on existing item, got: %+v err=%v", trackingView, err)
	}
	if len(trackingView.Workflow.Definition.Steps) != 6 {
		t.Fatalf("expected 6 steps, got %d", len(trackingView.Workflow.Definition.Steps))
	}
}

// CASE 3 — EXTERNAL REFERENCE
// Input: "Use external ticket F13456752 and tell me what WorkItem it is linked to."
// Expected:
// entity.resolve_external
func TestDecisionPolicy_Case3_ExternalReference(t *testing.T) {
	executor := &scriptedAIExecutor{}
	app, service, _, _, identityRepo := setupDecisionPolicyApp(t, executor)

	// Seed an existing WorkItem and bind an external reference
	existingWI, err := service.CreateLocalWorkItem(context.Background(), workitem.CreateLocalWorkItemInput{
		Title: "Bugfix Payment Gateway",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identityRepo.CreateExternalRef(context.Background(),
		"ref-1", existingWI.ID, "jira", "F13456752", foundation.RelationshipAuthoritative, nil); err != nil {
		t.Fatal(err)
	}

	executor.actionOutputs = map[ai.Action][]string{
		ai.ActionRuntimeCapabilityDecision: {
			// Step 1: Prompt 1 resolves external identifier
			`{"version":1,"decision":"CALL","calls":[{"capability_id":"entity.resolve_external","arguments_json":"{\"externalId\":\"F13456752\"}"}]}`,
			// Step 2: Prompt 1 decides no more reads needed
			`{"version":1,"decision":"FINAL","calls":[]}`,
		},
		ai.ActionAssistantAsk: {
			`{"version":1,"kind":"ANSWER","text":"External ticket F13456752 is linked to WorkItem 'Bugfix Payment Gateway'.","candidate":null}`,
		},
	}

	msg := "Use external ticket F13456752 and tell me what WorkItem it is linked to."
	result, err := app.AskAssistant(AskAssistantRequestDTO{
		Message:    msg,
		Surface:    "WORK_ITEMS",
		WorkItemID: "",
	})
	if err != nil {
		t.Fatalf("AskAssistant failed: %v", err)
	}

	if result.Kind != "ANSWER" {
		t.Fatalf("expected ANSWER, got %s", result.Kind)
	}
	if !strings.Contains(result.Text, "Bugfix Payment Gateway") {
		t.Fatalf("expected response to name the linked WorkItem, got %s", result.Text)
	}
}

// CASE 4 — EXPLICIT CREATE AFTER NEGATIVE EVIDENCE
// Simulate:
// User creation intent + authoritative NOT_FOUND evidence
// Expected:
// Prompt 2 can still produce the correct root-create/compound-create Proposal.
// NOT_FOUND does not automatically convert a valid creation request into ANSWER.
func TestDecisionPolicy_Case4_ExplicitCreateAfterNegativeEvidence(t *testing.T) {
	candidateJSON := `{"version":1,"capability_id":"work_item.create_with_workflow","arguments":{"title":"F13456752","workflow_name":"deployment","stages":["define","develop","test","stage","adoption","production"]},"reason":"Creating new feature with deployment workflow"}`

	executor := &scriptedAIExecutor{
		actionOutputs: map[ai.Action][]string{
			ai.ActionRuntimeCapabilityDecision: {
				// Simulate Prompt 1 erroneously resolving external reference first
				`{"version":1,"decision":"CALL","calls":[{"capability_id":"entity.resolve_external","arguments_json":"{\"externalId\":\"F13456752\"}"}]}`,
				// Step 2: NOT_FOUND received, exit read loop
				`{"version":1,"decision":"FINAL","calls":[]}`,
			},
			ai.ActionAssistantAsk: {
				// Prompt 2 correctly prioritizes creation intent over negative read evidence
				`{"version":1,"kind":"MUTATION_CANDIDATE","text":"Creating new WorkItem F13456752 with deployment workflow","candidate":` + candidateJSON + `}`,
			},
		},
	}

	app, _, _, proposalService, _ := setupDecisionPolicyApp(t, executor)

	msg := "Create a new feature WorkItem called F13456752 and give it a deployment workflow with stages define, develop, test, stage, adoption, production."
	result, err := app.AskAssistant(AskAssistantRequestDTO{
		Message:    msg,
		Surface:    "WORK_ITEMS",
		WorkItemID: "",
	})
	if err != nil {
		t.Fatalf("AskAssistant failed: %v", err)
	}

	if result.Kind != "MUTATION_INTENT" {
		t.Fatalf("expected MUTATION_INTENT despite negative read evidence, got %s", result.Kind)
	}
	if result.ProposalOutcome != "CREATED" || result.ProposalID == "" {
		t.Fatalf("expected proposal CREATED, got %+v", result)
	}

	prop, err := proposalService.GetByID(context.Background(), result.ProposalID)
	if err != nil {
		t.Fatalf("get proposal failed: %v", err)
	}
	if prop.OperationType != proposal.OperationTypeWorkItemCreateWithWorkflow {
		t.Fatalf("unexpected operation type: %s", prop.OperationType)
	}
	if prop.WorkItemCreateWithWorkflowPayload.Title != "F13456752" {
		t.Fatalf("unexpected title: %s", prop.WorkItemCreateWithWorkflowPayload.Title)
	}

	// Apply proposal to prove full chain works
	applyRes, err := app.ApplyProposal(ApplyProposalRequestDTO{ID: result.ProposalID})
	if err != nil {
		t.Fatalf("ApplyProposal failed: %v", err)
	}
	if applyRes.Outcome != "APPLIED" {
		t.Fatalf("expected APPLIED, got %s", applyRes.Outcome)
	}
}

// CASE 5 — EXISTING ENTITY NOT FOUND
// Input: "Move existing WorkItem F13456752 to paused."
// Evidence: NOT_FOUND
// Expected:
// ANSWER explaining that the target existing WorkItem cannot be found.
// Do NOT create a new WorkItem.
func TestDecisionPolicy_Case5_ExistingEntityNotFound(t *testing.T) {
	executor := &scriptedAIExecutor{
		actionOutputs: map[ai.Action][]string{
			ai.ActionRuntimeCapabilityDecision: {
				// Prompt 1 searches for the existing WorkItem by name
				`{"version":1,"decision":"CALL","calls":[{"capability_id":"entity.search","arguments_json":"{\"query\":\"F13456752\"}"}]}`,
				// Step 2: Empty candidates received, exit read loop
				`{"version":1,"decision":"FINAL","calls":[]}`,
			},
			ai.ActionAssistantAsk: {
				// Prompt 2 receives negative evidence for an EXISTING entity request,
				// returns kind ANSWER explaining it was not found, with candidate: null.
				`{"version":1,"kind":"ANSWER","text":"WorkItem F13456752 was not found. Cannot pause an item that does not exist.","candidate":null}`,
			},
		},
	}

	app, _, _, _, _ := setupDecisionPolicyApp(t, executor)

	msg := "Move existing WorkItem F13456752 to paused."
	result, err := app.AskAssistant(AskAssistantRequestDTO{
		Message:    msg,
		Surface:    "WORK_ITEMS",
		WorkItemID: "",
	})
	if err != nil {
		t.Fatalf("AskAssistant failed: %v", err)
	}

	if result.Kind != "ANSWER" {
		t.Fatalf("expected ANSWER, got %s", result.Kind)
	}
	if result.ProposalOutcome != "" || result.ProposalID != "" {
		t.Fatalf("expected NO proposal created, got outcome=%s id=%s", result.ProposalOutcome, result.ProposalID)
	}
	if !strings.Contains(result.Text, "not found") {
		t.Fatalf("expected explanation to state entity was not found, got %s", result.Text)
	}
}
