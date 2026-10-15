package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/source"
)

func ingestTestObservation(t *testing.T, f *round2TestFixture, instanceID, externalID, title, content string) string {
	t.Helper()
	payloadBytes, err := json.Marshal(map[string]string{
		"title":   title,
		"content": content,
	})
	if err != nil {
		t.Fatalf("json marshal payload: %v", err)
	}

	batch := source.FetchBatch{
		ExtensionID:  "synthetic.task",
		InstanceID:   instanceID,
		CapabilityID: "source.fetch",
		Records: []source.Observation{
			{
				ExternalID: externalID,
				SourceKind: "task",
				ObservedAt: time.Now().UTC(),
				Payload:    json.RawMessage(payloadBytes),
			},
		},
	}
	checkpoint, err := f.sourceService.PrepareFetch(context.Background(), batch.ExtensionID, batch.InstanceID, batch.CapabilityID)
	if err != nil {
		t.Fatalf("PrepareFetch: %v", err)
	}
	batch.Checkpoint = checkpoint

	res, err := f.sourceService.IngestFetchResult(context.Background(), batch)
	if err != nil {
		t.Fatalf("IngestFetchResult: %v", err)
	}
	if len(res.NewlyChangedRevisionIDs) == 0 {
		t.Fatalf("expected new revision for %s, got 0 changes", externalID)
	}
	return res.NewlyChangedRevisionIDs[0]
}

