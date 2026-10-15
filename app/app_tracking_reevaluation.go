package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"lum-harbor-notifier/internal/ai"
	"lum-harbor-notifier/internal/applicationruntime"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/source"
	"lum-harbor-notifier/internal/sourceprocessing"
	"lum-harbor-notifier/internal/trackingreevaluation"
)

const (
	TrackingReevaluationBatchLimit    = 5
	trackingReevaluationEvidenceLimit = 8
	trackingReevaluationHistoryLimit  = 8
	trackingReevaluationPayloadLimit  = 4096
)

func (app *App) ProcessPendingTrackingReevaluations(ctx context.Context) (applicationruntime.TrackingReevaluationCycleResult, error) {
	if app.trackingReevaluationService == nil {
		return applicationruntime.TrackingReevaluationCycleResult{}, nil
	}
	pending, err := app.trackingReevaluationService.ListPending(ctx, TrackingReevaluationBatchLimit)
	if err != nil {
		return applicationruntime.TrackingReevaluationCycleResult{}, err
	}
	result := applicationruntime.TrackingReevaluationCycleResult{Scanned: len(pending)}
	for _, request := range pending {
		claimed, err := app.trackingReevaluationService.Claim(ctx, request.ID)
		if err != nil {
			slog.Warn("claim tracking reevaluation failed", "request_id", request.ID, "error", ai.SafeLogText(err.Error(), 320))
			continue
		}
		if !claimed {
			continue
		}
		result.Claimed++
		if err := app.processTrackingReevaluation(ctx, request); err != nil {
			result.Failed++
			if failErr := app.trackingReevaluationService.Fail(context.WithoutCancel(ctx), request.ID, err, nil); failErr != nil {
				slog.Warn("finalize tracking reevaluation failure", "request_id", request.ID, "error", ai.SafeLogText(failErr.Error(), 320))
			}
			slog.Warn("tracking reevaluation failed", "request_id", request.ID, "error", ai.SafeLogText(err.Error(), 320))
			continue
		}
		result.Completed++
	}
	return result, nil
}

