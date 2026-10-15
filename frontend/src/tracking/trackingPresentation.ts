import type {
  CreateMonitorInput,
  ExpectedWorkItemLifecycle,
  MonitorKind,
  MonitorPoint,
  TrackedItem,
  TrackingStepState,
  TrackingWorkflow,
  WorkItemTracking,
} from '../api/tracking/trackingProductApi.ts';

/**
 * Pure presentation helpers over the authoritative Tracking projection.
 * Workflow progression is NEVER computed here: progress, current step and
 * completion come from the backend `progression`. These helpers only choose
 * which commands to offer and how to word facts.
 */

export interface TrackedItemRowModel {
  item: TrackedItem;
  depth: number;
  parentTitle: string | null;
  workflow: TrackingWorkflow | null;
}

/**
 * Keeps the backend's deterministic order (parents precede children) and only
 * adds display depth from the real parent identity.
 */
export function buildTrackedItemRows(tracking: WorkItemTracking): TrackedItemRowModel[] {
  const byId = new Map(tracking.trackedItems.map((item) => [item.id, item]));
  const depthOf = (item: TrackedItem): number => {
    let depth = 0;
    let cursor = item.parentItemId ? byId.get(item.parentItemId) : undefined;
    const seen = new Set<string>([item.id]);
    while (cursor && !seen.has(cursor.id)) {
      seen.add(cursor.id);
      depth += 1;
      cursor = cursor.parentItemId ? byId.get(cursor.parentItemId) : undefined;
    }
    return depth;
  };
  return tracking.trackedItems.map((item) => ({
    item,
    depth: depthOf(item),
    parentTitle: item.parentItemId ? (byId.get(item.parentItemId)?.title ?? null) : null,
    workflow: tracking.workflow,
  }));
}

export interface StepCommand {
  label: string;
  target: TrackingStepState;
  primary: boolean;
}

export interface StepCommandPlan {
  trackedItemId: string;
  workflowStepId: string;
  stepTitle: string;
  currentState: TrackingStepState;
  commands: StepCommand[];
}

/**
 * Behavior commands for the backend-reported current step. Returns null when
 * the item has no workflow, the workflow is complete, or the backend reports no
 * current step (so no command with an invented step ID can ever be sent).
 */
export function planStepCommands(
  item: TrackedItem,
  workflow: TrackingWorkflow | null,
): StepCommandPlan | null {
  const progression = item.progression;
  if (!workflow || !progression || progression.complete) return null;
  if (!progression.currentStepId || !progression.currentState) return null;
  const step = workflow.steps.find((s) => s.id === progression.currentStepId);
  if (!step) return null;
  const state = progression.currentState;
  let commands: StepCommand[];
  switch (state) {
    case 'NOT_STARTED':
      commands = [{ label: 'Start', target: 'IN_PROGRESS', primary: true }];
      break;
    case 'IN_PROGRESS':
      commands = [
        { label: 'Complete', target: 'COMPLETED', primary: true },
        { label: 'Waiting', target: 'WAITING', primary: false },
        { label: 'Blocked', target: 'BLOCKED', primary: false },
      ];
      break;
    case 'WAITING':
    case 'BLOCKED':
    case 'FAILED':
    case 'UNKNOWN':
      commands = [
        { label: 'Resume', target: 'IN_PROGRESS', primary: true },
        { label: 'Complete', target: 'COMPLETED', primary: false },
      ];
      break;
    default:
      return null;
  }
  return {
    trackedItemId: item.id,
    workflowStepId: step.id,
    stepTitle: step.title,
    currentState: state,
    commands,
  };
}

export function stepStateLabel(state: TrackingStepState): string {
  switch (state) {
    case 'NOT_STARTED':
      return 'Not started';
    case 'IN_PROGRESS':
      return 'In progress';
    case 'COMPLETED':
      return 'Completed';
    case 'WAITING':
      return 'Waiting';
    case 'BLOCKED':
      return 'Blocked';
    case 'FAILED':
      return 'Failed';
    case 'UNKNOWN':
      return 'Unknown';
    case 'SKIPPED':
      return 'Skipped';
  }
}

export function monitorKindLabel(kind: MonitorKind): string {
  switch (kind) {
    case 'FINAL_TARGET':
      return 'Final target';
    case 'CHECKPOINT':
      return 'Checkpoint';
    case 'DEADLINE':
      return 'Deadline';
    case 'REVIEW':
      return 'Review';
    case 'CUSTOM':
      return 'Custom';
  }
}

const lifecycleWords: Record<ExpectedWorkItemLifecycle, string> = {
  ACTIVE: 'active',
  PAUSED: 'paused',
  COMPLETED: 'completed',
  CANCELLED: 'cancelled',
};

