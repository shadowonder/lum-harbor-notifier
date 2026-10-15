import { useLayoutEffect, useRef, useState } from 'react';
import { Box, Tooltip, Typography, useTheme } from '@mui/material';
import type { CompactLineageNodeViewModel, CompactLineageViewModel } from '../../presentation/workItems/compactLineageProjection';
import { layoutCompactProgressLineage } from '../../presentation/workItems/compactProgressLayout';
import { resolveNodeShape } from '../../presentation/workItems/nodeTypePresentation';
import { statusPresentation } from '../../presentation/workItems/statusPresentation';
import type { ProposedWorkItemEntryDto } from '../../api/proposals';

function LineageNode({ node, x, y, color, surface }: {
  node: CompactLineageNodeViewModel;
  x: number;
  y: number;
  color: string;
  surface: string;
}) {
  const presentation = statusPresentation[node.status];
  const shape = node.target ? 'target' : resolveNodeShape(node.nodeType, node.status);
  const fill = presentation.fill === 'solid' ? color : surface;
  const common = { fill, stroke: color, strokeWidth: 1.7 };
  let glyph;
  if (shape === 'target') {
    glyph = <path d={`M ${x} ${y - 7} L ${x + 7} ${y} L ${x} ${y + 7} L ${x - 7} ${y} Z`} {...common} />;
  } else if (shape === 'roundedSquare') {
    glyph = <rect x={x - 5.7} y={y - 5.7} width="11.4" height="11.4" rx="2.3" {...common} />;
  } else if (shape === 'triangle') {
    glyph = <path d={`M ${x} ${y - 6.4} L ${x + 6.2} ${y + 5.2} L ${x - 6.2} ${y + 5.2} Z`} {...common} />;
  } else {
    glyph = <circle cx={x} cy={y} r="5.7" {...common} />;
  }
  return <>
    {glyph}
  </>;
}

function NodeTooltip({ node, children }: { node: CompactLineageNodeViewModel; children: React.ReactElement }) {
  const presentation = statusPresentation[node.status];
  return <Tooltip
    title={<Box>
      <Typography variant="caption" sx={{ display: 'block', color: 'inherit', fontWeight: 700 }}>{node.title}</Typography>
      <Typography variant="caption" sx={{ display: 'block', color: 'inherit', opacity: 0.82 }}>
        {presentation.label}{node.source ? ` · ${node.source}` : ''}{node.aggregateCount > 1 ? ` · ${node.aggregateCount} grouped` : ''}
      </Typography>
    </Box>}
  >{children}</Tooltip>;
}

