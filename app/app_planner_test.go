package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/reminder"
)

func TestAppPlanner_CreateLocalTodo_WithReminderAndHistory(t *testing.T) {
	app := newRegistryTestApp(t)
	now := time.Now().UTC().Truncate(time.Second)
	dueAt := now.Add(2 * time.Hour).Format(time.RFC3339)
	title := "Review Architecture Specs"
	notes := "Check slice 3 and 4 boundaries"

	req := CreateLocalTodoRequestDTO{
		Title:          title,
		Details:        &notes,
		DueAt:          &dueAt,
		OffsetsSeconds: []int{0, 900},
	}

	dto, err := app.CreateLocalTodo(req)
	if err != nil {
		t.Fatalf("CreateLocalTodo failed: %v", err)
	}

	if dto.ID == "" {
		t.Fatal("expected non-empty ID")
	}
	if dto.Title != title {
		t.Errorf("expected Title %q, got %q", title, dto.Title)
	}
	if dto.Reminder == nil {
		t.Fatal("expected non-nil Reminder")
	}
	if dto.Reminder.State != string(reminder.StateActive) {
		t.Errorf("expected reminder state ACTIVE, got %q", dto.Reminder.State)
	}
	if len(dto.Reminder.OffsetsSeconds) != 2 || dto.Reminder.OffsetsSeconds[0] != 0 || dto.Reminder.OffsetsSeconds[1] != 900 {
		t.Errorf("expected offsets [0, 900], got %+v", dto.Reminder.OffsetsSeconds)
	}

	// Verify history was recorded and returned in RecentChanges
	if len(dto.RecentChanges) == 0 {
		t.Fatal("expected RecentChanges to contain creation events")
	}
	foundCreate := false
	foundReminder := false
	for _, ch := range dto.RecentChanges {
		if ch.EventType == string(history.EventPlannerItemCreated) {
			foundCreate = true
		}
		if ch.EventType == string(history.EventPlannerItemReminderSet) {
			foundReminder = true
		}
	}
	if !foundCreate {
		t.Errorf("expected %s in RecentChanges", history.EventPlannerItemCreated)
	}
	if !foundReminder {
		t.Errorf("expected %s in RecentChanges", history.EventPlannerItemReminderSet)
	}

	// Query GetPlannerItemHistory directly
	hist, err := app.GetPlannerItemHistory(dto.ID)
	if err != nil {
		t.Fatalf("GetPlannerItemHistory failed: %v", err)
	}
	if len(hist) < 2 {
		t.Errorf("expected at least 2 history records, got %d", len(hist))
	}
}

