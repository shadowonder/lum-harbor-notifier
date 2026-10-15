import type {
  ChangeProposalDto,
  ProposedPlannerItemDto,
  ProposedWorkItemEntryDto,
  ProposalOperationDto,
} from '../../api/proposals';

export interface WorkItemProposalProjection {
  targetAt?: string;
  entries: ProposedWorkItemEntryDto[];
  attentionResolution?: { before: string; after: string };
}

export interface PlannerProposalProjection {
  addedItems: ProposedPlannerItemDto[];
  enrichments: Record<string, { before: string; after: string }>;
}

export interface ProposalProjection {
  workItems: Record<string, WorkItemProposalProjection>;
  planner: PlannerProposalProjection;
}

export interface ProposalPreviewState {
  proposalId: string;
  title: string;
  changeCount: number;
  proposed: ProposalProjection;
}

export const emptyProposalProjection = (): ProposalProjection => ({
  workItems: {},
  planner: { addedItems: [], enrichments: {} },
});

export function countProposalChanges(operations: ProposalOperationDto[]) {
  return operations.reduce(
    (total, operation) =>
      total + (operation.kind === 'ADD_WORK_ITEM_ENTRIES' ? operation.entries.length : 1),
    0,
  );
}

export function applyProposalOperations(
  base: ProposalProjection,
  operations: ProposalOperationDto[],
): ProposalProjection {
  const next: ProposalProjection = {
    workItems: Object.fromEntries(
      Object.entries(base.workItems).map(([id, item]) => [
        id,
        { ...item, entries: [...item.entries] },
      ]),
    ),
    planner: {
      addedItems: [...base.planner.addedItems],
      enrichments: { ...base.planner.enrichments },
    },
  };
  operations.forEach((operation) => {
    if (operation.kind === 'ADD_PLANNER_TODO') next.planner.addedItems.push(operation.item);
    else if (operation.kind === 'ENRICH_PLANNER_TODO')
      next.planner.enrichments[operation.todoId] = {
        before: operation.before,
        after: operation.after,
      };
    else {
      const item = next.workItems[operation.workItemId] ?? { entries: [] };
      next.workItems[operation.workItemId] = item;
      if (operation.kind === 'SET_WORK_ITEM_TARGET') item.targetAt = operation.after;
      if (operation.kind === 'ADD_WORK_ITEM_ENTRIES') item.entries.push(...operation.entries);
      if (operation.kind === 'RESOLVE_ATTENTION')
        item.attentionResolution = { before: operation.before, after: operation.after };
    }
  });
  return next;
}

export function buildPreviewState(
  authoritative: ProposalProjection,
  proposal: ChangeProposalDto,
): ProposalPreviewState {
  return {
    proposalId: proposal.id,
    title: proposal.title,
    changeCount: countProposalChanges(proposal.operations),
    proposed: applyProposalOperations(authoritative, proposal.operations),
  };
}
