import ErrorOutlineRounded from '@mui/icons-material/ErrorOutlineRounded';
import HourglassEmptyRounded from '@mui/icons-material/HourglassEmptyRounded';
import HelpOutlineRounded from '@mui/icons-material/HelpOutlineRounded';
import { Box, Chip, Divider, LinearProgress, Paper, Stack, Typography } from '@mui/material';
import { lazy, Suspense } from 'react';
import type { WorkItemDetailViewModel } from '../../presentation/workItems/workItemViewModels';
import type { ProgressNavigationDto } from '../../api/workItems';
import ProgressLineageLegend from './ProgressLineageLegend';
import { normalizeProgressStatus, statusColorToken, statusPresentation } from '../../presentation/workItems/statusPresentation';
import type { ProposedWorkItemEntryDto } from '../../api/proposals';

const sectionTitleSx = { fontWeight: 700, letterSpacing: '0.07em', textTransform: 'uppercase' } as const;
const ExpandedProgressLineage = lazy(() => import('./ExpandedProgressLineage'));

function Panel({ title, subtitle, children }: { title: string; subtitle?: string; children: React.ReactNode }) {
  return <Paper variant="outlined" sx={{ minWidth: 0, p: 2 }}>
    <Typography component="h2" variant="caption" color="text.secondary" sx={sectionTitleSx}>{title}</Typography>
    {subtitle && <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.25 }}>{subtitle}</Typography>}
    <Box sx={{ mt: 1.25 }}>{children}</Box>
  </Paper>;
}

