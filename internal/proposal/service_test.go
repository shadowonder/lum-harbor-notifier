package proposal

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"lum-harbor-notifier/internal/database"
	"lum-harbor-notifier/internal/database/codec"
	"lum-harbor-notifier/internal/testsupport"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

func setupTestContext(t *testing.T) (context.Context, *database.Database, *workitem.Service, *Service) {
	t.Helper()
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	wiRepo := workitem.NewRepository(db.DB)
	wiService := workitem.NewService(db.DB, wiRepo)
	proposalService := NewService(db.DB, wiService)
	return ctx, db, wiService, proposalService
}

func TestTrackedItemProposalOperationsApplyOnlyAtReviewBoundary(t *testing.T) {
	ctx := context.Background()
	db := testsupport.OpenTestDatabase(t)
	wiService := workitem.NewService(db.DB, workitem.NewRepository(db.DB))
	trackingService := tracking.NewService(db.DB, wiService)
	proposals := NewService(db.DB, wiService, trackingService)
	wi := createTestWorkItem(t, ctx, wiService, "Tracked product", nil)
	def, err := trackingService.CreateWorkflowDefinition(ctx, tracking.CreateWorkflowDefinitionInput{Name: "Delivery", Version: 1, Steps: []tracking.WorkflowStepInput{{Key: "plan", Title: "Plan"}, {Key: "verify", Title: "Verify", Terminal: true}}})
	if err != nil {
		t.Fatal(err)
	}
	wf, err := trackingService.CreateWorkflow(ctx, tracking.CreateWorkflowInput{WorkItemID: wi.ID, WorkflowDefinitionID: def.ID})
	if err != nil {
		t.Fatal(err)
	}
	createTitle, taskType := "API", "TASK"
	create, err := proposals.ProposeTrackedItemOperation(ctx, ProposeTrackedItemOperationInput{Producer: ProducerAI, Payload: TrackedItemOperationPayload{OperationKind: OperationTypeTrackedItemCreate, WorkItemID: &wi.ID, WorkflowID: &wf.ID, Title: &createTitle, ItemType: &taskType}})
	if err != nil {
		t.Fatal(err)
	}
	if view, _ := trackingService.GetWorkItemTracking(ctx, wi.ID); len(view.Items) != 0 {
		t.Fatal("preview/proposal creation mutated truth")
	}
	createdResult, err := proposals.ApplyProposal(ctx, ApplyProposalInput{ID: create.ID})
	if err != nil || createdResult.Outcome != ApplyOutcomeApplied {
		t.Fatalf("create apply: %+v %v", createdResult, err)
	}
	view, _ := trackingService.GetWorkItemTracking(ctx, wi.ID)
	if len(view.Items) != 1 {
		t.Fatalf("expected one created item: %+v", view.Items)
	}
	api := view.Items[0].Item
	newTitle := "GraphQL API"
	update, err := proposals.ProposeTrackedItemOperation(ctx, ProposeTrackedItemOperationInput{Producer: ProducerAI, Payload: TrackedItemOperationPayload{OperationKind: OperationTypeTrackedItemUpdate, TrackedItemID: &api.ID, Title: &newTitle, ItemType: &taskType}})
	if err != nil {
		t.Fatal(err)
	}
	if current, _ := trackingService.GetTrackedItem(ctx, api.ID); current.Title != "API" {
		t.Fatal("update preview mutated truth")
	}
	if _, err := proposals.ApplyProposal(ctx, ApplyProposalInput{ID: update.ID}); err != nil {
		t.Fatal(err)
	}
	stageState := string(tracking.StepBlocked)
	stage, err := proposals.ProposeTrackedItemOperation(ctx, ProposeTrackedItemOperationInput{Payload: TrackedItemOperationPayload{OperationKind: OperationTypeTrackedItemStageState, TrackedItemID: &api.ID, WorkflowStepID: &def.Steps[1].ID, State: &stageState}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := proposals.ApplyProposal(ctx, ApplyProposalInput{ID: stage.ID}); err != nil {
		t.Fatal(err)
	}
	second, _ := trackingService.CreateTrackedItem(ctx, tracking.CreateTrackedItemInput{WorkItemID: wi.ID, Title: "Schema"})
	add, err := proposals.ProposeTrackedItemOperation(ctx, ProposeTrackedItemOperationInput{Payload: TrackedItemOperationPayload{OperationKind: OperationTypeTrackedItemDependencyAdd, TrackedItemID: &api.ID, ToTrackedItemID: &second.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := proposals.ApplyProposal(ctx, ApplyProposalInput{ID: add.ID}); err != nil {
		t.Fatal(err)
	}
	remove, err := proposals.ProposeTrackedItemOperation(ctx, ProposeTrackedItemOperationInput{Payload: TrackedItemOperationPayload{OperationKind: OperationTypeTrackedItemDependencyRemove, TrackedItemID: &api.ID, ToTrackedItemID: &second.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := proposals.ApplyProposal(ctx, ApplyProposalInput{ID: remove.ID}); err != nil {
		t.Fatal(err)
	}
	archive, err := proposals.ProposeTrackedItemOperation(ctx, ProposeTrackedItemOperationInput{Payload: TrackedItemOperationPayload{OperationKind: OperationTypeTrackedItemArchive, TrackedItemID: &api.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := proposals.ApplyProposal(ctx, ApplyProposalInput{ID: archive.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := trackingService.GetTrackedItem(ctx, api.ID); !errors.Is(err, tracking.ErrTrackedItemNotFound) {
		t.Fatalf("archive Apply did not retire item: %v", err)
	}
}

func createTestWorkItem(t *testing.T, ctx context.Context, wiService *workitem.Service, title string, desc *string) *workitem.WorkItem {
	t.Helper()
	item, err := wiService.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{
		Title:       title,
		Description: desc,
	})
	if err != nil {
		t.Fatalf("failed to create test work item: %v", err)
	}
	return item
}

// 1 & 2. TestProposalIDUUIDv7 verifies that proposal creation produces a valid UUIDv7 identity.
func TestProposalIDUUIDv7(t *testing.T) {
	ctx, _, wiService, proposalService := setupTestContext(t)
	desc := "Initial description"
	item := createTestWorkItem(t, ctx, wiService, "Feature Work", &desc)

	prop, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: item.ID,
		ProposedTitle:    "Updated Feature Work",
	})
	if err != nil {
		t.Fatalf("failed to create proposal: %v", err)
	}

	parsedID, err := uuid.Parse(prop.ID)
	if err != nil {
		t.Fatalf("expected valid UUID, got %q: %v", prop.ID, err)
	}
	if parsedID.Version() != 7 {
		t.Fatalf("expected UUIDv7 identity, got version %d", parsedID.Version())
	}
}

// 3. TestProposalValidPendingRoundTrip tests creating a valid PENDING proposal and reading it back.
func TestProposalValidPendingRoundTrip(t *testing.T) {
	ctx, _, wiService, proposalService := setupTestContext(t)
	desc := "Original work description"
	item := createTestWorkItem(t, ctx, wiService, "Alpha Release", &desc)

	propTitle := "Rename to Alpha Release V1"
	proposedDesc := "Revised description"
	reason := "Clarify scope based on team alignment"
	summary := "Scope update"

	prop, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID:    item.ID,
		ProposedTitle:       "Alpha Release V1",
		ProposedDescription: &proposedDesc,
		ProposalTitle:       &propTitle,
		Summary:             &summary,
		Reason:              &reason,
	})
	if err != nil {
		t.Fatalf("failed to propose update: %v", err)
	}

	if prop.Status != StatusPending {
		t.Fatalf("expected status PENDING, got %s", prop.Status)
	}
	if prop.ResolvedAt != nil {
		t.Fatalf("expected resolved_at to be nil for PENDING proposal, got %v", prop.ResolvedAt)
	}
	if prop.AppliedRevisionID != nil {
		t.Fatalf("expected applied_revision_id to be nil, got %v", prop.AppliedRevisionID)
	}
	if prop.Title != propTitle {
		t.Fatalf("expected title %q, got %q", propTitle, prop.Title)
	}
	if prop.Summary == nil || *prop.Summary != summary {
		t.Fatalf("unexpected summary: %v", prop.Summary)
	}
	if prop.Reason == nil || *prop.Reason != reason {
		t.Fatalf("unexpected reason: %v", prop.Reason)
	}

	// Round-trip read back via GetByID
	loaded, err := proposalService.GetByID(ctx, prop.ID)
	if err != nil {
		t.Fatalf("failed to load proposal: %v", err)
	}
	if loaded.ID != prop.ID {
		t.Fatalf("expected ID %s, got %s", prop.ID, loaded.ID)
	}
	if loaded.TargetEntityID == nil || *loaded.TargetEntityID != item.ID {
		t.Fatalf("expected TargetEntityID %s, got %v", item.ID, loaded.TargetEntityID)
	}
	if loaded.Status != StatusPending {
		t.Fatalf("expected loaded status PENDING, got %s", loaded.Status)
	}
	if loaded.Payload.ProposedTitle != "Alpha Release V1" {
		t.Fatalf("unexpected proposed title: %s", loaded.Payload.ProposedTitle)
	}
	if loaded.Payload.ProposedDescription == nil || *loaded.Payload.ProposedDescription != proposedDesc {
		t.Fatalf("unexpected proposed description: %v", loaded.Payload.ProposedDescription)
	}
}

// 4. TestMalformedOperationPayloadCannotEnterServiceTruth verifies validation rejections.
func TestMalformedOperationPayloadCannotEnterServiceTruth(t *testing.T) {
	ctx, _, wiService, proposalService := setupTestContext(t)
	item := createTestWorkItem(t, ctx, wiService, "Work", nil)

	// Blank target ID
	_, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: "   ",
		ProposedTitle:    "New Title",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for blank target ID, got %v", err)
	}

	// Blank proposed title
	_, err = proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: item.ID,
		ProposedTitle:    "   ",
	})
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("expected ErrInvalidInput for blank proposed title, got %v", err)
	}

	// Structural payload validation directly
	badKindPayload := WorkItemUpdateDetailsPayload{
		OperationKind: "unknown.kind",
		Version:       OperationVersionV1,
		Preconditions: WorkItemPreconditions{ExpectedLastRevisionID: "rev-1"},
	}
	if err := badKindPayload.Validate(); !errors.Is(err, ErrInvalidOperationPayload) {
		t.Fatalf("expected ErrInvalidOperationPayload for invalid kind, got %v", err)
	}

	badVersionPayload := WorkItemUpdateDetailsPayload{
		OperationKind: OperationTypeWorkItemUpdateDetails,
		Version:       "v2",
		Preconditions: WorkItemPreconditions{ExpectedLastRevisionID: "rev-1"},
	}
	if err := badVersionPayload.Validate(); !errors.Is(err, ErrInvalidOperationPayload) {
		t.Fatalf("expected ErrInvalidOperationPayload for invalid version, got %v", err)
	}

	missingRevPayload := WorkItemUpdateDetailsPayload{
		OperationKind: OperationTypeWorkItemUpdateDetails,
		Version:       OperationVersionV1,
		Preconditions: WorkItemPreconditions{ExpectedLastRevisionID: "  "},
	}
	if err := missingRevPayload.Validate(); !errors.Is(err, ErrInvalidOperationPayload) {
		t.Fatalf("expected ErrInvalidOperationPayload for missing last revision, got %v", err)
	}

	// Malformed JSON parsing
	_, err = ParseWorkItemUpdateDetailsPayload([]byte(`{not valid json`))
	if !errors.Is(err, ErrInvalidOperationPayload) {
		t.Fatalf("expected ErrInvalidOperationPayload for malformed JSON, got %v", err)
	}
}

// 5, 6, 7 & 8. TestDeterministicProducerPreconditionsAndWorkItemInvariance
// Verifies producer reads WorkItem, captures exact preconditions, and DOES NOT mutate
// the WorkItem or create a domain Revision.
func TestDeterministicProducerPreconditionsAndWorkItemInvariance(t *testing.T) {
	ctx, db, wiService, proposalService := setupTestContext(t)
	desc := "Pre-existing description"
	item := createTestWorkItem(t, ctx, wiService, "Current Truth WorkItem", &desc)

	// Count revisions before proposal creation
	var revisionsBefore int
	if err := db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM revisions").Scan(&revisionsBefore); err != nil {
		t.Fatalf("count revisions before: %v", err)
	}

	proposedTitle := "Proposed Future Title"
	newDesc := "Proposed Future Description"
	prop, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID:    item.ID,
		ProposedTitle:       proposedTitle,
		ProposedDescription: &newDesc,
	})
	if err != nil {
		t.Fatalf("propose update failed: %v", err)
	}

	// 6. Preconditions captured must match current authoritative state
	if prop.Payload.Preconditions.ExpectedLastRevisionID != item.LastRevisionID {
		t.Fatalf("expected precondition revision %s, got %s", item.LastRevisionID, prop.Payload.Preconditions.ExpectedLastRevisionID)
	}
	if prop.Payload.Preconditions.ExpectedTitle != item.Title {
		t.Fatalf("expected precondition title %q, got %q", item.Title, prop.Payload.Preconditions.ExpectedTitle)
	}
	if prop.Payload.Preconditions.ExpectedDescription == nil || *prop.Payload.Preconditions.ExpectedDescription != *item.Description {
		t.Fatalf("expected precondition description %v, got %v", item.Description, prop.Payload.Preconditions.ExpectedDescription)
	}

	// 7. WorkItem must NOT be modified
	currentWI, err := wiService.GetByID(ctx, item.ID)
	if err != nil {
		t.Fatalf("load work item after proposal: %v", err)
	}
	if currentWI.Title != "Current Truth WorkItem" {
		t.Fatalf("work item title mutated! got %q", currentWI.Title)
	}
	if currentWI.Description == nil || *currentWI.Description != desc {
		t.Fatalf("work item description mutated! got %v", currentWI.Description)
	}
	if currentWI.LastRevisionID != item.LastRevisionID {
		t.Fatalf("work item revision mutated! got %s, want %s", currentWI.LastRevisionID, item.LastRevisionID)
	}
	if !currentWI.UpdatedAt.Equal(item.UpdatedAt) {
		t.Fatalf("work item updated_at changed! before=%v after=%v", item.UpdatedAt, currentWI.UpdatedAt)
	}

	// 8. Revisions table count must NOT increase
	var revisionsAfter int
	if err := db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM revisions").Scan(&revisionsAfter); err != nil {
		t.Fatalf("count revisions after: %v", err)
	}
	if revisionsAfter != revisionsBefore {
		t.Fatalf("expected %d revisions, but found %d; proposal creation must not create domain revisions",
			revisionsBefore, revisionsAfter)
	}
}

