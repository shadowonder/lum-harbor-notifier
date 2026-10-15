package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"lum-harbor-notifier/internal/planner"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/source"
	"lum-harbor-notifier/internal/temporal"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

// TestD09_A_SyntheticPayloadWithUnresolvedExternalWorkflow verifies Test A:
// Synthetic payload with external workflow_id="workflow-001" and no Core workflow:
// - Proposal created successfully
// - Proposal payload has WorkflowID == nil (external identity not confused with Core workflow_id)
// - Apply succeeds
// - Planner item has WorkflowID == nil
// - Source lineage preserved (PRIMARY_SOURCE)
// - DueAt preserved
func TestD09_A_SyntheticPayloadWithUnresolvedExternalWorkflow(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()

	storyPayload := map[string]any{
		"id":          "workflow-001/story-002",
		"workflowId":  "workflow-001",
		"title":       "Implement structured logging",
		"description": "Replace stdout logs with JSON logger conforming to runtime contract. Target completion due by 2026-09-27 14:08.",
		"status":      "NEW",
		"storyPoints": 3,
		"priority":    "MEDIUM",
		"version":     1,
		"dueDate":     "2026-09-27",
		"dueTime":     "14:08",
	}
	payloadBytes, _ := json.Marshal(storyPayload)

	// Model outputs candidate containing external workflow_id: "workflow-001" and due_at
	f.mockAI.cleanOutput = `{"version":"source.clean.v1","normalizedDocument":{"title":"Implement structured logging","dueDate":"2026-09-27","dueTime":"14:08"}}`
	f.mockAI.interpretDoc = `{"version":"source.interpret.v1","summary":"Implement structured logging","interpretationDocument":{"candidate":{"version":1,"capability_id":"planner.item.create","arguments":{"title":"Implement structured logging","due_at":"2026-09-27T14:08:00-05:00","workflow_id":"workflow-001"},"reason":"Create planner todo"}}}`

	rev, err := f.sourceService.CaptureIntentSource(ctx, source.CaptureIntentInput{
		ExtensionID:    "lh-plugin-synthetic-project",
		InstanceID:     "inst-1",
		CapabilityID:   "source.fetch",
		SourceKind:     "synthetic.project.story",
		ExternalID:     "workflow-001/story-002",
		Payload:        payloadBytes,
		ProcessingMode: source.ProcessingModeSemanticProposal,
	})
	if err != nil {
		t.Fatalf("CaptureIntentSource: %v", err)
	}

	// 1. Proposal created successfully
	res, err := f.app.InterpretSource(InterpretSourceRequestDTO{SourceRevisionID: rev.ID})
	if err != nil {
		t.Fatalf("InterpretSource: %v", err)
	}
	if !res.HasProposal || res.Proposal == nil {
		t.Fatalf("expected proposal from interpretation, got %+v", res)
	}

	prop, err := f.proposalService.GetByID(ctx, res.Proposal.ID)
	if err != nil {
		t.Fatalf("get proposal: %v", err)
	}
	if prop.PlannerItemPayload == nil {
		t.Fatal("expected non-nil PlannerItemPayload")
	}

	// External workflow_id must NOT be in proposal payload
	if prop.PlannerItemPayload.WorkflowID != nil {
		t.Fatalf("expected proposal WorkflowID to be nil for unresolved external workflow, got %q", *prop.PlannerItemPayload.WorkflowID)
	}

	// DueAt must be preserved
	if prop.PlannerItemPayload.DueAt == nil || !strings.HasPrefix(*prop.PlannerItemPayload.DueAt, "2026-09-27T14:08") {
		t.Fatalf("expected DueAt preserved in proposal payload, got %v", prop.PlannerItemPayload.DueAt)
	}

	// 2. Apply succeeds without SQLite foreign key constraint failure
	applyRes, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: prop.ID})
	if err != nil {
		t.Fatalf("ApplyProposal failed: %v", err)
	}
	if applyRes.Outcome != "APPLIED" {
		t.Fatalf("expected outcome APPLIED, got %s", applyRes.Outcome)
	}

	// 3. Verify created Planner item has WorkflowID == nil and DueAt preserved
	if prop.TargetEntityID == nil {
		// For root create, the applied revision links to entity
	}
	items, err := f.plannerService.ListCurrentOpen(ctx, temporal.LocalDate("2026-09-27"), time.Local)
	if err != nil {
		t.Fatalf("ListCurrentOpen: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 open planner item, got %d", len(items))
	}
	item := items[0]
	if item.WorkflowID != nil {
		t.Fatalf("expected PlannerItem.WorkflowID to be nil, got %q", *item.WorkflowID)
	}
	expectedDueAt, _ := time.Parse(time.RFC3339, "2026-09-27T14:08:00-05:00")
	if item.DueAt == nil || !item.DueAt.Equal(expectedDueAt) {
		t.Fatalf("expected PlannerItem.DueAt to equal %v, got %v", expectedDueAt, item.DueAt)
	}

	// 4. Source lineage preserved
	var revSourceRevID, revSourceRole string
	err = f.db.QueryRowContext(ctx, "SELECT source_revision_id, role FROM revision_sources WHERE revision_id = ?", item.LastRevisionID).Scan(&revSourceRevID, &revSourceRole)
	if err != nil {
		t.Fatalf("query revision_sources: %v", err)
	}
	if revSourceRevID != rev.ID {
		t.Fatalf("expected SourceRevisionID %q, got %q", rev.ID, revSourceRevID)
	}
	if revSourceRole != "PRIMARY_SOURCE" {
		t.Fatalf("expected relationship type PRIMARY_SOURCE, got %q", revSourceRole)
	}
}

