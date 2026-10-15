package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/ailineage"
	"lum-harbor-notifier/internal/assistant"
	"lum-harbor-notifier/internal/attention"
	"lum-harbor-notifier/internal/history"
	"lum-harbor-notifier/internal/identity"
	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/readcap"
	"lum-harbor-notifier/internal/source"
	"lum-harbor-notifier/internal/testsupport"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
	"lum-harbor-notifier/internal/workitemoverview"
)

type aiProductControlFixture struct {
	t           *testing.T
	db          *sql.DB
	app         *App
	workitemSvc *workitem.Service
	trackingSvc *tracking.Service
	proposalSvc *proposal.Service
	historySvc  *history.Service
	plannerSvc  *planner.Service
	sourceSvc   *source.Service
	executor    *scriptedAIExecutor
	engine      *assistant.RuntimeAdapter
	registry    *ProposeRegistry
}

func newAIProductControlFixture(t *testing.T) *aiProductControlFixture {
	t.Helper()
	dbConn := testsupport.OpenTestDatabase(t)
	db := dbConn.DB

	workitemRepo := workitem.NewRepository(db)
	workitemSvc := workitem.NewService(db, workitemRepo)
	trackingSvc := tracking.NewService(db, workitemSvc)
	plannerRepo := planner.NewRepository(db)
	plannerSvc := planner.NewService(db, plannerRepo)
	sourceSvc := source.NewService(db)
	historySvc := history.NewService(history.NewRepository(db))

	proposalSvc := proposal.NewService(db, workitemSvc)
	proposalSvc.SetTrackingOperator(trackingSvc)
	proposalSvc.SetPlannerOperator(plannerSvc)

	readcapSvc, err := readcap.NewService(workitemSvc, trackingSvc, plannerSvc, db, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	executor := &scriptedAIExecutor{
		outputs: map[ai.Action]string{
			ai.ActionRuntimeCapabilityDecision: `{"version":1,"decision":"FINAL","calls":[]}`,
		},
	}

	registry := NewProposeRegistry()
	engine, err := assistant.NewNativeRuntimeAdapter(executor, readcapSvc, registry)
	if err != nil {
		t.Fatal(err)
	}

	lineageSvc := ailineage.NewService(db)
	attentionSvc := attention.NewService(db, trackingSvc)
	overviewSvc := workitemoverview.NewService(workitemSvc, trackingSvc, attentionSvc, plannerSvc)

	app := &App{
		workitemService:         workitemSvc,
		trackingService:         trackingSvc,
		proposalService:         proposalSvc,
		historyService:          historySvc,
		plannerService:          plannerSvc,
		sourceService:           sourceSvc,
		lineageService:          lineageSvc,
		workItemOverviewService: overviewSvc,
		proposeRegistry:         registry,
		aiExecutor:              executor,
		assistantRuntime:        engine,
	}

	return &aiProductControlFixture{
		t:           t,
		db:          db,
		app:         app,
		workitemSvc: workitemSvc,
		trackingSvc: trackingSvc,
		proposalSvc: proposalSvc,
		historySvc:  historySvc,
		plannerSvc:  plannerSvc,
		sourceSvc:   sourceSvc,
		executor:    executor,
		engine:      engine,
		registry:    registry,
	}
}

// Requirement A, J & K: WorkItem create proposal lifecycle, immutability, and provenance convergence
func TestAIProductControl_WorkItemCreate_LifecycleAndProvenance(t *testing.T) {
	f := newAIProductControlFixture(t)
	ctx := context.Background()

	// 1. Assistant user turn captures real intent source
	reqID, _ := identity.New()
	sourceRev, err := f.sourceSvc.CaptureIntentSource(ctx, source.CaptureIntentInput{
		ExtensionID:    "core.assistant",
		InstanceID:     "direct-turn",
		CapabilityID:   "assistant.turn",
		SourceKind:     "USER_INTENT",
		ExternalID:     reqID,
		Payload:        []byte(`{"message":"Create project Alpha Release"}`),
		ProcessingMode: source.ProcessingModeSemanticProposal,
	})
	if err != nil {
		t.Fatal(err)
	}

	runID, _ := identity.New()
	procID, _ := identity.New()
	err = f.app.lineageService.Start(ctx, ailineage.StartRun{
		ID:               runID,
		ProcessingID:     procID,
		RequestID:        reqID,
		Action:           ai.ActionAssistantAsk,
		CanonicalRequest: []byte(`{"message":"Create project Alpha Release"}`),
		SourceRevisionID: sourceRev.ID,
		StartedAt:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}

	provCtx := ContextWithProposeProvenance(ctx, ProposeProvenance{
		AIRunID:           &runID,
		SourceRevisionIDs: []string{sourceRev.ID},
	})

	// 2. Assistant handler creates Proposal
	args := map[string]any{
		"title":       "Alpha Release",
		"description": "Preparing our October launch",
	}
	rawArgs, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}

	reason := "User requested project creation"
	prop, err := f.registry.Execute(provCtx, f.app, "work_item.create", nil, rawArgs, &reason)
	if err != nil {
		t.Fatalf("Execute work_item.create: %v", err)
	}

	// 3. Proposal is PENDING, target_entity_id == nil
	if prop.Status != proposal.StatusPending {
		t.Fatalf("expected status PENDING, got %s", prop.Status)
	}
	if prop.TargetEntityID != nil {
		t.Fatalf("expected nil target_entity_id before apply, got %v", prop.TargetEntityID)
	}
	if prop.AppliedRevisionID != nil {
		t.Fatalf("expected nil AppliedRevisionID before apply, got %v", prop.AppliedRevisionID)
	}

	// 4. Verify AI Run and Proposal link the Assistant SourceRevision S
	var runSourceRevID string
	err = f.db.QueryRow("SELECT source_revision_id FROM ai_run_sources WHERE ai_run_id = ?", runID).Scan(&runSourceRevID)
	if err != nil {
		t.Fatalf("query ai_run_sources: %v", err)
	}
	if runSourceRevID != sourceRev.ID {
		t.Fatalf("expected ai_run_sources to link %s, got %s", sourceRev.ID, runSourceRevID)
	}

	var propSourceRevID string
	err = f.db.QueryRow("SELECT source_revision_id FROM change_proposal_sources WHERE proposal_id = ?", prop.ID).Scan(&propSourceRevID)
	if err != nil {
		t.Fatalf("query change_proposal_sources: %v", err)
	}
	if propSourceRevID != sourceRev.ID {
		t.Fatalf("expected change_proposal_sources to link %s, got %s", sourceRev.ID, propSourceRevID)
	}

	// 5. WorkItem table does NOT contain the proposed item yet (zero domain truth)
	var count int
	err = f.db.QueryRow("SELECT COUNT(*) FROM work_items WHERE title = 'Alpha Release'").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("work_items table contains %d items before apply; expected 0", count)
	}

	var entityCount int
	err = f.db.QueryRow("SELECT COUNT(*) FROM entities WHERE entity_type = 'WORK_ITEM'").Scan(&entityCount)
	if err != nil {
		t.Fatal(err)
	}
	if entityCount != 0 {
		t.Fatalf("entities table contains %d work items before apply; expected 0", entityCount)
	}

	// Total source revisions before apply
	var sourceCountBefore int
	err = f.db.QueryRow("SELECT COUNT(*) FROM source_revisions").Scan(&sourceCountBefore)
	if err != nil {
		t.Fatal(err)
	}

	// 6. Apply creates the WorkItem
	applyRes, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: prop.ID})
	if err != nil {
		t.Fatalf("ApplyProposal: %v", err)
	}
	if applyRes.Outcome != "APPLIED" {
		t.Fatalf("expected outcome APPLIED, got %s", applyRes.Outcome)
	}
	if applyRes.WorkItem == nil {
		t.Fatal("expected non-nil WorkItem in apply result")
	}
	if applyRes.WorkItem.Title != "Alpha Release" {
		t.Fatalf("expected title Alpha Release, got %q", applyRes.WorkItem.Title)
	}

	// 7. Proposal becomes APPLIED, target_entity_id points to created WorkItem
	appliedProp, err := f.app.GetProposal(prop.ID)
	if err != nil {
		t.Fatalf("GetProposal: %v", err)
	}
	if appliedProp.Status != "APPLIED" {
		t.Fatalf("expected applied proposal status APPLIED, got %s", appliedProp.Status)
	}
	if appliedProp.TargetWorkItemID == nil || *appliedProp.TargetWorkItemID != applyRes.WorkItem.ID {
		t.Fatalf("expected targetWorkItemId %q, got %v", applyRes.WorkItem.ID, appliedProp.TargetWorkItemID)
	}

	// 8. Authoritative WorkItem exists
	wi, err := f.workitemSvc.GetByID(ctx, applyRes.WorkItem.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if wi.Title != "Alpha Release" {
		t.Fatalf("expected title Alpha Release, got %s", wi.Title)
	}
	if wi.Description == nil || *wi.Description != "Preparing our October launch" {
		t.Fatalf("expected description 'Preparing our October launch', got %v", wi.Description)
	}

	// 9. Provenance Convergence: Initial domain Revision links to exact Assistant SourceRevision
	var linkedSourceRevID, linkRole string
	err = f.db.QueryRow("SELECT source_revision_id, role FROM revision_sources WHERE revision_id = ?", wi.LastRevisionID).Scan(&linkedSourceRevID, &linkRole)
	if err != nil {
		t.Fatalf("query revision_sources for work item revision: %v", err)
	}
	if linkedSourceRevID != sourceRev.ID {
		t.Fatalf("expected revision_sources to link exact Assistant SourceRevision %s, got %s", sourceRev.ID, linkedSourceRevID)
	}
	if linkRole != "PRIMARY_SOURCE" {
		t.Fatalf("expected revision_sources role PRIMARY_SOURCE, got %s", linkRole)
	}

	// Zero fake sources: NO new SourceRevision was created during Apply
	var sourceCountAfter int
	err = f.db.QueryRow("SELECT COUNT(*) FROM source_revisions").Scan(&sourceCountAfter)
	if err != nil {
		t.Fatal(err)
	}
	if sourceCountAfter != sourceCountBefore {
		t.Fatalf("expected source_revisions count to remain %d (zero fake sources created at apply), got %d", sourceCountBefore, sourceCountAfter)
	}

	// 10. History record exists for WorkItem creation
	page, err := f.historySvc.QueryWorkItem(ctx, wi.ID)
	if err != nil {
		t.Fatalf("QueryWorkItem: %v", err)
	}
	foundCreateEvent := false
	for _, ev := range page.Events {
		if ev.Type == history.EventWorkItemCreated {
			foundCreateEvent = true
			if ev.Actor != history.ActorUser {
				t.Fatalf("expected WORK_ITEM_CREATED actor %s, got %s", history.ActorUser, ev.Actor)
			}
			break
		}
	}
	if !foundCreateEvent {
		t.Fatal("expected work_item.created event in history")
	}

	// 11. Proposal immutability (Requirement J):
	// Re-applying an APPLIED proposal returns ALREADY_APPLIED
	reapplyRes, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: prop.ID})
	if err != nil {
		t.Fatalf("re-apply error: %v", err)
	}
	if reapplyRes.Outcome != "ALREADY_APPLIED" {
		t.Fatalf("expected outcome ALREADY_APPLIED on reapply, got %s", reapplyRes.Outcome)
	}

	// Rejected proposals cannot be applied
	prop2, err := f.registry.Execute(provCtx, f.app, "work_item.create", nil, rawArgs, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.app.RejectProposal(RejectProposalRequestDTO{ID: prop2.ID})
	if err != nil {
		t.Fatalf("RejectProposal: %v", err)
	}
	_, err = f.app.ApplyProposal(ApplyProposalRequestDTO{ID: prop2.ID})
	if err == nil {
		t.Fatal("expected error applying rejected proposal, got nil")
	}
}

