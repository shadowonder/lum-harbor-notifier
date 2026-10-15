package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/assistant"
	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/openai"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/plugin/plugintest"
	"lum-harbor-notifier/internal/plugin/readadapter"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/readcap"
	"lum-harbor-notifier/internal/testsupport"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

// TestAppAskAssistant_GenericCapability_OllamaAcceptance proves the Round 31
// generic Proposal capability cutover is real against a live local Ollama
// model, not just scripted fakes: Scenarios A (Planner root-create, no
// WorkItem context), B (WorkItem lifecycle, resolved via a prior read), and
// C (Workflow create) all flow end to end through the same generic
// descriptor -> model selection -> registry dispatch -> Proposal mechanism
// as the preserved M16 WorkItem/Tracking flows.
//
// Gated via LUM_HARBOR_OLLAMA_ACCEPTANCE=1. Uses testsupport.OpenTestDatabase
// (throwaway HOME/TempDir), never the real user DB.
func TestAppAskAssistant_GenericCapability_OllamaAcceptance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real Ollama acceptance test in -short mode")
	}
	if os.Getenv("LUM_HARBOR_OLLAMA_ACCEPTANCE") != "1" && strings.ToLower(os.Getenv("LUM_HARBOR_OLLAMA_ACCEPTANCE")) != "true" {
		t.Skip("skipping real Ollama acceptance test; set LUM_HARBOR_OLLAMA_ACCEPTANCE=1 to run")
	}

	const (
		ollamaBaseURL    = "http://localhost:11434"
		ollamaV1Endpoint = "http://localhost:11434/v1"
		targetModel      = "qwen3:8b"
		dummyCredential  = "ollama"
		timeoutSeconds   = 600
	)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("%s/api/tags", ollamaBaseURL))
	if err != nil {
		t.Fatalf("Ollama acceptance requested but Ollama is unavailable: %v", err)
	}
	resp.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeoutSeconds*time.Second)
	defer cancel()

	provider, err := openai.NewProvider(openai.Config{
		Endpoint:   ollamaV1Endpoint,
		Credential: dummyCredential,
		Model:      targetModel,
		Timeout:    200 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create openai provider: %v", err)
	}

	database := testsupport.OpenTestDatabase(t)
	realUserDB := os.Getenv("HOME") // testsupport already redirected HOME to a t.TempDir()
	if strings.Contains(realUserDB, "Library/Application Support") {
		t.Fatalf("FATAL: HOME does not look redirected to a throwaway directory: %s", realUserDB)
	}
	t.Logf("isolated throwaway HOME: %s", realUserDB)

	workItems := workitem.NewService(database.DB, workitem.NewRepository(database.DB))
	trackingSvc := tracking.NewService(database.DB, workItems)
	plannerSvc := planner.NewService(database.DB, planner.NewRepository(database.DB))
	proposalSvc := proposal.NewService(database.DB, workItems)
	proposalSvc.SetTrackingOperator(trackingSvc)
	proposalSvc.SetPlannerOperator(plannerSvc)

	readcapSvc, err := readcap.NewService(workItems, trackingSvc, plannerSvc, database.DB, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to construct readcap service: %v", err)
	}
	registry := NewProposeRegistry()
	engine, err := assistant.NewNativeRuntimeAdapter(provider, readcapSvc, registry)
	if err != nil {
		t.Fatalf("failed to create iteration engine: %v", err)
	}
	application := &App{
		workitemService: workItems, trackingService: trackingSvc, plannerService: plannerSvc,
		proposalService: proposalSvc, assistantRuntime: engine, proposeRegistry: registry,
	}

	// Seed the WorkItem scenarios B and C target by title.
	item, err := workItems.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: "Payments Modernization Program"})
	if err != nil {
		t.Fatalf("failed to create seed work item: %v", err)
	}

	// --- Existing M16 regression: WorkItem update proposal still works ----
	t.Run("M16 regression: WorkItem update proposal", func(t *testing.T) {
		res, err := application.AskAssistant(AskAssistantRequestDTO{
			Message: "Rename this work item to 'Payments Modernization Program V2'", Surface: "WORK_ITEMS", WorkItemID: item.ID,
		})
		if err != nil {
			t.Fatalf("AskAssistant failed: %v", err)
		}
		t.Logf("result: Kind=%s ProposalOutcome=%s Text=%q", res.Kind, res.ProposalOutcome, res.Text)
		fresh, err := workItems.GetByID(context.Background(), item.ID)
		if err != nil || fresh.Title != "Payments Modernization Program" {
			t.Fatalf("VIOLATION: title mutated before Apply: %+v err=%v", fresh, err)
		}
		if res.ProposalOutcome == AssistantProposalCreated {
			p, err := proposalSvc.GetByID(context.Background(), res.ProposalID)
			if err != nil || p.OperationType != proposal.OperationTypeWorkItemUpdateDetails || p.Status != proposal.StatusPending {
				t.Fatalf("unexpected proposal: %+v err=%v", p, err)
			}
		}
	})

	// --- Scenario A: Planner root-create, no WorkItem context -------------
	// The message is date-only ("tomorrow", no time-of-day) - unambiguous
	// per the temporal rule, so due_date applies and no clarification is
	// needed. A separate ambiguous phrasing ("tomorrow afternoon", no exact
	// time or timezone) is verified below to return CLARIFY instead of
	// guessing - both are required product behavior, not just the create path.
	t.Run("Scenario A: Planner root-create", func(t *testing.T) {
		res, err := application.AskAssistant(AskAssistantRequestDTO{
			Message: "Remind me to submit my homework tomorrow.", Surface: "DASHBOARD",
		})
		if err != nil {
			t.Fatalf("AskAssistant failed: %v", err)
		}
		t.Logf("result: Kind=%s ProposalOutcome=%s ProposalID=%s Text=%q", res.Kind, res.ProposalOutcome, res.ProposalID, res.Text)
		if res.ProposalOutcome != AssistantProposalCreated {
			t.Fatalf("expected a created Proposal for an unambiguous date-only Planner reminder, got outcome=%s text=%q", res.ProposalOutcome, res.Text)
		}
		p, err := proposalSvc.GetByID(context.Background(), res.ProposalID)
		if err != nil {
			t.Fatal(err)
		}
		if p.OperationType != proposal.OperationTypePlannerItemCreate {
			t.Fatalf("expected planner.item.create, got %s", p.OperationType)
		}
		if p.TargetEntityID != nil {
			t.Fatalf("expected NULL target for a true root-create with no WorkItem context, got %v", *p.TargetEntityID)
		}
		if countRows(t, database.DB, "SELECT COUNT(*) FROM planner_items") != 0 {
			t.Fatal("Proposal creation must not create Planner domain truth before Apply")
		}
		applied, err := proposalSvc.ApplyProposal(context.Background(), proposal.ApplyProposalInput{ID: p.ID})
		if err != nil || applied.Outcome != proposal.ApplyOutcomeApplied {
			t.Fatalf("apply: %+v err=%v", applied, err)
		}
		if countRows(t, database.DB, "SELECT COUNT(*) FROM planner_items") != 1 {
			t.Fatal("Apply must atomically create exactly one PlannerItem")
		}
	})

	// --- Scenario A variant: ambiguous time-of-day must not guess ---------
	t.Run("Scenario A: ambiguous time-of-day asks for clarification", func(t *testing.T) {
		res, err := application.AskAssistant(AskAssistantRequestDTO{
			Message: "Remind me to submit my homework tomorrow afternoon.", Surface: "DASHBOARD",
		})
		if err != nil {
			t.Fatalf("AskAssistant failed: %v", err)
		}
		t.Logf("result: Kind=%s ProposalOutcome=%s Text=%q", res.Kind, res.ProposalOutcome, res.Text)
		if res.ProposalOutcome == AssistantProposalCreated {
			t.Fatalf("expected no Proposal for an ambiguous time-of-day with no exact time/timezone (must ask, never guess): %+v", res)
		}
	})

	// --- Scenario B: WorkItem lifecycle, resolved via a prior read --------
	t.Run("Scenario B: WorkItem lifecycle", func(t *testing.T) {
		res, err := application.AskAssistant(AskAssistantRequestDTO{
			Message: "Pause Payments Modernization Program.", Surface: "DASHBOARD",
		})
		if err != nil {
			t.Fatalf("AskAssistant failed: %v", err)
		}
		t.Logf("result: Kind=%s ProposalOutcome=%s ProposalID=%s Text=%q", res.Kind, res.ProposalOutcome, res.ProposalID, res.Text)
		if res.ProposalOutcome != AssistantProposalCreated {
			t.Fatalf("expected a created lifecycle Proposal, got outcome=%s text=%q", res.ProposalOutcome, res.Text)
		}
		p, err := proposalSvc.GetByID(context.Background(), res.ProposalID)
		if err != nil {
			t.Fatal(err)
		}
		if p.OperationType != proposal.OperationTypeWorkItemSetLifecycle {
			t.Fatalf("expected workitem.set_lifecycle, got %s", p.OperationType)
		}
		if p.TargetEntityID == nil || *p.TargetEntityID != item.ID {
			t.Fatalf("expected target = resolved WorkItem ID %s, got %v", item.ID, p.TargetEntityID)
		}
		fresh, err := workItems.GetByID(context.Background(), item.ID)
		if err != nil || fresh.Lifecycle != workitem.LifecycleActive {
			t.Fatalf("VIOLATION: lifecycle mutated before Apply: %+v err=%v", fresh, err)
		}
	})

	// --- Scenario C: Workflow create ---------------------------------------
	t.Run("Scenario C: Workflow create", func(t *testing.T) {
		res, err := application.AskAssistant(AskAssistantRequestDTO{
			Message: "Create a workflow for this project covering development, review and production.",
			Surface: "WORK_ITEMS", WorkItemID: item.ID,
		})
		if err != nil {
			t.Fatalf("AskAssistant failed: %v", err)
		}
		t.Logf("result: Kind=%s ProposalOutcome=%s ProposalID=%s Text=%q", res.Kind, res.ProposalOutcome, res.ProposalID, res.Text)
		if res.ProposalOutcome != AssistantProposalCreated {
			t.Fatalf("expected a created workflow Proposal, got outcome=%s text=%q", res.ProposalOutcome, res.Text)
		}
		p, err := proposalSvc.GetByID(context.Background(), res.ProposalID)
		if err != nil {
			t.Fatal(err)
		}
		if p.OperationType != proposal.OperationTypeWorkflowCreate {
			t.Fatalf("expected tracking.workflow_create, got %s", p.OperationType)
		}
		if view, _ := trackingSvc.GetWorkItemTracking(context.Background(), item.ID); view.Workflow != nil {
			t.Fatal("Proposal creation must not attach a Workflow before Apply")
		}
		applied, err := proposalSvc.ApplyProposal(context.Background(), proposal.ApplyProposalInput{ID: p.ID})
		if err != nil || applied.Outcome != proposal.ApplyOutcomeApplied {
			t.Fatalf("apply: %+v err=%v", applied, err)
		}
		view, err := trackingSvc.GetWorkItemTracking(context.Background(), item.ID)
		if err != nil || view.Workflow == nil || len(view.Workflow.Definition.Steps) < 2 {
			t.Fatalf("expected an attached multi-step workflow, got %+v err=%v", view.Workflow, err)
		}
		t.Logf("attached workflow with %d steps", len(view.Workflow.Definition.Steps))
	})
}

