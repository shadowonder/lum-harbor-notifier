import type { ProgressNodeTypeDto, ProgressStatusDto } from '../../api/workItems/contracts';

export interface NodeTypePresentation {
  label: string;
  shape: 'circle' | 'roundedSquare' | 'triangle';
}

export const nodeTypePresentation: Record<ProgressNodeTypeDto, NodeTypePresentation> = {
  action: { label: 'Action', shape: 'circle' },
  requirement: { label: 'Requirement', shape: 'roundedSquare' },
  issue: { label: 'Issue', shape: 'triangle' },
  checkpoint: { label: 'Checkpoint', shape: 'circle' },
};

export type NodeVisualShape = NodeTypePresentation['shape'] | 'target';

export function resolveNodeShape(nodeType: ProgressNodeTypeDto, status: ProgressStatusDto): NodeVisualShape {
  if (status === 'target') return 'target';
  return nodeTypePresentation[nodeType].shape;
}