// 9. TestProposalSurvivesDatabaseReopen verifies durability across connection reopen.
func TestProposalSurvivesDatabaseReopen(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	ctx := context.Background()
	db, err := database.Open(ctx)
	if err != nil {
		t.Fatalf("open initial database: %v", err)
	}
	if err := database.Migrate(db.DB); err != nil {
		t.Fatalf("migrate initial database: %v", err)
	}

	wiRepo := workitem.NewRepository(db.DB)
	wiService := workitem.NewService(db.DB, wiRepo)
	proposalService := NewService(db.DB, wiService)

	item, err := wiService.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{
		Title: "Persistent Target",
	})
	if err != nil {
		t.Fatalf("create work item: %v", err)
	}

	prop, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: item.ID,
		ProposedTitle:    "Persistent Proposed Title",
	})
	if err != nil {
		t.Fatalf("create proposal: %v", err)
	}

	// Close database
	if err := db.DB.Close(); err != nil {
		t.Fatalf("close database: %v", err)
	}

	// Reopen database from same directory
	reopenedDB, err := database.Open(ctx)
	if err != nil {
		t.Fatalf("reopen database: %v", err)
	}
	defer reopenedDB.DB.Close()

	reopenedWIRepo := workitem.NewRepository(reopenedDB.DB)
	reopenedWIService := workitem.NewService(reopenedDB.DB, reopenedWIRepo)
	reopenedProposalService := NewService(reopenedDB.DB, reopenedWIService)

	loaded, err := reopenedProposalService.GetByID(ctx, prop.ID)
	if err != nil {
		t.Fatalf("load proposal after reopen: %v", err)
	}
	if loaded.ID != prop.ID || loaded.Status != StatusPending || loaded.Payload.ProposedTitle != "Persistent Proposed Title" {
		t.Fatalf("unexpected proposal state after reopen: %+v", loaded)
	}
}

