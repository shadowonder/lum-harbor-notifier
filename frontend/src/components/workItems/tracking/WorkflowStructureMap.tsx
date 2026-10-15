import { useEffect, useMemo, useRef, useState } from 'react';
import { Box, ButtonBase, Paper, Typography, useTheme } from '@mui/material';
import type { Theme } from '@mui/material/styles';
import type {
  TrackingStepState,
  WorkItemTracking,
} from '../../../api/tracking/trackingProductApi.ts';
import { stepStateLabel } from '../../../tracking/trackingPresentation.ts';
import {
  buildWorkflowStructure,
  focusSet,
  type StructureNode,
} from '../../../tracking/workflowStructure.ts';

/**
 * Workflow structure: a single stage axis representing the WorkItem's single
 * workflow definition with its real ordered steps as columns; every TrackedItem
 * sits at the column of its backend-reported current step; lines mean parent / child
 * hierarchy only.
 */

const MIN_COL = 156;
const PAD = 8;
const STEP_HEADER_H = 34;
const NODE_H = 48;
const ROW_GAP = 7;
const TRUNK_GAP = 30;

const stateAccent = (theme: Theme, state: TrackingStepState | null): string => {
  switch (state) {
    case 'COMPLETED':
      return theme.palette.success.main;
    case 'IN_PROGRESS':
      return theme.palette.primary.main;
    case 'WAITING':
      return theme.palette.warning.main;
    case 'BLOCKED':
    case 'FAILED':
      return theme.palette.error.main;
    default:
      return theme.palette.text.disabled;
  }
};

/** Item type marker: shape only, never colour-coded state. */
function TypeMarker({
  type,
  complete,
  color,
}: {
  type: string | null;
  complete: boolean;
  color: string;
}) {
  const paths: Record<string, string> = {
    STORY: '<circle cx="7" cy="7" r="5"/>',
    TASK: '<rect x="2" y="2" width="10" height="10"/>',
    DEFECT: '<path d="M7 1.5 13 12H1Z"/>',
    GATE: '<path d="M7 1 13 7 7 13 1 7Z"/>',
  };
  const markup =
    paths[(type ?? '').toUpperCase()] ?? '<path d="M2 2h10v10H2z" stroke-dasharray="2 2"/>';
  return (
    <Box
      component="svg"
      viewBox="0 0 14 14"
      aria-hidden
      dangerouslySetInnerHTML={{ __html: markup }}
      sx={{
        width: 14,
        height: 14,
        flex: '0 0 auto',
        mt: '1px',
        fill: complete ? color : 'transparent',
        stroke: color,
        strokeWidth: 1.6,
      }}
    />
  );
}