// TestD09_B_RealCoreWorkflowExplicitlySuppliedOrResolved verifies Test B:
// If a real Core Workflow is explicitly supplied / resolved:
// - Planner item correctly links to that Core workflow
func TestD09_B_RealCoreWorkflowExplicitlySuppliedOrResolved(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()

	// Create a real Core WorkItem and Workflow
	wi, err := f.workitemService.CreateLocalWorkItem(ctx, workitem.CreateLocalWorkItemInput{
		Title: "Core Backend WorkItem",
	})
	if err != nil {
		t.Fatalf("CreateLocalWorkItem: %v", err)
	}

	def, err := f.trackingService.CreateWorkflowDefinition(ctx, tracking.CreateWorkflowDefinitionInput{
		Name:    "Feature Flow",
		Version: 1,
		Steps: []tracking.WorkflowStepInput{
			{Key: "step-1", Title: "Specification"},
			{Key: "step-2", Title: "Implementation"},
		},
	})
	if err != nil {
		t.Fatalf("CreateWorkflowDefinition: %v", err)
	}

	wf, err := f.trackingService.CreateWorkflow(ctx, tracking.CreateWorkflowInput{
		WorkItemID:           wi.ID,
		WorkflowDefinitionID: def.ID,
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}

	t.Run("Direct Core Workflow ID", func(t *testing.T) {
		rawArgs, _ := json.Marshal(map[string]any{
			"title":       "Implement core step",
			"workflow_id": wf.ID,
			"due_date":    "2026-09-28",
			"due_time":    "10:00",
		})
		prop, err := f.app.proposeRegistry.Execute(ctx, f.app, "planner.item.create", nil, rawArgs, nil)
		if err != nil {
			t.Fatalf("proposeRegistry.Execute: %v", err)
		}
		if prop.PlannerItemPayload == nil || prop.PlannerItemPayload.WorkflowID == nil {
			t.Fatalf("expected proposal to have WorkflowID set to %q, got nil", wf.ID)
		}
		if *prop.PlannerItemPayload.WorkflowID != wf.ID {
			t.Fatalf("expected WorkflowID %q, got %q", wf.ID, *prop.PlannerItemPayload.WorkflowID)
		}

		applyRes, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: prop.ID})
		if err != nil {
			t.Fatalf("ApplyProposal: %v", err)
		}
		if applyRes.Outcome != "APPLIED" {
			t.Fatalf("expected APPLIED, got %s", applyRes.Outcome)
		}

		items, err := f.plannerService.ListCurrentOpen(ctx, temporal.LocalDate("2026-09-28"), time.Local)
		if err != nil {
			t.Fatalf("ListCurrentOpen: %v", err)
		}
		found := false
		for _, it := range items {
			if it.Title == "Implement core step" {
				found = true
				if it.WorkflowID == nil || *it.WorkflowID != wf.ID {
					t.Fatalf("expected PlannerItem.WorkflowID == %q, got %v", wf.ID, it.WorkflowID)
				}
			}
		}
		if !found {
			t.Fatal("created planner item not found")
		}
	})

	t.Run("Mapped External Ref to Core Workflow", func(t *testing.T) {
		// Register an explicit external_refs binding for external workflow "ext-flow-999" -> work item wi.ID
		_, err := f.db.ExecContext(ctx, `INSERT INTO external_refs (id, entity_id, system, external_id, relationship_type)
			VALUES ('ref-ext-flow-999', ?, 'jira', 'ext-flow-999', 'AUTHORITATIVE')`, wi.ID)
		if err != nil {
			t.Fatalf("insert external ref: %v", err)
		}

		rawArgs, _ := json.Marshal(map[string]any{
			"title":       "Mapped external workflow task",
			"workflow_id": "ext-flow-999",
			"due_date":    "2026-09-28",
			"due_time":    "11:00",
		})
		prop, err := f.app.proposeRegistry.Execute(ctx, f.app, "planner.item.create", nil, rawArgs, nil)
		if err != nil {
			t.Fatalf("proposeRegistry.Execute: %v", err)
		}
		if prop.PlannerItemPayload == nil || prop.PlannerItemPayload.WorkflowID == nil {
			t.Fatalf("expected proposal to have resolved WorkflowID set to %q, got nil", wf.ID)
		}
		if *prop.PlannerItemPayload.WorkflowID != wf.ID {
			t.Fatalf("expected resolved WorkflowID %q, got %q", wf.ID, *prop.PlannerItemPayload.WorkflowID)
		}

		applyRes, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: prop.ID})
		if err != nil {
			t.Fatalf("ApplyProposal: %v", err)
		}
		if applyRes.Outcome != "APPLIED" {
			t.Fatalf("expected APPLIED, got %s", applyRes.Outcome)
		}
	})
}

