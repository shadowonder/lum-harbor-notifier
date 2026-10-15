import type { ProductProposal } from '../api/proposals/proposalProductApi.ts';
import type { ProposalHandoffResult } from '../assistant/assistantConversation.ts';

export interface ProposalHandoffDeps {
  /** Re-reads the authoritative PENDING collection; rejects on failure. */
  refreshPending: () => Promise<readonly ProductProposal[]>;
  /** The existing M9A begin-review entry (same one Needs Review cards use). */
  beginReview: (proposal: ProductProposal) => void;
  isAssistantOpen: () => boolean;
}

/**
 * Foreground handoff of an AI-created Proposal into the existing M9A review.
 * The Assistant result supplies identity only: the Proposal that starts review
 * is the one found in the freshly read authoritative collection, never one
 * built from the Assistant result. Nothing here re-sends an AI request.
 */
export async function handoffCreatedProposal(
  deps: ProposalHandoffDeps,
  proposalId: string,
): Promise<ProposalHandoffResult> {
  const pending = await deps.refreshPending();
  const created = pending.find((proposal) => proposal.id === proposalId);
  if (!created) return 'MISSING';
  if (!deps.isAssistantOpen()) return 'SKIPPED';
  deps.beginReview(created);
  return 'STARTED';
}
