import type { ProductProposal } from '../api/proposals/proposalProductApi.ts';

/**
 * Review wording for aggregate Proposals. An aggregate is decided as one unit:
 * Apply runs every action together and Reject declines all of them.
 */

export const AGGREGATE_OPERATION_TYPE = 'proposal.aggregate';

function actions(count: number): string {
  return count === 1 ? '1 action' : `${count} actions`;
}

export function aggregateActionCountLabel(count: number): string {
  return count === 1 ? '1 proposed action' : `${count} proposed actions`;
}

export function aggregateApplyLabel(count: number): string {
  return count === 1 ? 'Apply action' : `Apply all ${count} actions`;
}

export const AGGREGATE_REJECT_LABEL = 'Reject all';

export function aggregateAtomicityNotice(count: number): string {
  return `Applying runs all ${actions(count)} together in one transaction: either every action is applied or none is. Reject declines the whole proposal and applies nothing.`;
}

/** Display number of an action (1-based), matching "Created in action N" relation text. */
export function actionNumber(sequenceNo: number): number {
  return sequenceNo + 1;
}

/** The Needs Review row hint, unchanged for singular Proposals. */
export function proposalReviewRowHint(proposal: Pick<ProductProposal, 'operationType' | 'operations'>): string {
  const count = proposal.operations?.length ?? 0;
  if (count > 0) return `${actions(count)} · Review →`;
  return proposal.operationType.startsWith('planner.') ? 'Planner · Review →' : 'Work Items · Review →';
}
