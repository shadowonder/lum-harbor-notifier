import {
  WORKFLOW_STAGES,
  type WorkflowDependencyEdge,
  type WorkflowHealth,
  type WorkflowItemType,
  type WorkflowNode,
  type WorkflowStageId,
} from './workflowTypes.ts';
export { getItemTypeIcon } from './workflowTypes.ts';

export interface LayoutDimensions {
  width: number;
  height: number;
  paddingX: number;
  paddingY: number;
  trunkY: number;
  laneSpacing: number;
}

export interface WorkflowPoint {
  x: number;
  y: number;
}

export interface DependencyEdgePresentation {
  strokeWidth: number;
  opacity: number;
  showArrow: boolean;
}

export const WORKFLOW_LINE_HIERARCHY = {
  baselineWidth: 2.35,
  attachmentWidth: 0.85,
  dependencyWidth: 1.05,
} as const;

export const DEFAULT_MINIMAP_DIMENSIONS: LayoutDimensions = {
  width: 540,
  height: 64,
  paddingX: 16,
  paddingY: 8,
  trunkY: 32,
  laneSpacing: 12,
};

export const COMPACT_MINIMAP_DIMENSIONS: LayoutDimensions = {
  width: 540,
  height: 48,
  paddingX: 16,
  paddingY: 6,
  trunkY: 24,
  laneSpacing: 8,
};

export const DEFAULT_CANVAS_DIMENSIONS: LayoutDimensions = {
  width: 960,
  height: 300,
  paddingX: 30,
  paddingY: 20,
  trunkY: 160,
  laneSpacing: 48,
};

/** Returns the horizontal bounding box [xStart, xEnd] for each stage. */
export function getStageBounds(
  stageId: WorkflowStageId,
  dims: LayoutDimensions,
): { startX: number; endX: number; centerX: number; width: number } {
  const stageCount = WORKFLOW_STAGES.length; // 6
  const usableWidth = dims.width - dims.paddingX * 2;
  const stageWidth = usableWidth / stageCount;
  const stageIndex = WORKFLOW_STAGES.findIndex((s) => s.id === stageId);
  const idx = stageIndex === -1 ? 0 : stageIndex;

  const startX = dims.paddingX + idx * stageWidth;
  const endX = startX + stageWidth;
  const centerX = startX + stageWidth / 2;

  return { startX, endX, centerX, width: stageWidth };
}

/** Computes x, y coordinate for a node in a given stage and lane. */
export function getNodeCoordinates(
  node: WorkflowNode,
  dims: LayoutDimensions,
): { x: number; y: number } {
  const bounds = getStageBounds(node.stageId, dims);
  const lane = node.lane ?? -1;
  const y = dims.trunkY + lane * dims.laneSpacing;
  return { x: bounds.centerX, y };
}

/** Computes x, y coordinate for a ghost target marker. */
export function getTargetMarkerCoordinates(
  node: WorkflowNode,
  dims: LayoutDimensions,
): { x: number; y: number } | null {
  if (!node.targetStageId) return null;
  const bounds = getStageBounds(node.targetStageId, dims);
  const lane = node.lane ?? -1;
  const y = dims.trunkY + lane * dims.laneSpacing;
  return { x: bounds.centerX, y };
}

/**
 * Traverses upstream and downstream dependencies from a focused node.
 * Returns sets of highlighted node IDs and edge keys ('from->to').
 */
export function computeDependencyChain(
  activeNodeId: string,
  edges: WorkflowDependencyEdge[],
): {
  highlightedNodeIds: Set<string>;
  highlightedEdgeKeys: Set<string>;
  directEdgeKeys: Set<string>;
} {
  const highlightedNodeIds = new Set<string>([activeNodeId]);
  const highlightedEdgeKeys = new Set<string>();
  const directEdgeKeys = new Set<string>();

  // Upstream traversal (nodes that activeNode depends on: edge.to === current -> edge.from)
  const upstreamQueue: string[] = [activeNodeId];
  const visitedUpstream = new Set<string>([activeNodeId]);

  while (upstreamQueue.length > 0) {
    const current = upstreamQueue.shift()!;
    for (const edge of edges) {
      if (edge.to === current) {
        const key = `${edge.from}->${edge.to}`;
        highlightedEdgeKeys.add(key);
        if (current === activeNodeId) directEdgeKeys.add(key);
        if (!visitedUpstream.has(edge.from)) {
          visitedUpstream.add(edge.from);
          highlightedNodeIds.add(edge.from);
          upstreamQueue.push(edge.from);
        }
      }
    }
  }

  // Downstream traversal (nodes that depend on activeNode: edge.from === current -> edge.to)
  const downstreamQueue: string[] = [activeNodeId];
  const visitedDownstream = new Set<string>([activeNodeId]);

  while (downstreamQueue.length > 0) {
    const current = downstreamQueue.shift()!;
    for (const edge of edges) {
      if (edge.from === current) {
        const key = `${edge.from}->${edge.to}`;
        highlightedEdgeKeys.add(key);
        if (current === activeNodeId) directEdgeKeys.add(key);
        if (!visitedDownstream.has(edge.to)) {
          visitedDownstream.add(edge.to);
          highlightedNodeIds.add(edge.to);
          downstreamQueue.push(edge.to);
        }
      }
    }
  }

  return { highlightedNodeIds, highlightedEdgeKeys, directEdgeKeys };
}