// TestR3C_ScenarioA_ProposalApplied_RevBProcessed verifies:
// Rev A -> Proposal P pending. Rev B arrives -> cycle runs -> 0 AI calls, Rev B remains PENDING with nil proposal link.
// Proposal P is APPLIED -> subsequent cycle runs -> Rev B claimed, AI executes, Proposal P2 created for Rev B.
func TestR3C_ScenarioA_ProposalApplied_RevBProcessed(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()

	// 1. Ingest Revision A
	revA := ingestTestObservation(t, f, "inst-1", "ext-task-1", "Task V1", "Initial content")

	// Run cycle to process Rev A -> creates Proposal P
	cycle1, err := f.app.ProcessPendingSources(ctx)
	if err != nil {
		t.Fatalf("cycle1: %v", err)
	}
	if cycle1.Completed != 1 {
		t.Fatalf("cycle1 completed: got %d, want 1", cycle1.Completed)
	}

	stateA, err := f.sourceService.GetProcessingState(ctx, revA)
	if err != nil || stateA == nil {
		t.Fatalf("get stateA: %v", err)
	}
	if stateA.State != source.ProcessingStatusCompleted || stateA.ProposalID == nil {
		t.Fatalf("expected stateA COMPLETED with proposal, got state=%v, prop=%v", stateA.State, stateA.ProposalID)
	}
	proposalAID := *stateA.ProposalID

	propA, err := f.proposalService.GetByID(ctx, proposalAID)
	if err != nil || propA.Status != "PENDING" {
		t.Fatalf("expected proposalA PENDING, got err=%v, prop=%+v", err, propA)
	}

	aiCallsAfterA := f.mockAI.callCounts[ai.ActionSourceInterpret]
	if aiCallsAfterA == 0 {
		t.Fatal("expected at least 1 AI interpret call for Rev A")
	}

	// 2. Ingest Revision B for the same external task while Proposal A is PENDING
	revB := ingestTestObservation(t, f, "inst-1", "ext-task-1", "Task V2", "Updated content")
	if revB == revA {
		t.Fatal("expected distinct revision ID for Rev B")
	}

	// Verify Rev B starts in PENDING state
	stateBInit, err := f.sourceService.GetProcessingState(ctx, revB)
	if err != nil || stateBInit.State != source.ProcessingStatusPending {
		t.Fatalf("expected stateB initial PENDING, got %+v", stateBInit)
	}

	// Run cycle 2 while Proposal A is still PENDING
	cycle2, err := f.app.ProcessPendingSources(ctx)
	if err != nil {
		t.Fatalf("cycle2: %v", err)
	}
	// Claimed was 1, Completed was 0 (released to pending, not completed)
	if cycle2.Claimed != 1 || cycle2.Completed != 0 {
		t.Fatalf("cycle2 expected claimed=1, completed=0; got claimed=%d, completed=%d", cycle2.Claimed, cycle2.Completed)
	}

	// Crucial assertion: 0 new AI calls for Rev B!
	if f.mockAI.callCounts[ai.ActionSourceInterpret] != aiCallsAfterA {
		t.Fatalf("expected 0 AI calls for suppressed Rev B, got %d (was %d)",
			f.mockAI.callCounts[ai.ActionSourceInterpret], aiCallsAfterA)
	}

	// Crucial assertion: Rev B durable state is RELEASED back to PENDING and NOT linked to Proposal A
	stateBSuppressed, err := f.sourceService.GetProcessingState(ctx, revB)
	if err != nil || stateBSuppressed == nil {
		t.Fatalf("get stateBSuppressed: %v", err)
	}
	if stateBSuppressed.State != source.ProcessingStatusPending {
		t.Fatalf("expected stateB to remain PENDING, got %s", stateBSuppressed.State)
	}
	if stateBSuppressed.ProposalID != nil {
		t.Fatalf("expected stateB proposal_id to be nil, got %s", *stateBSuppressed.ProposalID)
	}

	// 3. User APPLIES Proposal A
	applyRes, err := f.app.ApplyProposal(ApplyProposalRequestDTO{ID: proposalAID})
	if err != nil {
		t.Fatalf("ApplyProposal: %v", err)
	}
	if applyRes.Proposal.Status != "APPLIED" {
		t.Fatalf("expected Proposal A status APPLIED, got %s", applyRes.Proposal.Status)
	}

	// 4. Run cycle 3 WITHOUT any new external ingestion
	cycle3, err := f.app.ProcessPendingSources(ctx)
	if err != nil {
		t.Fatalf("cycle3: %v", err)
	}
	if cycle3.Claimed != 1 || cycle3.Completed != 1 {
		t.Fatalf("cycle3 expected claimed=1, completed=1; got claimed=%d, completed=%d", cycle3.Claimed, cycle3.Completed)
	}

	// Crucial assertion: AI was invoked for Rev B
	if f.mockAI.callCounts[ai.ActionSourceInterpret] <= aiCallsAfterA {
		t.Fatalf("expected AI interpret called for Rev B after Proposal A applied; calls=%d, previous=%d",
			f.mockAI.callCounts[ai.ActionSourceInterpret], aiCallsAfterA)
	}

	// Crucial assertion: Rev B is now COMPLETED and points to a NEW Proposal P2
	stateBFinal, err := f.sourceService.GetProcessingState(ctx, revB)
	if err != nil || stateBFinal == nil {
		t.Fatalf("get stateBFinal: %v", err)
	}
	if stateBFinal.State != source.ProcessingStatusCompleted {
		t.Fatalf("expected stateB COMPLETED, got %s", stateBFinal.State)
	}
	if stateBFinal.ProposalID == nil || *stateBFinal.ProposalID == "" {
		t.Fatal("expected non-nil proposal_id for completed Rev B")
	}
	if *stateBFinal.ProposalID == proposalAID {
		t.Fatalf("expected new proposal ID for Rev B, got same as Proposal A: %s", proposalAID)
	}

	// Verify Proposal B lineage in DB points to Rev B
	var linkedRevID string
	err = f.db.QueryRow("SELECT source_revision_id FROM change_proposal_sources WHERE proposal_id = ?", *stateBFinal.ProposalID).Scan(&linkedRevID)
	if err != nil {
		t.Fatalf("query change_proposal_sources for proposal B: %v", err)
	}
	if linkedRevID != revB {
		t.Fatalf("expected Proposal B lineage to point to Rev B (%s), got %s", revB, linkedRevID)
	}
}

