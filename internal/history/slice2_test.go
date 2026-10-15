package history_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"lum-harbor-notifier/internal/database"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

func TestNotificationCancellationHistory(t *testing.T) {
	f := openFixture(t)
	now := time.Now().UTC()
	item := f.workItem(t)
	point := f.monitor(t, item, tracking.MonitorDeadline, now.Add(-time.Hour))
	if got, err := f.attention.Reconcile(f.ctx, now); err != nil || got.Opened != 1 {
		t.Fatalf("open: %+v %v", got, err)
	}
	views, err := f.attention.ListNotificationViews(f.ctx)
	if err != nil || len(views) != 1 {
		t.Fatalf("notifications: %+v %v", views, err)
	}
	notificationID := views[0].Notification.ID
	if _, err := f.tracking.SetMonitorPointState(f.ctx, tracking.SetMonitorPointStateInput{ID: point.ID, Target: tracking.MonitorSatisfied}); err != nil {
		t.Fatal(err)
	}
	if got, err := f.attention.Reconcile(f.ctx, now.Add(time.Minute)); err != nil || got.Resolved != 1 {
		t.Fatalf("resolve: %+v %v", got, err)
	}
	if got, err := f.attention.DispatchPendingNotifications(f.ctx, now.Add(2*time.Minute)); err != nil || got.Cancelled != 1 || got.Sent != 0 {
		t.Fatalf("cancel dispatch: %+v %v", got, err)
	}
	events, err := history.NewRepository(f.db).ListByCorrelation(f.ctx, history.MonitorCorrelation(point.ID), 20)
	if err != nil {
		t.Fatal(err)
	}
	byType := eventsByType(events)
	cancelled := byType[history.EventNotificationCancelled]
	resolved := byType[history.EventAttentionResolved]
	if cancelled.ID == "" || cancelled.Actor != history.ActorSystem || cancelled.SubjectEntityID == nil || *cancelled.SubjectEntityID != notificationID || cancelled.RevisionID == nil {
		t.Fatalf("invalid cancellation history: %+v", cancelled)
	}
	if cancelled.CausationEventID == nil || *cancelled.CausationEventID != resolved.ID {
		t.Fatalf("cancellation must be caused by resolution: resolved=%+v cancelled=%+v", resolved, cancelled)
	}
	if _, exists := byType[history.EventNotificationSent]; exists {
		t.Fatal("cancelled notification must not also be sent")
	}
	var after map[string]any
	if cancelled.AfterJSON == nil || json.Unmarshal([]byte(*cancelled.AfterJSON), &after) != nil || after["state"] != "CANCELLED" || after["sent_at"] != nil {
		t.Fatalf("unexpected cancellation snapshot: %v", cancelled.AfterJSON)
	}
}

func TestProposalApplyHistoryAndCorrelationIsolation(t *testing.T) {
	f := openFixture(t)
	service := proposal.NewService(f.db, f.workItems)
	item := f.workItem(t)
	first, err := service.ProposeWorkItemUpdate(f.ctx, proposal.ProposeWorkItemUpdateInput{TargetWorkItemID: item.ID, ProposedTitle: "Applied title", Producer: proposal.ProducerUser})
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.ProposeWorkItemUpdate(f.ctx, proposal.ProposeWorkItemUpdateInput{TargetWorkItemID: item.ID, ProposedTitle: "Other title", Producer: proposal.ProducerAI})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ApplyProposal(f.ctx, proposal.ApplyProposalInput{ID: first.ID})
	if err != nil || result.Outcome != proposal.ApplyOutcomeApplied {
		t.Fatalf("apply: %+v %v", result, err)
	}
	events, err := history.NewRepository(f.db).ListByCorrelation(f.ctx, history.ProposalCorrelation(first.ID), 10)
	if err != nil || len(events) != 3 {
		t.Fatalf("apply trace: %+v %v", events, err)
	}
	byType := eventsByType(events)
	created, applied, changed := byType[history.EventProposalCreated], byType[history.EventProposalApplied], byType[history.EventWorkItemDetailsChanged]
	if created.Actor != history.ActorUser || applied.Actor != history.ActorUser || changed.Actor != history.ActorUser {
		t.Fatalf("unexpected actors: %+v", events)
	}
	if applied.RevisionID == nil || changed.RevisionID == nil || *applied.RevisionID != result.WorkItem.LastRevisionID || *changed.RevisionID != *applied.RevisionID {
		t.Fatalf("apply and WorkItem history must share exact Revision: %+v %+v", applied, changed)
	}
	if changed.CausationEventID == nil || *changed.CausationEventID != applied.ID || changed.ProposalID == nil || *changed.ProposalID != first.ID {
		t.Fatalf("missing Proposal causal edge: %+v", changed)
	}
	other, err := history.NewRepository(f.db).ListByCorrelation(f.ctx, history.ProposalCorrelation(second.ID), 10)
	if err != nil || len(other) != 1 || other[0].Type != history.EventProposalCreated || other[0].Actor != history.ActorAI {
		t.Fatalf("second Proposal trace leaked or lost AI actor: %+v %v", other, err)
	}
	before := count(t, f.db, "SELECT COUNT(*) FROM history_events")
	again, err := service.ApplyProposal(f.ctx, proposal.ApplyProposalInput{ID: first.ID})
	if err != nil || again.Outcome != proposal.ApplyOutcomeAlreadyApplied || count(t, f.db, "SELECT COUNT(*) FROM history_events") != before {
		t.Fatalf("repeated apply must be a no-op: %+v %v", again, err)
	}
}

