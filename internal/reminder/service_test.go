package reminder

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	dbgen "lum-harbor-notifier/internal/database/generated"
	"lum-harbor-notifier/internal/foundation"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/identity"
	"lum-harbor-notifier/internal/testsupport"
)

type fixture struct {
	db      *sql.DB
	service *Service
	history *history.Repository
	ctx     context.Context
	now     time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	db := testsupport.OpenTestDatabase(t)

	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	svc := NewService(db.DB)
	svc.SetClock(func() time.Time { return now })

	return &fixture{
		db:      db.DB,
		service: svc,
		history: history.NewRepository(db.DB),
		ctx:     context.Background(),
		now:     now,
	}
}

func (f *fixture) createPlannerItem(t *testing.T, title string) string {
	t.Helper()
	id, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	revID, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}

	summary := "Create planner item test"
	found := foundation.NewRepository(f.db)
	if _, err := found.CreateRevision(f.ctx, revID, foundation.RevisionOriginUser, &summary, f.now); err != nil {
		t.Fatal(err)
	}
	if err := found.CreateEntity(f.ctx, id, "PLANNER_ITEM", f.now); err != nil {
		t.Fatal(err)
	}

	queries := dbgen.New(f.db)
	due := "2026-09-26T18:00:00Z"
	missingContext := "[]"
	if err := queries.CreatePlannerItem(f.ctx, dbgen.CreatePlannerItemParams{
		EntityID:           id,
		AuthorityType:      "LOCAL",
		ItemType:           "TODO",
		Title:              title,
		Status:             "OPEN",
		DueAt:              &due,
		ContextStatus:      "COMPLETE",
		MissingContextJson: &missingContext,
		LastRevisionID:     revID,
		CreatedAt:          f.now.Format(time.RFC3339Nano),
		UpdatedAt:          f.now.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSetReminderCreatesActiveRowAndHistory(t *testing.T) {
	f := newFixture(t)
	pID := f.createPlannerItem(t, "Buy milk")

	rem, err := f.service.SetReminder(f.ctx, SetReminderInput{
		PlannerItemID:  pID,
		OffsetsSeconds: []int{86400, 172800},
		RemindMode:     RemindModeBeforeDue,
	})
	if err != nil {
		t.Fatalf("set reminder: %v", err)
	}

	if rem.PlannerItemID != pID || rem.State != StateActive {
		t.Fatalf("unexpected reminder: %+v", rem)
	}
	if len(rem.OffsetsSeconds) != 2 || rem.OffsetsSeconds[0] != 86400 {
		t.Fatalf("unexpected offsets: %+v", rem.OffsetsSeconds)
	}

	// Verify history event was appended with PlannerCorrelation
	events, err := f.history.ListByCorrelation(f.ctx, history.PlannerCorrelation(pID), 10)
	if err != nil {
		t.Fatalf("list history: %v", err)
	}
	if len(events) != 1 || events[0].Type != history.EventPlannerItemReminderSet {
		t.Fatalf("expected 1 EventPlannerItemReminderSet event, got: %+v", events)
	}
}

func TestSetReminderUpdatesExistingReminder(t *testing.T) {
	f := newFixture(t)
	pID := f.createPlannerItem(t, "Write report")

	_, err := f.service.SetReminder(f.ctx, SetReminderInput{
		PlannerItemID:  pID,
		OffsetsSeconds: []int{86400},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Update with new offsets
	updated, err := f.service.SetReminder(f.ctx, SetReminderInput{
		PlannerItemID:  pID,
		OffsetsSeconds: []int{3600, 7200},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.OffsetsSeconds) != 2 || updated.OffsetsSeconds[0] != 3600 {
		t.Fatalf("expected updated offsets, got %+v", updated.OffsetsSeconds)
	}

	// Should have 2 history events for this planner item
	events, err := f.history.ListByCorrelation(f.ctx, history.PlannerCorrelation(pID), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 reminder set events, got %d", len(events))
	}
}

func TestSatisfyReminderMarksSatisfied(t *testing.T) {
	f := newFixture(t)
	pID := f.createPlannerItem(t, "Finish homework")

	rem, err := f.service.SetReminder(f.ctx, SetReminderInput{
		PlannerItemID:  pID,
		OffsetsSeconds: []int{86400},
	})
	if err != nil {
		t.Fatal(err)
	}

	tx, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	revID, _ := identity.New()
	summary := "Complete todo"
	_, _ = foundation.NewRepository(tx).CreateRevision(f.ctx, revID, foundation.RevisionOriginUser, &summary, f.now)

	if err := f.service.SatisfyReminderTx(f.ctx, tx, pID, revID, f.now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	loaded, err := f.service.GetByPlannerItemID(f.ctx, pID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State != StateSatisfied || loaded.SatisfiedAt == nil {
		t.Fatalf("expected satisfied reminder, got: %+v", loaded)
	}
	_ = rem
}

func TestRemoveReminderCancelsAndRecordsHistory(t *testing.T) {
	f := newFixture(t)
	pID := f.createPlannerItem(t, "Cancelable task")

	_, err := f.service.SetReminder(f.ctx, SetReminderInput{
		PlannerItemID:  pID,
		OffsetsSeconds: []int{86400},
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := f.service.RemoveReminder(f.ctx, pID); err != nil {
		t.Fatal(err)
	}

	loaded, err := f.service.GetByPlannerItemID(f.ctx, pID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.State != StateCancelled {
		t.Fatalf("expected cancelled state, got: %s", loaded.State)
	}

	events, err := f.history.ListByCorrelation(f.ctx, history.PlannerCorrelation(pID), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[len(events)-1].Type != history.EventPlannerItemReminderRemoved {
		t.Fatalf("expected EventPlannerItemReminderRemoved as newest event, got: %+v", events)
	}
}

func TestSetReminderKeepsFiredStateOfThresholdsThatRemainConfigured(t *testing.T) {
	f := newFixture(t)
	pID := f.createPlannerItem(t, "Near-term action")

	rem, err := f.service.SetReminder(f.ctx, SetReminderInput{PlannerItemID: pID, OffsetsSeconds: []int{900}})
	if err != nil {
		t.Fatal(err)
	}

	// The scheduler cycle fires the 15m threshold.
	tx, err := f.db.BeginTx(f.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	revID, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := foundation.NewRepository(tx).CreateRevision(f.ctx, revID, foundation.RevisionOriginSystem, nil, f.now); err != nil {
		t.Fatal(err)
	}
	if err := f.service.UpdateFiredOffsetsTx(f.ctx, tx, rem.ID, revID, []int{900}, f.now); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	// Adding another threshold must not re-arm the already-fired one.
	added, err := f.service.SetReminder(f.ctx, SetReminderInput{PlannerItemID: pID, OffsetsSeconds: []int{900, 0}})
	if err != nil {
		t.Fatal(err)
	}
	if len(added.FiredOffsets) != 1 || added.FiredOffsets[0] != 900 {
		t.Fatalf("expected fired [900] preserved after adding a threshold, got %v", added.FiredOffsets)
	}
	stored, err := f.service.GetByPlannerItemID(f.ctx, pID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.FiredOffsets) != 1 || stored.FiredOffsets[0] != 900 {
		t.Fatalf("expected persisted fired [900], got %v", stored.FiredOffsets)
	}

	// Removing the fired threshold drops its fired entry.
	removed, err := f.service.SetReminder(f.ctx, SetReminderInput{PlannerItemID: pID, OffsetsSeconds: []int{0}})
	if err != nil {
		t.Fatal(err)
	}
	if len(removed.FiredOffsets) != 0 {
		t.Fatalf("expected no fired offsets once the fired threshold is removed, got %v", removed.FiredOffsets)
	}
}

func (f *fixture) createUndatedPlannerItem(t *testing.T, title string) string {
	t.Helper()
	id, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}
	revID, err := identity.New()
	if err != nil {
		t.Fatal(err)
	}

	summary := "Create undated planner item test"
	found := foundation.NewRepository(f.db)
	if _, err := found.CreateRevision(f.ctx, revID, foundation.RevisionOriginUser, &summary, f.now); err != nil {
		t.Fatal(err)
	}
	if err := found.CreateEntity(f.ctx, id, "PLANNER_ITEM", f.now); err != nil {
		t.Fatal(err)
	}

	queries := dbgen.New(f.db)
	missingContext := "[]"
	if err := queries.CreatePlannerItem(f.ctx, dbgen.CreatePlannerItemParams{
		EntityID:           id,
		AuthorityType:      "LOCAL",
		ItemType:           "TODO",
		Title:              title,
		Status:             "OPEN",
		ContextStatus:      "COMPLETE",
		MissingContextJson: &missingContext,
		LastRevisionID:     revID,
		CreatedAt:          f.now.Format(time.RFC3339Nano),
		UpdatedAt:          f.now.Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSetReminderRejectsUndatedTodo(t *testing.T) {
	f := newFixture(t)
	undatedID := f.createUndatedPlannerItem(t, "Undated Todo without due date")

	// Attempting to set an "at due time" or relative reminder on an undated todo must fail closed
	_, err := f.service.SetReminder(f.ctx, SetReminderInput{
		PlannerItemID:  undatedID,
		OffsetsSeconds: []int{0},
		RemindMode:     RemindModeBeforeDue,
	})
	if err == nil {
		t.Fatal("expected error setting due-relative reminder on undated todo, got nil")
	}
	if !errors.Is(err, ErrUndatedTodo) {
		t.Fatalf("expected errors.Is(err, ErrUndatedTodo), got: %v", err)
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected errors.Is(err, ErrInvalidInput), got: %v", err)
	}
	if !strings.Contains(err.Error(), "set a due date before adding a due-time reminder") {
		t.Fatalf("expected clear actionable error message, got: %v", err)
	}

	// Verify no reminder was persisted
	rem, err := f.service.GetByPlannerItemID(f.ctx, undatedID)
	if err == nil || !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected no reminder persisted, got: %+v, err: %v", rem, err)
	}
}
