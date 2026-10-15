import type { WorkflowItemType } from './workflowTypes.ts';

interface WorkflowNodeShapeProps {
  x: number;
  y: number;
  itemType: WorkflowItemType;
  color: string;
  bgColor?: string;
  radius?: number;
  isGhost?: boolean;
  isHighlighted?: boolean;
  isDimmed?: boolean;
  isResolved?: boolean;
  onClick?: () => void;
  onMouseEnter?: () => void;
  onMouseLeave?: () => void;
  title?: string;
}

export default function WorkflowNodeShape({
  x,
  y,
  itemType,
  color,
  bgColor = 'transparent',
  radius = 6,
  isGhost = false,
  isHighlighted = false,
  isDimmed = false,
  isResolved = false,
  onClick,
  onMouseEnter,
  onMouseLeave,
  title,
}: WorkflowNodeShapeProps) {
  const opacity = isDimmed ? 0.22 : 1;
  const strokeWidth = isHighlighted ? 2.5 : isGhost ? 1.5 : 1.75;
  const fill = isGhost ? 'none' : isResolved || isHighlighted ? color : bgColor;
  const stroke = color;
  const strokeDasharray = isGhost ? '3,3' : undefined;

  const cursor = onClick ? 'pointer' : 'default';

  return (
    <g
      opacity={opacity}
      cursor={cursor}
      onClick={onClick}
      onMouseEnter={onMouseEnter}
      onMouseLeave={onMouseLeave}
      style={{ transition: 'opacity 150ms ease, transform 150ms ease' }}
    >
      {title && <title>{title}</title>}
      {/* Invisible larger hit target for easy mouse hover/click */}
      <circle cx={x} cy={y} r={radius + 8} fill="transparent" />

      {itemType === 'story' && (
        <circle
          cx={x}
          cy={y}
          r={radius}
          fill={fill}
          stroke={stroke}
          strokeWidth={strokeWidth}
          strokeDasharray={strokeDasharray}
        />
      )}

      {itemType === 'task' && (
        <rect
          x={x - radius}
          y={y - radius}
          width={radius * 2}
          height={radius * 2}
          rx={2}
          fill={fill}
          stroke={stroke}
          strokeWidth={strokeWidth}
          strokeDasharray={strokeDasharray}
        />
      )}

      {itemType === 'defect' && (
        <polygon
          points={`${x},${y - radius - 1} ${x + radius + 1},${y + radius} ${x - radius - 1},${y + radius}`}
          fill={fill}
          stroke={stroke}
          strokeWidth={strokeWidth}
          strokeDasharray={strokeDasharray}
        />
      )}

      {itemType === 'gate' && (
        <polygon
          points={`${x},${y - radius - 1} ${x + radius + 1},${y} ${x},${y + radius + 1} ${x - radius - 1},${y}`}
          fill={fill}
          stroke={stroke}
          strokeWidth={strokeWidth}
          strokeDasharray={strokeDasharray}
        />
      )}

      {/* Center dot for highlighted or solid nodes */}
      {!isGhost && isHighlighted && (
        <circle cx={x} cy={y} r={2} fill="#ffffff" />
      )}
    </g>
  );
}