// 10. TestListPendingDeterministicOrdering verifies list ordering by created_at ASC, id ASC.
func TestListPendingDeterministicOrdering(t *testing.T) {
	ctx, _, wiService, proposalService := setupTestContext(t)
	item := createTestWorkItem(t, ctx, wiService, "Multi Proposal Target", nil)

	p1, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: item.ID,
		ProposedTitle:    "First Update",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Small pause to guarantee distinct created_at if clock resolution is coarse
	time.Sleep(10 * time.Millisecond)

	p2, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: item.ID,
		ProposedTitle:    "Second Update",
	})
	if err != nil {
		t.Fatal(err)
	}

	list, err := proposalService.ListPending(ctx)
	if err != nil {
		t.Fatalf("list pending proposals: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 pending proposals, got %d", len(list))
	}
	if list[0].ID != p1.ID || list[1].ID != p2.ID {
		t.Fatalf("unexpected list ordering: want [%s, %s], got [%s, %s]", p1.ID, p2.ID, list[0].ID, list[1].ID)
	}
}

// 11, 12, 13 & 14. TestRejectProposalSemantics
// Verifies:
// - PENDING -> REJECTED transition with resolved_at set
// - WorkItem is not mutated
// - Reopen durability of rejection
// - Same-state Reject is an idempotent no-op
// - Illegal transitions (APPLIED -> REJECTED, STALE -> REJECTED) are rejected
func TestRejectProposalSemantics(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	ctx := context.Background()
	db, err := database.Open(ctx)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := database.Migrate(db.DB); err != nil {
		t.Fatalf("migrate database: %v", err)
	}

	wiRepo := workitem.NewRepository(db.DB)
	wiService := workitem.NewService(db.DB, wiRepo)
	proposalService := NewService(db.DB, wiService)

	item, err := wiService.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{
		Title: "Reject Target",
	})
	if err != nil {
		t.Fatal(err)
	}

	var revisionsBefore int
	if err := db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM revisions").Scan(&revisionsBefore); err != nil {
		t.Fatal(err)
	}

	prop, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: item.ID,
		ProposedTitle:    "Candidate To Reject",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 11. Reject: PENDING -> REJECTED
	rejected, err := proposalService.RejectProposal(ctx, RejectProposalInput{ID: prop.ID})
	if err != nil {
		t.Fatalf("reject proposal failed: %v", err)
	}
	if rejected.Status != StatusRejected {
		t.Fatalf("expected status REJECTED, got %s", rejected.Status)
	}
	if rejected.ResolvedAt == nil {
		t.Fatal("expected resolved_at to be non-nil after rejection")
	}

	// Verify it is no longer listed as pending
	pendingList, err := proposalService.ListPending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pendingList) != 0 {
		t.Fatalf("expected 0 pending proposals after reject, got %d", len(pendingList))
	}

	// 12. Reject does not mutate WorkItem or create Revisions
	wiAfterReject, err := wiService.GetByID(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if wiAfterReject.Title != "Reject Target" || wiAfterReject.LastRevisionID != item.LastRevisionID {
		t.Fatalf("WorkItem mutated after proposal rejection: %+v", wiAfterReject)
	}
	var revisionsAfter int
	if err := db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM revisions").Scan(&revisionsAfter); err != nil {
		t.Fatal(err)
	}
	if revisionsAfter != revisionsBefore {
		t.Fatalf("revisions count changed after reject: before=%d, after=%d", revisionsBefore, revisionsAfter)
	}

	// Idempotent repeat reject
	sameReject, err := proposalService.RejectProposal(ctx, RejectProposalInput{ID: prop.ID})
	if err != nil {
		t.Fatalf("repeat reject failed: %v", err)
	}
	if sameReject.Status != StatusRejected || !sameReject.ResolvedAt.Equal(*rejected.ResolvedAt) {
		t.Fatalf("idempotent reject mismatch: %+v vs %+v", sameReject, rejected)
	}

	// 13. Rejection survives database reopen
	if err := db.DB.Close(); err != nil {
		t.Fatal(err)
	}
	reopenedDB, err := database.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reopenedDB.DB.Close()

	reopenedProposalService := NewService(reopenedDB.DB, workitem.NewService(reopenedDB.DB, workitem.NewRepository(reopenedDB.DB)))
	loadedRejected, err := reopenedProposalService.GetByID(ctx, prop.ID)
	if err != nil {
		t.Fatalf("load rejected proposal after reopen: %v", err)
	}
	if loadedRejected.Status != StatusRejected || loadedRejected.ResolvedAt == nil {
		t.Fatalf("unexpected state after reopen: %+v", loadedRejected)
	}

	// 14. Illegal status transitions: APPLIED -> REJECTED and STALE -> REJECTED
	nowStr := codec.FormatTime(time.Now().UTC())
	validPayload, _ := prop.Payload.JSON()

	// Insert APPLIED fixture directly via SQL
	appliedPropID := "prop-applied-test"
	if _, err := reopenedDB.DB.Exec(`
		INSERT INTO change_proposals (
			id, target_entity_id, operation_type, title, status, operation_payload_json, applied_revision_id, created_at, updated_at, resolved_at
		) VALUES (?, ?, ?, 'Applied Prop', 'APPLIED', ?, ?, ?, ?, ?)
	`, appliedPropID, item.ID, OperationTypeWorkItemUpdateDetails, validPayload, item.LastRevisionID, nowStr, nowStr, nowStr); err != nil {
		t.Fatalf("insert APPLIED fixture: %v", err)
	}

	_, err = reopenedProposalService.RejectProposal(ctx, RejectProposalInput{ID: appliedPropID})
	if !errors.Is(err, ErrInvalidStatusTransition) {
		t.Fatalf("expected ErrInvalidStatusTransition when rejecting APPLIED proposal, got: %v", err)
	}

	// Insert STALE fixture directly via SQL
	stalePropID := "prop-stale-test"
	if _, err := reopenedDB.DB.Exec(`
		INSERT INTO change_proposals (
			id, target_entity_id, operation_type, title, status, operation_payload_json, applied_revision_id, created_at, updated_at, resolved_at
		) VALUES (?, ?, ?, 'Stale Prop', 'STALE', ?, NULL, ?, ?, ?)
	`, stalePropID, item.ID, OperationTypeWorkItemUpdateDetails, validPayload, nowStr, nowStr, nowStr); err != nil {
		t.Fatalf("insert STALE fixture: %v", err)
	}

	_, err = reopenedProposalService.RejectProposal(ctx, RejectProposalInput{ID: stalePropID})
	if !errors.Is(err, ErrInvalidStatusTransition) {
		t.Fatalf("expected ErrInvalidStatusTransition when rejecting STALE proposal, got: %v", err)
	}
}

