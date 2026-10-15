import type { ProductWorkItem } from '../../../api/workItems/productApi.ts';

export type WorkflowSectionId = 'build' | 'verify' | 'release';

export type WorkflowStageId = 'queue' | 'dev' | 'test' | 'stage' | 'adoption' | 'production';

export interface WorkflowSection {
  id: WorkflowSectionId;
  label: string;
  stageIds: WorkflowStageId[];
}

export interface WorkflowStage {
  id: WorkflowStageId;
  label: string;
  sectionId: WorkflowSectionId;
}

export const WORKFLOW_SECTIONS: readonly WorkflowSection[] = [
  { id: 'build', label: 'BUILD', stageIds: ['queue', 'dev'] },
  { id: 'verify', label: 'VERIFY', stageIds: ['test', 'stage'] },
  { id: 'release', label: 'RELEASE', stageIds: ['adoption', 'production'] },
];

export const WORKFLOW_STAGES: readonly WorkflowStage[] = [
  { id: 'queue', label: 'Queue', sectionId: 'build' },
  { id: 'dev', label: 'Dev', sectionId: 'build' },
  { id: 'test', label: 'Test', sectionId: 'verify' },
  { id: 'stage', label: 'Stage', sectionId: 'verify' },
  { id: 'adoption', label: 'Adoption', sectionId: 'release' },
  { id: 'production', label: 'Production', sectionId: 'release' },
];

export type WorkflowItemType = 'story' | 'task' | 'defect' | 'gate';
export type WorkflowHealth = 'green' | 'yellow' | 'red' | 'gray';

export interface WorkflowHistoryEvent {
  id: string;
  timestamp: string;
  title: string;
  description: string;
  type: 'transition' | 'health_change' | 'alert' | 'dependency' | 'milestone';
  actor?: string;
  nodeId?: string;
  reason?: string;
  source?: string;
  evidence?: string;
  affectedEntity?: string;
}

export interface WorkflowSource {
  label: string;
  kind: string;
  detail?: string;
  reference?: string;
  why?: string;
  observedAt?: string;
  linkedBy?: string;
}

export interface WorkflowTiming {
  label: string;
  targetAt: string;
  timingLabel: string;
}

export interface WorkflowRelatedTodo {
  id: string;
  title: string;
  status: 'OPEN' | 'BLOCKED' | 'COMPLETED';
  timing?: string;
}

export interface WorkflowRelatedNotification {
  id: string;
  title: string;
  severity: 'INFO' | 'ATTENTION' | 'CRITICAL';
  status: 'OPEN' | 'ACKNOWLEDGED' | 'RESOLVED';
  timing?: string;
}

export interface WorkflowNode {
  id: string;
  title: string;
  itemType: WorkflowItemType;
  stageId: WorkflowStageId;
  health: WorkflowHealth;
  targetStageId?: WorkflowStageId | null;
  lane?: number; // -1 (top lane), +1 (bottom lane), -2, +2
  dependencies?: string[]; // node IDs that this node depends on (must precede it)
  summary?: string;
  signal?: string;
  assignee?: string;
  isResolved?: boolean;
  statusReason?: string;
  latestActivity?: string;
  deadline?: string;
  targetDate?: string;
  description?: string;
  stateLabel?: string;
  source?: WorkflowSource;
  evidence?: string;
  nextMilestone?: WorkflowTiming;
  relatedTodos?: WorkflowRelatedTodo[];
  relatedNotifications?: WorkflowRelatedNotification[];
  linkedItemIds?: string[];
}

export interface WorkflowDependencyEdge {
  from: string; // prerequisite / dependency source
  to: string; // dependent target
}

export interface WorkflowTrunk {
  currentStageId: WorkflowStageId;
  health: WorkflowHealth;
  progressPercent: number; // 0..100
  label: string;
}

export interface WorkflowModel {
  workItemId: string;
  title: string;
  description?: string | null;
  trunk: WorkflowTrunk;
  nodes: WorkflowNode[];
  edges: WorkflowDependencyEdge[];
  aggregateHealth: WorkflowHealth;
  hasAuthoritativeHealth: boolean;
  healthLabel: string;
  hasTargetMarker: boolean;
  targetStageId?: WorkflowStageId | null;
  targetDate?: string | null;
  targetLabel: string;
  targetTimingLabel?: string;
  statusReason?: string;
  origin?: WorkflowSource;
  nextMilestone?: WorkflowTiming;
  historyEvents?: WorkflowHistoryEvent[];
  itemHistories?: Record<string, WorkflowHistoryEvent[]>;
  isRealData: boolean;
  workflowDataSource?: 'authoritative' | 'prototype' | 'unavailable';
}

