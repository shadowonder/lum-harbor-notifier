package planner

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"lum-harbor-notifier/internal/testsupport"
)

func setLocalNote(t *testing.T, service *Service, db *sql.DB, input SetExternalPlannerItemLocalNoteInput) (*PlannerItem, error) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	item, err := service.SetExternalPlannerItemLocalNoteTx(context.Background(), tx, input)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return item, nil
}

func TestExternalSyncNeverOverwritesLocalNote(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))
	item, err := service.CreateExternalPlannerItem(ctx, newExternalCreateInput("APPLE_REMINDER:instance-1", "r-1"))
	if err != nil {
		t.Fatal(err)
	}

	annotated, err := setLocalNote(t, service, db.DB, SetExternalPlannerItemLocalNoteInput{ID: item.ID, LocalNote: ptr("  keep me  ")})
	if err != nil {
		t.Fatal(err)
	}
	if annotated.LocalNote == nil || *annotated.LocalNote != "keep me" || annotated.Title != item.Title || annotated.Details != nil {
		t.Fatalf("local note write must touch only local_note: %+v", annotated)
	}

	result, err := service.SyncExternalPlannerItem(ctx, SyncExternalPlannerItemInput{ID: item.ID, Native: ExternalPlannerItemNativeState{
		Title: "Buy oat milk", Details: ptr("from Apple"), Status: StatusOpen,
	}})
	if err != nil || !result.Changed {
		t.Fatalf("sync: %+v err=%v", result, err)
	}
	if result.Item.LocalNote == nil || *result.Item.LocalNote != "keep me" || result.Item.Title != "Buy oat milk" || *result.Item.Details != "from Apple" {
		t.Fatalf("sync must refresh external fields and keep local_note: %+v", result.Item)
	}
	reloaded, err := service.GetByID(ctx, item.ID)
	if err != nil || reloaded.LocalNote == nil || *reloaded.LocalNote != "keep me" {
		t.Fatalf("persisted local note lost: %+v err=%v", reloaded, err)
	}
}

func TestSetExternalPlannerItemLocalNoteGuards(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	service := NewService(db.DB, NewRepository(db.DB))

	local, err := service.CreateLocalTodo(ctx, CreateLocalTodoInput{Title: "Local"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setLocalNote(t, service, db.DB, SetExternalPlannerItemLocalNoteInput{ID: local.ID, LocalNote: ptr("x")}); !errors.Is(err, ErrNotExternalAuthority) {
		t.Fatalf("LOCAL item must be rejected, got %v", err)
	}

	external, err := service.CreateExternalPlannerItem(ctx, newExternalCreateInput("APPLE_REMINDER:instance-1", "r-2"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setLocalNote(t, service, db.DB, SetExternalPlannerItemLocalNoteInput{ID: external.ID, LocalNote: ptr("x"), ExpectedLocalNote: ptr("stale")}); !errors.Is(err, ErrLocalNoteBaseChanged) {
		t.Fatalf("mismatched base must be rejected, got %v", err)
	}
	if _, err := setLocalNote(t, service, db.DB, SetExternalPlannerItemLocalNoteInput{ID: external.ID, LocalNote: ptr(strings.Repeat("a", MaxLocalNoteLength+1))}); err == nil {
		t.Fatal("over-long note must be rejected")
	}
	var events int
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM history_events WHERE event_type = 'PLANNER_ITEM_LOCAL_NOTE_CHANGED'`).Scan(&events)
	if events != 0 {
		t.Fatalf("rejected writes must leave no History, got %d", events)
	}

	if _, err := setLocalNote(t, service, db.DB, SetExternalPlannerItemLocalNoteInput{ID: external.ID, LocalNote: ptr("note")}); err != nil {
		t.Fatal(err)
	}
	cleared, err := setLocalNote(t, service, db.DB, SetExternalPlannerItemLocalNoteInput{ID: external.ID, LocalNote: ptr("  "), ExpectedLocalNote: ptr("note")})
	if err != nil || cleared.LocalNote != nil {
		t.Fatalf("blank must clear the note: %+v err=%v", cleared, err)
	}
	_ = db.DB.QueryRow(`SELECT COUNT(*) FROM history_events WHERE event_type = 'PLANNER_ITEM_LOCAL_NOTE_CHANGED' AND subject_entity_id = ?`, external.ID).Scan(&events)
	if events != 2 {
		t.Fatalf("expected one History event per real change, got %d", events)
	}
}
