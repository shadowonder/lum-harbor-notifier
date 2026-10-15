package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/assistant"
	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/openai"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/readcap"
	"lum-harbor-notifier/internal/testsupport"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

type engineTestFakeAIExecutor struct {
	handlers map[ai.Action]func(ai.Request) (ai.Response, error)
}

func (f *engineTestFakeAIExecutor) Execute(_ context.Context, req ai.Request) (ai.Response, error) {
	if f.handlers != nil {
		if handler, ok := f.handlers[req.Action]; ok {
			return handler(req)
		}
	}
	return ai.Response{}, errors.New("engineTestFakeAIExecutor: unhandled action")
}

// TestRuntimeAdapter_RealReadcapIntegration tests RuntimeAdapter wired to a real
// readcap.Service backed by an in-memory SQLite database.
func TestRuntimeAdapter_RealReadcapIntegration(t *testing.T) {
	ctx := context.Background()
	database := testsupport.OpenTestDatabase(t)
	workItems := workitem.NewService(database.DB, workitem.NewRepository(database.DB))
	trackingSvc := tracking.NewService(database.DB, workItems)
	plannerSvc := planner.NewService(database.DB, planner.NewRepository(database.DB))

	readcapSvc, err := readcap.NewService(workItems, trackingSvc, plannerSvc, database.DB, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to construct readcap service: %v", err)
	}

	// Create real entity in SQLite
	createdItem, err := workItems.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{
		Title: "Fix login button styling",
	})
	if err != nil {
		t.Fatalf("failed to create test work item: %v", err)
	}

	var prompt2ReceivedEntityMatch bool

	executor := &engineTestFakeAIExecutor{
		handlers: map[ai.Action]func(ai.Request) (ai.Response, error){
			ai.ActionRuntimeCapabilityDecision: func(req ai.Request) (ai.Response, error) {
				return ai.Response{
					Output: json.RawMessage(`{
						"version": 1,
						"decision": "CALL",
						"calls": [{"capability_id":"entity.search","arguments_json":"{\"query\":\"login\"}"}]
					}`),
				}, nil
			},
			ai.ActionAssistantAsk: func(req ai.Request) (ai.Response, error) {
				for _, msg := range req.Messages {
					if strings.Contains(msg.Content, createdItem.ID) &&
						strings.Contains(msg.Content, "Fix login button styling") {
						prompt2ReceivedEntityMatch = true
					}
				}
				return ai.Response{
					Output: json.RawMessage(`{
						"version": 1,
						"kind": "ANSWER",
						"text": "Found work item 'Fix login button styling' matching query.",
						"candidate": null
					}`),
				}, nil
			},
		},
	}

	engine, err := assistant.NewNativeRuntimeAdapter(executor, readcapSvc, NewProposeRegistry())
	if err != nil {
		t.Fatalf("NewNativeRuntimeAdapter failed: %v", err)
	}

	result, err := engine.Iterate(ctx, "req-real-readcap", "Find items related to login", nil)
	if err != nil {
		t.Fatalf("Iterate returned unexpected error: %v", err)
	}

	if !prompt2ReceivedEntityMatch {
		t.Fatalf("Prompt 2 was not fed the real SQLite search output containing created entity ID and title")
	}
	if result.Kind != assistant.KindAnswer {
		t.Fatalf("expected ANSWER kind, got %s", result.Kind)
	}
	if result.ReadRequest == nil || result.ReadRequest.CapabilityID != "entity.search" {
		t.Fatalf("expected ReadRequest recorded for entity.search")
	}
	if result.ReadResult == nil || result.ReadResult.CapabilityID != "entity.search" {
		t.Fatalf("expected ReadResult recorded for entity.search")
	}

	searchOutput, ok := result.ReadResult.Output.(readcap.SearchOutput)
	if !ok {
		t.Fatalf("expected ReadResult.Output to be readcap.SearchOutput, got %T", result.ReadResult.Output)
	}
	if len(searchOutput.Candidates) == 0 {
		t.Fatalf("expected at least 1 candidate returned from real SQLite search, got 0")
	}
	if searchOutput.Candidates[0].EntityID != createdItem.ID {
		t.Fatalf("expected candidate entity ID %s, got %s", createdItem.ID, searchOutput.Candidates[0].EntityID)
	}
}

