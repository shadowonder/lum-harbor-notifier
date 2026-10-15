import BoltRounded from '@mui/icons-material/BoltRounded';
import ChevronRightRounded from '@mui/icons-material/ChevronRightRounded';
import SearchRounded from '@mui/icons-material/SearchRounded';
import {
  Box, Button, ButtonBase, Collapse, Divider, InputAdornment, MenuItem, Paper,
  Select, Stack, Table, TableBody, TableCell, TableContainer, TableHead, TableRow,
  TextField, ToggleButton, ToggleButtonGroup, Typography,
} from '@mui/material';
import { useEffect, useMemo, useState, type MouseEvent } from 'react';
import { historyApi, type CanonicalHistoryScope, type HistoryEventDto, type HistoryTraceStage, type HistoryTraceSummaryDto } from '../api/history';
import { DetailField, HistoryTimeline } from '../components/history/HistoryTimeline';
import { CausalChainDialog } from '../components/history/CausalChainDialog';
import PageContainer from '../components/layout/PageContainer';
import PageHeader from '../components/layout/PageHeader';

type HistoryView = 'timeline' | 'traces' | 'audit';
type DateRange = 7 | 30 | 90;

interface HistoryProps { onOpenWorkItem?: (id: string, title: string) => void }

const stageLabels: Record<HistoryTraceStage, string> = {
  source: 'Source', evidence: 'Evidence', state: 'Fact / State', evaluation: 'Rule / Evaluation',
  attention: 'Attention Issue', notification: 'Notification', action: 'User Action', resolution: 'Resolution',
};
const canonicalScopeLabels: Record<CanonicalHistoryScope, string> = { TRACKING: 'Tracking', ATTENTION: 'Attention', NOTIFICATION: 'Notification', PROPOSAL: 'Proposal', WORK_ITEM: 'Work Items', PLANNER: 'Planner', SOURCE: 'Source', AI: 'AI' };

const dateTimeFormatter = new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' });

