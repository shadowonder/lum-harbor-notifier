package proposal

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"lum-harbor-notifier/internal/history"
)

// Producer identifies who produced the durable Proposal fact. It does not
// imply a durable AI Run; that relationship remains an independent nullable FK.
type Producer string

const (
	ProducerUser Producer = "USER"
	ProducerAI   Producer = "AI"
)

func (p Producer) valid() bool { return p == ProducerUser || p == ProducerAI }

func (p Producer) historyActor() history.ActorType {
	if p == ProducerAI {
		return history.ActorAI
	}
	return history.ActorUser
}

type proposalStatusSnapshot struct {
	Version       int    `json:"version"`
	OperationType string `json:"operation_type"`
	Status        string `json:"status"`
}

func recordProposalCreated(ctx context.Context, tx *sql.Tx, proposal *Proposal, producer Producer) error {
	after, err := history.Snapshot(proposalStatusSnapshot{Version: 1, OperationType: proposal.OperationType, Status: string(StatusPending)})
	if err != nil {
		return err
	}
	subjectID, workItemID, proposalID := proposal.TargetEntityID, proposalWorkItemID(proposal), proposal.ID
	histRepo := history.NewRepository(tx)
	var causationID *string
	if proposal.AIRunID != nil {
		if aiEv, err := histRepo.FindTerminalByAIRunID(ctx, *proposal.AIRunID); err == nil && aiEv != nil {
			causationID = &aiEv.ID
		}
	}
	var sourceRevID *string
	if len(proposal.SourceRevisionIDs) > 0 {
		sourceRevID = &proposal.SourceRevisionIDs[0]
		if causationID == nil {
			if srcEvents, err := histRepo.ListBySourceRevisionID(ctx, *sourceRevID); err == nil && len(srcEvents) > 0 {
				causationID = &srcEvents[0].ID
			}
		}
	}
	_, err = histRepo.Append(ctx, history.Draft{
		Type: history.EventProposalCreated, Scope: history.ScopeProposal, Actor: producer.historyActor(),
		Summary: proposalCreatedSummary(proposal), SubjectEntityID: subjectID, WorkItemID: workItemID,
		ProposalID: &proposalID, CorrelationID: history.ProposalCorrelation(proposal.ID), AfterJSON: after, OccurredAt: proposal.CreatedAt,
		AIRunID: proposal.AIRunID, SourceRevisionID: sourceRevID, CausationEventID: causationID,
	})
	return err
}

func recordProposalTransition(ctx context.Context, tx *sql.Tx, proposal *Proposal, eventType history.EventType, actor history.ActorType, revisionID *string) (*history.Event, error) {
	if proposal == nil || proposal.Status == StatusPending {
		return nil, fmt.Errorf("invalid Proposal transition history input")
	}
	before, err := history.Snapshot(proposalStatusSnapshot{Version: 1, OperationType: proposal.OperationType, Status: string(StatusPending)})
	if err != nil {
		return nil, err
	}
	after, err := history.Snapshot(proposalStatusSnapshot{Version: 1, OperationType: proposal.OperationType, Status: string(proposal.Status)})
	if err != nil {
		return nil, err
	}
	subjectID, workItemID, proposalID := proposal.TargetEntityID, proposalWorkItemID(proposal), proposal.ID
	histRepo := history.NewRepository(tx)
	var causationID *string
	if propEvents, err := histRepo.ListByCorrelation(ctx, history.ProposalCorrelation(proposal.ID), 10); err == nil {
		for _, ev := range propEvents {
			if ev.Type == history.EventProposalCreated {
				causationID = &ev.ID
				break
			}
		}
	}
	var sourceRevID *string
	if len(proposal.SourceRevisionIDs) > 0 {
		sourceRevID = &proposal.SourceRevisionIDs[0]
	}
	return histRepo.Append(ctx, history.Draft{
		Type: eventType, Scope: history.ScopeProposal, Actor: actor,
		Summary: proposalTransitionSummary(proposal), SubjectEntityID: subjectID, WorkItemID: workItemID,
		RevisionID: revisionID, ProposalID: &proposalID, CorrelationID: history.ProposalCorrelation(proposal.ID),
		BeforeJSON: before, AfterJSON: after, OccurredAt: proposal.UpdatedAt,
		AIRunID: proposal.AIRunID, SourceRevisionID: sourceRevID, CausationEventID: causationID,
	})
}

// proposalWorkItemID returns the WorkItem a Proposal's History event should
// be scoped under, or nil for a Proposal with no WorkItem association at all
// (a root-create Planner or WorkItem Proposal with no prior WorkItem context).
func proposalWorkItemID(proposal *Proposal) *string {
	if proposal.OperationType == OperationTypeWorkItemCreate || proposal.OperationType == OperationTypeWorkItemCreateWithWorkflow {
		return nil
	}
	if proposal.TrackingPayload != nil && proposal.TrackingPayload.WorkItemID != nil {
		return proposal.TrackingPayload.WorkItemID
	}
	if proposal.PlannerItemPayload != nil {
		return proposal.PlannerItemPayload.WorkItemID
	}
	if proposal.MonitorPointCreatePayload != nil {
		return &proposal.MonitorPointCreatePayload.WorkItemID
	}
	if proposal.MonitorPointSetStatePayload != nil {
		return &proposal.MonitorPointSetStatePayload.WorkItemID
	}
	if proposal.WorkflowSetCurrentStepPayload != nil {
		return &proposal.WorkflowSetCurrentStepPayload.WorkItemID
	}
	if proposal.PlannerLocalNotePayload != nil {
		return proposal.PlannerLocalNotePayload.WorkItemID
	}
	return proposal.TargetEntityID
}

// proposalDomainLabel names the authoritative domain a Proposal's operation
// mutates, so History never claims a Planner Apply changed Tracking.
func proposalDomainLabel(operationType string) string {
	switch {
	case strings.HasPrefix(operationType, "planner."):
		return "Planner"
	case strings.HasPrefix(operationType, "tracked_item."),
		strings.HasPrefix(operationType, "tracking."),
		strings.HasPrefix(operationType, "monitor_point."):
		return "Tracking"
	default:
		return "Work Item"
	}
}

func proposalCreatedSummary(proposal *Proposal) string {
	if proposal.OperationType == OperationTypeAggregate {
		return fmt.Sprintf("Aggregate proposal created for review (%d operations)", len(proposal.Operations))
	}
	return "Proposal created for " + proposalDomainLabel(proposal.OperationType) + " review"
}

func proposalTransitionSummary(proposal *Proposal) string {
	if proposal.OperationType == OperationTypeAggregate {
		switch proposal.Status {
		case StatusApplied:
			return fmt.Sprintf("Aggregate proposal applied (%d operations)", len(proposal.Operations))
		case StatusStale:
			return "Aggregate proposal marked stale after authoritative truth changed"
		}
	}
	domain := proposalDomainLabel(proposal.OperationType)
	switch proposal.Status {
	case StatusApplied:
		return "Proposal applied to " + domain
	case StatusRejected:
		return "Proposal rejected"
	case StatusStale:
		return "Proposal marked stale after authoritative " + domain + " changed"
	default:
		return "Proposal status changed"
	}
}
