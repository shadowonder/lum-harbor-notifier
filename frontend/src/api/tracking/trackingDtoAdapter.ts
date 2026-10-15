import type { app } from '../../../wailsjs/go/models';
import {
  TrackingUnavailableError,
  type ExpectedWorkItemLifecycle,
  type MonitorExpectationKind,
  type MonitorKind,
  type MonitorPoint,
  type MonitorState,
  type MonitorSubjectKind,
  type TrackedItem,
  type TrackedItemLifecycle,
  type TrackingProgression,
  type TrackingStep,
  type TrackingStepState,
  type TrackingStepStateRecord,
  type TrackingWorkflow,
  type TrackingWorkflowDefinition,
  type WorkItemTracking,
} from './trackingProductApi.ts';

const stepStates: readonly TrackingStepState[] = [
  'NOT_STARTED',
  'IN_PROGRESS',
  'COMPLETED',
  'WAITING',
  'BLOCKED',
  'FAILED',
  'UNKNOWN',
  'SKIPPED',
];
const itemLifecycles: readonly TrackedItemLifecycle[] = ['ACTIVE', 'COMPLETED', 'CANCELLED'];
const monitorKinds: readonly MonitorKind[] = [
  'FINAL_TARGET',
  'CHECKPOINT',
  'DEADLINE',
  'REVIEW',
  'CUSTOM',
];
const monitorStates: readonly MonitorState[] = ['ACTIVE', 'SATISFIED', 'CANCELLED'];
const subjectKinds: readonly MonitorSubjectKind[] = ['WORK_ITEM', 'TRACKED_ITEM'];
const expectationKinds: readonly MonitorExpectationKind[] = [
  'WORK_ITEM_LIFECYCLE',
  'TRACKED_ITEM_STEP_COMPLETED',
];
const workItemLifecycles: readonly ExpectedWorkItemLifecycle[] = [
  'ACTIVE',
  'PAUSED',
  'COMPLETED',
  'CANCELLED',
];

function oneOf<T extends string>(values: readonly T[], value: string, field: string): T {
  if (!values.includes(value as T)) throw new Error(`Unsupported ${field}: ${value}`);
  return value as T;
}

function instant(value: string, field: string): string {
  if (!value || Number.isNaN(Date.parse(value))) throw new Error(`Invalid ${field}: ${value}`);
  return value;
}

function optionalInstant(value: string | null | undefined, field: string): string | null {
  return value == null ? null : instant(value, field);
}

export function mapStepState(dto: app.StepStateDTO): TrackingStepStateRecord {
  return {
    workflowStepId: dto.workflowStepId,
    state: oneOf(stepStates, dto.state, 'step state'),
    enteredAt: optionalInstant(dto.enteredAt, 'step enteredAt'),
    completedAt: optionalInstant(dto.completedAt, 'step completedAt'),
  };
}

export function mapProgression(
  dto: app.ProgressionDTO | null | undefined,
): TrackingProgression | null {
  if (!dto) return null;
  return {
    totalSteps: dto.totalSteps,
    completedCount: dto.completedCount,
    skippedCount: dto.skippedCount,
    passedCount: dto.passedCount,
    progressPercent: dto.progressPercent,
    complete: dto.complete,
    currentStepId: dto.currentStepId ?? null,
    currentStepKey: dto.currentStepKey ?? null,
    currentState: dto.currentState ? oneOf(stepStates, dto.currentState, 'current state') : null,
  };
}

export function mapTrackedItem(dto: app.TrackedItemDTO): TrackedItem {
  return {
    id: dto.id,
    workflowId: dto.workflowId,
    parentItemId: dto.parentItemId ?? null,
    title: dto.title,
    itemType: dto.itemType ?? null,
    lifecycleState: oneOf(itemLifecycles, dto.lifecycleState, 'tracked item lifecycle'),
    completedAt: optionalInstant(dto.completedAt, 'tracked item completedAt'),
    createdAt: instant(dto.createdAt, 'tracked item createdAt'),
    updatedAt: instant(dto.updatedAt, 'tracked item updatedAt'),
    stepStates: (dto.stepStates ?? []).map(mapStepState),
    progression: mapProgression(dto.progression),
  };
}

function mapStep(dto: app.WorkflowStepDTO): TrackingStep {
  return {
    id: dto.id,
    key: dto.key,
    title: dto.title,
    sequence: dto.sequence,
    terminal: dto.terminal,
  };
}

