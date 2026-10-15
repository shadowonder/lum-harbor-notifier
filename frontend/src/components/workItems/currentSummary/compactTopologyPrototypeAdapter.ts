/**
 * Current Work Items row — presentation adapter.
 *
 * This is the ONLY file that turns an existing `WorkflowModel` (already
 * either real backend WorkItem data via `buildWorkflowModelFromWorkItem`,
 * or frontend prototype fixture data from `workItemPrototypeFixtures.ts`)
 * into the Compact Workflow Topology Summary's own presentation types. It
 * never calls a new backend API and never adds a field to `WorkflowModel`
 * itself — everything here is derived from fields that already exist on
 * the model, plus a small, explicitly-frontend-only stage LABEL overlay
 * (never a new stage ID, never new item/edge data).
 *
 * STAGE TAXONOMY: `WorkflowNode.stageId` is still the existing fixed
 * `WorkflowStageId` union (`workflowTypes.ts`) — this adapter does not and
 * cannot change that shared type. What it changes is the LABEL shown for
 * each stage id, per WorkItem, so two different WorkItems can read as two
 * genuinely different workflows ("Research/Implementation/Validation" vs
 * "Study/Practice/Assessment") instead of one hardcoded software-delivery
 * pipeline. A WorkItem with no override falls back to the existing
 * `WORKFLOW_STAGES` label for whichever stage ids its own nodes actually
 * use — still never the removed BUILD/VERIFY/RELEASE section taxonomy,
 * which this module does not reference at all. Stage labels are still
 * computed (used for hover only, per the Status Rollup round's contract —
 * see `compactTopologyTypes.ts`); the summary renderer no longer shows a
 * global "current stage" for the whole WorkItem, since real WorkItems
 * routinely have tracked items sitting at genuinely different stages at
 * once.
 */

import { getLocalTodayKey } from '../../../pages/plannerData.ts';
import {
  WORKFLOW_STAGES,
  type WorkflowModel,
  type WorkflowNode,
  type WorkflowRelatedNotification,
  type WorkflowStageId,
} from '../workflow/workflowTypes.ts';
import type {
  AttentionSummary,
  CompactStage,
  CompactTopologyModel,
  ItemRollup,
  RollupBucket,
  ScheduleSummary,
} from './compactTopologyTypes.ts';

/** Presentation-only per-WorkItem stage vocabulary — never written back to `workflowTypes.ts`. */
const STAGE_LABEL_OVERRIDES: Record<string, Partial<Record<WorkflowStageId, string>>> = {
  'wi-checkout-platform': {
    dev: 'Implementation',
    test: 'Verification',
    stage: 'Compliance & Staging',
    adoption: 'Canary Rollout',
    production: 'Production',
  },
  'wi-dist-systems': {
    queue: 'Study',
    dev: 'Practice',
    test: 'Assessment',
    stage: 'Review',
    production: 'Defense',
  },
  'wi-lob-onboarding': {
    dev: 'Integration',
    stage: 'Validation',
    adoption: 'Adoption',
  },
  'wi-security-audit': {
    dev: 'Evidence Collection',
    test: 'Independent Assessment',
    stage: 'Compliance Review',
    production: 'Certification',
  },
};

const DEFAULT_STAGE_LABELS: Record<WorkflowStageId, string> = Object.fromEntries(
  WORKFLOW_STAGES.map((stage) => [stage.id, stage.label]),
) as Record<WorkflowStageId, string>;

function stageLabelFor(workItemId: string, stageId: WorkflowStageId): string {
  return STAGE_LABEL_OVERRIDES[workItemId]?.[stageId] ?? DEFAULT_STAGE_LABELS[stageId];
}

/**
 * Ordered, de-duplicated stage list, in first-appearance order across this
 * WorkItem's own nodes. Still computed and still labeled — the renderer
 * uses it for stage count/ordering/band boundaries/item placement and
 * exposes each stage's label only via hover, never as permanent on-canvas
 * text (see `CompactWorkflowTopology.tsx`).
 */
function buildStages(workItemId: string, nodes: readonly WorkflowNode[]): CompactStage[] {
  const seen = new Set<string>();
  const stages: CompactStage[] = [];
  for (const node of nodes) {
    if (seen.has(node.stageId)) continue;
    seen.add(node.stageId);
    stages.push({ id: node.stageId, label: stageLabelFor(workItemId, node.stageId) });
  }
  return stages;
}

const MAX_TOPOLOGY_ITEMS = 24;
export const MAX_REPRESENTATIVE_EDGES = 3;

