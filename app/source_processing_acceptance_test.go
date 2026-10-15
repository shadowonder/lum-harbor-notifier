package app

import (
	"context"
	"errors"
	"testing"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/source"
)

func TestBackgroundSemanticSourceDetectionCycle(t *testing.T) {
	fixture := newRound2Fixture(t)
	ctx := context.Background()

	// 1. Out-of-band / local source created in PENDING semantic state
	localSource, err := fixture.app.CaptureLocalSource(LocalSourceCaptureRequestDTO{
		Title:   "Urgent security audit note",
		Content: "Need to audit SSL certificates before Friday",
	})
	if err != nil {
		t.Fatalf("failed to capture local source: %v", err)
	}

	state, err := fixture.sourceService.GetProcessingState(ctx, localSource.SourceRevisionID)
	if err != nil {
		t.Fatalf("failed to get processing state: %v", err)
	}
	if state == nil || state.State != source.ProcessingStatusPending {
		t.Fatalf("expected PENDING processing state, got %+v", state)
	}
	if state.ProposalID != nil {
		t.Fatalf("expected nil ProposalID initially, got %v", *state.ProposalID)
	}

	// 2. Background cycle runs: claims, cleans, interprets, creates proposal
	res, err := fixture.app.ProcessPendingSources(ctx)
	if err != nil {
		t.Fatalf("ProcessPendingSources failed: %v", err)
	}
	if res.Scanned != 1 || res.Claimed != 1 || res.Completed != 1 || res.Failed != 0 {
		t.Fatalf("expected 1 scanned/claimed/completed, got %+v", res)
	}

	// Verify durable processing state is COMPLETED with proposal_id
	stateAfter, err := fixture.sourceService.GetProcessingState(ctx, localSource.SourceRevisionID)
	if err != nil {
		t.Fatalf("failed to get processing state after cycle: %v", err)
	}
	if stateAfter.State != source.ProcessingStatusCompleted {
		t.Fatalf("expected COMPLETED state, got %v", stateAfter.State)
	}
	if stateAfter.ProposalID == nil || *stateAfter.ProposalID == "" {
		t.Fatal("expected non-nil ProposalID in completed state")
	}

	// Verify proposal was created in PENDING state (never auto-applied)
	prop, err := fixture.proposalService.GetByID(ctx, *stateAfter.ProposalID)
	if err != nil {
		t.Fatalf("failed to load created proposal: %v", err)
	}
	if prop.Status != "PENDING" {
		t.Fatalf("expected proposal status PENDING, got %s", prop.Status)
	}

	// 3. Crash recovery / Idempotency test:
	// Reset state back to PENDING to simulate a crash during or right before state finalization
	initialAICallCount := fixture.mockAI.callCounts[ai.ActionSourceInterpret]
	if _, err := fixture.sourceService.ClaimPendingProcessing(ctx, localSource.SourceRevisionID); err != nil {
		t.Fatalf("failed to reset to CLAIMED: %v", err)
	}
	// Run cycle again
	// Since state is CLAIMED, it won't be picked up by ListPendingSemanticSources directly,
	// but if InterpretSource is called on it directly or if reset to PENDING:
	_ = fixture.sourceService.FailProcessing(ctx, localSource.SourceRevisionID, "transient error")
	// Update back to PENDING directly or test InterpretSourceCtx idempotency
	interpRes, err := fixture.app.InterpretSourceCtx(ctx, InterpretSourceRequestDTO{
		SourceRevisionID: localSource.SourceRevisionID,
	})
	if err != nil {
		t.Fatalf("idempotent interpret source failed: %v", err)
	}
	if !interpRes.AlreadyPending {
		t.Fatal("expected AlreadyPending = true for existing proposal")
	}
	if fixture.mockAI.callCounts[ai.ActionSourceInterpret] != initialAICallCount {
		t.Fatalf("AI was called again during repeat interpretation! Expected %d, got %d",
			initialAICallCount, fixture.mockAI.callCounts[ai.ActionSourceInterpret])
	}

	// 4. Failure handling test:
	// Add another source and make AI fail
	failingSource, err := fixture.app.CaptureLocalSource(LocalSourceCaptureRequestDTO{
		Title:   "Broken note",
		Content: "This will trigger AI error",
	})
	if err != nil {
		t.Fatalf("failed to capture failing source: %v", err)
	}

	// Force AI to fail
	fixture.mockAI.mu.Lock()
	fixture.mockAI.cleanOutput = "INVALID JSON OUTPUT THAT BREAKS DECODING"
	fixture.mockAI.mu.Unlock()

	failRes, err := fixture.app.ProcessPendingSources(ctx)
	if err != nil {
		t.Fatalf("ProcessPendingSources unexpected hard failure: %v", err)
	}
	if failRes.Failed != 1 {
		t.Fatalf("expected 1 failed item, got %+v", failRes)
	}

	failedState, err := fixture.sourceService.GetProcessingState(ctx, failingSource.SourceRevisionID)
	if err != nil {
		t.Fatalf("failed to get failed processing state: %v", err)
	}
	if failedState.State != source.ProcessingStatusFailed {
		t.Fatalf("expected state FAILED, got %v", failedState.State)
	}
	if failedState.LastError == nil || *failedState.LastError == "" {
		t.Fatal("expected durable LastError to be populated on failure")
	}
}