export function mapWorkflowDefinition(dto: app.WorkflowDefinitionDTO): TrackingWorkflowDefinition {
  return {
    id: dto.id,
    name: dto.name,
    version: dto.version,
    active: dto.active,
    createdAt: instant(dto.createdAt, 'workflow definition createdAt'),
    steps: (dto.steps ?? []).map(mapStep),
  };
}

export function mapWorkflow(dto: app.WorkflowDTO): TrackingWorkflow {
  const definition = mapWorkflowDefinition(dto.definition);
  return {
    id: dto.id,
    workItemId: dto.workItemId,
    workflowDefinitionId: dto.workflowDefinitionId,
    createdAt: instant(dto.createdAt, 'workflow createdAt'),
    currentStepId: dto.currentStep?.id || null,
    definition,
    name: definition.name,
    version: definition.version,
    steps: definition.steps,
  };
}

export function mapMonitorPoint(dto: app.MonitorPointDTO): MonitorPoint {
  return {
    id: dto.id,
    subjectKind: oneOf(subjectKinds, dto.subjectKind, 'monitor subject kind'),
    subjectId: dto.subjectId,
    kind: oneOf(monitorKinds, dto.kind, 'monitor kind'),
    title: dto.title,
    dueAt: instant(dto.dueAt, 'monitor dueAt'),
    timezone: dto.timezone,
    expectationKind: oneOf(expectationKinds, dto.expectationKind, 'monitor expectation kind'),
    expectedWorkItemLifecycle: dto.expectedWorkItemLifecycle
      ? oneOf(workItemLifecycles, dto.expectedWorkItemLifecycle, 'expected WorkItem lifecycle')
      : null,
    expectedStepId: dto.expectedStepId ?? null,
    state: oneOf(monitorStates, dto.state, 'monitor state'),
    satisfiedAt: optionalInstant(dto.satisfiedAt, 'monitor satisfiedAt'),
    createdAt: instant(dto.createdAt, 'monitor createdAt'),
    updatedAt: instant(dto.updatedAt, 'monitor updatedAt'),
  };
}

export function mapWorkItemTracking(dto: app.WorkItemTrackingDTO): WorkItemTracking {
  return {
    workItemId: dto.workItemId,
    trackedItems: (dto.trackedItems ?? []).map(mapTrackedItem),
    workflow: dto.workflow ? mapWorkflow(dto.workflow) : null,
    relations: (dto.relations ?? []).map((relation) => ({
      fromTrackedItemId: relation.fromTrackedItemId,
      toTrackedItemId: relation.toTrackedItemId,
      relationType: 'DEPENDS_ON' as const,
    })),
    monitorPoints: (dto.monitorPoints ?? []).map(mapMonitorPoint),
    history: (dto.history ?? []).map((event) => ({
      id: event.id,
      eventType: event.eventType,
      scope: event.scope,
      actor: event.actor,
      summary: event.summary,
      subjectEntityId: event.subjectEntityId ?? null,
      occurredAt: instant(event.occurredAt, 'history occurredAt'),
    })),
    attentionIssues: (dto.attentionIssues ?? []).map((issue) => ({
      id: issue.id,
      subjectTrackedItemId: issue.subjectTrackedItemId,
      monitorPointId: issue.monitorPointId,
      state: oneOf(['OPEN', 'RESOLVED'] as const, issue.state, 'attention state'),
      severity: oneOf(
        ['INFO', 'WARNING', 'CRITICAL'] as const,
        issue.severity,
        'attention severity',
      ),
      reason: issue.reason,
      recommendedAction: issue.recommendedAction ?? null,
      snoozedUntil: optionalInstant(issue.snoozedUntil, 'attention snoozedUntil'),
      resolvedAt: optionalInstant(issue.resolvedAt, 'attention resolvedAt'),
    })),
  };
}

/**
 * Wails rejects with a plain string. The backend reports an unavailable
 * WorkItem with a stable message; keep that distinction typed so the UI never
 * confuses it with "valid WorkItem, no tracking data".
 */
export function normalizeTrackingError(error: unknown): Error {
  const message = error instanceof Error ? error.message : String(error);
  if (message.includes('work item is not available')) return new TrackingUnavailableError();
  return error instanceof Error ? error : new Error(message);
}
