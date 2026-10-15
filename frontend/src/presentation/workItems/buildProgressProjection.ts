import type {
  ProgressLinkDto,
  ProgressNodeDto,
  ProgressNodeTypeDto,
  ProgressNavigationDto,
  ProgressProjectionDto,
  ProgressStageDto,
  ProgressStatusDto,
  ProgressStreamDto,
} from '../../api/workItems/contracts';
import type { Requirement, TrackableItem, TrackingStatus, WorkItem } from '../../types/workItem';
import { isAttentionStatus, mostSignificantStatus, normalizeProgressStatus, statusPresentation } from './statusPresentation';

function itemStatus(item: TrackableItem): ProgressStatusDto {
  return normalizeProgressStatus(item.status);
}

function requirementStatus(requirement: Requirement): ProgressStatusDto {
  return normalizeProgressStatus(requirement.status);
}

function trackingStatus(status: TrackingStatus): ProgressStatusDto {
  return normalizeProgressStatus(status);
}

function aggregateNode(
  id: string,
  streamId: string,
  stageId: string,
  statuses: ProgressStatusDto[],
  entityIds: string[],
  shortLabel?: string,
  nodeType: ProgressNodeTypeDto = 'action',
  title?: string,
  reason?: string,
  navigation?: ProgressNavigationDto,
): ProgressNodeDto {
  const status = mostSignificantStatus(statuses);
  return {
    id,
    streamId,
    stageId,
    status,
    nodeType,
    title,
    reason,
    count: statuses.filter((entry) => entry === 'completed').length,
    total: statuses.length,
    shortLabel,
    sourceEntityIds: entityIds,
    attention: isAttentionStatus(status),
    navigation,
  };
}

function groupBy<T>(values: T[], getKey: (value: T) => string) {
  return values.reduce<Record<string, T[]>>((groups, value) => {
    (groups[getKey(value)] ??= []).push(value);
    return groups;
  }, {});
}