func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// TestAppAskAssistant_GenericCapability_TrackingSmoke_OllamaAcceptance runs a single
// targeted live Tracking mutation smoke test against real local Ollama (qwen3:8b)
// to verify that M16 Tracking mutation capabilities (tracked_item.create) flow end-to-end
// through the generic capability registry, producing a pending proposal without premature DB mutation.
func TestAppAskAssistant_GenericCapability_TrackingSmoke_OllamaAcceptance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real Ollama acceptance test in -short mode")
	}
	if os.Getenv("LUM_HARBOR_OLLAMA_ACCEPTANCE") != "1" && strings.ToLower(os.Getenv("LUM_HARBOR_OLLAMA_ACCEPTANCE")) != "true" {
		t.Skip("skipping real Ollama acceptance test; set LUM_HARBOR_OLLAMA_ACCEPTANCE=1 to run")
	}

	const (
		ollamaBaseURL    = "http://localhost:11434"
		ollamaV1Endpoint = "http://localhost:11434/v1"
		targetModel      = "qwen3:8b"
		dummyCredential  = "ollama"
		timeoutSeconds   = 200
	)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("%s/api/tags", ollamaBaseURL))
	if err != nil {
		t.Fatalf("Ollama acceptance requested but Ollama is unavailable: %v", err)
	}
	resp.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeoutSeconds*time.Second)
	defer cancel()

	provider, err := openai.NewProvider(openai.Config{
		Endpoint:   ollamaV1Endpoint,
		Credential: dummyCredential,
		Model:      targetModel,
		Timeout:    200 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create openai provider: %v", err)
	}

	database := testsupport.OpenTestDatabase(t)
	realUserDB := os.Getenv("HOME")
	if strings.Contains(realUserDB, "Library/Application Support") {
		t.Fatalf("FATAL: HOME does not look redirected to a throwaway directory: %s", realUserDB)
	}
	t.Logf("isolated throwaway HOME: %s", realUserDB)

	workItems := workitem.NewService(database.DB, workitem.NewRepository(database.DB))
	trackingSvc := tracking.NewService(database.DB, workItems)
	plannerSvc := planner.NewService(database.DB, planner.NewRepository(database.DB))
	proposalSvc := proposal.NewService(database.DB, workItems)
	proposalSvc.SetTrackingOperator(trackingSvc)
	proposalSvc.SetPlannerOperator(plannerSvc)

	readcapSvc, err := readcap.NewService(workItems, trackingSvc, plannerSvc, database.DB, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to construct readcap service: %v", err)
	}
	registry := NewProposeRegistry()
	engine, err := assistant.NewNativeRuntimeAdapter(provider, readcapSvc, registry)
	if err != nil {
		t.Fatalf("failed to create iteration engine: %v", err)
	}
	application := &App{
		workitemService: workItems, trackingService: trackingSvc, plannerService: plannerSvc,
		proposalService: proposalSvc, assistantRuntime: engine, proposeRegistry: registry,
	}

	// Seed WorkItem
	item, err := workItems.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: "Payments Modernization Program"})
	if err != nil {
		t.Fatalf("failed to create seed work item: %v", err)
	}

	// Seed attached Workflow to satisfy trackedItemCreateCapability requirements
	def, err := trackingSvc.CreateWorkflowDefinition(ctx, tracking.CreateWorkflowDefinitionInput{
		Name:    "Delivery",
		Version: 1,
		Steps: []tracking.WorkflowStepInput{
			{Key: "dev", Title: "Development"},
			{Key: "verify", Title: "Verification", Terminal: true},
		},
	})
	if err != nil {
		t.Fatalf("failed to create workflow def: %v", err)
	}
	_, err = trackingSvc.CreateWorkflow(ctx, tracking.CreateWorkflowInput{
		WorkItemID:           item.ID,
		WorkflowDefinitionID: def.ID,
	})
	if err != nil {
		t.Fatalf("failed to attach workflow: %v", err)
	}

	res, err := application.AskAssistant(AskAssistantRequestDTO{
		Message:    "Add a task 'Implement Authentication' to this project",
		Surface:    "WORK_ITEMS",
		WorkItemID: item.ID,
	})
	if err != nil {
		t.Fatalf("AskAssistant failed: %v", err)
	}
	t.Logf("result: Kind=%s ProposalOutcome=%s ProposalID=%s Text=%q", res.Kind, res.ProposalOutcome, res.ProposalID, res.Text)

	// Verify invariant: Proposal created with tracked_item.create
	if res.ProposalOutcome != AssistantProposalCreated {
		t.Fatalf("expected created proposal, got outcome=%s text=%q", res.ProposalOutcome, res.Text)
	}
	p, err := proposalSvc.GetByID(context.Background(), res.ProposalID)
	if err != nil {
		t.Fatalf("failed to load proposal: %v", err)
	}
	if p.OperationType != proposal.OperationTypeTrackedItemCreate {
		t.Fatalf("expected operation type %s, got %s", proposal.OperationTypeTrackedItemCreate, p.OperationType)
	}
	if p.Status != proposal.StatusPending {
		t.Fatalf("expected status PENDING, got %s", p.Status)
	}

	// Verify invariant: No tracked item created in database before apply
	view, err := trackingSvc.GetWorkItemTracking(context.Background(), item.ID)
	if err != nil {
		t.Fatalf("failed to get tracking view: %v", err)
	}
	if len(view.Items) != 0 {
		t.Fatalf("VIOLATION: tracked item created before apply: %d items exist", len(view.Items))
	}

	// Apply proposal and verify tracked item is created
	applied, err := proposalSvc.ApplyProposal(context.Background(), proposal.ApplyProposalInput{ID: p.ID})
	if err != nil || applied.Outcome != proposal.ApplyOutcomeApplied {
		t.Fatalf("apply proposal failed: outcome=%v err=%v", applied, err)
	}
	viewAfter, err := trackingSvc.GetWorkItemTracking(context.Background(), item.ID)
	if err != nil {
		t.Fatalf("failed to get tracking view after apply: %v", err)
	}
	if len(viewAfter.Items) != 1 || viewAfter.Items[0].Item.Title != "Implement Authentication" {
		t.Fatalf("expected 1 tracked item 'Implement Authentication' after apply, got: %+v", viewAfter.Items)
	}
	t.Logf("successfully verified tracked_item.create live smoke acceptance end-to-end")
}

