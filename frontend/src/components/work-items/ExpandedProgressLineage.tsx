import { useEffect, useMemo, useState } from 'react';
import {
  BaseEdge,
  Handle,
  Position,
  ReactFlow,
  type Edge,
  type EdgeProps,
  type Node,
  type NodeProps,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { Box, CircularProgress, Typography, useTheme } from '@mui/material';
import type { ProgressLinkDto, ProgressNavigationDto, ProgressNodeDto } from '../../api/workItems';
import { layoutProgressGraph, type ExpandedProgressLayout } from '../../presentation/workItems/elkProgressLayout';
import { resolveNodeShape } from '../../presentation/workItems/nodeTypePresentation';
import { statusColorToken, statusPresentation } from '../../presentation/workItems/statusPresentation';
import type { ProgressLineageViewModel } from '../../presentation/workItems/workItemViewModels';

interface ProgressNodeData extends Record<string, unknown> {
  node: ProgressNodeDto;
  dimmed: boolean;
  navigate: (navigation: ProgressNavigationDto) => void;
}

interface ProgressEdgeData extends Record<string, unknown> {
  link: ProgressLinkDto;
  points: { x: number; y: number }[];
  dimmed: boolean;
}

type ProgressFlowNode = Node<ProgressNodeData, 'progressNode'>;
type ProgressFlowEdge = Edge<ProgressEdgeData, 'progressEdge'>;

function StatusShape({ node }: { node: ProgressNodeDto }) {
  const presentation = statusPresentation[node.status];
  const shape = resolveNodeShape(node.nodeType, node.status);
  return <Box sx={{ display: 'grid', placeItems: 'center', width: 18, height: 18, flexShrink: 0, color: statusColorToken(node.status), border: presentation.fill === 'solid' ? 0 : 1.5, borderColor: 'currentColor', borderRadius: shape === 'circle' ? '50%' : shape === 'roundedSquare' ? 0.7 : shape === 'target' ? 0 : 0, transform: shape === 'target' ? 'rotate(45deg)' : undefined, clipPath: shape === 'triangle' ? 'polygon(50% 0, 100% 100%, 0 100%)' : undefined, bgcolor: presentation.fill === 'solid' ? statusColorToken(node.status) : 'background.paper' }}>
    <Typography component="span" sx={{ fontSize: 10, lineHeight: 1, fontWeight: 850, color: presentation.fill === 'solid' ? 'background.paper' : 'inherit', transform: shape === 'target' ? 'rotate(-45deg)' : undefined }}>{presentation.showSymbol ? presentation.symbol : ''}</Typography>
  </Box>;
}

function ProgressNodeCard({ data, selected }: NodeProps<ProgressFlowNode>) {
  const { node } = data;
  const presentation = statusPresentation[node.status];
  return <>
    <Handle type="target" position={Position.Left} style={{ opacity: 0, pointerEvents: 'none' }} />
    <Box
      component="button"
      type="button"
      aria-label={`${presentation.label}: ${node.reason ?? node.title ?? 'Progress node'}`}
      onClick={() => node.navigation && data.navigate(node.navigation)}
      sx={{
        display: 'flex', width: '100%', height: '100%', minWidth: 0, px: 1.05, py: 0.65,
        alignItems: 'center', gap: 0.8, overflow: 'hidden', color: 'text.primary', textAlign: 'left',
        border: 1, borderColor: selected ? 'primary.main' : node.attention ? statusColorToken(node.status) : 'divider',
        borderRadius: 1, bgcolor: 'background.paper', boxShadow: selected ? 1 : 0,
        opacity: data.dimmed ? 0.28 : 1, cursor: node.navigation ? 'pointer' : 'default', transition: 'opacity 120ms ease, border-color 120ms ease',
        '&:hover': { bgcolor: 'surface.subtle', borderColor: statusColorToken(node.status) },
        '&:focus-visible': { outline: '2px solid', outlineColor: 'primary.main', outlineOffset: 2 },
      }}
    >
      <StatusShape node={node} />
      <Box sx={{ minWidth: 0, flex: 1 }}>
        <Typography sx={{ fontSize: 10.5, lineHeight: 1.25, fontWeight: 700 }} noWrap>{node.reason ?? node.title ?? presentation.label}</Typography>
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', fontSize: 8.5, lineHeight: 1.25 }} noWrap>
          {node.title && node.reason ? node.title : presentation.label}{node.total ? ` · ${node.count ?? 0}/${node.total}` : ''}
        </Typography>
      </Box>
    </Box>
    <Handle type="source" position={Position.Right} style={{ opacity: 0, pointerEvents: 'none' }} />
  </>;
}

function roundedOrthogonalPath(points: { x: number; y: number }[], radius = 7) {
  if (points.length < 2) return '';
  let path = `M ${points[0].x} ${points[0].y}`;
  for (let index = 1; index < points.length - 1; index += 1) {
    const previous = points[index - 1];
    const current = points[index];
    const next = points[index + 1];
    const incoming = Math.min(radius, Math.hypot(current.x - previous.x, current.y - previous.y) / 2);
    const outgoing = Math.min(radius, Math.hypot(next.x - current.x, next.y - current.y) / 2);
    const beforeX = current.x - Math.sign(current.x - previous.x) * incoming;
    const beforeY = current.y - Math.sign(current.y - previous.y) * incoming;
    const afterX = current.x + Math.sign(next.x - current.x) * outgoing;
    const afterY = current.y + Math.sign(next.y - current.y) * outgoing;
    path += ` L ${beforeX} ${beforeY} Q ${current.x} ${current.y} ${afterX} ${afterY}`;
  }
  const last = points[points.length - 1];
  return `${path} L ${last.x} ${last.y}`;
}

function ProgressEdge({ data, sourceX, sourceY, targetX, targetY }: EdgeProps<ProgressFlowEdge>) {
  if (!data) return null;
  const rawPoints = data.points.length >= 2 ? data.points : [{ x: sourceX, y: sourceY }, { x: targetX, y: targetY }];
  const points = data.link.open && rawPoints.length >= 2
    ? rawPoints.map((point, index) => {
        if (index !== rawPoints.length - 1) return point;
        const previous = rawPoints[index - 1];
        const distance = Math.hypot(point.x - previous.x, point.y - previous.y);
        if (!distance) return point;
        return { x: point.x - ((point.x - previous.x) / distance) * 14, y: point.y - ((point.y - previous.y) / distance) * 14 };
      })
    : rawPoints;
  const path = roundedOrthogonalPath(points);
  return <BaseEdge path={path} style={{ stroke: data.link.open ? 'var(--lineage-open)' : 'var(--lineage-edge)', strokeWidth: data.link.kind === 'progress' ? 1.8 : 1.6, strokeDasharray: data.link.open ? '5 4' : undefined, opacity: data.dimmed ? 0.16 : data.link.kind === 'progress' ? 0.82 : 0.72, transition: 'opacity 120ms ease' }} />;
}

const nodeTypes = { progressNode: ProgressNodeCard };
const edgeTypes = { progressEdge: ProgressEdge };

function connectedNodeIds(model: ProgressLineageViewModel, startId: string) {
  const downstream = new Set([startId]);
  const upstream = new Set([startId]);
  let frontier = [startId];
  while (frontier.length) {
    const next = model.links.filter((link) => frontier.includes(link.fromNodeId)).map((link) => link.toNodeId).filter((id) => !downstream.has(id));
    next.forEach((id) => downstream.add(id));
    frontier = next;
  }
  frontier = [startId];
  while (frontier.length) {
    const next = model.links.filter((link) => frontier.includes(link.toNodeId)).map((link) => link.fromNodeId).filter((id) => !upstream.has(id));
    next.forEach((id) => upstream.add(id));
    frontier = next;
  }
  return new Set([...downstream, ...upstream]);
}

export default function ExpandedProgressLineage({ model, onNavigate }: { model: ProgressLineageViewModel; onNavigate: (navigation: ProgressNavigationDto) => void }) {
  const theme = useTheme();
  const [layout, setLayout] = useState<ExpandedProgressLayout | null>(null);
  const [hovered, setHovered] = useState<string | null>(null);
  const [selected, setSelected] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    layoutProgressGraph(model).then((result) => { if (active) setLayout(result); });
    return () => { active = false; };
  }, [model]);

  const related = useMemo(() => hovered ? connectedNodeIds(model, hovered) : null, [hovered, model]);
  const flowNodes = useMemo<ProgressFlowNode[]>(() => layout?.nodes.map((entry) => {
    const node = model.nodes.find((value) => value.id === entry.id);
    if (!node) throw new Error(`Missing semantic node: ${entry.id}`);
    return { id: entry.id, type: 'progressNode', position: { x: entry.x, y: entry.y }, width: entry.width, height: entry.height, style: { width: entry.width, height: entry.height }, selected: selected === entry.id, draggable: false, selectable: true, data: { node, navigate: onNavigate, dimmed: Boolean(related && !related.has(entry.id)) } };
  }) ?? [], [layout, model.nodes, onNavigate, related, selected]);
  const flowEdges = useMemo<ProgressFlowEdge[]>(() => layout?.edges.map((entry) => {
    const link = model.links.find((value) => value.id === entry.id);
    if (!link) throw new Error(`Missing semantic link: ${entry.id}`);
    return { id: entry.id, type: 'progressEdge', source: link.fromNodeId, target: link.toNodeId, data: { link, points: entry.points, dimmed: Boolean(related && (!related.has(link.fromNodeId) || !related.has(link.toNodeId))) } };
  }) ?? [], [layout, model.links, related]);

  if (!layout) return <Box sx={{ display: 'grid', placeItems: 'center', height: 340 }}><CircularProgress size={22} /></Box>;
  return <Box sx={{ height: `clamp(360px, ${Math.min(54, Math.max(40, layout.height / 9))}vh, 540px)`, minWidth: 0, '--lineage-edge': theme.palette.divider, '--lineage-open': theme.palette.warning.main, '& .react-flow__pane': { cursor: 'grab' }, '& .react-flow__node': { outline: 'none' }, '& .react-flow__attribution': { display: 'none' } }}>
    <ReactFlow
      nodes={flowNodes}
      edges={flowEdges}
      nodeTypes={nodeTypes}
      edgeTypes={edgeTypes}
      fitView
      fitViewOptions={{ padding: 0.14, maxZoom: 1.08 }}
      minZoom={0.5}
      maxZoom={1.4}
      nodesDraggable={false}
      nodesConnectable={false}
      elementsSelectable
      deleteKeyCode={null}
      onNodeMouseEnter={(_event, node) => setHovered(node.id)}
      onNodeMouseLeave={() => setHovered(null)}
      onNodeClick={(_event, node) => setSelected(node.id)}
      proOptions={{ hideAttribution: true }}
    />
  </Box>;
}