export default function CompactProgressLineage({ model, addedEntries = [], proposedEntries = [] }: { model: CompactLineageViewModel; addedEntries?: ProposedWorkItemEntryDto[]; proposedEntries?: ProposedWorkItemEntryDto[] }) {
  const theme = useTheme();
  const containerRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(720);
  useLayoutEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    const updateWidth = () => setWidth(Math.max(320, Math.round(container.getBoundingClientRect().width)));
    updateWidth();
    const observer = new ResizeObserver(updateWidth);
    observer.observe(container);
    return () => observer.disconnect();
  }, []);
  const layout = layoutCompactProgressLineage(model, width);
  const toneColor = {
    positive: theme.palette.success.main,
    info: theme.palette.primary.main,
    neutral: theme.palette.text.secondary,
    warning: theme.palette.warning.main,
    danger: theme.palette.error.main,
  };
  const visibleNodeIds = new Set([
    ...model.mainNodes.map((node) => node.id),
    ...model.branches.flatMap((branch) => branch.nodes.map((node) => node.id)),
    model.target.id,
  ]);
  const visibleNodes = [...model.mainNodes, ...model.branches.flatMap((branch) => branch.nodes), model.target]
    .filter((node, index, nodes) => visibleNodeIds.has(node.id) && nodes.findIndex((candidate) => candidate.id === node.id) === index);

  const addedY = layout.height + 7;
  const previewY = layout.height + (addedEntries.length ? 23 : 7);
  const svgHeight = layout.height + (addedEntries.length ? 20 : 0) + (proposedEntries.length ? 20 : 0);
  const previewStart = width * .42;
  const previewEnd = width * .78;
  return <Box ref={containerRef} sx={{ width: '100%', minWidth: 0, '& .compact-lineage-node:focus': { outline: 'none' }, '& .compact-lineage-node:focus .compact-lineage-focus': { opacity: 1 } }}>
    <svg
      viewBox={`0 0 ${layout.width} ${svgHeight}`}
      width="100%"
      height={svgHeight}
      role="img"
      aria-label={`Progress lineage with ${model.branches.length} visible branches and ${model.hiddenBranchCount} hidden branches`}
      style={{ display: 'block' }}
    >
      {layout.paths.map((path) => <path
        key={path.id}
        d={path.path}
        fill="none"
        stroke={path.kind === 'main' ? theme.palette.text.secondary : theme.palette.divider}
        strokeWidth={path.kind === 'main' ? 2.1 : path.kind === 'tail' ? 1.45 : 1.55}
        strokeLinecap="round"
        strokeLinejoin="round"
        opacity={path.kind === 'main' ? 0.72 : path.kind === 'merge' ? 0.7 : 0.88}
        vectorEffect="non-scaling-stroke"
      />)}
      {visibleNodes.map((node) => {
        const point = layout.nodes[node.id];
        if (!point) return null;
        const presentation = statusPresentation[node.status];
        return <NodeTooltip key={node.id} node={node}>
          <g
            className="compact-lineage-node"
            tabIndex={0}
            role="graphics-symbol"
            aria-label={`${node.title}. ${presentation.label}${node.source ? `. Source ${node.source}` : ''}`}
          >
            <circle className="compact-lineage-focus" cx={point.x} cy={point.y} r="9.5" fill="none" stroke={theme.palette.primary.main} strokeWidth="2" opacity="0" vectorEffect="non-scaling-stroke" />
            <LineageNode node={node} x={point.x} y={point.y} color={toneColor[presentation.tone]} surface={theme.palette.background.paper} />
          </g>
        </NodeTooltip>;
      })}
      {addedEntries.length > 0 && <g aria-label={`${addedEntries.length} added lineage items`}>
        <path d={`M ${width * .34} 10 Q ${width * .37} ${addedY} ${previewStart} ${addedY} H ${previewEnd} Q ${width * .84} ${addedY} ${width - 16} 10`} fill="none" stroke={theme.palette.primary.main} strokeWidth="1.6" vectorEffect="non-scaling-stroke" />
        {addedEntries.slice(0, 4).map((entry, index, entries) => { const x = previewStart + (previewEnd - previewStart) * (entries.length === 1 ? .5 : index / (entries.length - 1)); return <g key={entry.id} role="graphics-symbol" aria-label={`${entry.title}. Added`}><title>{entry.title} · Added</title><circle cx={x} cy={addedY} r="5.5" fill={theme.palette.primary.light} stroke={theme.palette.primary.main} strokeWidth="1.7" vectorEffect="non-scaling-stroke" /></g>; })}
      </g>}
      {proposedEntries.length > 0 && <g opacity=".72" aria-label={`${proposedEntries.length} proposed lineage items`}>
        <path d={`M ${width * .34} 10 Q ${width * .37} ${previewY} ${previewStart} ${previewY} H ${previewEnd} Q ${width * .84} ${previewY} ${width - 16} 10`} fill="none" stroke="#7969C7" strokeWidth="1.6" strokeDasharray="4 4" vectorEffect="non-scaling-stroke" />
        {proposedEntries.slice(0, 4).map((entry, index, entries) => { const x = previewStart + (previewEnd - previewStart) * (entries.length === 1 ? .5 : index / (entries.length - 1)); return <g key={entry.id} role="graphics-symbol" aria-label={`${entry.title}. Proposed`}><title>{entry.title} · Proposed</title><circle cx={x} cy={previewY} r="5.5" fill={theme.palette.background.paper} stroke="#7969C7" strokeWidth="1.7" strokeDasharray="2 1" vectorEffect="non-scaling-stroke" /></g>; })}
      </g>}
    </svg>
    {addedEntries.length > 0 && <Typography variant="caption" color="primary.main" sx={{ display: 'block', mt: -.1, fontSize: 9, fontWeight: 720 }}>{addedEntries.length} newly added</Typography>}
    {proposedEntries.length > 0 && <Typography variant="caption" sx={{ display: 'block', mt: -.1, color: '#7969C7', fontSize: 9, fontWeight: 720, letterSpacing: '.05em', textTransform: 'uppercase' }}>{proposedEntries.length} proposed</Typography>}
    {model.hiddenBranchCount > 0 && <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.15 }}>+{model.hiddenBranchCount} more</Typography>}
  </Box>;
}
