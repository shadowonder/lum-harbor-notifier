package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"database/sql"
	"time"

	"lum-harbor-notifier/internal/assistant"
	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/calendar"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/plannerview"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/proposalcap"
	"lum-harbor-notifier/internal/reminder"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

func newRegistryTestApp(t *testing.T) *App {
	t.Helper()
	application, _ := newRegistryTestAppWithDB(t)
	return application
}

// newRegistryTestAppWithDB also returns the underlying database for tests that
// assert persisted truth directly.
func newRegistryTestAppWithDB(t *testing.T) (*App, *sql.DB) {
	t.Helper()
	application, service, db := newWorkItemApp(t)
	trackingService := tracking.NewService(db, service)
	plannerService := planner.NewService(db, planner.NewRepository(db))
	plannerViewService := plannerview.NewService(plannerService, calendar.NewRepository(db))
	proposalService := proposal.NewService(db, service)
	reminderService := reminder.NewService(db)
	attentionService := attention.NewService(db, trackingService)
	historyService := history.NewService(history.NewRepository(db))

	proposalService.SetTrackingOperator(trackingService)
	proposalService.SetPlannerOperator(plannerService)
	proposalService.SetReminderOperator(reminderService)

	plannerService.AddCompletionHook(func(ctx context.Context, tx *sql.Tx, plannerItemID, revisionID string, now time.Time) error {
		if err := reminderService.SatisfyReminderTx(ctx, tx, plannerItemID, revisionID, now); err != nil {
			return err
		}
		return attentionService.ResolveByPlannerItemTx(ctx, tx, plannerItemID, revisionID, now)
	})

	application.trackingService = trackingService
	application.plannerService = plannerService
	application.plannerViewService = plannerViewService
	application.proposalService = proposalService
	application.reminderService = reminderService
	application.attentionService = attentionService
	application.historyService = historyService
	return application, db
}

// --- generic registry parity ------------------------------------------------

// TestProposeRegistryParity proves every advertised capability has exactly
// one handler (ListDescriptors and the internal entries map always agree by
// construction) and that duplicate registration fails closed.
func TestProposeRegistryParity(t *testing.T) {
	registry := NewProposeRegistry()
	descriptors := registry.ListDescriptors()
	if len(descriptors) != len(registry.entries) {
		t.Fatalf("descriptor count %d does not match registered handler count %d", len(descriptors), len(registry.entries))
	}
	seen := map[string]bool{}
	for _, d := range descriptors {
		if seen[d.ID] {
			t.Fatalf("duplicate descriptor ID %q", d.ID)
		}
		seen[d.ID] = true
		entry, ok := registry.entries[d.ID]
		if !ok || entry.handler == nil {
			t.Fatalf("descriptor %q has no handler", d.ID)
		}
	}
	// 7 mature M16 operations + 3 M16.5 operations + 4 M19 Round 4 product control operations + 1 compound scaffolding operation
	// + 1 EXTERNAL Planner local note operation.
	if len(descriptors) != 16 {
		t.Fatalf("expected 16 registered capabilities, got %d: %+v", len(descriptors), descriptors)
	}
}

func TestProposeCatalogExplainsConsequencesAndAvoidanceForEveryCapability(t *testing.T) {
	descriptors := NewProposeRegistry().ListDescriptors()
	for _, descriptor := range descriptors {
		if !strings.Contains(descriptor.Summary, "Propose") {
			t.Errorf("capability %q summary does not describe reviewable proposal intent", descriptor.ID)
		}
		if !strings.Contains(descriptor.UseWhen, "Do not") {
			t.Errorf("capability %q does not state an important non-use boundary", descriptor.ID)
		}
	}

	byID := make(map[string]proposalcap.Descriptor, len(descriptors))
	for _, descriptor := range descriptors {
		byID[descriptor.ID] = descriptor
		if strings.Contains(descriptor.ID, "attention") || strings.Contains(descriptor.ID, "notification") {
			t.Errorf("derived Attention/Notification must not be exposed as Proposal capability %q", descriptor.ID)
		}
	}
	plannerDescriptor := byID[proposal.OperationTypePlannerItemCreate]
	plannerSemantics := plannerDescriptor.Summary + " " + plannerDescriptor.UseWhen
	for _, marker := range []string{"reminder configuration", "background evaluation", "Attention and Notification", "Do not create separate"} {
		if !strings.Contains(plannerSemantics, marker) {
			t.Errorf("planner item descriptor does not explain %q", marker)
		}
	}
	workflowSemantics := byID[proposal.OperationTypeWorkflowSetCurrentStep].Summary
	trackedSemantics := byID[proposal.OperationTypeTrackedItemStageState].Summary
	if !strings.Contains(workflowSemantics, "does not alter Tracked Item") || !strings.Contains(trackedSemantics, "never moves the Workflow") {
		t.Fatal("Workflow and Tracked Item stage descriptors must preserve independent state ownership")
	}
}

