import { useState } from 'react';
import { Box, Stack, Typography } from '@mui/material';
import WorkItemDependencyExperimentCanvas from '../dependencyExperiment/WorkItemDependencyExperimentCanvas.tsx';
import WorkflowLegend from '../workflow/WorkflowLegend.tsx';
import type { WorkflowModel } from '../workflow/workflowTypes.ts';
import WorkItemInspector from './WorkItemInspector.tsx';
import WorkflowContextPanel from './WorkflowContextPanel.tsx';
import WorkflowHistoryPanel from './WorkflowHistoryPanel.tsx';

interface WorkItemOverviewTabProps {
  model: WorkflowModel;
  selectedNodeId?: string | null;
  onSelectNode?: (nodeId: string | null) => void;
  onViewFullHistory?: () => void;
  onViewItemDetails?: (nodeId: string) => void;
}

export default function WorkItemOverviewTab({
  model,
  selectedNodeId: controlledSelectedNodeId,
  onSelectNode: controlledOnSelectNode,
  onViewFullHistory,
  onViewItemDetails,
}: WorkItemOverviewTabProps) {
  const [internalSelectedNodeId, setInternalSelectedNodeId] = useState<string | null>(null);

  const selectedNodeId =
    controlledSelectedNodeId !== undefined ? controlledSelectedNodeId : internalSelectedNodeId;

  const handleSelectNode = (nodeId: string | null) => {
    if (controlledOnSelectNode) {
      controlledOnSelectNode(nodeId);
    } else {
      setInternalSelectedNodeId(nodeId);
    }
  };

  const selectedNode = selectedNodeId
    ? (model.nodes.find((n) => n.id === selectedNodeId) ?? null)
    : null;

  return (
    <Stack spacing={2}>
      <WorkflowContextPanel model={model} />

      <Box component="section" aria-labelledby="workflow-canvas-heading" sx={{ minWidth: 0 }}>
        <Stack
          direction={{ xs: 'column', md: 'row' }}
          spacing={1}
          sx={{ mb: 1, justifyContent: 'space-between', alignItems: { md: 'center' } }}
        >
          <Box>
            <Typography id="workflow-canvas-heading" variant="h2">
              Dependency Explorer
            </Typography>
            <Typography variant="caption" color="text.secondary">
              Who this depends on, who depends on it, and why the feature is stuck.
            </Typography>
          </Box>
          <WorkflowLegend />
        </Stack>
        <WorkItemDependencyExperimentCanvas
          model={model}
          selectedNodeId={selectedNodeId}
          onSelectNode={handleSelectNode}
        />
      </Box>

      <Box component="section" aria-labelledby="selected-item-context-heading">
        <Typography id="selected-item-context-heading" variant="h2" sx={{ mb: 1 }}>
          Selected Item Context
        </Typography>
        <WorkItemInspector
          node={selectedNode}
          model={model}
          mode="compact"
          onClearSelection={() => handleSelectNode(null)}
          onSelectNode={(id) => handleSelectNode(id)}
          onViewDetails={onViewItemDetails}
        />
      </Box>

      <WorkflowHistoryPanel
        model={model}
        onSelectNode={(id) => handleSelectNode(id)}
        limit={6}
        onViewFullHistory={onViewFullHistory}
      />
    </Stack>
  );
}
