import type { KeyboardEvent } from 'react';
import { Box, Chip, Paper, Stack, Typography } from '@mui/material';
import type { Attention } from '../../types/workItem';
import { formatDate, formatVariance, getCountdown, getVarianceDays } from '../../presentation/workItems/selectors';
import type { WorkItemSummaryViewModel } from '../../presentation/workItems/workItemViewModels';
import CompactProgressLineage from './CompactProgressLineage';
import type { ProposedWorkItemEntryDto } from '../../api/proposals';

interface WorkItemListProps { items: WorkItemSummaryViewModel[]; onOpen: (item: WorkItemSummaryViewModel) => void; committed?: Record<string, { entries: ProposedWorkItemEntryDto[] }>; proposed?: Record<string, { targetAt?: string; entries: ProposedWorkItemEntryDto[] }> }

const attentionLabels: Record<Attention, string> = {
  high: 'High attention', attention: 'Needs attention', normal: 'On track', low: 'Low attention',
};

function quantity(count: number, singular: string) {
  return `${count} ${singular}${count === 1 ? '' : 's'}`;
}

function primaryException(item: WorkItemSummaryViewModel) {
  const candidates = [
    ['failed', item.attentionSummary.failed],
    ['blocked', item.attentionSummary.blocked],
    ['waiting', item.attentionSummary.waiting],
    ['unknown', item.attentionSummary.unknown],
  ] as const;
  const match = candidates.find(([, count]) => count > 0);
  return match ? `${match[1]} ${match[0]}` : undefined;
}