// TestAppAskAssistant_GenericCapability_ScenarioD_ExplainWarning_OllamaAcceptance proves
// Scenario D: an explain-warning query grounded in real Attention/Tracking state executes
// purely through generic READ capabilities (attention.read_for_workitem, workitem.read)
// and produces a grounded answer without proposal or mutation.
func TestAppAskAssistant_GenericCapability_ScenarioD_ExplainWarning_OllamaAcceptance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real Ollama acceptance test in -short mode")
	}
	if os.Getenv("LUM_HARBOR_OLLAMA_ACCEPTANCE") != "1" && strings.ToLower(os.Getenv("LUM_HARBOR_OLLAMA_ACCEPTANCE")) != "true" {
		t.Skip("skipping real Ollama acceptance test; set LUM_HARBOR_OLLAMA_ACCEPTANCE=1 to run")
	}

	const (
		ollamaBaseURL    = "http://localhost:11434"
		ollamaV1Endpoint = "http://localhost:11434/v1"
		targetModel      = "qwen3:8b"
		dummyCredential  = "ollama"
		timeoutSeconds   = 200
	)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("%s/api/tags", ollamaBaseURL))
	if err != nil {
		t.Fatalf("Ollama acceptance requested but Ollama is unavailable: %v", err)
	}
	resp.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeoutSeconds*time.Second)
	defer cancel()

	provider, err := openai.NewProvider(openai.Config{
		Endpoint:   ollamaV1Endpoint,
		Credential: dummyCredential,
		Model:      targetModel,
		Timeout:    200 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create openai provider: %v", err)
	}

	database := testsupport.OpenTestDatabase(t)
	realUserDB := os.Getenv("HOME")
	if strings.Contains(realUserDB, "Library/Application Support") {
		t.Fatalf("FATAL: HOME does not look redirected to a throwaway directory: %s", realUserDB)
	}
	t.Logf("isolated throwaway HOME: %s", realUserDB)

	workItems := workitem.NewService(database.DB, workitem.NewRepository(database.DB))
	trackingSvc := tracking.NewService(database.DB, workItems)
	plannerSvc := planner.NewService(database.DB, planner.NewRepository(database.DB))
	proposalSvc := proposal.NewService(database.DB, workItems)
	proposalSvc.SetTrackingOperator(trackingSvc)
	proposalSvc.SetPlannerOperator(plannerSvc)
	attentionSvc := attention.NewService(database.DB, trackingSvc)
	historySvc := history.NewService(history.NewRepository(database.DB))

	readcapSvc, err := readcap.NewService(workItems, trackingSvc, plannerSvc, database.DB, nil, attentionSvc, historySvc)
	if err != nil {
		t.Fatalf("failed to construct readcap service: %v", err)
	}
	registry := NewProposeRegistry()
	engine, err := assistant.NewNativeRuntimeAdapter(provider, readcapSvc, registry)
	if err != nil {
		t.Fatalf("failed to create iteration engine: %v", err)
	}
	application := &App{
		workitemService: workItems, trackingService: trackingSvc, plannerService: plannerSvc,
		proposalService: proposalSvc, assistantRuntime: engine, proposeRegistry: registry,
	}

	item, err := workItems.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{
		Title: "Billing Service Integration",
	})
	if err != nil {
		t.Fatalf("failed to seed work item: %v", err)
	}

	// Seed overdue MonitorPoint to trigger an Attention warning
	due := time.Now().UTC().Add(-2 * time.Hour)
	expected := workitem.LifecycleCompleted
	_, err = trackingSvc.CreateMonitorPoint(ctx, tracking.CreateMonitorPointInput{
		WorkItemID:                item.ID,
		Kind:                      tracking.MonitorFinalTarget,
		Title:                     "Production Gate",
		DueAt:                     due,
		Timezone:                  "UTC",
		ExpectedWorkItemLifecycle: &expected,
	})
	if err != nil {
		t.Fatalf("failed to seed monitor point: %v", err)
	}
	_, err = attentionSvc.Reconcile(ctx, time.Now().UTC())
	if err != nil {
		t.Fatalf("failed to reconcile attention: %v", err)
	}

	res, err := application.AskAssistant(AskAssistantRequestDTO{
		Message:    "Why does this WorkItem have a warning?",
		Surface:    "WORK_ITEMS",
		WorkItemID: item.ID,
	})
	if err != nil {
		t.Fatalf("AskAssistant failed: %v", err)
	}
	t.Logf("result: Kind=%s ProposalOutcome=%s Text=%q", res.Kind, res.ProposalOutcome, res.Text)

	if res.Kind != "ANSWER" {
		t.Fatalf("expected ANSWER kind, got %s", res.Kind)
	}
	if res.ProposalOutcome != "" {
		t.Fatalf("expected NO proposal for explain-warning query, got %s", res.ProposalOutcome)
	}
	if strings.TrimSpace(res.Text) == "" {
		t.Fatal("expected non-empty explanation text")
	}
	t.Logf("successfully verified Scenario D explain warning acceptance")
}