// Requirement B: Target entity constraint
func TestAIProductControl_DatabaseConstraints_TargetEntityID(t *testing.T) {
	f := newAIProductControlFixture(t)
	ctx := context.Background()

	id1, _ := identity.New()
	id2, _ := identity.New()
	id3, _ := identity.New()
	id4, _ := identity.New()
	dummyEntity, _ := identity.New()
	now := time.Now().UTC().Format(time.RFC3339Nano)

	// 1. work_item.create with NULL target_entity_id must SUCCEED
	_, err := f.db.ExecContext(ctx, `
		INSERT INTO change_proposals (
			id, target_entity_id, operation_type, title, status, operation_payload_json, created_at, updated_at
		) VALUES (?, NULL, 'work_item.create', 'Root WI', 'PENDING', '{"version":1,"title":"Root WI"}', ?, ?)
	`, id1, now, now)
	if err != nil {
		t.Fatalf("expected work_item.create with NULL target_entity_id to succeed, got %v", err)
	}

	// 2. planner.item.create with NULL target_entity_id must SUCCEED
	_, err = f.db.ExecContext(ctx, `
		INSERT INTO change_proposals (
			id, target_entity_id, operation_type, title, status, operation_payload_json, created_at, updated_at
		) VALUES (?, NULL, 'planner.item.create', 'Todo', 'PENDING', '{"version":1,"title":"Todo"}', ?, ?)
	`, id2, now, now)
	if err != nil {
		t.Fatalf("expected planner.item.create with NULL target_entity_id to succeed, got %v", err)
	}

	// 3. work_item.update_details with NULL target_entity_id must FAIL constraint
	_, err = f.db.ExecContext(ctx, `
		INSERT INTO change_proposals (
			id, target_entity_id, operation_type, title, status, operation_payload_json, created_at, updated_at
		) VALUES (?, NULL, 'work_item.update_details', 'New', 'PENDING', '{"version":1,"proposed_title":"New"}', ?, ?)
	`, id3, now, now)
	if err == nil {
		t.Fatal("expected work_item.update_details with NULL target_entity_id to fail constraint, but it succeeded")
	}

	// 4. tracking.workflow_set_current_step with NULL target_entity_id must FAIL constraint
	_, err = f.db.ExecContext(ctx, `
		INSERT INTO change_proposals (
			id, target_entity_id, operation_type, title, status, operation_payload_json, created_at, updated_at
		) VALUES (?, NULL, 'tracking.workflow_set_current_step', 'Step', 'PENDING', '{"version":1,"target_step_id":"step1"}', ?, ?)
	`, id4, now, now)
	if err == nil {
		t.Fatal("expected tracking.workflow_set_current_step with NULL target_entity_id to fail constraint, but it succeeded")
	}

	// 5. Non-root operation with valid target_entity_id must SUCCEED
	id5, _ := identity.New()
	_, err = f.db.ExecContext(ctx, `
		INSERT INTO entities (id, entity_type, created_at) VALUES (?, 'WORK_ITEM', ?)
	`, dummyEntity, now)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.db.ExecContext(ctx, `
		INSERT INTO change_proposals (
			id, target_entity_id, operation_type, title, status, operation_payload_json, created_at, updated_at
		) VALUES (?, ?, 'tracking.workflow_set_current_step', 'Step', 'PENDING', '{"version":1,"target_step_id":"step1"}', ?, ?)
	`, id5, dummyEntity, now, now)
	if err != nil {
		t.Fatalf("expected operation with valid target_entity_id to succeed, got %v", err)
	}
}