export function buildProgressProjection(item: WorkItem): ProgressProjectionDto {
  const dimensions = [...item.trackingDimensions].sort((a, b) => a.order - b.order);
  const checkpoints = [...item.checkpoints].sort(
    (left, right) => new Date(left.targetAt).getTime() - new Date(right.targetAt).getTime(),
  );
  const stageSource = checkpoints.length
    ? checkpoints.map((checkpoint, index) => ({
        id: checkpoint.id,
        label: dimensions[index]?.label ?? checkpoint.title,
        date: checkpoint.targetAt,
      }))
    : [{ id: `${item.id}-current`, label: dimensions[0]?.label ?? 'Current' }];
  const stages: ProgressStageDto[] = [
    ...stageSource.map((stage, index) => ({ ...stage, order: index })),
    {
      id: `${item.id}-target-stage`,
      label: 'Target',
      order: stageSource.length,
      date: item.targetAt,
      target: true,
    },
  ];
  const targetStage = stages[stages.length - 1];
  const mainStream: ProgressStreamDto = {
    id: 'main',
    label: 'Main Scope',
    kind: 'main',
    summary: item.currentState,
    priority: 100,
    requiredBeforeTarget: true,
  };
  const progressItems = item.items.filter((entry) =>
    item.profile.progressCategoryIds.includes(entry.category),
  );
  const mainSubjects = progressItems.length ? progressItems : item.items;
  const nodes: ProgressNodeDto[] = [];
  const links: ProgressLinkDto[] = [];
  const navigationFor = (entityIds: string[]): ProgressNavigationDto => {
    const sourceId = item.items.find(
      (entry) => entityIds.includes(entry.id) && entry.sourceRefId,
    )?.sourceRefId;
    const externalUrl = item.sources?.find((source) => source.id === sourceId)?.url;
    if (externalUrl) return { destination: 'external', url: externalUrl };
    const activity = item.activity.find((entry) =>
      entry.entityIds?.some((entityId) => entityIds.includes(entityId)),
    );
    if (activity) return { destination: 'activity', activityId: activity.id };
    return { destination: 'details', entityId: entityIds[0] ?? item.id };
  };

  stages.slice(0, -1).forEach((stage, index) => {
    const dimension = dimensions[index];
    const statuses = mainSubjects.map((subject) => {
      const state = dimension
        ? item.trackingStates.find(
            (entry) => entry.dimensionId === dimension.id && entry.subjectId === subject.id,
          )
        : undefined;
      return state ? trackingStatus(state.status) : dimension ? 'queued' : itemStatus(subject);
    });
    nodes.push(
      aggregateNode(
        `main-${stage.id}`,
        mainStream.id,
        stage.id,
        statuses.length ? statuses : [item.status === 'complete' ? 'completed' : 'in_progress'],
        mainSubjects.map((entry) => entry.id),
        stage.label,
        'checkpoint',
        `${stage.label} progression`,
        statuses.length ? `${statuses.filter((entry) => entry === 'completed').length}/${statuses.length} complete` : item.currentState,
        mainSubjects[0] ? { destination: 'details', entityId: mainSubjects[0].id } : undefined,
      ),
    );
  });
  nodes.push({
    id: `${item.id}-target`,
    streamId: mainStream.id,
    stageId: targetStage.id,
    status: 'target',
    nodeType: 'checkpoint',
    title: 'Final target',
    reason: item.targetAt,
    shortLabel: item.targetAt,
    sourceEntityIds: [item.id],
    attention: false,
    navigation: { destination: 'details', entityId: item.id },
  });

  const itemBranches = groupBy(
    item.items.filter((entry) => !mainSubjects.includes(entry)),
    (entry) => entry.category,
  );
  const requirementBranches = groupBy(item.requirements.filter((entry) => entry.required), (entry) => entry.group);
  const mainSubjectIds = new Set(mainSubjects.map((entry) => entry.id));
  const trackingBranches = groupBy(
    item.trackingStates.filter(
      (state) => mainSubjectIds.has(state.subjectId) && isAttentionStatus(trackingStatus(state.status)),
    ),
    (state) => state.dimensionId,
  );
  interface BranchInput {
    id: string;
    label: string;
    statuses: ProgressStatusDto[];
    entityIds: string[];
    sourceHint?: string;
    nodeType: ProgressNodeTypeDto;
    reason?: string;
    navigation: ProgressNavigationDto;
  }
  const branchInputs: BranchInput[] = [
    ...Object.entries(itemBranches).map(([key, entries]) => ({
      id: `items-${key}`,
      label: item.profile.categoryLabels[key] ?? key,
      statuses: entries.map(itemStatus),
      entityIds: entries.map((entry) => entry.id),
      sourceHint: entries.find((entry) => entry.sourceRefId)?.sourceRefId,
      nodeType: (entries.some((entry) => ['blocked', 'unknown'].includes(entry.status)) ? 'issue' : 'action') as ProgressNodeTypeDto,
      reason: entries.find((entry) => entry.waitingOn)?.waitingOn
        ? `Waiting on ${entries.find((entry) => entry.waitingOn)?.waitingOn}`
        : entries.find((entry) => entry.status !== 'complete')?.title,
      navigation: navigationFor(entries.map((entry) => entry.id)),
    })),
    ...Object.entries(requirementBranches).map(([key, entries]) => ({
      id: `requirements-${key.toLowerCase().replace(/[^a-z0-9]+/g, '-')}`,
      label: key,
      statuses: entries.map(requirementStatus),
      entityIds: entries.map((entry) => entry.id),
      sourceHint: undefined,
      nodeType: 'requirement' as const,
      reason: entries.find((entry) => entry.status !== 'ready')?.title,
      navigation: navigationFor(entries.map((entry) => entry.id)),
    })),
    ...Object.entries(trackingBranches).map(([key, entries]) => ({
      id: `tracking-${key}`,
      label: `${dimensions.find((dimension) => dimension.id === key)?.label ?? key} follow-up`,
      statuses: entries.map((entry) => trackingStatus(entry.status)),
      entityIds: [...new Set(entries.map((entry) => entry.subjectId))],
      sourceHint: undefined,
      nodeType: 'issue' as const,
      reason: `${dimensions.find((dimension) => dimension.id === key)?.label ?? key} needs follow-up`,
      navigation: navigationFor([...new Set(entries.map((entry) => entry.subjectId))]),
    })),
  ];
  const streams: ProgressStreamDto[] = [mainStream];

  branchInputs.forEach((branch, branchIndex) => {
    const tracking = item.trackingStates
      .filter((state) => branch.entityIds.includes(state.subjectId))
      .map((state) => ({ ...state, normalized: trackingStatus(state.status) }));
    const overall = mostSignificantStatus([...branch.statuses, ...tracking.map((entry) => entry.normalized)]);
    const firstAttentionIndex = dimensions.findIndex((dimension) =>
      tracking.some(
        (state) => state.dimensionId === dimension.id && isAttentionStatus(state.normalized),
      ),
    );
    const firstActiveIndex = dimensions.findIndex((dimension) =>
      tracking.some(
        (state) => state.dimensionId === dimension.id && state.normalized !== 'completed',
      ),
    );
    const branchAt = Math.min(
      stages.length - 2,
      Math.max(0, firstAttentionIndex >= 0 ? firstAttentionIndex : firstActiveIndex >= 0 ? firstActiveIndex : Math.min(branchIndex, stages.length - 2)),
    );
    const stream: ProgressStreamDto = {
      id: branch.id,
      label: branch.label,
      kind: 'branch',
      summary: `${branch.statuses.filter((status) => status !== 'completed').length} unresolved`,
      priority: statusPresentation[overall].priority,
      requiredBeforeTarget: true,
      sourceHint: branch.sourceHint,
    };
    streams.push(stream);
    const branchStages = stages.slice(branchAt, -1);
    const effectiveStages = branchStages.length ? branchStages : stages.slice(0, 1);
    effectiveStages.forEach((stage, localIndex) => {
      const stageIndex = stages.findIndex((entry) => entry.id === stage.id);
      const dimension = dimensions[stageIndex];
      const stageStatuses = dimension
        ? tracking
            .filter((state) => state.dimensionId === dimension.id)
            .map((state) => state.normalized)
        : branch.statuses;
      nodes.push(
        aggregateNode(
          `${stream.id}-${stage.id}`,
          stream.id,
          stage.id,
          stageStatuses.length
            ? [...stageStatuses, ...(localIndex === 0 ? branch.statuses : [])]
            : branch.statuses,
          branch.entityIds,
          localIndex === 0 ? branch.label : undefined,
          branch.nodeType,
          branch.label,
          localIndex === 0 ? branch.reason : statusPresentation[mostSignificantStatus(stageStatuses.length ? stageStatuses : branch.statuses)].label,
          branch.navigation,
        ),
      );
    });
    const firstNodeId = `${stream.id}-${effectiveStages[0].id}`;
    const sourceStageIndex = Math.max(0, stages.findIndex((stage) => stage.id === effectiveStages[0].id));
    links.push({
      id: `branch-${stream.id}`,
      fromNodeId: `main-${stages[sourceStageIndex].id}`,
      toNodeId: firstNodeId,
      kind: 'branch',
    });
    effectiveStages.slice(1).forEach((stage, index) => {
      links.push({
        id: `progress-${stream.id}-${stage.id}`,
        fromNodeId: `${stream.id}-${effectiveStages[index].id}`,
        toNodeId: `${stream.id}-${stage.id}`,
        kind: 'progress',
      });
    });
    links.push({
      id: `merge-${stream.id}`,
      fromNodeId: `${stream.id}-${effectiveStages[effectiveStages.length - 1].id}`,
      toNodeId: `${item.id}-target`,
      kind: 'merge',
      open: overall !== 'completed',
    });
  });

  stages.slice(1).forEach((stage, index) => {
    links.push({
      id: `main-progress-${stage.id}`,
      fromNodeId: index === stages.length - 2 ? `main-${stages[index].id}` : `main-${stages[index].id}`,
      toNodeId: stage.target ? `${item.id}-target` : `main-${stage.id}`,
      kind: 'progress',
    });
  });

  return { stages, streams, nodes, links, targetNodeId: `${item.id}-target` };
}