func TestAppPlanner_SetAndRemoveReminder(t *testing.T) {
	app := newRegistryTestApp(t)
	now := time.Now().UTC().Truncate(time.Second)
	dueAt := now.Add(1 * time.Hour).Format(time.RFC3339)

	// Create plain Todo without reminder
	todo, err := app.CreateLocalTodo(CreateLocalTodoRequestDTO{
		Title: "Write acceptance test",
		DueAt: &dueAt,
	})
	if err != nil {
		t.Fatalf("CreateLocalTodo failed: %v", err)
	}
	if todo.Reminder != nil {
		t.Fatal("expected nil Reminder initially")
	}

	// Set reminder via app API
	remDTO, err := app.SetTodoReminder(SetTodoReminderRequestDTO{
		PlannerItemID:  todo.ID,
		OffsetsSeconds: []int{1800},
	})
	if err != nil {
		t.Fatalf("SetTodoReminder failed: %v", err)
	}
	if remDTO.State != string(reminder.StateActive) {
		t.Errorf("expected ACTIVE, got %q", remDTO.State)
	}
	if len(remDTO.OffsetsSeconds) != 1 || remDTO.OffsetsSeconds[0] != 1800 {
		t.Errorf("expected offset [1800], got %+v", remDTO.OffsetsSeconds)
	}

	// Verify enriched item has reminder in PlannerView
	view, err := app.GetPlannerView(PlannerViewRequestDTO{
		RangeStart: now.Add(-24 * time.Hour).Format("2006-01-02"),
		RangeEnd:   now.Add(48 * time.Hour).Format("2006-01-02"),
		Today:      now.Format("2006-01-02"),
		Timezone:   "UTC",
	})
	if err != nil {
		t.Fatalf("GetPlannerView failed: %v", err)
	}
	var foundItem *PlannerItemDTO
	for i := range view.CurrentOpenItems {
		if view.CurrentOpenItems[i].ID == todo.ID {
			foundItem = &view.CurrentOpenItems[i]
			break
		}
	}
	if foundItem == nil {
		for i := range view.CalendarItems {
			if view.CalendarItems[i].ID == todo.ID {
				foundItem = &view.CalendarItems[i]
				break
			}
		}
	}
	if foundItem == nil || foundItem.Reminder == nil {
		t.Fatalf("expected foundItem with Reminder in PlannerView")
	}

	// Remove reminder via app API
	if err := app.RemoveTodoReminder(todo.ID); err != nil {
		t.Fatalf("RemoveTodoReminder failed: %v", err)
	}

	// Verify reminder is now removed
	viewAfter, err := app.GetPlannerView(PlannerViewRequestDTO{
		RangeStart: now.Add(-24 * time.Hour).Format("2006-01-02"),
		RangeEnd:   now.Add(48 * time.Hour).Format("2006-01-02"),
		Today:      now.Format("2006-01-02"),
		Timezone:   "UTC",
	})
	if err != nil {
		t.Fatalf("GetPlannerView failed: %v", err)
	}
	for _, item := range viewAfter.CurrentOpenItems {
		if item.ID == todo.ID {
			if item.Reminder != nil {
				t.Fatalf("expected nil Reminder after removal, got %+v", item.Reminder)
			}
		}
	}
}

func TestAppPlanner_CompleteLocalTodo_SatisfiesReminderAndResolvesAttention(t *testing.T) {
	app := newRegistryTestApp(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)
	dueAt := now.Add(1 * time.Hour).Format(time.RFC3339)

	// Create Todo with reminder
	todo, err := app.CreateLocalTodo(CreateLocalTodoRequestDTO{
		Title:          "Finish release notes",
		DueAt:          &dueAt,
		OffsetsSeconds: []int{0},
	})
	if err != nil {
		t.Fatalf("CreateLocalTodo failed: %v", err)
	}

	// Simulate an AttentionIssue that was opened for this Todo
	issue, _, err := app.attentionService.OpenTodoReminderAttention(ctx, attention.OpenTodoReminderAttentionInput{
		PlannerItemID: todo.ID,
		Title:         todo.Title,
		OffsetSeconds: 0,
		DueAt:         now.Add(1 * time.Hour),
	}, now)
	if err != nil {
		t.Fatalf("OpenTodoReminderAttention failed: %v", err)
	}
	if issue.State != attention.IssueOpen {
		t.Fatalf("expected OPEN issue, got %q", issue.State)
	}

	// Complete the Todo via CompleteLocalTodo
	completed, err := app.CompleteLocalTodo(todo.ID)
	if err != nil {
		t.Fatalf("CompleteLocalTodo failed: %v", err)
	}
	if completed.Status != "COMPLETED" {
		t.Fatalf("expected Status COMPLETED, got %q", completed.Status)
	}
	if completed.Reminder == nil || completed.Reminder.State != string(reminder.StateSatisfied) {
		t.Fatalf("expected Reminder state SATISFIED, got %+v", completed.Reminder)
	}

	// Verify AttentionIssue is now RESOLVED
	reloadedIssue, err := app.attentionService.GetAttentionIssue(ctx, issue.ID)
	if err != nil {
		t.Fatalf("GetAttentionIssue failed: %v", err)
	}
	if reloadedIssue.State != attention.IssueResolved {
		t.Fatalf("expected issue to be RESOLVED, got %q", reloadedIssue.State)
	}

	// Verify RecentChanges contains item completed event and attention resolved event
	hist, err := app.GetPlannerItemHistory(todo.ID)
	if err != nil {
		t.Fatalf("GetPlannerItemHistory failed: %v", err)
	}
	hasCompleted := false
	hasAttentionResolved := false
	for _, h := range hist {
		if h.EventType == string(history.EventPlannerItemCompleted) {
			hasCompleted = true
		}
		if h.EventType == string(history.EventAttentionResolved) {
			hasAttentionResolved = true
		}
	}
	if !hasCompleted {
		t.Errorf("expected %s in history", history.EventPlannerItemCompleted)
	}
	if !hasAttentionResolved {
		t.Errorf("expected %s in history", history.EventAttentionResolved)
	}
}

