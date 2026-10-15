package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/source"
	"lum-harbor-notifier/internal/sourceprocessing"
)

// Real GUI acceptance (M19 Round 2 Phase B) found that source.interpret was
// never given a candidate contract or the current date, so every Source
// proposal fell back to "title = interpretation summary" with no due date.
func TestM19_Round2_InterpretSourceSuppliesCandidateContractAndDateContext(t *testing.T) {
	f := newRound2Fixture(t)
	f.mockAI.interpretDoc = `{"version":"source.interpret.v1","summary":"The note asks for a vendor checklist review tomorrow afternoon.","interpretationDocument":{"candidate":{"version":1,"capability_id":"planner.item.create","arguments":{"title":"Review the vendor security compliance checklist","due_date":"2026-09-27","due_time":"15:00"},"reason":"The note asks to create this todo."}}}`

	src, err := f.app.CaptureLocalSource(LocalSourceCaptureRequestDTO{
		Title:   "Q4 Security Review",
		Content: "Create a todo to review the vendor security compliance checklist tomorrow at 3 PM.",
	})
	if err != nil {
		t.Fatalf("CaptureLocalSource: %v", err)
	}
	res, err := f.app.InterpretSource(InterpretSourceRequestDTO{SourceRevisionID: src.SourceRevisionID})
	if err != nil {
		t.Fatalf("InterpretSource: %v", err)
	}

	request := f.mockAI.lastRequests[ai.ActionSourceInterpret]
	userMessage := request.Messages[len(request.Messages)-1].Content
	for _, want := range []string{"candidateContract", "planner.item.create", "Current local date/time", "due_date"} {
		if !strings.Contains(userMessage, want) {
			t.Errorf("interpret request context missing %q: %s", want, userMessage)
		}
	}
	if strings.Contains(userMessage, "workitem.update_details") {
		t.Errorf("capabilities requiring a selected Work Item must not be offered to Source interpretation")
	}
	if !strings.Contains(string(request.OutputSchema), `"candidate"`) {
		t.Errorf("interpret output schema must declare the candidate: %s", request.OutputSchema)
	}

	if !res.HasProposal || res.Proposal == nil {
		t.Fatalf("expected a proposal, got %+v", res)
	}
	p := res.Proposal
	if p.OperationType != proposal.OperationTypePlannerItemCreate {
		t.Fatalf("expected planner.item.create, got %q", p.OperationType)
	}
	encoded, _ := json.Marshal(p.Changes)
	for _, want := range []string{"Review the vendor security compliance checklist", "2026-09-27"} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("proposal changes must carry %q from the candidate: %s", want, encoded)
		}
	}
	if strings.Contains(string(encoded), "tomorrow afternoon") {
		t.Errorf("the interpretation summary must not become the Todo title: %s", encoded)
	}
}

func TestSourceInterpretationContextOffersOnlyWorkItemFreeCapabilities(t *testing.T) {
	now := time.Date(2026, 9, 26, 2, 40, 0, 0, time.FixedZone("CDT", -5*3600))
	raw, err := sourceInterpretationContext(now, NewProposeRegistry().ListDescriptors())
	if err != nil {
		t.Fatal(err)
	}
	var ctx struct {
		Runtime  string `json:"authoritativeRuntimeContext"`
		Contract struct {
			Capabilities []struct {
				ID string `json:"id"`
			} `json:"capabilities"`
			Rules []string `json:"rules"`
		} `json:"candidateContract"`
	}
	if err := json.Unmarshal(raw, &ctx); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ctx.Runtime, "2026-09-26T02:40:00-05:00") {
		t.Errorf("runtime context must carry the current local date/time: %q", ctx.Runtime)
	}
	registry := NewProposeRegistry()
	found := false
	for _, c := range ctx.Contract.Capabilities {
		if c.ID == proposal.OperationTypePlannerItemCreate {
			found = true
		}
		for _, d := range registry.ListDescriptors() {
			if d.ID == c.ID && d.RequiresWorkItemContext {
				t.Errorf("capability %s requires a Work Item and must not be offered", c.ID)
			}
		}
	}
	if !found || len(ctx.Contract.Rules) == 0 {
		t.Errorf("contract must offer planner.item.create with rules: %+v", ctx.Contract)
	}
	rules := strings.Join(ctx.Contract.Rules, " ")
	for _, want := range []string{"HH:MM", "Never put an ISO/RFC3339 value", "timezone suffix such as Z"} {
		if !strings.Contains(rules, want) {
			t.Errorf("source time guidance missing %q: %s", want, rules)
		}
	}
}

