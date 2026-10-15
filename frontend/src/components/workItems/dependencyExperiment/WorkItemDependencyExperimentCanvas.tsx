import { useEffect, useMemo, useRef, useState } from 'react';
import { Box, Paper, Typography } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import {
  computeDependencyChain,
  computeDirectRelations,
  getHealthColor,
  getHealthLabel,
} from '../workflow/workflowLayout.ts';
import {
  hasWorkflowData,
  type WorkflowItemType,
  type WorkflowModel,
} from '../workflow/workflowTypes.ts';
import {
  CANVAS_MAX_HEIGHT,
  CANVAS_MIN_HEIGHT,
  classifyEdgeRouting,
  computeRenderedGraphWidth,
  computeStageColumnWidth,
  computeSwimlaneLayout,
  DEFAULT_EXPERIMENT_LAYOUT_OPTIONS,
  experimentEdgeKey,
  getExperimentEdgePath,
  getExperimentEdgePresentation,
  resolveDefinedStages,
} from './dependencyRouting.ts';

const { cardWidth: CARD_WIDTH, cardHeight: CARD_HEIGHT } = DEFAULT_EXPERIMENT_LAYOUT_OPTIONS;

interface WorkItemDependencyExperimentCanvasProps {
  model: WorkflowModel;
  selectedNodeId?: string | null;
  onSelectNode?: (nodeId: string | null) => void;
}

function TypeMarker({ itemType, color }: { itemType: WorkflowItemType; color: string }) {
  switch (itemType) {
    case 'story':
      return <circle cx={0} cy={0} r={6} fill="none" stroke={color} strokeWidth={2} />;
    case 'task':
      return (
        <rect
          x={-6}
          y={-6}
          width={12}
          height={12}
          rx={2}
          fill="none"
          stroke={color}
          strokeWidth={2}
        />
      );
    case 'defect':
      return <polygon points="0,-7 7,6 -7,6" fill="none" stroke={color} strokeWidth={2} />;
    case 'gate':
      return <polygon points="0,-7 7,0 0,7 -7,0" fill="none" stroke={color} strokeWidth={2} />;
  }
}

