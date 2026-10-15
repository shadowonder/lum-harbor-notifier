import type { app } from '../../../wailsjs/go/models';
import type {
  ProductAttentionSeverity,
  ProductAttentionSummary,
  ProductTrackingSummary,
  ProductWorkItem,
  ProductWorkItemLifecycle,
  ProductWorkItemOverview,
} from './productApi.ts';
import { mapMonitorPoint } from '../tracking/trackingDtoAdapter.ts';

const lifecycles: readonly ProductWorkItemLifecycle[] = [
  'ACTIVE',
  'PAUSED',
  'COMPLETED',
  'CANCELLED',
];

function expectInstant(value: string, field: string) {
  if (!value || Number.isNaN(Date.parse(value))) throw new Error(`Invalid ${field}: ${value}`);
  return value;
}

/**
 * The single transport boundary from the generated Wails DTO into the
 * frontend's authoritative WorkItem product model. Generated binding types
 * do not propagate beyond this adapter.
 */
export function mapWorkItemDTO(dto: app.WorkItemDTO): ProductWorkItem {
  if (!lifecycles.includes(dto.lifecycleState as ProductWorkItemLifecycle)) {
    throw new Error(`Unsupported Work Item lifecycle: ${dto.lifecycleState}`);
  }
  const completedAt = dto.completedAt ?? null;
  if (completedAt !== null) expectInstant(completedAt, 'Work Item completedAt');
  return {
    id: dto.id,
    title: dto.title,
    description: dto.description ?? null,
    lifecycleState: dto.lifecycleState as ProductWorkItemLifecycle,
    completedAt,
    createdAt: expectInstant(dto.createdAt, 'Work Item createdAt'),
    updatedAt: expectInstant(dto.updatedAt, 'Work Item updatedAt'),
  };
}

const severities: readonly ProductAttentionSeverity[] = ['INFO', 'WARNING', 'CRITICAL'];

const stepStates = [
  'NOT_STARTED',
  'IN_PROGRESS',
  'COMPLETED',
  'WAITING',
  'BLOCKED',
  'FAILED',
  'UNKNOWN',
  'SKIPPED',
] as const;
const trackingSignals = ['NEUTRAL', 'SATISFIED', 'WATCH', 'PROBLEM'] as const;
const itemLifecycles = ['ACTIVE', 'COMPLETED', 'CANCELLED'] as const;

function mapTrackingSummary(dto: app.WorkItemOverviewTrackingDTO): ProductTrackingSummary {
  return {
    trackedItemCount: dto.trackedItemCount,
    workflow: dto.workflow
      ? {
          id: dto.workflow.id,
          definitionId: dto.workflow.definitionId,
          stages: (dto.workflow.stages ?? []).map((stage) => ({ ...stage })),
        }
      : null,
    items: (dto.items ?? []).map((item) => {
      const state = item.currentState ?? null;
      if (state !== null && !stepStates.includes(state as (typeof stepStates)[number])) {
        throw new Error(`Unsupported overview step state: ${state}`);
      }
      if (!itemLifecycles.includes(item.lifecycle as (typeof itemLifecycles)[number])) {
        throw new Error(`Unsupported overview item lifecycle: ${item.lifecycle}`);
      }
      if (!trackingSignals.includes(item.signal.state as (typeof trackingSignals)[number])) {
        throw new Error(`Unsupported overview tracking signal: ${item.signal.state}`);
      }
      const signalSeverity = item.signal.highestAttentionSeverity ?? null;
      if (
        signalSeverity !== null &&
        !severities.includes(signalSeverity as ProductAttentionSeverity)
      ) {
        throw new Error(`Unsupported overview signal severity: ${signalSeverity}`);
      }
      return {
        id: item.id,
        title: item.title,
        itemType: item.itemType ?? null,
        parentItemId: item.parentItemId ?? null,
        lifecycle: item.lifecycle as 'ACTIVE' | 'COMPLETED' | 'CANCELLED',
        currentStepId: item.currentStepId ?? null,
        currentState: state as ProductTrackingSummary['items'][number]['currentState'],
        complete: item.complete,
        resolved: item.resolved,
        signal: {
          state: item.signal.state as ProductTrackingSummary['items'][number]['signal']['state'],
          hasActiveTrackingTodo: item.signal.hasActiveTrackingTodo,
          governingTodoDueAt: item.signal.governingTodoDueAt
            ? expectInstant(item.signal.governingTodoDueAt, 'tracking Todo dueAt')
            : null,
          actionableAttentionCount: item.signal.actionableAttentionCount,
          highestAttentionSeverity: signalSeverity as ProductAttentionSeverity | null,
          hasActiveExpectation: item.signal.hasActiveExpectation,
          hasSatisfiedExpectation: item.signal.hasSatisfiedExpectation,
          hasOverdueUnmetExpectation: item.signal.hasOverdueUnmetExpectation,
        },
      };
    }),
    stateDistribution: { ...dto.stateDistribution },
    activeFinalTarget: dto.activeFinalTarget ? mapMonitorPoint(dto.activeFinalTarget) : null,
  };
}

function mapAttentionSummary(dto: app.WorkItemOverviewAttentionDTO): ProductAttentionSummary {
  const severity = dto.highestActiveSeverity ?? null;
  if (severity !== null && !severities.includes(severity as ProductAttentionSeverity)) {
    throw new Error(`Unsupported attention severity: ${severity}`);
  }
  return {
    openIssueCount: dto.openIssueCount,
    actionableIssueCount: dto.actionableIssueCount,
    snoozedIssueCount: dto.snoozedIssueCount,
    highestActionableSeverity: severity as ProductAttentionSeverity | null,
  };
}

/**
 * Maps the authoritative Work Items overview. When a section could not be read
 * (`*Available` false) the row carries `null` for it — never a fabricated zero —
 * so the UI can tell "valid empty" from "unavailable".
 */
export function mapWorkItemOverviewDTO(dto: app.WorkItemOverviewDTO): ProductWorkItemOverview {
  return {
    items: (dto.items ?? []).map((item) => ({
      workItem: mapWorkItemDTO(item.workItem),
      tracking: dto.trackingAvailable ? mapTrackingSummary(item.tracking) : null,
      attention: dto.attentionAvailable ? mapAttentionSummary(item.attention) : null,
    })),
  };
}