// TestRuntimeAdapter_OllamaAcceptance tests the full RuntimeAdapter with real local Ollama (qwen3:8b)
// and real readcap.Service.
//
// Gated via LUM_HARBOR_OLLAMA_ACCEPTANCE=1.
// If unset, skipped.
func TestRuntimeAdapter_OllamaAcceptance(t *testing.T) {
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
		timeoutSeconds   = 120
	)

	// Preflight check: verify Ollama is reachable
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("%s/api/tags", ollamaBaseURL))
	if err != nil {
		t.Fatalf("Ollama acceptance requested but Ollama is unavailable at %s: %v", ollamaBaseURL, err)
	}
	resp.Body.Close()

	ctx, cancel := context.WithTimeout(context.Background(), timeoutSeconds*time.Second)
	defer cancel()

	// Real provider talking to local Ollama
	provider, err := openai.NewProvider(openai.Config{
		Endpoint:   ollamaV1Endpoint,
		Credential: dummyCredential,
		Model:      targetModel,
		Timeout:    timeoutSeconds * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create openai provider: %v", err)
	}

	// Real SQLite database and readcap.Service
	database := testsupport.OpenTestDatabase(t)
	workItems := workitem.NewService(database.DB, workitem.NewRepository(database.DB))
	trackingSvc := tracking.NewService(database.DB, workItems)
	plannerSvc := planner.NewService(database.DB, planner.NewRepository(database.DB))

	readcapSvc, err := readcap.NewService(workItems, trackingSvc, plannerSvc, database.DB, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to construct readcap service: %v", err)
	}

	engine, err := assistant.NewNativeRuntimeAdapter(provider, readcapSvc, NewProposeRegistry())
	if err != nil {
		t.Fatalf("failed to create iteration engine: %v", err)
	}

	// Test Case 1: Simple informational query with base snapshot (NO_READ flow)
	now := time.Now().UTC()
	desc := "This work item tracks backend performance optimizations."
	snap := &assistant.WorkItemSnapshot{
		ID:          "wi-ollama-test-1",
		Title:       "Backend Performance Optimization",
		Description: &desc,
		Lifecycle:   "OPEN",
		CreatedAt:   now,
		UpdatedAt:   now,
	}

	t.Logf("Running RuntimeAdapter Case 1 (NO_READ) with real Ollama (%s)...", targetModel)
	started := time.Now()
	result, err := engine.Iterate(ctx, "req-ollama-1", "What is the purpose of this work item?", snap)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("Iterate returned error against Ollama: %v", err)
	}

	t.Logf("Ollama Iteration Case 1 completed in %s: Kind=%s, Text=%q, ReadExecuted=%v",
		elapsed, result.Kind, result.Text, result.ReadRequest != nil)

	if result.Kind != assistant.KindAnswer {
		t.Fatalf("expected Kind ANSWER, got %s", result.Kind)
	}
	if strings.TrimSpace(result.Text) == "" {
		t.Fatalf("expected non-empty text from Ollama")
	}

	// Test Case 2: Query requiring entity.search read (ONE_READ flow)
	_, err = workItems.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{
		Title: "Payment Gateway Integration",
	})
	if err != nil {
		t.Fatalf("failed to seed work item for search: %v", err)
	}

	t.Logf("Running RuntimeAdapter Case 2 (READ request) with real Ollama (%s)...", targetModel)
	started2 := time.Now()
	result2, err := engine.Iterate(ctx, "req-ollama-2", "Search the system for items matching query 'Payment'.", nil)
	elapsed2 := time.Since(started2)
	if err != nil {
		t.Fatalf("Iterate Case 2 returned error against Ollama: %v", err)
	}

	t.Logf("Ollama Iteration Case 2 completed in %s: Kind=%s, Text=%q, ReadExecuted=%v",
		elapsed2, result2.Kind, result2.Text, result2.ReadRequest != nil)

	if strings.TrimSpace(result2.Text) == "" {
		t.Fatalf("expected non-empty text from Ollama in Case 2")
	}
	if result2.ReadRequest == nil {
		t.Logf("Note: Ollama answered without requesting read; ReadExecuted=false")
	} else {
		t.Logf("Ollama requested read capability: %s with input: %s",
			result2.ReadRequest.CapabilityID, string(result2.ReadRequest.Input))
		if result2.ReadResult == nil {
			t.Fatalf("expected ReadResult when ReadRequest is present")
		}
	}
}