export default function WorkItemDependencyExperimentCanvas({
  model,
  selectedNodeId = null,
  onSelectNode,
}: WorkItemDependencyExperimentCanvasProps) {
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const [hoveredNodeId, setHoveredNodeId] = useState<string | null>(null);

  // Measured purely to pick a column width within [MIN, MAX] — never to
  // apply a scale transform to the graph. Logical geometry (card size,
  // font size, stroke widths) never changes because of this measurement.
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [availableWidth, setAvailableWidth] = useState<number | null>(null);
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return undefined;
    const observer = new ResizeObserver((entries) => {
      const width = entries[0]?.contentRect.width;
      if (width) setAvailableWidth(width);
    });
    observer.observe(container);
    return () => observer.disconnect();
  }, []);

  const stageCount = useMemo(() => resolveDefinedStages(model.nodes).length, [model.nodes]);
  // Before the first real measurement, fall back to the ideal width rather
  // than guessing 0 (which would otherwise clamp every column to MIN).
  const columnWidth = computeStageColumnWidth(stageCount, availableWidth ?? 0);

  const layout = useMemo(
    () => computeSwimlaneLayout(model.nodes, { columnWidth }),
    [model.nodes, columnWidth],
  );

  const chain = useMemo(() => {
    if (selectedNodeId) return computeDependencyChain(selectedNodeId, model.edges);
    if (hoveredNodeId) {
      const direct = computeDirectRelations(hoveredNodeId, model.edges);
      return { ...direct, directEdgeKeys: direct.highlightedEdgeKeys };
    }
    return {
      highlightedNodeIds: new Set<string>(),
      highlightedEdgeKeys: new Set<string>(),
      directEdgeKeys: new Set<string>(),
    };
  }, [selectedNodeId, hoveredNodeId, model.edges]);

  const hasFocus = chain.highlightedNodeIds.size > 0;

  // Stage headers render as plain HTML above the SVG (not inside it), and
  // stay pinned to the top of the scrolling viewport via `position:
  // sticky` — see the ScrollViewport/GraphSurface split below for why.
  const railGutter = 12;
  const contentWidth = layout.width;
  // The graph surface's actual rendered width: real content plus a small,
  // fixed visual safety margin so the last stage's local rail/arrowheads
  // never sit flush against the graph's own right edge. This is unrelated
  // to scrollbars and never feeds back into the layout/routing math above.
  const renderedWidth = computeRenderedGraphWidth(contentWidth);
  const contentHeight = railGutter + (layout.bottomRailY - layout.topRailY) + railGutter;
  const toSvgY = (y: number) => y - layout.topRailY + railGutter;

  const neutralBorder = isDark ? 'rgba(148, 163, 184, 0.22)' : 'rgba(203, 213, 225, 0.7)';
  const columnBg = isDark ? 'rgba(30, 41, 59, 0.28)' : 'rgba(241, 245, 249, 0.55)';
  const stageHeaderColor = isDark ? '#94a3b8' : '#64748b';
  const mutedText = isDark ? '#64748b' : '#94a3b8';
  const backwardStroke = isDark ? '#C69358' : '#A2682A';
  const forwardStroke = isDark ? 'rgba(167, 176, 186, 0.8)' : 'rgba(95, 105, 116, 0.65)';
  const focusStroke = isDark ? '#7FA4CA' : '#466B91';

  const isEmpty = model.nodes.length === 0;
  const workflowAvailable = hasWorkflowData(model);

  const populatedStageIds = useMemo(() => {
    const ids = new Set<string>();
    layout.positions.forEach((position) => ids.add(position.stageId));
    return ids;
  }, [layout.positions]);

  return (
    <Paper variant="outlined" sx={{ p: 1.5, borderRadius: 1, bgcolor: 'background.paper' }}>
      {isEmpty ? (
        <Box sx={{ py: 6, textAlign: 'center' }}>
          <Typography variant="body2" sx={{ fontWeight: 650 }}>
            {workflowAvailable
              ? `${model.title} — no branch items connected yet`
              : 'Workflow data unavailable'}
          </Typography>
          <Typography variant="caption" color="text.secondary">
            {workflowAvailable
              ? 'Branch items and their dependencies will appear here once authoritative WorkItem relations are available.'
              : 'Workflow, tracked-item, and dependency projections are not implemented yet.'}
          </Typography>
        </Box>
      ) : (
        // ScrollViewport: owns BOTH scroll axes and spans the Explorer's
        // full available width. This is deliberately the *only* scrolling
        // element — a vertical scrollbar therefore always renders at this
        // viewport's own right edge (the Explorer's true right boundary),
        // never at the edge of the (possibly much narrower) centered graph
        // content inside it. Content narrower than the viewport centers
        // (`justifyContent: center`); content wider overflows and scrolls
        // natively — never upscaled and never compressed below the fixed
        // geometry. Bounding the height here is what keeps the graph from
        // growing the whole WorkItem page taller than this.
        <Box
          ref={containerRef}
          sx={{
            width: '100%',
            overflowX: 'auto',
            overflowY: 'auto',
            minHeight: CANVAS_MIN_HEIGHT,
            maxHeight: `min(${CANVAS_MAX_HEIGHT}px, 60vh)`,
          }}
        >
          <Box sx={{ display: 'flex', justifyContent: 'center', minWidth: '100%' }}>
            {/* GraphSurface: the graph's own content width (few-stage:
                narrower than the viewport and centered; many-stage: can
                exceed it). It never knows about scrollbars — it is a plain
                child of the single ScrollViewport above, not a scroll
                owner itself. */}
            <Box sx={{ width: renderedWidth, flexShrink: 0 }}>
              <Box
                sx={{
                  display: 'flex',
                  position: 'sticky',
                  top: 0,
                  zIndex: 1,
                  bgcolor: 'background.paper',
                }}
                aria-hidden="true"
              >
                {layout.columns.map((column) => (
                  <Box
                    key={column.stage.id}
                    sx={{
                      width: columnWidth,
                      flex: '0 0 auto',
                      textAlign: 'center',
                      py: 0.75,
                      borderBottom: 1,
                      borderColor: 'divider',
                    }}
                  >
                    <Typography
                      variant="caption"
                      sx={{
                        fontWeight: 700,
                        letterSpacing: '0.08em',
                        color: stageHeaderColor,
                      }}
                    >
                      {column.stage.label}
                      {!populatedStageIds.has(column.stage.id) && (
                        <Typography
                          component="span"
                          variant="caption"
                          sx={{ display: 'block', fontWeight: 500, color: 'text.disabled' }}
                        >
                          No items
                        </Typography>
                      )}
                    </Typography>
                  </Box>
                ))}
              </Box>

              <svg
                width={renderedWidth}
                height={contentHeight}
                style={{ display: 'block' }}
                aria-label={`Dependency explorer for ${model.title}`}
              >
                <defs>
                  <marker
                    id="experiment-arrow"
                    viewBox="0 0 6 6"
                    refX="5.5"
                    refY="3"
                    markerWidth="3.5"
                    markerHeight="3.5"
                    orient="auto-start-reverse"
                  >
                    <path d="M 0 0 L 6 3 L 0 6 z" fill={focusStroke} />
                  </marker>
                </defs>

                {/* Stage swimlanes: the primary spatial structure. */}
                {layout.columns.map((column, index) => (
                  <g key={column.stage.id}>
                    {index % 2 === 1 && (
                      <rect
                        x={column.startX}
                        y={0}
                        width={column.endX - column.startX}
                        height={contentHeight}
                        fill={columnBg}
                      />
                    )}
                    {index > 0 && (
                      <line
                        x1={column.startX}
                        y1={0}
                        x2={column.startX}
                        y2={contentHeight}
                        stroke={neutralBorder}
                        strokeWidth={1}
                      />
                    )}
                    {!populatedStageIds.has(column.stage.id) && (
                      <text
                        x={column.centerX}
                        y={contentHeight / 2}
                        textAnchor="middle"
                        fill={mutedText}
                        fontSize="10"
                      >
                        No items
                      </text>
                    )}
                  </g>
                ))}

                {/* Dependency edges, routed by classification. */}
                {model.edges.map((edge) => {
                  const sourcePos = layout.positions.get(edge.from);
                  const targetPos = layout.positions.get(edge.to);
                  if (!sourcePos || !targetPos) return null;

                  const sourceOrder =
                    layout.columns.find((c) => c.stage.id === sourcePos.stageId)?.stage.order ?? 0;
                  const targetOrder =
                    layout.columns.find((c) => c.stage.id === targetPos.stageId)?.stage.order ?? 0;
                  const category = classifyEdgeRouting(sourceOrder, targetOrder);

                  const key = experimentEdgeKey(edge);
                  const isHighlighted = chain.highlightedEdgeKeys.has(key);
                  const isDirect = chain.directEdgeKeys.has(key);
                  const isDimmed = hasFocus && !isHighlighted;
                  const presentation = getExperimentEdgePresentation({
                    category,
                    highlighted: isHighlighted,
                    direct: isDirect,
                    dimmed: isDimmed,
                  });

                  const svgSource = { ...sourcePos, y: toSvgY(sourcePos.y) };
                  const svgTarget = { ...targetPos, y: toSvgY(targetPos.y) };
                  const svgLayout = {
                    ...layout,
                    topRailY: toSvgY(layout.topRailY),
                    bottomRailY: toSvgY(layout.bottomRailY),
                  };
                  const path = getExperimentEdgePath(category, svgSource, svgTarget, svgLayout);

                  return (
                    <path
                      key={key}
                      d={path}
                      fill="none"
                      stroke={
                        isHighlighted
                          ? focusStroke
                          : category === 'backward'
                            ? backwardStroke
                            : forwardStroke
                      }
                      strokeWidth={presentation.strokeWidth}
                      strokeOpacity={presentation.opacity}
                      strokeDasharray={presentation.dashed ? '5,4' : undefined}
                      strokeLinecap="round"
                      strokeLinejoin="round"
                      markerEnd={presentation.showArrow ? 'url(#experiment-arrow)' : undefined}
                      style={{ transition: 'stroke-opacity 150ms ease, stroke 150ms ease' }}
                    />
                  );
                })}

                {/* Node cards. */}
                {model.nodes.map((node) => {
                  const position = layout.positions.get(node.id);
                  if (!position) return null;
                  const svgY = toSvgY(position.y);
                  const color = getHealthColor(node.health, isDark);
                  const isSelected = selectedNodeId === node.id;
                  const isHighlighted = chain.highlightedNodeIds.has(node.id);
                  const isDimmed = hasFocus && !isHighlighted;
                  const cardX = position.x - CARD_WIDTH / 2;
                  const cardY = svgY - CARD_HEIGHT / 2;

                  return (
                    <g
                      key={node.id}
                      transform={`translate(${cardX}, ${cardY})`}
                      opacity={isDimmed ? 0.32 : 1}
                      cursor="pointer"
                      onClick={() => onSelectNode?.(isSelected ? null : node.id)}
                      onMouseEnter={() => setHoveredNodeId(node.id)}
                      onMouseLeave={() => setHoveredNodeId(null)}
                      style={{ transition: 'opacity 140ms ease' }}
                    >
                      <rect
                        x={0}
                        y={0}
                        width={CARD_WIDTH}
                        height={CARD_HEIGHT}
                        rx={8}
                        fill={
                          isSelected
                            ? isDark
                              ? '#1e293b'
                              : '#f0f9ff'
                            : isDark
                              ? '#0f172a'
                              : '#ffffff'
                        }
                        stroke={isSelected ? focusStroke : isHighlighted ? color : neutralBorder}
                        strokeWidth={isSelected ? 2 : 1.25}
                      />
                      <g transform="translate(18, 20)">
                        <TypeMarker itemType={node.itemType} color={color} />
                      </g>
                      <text
                        x={32}
                        y={24}
                        fontSize="11.5"
                        fontWeight="700"
                        fill={isSelected ? focusStroke : isDark ? '#f1f5f9' : '#0f172a'}
                      >
                        {node.title.length > 22 ? `${node.title.slice(0, 21)}…` : node.title}
                      </text>
                      <g transform="translate(18, 44)">
                        <circle cx={0} cy={-3} r={4} fill={color} />
                        <text x={10} y={0} fontSize="9.5" fontWeight="700" fill={color}>
                          {getHealthLabel(node.health)}
                        </text>
                      </g>
                      <text x={18} y={66} fontSize="9.5" fill={isDark ? '#94a3b8' : '#64748b'}>
                        {(node.summary ?? '').length > 26
                          ? `${(node.summary ?? '').slice(0, 25)}…`
                          : (node.summary ?? '')}
                      </text>
                    </g>
                  );
                })}
              </svg>
            </Box>
          </Box>
        </Box>
      )}
    </Paper>
  );
}
