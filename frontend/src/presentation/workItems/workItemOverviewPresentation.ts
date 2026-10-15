import type {
  ProductAttentionSeverity,
  ProductOverviewStateDistribution,
  ProductOverviewTrackedItem,
  ProductOverviewWorkflow,
  ProductWorkItemSummary,
} from '../../api/workItems/productApi.ts';
import { describeTiming } from '../../tracking/trackingPresentation.ts';

export type OverviewTargetView =
  | { status: 'unavailable' }
  | { status: 'none' }
  | { status: 'set'; dateLabel: string; timingLabel: string };

export type OverviewTrackingView =
  | { status: 'unavailable' }
  | { status: 'no-workflow'; trackedItemCount: number }
  | { status: 'workflow-empty'; workflow: ProductOverviewWorkflow }
  | {
      status: 'available';
      trackedItemCount: number;
      workflow: ProductOverviewWorkflow;
      items: ProductOverviewTrackedItem[];
      stateDistribution: ProductOverviewStateDistribution;
    };

export type OverviewAttentionView =
  | { status: 'unavailable' }
  | { status: 'none' }
  | {
      status: 'open';
      actionableCount: number;
      snoozedCount: number;
      highestActionableSeverity: ProductAttentionSeverity | null;
    };

export interface OverviewRowView {
  target: OverviewTargetView;
  tracking: OverviewTrackingView;
  attention: OverviewAttentionView;
}

export type OverviewItemTypeGroup = 'story' | 'task' | 'defect' | 'gate' | 'other';

export function overviewItemTypeGroup(itemType: string | null): OverviewItemTypeGroup {
  const normalized = (itemType ?? '')
    .trim()
    .toUpperCase()
    .replace(/[\s-]+/g, '_');
  if (normalized === 'STORY' || normalized === 'USER_STORY') return 'story';
  if (normalized === 'TASK') return 'task';
  if (normalized === 'DEFECT' || normalized === 'BUG') return 'defect';
  if (normalized === 'GATE' || normalized === 'APPROVAL' || normalized === 'MILESTONE')
    return 'gate';
  return 'other';
}

export interface OverviewMarkerTooltip {
  /** `TYPE · Title` */
  headline: string;
  /** Authoritative execution state exactly as reported by Tracking. */
  state: string;
  stage: string;
}

/**
 * Hover/accessible identity for one marker: who it is and its backend state.
 * The state is the item's backend progression fact (never derived from color
 * or stage); a completed workflow reads COMPLETED, as in the marker label.
 */
export function overviewMarkerTooltip(
  item: ProductOverviewTrackedItem,
  stageTitle: string,
): OverviewMarkerTooltip {
  const type = (item.itemType ?? '').trim().replace(/[_-]+/g, ' ').toUpperCase();
  const state = item.complete
    ? 'COMPLETED'
    : item.currentState
      ? item.currentState.replace(/_/g, ' ')
      : 'NO CURRENT STEP';
  return { headline: `${type || 'ITEM'} · ${item.title}`, state, stage: stageTitle };
}

export function groupOverviewItemsByType(items: ProductOverviewTrackedItem[]) {
  const groups: Record<OverviewItemTypeGroup, ProductOverviewTrackedItem[]> = {
    story: [],
    task: [],
    defect: [],
    gate: [],
    other: [],
  };
  for (const item of items) groups[overviewItemTypeGroup(item.itemType)].push(item);
  return (Object.entries(groups) as [OverviewItemTypeGroup, ProductOverviewTrackedItem[]][]).filter(
    ([, grouped]) => grouped.length > 0,
  );
}

function dateLabelIn(dueAt: string, timezone: string): string {
  const options: Intl.DateTimeFormatOptions = { month: 'short', day: 'numeric' };
  try {
    return new Intl.DateTimeFormat('en-US', { ...options, timeZone: timezone }).format(
      new Date(dueAt),
    );
  } catch {
    return new Intl.DateTimeFormat('en-US', options).format(new Date(dueAt));
  }
}

export function buildOverviewRowView(summary: ProductWorkItemSummary, now: Date): OverviewRowView {
  const { tracking, attention } = summary;
  const target: OverviewTargetView = !tracking
    ? { status: 'unavailable' }
    : tracking.activeFinalTarget
      ? {
          status: 'set',
          dateLabel: dateLabelIn(
            tracking.activeFinalTarget.dueAt,
            tracking.activeFinalTarget.timezone,
          ),
          timingLabel: describeTiming(tracking.activeFinalTarget, now),
        }
      : { status: 'none' };

  let trackingView: OverviewTrackingView;
  if (!tracking) trackingView = { status: 'unavailable' };
  else if (!tracking.workflow)
    trackingView = { status: 'no-workflow', trackedItemCount: tracking.trackedItemCount };
  else if (tracking.trackedItemCount === 0)
    trackingView = { status: 'workflow-empty', workflow: tracking.workflow };
  else {
    trackingView = {
      status: 'available',
      trackedItemCount: tracking.trackedItemCount,
      workflow: tracking.workflow,
      items: tracking.items,
      stateDistribution: tracking.stateDistribution,
    };
  }

  const attentionView: OverviewAttentionView = !attention
    ? { status: 'unavailable' }
    : attention.openIssueCount === 0
      ? { status: 'none' }
      : {
          status: 'open',
          actionableCount: attention.actionableIssueCount,
          snoozedCount: attention.snoozedIssueCount,
          highestActionableSeverity: attention.highestActionableSeverity,
        };
  return { target, tracking: trackingView, attention: attentionView };
}
