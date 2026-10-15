package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/applicationruntime"
	"lum-harbor-notifier/internal/assistant"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/proposalcap"
	"lum-harbor-notifier/internal/readcap"
	"lum-harbor-notifier/internal/reminder"
	"lum-harbor-notifier/internal/source"
	"lum-harbor-notifier/internal/sourceprocessing"
	"lum-harbor-notifier/internal/tracking"
)

// CaptureLocalSource is the compatibility boundary for injecting text/notes into the durable Source Layer.
func (app *App) CaptureLocalSource(request LocalSourceCaptureRequestDTO) (LocalSourceDTO, error) {
	if app.sourceService == nil {
		return LocalSourceDTO{}, fmt.Errorf("source service is not initialized")
	}
	title := strings.TrimSpace(request.Title)
	content := strings.TrimSpace(request.Content)
	if title == "" && content == "" {
		return LocalSourceDTO{}, fmt.Errorf("source title or content must not be empty")
	}
	sourceKind := "NOTE"
	if request.SourceKind != nil && strings.TrimSpace(*request.SourceKind) != "" {
		sourceKind = strings.TrimSpace(*request.SourceKind)
	}

	var extID string
	if request.ExternalID != nil {
		extID = strings.TrimSpace(*request.ExternalID)
	}

	rev, err := app.sourceService.CaptureLocalSource(appContext(app.ctx), source.CaptureLocalSourceInput{
		Title:      title,
		Body:       content,
		SourceKind: sourceKind,
		ExternalID: extID,
	})
	if err != nil {
		return LocalSourceDTO{}, fmt.Errorf("capture source: %w", err)
	}

	return LocalSourceDTO{
		SourceRecordID:   rev.SourceRecordID,
		SourceRevisionID: rev.ID,
		SourceKind:       sourceKind,
		Title:            title,
		Content:          content,
		ContentHash:      rev.ContentHash,
		CreatedAt:        rev.CreatedAt.UTC().Format(time.RFC3339Nano),
	}, nil
}