/**
 * Returns only the immediate (one-hop) upstream and downstream neighbors of
 * a node, without walking the full transitive chain. Intended for hover
 * feedback, where recomputing the entire chain on every pointer movement
 * causes the highlighted region to flicker as the mouse crosses the graph.
 * Selecting a node (click) should use computeDependencyChain instead.
 */
export function computeDirectRelations(
  activeNodeId: string,
  edges: WorkflowDependencyEdge[],
): {
  highlightedNodeIds: Set<string>;
  highlightedEdgeKeys: Set<string>;
} {
  const highlightedNodeIds = new Set<string>([activeNodeId]);
  const highlightedEdgeKeys = new Set<string>();

  for (const edge of edges) {
    if (edge.to === activeNodeId) {
      highlightedNodeIds.add(edge.from);
      highlightedEdgeKeys.add(`${edge.from}->${edge.to}`);
    }
    if (edge.from === activeNodeId) {
      highlightedNodeIds.add(edge.to);
      highlightedEdgeKeys.add(`${edge.from}->${edge.to}`);
    }
  }

  return { highlightedNodeIds, highlightedEdgeKeys };
}

function compactOrthogonalPoints(points: WorkflowPoint[]) {
  return points.filter((point, index, entries) => {
    if (index > 0 && point.x === entries[index - 1].x && point.y === entries[index - 1].y) {
      return false;
    }
    if (index === 0 || index === entries.length - 1) return true;
    const previous = entries[index - 1];
    const next = entries[index + 1];
    const collinearX = previous.x === point.x && point.x === next.x;
    const collinearY = previous.y === point.y && point.y === next.y;
    return !collinearX && !collinearY;
  });
}

/** Builds a rounded SVG path while preserving supplied orthogonal bend points. */
export function getRoundedOrthogonalPath(points: WorkflowPoint[], radius = 4): string {
  const route = compactOrthogonalPoints(points);
  if (route.length < 2) return '';
  let path = `M ${route[0].x} ${route[0].y}`;
  for (let index = 1; index < route.length - 1; index += 1) {
    const previous = route[index - 1];
    const current = route[index];
    const next = route[index + 1];
    const incoming = Math.min(
      radius,
      Math.hypot(current.x - previous.x, current.y - previous.y) / 2,
    );
    const outgoing = Math.min(radius, Math.hypot(next.x - current.x, next.y - current.y) / 2);
    const beforeX = current.x - Math.sign(current.x - previous.x) * incoming;
    const beforeY = current.y - Math.sign(current.y - previous.y) * incoming;
    const afterX = current.x + Math.sign(next.x - current.x) * outgoing;
    const afterY = current.y + Math.sign(next.y - current.y) * outgoing;
    path += ` L ${beforeX} ${beforeY} Q ${current.x} ${current.y} ${afterX} ${afterY}`;
  }
  const last = route[route.length - 1];
  return `${path} L ${last.x} ${last.y}`;
}

/**
 * Routes dependency wires from the source card's right port to the target card's left port.
 * Cross-stage links turn in the authoritative stage gutter. Same-stage links use a compact
 * outer loop so they never travel diagonally through either node body.
 */