function TracesView({ traces, selectedTraceId, selectedEvents, listLoading, traceLoading, error, listTruncated, traceTruncated, onSelectTrace, onOpenWorkItem }: {
  traces: HistoryTraceSummaryDto[]; selectedTraceId: string | null; selectedEvents: HistoryEventDto[]; listLoading: boolean; traceLoading: boolean;
  error: string | null; listTruncated: boolean; traceTruncated: boolean;
  onSelectTrace: (id: string) => void; onOpenWorkItem?: (id: string, title: string) => void;
}) {
  if (listLoading) return <Box sx={{ p: 3 }}><Typography color="text.secondary">Loading traces…</Typography></Box>;
  if (error) return <Box sx={{ p: 3 }}><Typography color="error.main">{error}</Typography></Box>;
  const trace = traces.find((entry) => entry.id === selectedTraceId) ?? traces[0];
  if (!trace) return <Box sx={{ p: 3 }}><Typography color="text.secondary">No traces are available.</Typography></Box>;
  if (traceLoading) return <Box sx={{ p: 3 }}><Typography color="text.secondary">Loading trace…</Typography></Box>;
  const first = selectedEvents[0];
  if (!first) return <Box sx={{ p: 3 }}><Typography color="text.secondary">This trace could not be loaded.</Typography></Box>;
  return <Box sx={{ display: 'grid', gridTemplateColumns: { xs: '1fr', md: '240px minmax(0, 1fr)' }, minHeight: 430 }}>
    <Box sx={{ borderRight: { md: 1 }, borderBottom: { xs: 1, md: 0 }, borderColor: 'divider', p: 1 }}>
      <Typography variant="caption" color="text.secondary" sx={{ px: 1, py: 0.75, display: 'block', fontWeight: 700, letterSpacing: '0.08em', textTransform: 'uppercase' }}>Available traces</Typography>
      {traces.map((entry) => <ButtonBase key={entry.id} onClick={() => onSelectTrace(entry.id)} sx={{ width: '100%', p: 1, mb: 0.4, borderRadius: 1, textAlign: 'left', alignItems: 'flex-start', bgcolor: entry.id === trace.id ? 'surface.secondary' : 'transparent', '&:hover': { bgcolor: 'action.hover' } }}><Box sx={{ minWidth: 0 }}><Typography variant="body2" sx={{ fontWeight: 650 }}>{entry.title}</Typography><Typography variant="caption" color="text.secondary">{entry.eventCount} linked events · {dateTimeFormatter.format(new Date(entry.firstOccurredAt))}</Typography></Box></ButtonBase>)}
    </Box>
    <Box sx={{ minWidth: 0, px: { xs: 2, md: 3 }, py: 2.25 }}>
      {(listTruncated || traceTruncated) && <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 1 }}>{traceTruncated ? 'This trace shows the oldest 200 events.' : 'Showing the newest 100 available traces.'}</Typography>}
      <Stack direction="row" spacing={1.5} sx={{ alignItems: 'flex-start', justifyContent: 'space-between', mb: 2 }}><Box><Typography component="h2" variant="h2">{first.workItemTitle ?? first.subject.label}</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.35 }}>Why Lum Harbor acted, and what happened next.</Typography></Box>{first.workItemId && first.workItemTitle && onOpenWorkItem && <Button size="small" variant="outlined" onClick={() => onOpenWorkItem(first.workItemId as string, first.workItemTitle as string)}>Open Work Item</Button>}</Stack>
      {selectedEvents.map((event, index) => <Box key={event.id} sx={{ display: 'grid', gridTemplateColumns: '28px minmax(0, 1fr)', columnGap: 1.25 }}>
        <Box sx={{ position: 'relative', display: 'flex', justifyContent: 'center' }}>{index < selectedEvents.length - 1 && <Box aria-hidden="true" sx={{ position: 'absolute', top: 21, bottom: -8, width: 1, bgcolor: 'divider' }} />}<Box sx={{ zIndex: 1, width: 20, height: 20, display: 'grid', placeItems: 'center', borderRadius: '50%', border: 1, borderColor: event.traceStage === 'resolution' ? 'success.main' : 'divider', bgcolor: 'background.paper', color: event.traceStage === 'resolution' ? 'success.main' : 'text.secondary' }}><ChevronRightRounded sx={{ fontSize: 14 }} /></Box></Box>
        <Box sx={{ pb: 1.7 }}><Stack direction="row" spacing={1} sx={{ alignItems: 'baseline', justifyContent: 'space-between' }}><Typography variant="caption" color="primary.main" sx={{ fontWeight: 700, letterSpacing: '0.06em', textTransform: 'uppercase' }}>{event.traceStage ? stageLabels[event.traceStage] : 'Event'}</Typography><Typography variant="caption" color="text.secondary">{dateTimeFormatter.format(new Date(event.occurredAt))}</Typography></Stack><Typography sx={{ mt: 0.2, fontWeight: 650 }}>{event.title}</Typography><Typography variant="body2" color="text.secondary" sx={{ mt: 0.25 }}>{event.summary}</Typography>{(event.reason || event.source) && <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.45 }}>{event.reason ?? event.source}</Typography>}{event.causationId && <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 0.25 }}>Direct cause · {event.causationId}</Typography>}</Box>
      </Box>)}
    </Box>
  </Box>;
}