// 15. TestTargetUnavailableRejected verifies that unknown or archived targets are rejected.
func TestTargetUnavailableRejected(t *testing.T) {
	ctx, db, wiService, proposalService := setupTestContext(t)

	// Unknown target ID
	_, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: "nonexistent-work-item-id",
		ProposedTitle:    "Title",
	})
	if !errors.Is(err, ErrTargetUnavailable) {
		t.Fatalf("expected ErrTargetUnavailable for nonexistent target, got: %v", err)
	}

	// Archived target entity
	item := createTestWorkItem(t, ctx, wiService, "To Be Archived", nil)
	nowStr := codec.FormatTime(time.Now().UTC())
	if _, err := db.DB.Exec(`UPDATE entities SET archived_at = ? WHERE id = ?`, nowStr, item.ID); err != nil {
		t.Fatalf("archive entity: %v", err)
	}

	_, err = proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: item.ID,
		ProposedTitle:    "Title for Archived",
	})
	if !errors.Is(err, ErrTargetUnavailable) {
		t.Fatalf("expected ErrTargetUnavailable for archived target, got: %v", err)
	}
}

// 16. TestMalformedPersistedProposalSafeError verifies that corrupt DB rows fail safely on mapping.
func TestMalformedPersistedProposalSafeError(t *testing.T) {
	ctx, db, wiService, proposalService := setupTestContext(t)
	item := createTestWorkItem(t, ctx, wiService, "Corrupt Row Target", nil)
	nowStr := codec.FormatTime(time.Now().UTC())

	// Insert row with corrupt JSON payload (bypassing Go validation)
	corruptID := "prop-corrupt-payload"
	// Temporarily disable foreign keys or use valid entity
	if _, err := db.DB.Exec(`
		INSERT INTO change_proposals (
			id, target_entity_id, operation_type, title, status, operation_payload_json, created_at, updated_at
		) VALUES (?, ?, 'work_item.update_details', 'Title', 'PENDING', '{"operation_kind":"bad.kind"}', ?, ?)
	`, corruptID, item.ID, nowStr, nowStr); err != nil {
		t.Fatalf("insert corrupt row: %v", err)
	}

	_, err := proposalService.GetByID(ctx, corruptID)
	if err == nil {
		t.Fatal("expected error mapping corrupt proposal payload, got nil")
	}
	if !errors.Is(err, ErrInvalidOperationPayload) {
		t.Fatalf("expected ErrInvalidOperationPayload, got: %v", err)
	}

	// ListPending should also fail safely on corrupt payload
	_, err = proposalService.ListPending(ctx)
	if err == nil {
		t.Fatal("expected ListPending to fail safely on corrupt proposal payload, got nil")
	}
}

// 17. TestTransactionRollbackOnFailure verifies failed persistence leaves no row.
func TestTransactionRollbackOnFailure(t *testing.T) {
	ctx, db, _, _ := setupTestContext(t)
	now := time.Now().UTC()

	// Simulate failed transaction via Repository directly
	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}

	repo := NewRepository(tx)
	badTarget := "nonexistent-entity-will-fail-fk"
	fakeProp := &Proposal{
		ID:             "prop-rollback-test",
		TargetEntityID: &badTarget,
		OperationType:  OperationTypeWorkItemUpdateDetails,
		Title:          "Rollback Title",
		Status:         StatusPending,
		Payload: WorkItemUpdateDetailsPayload{
			OperationKind: OperationTypeWorkItemUpdateDetails,
			Version:       OperationVersionV1,
			Preconditions: WorkItemPreconditions{ExpectedLastRevisionID: "rev-1"},
			ProposedTitle: "New Title",
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	err = repo.Create(ctx, fakeProp)
	if err == nil {
		_ = tx.Rollback()
		t.Fatal("expected foreign key constraint violation error")
	}
	_ = tx.Rollback()

	// Verify no proposal was persisted
	var count int
	if err := db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM change_proposals WHERE id = 'prop-rollback-test'").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0 rows after rollback, got %d", count)
	}
}

// 18. TestApplyProposalSuccess verifies the full atomic happy path:
// - WorkItem title and description are updated
// - Proposal transitions to APPLIED with applied_revision_id = target.LastRevisionID
// - Exactly 1 new Revision is created with origin USER and summary "Update local work item"
// - Proposal resolved_at is set
// - Proposal is no longer returned in ListPending
func TestApplyProposalSuccess(t *testing.T) {
	ctx, db, wiService, proposalService := setupTestContext(t)
	desc := "Original Description"
	item := createTestWorkItem(t, ctx, wiService, "Original Title", &desc)

	var revisionsBefore int
	if err := db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM revisions").Scan(&revisionsBefore); err != nil {
		t.Fatal(err)
	}

	proposedTitle := "Applied Title"
	proposedDesc := "Applied Description"
	prop, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID:    item.ID,
		ProposedTitle:       proposedTitle,
		ProposedDescription: &proposedDesc,
	})
	if err != nil {
		t.Fatalf("propose update failed: %v", err)
	}

	res, err := proposalService.ApplyProposal(ctx, ApplyProposalInput{ID: prop.ID})
	if err != nil {
		t.Fatalf("apply proposal failed: %v", err)
	}

	if res.Outcome != ApplyOutcomeApplied {
		t.Fatalf("expected outcome APPLIED, got %s", res.Outcome)
	}
	if res.Proposal.Status != StatusApplied {
		t.Fatalf("expected proposal status APPLIED, got %s", res.Proposal.Status)
	}
	if res.Proposal.AppliedRevisionID == nil {
		t.Fatal("expected applied_revision_id to be non-nil")
	}
	if *res.Proposal.AppliedRevisionID != res.WorkItem.LastRevisionID {
		t.Fatalf("applied_revision_id %s does not match WorkItem LastRevisionID %s",
			*res.Proposal.AppliedRevisionID, res.WorkItem.LastRevisionID)
	}
	if res.Proposal.ResolvedAt == nil {
		t.Fatal("expected resolved_at to be non-nil")
	}

	// Verify WorkItem mutated
	if res.WorkItem.Title != proposedTitle {
		t.Fatalf("expected work item title %q, got %q", proposedTitle, res.WorkItem.Title)
	}
	if res.WorkItem.Description == nil || *res.WorkItem.Description != proposedDesc {
		t.Fatalf("expected work item description %q, got %v", proposedDesc, res.WorkItem.Description)
	}
	if res.WorkItem.LastRevisionID == item.LastRevisionID {
		t.Fatalf("expected new revision ID, got original %s", item.LastRevisionID)
	}

	// Verify revisions count: exactly 1 new revision created
	var revisionsAfter int
	if err := db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM revisions").Scan(&revisionsAfter); err != nil {
		t.Fatal(err)
	}
	if revisionsAfter != revisionsBefore+1 {
		t.Fatalf("expected revisions count to increase by 1: before=%d, after=%d", revisionsBefore, revisionsAfter)
	}

	// Verify revision attributes in database
	var originType string
	var summary sql.NullString
	if err := db.DB.QueryRowContext(ctx, "SELECT origin_type, summary FROM revisions WHERE id = ?", *res.Proposal.AppliedRevisionID).Scan(&originType, &summary); err != nil {
		t.Fatalf("query applied revision: %v", err)
	}
	if originType != "USER" {
		t.Fatalf("expected revision origin USER, got %q", originType)
	}
	if !summary.Valid || summary.String != "Update local work item" {
		t.Fatalf("expected revision summary %q, got %v", "Update local work item", summary)
	}

	// Verify proposal no longer in ListPending
	pending, err := proposalService.ListPending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected 0 pending proposals, got %d", len(pending))
	}

	// Verify loaded proposal via GetByID matches
	loaded, err := proposalService.GetByID(ctx, prop.ID)
	if err != nil {
		t.Fatalf("load proposal by ID: %v", err)
	}
	if loaded.Status != StatusApplied || loaded.AppliedRevisionID == nil || *loaded.AppliedRevisionID != res.WorkItem.LastRevisionID {
		t.Fatalf("unexpected loaded proposal state: %+v", loaded)
	}
}

