import type { ProductProposal } from '../api/proposals/proposalProductApi.ts';

export interface ProposalReviewSession {
  proposalId: string;
}

export type ProposalTargetNavigation = 'NONE' | 'SELECT_TARGET' | 'OPEN_TARGET';

export function beginProposalReview(proposal: ProductProposal): ProposalReviewSession {
  return { proposalId: proposal.id };
}

export function proposalForReviewSession(
  proposals: readonly ProductProposal[],
  session: ProposalReviewSession | null,
): ProductProposal | null {
  if (!session) return null;
  return proposals.find((proposal) => proposal.id === session.proposalId) ?? null;
}

export function proposalTargetNavigation(
  currentPage: string,
  selectedWorkItemId: string | null,
  proposal: ProductProposal,
): ProposalTargetNavigation {
  // A root-create Proposal (e.g. a Planner item with no Work Item context)
  // has no target to navigate to at all - reviewing it never requires a
  // Work Item selection.
  if (proposal.targetWorkItemId === null) return 'NONE';
  // An aggregate may act on several Work Items; review never picks one.
  if ((proposal.operations?.length ?? 0) > 0) return 'NONE';
  if (currentPage !== 'workItems') return 'OPEN_TARGET';
  if (selectedWorkItemId !== proposal.targetWorkItemId) return 'SELECT_TARGET';
  return 'NONE';
}