export function getDependencyRoutePoints(
  source: WorkflowNode,
  target: WorkflowNode,
  dims: LayoutDimensions,
  nodeWidth = 126,
  bendPoints?: WorkflowPoint[],
): WorkflowPoint[] {
  const sourceCenter = getNodeCoordinates(source, dims);
  const targetCenter = getNodeCoordinates(target, dims);
  const sourcePort = { x: sourceCenter.x + nodeWidth / 2, y: sourceCenter.y };
  const targetPort = { x: targetCenter.x - nodeWidth / 2, y: targetCenter.y };

  if (bendPoints?.length) return compactOrthogonalPoints([sourcePort, ...bendPoints, targetPort]);

  if (source.stageId !== target.stageId && sourceCenter.x < targetCenter.x) {
    const sourceStage = getStageBounds(source.stageId, dims);
    const gutterX = sourceStage.endX;
    return compactOrthogonalPoints([
      sourcePort,
      { x: gutterX, y: sourcePort.y },
      { x: gutterX, y: targetPort.y },
      targetPort,
    ]);
  }

  const stage = getStageBounds(source.stageId, dims);
  const rightGutterX = stage.endX;
  const leftGutterX = stage.startX;
  let channelY = (sourcePort.y + targetPort.y) / 2;
  if (Math.abs(channelY - dims.trunkY) < 8) {
    channelY += targetPort.y >= sourcePort.y ? 10 : -10;
  }
  return compactOrthogonalPoints([
    sourcePort,
    { x: rightGutterX, y: sourcePort.y },
    { x: rightGutterX, y: channelY },
    { x: leftGutterX, y: channelY },
    { x: leftGutterX, y: targetPort.y },
    targetPort,
  ]);
}

export function getDependencyEdgePresentation({
  highlighted = false,
  direct = true,
  hovered = false,
  dimmed = false,
}: {
  highlighted?: boolean;
  /**
   * Whether this edge directly touches the focused node, as opposed to
   * being reachable only through another node in the transitive chain.
   * Ignored unless `highlighted` is true. Defaults to true so existing
   * callers that don't distinguish direct/transitive keep today's full-
   * strength highlighted style.
   */
  direct?: boolean;
  hovered?: boolean;
  dimmed?: boolean;
}): DependencyEdgePresentation {
  if (dimmed) return { strokeWidth: 0.9, opacity: 0.18, showArrow: false };
  if (highlighted) {
    return direct
      ? { strokeWidth: 1.8, opacity: 0.94, showArrow: true }
      : { strokeWidth: 1.35, opacity: 0.7, showArrow: true };
  }
  if (hovered) return { strokeWidth: 1.45, opacity: 0.86, showArrow: true };
  return {
    strokeWidth: WORKFLOW_LINE_HIERARCHY.dependencyWidth,
    opacity: 0.52,
    showArrow: false,
  };
}

export function getWorkflowEdgePalette(isDark: boolean) {
  return {
    dependency: isDark ? 'rgba(167, 176, 186, 0.72)' : 'rgba(95, 105, 116, 0.58)',
    dependencyFocus: isDark ? '#7FA4CA' : '#466B91',
    attachment: isDark ? 'rgba(143, 153, 165, 0.24)' : 'rgba(133, 145, 157, 0.24)',
    baseline: isDark ? 'rgba(143, 153, 165, 0.34)' : 'rgba(95, 105, 116, 0.28)',
  };
}

/** Generates SVG path connecting trunk baseline to branch node. */
export function getBranchConnectorPath(nodeX: number, nodeY: number, trunkY: number): string {
  return `M ${nodeX} ${trunkY} L ${nodeX} ${nodeY}`;
}

export function getHealthColor(health: WorkflowHealth, isDark: boolean): string {
  switch (health) {
    case 'green':
      return isDark ? '#83AA90' : '#52705E';
    case 'yellow':
      return isDark ? '#C69358' : '#A2682A';
    case 'red':
      return isDark ? '#D28075' : '#A85147';
    case 'gray':
    default:
      return isDark ? '#94a3b8' : '#64748b';
  }
}

/**
 * Short, scannable health text — health must never be color-only. Card
 * designs that show both `getHealthColor` and this label side by side
 * satisfy that contract without relying on color perception alone.
 */
export function getHealthLabel(health: WorkflowHealth): string {
  switch (health) {
    case 'green':
      return 'ON TRACK';
    case 'yellow':
      return 'AT RISK';
    case 'red':
      return 'BLOCKED';
    case 'gray':
    default:
      return 'QUEUED';
  }
}

export function getHealthBgColor(health: WorkflowHealth, isDark: boolean): string {
  switch (health) {
    case 'green':
      return isDark ? 'rgba(16, 185, 129, 0.2)' : 'rgba(16, 185, 129, 0.12)';
    case 'yellow':
      return isDark ? 'rgba(245, 158, 11, 0.2)' : 'rgba(245, 158, 11, 0.12)';
    case 'red':
      return isDark ? 'rgba(239, 68, 68, 0.2)' : 'rgba(239, 68, 68, 0.12)';
    case 'gray':
    default:
      return isDark ? 'rgba(148, 163, 184, 0.15)' : 'rgba(100, 116, 139, 0.1)';
  }
}

export function getItemTypeLabel(itemType: WorkflowItemType): string {
  switch (itemType) {
    case 'story':
      return 'User Story';
    case 'task':
      return 'Task';
    case 'defect':
      return 'Defect';
    case 'gate':
      return 'Quality Gate';
  }
}