// TestD09_C_InvalidInternalCoreWorkflowFailsWithDomainError verifies Test C:
// If an invalid internal Core workflow_id is directly supplied to Planner create / proposal apply:
// - Fails with clean domain validation error
// - NOT raw SQLite FK error
func TestD09_C_InvalidInternalCoreWorkflowFailsWithDomainError(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()

	invalidWF := "non-existent-core-workflow-uuid"

	t.Run("Direct Planner CreateLocalTodoTx fails with ErrInvalidWorkflow", func(t *testing.T) {
		tx, err := f.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatalf("BeginTx: %v", err)
		}
		defer func() { _ = tx.Rollback() }()

		_, err = f.plannerService.CreateLocalTodoTx(ctx, tx, planner.CreateLocalTodoInput{
			Title:      "Direct create with invalid workflow",
			WorkflowID: &invalidWF,
		})
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !errors.Is(err, planner.ErrInvalidWorkflow) {
			t.Fatalf("expected ErrInvalidWorkflow, got: %v", err)
		}
		if strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
			t.Fatalf("error must not expose raw SQLite FK failure: %v", err)
		}
	})

	t.Run("Proposal Apply with invalid workflow_id in payload fails with clean domain error", func(t *testing.T) {
		// Directly propose a planner item with an invalid workflow_id
		prop, err := f.proposalService.ProposePlannerItemCreate(ctx, proposal.ProposePlannerItemCreateInput{
			Title:      "Todo with direct invalid workflow",
			WorkflowID: &invalidWF,
		})
		if err != nil {
			t.Fatalf("ProposePlannerItemCreate: %v", err)
		}

		// Apply proposal
		_, err = f.proposalService.ApplyProposal(ctx, proposal.ApplyProposalInput{ID: prop.ID})
		if err == nil {
			t.Fatal("expected apply error, got nil")
		}
		if !errors.Is(err, proposal.ErrInvalidOperationPayload) {
			t.Fatalf("expected ErrInvalidOperationPayload, got: %v", err)
		}
		if !errors.Is(err, planner.ErrInvalidWorkflow) {
			t.Fatalf("expected wrapped ErrInvalidWorkflow, got: %v", err)
		}
		if strings.Contains(err.Error(), "FOREIGN KEY constraint failed") {
			t.Fatalf("error must not expose raw SQLite FK failure: %v", err)
		}
	})
}

