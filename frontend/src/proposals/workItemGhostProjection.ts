import type { ProductProposal } from '../api/proposals/proposalProductApi.ts';
import type { ProductWorkItem } from '../api/workItems/productApi.ts';

export interface PreviewField {
  value: string;
  isProposed: boolean;
}

export interface WorkItemGhostProjection {
  title: PreviewField;
  description: PreviewField;
  supported: boolean;
}

export function projectWorkItemGhost(
  item: ProductWorkItem,
  proposal: ProductProposal | null,
): WorkItemGhostProjection {
  const authoritativeDescription = item.description ?? '';
  const applicable =
    proposal?.status === 'PENDING' &&
    proposal.operationType === 'work_item.update_details' &&
    proposal.targetWorkItemId === item.id;

  if (!applicable) {
    return {
      title: { value: item.title, isProposed: false },
      description: { value: authoritativeDescription, isProposed: false },
      supported: proposal === null || proposal.targetWorkItemId !== item.id,
    };
  }

  const proposedDescription = proposal.proposedDescription ?? '';
  return {
    title: {
      value: proposal.proposedTitle,
      isProposed: proposal.proposedTitle !== item.title,
    },
    description: {
      value: proposedDescription,
      isProposed: proposedDescription !== authoritativeDescription,
    },
    supported: true,
  };
}