// TestAppAskAssistant_GenericCapability_ScenarioE_ExternalIDHistory_OllamaAcceptance proves
// Scenario E: an external identifier query ("Show me what happened to F12345678 recently.")
// from any surface resolves external identity via entity.resolve_external, loads history
// via history.read_for_workitem, and produces a grounded explanation with no WorkItem prerequisite.
func TestAppAskAssistant_GenericCapability_ScenarioE_ExternalIDHistory_OllamaAcceptance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real Ollama acceptance test in -short mode")
	}
	if os.Getenv("LUM_HARBOR_OLLAMA_ACCEPTANCE") != "1" && strings.ToLower(os.Getenv("LUM_HARBOR_OLLAMA_ACCEPTANCE")) != "true" {
		t.Skip("skipping real Ollama acceptance test; set LUM_HARBOR_OLLAMA_ACCEPTANCE=1 to run")
	}

	const (
		ollamaBaseURL    = "http://localhost:11434"
		ollamaV1Endpoint = "http://localhost:11434/v1"
		targetModel      = "qwen3:8b"
		dummyCredential  = "ollama"
		timeoutSeconds   = 200
	)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("%s/api/tags", ollamaBaseURL))
	if err != nil {
		t.Fatalf("Ollama acceptance requested but Ollama is unavailable: %v", err)
	}
	resp.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeoutSeconds*time.Second)
	defer cancel()

	provider, err := openai.NewProvider(openai.Config{
		Endpoint:   ollamaV1Endpoint,
		Credential: dummyCredential,
		Model:      targetModel,
		Timeout:    200 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create openai provider: %v", err)
	}

	database := testsupport.OpenTestDatabase(t)
	realUserDB := os.Getenv("HOME")
	if strings.Contains(realUserDB, "Library/Application Support") {
		t.Fatalf("FATAL: HOME does not look redirected to a throwaway directory: %s", realUserDB)
	}
	t.Logf("isolated throwaway HOME: %s", realUserDB)

	workItems := workitem.NewService(database.DB, workitem.NewRepository(database.DB))
	trackingSvc := tracking.NewService(database.DB, workItems)
	plannerSvc := planner.NewService(database.DB, planner.NewRepository(database.DB))
	proposalSvc := proposal.NewService(database.DB, workItems)
	proposalSvc.SetTrackingOperator(trackingSvc)
	proposalSvc.SetPlannerOperator(plannerSvc)
	attentionSvc := attention.NewService(database.DB, trackingSvc)
	historySvc := history.NewService(history.NewRepository(database.DB))

	readcapSvc, err := readcap.NewService(workItems, trackingSvc, plannerSvc, database.DB, nil, attentionSvc, historySvc)
	if err != nil {
		t.Fatalf("failed to construct readcap service: %v", err)
	}
	type recordedCall struct {
		CapabilityID string
		Input        string
		Output       any
	}
	var recordedCalls []recordedCall

	recorder := &testReadRecorder{
		source: readcapSvc,
		record: func(capID string, input json.RawMessage, output any) {
			recordedCalls = append(recordedCalls, recordedCall{
				CapabilityID: capID,
				Input:        string(input),
				Output:       output,
			})
		},
	}

	registry := NewProposeRegistry()
	engine, err := assistant.NewNativeRuntimeAdapter(provider, recorder, registry)
	if err != nil {
		t.Fatalf("failed to create iteration engine: %v", err)
	}
	application := &App{
		workitemService: workItems, trackingService: trackingSvc, plannerService: plannerSvc,
		proposalService: proposalSvc, assistantRuntime: engine, proposeRegistry: registry,
	}

	item, err := workItems.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{
		Title: "Platform Security Audit",
	})
	if err != nil {
		t.Fatalf("failed to seed work item: %v", err)
	}

	// Seed authoritative external ref for F12345678
	_, err = database.DB.ExecContext(ctx, "INSERT INTO external_refs (id, entity_id, system, external_id, relationship_type) VALUES ('ref-f123', ?, 'jira', 'F12345678', 'AUTHORITATIVE')", item.ID)
	if err != nil {
		t.Fatalf("failed to seed external ref: %v", err)
	}

	// Seed history event
	repo := history.NewRepository(database.DB)
	_, err = repo.Append(ctx, history.Draft{
		Type:            history.EventWorkItemDetailsChanged,
		Scope:           history.ScopeWorkItem,
		Actor:           history.ActorUser,
		Summary:         "Updated security audit scope to include authentication endpoints",
		SubjectEntityID: &item.ID,
		WorkItemID:      &item.ID,
		CorrelationID:   "corr-f123-1",
		OccurredAt:      time.Now().UTC().Add(-30 * time.Minute),
	})
	if err != nil {
		t.Fatalf("failed to append history event: %v", err)
	}

	const userMessage = "Show me what happened to F12345678 recently."
	res, err := application.AskAssistant(AskAssistantRequestDTO{
		Message:    userMessage,
		Surface:    "DASHBOARD",
		WorkItemID: "",
	})
	if err != nil {
		t.Fatalf("AskAssistant failed: %v", err)
	}

	t.Logf("user request: %q", userMessage)
	t.Logf("selected WorkItem: none (Surface=DASHBOARD, WorkItemID=\"\")")
	t.Logf("total read capability executions: %d", len(recordedCalls))
	for i, call := range recordedCalls {
		t.Logf("  read %d: capability=%s input=%s output=%+v", i+1, call.CapabilityID, call.Input, call.Output)
	}
	t.Logf("result: Kind=%s ProposalOutcome=%s Text=%q", res.Kind, res.ProposalOutcome, res.Text)

	if len(recordedCalls) < 2 {
		t.Fatalf("expected at least 2 read executions (entity.resolve_external -> history.read_for_workitem), got %d", len(recordedCalls))
	}
	if recordedCalls[0].CapabilityID != "entity.resolve_external" {
		t.Fatalf("expected first capability entity.resolve_external, got %s", recordedCalls[0].CapabilityID)
	}
	firstOut, ok := recordedCalls[0].Output.(readcap.ResolveExternalOutput)
	if !ok {
		t.Fatalf("expected ResolveExternalOutput, got %T", recordedCalls[0].Output)
	}
	t.Logf("resolved EntityID: %s, EntityType: %s", firstOut.EntityID, firstOut.EntityType)
	if firstOut.EntityID != item.ID {
		t.Fatalf("expected resolved entity ID %s, got %s", item.ID, firstOut.EntityID)
	}
	if recordedCalls[1].CapabilityID != "history.read_for_workitem" {
		t.Fatalf("expected second capability history.read_for_workitem, got %s", recordedCalls[1].CapabilityID)
	}
	if res.Kind != "ANSWER" {
		t.Fatalf("expected ANSWER kind, got %s", res.Kind)
	}
	if res.ProposalOutcome != "" {
		t.Fatalf("expected NO proposal for external history query, got %s", res.ProposalOutcome)
	}
	if strings.TrimSpace(res.Text) == "" {
		t.Fatal("expected non-empty explanation text")
	}
	t.Logf("Scenario E PASS: verified full capability chain entity.resolve_external -> history.read_for_workitem -> grounded answer")
}

