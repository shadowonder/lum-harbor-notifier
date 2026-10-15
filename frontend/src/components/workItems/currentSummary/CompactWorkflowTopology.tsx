import { useTheme } from '@mui/material/styles';
import {
  computeStageItemLayouts,
  selectRepresentativeEdgeRoutes,
  toPolylinePath,
  COMPACT_TOPOLOGY_DIMENSIONS,
  DEFAULT_TOPOLOGY_DIMENSIONS,
} from './compactTopologyLayout.ts';
import { MAX_REPRESENTATIVE_EDGES } from './compactTopologyPrototypeAdapter.ts';
import type { CompactTopologyModel } from './compactTopologyTypes.ts';
import { getHealthColor } from '../workflow/workflowLayout.ts';
import WorkflowNodeShape from '../workflow/WorkflowNodeShape.tsx';

/**
 * Compact Workflow Topology Summary — the CENTER block of a Current Work
 * Items row. Deliberately a much smaller, simpler renderer than the
 * WorkItem Dependency Explorer: a single main trunk, every item attached
 * to it by a straight stem (no floating nodes, no complex routing), and at
 * most a few representative relationships. Full dependency detail stays on
 * the WorkItem Dependency Explorer / detail page.
 *
 * Status Rollup round: this shows workflow DISTRIBUTION, not progression.
 * Stage bands mark how many segments the workflow has and roughly
 * partition item positions — they never render a permanent Stage name
 * (that's still available on hover only), and there is no "current stage"
 * band highlight, since a real WorkItem's tracked items can legitimately
 * sit at different stages at once. Emphasis comes only from each item's
 * own health/status marker.
 */
export default function CompactWorkflowTopology({
  model,
  compact = false,
}: {
  model: CompactTopologyModel;
  compact?: boolean;
}) {
  const theme = useTheme();
  const isDark = theme.palette.mode === 'dark';
  const dims = compact ? COMPACT_TOPOLOGY_DIMENSIONS : DEFAULT_TOPOLOGY_DIMENSIONS;

  const overflowLabelColor = isDark ? '#94a3b8' : '#64748b';
  const neutralBorder = isDark ? 'rgba(148, 163, 184, 0.2)' : 'rgba(203, 213, 225, 0.6)';
  const bandTint = isDark ? 'rgba(30, 41, 59, 0.4)' : 'rgba(241, 245, 249, 0.6)';
  const trunkColor = getHealthColor(model.trunkHealth, isDark);
  const stemColor = isDark ? 'rgba(148, 163, 184, 0.4)' : 'rgba(100, 116, 139, 0.32)';

  const stageLabelById = new Map(model.stages.map((stage) => [stage.id, stage.label]));
  const stageLayouts = computeStageItemLayouts(model, dims);
  const edgeRoutes = selectRepresentativeEdgeRoutes(model, dims, MAX_REPRESENTATIVE_EDGES);

  if (model.stages.length === 0) {
    return (
      <svg
        viewBox={`0 0 ${dims.width} ${dims.height}`}
        style={{ width: '100%', height: '100%', maxHeight: dims.height, display: 'block' }}
        aria-label="No tracked items yet"
      >
        <line
          x1={dims.paddingX}
          y1={dims.trunkY}
          x2={dims.width - dims.paddingX}
          y2={dims.trunkY}
          stroke={trunkColor}
          strokeWidth={2}
          strokeLinecap="round"
          opacity={0.5}
        />
        <text
          x={dims.width / 2}
          y={dims.trunkY - 8}
          textAnchor="middle"
          fill={overflowLabelColor}
          fontSize="8"
          fontStyle="italic"
        >
          No tracked items yet
        </text>
      </svg>
    );
  }

  return (
    <svg
      viewBox={`0 0 ${dims.width} ${dims.height}`}
      style={{ width: '100%', height: '100%', maxHeight: dims.height, display: 'block' }}
      aria-label="Compact workflow item distribution"
    >
      {/* Stage bands: unlabeled — they mark how many segments this
          workflow has and partition item positions, nothing more. Stage
          names remain available on each item's hover title only. */}
      {stageLayouts.map(({ stage, bounds }, index) => (
        <g key={stage.id}>
          {index % 2 === 1 && (
            <rect
              x={bounds.startX}
              y={1}
              width={bounds.width}
              height={dims.height - 2}
              fill={bandTint}
              rx={3}
            />
          )}
          {index > 0 && (
            <line
              x1={bounds.startX}
              y1={3}
              x2={bounds.startX}
              y2={dims.height - 3}
              stroke={neutralBorder}
              strokeWidth={1}
            />
          )}
        </g>
      ))}

      {/* Main trunk — the primary structure every item attaches to. No
          "current position" marker: emphasis lives on item markers only. */}
      <line
        x1={dims.paddingX}
        y1={dims.trunkY}
        x2={dims.width - dims.paddingX}
        y2={dims.trunkY}
        stroke={trunkColor}
        strokeWidth={2.25}
        strokeLinecap="round"
        opacity={0.8}
      />

      {/* Representative relationships — a short detour off the trunk, never full routing. */}
      {edgeRoutes.map((route) => (
        <path
          key={`${route.from}->${route.to}`}
          d={toPolylinePath(route.points)}
          fill="none"
          stroke={stemColor}
          strokeWidth={1}
          strokeDasharray="2,2"
        />
      ))}

      {/* Every item: a straight stem to the trunk, then its type/health
          marker. No floating nodes. Stage name surfaces only on hover. */}
      {stageLayouts.flatMap(({ visible, overflowCount, bounds }) =>
        [
          ...visible.map(({ item, x, y }) => (
            <g key={item.id}>
              <line x1={x} y1={dims.trunkY} x2={x} y2={y} stroke={stemColor} strokeWidth={1} />
              <WorkflowNodeShape
                x={x}
                y={y}
                itemType={item.itemType}
                color={getHealthColor(item.health, isDark)}
                radius={compact ? 4 : 4.5}
                isResolved={item.health === 'green'}
                title={`${item.title} · ${item.itemType} · ${item.health} · ${stageLabelById.get(item.stageId) ?? ''}`}
              />
            </g>
          )),
          overflowCount > 0 ? (
            <text
              key={`${bounds.centerX}-overflow`}
              x={bounds.centerX}
              y={dims.height - 3}
              textAnchor="middle"
              fill={overflowLabelColor}
              fontSize="6.5"
              style={{ userSelect: 'none' }}
            >
              +{overflowCount}
            </text>
          ) : null,
        ].filter(Boolean),
      )}
    </svg>
  );
}
