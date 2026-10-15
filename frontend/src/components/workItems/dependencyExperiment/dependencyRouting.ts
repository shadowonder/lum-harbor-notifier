import { getRoundedOrthogonalPath, type WorkflowPoint } from '../workflow/workflowLayout.ts';
import type { WorkflowDependencyEdge, WorkflowNode } from '../workflow/workflowTypes.ts';

/**
 * Visualization Experiment 1 — Stage/Dependency Explorer.
 *
 * Stage here is a *presentation-layer* grouping used only to give the
 * dependency graph strong spatial structure (swimlanes). It is deliberately
 * separate from `WorkflowStageId` (queue/dev/test/stage/adoption/production
 * — the software delivery pipeline position already used by Item Explorer
 * and the workflow context panel) and from any backend/domain concept.
 * Nothing here invents new domain truth: stage assignment is presentation
 * data the fixture/view-model layer supplies; the renderer only lays it
 * out and classifies real `model.edges` relative to it.
 */
export interface ExperimentStage {
  id: string;
  label: string;
  order: number;
}

export const EXPERIMENT_STAGES: readonly ExperimentStage[] = [
  { id: 'foundation', label: 'FOUNDATION', order: 0 },
  { id: 'api', label: 'API', order: 1 },
  { id: 'frontend', label: 'FRONTEND', order: 2 },
  { id: 'verification', label: 'VERIFICATION', order: 3 },
  { id: 'release', label: 'RELEASE', order: 4 },
];

/**
 * Items without curated stage data fall into one generic catch-all column
 * rather than the renderer inventing a mapping from the unrelated
 * `WorkflowStageId` delivery-pipeline enum onto this presentation taxonomy.
 */
export const FALLBACK_STAGE: ExperimentStage = {
  id: 'items',
  label: 'ITEMS',
  order: EXPERIMENT_STAGES.length,
};

/**
 * Curated stage assignment for the representative `wi-checkout-platform`
 * fixture, expressed at the presentation/view-model boundary — not part of
 * the shared `WorkflowNode` domain shape and not a backend concept.
 */
const CURATED_STAGE_BY_NODE_ID: Readonly<Record<string, string>> = {
  'wi-cp-1': 'foundation',
  'wi-cp-2': 'foundation',
  'wi-cp-3': 'api',
  'wi-cp-4': 'frontend',
  'wi-cp-5': 'verification',
  'wi-cp-6': 'frontend',
  'wi-cp-7': 'verification',
  'wi-cp-8': 'release',
  'wi-cp-9': 'release',
  'wi-cp-10': 'release',
};

export function getStageById(stageId: string): ExperimentStage {
  return EXPERIMENT_STAGES.find((stage) => stage.id === stageId) ?? FALLBACK_STAGE;
}

export function resolveNodeStage(nodeId: string): ExperimentStage {
  const stageId = CURATED_STAGE_BY_NODE_ID[nodeId];
  return stageId ? getStageById(stageId) : FALLBACK_STAGE;
}

/**
 * The set of stages this round's swimlane layout must always render as
 * columns — including any that currently hold zero items — as distinct
 * from the subset that *happens* to have nodes right now. This is what
 * lets "5 stages defined, all items in Frontend" keep showing all 5
 * columns instead of silently collapsing to the 1 that has content.
 *
 * When none of the given nodes have curated stage data at all, the only
 * stage that legitimately exists for this content is the generic
 * fallback bucket — that is a real "only one stage is defined" case, not
 * an "items happen to be concentrated" case, so exactly one column is
 * correct there.
 */
export function resolveDefinedStages(nodes: WorkflowNode[]): ExperimentStage[] {
  const anyCurated = nodes.some((node) => node.id in CURATED_STAGE_BY_NODE_ID);
  return anyCurated ? [...EXPERIMENT_STAGES] : [FALLBACK_STAGE];
}

export type RoutingCategory = 'same-stage' | 'adjacent-stage' | 'long-cross-stage' | 'backward';