func TestCatalogDoesNotClaimUnsupportedWorkflowTrackedItemReminderCompound(t *testing.T) {
	for _, descriptor := range NewProposeRegistry().ListDescriptors() {
		_, hasTrackedTarget := descriptor.Arguments["target_item_title"]
		_, hasReminderOffset := descriptor.Arguments["reminder_offset_minutes"]
		if hasTrackedTarget && hasReminderOffset {
			t.Fatalf("capability %q falsely combines tracked-item mutation and reminder configuration", descriptor.ID)
		}
	}
}

func TestProposeRegistryRejectsDuplicateRegistration(t *testing.T) {
	registry := &ProposeRegistry{entries: map[string]proposeCapabilityRegistration{}}
	registry.register(workItemUpdateDetailsCapability())
	defer func() {
		if recover() == nil {
			t.Fatal("expected duplicate registration to panic (fail closed at construction, not at runtime)")
		}
	}()
	registry.register(workItemUpdateDetailsCapability())
}

func TestProposeRegistryUnknownCapabilityFailsClosed(t *testing.T) {
	application := newRegistryTestApp(t)
	_, err := application.proposeRegistry.Execute(context.Background(), application, "not.a.real.capability", nil, json.RawMessage(`{}`), nil)
	if !errors.Is(err, errUnknownCapability) {
		t.Fatalf("expected errUnknownCapability, got %v", err)
	}
}

