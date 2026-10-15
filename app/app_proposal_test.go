package app

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"lum-harbor-notifier/internal/database"
	"lum-harbor-notifier/internal/database/codec"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/testsupport"
	"lum-harbor-notifier/internal/workitem"
)

func newProposalApp(t *testing.T) (*App, *workitem.Service, *proposal.Service, *sql.DB) {
	t.Helper()
	database := testsupport.OpenTestDatabase(t)
	wiRepo := workitem.NewRepository(database.DB)
	wiService := workitem.NewService(database.DB, wiRepo)
	propService := proposal.NewService(database.DB, wiService)
	application := &App{
		workitemService: wiService,
		proposalService: propService,
	}
	return application, wiService, propService, database.DB
}

func createTestWorkItemForProposal(t *testing.T, wiService *workitem.Service, title string, desc *string) *workitem.WorkItem {
	t.Helper()
	item, err := wiService.CreateLocalWorkItem(context.Background(), workitem.CreateLocalWorkItemInput{
		Title:       title,
		Description: desc,
	})
	if err != nil {
		t.Fatalf("failed to create test work item: %v", err)
	}
	return item
}

// 1 & 2. TestAppGetPendingProposalsReturnsDTOsWithoutLeakingRawJSON
func TestAppGetPendingProposalsReturnsDTOsWithoutLeakingRawJSON(t *testing.T) {
	app, wiService, propService, _ := newProposalApp(t)
	ctx := context.Background()
	desc := "Authoritative Description"
	item := createTestWorkItemForProposal(t, wiService, "Authoritative Title", &desc)

	propTitle := "Proposed Evolution Title"
	propDesc := "Proposed Evolution Description"
	reason := "Align with product roadmap"
	createdProp, err := propService.ProposeWorkItemUpdate(ctx, proposal.ProposeWorkItemUpdateInput{
		TargetWorkItemID:    item.ID,
		ProposedTitle:       propTitle,
		ProposedDescription: &propDesc,
		Reason:              &reason,
	})
	if err != nil {
		t.Fatalf("propose update: %v", err)
	}

	dtos, err := app.GetPendingProposals()
	if err != nil {
		t.Fatalf("GetPendingProposals: %v", err)
	}
	if len(dtos) != 1 {
		t.Fatalf("expected 1 proposal DTO, got %d", len(dtos))
	}

	dto := dtos[0]
	if dto.ID != createdProp.ID {
		t.Fatalf("expected ID %s, got %s", createdProp.ID, dto.ID)
	}
	if dto.TargetWorkItemID == nil || *dto.TargetWorkItemID != item.ID {
		t.Fatalf("expected TargetWorkItemID %s, got %v", item.ID, dto.TargetWorkItemID)
	}
	if dto.Status != "PENDING" {
		t.Fatalf("expected status PENDING, got %s", dto.Status)
	}
	// Truth invariant: baseTitle and baseDescription represent the creation snapshot
	if dto.BaseTitle != "Authoritative Title" {
		t.Fatalf("expected BaseTitle %q, got %q", "Authoritative Title", dto.BaseTitle)
	}
	if dto.BaseDescription == nil || *dto.BaseDescription != desc {
		t.Fatalf("expected BaseDescription %v, got %v", desc, dto.BaseDescription)
	}
	// Proposed values
	if dto.ProposedTitle != propTitle {
		t.Fatalf("expected ProposedTitle %q, got %q", propTitle, dto.ProposedTitle)
	}
	if dto.ProposedDescription == nil || *dto.ProposedDescription != propDesc {
		t.Fatalf("expected ProposedDescription %v, got %v", propDesc, dto.ProposedDescription)
	}
	if dto.Reason == nil || *dto.Reason != reason {
		t.Fatalf("expected Reason %v, got %v", reason, dto.Reason)
	}
	if dto.CreatedAt == "" || dto.UpdatedAt == "" {
		t.Fatal("expected non-empty timestamps")
	}
	if dto.ResolvedAt != nil {
		t.Fatalf("expected nil ResolvedAt for PENDING, got %v", dto.ResolvedAt)
	}
}

