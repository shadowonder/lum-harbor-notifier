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
	"lum-harbor-notifier/internal/ailineage"
	"lum-harbor-notifier/internal/assistant"
	"lum-harbor-notifier/internal/identity"
	"lum-harbor-notifier/internal/plugin"
	"lum-harbor-notifier/internal/proposal"
	"lum-harbor-notifier/internal/proposalcap"
	"lum-harbor-notifier/internal/source"
	"lum-harbor-notifier/internal/tracking"
	"lum-harbor-notifier/internal/workitem"
)

// AskAssistantRequestDTO carries the user's message plus the only thing the
// UI may say about its context: which surface and which WorkItem is selected.
// WorkItem fields are never accepted from the frontend; the backend re-reads
// authoritative truth by ID.
type AskAssistantRequestDTO struct {
	Message    string `json:"message"`
	Surface    string `json:"surface"`
	WorkItemID string `json:"workItemId"`
}

// Assistant mutation-intent outcomes reported in AssistantResultDTO.ProposalOutcome.
const (
	AssistantProposalCreated        = "CREATED"
	AssistantProposalNoChange       = "NO_CHANGE"
	AssistantProposalContextChanged = "CONTEXT_CHANGED"
)

// AssistantResultDTO is the validated Assistant outcome. ChangeApplied is
// always false: creating a Proposal is not mutating authoritative truth.
// ProposalOutcome/ProposalID are set only for MUTATION_INTENT; ProposalID is
// the real durable M7 Proposal ID (frontend re-reads the authoritative
// collection rather than trusting a copied payload).
type AssistantResultDTO struct {
	Kind            string `json:"kind"`
	Text            string `json:"text"`
	ContextLabel    string `json:"contextLabel"`
	ChangeApplied   bool   `json:"changeApplied"`
	ProposalOutcome string `json:"proposalOutcome"`
	ProposalID      string `json:"proposalId"`
}

// assistantWorkItemReader exposes only WorkItem reads to the Assistant.
type assistantWorkItemReader struct {
	service  *workitem.Service
	tracking *tracking.Service
}

func (r assistantWorkItemReader) ReadWorkItem(ctx context.Context, id string) (assistant.WorkItemSnapshot, error) {
	item, err := r.service.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, workitem.ErrNotFound) {
			return assistant.WorkItemSnapshot{}, assistant.ErrWorkItemNotFound
		}
		return assistant.WorkItemSnapshot{}, err
	}
	snapshot := assistant.WorkItemSnapshot{
		ID: item.ID, LastRevisionID: item.LastRevisionID, Title: item.Title, Description: item.Description,
		Lifecycle: string(item.Lifecycle), CreatedAt: item.CreatedAt,
		UpdatedAt: item.UpdatedAt, CompletedAt: item.CompletedAt,
	}
	if r.tracking != nil {
		view, trackingErr := r.tracking.GetWorkItemTracking(ctx, id)
		if trackingErr == nil && view.Workflow != nil {
			trackingSnapshot := &assistant.TrackingSnapshot{WorkflowID: view.Workflow.Workflow.ID}
			if curStep, ok := view.Workflow.CurrentStep(); ok {
				trackingSnapshot.CurrentStep = &assistant.TrackingStepSnapshot{ID: curStep.ID, Title: curStep.Title}
			}
			for _, step := range view.Workflow.Definition.Steps {
				trackingSnapshot.Steps = append(trackingSnapshot.Steps, assistant.TrackingStepSnapshot{ID: step.ID, Title: step.Title})
			}
			itemTitles := make(map[string]string, len(view.Items))
			for _, viewItem := range view.Items {
				itemTitles[viewItem.Item.ID] = viewItem.Item.Title
			}
			for _, viewItem := range view.Items {
				item := viewItem.Item
				itemSnapshot := assistant.TrackedItemSnapshot{
					ID:             item.ID,
					Title:          item.Title,
					ItemType:       item.ItemType,
					ParentItemID:   item.ParentItemID,
					LastRevisionID: item.LastRevisionID,
					State:          "NOT_STARTED",
				}
				if item.ParentItemID != nil {
					if parentTitle, ok := itemTitles[*item.ParentItemID]; ok {
						itemSnapshot.ParentItemTitle = &parentTitle
					}
				}
				if p := viewItem.Progression; p != nil {
					itemSnapshot.Complete = p.Complete
					itemSnapshot.State = p.State()
					if !p.Complete && p.CurrentStepID != nil {
						itemSnapshot.CurrentStepID = p.CurrentStepID
						if step, ok := view.Workflow.Definition.StepByID(*p.CurrentStepID); ok {
							itemSnapshot.CurrentStepTitle = &step.Title
						}
					}
				}
				trackingSnapshot.Items = append(trackingSnapshot.Items, itemSnapshot)
			}
			for _, relation := range view.Relations {
				trackingSnapshot.Relations = append(trackingSnapshot.Relations, assistant.TrackingRelationSnapshot{FromTrackedItemID: relation.FromTrackedItemID, ToTrackedItemID: relation.ToTrackedItemID})
			}
			for _, mp := range view.MonitorPoints {
				trackingSnapshot.MonitorPoints = append(trackingSnapshot.MonitorPoints, assistant.MonitorPointSnapshot{
					ID:          mp.ID,
					Kind:        string(mp.Kind),
					Title:       mp.Title,
					State:       string(mp.State),
					DueAt:       mp.DueAt.Format(time.RFC3339),
					Timezone:    mp.Timezone,
					SubjectKind: string(mp.Subject.Kind),
					SubjectID:   mp.Subject.ID,
				})
			}
			snapshot.Tracking = trackingSnapshot
		}
	}
	return snapshot, nil
}