// Requirement C: Source interpret isolation & Assistant availability
func TestAIProductControl_SourceInterpretIsolation(t *testing.T) {
	registry := NewProposeRegistry()

	// 1. source.interpret candidate contract filters out ineligible capabilities
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	rawCtx, err := sourceInterpretationContext(now, registry.ListDescriptors())
	if err != nil {
		t.Fatalf("sourceInterpretationContext: %v", err)
	}

	var parsedCtx struct {
		Contract struct {
			Capabilities []struct {
				ID string `json:"id"`
			} `json:"capabilities"`
		} `json:"candidateContract"`
	}
	if err := json.Unmarshal(rawCtx, &parsedCtx); err != nil {
		t.Fatal(err)
	}

	forbiddenSourceCaps := map[string]bool{
		"work_item.create":                   true,
		"monitor_point.create":               true,
		"monitor_point.set_state":            true,
		"tracking.workflow_set_current_step": true,
		"work_item.update_details":           true,
	}

	for _, cap := range parsedCtx.Contract.Capabilities {
		if forbiddenSourceCaps[cap.ID] {
			t.Errorf("source.interpret candidate contract unexpectedly contains forbidden capability %q", cap.ID)
		}
	}

	// 2. Assistant offers all four new capabilities
	allDescriptors := registry.ListDescriptors()
	descriptorMap := make(map[string]bool)
	for _, d := range allDescriptors {
		descriptorMap[d.ID] = true
	}

	requiredAssistantCaps := []string{
		"work_item.create",
		"monitor_point.create",
		"monitor_point.set_state",
		"tracking.workflow_set_current_step",
	}

	for _, capID := range requiredAssistantCaps {
		if !descriptorMap[capID] {
			t.Errorf("Assistant descriptor list missing required capability %q", capID)
		}
	}
}