func TestProposeRegistryBadArgumentsFailClosed(t *testing.T) {
	application := newRegistryTestApp(t)
	item, err := application.workitemService.CreateLocalWorkItem(context.Background(), workitem.CreateLocalWorkItemInput{Title: "Target"})
	if err != nil {
		t.Fatal(err)
	}
	base := assistantSnapshotFor(t, application, item.ID)
	for name, raw := range map[string]string{
		"blank proposed_title": `{"proposed_title":"   "}`,
		"unknown argument":     `{"proposed_title":"x","bogus_field":true}`,
		"wrong type":           `{"proposed_title":123}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := application.proposeRegistry.Execute(context.Background(), application, "work_item.update_details", &base, json.RawMessage(raw), nil)
			if !errors.Is(err, errInvalidArguments) {
				t.Fatalf("expected errInvalidArguments, got %v", err)
			}
		})
	}
}

// TestWorkItemUpdateDetailsToleratesHarmlessWorkItemIDArgument locks in a real
// finding from live-model acceptance testing: a model can generalize from
// other capabilities' explicit work_item_id argument and supply one on
// work_item.update_details too, even though that capability always targets
// the authoritative base Work Item. The descriptor declares work_item_id as
// optional and the handler ignores it (never trusts a model-supplied ID
// regardless), so this must succeed rather than fail closed as an unknown
// argument.
func TestWorkItemUpdateDetailsToleratesHarmlessWorkItemIDArgument(t *testing.T) {
	application := newRegistryTestApp(t)
	item, err := application.workitemService.CreateLocalWorkItem(context.Background(), workitem.CreateLocalWorkItemInput{Title: "Payments Modernization Program"})
	if err != nil {
		t.Fatal(err)
	}
	base := assistantSnapshotFor(t, application, item.ID)
	created, err := application.proposeRegistry.Execute(context.Background(), application, "work_item.update_details", &base,
		json.RawMessage(`{"proposed_title":"Payments Modernization Program V2","work_item_id":"`+item.ID+`"}`), nil)
	if err != nil {
		t.Fatalf("expected the harmless work_item_id argument to be tolerated, got %v", err)
	}
	if created.TargetEntityID == nil || *created.TargetEntityID != item.ID {
		t.Fatalf("target must still come from the authoritative base, never the argument: %+v", created)
	}
}

// --- work_item.update_details: no-change dedup (domain knowledge now owned
// by this handler, not internal/assistant) -------------------------------

func TestWorkItemUpdateDetailsNoChangeDedup(t *testing.T) {
	application := newRegistryTestApp(t)
	desc := "Original description"
	item, err := application.workitemService.CreateLocalWorkItem(context.Background(), workitem.CreateLocalWorkItemInput{Title: "Same Title", Description: &desc})
	if err != nil {
		t.Fatal(err)
	}
	base := assistantSnapshotFor(t, application, item.ID)
	_, err = application.proposeRegistry.Execute(context.Background(), application, "work_item.update_details", &base, json.RawMessage(`{"proposed_title":"Same Title","proposed_description":"Original description"}`), nil)
	if !errors.Is(err, errCapabilityNoChange) {
		t.Fatalf("expected errCapabilityNoChange, got %v", err)
	}
}

// --- context is a hint, not permission --------------------------------------

// TestContextFreeCapabilityWorksWithExplicitTargetAndNoSelection proves a
// capability with a stable target ID argument (workitem.set_lifecycle) works
// with base == nil, exactly as it would from any other page - the handler
// authoritative-loads/revalidates the target itself.
func TestContextFreeCapabilityWorksWithExplicitTargetAndNoSelection(t *testing.T) {
	application := newRegistryTestApp(t)
	item, err := application.workitemService.CreateLocalWorkItem(context.Background(), workitem.CreateLocalWorkItemInput{Title: "Payments Modernization Program"})
	if err != nil {
		t.Fatal(err)
	}
	created, err := application.proposeRegistry.Execute(context.Background(), application, "workitem.set_lifecycle", nil, json.RawMessage(`{"work_item_id":"`+item.ID+`","target_state":"PAUSED"}`), nil)
	if err != nil {
		t.Fatalf("expected success with nil base (context is a hint, not permission): %v", err)
	}
	if created.TargetEntityID == nil || *created.TargetEntityID != item.ID {
		t.Fatalf("unexpected target: %+v", created)
	}
}

// TestTrackedItemCapabilityLoadsAuthoritativeStateNotPageContext proves the
// legacy Tracking handlers now authoritative-load their own Tracking view
// from work_item_id rather than depending on a UI-selected base.Tracking
// snapshot.
func TestTrackedItemCapabilityLoadsAuthoritativeStateNotPageContext(t *testing.T) {
	application := newRegistryTestApp(t)
	ctx := context.Background()
	item, err := application.workitemService.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{Title: "Some Project"})
	if err != nil {
		t.Fatal(err)
	}
	def, err := application.trackingService.CreateWorkflowDefinition(ctx, tracking.CreateWorkflowDefinitionInput{Name: "Delivery", Version: 1, Steps: []tracking.WorkflowStepInput{{Key: "plan", Title: "Plan"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := application.trackingService.CreateWorkflow(ctx, tracking.CreateWorkflowInput{WorkItemID: item.ID, WorkflowDefinitionID: def.ID}); err != nil {
		t.Fatal(err)
	}
	// base is nil (no page selection); the handler must still resolve
	// work_item_id itself.
	created, err := application.proposeRegistry.Execute(ctx, application, "tracked_item.create", nil, json.RawMessage(`{"work_item_id":"`+item.ID+`","title":"API"}`), nil)
	if err != nil {
		t.Fatalf("expected success without page context: %v", err)
	}
	if created.OperationType != proposal.OperationTypeTrackedItemCreate {
		t.Fatalf("unexpected operation: %+v", created)
	}
}

// assistantSnapshotFor loads a minimal base snapshot the same way
// app/assistant.go's assistantWorkItemReader would, for tests exercising
// RequiresWorkItemContext:true capabilities.
func assistantSnapshotFor(t *testing.T, application *App, workItemID string) assistant.WorkItemSnapshot {
	t.Helper()
	reader := assistantWorkItemReader{service: application.workitemService, tracking: application.trackingService}
	result, err := reader.ReadWorkItem(context.Background(), workItemID)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestWorkItemCreateWithWorkflowRegistryExecution(t *testing.T) {
	application := newRegistryTestApp(t)
	ctx := context.Background()

	// 1. Success execution
	inputJSON := `{"title":"F13456752","workflow_name":"Deploy Pipeline","stages":["define","develop","test","stage","adoption","production"]}`
	prop, err := application.proposeRegistry.Execute(ctx, application, proposal.OperationTypeWorkItemCreateWithWorkflow, nil, json.RawMessage(inputJSON), nil)
	if err != nil {
		t.Fatalf("expected successful execution: %v", err)
	}
	if prop.OperationType != proposal.OperationTypeWorkItemCreateWithWorkflow {
		t.Fatalf("unexpected operation type: %s", prop.OperationType)
	}
	if prop.WorkItemCreateWithWorkflowPayload == nil {
		t.Fatal("expected non-nil WorkItemCreateWithWorkflowPayload")
	}
	if len(prop.WorkItemCreateWithWorkflowPayload.Stages) != 6 {
		t.Fatalf("expected 6 stages, got %d", len(prop.WorkItemCreateWithWorkflowPayload.Stages))
	}

	// 2. Missing title fails closed
	badTitleJSON := `{"workflow_name":"Deploy Pipeline","stages":["define","develop"]}`
	_, err = application.proposeRegistry.Execute(ctx, application, proposal.OperationTypeWorkItemCreateWithWorkflow, nil, json.RawMessage(badTitleJSON), nil)
	if !errors.Is(err, errInvalidArguments) {
		t.Fatalf("expected errInvalidArguments on missing title, got %v", err)
	}

	// 3. Empty stages fails closed
	emptyStagesJSON := `{"title":"F13456752","stages":[]}`
	_, err = application.proposeRegistry.Execute(ctx, application, proposal.OperationTypeWorkItemCreateWithWorkflow, nil, json.RawMessage(emptyStagesJSON), nil)
	if !errors.Is(err, errInvalidArguments) {
		t.Fatalf("expected errInvalidArguments on empty stages, got %v", err)
	}

	// 4. Apply through application proposalService
	applyRes, err := application.proposalService.ApplyProposal(ctx, proposal.ApplyProposalInput{ID: prop.ID})
	if err != nil {
		t.Fatalf("apply proposal failed: %v", err)
	}
	if applyRes.Outcome != proposal.ApplyOutcomeApplied {
		t.Fatalf("expected ApplyOutcomeApplied, got %v", applyRes.Outcome)
	}
	if applyRes.WorkItem == nil || applyRes.WorkItem.Title != "F13456752" {
		t.Fatalf("unexpected applied work item: %+v", applyRes.WorkItem)
	}
}