/**
 * Classifies a dependency edge purely from the stage *order* of its source
 * and target — never from card pixel position. `A -> B` always means
 * "B depends on A / A must happen before B" throughout this renderer.
 *
 * - same order            -> same-stage (needs a local in-column rail)
 * - exactly one stage further forward -> adjacent-stage (direct short edge)
 * - more than one stage further forward -> long-cross-stage (needs the
 *   cross-stage rail so it doesn't cut through intervening stage columns)
 * - any stage further BACKWARD -> backward (an exception relative to the
 *   stage order; the renderer only classifies it as backward — it must
 *   never infer or label it as "rework"/"regression" unless the real data
 *   already says so)
 */
export function classifyEdgeRouting(
  sourceStageOrder: number,
  targetStageOrder: number,
): RoutingCategory {
  const diff = targetStageOrder - sourceStageOrder;
  if (diff === 0) return 'same-stage';
  if (diff < 0) return 'backward';
  if (diff === 1) return 'adjacent-stage';
  return 'long-cross-stage';
}

/**
 * Stage column width contract (Experiment 1.1). Logical geometry — never a
 * viewport-driven scale factor. A stage column adapts within this band
 * based on measured available width (`computeStageColumnWidth`); the fixed
 * card (`DEFAULT_EXPERIMENT_LAYOUT_OPTIONS.cardWidth`) never stretches or
 * shrinks with it — only the breathing-room padding around it changes.
 */
export const STAGE_MIN_WIDTH = 248;
export const STAGE_IDEAL_WIDTH = 272;
export const STAGE_MAX_WIDTH = 300;

/** Bounded canvas viewport height policy (Experiment 1.1): the graph body
 * never forces the whole WorkItem page taller than this; it scrolls
 * internally instead once content exceeds it. */
export const CANVAS_MIN_HEIGHT = 420;
export const CANVAS_MAX_HEIGHT = 640;

/**
 * Picks one column width for every stage, adapting to available width
 * without ever upscaling: if the viewport can give every stage at least
 * `STAGE_MIN_WIDTH`, columns adapt anywhere between `min` and `max`, biased
 * toward filling the space evenly. Once `availableWidth / stageCount`
 * would fall below `min`, shrinking stops entirely — the column freezes at
 * `min` and the caller is expected to let content overflow horizontally
 * (scroll) instead of compressing further.
 */
export function computeStageColumnWidth(
  stageCount: number,
  availableWidth: number,
  options: { min?: number; ideal?: number; max?: number } = {},
): number {
  const { min = STAGE_MIN_WIDTH, ideal = STAGE_IDEAL_WIDTH, max = STAGE_MAX_WIDTH } = options;
  if (stageCount <= 0 || !Number.isFinite(availableWidth) || availableWidth <= 0) return ideal;
  const candidate = availableWidth / stageCount;
  return Math.min(max, Math.max(min, candidate));
}

/**
 * The single, explicit horizontal-overflow contract (Experiment 1.1 bugfix):
 * horizontal scrolling is warranted only when the graph's real logical
 * content width genuinely exceeds the scroll viewport's actual usable
 * client width. In particular, this must never be affected by whether a
 * *vertical* scrollbar happens to be showing elsewhere in the canvas —
 * `contentWidth` (driven purely by stage count/column width) and
 * `clientWidth` (the real measured viewport) are the only two inputs.
 */
export function hasHorizontalOverflow(contentWidth: number, clientWidth: number): boolean {
  return contentWidth > clientWidth;
}

/**
 * Small, fixed visual safety margin added to the right of the graph's real
 * content width (`ExperimentLayout.width`) purely so the last stage's local
 * dependency rail / arrowheads never sit flush against the graph surface's
 * own right boundary. This is graph visual padding, not a scrollbar
 * compensation — it never depends on measured viewport width, scrollbar
 * presence, or column width, and it never feeds back into
 * `computeStageColumnWidth`/`computeSwimlaneLayout`.
 */
export const GRAPH_RIGHT_PADDING = 28;

/** The actual rendered width of the graph surface: real content plus the
 * fixed right-side visual safety margin above. */
export function computeRenderedGraphWidth(contentWidth: number): number {
  return contentWidth + GRAPH_RIGHT_PADDING;
}

