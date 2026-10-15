import { Box, Stack, Typography } from '@mui/material';
import { hasWorkflowData, WORKFLOW_STAGES, type WorkflowModel } from '../workflow/workflowTypes.ts';

function ContextValue({ label, value }: { label: string; value: string }) {
  return (
    <Stack direction="row" spacing={0.55} sx={{ alignItems: 'baseline', minWidth: 0 }}>
      <Typography
        variant="caption"
        color="text.secondary"
        sx={{ fontWeight: 650, flex: '0 0 auto' }}
      >
        {label}
      </Typography>
      <Typography variant="body2" sx={{ fontWeight: 650, overflowWrap: 'anywhere' }}>
        {value}
      </Typography>
    </Stack>
  );
}

export default function WorkflowContextPanel({ model }: { model: WorkflowModel }) {
  const workflowAvailable = hasWorkflowData(model);
  const currentStage = WORKFLOW_STAGES.find((stage) => stage.id === model.trunk.currentStageId);
  const blocker =
    model.aggregateHealth === 'red' || model.aggregateHealth === 'yellow'
      ? model.statusReason
      : undefined;

  return (
    <Box
      component="section"
      aria-label="Workflow context"
      sx={{
        px: 1.5,
        py: 1.15,
        borderTop: 1,
        borderBottom: 1,
        borderColor: 'divider',
        bgcolor: 'surface.subtle',
      }}
    >
      <Stack direction="row" spacing={{ xs: 1.5, md: 3 }} useFlexGap sx={{ flexWrap: 'wrap' }}>
        <ContextValue
          label="Health"
          value={model.hasAuthoritativeHealth ? model.healthLabel : 'Unknown'}
        />
        <ContextValue
          label="Current stage"
          value={
            workflowAvailable
              ? `${currentStage?.label ?? model.trunk.currentStageId.toUpperCase()} · ${model.trunk.progressPercent}%`
              : 'Unavailable'
          }
        />
        <ContextValue
          label="Target"
          value={
            workflowAvailable
              ? `${model.targetDate ?? 'Not configured'}${model.targetTimingLabel ? ` · ${model.targetTimingLabel}` : ''}`
              : 'Unavailable'
          }
        />
        <ContextValue
          label="Next checkpoint"
          value={
            workflowAvailable && model.nextMilestone
              ? `${model.nextMilestone.label} · ${model.nextMilestone.timingLabel}`
              : 'Not available'
          }
        />
      </Stack>
      <Stack
        direction="row"
        spacing={{ xs: 1.5, md: 3 }}
        useFlexGap
        sx={{ mt: 0.55, flexWrap: 'wrap' }}
      >
        <ContextValue
          label="Source"
          value={
            workflowAvailable
              ? (model.origin?.label ??
                (model.isRealData ? 'Workflow projection' : 'Prototype fixture'))
              : 'Unavailable'
          }
        />
        <ContextValue
          label="Blocker"
          value={workflowAvailable ? (blocker ?? 'No active blocker') : 'Unavailable'}
        />
      </Stack>
    </Box>
  );
}