// TestBackgroundCycleCancellationRecordsFailedNotProcessing reproduces the
// real GUI defect: the cycle deadline cancelled source.interpret, and the
// FAILED write reused the cancelled context, stranding the Source in
// PROCESSING forever.
func TestBackgroundCycleCancellationRecordsFailedNotProcessing(t *testing.T) {
	fixture := newRound2Fixture(t)
	captured, err := fixture.app.CaptureLocalSource(LocalSourceCaptureRequestDTO{
		Title:   "Deadline note",
		Content: "Need to audit AWS IAM permissions before next Tuesday.",
	})
	if err != nil {
		t.Fatalf("capture local source: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fixture.mockAI.mu.Lock()
	fixture.mockAI.onInterpret = cancel
	fixture.mockAI.mu.Unlock()

	result, err := fixture.app.ProcessPendingSources(ctx)
	if err != nil {
		t.Fatalf("ProcessPendingSources unexpected hard failure: %v", err)
	}
	if result.Failed != 1 {
		t.Fatalf("expected 1 failed item, got %+v", result)
	}
	state, err := fixture.sourceService.GetProcessingState(context.Background(), captured.SourceRevisionID)
	if err != nil {
		t.Fatalf("get processing state: %v", err)
	}
	if state.State != source.ProcessingStatusFailed || state.LastError == nil || *state.LastError == "" {
		t.Fatalf("a cancelled interpretation must be durably FAILED with its error, got %+v", state)
	}
}

func TestStartupRecoveryFailsInterruptedProcessingOnly(t *testing.T) {
	fixture := newRound2Fixture(t)
	ctx := context.Background()
	interrupted, err := fixture.app.CaptureLocalSource(LocalSourceCaptureRequestDTO{Title: "Interrupted", Content: "Review contract before Monday."})
	if err != nil {
		t.Fatalf("capture interrupted source: %v", err)
	}
	waiting, err := fixture.app.CaptureLocalSource(LocalSourceCaptureRequestDTO{Title: "Waiting", Content: "Plan offsite before Friday."})
	if err != nil {
		t.Fatalf("capture waiting source: %v", err)
	}
	if claimed, err := fixture.sourceService.ClaimPendingProcessing(ctx, interrupted.SourceRevisionID); err != nil || !claimed {
		t.Fatalf("claim interrupted source: claimed=%v err=%v", claimed, err)
	}

	count, err := fixture.sourceService.FailOrphanedProcessing(ctx)
	if err != nil || count != 1 {
		t.Fatalf("expected exactly 1 interrupted row recovered, got %d (err %v)", count, err)
	}
	failed, _ := fixture.sourceService.GetProcessingState(ctx, interrupted.SourceRevisionID)
	if failed == nil || failed.State != source.ProcessingStatusFailed || failed.LastError == nil {
		t.Fatalf("interrupted source must become FAILED with a reason, got %+v", failed)
	}
	pending, _ := fixture.sourceService.GetProcessingState(ctx, waiting.SourceRevisionID)
	if pending == nil || pending.State != source.ProcessingStatusPending {
		t.Fatalf("a PENDING source must stay PENDING for the cycle, got %+v", pending)
	}
}

func (m *mockAIService) FailNextClean(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// custom hook if needed
}

var _ = errors.New
