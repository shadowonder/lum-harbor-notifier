import { workItems, WORK_ITEMS_TODAY } from './mockData';
import {
  getActiveCheckpoint,
  getProgressSummary,
  getReadinessSummary,
} from '../../presentation/workItems/selectors';
import {
  applyProgressBranchHints,
  buildHistoricalProgressProjection,
  buildProgressProjection,
  type ProgressProjectionBranchHint,
} from '../../presentation/workItems/buildProgressProjection';
import { mostSignificantStatus } from '../../presentation/workItems/statusPresentation';
import type { WorkItem } from '../../types/workItem';
import type {
  AttentionSummaryDto,
  ProgressProjectionDto,
  ReadinessSummaryDto,
  WorkItemApi,
  WorkItemListQueryDto,
  WorkItemDetailDto,
  WorkItemSummaryDto,
} from './contracts';

function readinessDto(item: WorkItem): ReadinessSummaryDto {
  const summary = getReadinessSummary(item);
  return {
    scopeLabel: getActiveCheckpoint(item)?.title ?? 'Final target',
    ready: summary.ready.length,
    required: summary.required.length,
    blocked: summary.blocked.length,
    waiting: summary.missing.length,
    unknown: summary.unknown.length,
  };
}

function attentionDto(projection: ProgressProjectionDto): AttentionSummaryDto {
  const branchStatuses = projection.streams
    .filter((stream) => stream.kind === 'branch')
    .map((stream) =>
      mostSignificantStatus(
        projection.nodes.filter((node) => node.streamId === stream.id).map((node) => node.status),
      ),
    )
    .filter((status) => status !== undefined && status !== 'completed');
  return {
    unresolvedBranches: branchStatuses.length,
    failed: branchStatuses.filter((status) => status === 'failed').length,
    blocked: branchStatuses.filter((status) => status === 'blocked').length,
    waiting: branchStatuses.filter((status) => status === 'waiting').length,
    unknown: branchStatuses.filter((status) => status === 'unknown').length,
  };
}

function toSummary(item: WorkItem): WorkItemSummaryDto {
  const fullProjection = projectionFor(item);
  const readiness = readinessDto(item);
  const itemProgress = getProgressSummary(item);
  const progress = readiness.required
    ? {
        completed: readiness.ready,
        total: readiness.required,
        label: `${readiness.ready}/${readiness.required} ready`,
      }
    : {
        completed: itemProgress.complete.length,
        total: itemProgress.total,
        label: `${itemProgress.complete.length}/${itemProgress.total} complete`,
      };
  return {
    id: item.id,
    title: item.title,
    labels: item.labels,
    attention: item.attention,
    currentState: item.currentState,
    lifecycleState: item.status === 'complete' ? 'completed' : 'active',
    targetAt: item.targetAt,
    forecastAt: item.forecastAt,
    startedAt: item.startedAt,
    completedAt: item.completedAt,
    finalOutcome: item.finalOutcome,
    milestones: item.milestones ?? [],
    nextAction: item.nextActions[0],
    progress,
    readiness,
    attentionSummary: attentionDto(fullProjection),
    progressProjection: fullProjection,
  };
}

function toDetail(item: WorkItem): WorkItemDetailDto {
  const progressProjection = projectionFor(item);
  return {
    id: item.id,
    title: item.title,
    labels: item.labels,
    profile: item.profile,
    status: item.status,
    currentState: item.currentState,
    attention: item.attention,
    targetAt: item.targetAt,
    forecastAt: item.forecastAt,
    startedAt: item.startedAt,
    completedAt: item.completedAt,
    finalOutcome: item.finalOutcome,
    milestones: item.milestones ?? [],
    updatedAt: item.updatedAt,
    checkpoints: item.checkpoints,
    activeCheckpointId: item.activeCheckpointId,
    items: item.items,
    requirements: item.requirements,
    relations: item.relations,
    trackingDimensions: item.trackingDimensions,
    trackingStates: item.trackingStates,
    nextActions: item.nextActions,
    relatedTodoCount: item.relatedTodoCount,
    relatedEventCount: item.relatedEventCount,
    sources: item.sources ?? [],
    evidence: item.evidence ?? [],
    readiness: readinessDto(item),
    attentionSummary: attentionDto(progressProjection),
    progressProjection,
  };
}

const branchHints: Record<string, ProgressProjectionBranchHint[]> = {
  'production-release': [{ childStreamId: 'items-critical_fix', parentStreamId: 'items-defect' }],
  'distributed-systems-learning': [
    { childStreamId: 'items-assignment', parentStreamId: 'tracking-exam' },
  ],
};

function projectionFor(item: WorkItem) {
  const finalProjection = applyProgressBranchHints(
    buildProgressProjection(item),
    branchHints[item.id] ?? [],
  );
  return buildHistoricalProgressProjection(item, finalProjection);
}

export class MockWorkItemApi implements WorkItemApi {
  async listWorkItems(query: WorkItemListQueryDto = { lifecycleState: 'active' }) {
    const lifecycleState = query.lifecycleState ?? 'active';
    const matching = workItems
      .filter((item) =>
        lifecycleState === 'completed' ? item.status === 'complete' : item.status !== 'complete',
      )
      .sort((left, right) =>
        lifecycleState === 'completed'
          ? new Date(right.completedAt ?? 0).getTime() - new Date(left.completedAt ?? 0).getTime()
          : 0,
      );
    const items = query.limit ? matching.slice(0, query.limit) : matching;
    return { asOf: WORK_ITEMS_TODAY, items: items.map(toSummary), total: matching.length };
  }

  async getWorkItem(id: string) {
    const item = workItems.find((entry) => entry.id === id);
    return item ? toDetail(item) : null;
  }

  async getWorkItemActivity(id: string) {
    return workItems.find((entry) => entry.id === id)?.activity ?? [];
  }
}
