import type {
  Checkpoint,
  Requirement,
  TrackableItem,
  TrackingDimension,
  TrackingStatus,
  WorkItem,
} from '../../types/workItem';

const dayMs = 24 * 60 * 60 * 1000;
const positiveTrackingStatuses: TrackingStatus[] = ['complete', 'verified'];

export interface WorkItemCapabilities {
  hasProgress: boolean;
  hasReadiness: boolean;
  hasTracking: boolean;
}

export interface KeySignal {
  id: string;
  label: string;
  severity: 'neutral' | 'positive' | 'warning' | 'critical';
  priority: number;
}

export interface AttentionException {
  id: string;
  entityId: string;
  title: string;
  context: string;
  status: 'failed' | 'blocked' | 'waiting' | 'unknown';
}

function parseDate(value: string) {
  return new Date(value.includes('T') ? value : `${value}T12:00:00`);
}

export function getCountdown(targetAt: string, long = false, asOf: Date = new Date()) {
  const today = new Date(asOf.getFullYear(), asOf.getMonth(), asOf.getDate(), 12);
  const target = parseDate(targetAt);
  const days = Math.round((target.getTime() - today.getTime()) / dayMs);
  if (days < 0) return `Overdue ${Math.abs(days)} ${Math.abs(days) === 1 ? 'day' : 'days'}`;
  if (days === 0) return targetAt.includes('T') ? `Today · ${formatTime(target)}` : 'Today';
  if (days === 1) return 'Tomorrow';
  return `${days} days${long ? ' remaining' : ''}`;
}

export function formatDate(value: string) {
  return new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric' }).format(
    parseDate(value),
  );
}

export function getVarianceDays(targetAt: string, forecastAt: string) {
  return Math.round((parseDate(forecastAt).getTime() - parseDate(targetAt).getTime()) / dayMs);
}

export function formatVariance(days: number) {
  return `${days > 0 ? '+' : ''}${days}d`;
}

function formatTime(date: Date) {
  return new Intl.DateTimeFormat('en-US', { hour: 'numeric', minute: '2-digit' }).format(date);
}

export function getActiveCheckpoint(item: WorkItem): Checkpoint | undefined {
  return item.checkpoints.find((entry) => entry.id === item.activeCheckpointId);
}

export function getPrimaryTarget(item: WorkItem) {
  if (!item.targetAt) return undefined;
  return {
    targetAt: item.targetAt,
    dateLabel: formatDate(item.targetAt),
    countdown: getCountdown(item.targetAt),
    forecastAt: item.forecastAt,
    forecastDateLabel: item.forecastAt ? formatDate(item.forecastAt) : undefined,
    varianceDays: item.forecastAt ? getVarianceDays(item.targetAt, item.forecastAt) : undefined,
  };
}

export function getActiveItems(item: WorkItem) {
  const activeCheckpointId = item.activeCheckpointId;
  if (!activeCheckpointId) return item.items;
  return item.items.filter(
    (entry) => !entry.checkpointIds?.length || entry.checkpointIds.includes(activeCheckpointId),
  );
}

export function getProgressItems(item: WorkItem) {
  const categoryIds = new Set(item.profile.progressCategoryIds);
  return getActiveItems(item).filter((entry) => categoryIds.has(entry.category));
}

export function getProgressSummary(item: WorkItem) {
  const items = getProgressItems(item);
  const complete = items.filter((entry) => entry.status === 'complete');
  return {
    items,
    complete,
    total: items.length,
    percent: items.length ? (complete.length / items.length) * 100 : 0,
  };
}

export function getActiveRequirements(item: WorkItem) {
  if (!item.activeCheckpointId) return item.requirements.filter((entry) => !entry.checkpointId);
  const checkpointRequirements = item.requirements.filter(
    (entry) => entry.checkpointId === item.activeCheckpointId,
  );
  return checkpointRequirements.some((entry) => entry.required)
    ? checkpointRequirements
    : item.requirements.filter((entry) => !entry.checkpointId);
}

export function getReadinessSummary(item: WorkItem) {
  const activeRequirements = getActiveRequirements(item);
  const required = activeRequirements.filter((entry) => entry.required);
  const ready = required.filter((entry) => entry.status === 'ready');
  const blocked = required.filter((entry) => entry.status === 'blocked');
  const missing = required.filter((entry) => entry.status === 'missing');
  const unknown = required.filter((entry) => entry.status === 'unknown');
  const remaining = required.filter((entry) => entry.status !== 'ready');
  return { activeRequirements, required, ready, blocked, missing, unknown, remaining };
}