// 19. TestApplyProposalStaleWhenPreconditionMismatch verifies that when the target
// WorkItem is mutated after the proposal is created, Apply transitions to STALE via CAS,
// does not mutate the target, and creates no new revisions.
func TestApplyProposalStaleWhenPreconditionMismatch(t *testing.T) {
	ctx, db, wiService, proposalService := setupTestContext(t)
	desc := "Original Description"
	item := createTestWorkItem(t, ctx, wiService, "Original Title", &desc)

	prop, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: item.ID,
		ProposedTitle:    "Proposed Title",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Intervening direct mutation on target WorkItem
	interveningTitle := "Intervening Title"
	mutatedWI, err := wiService.UpdateLocalWorkItemDetails(ctx, workitem.UpdateLocalWorkItemDetailsInput{
		ID:    item.ID,
		Title: interveningTitle,
	})
	if err != nil {
		t.Fatal(err)
	}

	var revisionsBefore int
	if err := db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM revisions").Scan(&revisionsBefore); err != nil {
		t.Fatal(err)
	}

	// Apply should detect stale preconditions and transition to STALE
	res, err := proposalService.ApplyProposal(ctx, ApplyProposalInput{ID: prop.ID})
	if err != nil {
		t.Fatalf("expected nil error on stale outcome, got %v", err)
	}

	if res.Outcome != ApplyOutcomeStale {
		t.Fatalf("expected outcome STALE, got %s", res.Outcome)
	}
	if res.Proposal.Status != StatusStale {
		t.Fatalf("expected proposal status STALE, got %s", res.Proposal.Status)
	}
	if res.Proposal.AppliedRevisionID != nil {
		t.Fatalf("expected applied_revision_id to be nil, got %v", res.Proposal.AppliedRevisionID)
	}
	if res.Proposal.ResolvedAt == nil {
		t.Fatal("expected resolved_at to be non-nil for STALE proposal")
	}

	// Target WorkItem must remain in its intervening state (NOT mutated to proposed)
	currentWI, err := wiService.GetByID(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if currentWI.Title != interveningTitle || currentWI.LastRevisionID != mutatedWI.LastRevisionID {
		t.Fatalf("work item was unexpectedly mutated during stale apply: %+v", currentWI)
	}

	// Revisions count must NOT have increased
	var revisionsAfter int
	if err := db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM revisions").Scan(&revisionsAfter); err != nil {
		t.Fatal(err)
	}
	if revisionsAfter != revisionsBefore {
		t.Fatalf("expected 0 new revisions on stale apply: before=%d, after=%d", revisionsBefore, revisionsAfter)
	}

	// Proposal no longer in ListPending
	pending, err := proposalService.ListPending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected 0 pending proposals, got %d", len(pending))
	}
}

// 20. TestApplyProposalStaleABARevisionProtection verifies ABA protection:
// If target WorkItem is changed and then restored to the original title and description,
// the LastRevisionID will still differ, and Apply MUST detect the mismatch and transition to STALE.
func TestApplyProposalStaleABARevisionProtection(t *testing.T) {
	ctx, db, wiService, proposalService := setupTestContext(t)
	desc := "Stable Description"
	item := createTestWorkItem(t, ctx, wiService, "Stable Title", &desc)

	prop, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: item.ID,
		ProposedTitle:    "Proposed Evolution",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Intervening mutation A -> B
	_, err = wiService.UpdateLocalWorkItemDetails(ctx, workitem.UpdateLocalWorkItemDetailsInput{
		ID:          item.ID,
		Title:       "Temporary Changed Title",
		Description: &desc,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Intervening mutation B -> A (restores title and description)
	restoredWI, err := wiService.UpdateLocalWorkItemDetails(ctx, workitem.UpdateLocalWorkItemDetailsInput{
		ID:          item.ID,
		Title:       "Stable Title",
		Description: &desc,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify values are identical to proposal preconditions but revision is different
	if restoredWI.Title != prop.Payload.Preconditions.ExpectedTitle {
		t.Fatalf("title mismatch setup: %s vs %s", restoredWI.Title, prop.Payload.Preconditions.ExpectedTitle)
	}
	if restoredWI.LastRevisionID == prop.Payload.Preconditions.ExpectedLastRevisionID {
		t.Fatal("revision ID should have changed through intervening updates")
	}

	var revisionsBefore int
	if err := db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM revisions").Scan(&revisionsBefore); err != nil {
		t.Fatal(err)
	}

	res, err := proposalService.ApplyProposal(ctx, ApplyProposalInput{ID: prop.ID})
	if err != nil {
		t.Fatalf("expected nil error on stale outcome, got: %v", err)
	}
	if res.Outcome != ApplyOutcomeStale {
		t.Fatalf("expected ABA condition to trigger STALE outcome, got %s", res.Outcome)
	}
	if res.Proposal.Status != StatusStale {
		t.Fatalf("expected status STALE, got %s", res.Proposal.Status)
	}

	var revisionsAfter int
	if err := db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM revisions").Scan(&revisionsAfter); err != nil {
		t.Fatal(err)
	}
	if revisionsAfter != revisionsBefore {
		t.Fatalf("expected no revisions created on ABA stale: before=%d, after=%d", revisionsBefore, revisionsAfter)
	}
}

// 21. TestApplyProposalStaleWhenTargetUnavailable verifies that if the target WorkItem
// is archived prior to Apply, Apply transitions the proposal to STALE and returns WorkItem = nil.
func TestApplyProposalStaleWhenTargetUnavailable(t *testing.T) {
	ctx, db, wiService, proposalService := setupTestContext(t)
	item := createTestWorkItem(t, ctx, wiService, "Active Item", nil)

	prop, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: item.ID,
		ProposedTitle:    "Updated While Active",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Archive the target entity
	nowStr := codec.FormatTime(time.Now().UTC())
	if _, err := db.DB.ExecContext(ctx, `UPDATE entities SET archived_at = ? WHERE id = ?`, nowStr, item.ID); err != nil {
		t.Fatalf("archive entity: %v", err)
	}

	res, err := proposalService.ApplyProposal(ctx, ApplyProposalInput{ID: prop.ID})
	if err != nil {
		t.Fatalf("expected nil error on stale outcome, got: %v", err)
	}
	if res.Outcome != ApplyOutcomeStale {
		t.Fatalf("expected outcome STALE, got %s", res.Outcome)
	}
	if res.Proposal.Status != StatusStale {
		t.Fatalf("expected proposal status STALE, got %s", res.Proposal.Status)
	}
	if res.WorkItem != nil {
		t.Fatalf("expected nil WorkItem when target is unavailable, got %+v", res.WorkItem)
	}
}

// 22. TestApplyProposalRollbackOnProposalFinalizationFailure verifies that if proposal
// finalization (marking APPLIED) fails, the entire transaction rolls back cleanly:
// WorkItem is not modified, no revision remains, and proposal remains PENDING.
func TestApplyProposalRollbackOnProposalFinalizationFailure(t *testing.T) {
	ctx, db, wiService, proposalService := setupTestContext(t)
	desc := "Stable Desc"
	item := createTestWorkItem(t, ctx, wiService, "Initial Title", &desc)

	prop, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: item.ID,
		ProposedTitle:    "Must Roll Back All",
	})
	if err != nil {
		t.Fatal(err)
	}

	var revisionsBefore int
	if err := db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM revisions").Scan(&revisionsBefore); err != nil {
		t.Fatal(err)
	}

	// Add trigger to abort when proposal status is updated to APPLIED
	if _, err := db.DB.ExecContext(ctx, `
		CREATE TRIGGER fail_proposal_applied
		BEFORE UPDATE OF status ON change_proposals
		WHEN NEW.status = 'APPLIED'
		BEGIN
			SELECT RAISE(ABORT, 'forced proposal applied failure');
		END;
	`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	_, err = proposalService.ApplyProposal(ctx, ApplyProposalInput{ID: prop.ID})
	if err == nil {
		t.Fatal("expected ApplyProposal to fail due to trigger")
	}

	// 1. Proposal must still be PENDING in DB
	loadedProp, getErr := proposalService.GetByID(ctx, prop.ID)
	if getErr != nil {
		t.Fatalf("load proposal: %v", getErr)
	}
	if loadedProp.Status != StatusPending {
		t.Fatalf("expected proposal to remain PENDING after rollback, got %s", loadedProp.Status)
	}
	if loadedProp.AppliedRevisionID != nil {
		t.Fatalf("expected applied_revision_id to be nil, got %v", loadedProp.AppliedRevisionID)
	}

	// 2. WorkItem must be unchanged
	loadedWI, wiErr := wiService.GetByID(ctx, item.ID)
	if wiErr != nil {
		t.Fatalf("load work item: %v", wiErr)
	}
	if loadedWI.Title != "Initial Title" || loadedWI.LastRevisionID != item.LastRevisionID {
		t.Fatalf("WorkItem mutated despite rollback: %+v", loadedWI)
	}

	// 3. Zero new revisions left behind
	var revisionsAfter int
	if err := db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM revisions").Scan(&revisionsAfter); err != nil {
		t.Fatal(err)
	}
	if revisionsAfter != revisionsBefore {
		t.Fatalf("dangling revisions left behind: before=%d, after=%d", revisionsBefore, revisionsAfter)
	}
}

