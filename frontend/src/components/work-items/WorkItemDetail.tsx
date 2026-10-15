import ArrowBackRounded from '@mui/icons-material/ArrowBackRounded';
import ErrorOutlineRounded from '@mui/icons-material/ErrorOutlineRounded';
import { useEffect, useState } from 'react';
import { Box, Button, Chip, Divider, Paper, Stack, Tab, Tabs, Typography } from '@mui/material';
import type { ProgressNavigationDto, WorkItemActivityDto } from '../../api/workItems';
import { BrowserOpenURL } from '../../../wailsjs/runtime/runtime';
import type { WorkItemDetailViewModel } from '../../presentation/workItems/workItemViewModels';
import {
  formatDate,
  formatVariance,
  getCountdown,
} from '../../presentation/workItems/selectors';
import WorkItemOverview from './WorkItemOverview';
import WorkItemDetails from './WorkItemDetails';
import type { WorkItemProposalProjection } from '../../presentation/proposals/buildPreviewState';

export type WorkItemDetailView = 'Overview' | 'Details' | 'Activity';

interface WorkItemDetailProps {
  model: WorkItemDetailViewModel;
  activity: WorkItemActivityDto[];
  view: WorkItemDetailView;
  onBack: () => void;
  onViewChange: (view: WorkItemDetailView) => void;
  committedProposal?: WorkItemProposalProjection;
  previewProposal?: WorkItemProposalProjection;
}

function Activity({ activity, focusActivityId }: { activity: WorkItemActivityDto[]; focusActivityId?: string }) {
  useEffect(() => {
    if (!focusActivityId) return;
    requestAnimationFrame(() => document.getElementById(`activity-${focusActivityId}`)?.scrollIntoView({ block: 'center', behavior: 'smooth' }));
  }, [focusActivityId]);
  return (
    <Paper variant="outlined" sx={{ p: 2 }}>
      <Typography
        component="h2"
        variant="caption"
        color="text.secondary"
        sx={{ fontWeight: 700, letterSpacing: '0.07em', textTransform: 'uppercase' }}
      >
        Recent Activity
      </Typography>
      <Stack spacing={1.1} divider={<Divider flexItem />} sx={{ mt: 1.2 }}>
        {activity.map((entry) => (
          <Stack
            key={entry.id}
            id={`activity-${entry.id}`}
            direction="row"
            spacing={2}
            sx={{ px: 0.6, py: 0.35, justifyContent: 'space-between', borderRadius: 0.75, bgcolor: entry.id === focusActivityId ? 'primary.light' : 'transparent' }}
          >
            <Typography variant="body2">{entry.label}</Typography>
            <Typography variant="caption" color="text.secondary" sx={{ flexShrink: 0 }}>
              {entry.occurredAt}
            </Typography>
          </Stack>
        ))}
        <Typography variant="body2" color="text.secondary">
          Complete activity history will be designed separately.
        </Typography>
      </Stack>
    </Paper>
  );
}