export function getBlockers(item: WorkItem) {
  const requirementBlockers = getReadinessSummary(item).remaining.map((entry) => ({
    id: entry.id,
    title: entry.title,
    status: entry.status,
    kind: 'requirement' as const,
  }));
  const itemBlockers = getActiveItems(item)
    .filter((entry) => entry.critical && entry.status === 'blocked')
    .map((entry) => ({
      id: entry.id,
      entityId: entry.id,
      title: entry.title,
      status: 'blocked' as const,
      kind: 'item' as const,
    }));
  return [...requirementBlockers, ...itemBlockers];
}

export function deriveAttentionExceptions(item: WorkItem): AttentionException[] {
  const exceptions: AttentionException[] = [];
  getActiveItems(item).forEach((entry) => {
    if (!['blocked', 'waiting', 'unknown'].includes(entry.status)) return;
    exceptions.push({
      id: entry.id,
      entityId: entry.id,
      title: entry.reference ? `${entry.reference} · ${entry.title}` : entry.title,
      context: entry.waitingOn ? `Waiting on ${entry.waitingOn}` : item.profile.categoryLabels[entry.category] ?? item.profile.itemsLabel,
      status: entry.status as AttentionException['status'],
    });
  });
  getActiveRequirements(item).forEach((entry) => {
    if (entry.status === 'ready') return;
    exceptions.push({
      id: entry.id,
      entityId: entry.id,
      title: entry.title,
      context: entry.group,
      status: entry.status === 'missing' ? 'waiting' : entry.status,
    });
  });
  item.trackingStates.forEach((entry) => {
    if (!['failed', 'blocked', 'waiting', 'unknown', 'behind'].includes(entry.status)) return;
    const subject = getEntityLabel(item, entry.subjectId);
    const dimension = item.trackingDimensions.find((value) => value.id === entry.dimensionId);
    const id = `${entry.subjectId}-${entry.dimensionId}`;
    if (exceptions.some((value) => value.id === entry.subjectId || value.id === id)) return;
    exceptions.push({
      id,
      entityId: entry.subjectId,
      title: subject.title,
      context: dimension?.label ?? item.profile.trackingLabel,
      status: (entry.status === 'behind' ? 'failed' : entry.status) as AttentionException['status'],
    });
  });
  const priority: Record<AttentionException['status'], number> = { failed: 4, blocked: 3, waiting: 2, unknown: 1 };
  return exceptions.sort((left, right) => priority[right.status] - priority[left.status]);
}

export function groupItems(item: WorkItem) {
  return getActiveItems(item).reduce<Record<string, TrackableItem[]>>((groups, entry) => {
    (groups[entry.category] ??= []).push(entry);
    return groups;
  }, {});
}

export function groupRequirements(requirements: Requirement[]) {
  return requirements.reduce<Record<string, Requirement[]>>((groups, entry) => {
    (groups[entry.group] ??= []).push(entry);
    return groups;
  }, {});
}

export function getTrackingStatus(
  item: WorkItem,
  dimensionId: string,
  subjectId: string,
): TrackingStatus {
  return (
    item.trackingStates.find(
      (state) => state.dimensionId === dimensionId && state.subjectId === subjectId,
    )?.status ?? 'unknown'
  );
}

export interface TrackingSummary {
  dimension: TrackingDimension;
  complete: number;
  total: number;
  problems: number;
  problemStatus?: 'failed' | 'blocked';
  unknown: number;
  state: 'complete' | 'atRisk' | 'partial' | 'notStarted' | 'unknown';
}

export function getTrackingSummaries(item: WorkItem): TrackingSummary[] {
  const subjects = getTrackingSubjects(item);
  return [...item.trackingDimensions]
    .sort((a, b) => a.order - b.order)
    .map((dimension) => {
      const states = subjects.map((subject) => getTrackingStatus(item, dimension.id, subject.id));
      const complete = states.filter((status) => positiveTrackingStatuses.includes(status)).length;
      const problemStates = item.trackingStates.filter(
        (state) =>
          state.dimensionId === dimension.id &&
          (state.status === 'failed' || state.status === 'blocked'),
      );
      const problems = problemStates.length;
      const problemStatus = problemStates.some((state) => state.status === 'failed')
        ? 'failed'
        : problemStates.some((state) => state.status === 'blocked')
          ? 'blocked'
          : undefined;
      const unknown = states.filter((status) => status === 'unknown').length;
      let state: TrackingSummary['state'] = 'partial';
      if (problems) state = 'atRisk';
      else if (complete === states.length && states.length) state = 'complete';
      else if (!complete && unknown === states.length) state = 'unknown';
      else if (!complete) state = 'notStarted';
      return { dimension, complete, total: states.length, problems, problemStatus, unknown, state };
    });
}

