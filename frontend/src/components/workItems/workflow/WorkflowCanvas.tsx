import { useMemo, useState } from 'react';
import { Box, Paper, Typography } from '@mui/material';
import { useTheme } from '@mui/material/styles';
import { WORKFLOW_SECTIONS, WORKFLOW_STAGES, type WorkflowModel } from './workflowTypes.ts';
import {
  computeDependencyChain,
  computeDirectRelations,
  DEFAULT_CANVAS_DIMENSIONS,
  getBranchConnectorPath,
  getDependencyEdgePresentation,
  getDependencyRoutePoints,
  getHealthColor,
  getItemTypeLabel,
  getNodeCoordinates,
  getStageBounds,
  getTargetMarkerCoordinates,
  getRoundedOrthogonalPath,
  getWorkflowEdgePalette,
  WORKFLOW_LINE_HIERARCHY,
} from './workflowLayout.ts';
import WorkflowNodeShape from './WorkflowNodeShape.tsx';

interface WorkflowCanvasProps {
  model: WorkflowModel;
  selectedNodeId?: string | null;
  onSelectNode?: (nodeId: string | null) => void;
}

const CARD_W = 126;
const CARD_H = 40;

export default function WorkflowCanvas({
  model,
  selectedNodeId = null,
  onSelectNode,
}: WorkflowCanvasProps) {
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const dims = DEFAULT_CANVAS_DIMENSIONS;

  const [hoveredNodeId, setHoveredNodeId] = useState<string | null>(null);
  const [hoveredEdgeKey, setHoveredEdgeKey] = useState<string | null>(null);

  // Selecting a node reveals its full upstream/downstream dependency chain
  // (direct and transitive). Hovering only reveals immediate neighbors, so
  // moving the pointer across the graph does not repeatedly recompute and
  // flash the entire transitive chain.
  const { highlightedNodeIds, highlightedEdgeKeys, directEdgeKeys } = useMemo(() => {
    const empty = {
      highlightedNodeIds: new Set<string>(),
      highlightedEdgeKeys: new Set<string>(),
      directEdgeKeys: new Set<string>(),
    };
    if (model.nodes.length === 0) return empty;
    if (selectedNodeId) {
      return computeDependencyChain(selectedNodeId, model.edges);
    }
    if (hoveredNodeId) {
      const direct = computeDirectRelations(hoveredNodeId, model.edges);
      return { ...direct, directEdgeKeys: direct.highlightedEdgeKeys };
    }
    return empty;
  }, [selectedNodeId, hoveredNodeId, model.edges, model.nodes.length]);

  const hasActiveFocus = highlightedNodeIds.size > 0;

  const currentStageBounds = getStageBounds(model.trunk.currentStageId, dims);
  const trunkHealthColor = getHealthColor(model.trunk.health, isDark);
  const neutralBorderColor = isDark ? 'rgba(148, 163, 184, 0.18)' : 'rgba(203, 213, 225, 0.6)';
  const stageHeaderColor = isDark ? '#94a3b8' : '#64748b';
  const sectionBgColor = isDark ? 'rgba(30, 41, 59, 0.3)' : 'rgba(241, 245, 249, 0.45)';
  const edgePalette = getWorkflowEdgePalette(isDark);

  return (
    <Paper
      variant="outlined"
      sx={{
        p: 1.5,
        borderRadius: 1,
        bgcolor: 'background.paper',
        overflow: 'hidden',
      }}
    >
      <Box sx={{ width: '100%', overflowX: 'auto' }}>
        <svg
          viewBox={`0 0 ${dims.width} ${dims.height}`}
          style={{
            minWidth: 760,
            width: '100%',
            height: 'auto',
            display: 'block',
            overflow: 'visible',
          }}
          aria-label={`Workflow graph for ${model.title}`}
        >
          <defs>
            <marker
              id={`arrow-canvas-${model.workItemId}`}
              viewBox="0 0 6 6"
              refX="5.5"
              refY="3"
              markerWidth="3.5"
              markerHeight="3.5"
              orient="auto-start-reverse"
            >
              <path d="M 0 0 L 6 3 L 0 6 z" fill={edgePalette.dependencyFocus} />
            </marker>
          </defs>

          {/* 1. Stage section background stripes & vertical boundaries */}
          {WORKFLOW_SECTIONS.map((section, idx) => {
            const firstStage = section.stageIds[0];
            const lastStage = section.stageIds[section.stageIds.length - 1];
            const startBounds = getStageBounds(firstStage, dims);
            const endBounds = getStageBounds(lastStage, dims);
            const secWidth = endBounds.endX - startBounds.startX;

            return (
              <g key={section.id}>
                {idx % 2 === 1 && (
                  <rect
                    x={startBounds.startX}
                    y={6}
                    width={secWidth}
                    height={dims.height - 12}
                    fill={sectionBgColor}
                    rx={6}
                  />
                )}
                {/* Section title */}
                <text
                  x={startBounds.startX + secWidth / 2}
                  y={18}
                  textAnchor="middle"
                  fill={stageHeaderColor}
                  fontSize="9.5"
                  fontWeight="700"
                  letterSpacing="0.1em"
                  style={{ userSelect: 'none' }}
                >
                  {section.label}
                </text>
                {/* Boundary line between sections */}
                {idx < WORKFLOW_SECTIONS.length - 1 && (
                  <line
                    x1={endBounds.endX}
                    y1={8}
                    x2={endBounds.endX}
                    y2={dims.height - 10}
                    stroke={neutralBorderColor}
                    strokeWidth={1.25}
                    strokeDasharray="3,3"
                  />
                )}
              </g>
            );
          })}

          {/* Sub-stage headers */}
          {WORKFLOW_STAGES.map((stage) => {
            const bounds = getStageBounds(stage.id, dims);
            const isCurrentStage = stage.id === model.trunk.currentStageId;
            return (
              <g key={stage.id}>
                <text
                  x={bounds.centerX}
                  y={34}
                  textAnchor="middle"
                  fill={
                    isCurrentStage
                      ? isDark
                        ? '#38bdf8'
                        : '#0284c7'
                      : isDark
                        ? '#94a3b8'
                        : '#64748b'
                  }
                  fontSize="9"
                  fontWeight={isCurrentStage ? '700' : '500'}
                  style={{ userSelect: 'none' }}
                >
                  {stage.label}
                </text>
                {/* Subtle stage column vertical tick */}
                <line
                  x1={bounds.centerX}
                  y1={40}
                  x2={bounds.centerX}
                  y2={46}
                  stroke={isCurrentStage ? (isDark ? '#38bdf8' : '#0284c7') : neutralBorderColor}
                  strokeWidth={1}
                />
              </g>
            );
          })}

          {/* 2. Trunk Baseline Path */}
          {/* Background full baseline path */}
          <line
            x1={dims.paddingX}
            y1={dims.trunkY}
            x2={dims.width - dims.paddingX}
            y2={dims.trunkY}
            stroke={edgePalette.baseline}
            strokeWidth={WORKFLOW_LINE_HIERARCHY.baselineWidth}
            strokeLinecap="round"
          />

          {/* Active colored trunk up to current stage */}
          <line
            x1={dims.paddingX}
            y1={dims.trunkY}
            x2={currentStageBounds.centerX}
            y2={dims.trunkY}
            stroke={trunkHealthColor}
            strokeWidth={WORKFLOW_LINE_HIERARCHY.baselineWidth}
            opacity={0.76}
            strokeLinecap="round"
          />

          {/* Current progress indicator on trunk */}
          <circle
            cx={currentStageBounds.centerX}
            cy={dims.trunkY}
            r={7}
            fill={isDark ? '#0f172a' : '#ffffff'}
            stroke={trunkHealthColor}
            strokeWidth={2}
          />
          <circle
            cx={currentStageBounds.centerX}
            cy={dims.trunkY}
            r={3.5}
            fill={trunkHealthColor}
          />

          {/* 3. Empty state Feature Baseline Graph Workspace (Production Mode) */}
          {model.nodes.length === 0 && (
            <g>
              <rect
                x={dims.paddingX + 32}
                y={dims.trunkY + 28}
                width={dims.width - dims.paddingX * 2 - 64}
                height={52}
                fill={isDark ? 'rgba(15, 23, 42, 0.7)' : 'rgba(248, 250, 252, 0.85)'}
                stroke={neutralBorderColor}
                strokeWidth={1}
                rx={8}
              />
              <text
                x={dims.width / 2}
                y={dims.trunkY + 48}
                textAnchor="middle"
                fill={isDark ? '#f1f5f9' : '#0f172a'}
                fontSize="10.5"
                fontWeight="700"
              >
                Feature Baseline Trunk ·{' '}
                {WORKFLOW_STAGES.find((s) => s.id === model.trunk.currentStageId)?.label ?? 'Dev'}{' '}
                Stage
              </text>
              <text
                x={dims.width / 2}
                y={dims.trunkY + 64}
                textAnchor="middle"
                fill={isDark ? '#94a3b8' : '#64748b'}
                fontSize="8.5"
              >
                No branch items connected yet. Branch items will appear here when authoritative
                WorkItem relations are available.
              </text>
            </g>
          )}

          {/* 4. Branch connector paths (trunk to card nodes) */}
          {model.nodes.map((node) => {
            const { x, y } = getNodeCoordinates(node, dims);
            const isDimmed = hasActiveFocus && !highlightedNodeIds.has(node.id);

            return (
              <path
                key={`canvas-branch-${node.id}`}
                d={getBranchConnectorPath(x, y, dims.trunkY)}
                stroke={edgePalette.attachment}
                strokeWidth={WORKFLOW_LINE_HIERARCHY.attachmentWidth}
                opacity={isDimmed ? 0.12 : 0.74}
                fill="none"
              />
            );
          })}

          {/* 5. Dependency curves between card nodes */}
          {model.edges.map((edge) => {
            const sourceNode = model.nodes.find((n) => n.id === edge.from);
            const targetNode = model.nodes.find((n) => n.id === edge.to);
            if (!sourceNode || !targetNode) return null;

            const edgeKey = `${edge.from}->${edge.to}`;

            const isEdgeHighlighted = highlightedEdgeKeys.has(edgeKey);
            const isDimmed = hasActiveFocus && !isEdgeHighlighted;
            const isEdgeHovered = hoveredEdgeKey === edgeKey;
            const presentation = getDependencyEdgePresentation({
              highlighted: isEdgeHighlighted,
              direct: directEdgeKeys.has(edgeKey),
              hovered: isEdgeHovered,
              dimmed: isDimmed,
            });
            const route = getDependencyRoutePoints(sourceNode, targetNode, dims, CARD_W);
            const path = getRoundedOrthogonalPath(route);

            return (
              <g key={`canvas-edge-${edgeKey}`}>
                <path
                  d={path}
                  stroke={
                    isEdgeHighlighted || isEdgeHovered
                      ? edgePalette.dependencyFocus
                      : edgePalette.dependency
                  }
                  strokeWidth={presentation.strokeWidth}
                  opacity={presentation.opacity}
                  fill="none"
                  strokeLinecap="round"
                  strokeLinejoin="round"
                  markerEnd={
                    presentation.showArrow ? `url(#arrow-canvas-${model.workItemId})` : undefined
                  }
                  style={{ transition: 'opacity 150ms ease, stroke 150ms ease' }}
                />
                <path
                  d={path}
                  stroke="transparent"
                  strokeWidth={8}
                  fill="none"
                  pointerEvents="stroke"
                  onMouseEnter={() => setHoveredEdgeKey(edgeKey)}
                  onMouseLeave={() => setHoveredEdgeKey(null)}
                />
              </g>
            );
          })}

          {/* 6. Ghost Target Marker Cards */}
          {model.nodes.map((node) => {
            const targetCoord = getTargetMarkerCoordinates(node, dims);
            if (!targetCoord) return null;
            const fromCoord = getNodeCoordinates(node, dims);
            const isDimmed = hasActiveFocus && !highlightedNodeIds.has(node.id);

            const cardX = targetCoord.x - CARD_W / 2;
            const cardY = targetCoord.y - CARD_H / 2;

            return (
              <g key={`canvas-target-${node.id}`} opacity={isDimmed ? 0.15 : 0.7}>
                <line
                  x1={fromCoord.x + CARD_W / 2}
                  y1={fromCoord.y}
                  x2={cardX}
                  y2={targetCoord.y}
                  stroke={getHealthColor(node.health, isDark)}
                  strokeWidth={1.25}
                  strokeDasharray="2,2"
                />
                <rect
                  x={cardX}
                  y={cardY}
                  width={CARD_W}
                  height={CARD_H}
                  rx={6}
                  fill="none"
                  stroke={getHealthColor(node.health, isDark)}
                  strokeWidth={1.25}
                  strokeDasharray="3,3"
                />
                <WorkflowNodeShape
                  x={cardX + 16}
                  y={targetCoord.y}
                  itemType={node.itemType}
                  color={getHealthColor(node.health, isDark)}
                  radius={5}
                  isGhost
                />
                <text
                  x={cardX + 30}
                  y={targetCoord.y + 4}
                  fill={isDark ? '#94a3b8' : '#64748b'}
                  fontSize="7.5"
                  fontWeight="600"
                >
                  {node.title.slice(0, 14)} (Target)
                </text>
              </g>
            );
          })}

          {/* 7. Actual Branch Card-Nodes (ERD / Flow-Chart style) */}
          {model.nodes.map((node) => {
            const { x, y } = getNodeCoordinates(node, dims);
            const nodeColor = getHealthColor(node.health, isDark);
            const isSelected = selectedNodeId === node.id;
            const isNodeHighlighted = highlightedNodeIds.has(node.id);
            const isDimmed = hasActiveFocus && !isNodeHighlighted;

            const cardX = x - CARD_W / 2;
            const cardY = y - CARD_H / 2;

            return (
              <g
                key={`canvas-node-card-${node.id}`}
                cursor="pointer"
                opacity={isDimmed ? 0.2 : 1}
                onClick={() => onSelectNode?.(isSelected ? null : node.id)}
                onMouseEnter={() => setHoveredNodeId(node.id)}
                onMouseLeave={() => setHoveredNodeId(null)}
                style={{ transition: 'opacity 140ms ease' }}
              >
                {/* Card Background Container */}
                <rect
                  x={cardX}
                  y={cardY}
                  width={CARD_W}
                  height={CARD_H}
                  rx={6}
                  fill={
                    isDark
                      ? isSelected
                        ? '#1e293b'
                        : '#0f172a'
                      : isSelected
                        ? '#f0f9ff'
                        : '#ffffff'
                  }
                  stroke={
                    isSelected
                      ? isDark
                        ? '#38bdf8'
                        : '#0284c7'
                      : isNodeHighlighted
                        ? nodeColor
                        : isDark
                          ? 'rgba(148, 163, 184, 0.25)'
                          : 'rgba(203, 213, 225, 0.9)'
                  }
                  strokeWidth={isSelected ? 2 : 1.25}
                  filter={isSelected ? 'drop-shadow(0 2px 6px rgba(2, 132, 199, 0.25))' : undefined}
                />

                {/* Left shape/color visual indicator */}
                <WorkflowNodeShape
                  x={cardX + 16}
                  y={y}
                  itemType={node.itemType}
                  color={nodeColor}
                  radius={5.5}
                  isHighlighted={isSelected || isNodeHighlighted}
                  isResolved={node.isResolved}
                />

                {/* Bold Item Title */}
                <text
                  x={cardX + 30}
                  y={y - 3}
                  fill={
                    isSelected ? (isDark ? '#38bdf8' : '#0284c7') : isDark ? '#f1f5f9' : '#0f172a'
                  }
                  fontSize="8.5"
                  fontWeight="700"
                  style={{ userSelect: 'none' }}
                >
                  {node.title.length > 15 ? `${node.title.slice(0, 14)}…` : node.title}
                </text>

                {/* Muted Subtitle / Item Type */}
                <text
                  x={cardX + 30}
                  y={y + 10}
                  fill={isDark ? '#94a3b8' : '#64748b'}
                  fontSize="7"
                  style={{ userSelect: 'none' }}
                >
                  {node.summary
                    ? node.summary.length > 18
                      ? `${node.summary.slice(0, 17)}…`
                      : node.summary
                    : getItemTypeLabel(node.itemType)}
                </text>
              </g>
            );
          })}
        </svg>
      </Box>

      {/* Footer bar with baseline summary */}
      <Box
        sx={{
          mt: 1.5,
          pt: 1.5,
          borderTop: 1,
          borderColor: 'divider',
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          flexWrap: 'wrap',
          gap: 1.5,
        }}
      >
        <Typography variant="caption" color="text.secondary">
          Baseline Trunk: <strong>{model.trunk.label}</strong> ({model.trunk.progressPercent}%)
        </Typography>
        <Typography variant="caption" color="text.secondary">
          {model.nodes.length > 0 ? (
            <>
              {model.nodes.length} branch {model.nodes.length === 1 ? 'item' : 'items'} ·{' '}
              {model.edges.length} {model.edges.length === 1 ? 'dependency' : 'dependencies'}
            </>
          ) : (
            'Zero branch items linked · Baseline only'
          )}
        </Typography>
      </Box>
    </Paper>
  );
}