// TestDesignatedSyntheticStoryProposalCandidatePropagatesDueDateAndDueTime proves the complete mapping:
// Synthetic payload (dueDate + dueTime)
//
//	↓
//
// source.clean / source.interpret contract
//
//	↓
//
// Proposal candidate (due_date + due_time)
//
//	↓
//
// Proposal payload (combined DueAt)
//
//	↓
//
// Planner item creation (DueAt populated for near-term reminder firing)
func TestDesignatedSyntheticStoryProposalCandidatePropagatesDueDateAndDueTime(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()

	// 1. Synthetic story payload with dueDate and dueTime
	storyPayload := map[string]any{
		"id":          "workflow-001/story-002",
		"workflowId":  "workflow-001",
		"title":       "Implement structured logging",
		"description": "Replace stdout logs with JSON logger conforming to runtime contract. Target completion due by 2026-09-27 13:12.",
		"status":      "NEW",
		"storyPoints": 3,
		"priority":    "MEDIUM",
		"version":     1,
		"updatedAt":   "2026-09-27T13:04:00Z",
		"dueDate":     "2026-09-27",
		"dueTime":     "13:12",
	}
	payloadBytes, _ := json.Marshal(storyPayload)

	// Configure mock AI to output structured candidate with both due_date and due_time
	f.mockAI.cleanOutput = `{"version":"source.clean.v1","normalizedDocument":{"title":"Implement structured logging","dueDate":"2026-09-27","dueTime":"13:12","description":"Replace stdout logs with JSON logger conforming to runtime contract. Target completion due by 2026-09-27 13:12."}}`
	f.mockAI.interpretDoc = `{"version":"source.interpret.v1","summary":"External story workflow-001/story-002 requests implementing structured logging","interpretationDocument":{"candidate":{"version":1,"capability_id":"planner.item.create","arguments":{"title":"Implement structured logging","due_date":"2026-09-27","due_time":"13:12"},"reason":"Create planner todo for structured logging with specified due deadline"}}}`

	// 2. Ingest source revision as synthetic project story
	rev, err := f.sourceService.CaptureIntentSource(ctx, source.CaptureIntentInput{
		ExtensionID:    "lh-plugin-synthetic-project",
		InstanceID:     "inst-1",
		CapabilityID:   "source.fetch",
		SourceKind:     "synthetic.project.story",
		ExternalID:     "workflow-001/story-002",
		Payload:        payloadBytes,
		ProcessingMode: source.ProcessingModeSemanticProposal,
	})
	if err != nil {
		t.Fatalf("capture source revision: %v", err)
	}

	// 3. Interpret source -> triggers AI clean & interpret -> creates Proposal
	res, err := f.app.InterpretSource(InterpretSourceRequestDTO{SourceRevisionID: rev.ID})
	if err != nil {
		t.Fatalf("InterpretSource: %v", err)
	}
	if !res.HasProposal || res.Proposal == nil {
		t.Fatalf("expected proposal from interpretation, got %+v", res)
	}

	// 4. Assert Proposal candidate contains BOTH due_date and due_time in Proposal changes / payload
	p, err := f.proposalService.GetByID(ctx, res.Proposal.ID)
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	if p.PlannerItemPayload == nil {
		t.Fatal("expected non-nil PlannerItemPayload")
	}

	// Because both due_date and due_time were in candidate arguments, proposeRegistry combined them into DueAt:
	if p.PlannerItemPayload.DueAt == nil || *p.PlannerItemPayload.DueAt == "" {
		t.Fatalf("expected combined DueAt in proposal payload, got nil")
	}
	if !strings.HasPrefix(*p.PlannerItemPayload.DueAt, "2026-09-27T13:12") {
		t.Fatalf("expected DueAt to start with 2026-09-27T13:12, got %q", *p.PlannerItemPayload.DueAt)
	}

	// 5. Apply Proposal -> verify created Planner item has DueAt set
	applyRes, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: p.ID})
	if err != nil {
		t.Fatalf("ApplyProposal: %v", err)
	}
	if applyRes.Outcome != "APPLIED" {
		t.Fatalf("expected outcome APPLIED, got %s", applyRes.Outcome)
	}

	// 6. An interpretation without a typed candidate is explicitly no action.
	fallbackOutput := sourceprocessing.InterpretOutputV1{
		Version: "source.interpret.v1",
		Summary: "Fallback logging task",
		InterpretationDocument: json.RawMessage(`{
			"dueDate": "2026-09-27",
			"dueTime": "13:12"
		}`),
	}
	if _, ok := extractMutationCandidate(fallbackOutput); ok {
		t.Fatal("summary-only interpretation must not manufacture a mutation candidate")
	}
}
