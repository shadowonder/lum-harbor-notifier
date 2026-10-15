import BlockRounded from '@mui/icons-material/BlockRounded';
import CheckRounded from '@mui/icons-material/CheckRounded';
import CloseRounded from '@mui/icons-material/CloseRounded';
import HelpOutlineRounded from '@mui/icons-material/HelpOutlineRounded';
import HourglassEmptyRounded from '@mui/icons-material/HourglassEmptyRounded';
import RadioButtonCheckedRounded from '@mui/icons-material/RadioButtonCheckedRounded';
import RadioButtonUncheckedRounded from '@mui/icons-material/RadioButtonUncheckedRounded';
import { Box, ToggleButton, ToggleButtonGroup, Tooltip, Typography } from '@mui/material';
import type { SvgIconComponent } from '@mui/icons-material';
import { useState } from 'react';
import type { TrackingStatus, WorkItem } from '../../types/workItem';
import { getTrackingStatus, getTrackingSubjects } from '../../presentation/workItems/selectors';
import { normalizeProgressStatus, statusColorToken, statusPresentation } from '../../presentation/workItems/statusPresentation';

const stateIcon: Record<TrackingStatus, SvgIconComponent> = {
  queued: RadioButtonUncheckedRounded,
  inProgress: RadioButtonCheckedRounded,
  complete: CheckRounded,
  verified: CheckRounded,
  waiting: HourglassEmptyRounded,
  behind: CloseRounded,
  failed: CloseRounded,
  blocked: BlockRounded,
  unknown: HelpOutlineRounded,
};

export function TrackingStateMark({ status, showLabel = false }: { status: TrackingStatus; showLabel?: boolean }) {
  const normalized = normalizeProgressStatus(status);
  const presentation = statusPresentation[normalized];
  const Icon = stateIcon[status];
  return <Tooltip title={presentation.label}><Box component="span" aria-label={presentation.label} sx={{ display: 'inline-flex', minWidth: showLabel ? 76 : 26, height: 24, px: showLabel ? 0.6 : 0, alignItems: 'center', justifyContent: 'center', gap: 0.4, borderRadius: 0.75, color: statusColorToken(normalized), bgcolor: presentation.tone === 'danger' || presentation.tone === 'warning' ? 'warning.light' : 'surface.subtle' }}>
    <Icon sx={{ fontSize: 15 }} />{showLabel && <Typography variant="caption" sx={{ color: 'inherit', fontWeight: 650 }}>{presentation.label}</Typography>}
  </Box></Tooltip>;
}

const attentionStatuses: TrackingStatus[] = ['failed', 'blocked', 'unknown', 'waiting', 'behind'];

export default function TrackingMatrix({ item, onSelect }: { item: WorkItem; onSelect?: (subjectId: string) => void }) {
  const [filter, setFilter] = useState<'attention' | 'all'>('attention');
  if (!item.trackingDimensions.length) return <Typography variant="body2" color="text.secondary">No tracking dimensions configured.</Typography>;
  const dimensions = [...item.trackingDimensions].sort((left, right) => left.order - right.order);
  const allSubjects = getTrackingSubjects(item);
  const subjects = filter === 'all' ? allSubjects : allSubjects.filter((subject) => dimensions.some((dimension) => attentionStatuses.includes(getTrackingStatus(item, dimension.id, subject.id))));
  const columns = `minmax(220px, 1fr) repeat(${dimensions.length}, 108px)`;
  return <Box sx={{ minWidth: 0 }}>
    <ToggleButtonGroup exclusive size="small" value={filter} onChange={(_event, value: 'attention' | 'all' | null) => value && setFilter(value)} sx={{ m: 1.25, '& .MuiToggleButton-root': { px: 1.2, py: 0.45, fontSize: 11, textTransform: 'none' } }}>
      <ToggleButton value="attention">Needs Attention</ToggleButton><ToggleButton value="all">All</ToggleButton>
    </ToggleButtonGroup>
    <Box sx={{ maxWidth: '100%', maxHeight: 430, overflow: 'auto', overscrollBehavior: 'contain' }}>
      <Box role="table" aria-label={`${item.profile.trackingLabel} matrix`} sx={{ minWidth: 220 + dimensions.length * 108 }}>
        <Box role="row" sx={{ position: 'sticky', top: 0, zIndex: 3, display: 'grid', gridTemplateColumns: columns, borderBottom: 1, borderColor: 'divider', bgcolor: 'surface.subtle' }}>
          <Typography role="columnheader" variant="caption" color="text.secondary" sx={{ position: 'sticky', left: 0, zIndex: 4, px: 1.5, py: 0.9, bgcolor: 'surface.subtle', fontWeight: 700, letterSpacing: '.06em', textTransform: 'uppercase' }}>Item</Typography>
          {dimensions.map((dimension) => <Typography role="columnheader" key={dimension.id} variant="caption" color="text.secondary" sx={{ px: 1.5, py: 0.9, fontWeight: 700, letterSpacing: '.06em', textTransform: 'uppercase' }}>{dimension.label}</Typography>)}
        </Box>
        {subjects.map((subject) => <Box role="row" key={subject.id} onClick={() => onSelect?.(subject.id)} sx={{ display: 'grid', gridTemplateColumns: columns, alignItems: 'center', borderBottom: 1, borderColor: 'divider', cursor: onSelect ? 'pointer' : 'default', '&:hover': onSelect ? { bgcolor: 'surface.subtle' } : undefined }}>
          <Box role="rowheader" sx={{ position: 'sticky', left: 0, zIndex: 1, minWidth: 0, px: 1.5, py: 0.75, bgcolor: 'background.paper' }}>
            <Typography variant="body2" sx={{ fontWeight: 650 }} noWrap>{subject.label}</Typography>
            {subject.detail !== subject.label && <Typography variant="caption" color="text.secondary" noWrap>{subject.detail}</Typography>}
          </Box>
          {dimensions.map((dimension) => <Box role="cell" key={dimension.id} sx={{ px: 1.5, py: 0.75 }}><TrackingStateMark status={getTrackingStatus(item, dimension.id, subject.id)} /></Box>)}
        </Box>)}
        {!subjects.length && <Typography variant="body2" color="text.secondary" sx={{ px: 1.5, py: 2 }}>No tracking states need attention.</Typography>}
      </Box>
    </Box>
  </Box>;
}
