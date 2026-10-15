import LaunchRounded from '@mui/icons-material/LaunchRounded';
import { Box, Chip, Divider, Paper, Stack, Typography } from '@mui/material';
import { useEffect, useState } from 'react';
import type { Requirement, TrackableItem, WorkItem } from '../../types/workItem';
import { getActiveCheckpoint, groupItems, groupRequirements } from '../../presentation/workItems/selectors';
import TrackingMatrix from './TrackingMatrix';
import { normalizeProgressStatus, statusColorToken, statusPresentation } from '../../presentation/workItems/statusPresentation';

type Selection = { kind: 'item'; value: TrackableItem } | { kind: 'requirement'; value: Requirement };
const titleSx = { fontWeight: 700, letterSpacing: '.07em', textTransform: 'uppercase' } as const;

function Section({ title, meta, children }: { title: string; meta?: string; children: React.ReactNode }) {
  return <Paper variant="outlined" sx={{ minWidth: 0, overflow: 'hidden' }}>
    <Stack direction="row" spacing={2} sx={{ px: 1.7, py: 1.15, alignItems: 'center', justifyContent: 'space-between', bgcolor: 'surface.subtle', borderBottom: 1, borderColor: 'divider' }}>
      <Typography component="h2" variant="caption" color="text.secondary" sx={titleSx}>{title}</Typography>
      {meta && <Typography variant="caption" color="text.secondary">{meta}</Typography>}
    </Stack>
    {children}
  </Paper>;
}

function Inspector({ selection, item }: { selection: Selection; item: WorkItem }) {
  const value = selection.value;
  const evidence = value.evidenceRefs?.map((id) => item.evidence?.find((entry) => entry.id === id)?.label ?? id) ?? [];
  const trackable = selection.kind === 'item' ? selection.value : undefined;
  const requirement = selection.kind === 'requirement' ? selection.value : undefined;
  const source = trackable?.sourceRefId ? item.sources?.find((entry) => entry.id === trackable.sourceRefId) : undefined;
  const normalizedStatus = normalizeProgressStatus(value.status);
  return <Paper variant="outlined" sx={{ position: { lg: 'sticky' }, top: 16, p: 1.7, minWidth: 0, borderColor: 'primary.main' }}>
    <Typography variant="caption" color="text.secondary" sx={titleSx}>Inspector</Typography>
    <Typography sx={{ mt: 0.7, fontSize: 14, fontWeight: 720 }}>{value.title}</Typography>
    <Stack direction="row" spacing={0.5} useFlexGap sx={{ mt: 0.7, flexWrap: 'wrap' }}>
      <Chip size="small" label={statusPresentation[normalizedStatus].label} sx={{ height: 21, fontSize: 10, color: statusColorToken(normalizedStatus) }} />
      <Chip size="small" variant="outlined" label={trackable ? (item.profile.categoryLabels[trackable.category] ?? trackable.category) : requirement?.group} sx={{ height: 21, fontSize: 10 }} />
    </Stack>
    <Divider sx={{ my: 1.2 }} />
    <Stack spacing={0.65}>
      <Typography variant="body2"><Box component="span" color="text.secondary">Work item · </Box>{item.title}</Typography>
      <Typography variant="body2"><Box component="span" color="text.secondary">Current stage · </Box>{getActiveCheckpoint(item)?.title ?? item.currentState}</Typography>
      {requirement && <Typography variant="body2"><Box component="span" color="text.secondary">Required before · </Box>{item.checkpoints.find((entry) => entry.id === requirement.checkpointId)?.title ?? 'Final target'}</Typography>}
      {trackable?.waitingOn && <Typography variant="body2"><Box component="span" color="text.secondary">Waiting on · </Box>{trackable.waitingOn}</Typography>}
      {source && <Typography variant="body2"><Box component="span" color="text.secondary">Source · </Box>{source.source} · {source.externalId}</Typography>}
      <Typography variant="body2"><Box component="span" color="text.secondary">Evidence · </Box>{evidence.length ? evidence.join(', ') : 'None linked'}</Typography>
      <Typography variant="body2"><Box component="span" color="text.secondary">Updated · </Box>{trackable?.updatedAt ?? item.updatedAt}</Typography>
    </Stack>
    <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 1.2 }}>AI summary will be generated from linked evidence; it does not replace source status.</Typography>
    {source?.hrefLabel && <Stack direction="row" spacing={0.4} sx={{ mt: 1.1, alignItems: 'center', color: 'primary.main' }}><Typography variant="caption" sx={{ fontWeight: 700 }}>{source.hrefLabel}</Typography><LaunchRounded sx={{ fontSize: 14 }} /></Stack>}
  </Paper>;
}