// TestD09_D_ExistingBoundedSyntheticChainSucceeds verifies Test D:
// Existing bounded Synthetic chain succeeds end to end without creating fake Core workflows.
func TestD09_D_ExistingBoundedSyntheticChainSucceeds(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()

	storyPayload := map[string]any{
		"id":          "workflow-001/story-002",
		"workflowId":  "workflow-001",
		"title":       "Implement structured logging",
		"description": "Replace stdout logs with JSON logger conforming to runtime contract. Target completion due by 2026-09-27 14:08.",
		"status":      "NEW",
		"storyPoints": 3,
		"priority":    "MEDIUM",
		"version":     1,
		"dueDate":     "2026-09-27",
		"dueTime":     "14:08",
	}
	payloadBytes, _ := json.Marshal(storyPayload)

	// Clean & interpret outputs
	f.mockAI.cleanOutput = `{"version":"source.clean.v1","normalizedDocument":{"title":"Implement structured logging","dueDate":"2026-09-27","dueTime":"14:08"}}`
	f.mockAI.interpretDoc = `{"version":"source.interpret.v1","summary":"Implement structured logging","interpretationDocument":{"candidate":{"version":1,"capability_id":"planner.item.create","arguments":{"title":"Implement structured logging","due_at":"2026-09-27T14:08:00-05:00","workflow_id":"workflow-001"},"reason":"Create planner todo"}}}`

	rev, err := f.sourceService.CaptureIntentSource(ctx, source.CaptureIntentInput{
		ExtensionID:    "lh-plugin-synthetic-project",
		InstanceID:     "inst-1",
		CapabilityID:   "source.fetch",
		SourceKind:     "synthetic.project.story",
		ExternalID:     "workflow-001/story-002",
		Payload:        payloadBytes,
		ProcessingMode: source.ProcessingModeSemanticProposal,
	})
	if err != nil {
		t.Fatalf("CaptureIntentSource: %v", err)
	}

	// Interpret source -> creates Proposal
	res, err := f.app.InterpretSource(InterpretSourceRequestDTO{SourceRevisionID: rev.ID})
	if err != nil {
		t.Fatalf("InterpretSource: %v", err)
	}
	if !res.HasProposal || res.Proposal == nil {
		t.Fatalf("expected proposal from interpretation, got %+v", res)
	}

	// Apply proposal
	applyRes, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: res.Proposal.ID})
	if err != nil {
		t.Fatalf("ApplyProposal: %v", err)
	}
	if applyRes.Outcome != "APPLIED" {
		t.Fatalf("expected outcome APPLIED, got %s", applyRes.Outcome)
	}

	// Verify no fake Core workflows were created in workflows table
	var wfCount int
	if err := f.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM workflows").Scan(&wfCount); err != nil {
		t.Fatalf("count workflows: %v", err)
	}
	if wfCount != 0 {
		t.Fatalf("invariant violated: expected 0 Core workflows created, found %d", wfCount)
	}

	// Verify the Planner item was created with DueAt intact and WorkflowID nil
	items, err := f.plannerService.ListCurrentOpen(ctx, temporal.LocalDate("2026-09-27"), time.Local)
	if err != nil {
		t.Fatalf("ListCurrentOpen: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 open planner item, got %d", len(items))
	}
	if items[0].WorkflowID != nil {
		t.Fatalf("expected WorkflowID == nil, got %v", items[0].WorkflowID)
	}
	expectedDueAt, _ := time.Parse(time.RFC3339, "2026-09-27T14:08:00-05:00")
	if items[0].DueAt == nil || !items[0].DueAt.Equal(expectedDueAt) {
		t.Fatalf("expected DueAt %v preserved, got %v", expectedDueAt, items[0].DueAt)
	}
	if items[0].Status != planner.StatusOpen {
		t.Fatalf("expected StatusOpen, got %v", items[0].Status)
	}
}
