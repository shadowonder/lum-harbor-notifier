import type { WorkflowModel, WorkflowNode, WorkflowStageId } from '../workflow/workflowTypes.ts';
import { getItemTypeIcon, getItemTypeLabel } from '../workflow/workflowLayout.ts';

export type ItemExplorerRiskFilter = 'all' | 'at-risk' | 'blocked';
export type ItemExplorerStageFilter = 'all' | WorkflowStageId;

const STAGE_SHORT_LABELS: Record<WorkflowStageId, string> = {
  queue: 'QUEUE',
  dev: 'DEV',
  test: 'TEST',
  stage: 'STAGE',
  adoption: 'ADOPT',
  production: 'PROD',
};

const HEALTH_ACCESSIBLE_LABELS = {
  green: 'On Track',
  yellow: 'At Risk',
  red: 'Blocked',
  gray: 'Unknown',
} as const;

export function getItemStageShortLabel(stageId: WorkflowStageId) {
  return STAGE_SHORT_LABELS[stageId];
}

export function getItemSignal(node: WorkflowNode, model: WorkflowModel): string {
  const timing = node.nextMilestone?.timingLabel ?? '';
  if (timing.toLowerCase().includes('overdue')) {
    return timing.replace(/by\s+/i, '').replace(/\s+days?/i, 'd');
  }
  const blocks = model.edges.filter((edge) => edge.from === node.id).length;
  if (node.health === 'red' && blocks > 0) return `Blocks ${blocks} item${blocks === 1 ? '' : 's'}`;
  const alerts =
    node.relatedNotifications?.filter(
      (item) => item.status === 'OPEN' && item.severity === 'CRITICAL',
    ).length ?? 0;
  if (alerts > 0) return `${alerts} alert${alerts === 1 ? '' : 's'}`;
  if (node.signal) return node.signal;
  const issues =
    node.relatedNotifications?.filter(
      (item) => item.status === 'OPEN' && item.severity === 'ATTENTION',
    ).length ?? 0;
  if (issues > 0) return `${issues} issue${issues === 1 ? '' : 's'}`;
  const waiting = model.edges.filter((edge) => edge.to === node.id).length;
  if (waiting > 0 && !node.isResolved) {
    const dependencyId = model.edges.find((edge) => edge.to === node.id)?.from;
    const dependency = model.nodes.find((item) => item.id === dependencyId);
    const shortTitle = dependency?.title
      .replace('GraphQL Payment API', 'GraphQL')
      .replace('Defect #142 Token Leak', 'Defect #142');
    return shortTitle
      ? `Waiting on ${shortTitle}`
      : `Waiting on ${waiting} item${waiting === 1 ? '' : 's'}`;
  }
  if (node.health === 'red') return 'Blocked';
  if (node.isResolved) return 'Complete';
  return 'Ready';
}

export function getItemListRowPresentation(node: WorkflowNode, model: WorkflowModel) {
  const stage = getItemStageShortLabel(node.stageId);
  const signal = getItemSignal(node, model);
  const type = getItemTypeLabel(node.itemType);
  const health = HEALTH_ACCESSIBLE_LABELS[node.health];
  return {
    title: node.title,
    shapeSymbol: getItemTypeIcon(node.itemType),
    shapeHealth: node.health,
    stage,
    signal,
    secondaryText: `${stage} · ${signal}`,
    tooltip: `${type} · ${health}`,
    accessibilityLabel: `${node.title}. ${type}. ${health}. ${stage}. ${signal}.`,
  };
}

export function filterItemExplorerNodes(
  model: WorkflowModel,
  query: string,
  risk: ItemExplorerRiskFilter,
  stage: ItemExplorerStageFilter,
): WorkflowNode[] {
  const normalizedQuery = query.trim().toLowerCase();
  return model.nodes.filter((node) => {
    if (risk === 'at-risk' && node.health !== 'yellow') return false;
    if (risk === 'blocked' && node.health !== 'red') return false;
    if (stage !== 'all' && node.stageId !== stage) return false;
    if (!normalizedQuery) return true;
    return [node.title, node.summary, node.description, getItemSignal(node, model)]
      .filter(Boolean)
      .some((value) => value!.toLowerCase().includes(normalizedQuery));
  });
}

export function getItemRelationships(model: WorkflowModel, nodeId: string) {
  const dependsOn = model.edges
    .filter((edge) => edge.to === nodeId)
    .map((edge) => model.nodes.find((node) => node.id === edge.from))
    .filter((node): node is WorkflowNode => Boolean(node));
  const blocks = model.edges
    .filter((edge) => edge.from === nodeId)
    .map((edge) => model.nodes.find((node) => node.id === edge.to))
    .filter((node): node is WorkflowNode => Boolean(node));
  return { dependsOn, blocks };
}

export function resolveItemExplorerSelection(model: WorkflowModel, requestedId: string | null) {
  if (requestedId && model.nodes.some((node) => node.id === requestedId)) return requestedId;
  return model.nodes[0]?.id ?? null;
}

export function createItemExplorerDeepLink(nodeId: string) {
  return { activeTab: 'items' as const, selectedItemId: nodeId };
}
