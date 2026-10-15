/**
 * Pure layout math for the Compact Workflow Topology Summary. No React, no
 * SVG strings for full dependency routing — this is intentionally much
 * simpler than the WorkItem Dependency Explorer's swimlane router:
 *
 * - Every item sits directly above or below the trunk, at its stage's
 *   horizontal center, connected by a single straight vertical stem. There
 *   is no item x-jitter and no bend-point routing to compute, so every
 *   item is attached to the trunk by construction — a floating node is
 *   structurally impossible here.
 * - Representative dependency edges are drawn as a short orthogonal detour
 *   just off the trunk line between two stage centers — never a routing
 *   system with same-stage/adjacent/cross-stage/backward rail categories.
 */

import type {
  CompactStage,
  CompactTopologyItem,
  CompactTopologyModel,
} from './compactTopologyTypes.ts';

export interface CompactTopologyDimensions {
  width: number;
  height: number;
  paddingX: number;
  trunkY: number;
  laneSpacing: number;
}

export const DEFAULT_TOPOLOGY_DIMENSIONS: CompactTopologyDimensions = {
  width: 420,
  height: 68,
  paddingX: 14,
  trunkY: 36,
  laneSpacing: 13,
};

export const COMPACT_TOPOLOGY_DIMENSIONS: CompactTopologyDimensions = {
  width: 420,
  height: 48,
  paddingX: 14,
  trunkY: 26,
  laneSpacing: 10,
};

/** A stage never shows more than this many item markers; the rest collapse into a "+N" indicator. */
export const MAX_VISIBLE_ITEMS_PER_STAGE = 4;
/** Deterministic lane order: alternating above/below the trunk, growing outward. */
const LANE_OFFSETS = [-1, 1, -2, 2] as const;

export interface StageBounds {
  startX: number;
  endX: number;
  centerX: number;
  width: number;
}

/** Horizontal bounds for one stage, given however many stages this WorkItem actually has. */
export function computeStageBounds(
  stageId: string,
  stages: readonly CompactStage[],
  dims: CompactTopologyDimensions,
): StageBounds {
  const count = Math.max(1, stages.length);
  const usableWidth = dims.width - dims.paddingX * 2;
  const stageWidth = usableWidth / count;
  const index = Math.max(
    0,
    stages.findIndex((stage) => stage.id === stageId),
  );
  const startX = dims.paddingX + index * stageWidth;
  return { startX, endX: startX + stageWidth, centerX: startX + stageWidth / 2, width: stageWidth };
}

export interface PositionedTopologyItem {
  item: CompactTopologyItem;
  x: number;
  y: number;
}

export interface StageItemLayout {
  stage: CompactStage;
  bounds: StageBounds;
  visible: PositionedTopologyItem[];
  overflowCount: number;
}

/**
 * Groups items by stage and assigns each a deterministic lane slot so no
 * two items in the same stage share the same visual position. Items beyond
 * `MAX_VISIBLE_ITEMS_PER_STAGE` are counted (not silently dropped) via
 * `overflowCount`, and the same items always land in the same lanes for
 * the same input (no randomness).
 */
export function computeStageItemLayouts(
  model: CompactTopologyModel,
  dims: CompactTopologyDimensions,
): StageItemLayout[] {
  return model.stages.map((stage) => {
    const bounds = computeStageBounds(stage.id, model.stages, dims);
    const stageItems = model.items.filter((item) => item.stageId === stage.id);
    const visibleItems = stageItems.slice(0, MAX_VISIBLE_ITEMS_PER_STAGE);
    const visible = visibleItems.map((item, index) => ({
      item,
      x: bounds.centerX,
      y: dims.trunkY + LANE_OFFSETS[index]! * dims.laneSpacing,
    }));
    return {
      stage,
      bounds,
      visible,
      overflowCount: Math.max(0, stageItems.length - visibleItems.length),
    };
  });
}

export interface RepresentativeEdgeRoute {
  from: string;
  to: string;
  points: [number, number][];
}

/**
 * Selects a small, representative subset of edges (never "draw
 * everything") and routes each as a short orthogonal detour just off the
 * trunk line between the two stage centers it connects — deliberately not
 * the Dependency Explorer's multi-category rail system. Edges touching a
 * non-green item are prioritized as the more "critical" relationships.
 */
export function selectRepresentativeEdgeRoutes(
  model: CompactTopologyModel,
  dims: CompactTopologyDimensions,
  maxEdges: number,
): RepresentativeEdgeRoute[] {
  const itemById = new Map(model.items.map((item) => [item.id, item]));
  const isCritical = (edge: { from: string; to: string }) => {
    const from = itemById.get(edge.from);
    const to = itemById.get(edge.to);
    return from?.health !== 'green' || to?.health !== 'green';
  };

  const eligible = model.edges.filter((edge) => itemById.has(edge.from) && itemById.has(edge.to));
  const ordered = [...eligible].sort((a, b) => Number(isCritical(b)) - Number(isCritical(a)));
  const selected = ordered.slice(0, Math.max(0, maxEdges));

  const detourY = dims.trunkY + dims.laneSpacing * 0.4;
  return selected.map((edge) => {
    const from = itemById.get(edge.from)!;
    const to = itemById.get(edge.to)!;
    const fromX = computeStageBounds(from.stageId, model.stages, dims).centerX;
    const toX = computeStageBounds(to.stageId, model.stages, dims).centerX;
    return {
      from: edge.from,
      to: edge.to,
      points: [
        [fromX, dims.trunkY],
        [fromX, detourY],
        [toX, detourY],
        [toX, dims.trunkY],
      ],
    };
  });
}

/** Builds a plain polyline path (no rounding needed for this short, low-count detour). */
export function toPolylinePath(points: readonly (readonly [number, number])[]): string {
  if (points.length === 0) return '';
  return points.map(([x, y], index) => `${index === 0 ? 'M' : 'L'} ${x} ${y}`).join(' ');
}
