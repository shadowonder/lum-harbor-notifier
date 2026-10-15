/**
 * Current Work Items row — Compact Workflow Topology Summary (frontend
 * product surface, WorkItems page).
 *
 * This is deliberately NOT the WorkItem Dependency Explorer. It answers a
 * much narrower question at a glance: roughly how many stages this
 * WorkItem's workflow has, how its tracked items are DISTRIBUTED across
 * them, which representative items exist, and which relationships matter
 * — never full dependency detail, never a Stage/blocker/health deep-dive,
 * and never a single "current stage" for the whole WorkItem (real
 * WorkItems routinely have items at genuinely different stages at once).
 * That full detail remains the WorkItem Dependency Explorer's job.
 *
 * STAGE TAXONOMY BOUNDARY (the reason this file exists separately from
 * `workflowTypes.ts`): `WorkflowStageId` there is a fixed six-value union
 * used by the legacy full-size minimap/canvas renderers. This summary
 * renderer must never own a fixed software-delivery taxonomy (no
 * BUILD/VERIFY/RELEASE, no hardcoded pipeline). `CompactStage.id`/`label`
 * are arbitrary strings supplied entirely by the presentation adapter —
 * the renderer only ever consumes an ordered list of stages, and has no
 * idea what any of them mean.
 */

import type { WorkflowHealth, WorkflowItemType } from '../workflow/workflowTypes.ts';

/** An arbitrary, presentation-supplied workflow stage — never a fixed renderer taxonomy. */
export interface CompactStage {
  id: string;
  label: string;
}

export interface CompactTopologyItem {
  id: string;
  title: string;
  itemType: WorkflowItemType;
  health: WorkflowHealth;
  stageId: string;
}

/** A simplified, representative relationship — not the full dependency graph. */
export interface CompactTopologyEdge {
  from: string;
  to: string;
}

export interface CompactTopologyModel {
  /**
   * Ordered stages actually present in this WorkItem's data — count and
   * labels vary per WorkItem. Still used for stage count/ordering/band
   * boundaries/item placement, and each stage's label is still available
   * for hover — there is deliberately no "current stage" field here: a
   * real WorkItem's tracked items can legitimately sit at different
   * stages at once, so the summary never collapses to one position.
   */
  stages: CompactStage[];
  trunkHealth: WorkflowHealth;
  items: CompactTopologyItem[];
  /** Already filtered to a small, representative set — never "draw everything". */
  edges: CompactTopologyEdge[];
  /** True when `items` was truncated; the renderer must not silently drop items without a signal. */
  hasMoreItems: boolean;
}

export interface ScheduleSummary {
  startLabel: string | null;
  targetLabel: string | null;
  /** Pure date-arithmetic wording only ("N days remaining"/"Overdue Nd"/"Due today") — never health language. */
  remainingLabel: string | null;
}

/**
 * "How much work is there, and roughly what state is it in" — LEFT column,
 * distinct from the single overall health/posture chip above it. Every
 * WorkItem node falls into exactly one bucket, so `entries` always sum to
 * `totalItems`; buckets with zero items are omitted rather than shown as
 * "0 blocked".
 */
export type RollupBucket = 'active' | 'blocked' | 'atRisk' | 'queued' | 'done';

export interface ItemRollupEntry {
  bucket: RollupBucket;
  label: string;
  count: number;
}

export interface ItemRollup {
  totalItems: number;
  entries: ItemRollupEntry[];
}

/**
 * "Is there anything here worth my attention right now" — RIGHT column.
 * Bounded risk-item lists (never the full list) plus notification
 * highlights, all derived from fields that already exist on
 * `WorkflowModel`'s nodes.
 */
export interface AttentionSummary {
  blockedCount: number;
  atRiskCount: number;
  /** Pure date arithmetic against a real per-item target date only — never inferred from createdAt/updatedAt. */
  overdueCount: number;
  blockedItems: string[];
  hasMoreBlocked: boolean;
  atRiskItems: string[];
  hasMoreAtRisk: boolean;
  notificationCount: number;
  notificationHighlights: string[];
}