export default function WorkItemDetails({ item, initialFocusEntityId }: { item: WorkItem; initialFocusEntityId?: string }) {
  const initialSelection = (() => {
    const trackable = item.items.find((entry) => entry.id === initialFocusEntityId);
    if (trackable) return { kind: 'item', value: trackable } as const;
    const requirement = item.requirements.find((entry) => entry.id === initialFocusEntityId);
    return requirement ? ({ kind: 'requirement', value: requirement } as const) : null;
  })();
  const [selection, setSelection] = useState<Selection | null>(initialSelection);
  useEffect(() => {
    if (!initialFocusEntityId) return;
    requestAnimationFrame(() => document.getElementById(`detail-${initialFocusEntityId}`)?.focus());
  }, [initialFocusEntityId]);
  const itemGroups = groupItems(item);
  const requirementGroups = groupRequirements(item.requirements);
  const selectSubject = (id: string) => {
    const trackable = item.items.find((entry) => entry.id === id);
    if (trackable) setSelection({ kind: 'item', value: trackable });
    else {
      const requirement = item.requirements.find((entry) => entry.id === id);
      if (requirement) setSelection({ kind: 'requirement', value: requirement });
    }
  };

  return <Box sx={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1fr)', gap: 1.5, alignItems: 'start', '@container (min-width: 1040px)': { gridTemplateColumns: selection ? 'minmax(0, 1fr) 300px' : 'minmax(0, 1fr)' } }}>
    <Stack spacing={1.5} sx={{ minWidth: 0 }}>
      {item.items.length > 0 && <Section title="Scope / Items" meta={`${item.items.length} total`}>
        {Object.entries(itemGroups).map(([category, entries]) => <Box key={category}>
          <Stack direction="row" sx={{ px: 1.7, py: 0.7, justifyContent: 'space-between', bgcolor: 'surface.subtle' }}><Typography variant="caption" sx={{ fontWeight: 700 }}>{item.profile.categoryLabels[category] ?? category}</Typography><Typography variant="caption" color="text.secondary">{entries.length}</Typography></Stack>
          {entries.map((entry) => <Stack component="button" id={`detail-${entry.id}`} type="button" key={entry.id} direction="row" spacing={1.2} onClick={() => setSelection({ kind: 'item', value: entry })} sx={{ width: '100%', px: 1.7, py: 0.85, alignItems: 'center', border: 0, borderTop: 1, borderColor: 'divider', bgcolor: selection?.value.id === entry.id ? 'primary.light' : 'background.paper', color: 'text.primary', textAlign: 'left', cursor: 'pointer', '&:hover': { bgcolor: 'surface.subtle' }, '&:focus-visible': { outline: '2px solid', outlineColor: 'primary.main', outlineOffset: -2 } }}>
            <Box sx={{ minWidth: 0, flex: 1 }}><Typography variant="body2" sx={{ fontWeight: 650 }} noWrap>{entry.reference ? `${entry.reference} · ` : ''}{entry.title}</Typography>{entry.waitingOn && <Typography variant="caption" color="warning.main">Waiting on {entry.waitingOn}</Typography>}</Box>
            <Typography variant="caption" color={statusColorToken(normalizeProgressStatus(entry.status))} sx={{ flexShrink: 0, fontWeight: 650 }}>{statusPresentation[normalizeProgressStatus(entry.status)].label}</Typography>
          </Stack>)}
        </Box>)}
      </Section>}

      {item.requirements.length > 0 && <Section title="Requirements / Readiness" meta={`${item.requirements.filter((entry) => entry.status === 'ready' && entry.required).length}/${item.requirements.filter((entry) => entry.required).length} required ready`}>
        {Object.entries(requirementGroups).map(([group, entries]) => <Box key={group}>
          <Typography variant="caption" sx={{ display: 'block', px: 1.7, py: 0.7, bgcolor: 'surface.subtle', fontWeight: 700 }}>{group}</Typography>
          {entries.map((entry) => <Stack component="button" id={`detail-${entry.id}`} type="button" key={entry.id} direction="row" spacing={1.2} onClick={() => setSelection({ kind: 'requirement', value: entry })} sx={{ width: '100%', px: 1.7, py: 0.8, alignItems: 'center', border: 0, borderTop: 1, borderColor: 'divider', bgcolor: selection?.value.id === entry.id ? 'primary.light' : 'background.paper', color: 'text.primary', textAlign: 'left', cursor: 'pointer', '&:hover': { bgcolor: 'surface.subtle' }, '&:focus-visible': { outline: '2px solid', outlineColor: 'primary.main', outlineOffset: -2 } }}><Typography variant="body2" sx={{ minWidth: 0, flex: 1 }}>{entry.title}</Typography><Typography variant="caption" color={statusColorToken(normalizeProgressStatus(entry.status))} sx={{ flexShrink: 0, fontWeight: 650 }}>{statusPresentation[normalizeProgressStatus(entry.status)].label}</Typography></Stack>)}
        </Box>)}
      </Section>}

      {item.trackingDimensions.length > 0 && <Section title="Tracking Matrix" meta={item.profile.trackingLabel}><TrackingMatrix item={item} onSelect={selectSubject} /></Section>}

      <Section title="Sources & Evidence" meta={`${item.evidence?.length ?? 0} evidence records`}>
        <Stack divider={<Divider flexItem />} sx={{ px: 1.7 }}>
          {(item.evidence ?? []).map((entry) => <Stack key={entry.id} direction="row" spacing={2} sx={{ py: 0.85, justifyContent: 'space-between' }}><Typography variant="body2">{entry.label}</Typography><Typography variant="caption" color="text.secondary">{entry.capturedAt ?? 'Linked'}</Typography></Stack>)}
          {!item.evidence?.length && <Typography variant="body2" color="text.secondary" sx={{ py: 1.2 }}>No evidence records linked yet.</Typography>}
        </Stack>
      </Section>

      <Section title="External References" meta={`${item.sources?.length ?? 0} linked sources`}>
        <Stack divider={<Divider flexItem />} sx={{ px: 1.7 }}>
          {(item.sources ?? []).map((entry) => <Stack key={entry.id} direction="row" spacing={2} sx={{ py: 0.85, alignItems: 'center', justifyContent: 'space-between' }}><Box><Typography variant="body2" sx={{ fontWeight: 650 }}>{entry.source} · {entry.label}</Typography><Typography variant="caption" color="text.secondary">{entry.externalId ?? 'External record'} · {entry.updatedAt ?? 'Update pending'}</Typography></Box><LaunchRounded sx={{ fontSize: 15, color: 'text.secondary' }} /></Stack>)}
          {!item.sources?.length && <Typography variant="body2" color="text.secondary" sx={{ py: 1.2 }}>No external source linked yet.</Typography>}
        </Stack>
      </Section>
    </Stack>
    {selection && <Inspector selection={selection} item={item} />}
  </Box>;
}