export default function WorkItemOverview({ model, onNavigate, addedEntries = [], proposedEntries = [] }: { model: WorkItemDetailViewModel; onNavigate: (navigation: ProgressNavigationDto) => void; addedEntries?: ProposedWorkItemEntryDto[]; proposedEntries?: ProposedWorkItemEntryDto[] }) {
  const { item, readiness, progress, lineage } = model;
  const exceptions = model.exceptions.slice(0, 6);
  const tracking = model.tracking;
  const trackingIssues = tracking.filter((entry) => entry.state !== 'complete');
  const hasReadiness = readiness.required > 0;
  const hasProgress = progress.total > 0;
  const hasTracking = tracking.length > 0;

  return <Stack spacing={1.5}>
    <Panel title="Progress Lineage" subtitle="Where branches started, how they progressed, and what must still converge">
      <Suspense fallback={<Box sx={{ height: 360, bgcolor: 'surface.subtle', borderRadius: 1 }} />}>
        <ExpandedProgressLineage model={lineage} onNavigate={onNavigate} />
      </Suspense>
      {addedEntries.length > 0 && <Box sx={{ mt: 1, p: 1, border: 1, borderColor: 'primary.main', borderRadius: 1, bgcolor: 'primary.light' }}><Typography variant="caption" color="primary.dark" sx={{ fontWeight: 750, letterSpacing: '.05em', textTransform: 'uppercase' }}>New lineage entries</Typography><Stack direction="row" spacing={.65} useFlexGap sx={{ mt: .6, flexWrap: 'wrap' }}>{addedEntries.map((entry) => <Chip key={entry.id} size="small" variant="outlined" label={entry.title} sx={{ height: 22 }} />)}</Stack></Box>}
      {proposedEntries.length > 0 && <Box sx={{ mt: 1, p: 1, border: 1, borderStyle: 'dashed', borderColor: '#7969C7', borderRadius: 1, bgcolor: 'rgba(121, 105, 199, .09)' }}><Typography variant="caption" sx={{ color: '#7969C7', fontWeight: 750, letterSpacing: '.05em', textTransform: 'uppercase' }}>Proposed lineage branch</Typography><Stack direction="row" spacing={.65} useFlexGap sx={{ mt: .6, alignItems: 'center', flexWrap: 'wrap' }}>{proposedEntries.map((entry) => <Chip key={entry.id} size="small" variant="outlined" label={`${entry.title} · Proposed`} sx={{ height: 22, color: '#7969C7', borderStyle: 'dashed', borderColor: 'currentColor' }} />)}</Stack></Box>}
      <Divider sx={{ my: 1.25 }} />
      <ProgressLineageLegend />
    </Panel>

    <Box sx={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1fr)', gap: 1.5, '@container (min-width: 800px)': { gridTemplateColumns: 'minmax(0, .9fr) minmax(0, 1.1fr)' } }}>
      <Panel title="Readiness" subtitle={hasReadiness ? `${readiness.scopeLabel} readiness` : undefined}>
        {hasReadiness ? <>
          <Stack direction="row" spacing={1.2} sx={{ alignItems: 'baseline' }}>
            <Typography sx={{ fontSize: 20, fontWeight: 730 }}>{readiness.ready} / {readiness.required}</Typography>
            <Typography variant="body2" color="text.secondary">required ready</Typography>
          </Stack>
          <LinearProgress variant="determinate" value={readiness.required ? readiness.ready / readiness.required * 100 : 0} sx={{ mt: 1.1, height: 6, borderRadius: 4, bgcolor: 'surface.secondary' }} />
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.8 }}>
            {readiness.blocked} blocked · {readiness.waiting} waiting · {readiness.unknown} unknown
          </Typography>
        </> : hasProgress ? <>
          <Stack direction="row" spacing={1.2} sx={{ alignItems: 'baseline' }}>
            <Typography sx={{ fontSize: 20, fontWeight: 730 }}>{progress.complete} / {progress.total}</Typography>
            <Typography variant="body2" color="text.secondary">{item.profile.progressLabel} complete</Typography>
          </Stack>
          <LinearProgress variant="determinate" value={progress.percent} sx={{ mt: 1.1, height: 6, borderRadius: 4, bgcolor: 'surface.secondary' }} />
        </> : <Typography variant="body2" color="text.secondary">No readiness conditions are configured.</Typography>}
      </Panel>

      <Panel title="Next Actions">
        <Stack spacing={0.75}>
          {item.nextActions.map((action, index) => <Stack key={action} direction="row" spacing={0.9} sx={{ alignItems: 'flex-start' }}>
            <Typography variant="caption" color="text.secondary" sx={{ width: 16, pt: 0.1, flexShrink: 0 }}>{index + 1}</Typography>
            <Typography variant="body2">{action}</Typography>
          </Stack>)}
        </Stack>
      </Panel>
    </Box>

    {(exceptions.length > 0 || hasTracking) && <Box sx={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1fr)', gap: 1.5, '@container (min-width: 900px)': { gridTemplateColumns: exceptions.length && hasTracking ? 'minmax(0, 1.35fr) minmax(260px, .65fr)' : 'minmax(0, 1fr)' } }}>
      {exceptions.length > 0 && <Panel title="Attention / Exceptions" subtitle="Only failed, blocked, waiting, or unknown states">
        <Stack divider={<Divider flexItem />}>
          {exceptions.map((entry) => {
            const normalized = normalizeProgressStatus(entry.status);
            const Icon = entry.status === 'waiting' ? HourglassEmptyRounded : entry.status === 'unknown' ? HelpOutlineRounded : ErrorOutlineRounded;
            return <Stack component="button" type="button" key={entry.id} direction="row" spacing={1} onClick={() => onNavigate({ destination: 'details', entityId: entry.entityId })} sx={{ width: '100%', py: 0.65, px: 0.35, minWidth: 0, alignItems: 'center', border: 0, borderRadius: 0.75, bgcolor: 'transparent', color: 'text.primary', textAlign: 'left', cursor: 'pointer', '&:hover': { bgcolor: 'surface.subtle' }, '&:focus-visible': { outline: '2px solid', outlineColor: 'primary.main' } }}>
              <Icon sx={{ flexShrink: 0, fontSize: 16, color: statusColorToken(normalized) }} />
              <Box sx={{ minWidth: 0, flex: 1 }}>
                <Typography variant="body2" sx={{ fontWeight: 650 }} noWrap>{entry.title}</Typography>
                <Typography variant="caption" color="text.secondary">{entry.context}</Typography>
              </Box>
              <Typography variant="caption" sx={{ flexShrink: 0, color: statusColorToken(normalized), fontWeight: 700 }}>{statusPresentation[normalized].label}</Typography>
            </Stack>;
          })}
        </Stack>
      </Panel>}
      {hasTracking && <Panel title="Supporting Tracking" subtitle={item.profile.trackingLabel}>
        <Stack spacing={0.8}>
          {trackingIssues.slice(0, 4).map((entry) => <Stack key={entry.dimension.id} direction="row" spacing={1} sx={{ alignItems: 'center', justifyContent: 'space-between' }}>
            <Typography variant="body2" sx={{ fontWeight: 650 }}>{entry.dimension.label}</Typography>
            <Typography variant="caption" color={entry.state === 'atRisk' ? 'error.main' : 'text.secondary'}>{entry.complete}/{entry.total} complete{entry.problems ? ` · ${entry.problems} issues` : ''}</Typography>
          </Stack>)}
        </Stack>
      </Panel>}
    </Box>}
  </Stack>;
}