// TestR3C_ScenarioB_ProposalRejected_RevBProcessed verifies:
// Rev A -> Proposal P pending. Rev B arrives -> suppressed to PENDING with 0 AI calls.
// Proposal P is REJECTED -> subsequent cycle runs -> Rev B processed and creates Proposal P2.
func TestR3C_ScenarioB_ProposalRejected_RevBProcessed(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()

	revA := ingestTestObservation(t, f, "inst-1", "ext-task-2", "Task V1", "Initial content")

	cycle1, err := f.app.ProcessPendingSources(ctx)
	if err != nil || cycle1.Completed != 1 {
		t.Fatalf("cycle1: err=%v, res=%+v", err, cycle1)
	}

	stateA, _ := f.sourceService.GetProcessingState(ctx, revA)
	proposalAID := *stateA.ProposalID

	aiCallsAfterA := f.mockAI.callCounts[ai.ActionSourceInterpret]

	revB := ingestTestObservation(t, f, "inst-1", "ext-task-2", "Task V2", "Updated content")

	cycle2, err := f.app.ProcessPendingSources(ctx)
	if err != nil || cycle2.Claimed != 1 || cycle2.Completed != 0 {
		t.Fatalf("cycle2: err=%v, res=%+v", err, cycle2)
	}

	if f.mockAI.callCounts[ai.ActionSourceInterpret] != aiCallsAfterA {
		t.Fatalf("expected 0 AI calls for suppressed Rev B")
	}

	stateBSuppressed, _ := f.sourceService.GetProcessingState(ctx, revB)
	if stateBSuppressed.State != source.ProcessingStatusPending || stateBSuppressed.ProposalID != nil {
		t.Fatalf("expected stateB PENDING with nil proposal, got %+v", stateBSuppressed)
	}

	// User REJECTS Proposal A
	rejectRes, err := f.app.RejectProposal(RejectProposalRequestDTO{ID: proposalAID})
	if err != nil {
		t.Fatalf("RejectProposal: %v", err)
	}
	if rejectRes.Status != "REJECTED" {
		t.Fatalf("expected Proposal A status REJECTED, got %s", rejectRes.Status)
	}

	// Next cycle processes Rev B
	cycle3, err := f.app.ProcessPendingSources(ctx)
	if err != nil || cycle3.Claimed != 1 || cycle3.Completed != 1 {
		t.Fatalf("cycle3: err=%v, res=%+v", err, cycle3)
	}

	stateBFinal, _ := f.sourceService.GetProcessingState(ctx, revB)
	if stateBFinal.State != source.ProcessingStatusCompleted || stateBFinal.ProposalID == nil || *stateBFinal.ProposalID == proposalAID {
		t.Fatalf("expected stateB COMPLETED with distinct proposal, got %+v", stateBFinal)
	}

	var linkedRevID string
	err = f.db.QueryRow("SELECT source_revision_id FROM change_proposal_sources WHERE proposal_id = ?", *stateBFinal.ProposalID).Scan(&linkedRevID)
	if err != nil {
		t.Fatalf("query change_proposal_sources for proposal B: %v", err)
	}
	if linkedRevID != revB {
		t.Fatalf("expected Proposal B lineage to point to Rev B (%s), got %s", revB, linkedRevID)
	}
}