/**
 * `CompactTopologyModel` intentionally has no "current stage" concept: a
 * real WorkItem's tracked items can legitimately sit at different stages
 * at the same time (an early-stage defect alongside a late-stage release
 * candidate), so the summary never collapses the whole WorkItem down to
 * one current position. Per-item emphasis comes only from each item's own
 * health/status marker.
 */
export function buildCompactTopologyModel(model: WorkflowModel): CompactTopologyModel {
  const stages = buildStages(model.workItemId, model.nodes);
  const items = model.nodes.slice(0, MAX_TOPOLOGY_ITEMS).map((node) => ({
    id: node.id,
    title: node.title,
    itemType: node.itemType,
    health: node.health,
    stageId: node.stageId,
  }));
  const itemIds = new Set(items.map((item) => item.id));
  const edges = model.edges
    .filter((edge) => itemIds.has(edge.from) && itemIds.has(edge.to))
    .slice(0, MAX_REPRESENTATIVE_EDGES * 4); // upper bound before layout does its own critical-first selection

  return {
    stages,
    trunkHealth: model.trunk.health,
    items,
    edges,
    hasMoreItems: model.nodes.length > MAX_TOPOLOGY_ITEMS,
  };
}

function parseLocalDateKey(key: string): Date | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(key);
  if (!match) return null;
  const [, year, month, day] = match;
  return new Date(Number(year), Number(month) - 1, Number(day), 12);
}

function daysBetweenLocalDates(fromKey: string, toKey: string): number | null {
  const from = parseLocalDateKey(fromKey);
  const to = parseLocalDateKey(toKey);
  if (!from || !to) return null;
  return Math.round((to.getTime() - from.getTime()) / (24 * 60 * 60 * 1000));
}

function formatLocalDateKey(dateKey: string): string {
  const [year, month, day] = dateKey.split('-').map(Number);
  return new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric' }).format(
    new Date(year!, month! - 1, day!, 12),
  );
}

/** Pure date arithmetic on the WorkItem's own real target date — never a fabricated health signal. */
function describeRemaining(targetDateKey: string, todayKey: string): string {
  const diff = daysBetweenLocalDates(todayKey, targetDateKey);
  if (diff === null) return '';
  if (diff < 0) return `Overdue ${-diff}d`;
  if (diff === 0) return 'Due today';
  return `${diff} day${diff === 1 ? '' : 's'} remaining`;
}

/**
 * Schedule summary for the LEFT identity column. `startLabel` reuses the
 * WorkItem's own existing `origin.observedAt` free-text field (already
 * present on prototype fixtures) — never a new start-date field. `target`
 * reuses the existing `targetDate`/`targetLabel`. `remainingLabel` is pure
 * date arithmetic computed against today, so it never drifts stale the way
 * a hardcoded fixture string would.
 */
export function buildScheduleSummary(model: WorkflowModel): ScheduleSummary {
  const startLabel = model.origin?.observedAt ? model.origin.observedAt.split(' · ')[0]! : null;
  if (!model.targetDate) {
    return { startLabel, targetLabel: model.targetLabel || null, remainingLabel: null };
  }
  const today = getLocalTodayKey();
  return {
    startLabel,
    targetLabel: formatLocalDateKey(model.targetDate),
    remainingLabel: describeRemaining(model.targetDate, today),
  };
}

/**
 * Item rollup for the LEFT column: "how much work is there, and roughly
 * what state is it in" — distinct from the single overall
 * `healthLabel`/`aggregateHealth` posture chip above it. Buckets are
 * derived purely from each node's existing `isResolved`/`health` fields
 * (no new backend state enum invented): a resolved item is "done"
 * regardless of its last health reading; among unresolved items, red/
 * yellow/green/gray map to blocked/at-risk/active/queued. Every node
 * falls into exactly one bucket, so the entries always sum to the total.
 */
const ROLLUP_BUCKET_LABELS: Record<RollupBucket, string> = {
  active: 'active',
  blocked: 'blocked',
  atRisk: 'at risk',
  queued: 'queued',
  done: 'done',
};
const ROLLUP_BUCKET_ORDER: RollupBucket[] = ['active', 'blocked', 'atRisk', 'queued', 'done'];

function classifyRollupBucket(node: WorkflowNode): RollupBucket {
  if (node.isResolved) return 'done';
  switch (node.health) {
    case 'red':
      return 'blocked';
    case 'yellow':
      return 'atRisk';
    case 'green':
      return 'active';
    case 'gray':
    default:
      return 'queued';
  }
}