// 23. TestApplyProposalRollbackOnWorkItemMutationFailure verifies that if WorkItem
// update fails, transaction rolls back: no revisions created, proposal remains PENDING.
func TestApplyProposalRollbackOnWorkItemMutationFailure(t *testing.T) {
	ctx, db, wiService, proposalService := setupTestContext(t)
	desc := "Stable Desc"
	item := createTestWorkItem(t, ctx, wiService, "Initial Title", &desc)

	prop, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: item.ID,
		ProposedTitle:    "Will Fail Mutation",
	})
	if err != nil {
		t.Fatal(err)
	}

	var revisionsBefore int
	if err := db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM revisions").Scan(&revisionsBefore); err != nil {
		t.Fatal(err)
	}

	// Add trigger to abort when work_items is updated
	if _, err := db.DB.ExecContext(ctx, `
		CREATE TRIGGER fail_work_item_update_apply
		BEFORE UPDATE ON work_items
		BEGIN
			SELECT RAISE(ABORT, 'forced work item update failure');
		END;
	`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	_, err = proposalService.ApplyProposal(ctx, ApplyProposalInput{ID: prop.ID})
	if err == nil {
		t.Fatal("expected ApplyProposal to fail due to work item update trigger")
	}

	// 1. Proposal remains PENDING
	loadedProp, getErr := proposalService.GetByID(ctx, prop.ID)
	if getErr != nil {
		t.Fatalf("load proposal: %v", getErr)
	}
	if loadedProp.Status != StatusPending {
		t.Fatalf("expected proposal to remain PENDING, got %s", loadedProp.Status)
	}

	// 2. WorkItem unchanged
	loadedWI, wiErr := wiService.GetByID(ctx, item.ID)
	if wiErr != nil {
		t.Fatalf("load work item: %v", wiErr)
	}
	if loadedWI.Title != "Initial Title" || loadedWI.LastRevisionID != item.LastRevisionID {
		t.Fatalf("WorkItem mutated despite rollback: %+v", loadedWI)
	}

	// 3. No dangling revision
	var revisionsAfter int
	if err := db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM revisions").Scan(&revisionsAfter); err != nil {
		t.Fatal(err)
	}
	if revisionsAfter != revisionsBefore {
		t.Fatalf("dangling revisions left behind: before=%d, after=%d", revisionsBefore, revisionsAfter)
	}
}

// 24. TestApplyProposalIdempotentWhenAlreadyApplied verifies that repeated Apply
// on an already APPLIED proposal returns ApplyOutcomeAlreadyApplied without mutating
// WorkItem or creating a new Revision.
func TestApplyProposalIdempotentWhenAlreadyApplied(t *testing.T) {
	ctx, db, wiService, proposalService := setupTestContext(t)
	item := createTestWorkItem(t, ctx, wiService, "First Title", nil)

	prop, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: item.ID,
		ProposedTitle:    "Applied Once",
	})
	if err != nil {
		t.Fatal(err)
	}

	// First Apply -> APPLIED
	firstRes, err := proposalService.ApplyProposal(ctx, ApplyProposalInput{ID: prop.ID})
	if err != nil {
		t.Fatalf("first apply failed: %v", err)
	}
	if firstRes.Outcome != ApplyOutcomeApplied {
		t.Fatalf("expected APPLIED outcome, got %s", firstRes.Outcome)
	}

	var revisionsAfterFirst int
	if err := db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM revisions").Scan(&revisionsAfterFirst); err != nil {
		t.Fatal(err)
	}

	// Second Apply -> ALREADY_APPLIED
	secondRes, err := proposalService.ApplyProposal(ctx, ApplyProposalInput{ID: prop.ID})
	if err != nil {
		t.Fatalf("second apply failed: %v", err)
	}
	if secondRes.Outcome != ApplyOutcomeAlreadyApplied {
		t.Fatalf("expected outcome ALREADY_APPLIED, got %s", secondRes.Outcome)
	}
	if secondRes.Proposal.Status != StatusApplied {
		t.Fatalf("expected proposal status APPLIED, got %s", secondRes.Proposal.Status)
	}
	if secondRes.WorkItem.LastRevisionID != firstRes.WorkItem.LastRevisionID {
		t.Fatalf("work item revision changed on repeat apply: %s vs %s",
			secondRes.WorkItem.LastRevisionID, firstRes.WorkItem.LastRevisionID)
	}

	var revisionsAfterSecond int
	if err := db.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM revisions").Scan(&revisionsAfterSecond); err != nil {
		t.Fatal(err)
	}
	if revisionsAfterSecond != revisionsAfterFirst {
		t.Fatalf("new revisions created on repeat apply: %d vs %d", revisionsAfterFirst, revisionsAfterSecond)
	}
}