// Requirement D & E: Workflow current step transition, stale check, no-op same step
func TestAIProductControl_WorkflowSetCurrentStep(t *testing.T) {
	f := newAIProductControlFixture(t)
	ctx := context.Background()

	// Setup WorkItem + Workflow (Planning -> Development -> QA -> Released)
	wi, err := f.workitemSvc.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{
		Title: "Alpha Workflow Test",
	})
	if err != nil {
		t.Fatal(err)
	}

	wfDef, err := f.app.CreateWorkflowDefinition(CreateWorkflowDefinitionRequestDTO{
		Name:    "Standard SDLC",
		Version: 1,
		Steps: []WorkflowStepRequestDTO{
			{Key: "planning", Title: "Planning"},
			{Key: "dev", Title: "Development"},
			{Key: "qa", Title: "QA"},
			{Key: "release", Title: "Released", Terminal: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	wf, err := f.app.CreateWorkflow(CreateWorkflowRequestDTO{
		WorkItemID:           wi.ID,
		WorkflowDefinitionID: wfDef.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create a TrackedItem to verify it remains untouched
	_, err = f.app.CreateTrackedItem(CreateTrackedItemRequestDTO{
		WorkItemID: wi.ID,
		Title:      "Core Engine",
	})
	if err != nil {
		t.Fatal(err)
	}

	planningStepID := wfDef.Steps[0].ID
	devStepID := wfDef.Steps[1].ID
	qaStepID := wfDef.Steps[2].ID
	releaseStepID := wfDef.Steps[3].ID

	if wf.CurrentStep.ID != planningStepID {
		t.Fatalf("expected initial step %s, got %s", planningStepID, wf.CurrentStep.ID)
	}

	// Propose tracking.workflow_set_current_step to "Development"
	args := map[string]any{
		"work_item_id": wi.ID,
		"step_title":   "Development",
	}
	rawArgs, _ := json.Marshal(args)
	reason := "Planning phase complete"
	prop, err := f.registry.Execute(ctx, f.app, "tracking.workflow_set_current_step", nil, rawArgs, &reason)
	if err != nil {
		t.Fatalf("Execute workflow_set_current_step: %v", err)
	}
	if prop.Status != proposal.StatusPending {
		t.Fatalf("expected PENDING, got %s", prop.Status)
	}

	// Record initial tracked item and work item revisions
	trackingViewBefore, err := f.trackingSvc.GetWorkItemTracking(ctx, wi.ID)
	if err != nil {
		t.Fatal(err)
	}
	initialItemRev := trackingViewBefore.Items[0].Item.LastRevisionID

	// Apply proposal
	applyRes, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: prop.ID})
	if err != nil {
		t.Fatalf("ApplyProposal: %v", err)
	}
	if applyRes.Outcome != "APPLIED" {
		t.Fatalf("expected APPLIED, got %s", applyRes.Outcome)
	}

	// Authoritative Workflow.current_step_id becomes Development step ID
	updatedWF, err := f.trackingSvc.GetWorkflow(ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedWF.CurrentStepID != devStepID {
		t.Fatalf("expected current step %s (dev), got %s", devStepID, updatedWF.CurrentStepID)
	}

	// TrackedItems are untouched
	trackingViewAfter, err := f.trackingSvc.GetWorkItemTracking(ctx, wi.ID)
	if err != nil {
		t.Fatal(err)
	}
	if trackingViewAfter.Items[0].Item.LastRevisionID != initialItemRev {
		t.Fatalf("tracked item revision changed from %s to %s", initialItemRev, trackingViewAfter.Items[0].Item.LastRevisionID)
	}

	// WorkItem is untouched
	reloadedWI, err := f.workitemSvc.GetByID(ctx, wi.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloadedWI.LastRevisionID != wi.LastRevisionID {
		t.Fatalf("work item revision changed from %s to %s", wi.LastRevisionID, reloadedWI.LastRevisionID)
	}

	// History records Workflow mutation
	page, err := f.historySvc.QueryWorkItem(ctx, wi.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundStepChangeEvent := false
	for _, ev := range page.Events {
		if ev.Type == history.EventWorkflowCurrentStepChanged {
			foundStepChangeEvent = true
			break
		}
	}
	if !foundStepChangeEvent {
		t.Fatal("expected tracking.workflow_current_step_changed history event")
	}

	// Requirement E: Same-step produces no-op (errCapabilityNoChange)
	_, err = f.registry.Execute(ctx, f.app, "tracking.workflow_set_current_step", nil, rawArgs, nil)
	if !errors.Is(err, errCapabilityNoChange) {
		t.Fatalf("expected errCapabilityNoChange for transition to current step, got %v", err)
	}

	// Requirement E: Invalid step title fails closed
	badArgs, _ := json.Marshal(map[string]any{"work_item_id": wi.ID, "step_title": "Nonexistent Step"})
	_, err = f.registry.Execute(ctx, f.app, "tracking.workflow_set_current_step", nil, badArgs, nil)
	if err == nil {
		t.Fatal("expected error proposing invalid step title, got nil")
	}

	// Requirement E: Proposal targeting stale step ID fails closed
	// 1. Propose transition to QA (captures current step = dev)
	qaArgs, _ := json.Marshal(map[string]any{"work_item_id": wi.ID, "step_title": "QA"})
	staleProp, err := f.registry.Execute(ctx, f.app, "tracking.workflow_set_current_step", nil, qaArgs, nil)
	if err != nil {
		t.Fatal(err)
	}

	// 2. Concurrently move workflow to Released directly
	_, err = f.trackingSvc.SetWorkflowCurrentStep(ctx, tracking.SetWorkflowCurrentStepInput{
		WorkflowID: wf.ID,
		StepID:     releaseStepID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 3. Apply the stale proposal -> must return STALE
	staleApplyRes, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: staleProp.ID})
	if err != nil {
		t.Fatalf("ApplyProposal stale: %v", err)
	}
	if staleApplyRes.Outcome != "STALE" {
		t.Fatalf("expected outcome STALE, got %s", staleApplyRes.Outcome)
	}

	// Workflow current step remains Released (not overwritten by stale proposal)
	finalWF, err := f.trackingSvc.GetWorkflow(ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if finalWF.CurrentStepID != releaseStepID {
		t.Fatalf("expected current step to remain %s, got %s", releaseStepID, finalWF.CurrentStepID)
	}
	_ = qaStepID
}

// Requirement F & H: MonitorPoint create (WorkItem subject) and validation
func TestAIProductControl_MonitorPointCreate_WorkItemSubject(t *testing.T) {
	f := newAIProductControlFixture(t)
	ctx := context.Background()

	wi, err := f.workitemSvc.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{
		Title: "Release M20",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Propose monitor_point.create with subject_type=WORK_ITEM
	args := map[string]any{
		"work_item_id":       wi.ID,
		"kind":               "DEADLINE",
		"title":              "M20 Launch Deadline",
		"due_date":           "2026-10-15",
		"due_time":           "17:00",
		"timezone":           "America/Chicago",
		"expected_lifecycle": "COMPLETED",
	}
	rawArgs, _ := json.Marshal(args)
	reason := "Delivery target commitment"
	prop, err := f.registry.Execute(ctx, f.app, "monitor_point.create", nil, rawArgs, &reason)
	if err != nil {
		t.Fatalf("Execute monitor_point.create: %v", err)
	}

	if prop.Status != proposal.StatusPending {
		t.Fatalf("expected PENDING, got %s", prop.Status)
	}

	// Due instant computed from deterministic local date/time + IANA timezone
	// 2026-10-15 17:00 in America/Chicago (CDT, UTC-5) = 2026-10-15 22:00:00 UTC
	expectedDueUTC := time.Date(2026, 10, 15, 22, 0, 0, 0, time.UTC)
	parsedDue, err := time.Parse(time.RFC3339, prop.MonitorPointCreatePayload.DueAt)
	if err != nil {
		t.Fatalf("parse DueAt: %v", err)
	}
	if !parsedDue.Equal(expectedDueUTC) {
		t.Fatalf("expected DueAt %v, got %v", expectedDueUTC, parsedDue)
	}
	if prop.MonitorPointCreatePayload.Timezone != "America/Chicago" {
		t.Fatalf("expected timezone America/Chicago, got %s", prop.MonitorPointCreatePayload.Timezone)
	}

	// Zero domain truth before apply
	var count int
	err = f.db.QueryRow("SELECT COUNT(*) FROM monitor_points WHERE title = 'M20 Launch Deadline'").Scan(&count)
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("expected 0 monitor points before apply, got %d", count)
	}

	// Apply proposal
	applyRes, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: prop.ID})
	if err != nil {
		t.Fatalf("ApplyProposal: %v", err)
	}
	if applyRes.Outcome != "APPLIED" {
		t.Fatalf("expected APPLIED, got %s", applyRes.Outcome)
	}

	// Authoritative MonitorPoint exists, state == ACTIVE
	trackingView, err := f.trackingSvc.GetWorkItemTracking(ctx, wi.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(trackingView.MonitorPoints) != 1 {
		t.Fatalf("expected 1 monitor point, got %d", len(trackingView.MonitorPoints))
	}
	mp := trackingView.MonitorPoints[0]
	if mp.Title != "M20 Launch Deadline" {
		t.Fatalf("expected title 'M20 Launch Deadline', got %q", mp.Title)
	}
	if mp.State != tracking.MonitorActive {
		t.Fatalf("expected state ACTIVE, got %s", mp.State)
	}
	if mp.Kind != tracking.MonitorDeadline {
		t.Fatalf("expected kind DEADLINE, got %s", mp.Kind)
	}
	if mp.Expected.Kind != tracking.ExpectWorkItemLifecycle {
		t.Fatalf("expected lifecycle expectation, got %s", mp.Expected.Kind)
	}

	// History recorded
	page, err := f.historySvc.QueryWorkItem(ctx, wi.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundCreateEvent := false
	for _, ev := range page.Events {
		if ev.Type == history.EventMonitorCreated {
			foundCreateEvent = true
			break
		}
	}
	if !foundCreateEvent {
		t.Fatal("expected history.EventMonitorCreated history event")
	}

	// Requirement H: Invalid context validation
	// 1. Malformed timezone -> rejected
	badTZArgs, _ := json.Marshal(map[string]any{
		"work_item_id": wi.ID,
		"kind":         "DEADLINE",
		"title":        "Bad TZ",
		"due_date":     "2026-10-15",
		"timezone":     "Not/A_Real_Timezone_12345",
	})
	_, err = f.registry.Execute(ctx, f.app, "monitor_point.create", nil, badTZArgs, nil)
	if err == nil {
		t.Fatal("expected error proposing malformed timezone, got nil")
	}

	// 2. Nonexistent work item -> rejected
	badWIArgs, _ := json.Marshal(map[string]any{
		"work_item_id": "nonexistent-work-item-id",
		"kind":         "DEADLINE",
		"title":        "Bad WI",
		"due_date":     "2026-10-15",
		"timezone":     "UTC",
	})
	_, err = f.registry.Execute(ctx, f.app, "monitor_point.create", nil, badWIArgs, nil)
	if err == nil {
		t.Fatal("expected error proposing for nonexistent work item, got nil")
	}
}

// Requirement G & H: MonitorPoint create (TrackedItem subject) and cross-item rejection
func TestAIProductControl_MonitorPointCreate_TrackedItemSubject(t *testing.T) {
	f := newAIProductControlFixture(t)
	ctx := context.Background()

	// 1. Create WorkItem 1 + Workflow + TrackedItem
	wi1, err := f.workitemSvc.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{
		Title: "Project Alpha",
	})
	if err != nil {
		t.Fatal(err)
	}

	wfDef, err := f.app.CreateWorkflowDefinition(CreateWorkflowDefinitionRequestDTO{
		Name:    "Two Phase",
		Version: 1,
		Steps: []WorkflowStepRequestDTO{
			{Key: "build", Title: "Build"},
			{Key: "verify", Title: "Verify", Terminal: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = f.app.CreateWorkflow(CreateWorkflowRequestDTO{
		WorkItemID:           wi1.ID,
		WorkflowDefinitionID: wfDef.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	item1, err := f.app.CreateTrackedItem(CreateTrackedItemRequestDTO{
		WorkItemID: wi1.ID,
		Title:      "Frontend Dashboard",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. Propose monitor_point.create with subject_item_title and expected_step_title
	args := map[string]any{
		"work_item_id":        wi1.ID,
		"kind":                "CHECKPOINT",
		"title":               "Frontend Verification Checkpoint",
		"subject_item_title":  "Frontend Dashboard",
		"expected_step_title": "Verify",
		"due_date":            "2026-10-20",
		"due_time":            "15:00",
		"timezone":            "America/New_York",
	}
	rawArgs, _ := json.Marshal(args)
	prop, err := f.registry.Execute(ctx, f.app, "monitor_point.create", nil, rawArgs, nil)
	if err != nil {
		t.Fatalf("Execute monitor_point.create for tracked item: %v", err)
	}

	verifyStepID := wfDef.Steps[1].ID
	if prop.MonitorPointCreatePayload.SubjectTrackedItemID == nil || *prop.MonitorPointCreatePayload.SubjectTrackedItemID != item1.ID {
		t.Fatalf("expected subject tracked item ID %s, got %v", item1.ID, prop.MonitorPointCreatePayload.SubjectTrackedItemID)
	}
	if prop.MonitorPointCreatePayload.ExpectedStepID == nil || *prop.MonitorPointCreatePayload.ExpectedStepID != verifyStepID {
		t.Fatalf("expected expected_step_id %s, got %v", verifyStepID, prop.MonitorPointCreatePayload.ExpectedStepID)
	}

	// Apply proposal
	applyRes, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: prop.ID})
	if err != nil {
		t.Fatalf("ApplyProposal: %v", err)
	}
	if applyRes.Outcome != "APPLIED" {
		t.Fatalf("expected outcome APPLIED, got %s", applyRes.Outcome)
	}

	// Authoritative MonitorPoint exists with TrackedItem subject
	trackingView, err := f.trackingSvc.GetWorkItemTracking(ctx, wi1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(trackingView.MonitorPoints) != 1 {
		t.Fatalf("expected 1 monitor point, got %d", len(trackingView.MonitorPoints))
	}
	mp := trackingView.MonitorPoints[0]
	if mp.Subject.Kind != tracking.SubjectTrackedItem || mp.Subject.ID != item1.ID {
		t.Fatalf("expected subject TrackedItem %s, got %+v", item1.ID, mp.Subject)
	}
	if mp.Expected.StepID == nil || *mp.Expected.StepID != verifyStepID {
		t.Fatalf("expected step ID %s, got %v", verifyStepID, mp.Expected.StepID)
	}

	// Requirement H: Cross-WorkItem isolation
	// Create WorkItem 2 with its own item
	wi2, err := f.workitemSvc.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{
		Title: "Project Beta",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.app.CreateWorkflow(CreateWorkflowRequestDTO{
		WorkItemID:           wi2.ID,
		WorkflowDefinitionID: wfDef.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	item2, err := f.app.CreateTrackedItem(CreateTrackedItemRequestDTO{
		WorkItemID: wi2.ID,
		Title:      "Backend Service",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Propose for wi1 with item2 (belongs to wi2) -> rejected
	crossArgs, _ := json.Marshal(map[string]any{
		"work_item_id":        wi1.ID,
		"kind":                "CHECKPOINT",
		"title":               "Cross Checkpoint",
		"subject_item_title":  item2.Title,
		"expected_step_title": "Verify",
		"due_date":            "2026-10-20",
		"timezone":            "UTC",
	})
	_, err = f.registry.Execute(ctx, f.app, "monitor_point.create", nil, crossArgs, nil)
	if err == nil {
		t.Fatal("expected error proposing monitor point with item belonging to another WorkItem, got nil")
	}

	// Workflow step not belonging to active workflow -> rejected
	badStepArgs, _ := json.Marshal(map[string]any{
		"work_item_id":        wi1.ID,
		"kind":                "CHECKPOINT",
		"title":               "Bad Step Checkpoint",
		"subject_item_title":  item1.Title,
		"expected_step_title": "Nonexistent Step",
		"due_date":            "2026-10-20",
		"timezone":            "UTC",
	})
	_, err = f.registry.Execute(ctx, f.app, "monitor_point.create", nil, badStepArgs, nil)
	if err == nil {
		t.Fatal("expected error proposing nonexistent step title, got nil")
	}
}

// Requirement I: MonitorPoint state transitions, terminal states, and no-ops
func TestAIProductControl_MonitorPointSetState_Transitions(t *testing.T) {
	f := newAIProductControlFixture(t)
	ctx := context.Background()

	wi, err := f.workitemSvc.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{
		Title: "State Transition WorkItem",
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1. Create an active MonitorPoint
	due := time.Now().Add(24 * time.Hour).UTC()
	mp, err := f.trackingSvc.CreateMonitorPoint(ctx, tracking.CreateMonitorPointInput{
		WorkItemID:                wi.ID,
		Kind:                      tracking.MonitorCheckpoint,
		Title:                     "Security Review",
		DueAt:                     due,
		Timezone:                  "UTC",
		ExpectedWorkItemLifecycle: (*workitem.LifecycleState)(stringPtr("COMPLETED")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if mp.State != tracking.MonitorActive {
		t.Fatalf("expected initial state ACTIVE, got %s", mp.State)
	}

	// 2. Propose transition to SATISFIED
	satArgs, _ := json.Marshal(map[string]any{
		"work_item_id":     wi.ID,
		"monitor_point_id": mp.ID,
		"target_state":     "SATISFIED",
	})
	propSat, err := f.registry.Execute(ctx, f.app, "monitor_point.set_state", nil, satArgs, nil)
	if err != nil {
		t.Fatalf("Execute monitor_point.set_state to SATISFIED: %v", err)
	}

	applySat, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: propSat.ID})
	if err != nil {
		t.Fatalf("ApplyProposal SATISFIED: %v", err)
	}
	if applySat.Outcome != "APPLIED" {
		t.Fatalf("expected outcome APPLIED, got %s", applySat.Outcome)
	}

	// Authoritative state is SATISFIED
	reloadedMP, err := f.trackingSvc.GetMonitorPoint(ctx, mp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloadedMP.State != tracking.MonitorSatisfied {
		t.Fatalf("expected state SATISFIED, got %s", reloadedMP.State)
	}
	if reloadedMP.SatisfiedAt == nil {
		t.Fatal("expected non-nil SatisfiedAt")
	}

	// 3. Propose transition from SATISFIED to SATISFIED -> no-op (errCapabilityNoChange)
	_, err = f.registry.Execute(ctx, f.app, "monitor_point.set_state", nil, satArgs, nil)
	if !errors.Is(err, errCapabilityNoChange) {
		t.Fatalf("expected errCapabilityNoChange for transition to same state, got %v", err)
	}

	// 4. Propose transition from SATISFIED to CANCELLED -> rejected (terminal state)
	cancelArgs, _ := json.Marshal(map[string]any{
		"work_item_id":     wi.ID,
		"monitor_point_id": mp.ID,
		"target_state":     "CANCELLED",
	})
	_, err = f.registry.Execute(ctx, f.app, "monitor_point.set_state", nil, cancelArgs, nil)
	if err == nil {
		t.Fatal("expected error proposing transition from SATISFIED to CANCELLED, got nil")
	}

	// 5. Test CANCELLED terminal state
	mp2, err := f.trackingSvc.CreateMonitorPoint(ctx, tracking.CreateMonitorPointInput{
		WorkItemID:                wi.ID,
		Kind:                      tracking.MonitorCheckpoint,
		Title:                     "Design Review",
		DueAt:                     due,
		Timezone:                  "UTC",
		ExpectedWorkItemLifecycle: (*workitem.LifecycleState)(stringPtr("COMPLETED")),
	})
	if err != nil {
		t.Fatal(err)
	}

	cancelArgs2, _ := json.Marshal(map[string]any{
		"work_item_id":     wi.ID,
		"monitor_point_id": mp2.ID,
		"target_state":     "CANCELLED",
	})
	propCancel2, err := f.registry.Execute(ctx, f.app, "monitor_point.set_state", nil, cancelArgs2, nil)
	if err != nil {
		t.Fatal(err)
	}
	applyCancel2, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: propCancel2.ID})
	if err != nil {
		t.Fatal(err)
	}
	if applyCancel2.Outcome != "APPLIED" {
		t.Fatalf("expected outcome APPLIED, got %s", applyCancel2.Outcome)
	}

	// Transition from CANCELLED to SATISFIED -> rejected at proposal time
	satArgs2, _ := json.Marshal(map[string]any{
		"work_item_id":     wi.ID,
		"monitor_point_id": mp2.ID,
		"target_state":     "SATISFIED",
	})
	_, err = f.registry.Execute(ctx, f.app, "monitor_point.set_state", nil, satArgs2, nil)
	if err == nil {
		t.Fatal("expected error proposing transition from CANCELLED to SATISFIED, got nil")
	}
}

// Requirement K & Provenance Convergence: Full conversational end-to-end flow with AskAssistant
// proving the unbroken chain for all four capability families:
// Assistant user turn -> SourceRevision S -> AI Run links S -> Proposal links S -> Apply -> resulting domain Revision links S
func TestAIProductControl_ConversationalAssistantEndToEnd(t *testing.T) {
	f := newAIProductControlFixture(t)
	ctx := context.Background()

	// =========================================================================
	// Turn 1: work_item.create
	// User says "Create a project called Launch Alpha"
	// =========================================================================
	f.executor.outputs[ai.ActionAssistantAsk] = `{"version":1,"kind":"MUTATION_CANDIDATE","text":"I will propose creating the Launch Alpha project.","candidate":{"version":1,"capability_id":"work_item.create","arguments":{"title":"Launch Alpha","description":"Initial product release"},"reason":"User requested project creation"}}`

	res1, err := f.app.AskAssistant(AskAssistantRequestDTO{
		Message: "Create a project called Launch Alpha",
		Surface: "DASHBOARD",
	})
	if err != nil {
		t.Fatalf("AskAssistant Turn 1: %v", err)
	}
	if res1.Kind != "MUTATION_INTENT" || res1.ProposalID == "" {
		t.Fatalf("expected MUTATION_INTENT result in Turn 1, got %+v", res1)
	}
	prop1, err := f.app.GetProposal(res1.ProposalID)
	if err != nil {
		t.Fatalf("GetProposal Turn 1: %v", err)
	}
	if prop1.OperationType != "work_item.create" {
		t.Fatalf("expected work_item.create proposal, got %s", prop1.OperationType)
	}

	// Turn 1 Provenance Lineage:
	// 1. Proposal links Assistant SourceRevision S1
	var s1ID string
	err = f.db.QueryRow("SELECT source_revision_id FROM change_proposal_sources WHERE proposal_id = ?", res1.ProposalID).Scan(&s1ID)
	if err != nil {
		t.Fatalf("query change_proposal_sources Turn 1: %v", err)
	}
	// 2. AI Run links Assistant SourceRevision S1
	var run1SourceRevID string
	err = f.db.QueryRow("SELECT source_revision_id FROM ai_run_sources WHERE ai_run_id = (SELECT ai_run_id FROM change_proposals WHERE id = ?)", res1.ProposalID).Scan(&run1SourceRevID)
	if err != nil {
		t.Fatalf("query ai_run_sources Turn 1: %v", err)
	}
	if run1SourceRevID != s1ID {
		t.Fatalf("expected AI Run to link S1 (%s), got %s", s1ID, run1SourceRevID)
	}
	// 3. Zero WorkItem domain truth before Apply
	var wiCountBefore int
	err = f.db.QueryRow("SELECT COUNT(*) FROM work_items WHERE title = 'Launch Alpha'").Scan(&wiCountBefore)
	if err != nil {
		t.Fatal(err)
	}
	if wiCountBefore != 0 {
		t.Fatalf("expected 0 work items before apply, got %d", wiCountBefore)
	}
	var srcCountBefore1 int
	err = f.db.QueryRow("SELECT COUNT(*) FROM source_revisions").Scan(&srcCountBefore1)
	if err != nil {
		t.Fatal(err)
	}

	// 4. User applies proposal 1
	apply1, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: res1.ProposalID})
	if err != nil {
		t.Fatalf("ApplyProposal Turn 1: %v", err)
	}
	if apply1.Outcome != "APPLIED" || apply1.WorkItem == nil {
		t.Fatalf("unexpected apply outcome: %+v", apply1)
	}
	createdWIID := apply1.WorkItem.ID

	// 5. Zero fake sources created during Apply
	var srcCountAfter1 int
	err = f.db.QueryRow("SELECT COUNT(*) FROM source_revisions").Scan(&srcCountAfter1)
	if err != nil {
		t.Fatal(err)
	}
	if srcCountAfter1 != srcCountBefore1 {
		t.Fatalf("expected source_revisions count to remain %d (zero fake sources created at apply), got %d", srcCountBefore1, srcCountAfter1)
	}

	// 6. Resulting initial WorkItem Revision links S1
	wi1, err := f.workitemSvc.GetByID(ctx, createdWIID)
	if err != nil {
		t.Fatal(err)
	}
	var linkedS1, role1 string
	err = f.db.QueryRow("SELECT source_revision_id, role FROM revision_sources WHERE revision_id = ?", wi1.LastRevisionID).Scan(&linkedS1, &role1)
	if err != nil {
		t.Fatalf("query revision_sources for WorkItem revision: %v", err)
	}
	if linkedS1 != s1ID || role1 != "PRIMARY_SOURCE" {
		t.Fatalf("expected revision_sources to link S1 (%s, PRIMARY_SOURCE), got (%s, %s)", s1ID, linkedS1, role1)
	}

	// =========================================================================
	// Turn 2: tracking.workflow_set_current_step
	// Attach a Workflow to the created WorkItem, then move its current step via Assistant
	// =========================================================================
	wfDef, err := f.app.CreateWorkflowDefinition(CreateWorkflowDefinitionRequestDTO{
		Name:    "Alpha Workflow Def",
		Version: 1,
		Steps: []WorkflowStepRequestDTO{
			{Key: "planning", Title: "Planning"},
			{Key: "development", Title: "Development"},
			{Key: "qa", Title: "QA"},
			{Key: "released", Title: "Released", Terminal: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	wf, err := f.app.CreateWorkflow(CreateWorkflowRequestDTO{
		WorkItemID:           createdWIID,
		WorkflowDefinitionID: wfDef.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	planningStepID := wfDef.Steps[0].ID
	devStepID := wfDef.Steps[1].ID
	if wf.CurrentStep.ID != planningStepID {
		t.Fatalf("expected initial workflow step %s, got %s", planningStepID, wf.CurrentStep.ID)
	}

	f.executor.outputs[ai.ActionAssistantAsk] = `{"version":1,"kind":"MUTATION_CANDIDATE","text":"I will move the workflow to Development.","candidate":{"version":1,"capability_id":"tracking.workflow_set_current_step","arguments":{"work_item_id":"` + createdWIID + `","step_title":"Development"},"reason":"Transition to implementation"}}`

	res2, err := f.app.AskAssistant(AskAssistantRequestDTO{
		Message:    "Move workflow to Development",
		Surface:    "WORK_ITEMS",
		WorkItemID: createdWIID,
	})
	if err != nil {
		t.Fatalf("AskAssistant Turn 2: %v", err)
	}
	if res2.Kind != "MUTATION_INTENT" || res2.ProposalID == "" {
		t.Fatalf("expected MUTATION_INTENT in Turn 2, got %+v", res2)
	}

	// Turn 2 Provenance Lineage:
	var s2ID string
	err = f.db.QueryRow("SELECT source_revision_id FROM change_proposal_sources WHERE proposal_id = ?", res2.ProposalID).Scan(&s2ID)
	if err != nil {
		t.Fatalf("query change_proposal_sources Turn 2: %v", err)
	}
	var run2SourceRevID string
	err = f.db.QueryRow("SELECT source_revision_id FROM ai_run_sources WHERE ai_run_id = (SELECT ai_run_id FROM change_proposals WHERE id = ?)", res2.ProposalID).Scan(&run2SourceRevID)
	if err != nil {
		t.Fatalf("query ai_run_sources Turn 2: %v", err)
	}
	if run2SourceRevID != s2ID {
		t.Fatalf("expected AI Run 2 to link S2 (%s), got %s", s2ID, run2SourceRevID)
	}

	var srcCountBefore2 int
	err = f.db.QueryRow("SELECT COUNT(*) FROM source_revisions").Scan(&srcCountBefore2)
	if err != nil {
		t.Fatal(err)
	}

	// User applies proposal 2
	apply2, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: res2.ProposalID})
	if err != nil {
		t.Fatalf("ApplyProposal Turn 2: %v", err)
	}
	if apply2.Outcome != "APPLIED" {
		t.Fatalf("expected outcome APPLIED in Turn 2, got %s", apply2.Outcome)
	}

	// Zero fake sources created during Apply
	var srcCountAfter2 int
	err = f.db.QueryRow("SELECT COUNT(*) FROM source_revisions").Scan(&srcCountAfter2)
	if err != nil {
		t.Fatal(err)
	}
	if srcCountAfter2 != srcCountBefore2 {
		t.Fatalf("expected source_revisions count to remain %d, got %d", srcCountBefore2, srcCountAfter2)
	}

	// Resulting updated Workflow Revision links S2
	updatedWF, err := f.trackingSvc.GetWorkflow(ctx, wf.ID)
	if err != nil {
		t.Fatal(err)
	}
	if updatedWF.CurrentStepID != devStepID {
		t.Fatalf("expected workflow current step %s, got %s", devStepID, updatedWF.CurrentStepID)
	}
	var linkedS2, role2 string
	err = f.db.QueryRow("SELECT source_revision_id, role FROM revision_sources WHERE revision_id = ?", updatedWF.LastRevisionID).Scan(&linkedS2, &role2)
	if err != nil {
		t.Fatalf("query revision_sources for Workflow revision: %v", err)
	}
	if linkedS2 != s2ID || role2 != "PRIMARY_SOURCE" {
		t.Fatalf("expected revision_sources to link S2 (%s, PRIMARY_SOURCE), got (%s, %s)", s2ID, linkedS2, role2)
	}

	// =========================================================================
	// Turn 3: monitor_point.create
	// User says "Add a deadline for tomorrow at 5 PM"
	// =========================================================================
	f.executor.outputs[ai.ActionAssistantAsk] = `{"version":1,"kind":"MUTATION_CANDIDATE","text":"I will create a deadline for tomorrow at 5 PM.","candidate":{"version":1,"capability_id":"monitor_point.create","arguments":{"work_item_id":"` + createdWIID + `","kind":"DEADLINE","title":"Code Freeze","due_date":"2026-10-01","due_time":"17:00","timezone":"UTC","expected_lifecycle":"COMPLETED"},"reason":"Target deadline"}}`

	res3, err := f.app.AskAssistant(AskAssistantRequestDTO{
		Message:    "Add a deadline for tomorrow at 5 PM",
		Surface:    "WORK_ITEMS",
		WorkItemID: createdWIID,
	})
	if err != nil {
		t.Fatalf("AskAssistant Turn 3: %v", err)
	}
	if res3.Kind != "MUTATION_INTENT" || res3.ProposalID == "" {
		t.Fatalf("expected MUTATION_INTENT in Turn 3, got %+v", res3)
	}

	// Turn 3 Provenance Lineage:
	var s3ID string
	err = f.db.QueryRow("SELECT source_revision_id FROM change_proposal_sources WHERE proposal_id = ?", res3.ProposalID).Scan(&s3ID)
	if err != nil {
		t.Fatalf("query change_proposal_sources Turn 3: %v", err)
	}
	var run3SourceRevID string
	err = f.db.QueryRow("SELECT source_revision_id FROM ai_run_sources WHERE ai_run_id = (SELECT ai_run_id FROM change_proposals WHERE id = ?)", res3.ProposalID).Scan(&run3SourceRevID)
	if err != nil {
		t.Fatalf("query ai_run_sources Turn 3: %v", err)
	}
	if run3SourceRevID != s3ID {
		t.Fatalf("expected AI Run 3 to link S3 (%s), got %s", s3ID, run3SourceRevID)
	}

	var srcCountBefore3 int
	err = f.db.QueryRow("SELECT COUNT(*) FROM source_revisions").Scan(&srcCountBefore3)
	if err != nil {
		t.Fatal(err)
	}

	// User applies proposal 3
	apply3, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: res3.ProposalID})
	if err != nil {
		t.Fatalf("ApplyProposal Turn 3: %v", err)
	}
	if apply3.Outcome != "APPLIED" {
		t.Fatalf("expected outcome APPLIED in Turn 3, got %s", apply3.Outcome)
	}

	// Zero fake sources created during Apply
	var srcCountAfter3 int
	err = f.db.QueryRow("SELECT COUNT(*) FROM source_revisions").Scan(&srcCountAfter3)
	if err != nil {
		t.Fatal(err)
	}
	if srcCountAfter3 != srcCountBefore3 {
		t.Fatalf("expected source_revisions count to remain %d, got %d", srcCountBefore3, srcCountAfter3)
	}

	// Resulting initial MonitorPoint Revision links S3
	trackingView, err := f.trackingSvc.GetWorkItemTracking(ctx, createdWIID)
	if err != nil {
		t.Fatal(err)
	}
	if len(trackingView.MonitorPoints) != 1 || trackingView.MonitorPoints[0].Title != "Code Freeze" {
		t.Fatalf("unexpected monitor points: %+v", trackingView.MonitorPoints)
	}
	createdMP := trackingView.MonitorPoints[0]
	var linkedS3, role3 string
	err = f.db.QueryRow("SELECT source_revision_id, role FROM revision_sources WHERE revision_id = ?", createdMP.LastRevisionID).Scan(&linkedS3, &role3)
	if err != nil {
		t.Fatalf("query revision_sources for MonitorPoint revision: %v", err)
	}
	if linkedS3 != s3ID || role3 != "PRIMARY_SOURCE" {
		t.Fatalf("expected revision_sources to link S3 (%s, PRIMARY_SOURCE), got (%s, %s)", s3ID, linkedS3, role3)
	}

	// =========================================================================
	// Turn 4: monitor_point.set_state
	// User says "Mark deadline Code Freeze satisfied"
	// =========================================================================
	f.executor.outputs[ai.ActionAssistantAsk] = `{"version":1,"kind":"MUTATION_CANDIDATE","text":"I will mark Code Freeze satisfied.","candidate":{"version":1,"capability_id":"monitor_point.set_state","arguments":{"work_item_id":"` + createdWIID + `","monitor_point_id":"` + createdMP.ID + `","target_state":"SATISFIED"},"reason":"Target completed"}}`

	res4, err := f.app.AskAssistant(AskAssistantRequestDTO{
		Message:    "Mark deadline Code Freeze satisfied",
		Surface:    "WORK_ITEMS",
		WorkItemID: createdWIID,
	})
	if err != nil {
		t.Fatalf("AskAssistant Turn 4: %v", err)
	}
	if res4.Kind != "MUTATION_INTENT" || res4.ProposalID == "" {
		t.Fatalf("expected MUTATION_INTENT in Turn 4, got %+v", res4)
	}

	// Turn 4 Provenance Lineage:
	var s4ID string
	err = f.db.QueryRow("SELECT source_revision_id FROM change_proposal_sources WHERE proposal_id = ?", res4.ProposalID).Scan(&s4ID)
	if err != nil {
		t.Fatalf("query change_proposal_sources Turn 4: %v", err)
	}
	var run4SourceRevID string
	err = f.db.QueryRow("SELECT source_revision_id FROM ai_run_sources WHERE ai_run_id = (SELECT ai_run_id FROM change_proposals WHERE id = ?)", res4.ProposalID).Scan(&run4SourceRevID)
	if err != nil {
		t.Fatalf("query ai_run_sources Turn 4: %v", err)
	}
	if run4SourceRevID != s4ID {
		t.Fatalf("expected AI Run 4 to link S4 (%s), got %s", s4ID, run4SourceRevID)
	}

	var srcCountBefore4 int
	err = f.db.QueryRow("SELECT COUNT(*) FROM source_revisions").Scan(&srcCountBefore4)
	if err != nil {
		t.Fatal(err)
	}

	// User applies proposal 4
	apply4, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: res4.ProposalID})
	if err != nil {
		t.Fatalf("ApplyProposal Turn 4: %v", err)
	}
	if apply4.Outcome != "APPLIED" {
		t.Fatalf("expected outcome APPLIED in Turn 4, got %s", apply4.Outcome)
	}

	// Zero fake sources created during Apply
	var srcCountAfter4 int
	err = f.db.QueryRow("SELECT COUNT(*) FROM source_revisions").Scan(&srcCountAfter4)
	if err != nil {
		t.Fatal(err)
	}
	if srcCountAfter4 != srcCountBefore4 {
		t.Fatalf("expected source_revisions count to remain %d, got %d", srcCountBefore4, srcCountAfter4)
	}

	// Resulting updated MonitorPoint Revision links S4
	reloadedMP, err := f.trackingSvc.GetMonitorPoint(ctx, createdMP.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloadedMP.State != tracking.MonitorSatisfied || reloadedMP.SatisfiedAt == nil {
		t.Fatalf("expected monitor point to be SATISFIED with non-nil SatisfiedAt, got state=%s", reloadedMP.State)
	}
	var linkedS4, role4 string
	err = f.db.QueryRow("SELECT source_revision_id, role FROM revision_sources WHERE revision_id = ?", reloadedMP.LastRevisionID).Scan(&linkedS4, &role4)
	if err != nil {
		t.Fatalf("query revision_sources for updated MonitorPoint revision: %v", err)
	}
	if linkedS4 != s4ID || role4 != "PRIMARY_SOURCE" {
		t.Fatalf("expected revision_sources to link S4 (%s, PRIMARY_SOURCE), got (%s, %s)", s4ID, linkedS4, role4)
	}
}

// Provenance Negative Invariants:
// 1. Proposal rejection creates NO domain revision and NO revision_sources link.
// 2. Stale/failing proposal apply creates NO misleading revision_sources link.
func TestAIProductControl_ConversationalAssistant_NegativeProofs(t *testing.T) {
	f := newAIProductControlFixture(t)
	ctx := context.Background()

	// -------------------------------------------------------------------------
	// Negative Proof 1: Reject creates no domain revision, no revision_sources link
	// -------------------------------------------------------------------------
	f.executor.outputs[ai.ActionAssistantAsk] = `{"version":1,"kind":"MUTATION_CANDIDATE","text":"I will propose creating a project.","candidate":{"version":1,"capability_id":"work_item.create","arguments":{"title":"Rejected Project"},"reason":"Test reject"}}`

	rejRes, err := f.app.AskAssistant(AskAssistantRequestDTO{
		Message: "Create a project called Rejected Project",
		Surface: "DASHBOARD",
	})
	if err != nil {
		t.Fatalf("AskAssistant reject flow: %v", err)
	}
	if rejRes.ProposalID == "" {
		t.Fatal("expected proposal ID in reject flow")
	}

	var rejSourceRevID string
	err = f.db.QueryRow("SELECT source_revision_id FROM change_proposal_sources WHERE proposal_id = ?", rejRes.ProposalID).Scan(&rejSourceRevID)
	if err != nil {
		t.Fatalf("query change_proposal_sources for reject proposal: %v", err)
	}

	var srcCountBeforeReject int
	err = f.db.QueryRow("SELECT COUNT(*) FROM source_revisions").Scan(&srcCountBeforeReject)
	if err != nil {
		t.Fatal(err)
	}

	// Reject the proposal
	_, err = f.app.RejectProposal(RejectProposalRequestDTO{ID: rejRes.ProposalID})
	if err != nil {
		t.Fatalf("RejectProposal: %v", err)
	}

	// Assert no domain entities created
	var wiCount int
	err = f.db.QueryRow("SELECT COUNT(*) FROM work_items WHERE title = 'Rejected Project'").Scan(&wiCount)
	if err != nil {
		t.Fatal(err)
	}
	if wiCount != 0 {
		t.Fatalf("expected 0 work items after reject, got %d", wiCount)
	}

	// Assert no revision_sources linked to rejSourceRevID
	var revSrcCount int
	err = f.db.QueryRow("SELECT COUNT(*) FROM revision_sources WHERE source_revision_id = ?", rejSourceRevID).Scan(&revSrcCount)
	if err != nil {
		t.Fatal(err)
	}
	if revSrcCount != 0 {
		t.Fatalf("expected 0 revision_sources links for rejected proposal source, got %d", revSrcCount)
	}

	// Assert zero fake sources created
	var srcCountAfterReject int
	err = f.db.QueryRow("SELECT COUNT(*) FROM source_revisions").Scan(&srcCountAfterReject)
	if err != nil {
		t.Fatal(err)
	}
	if srcCountAfterReject != srcCountBeforeReject {
		t.Fatalf("expected source_revisions count to remain %d, got %d", srcCountBeforeReject, srcCountAfterReject)
	}

	// -------------------------------------------------------------------------
	// Negative Proof 2: Stale/failing apply creates no misleading revision_sources link
	// -------------------------------------------------------------------------
	wi, err := f.workitemSvc.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{
		Title: "Stale Workflow Test",
	})
	if err != nil {
		t.Fatal(err)
	}
	wfDef, err := f.app.CreateWorkflowDefinition(CreateWorkflowDefinitionRequestDTO{
		Name:    "Stale SDLC",
		Version: 1,
		Steps: []WorkflowStepRequestDTO{
			{Key: "step1", Title: "Step 1"},
			{Key: "step2", Title: "Step 2"},
			{Key: "step3", Title: "Step 3", Terminal: true},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	wf, err := f.app.CreateWorkflow(CreateWorkflowRequestDTO{
		WorkItemID:           wi.ID,
		WorkflowDefinitionID: wfDef.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// User requests transition to Step 2
	f.executor.outputs[ai.ActionAssistantAsk] = `{"version":1,"kind":"MUTATION_CANDIDATE","text":"I will move workflow to Step 2.","candidate":{"version":1,"capability_id":"tracking.workflow_set_current_step","arguments":{"work_item_id":"` + wi.ID + `","step_title":"Step 2"},"reason":"Advance stage"}}`

	staleRes, err := f.app.AskAssistant(AskAssistantRequestDTO{
		Message:    "Move workflow to Step 2",
		Surface:    "WORK_ITEMS",
		WorkItemID: wi.ID,
	})
	if err != nil {
		t.Fatalf("AskAssistant stale flow: %v", err)
	}

	var staleSourceRevID string
	err = f.db.QueryRow("SELECT source_revision_id FROM change_proposal_sources WHERE proposal_id = ?", staleRes.ProposalID).Scan(&staleSourceRevID)
	if err != nil {
		t.Fatalf("query change_proposal_sources for stale proposal: %v", err)
	}

	// Before applying, an out-of-band mutation advances the workflow directly to Step 2,
	// mutating the Workflow's revision and current step ID
	_, err = f.trackingSvc.SetWorkflowCurrentStep(ctx, tracking.SetWorkflowCurrentStepInput{
		WorkflowID: wf.ID,
		StepID:     wfDef.Steps[1].ID,
	})
	if err != nil {
		t.Fatalf("out-of-band SetWorkflowCurrentStep: %v", err)
	}

	// User now tries to apply the stale proposal
	applyStaleRes, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: staleRes.ProposalID})
	if err != nil {
		t.Fatalf("ApplyProposal for stale proposal: %v", err)
	}
	if applyStaleRes.Outcome != "STALE" {
		t.Fatalf("expected outcome STALE, got %s", applyStaleRes.Outcome)
	}

	// Proposal is marked STALE
	staleProp, err := f.app.GetProposal(staleRes.ProposalID)
	if err != nil {
		t.Fatal(err)
	}
	if staleProp.Status != "STALE" {
		t.Fatalf("expected proposal status STALE, got %s", staleProp.Status)
	}

	// No revision_sources link was created for the stale proposal's source revision!
	var staleRevSrcCount int
	err = f.db.QueryRow("SELECT COUNT(*) FROM revision_sources WHERE source_revision_id = ?", staleSourceRevID).Scan(&staleRevSrcCount)
	if err != nil {
		t.Fatal(err)
	}
	if staleRevSrcCount != 0 {
		t.Fatalf("expected 0 revision_sources links for stale proposal source, got %d", staleRevSrcCount)
	}
}