type testReadRecorder struct {
	source assistant.ReadCapabilitySource
	record func(capID string, input json.RawMessage, output any)
}

func (r *testReadRecorder) ListDescriptors() []readcap.Descriptor {
	return r.source.ListDescriptors()
}

func (r *testReadRecorder) Execute(ctx context.Context, req readcap.Request) (readcap.Result, error) {
	res, err := r.source.Execute(ctx, req)
	if err == nil && r.record != nil {
		r.record(req.CapabilityID, req.Input, res.Output)
	}
	return res, err
}

// TestAppAskAssistant_GenericCapability_LivePluginRead_OllamaAcceptance proves
// the live Plugin read path end-to-end: real Plugin subprocess + production Host/readadapter
// + readcap + real local Ollama provider. AI selects the Plugin read capability through
// the dynamic descriptor, subprocess returns query result over JSON RPC, and AI produces
// a grounded response without plugin-specific assistant code.
func TestAppAskAssistant_GenericCapability_LivePluginRead_OllamaAcceptance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real Ollama acceptance test in -short mode")
	}
	if os.Getenv("LUM_HARBOR_OLLAMA_ACCEPTANCE") != "1" && strings.ToLower(os.Getenv("LUM_HARBOR_OLLAMA_ACCEPTANCE")) != "true" {
		t.Skip("skipping real Ollama acceptance test; set LUM_HARBOR_OLLAMA_ACCEPTANCE=1 to run")
	}

	const (
		ollamaBaseURL    = "http://localhost:11434"
		ollamaV1Endpoint = "http://localhost:11434/v1"
		targetModel      = "qwen3:8b"
		dummyCredential  = "ollama"
		timeoutSeconds   = 200
	)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("%s/api/tags", ollamaBaseURL))
	if err != nil {
		t.Fatalf("Ollama acceptance requested but Ollama is unavailable: %v", err)
	}
	resp.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeoutSeconds*time.Second)
	defer cancel()

	provider, err := openai.NewProvider(openai.Config{
		Endpoint:   ollamaV1Endpoint,
		Credential: dummyCredential,
		Model:      targetModel,
		Timeout:    200 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create openai provider: %v", err)
	}

	database := testsupport.OpenTestDatabase(t)
	realUserDB := os.Getenv("HOME")
	if strings.Contains(realUserDB, "Library/Application Support") {
		t.Fatalf("FATAL: HOME does not look redirected to a throwaway directory: %s", realUserDB)
	}
	t.Logf("isolated throwaway HOME: %s", realUserDB)

	// Set up real Plugin subprocess
	root := t.TempDir()
	extensionID := "acceptance-read-ext"
	instanceID := "acceptance-read-inst"

	plugintest.WriteExtension(t, root, plugintest.ExtensionSpec{
		ID:   extensionID,
		Name: "Acceptance Read Plugin",
		Mode: "normal",
		CapabilityDeclarations: []plugin.CapabilityDeclaration{{
			ID:              plugin.CapabilityReadQuery,
			Description:     "Query external system status and telemetry facts.",
			InputSemantics:  `{"query": string}`,
			ResultSemantics: `{"summary": string, "facts": [{"name": string, "value": string}]}`,
		}},
		Schema: plugin.PluginConfigSchema{Version: 1, Fields: []plugin.PluginConfigField{
			{Key: "label", Label: "Label", Type: plugin.ConfigString},
		}},
	})

	registry, failures, err := plugin.Discover(root)
	if err != nil || len(failures) != 0 {
		t.Fatalf("Discover: err=%v failures=%v", err, failures)
	}

	host, err := plugin.NewHost(registry, plugin.HostOptions{
		StartupTimeout:  5 * time.Second,
		RequestTimeout:  2 * time.Second,
		ShutdownTimeout: 500 * time.Millisecond,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("NewHost: %v", err)
	}
	if err := host.Start(ctx, extensionID); err != nil {
		t.Fatalf("host.Start: %v", err)
	}
	t.Cleanup(func() { _ = host.Shutdown(context.Background()) })

	instances := []plugin.Instance{{
		ID:           instanceID,
		ExtensionID:  extensionID,
		CapabilityID: plugin.CapabilityReadQuery,
		Config:       json.RawMessage(`{"label":"Acceptance"}`),
	}}
	source, err := readadapter.New(host, registry, instances)
	if err != nil {
		t.Fatalf("readadapter.New: %v", err)
	}

	workItems := workitem.NewService(database.DB, workitem.NewRepository(database.DB))
	trackingSvc := tracking.NewService(database.DB, workItems)
	plannerSvc := planner.NewService(database.DB, planner.NewRepository(database.DB))
	proposalSvc := proposal.NewService(database.DB, workItems)
	proposalSvc.SetTrackingOperator(trackingSvc)
	proposalSvc.SetPlannerOperator(plannerSvc)
	attentionSvc := attention.NewService(database.DB, trackingSvc)
	historySvc := history.NewService(history.NewRepository(database.DB))

	readcapSvc, err := readcap.NewService(workItems, trackingSvc, plannerSvc, database.DB, source, attentionSvc, historySvc)
	if err != nil {
		t.Fatalf("failed to construct readcap service: %v", err)
	}

	// Verify plugin capability is advertised in unified descriptors
	descriptors := readcapSvc.ListDescriptors()
	var pluginCapID string
	for _, d := range descriptors {
		if d.Origin == readcap.OriginPlugin {
			pluginCapID = d.ID
			break
		}
	}
	if pluginCapID == "" {
		t.Fatalf("expected plugin descriptor in readcap service, got: %+v", descriptors)
	}
	t.Logf("found plugin read descriptor: %s", pluginCapID)

	proposeRegistry := NewProposeRegistry()
	engine, err := assistant.NewNativeRuntimeAdapter(provider, readcapSvc, proposeRegistry)
	if err != nil {
		t.Fatalf("failed to create iteration engine: %v", err)
	}
	application := &App{
		workitemService: workItems, trackingService: trackingSvc, plannerService: plannerSvc,
		proposalService: proposalSvc, assistantRuntime: engine, proposeRegistry: proposeRegistry,
	}

	res, err := application.AskAssistant(AskAssistantRequestDTO{
		Message: "Query external system status and telemetry facts.",
		Surface: "DASHBOARD",
	})
	if err != nil {
		t.Fatalf("AskAssistant failed: %v", err)
	}
	t.Logf("result: Kind=%s ProposalOutcome=%s Text=%q", res.Kind, res.ProposalOutcome, res.Text)

	if res.Kind != "ANSWER" {
		t.Fatalf("expected ANSWER kind, got %s", res.Kind)
	}
	if res.ProposalOutcome != "" {
		t.Fatalf("expected NO proposal for plugin read, got %s", res.ProposalOutcome)
	}
	if strings.TrimSpace(res.Text) == "" {
		t.Fatal("expected non-empty answer text from plugin read")
	}
	t.Logf("successfully verified live Plugin read acceptance with real subprocess and local Ollama")
}
