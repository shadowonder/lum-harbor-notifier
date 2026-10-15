import type { CompactLineageViewModel } from './compactLineageProjection';

export interface CompactPoint { x: number; y: number }
export interface CompactLineagePath {
  id: string;
  path: string;
  kind: 'main' | 'branch' | 'merge' | 'tail';
}

export interface CompactLineageLayout {
  width: number;
  height: number;
  nodes: Record<string, CompactPoint>;
  paths: CompactLineagePath[];
}

function roundedBranchPath(from: CompactPoint, to: CompactPoint, turnX: number) {
  const radius = Math.min(7, Math.abs(to.y - from.y) / 2, Math.abs(turnX - from.x));
  return `M ${from.x} ${from.y} H ${turnX - radius} Q ${turnX} ${from.y} ${turnX} ${from.y + radius} V ${to.y - radius} Q ${turnX} ${to.y} ${turnX + radius} ${to.y} H ${to.x}`;
}

function roundedMergePath(from: CompactPoint, to: CompactPoint, turnX: number) {
  const radius = Math.min(7, Math.abs(from.y - to.y) / 2, Math.abs(turnX - from.x));
  return `M ${from.x} ${from.y} H ${turnX - radius} Q ${turnX} ${from.y} ${turnX} ${from.y - radius} V ${to.y + radius} Q ${turnX} ${to.y} ${turnX + radius} ${to.y} H ${to.x}`;
}

export function layoutCompactProgressLineage(model: CompactLineageViewModel, width = 720): CompactLineageLayout {
  const left = 16;
  const right = 16;
  const mainY = 10;
  const laneGap = 16;
  const targetX = width - right;
  const mainNodes = model.mainNodes;
  const mainStep = (targetX - left) / Math.max(1, mainNodes.length);
  const nodes: Record<string, CompactPoint> = Object.fromEntries(
    mainNodes.map((node, index) => [node.id, { x: left + mainStep * index, y: mainY }]),
  );
  nodes[model.target.id] = { x: targetX, y: mainY };
  const paths: CompactLineagePath[] = [{
    id: 'main-line',
    path: `M ${nodes[mainNodes[0]?.id]?.x ?? left} ${mainY} H ${targetX}`,
    kind: 'main',
  }];

  model.branches.forEach((branch, branchIndex) => {
    const y = mainY + laneGap * (branchIndex + 1);
    const originNode = mainNodes[Math.min(branch.originMainIndex, Math.max(0, mainNodes.length - 1))];
    const origin = nodes[originNode?.id] ?? { x: left, y: mainY };
    const availableEnd = branch.merged
      ? Math.min(targetX - 26, origin.x + Math.max(84, mainStep * 1.15))
      : Math.min(targetX - 28, origin.x + Math.max(42, mainStep * 0.72));
    const startX = Math.min(availableEnd - 18, origin.x + 28);
    const nodeStep = branch.nodes.length > 1 ? (availableEnd - startX) / (branch.nodes.length - 1) : 0;
    branch.nodes.forEach((node, nodeIndex) => {
      nodes[node.id] = { x: startX + nodeStep * nodeIndex, y };
    });
    const first = nodes[branch.nodes[0].id];
    const last = nodes[branch.nodes[branch.nodes.length - 1].id];
    paths.push({ id: `branch-${branch.id}`, path: roundedBranchPath(origin, first, origin.x + 14), kind: 'branch' });
    if (branch.nodes.length > 1) {
      paths.push({ id: `branch-progress-${branch.id}`, path: `M ${first.x} ${y} H ${last.x}`, kind: 'branch' });
    }
    if (branch.merged) {
      const nextMainNode = mainNodes[Math.min(branch.originMainIndex + 1, Math.max(0, mainNodes.length - 1))];
      const nextMain = nodes[nextMainNode?.id];
      const merge = nextMain && nextMain.x > last.x + 18
        ? nextMain
        : { x: Math.min(targetX - 10, last.x + 34), y: mainY };
      paths.push({ id: `merge-${branch.id}`, path: roundedMergePath(last, merge, last.x + 14), kind: 'merge' });
    } else {
      paths.push({ id: `tail-${branch.id}`, path: `M ${last.x} ${last.y} h 9`, kind: 'tail' });
    }
  });

  return {
    width,
    height: model.branches.length ? mainY + laneGap * model.branches.length + 8 : 22,
    nodes,
    paths,
  };
}