// TestR3C_ScenarioC_ProposalStale_RevBProcessed verifies:
// Rev A -> Proposal P pending. Rev B arrives -> suppressed to PENDING with 0 AI calls.
// Proposal P transitions to STALE -> subsequent cycle runs -> Rev B processed and creates Proposal P2.
func TestR3C_ScenarioC_ProposalStale_RevBProcessed(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()

	revA := ingestTestObservation(t, f, "inst-1", "ext-task-3", "Task V1", "Initial content")

	cycle1, err := f.app.ProcessPendingSources(ctx)
	if err != nil || cycle1.Completed != 1 {
		t.Fatalf("cycle1: err=%v, res=%+v", err, cycle1)
	}

	stateA, _ := f.sourceService.GetProcessingState(ctx, revA)
	proposalAID := *stateA.ProposalID

	aiCallsAfterA := f.mockAI.callCounts[ai.ActionSourceInterpret]

	revB := ingestTestObservation(t, f, "inst-1", "ext-task-3", "Task V2", "Updated content")

	cycle2, err := f.app.ProcessPendingSources(ctx)
	if err != nil || cycle2.Claimed != 1 || cycle2.Completed != 0 {
		t.Fatalf("cycle2: err=%v, res=%+v", err, cycle2)
	}

	if f.mockAI.callCounts[ai.ActionSourceInterpret] != aiCallsAfterA {
		t.Fatalf("expected 0 AI calls for suppressed Rev B")
	}

	// Transition Proposal A to STALE
	nowStr := time.Now().UTC().Format(time.RFC3339)
	_, err = f.db.Exec("UPDATE change_proposals SET status = 'STALE', resolved_at = ?, updated_at = ? WHERE id = ?", nowStr, nowStr, proposalAID)
	if err != nil {
		t.Fatalf("update proposal A to STALE: %v", err)
	}

	// Verify proposal A is STALE
	propA, err := f.proposalService.GetByID(ctx, proposalAID)
	if err != nil || propA.Status != "STALE" {
		t.Fatalf("expected Proposal A status STALE, got %+v", propA)
	}

	// Next cycle processes Rev B
	cycle3, err := f.app.ProcessPendingSources(ctx)
	if err != nil || cycle3.Claimed != 1 || cycle3.Completed != 1 {
		t.Fatalf("cycle3: err=%v, res=%+v", err, cycle3)
	}

	stateBFinal, _ := f.sourceService.GetProcessingState(ctx, revB)
	if stateBFinal.State != source.ProcessingStatusCompleted || stateBFinal.ProposalID == nil || *stateBFinal.ProposalID == proposalAID {
		t.Fatalf("expected stateB COMPLETED with distinct proposal, got %+v", stateBFinal)
	}

	var linkedRevID string
	err = f.db.QueryRow("SELECT source_revision_id FROM change_proposal_sources WHERE proposal_id = ?", *stateBFinal.ProposalID).Scan(&linkedRevID)
	if err != nil {
		t.Fatalf("query change_proposal_sources for proposal B: %v", err)
	}
	if linkedRevID != revB {
		t.Fatalf("expected Proposal B lineage to point to Rev B (%s), got %s", revB, linkedRevID)
	}
}

