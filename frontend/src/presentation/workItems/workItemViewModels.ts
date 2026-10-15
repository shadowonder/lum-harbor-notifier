import type {
  AttentionSummaryDto,
  ProgressProjectionDto,
  ReadinessSummaryDto,
  WorkItemDetailDto,
  WorkItemSummaryDto,
  WorkItemMilestoneDto,
} from '../../api/workItems/contracts';
import type { Attention, WorkItem, WorkItemLifecycleState } from '../../types/workItem';
import {
  deriveAttentionExceptions,
  deriveKeySignals,
  getPrimaryTarget,
  getProgressSummary,
  getTrackingSummaries,
  type AttentionException,
  type TrackingSummary,
  type KeySignal,
} from './selectors';
import { buildCompactLineage, type CompactLineageViewModel } from './compactLineageProjection';

export type ProgressLineageViewModel = ProgressProjectionDto;

export interface WorkItemSummaryViewModel {
  id: string;
  title: string;
  labels: string[];
  attention: Attention;
  currentState: string;
  lifecycleState: WorkItemLifecycleState;
  targetAt?: string;
  forecastAt?: string;
  nextAction?: string;
  latestProgress: TimelineFactViewModel[];
  completedAt?: string;
  duration?: string;
  finalOutcome?: string;
  readiness: ReadinessSummaryDto;
  attentionSummary: AttentionSummaryDto;
  compactLineage: CompactLineageViewModel;
  narrowCompactLineage: CompactLineageViewModel;
}

export interface TimelineFactViewModel {
  id: string;
  date: string;
  label: string;
}

export interface WorkItemDetailViewModel {
  item: WorkItem;
  readiness: ReadinessSummaryDto;
  attentionSummary: AttentionSummaryDto;
  lineage: ProgressLineageViewModel;
  exceptions: AttentionException[];
  tracking: TrackingSummary[];
  progress: { complete: number; total: number; percent: number };
  target: ReturnType<typeof getPrimaryTarget>;
  headerSignals: KeySignal[];
}

function timelineFacts(milestones: WorkItemMilestoneDto[], limit: number): TimelineFactViewModel[] {
  return [...milestones]
    .sort((left, right) => new Date(right.occurredAt).getTime() - new Date(left.occurredAt).getTime())
    .slice(0, limit)
    .map((milestone) => ({ id: milestone.id, date: new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric' }).format(new Date(milestone.occurredAt)), label: milestone.label }));
}

function durationLabel(startedAt?: string, completedAt?: string) {
  if (!startedAt || !completedAt) return undefined;
  const days = Math.max(1, Math.round((new Date(completedAt).getTime() - new Date(startedAt).getTime()) / (24 * 60 * 60 * 1000)));
  return `${days} ${days === 1 ? 'day' : 'days'} total`;
}

export function toWorkItemSummaryViewModel(dto: WorkItemSummaryDto): WorkItemSummaryViewModel {
  return {
    id: dto.id,
    title: dto.title,
    labels: dto.labels,
    attention: dto.attention,
    currentState: dto.currentState,
    lifecycleState: dto.lifecycleState,
    targetAt: dto.targetAt,
    forecastAt: dto.forecastAt,
    nextAction: dto.nextAction,
    latestProgress: timelineFacts(dto.milestones, dto.lifecycleState === 'completed' ? 2 : 1),
    completedAt: dto.completedAt,
    duration: durationLabel(dto.startedAt, dto.completedAt),
    finalOutcome: dto.finalOutcome,
    readiness: dto.readiness,
    attentionSummary: dto.attentionSummary,
    compactLineage: buildCompactLineage(dto.progressProjection, 5, dto.readiness, dto.lifecycleState),
    narrowCompactLineage: buildCompactLineage(dto.progressProjection, 3, dto.readiness, dto.lifecycleState),
  };
}

export function toWorkItemDetailViewModel(dto: WorkItemDetailDto): WorkItemDetailViewModel {
  const item: WorkItem = {
    id: dto.id,
    title: dto.title,
    labels: dto.labels,
    profile: dto.profile,
    status: dto.status,
    currentState: dto.currentState,
    attention: dto.attention,
    targetAt: dto.targetAt,
    forecastAt: dto.forecastAt,
    startedAt: dto.startedAt,
    completedAt: dto.completedAt,
    finalOutcome: dto.finalOutcome,
    milestones: dto.milestones,
    updatedAt: dto.updatedAt,
    checkpoints: dto.checkpoints,
    activeCheckpointId: dto.activeCheckpointId,
    items: dto.items,
    requirements: dto.requirements,
    relations: dto.relations,
    trackingDimensions: dto.trackingDimensions,
    trackingStates: dto.trackingStates,
    nextActions: dto.nextActions,
    relatedTodoCount: dto.relatedTodoCount,
    relatedEventCount: dto.relatedEventCount,
    sources: dto.sources,
    evidence: dto.evidence,
    activity: [],
  };
  const progress = getProgressSummary(item);
  return {
    item,
    readiness: dto.readiness,
    attentionSummary: dto.attentionSummary,
    lineage: dto.progressProjection,
    exceptions: deriveAttentionExceptions(item),
    tracking: getTrackingSummaries(item),
    progress: { complete: progress.complete.length, total: progress.total, percent: progress.percent },
    target: getPrimaryTarget(item),
    headerSignals: deriveKeySignals(item)
      .filter((signal) => !['progress', 'readiness', 'variance', 'checkpoint'].includes(signal.id))
      .slice(0, 3),
  };
}