export interface ExperimentLayoutOptions {
  cardWidth?: number;
  cardHeight?: number;
  rowGap?: number;
  /** Total column width (card + padding + local gutter). Adapts within
   * [STAGE_MIN_WIDTH, STAGE_MAX_WIDTH] via `computeStageColumnWidth`;
   * defaults to STAGE_IDEAL_WIDTH when the caller has no measurement yet. */
  columnWidth?: number;
  localGutterWidth?: number;
  topRailMargin?: number;
  bottomRailMargin?: number;
}

export const DEFAULT_EXPERIMENT_LAYOUT_OPTIONS: Required<ExperimentLayoutOptions> = {
  cardWidth: 208,
  cardHeight: 84,
  rowGap: 20,
  columnWidth: STAGE_IDEAL_WIDTH,
  localGutterWidth: 28,
  topRailMargin: 44,
  bottomRailMargin: 44,
};

export interface ExperimentNodePosition {
  x: number;
  y: number;
  stageId: string;
}

export interface ExperimentStageColumn {
  stage: ExperimentStage;
  startX: number;
  endX: number;
  centerX: number;
  /** X of the in-column local dependency rail, used for same-stage routing. */
  localRailX: number;
}

export interface ExperimentLayout {
  columns: ExperimentStageColumn[];
  positions: Map<string, ExperimentNodePosition>;
  width: number;
  contentTop: number;
  contentBottom: number;
  topRailY: number;
  bottomRailY: number;
}

/**
 * Lays every node out purely by its resolved stage (a strong spatial
 * swimlane, not a small badge) and stacks same-stage nodes vertically. This
 * never runs dependency-depth math — Stage is the primary spatial channel
 * this round, by design.
 */
export function computeSwimlaneLayout(
  nodes: WorkflowNode[],
  options: ExperimentLayoutOptions = {},
): ExperimentLayout {
  const {
    cardWidth,
    cardHeight,
    rowGap,
    columnWidth,
    localGutterWidth,
    topRailMargin,
    bottomRailMargin,
  } = {
    ...DEFAULT_EXPERIMENT_LAYOUT_OPTIONS,
    ...options,
  };

  const nodesByStage = new Map<string, WorkflowNode[]>();
  for (const node of nodes) {
    const stage = resolveNodeStage(node.id);
    if (!nodesByStage.has(stage.id)) nodesByStage.set(stage.id, []);
    nodesByStage.get(stage.id)!.push(node);
  }

  // Every stage the presentation model defines gets a column — including
  // ones with zero items right now — never just the subset that currently
  // has content. Only when nothing is curated at all does the "defined"
  // set legitimately shrink to the single fallback bucket.
  const activeStages = resolveDefinedStages(nodes)
    .slice()
    .sort((a, b) => a.order - b.order);

  // Left-side breathing room is whatever remains once the fixed card and
  // fixed local-gutter width are subtracted from the adaptive column
  // width — the card itself never grows or shrinks with it.
  const leftPadding = Math.max(0, columnWidth - cardWidth - localGutterWidth);

  const columns: ExperimentStageColumn[] = activeStages.map((stage, index) => {
    const startX = index * columnWidth;
    const endX = startX + columnWidth;
    return {
      stage,
      startX,
      endX,
      centerX: startX + leftPadding + cardWidth / 2,
      localRailX: endX - localGutterWidth / 2,
    };
  });

  const positions = new Map<string, ExperimentNodePosition>();
  let maxRows = 1;

  for (const column of columns) {
    const stageNodes = nodesByStage.get(column.stage.id) ?? [];
    maxRows = Math.max(maxRows, stageNodes.length);
    stageNodes.forEach((node, row) => {
      positions.set(node.id, {
        x: column.centerX,
        y: row * (cardHeight + rowGap),
        stageId: column.stage.id,
      });
    });
  }

  const contentTop = 0 - cardHeight / 2;
  const contentBottom = (maxRows - 1) * (cardHeight + rowGap) + cardHeight / 2;

  return {
    columns,
    positions,
    width: columns.length > 0 ? columns[columns.length - 1]!.endX : columnWidth,
    contentTop,
    contentBottom,
    topRailY: contentTop - topRailMargin,
    bottomRailY: contentBottom + bottomRailMargin,
  };
}