// AskAssistant executes the selected high-level AI Runtime through the
// production composition. It never mutates a WorkItem. A validated candidate
// becomes a durable PENDING M7 Proposal through the M7 producer, bound to the
// authoritative snapshot the model actually saw.
func (a *App) AskAssistant(request AskAssistantRequestDTO) (AssistantResultDTO, error) {
	if a.workitemService == nil || a.assistantRuntime == nil {
		return AssistantResultDTO{}, fmt.Errorf("assistant is not initialized")
	}
	service, err := assistant.NewService(assistantWorkItemReader{service: a.workitemService, tracking: a.trackingService}, a.assistantRuntime)
	if err != nil {
		return AssistantResultDTO{}, err
	}
	requestID, err := identity.New()
	if err != nil {
		return AssistantResultDTO{}, err
	}

	var sourceRevisionID *string
	if a.sourceService != nil {
		reqPayload, _ := json.Marshal(map[string]any{
			"message":    request.Message,
			"surface":    request.Surface,
			"workItemId": request.WorkItemID,
		})
		rev, captureErr := a.sourceService.CaptureIntentSource(appContext(a.ctx), source.CaptureIntentInput{
			ExtensionID:    "core.assistant",
			InstanceID:     "direct-turn",
			CapabilityID:   "assistant.turn",
			SourceKind:     "USER_INTENT",
			ExternalID:     requestID,
			Payload:        reqPayload,
			ProcessingMode: source.ProcessingModeSemanticProposal,
		})
		if captureErr != nil {
			slog.Warn("failed to capture assistant intent source", "request_id", requestID, "error", ai.SafeLogText(captureErr.Error(), 320))
		} else if rev != nil {
			sourceRevisionID = &rev.ID
			_, _ = a.sourceService.ClaimPendingProcessing(appContext(a.ctx), rev.ID)
		}
	}

	var assistantRunID *string
	if a.lineageService != nil {
		runID, err := identity.New()
		if err == nil {
			procID, _ := identity.New()
			reqJSON, _ := json.Marshal(map[string]any{
				"message":    request.Message,
				"surface":    request.Surface,
				"workItemId": request.WorkItemID,
			})
			srcRevID := ""
			if sourceRevisionID != nil {
				srcRevID = *sourceRevisionID
			}
			startErr := a.lineageService.Start(appContext(a.ctx), ailineage.StartRun{
				ID:               runID,
				ProcessingID:     procID,
				RequestID:        requestID,
				Action:           ai.ActionAssistantAsk,
				CanonicalRequest: reqJSON,
				SourceRevisionID: srcRevID,
				StartedAt:        time.Now().UTC(),
			})
			if startErr == nil {
				assistantRunID = &runID
			}
		}
	}

	result, err := service.Ask(appContext(a.ctx), requestID, request.Message, assistant.ContextLocator{
		Surface: request.Surface, WorkItemID: request.WorkItemID,
	})
	if err != nil {
		if assistantRunID != nil && a.lineageService != nil {
			if failErr := a.lineageService.Fail(appContext(a.ctx), *assistantRunID, err, nil, time.Now().UTC()); failErr != nil {
				slog.Error("finalize failed assistant AI run", "aiRunId", *assistantRunID, "error", failErr)
			}
		}
		if sourceRevisionID != nil && a.sourceService != nil {
			_ = a.sourceService.FailProcessing(appContext(a.ctx), *sourceRevisionID, err.Error())
		}
		// RuntimeAdapter emits the cohesive execution failure before Service
		// intentionally maps runtime errors to a UI-safe sentinel. Context reads
		// happen before that engine, so retain their root cause here.
		if errors.Is(err, assistant.ErrContext) {
			slog.Warn("Assistant context resolution failed",
				"request_id", requestID,
				"surface", ai.SafeLogText(request.Surface, 320),
				"work_item_id", ai.SafeLogText(request.WorkItemID, 320),
				"error", ai.SafeLogText(err.Error(), 320),
			)
		}
		var rpcErr *plugin.RPCError
		if errors.As(err, &rpcErr) {
			return AssistantResultDTO{}, fmt.Errorf("The AI runtime returned an error (%s: %s). Check Settings → AI. Nothing was changed.", rpcErr.Code, rpcErr.Message)
		}
		switch {
		case errors.Is(err, assistant.ErrEmptyMessage):
			return AssistantResultDTO{}, fmt.Errorf("Type a message first.")
		case errors.Is(err, assistant.ErrMessageTooLong):
			return AssistantResultDTO{}, fmt.Errorf("That message is too long.")
		case errors.Is(err, assistant.ErrMalformedResult):
			return AssistantResultDTO{}, fmt.Errorf("The AI returned an unusable response. Nothing was changed.")
		case errors.Is(err, assistant.ErrContext):
			return AssistantResultDTO{}, fmt.Errorf("The Assistant could not read its context. Nothing was changed.")
		case errors.Is(err, assistant.ErrRuntime):
			return AssistantResultDTO{}, fmt.Errorf("The AI runtime could not answer. Check Settings → AI. Nothing was changed.")
		}
		return AssistantResultDTO{}, fmt.Errorf("The AI runtime could not answer. Check Settings → AI. Nothing was changed.")
	}

	if assistantRunID != nil && a.lineageService != nil {
		outMap := map[string]any{
			"kind": string(result.Kind),
			"text": result.Text,
		}
		if result.Plan != nil {
			outMap["planOperations"] = len(result.Plan.Operations)
		}
		if result.ReadRequest != nil {
			outMap["readEvidence"] = map[string]any{
				"capabilityId": result.ReadRequest.CapabilityID,
				"hasResult":    result.ReadResult != nil,
			}
		}
		outJSON, _ := json.Marshal(outMap)
		// Record the real provider provenance of the final model call; a
		// finalization failure would otherwise leave the run RUNNING forever.
		if succeedErr := a.lineageService.Succeed(appContext(a.ctx), *assistantRunID, outJSON, result.Provenance, time.Now().UTC()); succeedErr != nil {
			slog.Error("finalize succeeded assistant AI run", "aiRunId", *assistantRunID, "error", succeedErr)
		}
	}

	dto := AssistantResultDTO{
		Kind: string(result.Kind), Text: result.Text,
		ContextLabel: result.ContextLabel, ChangeApplied: false,
	}
	if result.Kind != assistant.KindMutationIntent {
		if sourceRevisionID != nil && a.sourceService != nil {
			_ = a.sourceService.CompleteProcessing(appContext(a.ctx), *sourceRevisionID)
		}
		return dto, nil
	}
	return a.completeAssistantMutation(requestID, dto, result, assistantRunID, sourceRevisionID)
}