export default function WorkItemDetail({ model, activity, view, onBack, onViewChange, committedProposal, previewProposal }: WorkItemDetailProps) {
  const item = model.item;
  const [navigation, setNavigation] = useState<ProgressNavigationDto | null>(null);
  const target = model.target;
  const currentTargetAt = committedProposal?.targetAt ?? item.targetAt;
  const proposedTargetAt = previewProposal?.targetAt !== currentTargetAt ? previewProposal?.targetAt : undefined;
  const committedEntryIds = new Set(committedProposal?.entries.map((entry) => entry.id) ?? []);
  const proposedEntries = previewProposal?.entries.filter((entry) => !committedEntryIds.has(entry.id)) ?? [];
  const headerSignals = model.headerSignals;
  const views: WorkItemDetailView[] = ['Overview', 'Details', 'Activity'];
  const attentionLabel =
    item.attention === 'attention'
      ? 'Needs Attention'
      : `${item.attention[0].toUpperCase() + item.attention.slice(1)} Attention`;
  const handleLineageNavigation = (next: ProgressNavigationDto) => {
    setNavigation(next);
    if (next.destination === 'external') {
      try { BrowserOpenURL(next.url); } catch { window.open(next.url, '_blank', 'noopener,noreferrer'); }
      return;
    }
    onViewChange(next.destination === 'activity' ? 'Activity' : 'Details');
  };
  return (
    <>
      <Button
        size="small"
        color="inherit"
        startIcon={<ArrowBackRounded />}
        onClick={onBack}
        sx={{ mb: 1.25, ml: -1, color: 'text.secondary' }}
      >
        Work Items
      </Button>
      <Stack
        direction="row"
        spacing={2}
        useFlexGap
        sx={{ alignItems: 'flex-start', justifyContent: 'space-between', flexWrap: 'wrap' }}
      >
        <Box sx={{ minWidth: 0 }}>
          <Stack direction="row" spacing={1} useFlexGap sx={{ alignItems: 'center', flexWrap: 'wrap' }}>
            <Typography component="h1" variant="h1">{item.title}</Typography>
            <Chip
              size="small" variant="outlined" icon={<ErrorOutlineRounded />} label={attentionLabel}
              sx={{ height: 24, color: item.attention === 'high' ? 'error.main' : 'text.secondary', borderColor: item.attention === 'high' ? 'error.main' : 'divider' }}
            />
          </Stack>
          <Stack direction="row" spacing={0.5} useFlexGap sx={{ mt: 0.8, flexWrap: 'wrap' }}>
            {item.labels.map((label) => (
              <Chip
                key={label}
                size="small"
                variant="outlined"
                label={label}
                sx={{
                  height: 20,
                  color: 'text.secondary',
                  borderColor: 'divider',
                  bgcolor: 'surface.subtle',
                  fontSize: 9,
                }}
              />
            ))}
          </Stack>
        </Box>
        {(target || currentTargetAt) && (
            <Box sx={{ minWidth: 190, px: 1.5, py: 1.1, borderLeft: { xs: 0, sm: 1 }, borderColor: 'divider', textAlign: { xs: 'left', sm: 'right' } }}>
              <Typography variant="caption" color="text.secondary" sx={{ fontWeight: 700, letterSpacing: '.06em', textTransform: 'uppercase' }}>Final target</Typography>
              <Typography sx={{ fontSize: 14, fontWeight: 680, opacity: proposedTargetAt ? .48 : 1, textDecoration: proposedTargetAt ? 'line-through' : 'none' }}>
                {currentTargetAt ? `${formatDate(currentTargetAt)} · ${getCountdown(currentTargetAt)}` : target?.dateLabel}
              </Typography>
              {proposedTargetAt && <Box sx={{ display: 'inline-block', mt: .45, px: .7, py: .35, border: 1, borderStyle: 'dashed', borderColor: '#7969C7', borderRadius: .6, bgcolor: 'rgba(121, 105, 199, .09)', color: '#7969C7' }}><Typography variant="caption" sx={{ fontWeight: 750 }}>{formatDate(proposedTargetAt)} · Proposed</Typography></Box>}
              {target?.forecastAt && (
                <Typography
                  variant="body2"
                  sx={{
                    mt: 0.2,
                    color:
                      target.varianceDays && target.varianceDays > 0
                        ? 'warning.main'
                        : 'text.secondary',
                  }}
                >
                  Forecast {target.forecastDateLabel}
                  {target.varianceDays ? ` · ${formatVariance(target.varianceDays)} late` : ''}
                </Typography>
              )}
            </Box>
          )}
      </Stack>

      <Stack direction="row" spacing={0.65} useFlexGap sx={{ mt: 1.6, flexWrap: 'wrap' }}>
        {headerSignals.map((signal) => (
          <Chip
            key={signal.id}
            size="small"
            variant="outlined"
            label={signal.label}
            sx={{
              height: 24,
              color:
                signal.severity === 'critical'
                  ? 'error.main'
                  : signal.severity === 'warning'
                    ? 'warning.main'
                    : 'text.secondary',
              borderColor: signal.severity === 'neutral' ? 'divider' : 'attention.controlBorder',
              bgcolor: signal.severity === 'neutral' ? 'surface.subtle' : 'warning.light',
            }}
          />
        ))}
      </Stack>

      <Tabs
        value={view}
        onChange={(_event, next: WorkItemDetailView) => onViewChange(next)}
        variant="scrollable"
        scrollButtons="auto"
        allowScrollButtonsMobile
        sx={{
          mt: 2,
          mb: 1.5,
          minHeight: 36,
          borderBottom: 1,
          borderColor: 'divider',
          '& .MuiTab-root': {
            minHeight: 36,
            px: 1.5,
            py: 0.7,
            fontSize: 12,
            textTransform: 'none',
          },
        }}
      >
        {views.map((entry) => (
          <Tab key={entry} value={entry} label={entry} />
        ))}
      </Tabs>
      {view === 'Overview' && <WorkItemOverview model={model} onNavigate={handleLineageNavigation} addedEntries={committedProposal?.entries} proposedEntries={proposedEntries} />}
      {view === 'Details' && <WorkItemDetails key={navigation?.destination === 'details' ? navigation.entityId : 'details'} item={item} initialFocusEntityId={navigation?.destination === 'details' ? navigation.entityId : undefined} />}
      {view === 'Activity' && <Activity activity={activity} focusActivityId={navigation?.destination === 'activity' ? navigation.activityId : undefined} />}
    </>
  );
}