export function buildItemRollup(model: WorkflowModel): ItemRollup {
  const counts: Record<RollupBucket, number> = {
    active: 0,
    blocked: 0,
    atRisk: 0,
    queued: 0,
    done: 0,
  };
  for (const node of model.nodes) counts[classifyRollupBucket(node)] += 1;
  const entries = ROLLUP_BUCKET_ORDER.filter((bucket) => counts[bucket] > 0).map((bucket) => ({
    bucket,
    label: ROLLUP_BUCKET_LABELS[bucket],
    count: counts[bucket],
  }));
  return { totalItems: model.nodes.length, entries };
}

const MAX_RISK_ITEMS = 2;
const MAX_NOTIFICATION_HIGHLIGHTS = 2;
const SEVERITY_RANK: Record<WorkflowRelatedNotification['severity'], number> = {
  CRITICAL: 2,
  ATTENTION: 1,
  INFO: 0,
};

const MONTH_ABBREVIATIONS = [
  'jan',
  'feb',
  'mar',
  'apr',
  'may',
  'jun',
  'jul',
  'aug',
  'sep',
  'oct',
  'nov',
  'dec',
];

/**
 * Resolves an informal "Sep 20"-style per-item target date (an existing
 * `WorkflowNode.targetDate` presentation field) to a local date key, using
 * the same year "today" is in — these fixture strings were always written
 * relative to the same implicit year every other date string in this
 * dataset uses (e.g. WorkItem-level `updatedAt`/history timestamps), never
 * a newly invented year. Returns null (never a guess) when the text isn't
 * a recognizable "Mon D" date, so an unparseable value is simply excluded
 * from overdue counting rather than misread.
 */
function resolveInformalNodeTargetDateKey(text: string, todayKey: string): string | null {
  const match = /^([A-Za-z]{3})[a-z]*\s+(\d{1,2})$/.exec(text.trim());
  if (!match) return null;
  const monthIndex = MONTH_ABBREVIATIONS.indexOf(match[1]!.toLowerCase());
  if (monthIndex === -1) return null;
  const day = Number(match[2]);
  const year = todayKey.slice(0, 4);
  const month = String(monthIndex + 1).padStart(2, '0');
  return `${year}-${month}-${String(day).padStart(2, '0')}`;
}

function isNodeOverdue(node: WorkflowNode, todayKey: string): boolean {
  if (node.isResolved || !node.targetDate) return false;
  const resolved = resolveInformalNodeTargetDateKey(node.targetDate, todayKey);
  if (!resolved) return false;
  return resolved < todayKey;
}

/**
 * RIGHT column attention summary: "is there anything here worth my
 * attention right now" — blocked/at-risk/overdue counts, a small bounded
 * set of the most important risk items, and notification highlights.
 * Entirely derived from fields that already exist on `WorkflowModel`'s
 * nodes (`health`, `isResolved`, `targetDate`, `relatedNotifications`) —
 * no new backend concept, no Attention backend, no Monitor Point backend,
 * no Notification backend.
 */
export function buildAttentionSummary(model: WorkflowModel): AttentionSummary {
  const today = getLocalTodayKey();
  const blockedNodes = model.nodes.filter((node) => !node.isResolved && node.health === 'red');
  const atRiskNodes = model.nodes.filter((node) => !node.isResolved && node.health === 'yellow');
  const overdueCount = model.nodes.filter((node) => isNodeOverdue(node, today)).length;

  const allNotifications = model.nodes.flatMap((node) => node.relatedNotifications ?? []);
  const activeNotifications = allNotifications.filter((n) => n.status !== 'RESOLVED');
  const notificationHighlights = [...activeNotifications]
    .sort((a, b) => SEVERITY_RANK[b.severity] - SEVERITY_RANK[a.severity])
    .slice(0, MAX_NOTIFICATION_HIGHLIGHTS)
    .map((n) => n.title);

  return {
    blockedCount: blockedNodes.length,
    atRiskCount: atRiskNodes.length,
    overdueCount,
    blockedItems: blockedNodes.slice(0, MAX_RISK_ITEMS).map((n) => n.title),
    hasMoreBlocked: blockedNodes.length > MAX_RISK_ITEMS,
    atRiskItems: atRiskNodes.slice(0, MAX_RISK_ITEMS).map((n) => n.title),
    hasMoreAtRisk: atRiskNodes.length > MAX_RISK_ITEMS,
    notificationCount: activeNotifications.length,
    notificationHighlights,
  };
}