// TestAppAskAssistant_OllamaAcceptance tests the top-level App.AskAssistant product flow
// with real Ollama (qwen3:8b) verifying Flow A (NO_READ), Flow B (ONE_READ), and Flow C (MUTATION).
func TestAppAskAssistant_OllamaAcceptance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real Ollama product acceptance test in -short mode")
	}
	if os.Getenv("LUM_HARBOR_OLLAMA_ACCEPTANCE") != "1" && strings.ToLower(os.Getenv("LUM_HARBOR_OLLAMA_ACCEPTANCE")) != "true" {
		t.Skip("skipping real Ollama product acceptance test; set LUM_HARBOR_OLLAMA_ACCEPTANCE=1 to run")
	}

	const (
		ollamaBaseURL    = "http://localhost:11434"
		ollamaV1Endpoint = "http://localhost:11434/v1"
		targetModel      = "qwen3:8b"
		dummyCredential  = "ollama"
		timeoutSeconds   = 360
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
		Timeout:    120 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create openai provider: %v", err)
	}

	database := testsupport.OpenTestDatabase(t)
	workItems := workitem.NewService(database.DB, workitem.NewRepository(database.DB))
	trackingSvc := tracking.NewService(database.DB, workItems)
	plannerSvc := planner.NewService(database.DB, planner.NewRepository(database.DB))
	proposalSvc := proposal.NewService(database.DB, workItems)

	readcapSvc, err := readcap.NewService(workItems, trackingSvc, plannerSvc, database.DB, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to construct readcap service: %v", err)
	}

	engine, err := assistant.NewNativeRuntimeAdapter(provider, readcapSvc, NewProposeRegistry())
	if err != nil {
		t.Fatalf("failed to create iteration engine: %v", err)
	}

	application := &App{
		workitemService:  workItems,
		proposalService:  proposalSvc,
		trackingService:  trackingSvc,
		assistantRuntime: engine,
		proposeRegistry:  NewProposeRegistry(),
	}

	desc := "Acceptance work item for Ollama end-to-end testing"
	item, err := workItems.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{
		Title:       "Payment Microservice Architecture",
		Description: &desc,
	})
	if err != nil {
		t.Fatalf("failed to create seed work item: %v", err)
	}

	// Product Flow A: NO_READ flow (question directly answerable from current WorkItem context)
	t.Logf("Running Product Flow A (NO_READ) through App.AskAssistant...")
	startFlowA := time.Now()
	resA, err := application.AskAssistant(AskAssistantRequestDTO{
		Message:    "What is the focus of this work item?",
		Surface:    "WORK_ITEMS",
		WorkItemID: item.ID,
	})
	durationA := time.Since(startFlowA)
	if err != nil {
		t.Fatalf("Flow A failed: %v", err)
	}
	t.Logf("Product Flow A finished in %s: Kind=%s, Text=%q, ProposalOutcome=%s",
		durationA, resA.Kind, resA.Text, resA.ProposalOutcome)
	if resA.Kind != "ANSWER" {
		t.Fatalf("expected Flow A Kind ANSWER, got %s", resA.Kind)
	}
	if resA.ProposalID != "" {
		t.Fatalf("Flow A must not create a proposal")
	}

	// Product Flow B: ONE_READ flow (user asks to look up other information)
	t.Logf("Running Product Flow B (ONE_READ) through App.AskAssistant...")
	startFlowB := time.Now()
	resB, err := application.AskAssistant(AskAssistantRequestDTO{
		Message:    "Search the database for items about Payment.",
		Surface:    "WORK_ITEMS",
		WorkItemID: item.ID,
	})
	durationB := time.Since(startFlowB)
	if err != nil {
		t.Fatalf("Flow B failed: %v", err)
	}
	t.Logf("Product Flow B finished in %s: Kind=%s, Text=%q, ProposalOutcome=%s",
		durationB, resB.Kind, resB.Text, resB.ProposalOutcome)
	if strings.TrimSpace(resB.Text) == "" {
		t.Fatalf("expected non-empty text in Flow B")
	}
	if resB.ProposalID != "" {
		t.Fatalf("Flow B must not create a proposal")
	}

	// Product Flow C: Mutation attempt (rename)
	t.Logf("Running Product Flow C (MUTATION) through App.AskAssistant...")
	startFlowC := time.Now()
	resC, err := application.AskAssistant(AskAssistantRequestDTO{
		Message:    "Rename this work item to 'Payment Microservice Architecture V2'",
		Surface:    "WORK_ITEMS",
		WorkItemID: item.ID,
	})
	durationC := time.Since(startFlowC)
	if err != nil {
		t.Fatalf("Flow C failed: %v", err)
	}
	t.Logf("Product Flow C finished in %s: Kind=%s, ProposalOutcome=%s, ProposalID=%s, Text=%q",
		durationC, resC.Kind, resC.ProposalOutcome, resC.ProposalID, resC.Text)

	// In Flow C, whether the model produces MUTATION_INTENT or ANSWER, domain truth MUST remain unchanged!
	unmutated, err := workItems.GetByID(context.Background(), item.ID)
	if err != nil {
		t.Fatalf("failed to retrieve work item: %v", err)
	}
	if unmutated.Title != "Payment Microservice Architecture" {
		t.Fatalf("VIOLATION: authoritative truth was mutated directly by AI! Title=%s", unmutated.Title)
	}
	if resC.ProposalOutcome == AssistantProposalCreated {
		if resC.ProposalID == "" {
			t.Fatalf("expected non-empty ProposalID on CREATED outcome")
		}
		p, err := proposalSvc.GetByID(context.Background(), resC.ProposalID)
		if err != nil {
			t.Fatalf("failed to fetch created proposal: %v", err)
		}
		if p.Status != proposal.StatusPending {
			t.Fatalf("expected proposal status PENDING, got %s", p.Status)
		}
		t.Logf("Flow C successfully created pending proposal: %s with proposed title %q",
			p.ID, p.Payload.ProposedTitle)
	}
}

