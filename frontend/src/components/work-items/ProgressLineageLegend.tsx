import { Box, Stack, Typography } from '@mui/material';
import type { ProgressStatusDto } from '../../api/workItems';
import { statusColorToken, statusPresentation } from '../../presentation/workItems/statusPresentation';

export default function ProgressLineageLegend() {
  const statuses: ProgressStatusDto[] = ['completed', 'in_progress', 'queued', 'waiting', 'blocked', 'failed', 'unknown', 'target'];
  return <Stack direction="row" spacing={1.35} useFlexGap sx={{ flexWrap: 'wrap' }}>
    {statuses.map((status) => <Typography key={status} variant="caption" color="text.secondary"><Box component="span" sx={{ mr: 0.35, color: statusColorToken(status), fontWeight: 800 }}>{statusPresentation[status].symbol}</Box>{statusPresentation[status].label}</Typography>)}
  </Stack>;
}