// TestR3C_ScenarioD_MultipleRevisionsPruning verifies:
// While Proposal P is pending, Rev B, C, D arrive.
// Intermediate revisions (B, C) are retired to COMPLETED with 0 AI calls and nil proposal_id.
// Head revision (D) is released back to PENDING.
// Once P resolves, D is processed into Proposal P2; B and C never invoke AI or create proposals.
func TestR3C_ScenarioD_MultipleRevisionsPruning(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()

	// 1. Rev A creates pending Proposal P
	revA := ingestTestObservation(t, f, "inst-1", "ext-task-multi", "Task V1", "Rev A")
	_, err := f.app.ProcessPendingSources(ctx)
	if err != nil {
		t.Fatalf("process Rev A: %v", err)
	}
	stateA, _ := f.sourceService.GetProcessingState(ctx, revA)
	proposalAID := *stateA.ProposalID

	aiCallsAfterA := f.mockAI.callCounts[ai.ActionSourceInterpret]

	// 2. Sequential arrival of B, C, D while Proposal A is still PENDING
	revB := ingestTestObservation(t, f, "inst-1", "ext-task-multi", "Task V2", "Rev B")
	revC := ingestTestObservation(t, f, "inst-1", "ext-task-multi", "Task V3", "Rev C")
	revD := ingestTestObservation(t, f, "inst-1", "ext-task-multi", "Task V4", "Rev D")

	// Current revision of the record is now Rev D
	revARec, err := f.sourceService.GetSourceRevision(ctx, revA)
	if err != nil || revARec == nil {
		t.Fatalf("get revA record: %v", err)
	}
	rec, err := f.sourceService.GetSourceRecord(ctx, revARec.SourceRecordID)
	if err != nil {
		t.Fatalf("get record: %v", err)
	}
	if rec.CurrentRevisionID != revD {
		t.Fatalf("expected current revision to be Rev D (%s), got %s", revD, rec.CurrentRevisionID)
	}

	// 3. Run processing cycle while Proposal A is still PENDING
	// This cycle will process Rev B (pruned), Rev C (pruned), Rev D (head -> suppressed to PENDING)
	cycle2, err := f.app.ProcessPendingSources(ctx)
	if err != nil {
		t.Fatalf("cycle2: %v", err)
	}
	// B and C are pruned (completed), D is deferred (AlreadyPending -> not completed)
	// So claimed=3, completed=2
	if cycle2.Claimed != 3 || cycle2.Completed != 2 {
		t.Fatalf("cycle2 expected claimed=3, completed=2; got claimed=%d, completed=%d", cycle2.Claimed, cycle2.Completed)
	}

	// ZERO new AI calls during this pruning/suppression cycle!
	if f.mockAI.callCounts[ai.ActionSourceInterpret] != aiCallsAfterA {
		t.Fatalf("expected 0 AI calls during multi-revision pruning, got %d (was %d)",
			f.mockAI.callCounts[ai.ActionSourceInterpret], aiCallsAfterA)
	}

	// Verify Rev B state: COMPLETED, proposal_id == nil
	stateB, _ := f.sourceService.GetProcessingState(ctx, revB)
	if stateB.State != source.ProcessingStatusCompleted || stateB.ProposalID != nil {
		t.Fatalf("expected Rev B COMPLETED with nil proposal_id, got state=%s, prop=%v", stateB.State, stateB.ProposalID)
	}

	// Verify Rev C state: COMPLETED, proposal_id == nil
	stateC, _ := f.sourceService.GetProcessingState(ctx, revC)
	if stateC.State != source.ProcessingStatusCompleted || stateC.ProposalID != nil {
		t.Fatalf("expected Rev C COMPLETED with nil proposal_id, got state=%s, prop=%v", stateC.State, stateC.ProposalID)
	}

	// Verify Rev D state: PENDING, proposal_id == nil
	stateD, _ := f.sourceService.GetProcessingState(ctx, revD)
	if stateD.State != source.ProcessingStatusPending || stateD.ProposalID != nil {
		t.Fatalf("expected Rev D PENDING with nil proposal_id, got state=%s, prop=%v", stateD.State, stateD.ProposalID)
	}

	// 4. Resolve Proposal A
	_, err = f.app.ApplyProposal(ApplyProposalRequestDTO{ID: proposalAID})
	if err != nil {
		t.Fatalf("ApplyProposal A: %v", err)
	}

	// 5. Run next cycle -> only Rev D is eligible!
	cycle3, err := f.app.ProcessPendingSources(ctx)
	if err != nil {
		t.Fatalf("cycle3: %v", err)
	}
	if cycle3.Claimed != 1 || cycle3.Completed != 1 {
		t.Fatalf("cycle3 expected claimed=1, completed=1; got claimed=%d, completed=%d", cycle3.Claimed, cycle3.Completed)
	}

	// Rev D creates new Proposal P2
	stateDFinal, _ := f.sourceService.GetProcessingState(ctx, revD)
	if stateDFinal.State != source.ProcessingStatusCompleted || stateDFinal.ProposalID == nil || *stateDFinal.ProposalID == proposalAID {
		t.Fatalf("expected Rev D COMPLETED with new proposal, got %+v", stateDFinal)
	}

	var linkedRevID string
	err = f.db.QueryRow("SELECT source_revision_id FROM change_proposal_sources WHERE proposal_id = ?", *stateDFinal.ProposalID).Scan(&linkedRevID)
	if err != nil {
		t.Fatalf("query change_proposal_sources for proposal D: %v", err)
	}
	if linkedRevID != revD {
		t.Fatalf("expected Proposal D lineage to point to Rev D (%s), got %s", revD, linkedRevID)
	}
}

