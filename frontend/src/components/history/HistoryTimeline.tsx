import ExpandMoreRounded from '@mui/icons-material/ExpandMoreRounded';
import AutoAwesomeRounded from '@mui/icons-material/AutoAwesomeRounded';
import FactCheckOutlined from '@mui/icons-material/FactCheckOutlined';
import NotificationsActiveOutlined from '@mui/icons-material/NotificationsActiveOutlined';
import TodayOutlined from '@mui/icons-material/TodayOutlined';
import WorkOutlineRounded from '@mui/icons-material/WorkOutlineRounded';
import { Box, Button, ButtonBase, Chip, Collapse, Stack, Typography } from '@mui/material';
import type { SvgIconComponent } from '@mui/icons-material';
import { useState } from 'react';
import type { HistoryEventDto, HistoryScope } from '../../api/history';

const scopePresentation: Record<HistoryScope, { label: string; icon: SvgIconComponent }> = {
  planner: { label: 'Planner', icon: TodayOutlined },
  workItems: { label: 'Work Items', icon: WorkOutlineRounded },
  attention: { label: 'Attention', icon: NotificationsActiveOutlined },
  automation: { label: 'AI & Automation', icon: AutoAwesomeRounded },
  evidence: { label: 'Evidence', icon: FactCheckOutlined },
};

const timeFormatter = new Intl.DateTimeFormat('en-US', { hour: 'numeric', minute: '2-digit' });

function groupLabel(occurredAt: string, asOf: string) {
  const event = new Date(occurredAt);
  const anchor = new Date(asOf);
  const dateKey = (date: Date) => `${date.getFullYear()}-${date.getMonth()}-${date.getDate()}`;
  const yesterday = new Date(anchor);
  yesterday.setDate(yesterday.getDate() - 1);
  if (dateKey(event) === dateKey(anchor)) return 'Today';
  if (dateKey(event) === dateKey(yesterday)) return 'Yesterday';
  return new Intl.DateTimeFormat('en-US', { month: 'long', day: 'numeric' }).format(event);
}

export function DetailField({ label, value }: { label: string; value?: string }) {
  if (!value) return null;
  return <Box sx={{ minWidth: 0 }}><Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 0.25, fontWeight: 650 }}>{label}</Typography><Typography variant="body2" sx={{ overflowWrap: 'anywhere' }}>{value}</Typography></Box>;
}

export function HistoryTimeline({ events, asOf, onViewTrace, onOpenWorkItem, showWorkItem = true, emptyText = 'No meaningful events match these filters.' }: {
  events: HistoryEventDto[]; asOf: string; onViewTrace?: (traceId: string) => void;
  /** Global History names the owning WorkItem per row; a WorkItem-scoped view already is that WorkItem. */
  showWorkItem?: boolean;
  emptyText?: string;
  onOpenWorkItem?: (id: string, title: string) => void;
}) {
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const groups = events.reduce<Array<{ label: string; events: HistoryEventDto[] }>>((result, event) => {
    const label = groupLabel(event.occurredAt, asOf);
    const existing = result.at(-1);
    if (existing?.label === label) existing.events.push(event);
    else result.push({ label, events: [event] });
    return result;
  }, []);

  if (!events.length) return <Box sx={{ py: 7, textAlign: 'center' }}><Typography color="text.secondary">{emptyText}</Typography></Box>;

  return groups.map((group) => <Box component="section" key={group.label} aria-labelledby={`history-${group.label}`}>
    <Box sx={{ px: 2, py: 0.8, borderBottom: 1, borderColor: 'divider', bgcolor: 'surface.subtle' }}><Typography id={`history-${group.label}`} variant="caption" color="text.secondary" sx={{ fontWeight: 700, letterSpacing: '0.08em', textTransform: 'uppercase' }}>{group.label}</Typography></Box>
    {group.events.map((event) => {
      const expanded = expandedId === event.id;
      const ScopeIcon = scopePresentation[event.scope].icon;
      return <Box key={event.id} sx={{ borderBottom: 1, borderColor: 'divider', '&:last-child': { borderBottom: 0 } }}>
        <ButtonBase onClick={() => setExpandedId(expanded ? null : event.id)} aria-expanded={expanded} sx={{ width: '100%', px: 2, py: 1.15, textAlign: 'left', alignItems: 'flex-start', '&:hover': { bgcolor: 'action.hover' } }}>
          <Typography variant="caption" color="text.secondary" sx={{ width: 76, flex: '0 0 auto', pt: 0.25, fontVariantNumeric: 'tabular-nums' }}>{timeFormatter.format(new Date(event.occurredAt))}</Typography>
          <Box sx={{ width: 26, height: 26, mr: 1.25, flex: '0 0 auto', display: 'grid', placeItems: 'center', borderRadius: 1, bgcolor: 'surface.secondary', color: 'text.secondary' }}><ScopeIcon sx={{ fontSize: 15 }} /></Box>
          <Box sx={{ flex: 1, minWidth: 0 }}>
            <Stack direction="row" spacing={0.8} useFlexGap sx={{ alignItems: 'center', flexWrap: 'wrap' }}><Typography sx={{ fontWeight: 650 }}>{event.title}</Typography><Chip label={scopePresentation[event.scope].label} size="small" variant="outlined" sx={{ height: 20, '& .MuiChip-label': { px: 0.75, fontSize: 10.5 } }} /></Stack>
            <Typography variant="body2" color="text.secondary" sx={{ mt: 0.2 }}>{event.summary}</Typography>
            {showWorkItem && event.workItemTitle && <Typography variant="caption" color="primary.main" sx={{ display: 'block', mt: 0.35 }}>{event.workItemTitle}</Typography>}
          </Box>
          <ExpandMoreRounded sx={{ ml: 1, mt: 0.15, fontSize: 18, color: 'text.secondary', transform: expanded ? 'rotate(180deg)' : 'none', transition: 'transform 140ms ease' }} />
        </ButtonBase>
        <Collapse in={expanded} unmountOnExit>
          <Box sx={{ ml: { xs: 2, sm: '118px' }, mr: 2, mb: 1.5, p: 1.5, border: 1, borderColor: 'divider', borderRadius: 1, bgcolor: 'surface.subtle' }}>
            <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: 'repeat(2, minmax(0, 1fr))' }, gap: 1.25 }}>
              <DetailField label="Before" value={event.before} /><DetailField label="After" value={event.after} /><DetailField label="Source / Evidence" value={event.source} /><DetailField label="Reason" value={event.reason} /><DetailField label="Related entity" value={`${event.subject.label} · ${event.subject.type}`} />
              {event.details?.map((detail) => <DetailField key={detail.label} label={detail.label} value={detail.value} />)}
            </Box>
            <Stack direction="row" spacing={1} sx={{ mt: 1.4 }}>
              {onViewTrace && event.traceId && <Button size="small" variant="outlined" onClick={() => onViewTrace(event.traceId as string)}>View Trace</Button>}
              {event.workItemId && event.workItemTitle && onOpenWorkItem && <Button size="small" onClick={() => onOpenWorkItem(event.workItemId as string, event.workItemTitle as string)}>Open Work Item</Button>}
            </Stack>
          </Box>
        </Collapse>
      </Box>;
    })}
  </Box>);
}