// 3. TestAppProposeWorkItemUpdateCreatesProposalWithoutMutatingWorkItem
func TestAppProposeWorkItemUpdateCreatesProposalWithoutMutatingWorkItem(t *testing.T) {
	app, wiService, _, _ := newProposalApp(t)
	ctx := context.Background()
	desc := "Original Description"
	item := createTestWorkItemForProposal(t, wiService, "Original Title", &desc)

	proposedTitle := "New Suggested Title"
	proposedDesc := "New Suggested Description"
	reason := "Design feedback"
	dto, err := app.ProposeWorkItemUpdate(ProposeWorkItemUpdateRequestDTO{
		TargetWorkItemID:    item.ID,
		ProposedTitle:       proposedTitle,
		ProposedDescription: &proposedDesc,
		Reason:              &reason,
	})
	if err != nil {
		t.Fatalf("ProposeWorkItemUpdate: %v", err)
	}

	if dto.ID == "" || dto.Status != "PENDING" {
		t.Fatalf("unexpected created proposal DTO: %+v", dto)
	}
	if dto.ProposedTitle != proposedTitle || dto.ProposedDescription == nil || *dto.ProposedDescription != proposedDesc {
		t.Fatalf("mismatched proposed fields in DTO: %+v", dto)
	}

	// WorkItem truth MUST NOT be modified
	currentWI, err := wiService.GetByID(ctx, item.ID)
	if err != nil {
		t.Fatalf("load work item: %v", err)
	}
	if currentWI.Title != "Original Title" || currentWI.LastRevisionID != item.LastRevisionID {
		t.Fatalf("WorkItem was mutated on proposal creation: %+v", currentWI)
	}
}