// TestR3C_ScenarioE_RestartSafety verifies:
// While Proposal P is pending, Rev B is deferred to PENDING.
// Application restarts / FailOrphanedProcessing runs.
// Rev B remains in PENDING state (not converted to FAILED).
// After Proposal P is applied, Rev B is processed cleanly.
func TestR3C_ScenarioE_RestartSafety(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()

	revA := ingestTestObservation(t, f, "inst-1", "ext-task-restart", "Task V1", "Rev A")
	_, err := f.app.ProcessPendingSources(ctx)
	if err != nil {
		t.Fatalf("process Rev A: %v", err)
	}
	stateA, _ := f.sourceService.GetProcessingState(ctx, revA)
	proposalAID := *stateA.ProposalID

	revB := ingestTestObservation(t, f, "inst-1", "ext-task-restart", "Task V2", "Rev B")

	// Run cycle to defer Rev B
	_, err = f.app.ProcessPendingSources(ctx)
	if err != nil {
		t.Fatalf("cycle2: %v", err)
	}

	stateBBeforeRestart, _ := f.sourceService.GetProcessingState(ctx, revB)
	if stateBBeforeRestart.State != source.ProcessingStatusPending {
		t.Fatalf("expected Rev B PENDING before restart, got %s", stateBBeforeRestart.State)
	}

	// Simulate restart recovery: FailOrphanedProcessing is called during startup
	orphanedCount, err := f.sourceService.FailOrphanedProcessing(ctx)
	if err != nil {
		t.Fatalf("FailOrphanedProcessing: %v", err)
	}
	if orphanedCount != 0 {
		t.Fatalf("expected 0 orphaned rows failed, got %d", orphanedCount)
	}

	// Restart the fixture (closes and re-opens DB)
	f.restart()

	// Verify Rev B is STILL in PENDING state
	stateBAfterRestart, err := f.sourceService.GetProcessingState(ctx, revB)
	if err != nil || stateBAfterRestart == nil {
		t.Fatalf("get stateB after restart: %v", err)
	}
	if stateBAfterRestart.State != source.ProcessingStatusPending {
		t.Fatalf("expected Rev B to remain PENDING after restart, got %s", stateBAfterRestart.State)
	}

	// Resolve Proposal A
	_, err = f.app.ApplyProposal(ApplyProposalRequestDTO{ID: proposalAID})
	if err != nil {
		t.Fatalf("ApplyProposal A: %v", err)
	}

	// Run cycle after restart and resolution
	cycle3, err := f.app.ProcessPendingSources(ctx)
	if err != nil {
		t.Fatalf("cycle3: %v", err)
	}
	if cycle3.Claimed != 1 || cycle3.Completed != 1 {
		t.Fatalf("cycle3 expected claimed=1, completed=1; got claimed=%d, completed=%d", cycle3.Claimed, cycle3.Completed)
	}

	stateBFinal, _ := f.sourceService.GetProcessingState(ctx, revB)
	if stateBFinal.State != source.ProcessingStatusCompleted || stateBFinal.ProposalID == nil || *stateBFinal.ProposalID == proposalAID {
		t.Fatalf("expected Rev B COMPLETED with new proposal after restart, got %+v", stateBFinal)
	}
}

// TestR3C_ScenarioF_NoSameCycleHotLoop verifies:
// With Proposal P pending and Rev B released back to PENDING:
// ProcessPendingSources terminates immediately with 1 claim attempt, 0 AI calls,
// and leaves Rev B in PENDING without any same-cycle re-claim loop.
func TestR3C_ScenarioF_NoSameCycleHotLoop(t *testing.T) {
	f := newRound2Fixture(t)
	ctx := context.Background()

	revA := ingestTestObservation(t, f, "inst-1", "ext-task-loop", "Task V1", "Rev A")
	_, err := f.app.ProcessPendingSources(ctx)
	if err != nil {
		t.Fatalf("process Rev A: %v", err)
	}
	stateA, _ := f.sourceService.GetProcessingState(ctx, revA)
	if stateA.ProposalID == nil {
		t.Fatal("expected Proposal A")
	}

	aiCallsAfterA := f.mockAI.callCounts[ai.ActionSourceInterpret]

	revB := ingestTestObservation(t, f, "inst-1", "ext-task-loop", "Task V2", "Rev B")

	// Run ProcessPendingSources. If there were a hot loop, this would either loop or claim multiple times.
	cycleRes, err := f.app.ProcessPendingSources(ctx)
	if err != nil {
		t.Fatalf("ProcessPendingSources: %v", err)
	}

	// Exactly 1 scanned, 1 claimed, 0 completed, 0 failed
	if cycleRes.Scanned != 1 || cycleRes.Claimed != 1 || cycleRes.Completed != 0 || cycleRes.Failed != 0 {
		t.Fatalf("unexpected cycle result: %+v", cycleRes)
	}

	// Zero AI calls
	if f.mockAI.callCounts[ai.ActionSourceInterpret] != aiCallsAfterA {
		t.Fatalf("expected 0 AI calls, got %d (was %d)", f.mockAI.callCounts[ai.ActionSourceInterpret], aiCallsAfterA)
	}

	// Rev B is in PENDING state
	stateB, _ := f.sourceService.GetProcessingState(ctx, revB)
	if stateB.State != source.ProcessingStatusPending {
		t.Fatalf("expected Rev B in PENDING state, got %s", stateB.State)
	}
}