function AuditView({ events, onInspectCausalChain }: { events: HistoryEventDto[]; onInspectCausalChain?: (id: string) => void }) {
  const [expandedId, setExpandedId] = useState<string | null>(null);
  return <TableContainer sx={{ maxHeight: 570 }}><Table size="small" stickyHeader aria-label="History audit events" sx={{ minWidth: 980 }}>
    <TableHead><TableRow><TableCell width={150}>Timestamp</TableCell><TableCell width={145}>Actor / Source</TableCell><TableCell width={180}>Subject</TableCell><TableCell>Event / Change</TableCell><TableCell width={190}>Before → After</TableCell><TableCell width={140}>Correlation ID</TableCell></TableRow></TableHead>
    <TableBody>{events.length === 0 ? <TableRow><TableCell colSpan={6} sx={{ py: 7, textAlign: 'center', color: 'text.secondary' }}>No audit events match these filters.</TableCell></TableRow> : events.flatMap((event) => {
      const expanded = expandedId === event.id;
      return [
        <TableRow key={event.id} hover onClick={() => setExpandedId(expanded ? null : event.id)} sx={{ cursor: 'pointer', '& td': { borderBottom: expanded ? 0 : undefined } }}>
          <TableCell sx={{ fontVariantNumeric: 'tabular-nums', whiteSpace: 'nowrap' }}>{dateTimeFormatter.format(new Date(event.occurredAt))}</TableCell>
          <TableCell>{event.actor.label}<Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>{event.source ?? event.actor.kind}</Typography></TableCell>
          <TableCell>{event.subject.label}<Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>{event.subject.type} · {event.subject.id}</Typography></TableCell>
          <TableCell><Typography variant="caption" color="text.secondary" sx={{ fontFamily: 'monospace' }}>{event.eventType}</Typography><Typography variant="body2">{event.title}</Typography></TableCell>
          <TableCell>{event.before || event.after ? `${event.before ?? '—'} → ${event.after ?? '—'}` : '—'}</TableCell><TableCell sx={{ fontFamily: 'monospace', fontSize: 11 }}>{event.correlationId ?? '—'}</TableCell>
        </TableRow>,
        <TableRow key={`${event.id}-detail`}><TableCell colSpan={6} sx={{ p: 0, borderBottom: expanded ? 1 : 0 }}><Collapse in={expanded} unmountOnExit><Box sx={{ px: 2, py: 1.5, bgcolor: 'surface.subtle', display: 'grid', gridTemplateColumns: 'repeat(3, minmax(0, 1fr))', gap: 1.25 }}><DetailField label="Subject / Entity" value={`${event.subject.type}:${event.subject.id}`} /><DetailField label="Before" value={event.before ?? '—'} /><DetailField label="After" value={event.after ?? '—'} /><DetailField label="Source" value={event.source ?? event.actor.label} /><DetailField label="Causation ID" value={event.causationId ?? '—'} /><DetailField label="Correlation ID" value={event.correlationId ?? '—'} />{event.details?.map((detail) => <DetailField key={detail.label} label={detail.label} value={detail.value} />)}{onInspectCausalChain && <Box sx={{ gridColumn: 'span 3', mt: 0.5 }}><Button size="small" variant="outlined" sx={{ textTransform: 'none', fontSize: 12 }} onClick={(e) => { e.stopPropagation(); onInspectCausalChain(event.id); }}>Inspect Complete Causal Lineage</Button></Box>}</Box></Collapse></TableCell></TableRow>,
      ];
    })}</TableBody>
  </Table></TableContainer>;
}