// 25. TestApplyProposalInvalidStatusTransition verifies that applying a REJECTED
// or STALE proposal returns ErrInvalidStatusTransition.
func TestApplyProposalInvalidStatusTransition(t *testing.T) {
	ctx, _, wiService, proposalService := setupTestContext(t)
	item := createTestWorkItem(t, ctx, wiService, "Transition Target", nil)

	// Case 1: REJECTED proposal
	p1, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: item.ID,
		ProposedTitle:    "Rejected Proposal",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := proposalService.RejectProposal(ctx, RejectProposalInput{ID: p1.ID}); err != nil {
		t.Fatal(err)
	}

	_, err = proposalService.ApplyProposal(ctx, ApplyProposalInput{ID: p1.ID})
	if !errors.Is(err, ErrInvalidStatusTransition) {
		t.Fatalf("expected ErrInvalidStatusTransition for REJECTED proposal, got: %v", err)
	}

	// Case 2: STALE proposal
	p2, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: item.ID,
		ProposedTitle:    "Stale Proposal",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Cause it to become STALE
	if _, err := wiService.UpdateLocalWorkItemDetails(ctx, workitem.UpdateLocalWorkItemDetailsInput{
		ID:    item.ID,
		Title: "Direct Change To Cause Stale",
	}); err != nil {
		t.Fatal(err)
	}

	staleRes, err := proposalService.ApplyProposal(ctx, ApplyProposalInput{ID: p2.ID})
	if err != nil {
		t.Fatal(err)
	}
	if staleRes.Outcome != ApplyOutcomeStale {
		t.Fatalf("expected outcome STALE, got %s", staleRes.Outcome)
	}

	// Attempting to apply again from STALE must return ErrInvalidStatusTransition
	_, err = proposalService.ApplyProposal(ctx, ApplyProposalInput{ID: p2.ID})
	if !errors.Is(err, ErrInvalidStatusTransition) {
		t.Fatalf("expected ErrInvalidStatusTransition for STALE proposal, got: %v", err)
	}
}

// 26. TestAppliedAndStaleSurviveDatabaseReopen verifies durability of both APPLIED
// and STALE proposals across process / connection reopen.
func TestAppliedAndStaleSurviveDatabaseReopen(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	ctx := context.Background()
	db, err := database.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Migrate(db.DB); err != nil {
		t.Fatal(err)
	}

	wiRepo := workitem.NewRepository(db.DB)
	wiService := workitem.NewService(db.DB, wiRepo)
	proposalService := NewService(db.DB, wiService)

	item1, err := wiService.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: "Item 1"})
	if err != nil {
		t.Fatal(err)
	}
	item2, err := wiService.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: "Item 2"})
	if err != nil {
		t.Fatal(err)
	}

	// Proposal 1 -> APPLIED
	p1, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: item1.ID,
		ProposedTitle:    "Item 1 Applied",
	})
	if err != nil {
		t.Fatal(err)
	}
	res1, err := proposalService.ApplyProposal(ctx, ApplyProposalInput{ID: p1.ID})
	if err != nil || res1.Outcome != ApplyOutcomeApplied {
		t.Fatalf("apply p1 failed: outcome=%s, err=%v", res1.Outcome, err)
	}

	// Proposal 2 -> STALE
	p2, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: item2.ID,
		ProposedTitle:    "Item 2 Will Be Stale",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wiService.UpdateLocalWorkItemDetails(ctx, workitem.UpdateLocalWorkItemDetailsInput{
		ID:    item2.ID,
		Title: "Item 2 Intervening",
	}); err != nil {
		t.Fatal(err)
	}
	res2, err := proposalService.ApplyProposal(ctx, ApplyProposalInput{ID: p2.ID})
	if err != nil || res2.Outcome != ApplyOutcomeStale {
		t.Fatalf("apply p2 failed: outcome=%s, err=%v", res2.Outcome, err)
	}

	// Close database
	if err := db.DB.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen database
	reopenedDB, err := database.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer reopenedDB.DB.Close()

	reopenedWIRepo := workitem.NewRepository(reopenedDB.DB)
	reopenedWIService := workitem.NewService(reopenedDB.DB, reopenedWIRepo)
	reopenedProposalService := NewService(reopenedDB.DB, reopenedWIService)

	// Verify APPLIED proposal
	loadedP1, err := reopenedProposalService.GetByID(ctx, p1.ID)
	if err != nil {
		t.Fatalf("load p1 after reopen: %v", err)
	}
	if loadedP1.Status != StatusApplied {
		t.Fatalf("expected p1 status APPLIED after reopen, got %s", loadedP1.Status)
	}
	if loadedP1.AppliedRevisionID == nil || *loadedP1.AppliedRevisionID != res1.WorkItem.LastRevisionID {
		t.Fatalf("unexpected applied_revision_id on p1: %v", loadedP1.AppliedRevisionID)
	}
	if loadedP1.ResolvedAt == nil {
		t.Fatal("expected resolved_at on p1 to be non-nil")
	}

	// Verify STALE proposal
	loadedP2, err := reopenedProposalService.GetByID(ctx, p2.ID)
	if err != nil {
		t.Fatalf("load p2 after reopen: %v", err)
	}
	if loadedP2.Status != StatusStale {
		t.Fatalf("expected p2 status STALE after reopen, got %s", loadedP2.Status)
	}
	if loadedP2.AppliedRevisionID != nil {
		t.Fatalf("expected applied_revision_id on p2 to be nil, got %v", loadedP2.AppliedRevisionID)
	}
	if loadedP2.ResolvedAt == nil {
		t.Fatal("expected resolved_at on p2 to be non-nil")
	}
}