/** Human meaning of a MonitorPoint's typed expectation (no internal field names). */
export function describeExpectation(monitor: MonitorPoint, tracking: WorkItemTracking): string {
  if (monitor.expectationKind === 'WORK_ITEM_LIFECYCLE' && monitor.expectedWorkItemLifecycle) {
    return `Work Item expected to be ${lifecycleWords[monitor.expectedWorkItemLifecycle]}`;
  }
  const item = tracking.trackedItems.find((candidate) => candidate.id === monitor.subjectId);
  const workflow = tracking.workflow;
  const step = workflow?.steps.find((s) => s.id === monitor.expectedStepId);
  if (item && step) return `“${step.title}” expected to be completed for ${item.title}`;
  return 'Step expected to be completed';
}

const DAY_MS = 24 * 60 * 60 * 1000;

/**
 * Pure display wording from due time + now + authoritative state. It never
 * produces AT RISK / health / attention and is never written back.
 */
export function describeTiming(monitor: Pick<MonitorPoint, 'state' | 'dueAt'>, now: Date): string {
  if (monitor.state === 'SATISFIED') return 'Satisfied';
  if (monitor.state === 'CANCELLED') return 'Cancelled';
  const diff = Date.parse(monitor.dueAt) - now.getTime();
  if (diff < 0) {
    const days = Math.max(1, Math.floor(-diff / DAY_MS));
    return diff > -DAY_MS ? 'Overdue today' : `Overdue by ${days} ${days === 1 ? 'day' : 'days'}`;
  }
  if (diff < DAY_MS) return 'Due within a day';
  const days = Math.floor(diff / DAY_MS);
  return `Due in ${days} ${days === 1 ? 'day' : 'days'}`;
}

/** The Work Item's current final target: its first ACTIVE WorkItem-subject FINAL_TARGET. */
export function findActiveFinalTarget(tracking: WorkItemTracking): MonitorPoint | null {
  return (
    tracking.monitorPoints.find(
      (m) => m.kind === 'FINAL_TARGET' && m.state === 'ACTIVE' && m.subjectKind === 'WORK_ITEM',
    ) ?? null
  );
}

/** Backend order is by due time; the next expectation is the first ACTIVE one. */
export function findNextExpectation(tracking: WorkItemTracking): MonitorPoint | null {
  return tracking.monitorPoints.find((m) => m.state === 'ACTIVE') ?? null;
}

export interface MonitorFormValues {
  kind: MonitorKind;
  title: string;
  /** `datetime-local` value: YYYY-MM-DDTHH:mm, interpreted in `timezone`. */
  dueLocal: string;
  expectedLifecycle: ExpectedWorkItemLifecycle;
}

/**
 * Converts the product form into the typed backend command. `zoneOffsetMinutes`
 * is the offset (minutes east of UTC) of `timezone` at that instant, supplied by
 * the caller so this stays pure and testable.
 */
export function buildMonitorInput(
  workItemId: string,
  values: MonitorFormValues,
  timezone: string,
  zoneOffsetMinutes: number,
): CreateMonitorInput {
  const match = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(values.dueLocal.trim());
  if (!match) throw new Error('Choose a valid due date and time.');
  const [, y, mo, d, h, mi] = match;
  const utcMs =
    Date.UTC(Number(y), Number(mo) - 1, Number(d), Number(h), Number(mi)) -
    zoneOffsetMinutes * 60 * 1000;
  const title = values.title.trim();
  if (!title) throw new Error('A target title is required.');
  return {
    workItemId,
    kind: values.kind,
    title,
    dueAt: new Date(utcMs).toISOString(),
    timezone,
    expectedWorkItemLifecycle: values.expectedLifecycle,
  };
}

/**
 * The calendar date of a MonitorPoint instant in the MonitorPoint's own
 * timezone (YYYY-MM-DD). Every surface that shows the same target must agree
 * with this day; the viewer's browser timezone never decides it.
 */
export function monitorCalendarDate(dueAt: string, timezone: string): string {
  const instant = new Date(dueAt);
  const format = (timeZone?: string) => {
    const parts = new Intl.DateTimeFormat('en-CA', {
      timeZone,
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
    }).formatToParts(instant);
    const get = (type: string) => parts.find((part) => part.type === type)?.value ?? '';
    return `${get('year')}-${get('month')}-${get('day')}`;
  };
  try {
    return format(timezone || undefined);
  } catch {
    return format(undefined);
  }
}

/** Medium date (e.g. "Oct 11, 2026") of the target in its own timezone. */
export function formatMonitorDate(dueAt: string, timezone: string): string {
  try {
    return new Intl.DateTimeFormat('en-US', { dateStyle: 'medium', timeZone: timezone }).format(
      new Date(dueAt),
    );
  } catch {
    return monitorCalendarDate(dueAt, timezone);
  }
}

/** Date, time and timezone of the target, in its own timezone. */
export function formatMonitorDue(monitor: Pick<MonitorPoint, 'dueAt' | 'timezone'>): string {
  try {
    return `${new Intl.DateTimeFormat(undefined, {
      dateStyle: 'medium',
      timeStyle: 'short',
      timeZone: monitor.timezone,
    }).format(new Date(monitor.dueAt))} (${monitor.timezone})`;
  } catch {
    return monitor.dueAt;
  }
}
