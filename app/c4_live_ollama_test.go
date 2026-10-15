package app

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/assistant"
	"lum-harbor-notifier/internal/openai"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/readcap"
	"lum-harbor-notifier/internal/reminder"
	"lum-harbor-notifier/internal/testsupport"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

// TestC4LiveOllamaAggregatePlanSafety is an opt-in live check of the real
// Prompt 2 plan contract against local Ollama (qwen3:8b). Model output is not
// deterministic, so it asserts only safety invariants and logs what the model
// produced; deterministic acceptance lives in c4_ai_aggregate_test.go.
func TestC4LiveOllamaAggregatePlanSafety(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real Ollama acceptance test in -short mode")
	}
	if os.Getenv("LUM_HARBOR_OLLAMA_ACCEPTANCE") != "1" && strings.ToLower(os.Getenv("LUM_HARBOR_OLLAMA_ACCEPTANCE")) != "true" {
		t.Skip("skipping real Ollama acceptance test; set LUM_HARBOR_OLLAMA_ACCEPTANCE=1 to run")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://localhost:11434/api/tags")
	if err != nil {
		t.Fatalf("Ollama acceptance requested but Ollama is unavailable: %v", err)
	}
	_ = resp.Body.Close()

	provider, err := openai.NewProvider(openai.Config{Endpoint: "http://localhost:11434/v1", Credential: "ollama", Model: "qwen3:8b", Timeout: 300 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	database := testsupport.OpenTestDatabase(t)
	db := database.DB
	workItems := workitem.NewService(db, workitem.NewRepository(db))
	trackingSvc := tracking.NewService(db, workItems)
	plannerSvc := planner.NewService(db, planner.NewRepository(db))
	proposalSvc := proposal.NewService(db, workItems)
	proposalSvc.SetTrackingOperator(trackingSvc)
	proposalSvc.SetPlannerOperator(plannerSvc)
	proposalSvc.SetReminderOperator(reminder.NewService(db))
	readcapSvc, err := readcap.NewService(workItems, trackingSvc, plannerSvc, db, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewProposeRegistry()
	engine, err := assistant.NewNativeRuntimeAdapter(provider, readcapSvc, registry)
	if err != nil {
		t.Fatal(err)
	}
	application := &App{workitemService: workItems, trackingService: trackingSvc, plannerService: plannerSvc, proposalService: proposalSvc, assistantRuntime: engine, proposeRegistry: registry}
	count := func(query string) int {
		var n int
		if err := db.QueryRow(query).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	res, err := application.AskAssistant(AskAssistantRequestDTO{Message: flagshipMessage, Surface: "DASHBOARD"})
	t.Logf("live result: kind=%s outcome=%s proposal=%s text=%q err=%v", res.Kind, res.ProposalOutcome, res.ProposalID, res.Text, err)
	if n := count(`SELECT COUNT(*) FROM work_items`) + count(`SELECT COUNT(*) FROM planner_items`); n != 0 {
		t.Fatalf("VIOLATION: domain truth changed before Apply (%d rows)", n)
	}
	if n := count(`SELECT COUNT(*) FROM change_proposals`); n > 1 {
		t.Fatalf("VIOLATION: one request created %d Proposals", n)
	}
	if res.ProposalOutcome != AssistantProposalCreated {
		t.Logf("model did not produce a Proposal for the flagship request (safe outcome)")
		return
	}
	created, err := proposalSvc.GetByID(context.Background(), res.ProposalID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("live Proposal: type=%s title=%q operations=%d", created.OperationType, created.Title, len(created.Operations))
	for _, op := range created.Operations {
		t.Logf("  #%d %s %s", op.SequenceNo, op.OperationType, op.PayloadJSON)
	}
	dto := application.proposalReviewDTO(*created)
	raw, _ := json.Marshal(dto.Operations)
	if strings.Contains(string(raw), "$op[") {
		t.Fatalf("VIOLATION: review exposes reference syntax: %s", raw)
	}
	applied, applyErr := application.ApplyProposal(ApplyProposalRequestDTO{ID: created.ID})
	t.Logf("live Apply: outcome=%s err=%v", applied.Outcome, applyErr)
	if applyErr != nil {
		if n := count(`SELECT COUNT(*) FROM work_items`) + count(`SELECT COUNT(*) FROM planner_items`) + count(`SELECT COUNT(*) FROM tracked_items`); n != 0 {
			t.Fatalf("VIOLATION: failed Apply left %d domain rows", n)
		}
		return
	}
	t.Logf("live domain truth: work_items=%d workflows=%d tracked_items=%d planner_items=%d reminders=%d",
		count(`SELECT COUNT(*) FROM work_items`), count(`SELECT COUNT(*) FROM workflows`), count(`SELECT COUNT(*) FROM tracked_items`),
		count(`SELECT COUNT(*) FROM planner_items`), count(`SELECT COUNT(*) FROM todo_reminders`))
}
