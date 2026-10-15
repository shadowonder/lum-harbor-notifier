/**
 * Frontend product contract for M10 Tracking. Semantic, transport-free models:
 * generated Wails types stop at trackingDtoAdapter.ts. Progression is
 * backend-derived and must never be recomputed in TypeScript.
 */
export type TrackingStepState =
  | 'NOT_STARTED'
  | 'IN_PROGRESS'
  | 'COMPLETED'
  | 'WAITING'
  | 'BLOCKED'
  | 'FAILED'
  | 'UNKNOWN'
  | 'SKIPPED';

export type TrackedItemLifecycle = 'ACTIVE' | 'COMPLETED' | 'CANCELLED';
export type MonitorKind = 'FINAL_TARGET' | 'CHECKPOINT' | 'DEADLINE' | 'REVIEW' | 'CUSTOM';
export type MonitorState = 'ACTIVE' | 'SATISFIED' | 'CANCELLED';
export type MonitorSubjectKind = 'WORK_ITEM' | 'TRACKED_ITEM';
export type MonitorExpectationKind = 'WORK_ITEM_LIFECYCLE' | 'TRACKED_ITEM_STEP_COMPLETED';
export type ExpectedWorkItemLifecycle = 'ACTIVE' | 'PAUSED' | 'COMPLETED' | 'CANCELLED';

export interface TrackingStep {
  id: string;
  key: string;
  title: string;
  sequence: number;
  terminal: boolean;
}

export interface TrackingWorkflowDefinition {
  id: string;
  name: string;
  version: number;
  active: boolean;
  createdAt: string;
  steps: TrackingStep[];
}

export interface TrackingWorkflow {
  id: string;
  workItemId: string;
  workflowDefinitionId: string;
  createdAt: string;
  /** The Workflow's own authoritative current step, independent of TrackedItem progression. */
  currentStepId: string | null;
  definition: TrackingWorkflowDefinition;
  name: string;
  version: number;
  steps: TrackingStep[];
}

export interface TrackingStepStateRecord {
  workflowStepId: string;
  state: TrackingStepState;
  enteredAt: string | null;
  completedAt: string | null;
}

export interface TrackingProgression {
  totalSteps: number;
  completedCount: number;
  skippedCount: number;
  passedCount: number;
  progressPercent: number;
  complete: boolean;
  currentStepId: string | null;
  currentStepKey: string | null;
  currentState: TrackingStepState | null;
}

export interface TrackedItem {
  id: string;
  workflowId: string;
  parentItemId: string | null;
  title: string;
  itemType: string | null;
  lifecycleState: TrackedItemLifecycle;
  completedAt: string | null;
  createdAt: string;
  updatedAt: string;
  stepStates: TrackingStepStateRecord[];
  progression: TrackingProgression | null;
}

export interface MonitorPoint {
  id: string;
  subjectKind: MonitorSubjectKind;
  subjectId: string;
  kind: MonitorKind;
  title: string;
  dueAt: string;
  timezone: string;
  expectationKind: MonitorExpectationKind;
  expectedWorkItemLifecycle: ExpectedWorkItemLifecycle | null;
  expectedStepId: string | null;
  state: MonitorState;
  satisfiedAt: string | null;
  createdAt: string;
  updatedAt: string;
}

export interface TrackedItemRelation {
  fromTrackedItemId: string;
  toTrackedItemId: string;
  relationType: 'DEPENDS_ON';
}

export interface TrackingHistoryEvent {
  id: string;
  eventType: string;
  scope: string;
  actor: string;
  summary: string;
  subjectEntityId: string | null;
  occurredAt: string;
}

export interface TrackedItemAttention {
  id: string;
  subjectTrackedItemId: string;
  monitorPointId: string;
  state: 'OPEN' | 'RESOLVED';
  severity: 'INFO' | 'WARNING' | 'CRITICAL';
  reason: string;
  recommendedAction: string | null;
  snoozedUntil: string | null;
  resolvedAt: string | null;
}

export interface WorkItemTracking {
  workItemId: string;
  trackedItems: TrackedItem[];
  workflow: TrackingWorkflow | null;
  relations?: TrackedItemRelation[];
  monitorPoints: MonitorPoint[];
  history?: TrackingHistoryEvent[];
  attentionIssues?: TrackedItemAttention[];
}

export interface CreateWorkflowInput {
  name: string;
  version: number;
  steps: Array<{ key: string; title: string; terminal: boolean }>;
}

export interface CreateWorkflowForWorkItemInput {
  workItemId: string;
  workflowDefinitionId: string;
}

export interface CreateTrackedItemInput {
  workItemId: string;
  title: string;
  itemType?: string;
  parentItemId?: string;
  workflowId?: string;
}

export interface SetStepStateInput {
  trackedItemId: string;
  workflowStepId: string;
  state: TrackingStepState;
}

export interface CreateMonitorInput {
  workItemId: string;
  kind: MonitorKind;
  title: string;
  /** RFC 3339 instant. */
  dueAt: string;
  /** IANA timezone name the due time was authored in. */
  timezone: string;
  expectedWorkItemLifecycle: ExpectedWorkItemLifecycle;
}

export interface SetMonitorStateInput {
  id: string;
  target: 'SATISFIED' | 'CANCELLED';
}

/** The WorkItem does not exist or is archived (distinct from "no tracking data"). */
export class TrackingUnavailableError extends Error {
  constructor(message = 'The Work Item is not available.') {
    super(message);
    this.name = 'TrackingUnavailableError';
  }
}

export interface ProductTrackingApi {
  getWorkItemTracking(workItemId: string): Promise<WorkItemTracking>;
  listActiveWorkflows(): Promise<TrackingWorkflowDefinition[]>;
  createWorkflow(input: CreateWorkflowInput): Promise<TrackingWorkflowDefinition>;
  createWorkflowForWorkItem?(input: CreateWorkflowForWorkItemInput): Promise<TrackingWorkflow>;
  createTrackedItem(input: CreateTrackedItemInput): Promise<TrackedItem>;
  setStepState(input: SetStepStateInput): Promise<TrackingStepStateRecord>;
  createMonitor(input: CreateMonitorInput): Promise<MonitorPoint>;
  setMonitorState(input: SetMonitorStateInput): Promise<MonitorPoint>;
}