export function getCapabilities(item: WorkItem): WorkItemCapabilities {
  const progress = getProgressSummary(item);
  const readiness = getReadinessSummary(item);
  return {
    hasProgress: progress.total > 0,
    hasReadiness: readiness.required.length > 0,
    hasTracking: item.trackingDimensions.length > 0,
  };
}

export function deriveKeySignals(item: WorkItem): KeySignal[] {
  const capabilities = getCapabilities(item);
  const signals: KeySignal[] = [];
  const progress = getProgressSummary(item);
  const readiness = getReadinessSummary(item);
  const blockers = getBlockers(item);
  const target = getPrimaryTarget(item);
  if (capabilities.hasProgress)
    signals.push({
      id: 'progress',
      label: `${progress.complete.length}/${progress.total} ${item.profile.progressLabel}`,
      severity: 'neutral',
      priority: 40,
    });
  if (capabilities.hasReadiness)
    signals.push({
      id: 'readiness',
      label: `${readiness.ready.length}/${readiness.required.length} ready`,
      severity: readiness.remaining.length ? 'warning' : 'positive',
      priority: 50,
    });
  if (blockers.length)
    signals.push({
      id: 'blockers',
      label: `${blockers.length} blocker${blockers.length === 1 ? '' : 's'}`,
      severity: blockers.some((entry) => entry.status === 'blocked' || entry.status === 'missing')
        ? 'critical'
        : 'warning',
      priority: 100,
    });
  const trackingIssue = getTrackingSummaries(item).find((entry) => entry.state === 'atRisk');
  if (trackingIssue)
    signals.push({
      id: 'tracking',
      label: `${trackingIssue.dimension.label} at risk`,
      severity: 'warning',
      priority: 85,
    });
  const checkpoint = getActiveCheckpoint(item);
  if (checkpoint && checkpoint.targetAt !== item.targetAt)
    signals.push({
      id: 'checkpoint',
      label: `${checkpoint.title} ${getCountdown(checkpoint.targetAt)}`,
      severity:
        checkpoint.status === 'atRisk' || checkpoint.status === 'blocked' ? 'warning' : 'neutral',
      priority: 60,
    });
  if (target?.varianceDays && target.varianceDays > 0)
    signals.push({
      id: 'variance',
      label: `Forecast ${formatVariance(target.varianceDays)}`,
      severity: 'warning',
      priority: 90,
    });
  if (!signals.length)
    signals.push({ id: 'status', label: item.currentState, severity: 'neutral', priority: 30 });
  return signals.sort((left, right) => right.priority - left.priority);
}

export function getEntityLabel(item: WorkItem, entityId: string) {
  if (entityId === item.id)
    return {
      title: item.targetAt ? `Target · ${formatDate(item.targetAt)}` : 'Target',
      status:
        item.attention === 'high' ||
        (item.targetAt && item.forecastAt && getVarianceDays(item.targetAt, item.forecastAt) > 0)
          ? 'atRisk'
          : 'current',
      kind: item.title,
    };
  const checkpoint = item.checkpoints.find((entry) => entry.id === entityId);
  if (checkpoint) return { title: checkpoint.title, status: checkpoint.status, kind: 'Checkpoint' };
  const requirement = item.requirements.find((entry) => entry.id === entityId);
  if (requirement)
    return { title: requirement.title, status: requirement.status, kind: 'Requirement' };
  const trackable = item.items.find((entry) => entry.id === entityId);
  if (trackable)
    return {
      title: trackable.title,
      status: trackable.status,
      kind: item.profile.categoryLabels[trackable.category] ?? 'Item',
    };
  return { title: entityId, status: 'unknown', kind: 'Item' };
}

export function getTrackingSubjects(item: WorkItem) {
  const idsWithState = new Set(item.trackingStates.map((state) => state.subjectId));
  const trackables = getActiveItems(item)
    .filter((entry) => idsWithState.has(entry.id))
    .map((entry) => ({ id: entry.id, label: entry.reference ?? entry.title, detail: entry.title }));
  const requirements = getActiveRequirements(item)
    .filter((entry) => idsWithState.has(entry.id))
    .map((entry) => ({ id: entry.id, label: entry.title, detail: entry.group }));
  return [...trackables, ...requirements];
}