func (app *App) processTrackingReevaluation(ctx context.Context, request trackingreevaluation.Request) error {
	if app.sourceService == nil || app.sourceProcessor == nil || app.proposeRegistry == nil || app.proposalService == nil || app.trackingService == nil || app.plannerService == nil {
		return fmt.Errorf("tracking reevaluation dependencies are not initialized")
	}
	completedTodo, err := app.plannerService.GetByID(ctx, request.PlannerItemID)
	if err != nil {
		return fmt.Errorf("load completed Todo: %w", err)
	}
	tracked, err := app.trackingService.GetTrackedItem(ctx, request.TrackedItemID)
	if err != nil {
		return fmt.Errorf("load related TrackedItem: %w", err)
	}
	evidence, err := app.trackingReevaluationService.ListRelevantEvidence(ctx, request, trackingReevaluationEvidenceLimit)
	if err != nil {
		return err
	}
	// No existing Source evidence means there is no business fact from which AI
	// may justify a mutation. Complete as NO ACTION; never manufacture a Source.
	if len(evidence) == 0 {
		return app.trackingReevaluationService.Complete(context.WithoutCancel(ctx), request.ID, nil, nil)
	}

	evidenceIDs := make([]string, 0, len(evidence))
	evidenceContext := make([]map[string]any, 0, len(evidence))
	var primaryRevision *source.SourceRevision
	var primaryRecord *source.SourceRecord
	for _, item := range evidence {
		revision, loadErr := app.sourceService.GetSourceRevision(ctx, item.SourceRevisionID)
		if loadErr != nil || revision == nil {
			continue
		}
		record, loadErr := app.sourceService.GetSourceRecord(ctx, revision.SourceRecordID)
		if loadErr != nil || record == nil {
			continue
		}
		if primaryRevision == nil {
			primaryRevision, primaryRecord = revision, record
		}
		evidenceIDs = append(evidenceIDs, revision.ID)
		var payload any
		if len(revision.Payload) <= trackingReevaluationPayloadLimit {
			_ = json.Unmarshal(revision.Payload, &payload)
		} else {
			payload = string(revision.Payload[:trackingReevaluationPayloadLimit]) + "…[bounded]"
		}
		evidenceContext = append(evidenceContext, map[string]any{
			"sourceRevisionId": revision.ID, "sourceRecordId": record.ID,
			"extensionId": record.ExtensionID, "sourceKind": record.SourceKind,
			"externalId": record.ExternalID, "currentHead": item.CurrentHead,
			"payload": payload,
		})
	}
	if primaryRevision == nil || primaryRecord == nil {
		return app.trackingReevaluationService.Complete(context.WithoutCancel(ctx), request.ID, nil, nil)
	}

	// If a previous attempt committed a relevant Proposal but crashed before
	// finalizing this request, adopt it rather than producing a duplicate.
	for _, revisionID := range evidenceIDs {
		pending, findErr := app.proposalService.FindPendingBySourceRevision(ctx, revisionID)
		if findErr == nil && pending != nil && proposalMatchesReevaluation(pending, tracked.ID, tracked.WorkItemID) {
			return app.trackingReevaluationService.Complete(context.WithoutCancel(ctx), request.ID, pending.AIRunID, &pending.ID)
		}
	}

	trackedContext, err := app.buildTrackedItemContext(ctx, primaryRecord, primaryRevision, tracked)
	if err != nil {
		return fmt.Errorf("build tracking reevaluation context: %w", err)
	}
	contextJSON, err := sourceInterpretationContextWithPending(time.Now(), app.proposeRegistry.ListDescriptors(), nil, trackedContext)
	if err != nil {
		return err
	}
	var contextMap map[string]any
	if err := json.Unmarshal(contextJSON, &contextMap); err != nil {
		return err
	}
	historyFacts, err := app.trackingReevaluationService.ListRelevantHistory(ctx, request, trackingReevaluationHistoryLimit)
	if err != nil {
		return err
	}
	contextMap["reevaluationTrigger"] = map[string]any{
		"kind": "TRACKING_TODO_COMPLETED", "requestId": request.ID,
		"completionRevisionId": request.CompletionRevisionID,
		"todo": map[string]any{
			"id": completedTodo.ID, "title": completedTodo.Title,
			"status": string(completedTodo.Status), "completedAt": completedTodo.CompletedAt,
		},
	}
	contextMap["relevantSourceEvidence"] = evidenceContext
	contextMap["relevantHistory"] = historyFacts
	appendReevaluationRules(contextMap, tracked.ID, tracked.Title, tracked.WorkItemID)
	contextJSON, err = json.Marshal(contextMap)
	if err != nil {
		return err
	}

	processed, err := app.sourceProcessor.ProcessRevision(ctx, sourceprocessing.ProcessRequest{
		SourceRevisionID: primaryRevision.ID, InterpretationContext: contextJSON,
	})
	if err != nil {
		return fmt.Errorf("execute tracking reevaluation: %w", err)
	}
	// Reevaluation stays bounded to NO ACTION or one typed Proposal: a
	// multi-operation plan is outside its contract and yields no Proposal.
	if _, plan, planErr := extractInterpretSuggestion(processed.InterpretResult); plan != nil || planErr != nil {
		slog.Warn("tracking reevaluation ignored an interpretation plan", "request_id", request.ID, "tracked_item_id", tracked.ID)
		return app.trackingReevaluationService.Complete(context.WithoutCancel(ctx), request.ID, &processed.InterpretRunID, nil)
	}
	candidate, ok := extractMutationCandidate(processed.InterpretResult)
	if !ok {
		return app.trackingReevaluationService.Complete(context.WithoutCancel(ctx), request.ID, &processed.InterpretRunID, nil)
	}
	if err := validateReevaluationCandidate(candidate.CapabilityID, candidate.Arguments, tracked.ID, tracked.Title, tracked.WorkItemID); err != nil {
		return err
	}

	if candidate.CapabilityID == proposal.OperationTypePlannerItemCreate {
		var args map[string]any
		if err := json.Unmarshal(candidate.Arguments, &args); err != nil {
			return err
		}
		if !candidateHasDueTarget(args) {
			return fmt.Errorf("tracking reevaluation planner.item.create must include a due target")
		}
		hasActive, err := app.plannerService.HasOpenTrackingTodoFromSourceRecord(ctx, tracked.ID, primaryRecord.ID)
		if err != nil {
			return err
		}
		if hasActive {
			return app.trackingReevaluationService.Complete(context.WithoutCancel(ctx), request.ID, &processed.InterpretRunID, nil)
		}
	}

	execCtx := ContextWithProposeProvenance(ctx, ProposeProvenance{AIRunID: &processed.InterpretRunID, SourceRevisionIDs: evidenceIDs})
	created, err := app.proposeRegistry.Execute(execCtx, app, candidate.CapabilityID, nil, candidate.Arguments, candidate.Reason)
	if err != nil {
		return fmt.Errorf("create proposal from tracking reevaluation: %w", err)
	}
	return app.trackingReevaluationService.Complete(context.WithoutCancel(ctx), request.ID, &processed.InterpretRunID, &created.ID)
}