function WorkItemRow({ item, onOpen, addedEntries = [], proposedTargetAt, proposedEntries = [] }: { item: WorkItemSummaryViewModel; onOpen: (item: WorkItemSummaryViewModel) => void; addedEntries?: ProposedWorkItemEntryDto[]; proposedTargetAt?: string; proposedEntries?: ProposedWorkItemEntryDto[] }) {
  const varianceDays = item.targetAt && item.forecastAt ? getVarianceDays(item.targetAt, item.forecastAt) : undefined;
  const completed = item.lifecycleState === 'completed';
  const attentionTone = item.attention === 'high' ? 'error.main' : item.attention === 'attention' ? 'warning.main' : 'text.secondary';
  const exception = primaryException(item);
  const summary = item.attentionSummary.unresolvedBranches
    ? `${quantity(item.attentionSummary.unresolvedBranches, 'open branch')}${exception ? ` · ${exception}` : ''}`
    : 'All branches resolved';
  const ariaLabel = [
    item.title,
    completed ? 'Completed' : attentionLabels[item.attention],
    completed && item.completedAt
      ? `Completed ${formatDate(item.completedAt)}`
      : item.targetAt ? `Target ${formatDate(item.targetAt)}, ${getCountdown(item.targetAt)}` : undefined,
  ].filter(Boolean).join('. ');
  const openItem = () => onOpen(item);
  const handleKeyDown = (event: KeyboardEvent<HTMLElement>) => {
    if (event.key !== 'Enter' && event.key !== ' ') return;
    event.preventDefault();
    openItem();
  };

  return <Paper
    component="article"
    variant="outlined"
    role="button"
    tabIndex={0}
    aria-label={ariaLabel}
    onClick={openItem}
    onKeyDown={handleKeyDown}
    sx={{
      display: 'grid',
      gridTemplateColumns: 'minmax(0, 1fr)',
      gridTemplateAreas: '"meta" "action" "graph"',
      columnGap: 'clamp(18px, 2.4vw, 34px)',
      rowGap: 1,
      width: '100%', minWidth: 0, px: 'clamp(14px, 1.7vw, 22px)', py: 1.25,
      overflow: 'hidden', color: 'text.primary', bgcolor: completed ? 'surface.subtle' : 'background.paper', cursor: 'pointer',
      transition: 'border-color 120ms ease, background-color 120ms ease, box-shadow 120ms ease, transform 120ms ease',
      '@container (min-width: 650px)': {
        gridTemplateColumns: 'minmax(0, 1fr) minmax(220px, .8fr)',
        gridTemplateAreas: '"meta action" "graph graph"',
      },
      '@container (min-width: 1050px)': {
        gridTemplateColumns: 'minmax(210px, 260px) minmax(280px, 1fr) minmax(230px, 290px)',
        gridTemplateAreas: '"meta graph action"',
        alignItems: 'center',
      },
      '&:hover': { borderColor: 'primary.main', bgcolor: 'surface.subtle', boxShadow: 1, transform: 'translateY(-1px)' },
      '&:focus-visible': { outline: '2px solid', outlineColor: 'primary.main', outlineOffset: 2, borderColor: 'primary.main' },
    }}
  >
    <Box sx={{ gridArea: 'meta', minWidth: 0, alignSelf: 'start' }}>
      <Typography component="h2" sx={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontSize: 14.5, lineHeight: 1.35, fontWeight: 730 }}>
        {item.title}
      </Typography>
      <Stack direction="row" spacing={0.4} useFlexGap sx={{ mt: 0.55, minWidth: 0, flexWrap: 'wrap' }}>
        {item.labels.slice(0, 3).map((label) => <Chip key={label} size="small" variant="outlined" label={label} sx={{ height: 18, color: 'text.secondary', borderColor: 'divider', bgcolor: 'background.paper', fontSize: 9, '& .MuiChip-label': { px: 0.7 } }} />)}
      </Stack>
      <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.7 }}>{summary}</Typography>
      {item.readiness.required > 0 && <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.1 }}>
        {item.readiness.ready}/{item.readiness.required} required ready
      </Typography>}
      {item.latestProgress.length > 0 && <Box sx={{ mt: 0.65, minWidth: 0 }}>
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', fontSize: 9, fontWeight: 720, letterSpacing: '.045em', textTransform: 'uppercase' }}>
          {completed ? 'Final progress' : 'Latest progress'}
        </Typography>
        {item.latestProgress.map((fact) => <Typography key={fact.id} variant="caption" sx={{ display: 'block', mt: 0.05, overflow: 'hidden', color: 'text.primary', textOverflow: 'ellipsis', whiteSpace: 'nowrap', lineHeight: 1.35 }}>
          <Box component="span" sx={{ color: 'text.secondary' }}>{fact.date} · </Box>{fact.label}
        </Typography>)}
      </Box>}
    </Box>

    <Box sx={{ gridArea: 'graph', minWidth: 0, width: '100%', maxWidth: 590, mx: 'auto', alignSelf: 'center' }}>
      <Box sx={{ display: 'block', '@container (min-width: 1050px)': { display: 'none' } }}>
        <CompactProgressLineage model={item.narrowCompactLineage} addedEntries={addedEntries} proposedEntries={proposedEntries} />
      </Box>
      <Box sx={{ display: 'none', '@container (min-width: 1050px)': { display: 'block' } }}>
        <CompactProgressLineage model={item.compactLineage} addedEntries={addedEntries} proposedEntries={proposedEntries} />
      </Box>
    </Box>

    <Box sx={{ gridArea: 'action', minWidth: 0, alignSelf: 'start', pl: { xs: 0, sm: 0.5 } }}>
      <Typography variant="caption" sx={{ display: 'block', color: completed ? 'success.main' : attentionTone, fontWeight: 760, letterSpacing: '.045em', textTransform: 'uppercase' }}>
        {completed ? 'Completed' : attentionLabels[item.attention]}
      </Typography>
      {completed && item.completedAt ? <Typography sx={{ mt: 0.25, fontSize: 12.5, lineHeight: 1.35, fontWeight: 680 }}>
        {formatDate(item.completedAt)}
      </Typography> : item.targetAt && <Typography sx={{ mt: 0.25, fontSize: 12.5, lineHeight: 1.35, fontWeight: 680, opacity: proposedTargetAt ? .48 : 1, textDecoration: proposedTargetAt ? 'line-through' : 'none' }}>
        {formatDate(item.targetAt)} <Typography component="span" variant="caption" color="text.secondary">· {getCountdown(item.targetAt)}</Typography>
      </Typography>}
      {proposedTargetAt && <Box sx={{ display: 'inline-block', mt: .45, px: .65, py: .3, border: 1, borderStyle: 'dashed', borderColor: '#7969C7', borderRadius: .6, bgcolor: 'rgba(121, 105, 199, .09)', color: '#7969C7' }}><Typography variant="caption" sx={{ fontWeight: 750 }}>{formatDate(proposedTargetAt)} · Proposed</Typography></Box>}
      {completed && item.duration && <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.2 }}>{item.duration}</Typography>}
      {!completed && item.forecastAt && <Typography variant="caption" sx={{ display: 'block', mt: 0.2, color: varianceDays && varianceDays > 0 ? 'warning.main' : 'text.secondary', fontWeight: 620 }}>
        Forecast {formatDate(item.forecastAt)}{varianceDays ? ` · ${formatVariance(varianceDays)}` : ''}
      </Typography>}
      {completed && item.finalOutcome && <Box sx={{ mt: 0.75, minWidth: 0 }}>
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', fontSize: 9, fontWeight: 720, letterSpacing: '.045em', textTransform: 'uppercase' }}>Outcome</Typography>
        <Typography variant="caption" sx={{ display: '-webkit-box', mt: 0.05, overflow: 'hidden', WebkitBoxOrient: 'vertical', WebkitLineClamp: 2, color: 'text.primary', lineHeight: 1.35 }}>{item.finalOutcome}</Typography>
      </Box>}
      {!completed && item.nextAction && <Box sx={{ mt: 0.75, minWidth: 0 }}>
        <Typography variant="caption" color="text.secondary" sx={{ display: 'block', fontSize: 9, fontWeight: 720, letterSpacing: '.045em', textTransform: 'uppercase' }}>Next</Typography>
        <Typography variant="caption" sx={{ display: '-webkit-box', mt: 0.05, overflow: 'hidden', WebkitBoxOrient: 'vertical', WebkitLineClamp: 2, color: 'text.primary', lineHeight: 1.35 }}>
          {item.nextAction}
        </Typography>
      </Box>}
    </Box>
  </Paper>;
}

export default function WorkItemList({ items, onOpen, committed = {}, proposed = {} }: WorkItemListProps) {
  return <Stack spacing={1}>{items.map((item) => <WorkItemRow key={item.id} item={item} onOpen={onOpen} addedEntries={committed[item.id]?.entries} proposedTargetAt={proposed[item.id]?.targetAt} proposedEntries={proposed[item.id]?.entries} />)}</Stack>;
}