/**
 * Builds the routed SVG path for one classified edge. Every category uses
 * horizontal/vertical (Manhattan) segments only — no long diagonals — and
 * is specifically shaped to avoid crossing intervening card bodies:
 *
 * - same-stage: hugs the stage's own local rail on the cards' right side,
 *   so it never needs to pass through a card between source and target.
 * - adjacent-stage: a short, direct right-to-left hop across the stage
 *   boundary.
 * - long-cross-stage: routed onto the shared rail above every column, so
 *   it never cuts through the stage columns it skips over.
 * - backward: routed onto the shared rail below every column, kept
 *   visually and topologically separate from the forward-flow rails above.
 */
export function getExperimentEdgePath(
  category: RoutingCategory,
  source: ExperimentNodePosition,
  target: ExperimentNodePosition,
  layout: ExperimentLayout,
  cardWidth: number = DEFAULT_EXPERIMENT_LAYOUT_OPTIONS.cardWidth,
  cardHeight: number = DEFAULT_EXPERIMENT_LAYOUT_OPTIONS.cardHeight,
): string {
  switch (category) {
    case 'adjacent-stage': {
      const sourcePort: WorkflowPoint = { x: source.x + cardWidth / 2, y: source.y };
      const targetPort: WorkflowPoint = { x: target.x - cardWidth / 2, y: target.y };
      const midX = (sourcePort.x + targetPort.x) / 2;
      return getRoundedOrthogonalPath([
        sourcePort,
        { x: midX, y: sourcePort.y },
        { x: midX, y: targetPort.y },
        targetPort,
      ]);
    }
    case 'same-stage': {
      const column = layout.columns.find((entry) => entry.stage.id === source.stageId);
      const railX = column?.localRailX ?? source.x + cardWidth / 2 + 14;
      const sourcePort: WorkflowPoint = { x: source.x + cardWidth / 2, y: source.y };
      const targetPort: WorkflowPoint = { x: target.x + cardWidth / 2, y: target.y };
      return getRoundedOrthogonalPath([
        sourcePort,
        { x: railX, y: sourcePort.y },
        { x: railX, y: targetPort.y },
        targetPort,
      ]);
    }
    case 'long-cross-stage': {
      const sourcePort: WorkflowPoint = { x: source.x, y: source.y - cardHeight / 2 };
      const targetPort: WorkflowPoint = { x: target.x, y: target.y - cardHeight / 2 };
      const railY = layout.topRailY;
      return getRoundedOrthogonalPath([
        sourcePort,
        { x: sourcePort.x, y: railY },
        { x: targetPort.x, y: railY },
        targetPort,
      ]);
    }
    case 'backward': {
      const sourcePort: WorkflowPoint = { x: source.x, y: source.y + cardHeight / 2 };
      const targetPort: WorkflowPoint = { x: target.x, y: target.y + cardHeight / 2 };
      const railY = layout.bottomRailY;
      return getRoundedOrthogonalPath([
        sourcePort,
        { x: sourcePort.x, y: railY },
        { x: targetPort.x, y: railY },
        targetPort,
      ]);
    }
  }
}

export interface ExperimentEdgePresentation {
  strokeWidth: number;
  opacity: number;
  dashed: boolean;
  showArrow: boolean;
}

/**
 * Backward edges are always dashed, even when idle — direction must be
 * legible from line style alone, not only from an accent color, so it
 * reads as "an exception" at a glance regardless of focus state.
 */
export function getExperimentEdgePresentation({
  category,
  highlighted = false,
  direct = true,
  dimmed = false,
}: {
  category: RoutingCategory;
  highlighted?: boolean;
  direct?: boolean;
  dimmed?: boolean;
}): ExperimentEdgePresentation {
  const dashed = category === 'backward';

  if (dimmed) return { strokeWidth: 1, opacity: 0.15, dashed, showArrow: false };

  if (highlighted) {
    return direct
      ? { strokeWidth: 2, opacity: 0.95, dashed, showArrow: true }
      : { strokeWidth: 1.5, opacity: 0.72, dashed, showArrow: true };
  }

  if (category === 'long-cross-stage')
    return { strokeWidth: 1.1, opacity: 0.55, dashed, showArrow: false };
  return { strokeWidth: 1.15, opacity: 0.65, dashed, showArrow: false };
}

export function experimentEdgeKey(edge: WorkflowDependencyEdge): string {
  return `${edge.from}->${edge.to}`;
}
