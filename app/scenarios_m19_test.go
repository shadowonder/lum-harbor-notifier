package app

import (
	"context"
	"encoding/json"
	"testing"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/assistant"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/readcap"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

// TestScenarioM19CreateFeatureWithWorkflowTurn tests the end-to-end Assistant flow
// for the user request:
// "help me generate new workflow. A feature, the new name is 'F13456752'. It used for deploy a feature, the stages should include define/develop/test/stage/adoption/production."
//
// Trace:
// User intent -> AskAssistant (no WorkItemID) -> RuntimeAdapter ->
// Prompt 1 (NO_READ) -> Prompt 2 (MUTATION_CANDIDATE work_item.create_with_workflow) ->
// ProposeRegistry validation -> Proposal creation (PENDING, target_entity_id = NULL) ->
// Human Apply -> Atomic WorkItem + Workflow creation -> Verified truth & revision lineage.
func TestScenarioM19CreateFeatureWithWorkflowTurn(t *testing.T) {
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

	candidateJSON := `{"version":1,"capability_id":"work_item.create_with_workflow","arguments":{"title":"F13456752","workflow_name":"Feature Deployment","stages":["define","develop","test","stage","adoption","production"]},"reason":"Creating new feature with requested deployment workflow"}`

	executor := &scriptedAIExecutor{
		outputs: map[ai.Action]string{
			ai.ActionRuntimeCapabilityDecision: `{"version":1,"decision":"FINAL","calls":[]}`,
			ai.ActionAssistantAsk:              `{"version":1,"kind":"MUTATION_CANDIDATE","text":"I will propose creating feature 'F13456752' with the requested deployment workflow.","candidate":` + candidateJSON + `}`,
		},
	}
	registry := NewProposeRegistry()
	engine, err := assistant.NewNativeRuntimeAdapter(executor, readcapService, registry)
	if err != nil {
		t.Fatal(err)
	}
	application.aiExecutor = executor
	application.assistantRuntime = engine
	application.proposeRegistry = registry

	// 1. Assistant User Turn
	userMessage := "help me generate new workflow. A feature, the new name is 'F13456752'. It used for deploy a feature, the stages should include define/develop/test/stage/adoption/production."
	result, err := application.AskAssistant(AskAssistantRequestDTO{
		Message:    userMessage,
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

	// 2. Authoritative check: zero domain truth created before Apply
	var entityCount, workItemCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM entities WHERE entity_type = 'WORK_ITEM'").Scan(&entityCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM work_items WHERE title = 'F13456752'").Scan(&workItemCount); err != nil {
		t.Fatal(err)
	}
	if entityCount != 0 || workItemCount != 0 {
		t.Fatal("domain entities must not exist before human apply")
	}

	// 3. Inspect Proposal in database
	prop, err := proposalService.GetByID(context.Background(), result.ProposalID)
	if err != nil {
		t.Fatalf("get proposal failed: %v", err)
	}
	if prop.OperationType != proposal.OperationTypeWorkItemCreateWithWorkflow {
		t.Fatalf("unexpected operation type: %s", prop.OperationType)
	}
	if prop.TargetEntityID != nil {
		t.Fatalf("target_entity_id must be null before apply, got %s", *prop.TargetEntityID)
	}
	if prop.WorkItemCreateWithWorkflowPayload == nil {
		t.Fatal("expected non-nil WorkItemCreateWithWorkflowPayload")
	}
	if prop.WorkItemCreateWithWorkflowPayload.Title != "F13456752" {
		t.Fatalf("unexpected title: %s", prop.WorkItemCreateWithWorkflowPayload.Title)
	}
	if len(prop.WorkItemCreateWithWorkflowPayload.Stages) != 6 {
		t.Fatalf("expected 6 stages, got %d", len(prop.WorkItemCreateWithWorkflowPayload.Stages))
	}

	// 4. Human Apply Proposal
	applyRes, err := application.ApplyProposal(ApplyProposalRequestDTO{ID: result.ProposalID})
	if err != nil {
		t.Fatalf("ApplyProposal failed: %v", err)
	}
	if applyRes.Outcome != "APPLIED" {
		t.Fatalf("expected outcome APPLIED, got %s", applyRes.Outcome)
	}
	if applyRes.WorkItem == nil || applyRes.WorkItem.Title != "F13456752" {
		t.Fatalf("unexpected applied work item: %+v", applyRes.WorkItem)
	}

	// 5. Verify created WorkItem and Workflow
	wi, err := service.GetByID(context.Background(), applyRes.WorkItem.ID)
	if err != nil {
		t.Fatalf("expected created work item: %v", err)
	}
	if wi.Title != "F13456752" {
		t.Fatalf("unexpected title: %s", wi.Title)
	}

	trackingView, err := trackingService.GetWorkItemTracking(context.Background(), wi.ID)
	if err != nil || trackingView.Workflow == nil {
		t.Fatalf("expected attached workflow: %+v err=%v", trackingView, err)
	}
	if len(trackingView.Workflow.Definition.Steps) != 6 {
		t.Fatalf("expected 6 workflow steps, got %d", len(trackingView.Workflow.Definition.Steps))
	}
	stageTitles := []string{"define", "develop", "test", "stage", "adoption", "production"}
	for i, step := range trackingView.Workflow.Definition.Steps {
		if step.Title != stageTitles[i] {
			t.Fatalf("step %d title mismatch: expected %q, got %q", i, stageTitles[i], step.Title)
		}
	}
}

// TestScenarioM19ReadWorkItemListAndRelations tests the read capabilities
// workitem.list and workitem.read (with Relations) in the assistant flow.
func TestScenarioM19ReadWorkItemListAndRelations(t *testing.T) {
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

	// Create a WorkItem with workflow and dependencies
	wi, err := service.CreateLocalWorkItem(context.Background(), workitem.CreateLocalWorkItemInput{
		Title: "Project Apollo",
	})
	if err != nil {
		t.Fatal(err)
	}
	def, err := trackingService.CreateWorkflowDefinition(context.Background(), tracking.CreateWorkflowDefinitionInput{
		Name:    "Pipeline",
		Version: 1,
		Steps: []tracking.WorkflowStepInput{
			{Key: "s1", Title: "Design"},
			{Key: "s2", Title: "Deploy"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = trackingService.CreateWorkflow(context.Background(), tracking.CreateWorkflowInput{
		WorkItemID:           wi.ID,
		WorkflowDefinitionID: def.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	t1, err := trackingService.CreateTrackedItem(context.Background(), tracking.CreateTrackedItemInput{
		WorkItemID: wi.ID,
		Title:      "Architecture",
	})
	if err != nil {
		t.Fatal(err)
	}
	t2, err := trackingService.CreateTrackedItem(context.Background(), tracking.CreateTrackedItemInput{
		WorkItemID: wi.ID,
		Title:      "Deployment Run",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := trackingService.AddDependency(context.Background(), tracking.TrackedItemRelationInput{
		FromTrackedItemID: t2.ID,
		ToTrackedItemID:   t1.ID,
	}); err != nil {
		t.Fatal(err)
	}

	// 1. Test workitem.list execution via readcap
	listRes, err := readcapService.Execute(context.Background(), readcap.Request{
		CapabilityID: readcap.CapabilityWorkItemList,
		Input:        json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("workitem.list failed: %v", err)
	}
	listOutput, ok := listRes.Output.(readcap.WorkItemListOutput)
	if !ok || len(listOutput.Items) != 1 || listOutput.Items[0].Title != "Project Apollo" {
		t.Fatalf("unexpected list output: %+v", listRes.Output)
	}

	// 2. Test workitem.read exposes Relations
	readInput, _ := json.Marshal(readcap.WorkItemReadInput{WorkItemID: wi.ID})
	readRes, err := readcapService.Execute(context.Background(), readcap.Request{
		CapabilityID: readcap.CapabilityWorkItemRead,
		Input:        readInput,
	})
	if err != nil {
		t.Fatalf("workitem.read failed: %v", err)
	}
	readOutput, ok := readRes.Output.(readcap.WorkItemReadOutput)
	if !ok || readOutput.Tracking == nil {
		t.Fatalf("unexpected read output: %+v", readRes.Output)
	}
	if len(readOutput.Tracking.Relations) != 1 {
		t.Fatalf("expected 1 relation, got %d", len(readOutput.Tracking.Relations))
	}
	if readOutput.Tracking.Relations[0].FromTrackedItemID != t2.ID || readOutput.Tracking.Relations[0].ToTrackedItemID != t1.ID {
		t.Fatalf("unexpected relation: %+v", readOutput.Tracking.Relations[0])
	}
}