func appendReevaluationRules(contextMap map[string]any, trackedID, trackedTitle, workItemID string) {
	contract, ok := contextMap["candidateContract"].(map[string]any)
	if !ok {
		return
	}
	rules, _ := contract["rules"].([]any)
	for _, rule := range []string{
		"Todo completion is a user action, not proof that Source-backed business state changed.",
		"Return candidate null unless the supplied existing Source evidence currently justifies one mutation.",
		"Return at most one candidate using the current flat typed capability contract; never combine operations, and always set plan to null.",
		"Any candidate must remain within Work Item " + workItemID + " and the reevaluated TrackedItem " + trackedID + " (" + trackedTitle + ").",
		"A planner.item.create candidate must set tracked_item_id to exactly " + trackedID + " and include an actionable due target.",
	} {
		rules = append(rules, rule)
	}
	contract["rules"] = rules
}

func validateReevaluationCandidate(capabilityID string, raw json.RawMessage, trackedID, trackedTitle, workItemID string) error {
	var args map[string]any
	if err := json.Unmarshal(raw, &args); err != nil {
		return err
	}
	if value, ok := args["work_item_id"].(string); ok && strings.TrimSpace(value) != workItemID {
		return fmt.Errorf("tracking reevaluation candidate escaped Work Item scope")
	}
	if value, ok := args["target_item_title"].(string); ok && !strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(trackedTitle)) {
		return fmt.Errorf("tracking reevaluation candidate escaped TrackedItem scope")
	}
	if capabilityID == proposal.OperationTypePlannerItemCreate {
		value, _ := args["tracked_item_id"].(string)
		if strings.TrimSpace(value) != trackedID {
			return fmt.Errorf("tracking reevaluation planner.item.create must target TrackedItem %s", trackedID)
		}
	}
	return nil
}

func proposalMatchesReevaluation(p *proposal.Proposal, trackedID, workItemID string) bool {
	if p == nil {
		return false
	}
	if p.PlannerItemPayload != nil && p.PlannerItemPayload.TrackedItemID != nil {
		return *p.PlannerItemPayload.TrackedItemID == trackedID
	}
	if p.TrackingPayload != nil {
		if p.TrackingPayload.TrackedItemID != nil {
			return *p.TrackingPayload.TrackedItemID == trackedID
		}
		return p.TrackingPayload.WorkItemID != nil && *p.TrackingPayload.WorkItemID == workItemID
	}
	return false
}
