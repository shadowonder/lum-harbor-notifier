import { AGGREGATE_OPERATION_TYPE } from './aggregateProposalReview.ts';

/** The authoritative domain a Proposal's operation mutates, used in user-facing result notices. */
type ProposalDomain = 'planner' | 'tracking' | 'workItem';

function proposalDomain(operationType: string): ProposalDomain {
  if (operationType.startsWith('planner.')) return 'planner';
  if (operationType.startsWith('tracked_item.')) return 'tracking';
  return 'workItem';
}

function actionCountText(actionCount: number): string {
  return actionCount === 1 ? 'its action' : `its ${actionCount} actions`;
}

// actionCount is the number of actions of an aggregate Proposal
// (operationType proposal.aggregate); singular Proposals ignore it.
export function proposalAppliedMessage(operationType: string, actionCount = 0): string {
  if (operationType === AGGREGATE_OPERATION_TYPE) {
    return `Proposal applied: all of ${actionCountText(actionCount)} were applied together.`;
  }
  switch (proposalDomain(operationType)) {
    case 'planner':
      return 'Proposal applied to the Planner.';
    case 'tracking':
      return 'Proposal applied to authoritative Tracking.';
    case 'workItem':
      return 'Proposal applied to the authoritative WorkItem.';
  }
}

export function proposalStaleMessage(operationType: string, actionCount = 0): string {
  if (operationType === AGGREGATE_OPERATION_TYPE) {
    return `This proposal is no longer current. None of ${actionCountText(actionCount)} were applied.`;
  }
  switch (proposalDomain(operationType)) {
    case 'planner':
      return 'This proposal is no longer current. Nothing was applied.';
    case 'tracking':
      return 'This proposal is no longer current because Tracking changed. Nothing was applied.';
    case 'workItem':
      return 'This proposal is no longer current because the WorkItem changed. Nothing was applied.';
  }
}

export function proposalRejectedMessage(operationType: string, actionCount = 0): string {
  if (operationType === AGGREGATE_OPERATION_TYPE) {
    return `Proposal rejected. None of ${actionCountText(actionCount)} were applied.`;
  }
  switch (proposalDomain(operationType)) {
    case 'planner':
      return 'Proposal rejected. The Planner was not changed.';
    case 'tracking':
      return 'Proposal rejected. Tracking was not changed.';
    case 'workItem':
      return 'Proposal rejected. The authoritative WorkItem was not changed.';
  }
}