// completeAssistantMutation turns a validated, generic candidate into a
// durable Proposal via the single generic capability registry dispatch:
// lookup the capability, validate its arguments, invoke its handler. This
// function contains zero capability-specific knowledge - adding capability
// #11 never requires editing it. The Assistant package produced only an
// opaque {capability_id, arguments} selection; only this application layer
// owns descriptor/handler dispatch and Proposal creation.
func (a *App) completeAssistantMutation(requestID string, dto AssistantResultDTO, result assistant.Result, assistantRunID *string, sourceRevisionID *string) (AssistantResultDTO, error) {
	// A plan with a single operation is an ordinary singular Proposal.
	if result.Plan != nil && len(result.Plan.Operations) == 1 {
		operation := result.Plan.Operations[0]
		result.Candidate, result.Plan = &operation, nil
	}
	if result.Plan != nil {
		return a.completeAssistantPlan(requestID, dto, result, assistantRunID, sourceRevisionID)
	}
	if result.Candidate == nil {
		a.logAssistantProposalDestination(requestID, "", "FAILED", "", errors.New("mutation result did not contain a proposal candidate"))
		return AssistantResultDTO{}, fmt.Errorf("The AI returned an unusable response. Nothing was changed.")
	}
	if a.proposalService == nil || a.proposeRegistry == nil {
		a.logAssistantProposalDestination(requestID, result.Candidate.CapabilityID, "FAILED", "", errors.New("proposal services are not initialized"))
		return AssistantResultDTO{}, fmt.Errorf("assistant proposals are not initialized")
	}

	execCtx := appContext(a.ctx)
	var srcRevIDs []string
	if sourceRevisionID != nil {
		srcRevIDs = []string{*sourceRevisionID}
	}
	if assistantRunID != nil || len(srcRevIDs) > 0 {
		execCtx = ContextWithProposeProvenance(execCtx, ProposeProvenance{
			AIRunID:           assistantRunID,
			SourceRevisionIDs: srcRevIDs,
		})
	}

	created, err := a.proposeRegistry.Execute(execCtx, a, result.Candidate.CapabilityID, result.Base, result.Candidate.Arguments, result.Candidate.Reason)
	switch {
	case err == nil:
		dto.ProposalOutcome = AssistantProposalCreated
		dto.ProposalID = created.ID
		dto.Text = fmt.Sprintf("I prepared %q for your review in Needs Review. Nothing has been changed yet.", created.Title)
		if sourceRevisionID != nil && a.sourceService != nil {
			_ = a.sourceService.CompleteProcessingWithProposal(appContext(a.ctx), *sourceRevisionID, created.ID)
		}
		a.logAssistantProposalDestination(requestID, result.Candidate.CapabilityID, AssistantProposalCreated, created.ID, nil)
	case errors.Is(err, errCapabilityNoChange):
		dto.ProposalOutcome = AssistantProposalNoChange
		dto.Text = "No actual change was produced, so no Proposal was created. Nothing was changed."
		if sourceRevisionID != nil && a.sourceService != nil {
			_ = a.sourceService.CompleteProcessing(appContext(a.ctx), *sourceRevisionID)
		}
		a.logAssistantProposalDestination(requestID, result.Candidate.CapabilityID, AssistantProposalNoChange, "", nil)
	case errors.Is(err, proposal.ErrBaseChanged):
		dto.ProposalOutcome = AssistantProposalContextChanged
		dto.Text = "The Work Item changed while I was preparing this suggestion, so no Proposal was created. Please try again."
		if sourceRevisionID != nil && a.sourceService != nil {
			_ = a.sourceService.FailProcessing(appContext(a.ctx), *sourceRevisionID, "context changed")
		}
		a.logAssistantProposalDestination(requestID, result.Candidate.CapabilityID, AssistantProposalContextChanged, "", err)
	case errors.Is(err, proposal.ErrTargetUnavailable):
		dto.ProposalOutcome = AssistantProposalContextChanged
		dto.Text = "The target is no longer available, so no Proposal was created."
		if sourceRevisionID != nil && a.sourceService != nil {
			_ = a.sourceService.FailProcessing(appContext(a.ctx), *sourceRevisionID, "target unavailable")
		}
		a.logAssistantProposalDestination(requestID, result.Candidate.CapabilityID, AssistantProposalContextChanged, "", err)
	case errors.Is(err, errInvalidArguments):
		a.logAssistantProposalDestination(requestID, result.Candidate.CapabilityID, "FAILED", "", err)
		if sourceRevisionID != nil && a.sourceService != nil {
			_ = a.sourceService.FailProcessing(appContext(a.ctx), *sourceRevisionID, err.Error())
		}
		detail := strings.TrimPrefix(err.Error(), "invalid proposal capability arguments: ")
		detail = strings.TrimSpace(detail)
		if detail == "" || detail == "invalid proposal capability arguments" {
			return AssistantResultDTO{}, fmt.Errorf("The Assistant could not create the Proposal due to invalid arguments. Nothing was changed.")
		}
		return AssistantResultDTO{}, fmt.Errorf("The Assistant could not create the Proposal: %s. Nothing was changed.", detail)
	default:
		a.logAssistantProposalDestination(requestID, result.Candidate.CapabilityID, "FAILED", "", err)
		if sourceRevisionID != nil && a.sourceService != nil {
			_ = a.sourceService.FailProcessing(appContext(a.ctx), *sourceRevisionID, err.Error())
		}
		return AssistantResultDTO{}, fmt.Errorf("The Assistant could not create the Proposal. Nothing was changed.")
	}
	return dto, nil
}

