package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/assistant"
	"lum-harbor-notifier/internal/credential"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/openai"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/readcap"
	"lum-harbor-notifier/internal/testsupport"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

// TestAppAskAssistant_RealOpenAIProductAcceptance tests the real Lum Harbor
// application and Assistant control surface end-to-end against the configured
// real OpenAI provider (gpt-4o-mini) using an isolated throwaway database.
//
// Gated via LUM_HARBOR_OPENAI_ACCEPTANCE=1.
func TestAppAskAssistant_RealOpenAIProductAcceptance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real OpenAI acceptance test in -short mode")
	}
	if os.Getenv("LUM_HARBOR_OPENAI_ACCEPTANCE") != "1" && strings.ToLower(os.Getenv("LUM_HARBOR_OPENAI_ACCEPTANCE")) != "true" {
		t.Skip("skipping real OpenAI acceptance test; set LUM_HARBOR_OPENAI_ACCEPTANCE=1 to run")
	}

	// Read credential safely from user's configured location without logging secrets.
	userHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("resolve user home dir: %v", err)
	}
	credPath := filepath.Join(userHome, "Library", "Application Support", "lum_harbor", "credentials", "builtin-openai-credential.secret")
	credBytes, err := os.ReadFile(credPath)
	if err != nil {
		t.Fatalf("OpenAI credential secret file not found at %s: %v", credPath, err)
	}
	apiKey := strings.TrimSpace(string(credBytes))
	if apiKey == "" {
		t.Fatal("OpenAI credential is empty")
	}

	const (
		endpoint       = "https://api.openai.com/v1"
		model          = "gpt-4o-mini"
		timeoutSeconds = 60
	)

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	provider, err := openai.NewProvider(openai.Config{
		Endpoint:   endpoint,
		Credential: apiKey,
		Model:      model,
		Timeout:    timeoutSeconds * time.Second,
	})
	if err != nil {
		t.Fatalf("failed to create openai provider: %v", err)
	}

	database := testsupport.OpenTestDatabase(t)
	isolatedHome := os.Getenv("HOME")
	if strings.Contains(isolatedHome, "Library/Application Support") {
		t.Fatalf("FATAL: HOME not isolated: %s", isolatedHome)
	}

	workItems := workitem.NewService(database.DB, workitem.NewRepository(database.DB))
	trackingSvc := tracking.NewService(database.DB, workItems)
	plannerSvc := planner.NewService(database.DB, planner.NewRepository(database.DB))
	historySvc := history.NewService(history.NewRepository(database.DB))
	proposalSvc := proposal.NewService(database.DB, workItems)
	proposalSvc.SetTrackingOperator(trackingSvc)
	proposalSvc.SetPlannerOperator(plannerSvc)

	readcapSvc, err := readcap.NewService(workItems, trackingSvc, plannerSvc, database.DB, nil, nil, historySvc)
	if err != nil {
		t.Fatalf("failed to construct readcap service: %v", err)
	}
	registry := NewProposeRegistry()
	engine, err := assistant.NewNativeRuntimeAdapter(provider, readcapSvc, registry)
	if err != nil {
		t.Fatalf("failed to create iteration engine: %v", err)
	}

	store := credential.NewOpenAIStore(false)
	_ = store.Set(ctx, apiKey)

	application := &App{
		workitemService: workItems, trackingService: trackingSvc, plannerService: plannerSvc,
		proposalService: proposalSvc, assistantRuntime: engine, proposeRegistry: registry,
		aiExecutor:                provider,
		aiCredentialStoreResolver: func(bool) credential.OpenAIStore { return store },
	}

	// Seed seed work item for scenarios B & C
	seedItem, err := workItems.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{
		Title: "Payments Modernization Program",
	})
	if err != nil {
		t.Fatalf("failed to create seed work item: %v", err)
	}

	// --- Surface A: Dashboard "What can you help me with right now?" ---
	t.Run("Surface A: Dashboard help question", func(t *testing.T) {
		res, err := application.AskAssistant(AskAssistantRequestDTO{
			Message: "What can you help me with right now?",
			Surface: "DASHBOARD",
		})
		if err != nil {
			t.Fatalf("AskAssistant failed: %v", err)
		}
		if res.Kind != "ANSWER" || res.Text == "" {
			t.Fatalf("expected ANSWER on Dashboard, got kind=%s text=%q", res.Kind, res.Text)
		}
		t.Logf("Dashboard answer: %s", res.Text)
	})

	// --- Surface B: Dashboard "Remind me to buy groceries tomorrow at 3 PM" ---
	t.Run("TEST 1: Dashboard English 3 PM reminder", func(t *testing.T) {
		res, err := application.AskAssistant(AskAssistantRequestDTO{
			Message: "Remind me to buy groceries tomorrow at 3 PM",
			Surface: "DASHBOARD",
		})
		if err != nil {
			t.Fatalf("AskAssistant failed: %v", err)
		}
		if res.Kind != "MUTATION_INTENT" || res.ProposalOutcome != AssistantProposalCreated {
			t.Fatalf("expected Proposal created from Dashboard, got kind=%s outcome=%s text=%q", res.Kind, res.ProposalOutcome, res.Text)
		}
		p, err := proposalSvc.GetByID(ctx, res.ProposalID)
		if err != nil {
			t.Fatalf("get proposal: %v", err)
		}
		if p.OperationType != proposal.OperationTypePlannerItemCreate {
			t.Fatalf("expected planner.item.create, got %s", p.OperationType)
		}
		if p.PlannerItemPayload == nil || p.PlannerItemPayload.DueAt == nil {
			t.Fatalf("expected PlannerItemPayload.DueAt with system timezone, got %+v", p.PlannerItemPayload)
		}
		t.Logf("Dashboard Planner proposal created: DueAt=%s Title=%q", *p.PlannerItemPayload.DueAt, p.PlannerItemPayload.Title)
	})

	// --- Surface B2: Dashboard "提醒我明天下午三点去买菜" ---
	t.Run("TEST 2: Dashboard Chinese 下午三点 reminder", func(t *testing.T) {
		res, err := application.AskAssistant(AskAssistantRequestDTO{
			Message: "提醒我明天下午三点去买菜",
			Surface: "DASHBOARD",
		})
		if err != nil {
			t.Fatalf("AskAssistant failed: %v", err)
		}
		if res.Kind != "MUTATION_INTENT" || res.ProposalOutcome != AssistantProposalCreated {
			t.Fatalf("expected Proposal created from Chinese reminder, got kind=%s outcome=%s text=%q", res.Kind, res.ProposalOutcome, res.Text)
		}
		p, err := proposalSvc.GetByID(ctx, res.ProposalID)
		if err != nil {
			t.Fatalf("get proposal: %v", err)
		}
		if p.OperationType != proposal.OperationTypePlannerItemCreate {
			t.Fatalf("expected planner.item.create, got %s", p.OperationType)
		}
		if p.PlannerItemPayload == nil || p.PlannerItemPayload.DueAt == nil || !strings.Contains(*p.PlannerItemPayload.DueAt, "15:00:00") {
			t.Fatalf("expected PlannerItemPayload.DueAt with 15:00:00, got %+v", p.PlannerItemPayload)
		}
		t.Logf("Chinese Planner proposal created: DueAt=%s Title=%q", *p.PlannerItemPayload.DueAt, p.PlannerItemPayload.Title)
	})

	// --- Surface C: Planner "Remind me to buy groceries tomorrow at 3 PM" ---
	t.Run("TEST 5: Planner reminder produces same behavior", func(t *testing.T) {
		res, err := application.AskAssistant(AskAssistantRequestDTO{
			Message: "Remind me to buy groceries tomorrow at 3 PM",
			Surface: "PLANNER",
		})
		if err != nil {
			t.Fatalf("AskAssistant failed: %v", err)
		}
		if res.Kind != "MUTATION_INTENT" || res.ProposalOutcome != AssistantProposalCreated {
			t.Fatalf("expected Proposal created from Planner, got kind=%s outcome=%s text=%q", res.Kind, res.ProposalOutcome, res.Text)
		}
		p, err := proposalSvc.GetByID(ctx, res.ProposalID)
		if err != nil {
			t.Fatalf("get proposal: %v", err)
		}
		if p.OperationType != proposal.OperationTypePlannerItemCreate {
			t.Fatalf("expected planner.item.create, got %s", p.OperationType)
		}
		t.Logf("Planner proposal created from Planner surface: ID=%s", p.ID)
	})

	// --- Surface D: Work Items list without selection "Pause Payments Modernization Program" ---
	t.Run("Regression: Pause Payments Modernization Program", func(t *testing.T) {
		res, err := application.AskAssistant(AskAssistantRequestDTO{
			Message: "Pause Payments Modernization Program",
			Surface: "WORK_ITEMS",
		})
		if err != nil {
			t.Fatalf("AskAssistant failed: %v", err)
		}
		if res.Kind != "MUTATION_INTENT" || res.ProposalOutcome != AssistantProposalCreated {
			t.Fatalf("expected lifecycle Proposal created, got outcome=%s text=%q", res.ProposalOutcome, res.Text)
		}
		p, err := proposalSvc.GetByID(ctx, res.ProposalID)
		if err != nil {
			t.Fatalf("get proposal: %v", err)
		}
		if p.OperationType != proposal.OperationTypeWorkItemSetLifecycle {
			t.Fatalf("expected workitem.set_lifecycle, got %s", p.OperationType)
		}
		if p.TargetEntityID == nil || *p.TargetEntityID != seedItem.ID {
			t.Fatalf("expected target %s, got %v", seedItem.ID, p.TargetEntityID)
		}
		t.Logf("Lifecycle proposal created via discovery: ID=%s Target=%s", p.ID, *p.TargetEntityID)
	})

	// --- Surfaces E, F, G: Notifications, History, Settings ---
	for _, surface := range []string{"NOTIFICATIONS", "HISTORY", "SETTINGS"} {
		sName := surface
		t.Run("Surface: "+sName+" help question", func(t *testing.T) {
			res, err := application.AskAssistant(AskAssistantRequestDTO{
				Message: "What can you help me with right now?",
				Surface: surface,
			})
			if err != nil {
				t.Fatalf("AskAssistant on %s failed: %v", sName, err)
			}
			if res.Kind != "ANSWER" || res.Text == "" {
				t.Fatalf("expected ANSWER on %s, got kind=%s text=%q", sName, res.Kind, res.Text)
			}
			t.Logf("%s answer: %s", sName, res.Text)
		})
	}

	// --- Time Acceptance (Section 11) ---
	t.Run("Time Acceptance: ambiguous afternoon asks for clock time", func(t *testing.T) {
		res, err := application.AskAssistant(AskAssistantRequestDTO{
			Message: "Remind me tomorrow afternoon to review contracts",
			Surface: "PLANNER",
		})
		if err != nil {
			t.Fatalf("AskAssistant failed: %v", err)
		}
		if res.Kind != "ANSWER" || res.ProposalOutcome != "" {
			t.Fatalf("expected clarification ANSWER for ambiguous afternoon, got kind=%s outcome=%s text=%q", res.Kind, res.ProposalOutcome, res.Text)
		}
		t.Logf("Ambiguous afternoon correctly clarified: %s", res.Text)
	})

	t.Run("Time Acceptance: explicit Eastern timezone used", func(t *testing.T) {
		res, err := application.AskAssistant(AskAssistantRequestDTO{
			Message: "Remind me tomorrow at 3 PM Eastern to submit timesheet",
			Surface: "PLANNER",
		})
		if err != nil {
			t.Fatalf("AskAssistant failed: %v", err)
		}
		if res.Kind != "MUTATION_INTENT" || res.ProposalOutcome != AssistantProposalCreated {
			t.Fatalf("expected Proposal created with explicit Eastern timezone, got outcome=%s text=%q", res.ProposalOutcome, res.Text)
		}
		p, err := proposalSvc.GetByID(ctx, res.ProposalID)
		if err != nil {
			t.Fatalf("get proposal: %v", err)
		}
		if p.PlannerItemPayload == nil || p.PlannerItemPayload.DueAt == nil || !strings.Contains(*p.PlannerItemPayload.DueAt, "-04:00") && !strings.Contains(*p.PlannerItemPayload.DueAt, "-05:00") {
			t.Fatalf("expected Eastern offset in DueAt, got %+v", p.PlannerItemPayload)
		}
		t.Logf("Explicit Eastern reminder proposal created: DueAt=%s", *p.PlannerItemPayload.DueAt)
	})
}
