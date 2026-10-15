package app

import (
	"context"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/testsupport"
	"lum-harbor-notifier/internal/workitem"
	"strings"
	"testing"
)

func TestAppHistoryProjectionPreservesDurableProvenance(t *testing.T) {
	testDB := testsupport.OpenTestDatabase(t)
	repository := history.NewRepository(testDB.DB)
	workItems := workitem.NewService(testDB.DB, workitem.NewRepository(testDB.DB))
	item, err := workItems.CreateLocalWorkItem(context.Background(), workitem.CreateLocalWorkItemInput{Title: "History projection"})
	if err != nil {
		t.Fatal(err)
	}
	proposals := proposal.NewService(testDB.DB, workItems)
	created, err := proposals.ProposeWorkItemUpdate(context.Background(), proposal.ProposeWorkItemUpdateInput{TargetWorkItemID: item.ID, ProposedTitle: "Projected title"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := proposals.ApplyProposal(context.Background(), proposal.ApplyProposalInput{ID: created.ID}); err != nil {
		t.Fatal(err)
	}
	correlationID := history.ProposalCorrelation(created.ID)

	application := &App{historyService: history.NewService(repository)}
	page, err := application.QueryHistory(HistoryQueryRequestDTO{Days: 7, Scope: "PROPOSAL", Search: "created"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) != 1 || page.Truncated {
		t.Fatalf("unexpected event page: %+v", page)
	}
	event := page.Events[0]
	if event.ProposalID == nil || *event.ProposalID != created.ID || event.WorkItemID == nil || *event.WorkItemID != item.ID {
		t.Fatalf("provenance identity was not preserved: %+v", event)
	}
	if event.Before != nil || event.After["status"] != "PENDING" {
		t.Fatalf("snapshots were not projected: before=%v after=%v", event.Before, event.After)
	}

	traces, err := application.ListHistoryTraces()
	if err != nil {
		t.Fatal(err)
	}
	if len(traces.Traces) != 1 || traces.Traces[0].CorrelationID != correlationID || traces.Traces[0].EventCount < 2 {
		t.Fatalf("unexpected trace summaries: %+v", traces)
	}
	trace, err := application.GetHistoryTrace(correlationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(trace.Events) < 2 || trace.Events[0].ID != event.ID {
		t.Fatalf("trace ordering or causation changed: %+v", trace)
	}
	var applied *HistoryEventDTO
	for index := range trace.Events {
		if trace.Events[index].EventType == string(history.EventProposalApplied) {
			applied = &trace.Events[index]
			break
		}
	}
	if applied == nil || applied.Before["status"] != "PENDING" || applied.After["status"] != "APPLIED" {
		t.Fatalf("proposal transition snapshots changed: %+v", trace)
	}
}

func TestAppHistoryProjectionRejectsInvalidFiltersAndUnavailableTraces(t *testing.T) {
	testDB := testsupport.OpenTestDatabase(t)
	application := &App{historyService: history.NewService(history.NewRepository(testDB.DB))}

	if _, err := application.QueryHistory(HistoryQueryRequestDTO{Days: 1}); err == nil || !strings.Contains(err.Error(), "7, 30, or 90") {
		t.Fatalf("expected range validation, got %v", err)
	}
	if _, err := application.QueryHistory(HistoryQueryRequestDTO{Days: 7, Scope: "FAKE"}); err == nil || !strings.Contains(err.Error(), "unsupported history scope") {
		t.Fatalf("expected scope validation, got %v", err)
	}
	if _, err := application.GetHistoryTrace("not-a-real-trace"); err == nil {
		t.Fatal("expected unavailable correlation to be rejected")
	}
}

func TestQueryWorkItemHistoryScopesByAuthoritativeLinkageNewestFirst(t *testing.T) {
	testDB := testsupport.OpenTestDatabase(t)
	ctx := context.Background()
	workItems := workitem.NewService(testDB.DB, workitem.NewRepository(testDB.DB))
	a, err := workItems.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: "Scoped A"})
	if err != nil {
		t.Fatal(err)
	}
	// B's title deliberately contains A's title: membership must not use text.
	b, err := workItems.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: "Scoped A lookalike"})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := workItems.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: "Untouched"})
	if err != nil {
		t.Fatal(err)
	}
	proposals := proposal.NewService(testDB.DB, workItems)
	created, err := proposals.ProposeWorkItemUpdate(ctx, proposal.ProposeWorkItemUpdateInput{TargetWorkItemID: a.ID, ProposedTitle: "Scoped A renamed"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := proposals.ApplyProposal(ctx, proposal.ApplyProposalInput{ID: created.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := workItems.SetLocalWorkItemLifecycleState(ctx, workitem.SetLocalWorkItemLifecycleStateInput{ID: a.ID, Target: workitem.LifecycleCompleted}); err != nil {
		t.Fatal(err)
	}
	application := &App{historyService: history.NewService(history.NewRepository(testDB.DB))}

	page, err := application.QueryWorkItemHistory(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) < 4 || page.Truncated {
		t.Fatalf("expected create + proposal + lifecycle history for A: %+v", page)
	}
	scopes := map[string]bool{}
	for index, event := range page.Events {
		if event.WorkItemID == nil || *event.WorkItemID != a.ID {
			t.Fatalf("event %s is not linked to A: %+v", event.ID, event)
		}
		if index > 0 && page.Events[index-1].OccurredAt < event.OccurredAt {
			t.Fatalf("history must be newest first: %s before %s", page.Events[index-1].OccurredAt, event.OccurredAt)
		}
		scopes[event.Scope] = true
	}
	if !scopes["WORK_ITEM"] || !scopes["PROPOSAL"] {
		t.Fatalf("scoped history must include every linked scope, got %v", scopes)
	}

	other, err := application.QueryWorkItemHistory(b.ID)
	if err != nil || len(other.Events) != 1 || *other.Events[0].WorkItemID != b.ID {
		t.Fatalf("B must only see its own creation: %+v err=%v", other, err)
	}
	none, err := application.QueryWorkItemHistory(empty.ID)
	if err != nil || len(none.Events) != 1 {
		t.Fatalf("a WorkItem with only its creation shows exactly that: %+v err=%v", none, err)
	}
	unknown, err := application.QueryWorkItemHistory("no-such-work-item")
	if err != nil || len(unknown.Events) != 0 {
		t.Fatalf("no linked events is a valid empty page: %+v err=%v", unknown, err)
	}
	if _, err := application.QueryWorkItemHistory("  "); err == nil {
		t.Fatal("a blank WorkItem ID must be rejected")
	}
}

func TestAppGetCausalChain(t *testing.T) {
	testDB := testsupport.OpenTestDatabase(t)
	repository := history.NewRepository(testDB.DB)
	workItems := workitem.NewService(testDB.DB, workitem.NewRepository(testDB.DB))
	item, err := workItems.CreateLocalWorkItem(context.Background(), workitem.CreateLocalWorkItemInput{Title: "Causal chain item"})
	if err != nil {
		t.Fatal(err)
	}

	proposals := proposal.NewService(testDB.DB, workItems)
	created, err := proposals.ProposeWorkItemUpdate(context.Background(), proposal.ProposeWorkItemUpdateInput{TargetWorkItemID: item.ID, ProposedTitle: "Updated title"})
	if err != nil {
		t.Fatal(err)
	}
	applied, err := proposals.ApplyProposal(context.Background(), proposal.ApplyProposalInput{ID: created.ID})
	if err != nil {
		t.Fatal(err)
	}

	application := &App{historyService: history.NewService(repository)}

	// Find the applied event
	page, err := application.QueryHistory(HistoryQueryRequestDTO{Days: 7, Scope: "PROPOSAL", Search: "applied"})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Events) == 0 {
		t.Fatal("expected at least one proposal applied event")
	}
	appliedEvent := page.Events[0]

	chain, err := application.GetCausalChain(appliedEvent.ID)
	if err != nil {
		t.Fatalf("GetCausalChain failed: %v", err)
	}
	if chain.TargetEventID != appliedEvent.ID {
		t.Fatalf("expected target event %s, got %s", appliedEvent.ID, chain.TargetEventID)
	}
	if len(chain.Events) < 2 {
		t.Fatalf("expected at least 2 events in chain, got %d", len(chain.Events))
	}
	_ = applied
}