func TestProposalRejectedStaleAndApplyRollbackHistory(t *testing.T) {
	t.Run("rejected", func(t *testing.T) {
		f := openFixture(t)
		s := proposal.NewService(f.db, f.workItems)
		item := f.workItem(t)
		p, _ := s.ProposeWorkItemUpdate(f.ctx, proposal.ProposeWorkItemUpdateInput{TargetWorkItemID: item.ID, ProposedTitle: "Rejected"})
		revisions := count(t, f.db, "SELECT COUNT(*) FROM revisions")
		if _, err := s.RejectProposal(f.ctx, proposal.RejectProposalInput{ID: p.ID}); err != nil {
			t.Fatal(err)
		}
		before := count(t, f.db, "SELECT COUNT(*) FROM history_events")
		if _, err := s.RejectProposal(f.ctx, proposal.RejectProposalInput{ID: p.ID}); err != nil || count(t, f.db, "SELECT COUNT(*) FROM history_events") != before {
			t.Fatalf("repeat reject churned: %v", err)
		}
		events, _ := history.NewRepository(f.db).ListByCorrelation(f.ctx, history.ProposalCorrelation(p.ID), 10)
		if len(events) != 2 || events[1].Type != history.EventProposalRejected || count(t, f.db, "SELECT COUNT(*) FROM revisions") != revisions {
			t.Fatalf("rejected trace/revisions: %+v", events)
		}
	})

	t.Run("stale", func(t *testing.T) {
		f := openFixture(t)
		s := proposal.NewService(f.db, f.workItems)
		item := f.workItem(t)
		p, _ := s.ProposeWorkItemUpdate(f.ctx, proposal.ProposeWorkItemUpdateInput{TargetWorkItemID: item.ID, ProposedTitle: "Stale"})
		if _, err := f.workItems.UpdateLocalWorkItemDetails(f.ctx, workitem.UpdateLocalWorkItemDetailsInput{ID: item.ID, Title: "Changed first"}); err != nil {
			t.Fatal(err)
		}
		revisions := count(t, f.db, "SELECT COUNT(*) FROM revisions")
		result, err := s.ApplyProposal(f.ctx, proposal.ApplyProposalInput{ID: p.ID})
		if err != nil || result.Outcome != proposal.ApplyOutcomeStale || count(t, f.db, "SELECT COUNT(*) FROM revisions") != revisions {
			t.Fatalf("stale: %+v %v", result, err)
		}
		events, _ := history.NewRepository(f.db).ListByCorrelation(f.ctx, history.ProposalCorrelation(p.ID), 10)
		if len(events) != 2 || events[1].Type != history.EventProposalStale || events[1].Actor != history.ActorSystem || events[1].RevisionID != nil {
			t.Fatalf("stale trace: %+v", events)
		}
	})

	t.Run("second history insert failure", func(t *testing.T) {
		f := openFixture(t)
		s := proposal.NewService(f.db, f.workItems)
		item := f.workItem(t)
		p, _ := s.ProposeWorkItemUpdate(f.ctx, proposal.ProposeWorkItemUpdateInput{TargetWorkItemID: item.ID, ProposedTitle: "Must roll back"})
		revisions := count(t, f.db, "SELECT COUNT(*) FROM revisions")
		failEvent(t, f.db, history.EventWorkItemDetailsChanged)
		if _, err := s.ApplyProposal(f.ctx, proposal.ApplyProposalInput{ID: p.ID}); err == nil {
			t.Fatal("expected injected history failure")
		}
		loaded, _ := f.workItems.GetByID(f.ctx, item.ID)
		current, _ := s.GetByID(f.ctx, p.ID)
		events, _ := history.NewRepository(f.db).ListByCorrelation(f.ctx, history.ProposalCorrelation(p.ID), 10)
		if loaded.Title != item.Title || current.Status != proposal.StatusPending || len(events) != 1 || count(t, f.db, "SELECT COUNT(*) FROM revisions") != revisions {
			t.Fatalf("partial apply committed: item=%+v proposal=%+v events=%+v", loaded, current, events)
		}
	})
}