// TestAppAskAssistant_M16_5_MultiRead_OllamaAcceptance tests M16.5 multi-read chaining with real Ollama (qwen3:8b)
// without a selected WorkItem (e.g. from Dashboard surface).
func TestAppAskAssistant_M16_5_MultiRead_OllamaAcceptance(t *testing.T) {
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
		timeoutSeconds   = 360
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
		Timeout:    120 * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create openai provider: %v", err)
	}

	database := testsupport.OpenTestDatabase(t)
	workItems := workitem.NewService(database.DB, workitem.NewRepository(database.DB))
	trackingSvc := tracking.NewService(database.DB, workItems)
	plannerSvc := planner.NewService(database.DB, planner.NewRepository(database.DB))
	proposalSvc := proposal.NewService(database.DB, workItems)
	attentionSvc := attention.NewService(database.DB, trackingSvc)
	historySvc := history.NewService(history.NewRepository(database.DB))

	readcapSvc, err := readcap.NewService(workItems, trackingSvc, plannerSvc, database.DB, nil, attentionSvc, historySvc)
	if err != nil {
		t.Fatalf("failed to construct readcap service: %v", err)
	}

	engine, err := assistant.NewNativeRuntimeAdapter(provider, readcapSvc, NewProposeRegistry())
	if err != nil {
		t.Fatalf("failed to create iteration engine: %v", err)
	}

	application := &App{
		workitemService:  workItems,
		proposalService:  proposalSvc,
		trackingService:  trackingSvc,
		assistantRuntime: engine,
		proposeRegistry:  NewProposeRegistry(),
	}

	desc := "Core invoice and billing subsystem tracking"
	created, err := workItems.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{
		Title:       "Invoice Processing Pipeline",
		Description: &desc,
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Running M16.5 Multi-Read Acceptance with Ollama (%s)...", targetModel)
	start := time.Now()
	res, err := application.AskAssistant(AskAssistantRequestDTO{
		Message: "Find the work item about Invoice Processing and describe what it is about.",
		Surface: "DASHBOARD",
	})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("AskAssistant failed: %v", err)
	}
	t.Logf("M16.5 Ollama Multi-Read completed in %s: Kind=%s, Text=%q, ProposalOutcome=%s",
		elapsed, res.Kind, res.Text, res.ProposalOutcome)

	if res.Kind != "ANSWER" {
		t.Fatalf("expected ANSWER kind, got %s", res.Kind)
	}
	if strings.TrimSpace(res.Text) == "" {
		t.Fatalf("expected non-empty answer text")
	}
	_ = created
}