// ListLocalSources is the compatibility boundary for Sources captured by core.local.
func (app *App) ListLocalSources() ([]LocalSourceDTO, error) {
	if app.sourceService == nil {
		return nil, fmt.Errorf("source service is not initialized")
	}
	heads, err := app.sourceService.ListLocalSources(appContext(app.ctx))
	if err != nil {
		return nil, fmt.Errorf("list sources: %w", err)
	}
	dtos := make([]LocalSourceDTO, 0, len(heads))
	for _, h := range heads {
		var payload struct {
			Title string `json:"title"`
			Body  string `json:"body"`
		}
		_ = json.Unmarshal(h.Payload, &payload)
		dtos = append(dtos, LocalSourceDTO{
			SourceRecordID:   h.RecordID,
			SourceRevisionID: h.CurrentRevisionID,
			SourceKind:       h.SourceKind,
			Title:            payload.Title,
			Content:          payload.Body,
			ContentHash:      h.CurrentContentHash,
			CreatedAt:        h.ObservedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return dtos, nil
}

// InterpretSource orchestrates AI understanding of an immutable Source Revision
// and dispatches a validated Mutation Candidate to the Proposal registry with full provenance.
func (app *App) InterpretSource(request InterpretSourceRequestDTO) (SourceInterpretationResultDTO, error) {
	return app.InterpretSourceCtx(appContext(app.ctx), request)
}

// InterpretSourceCtx executes interpretation within a provided context and guarantees
// durable Source processing state transitions (COMPLETED with proposal or FAILED with error).
func (app *App) InterpretSourceCtx(ctx context.Context, request InterpretSourceRequestDTO) (SourceInterpretationResultDTO, error) {
	if app.sourceProcessor == nil {
		return SourceInterpretationResultDTO{}, fmt.Errorf("source processor is not initialized")
	}
	if app.proposalService == nil || app.proposeRegistry == nil {
		return SourceInterpretationResultDTO{}, fmt.Errorf("proposal subsystem is not initialized")
	}
	revID := strings.TrimSpace(request.SourceRevisionID)
	if revID == "" {
		return SourceInterpretationResultDTO{}, fmt.Errorf("source revision ID must not be empty")
	}
	// The terminal processing state must be recorded even when ctx was
	// cancelled (for example a background cycle deadline); otherwise the
	// Source would stay PROCESSING forever.
	stateCtx := context.WithoutCancel(ctx)

	// Bounded repeat interpretation rule (V1 product contract) / Crash Recovery:
	// If the same immutable SourceRevision already has a PENDING Proposal:
	// DO NOT call AI again. DO NOT execute source.clean, source.interpret, or provider token generation.
	// Return the existing PENDING proposal and notify the UI that review is already pending.
	existingPending, err := app.proposalService.FindPendingBySourceRevision(ctx, revID)
	if err == nil && existingPending != nil {
		if app.sourceService != nil {
			_ = app.sourceService.CompleteProcessingWithProposal(stateCtx, revID, existingPending.ID)
		}
		propDTO := app.proposalReviewDTO(*existingPending)
		var interpretRunID string
		if existingPending.AIRunID != nil {
			interpretRunID = *existingPending.AIRunID
		}
		var summary string
		if existingPending.Summary != nil {
			summary = *existingPending.Summary
		} else {
			summary = existingPending.Title
		}
		return SourceInterpretationResultDTO{
			SourceRevisionID: revID,
			InterpretRunID:   interpretRunID,
			Summary:          summary,
			HasProposal:      true,
			Proposal:         &propDTO,
			Notice:           "This source already has a proposal waiting for review.",
			AlreadyPending:   true,
		}, nil
	}

	var rev *source.SourceRevision
	var record *source.SourceRecord
	if app.sourceService != nil {
		rev, _ = app.sourceService.GetSourceRevision(ctx, revID)
		if rev != nil {
			record, _ = app.sourceService.GetSourceRecord(ctx, rev.SourceRecordID)
		}
	}

	// Head-Revision Pruning:
	// If the claimed revision is not the authoritative current revision of the source record,
	// it is an obsolete intermediate revision superseded by a newer revision.
	// Retire it without AI and without linking to any older proposal.
	// This COMPLETED state means: authoritatively superseded by a later SourceRevision; no semantic proposal required.
	if record != nil && record.CurrentRevisionID != "" && revID != record.CurrentRevisionID {
		if app.sourceService != nil {
			_ = app.sourceService.CompleteProcessing(stateCtx, revID)
		}
		return SourceInterpretationResultDTO{
			SourceRevisionID: revID,
			Notice:           "Source revision superseded by a newer revision; interpretation retired.",
		}, nil
	}

	// Pending Proposal Visibility for External Tasks:
	// Check whether ANY earlier revision of this same Source Record already has a pending proposal
	// waiting for human review.
	var pendingForRecord *proposal.Proposal
	if rev != nil {
		pendingForRecord, _ = app.findPendingForSourceRecord(ctx, rev.SourceRecordID)
	}

	if pendingForRecord != nil {
		// An existing pending proposal for this external task already awaits human review.
		// DO NOT mark this revision COMPLETED; DO NOT link it to the old proposal.
		// Release the processing claim back to PENDING so it remains eligible for processing
		// once the prior proposal is resolved (APPLIED, REJECTED, or STALE).
		if app.sourceService != nil {
			_, _ = app.sourceService.ReleaseClaimToPending(stateCtx, revID)
		}
		propDTO := app.proposalReviewDTO(*pendingForRecord)
		var interpretRunID string
		if pendingForRecord.AIRunID != nil {
			interpretRunID = *pendingForRecord.AIRunID
		}
		var summary string
		if pendingForRecord.Summary != nil {
			summary = *pendingForRecord.Summary
		} else {
			summary = pendingForRecord.Title
		}
		return SourceInterpretationResultDTO{
			SourceRevisionID: revID,
			InterpretRunID:   interpretRunID,
			Summary:          summary,
			HasProposal:      true,
			Proposal:         &propDTO,
			Notice:           "This external task already has a proposal waiting for review (" + pendingForRecord.Title + "). Latest revision remains pending.",
			AlreadyPending:   true,
		}, nil
	}

	// Only if no PENDING proposal exists (e.g. first interpretation, or previous was APPLIED / REJECTED / STALE):
	// execute real source processing (clean + interpret).
	resolvedContext, err := app.resolveSourceTrackedItemContext(ctx, record, rev)
	if err != nil {
		if app.sourceService != nil {
			_ = app.sourceService.FailProcessing(stateCtx, revID, err.Error())
		}
		return SourceInterpretationResultDTO{SourceRevisionID: revID}, fmt.Errorf("resolve source context: %w", err)
	}
	interpretationContext, err := sourceInterpretationContextWithPending(time.Now(), app.proposeRegistry.ListDescriptors(), pendingForRecord, resolvedContext)
	if err != nil {
		if app.sourceService != nil {
			_ = app.sourceService.FailProcessing(stateCtx, revID, err.Error())
		}
		return SourceInterpretationResultDTO{SourceRevisionID: revID}, fmt.Errorf("build interpretation context: %w", err)
	}
	procResult, err := app.sourceProcessor.ProcessRevision(ctx, sourceprocessing.ProcessRequest{
		SourceRevisionID:      revID,
		InterpretationContext: interpretationContext,
	})
	if err != nil {
		if app.sourceService != nil {
			_ = app.sourceService.FailProcessing(stateCtx, revID, err.Error())
		}
		return SourceInterpretationResultDTO{
			SourceRevisionID: revID,
		}, fmt.Errorf("interpret source: %w", err)
	}

	resultDTO := SourceInterpretationResultDTO{
		SourceRevisionID: revID,
		CleanRunID:       procResult.CleanRunID,
		InterpretRunID:   procResult.InterpretRunID,
		Summary:          procResult.InterpretResult.Summary,
	}

	// Extract the reviewable suggestion: none, one candidate, or one plan.
	candidate, plan, err := extractInterpretSuggestion(procResult.InterpretResult)
	if err != nil {
		if app.sourceService != nil {
			_ = app.sourceService.FailProcessing(stateCtx, revID, err.Error())
		}
		return resultDTO, fmt.Errorf("interpret source: %w", err)
	}
	if plan != nil {
		return app.createSourcePlanProposal(ctx, stateCtx, resultDTO, plan, resolvedContext, procResult.InterpretRunID, revID)
	}
	if candidate == nil {
		if app.sourceService != nil {
			_ = app.sourceService.CompleteProcessing(stateCtx, revID)
		}
		return resultDTO, nil
	}

	if resolvedContext != nil && candidate.CapabilityID == proposal.OperationTypePlannerItemCreate {
		var args map[string]any
		if err := json.Unmarshal(candidate.Arguments, &args); err != nil {
			_ = app.sourceService.FailProcessing(stateCtx, revID, err.Error())
			return resultDTO, fmt.Errorf("validate resolved tracking consequence: %w", err)
		}
		candidateTrackedItemID, _ := args["tracked_item_id"].(string)
		if strings.TrimSpace(candidateTrackedItemID) != resolvedContext.TrackedItem.ID {
			err := fmt.Errorf("planner.item.create for resolved source must target tracked_item_id %s", resolvedContext.TrackedItem.ID)
			_ = app.sourceService.FailProcessing(stateCtx, revID, err.Error())
			return resultDTO, err
		}
		if !candidateHasDueTarget(args) {
			err := fmt.Errorf("planner.item.create for resolved source must include a due date or time")
			_ = app.sourceService.FailProcessing(stateCtx, revID, err.Error())
			return resultDTO, err
		}
		hasActive, err := app.plannerService.HasOpenTrackingTodoFromSourceRecord(ctx, resolvedContext.TrackedItem.ID, resolvedContext.Source.RecordID)
		if err != nil {
			_ = app.sourceService.FailProcessing(stateCtx, revID, err.Error())
			return resultDTO, fmt.Errorf("check active source consequence: %w", err)
		}
		if hasActive {
			_ = app.sourceService.CompleteProcessing(stateCtx, revID)
			resultDTO.Notice = "Equivalent active action already exists for this tracked item."
			return resultDTO, nil
		}
	}

	// Execute through proposeRegistry with full provenance:
	// - AIRunID = procResult.InterpretRunID
	// - SourceRevisionIDs = [revID]
	execCtx := ContextWithProposeProvenance(ctx, ProposeProvenance{
		AIRunID:           &procResult.InterpretRunID,
		SourceRevisionIDs: []string{revID},
	})

	created, err := app.proposeRegistry.Execute(execCtx, app, candidate.CapabilityID, nil, candidate.Arguments, candidate.Reason)
	if err != nil {
		if app.sourceService != nil {
			_ = app.sourceService.FailProcessing(stateCtx, revID, err.Error())
		}
		return resultDTO, fmt.Errorf("create proposal from interpretation: %w", err)
	}

	if app.sourceService != nil {
		_ = app.sourceService.CompleteProcessingWithProposal(stateCtx, revID, created.ID)
	}

	propDTO := app.proposalReviewDTO(*created)
	resultDTO.HasProposal = true
	resultDTO.Proposal = &propDTO

	return resultDTO, nil
}

// createSourcePlanProposal persists a multi-operation interpretation plan as
// ONE aggregate Proposal linked to the exact interpreted SourceRevision and
// interpret AI Run. A Source resolved to an existing TrackedItem keeps its
// single bounded consequence and never accepts a plan.
func (app *App) createSourcePlanProposal(ctx, stateCtx context.Context, resultDTO SourceInterpretationResultDTO, plan *assistant.MutationPlan, resolvedContext *sourceTrackedItemContext, interpretRunID, revID string) (SourceInterpretationResultDTO, error) {
	fail := func(err error) (SourceInterpretationResultDTO, error) {
		if app.sourceService != nil {
			_ = app.sourceService.FailProcessing(stateCtx, revID, err.Error())
		}
		return resultDTO, fmt.Errorf("create proposal from interpretation: %w", err)
	}
	if resolvedContext != nil {
		return fail(fmt.Errorf("a source resolved to tracked item %s accepts at most one action, not a %d-operation plan", resolvedContext.TrackedItem.ID, len(plan.Operations)))
	}
	if app.proposalService == nil || app.proposeRegistry == nil {
		return fail(fmt.Errorf("proposal services are not initialized"))
	}
	created, err := app.createAggregateFromPlan(ctx, plan, nil, &interpretRunID, []string{revID})
	if err != nil {
		return fail(err)
	}
	if app.sourceService != nil {
		_ = app.sourceService.CompleteProcessingWithProposal(stateCtx, revID, created.ID)
	}
	propDTO := app.proposalReviewDTO(*created)
	resultDTO.HasProposal = true
	resultDTO.Proposal = &propDTO
	return resultDTO, nil
}

// findPendingForSourceRecord scans pending proposals to check whether any revision
// belonging to the given sourceRecordID has a proposal currently waiting for user review.
func (app *App) findPendingForSourceRecord(ctx context.Context, sourceRecordID string) (*proposal.Proposal, error) {
	if app.proposalService == nil || app.sourceService == nil {
		return nil, nil
	}
	pendingList, err := app.proposalService.ListPending(ctx)
	if err != nil {
		return nil, fmt.Errorf("list pending proposals: %w", err)
	}
	for _, p := range pendingList {
		for _, revID := range p.SourceRevisionIDs {
			rev, err := app.sourceService.GetSourceRevision(ctx, revID)
			if err == nil && rev != nil && rev.SourceRecordID == sourceRecordID {
				return &p, nil
			}
		}
	}
	return nil, nil
}

// SourceProcessingBatchLimit bounds how many pending Sources one background
// processing cycle claims.
const SourceProcessingBatchLimit = 10

// ProcessPendingSources detects pending semantic sources, safely claims them, runs interpretation,
// and produces proposals with durable state updates. It fulfills applicationruntime.SourceProcessingCycle.
func (app *App) ProcessPendingSources(ctx context.Context) (applicationruntime.SourceProcessingCycleResult, error) {
	if app.sourceService == nil {
		return applicationruntime.SourceProcessingCycleResult{}, nil
	}
	pending, err := app.sourceService.ListPendingSemanticSources(ctx, SourceProcessingBatchLimit)
	if err != nil {
		return applicationruntime.SourceProcessingCycleResult{}, fmt.Errorf("list pending semantic sources: %w", err)
	}
	result := applicationruntime.SourceProcessingCycleResult{Scanned: len(pending)}
	for _, item := range pending {
		claimed, err := app.sourceService.ClaimPendingProcessing(ctx, item.SourceRevisionID)
		if err != nil {
			slog.Warn("failed to claim pending source processing", "revision_id", item.SourceRevisionID, "error", ai.SafeLogText(err.Error(), 320))
			continue
		}
		if !claimed {
			continue
		}
		result.Claimed++

		res, err := app.InterpretSourceCtx(ctx, InterpretSourceRequestDTO{
			SourceRevisionID: item.SourceRevisionID,
		})
		if err != nil {
			result.Failed++
			slog.Warn("source processing failed", "revision_id", item.SourceRevisionID, "error", ai.SafeLogText(err.Error(), 320))
			continue
		}
		if !res.AlreadyPending {
			result.Completed++
		}
	}
	return result, nil
}

// sourceCandidateRules mirror the Assistant's Prompt 2 date/time rules so a
// Source-derived candidate resolves "tomorrow at 3 PM" exactly like a
// conversational request would.
var sourceCandidateRules = []string{
	"Use only facts stated in the source and this context; never invent dates, times, or entities.",
	"title is the action to perform (for example \"Review the vendor security compliance checklist\"), not a description of the source.",
	"When the source gives a due date, deadline, or dueDate (e.g. \"2026-10-01\", \"due by 2026-10-01\", or relative dates like \"tomorrow\"), compute or format the local calendar date (YYYY-MM-DD) and put it in due_date.",
	"When the source gives an explicit clock time (such as \"3 PM\" or \"15:00\" or \"dueTime\"), put it in due_time using only local clock text such as HH:MM or h:mm AM/PM. Never put an ISO/RFC3339 value or a timezone suffix such as Z in due_time; use due_at for an exact RFC3339 timestamp. Omit due_time when no explicit clock time is given; do not guess.",
	"Omit timezone unless the source names one; the host system timezone is used automatically.",
	"External source identifiers (such as workflowId, story ID, or external issue keys) belong to external provenance and must NOT be put into workflow_id or work_item_id; omit workflow_id and work_item_id.",
	"When resolvedTrackedItemContext is present, create planner.item.create only when current evidence requires a new user action; set tracked_item_id to that exact internal ID and include an actionable due date/time.",
	"If no new action is required, or an equivalent OPEN linked Todo already represents the current consequence, return candidate as null.",
	"A plan is allowed only when the source asks for two or more distinct actions and resolvedTrackedItemContext is absent; one source produces at most one plan, with the minimum number of operations and never more than 20.",
	"Inside a plan, an argument that says it accepts an earlier-operation reference may be exactly \"$op[N].<output>\" naming an EARLIER operation; never put references in titles or other text and never invent IDs.",
}

type sourceTrackedItemContext struct {
	Source struct {
		RecordID   string `json:"recordId"`
		RevisionID string `json:"revisionId"`
		ExternalID string `json:"externalId"`
	} `json:"source"`
	TrackedItem struct {
		ID         string `json:"id"`
		Title      string `json:"title"`
		Lifecycle  string `json:"lifecycle"`
		WorkItemID string `json:"workItemId"`
		WorkflowID string `json:"workflowId"`
	} `json:"trackedItem"`
	Progression *struct {
		CurrentStepID *string `json:"currentStepId"`
		CurrentState  *string `json:"currentState"`
		Complete      bool    `json:"complete"`
	} `json:"progression,omitempty"`
	OpenTodos []sourceOpenTodoContext `json:"openTodos"`
}

type sourceOpenTodoContext struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	DueAt    *string `json:"dueAt,omitempty"`
	DueDate  *string `json:"dueDate,omitempty"`
	Reminder *struct {
		State          string `json:"state"`
		OffsetsSeconds []int  `json:"offsetsSeconds"`
	} `json:"reminder,omitempty"`
}

func (app *App) resolveSourceTrackedItemContext(ctx context.Context, record *source.SourceRecord, rev *source.SourceRevision) (*sourceTrackedItemContext, error) {
	if record == nil || rev == nil || app.sourceContextReader == nil || app.trackingService == nil || app.plannerService == nil || strings.TrimSpace(record.ExternalID) == "" {
		return nil, nil
	}
	input, err := json.Marshal(readcap.ResolveExternalInput{ExternalID: record.ExternalID})
	if err != nil {
		return nil, err
	}
	resolved, err := app.sourceContextReader.Execute(ctx, readcap.Request{CapabilityID: readcap.CapabilityResolveExternal, Input: input})
	if err != nil {
		if errors.Is(err, readcap.ErrEntityNotFound) {
			return nil, nil
		}
		return nil, err
	}
	identity, ok := resolved.Output.(readcap.ResolveExternalOutput)
	if !ok {
		return nil, fmt.Errorf("entity.resolve_external returned unexpected output %T", resolved.Output)
	}
	if identity.EntityType != "TRACKED_ITEM" {
		return nil, nil
	}
	tracked, err := app.trackingService.GetTrackedItem(ctx, identity.EntityID)
	if err != nil {
		return nil, err
	}
	return app.buildTrackedItemContext(ctx, record, rev, tracked)
}

func (app *App) buildTrackedItemContext(ctx context.Context, record *source.SourceRecord, rev *source.SourceRevision, tracked *tracking.TrackedItem) (*sourceTrackedItemContext, error) {
	trackingView, err := app.trackingService.GetWorkItemTracking(ctx, tracked.WorkItemID)
	if err != nil {
		return nil, err
	}
	openTodos, err := app.plannerService.ListOpenByTrackedItemID(ctx, tracked.ID)
	if err != nil {
		return nil, err
	}

	result := &sourceTrackedItemContext{OpenTodos: make([]sourceOpenTodoContext, 0, len(openTodos))}
	result.Source.RecordID = record.ID
	result.Source.RevisionID = rev.ID
	result.Source.ExternalID = record.ExternalID
	result.TrackedItem.ID = tracked.ID
	result.TrackedItem.Title = tracked.Title
	result.TrackedItem.Lifecycle = string(tracked.Lifecycle)
	result.TrackedItem.WorkItemID = tracked.WorkItemID
	result.TrackedItem.WorkflowID = tracked.WorkflowID
	for _, item := range trackingView.Items {
		if item.Item.ID != tracked.ID || item.Progression == nil {
			continue
		}
		progression := &struct {
			CurrentStepID *string `json:"currentStepId"`
			CurrentState  *string `json:"currentState"`
			Complete      bool    `json:"complete"`
		}{CurrentStepID: item.Progression.CurrentStepID, Complete: item.Progression.Complete}
		if item.Progression.CurrentState != nil {
			state := string(*item.Progression.CurrentState)
			progression.CurrentState = &state
		}
		result.Progression = progression
		break
	}
	for _, item := range openTodos {
		todo := sourceOpenTodoContext{ID: item.ID, Title: item.Title}
		if item.DueAt != nil {
			value := item.DueAt.UTC().Format(time.RFC3339Nano)
			todo.DueAt = &value
		}
		if item.DueDate != nil {
			value := string(*item.DueDate)
			todo.DueDate = &value
		}
		if app.reminderService != nil {
			rem, remErr := app.reminderService.GetByPlannerItemID(ctx, item.ID)
			if remErr != nil && !errors.Is(remErr, reminder.ErrNotFound) {
				return nil, remErr
			}
			if rem != nil {
				todo.Reminder = &struct {
					State          string `json:"state"`
					OffsetsSeconds []int  `json:"offsetsSeconds"`
				}{State: string(rem.State), OffsetsSeconds: append([]int(nil), rem.OffsetsSeconds...)}
			}
		}
		result.OpenTodos = append(result.OpenTodos, todo)
	}
	return result, nil
}

// sourceInterpretationContext is the authoritative, immutable context
// snapshot supplied to source.interpret: the host's current local date/time
// and the Proposal candidate contract. Only capabilities that need no
// selected Work Item are offered, because Source interpretation has no base
// Work Item.
func sourceInterpretationContext(now time.Time, descriptors []proposalcap.Descriptor) (json.RawMessage, error) {
	return sourceInterpretationContextWithPending(now, descriptors, nil)
}

func sourceInterpretationContextWithPending(now time.Time, descriptors []proposalcap.Descriptor, pendingProposal *proposal.Proposal, resolvedContexts ...*sourceTrackedItemContext) (json.RawMessage, error) {
	type argumentContract struct {
		Type        string `json:"type"`
		Required    bool   `json:"required"`
		Description string `json:"description"`
	}
	type capabilityContract struct {
		ID        string                      `json:"id"`
		Summary   string                      `json:"summary"`
		UseWhen   string                      `json:"useWhen,omitempty"`
		Arguments map[string]argumentContract `json:"arguments"`
	}
	capabilities := []capabilityContract{}
	for _, d := range descriptors {
		if d.RequiresWorkItemContext || !d.SourceInterpretEligible {
			continue
		}
		args := make(map[string]argumentContract, len(d.Arguments))
		for name, spec := range d.Arguments {
			args[name] = argumentContract{Type: string(spec.Type), Required: spec.Required, Description: spec.Description}
		}
		capabilities = append(capabilities, capabilityContract{ID: d.ID, Summary: d.Summary, UseWhen: d.UseWhen, Arguments: args})
	}
	ctxMap := map[string]any{
		"authoritativeRuntimeContext": assistant.ApplicationContext{Now: now}.FormatAuthoritativeContext(nil),
		"candidateContract": map[string]any{
			"capabilities": capabilities,
			"rules":        sourceCandidateRules,
		},
	}
	if pendingProposal != nil {
		ctxMap["pendingReviewState"] = map[string]any{
			"hasPendingProposal": true,
			"pendingProposalId":  pendingProposal.ID,
			"title":              pendingProposal.Title,
			"operationType":      pendingProposal.OperationType,
			"status":             string(pendingProposal.Status),
			"createdAt":          pendingProposal.CreatedAt.UTC().Format(time.RFC3339),
		}
	}
	if len(resolvedContexts) > 0 && resolvedContexts[0] != nil {
		ctxMap["resolvedTrackedItemContext"] = resolvedContexts[0]
	}
	return json.Marshal(ctxMap)
}

func extractMutationCandidate(res sourceprocessing.InterpretOutputV1) (assistant.MutationCandidate, bool) {
	var doc struct {
		Candidate json.RawMessage `json:"candidate"`
	}
	// 1. Try decoding structured candidate in interpretationDocument
	if len(res.InterpretationDocument) > 0 {
		if err := json.Unmarshal(res.InterpretationDocument, &doc); err == nil && len(doc.Candidate) > 0 && string(doc.Candidate) != "null" {
			if cand, err := assistant.DecodeCandidate(doc.Candidate); err == nil {
				return cand, true
			}
		}
	}

	return assistant.MutationCandidate{}, false
}

// extractInterpretSuggestion reads an interpretation's reviewable
// suggestion: nothing (NO ACTION), one candidate, or one plan. A plan with a
// single operation is returned as that candidate. An undecodable candidate
// keeps its existing no-action treatment; a malformed plan, or both a
// candidate and a plan, is an error.
func extractInterpretSuggestion(res sourceprocessing.InterpretOutputV1) (*assistant.MutationCandidate, *assistant.MutationPlan, error) {
	var doc struct {
		Plan json.RawMessage `json:"plan"`
	}
	candidate, hasCandidate := extractMutationCandidate(res)
	if len(res.InterpretationDocument) > 0 {
		_ = json.Unmarshal(res.InterpretationDocument, &doc)
	}
	if len(doc.Plan) == 0 || string(doc.Plan) == "null" {
		if !hasCandidate {
			return nil, nil, nil
		}
		return &candidate, nil, nil
	}
	if hasCandidate {
		return nil, nil, fmt.Errorf("interpretation must not contain both a candidate and a plan")
	}
	plan, err := assistant.DecodePlan(doc.Plan)
	if err != nil {
		return nil, nil, err
	}
	if len(plan.Operations) == 1 {
		single := plan.Operations[0]
		return &single, nil, nil
	}
	return nil, &plan, nil
}

func candidateHasDueTarget(args map[string]any) bool {
	for _, key := range []string{"due_at", "due_date", "due_time"} {
		if value, ok := args[key].(string); ok && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}
