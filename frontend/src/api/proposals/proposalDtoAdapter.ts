import type { app } from '../../../wailsjs/go/models';
import type {
  ProductProposal,
  ProductProposalOperation,
  ProductProposalStatus,
  ProductProposalOperationType,
  ApplyProposalResult,
  ApplyProposalOutcome,
  DashboardProposalSummary,
} from './proposalProductApi.ts';
import { mapWorkItemDTO } from '../workItems/workItemDtoAdapter.ts';

const validStatuses: readonly ProductProposalStatus[] = ['PENDING', 'APPLIED', 'REJECTED', 'STALE'];

const validOutcomes: readonly ApplyProposalOutcome[] = ['APPLIED', 'STALE', 'ALREADY_APPLIED'];

function expectInstant(value: string, field: string): string {
  if (!value || Number.isNaN(Date.parse(value))) throw new Error(`Invalid ${field}: ${value}`);
  return value;
}

/**
 * The single transport boundary from the generated Wails ProposalDTO into the
 * frontend's ProductProposal model. Generated binding types do not propagate
 * beyond this adapter.
 */
export function mapProposalDTO(dto: app.ProposalDTO): ProductProposal {
  if (!validStatuses.includes(dto.status as ProductProposalStatus)) {
    throw new Error(`Unsupported Proposal status: ${dto.status}`);
  }
  const resolvedAt = dto.resolvedAt ?? null;
  if (resolvedAt !== null) expectInstant(resolvedAt, 'Proposal resolvedAt');

  return {
    id: dto.id,
    targetWorkItemId: dto.targetWorkItemId ?? null,
    operationType: dto.operationType as ProductProposalOperationType,
    title: dto.title,
    summary: dto.summary ?? null,
    reason: dto.reason ?? null,
    status: dto.status as ProductProposalStatus,
    baseTitle: dto.baseTitle,
    baseDescription: dto.baseDescription ?? null,
    proposedTitle: dto.proposedTitle,
    proposedDescription: dto.proposedDescription ?? null,
    createdAt: expectInstant(dto.createdAt, 'Proposal createdAt'),
    updatedAt: expectInstant(dto.updatedAt, 'Proposal updatedAt'),
    resolvedAt,
    aiRunId: (dto as unknown as { aiRunId?: string | null }).aiRunId ?? null,
    sourceRevisionIds: (dto as unknown as { sourceRevisionIds?: string[] }).sourceRevisionIds ?? [],
    sourceExtensionIds: (dto as unknown as { sourceExtensionIds?: string[] }).sourceExtensionIds ?? [],
    sourceProvenances: (dto as unknown as { sourceProvenances?: Array<{ extensionId: string; extensionName: string; externalId: string; sourceKind: string }> }).sourceProvenances ?? [],
    changes: (dto.changes ?? []).map((change) => ({
      label: change.label,
      before: change.before ?? null,
      after: change.after ?? null,
    })),
    operations: mapProposalOperations(dto.operations ?? []),
  };
}

/** Maps aggregate actions, refusing anything but a strictly ordered sequence 0..n-1. */
function mapProposalOperations(operations: app.ProposalOperationDTO[]): ProductProposalOperation[] {
  return operations.map((operation, index) => {
    if (operation.sequenceNo !== index) {
      throw new Error(`Proposal operations out of order: expected ${index}, got ${operation.sequenceNo}`);
    }
    return {
      id: operation.id,
      sequenceNo: operation.sequenceNo,
      operationType: operation.operationType,
      label: operation.label,
      relations: (operation.relations ?? []).map((relation) => ({
        label: relation.label,
        value: relation.value,
        createdBySequenceNo: relation.createdBySequenceNo ?? null,
      })),
      changes: (operation.changes ?? []).map((change) => ({
        label: change.label,
        before: change.before ?? null,
        after: change.after ?? null,
      })),
    };
  });
}

/**
 * Maps the generated ApplyProposalResultDTO into the frontend's ApplyProposalResult.
 */
export function mapApplyProposalResultDTO(dto: app.ApplyProposalResultDTO): ApplyProposalResult {
  if (!validOutcomes.includes(dto.outcome as ApplyProposalOutcome)) {
    throw new Error(`Unsupported ApplyProposal outcome: ${dto.outcome}`);
  }
  return {
    outcome: dto.outcome as ApplyProposalOutcome,
    proposal: mapProposalDTO(dto.proposal),
    workItem: dto.workItem ? mapWorkItemDTO(dto.workItem) : null,
  };
}

/**
 * Maps the generated DashboardProposalSummaryDTO into the frontend's DashboardProposalSummary.
 */
export function mapDashboardProposalSummaryDTO(
  dto: app.DashboardProposalSummaryDTO,
): DashboardProposalSummary {
  if (typeof dto.pendingCount !== 'number' || dto.pendingCount < 0) {
    throw new Error(`Invalid pendingCount: ${dto.pendingCount}`);
  }
  if (typeof dto.affectedWorkItemCount !== 'number' || dto.affectedWorkItemCount < 0) {
    throw new Error(`Invalid affectedWorkItemCount: ${dto.affectedWorkItemCount}`);
  }
  return {
    pendingCount: dto.pendingCount,
    affectedWorkItemCount: dto.affectedWorkItemCount,
  };
}
