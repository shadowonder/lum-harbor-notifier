package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/plannersync"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/source"
)

const localNoteTestInstance = "apple-reminders-main"

// ingestAppleReminder records one Apple Reminders observation exactly as the
// real lh-plugin-apple-reminders source.fetch result is ingested.
func ingestAppleReminder(t *testing.T, f *round2TestFixture, externalID string, payload map[string]any) {
	t.Helper()
	ctx := context.Background()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := f.sourceService.PrepareFetch(ctx, plannersync.ReminderExtensionID, localNoteTestInstance, "source.fetch")
	if err != nil {
		t.Fatalf("PrepareFetch: %v", err)
	}
	if _, err := f.sourceService.IngestFetchResult(ctx, source.FetchBatch{
		ExtensionID: plannersync.ReminderExtensionID, InstanceID: localNoteTestInstance, CapabilityID: "source.fetch", Checkpoint: checkpoint,
		Records: []source.Observation{{
			ExternalID: externalID, SourceKind: plannersync.SourceKindReminder, ObservedAt: time.Now().UTC(),
			Payload: json.RawMessage(raw),
		}},
	}); err != nil {
		t.Fatalf("IngestFetchResult: %v", err)
	}
}

func reconcileAppleReminders(t *testing.T, f *round2TestFixture) plannersync.Result {
	t.Helper()
	reconciler, err := plannersync.NewReconciler(f.sourceService, f.plannerService, plannersync.ReminderExtensionID)
	if err != nil {
		t.Fatal(err)
	}
	result, err := reconciler.Reconcile(context.Background())
	if err != nil || !result.FullySuccessful() {
		t.Fatalf("Reconcile: %+v err=%v", result, err)
	}
	return result
}

func dateOnlyReminder(title, notes string, year, month, day int) map[string]any {
	return map[string]any{
		"title": title, "notes": notes, "completed": false,
		"dueKind": "DATE_ONLY", "dueYear": year, "dueMonth": month, "dueDay": day,
	}
}

type plannerRow struct {
	id, title, authority string
	details, localNote   sql.NullString
	dueAt, dueDate       sql.NullString
	lastRevisionID       string
}