// completeAssistantPlan turns a validated multi-operation plan into ONE
// pending aggregate Proposal through the same capability registry: every
// operation is prepared by its registered handler, the whole plan is
// preflighted, and the parent carries the turn's AI Run and Source. Nothing is
// persisted unless every operation and the plan as a whole are valid.
func (a *App) completeAssistantPlan(requestID string, dto AssistantResultDTO, result assistant.Result, assistantRunID *string, sourceRevisionID *string) (AssistantResultDTO, error) {
	plan := result.Plan
	label := fmt.Sprintf("plan(%d)", len(plan.Operations))
	if a.proposalService == nil || a.proposeRegistry == nil {
		a.logAssistantProposalDestination(requestID, label, "FAILED", "", errors.New("proposal services are not initialized"))
		return AssistantResultDTO{}, fmt.Errorf("assistant proposals are not initialized")
	}
	var srcRevIDs []string
	if sourceRevisionID != nil {
		srcRevIDs = []string{*sourceRevisionID}
	}
	created, err := a.createAggregateFromPlan(appContext(a.ctx), plan, result.Base, assistantRunID, srcRevIDs)
	if err == nil {
		dto.ProposalOutcome = AssistantProposalCreated
		dto.ProposalID = created.ID
		dto.Text = fmt.Sprintf("I prepared %q with %d actions for your review in Needs Review. Applying it applies all of them together; nothing has been changed yet.", created.Title, len(created.Operations))
		if sourceRevisionID != nil && a.sourceService != nil {
			_ = a.sourceService.CompleteProcessingWithProposal(appContext(a.ctx), *sourceRevisionID, created.ID)
		}
		a.logAssistantProposalDestination(requestID, label, AssistantProposalCreated, created.ID, nil)
		return dto, nil
	}
	if sourceRevisionID != nil && a.sourceService != nil {
		_ = a.sourceService.FailProcessing(appContext(a.ctx), *sourceRevisionID, err.Error())
	}
	switch {
	case errors.Is(err, proposal.ErrBaseChanged):
		a.logAssistantProposalDestination(requestID, label, AssistantProposalContextChanged, "", err)
		dto.ProposalOutcome = AssistantProposalContextChanged
		dto.Text = "The Work Item changed while I was preparing this plan, so no Proposal was created. Please try again."
		return dto, nil
	case errors.Is(err, proposal.ErrTargetUnavailable):
		a.logAssistantProposalDestination(requestID, label, AssistantProposalContextChanged, "", err)
		dto.ProposalOutcome = AssistantProposalContextChanged
		dto.Text = "A target in this plan is no longer available, so no Proposal was created."
		return dto, nil
	}
	a.logAssistantProposalDestination(requestID, label, "FAILED", "", err)
	return AssistantResultDTO{}, fmt.Errorf("The Assistant could not create the Proposal: %s. Nothing was changed.", planFailureDetail(err))
}

