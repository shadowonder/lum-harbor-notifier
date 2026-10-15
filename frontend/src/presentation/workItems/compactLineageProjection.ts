import type {
  ProgressNodeDto,
  ProgressProjectionDto,
  ReadinessSummaryDto,
  ProgressStatusDto,
  ProgressStreamDto,
} from '../../api/workItems/contracts';
import { mostSignificantStatus, statusPresentation } from './statusPresentation';

export interface CompactLineageNodeViewModel {
  id: string;
  status: ProgressStatusDto;
  nodeType: ProgressNodeDto['nodeType'];
  title: string;
  source?: string;
  aggregateCount: number;
  target?: boolean;
}

export interface CompactLineageBranchViewModel {
  id: string;
  originMainIndex: number;
  nodes: CompactLineageNodeViewModel[];
  status: ProgressStatusDto;
  merged: boolean;
  priority: number;
}

export interface CompactLineageViewModel {
  mainNodes: CompactLineageNodeViewModel[];
  branches: CompactLineageBranchViewModel[];
  target: CompactLineageNodeViewModel;
  hiddenBranchCount: number;
  totalBranchCount: number;
  statusCounts: Partial<Record<ProgressStatusDto, number>>;
  readiness: Pick<ReadinessSummaryDto, 'ready' | 'required'>;
}

function toCompactNode(node: ProgressNodeDto, aggregateCount = node.total ?? 1, source?: string, completedTarget = false): CompactLineageNodeViewModel {
  return {
    id: node.id,
    status: completedTarget ? 'completed' : node.status,
    nodeType: node.nodeType,
    title: node.reason ?? node.title ?? statusPresentation[node.status].label,
    source: node.metadata?.source ?? source,
    aggregateCount,
    target: node.status === 'target',
  };
}

function representativeMainNodes(nodes: ProgressNodeDto[]) {
  if (nodes.length <= 3) return nodes;
  return [nodes[0], nodes[Math.floor((nodes.length - 1) / 2)], nodes[nodes.length - 1]];
}

function representativeBranchNodes(projection: ProgressProjectionDto, nodes: ProgressNodeDto[], source?: string) {
  const outgoingSignature = (node: ProgressNodeDto) => projection.links
    .filter((link) => link.fromNodeId === node.id)
    .map((link) => `${link.kind}:${Boolean(link.open)}`)
    .sort()
    .join('|');
  const grouped = nodes.reduce<CompactLineageNodeViewModel[]>((groups, node) => {
    const previousNode = nodes[nodes.indexOf(node) - 1];
    const previous = groups[groups.length - 1];
    const canAggregate = previous && previousNode
      && previous.status === node.status
      && previous.nodeType === node.nodeType
      && outgoingSignature(previousNode) === outgoingSignature(node);
    if (canAggregate) {
      previous.aggregateCount += node.total ?? 1;
      return groups;
    }
    return [...groups, toCompactNode(node, node.total ?? 1, source)];
  }, []);
  if (grouped.length <= 3) return grouped;
  const attention = grouped.filter((node) => statusPresentation[node.status].priority >= 5);
  const selected = [grouped[0], ...attention, grouped[grouped.length - 1]]
    .filter((node, index, values) => values.findIndex((candidate) => candidate.id === node.id) === index)
    .sort((left, right) => grouped.indexOf(left) - grouped.indexOf(right));
  return selected.length > 3
    ? [selected[0], selected[selected.length - 2], selected[selected.length - 1]]
    : selected;
}

function branchOriginIndex(projection: ProgressProjectionDto, stream: ProgressStreamDto, mainNodes: ProgressNodeDto[]): number {
  const branchLink = projection.links.find(
    (link) => link.kind === 'branch' && projection.nodes.find((node) => node.id === link.toNodeId)?.streamId === stream.id,
  );
  const source = projection.nodes.find((node) => node.id === branchLink?.fromNodeId);
  if (!source) return Math.max(0, mainNodes.length - 1);
  if (source.streamId === 'main') {
    const exact = mainNodes.findIndex((node) => node.id === source.id);
    if (exact >= 0) return exact;
    const sourceStage = projection.stages.findIndex((stage) => stage.id === source.stageId);
    return mainNodes
      .map((node, index) => ({ index, distance: Math.abs(projection.stages.findIndex((stage) => stage.id === node.stageId) - sourceStage) }))
      .sort((left, right) => left.distance - right.distance)[0]?.index ?? 0;
  }
  const parentStream = projection.streams.find((candidate) => candidate.id === source.streamId);
  return parentStream ? branchOriginIndex(projection, parentStream, mainNodes) : 0;
}

export function buildCompactLineage(
  projection: ProgressProjectionDto,
  visibleBranchLimit: number,
  readiness: ReadinessSummaryDto,
  lifecycleState: 'active' | 'completed',
): CompactLineageViewModel {
  const targetNode = projection.nodes.find((node) => node.id === projection.targetNodeId);
  if (!targetNode) throw new Error(`Missing target node: ${projection.targetNodeId}`);
  const allMainNodes = projection.nodes
    .filter((node) => node.streamId === 'main' && node.id !== projection.targetNodeId)
    .sort((left, right) => projection.stages.findIndex((stage) => stage.id === left.stageId) - projection.stages.findIndex((stage) => stage.id === right.stageId));
  const mainNodes = representativeMainNodes(allMainNodes);
  const branches = projection.streams
    .filter((stream) => stream.kind === 'branch')
    .flatMap((stream): CompactLineageBranchViewModel[] => {
      const nodes = projection.nodes
        .filter((node) => node.streamId === stream.id)
        .sort((left, right) => projection.stages.findIndex((stage) => stage.id === left.stageId) - projection.stages.findIndex((stage) => stage.id === right.stageId));
      if (!nodes.length) return [];
      const status = mostSignificantStatus(nodes.map((node) => node.status));
      return [{
        id: stream.id,
        originMainIndex: branchOriginIndex(projection, stream, mainNodes),
        nodes: representativeBranchNodes(projection, nodes, stream.sourceHint),
        status,
        merged: status === 'completed',
        priority: statusPresentation[status].priority,
      }];
    });
  const unresolved = branches.filter((branch) => !branch.merged).sort((left, right) => right.priority - left.priority);
  const completed = branches.filter((branch) => branch.merged).sort((left, right) => right.priority - left.priority);
  const visibleBranches = lifecycleState === 'completed'
    ? completed.slice(0, visibleBranchLimit)
    : unresolved.slice(0, completed.length && visibleBranchLimit > 1 ? visibleBranchLimit - 1 : visibleBranchLimit);
  if (lifecycleState === 'active' && completed.length && visibleBranches.length < visibleBranchLimit) {
    visibleBranches.push(completed[0]);
  }

  return {
    mainNodes: mainNodes.map((node) => toCompactNode(node)),
    branches: visibleBranches,
    target: toCompactNode(targetNode, 1, undefined, lifecycleState === 'completed'),
    hiddenBranchCount: Math.max(0, branches.length - visibleBranches.length),
    totalBranchCount: branches.length,
    statusCounts: branches.reduce<Partial<Record<ProgressStatusDto, number>>>((counts, branch) => ({
      ...counts,
      [branch.status]: (counts[branch.status] ?? 0) + 1,
    }), {}),
    readiness: { ready: readiness.ready, required: readiness.required },
  };
}
