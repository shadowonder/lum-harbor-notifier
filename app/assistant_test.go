package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/assistant"
	"lum-harbor-notifier/internal/database"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/readcap"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

func newAssistantApp(t *testing.T, executor AIExecutor) (*App, *workitem.WorkItem, func() (string, string, string)) {
	t.Helper()
	application, service, database := newWorkItemApp(t)
	description := "Original description"
	item, err := service.CreateLocalWorkItem(context.Background(), workitem.CreateLocalWorkItemInput{Title: "Real Persisted Item", Description: &description})
	if err != nil {
		t.Fatal(err)
	}
	trackingService := tracking.NewService(database, service)
	plannerService := planner.NewService(database, planner.NewRepository(database))
	readcapService, err := readcap.NewService(service, trackingService, plannerService, database, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	engine, err := assistant.NewNativeRuntimeAdapter(executor, readcapService, NewProposeRegistry())
	if err != nil {
		t.Fatal(err)
	}
	application.trackingService = trackingService
	application.aiExecutor = executor
	application.assistantRuntime = engine
	snapshot := func() (string, string, string) {
		var title, desc, updated string
		if err := database.QueryRow("SELECT title, COALESCE(description,''), updated_at FROM work_items WHERE entity_id = ?", item.ID).Scan(&title, &desc, &updated); err != nil {
			t.Fatal(err)
		}
		var proposals int
		if err := database.QueryRow("SELECT COUNT(*) FROM change_proposals").Scan(&proposals); err != nil {
			t.Fatal(err)
		}
		if proposals != 0 {
			t.Fatalf("Assistant read path created %d proposals", proposals)
		}
		return title, desc, updated
	}
	return application, item, snapshot
}

func TestAskAssistantAnswersFromAuthoritativeWorkItemThroughCanonicalAction(t *testing.T) {
	executor := &scriptedAIExecutor{
		outputs: map[ai.Action]string{
			ai.ActionRuntimeCapabilityDecision: `{"version":1,"decision":"FINAL","calls":[]}`,
			ai.ActionAssistantAsk:              `{"version":1,"kind":"ANSWER","text":"It tracks a real item.","candidate":null}`,
		},
	}
	application, item, _ := newAssistantApp(t, executor)
	result, err := application.AskAssistant(AskAssistantRequestDTO{
		Message: "Summarize this work.", Surface: "WORK_ITEMS", WorkItemID: item.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != "ANSWER" || result.ContextLabel != "Real Persisted Item" || result.ChangeApplied {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(executor.actions) != 2 || executor.actions[0] != ai.ActionRuntimeCapabilityDecision || executor.actions[1] != ai.ActionAssistantAsk {
		t.Fatalf("expected read_decision then ask, got %v", executor.actions)
	}
	contextMessage := executor.requests[1].Messages[1].Content
	if !strings.Contains(contextMessage, "Real Persisted Item") || !strings.Contains(contextMessage, "Original description") {
		t.Fatalf("context did not come from the persisted WorkItem: %s", contextMessage)
	}
}

func TestAskAssistantFailuresAreSafeAndRedacted(t *testing.T) {
	for name, executor := range map[string]*scriptedAIExecutor{
		"runtime": {
			errs: map[ai.Action]error{ai.ActionRuntimeCapabilityDecision: errors.New("provider-secret-detail")},
		},
		"malformed": {
			outputs: map[ai.Action]string{
				ai.ActionRuntimeCapabilityDecision: `{"version":1,"decision":"FINAL","calls":[]}`,
				ai.ActionAssistantAsk:              `{"kind":"ANSWER"}`,
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			application, item, snapshot := newAssistantApp(t, executor)
			beforeTitle, beforeDesc, beforeUpdated := snapshot()
			_, err := application.AskAssistant(AskAssistantRequestDTO{
				Message: "hi", Surface: "WORK_ITEMS", WorkItemID: item.ID,
			})
			if err == nil || strings.Contains(err.Error(), "provider-secret-detail") {
				t.Fatalf("expected safe error, got %v", err)
			}
			afterTitle, afterDesc, afterUpdated := snapshot()
			if beforeTitle != afterTitle || beforeDesc != afterDesc || beforeUpdated != afterUpdated {
				t.Fatal("failure modified the WorkItem")
			}
		})
	}
}

func TestAskAssistantWithoutSelectionAnswersGlobally(t *testing.T) {
	executor := &scriptedAIExecutor{
		outputs: map[ai.Action]string{
			ai.ActionRuntimeCapabilityDecision: `{"version":1,"decision":"FINAL","calls":[]}`,
			ai.ActionAssistantAsk:              `{"version":1,"kind":"ANSWER","text":"Global answer without selection.","candidate":null}`,
		},
	}
	application, _, _ := newAssistantApp(t, executor)
	result, err := application.AskAssistant(AskAssistantRequestDTO{Message: "hi", Surface: "DASHBOARD"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Kind != "ANSWER" || result.Text != "Global answer without selection." || result.ContextLabel != "" {
		t.Fatalf("unexpected: %+v", result)
	}
	if len(executor.actions) != 2 || executor.actions[0] != ai.ActionRuntimeCapabilityDecision || executor.actions[1] != ai.ActionAssistantAsk {
		t.Fatalf("expected read_decision then ask, got %v", executor.actions)
	}
}

func TestAskAssistantIsUnavailableWithoutProductionComposition(t *testing.T) {
	application, _, _ := newWorkItemApp(t)
	if _, err := application.AskAssistant(AskAssistantRequestDTO{Message: "hi"}); err == nil {
		t.Fatal("expected uninitialized assistant to fail honestly")
	}
}

// scriptedAIExecutor answers each canonical Action with its own output and can
// run a hook while an action "executes" (to simulate concurrent change).
type scriptedAIExecutor struct {
	outputs       map[ai.Action]string
	actionOutputs map[ai.Action][]string
	errs          map[ai.Action]error
	onAction      map[ai.Action]func()
	actions       []ai.Action
	requests      []ai.Request
}

func (e *scriptedAIExecutor) Execute(_ context.Context, request ai.Request) (ai.Response, error) {
	e.actions = append(e.actions, request.Action)
	e.requests = append(e.requests, request)
	if hook := e.onAction[request.Action]; hook != nil {
		hook()
	}
	if err := e.errs[request.Action]; err != nil {
		return ai.Response{}, err
	}
	var out string
	if len(e.actionOutputs[request.Action]) > 0 {
		out = e.actionOutputs[request.Action][0]
		e.actionOutputs[request.Action] = e.actionOutputs[request.Action][1:]
	} else if e.outputs != nil {
		out = e.outputs[request.Action]
	}
	prov := ai.ResponseProvenance{
		ProviderInvoked: true,
		ActualProvider: &ai.ProviderInfo{
			ID:     "mock-ai",
			Name:   "Mock AI Provider",
			Source: ai.ProviderSourceBuiltIn,
		},
		Origin: ai.ResponseOrigin{
			Kind: ai.ResponseOriginProvider,
			ID:   "mock-ai",
			Name: "Mock AI Provider",
		},
	}
	return ai.Response{RequestID: request.RequestID, Action: request.Action, Output: json.RawMessage(out), Provenance: prov}, nil
}

func candidateOutput(title, description string) string {
	return `{"version":1,"capability_id":"work_item.update_details","arguments":{"proposed_title":"` + title + `","proposed_description":` + description + `},"reason":"clearer"}`
}

type mutationHarness struct {
	app      *App
	item     *workitem.WorkItem
	service  *workitem.Service
	db       *sql.DB
	executor *scriptedAIExecutor
}

func newMutationHarness(t *testing.T, proposalOutput string) *mutationHarness {
	t.Helper()
	application, service, db := newWorkItemApp(t)
	description := "Original description"
	item, err := service.CreateLocalWorkItem(context.Background(), workitem.CreateLocalWorkItemInput{Title: "Real Persisted Item", Description: &description})
	if err != nil {
		t.Fatal(err)
	}
	trackingService := tracking.NewService(db, service)
	plannerService := planner.NewService(db, planner.NewRepository(db))
	readcapService, err := readcap.NewService(service, trackingService, plannerService, db, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	application.proposalService = proposal.NewService(db, service)
	application.trackingService = trackingService
	executor := &scriptedAIExecutor{
		outputs: map[ai.Action]string{
			ai.ActionRuntimeCapabilityDecision: `{"version":1,"decision":"FINAL","calls":[]}`,
			ai.ActionAssistantAsk:              `{"version":1,"kind":"MUTATION_CANDIDATE","text":"Rename it.","candidate":` + proposalOutput + `}`,
		},
	}
	engine, err := assistant.NewNativeRuntimeAdapter(executor, readcapService, NewProposeRegistry())
	if err != nil {
		t.Fatal(err)
	}
	application.aiExecutor = executor
	application.assistantRuntime = engine
	return &mutationHarness{app: application, item: item, service: service, db: db, executor: executor}
}

func (h *mutationHarness) ask(t *testing.T) (AssistantResultDTO, error) {
	t.Helper()
	return h.app.AskAssistant(AskAssistantRequestDTO{Message: "Rename it", Surface: "WORK_ITEMS", WorkItemID: h.item.ID})
}

func (h *mutationHarness) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := h.db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (h *mutationHarness) proposals(t *testing.T) int {
	return h.count(t, "SELECT COUNT(*) FROM change_proposals")
}

func (h *mutationHarness) revisions(t *testing.T) int {
	return h.count(t, "SELECT COUNT(*) FROM revisions")
}

func (h *mutationHarness) assertWorkItemUnchanged(t *testing.T, want *workitem.WorkItem) {
	t.Helper()
	got, err := h.service.GetByID(context.Background(), want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != want.Title || got.LastRevisionID != want.LastRevisionID || !got.UpdatedAt.Equal(want.UpdatedAt) ||
		(got.Description == nil) != (want.Description == nil) || (got.Description != nil && *got.Description != *want.Description) {
		t.Fatalf("WorkItem truth changed: before=%+v after=%+v", want, got)
	}
}

func TestAssistantMutationCreatesDurablePendingProposalWithoutChangingTruth(t *testing.T) {
	h := newMutationHarness(t, candidateOutput("Renamed Item", `"Clearer description"`))
	revisionsBefore := h.revisions(t)

	result, err := h.ask(t)
	if err != nil {
		t.Fatal(err)
	}
	if result.Kind != "MUTATION_INTENT" || result.ProposalOutcome != AssistantProposalCreated || result.ProposalID == "" || result.ChangeApplied {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(h.executor.actions) != 2 || h.executor.actions[0] != ai.ActionRuntimeCapabilityDecision || h.executor.actions[1] != ai.ActionAssistantAsk {
		t.Fatalf("expected read_decision then ask, got %v", h.executor.actions)
	}

	created, err := h.app.proposalService.GetByID(context.Background(), result.ProposalID)
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != proposal.StatusPending || created.TargetEntityID == nil || *created.TargetEntityID != h.item.ID || created.OperationType != proposal.OperationTypeWorkItemUpdateDetails {
		t.Fatalf("unexpected proposal: %+v", created)
	}
	payload := created.Payload
	if payload.ProposedTitle != "Renamed Item" || payload.ProposedDescription == nil || *payload.ProposedDescription != "Clearer description" {
		t.Fatalf("proposed values do not match the validated candidate: %+v", payload)
	}
	if payload.Preconditions.ExpectedLastRevisionID != h.item.LastRevisionID || payload.Preconditions.ExpectedTitle != "Real Persisted Item" ||
		payload.Preconditions.ExpectedDescription == nil || *payload.Preconditions.ExpectedDescription != "Original description" {
		t.Fatalf("preconditions are not the authoritative snapshot: %+v", payload.Preconditions)
	}
	if created.Reason == nil || *created.Reason != "clearer" {
		t.Fatalf("reason: %+v", created.Reason)
	}

	h.assertWorkItemUnchanged(t, h.item)
	if h.revisions(t) != revisionsBefore {
		t.Fatal("Proposal creation must not create a Foundation Revision")
	}
	events, err := history.NewRepository(h.db).ListByCorrelation(context.Background(), history.ProposalCorrelation(result.ProposalID), 10)
	if err != nil || len(events) != 1 {
		t.Fatalf("AI Proposal history: events=%+v err=%v", events, err)
	}
	createdEvent := events[0]
	if createdEvent.Type != history.EventProposalCreated || createdEvent.Actor != history.ActorAI || createdEvent.AIRunID != nil || createdEvent.SourceRevisionID != nil {
		t.Fatalf("AI Proposal provenance must be honest and must not invent durable AI/source links: %+v", createdEvent)
	}
	pending, err := h.app.GetPendingProposals()
	if err != nil || len(pending) != 1 || pending[0].ID != result.ProposalID {
		t.Fatalf("AI Proposal must appear in the authoritative pending collection: %+v err=%v", pending, err)
	}
}

func TestAssistantProposalSurvivesDatabaseReopen(t *testing.T) {
	h := newMutationHarness(t, candidateOutput("Renamed Item", `null`))
	result, err := h.ask(t)
	if err != nil || result.ProposalID == "" {
		t.Fatalf("unexpected: %+v err=%v", result, err)
	}
	if err := h.db.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := database.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.DB.Close() })
	persisted, err := proposal.NewService(reopened.DB, workitem.NewService(reopened.DB, workitem.NewRepository(reopened.DB))).GetByID(context.Background(), result.ProposalID)
	if err != nil || persisted.Status != proposal.StatusPending || persisted.Payload.ProposedTitle != "Renamed Item" {
		t.Fatalf("proposal did not survive reopen: %+v err=%v", persisted, err)
	}
}

// Completion blocker: the candidate is bound to the snapshot the model saw.
func TestAssistantContextChangedDuringGenerationCreatesNoProposal(t *testing.T) {
	h := newMutationHarness(t, candidateOutput("Candidate B", `"Original description"`))
	h.executor.onAction = map[ai.Action]func(){ai.ActionAssistantAsk: func() {
		// The WorkItem moves to C/R2 while the model prepares candidate B.
		if _, err := h.service.UpdateLocalWorkItemDetails(context.Background(), workitem.UpdateLocalWorkItemDetailsInput{ID: h.item.ID, Title: "Concurrent C"}); err != nil {
			t.Fatal(err)
		}
	}}
	current := func() *workitem.WorkItem {
		got, err := h.service.GetByID(context.Background(), h.item.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	revisionsBefore := h.revisions(t)
	result, err := h.ask(t)
	if err != nil {
		t.Fatal(err)
	}
	if result.ProposalOutcome != AssistantProposalContextChanged || result.ProposalID != "" || result.ChangeApplied {
		t.Fatalf("expected context-changed, got %+v", result)
	}
	if h.proposals(t) != 0 {
		t.Fatal("a Proposal derived from a stale snapshot must never be created")
	}
	after := current()
	if after.Title != "Concurrent C" || after.LastRevisionID == h.item.LastRevisionID {
		t.Fatalf("concurrent change must be preserved untouched: %+v", after)
	}
	// Only the concurrent update produced a Revision; the refused creation none.
	if h.revisions(t) != revisionsBefore+1 {
		t.Fatalf("expected exactly the concurrent update's Revision, got %d -> %d", revisionsBefore, h.revisions(t))
	}
}

func TestAssistantFreshSnapshotAfterChangeCreatesProposalAgainstNewBase(t *testing.T) {
	h := newMutationHarness(t, candidateOutput("Candidate B", `null`))
	updated, err := h.service.UpdateLocalWorkItemDetails(context.Background(), workitem.UpdateLocalWorkItemDetailsInput{ID: h.item.ID, Title: "Concurrent C"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := h.ask(t)
	if err != nil || result.ProposalOutcome != AssistantProposalCreated {
		t.Fatalf("unexpected: %+v err=%v", result, err)
	}
	created, _ := h.app.proposalService.GetByID(context.Background(), result.ProposalID)
	if created.Payload.Preconditions.ExpectedLastRevisionID != updated.LastRevisionID || created.Payload.Preconditions.ExpectedTitle != "Concurrent C" {
		t.Fatalf("a fresh ask must capture the fresh snapshot: %+v", created.Payload.Preconditions)
	}
}

func TestAssistantNoOpCandidateCreatesNoProposal(t *testing.T) {
	h := newMutationHarness(t, candidateOutput("Real Persisted Item", `"Original description"`))
	revisionsBefore := h.revisions(t)
	result, err := h.ask(t)
	if err != nil || result.ProposalOutcome != AssistantProposalNoChange || result.ProposalID != "" {
		t.Fatalf("unexpected: %+v err=%v", result, err)
	}
	if h.proposals(t) != 0 || h.revisions(t) != revisionsBefore {
		t.Fatal("no-op must create neither a Proposal nor a Revision")
	}
	h.assertWorkItemUnchanged(t, h.item)
}

func TestAssistantMutationFailuresCreateNothing(t *testing.T) {
	cases := map[string]func(*mutationHarness){
		"ask prompt 2 runtime failure": func(h *mutationHarness) {
			h.executor.errs = map[ai.Action]error{ai.ActionAssistantAsk: errors.New("provider-secret")}
		},
		"proposal malformed": func(h *mutationHarness) {
			h.executor.outputs[ai.ActionAssistantAsk] = `{"version":1,"kind":"MUTATION_CANDIDATE","text":"Rename","candidate":{"version":1}}`
		},
		"real capability with wrong arguments": func(h *mutationHarness) {
			h.executor.outputs[ai.ActionAssistantAsk] = `{"version":1,"kind":"MUTATION_CANDIDATE","text":"Rename","candidate":{"version":1,"capability_id":"workitem.set_lifecycle","arguments":{"proposed_title":"x"},"reason":""}}`
		},
		"unknown capability": func(h *mutationHarness) {
			h.executor.outputs[ai.ActionAssistantAsk] = `{"version":1,"kind":"MUTATION_CANDIDATE","text":"Rename","candidate":{"version":1,"capability_id":"not.a.real.capability","arguments":{},"reason":""}}`
		},
		"blank candidate title": func(h *mutationHarness) {
			h.executor.outputs[ai.ActionAssistantAsk] = `{"version":1,"kind":"MUTATION_CANDIDATE","text":"Rename","candidate":` + candidateOutput("  ", `null`) + `}`
		},
		"read decision prompt 1 runtime failure": func(h *mutationHarness) {
			h.executor.errs = map[ai.Action]error{ai.ActionRuntimeCapabilityDecision: errors.New("provider-secret")}
		},
		"unsupported classification": func(h *mutationHarness) {
			h.executor.outputs[ai.ActionAssistantAsk] = `{"version":1,"kind":"UNSUPPORTED","text":"Only title and description changes."}`
		},
		"needs context classification": func(h *mutationHarness) {
			h.executor.outputs[ai.ActionAssistantAsk] = `{"version":1,"kind":"NEEDS_CONTEXT","text":"Which item?"}`
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			h := newMutationHarness(t, candidateOutput("Renamed", `null`))
			mutate(h)
			revisionsBefore := h.revisions(t)
			result, err := h.ask(t)
			if err != nil && strings.Contains(err.Error(), "provider-secret") {
				t.Fatalf("provider detail leaked: %v", err)
			}
			if err == nil && (result.ProposalID != "" || result.ProposalOutcome == AssistantProposalCreated) {
				t.Fatalf("no Proposal may be reported: %+v", result)
			}
			if h.proposals(t) != 0 || h.revisions(t) != revisionsBefore {
				t.Fatal("failure created a Proposal or Revision")
			}
			h.assertWorkItemUnchanged(t, h.item)
		})
	}
}

func TestAssistantOnlyMutationIntentReachesAssistantProposal(t *testing.T) {
	h := newMutationHarness(t, candidateOutput("Renamed", `null`))
	h.executor.outputs[ai.ActionAssistantAsk] = `{"version":1,"kind":"ANSWER","text":"Just an answer."}`
	result, err := h.ask(t)
	if err != nil || result.Kind != "ANSWER" || result.ProposalOutcome != "" {
		t.Fatalf("unexpected: %+v err=%v", result, err)
	}
	if len(h.executor.actions) != 2 || h.proposals(t) != 0 {
		t.Fatalf("answer path must not generate a candidate: %v", h.executor.actions)
	}
}

func TestAssistantProposalCreationFailureIsHonest(t *testing.T) {
	h := newMutationHarness(t, candidateOutput("Renamed", `null`))
	h.app.proposalService = nil
	if _, err := h.ask(t); err == nil {
		t.Fatal("missing proposal service must fail honestly")
	}
	if h.proposals(t) != 0 {
		t.Fatal("no Proposal expected")
	}
}

// Evidence boundary: an AI-created Proposal is an ordinary M7 Proposal, so the
// existing App Apply/Reject entries (used by the M9A review UI) act on it
// exactly like any other producer's Proposal.
func TestAIProposalAppliesThroughExistingM7PathOnlyWhenApplied(t *testing.T) {
	h := newMutationHarness(t, candidateOutput("Renamed Item", `"Clearer description"`))
	result, err := h.ask(t)
	if err != nil || result.ProposalID == "" {
		t.Fatalf("unexpected: %+v err=%v", result, err)
	}
	h.assertWorkItemUnchanged(t, h.item) // still old truth while pending/previewed

	applied, err := h.app.ApplyProposal(ApplyProposalRequestDTO{ID: result.ProposalID})
	if err != nil {
		t.Fatal(err)
	}
	if applied.Outcome != "APPLIED" || applied.WorkItem == nil || applied.WorkItem.Title != "Renamed Item" {
		t.Fatalf("unexpected apply result: %+v", applied)
	}
	after, err := h.service.GetByID(context.Background(), h.item.ID)
	if err != nil || after.Title != "Renamed Item" || after.Description == nil || *after.Description != "Clearer description" {
		t.Fatalf("committed truth wrong: %+v err=%v", after, err)
	}
}

func TestAIProposalRejectLeavesWorkItemUnchanged(t *testing.T) {
	h := newMutationHarness(t, candidateOutput("Renamed Item", `null`))
	result, err := h.ask(t)
	if err != nil || result.ProposalID == "" {
		t.Fatalf("unexpected: %+v err=%v", result, err)
	}
	rejected, err := h.app.RejectProposal(RejectProposalRequestDTO{ID: result.ProposalID})
	if err != nil || rejected.Status != "REJECTED" {
		t.Fatalf("unexpected: %+v err=%v", rejected, err)
	}
	h.assertWorkItemUnchanged(t, h.item)
}

func TestAIProposalGoesStaleLikeAnyOtherProposal(t *testing.T) {
	h := newMutationHarness(t, candidateOutput("Renamed Item", `null`))
	result, err := h.ask(t)
	if err != nil || result.ProposalID == "" {
		t.Fatalf("unexpected: %+v err=%v", result, err)
	}
	if _, err := h.service.UpdateLocalWorkItemDetails(context.Background(), workitem.UpdateLocalWorkItemDetailsInput{ID: h.item.ID, Title: "Changed later"}); err != nil {
		t.Fatal(err)
	}
	applied, err := h.app.ApplyProposal(ApplyProposalRequestDTO{ID: result.ProposalID})
	if err != nil || applied.Outcome != "STALE" {
		t.Fatalf("expected STALE via existing M7 semantics: %+v err=%v", applied, err)
	}
}

func TestAskAssistant_CoreOneReadFlow(t *testing.T) {
	app, service, db := newWorkItemApp(t)
	desc := "Searchable description"
	item, err := service.CreateLocalWorkItem(context.Background(), workitem.CreateLocalWorkItemInput{Title: "Special Target Item", Description: &desc})
	if err != nil {
		t.Fatal(err)
	}
	trackingService := tracking.NewService(db, service)
	plannerService := planner.NewService(db, planner.NewRepository(db))
	readcapService, err := readcap.NewService(service, trackingService, plannerService, db, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	executor := &scriptedAIExecutor{
		actionOutputs: map[ai.Action][]string{
			ai.ActionRuntimeCapabilityDecision: {
				`{"version":1,"decision":"CALL","calls":[{"capability_id":"entity.search","arguments_json":"{\"query\":\"Special\"}"}]}`,
				`{"version":1,"decision":"FINAL","calls":[]}`,
			},
		},
		outputs: map[ai.Action]string{
			ai.ActionAssistantAsk: `{"version":1,"kind":"ANSWER","text":"Found item: Special Target Item"}`,
		},
	}
	engine, err := assistant.NewNativeRuntimeAdapter(executor, readcapService, NewProposeRegistry())
	if err != nil {
		t.Fatal(err)
	}
	app.assistantRuntime = engine

	result, err := app.AskAssistant(AskAssistantRequestDTO{
		Message:    "Find my special item",
		Surface:    "WORK_ITEMS",
		WorkItemID: item.ID,
	})
	if err != nil {
		t.Fatalf("AskAssistant failed: %v", err)
	}
	if result.Kind != "ANSWER" || result.Text != "Found item: Special Target Item" {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(executor.actions) != 3 || executor.actions[0] != ai.ActionRuntimeCapabilityDecision || executor.actions[1] != ai.ActionRuntimeCapabilityDecision || executor.actions[2] != ai.ActionAssistantAsk {
		t.Fatalf("expected read decision then ask, got: %v", executor.actions)
	}
	if result.ProposalID != "" || result.ProposalOutcome != "" {
		t.Fatalf("no proposal expected on read flow: %+v", result)
	}
	_ = item
}

func TestAskAssistant_ChainedTwoRead_ResolveExternalToHistory(t *testing.T) {
	app, service, db := newWorkItemApp(t)
	item, err := service.CreateLocalWorkItem(context.Background(), workitem.CreateLocalWorkItemInput{Title: "Issue from External System"})
	if err != nil {
		t.Fatal(err)
	}
	// Seed external ref
	if _, err := db.Exec("INSERT INTO external_refs (id, entity_id, system, external_id, relationship_type) VALUES ('ref-999', ?, 'jira', 'JIRA-999', 'AUTHORITATIVE')", item.ID); err != nil {
		t.Fatal(err)
	}
	trackingService := tracking.NewService(db, service)
	plannerService := planner.NewService(db, planner.NewRepository(db))
	historyService := history.NewService(history.NewRepository(db))
	readcapService, err := readcap.NewService(service, trackingService, plannerService, db, nil, nil, historyService)
	if err != nil {
		t.Fatal(err)
	}
	executor := &scriptedAIExecutor{
		actionOutputs: map[ai.Action][]string{
			ai.ActionRuntimeCapabilityDecision: {
				`{"version":1,"decision":"CALL","calls":[{"capability_id":"entity.resolve_external","arguments_json":"{\"externalId\":\"JIRA-999\"}"}]}`,
				`{"version":1,"decision":"CALL","calls":[{"capability_id":"history.read_for_workitem","arguments_json":"{\"workItemId\":\"` + item.ID + `\"}"}]}`,
			},
		},
		outputs: map[ai.Action]string{
			ai.ActionAssistantAsk: `{"version":1,"kind":"ANSWER","text":"Resolved JIRA-999 and inspected history."}`,
		},
	}
	engine, err := assistant.NewNativeRuntimeAdapter(executor, readcapService, NewProposeRegistry())
	if err != nil {
		t.Fatal(err)
	}
	app.assistantRuntime = engine

	result, err := app.AskAssistant(AskAssistantRequestDTO{
		Message: "What happened on JIRA-999?",
		Surface: "DASHBOARD",
	})
	if err != nil {
		t.Fatalf("AskAssistant failed: %v", err)
	}
	if result.Kind != "ANSWER" || result.Text != "Resolved JIRA-999 and inspected history." {
		t.Fatalf("unexpected result: %+v", result)
	}
	// 2 Prompt 1 calls + 1 Prompt 2 call = 3 AI calls
	if len(executor.actions) != 3 {
		t.Fatalf("expected 3 actions, got %d: %v", len(executor.actions), executor.actions)
	}
	if executor.actions[0] != ai.ActionRuntimeCapabilityDecision || executor.actions[1] != ai.ActionRuntimeCapabilityDecision || executor.actions[2] != ai.ActionAssistantAsk {
		t.Fatalf("unexpected actions: %v", executor.actions)
	}
}

func TestAskAssistant_NilBaseCandidateRejectedToUnsupported(t *testing.T) {
	app, service, db := newWorkItemApp(t)
	trackingService := tracking.NewService(db, service)
	plannerService := planner.NewService(db, planner.NewRepository(db))
	readcapService, err := readcap.NewService(service, trackingService, plannerService, db, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	executor := &scriptedAIExecutor{
		outputs: map[ai.Action]string{
			ai.ActionRuntimeCapabilityDecision: `{"version":1,"decision":"FINAL","calls":[]}`,
			ai.ActionAssistantAsk:              `{"version":1,"kind":"MUTATION_CANDIDATE","text":"Rename something","candidate":` + candidateOutput("Bad Candidate", `null`) + `}`,
		},
	}
	engine, err := assistant.NewNativeRuntimeAdapter(executor, readcapService, NewProposeRegistry())
	if err != nil {
		t.Fatal(err)
	}
	app.assistantRuntime = engine

	result, err := app.AskAssistant(AskAssistantRequestDTO{
		Message: "Rename the item",
		Surface: "DASHBOARD",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Kind != "UNSUPPORTED" {
		t.Fatalf("expected candidate without base to be downgraded to UNSUPPORTED, got: %+v", result)
	}
	if result.ProposalOutcome != "" || result.ProposalID != "" {
		t.Fatalf("no proposal must be created when base is nil: %+v", result)
	}
}