function NodeCard({
  node,
  selected,
  dimmed,
  width,
  height,
  left,
  top,
  onSelect,
}: {
  node: StructureNode;
  selected: boolean;
  dimmed: boolean;
  width: number;
  height: number;
  left: number;
  top: number;
  onSelect: () => void;
}) {
  const theme = useTheme();
  const accent = stateAccent(theme, node.state);
  const stateText = node.state ? stepStateLabel(node.state).toUpperCase() : 'NO CURRENT STEP';
  return (
    <ButtonBase
      data-testid="structure-node"
      data-state={node.state ?? 'NONE'}
      aria-pressed={selected}
      aria-label={`${node.item.title}, ${node.item.itemType ?? 'item'}, ${stateText}${node.step ? ` at ${node.step.title}` : ''}`}
      title={`${node.item.title}${node.item.itemType ? ` · ${node.item.itemType}` : ''}`}
      onClick={onSelect}
      sx={{
        position: 'absolute',
        left,
        top,
        width,
        height,
        display: 'block',
        textAlign: 'left',
        px: 1,
        py: 0.65,
        pl: 1.25,
        borderRadius: 1,
        border: 1,
        borderColor: selected ? 'primary.main' : 'divider',
        borderLeft: `3px solid ${accent}`,
        bgcolor: 'background.paper',
        outline: selected ? `2px solid ${theme.palette.primary.main}` : 'none',
        outlineOffset: 1,
        opacity: dimmed ? 0.45 : 1,
        transition: 'opacity 120ms ease',
        overflow: 'hidden',
        zIndex: 2,
        '&:hover': { bgcolor: 'action.hover' },
      }}
    >
      <Box sx={{ display: 'flex', gap: 0.75, alignItems: 'flex-start' }}>
        <TypeMarker
          type={node.item.itemType}
          complete={node.item.lifecycleState === 'COMPLETED' || !!node.item.progression?.complete}
          color={accent}
        />
        <Typography
          component="span"
          sx={{
            fontSize: 12,
            fontWeight: 650,
            lineHeight: 1.25,
            overflow: 'hidden',
            display: '-webkit-box',
            WebkitLineClamp: 1,
            WebkitBoxOrient: 'vertical',
            minWidth: 0,
          }}
        >
          {node.item.title}
        </Typography>
      </Box>
      <Typography
        component="div"
        noWrap
        sx={{ fontSize: 10, fontWeight: 700, letterSpacing: '0.04em', color: accent, mt: 0.25 }}
      >
        {stateText}
        {node.step ? ` · ${node.step.title}` : ''}
      </Typography>
    </ButtonBase>
  );
}