export default function History({ onOpenWorkItem }: HistoryProps) {
  const [view, setView] = useState<HistoryView>('timeline');
  const [range, setRange] = useState<DateRange>(30);
  const [scope, setScope] = useState<CanonicalHistoryScope | 'all'>('all');
  const [search, setSearch] = useState('');
  const [events, setEvents] = useState<HistoryEventDto[]>([]);
  const [asOf, setAsOf] = useState('');
  const [selectedTraceId, setSelectedTraceId] = useState<string | null>(null);
  const [traces, setTraces] = useState<HistoryTraceSummaryDto[]>([]);
  const [selectedTraceEvents, setSelectedTraceEvents] = useState<HistoryEventDto[]>([]);
  const [loading, setLoading] = useState(true);
  const [traceLoading, setTraceLoading] = useState(false);
  const [traceListLoading, setTraceListLoading] = useState(true);
  const [eventError, setEventError] = useState<string | null>(null);
  const [traceError, setTraceError] = useState<string | null>(null);
  const [truncated, setTruncated] = useState(false);
  const [traceListTruncated, setTraceListTruncated] = useState(false);
  const [traceTruncated, setTraceTruncated] = useState(false);

  useEffect(() => { let active = true; historyApi.listTraces().then((response) => { if (active) { setTraces(response.traces); setTraceListTruncated(response.truncated); setSelectedTraceId((current) => current ?? response.traces[0]?.id ?? null); } }).catch(() => { if (active) setTraceError('History traces could not be loaded.'); }).finally(() => { if (active) setTraceListLoading(false); }); return () => { active = false; }; }, []);
  useEffect(() => { let active = true; const timer = window.setTimeout(() => { if (active) { setLoading(true); setEventError(null); } historyApi.listEvents({ days: range, scope: scope === 'all' ? undefined : scope, search }).then((response) => { if (active) { setEvents(response.events); setAsOf(response.asOf); setTruncated(response.truncated); } }).catch(() => { if (active) setEventError('History could not be loaded.'); }).finally(() => { if (active) setLoading(false); }); }, 180); return () => { active = false; window.clearTimeout(timer); }; }, [range, scope, search]);
  useEffect(() => { if (!selectedTraceId) return; let active = true; Promise.resolve().then(() => { if (active) { setTraceLoading(true); setTraceError(null); setSelectedTraceEvents([]); } return historyApi.getTrace(selectedTraceId); }).then((response) => { if (active) { setSelectedTraceEvents(response.events); setTraceTruncated(response.truncated); } }).catch(() => { if (active) { setSelectedTraceEvents([]); setTraceError('This trace could not be loaded.'); } }).finally(() => { if (active) setTraceLoading(false); }); return () => { active = false; }; }, [selectedTraceId]);
  const traceIds = useMemo(() => new Set(traces.map((trace) => trace.id)), [traces]);
  const visibleEvents = useMemo(() => events.map((event) => ({ ...event, traceId: traceIds.has(event.correlationId ?? '') ? event.correlationId : undefined })), [events, traceIds]);
  const timelineEvents = visibleEvents.filter((event) => event.meaningful);

  const [inspectEventId, setInspectEventId] = useState<string | null>(null);

  const openTrace = (traceId: string) => { setSelectedTraceId(traceId); setView('traces'); };
  const changeView = (_event: MouseEvent<HTMLElement>, next: HistoryView | null) => { if (next) setView(next); };
  return <PageContainer>
    <PageHeader eyebrow="Facts, decisions, outcomes" title="History" subtitle="Understand what changed, why Lum Harbor acted, and what happened next." status={`${events.length} retained events · ${traces.length} linked traces`} />
    <Paper variant="outlined" sx={{ overflow: 'hidden', minWidth: 0 }}>
      <Box sx={{ px: 1.25, py: 1, display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 1.5, flexWrap: 'wrap', bgcolor: 'surface.subtle' }}>
        <ToggleButtonGroup exclusive size="small" value={view} onChange={changeView} aria-label="History view"><ToggleButton value="timeline">Timeline</ToggleButton><ToggleButton value="traces">Traces</ToggleButton><ToggleButton value="audit">Audit</ToggleButton></ToggleButtonGroup>
        {view === 'timeline' && <Stack direction="row" spacing={1} useFlexGap sx={{ alignItems: 'center', flexWrap: 'wrap' }}>
          <TextField size="small" value={search} onChange={(event) => setSearch(event.target.value)} placeholder="Search history" slotProps={{ input: { startAdornment: <InputAdornment position="start"><SearchRounded sx={{ fontSize: 17 }} /></InputAdornment> } }} sx={{ width: { xs: 190, md: 230 } }} />
          <ToggleButtonGroup exclusive size="small" value={range} onChange={(_event, next: DateRange | null) => { if (next) setRange(next); }} aria-label="History date range"><ToggleButton value={7}>7D</ToggleButton><ToggleButton value={30}>30D</ToggleButton><ToggleButton value={90}>90D</ToggleButton></ToggleButtonGroup>
          <Select size="small" value={scope} onChange={(event) => setScope(event.target.value as CanonicalHistoryScope | 'all')} aria-label="History scope" sx={{ minWidth: 154 }}><MenuItem value="all">All scopes</MenuItem>{Object.entries(canonicalScopeLabels).map(([id, label]) => <MenuItem key={id} value={id}>{label}</MenuItem>)}</Select>
        </Stack>}
        {view === 'audit' && <Stack direction="row" spacing={0.75} sx={{ alignItems: 'center', color: 'text.secondary' }}><BoltRounded sx={{ fontSize: 16 }} /><Typography variant="caption">Immutable event detail · newest first</Typography></Stack>}
      </Box>
      <Divider />
      {eventError && view !== 'traces' ? <Box sx={{ py: 7, textAlign: 'center' }}><Typography color="error.main">{eventError}</Typography></Box> : loading && view !== 'traces' ? <Box sx={{ py: 7, textAlign: 'center' }}><Typography color="text.secondary">Loading History…</Typography></Box> : <>
        {truncated && view !== 'traces' && <Box sx={{ px: 2, py: 0.75, borderBottom: 1, borderColor: 'divider' }}><Typography variant="caption" color="text.secondary">Showing the newest 200 matching events. Refine the filters for a complete result.</Typography></Box>}
        {view === 'timeline' && <HistoryTimeline events={timelineEvents} asOf={asOf} onViewTrace={openTrace} onOpenWorkItem={onOpenWorkItem} />}
        {view === 'traces' && <TracesView traces={traces} selectedTraceId={selectedTraceId} selectedEvents={selectedTraceEvents} listLoading={traceListLoading} traceLoading={traceLoading} error={traceError} listTruncated={traceListTruncated} traceTruncated={traceTruncated} onSelectTrace={setSelectedTraceId} onOpenWorkItem={onOpenWorkItem} />}
        {view === 'audit' && <AuditView events={visibleEvents} onInspectCausalChain={setInspectEventId} />}
      </>}
    </Paper>
    <CausalChainDialog eventId={inspectEventId} onClose={() => setInspectEventId(null)} />
  </PageContainer>;
}
