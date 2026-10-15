import AccountTreeRounded from '@mui/icons-material/AccountTreeRounded';
import ErrorOutlineRounded from '@mui/icons-material/ErrorOutlineRounded';
import FlagRounded from '@mui/icons-material/FlagRounded';
import SwapHorizRounded from '@mui/icons-material/SwapHorizRounded';
import TrendingUpRounded from '@mui/icons-material/TrendingUpRounded';
import { Box, Button, ButtonBase, Divider, Paper, Stack, Typography } from '@mui/material';
import {
  getRecentWorkflowHistory,
  hasWorkflowData,
  type WorkflowHistoryEvent,
  type WorkflowModel,
} from '../workflow/workflowTypes.ts';

function eventIcon(type: WorkflowHistoryEvent['type']) {
  switch (type) {
    case 'alert':
      return <ErrorOutlineRounded sx={{ fontSize: 15, color: 'error.main' }} />;
    case 'health_change':
      return <TrendingUpRounded sx={{ fontSize: 15, color: 'warning.main' }} />;
    case 'dependency':
      return <AccountTreeRounded sx={{ fontSize: 15, color: 'text.secondary' }} />;
    case 'transition':
      return <SwapHorizRounded sx={{ fontSize: 15, color: 'primary.main' }} />;
    case 'milestone':
      return <FlagRounded sx={{ fontSize: 15, color: 'success.main' }} />;
  }
}

export default function WorkflowHistoryPanel({
  model,
  onSelectNode,
  limit = 6,
  onViewFullHistory,
}: {
  model: WorkflowModel;
  onSelectNode: (nodeId: string) => void;
  limit?: number;
  onViewFullHistory?: () => void;
}) {
  const workflowAvailable = hasWorkflowData(model);
  const allEvents = model.historyEvents ?? [];
  const events = getRecentWorkflowHistory(model, limit);

  return (
    <Box component="section" aria-labelledby="workflow-history-heading">
      <Stack
        direction="row"
        sx={{ mb: 1, alignItems: 'baseline', justifyContent: 'space-between' }}
      >
        <Typography id="workflow-history-heading" variant="h2">
          Workflow Activity
        </Typography>
        <Typography variant="caption" color="text.secondary">
          Latest {events.length} of {allEvents.length} · workflow-level
        </Typography>
      </Stack>
      <Paper variant="outlined" sx={{ overflow: 'hidden' }}>
        <Box sx={{ px: 2, py: 0.8, bgcolor: 'surface.subtle' }}>
          <Typography variant="caption" color="text.secondary">
            How the workflow reached its current state. Selected-item activity appears in the
            context deck.
          </Typography>
        </Box>
        <Divider />
        {events.length === 0 ? (
          <Box sx={{ py: 5, textAlign: 'center' }}>
            <Typography color="text.secondary">
              {workflowAvailable
                ? 'No workflow history is available.'
                : 'Workflow history unavailable.'}
            </Typography>
          </Box>
        ) : (
          <>
            {events.map((event) => {
              const affectedNode = event.nodeId
                ? model.nodes.find((node) => node.id === event.nodeId)
                : null;
              return (
                <ButtonBase
                  key={event.id}
                  component="div"
                  disabled={!affectedNode}
                  onClick={() => affectedNode && onSelectNode(affectedNode.id)}
                  sx={{
                    width: '100%',
                    px: 2,
                    py: 1.15,
                    textAlign: 'left',
                    alignItems: 'flex-start',
                    borderBottom: 1,
                    borderColor: 'divider',
                    '&:last-child': { borderBottom: 0 },
                    '&:hover': affectedNode ? { bgcolor: 'action.hover' } : undefined,
                    '&.Mui-disabled': { opacity: 1 },
                  }}
                >
                  <Typography
                    variant="caption"
                    color="text.secondary"
                    sx={{
                      width: 112,
                      flex: '0 0 auto',
                      pt: 0.2,
                      fontVariantNumeric: 'tabular-nums',
                    }}
                  >
                    {event.timestamp}
                  </Typography>
                  <Box
                    sx={{
                      width: 26,
                      height: 26,
                      mr: 1.25,
                      flex: '0 0 auto',
                      display: 'grid',
                      placeItems: 'center',
                      borderRadius: 1,
                      bgcolor: 'surface.secondary',
                    }}
                  >
                    {eventIcon(event.type)}
                  </Box>
                  <Box sx={{ flex: 1, minWidth: 0 }}>
                    <Typography sx={{ fontWeight: 650 }}>{event.title}</Typography>
                    <Typography variant="body2" color="text.secondary" sx={{ mt: 0.15 }}>
                      {event.description}
                    </Typography>
                    <Stack
                      direction="row"
                      spacing={1.5}
                      useFlexGap
                      sx={{ mt: 0.45, flexWrap: 'wrap' }}
                    >
                      <Typography variant="caption" color="text.secondary">
                        Source: {event.source ?? event.actor ?? 'Unknown'}
                      </Typography>
                      <Typography variant="caption" color="text.secondary">
                        Affects: {event.affectedEntity ?? affectedNode?.title ?? 'Workflow trunk'}
                      </Typography>
                      {event.reason && (
                        <Typography variant="caption" color="text.secondary">
                          Why: {event.reason}
                        </Typography>
                      )}
                    </Stack>
                  </Box>
                </ButtonBase>
              );
            })}
            {onViewFullHistory && allEvents.length > events.length && (
              <Box sx={{ px: 2, py: 1, textAlign: 'right' }}>
                <Button size="small" onClick={onViewFullHistory}>
                  View full history
                </Button>
              </Box>
            )}
          </>
        )}
      </Paper>
    </Box>
  );
}