// 27. TestApplyProposalSafeErrorOnCorruptPayload verifies that Apply fails safely
// without modifying target WorkItem or advancing status if persisted operation payload is corrupt.
func TestApplyProposalSafeErrorOnCorruptPayload(t *testing.T) {
	ctx, db, wiService, proposalService := setupTestContext(t)
	item := createTestWorkItem(t, ctx, wiService, "Corrupt Apply Target", nil)
	nowStr := codec.FormatTime(time.Now().UTC())

	corruptID := "prop-corrupt-apply"
	if _, err := db.DB.ExecContext(ctx, `
		INSERT INTO change_proposals (
			id, target_entity_id, operation_type, title, status, operation_payload_json, created_at, updated_at
		) VALUES (?, ?, 'work_item.update_details', 'Title', 'PENDING', '{"bad":"payload"}', ?, ?)
	`, corruptID, item.ID, nowStr, nowStr); err != nil {
		t.Fatalf("insert corrupt row: %v", err)
	}

	_, err := proposalService.ApplyProposal(ctx, ApplyProposalInput{ID: corruptID})
	if err == nil {
		t.Fatal("expected ApplyProposal to fail on corrupt payload, got nil")
	}
	if !errors.Is(err, ErrInvalidOperationPayload) {
		t.Fatalf("expected ErrInvalidOperationPayload, got %v", err)
	}

	// Verify WorkItem is intact
	currentWI, err := wiService.GetByID(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if currentWI.Title != "Corrupt Apply Target" || currentWI.LastRevisionID != item.LastRevisionID {
		t.Fatalf("WorkItem mutated on corrupt payload apply: %+v", currentWI)
	}
}

// 28. TestProposalServiceGetSummary verifies that GetSummary returns accurate
// pending proposal counts and deduplicated affected work item counts, excluding non-pending proposals.
func TestProposalServiceGetSummary(t *testing.T) {
	ctx, _, wiService, proposalService := setupTestContext(t)

	// Initial empty state
	summary, err := proposalService.GetSummary(ctx)
	if err != nil {
		t.Fatalf("GetSummary on empty db: %v", err)
	}
	if summary.PendingCount != 0 || summary.AffectedWorkItemCount != 0 {
		t.Fatalf("expected (0, 0), got (%d, %d)", summary.PendingCount, summary.AffectedWorkItemCount)
	}

	// Create 3 work items
	item1 := createTestWorkItem(t, ctx, wiService, "Work Item 1", nil)
	item2 := createTestWorkItem(t, ctx, wiService, "Work Item 2", nil)
	item3 := createTestWorkItem(t, ctx, wiService, "Work Item 3", nil)

	// Item 1 has 2 pending proposals
	p1, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: item1.ID,
		ProposedTitle:    "WI1 Proposal 1",
	})
	if err != nil {
		t.Fatalf("propose p1: %v", err)
	}
	_, err = proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: item1.ID,
		ProposedTitle:    "WI1 Proposal 2",
	})
	if err != nil {
		t.Fatalf("propose p2: %v", err)
	}

	// Item 2 has 1 pending proposal
	p3, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: item2.ID,
		ProposedTitle:    "WI2 Proposal 1",
	})
	if err != nil {
		t.Fatalf("propose p3: %v", err)
	}

	// Item 3 has 1 proposal that will be rejected
	p4, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: item3.ID,
		ProposedTitle:    "WI3 Proposal 1",
	})
	if err != nil {
		t.Fatalf("propose p4: %v", err)
	}

	// Verify before resolving: 4 pending proposals across 3 unique work items
	summary, err = proposalService.GetSummary(ctx)
	if err != nil {
		t.Fatalf("GetSummary: %v", err)
	}
	if summary.PendingCount != 4 {
		t.Fatalf("expected PendingCount 4, got %d", summary.PendingCount)
	}
	if summary.AffectedWorkItemCount != 3 {
		t.Fatalf("expected AffectedWorkItemCount 3, got %d", summary.AffectedWorkItemCount)
	}

	// Apply p1 on item 1
	applyRes, err := proposalService.ApplyProposal(ctx, ApplyProposalInput{ID: p1.ID})
	if err != nil || applyRes.Outcome != ApplyOutcomeApplied {
		t.Fatalf("apply p1: %v, outcome=%s", err, applyRes.Outcome)
	}

	// Reject p4 on item 3
	if _, err := proposalService.RejectProposal(ctx, RejectProposalInput{ID: p4.ID}); err != nil {
		t.Fatalf("reject p4: %v", err)
	}

	// Now: item 1 has 1 pending proposal (WI1 Proposal 2 is now stale or pending depending on preconditions)
	// Let's check summary:
	// item 1 still has WI1 Proposal 2 (still pending until evaluated), item 2 has p3 (pending).
	// Total pending: 2. Unique affected items: item 1 and item 2 = 2.
	summary, err = proposalService.GetSummary(ctx)
	if err != nil {
		t.Fatalf("GetSummary after apply and reject: %v", err)
	}
	if summary.PendingCount != 2 {
		t.Fatalf("expected PendingCount 2, got %d", summary.PendingCount)
	}
	if summary.AffectedWorkItemCount != 2 {
		t.Fatalf("expected AffectedWorkItemCount 2, got %d", summary.AffectedWorkItemCount)
	}

	// Reject p3 on item 2
	if _, err := proposalService.RejectProposal(ctx, RejectProposalInput{ID: p3.ID}); err != nil {
		t.Fatalf("reject p3: %v", err)
	}

	summary, err = proposalService.GetSummary(ctx)
	if err != nil {
		t.Fatalf("GetSummary after reject p3: %v", err)
	}
	if summary.PendingCount != 1 {
		t.Fatalf("expected PendingCount 1, got %d", summary.PendingCount)
	}
	if summary.AffectedWorkItemCount != 1 {
		t.Fatalf("expected AffectedWorkItemCount 1, got %d", summary.AffectedWorkItemCount)
	}
}

func TestProposeWithExpectedBaseCreatesWhenTargetStillMatches(t *testing.T) {
	ctx, _, wiService, proposalService := setupTestContext(t)
	desc := "Base description"
	item := createTestWorkItem(t, ctx, wiService, "Base title", &desc)

	prop, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: item.ID,
		ProposedTitle:    "New title",
		ExpectedBase:     &ExpectedWorkItemBase{LastRevisionID: item.LastRevisionID, Title: item.Title, Description: item.Description},
	})
	if err != nil {
		t.Fatalf("expected checked creation to succeed: %v", err)
	}
	pre := prop.Payload.Preconditions
	if pre.ExpectedLastRevisionID != item.LastRevisionID || pre.ExpectedTitle != "Base title" || pre.ExpectedDescription == nil || *pre.ExpectedDescription != desc {
		t.Fatalf("preconditions must equal the verified base: %+v", pre)
	}
}

func TestProposeWithExpectedBaseRefusesChangedTargetAndCreatesNothing(t *testing.T) {
	ctx, db, wiService, proposalService := setupTestContext(t)
	desc := "Base description"
	item := createTestWorkItem(t, ctx, wiService, "Base title", &desc)
	base := &ExpectedWorkItemBase{LastRevisionID: item.LastRevisionID, Title: item.Title, Description: item.Description}

	if _, err := wiService.UpdateLocalWorkItemDetails(ctx, workitem.UpdateLocalWorkItemDetailsInput{ID: item.ID, Title: "Changed", Description: &desc}); err != nil {
		t.Fatal(err)
	}
	_, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{TargetWorkItemID: item.ID, ProposedTitle: "New title", ExpectedBase: base})
	if !errors.Is(err, ErrBaseChanged) {
		t.Fatalf("expected ErrBaseChanged, got %v", err)
	}
	var count int
	if err := db.DB.QueryRow("SELECT COUNT(*) FROM change_proposals").Scan(&count); err != nil || count != 0 {
		t.Fatalf("no Proposal may exist after a refused creation: count=%d err=%v", count, err)
	}
}

func TestProposeWithExpectedBaseChecksEveryBaseFact(t *testing.T) {
	ctx, _, wiService, proposalService := setupTestContext(t)
	desc := "Base description"
	item := createTestWorkItem(t, ctx, wiService, "Base title", &desc)
	other := "Different"
	for name, base := range map[string]*ExpectedWorkItemBase{
		"revision":        {LastRevisionID: "other", Title: item.Title, Description: item.Description},
		"title":           {LastRevisionID: item.LastRevisionID, Title: "Other", Description: item.Description},
		"description":     {LastRevisionID: item.LastRevisionID, Title: item.Title, Description: &other},
		"description nil": {LastRevisionID: item.LastRevisionID, Title: item.Title, Description: nil},
	} {
		_, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{TargetWorkItemID: item.ID, ProposedTitle: "X", ExpectedBase: base})
		if !errors.Is(err, ErrBaseChanged) {
			t.Fatalf("%s mismatch must be refused, got %v", name, err)
		}
	}
}

func TestProposeWithExpectedBaseStillReportsUnavailableTarget(t *testing.T) {
	ctx, _, _, proposalService := setupTestContext(t)
	_, err := proposalService.ProposeWorkItemUpdate(ctx, ProposeWorkItemUpdateInput{
		TargetWorkItemID: "missing", ProposedTitle: "X",
		ExpectedBase: &ExpectedWorkItemBase{LastRevisionID: "r", Title: "t"},
	})
	if !errors.Is(err, ErrTargetUnavailable) {
		t.Fatalf("expected ErrTargetUnavailable, got %v", err)
	}
}
