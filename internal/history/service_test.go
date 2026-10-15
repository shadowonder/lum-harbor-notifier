package history_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/ailineage"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/source"
	"lum-harbor-notifier/internal/tracking"
)

func TestApplicationProjectionUsesRealEventsAndCorrelations(t *testing.T) {
	f := openFixture(t)
	now := time.Now().UTC()
	item := f.workItem(t)
	monitor := f.monitor(t, item, tracking.MonitorDeadline, now.Add(-time.Hour))
	if _, err := f.attention.Reconcile(f.ctx, now); err != nil {
		t.Fatal(err)
	}
	proposalService := proposal.NewService(f.db, f.workItems)
	p, err := proposalService.ProposeWorkItemUpdate(f.ctx, proposal.ProposeWorkItemUpdateInput{TargetWorkItemID: item.ID, ProposedTitle: "Projected title"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := proposalService.ApplyProposal(f.ctx, proposal.ApplyProposalInput{ID: p.ID}); err != nil {
		t.Fatal(err)
	}
	plannerService := planner.NewService(f.db, planner.NewRepository(f.db))
	planned, err := plannerService.CreateLocalTodo(f.ctx, planner.CreateLocalTodoInput{Title: "Projected todo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plannerService.CompleteLocalTodo(f.ctx, planned.ID); err != nil {
		t.Fatal(err)
	}

	sourceService := source.NewService(f.db)
	checkpoint, _ := sourceService.PrepareFetch(f.ctx, "projection", "instance", "source.fetch")
	ingested, err := sourceService.IngestFetchResult(f.ctx, source.FetchBatch{ExtensionID: "projection", InstanceID: "instance", CapabilityID: "source.fetch", Checkpoint: checkpoint, Records: []source.Observation{{ExternalID: "external", SourceKind: "test.item", ObservedAt: now, Payload: json.RawMessage(`{"value":1}`)}}})
	if err != nil {
		t.Fatal(err)
	}
	sourceRevisionID := ingested.NewlyChangedRevisionIDs[0]
	lineage := ailineage.NewService(f.db)
	clean := ailineage.StartRun{ID: "projection-clean", ProcessingID: "projection-processing", RequestID: "projection-clean-request", Action: ai.ActionSourceClean, CanonicalRequest: json.RawMessage(`{"action":"source.clean"}`), SourceRevisionID: sourceRevisionID, StartedAt: now}
	if err := lineage.Start(f.ctx, clean); err != nil {
		t.Fatal(err)
	}
	provenance := ai.ResponseProvenance{ProviderInvoked: true, ActualProvider: &ai.ProviderInfo{ID: "test", Name: "Test", Source: ai.ProviderSourceBuiltIn}, Origin: ai.ResponseOrigin{Kind: ai.ResponseOriginProvider, ID: "test", Name: "Test"}}
	if err := lineage.Succeed(f.ctx, clean.ID, json.RawMessage(`{"ok":true}`), provenance, now.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	interpret := ailineage.StartRun{ID: "projection-interpret", ProcessingID: clean.ProcessingID, RequestID: "projection-interpret-request", Action: ai.ActionSourceInterpret, CanonicalRequest: json.RawMessage(`{"action":"source.interpret"}`), SourceRevisionID: sourceRevisionID, UpstreamRunID: &clean.ID, StartedAt: now.Add(2 * time.Second)}
	if err := lineage.Start(f.ctx, interpret); err != nil {
		t.Fatal(err)
	}
	if err := lineage.Succeed(f.ctx, interpret.ID, json.RawMessage(`{"ok":true}`), provenance, now.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}

	service := history.NewService(history.NewRepository(f.db))
	page, err := service.Query(context.Background(), history.Query{Since: now.Add(-24 * time.Hour)})
	if err != nil || page.Truncated {
		t.Fatalf("query: %+v %v", page, err)
	}
	wantTypes := map[history.EventType]bool{
		history.EventMonitorCreated: false, history.EventAttentionOpened: false, history.EventNotificationCreated: false,
		history.EventProposalApplied: false, history.EventPlannerItemCompleted: false,
		history.EventSourceRevisionObserved: false, history.EventAIRunSucceeded: false,
	}
	for _, event := range page.Events {
		if _, ok := wantTypes[event.Type]; ok {
			wantTypes[event.Type] = true
		}
	}
	for eventType, found := range wantTypes {
		if !found {
			t.Errorf("missing projected event %s", eventType)
		}
	}

	traces, err := service.ListTraces(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wantCorrelations := map[string]bool{history.MonitorCorrelation(monitor.ID): false, history.ProposalCorrelation(p.ID): false, history.ProcessingCorrelation(clean.ProcessingID): false}
	for _, trace := range traces.Traces {
		if _, ok := wantCorrelations[trace.CorrelationID]; ok {
			wantCorrelations[trace.CorrelationID] = true
			if trace.EventCount < 2 {
				t.Errorf("single-event trace leaked: %+v", trace)
			}
		}
	}
	if monitorTrace, err := service.GetTrace(context.Background(), history.MonitorCorrelation(monitor.ID)); err != nil {
		t.Fatal(err)
	} else {
		monitorTypes := map[history.EventType]bool{}
		for _, event := range monitorTrace.Events {
			monitorTypes[event.Type] = true
		}
		for _, eventType := range []history.EventType{history.EventMonitorCreated, history.EventAttentionOpened, history.EventNotificationCreated} {
			if !monitorTypes[eventType] {
				t.Errorf("monitor trace is missing %s", eventType)
			}
		}
	}
	for correlation, found := range wantCorrelations {
		if !found {
			t.Errorf("missing trace %s", correlation)
		}
	}
	trace, err := service.GetTrace(context.Background(), history.ProcessingCorrelation(clean.ProcessingID))
	if err != nil || len(trace.Events) != 2 || trace.Events[1].CausationEventID == nil || *trace.Events[1].CausationEventID != trace.Events[0].ID {
		t.Fatalf("ordered causal trace: %+v %v", trace, err)
	}
}

func TestApplicationProjectionFiltersAndReportsTruncation(t *testing.T) {
	f := openFixture(t)
	repo := history.NewRepository(f.db)
	now := time.Now().UTC()
	for index := 0; index < 201; index++ {
		if _, err := repo.Append(f.ctx, history.Draft{Type: history.EventMonitorCreated, Scope: history.ScopeTracking, Actor: history.ActorUser, Summary: "bounded search event", CorrelationID: "single:" + string(rune(index+1000)), OccurredAt: now.Add(time.Duration(index) * time.Nanosecond)}); err != nil {
			t.Fatal(err)
		}
	}
	service := history.NewService(repo)
	page, err := service.Query(f.ctx, history.Query{Since: now.Add(-time.Hour), Search: "bounded search"})
	if err != nil || len(page.Events) != 200 || !page.Truncated {
		t.Fatalf("bounded page: len=%d truncated=%v err=%v", len(page.Events), page.Truncated, err)
	}
	scope := history.ScopeAI
	empty, err := service.Query(f.ctx, history.Query{Since: now.Add(-time.Hour), Scope: &scope})
	if err != nil || len(empty.Events) != 0 || empty.Truncated {
		t.Fatalf("scope filter: %+v %v", empty, err)
	}
	literalWildcard, err := service.Query(f.ctx, history.Query{Since: now.Add(-time.Hour), Search: "%"})
	if err != nil || len(literalWildcard.Events) != 0 {
		t.Fatalf("search wildcard must be literal: %+v %v", literalWildcard, err)
	}
}

func TestGetCausalChain(t *testing.T) {
	f := openFixture(t)
	repo := history.NewRepository(f.db)
	now := time.Now().UTC()

	// 1. Root Source event
	sourceService := source.NewService(f.db)
	rev, err := sourceService.CaptureIntentSource(f.ctx, source.CaptureIntentInput{
		ExtensionID: "core.manual",
		InstanceID:  "manual",
		SourceKind:  "test.intent",
		Payload:     []byte(`{"title":"Test intent"}`),
	})
	if err != nil {
		t.Fatalf("capture intent source: %v", err)
	}
	srcRevID := rev.ID
	srcEvents, err := repo.ListBySourceRevisionID(f.ctx, srcRevID)
	if err != nil || len(srcEvents) == 0 {
		t.Fatalf("expected observed event for source revision: %v", err)
	}
	ev1 := &srcEvents[0]
	corrID := ev1.CorrelationID

	// 2. Domain creation caused by Source event
	ev2, err := repo.Append(f.ctx, history.Draft{
		Type:             history.EventPlannerItemCreated,
		Scope:            history.ScopePlanner,
		Actor:            history.ActorUser,
		Summary:          "Created Todo Item",
		CorrelationID:    corrID,
		SourceRevisionID: &srcRevID,
		CausationEventID: &ev1.ID,
		OccurredAt:       now.Add(time.Second),
	})
	if err != nil {
		t.Fatalf("append ev2: %v", err)
	}

	// 3. Attention opened caused by Todo
	ev3, err := repo.Append(f.ctx, history.Draft{
		Type:             history.EventAttentionOpened,
		Scope:            history.ScopeAttention,
		Actor:            history.ActorSystem,
		Summary:          "Attention Issue Opened",
		CorrelationID:    corrID,
		CausationEventID: &ev2.ID,
		OccurredAt:       now.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("append ev3: %v", err)
	}

	// 4. Notification sent caused by Attention
	ev4, err := repo.Append(f.ctx, history.Draft{
		Type:             history.EventNotificationSent,
		Scope:            history.ScopeNotification,
		Actor:            history.ActorSystem,
		Summary:          "Notification Sent",
		CorrelationID:    corrID,
		CausationEventID: &ev3.ID,
		OccurredAt:       now.Add(3 * time.Second),
	})
	if err != nil {
		t.Fatalf("append ev4: %v", err)
	}

	// 5. Notification presented caused by Dispatch
	ev5, err := repo.Append(f.ctx, history.Draft{
		Type:             history.EventNotificationPresented,
		Scope:            history.ScopeNotification,
		Actor:            history.ActorUser,
		Summary:          "Notification Presented in App",
		CorrelationID:    corrID,
		CausationEventID: &ev4.ID,
		OccurredAt:       now.Add(4 * time.Second),
	})
	if err != nil {
		t.Fatalf("append ev5: %v", err)
	}

	service := history.NewService(repo)

	// Test 1: From terminal event (ev5), traverse all the way back to root
	chain, err := service.GetCausalChain(f.ctx, ev5.ID)
	if err != nil {
		t.Fatalf("GetCausalChain failed: %v", err)
	}
	if chain.TargetEvent.ID != ev5.ID {
		t.Fatalf("expected target event %s, got %s", ev5.ID, chain.TargetEvent.ID)
	}
	if chain.RootEvent == nil || chain.RootEvent.ID != ev1.ID {
		t.Fatalf("expected root event %s, got %+v", ev1.ID, chain.RootEvent)
	}
	if len(chain.Events) != 5 {
		t.Fatalf("expected 5 events in causal chain, got %d", len(chain.Events))
	}
	// Verify chronological order: ev1 -> ev2 -> ev3 -> ev4 -> ev5
	for i, expected := range []*history.Event{ev1, ev2, ev3, ev4, ev5} {
		if chain.Events[i].ID != expected.ID {
			t.Errorf("step %d: expected event %s (%s), got %s (%s)",
				i, expected.ID, expected.Type, chain.Events[i].ID, chain.Events[i].Type)
		}
	}

	// Test 2: From middle event (ev2), traverse both upstream and downstream
	midChain, err := service.GetCausalChain(f.ctx, ev2.ID)
	if err != nil {
		t.Fatalf("GetCausalChain on ev2 failed: %v", err)
	}
	if midChain.RootEvent == nil || midChain.RootEvent.ID != ev1.ID {
		t.Fatalf("expected root event %s on midChain, got %+v", ev1.ID, midChain.RootEvent)
	}
	if len(midChain.Events) != 5 {
		t.Fatalf("expected 5 events in midChain (1 upstream + target + 3 downstream), got %d", len(midChain.Events))
	}

	// Test 3: Get by subject entity ID using a real work item
	item := f.workItem(t)
	evItem, err := repo.Append(f.ctx, history.Draft{
		Type:             history.EventWorkItemCreated,
		Scope:            history.ScopeWorkItem,
		Actor:            history.ActorUser,
		Summary:          "Created Work Item",
		CorrelationID:    corrID,
		SubjectEntityID:  &item.ID,
		CausationEventID: &ev1.ID,
		OccurredAt:       now.Add(5 * time.Second),
	})
	if err != nil {
		t.Fatalf("append evItem: %v", err)
	}
	subjectChain, err := service.GetCausalChainBySubject(f.ctx, item.ID)
	if err != nil {
		t.Fatalf("GetCausalChainBySubject failed: %v", err)
	}
	if subjectChain.RootEvent == nil || subjectChain.RootEvent.ID != ev1.ID {
		t.Fatalf("expected root event %s on subjectChain, got %+v", ev1.ID, subjectChain.RootEvent)
	}
	_ = evItem
}
