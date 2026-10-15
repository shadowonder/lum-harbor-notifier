export type Attention = 'high' | 'attention' | 'normal' | 'low';
export type WorkItemStatus = 'active' | 'planned' | 'paused' | 'complete';
export type WorkItemLifecycleState = 'active' | 'completed';

export interface WorkItemMilestone {
  id: string;
  label: string;
  occurredAt: string;
  kind: 'progress' | 'deployment' | 'submission' | 'review' | 'grading' | 'completion';
}

export interface WorkItemProfile {
  label: string;
  itemsLabel: string;
  trackingLabel: string;
  progressLabel: string;
  progressCategoryIds: string[];
  categoryLabels: Record<string, string>;
}

export type CheckpointStatus =
  'completed' | 'current' | 'upcoming' | 'atRisk' | 'blocked' | 'unknown';

export interface Checkpoint {
  id: string;
  title: string;
  targetAt: string;
  forecastAt?: string;
  status: CheckpointStatus;
}

export type TrackableItemStatus =
  | 'queued'
  | 'inProgress'
  | 'complete'
  | 'waiting'
  | 'blocked'
  | 'unknown'
  | 'deferred';

export interface TrackableItem {
  id: string;
  reference?: string;
  title: string;
  category: string;
  status: TrackableItemStatus;
  checkpointIds?: string[];
  critical?: boolean;
  waitingOn?: string;
  sourceRefId?: string;
  updatedAt?: string;
  evidenceRefs?: string[];
}

export type RequirementStatus = 'ready' | 'blocked' | 'missing' | 'unknown';

export interface Requirement {
  id: string;
  checkpointId?: string;
  title: string;
  group: string;
  required: boolean;
  status: RequirementStatus;
  evidenceRefs?: string[];
}

export interface Relation {
  id: string;
  sourceId: string;
  targetId: string;
  type: 'blocks' | 'requires' | 'dependsOn';
}

export interface TrackingDimension {
  id: string;
  label: string;
  order: number;
}

export type TrackingStatus =
  | 'queued'
  | 'inProgress'
  | 'complete'
  | 'verified'
  | 'waiting'
  | 'behind'
  | 'failed'
  | 'blocked'
  | 'unknown';

export interface SourceReference {
  id: string;
  source: string;
  sourceType?: string;
  externalId?: string;
  label: string;
  summary?: string;
  hrefLabel?: string;
  url?: string;
  updatedAt?: string;
}

export interface EvidenceReference {
  id: string;
  label: string;
  sourceRefId?: string;
  capturedAt?: string;
}

export interface TrackingState {
  dimensionId: string;
  subjectId: string;
  status: TrackingStatus;
  evidenceRefs?: string[];
}

export interface WorkItemActivity {
  id: string;
  label: string;
  occurredAt: string;
  entityIds?: string[];
}

export interface WorkItem {
  id: string;
  title: string;
  labels: string[];
  profile: WorkItemProfile;
  status: WorkItemStatus;
  currentState: string;
  attention: Attention;
  targetAt?: string;
  forecastAt?: string;
  startedAt?: string;
  completedAt?: string;
  finalOutcome?: string;
  milestones?: WorkItemMilestone[];
  updatedAt: string;
  checkpoints: Checkpoint[];
  activeCheckpointId?: string;
  items: TrackableItem[];
  requirements: Requirement[];
  relations: Relation[];
  trackingDimensions: TrackingDimension[];
  trackingStates: TrackingState[];
  sources?: SourceReference[];
  evidence?: EvidenceReference[];
  nextActions: string[];
  relatedTodoCount: number;
  relatedEventCount: number;
  activity: WorkItemActivity[];
}