// createAggregateFromPlan prepares and preflights a model plan through the
// capability registry and persists it as one pending aggregate Proposal with
// the given AI Run and Source provenance.
func (a *App) createAggregateFromPlan(ctx context.Context, plan *assistant.MutationPlan, base *assistant.WorkItemSnapshot, aiRunID *string, sourceRevisionIDs []string) (*proposal.Proposal, error) {
	calls := make([]proposalcap.Call, 0, len(plan.Operations))
	for _, operation := range plan.Operations {
		calls = append(calls, proposalcap.Call{CapabilityID: operation.CapabilityID, Arguments: operation.Arguments})
	}
	prepared, err := a.proposeRegistry.PrepareAggregatePlan(ctx, a, base, calls)
	if err != nil {
		return nil, err
	}
	return a.proposalService.CreateAggregateProposal(ctx, proposal.CreateAggregateProposalInput{
		Title: plan.Title, Summary: plan.Summary, Reason: plan.Reason,
		AIRunID: aiRunID, SourceRevisionIDs: sourceRevisionIDs, Producer: proposal.ProducerAI,
		Operations: prepared,
	})
}

// planFailureDetail renders why a plan was refused without repeating the
// internal sentinel prefixes.
func planFailureDetail(err error) string {
	detail := err.Error()
	for _, prefix := range []string{proposal.ErrInvalidAggregatePlan.Error() + ": ", errInvalidArguments.Error() + ": "} {
		detail = strings.ReplaceAll(detail, prefix, "")
	}
	if errors.Is(err, errCapabilityNoChange) {
		detail = strings.ReplaceAll(detail, errCapabilityNoChange.Error(), "an action would not change anything")
	}
	return strings.TrimSpace(detail)
}

func (a *App) logAssistantProposalDestination(requestID, capabilityID, outcome, proposalID string, err error) {
	destination := "Assistant"
	if outcome == AssistantProposalCreated {
		destination = "Proposal Review"
	}
	attrs := []slog.Attr{
		slog.String("request_id", requestID),
		slog.String("capability_id", capabilityID),
		slog.String("outcome", outcome),
		slog.String("destination", destination),
	}
	if proposalID != "" {
		attrs = append(attrs, slog.String("proposal_id", proposalID))
	}
	level := slog.LevelInfo
	if err != nil {
		level = slog.LevelWarn
		attrs = append(attrs, slog.String("error", ai.SafeLogText(err.Error(), 320)))
	}
	slog.Default().LogAttrs(appContext(a.ctx), level, "AI proposal destination", attrs...)
}
