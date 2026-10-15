import { useEffect, useState } from 'react';
import { Box, Paper, Typography } from '@mui/material';
import { historyApi, type HistoryApi } from '../../../api/history';
import {
  loadWorkItemHistory,
  type WorkItemHistoryState,
} from '../../../history/workItemHistoryLoad.ts';
import { HistoryTimeline } from '../../history/HistoryTimeline.tsx';

/**
 * The Workspace History tab: the same authoritative M12 History as the global
 * Timeline, scoped to this WorkItem by its recorded work_item_id linkage.
 */
export default function WorkItemScopedHistory({
  workItemId,
  api = historyApi,
}: {
  workItemId: string;
  api?: Pick<HistoryApi, 'listWorkItemEvents'>;
}) {
  const [state, setState] = useState<{ workItemId: string; value: WorkItemHistoryState } | null>(
    null,
  );
  useEffect(() => {
    let active = true;
    void loadWorkItemHistory(api, workItemId).then(
      (value) => active && setState({ workItemId, value }),
    );
    return () => {
      active = false;
    };
  }, [api, workItemId]);

  const current: WorkItemHistoryState =
    state && state.workItemId === workItemId ? state.value : { status: 'loading' };

  return (
    <Paper variant="outlined" sx={{ overflow: 'hidden' }}>
      <Box
        sx={{ px: 1.25, py: 1, bgcolor: 'surface.subtle', borderBottom: 1, borderColor: 'divider' }}
      >
        <Typography variant="h2">History</Typography>
        <Typography variant="caption" color="text.secondary">
          Durable record of what happened to this Work Item, newest first.
        </Typography>
      </Box>
      {current.status === 'loading' && (
        <Box sx={{ py: 7, textAlign: 'center' }}>
          <Typography color="text.secondary">Loading History…</Typography>
        </Box>
      )}
      {current.status === 'error' && (
        <Box sx={{ py: 7, textAlign: 'center' }} role="alert">
          <Typography color="error">History could not be loaded.</Typography>
        </Box>
      )}
      {current.status === 'ready' && (
        <>
          <HistoryTimeline
            events={current.events}
            asOf={current.asOf}
            showWorkItem={false}
            emptyText="No History has been recorded for this Work Item."
          />
          {current.truncated && (
            <Box
              sx={{ px: 2, py: 1, borderTop: 1, borderColor: 'divider', bgcolor: 'surface.subtle' }}
            >
              <Typography variant="caption" color="text.secondary">
                Showing the most recent 200 events; older History is not shown.
              </Typography>
            </Box>
          )}
        </>
      )}
    </Paper>
  );
}
