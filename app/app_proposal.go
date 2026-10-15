package app

import (
	"fmt"

	"lum-harbor-notifier/internal/proposal"
)

// GetPendingProposals returns all proposals currently awaiting user review
// in deterministic order.
func (app *App) GetPendingProposals() ([]ProposalDTO, error) {
	if app.proposalService == nil {
		return nil, fmt.Errorf("proposal service is not initialized")
	}
	proposals, err := app.proposalService.ListPending(appContext(app.ctx))
	if err != nil {
		return nil, fmt.Errorf("list pending proposals: %w", err)
	}
	resolve := app.entityTitleResolver()
	dtos := make([]ProposalDTO, 0, len(proposals))
	for _, p := range proposals {
		dtos = append(dtos, app.withSourceExtensions(proposalToReviewDTO(p, resolve)))
	}
	return dtos, nil
}

// proposalReviewDTO is the product transport for one Proposal: the typed
// review mapping, existing-entity titles for aggregate children, and Source
// provenance.
func (app *App) proposalReviewDTO(p proposal.Proposal) ProposalDTO {
	return app.withSourceExtensions(proposalToReviewDTO(p, app.entityTitleResolver()))
}

// entityTitleResolver resolves existing Work Item / Tracked Item titles for
// aggregate review, memoized per call so repeated links cost one read. It is
// consulted only for aggregate children, never for singular Proposals.
func (app *App) entityTitleResolver() entityTitleResolver {
	cache := map[string]*string{}
	ctx := appContext(app.ctx)
	return func(entityID string) (string, bool) {
		if cached, ok := cache[entityID]; ok {
			return valueOrEmpty(cached), cached != nil
		}
		var title *string
		if app.workitemService != nil {
			if item, err := app.workitemService.GetByID(ctx, entityID); err == nil && item != nil {
				title = &item.Title
			}
		}
		if title == nil && app.trackingService != nil {
			if item, err := app.trackingService.GetTrackedItem(ctx, entityID); err == nil && item != nil {
				title = &item.Title
			}
		}
		cache[entityID] = title
		return valueOrEmpty(title), title != nil
	}
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// withSourceExtensions resolves which extension captured each Source linked
// to the Proposal and enriches sourceProvenances. Unresolvable revisions are
// skipped: the label is a presentation aid, never a gate.
func (app *App) withSourceExtensions(dto ProposalDTO) ProposalDTO {
	dto.SourceExtensionIDs = []string{}
	dto.SourceProvenances = []SourceProvenanceDTO{}
	if app.sourceService == nil {
		return dto
	}
	ctx := appContext(app.ctx)
	for _, revisionID := range dto.SourceRevisionIDs {
		revision, err := app.sourceService.GetSourceRevision(ctx, revisionID)
		if err != nil || revision == nil {
			continue
		}
		record, err := app.sourceService.GetSourceRecord(ctx, revision.SourceRecordID)
		if err != nil || record == nil {
			continue
		}
		dto.SourceExtensionIDs = append(dto.SourceExtensionIDs, record.ExtensionID)

		name := ""
		if app.pluginInventory != nil {
			name = app.pluginInventory.ExtensionName(record.ExtensionID)
		}
		if name == "" {
			switch record.ExtensionID {
			case "core.assistant":
				name = "AI Assistant"
			case "core.local":
				name = "Source"
			case "lh-plugin-synthetic-project":
				name = "Sample Synthetic Project"
			case "lh-plugin-ai-runtime":
				name = "Sample AI Runtime Takeover"
			default:
				name = record.ExtensionID
			}
		}

		dto.SourceProvenances = append(dto.SourceProvenances, SourceProvenanceDTO{
			ExtensionID:   record.ExtensionID,
			ExtensionName: name,
			ExternalID:    record.ExternalID,
			SourceKind:    record.SourceKind,
		})
	}
	return dto
}

// GetProposal returns a single proposal by ID.
func (app *App) GetProposal(id string) (ProposalDTO, error) {
	if app.proposalService == nil {
		return ProposalDTO{}, fmt.Errorf("proposal service is not initialized")
	}
	p, err := app.proposalService.GetByID(appContext(app.ctx), id)
	if err != nil {
		return ProposalDTO{}, fmt.Errorf("get proposal %s: %w", id, err)
	}
	return app.proposalReviewDTO(*p), nil
}

// GetDashboardProposalSummary returns the lightweight proposal awareness summary
// for the Dashboard command and orientation surface.
func (app *App) GetDashboardProposalSummary() (DashboardProposalSummaryDTO, error) {
	if app.proposalService == nil {
		return DashboardProposalSummaryDTO{}, fmt.Errorf("proposal service is not initialized")
	}
	summary, err := app.proposalService.GetSummary(appContext(app.ctx))
	if err != nil {
		return DashboardProposalSummaryDTO{}, fmt.Errorf("get proposal summary: %w", err)
	}
	return DashboardProposalSummaryDTO{
		PendingCount:          summary.PendingCount,
		AffectedWorkItemCount: summary.AffectedWorkItemCount,
	}, nil
}

// ProposeWorkItemUpdate delegates deterministic proposal creation to the
// Proposal Service, capturing preconditions without modifying the target WorkItem.
func (app *App) ProposeWorkItemUpdate(request ProposeWorkItemUpdateRequestDTO) (ProposalDTO, error) {
	if app.proposalService == nil {
		return ProposalDTO{}, fmt.Errorf("proposal service is not initialized")
	}
	created, err := app.proposalService.ProposeWorkItemUpdate(
		appContext(app.ctx),
		proposal.ProposeWorkItemUpdateInput{
			TargetWorkItemID:    request.TargetWorkItemID,
			ProposedTitle:       request.ProposedTitle,
			ProposedDescription: request.ProposedDescription,
			Reason:              request.Reason,
			Producer:            proposal.ProducerUser,
		},
	)
	if err != nil {
		return ProposalDTO{}, fmt.Errorf("propose work item update: %w", err)
	}
	return app.proposalReviewDTO(*created), nil
}

// ApplyProposal executes atomic proposal application through the Proposal Service.
// It returns a typed outcome (APPLIED, STALE, ALREADY_APPLIED).
// Note: When the outcome is STALE due to an unavailable target, the returned WorkItem is nil.
func (app *App) ApplyProposal(request ApplyProposalRequestDTO) (ApplyProposalResultDTO, error) {
	if app.proposalService == nil {
		return ApplyProposalResultDTO{}, fmt.Errorf("proposal service is not initialized")
	}
	res, err := app.proposalService.ApplyProposal(
		appContext(app.ctx),
		proposal.ApplyProposalInput{ID: request.ID},
	)
	if err != nil {
		return ApplyProposalResultDTO{}, fmt.Errorf("apply proposal: %w", err)
	}
	if res.Outcome == proposal.ApplyOutcomeApplied && res.Proposal != nil && res.Proposal.SchedulesTodoReminder() {
		app.triggerTodoReminderEvaluation()
	}

	var workItemDTO *WorkItemDTO
	if res.WorkItem != nil {
		dto := workItemToDTO(*res.WorkItem)
		workItemDTO = &dto
	}

	return ApplyProposalResultDTO{
		Outcome:  string(res.Outcome),
		Proposal: app.proposalReviewDTO(*res.Proposal),
		WorkItem: workItemDTO,
	}, nil
}

// RejectProposal transitions a PENDING proposal to REJECTED via the Proposal Service.
func (app *App) RejectProposal(request RejectProposalRequestDTO) (ProposalDTO, error) {
	if app.proposalService == nil {
		return ProposalDTO{}, fmt.Errorf("proposal service is not initialized")
	}
	rejected, err := app.proposalService.RejectProposal(
		appContext(app.ctx),
		proposal.RejectProposalInput{ID: request.ID},
	)
	if err != nil {
		return ProposalDTO{}, fmt.Errorf("reject proposal: %w", err)
	}
	return app.proposalReviewDTO(*rejected), nil
}