func TestWorkItemAndPlannerHistory(t *testing.T) {
	f := openFixture(t)
	item := f.workItem(t)
	updated, err := f.workItems.UpdateLocalWorkItemDetails(f.ctx, workitem.UpdateLocalWorkItemDetailsInput{ID: item.ID, Title: "History release updated"})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := f.workItems.SetLocalWorkItemLifecycleState(f.ctx, workitem.SetLocalWorkItemLifecycleStateInput{ID: item.ID, Target: workitem.LifecycleCompleted})
	if err != nil {
		t.Fatal(err)
	}
	beforeNoOp := count(t, f.db, "SELECT COUNT(*) FROM history_events")
	if _, err := f.workItems.SetLocalWorkItemLifecycleState(f.ctx, workitem.SetLocalWorkItemLifecycleStateInput{ID: item.ID, Target: workitem.LifecycleCompleted}); err != nil || count(t, f.db, "SELECT COUNT(*) FROM history_events") != beforeNoOp {
		t.Fatalf("same lifecycle churned: %v", err)
	}
	reopened, err := f.workItems.SetLocalWorkItemLifecycleState(f.ctx, workitem.SetLocalWorkItemLifecycleStateInput{ID: item.ID, Target: workitem.LifecycleActive})
	if err != nil {
		t.Fatal(err)
	}
	events, _ := history.NewRepository(f.db).ListByWorkItem(f.ctx, item.ID, 20)
	byType := eventsByType(events)
	if byType[history.EventWorkItemCreated].RevisionID == nil || *byType[history.EventWorkItemCreated].RevisionID != item.LastRevisionID || byType[history.EventWorkItemDetailsChanged].RevisionID == nil || *byType[history.EventWorkItemDetailsChanged].RevisionID != updated.LastRevisionID {
		t.Fatalf("WorkItem events do not match exact revisions: %+v", events)
	}
	lifecycleLinks := map[string]string{}
	for _, event := range events {
		if event.Type == history.EventWorkItemLifecycleChanged && event.RevisionID != nil {
			lifecycleLinks[*event.RevisionID] = event.CorrelationID
		}
	}
	if len(lifecycleLinks) != 2 || lifecycleLinks[completed.LastRevisionID] != history.RevisionCorrelation(completed.LastRevisionID) || lifecycleLinks[reopened.LastRevisionID] != history.RevisionCorrelation(reopened.LastRevisionID) {
		t.Fatalf("direct lifecycle mutations must use distinct Revision correlations: %+v", lifecycleLinks)
	}

	ps := planner.NewService(f.db, planner.NewRepository(f.db))
	planned, err := ps.CreateLocalTodo(f.ctx, planner.CreateLocalTodoInput{Title: "Document provenance"})
	if err != nil {
		t.Fatal(err)
	}
	done, err := ps.CompleteLocalTodo(f.ctx, planned.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeNoOp = count(t, f.db, "SELECT COUNT(*) FROM history_events")
	if _, err := ps.CompleteLocalTodo(f.ctx, planned.ID); err != nil || count(t, f.db, "SELECT COUNT(*) FROM history_events") != beforeNoOp {
		t.Fatalf("repeat completion churned: %v", err)
	}
	createdEvent := latestEvent(t, f, planned.ID, history.EventPlannerItemCreated)
	completedEvent := latestEvent(t, f, planned.ID, history.EventPlannerItemCompleted)
	if createdEvent.RevisionID == nil || *createdEvent.RevisionID != planned.LastRevisionID || createdEvent.CorrelationID != history.RevisionCorrelation(planned.LastRevisionID) || completedEvent.RevisionID == nil || *completedEvent.RevisionID != done.LastRevisionID || completedEvent.CorrelationID != history.RevisionCorrelation(done.LastRevisionID) {
		t.Fatalf("Planner events do not match exact revisions: %+v %+v", createdEvent, completedEvent)
	}
}

func TestSlice2HistoryFailuresRollbackBusinessMutations(t *testing.T) {
	t.Run("WorkItem create", func(t *testing.T) {
		f := openFixture(t)
		failEvent(t, f.db, history.EventWorkItemCreated)
		if _, err := f.workItems.CreateLocalWorkItem(f.ctx, workitem.CreateLocalWorkItemInput{Title: "rollback"}); err == nil {
			t.Fatal("expected failure")
		}
		if count(t, f.db, "SELECT COUNT(*) FROM work_items") != 0 || count(t, f.db, "SELECT COUNT(*) FROM revisions") != 0 {
			t.Fatal("WorkItem mutation escaped rollback")
		}
	})
	t.Run("WorkItem details", func(t *testing.T) {
		f := openFixture(t)
		item := f.workItem(t)
		revisions := count(t, f.db, "SELECT COUNT(*) FROM revisions")
		failEvent(t, f.db, history.EventWorkItemDetailsChanged)
		if _, err := f.workItems.UpdateLocalWorkItemDetails(f.ctx, workitem.UpdateLocalWorkItemDetailsInput{ID: item.ID, Title: "rollback"}); err == nil {
			t.Fatal("expected failure")
		}
		loaded, _ := f.workItems.GetByID(f.ctx, item.ID)
		if loaded.Title != item.Title || count(t, f.db, "SELECT COUNT(*) FROM revisions") != revisions {
			t.Fatal("WorkItem details mutation escaped rollback")
		}
	})
	t.Run("WorkItem lifecycle", func(t *testing.T) {
		f := openFixture(t)
		item := f.workItem(t)
		revisions := count(t, f.db, "SELECT COUNT(*) FROM revisions")
		failEvent(t, f.db, history.EventWorkItemLifecycleChanged)
		if _, err := f.workItems.SetLocalWorkItemLifecycleState(f.ctx, workitem.SetLocalWorkItemLifecycleStateInput{ID: item.ID, Target: workitem.LifecycleCompleted}); err == nil {
			t.Fatal("expected failure")
		}
		loaded, _ := f.workItems.GetByID(f.ctx, item.ID)
		if loaded.Lifecycle != item.Lifecycle || count(t, f.db, "SELECT COUNT(*) FROM revisions") != revisions {
			t.Fatal("WorkItem lifecycle mutation escaped rollback")
		}
	})
	t.Run("Planner create", func(t *testing.T) {
		f := openFixture(t)
		failEvent(t, f.db, history.EventPlannerItemCreated)
		if _, err := planner.NewService(f.db, planner.NewRepository(f.db)).CreateLocalTodo(f.ctx, planner.CreateLocalTodoInput{Title: "rollback"}); err == nil {
			t.Fatal("expected failure")
		}
		if count(t, f.db, "SELECT COUNT(*) FROM planner_items") != 0 || count(t, f.db, "SELECT COUNT(*) FROM revisions") != 0 {
			t.Fatal("Planner mutation escaped rollback")
		}
	})
	t.Run("Planner completion", func(t *testing.T) {
		f := openFixture(t)
		s := planner.NewService(f.db, planner.NewRepository(f.db))
		item, err := s.CreateLocalTodo(f.ctx, planner.CreateLocalTodoInput{Title: "rollback completion"})
		if err != nil {
			t.Fatal(err)
		}
		revisions := count(t, f.db, "SELECT COUNT(*) FROM revisions")
		failEvent(t, f.db, history.EventPlannerItemCompleted)
		if _, err := s.CompleteLocalTodo(f.ctx, item.ID); err == nil {
			t.Fatal("expected failure")
		}
		loaded, _ := planner.NewRepository(f.db).GetByID(f.ctx, item.ID)
		if loaded.Status != planner.StatusOpen || count(t, f.db, "SELECT COUNT(*) FROM revisions") != revisions {
			t.Fatal("Planner completion escaped rollback")
		}
	})
	t.Run("Notification cancellation", func(t *testing.T) {
		f := openFixture(t)
		now := time.Now().UTC()
		item := f.workItem(t)
		point := f.monitor(t, item, tracking.MonitorDeadline, now.Add(-time.Hour))
		if _, err := f.attention.Reconcile(f.ctx, now); err != nil {
			t.Fatal(err)
		}
		views, _ := f.attention.ListNotificationViews(f.ctx)
		if _, err := f.tracking.SetMonitorPointState(f.ctx, tracking.SetMonitorPointStateInput{ID: point.ID, Target: tracking.MonitorSatisfied}); err != nil {
			t.Fatal(err)
		}
		if _, err := f.attention.Reconcile(f.ctx, now.Add(time.Minute)); err != nil {
			t.Fatal(err)
		}
		revisions := count(t, f.db, "SELECT COUNT(*) FROM revisions")
		failEvent(t, f.db, history.EventNotificationCancelled)
		if _, err := f.attention.DispatchPendingNotifications(f.ctx, now.Add(2*time.Minute)); err == nil {
			t.Fatal("expected failure")
		}
		assertScalar(t, f.db, "SELECT state FROM notifications WHERE entity_id = ?", views[0].Notification.ID, "PENDING")
		if count(t, f.db, "SELECT COUNT(*) FROM revisions") != revisions {
			t.Fatal("Notification cancellation Revision escaped rollback")
		}
	})
}

func TestSlice2HistorySurvivesDatabaseRestart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	opened, err := database.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	f := services(opened.DB)
	item := f.workItem(t)
	proposalService := proposal.NewService(opened.DB, f.workItems)
	p, err := proposalService.ProposeWorkItemUpdate(f.ctx, proposal.ProposeWorkItemUpdateInput{TargetWorkItemID: item.ID, ProposedTitle: "Persisted apply", Producer: proposal.ProducerAI})
	if err != nil {
		t.Fatal(err)
	}
	applied, err := proposalService.ApplyProposal(f.ctx, proposal.ApplyProposalInput{ID: p.ID})
	if err != nil || applied.Outcome != proposal.ApplyOutcomeApplied {
		t.Fatalf("apply: %+v %v", applied, err)
	}
	plannerService := planner.NewService(opened.DB, planner.NewRepository(opened.DB))
	planned, err := plannerService.CreateLocalTodo(f.ctx, planner.CreateLocalTodoInput{Title: "Persistent Planner history"})
	if err != nil {
		t.Fatal(err)
	}
	done, err := plannerService.CompleteLocalTodo(f.ctx, planned.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := opened.DB.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := database.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.DB.Close()
	repo := history.NewRepository(reopened.DB)
	trace, err := repo.ListByCorrelation(context.Background(), history.ProposalCorrelation(p.ID), 10)
	if err != nil || len(trace) != 3 {
		t.Fatalf("Proposal trace did not survive restart: %+v %v", trace, err)
	}
	byType := eventsByType(trace)
	if byType[history.EventProposalCreated].Actor != history.ActorAI || byType[history.EventWorkItemDetailsChanged].CausationEventID == nil || *byType[history.EventWorkItemDetailsChanged].CausationEventID != byType[history.EventProposalApplied].ID {
		t.Fatalf("Proposal provenance did not survive restart: %+v", trace)
	}
	created, err := repo.FindLatestBySubjectAndType(context.Background(), planned.ID, history.EventPlannerItemCreated)
	if err != nil || created == nil || created.RevisionID == nil || *created.RevisionID != planned.LastRevisionID {
		t.Fatalf("Planner creation history did not survive restart: %+v %v", created, err)
	}
	completed, err := repo.FindLatestBySubjectAndType(context.Background(), planned.ID, history.EventPlannerItemCompleted)
	if err != nil || completed == nil || completed.RevisionID == nil || *completed.RevisionID != done.LastRevisionID {
		t.Fatalf("Planner completion history did not survive restart: %+v %v", completed, err)
	}
}

func eventsByType(events []history.Event) map[history.EventType]history.Event {
	result := make(map[history.EventType]history.Event, len(events))
	for _, event := range events {
		result[event.Type] = event
	}
	return result
}

func latestEvent(t *testing.T, f *fixture, subjectID string, eventType history.EventType) history.Event {
	t.Helper()
	event, err := history.NewRepository(f.db).FindLatestBySubjectAndType(context.Background(), subjectID, eventType)
	if err != nil || event == nil {
		t.Fatalf("find %s for %s: %+v %v", eventType, subjectID, event, err)
	}
	return *event
}
