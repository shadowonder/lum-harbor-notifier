import type { MonitorPoint } from '../tracking/trackingProductApi.ts';

export type ProductWorkItemLifecycle = 'ACTIVE' | 'PAUSED' | 'COMPLETED' | 'CANCELLED';

export interface ProductWorkItem {
  id: string;
  title: string;
  description: string | null;
  lifecycleState: ProductWorkItemLifecycle;
  completedAt: string | null;
  createdAt: string;
  updatedAt: string;
}

/**
 * Real Tracking/Attention summaries for the Work Items overview (one
 * authoritative request). A section that could not be read is `null` on the
 * row summary (unavailable) — distinct from a valid zero summary.
 */
export type ProductOverviewStepState =
  | 'NOT_STARTED'
  | 'IN_PROGRESS'
  | 'COMPLETED'
  | 'WAITING'
  | 'BLOCKED'
  | 'FAILED'
  | 'UNKNOWN'
  | 'SKIPPED';

export interface ProductOverviewStage {
  id: string;
  key: string;
  title: string;
  sequence: number;
  terminal: boolean;
}

export interface ProductOverviewWorkflow {
  id: string;
  definitionId: string;
  stages: ProductOverviewStage[];
}

export type ProductOverviewTrackingSignal = 'NEUTRAL' | 'SATISFIED' | 'WATCH' | 'PROBLEM';

export interface ProductOverviewTrackedItemSignal {
  state: ProductOverviewTrackingSignal;
  hasActiveTrackingTodo: boolean;
  governingTodoDueAt: string | null;
  actionableAttentionCount: number;
  highestAttentionSeverity: ProductAttentionSeverity | null;
  hasActiveExpectation: boolean;
  hasSatisfiedExpectation: boolean;
  hasOverdueUnmetExpectation: boolean;
}

export interface ProductOverviewTrackedItem {
  id: string;
  title: string;
  itemType: string | null;
  parentItemId: string | null;
  lifecycle: 'ACTIVE' | 'COMPLETED' | 'CANCELLED';
  currentStepId: string | null;
  currentState: ProductOverviewStepState | null;
  complete: boolean;
  resolved: boolean;
  signal: ProductOverviewTrackedItemSignal;
}

export interface ProductOverviewStateDistribution {
  notStarted: number;
  inProgress: number;
  waiting: number;
  blocked: number;
  failed: number;
  unknown: number;
  skipped: number;
  completed: number;
}

export interface ProductTrackingSummary {
  trackedItemCount: number;
  workflow: ProductOverviewWorkflow | null;
  items: ProductOverviewTrackedItem[];
  stateDistribution: ProductOverviewStateDistribution;
  /** WorkItem-subject ACTIVE FINAL_TARGET only (backend-selected). */
  activeFinalTarget: MonitorPoint | null;
}

export type ProductAttentionSeverity = 'INFO' | 'WARNING' | 'CRITICAL';

export interface ProductAttentionSummary {
  openIssueCount: number;
  actionableIssueCount: number;
  snoozedIssueCount: number;
  highestActionableSeverity: ProductAttentionSeverity | null;
}

export interface ProductWorkItemSummary {
  tracking: ProductTrackingSummary | null;
  attention: ProductAttentionSummary | null;
}

export interface ProductWorkItemOverviewItem extends ProductWorkItemSummary {
  workItem: ProductWorkItem;
}

export interface ProductWorkItemOverview {
  items: ProductWorkItemOverviewItem[];
}

export interface CreateProductWorkItemInput {
  title: string;
  description?: string;
}

export interface SetProductWorkItemLifecycleInput {
  id: string;
  targetLifecycleState: ProductWorkItemLifecycle;
}

export interface ProductWorkItemApi {
  /** Base domain read (kept for compatibility); the Work Items page uses overview(). */
  list(): Promise<ProductWorkItem[]>;
  overview(): Promise<ProductWorkItemOverview>;
  create(input: CreateProductWorkItemInput): Promise<ProductWorkItem>;
  setLifecycle(input: SetProductWorkItemLifecycleInput): Promise<ProductWorkItem>;
}

export async function setProductWorkItemLifecycle(
  api: ProductWorkItemApi,
  input: SetProductWorkItemLifecycleInput,
): Promise<ProductWorkItem> {
  const id = input.id.trim();
  if (!id) throw new Error('Work Item ID is required.');
  return api.setLifecycle({ id, targetLifecycleState: input.targetLifecycleState });
}

export async function createProductWorkItem(
  api: ProductWorkItemApi,
  input: CreateProductWorkItemInput,
): Promise<ProductWorkItem> {
  const title = input.title.trim();
  if (!title) throw new Error('Work Item title is required.');
  const description = input.description?.trim();
  return api.create({ title, ...(description ? { description } : {}) });
}
