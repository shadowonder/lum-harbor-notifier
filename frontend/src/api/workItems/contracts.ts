import type {
  Attention,
  CheckpointStatus,
  RequirementStatus,
  TrackableItemStatus,
  TrackingStatus,
  WorkItemStatus,
  WorkItemLifecycleState,
} from '../../types/workItem';

export interface WorkItemMilestoneDto {
  id: string;
  label: string;
  occurredAt: string;
  kind: 'progress' | 'deployment' | 'submission' | 'review' | 'grading' | 'completion';
}

export interface WorkItemProfileDto {
  label: string;
  itemsLabel: string;
  trackingLabel: string;
  progressLabel: string;
  progressCategoryIds: string[];
  categoryLabels: Record<string, string>;
}

export interface CheckpointDto {
  id: string;
  title: string;
  targetAt: string;
  forecastAt?: string;
  status: CheckpointStatus;
}

export interface TrackableItemDto {
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

export interface RequirementDto {
  id: string;
  checkpointId?: string;
  title: string;
  group: string;
  required: boolean;
  status: RequirementStatus;
  evidenceRefs?: string[];
}

export interface RelationDto {
  id: string;
  sourceId: string;
  targetId: string;
  type: 'blocks' | 'requires' | 'dependsOn';
}

export interface TrackingDimensionDto {
  id: string;
  label: string;
  order: number;
}
export interface TrackingStateDto {
  dimensionId: string;
  subjectId: string;
  status: TrackingStatus;
  evidenceRefs?: string[];
}

export interface SourceReferenceDto {
  id: string;
  source: string;
  sourceType?: string;
  externalId?: string;
  label: string;
  summary?: string;
  hrefLabel?: string;
  url?: string;
  lastUpdated?: string;
  updatedAt?: string;
}

export interface EvidenceReferenceDto {
  id: string;
  label: string;
  sourceRefId?: string;
  capturedAt?: string;
}

export type ProgressStatusDto =
  | 'completed'
  | 'in_progress'
  | 'queued'
  | 'waiting'
  | 'blocked'
  | 'failed'
  | 'unknown'
  | 'deferred'
  | 'target';

export type ProgressNodeTypeDto = 'action' | 'requirement' | 'issue' | 'checkpoint';

export type ProgressNavigationDto =
  | { destination: 'details'; entityId: string }
  | { destination: 'activity'; activityId: string }
  | { destination: 'external'; url: string };

export interface ProgressStageDto {
  id: string;
  label: string;
  order: number;
  date?: string;
  target?: boolean;
}

export interface ProgressStreamDto {
  id: string;
  label: string;
  kind: 'main' | 'branch';
  summary?: string;
  priority: number;
  requiredBeforeTarget: boolean;
  sourceHint?: string;
}

export interface ProgressNodeDto {
  id: string;
  streamId: string;
  stageId: string;
  status: ProgressStatusDto;
  nodeType: ProgressNodeTypeDto;
  title?: string;
  reason?: string;
  count?: number;
  total?: number;
  shortLabel?: string;
  sourceEntityIds: string[];
  attention: boolean;
  navigation?: ProgressNavigationDto;
  metadata?: Record<string, string>;
}

export interface ProgressLinkDto {
  id: string;
  fromNodeId: string;
  toNodeId: string;
  kind: 'progress' | 'branch' | 'merge';
  open?: boolean;
}

export interface ProgressProjectionDto {
  stages: ProgressStageDto[];
  streams: ProgressStreamDto[];
  nodes: ProgressNodeDto[];
  links: ProgressLinkDto[];
  targetNodeId: string;
}

export interface ReadinessSummaryDto {
  scopeLabel: string;
  ready: number;
  required: number;
  blocked: number;
  waiting: number;
  unknown: number;
}

export interface AttentionSummaryDto {
  unresolvedBranches: number;
  failed: number;
  blocked: number;
  waiting: number;
  unknown: number;
}

export interface WorkItemProgressSummaryDto {
  completed: number;
  total: number;
  label: string;
}

export interface WorkItemSummaryDto {
  id: string;
  title: string;
  labels: string[];
  attention: Attention;
  currentState: string;
  lifecycleState: WorkItemLifecycleState;
  targetAt?: string;
  forecastAt?: string;
  startedAt?: string;
  completedAt?: string;
  finalOutcome?: string;
  milestones: WorkItemMilestoneDto[];
  nextAction?: string;
  progress: WorkItemProgressSummaryDto;
  readiness: ReadinessSummaryDto;
  attentionSummary: AttentionSummaryDto;
  progressProjection: ProgressProjectionDto;
}

export interface WorkItemDetailDto {
  id: string;
  title: string;
  labels: string[];
  profile: WorkItemProfileDto;
  status: WorkItemStatus;
  currentState: string;
  attention: Attention;
  targetAt?: string;
  forecastAt?: string;
  startedAt?: string;
  completedAt?: string;
  finalOutcome?: string;
  milestones: WorkItemMilestoneDto[];
  updatedAt: string;
  checkpoints: CheckpointDto[];
  activeCheckpointId?: string;
  items: TrackableItemDto[];
  requirements: RequirementDto[];
  relations: RelationDto[];
  trackingDimensions: TrackingDimensionDto[];
  trackingStates: TrackingStateDto[];
  nextActions: string[];
  relatedTodoCount: number;
  relatedEventCount: number;
  sources: SourceReferenceDto[];
  evidence: EvidenceReferenceDto[];
  readiness: ReadinessSummaryDto;
  attentionSummary: AttentionSummaryDto;
  progressProjection: ProgressProjectionDto;
}

export interface WorkItemListResponseDto {
  asOf: string;
  items: WorkItemSummaryDto[];
  total: number;
}

export interface WorkItemListQueryDto {
  lifecycleState?: WorkItemLifecycleState;
  limit?: number;
}

export interface WorkItemActivityDto {
  id: string;
  label: string;
  occurredAt: string;
  entityIds?: string[];
}

export interface WorkItemApi {
  listWorkItems(query?: WorkItemListQueryDto): Promise<WorkItemListResponseDto>;
  getWorkItem(id: string): Promise<WorkItemDetailDto | null>;
  getWorkItemActivity(id: string): Promise<WorkItemActivityDto[]>;
}
