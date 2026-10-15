import ELK from 'elkjs/lib/elk.bundled.js';
import type { ElkExtendedEdge, ElkNode, ElkPoint } from 'elkjs/lib/elk-api';
import type { ProgressLineageViewModel } from './workItemViewModels';

export interface ExpandedLayoutNode {
  id: string;
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface ExpandedLayoutEdge {
  id: string;
  points: ElkPoint[];
}

export interface ExpandedProgressLayout {
  nodes: ExpandedLayoutNode[];
  edges: ExpandedLayoutEdge[];
  width: number;
  height: number;
}

const elk = new ELK();

function nodeSize(nodeId: string, model: ProgressLineageViewModel) {
  const node = model.nodes.find((entry) => entry.id === nodeId);
  if (nodeId === model.targetNodeId) return { width: 112, height: 42 };
  if (node?.streamId === 'main') return { width: 148, height: 40 };
  return { width: 218, height: 42 };
}

export async function layoutProgressGraph(
  model: ProgressLineageViewModel,
): Promise<ExpandedProgressLayout> {
  const children: ElkNode[] = model.nodes.map((node) => ({
    id: node.id,
    ...nodeSize(node.id, model),
  }));
  const edges: ElkExtendedEdge[] = model.links.map((link) => ({
    id: link.id,
    sources: [link.fromNodeId],
    targets: [link.toNodeId],
  }));
  const graph: ElkNode = {
    id: 'progress-lineage',
    layoutOptions: {
      'elk.algorithm': 'layered',
      'elk.direction': 'RIGHT',
      'elk.edgeRouting': 'ORTHOGONAL',
      'elk.layered.considerModelOrder.strategy': 'NODES_AND_EDGES',
      'elk.layered.nodePlacement.strategy': 'NETWORK_SIMPLEX',
      'elk.layered.crossingMinimization.strategy': 'LAYER_SWEEP',
      'elk.spacing.nodeNode': '30',
      'elk.layered.spacing.nodeNodeBetweenLayers': '74',
      'elk.spacing.edgeNode': '18',
      'elk.padding': '[top=28,left=28,bottom=28,right=28]',
    },
    children,
    edges,
  };
  const result = await elk.layout(graph);
  return {
    nodes: (result.children ?? []).map((node) => ({
      id: node.id,
      x: node.x ?? 0,
      y: node.y ?? 0,
      width: node.width ?? nodeSize(node.id, model).width,
      height: node.height ?? nodeSize(node.id, model).height,
    })),
    edges: (result.edges ?? []).map((edge) => {
      const section = edge.sections?.[0];
      return {
        id: edge.id,
        points: section
          ? [section.startPoint, ...(section.bendPoints ?? []), section.endPoint]
          : [],
      };
    }),
    width: result.width ?? 800,
    height: result.height ?? 360,
  };
}