// 4. TestAppApplyProposalHandlesAllTypedOutcomes
func TestAppApplyProposalHandlesAllTypedOutcomes(t *testing.T) {
	app, wiService, propService, db := newProposalApp(t)
	ctx := context.Background()

	// Scenario 4A: Outcome APPLIED
	desc := "Desc 1"
	item1 := createTestWorkItemForProposal(t, wiService, "Title 1", &desc)
	newTitle := "Committed Title 1"
	p1, err := propService.ProposeWorkItemUpdate(ctx, proposal.ProposeWorkItemUpdateInput{
		TargetWorkItemID: item1.ID,
		ProposedTitle:    newTitle,
	})
	if err != nil {
		t.Fatal(err)
	}

	res1, err := app.ApplyProposal(ApplyProposalRequestDTO{ID: p1.ID})
	if err != nil {
		t.Fatalf("ApplyProposal 1: %v", err)
	}
	if res1.Outcome != "APPLIED" {
		t.Fatalf("expected outcome APPLIED, got %s", res1.Outcome)
	}
	if res1.Proposal.Status != "APPLIED" || res1.Proposal.ResolvedAt == nil {
		t.Fatalf("unexpected proposal state after Apply: %+v", res1.Proposal)
	}
	if res1.WorkItem == nil || res1.WorkItem.Title != newTitle {
		t.Fatalf("unexpected WorkItem returned in Apply result: %+v", res1.WorkItem)
	}

	// Scenario 4B: Outcome ALREADY_APPLIED (idempotent repeat)
	res1Repeat, err := app.ApplyProposal(ApplyProposalRequestDTO{ID: p1.ID})
	if err != nil {
		t.Fatalf("ApplyProposal 1 repeat: %v", err)
	}
	if res1Repeat.Outcome != "ALREADY_APPLIED" {
		t.Fatalf("expected outcome ALREADY_APPLIED, got %s", res1Repeat.Outcome)
	}
	if res1Repeat.WorkItem == nil || res1Repeat.WorkItem.Title != newTitle {
		t.Fatalf("expected current WorkItem returned on ALREADY_APPLIED: %+v", res1Repeat.WorkItem)
	}

	// Scenario 4C: Outcome STALE with target modified
	item2 := createTestWorkItemForProposal(t, wiService, "Title 2", nil)
	p2, err := propService.ProposeWorkItemUpdate(ctx, proposal.ProposeWorkItemUpdateInput{
		TargetWorkItemID: item2.ID,
		ProposedTitle:    "Suggested Title 2",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Intervening direct update to item2
	if _, err := wiService.UpdateLocalWorkItemDetails(ctx, workitem.UpdateLocalWorkItemDetailsInput{
		ID:    item2.ID,
		Title: "Intervening Title 2",
	}); err != nil {
		t.Fatal(err)
	}

	res2, err := app.ApplyProposal(ApplyProposalRequestDTO{ID: p2.ID})
	if err != nil {
		t.Fatalf("ApplyProposal 2: %v", err)
	}
	if res2.Outcome != "STALE" {
		t.Fatalf("expected outcome STALE, got %s", res2.Outcome)
	}
	if res2.Proposal.Status != "STALE" || res2.Proposal.ResolvedAt == nil {
		t.Fatalf("unexpected proposal state after stale: %+v", res2.Proposal)
	}
	if res2.WorkItem == nil || res2.WorkItem.Title != "Intervening Title 2" {
		t.Fatalf("expected intervening WorkItem returned on stale: %+v", res2.WorkItem)
	}

	// Scenario 4D: Outcome STALE with target archived -> WorkItem is nil!
	item3 := createTestWorkItemForProposal(t, wiService, "Title 3", nil)
	p3, err := propService.ProposeWorkItemUpdate(ctx, proposal.ProposeWorkItemUpdateInput{
		TargetWorkItemID: item3.ID,
		ProposedTitle:    "Suggested Title 3",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Archive target entity
	nowStr := codec.FormatTime(time.Now().UTC())
	if _, err := db.ExecContext(ctx, `UPDATE entities SET archived_at = ? WHERE id = ?`, nowStr, item3.ID); err != nil {
		t.Fatal(err)
	}

	res3, err := app.ApplyProposal(ApplyProposalRequestDTO{ID: p3.ID})
	if err != nil {
		t.Fatalf("ApplyProposal 3 with archived target: %v", err)
	}
	if res3.Outcome != "STALE" {
		t.Fatalf("expected outcome STALE for archived target, got %s", res3.Outcome)
	}
	if res3.WorkItem != nil {
		t.Fatalf("expected nil WorkItem when target is archived, got: %+v", res3.WorkItem)
	}
}

// 5. TestAppRejectProposalTransitionsStatus
func TestAppRejectProposalTransitionsStatus(t *testing.T) {
	app, wiService, propService, _ := newProposalApp(t)
	ctx := context.Background()
	item := createTestWorkItemForProposal(t, wiService, "Reject Target", nil)

	p, err := propService.ProposeWorkItemUpdate(ctx, proposal.ProposeWorkItemUpdateInput{
		TargetWorkItemID: item.ID,
		ProposedTitle:    "To Reject",
	})
	if err != nil {
		t.Fatal(err)
	}

	rejectedDTO, err := app.RejectProposal(RejectProposalRequestDTO{ID: p.ID})
	if err != nil {
		t.Fatalf("RejectProposal: %v", err)
	}
	if rejectedDTO.Status != "REJECTED" || rejectedDTO.ResolvedAt == nil {
		t.Fatalf("expected status REJECTED with resolvedAt, got %+v", rejectedDTO)
	}

	// WorkItem truth remains untouched
	currentWI, err := wiService.GetByID(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if currentWI.Title != "Reject Target" || currentWI.LastRevisionID != item.LastRevisionID {
		t.Fatalf("WorkItem mutated on rejection: %+v", currentWI)
	}
}

// 6. TestAppMalformedPersistedProposalSafeError
func TestAppMalformedPersistedProposalSafeError(t *testing.T) {
	app, wiService, _, db := newProposalApp(t)
	ctx := context.Background()
	item := createTestWorkItemForProposal(t, wiService, "Target", nil)
	nowStr := codec.FormatTime(time.Now().UTC())

	corruptID := "corrupt-app-prop"
	if _, err := db.ExecContext(ctx, `
		INSERT INTO change_proposals (
			id, target_entity_id, operation_type, title, status, operation_payload_json, created_at, updated_at
		) VALUES (?, ?, 'work_item.update_details', 'Corrupt', 'PENDING', '{"bad":"payload"}', ?, ?)
	`, corruptID, item.ID, nowStr, nowStr); err != nil {
		t.Fatal(err)
	}

	// GetPendingProposals should fail safely with an error, not return corrupt DTO
	_, err := app.GetPendingProposals()
	if err == nil {
		t.Fatal("expected error on corrupt proposal row in GetPendingProposals, got nil")
	}

	// ApplyProposal should fail safely
	_, err = app.ApplyProposal(ApplyProposalRequestDTO{ID: corruptID})
	if err == nil {
		t.Fatal("expected error on corrupt proposal in ApplyProposal, got nil")
	}
}

// 7. TestAppUnknownProposalReturnsProductError
func TestAppUnknownProposalReturnsProductError(t *testing.T) {
	app, _, _, _ := newProposalApp(t)

	_, err := app.ApplyProposal(ApplyProposalRequestDTO{ID: "nonexistent-id"})
	if err == nil {
		t.Fatal("expected error for nonexistent proposal in ApplyProposal, got nil")
	}

	_, err = app.RejectProposal(RejectProposalRequestDTO{ID: "nonexistent-id"})
	if err == nil {
		t.Fatal("expected error for nonexistent proposal in RejectProposal, got nil")
	}
}

// 8. TestAppServiceUninitializedErrors
func TestAppServiceUninitializedErrors(t *testing.T) {
	emptyApp := &App{}

	if _, err := emptyApp.GetPendingProposals(); err == nil {
		t.Fatal("expected error when proposalService is uninitialized")
	}
	if _, err := emptyApp.ProposeWorkItemUpdate(ProposeWorkItemUpdateRequestDTO{}); err == nil {
		t.Fatal("expected error when proposalService is uninitialized")
	}
	if _, err := emptyApp.ApplyProposal(ApplyProposalRequestDTO{}); err == nil {
		t.Fatal("expected error when proposalService is uninitialized")
	}
	if _, err := emptyApp.RejectProposal(RejectProposalRequestDTO{}); err == nil {
		t.Fatal("expected error when proposalService is uninitialized")
	}
	if _, err := emptyApp.GetDashboardProposalSummary(); err == nil {
		t.Fatal("expected error when proposalService is uninitialized in GetDashboardProposalSummary")
	}
}

// 9. TestAppGetDashboardProposalSummary verifies that GetDashboardProposalSummary
// returns accurate pending counts and deduplicated affected work item counts through Wails boundary.
func TestAppGetDashboardProposalSummary(t *testing.T) {
	app, wiService, _, _ := newProposalApp(t)

	// Initial empty state
	summary, err := app.GetDashboardProposalSummary()
	if err != nil {
		t.Fatalf("GetDashboardProposalSummary on empty db: %v", err)
	}
	if summary.PendingCount != 0 || summary.AffectedWorkItemCount != 0 {
		t.Fatalf("expected (0, 0), got (%d, %d)", summary.PendingCount, summary.AffectedWorkItemCount)
	}

	// Create 2 work items
	item1 := createTestWorkItemForProposal(t, wiService, "Work Item 1", nil)
	item2 := createTestWorkItemForProposal(t, wiService, "Work Item 2", nil)

	// Item 1 has 2 proposals, Item 2 has 1 proposal
	p1, err := app.ProposeWorkItemUpdate(ProposeWorkItemUpdateRequestDTO{
		TargetWorkItemID: item1.ID,
		ProposedTitle:    "WI1 Proposed 1",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.ProposeWorkItemUpdate(ProposeWorkItemUpdateRequestDTO{
		TargetWorkItemID: item1.ID,
		ProposedTitle:    "WI1 Proposed 2",
	})
	if err != nil {
		t.Fatal(err)
	}
	p3, err := app.ProposeWorkItemUpdate(ProposeWorkItemUpdateRequestDTO{
		TargetWorkItemID: item2.ID,
		ProposedTitle:    "WI2 Proposed 1",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 3 pending proposals affecting 2 unique work items
	summary, err = app.GetDashboardProposalSummary()
	if err != nil {
		t.Fatal(err)
	}
	if summary.PendingCount != 3 {
		t.Fatalf("expected PendingCount 3, got %d", summary.PendingCount)
	}
	if summary.AffectedWorkItemCount != 2 {
		t.Fatalf("expected AffectedWorkItemCount 2, got %d", summary.AffectedWorkItemCount)
	}

	// Apply p1 on item 1
	applyRes, err := app.ApplyProposal(ApplyProposalRequestDTO{ID: p1.ID})
	if err != nil || applyRes.Outcome != "APPLIED" {
		t.Fatalf("apply p1: %v, outcome=%s", err, applyRes.Outcome)
	}

	// Item 1 still has 1 pending proposal, Item 2 has 1 pending proposal
	summary, err = app.GetDashboardProposalSummary()
	if err != nil {
		t.Fatal(err)
	}
	if summary.PendingCount != 2 {
		t.Fatalf("expected PendingCount 2, got %d", summary.PendingCount)
	}
	if summary.AffectedWorkItemCount != 2 {
		t.Fatalf("expected AffectedWorkItemCount 2, got %d", summary.AffectedWorkItemCount)
	}

	// Reject p3 on item 2
	_, err = app.RejectProposal(RejectProposalRequestDTO{ID: p3.ID})
	if err != nil {
		t.Fatal(err)
	}

	// Now only 1 pending proposal left affecting 1 work item
	summary, err = app.GetDashboardProposalSummary()
	if err != nil {
		t.Fatal(err)
	}
	if summary.PendingCount != 1 {
		t.Fatalf("expected PendingCount 1, got %d", summary.PendingCount)
	}
	if summary.AffectedWorkItemCount != 1 {
		t.Fatalf("expected AffectedWorkItemCount 1, got %d", summary.AffectedWorkItemCount)
	}
}

// 10. TestAppFinalRestartAcceptanceScenarios tests the 4 canonical Proposal product
// scenarios (Pending, Apply, Reject, Stale) across real application/SQLite restart.
func TestAppFinalRestartAcceptanceScenarios(t *testing.T) {
	ctx := context.Background()
	testDB := testsupport.OpenTestDatabase(t)

	constructApp := func(sqlDB *sql.DB) (*App, *workitem.Service, *proposal.Service) {
		repo := workitem.NewRepository(sqlDB)
		wiSvc := workitem.NewService(sqlDB, repo)
		propSvc := proposal.NewService(sqlDB, wiSvc)
		return &App{workitemService: wiSvc, proposalService: propSvc}, wiSvc, propSvc
	}

	findWorkItem := func(application *App, id string) (WorkItemDTO, error) {
		items, err := application.GetWorkItems()
		if err != nil {
			return WorkItemDTO{}, err
		}
		for _, item := range items {
			if item.ID == id {
				return item, nil
			}
		}
		return WorkItemDTO{}, fmt.Errorf("item not found: %s", id)
	}

	currentApp, wiSvc, _ := constructApp(testDB.DB)

	// =========================================================================
	// Scenario 1: Pending Proposal Survives Restart
	// =========================================================================
	descA := "Original Desc A"
	itemA := createTestWorkItemForProposal(t, wiSvc, "Original Title A", &descA)
	propDescA := "Desc A Proposed"
	propReasonA := "Sprint refinement"

	propA, err := currentApp.ProposeWorkItemUpdate(ProposeWorkItemUpdateRequestDTO{
		TargetWorkItemID:    itemA.ID,
		ProposedTitle:       "Title A Proposed",
		ProposedDescription: &propDescA,
		Reason:              &propReasonA,
	})
	if err != nil {
		t.Fatalf("propose A: %v", err)
	}
	if propA.Status != "PENDING" {
		t.Fatalf("expected PENDING, got %s", propA.Status)
	}

	// Simulate Restart 1
	if err := testDB.DB.Close(); err != nil {
		t.Fatalf("close db restart 1: %v", err)
	}
	reopenedDB1, err := database.Open(ctx)
	if err != nil {
		t.Fatalf("reopen db restart 1: %v", err)
	}
	currentApp, wiSvc, _ = constructApp(reopenedDB1.DB)

	// Verify Scenario 1 post-restart
	pendingList, err := currentApp.GetPendingProposals()
	if err != nil {
		t.Fatalf("get pending restart 1: %v", err)
	}
	if len(pendingList) != 1 || pendingList[0].ID != propA.ID {
		t.Fatalf("expected 1 pending proposal with ID %s, got %v", propA.ID, pendingList)
	}
	if pendingList[0].Status != "PENDING" {
		t.Fatalf("expected status PENDING after restart, got %s", pendingList[0].Status)
	}
	if pendingList[0].BaseTitle != "Original Title A" || pendingList[0].ProposedTitle != "Title A Proposed" {
		t.Fatalf("unexpected titles after restart: base=%q, proposed=%q", pendingList[0].BaseTitle, pendingList[0].ProposedTitle)
	}

	dashSummary, err := currentApp.GetDashboardProposalSummary()
	if err != nil {
		t.Fatalf("dashboard summary restart 1: %v", err)
	}
	if dashSummary.PendingCount != 1 || dashSummary.AffectedWorkItemCount != 1 {
		t.Fatalf("expected summary (1, 1), got (%d, %d)", dashSummary.PendingCount, dashSummary.AffectedWorkItemCount)
	}

	wiA, err := findWorkItem(currentApp, itemA.ID)
	if err != nil {
		t.Fatalf("get work item A: %v", err)
	}
	if wiA.Title != "Original Title A" {
		t.Fatalf("WorkItem A unexpectedly mutated before apply: %s", wiA.Title)
	}

	// =========================================================================
	// Scenario 2: Apply Proposal Survives Restart and Repeated Apply Idempotency
	// =========================================================================
	applyRes, err := currentApp.ApplyProposal(ApplyProposalRequestDTO{ID: propA.ID})
	if err != nil {
		t.Fatalf("apply A: %v", err)
	}
	if applyRes.Outcome != "APPLIED" {
		t.Fatalf("expected outcome APPLIED, got %s", applyRes.Outcome)
	}
	if applyRes.WorkItem == nil || applyRes.WorkItem.Title != "Title A Proposed" {
		t.Fatalf("expected updated work item title 'Title A Proposed', got %v", applyRes.WorkItem)
	}

	// Simulate Restart 2
	if err := reopenedDB1.DB.Close(); err != nil {
		t.Fatalf("close db restart 2: %v", err)
	}
	reopenedDB2, err := database.Open(ctx)
	if err != nil {
		t.Fatalf("reopen db restart 2: %v", err)
	}
	currentApp, wiSvc, _ = constructApp(reopenedDB2.DB)

	// Verify Scenario 2 post-restart:
	// - Proposal A is no longer pending
	pendingList, err = currentApp.GetPendingProposals()
	if err != nil {
		t.Fatalf("get pending restart 2: %v", err)
	}
	if len(pendingList) != 0 {
		t.Fatalf("expected 0 pending proposals after apply, got %d", len(pendingList))
	}

	// - Dashboard summary is 0, 0
	dashSummary, err = currentApp.GetDashboardProposalSummary()
	if err != nil {
		t.Fatalf("dashboard summary restart 2: %v", err)
	}
	if dashSummary.PendingCount != 0 || dashSummary.AffectedWorkItemCount != 0 {
		t.Fatalf("expected summary (0, 0), got (%d, %d)", dashSummary.PendingCount, dashSummary.AffectedWorkItemCount)
	}

	// - WorkItem A persisted new title and description
	wiA, err = findWorkItem(currentApp, itemA.ID)
	if err != nil {
		t.Fatalf("get work item A restart 2: %v", err)
	}
	if wiA.Title != "Title A Proposed" {
		t.Fatalf("expected WorkItem A title 'Title A Proposed', got %q", wiA.Title)
	}
	if wiA.Description == nil || *wiA.Description != "Desc A Proposed" {
		t.Fatalf("expected WorkItem A desc 'Desc A Proposed', got %v", wiA.Description)
	}

	// - Repeated Apply returns ALREADY_APPLIED idempotently without error or re-mutation
	repeatedApply, err := currentApp.ApplyProposal(ApplyProposalRequestDTO{ID: propA.ID})
	if err != nil {
		t.Fatalf("repeated apply A: %v", err)
	}
	if repeatedApply.Outcome != "ALREADY_APPLIED" {
		t.Fatalf("expected outcome ALREADY_APPLIED on repeat, got %s", repeatedApply.Outcome)
	}
	if repeatedApply.WorkItem == nil || repeatedApply.WorkItem.Title != "Title A Proposed" {
		t.Fatalf("expected WorkItem on repeated apply, got %v", repeatedApply.WorkItem)
	}

	// =========================================================================
	// Scenario 3: Reject Proposal Survives Restart
	// =========================================================================
	itemB := createTestWorkItemForProposal(t, wiSvc, "Original Title B", nil)

	propB, err := currentApp.ProposeWorkItemUpdate(ProposeWorkItemUpdateRequestDTO{
		TargetWorkItemID: itemB.ID,
		ProposedTitle:    "Title B Proposed",
	})
	if err != nil {
		t.Fatalf("propose B: %v", err)
	}

	// Simulate Restart 3 before reject
	if err := reopenedDB2.DB.Close(); err != nil {
		t.Fatalf("close db restart 3: %v", err)
	}
	reopenedDB3, err := database.Open(ctx)
	if err != nil {
		t.Fatalf("reopen db restart 3: %v", err)
	}
	currentApp, wiSvc, _ = constructApp(reopenedDB3.DB)

	// Reject proposal B
	rejectRes, err := currentApp.RejectProposal(RejectProposalRequestDTO{ID: propB.ID})
	if err != nil {
		t.Fatalf("reject B: %v", err)
	}
	if rejectRes.Status != "REJECTED" {
		t.Fatalf("expected status REJECTED, got %s", rejectRes.Status)
	}

	// Simulate Restart 4 after reject
	if err := reopenedDB3.DB.Close(); err != nil {
		t.Fatalf("close db restart 4: %v", err)
	}
	reopenedDB4, err := database.Open(ctx)
	if err != nil {
		t.Fatalf("reopen db restart 4: %v", err)
	}
	currentApp, wiSvc, _ = constructApp(reopenedDB4.DB)

	// Verify Scenario 3 post-restart:
	// - 0 pending proposals
	pendingList, err = currentApp.GetPendingProposals()
	if err != nil {
		t.Fatalf("get pending restart 4: %v", err)
	}
	if len(pendingList) != 0 {
		t.Fatalf("expected 0 pending proposals after reject, got %d", len(pendingList))
	}

	// - Dashboard summary is 0, 0
	dashSummary, err = currentApp.GetDashboardProposalSummary()
	if err != nil {
		t.Fatalf("dashboard summary restart 4: %v", err)
	}
	if dashSummary.PendingCount != 0 || dashSummary.AffectedWorkItemCount != 0 {
		t.Fatalf("expected summary (0, 0), got (%d, %d)", dashSummary.PendingCount, dashSummary.AffectedWorkItemCount)
	}

	// - WorkItem B is completely unmodified
	wiB, err := findWorkItem(currentApp, itemB.ID)
	if err != nil {
		t.Fatalf("get work item B restart 4: %v", err)
	}
	if wiB.Title != "Original Title B" {
		t.Fatalf("expected WorkItem B title 'Original Title B', got %q", wiB.Title)
	}

	// =========================================================================
	// Scenario 4: Intervening WorkItem Mutation Yields STALE and Survives Restart
	// =========================================================================
	itemC := createTestWorkItemForProposal(t, wiSvc, "Original Title C", nil)

	propC, err := currentApp.ProposeWorkItemUpdate(ProposeWorkItemUpdateRequestDTO{
		TargetWorkItemID: itemC.ID,
		ProposedTitle:    "Title C Proposed",
	})
	if err != nil {
		t.Fatalf("propose C: %v", err)
	}

	// Simulate an intervening authoritative domain mutation without depending
	// on a product direct-edit Wails surface.
	_, err = currentApp.workitemService.UpdateLocalWorkItemDetails(ctx, workitem.UpdateLocalWorkItemDetailsInput{
		ID:    itemC.ID,
		Title: "Intervening Title C",
	})
	if err != nil {
		t.Fatalf("direct update C: %v", err)
	}

	// Simulate Restart 5
	if err := reopenedDB4.DB.Close(); err != nil {
		t.Fatalf("close db restart 5: %v", err)
	}
	reopenedDB5, err := database.Open(ctx)
	if err != nil {
		t.Fatalf("reopen db restart 5: %v", err)
	}
	defer reopenedDB5.DB.Close()
	currentApp, _, _ = constructApp(reopenedDB5.DB)

	// Apply stale proposal C
	applyStaleRes, err := currentApp.ApplyProposal(ApplyProposalRequestDTO{ID: propC.ID})
	if err != nil {
		t.Fatalf("apply stale C: %v", err)
	}
	if applyStaleRes.Outcome != "STALE" {
		t.Fatalf("expected outcome STALE, got %s", applyStaleRes.Outcome)
	}
	if applyStaleRes.Proposal.Status != "STALE" {
		t.Fatalf("expected proposal status STALE, got %s", applyStaleRes.Proposal.Status)
	}

	// Verify WorkItem C preserved its directly edited title
	wiC, err := findWorkItem(currentApp, itemC.ID)
	if err != nil {
		t.Fatalf("get work item C restart 5: %v", err)
	}
	if wiC.Title != "Intervening Title C" {
		t.Fatalf("expected WorkItem C title 'Intervening Title C', got %q", wiC.Title)
	}

	// Dashboard summary has 0 pending
	dashSummary, err = currentApp.GetDashboardProposalSummary()
	if err != nil {
		t.Fatalf("dashboard summary restart 5: %v", err)
	}
	if dashSummary.PendingCount != 0 || dashSummary.AffectedWorkItemCount != 0 {
		t.Fatalf("expected summary (0, 0), got (%d, %d)", dashSummary.PendingCount, dashSummary.AffectedWorkItemCount)
	}
}