export interface ProgressProjectionBranchHint {
  childStreamId: string;
  parentStreamId: string;
}

export function applyProgressBranchHints(
  projection: ProgressProjectionDto,
  hints: ProgressProjectionBranchHint[],
): ProgressProjectionDto {
  if (!hints.length) return projection;
  const links = projection.links.map((link) => {
    const hint = hints.find(
      (entry) => link.kind === 'branch' && link.id === `branch-${entry.childStreamId}`,
    );
    if (!hint) return link;
    const childNode = projection.nodes.find((node) => node.id === link.toNodeId);
    const parentNodes = projection.nodes.filter((node) => node.streamId === hint.parentStreamId);
    const parentNode =
      parentNodes.find((node) => node.stageId === childNode?.stageId) ?? parentNodes[parentNodes.length - 1];
    return parentNode ? { ...link, fromNodeId: parentNode.id } : link;
  });
  return { ...projection, links };
}

export function buildHistoricalProgressProjection(
  item: WorkItem,
  finalProjection: ProgressProjectionDto,
): ProgressProjectionDto {
  if (item.status !== 'complete') return finalProjection;
  const latestMilestone = [...(item.milestones ?? [])]
    .sort((left, right) => new Date(right.occurredAt).getTime() - new Date(left.occurredAt).getTime())[0];
  return {
    ...finalProjection,
    nodes: finalProjection.nodes.map((node) => node.id === finalProjection.targetNodeId ? {
      ...node,
      metadata: {
        ...node.metadata,
        lifecycleState: 'completed',
        ...(item.completedAt ? { completedAt: item.completedAt } : {}),
        ...(latestMilestone ? { latestMilestone: latestMilestone.label } : {}),
      },
    } : node),
  };
}