func TestAppPlanner_Proposal_PlannerItemCreate_OneHourReminderIsCanonicalIntent(t *testing.T) {
	app := newRegistryTestApp(t)
	registry := NewProposeRegistry()
	ctx := context.Background()

	// Natural intent: "Create a todo to submit the release report tomorrow at
	// 3 PM and remind me one hour before." Prompt date resolution supplies the
	// concrete local date; the capability carries the Todo and reminder intent.
	p, err := registry.Execute(ctx, app, proposal.OperationTypePlannerItemCreate, nil, json.RawMessage(`{
		"title": "Submit the release report",
		"due_date": "2026-09-30",
		"due_time": "15:00",
		"timezone": "UTC",
		"remind": true,
		"reminder_offset_minutes": 60
	}`), nil)
	if err != nil {
		t.Fatalf("registry.Execute failed: %v", err)
	}

	if p.PlannerItemPayload == nil {
		t.Fatal("expected non-nil PlannerItemPayload")
	}
	if len(p.PlannerItemPayload.OffsetsSeconds) != 1 || p.PlannerItemPayload.OffsetsSeconds[0] != 3600 {
		t.Fatalf("expected one-hour OffsetsSeconds [3600], got %+v", p.PlannerItemPayload.OffsetsSeconds)
	}

	// Apply proposal
	applied, err := app.ApplyProposal(ApplyProposalRequestDTO{ID: p.ID})
	if err != nil {
		t.Fatalf("ApplyProposal failed: %v", err)
	}
	if applied.Outcome != "APPLIED" {
		t.Fatalf("expected APPLIED outcome, got %q", applied.Outcome)
	}

	// Verify Todo and Reminder were created in the database via GetPlannerView
	view, err := app.GetPlannerView(PlannerViewRequestDTO{
		RangeStart: "2026-09-01",
		RangeEnd:   "2026-10-15",
		Today:      "2026-09-25",
		Timezone:   "UTC",
	})
	if err != nil {
		t.Fatalf("GetPlannerView failed: %v", err)
	}
	var createdItem *PlannerItemDTO
	for i := range view.CalendarItems {
		if view.CalendarItems[i].Title == "Submit the release report" {
			createdItem = &view.CalendarItems[i]
			break
		}
	}
	if createdItem == nil {
		for i := range view.CurrentOpenItems {
			if view.CurrentOpenItems[i].Title == "Submit the release report" {
				createdItem = &view.CurrentOpenItems[i]
				break
			}
		}
	}
	if createdItem == nil {
		t.Fatal("expected to find created PlannerItem in PlannerView")
	}

	rem, err := app.reminderService.GetByPlannerItemID(ctx, createdItem.ID)
	if err != nil {
		t.Fatalf("GetByPlannerItemID failed: %v", err)
	}
	if rem == nil {
		t.Fatal("expected active reminder for created item")
	}
	if rem.State != reminder.StateActive {
		t.Errorf("expected reminder state ACTIVE, got %q", rem.State)
	}
	if len(rem.OffsetsSeconds) != 1 || rem.OffsetsSeconds[0] != 3600 {
		t.Errorf("expected [3600], got %+v", rem.OffsetsSeconds)
	}

	// Apply creates canonical Todo + reminder truth only. Attention and
	// Notification remain absent until the background evaluator reaches the
	// configured threshold; no direct Notification mutation is involved.
	center, err := app.GetNotificationCenter()
	if err != nil {
		t.Fatalf("GetNotificationCenter failed: %v", err)
	}
	if len(center.Items) != 0 {
		t.Fatalf("expected no directly-created notifications, got %+v", center.Items)
	}
}