export function hasWorkflowData(model: WorkflowModel): boolean {
  return (
    (model.workflowDataSource ?? (model.isRealData ? 'authoritative' : 'prototype')) !==
    'unavailable'
  );
}

export function getRecentWorkflowHistory(model: WorkflowModel, limit = 6) {
  return (model.historyEvents ?? []).slice(0, Math.max(0, limit));
}

export function getItemTypeIcon(type: WorkflowItemType): string {
  switch (type) {
    case 'story':
      return '○';
    case 'task':
      return '□';
    case 'defect':
      return '△';
    case 'gate':
      return '◇';
  }
}

/**
 * Builds an authoritative WorkflowModel from real backend WorkItem data.
 * Strictly avoids fabricating child items, fake dependencies, or unconfigured deadlines.
 */
export function buildWorkflowModelFromWorkItem(item: ProductWorkItem): WorkflowModel {
  return {
    workItemId: item.id,
    title: item.title,
    description: item.description,
    trunk: {
      currentStageId: 'queue',
      health: 'gray', // Backend lacks health telemetry; gray = neutral/unknown
      progressPercent: 0,
      label: 'Workflow data unavailable',
    },
    nodes: [], // Authoritative backend currently has no child items
    edges: [],
    aggregateHealth: 'gray',
    hasAuthoritativeHealth: false,
    healthLabel: 'HEALTH UNKNOWN',
    hasTargetMarker: false,
    targetStageId: null,
    targetDate: null,
    targetLabel: 'Target data unavailable',
    workflowDataSource: 'unavailable',
    isRealData: true,
  };
}

/**
 * Creates a rich workflow fixture with multiple branch items, node shapes,
 * health colors, target markers, and dependencies.
 *
 * ONLY for component tests, visual verification, and dev previews.
 * NEVER exposed as real user data in production.
 */
export function createDemoWorkflowFixture(workItemId = 'demo-item-1'): WorkflowModel {
  const nodes: WorkflowNode[] = [
    {
      id: `${workItemId}-n1`,
      title: 'OAuth integration',
      summary: 'Authentication provider integration',
      itemType: 'story',
      stageId: 'dev',
      health: 'green',
      lane: -1,
      targetStageId: 'test',
      dependencies: [],
      isResolved: false,
    },
    {
      id: `${workItemId}-n2`,
      title: 'Token refresh daemon',
      summary: 'Background token rotation worker',
      itemType: 'task',
      stageId: 'queue',
      health: 'gray',
      lane: 1,
      dependencies: [],
      isResolved: false,
    },
    {
      id: `${workItemId}-n3`,
      title: 'Session expiry flaw',
      summary: 'Regression in JWT expiry validation',
      itemType: 'defect',
      stageId: 'dev',
      health: 'red',
      lane: 1,
      dependencies: [`${workItemId}-n1`],
      isResolved: false,
    },
    {
      id: `${workItemId}-n4`,
      title: 'Regression suite verify',
      summary: 'Automated end-to-end regression tests',
      itemType: 'task',
      stageId: 'test',
      health: 'yellow',
      lane: -1,
      targetStageId: 'stage',
      dependencies: [`${workItemId}-n1`],
      isResolved: false,
    },
    {
      id: `${workItemId}-n5`,
      title: 'Security approval gate',
      summary: 'Compliance audit sign-off',
      itemType: 'gate',
      stageId: 'stage',
      health: 'green',
      lane: -1,
      dependencies: [`${workItemId}-n3`, `${workItemId}-n4`],
      isResolved: false,
    },
  ];

  const edges: WorkflowDependencyEdge[] = [
    { from: `${workItemId}-n1`, to: `${workItemId}-n3` },
    { from: `${workItemId}-n1`, to: `${workItemId}-n4` },
    { from: `${workItemId}-n3`, to: `${workItemId}-n5` },
    { from: `${workItemId}-n4`, to: `${workItemId}-n5` },
  ];

  return {
    workItemId,
    title: 'Production Authentication Gateway',
    description: 'Enterprise OAuth2 and multi-tenant authentication service.',
    trunk: {
      currentStageId: 'test',
      health: 'yellow',
      progressPercent: 50,
      label: 'At Risk · Pending Test',
    },
    nodes,
    edges,
    aggregateHealth: 'yellow',
    hasAuthoritativeHealth: true,
    healthLabel: 'AT RISK',
    hasTargetMarker: true,
    targetStageId: 'stage',
    targetDate: '2026-09-30',
    targetLabel: 'Target: Sep 30, 2026 (Stage)',
    isRealData: false,
  };
}