func onlyPlannerRow(t *testing.T, db *sql.DB) plannerRow {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM planner_items`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("expected exactly one Planner item (no duplicate), got %d err=%v", count, err)
	}
	var row plannerRow
	if err := db.QueryRow(`SELECT entity_id, title, authority_type, details, local_note, due_at, due_date, last_revision_id FROM planner_items`).
		Scan(&row.id, &row.title, &row.authority, &row.details, &row.localNote, &row.dueAt, &row.dueDate, &row.lastRevisionID); err != nil {
		t.Fatal(err)
	}
	return row
}

func setLocalNoteAssistantReply(f *round2TestFixture, args map[string]string) {
	raw, _ := json.Marshal(args)
	reply, _ := json.Marshal(map[string]any{
		"version": 1, "kind": "MUTATION_CANDIDATE",
		"text": "I will propose adding a Lum Harbor note to the existing reminder.",
		"candidate": map[string]any{
			"version": 1, "capability_id": proposal.OperationTypePlannerItemSetLocalNote,
			"arguments": string(raw), "reason": "User asked to add context to an existing reminder",
		},
	})
	f.mockAI.mu.Lock()
	f.mockAI.assistantAsk = string(reply)
	f.mockAI.mu.Unlock()
}

// TestExternalPlannerLocalNote_AssistantApplyAndResyncPreservesOwnership is the
// ownership proof: an Apple Reminder synced to an EXTERNAL Planner item gets a
// Lum Harbor note through Assistant -> Proposal -> human Apply on the same
// entity (no duplicate Todo, external notes untouched); a later Apple
// Reminders change to title, notes and due updates the external fields of
// that same entity and leaves the Lum Harbor note unchanged.
func TestExternalPlannerLocalNote_AssistantApplyAndResyncPreservesOwnership(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()

	// 1. Apple Reminder sync creates the EXTERNAL Planner item.
	ingestAppleReminder(t, f, "EK-1", dateOnlyReminder("New reminder test 1", "Apple notes v1", 2026, 10, 20))
	if res := reconcileAppleReminders(t, f); res.Created != 1 {
		t.Fatalf("expected one created item, got %+v", res)
	}
	created := onlyPlannerRow(t, f.db)
	if created.authority != "EXTERNAL" || created.localNote.Valid {
		t.Fatalf("unexpected synced item: %+v", created)
	}

	// 2. The user asks the Assistant to add context; the Assistant targets the
	// existing item by title and proposes one typed local-note update.
	const note = "This is a test reminder synced from Apple Reminders."
	setLocalNoteAssistantReply(f, map[string]string{"planner_item_title": "new reminder test 1", "local_note": note})
	reply, err := f.app.AskAssistant(AskAssistantRequestDTO{
		Message: "Add a note to 'New reminder test 1' saying this is a test reminder synced from Apple Reminders.",
		Surface: "PLANNER",
	})
	if err != nil {
		t.Fatalf("AskAssistant: %v", err)
	}
	if reply.ProposalOutcome != AssistantProposalCreated || reply.ProposalID == "" {
		t.Fatalf("expected a created Proposal, got %+v", reply)
	}
	stored, err := f.proposalService.GetByID(ctx, reply.ProposalID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.OperationType != proposal.OperationTypePlannerItemSetLocalNote || stored.TargetEntityID == nil || *stored.TargetEntityID != created.id {
		t.Fatalf("Proposal must target the existing item: %+v", stored)
	}
	if got := onlyPlannerRow(t, f.db); got.localNote.Valid || got.lastRevisionID != created.lastRevisionID {
		t.Fatalf("a pending Proposal must not mutate Planner truth: %+v", got)
	}

	// Human review shows exactly the owned change.
	pending, err := f.app.GetPendingProposals()
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending proposals: %+v err=%v", pending, err)
	}
	var reviewed bool
	for _, change := range pending[0].Changes {
		if change.Label == "Lum Harbor note" && change.Before == nil && change.After != nil && *change.After == note {
			reviewed = true
		}
		if change.Label == "Title" || change.Label == "Notes" || change.Label == "Due" {
			t.Fatalf("review must not show external-authoritative fields as changing: %+v", change)
		}
	}
	if !reviewed {
		t.Fatalf("review is missing the local note change: %+v", pending[0].Changes)
	}

	// 3. Human Apply updates only the local note of the same entity.
	applied, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: reply.ProposalID})
	if err != nil || applied.Outcome != "APPLIED" {
		t.Fatalf("ApplyProposal: %+v err=%v", applied, err)
	}
	annotated := onlyPlannerRow(t, f.db)
	if annotated.id != created.id || annotated.localNote.String != note ||
		annotated.title != "New reminder test 1" || annotated.details.String != "Apple notes v1" || annotated.dueDate.String != "2026-10-20" {
		t.Fatalf("Apply must change only the local note: %+v", annotated)
	}
	var origin string
	if err := f.db.QueryRow(`SELECT origin_type FROM revisions WHERE id = ?`, annotated.lastRevisionID).Scan(&origin); err != nil || origin != "USER" {
		t.Fatalf("local note revision origin: %q err=%v", origin, err)
	}
	var noteEvents int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM history_events WHERE event_type = 'PLANNER_ITEM_LOCAL_NOTE_CHANGED' AND subject_entity_id = ? AND revision_id = ?`,
		created.id, annotated.lastRevisionID).Scan(&noteEvents)
	if noteEvents != 1 {
		t.Fatalf("expected one PLANNER_ITEM_LOCAL_NOTE_CHANGED event, got %d", noteEvents)
	}
	// Exact Source lineage: the applied Revision links to the Assistant turn's
	// Source revision, never to the Apple Reminders Source.
	var linkedExtension string
	if err := f.db.QueryRow(`
SELECT sr.extension_id FROM revision_sources rs
JOIN source_revisions sv ON sv.id = rs.source_revision_id
JOIN source_records sr ON sr.id = sv.source_record_id
WHERE rs.revision_id = ?`, annotated.lastRevisionID).Scan(&linkedExtension); err != nil || linkedExtension != "core.assistant" {
		t.Fatalf("applied Revision must link to the Assistant Source, got %q err=%v", linkedExtension, err)
	}

	// 4. Apple Reminders later changes title, notes and due (date -> timed).
	ingestAppleReminder(t, f, "EK-1", map[string]any{
		"title": "New reminder test 1 (renamed)", "notes": "Apple notes v2", "completed": false,
		"dueKind": "TIMED", "dueYear": 2026, "dueMonth": 10, "dueDay": 22, "dueHour": 9, "dueMinute": 30, "dueSecond": 0,
		"timeZone": "UTC",
	})
	if res := reconcileAppleReminders(t, f); res.Updated != 1 || res.Created != 0 {
		t.Fatalf("expected one in-place update, got %+v", res)
	}

	// 5-7. External fields follow Apple Reminders; the local note survives on
	// the same entity; no duplicate item or external identity appears.
	resynced := onlyPlannerRow(t, f.db)
	if resynced.id != created.id {
		t.Fatalf("re-sync must keep the same entity: %s != %s", resynced.id, created.id)
	}
	dueAt, _ := time.Parse(time.RFC3339Nano, resynced.dueAt.String)
	if resynced.title != "New reminder test 1 (renamed)" || resynced.details.String != "Apple notes v2" ||
		resynced.dueDate.Valid || !dueAt.Equal(time.Date(2026, 10, 22, 9, 30, 0, 0, time.UTC)) {
		t.Fatalf("external-authoritative fields must follow Apple Reminders: %+v", resynced)
	}
	if resynced.localNote.String != note {
		t.Fatalf("local note must survive external reconciliation, got %+v", resynced.localNote)
	}
	var refs int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM external_refs WHERE entity_id = ?`, created.id).Scan(&refs)
	if refs != 1 {
		t.Fatalf("expected one external identity, got %d", refs)
	}
	var syncAfter sql.NullString
	_ = f.db.QueryRow(`SELECT after_json FROM history_events WHERE event_type = 'PLANNER_ITEM_EXTERNAL_UPDATED' AND subject_entity_id = ?`, created.id).Scan(&syncAfter)
	if strings.Contains(syncAfter.String, "local_note") {
		t.Fatalf("external sync History must not claim the local note: %s", syncAfter.String)
	}

	// The Planner projection carries the note and backend-derived provenance.
	item, err := f.plannerService.GetByID(ctx, created.id)
	if err != nil {
		t.Fatal(err)
	}
	dto := plannerItemToDTO(*item)
	f.app.enrichPlannerItem(ctx, &dto)
	if dto.LocalNote == nil || *dto.LocalNote != note || dto.Details == nil || *dto.Details != "Apple notes v2" {
		t.Fatalf("DTO must expose external notes and local note separately: %+v", dto)
	}
	if dto.ExternalOrigin == nil || dto.ExternalOrigin.SystemLabel != "Apple Reminders" ||
		dto.ExternalOrigin.ExtensionID == nil || *dto.ExternalOrigin.ExtensionID != plannersync.ReminderExtensionID ||
		dto.ExternalOrigin.InstanceID == nil || *dto.ExternalOrigin.InstanceID != localNoteTestInstance {
		t.Fatalf("unexpected external origin: %+v", dto.ExternalOrigin)
	}
}

// TestExternalPlannerLocalNote_StaleGuardIsTheNoteNotExternalSync proves the
// Apply precondition is scoped to the owned field: an external sync between
// Proposal and Apply does not stale it, while a competing note change does.
func TestExternalPlannerLocalNote_StaleGuardIsTheNoteNotExternalSync(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()
	ingestAppleReminder(t, f, "EK-2", dateOnlyReminder("Pay rent", "Apple notes", 2026, 11, 1))
	reconcileAppleReminders(t, f)
	itemID := onlyPlannerRow(t, f.db).id

	propose := func(note string) string {
		t.Helper()
		args, _ := json.Marshal(map[string]string{"planner_item_id": itemID, "local_note": note, "mode": "replace"})
		p, err := f.app.proposeRegistry.Execute(ctx, f.app, proposal.OperationTypePlannerItemSetLocalNote, nil, args, nil)
		if err != nil {
			t.Fatalf("propose %q: %v", note, err)
		}
		return p.ID
	}
	first, second := propose("Landlord prefers e-transfer"), propose("Ask about lease renewal")

	ingestAppleReminder(t, f, "EK-2", dateOnlyReminder("Pay rent (Nov)", "Apple notes", 2026, 11, 2))
	if res := reconcileAppleReminders(t, f); res.Updated != 1 {
		t.Fatalf("expected external update, got %+v", res)
	}

	if res, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: first}); err != nil || res.Outcome != "APPLIED" {
		t.Fatalf("external sync must not stale a local note Proposal: %+v err=%v", res, err)
	}
	if res, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: second}); err != nil || res.Outcome != "STALE" {
		t.Fatalf("a competing local note change must stale the second Proposal: %+v err=%v", res, err)
	}
	if row := onlyPlannerRow(t, f.db); row.localNote.String != "Landlord prefers e-transfer" || row.title != "Pay rent (Nov)" {
		t.Fatalf("unexpected final state: %+v", row)
	}
}

// TestExternalPlannerLocalNote_CapabilityBoundaries covers the narrow scope:
// LOCAL Todos are refused, append keeps the prior note, and an unknown title
// is reported rather than guessed (never a new Todo).
func TestExternalPlannerLocalNote_CapabilityBoundaries(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()
	execute := func(args map[string]string) (*proposal.Proposal, error) {
		raw, _ := json.Marshal(args)
		return f.app.proposeRegistry.Execute(ctx, f.app, proposal.OperationTypePlannerItemSetLocalNote, nil, raw, nil)
	}

	local, err := f.app.CreateLocalTodo(CreateLocalTodoRequestDTO{Title: "Local errand"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := execute(map[string]string{"planner_item_id": local.ID, "local_note": "x"}); err == nil {
		t.Fatal("a LOCAL Todo must be refused")
	}
	if _, err := execute(map[string]string{"planner_item_title": "No such reminder", "local_note": "x"}); err == nil {
		t.Fatal("an unknown title must be reported, not guessed")
	}

	ingestAppleReminder(t, f, "EK-3", dateOnlyReminder("Call dentist", "", 2026, 12, 1))
	reconcileAppleReminders(t, f)
	first, err := execute(map[string]string{"planner_item_title": "Call dentist", "local_note": "Office opens at 9"})
	if err != nil {
		t.Fatal(err)
	}
	if res, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: first.ID}); err != nil || res.Outcome != "APPLIED" {
		t.Fatalf("apply first: %+v err=%v", res, err)
	}
	appended, err := execute(map[string]string{"planner_item_title": "Call dentist", "local_note": "Bring insurance card"})
	if err != nil {
		t.Fatal(err)
	}
	want := "Office opens at 9\n\nBring insurance card"
	if appended.PlannerLocalNotePayload == nil || appended.PlannerLocalNotePayload.LocalNote == nil || *appended.PlannerLocalNotePayload.LocalNote != want {
		t.Fatalf("append must keep the prior note: %+v", appended.PlannerLocalNotePayload)
	}
	var count int
	_ = f.db.QueryRow(`SELECT COUNT(*) FROM planner_items`).Scan(&count)
	if count != 2 {
		t.Fatalf("no Planner item may be created by this capability, got %d items", count)
	}
}