export default function WorkflowStructureMap({ tracking }: { tracking: WorkItemTracking }) {
  const structure = useMemo(() => buildWorkflowStructure(tracking), [tracking]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const [innerWidth, setInnerWidth] = useState(720);
  const theme = useTheme();

  useEffect(() => {
    const element = scrollRef.current;
    if (!element || typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(() => setInnerWidth(Math.max(0, element.clientWidth - 28)));
    observer.observe(element);
    setInnerWidth(Math.max(0, element.clientWidth - 28));
    return () => observer.disconnect();
  }, []);

  const focus = useMemo(
    () =>
      selectedId && tracking.trackedItems.some((i) => i.id === selectedId)
        ? focusSet(tracking, selectedId)
        : null,
    [tracking, selectedId],
  );
  const select = (id: string) => setSelectedId((current) => (current === id ? null : id));

  if (!structure.workflow) {
    return (
      <Paper variant="outlined" component="section" aria-labelledby="workflow-structure-heading">
        <Box
          sx={{
            px: 1.5,
            py: 1,
            borderBottom: 1,
            borderColor: 'divider',
            bgcolor: 'surface.subtle',
          }}
        >
          <Typography id="workflow-structure-heading" variant="h2">
            Workflow structure
          </Typography>
        </Box>
        <Box sx={{ p: 3, textAlign: 'center' }}>
          <Typography variant="body2" color="text.secondary">
            No workflow assigned to this work item.
          </Typography>
        </Box>
      </Paper>
    );
  }

  const steps = structure.steps;
  const colW = Math.max(MIN_COL, Math.floor(innerWidth / Math.max(1, steps.length)));
  const width = colW * steps.length;
  const nodeW = colW - PAD * 2;

  const byColumn = new Map<number, StructureNode[]>();
  for (const node of structure.nodes)
    byColumn.set(node.column, [...(byColumn.get(node.column) ?? []), node]);
  const maxAbove = Math.max(
    1,
    ...[...byColumn.values()].map((nodes) => Math.ceil(nodes.length / 2)),
  );
  const maxBelow = Math.max(
    1,
    ...[...byColumn.values()].map((nodes) => Math.floor(nodes.length / 2)),
  );
  const trunkY = STEP_HEADER_H + maxAbove * (NODE_H + ROW_GAP) + TRUNK_GAP / 2;
  const boxes = new Map<string, { left: number; top: number; height: number }>();
  for (const [column, nodes] of byColumn) {
    nodes.forEach((node, index) => {
      const top =
        index % 2 === 0
          ? trunkY - TRUNK_GAP / 2 - NODE_H - Math.floor(index / 2) * (NODE_H + ROW_GAP)
          : trunkY + TRUNK_GAP / 2 + Math.floor(index / 2) * (NODE_H + ROW_GAP);
      boxes.set(node.item.id, { left: column * colW + PAD, top, height: NODE_H });
    });
  }
  const layoutHeight = trunkY + TRUNK_GAP / 2 + maxBelow * (NODE_H + ROW_GAP) + PAD;

  const edges: Array<{ id: string; d: string; emphasized: boolean }> = [];
  const childrenOf = new Map<string, StructureNode[]>();
  for (const node of structure.nodes) {
    if (node.parentId) {
      childrenOf.set(node.parentId, [...(childrenOf.get(node.parentId) ?? []), node]);
    }
  }

  const dependencyEdges = (tracking.relations ?? []).flatMap((relation) => {
    const from = boxes.get(relation.fromTrackedItemId);
    const to = boxes.get(relation.toTrackedItemId);
    if (!from || !to) return [];
    const fromX = from.left + nodeW / 2;
    const toX = to.left + nodeW / 2;
    const fromY = from.top + (from.top < trunkY ? NODE_H : 0);
    const toY = to.top + (to.top < trunkY ? NODE_H : 0);
    const routeY = trunkY + (from.top < trunkY && to.top < trunkY ? -6 : 6);
    return [
      {
        id: `dependency:${relation.fromTrackedItemId}>${relation.toTrackedItemId}`,
        d: `M${fromX},${fromY} V${routeY} H${toX} V${toY}`,
        emphasized:
          !!focus && focus.has(relation.fromTrackedItemId) && focus.has(relation.toTrackedItemId),
      },
    ];
  });
  for (const [parentId, kids] of childrenOf) {
    const parent = boxes.get(parentId);
    const parentNode = structure.nodes.find((n) => n.item.id === parentId);
    if (!parent || !parentNode) continue;
    const minLeft = Math.min(parent.left, ...kids.map((k) => boxes.get(k.item.id)!.left));
    const trunkX = minLeft - 10 + 3 * Math.min(parentNode.depth, 3);
    const py = parent.top + parent.height / 2;
    for (const kid of kids) {
      const box = boxes.get(kid.item.id)!;
      edges.push({
        id: `${parentId}>${kid.item.id}`,
        d: `M${parent.left},${py} H${trunkX} V${box.top + box.height / 2} H${box.left}`,
        emphasized: !!focus && focus.has(parentId) && focus.has(kid.item.id),
      });
    }
  }

  return (
    <Paper variant="outlined" component="section" aria-labelledby="workflow-structure-heading">
      <Box
        sx={{ px: 1.5, py: 1, borderBottom: 1, borderColor: 'divider', bgcolor: 'surface.subtle' }}
      >
        <Typography id="workflow-structure-heading" variant="h2">
          Workflow structure
        </Typography>
        <Typography variant="caption" color="text.secondary">
          Stage axis shows ordered steps; items sit at their current step with independent states.
          Solid branches show hierarchy. Dashed arrows show DEPENDS ON; the inverse reads as BLOCKS.
        </Typography>
      </Box>
      <Box
        ref={scrollRef}
        sx={{ p: 1.75, overflowX: 'auto' }}
        onClick={(event) => {
          if (event.target === event.currentTarget) setSelectedId(null);
        }}
      >
        <Box
          component="section"
          aria-label={`${structure.workflow.name} workflow`}
          data-testid="workflow-lane"
          sx={{ mb: 1.5 }}
        >
          <Box sx={{ display: 'flex', alignItems: 'baseline', gap: 0.75, px: 0.5, pb: 0.5 }}>
            <Typography variant="h3">{structure.workflow.name}</Typography>
            <Typography variant="caption" color="text.secondary">
              v{structure.workflow.version}
            </Typography>
          </Box>
          <Box
            sx={{
              position: 'relative',
              width,
              height: layoutHeight,
              border: 1,
              borderColor: 'divider',
              borderRadius: 1,
              bgcolor: 'surface.subtle',
            }}
          >
            {steps.map((step, index) => (
              <Box
                key={step.id}
                data-testid="structure-step-header"
                sx={{ position: 'absolute', left: index * colW, top: 0, bottom: 0, width: colW }}
              >
                <Typography
                  component="div"
                  noWrap
                  sx={{
                    px: `${PAD}px`,
                    height: STEP_HEADER_H,
                    lineHeight: `${STEP_HEADER_H}px`,
                    fontSize: 11,
                    fontWeight: 700,
                    letterSpacing: '0.06em',
                    textTransform: 'uppercase',
                    color: 'text.secondary',
                    borderBottom: 1,
                    borderColor: 'divider',
                  }}
                  title={step.title}
                >
                  {step.title}
                  {step.terminal ? ' ⚑' : ''}
                </Typography>
                {index > 0 && (
                  <Box
                    aria-hidden
                    sx={{
                      position: 'absolute',
                      left: 0,
                      top: STEP_HEADER_H,
                      bottom: 0,
                      borderLeft: 1,
                      borderColor: 'divider',
                      borderLeftStyle: 'dotted',
                      opacity: 0.7,
                    }}
                  />
                )}
              </Box>
            ))}
            <Box
              component="svg"
              aria-hidden
              width={width}
              height={layoutHeight}
              sx={{ position: 'absolute', inset: 0, zIndex: 1, pointerEvents: 'none' }}
            >
              <defs>
                <marker
                  id="dependency-arrow"
                  markerWidth="6"
                  markerHeight="6"
                  refX="5"
                  refY="3"
                  orient="auto"
                >
                  <path d="M0,0 L6,3 L0,6 Z" fill={theme.palette.text.secondary} />
                </marker>
              </defs>
              <path
                data-testid="workflow-trunk"
                d={`M0,${trunkY} H${width}`}
                stroke={theme.palette.text.secondary}
                strokeWidth="2"
                opacity="0.55"
              />
              {steps.map((_, index) => (
                <circle
                  key={`trunk-${index}`}
                  cx={index * colW + colW / 2}
                  cy={trunkY}
                  r="3.5"
                  fill={theme.palette.background.paper}
                  stroke={theme.palette.text.secondary}
                  strokeWidth="1.5"
                />
              ))}
              {edges.map((edge) => (
                <path
                  key={edge.id}
                  d={edge.d}
                  fill="none"
                  data-testid="hierarchy-edge"
                  stroke={
                    edge.emphasized ? theme.palette.primary.main : theme.palette.text.disabled
                  }
                  strokeWidth={edge.emphasized ? 2 : 1.25}
                  opacity={focus && !edge.emphasized ? 0.3 : 1}
                />
              ))}
              {dependencyEdges.map((edge) => (
                <path
                  key={edge.id}
                  d={edge.d}
                  fill="none"
                  data-testid="dependency-edge"
                  stroke={
                    edge.emphasized ? theme.palette.primary.main : theme.palette.text.secondary
                  }
                  strokeWidth={edge.emphasized ? 2 : 1.1}
                  strokeDasharray="4 3"
                  markerEnd="url(#dependency-arrow)"
                  opacity={focus && !edge.emphasized ? 0.18 : 0.52}
                />
              ))}
            </Box>
            {structure.nodes.map((node) => {
              const box = boxes.get(node.item.id)!;
              return (
                <NodeCard
                  key={node.item.id}
                  node={node}
                  selected={selectedId === node.item.id}
                  dimmed={!!focus && !focus.has(node.item.id)}
                  width={nodeW}
                  height={box.height}
                  left={box.left}
                  top={box.top}
                  onSelect={() => select(node.item.id)}
                />
              );
            })}
          </Box>
        </Box>
      </Box>
    </Paper>
  );
}
