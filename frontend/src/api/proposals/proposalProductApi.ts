import type { ProductWorkItem } from '../workItems/productApi.ts';

export type ProductProposalStatus = 'PENDING' | 'APPLIED' | 'REJECTED' | 'STALE';
export type ProductProposalOperationType =
  | 'work_item.update_details'
  | 'tracked_item.create'
  | 'tracked_item.update'
  | 'tracked_item.stage_state'
  | 'tracked_item.archive'
  | 'tracked_item.dependency_add'
  | 'tracked_item.dependency_remove'
  | 'planner.item.create'
  | 'planner.item.set_local_note'
  | 'workitem.set_lifecycle'
  | 'tracking.workflow_create'
  | 'tracking.workflow_set_current_step'
  | 'work_item.create'
  | 'work_item.create_with_workflow'
  | 'monitor_point.create'
  | 'monitor_point.set_state'
  // An aggregate parent: its ordered `operations` are the review content.
  | 'proposal.aggregate';

export interface ProductProposalChange {
  label: string;
  before: string | null;
  after: string | null;
}

/** An entity an aggregate action acts on or links to, named for review. */
export interface ProductProposalRelation {
  label: string;
  value: string;
  /** Earlier action (sequence number) of the same Proposal that creates it; null when it already exists. */
  createdBySequenceNo: number | null;
}

/** One ordered child action of an aggregate Proposal, as interpreted by the backend. */
export interface ProductProposalOperation {
  id: string;
  sequenceNo: number;
  operationType: string;
  label: string;
  relations: ProductProposalRelation[];
  changes: ProductProposalChange[];
}

export interface SourceProvenance {
  extensionId: string;
  extensionName: string;
  externalId: string;
  sourceKind: string;
}

export interface ProductProposal {
  id: string;
  // null only for a pending root-create Proposal with no WorkItem context
  // (currently only planner.item.create can have no target at all).
  targetWorkItemId: string | null;
  operationType: ProductProposalOperationType;
  title: string;
  summary: string | null;
  reason: string | null;
  status: ProductProposalStatus;
  baseTitle: string;
  baseDescription: string | null;
  proposedTitle: string;
  proposedDescription: string | null;
  createdAt: string;
  updatedAt: string;
  resolvedAt: string | null;
  aiRunId?: string | null;
  sourceRevisionIds?: string[];
  sourceExtensionIds?: string[];
  sourceProvenances?: SourceProvenance[];
  changes?: ProductProposalChange[];
  /** Empty for a singular Proposal; the ordered actions of an aggregate Proposal. */
  operations: ProductProposalOperation[];
}

/** An aggregate Proposal is reviewed and decided as one unit over its ordered actions. */
export function isAggregateProposal(proposal: Pick<ProductProposal, 'operations'>): boolean {
  return (proposal.operations?.length ?? 0) > 0;
}

export type ApplyProposalOutcome = 'APPLIED' | 'STALE' | 'ALREADY_APPLIED';

export interface ApplyProposalResult {
  outcome: ApplyProposalOutcome;
  proposal: ProductProposal;
  workItem: ProductWorkItem | null;
}

export interface ProposeWorkItemUpdateInput {
  targetWorkItemId: string;
  proposedTitle: string;
  proposedDescription?: string;
  reason?: string;
}

export interface DashboardProposalSummary {
  pendingCount: number;
  affectedWorkItemCount: number;
}

export interface ProposalProductApi {
  getPendingProposals(): Promise<ProductProposal[]>;
  proposeWorkItemUpdate(input: ProposeWorkItemUpdateInput): Promise<ProductProposal>;
  applyProposal(id: string): Promise<ApplyProposalResult>;
  rejectProposal(id: string): Promise<ProductProposal>;
  getDashboardSummary(): Promise<DashboardProposalSummary>;
}

export async function proposeWorkItemUpdate(
  api: ProposalProductApi,
  input: ProposeWorkItemUpdateInput,
): Promise<ProductProposal> {
  const targetWorkItemId = input.targetWorkItemId.trim();
  if (!targetWorkItemId) throw new Error('Target Work Item ID is required.');
  const proposedTitle = input.proposedTitle.trim();
  if (!proposedTitle) throw new Error('Proposed title is required.');
  const proposedDescription = input.proposedDescription?.trim();
  const reason = input.reason?.trim();

  return api.proposeWorkItemUpdate({
    targetWorkItemId,
    proposedTitle,
    ...(proposedDescription !== undefined ? { proposedDescription } : {}),
    ...(reason !== undefined ? { reason } : {}),
  });
}

export async function applyProposal(
  api: ProposalProductApi,
  id: string,
): Promise<ApplyProposalResult> {
  const trimmedId = id.trim();
  if (!trimmedId) throw new Error('Proposal ID is required.');
  return api.applyProposal(trimmedId);
}

export async function rejectProposal(
  api: ProposalProductApi,
  id: string,
): Promise<ProductProposal> {
  const trimmedId = id.trim();
  if (!trimmedId) throw new Error('Proposal ID is required.');
  return api.rejectProposal(trimmedId);
}

export async function getDashboardProposalSummary(
  api: ProposalProductApi,
): Promise<DashboardProposalSummary> {
  return api.getDashboardSummary();
}
