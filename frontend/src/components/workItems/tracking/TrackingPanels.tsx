import { useState } from 'react';
import { Alert, Box, Button, Chip, Divider, Paper, Stack, Typography } from '@mui/material';
import WorkflowStructureMap from './WorkflowStructureMap.tsx';
import type {
  TrackingStepState,
  WorkItemTracking,
} from '../../../api/tracking/trackingProductApi.ts';
import {
  describeExpectation,
  describeTiming,
  findActiveFinalTarget,
  formatMonitorDue,
  findNextExpectation,
  monitorKindLabel,
  stepStateLabel,
} from '../../../tracking/trackingPresentation.ts';

export type TrackingViewProps =
  | { status: 'idle' | 'loading' }
  | { status: 'unavailable' }
  | { status: 'error'; message: string; onRetry: () => void }
  | { status: 'ready'; data: WorkItemTracking };

const stateColor = (
  state: TrackingStepState,
): 'default' | 'primary' | 'success' | 'warning' | 'error' => {
  switch (state) {
    case 'COMPLETED':
      return 'success';
    case 'IN_PROGRESS':
      return 'primary';
    case 'WAITING':
      return 'warning';
    case 'BLOCKED':
    case 'FAILED':
      return 'error';
    default:
      return 'default';
  }
};

/** Honest non-ready states shared by both tracking tabs. */
export function TrackingStateNotice({ view }: { view: TrackingViewProps }) {
  if (view.status === 'idle' || view.status === 'loading') {
    return (
      <Paper variant="outlined" sx={{ p: 3 }}>
        <Typography color="text.secondary">Loading tracking data…</Typography>
      </Paper>
    );
  }
  if (view.status === 'unavailable') {
    return (
      <Alert severity="warning">
        This Work Item is no longer available, so its tracking data cannot be shown.
      </Alert>
    );
  }
  if (view.status === 'error') {
    return (
      <Alert
        severity="error"
        action={
          <Button color="inherit" size="small" onClick={view.onRetry}>
            Retry
          </Button>
        }
      >
        Tracking data could not be loaded: {view.message}
      </Alert>
    );
  }
  return null;
}

function ContextValue({ label, value }: { label: string; value: string }) {
  return (
    <Stack direction="row" spacing={0.55} sx={{ alignItems: 'baseline', minWidth: 0 }}>
      <Typography
        variant="caption"
        color="text.secondary"
        sx={{ fontWeight: 650, flex: '0 0 auto' }}
      >
        {label}
      </Typography>
      <Typography variant="body2" sx={{ fontWeight: 650, overflowWrap: 'anywhere' }}>
        {value}
      </Typography>
    </Stack>
  );
}

/** Workflow tab: real target/expectation summary, per-item workflow progress, expectations. */
export function TrackingWorkflowPanel({
  tracking,
  now,
}: {
  tracking: WorkItemTracking;
  now: Date;
}) {
  const target = findActiveFinalTarget(tracking);
  const next = findNextExpectation(tracking);

  return (
    <Stack spacing={2}>
      <Box
        component="section"
        aria-label="Tracking context"
        sx={{
          px: 1.5,
          py: 1.15,
          borderTop: 1,
          borderBottom: 1,
          borderColor: 'divider',
          bgcolor: 'surface.subtle',
        }}
      >
        <Stack direction="row" spacing={{ xs: 1.5, md: 3 }} useFlexGap sx={{ flexWrap: 'wrap' }}>
          <ContextValue label="Tracked items" value={String(tracking.trackedItems.length)} />
          <ContextValue
            label="Target"
            value={
              target ? `${formatMonitorDue(target)} · ${describeTiming(target, now)}` : 'Not set'
            }
          />
          <ContextValue
            label="Next expectation"
            value={next ? `${next.title} · ${describeTiming(next, now)}` : 'None'}
          />
        </Stack>
      </Box>

      <WorkflowStructureMap tracking={tracking} />
      <HistorySection
        title="Workflow History"
        events={(tracking.history ?? []).filter((event) => event.scope === 'TRACKING')}
      />
    </Stack>
  );
}

function HistorySection({
  title,
  events,
}: {
  title: string;
  events: NonNullable<WorkItemTracking['history']>;
}) {
  return (
    <Box component="section" aria-label={title}>
      <Typography variant="h2" sx={{ mb: 1 }}>
        {title}
      </Typography>
      <Paper variant="outlined">
        {events.length === 0 ? (
          <Typography variant="body2" color="text.secondary" sx={{ p: 2 }}>
            No recorded Tracking history yet.
          </Typography>
        ) : (
          events.slice(0, 16).map((event, index) => (
            <Box
              key={event.id}
              sx={{
                display: 'grid',
                gridTemplateColumns: '110px minmax(0,1fr)',
                gap: 1.5,
                px: 1.5,
                py: 1,
                borderTop: index ? 1 : 0,
                borderColor: 'divider',
              }}
            >
              <Typography variant="caption" color="text.secondary">
                {new Date(event.occurredAt).toLocaleString([], {
                  month: 'short',
                  day: 'numeric',
                  hour: '2-digit',
                  minute: '2-digit',
                })}
              </Typography>
              <Box>
                <Typography variant="body2" sx={{ fontWeight: 620 }}>
                  {event.summary}
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  {event.actor} · {event.eventType.replaceAll('_', ' ')}
                </Typography>
              </Box>
            </Box>
          ))
        )}
      </Paper>
    </Box>
  );
}

/** Read-first master/detail TrackedItem explorer. */
export function TrackedItemsPanel({ tracking }: { tracking: WorkItemTracking }) {
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const selected = tracking.trackedItems.find((item) => item.id === selectedId) ?? null;
  const byId = new Map(tracking.trackedItems.map((item) => [item.id, item]));
  return (
    <Box
      sx={{
        display: 'grid',
        gridTemplateColumns: { xs: '1fr', md: 'minmax(220px, 30%) minmax(0, 1fr)' },
        minHeight: 470,
        border: 1,
        borderColor: 'divider',
        borderRadius: 1,
        overflow: 'hidden',
      }}
    >
      <Box
        component="nav"
        aria-label="Tracked Items"
        sx={{
          borderRight: { md: 1 },
          borderBottom: { xs: 1, md: 0 },
          borderColor: 'divider',
          bgcolor: 'surface.subtle',
        }}
      >
        <Box sx={{ px: 1.5, py: 1.25, borderBottom: 1, borderColor: 'divider' }}>
          <Typography variant="h2">Items</Typography>
          <Typography variant="caption" color="text.secondary">
            {tracking.trackedItems.length} tracked units
          </Typography>
        </Box>
        {tracking.trackedItems.length === 0 ? (
          <Paper variant="outlined" sx={{ p: 4.5, textAlign: 'center' }}>
            <Typography variant="h3">No tracked items yet</Typography>
            <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
              Ask the AI Assistant to create the first item.
            </Typography>
          </Paper>
        ) : (
          <Stack component="ul" sx={{ p: 0, m: 0, listStyle: 'none' }}>
            {tracking.trackedItems.map((item) => (
              <Box component="li" key={item.id}>
                <Button
                  fullWidth
                  onClick={() => setSelectedId(item.id)}
                  data-testid="tracked-item-row"
                  sx={{
                    justifyContent: 'flex-start',
                    textAlign: 'left',
                    px: 1.5,
                    py: 1,
                    borderRadius: 0,
                    borderBottom: 1,
                    borderColor: 'divider',
                    bgcolor: selectedId === item.id ? 'action.selected' : 'transparent',
                  }}
                >
                  <Box sx={{ minWidth: 0, width: '100%' }}>
                    <Typography variant="body2" noWrap sx={{ fontWeight: 650 }}>
                      {item.title}
                    </Typography>
                    <Typography variant="caption" color="text.secondary" noWrap component="div">
                      {item.itemType ?? 'Item'} ·{' '}
                      {item.progression?.currentState
                        ? stepStateLabel(item.progression.currentState)
                        : item.lifecycleState}
                    </Typography>
                  </Box>
                </Button>
              </Box>
            ))}
          </Stack>
        )}
      </Box>
      <Box
        component="section"
        aria-label="Selected Item Detail"
        sx={{ p: { xs: 2, md: 2.5 }, minWidth: 0 }}
      >
        {!selected ? (
          <Box
            sx={{
              display: 'grid',
              placeItems: 'center',
              height: '100%',
              minHeight: 280,
              textAlign: 'center',
            }}
          >
            <Box>
              <Typography variant="h2">Selected Item Detail</Typography>
              <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
                Select an item to inspect its workflow position, structure, dependencies,
                expectations, and history.
              </Typography>
            </Box>
          </Box>
        ) : (
          <ItemDetail item={selected} tracking={tracking} byId={byId} />
        )}
      </Box>
    </Box>
  );
}

function ItemDetail({
  item,
  tracking,
  byId,
}: {
  item: WorkItemTracking['trackedItems'][number];
  tracking: WorkItemTracking;
  byId: Map<string, WorkItemTracking['trackedItems'][number]>;
}) {
  const step = tracking.workflow?.steps.find(
    (candidate) => candidate.id === item.progression?.currentStepId,
  );
  const children = tracking.trackedItems.filter((candidate) => candidate.parentItemId === item.id);
  const dependsOn = (tracking.relations ?? [])
    .filter((relation) => relation.fromTrackedItemId === item.id)
    .map((relation) => byId.get(relation.toTrackedItemId))
    .filter(Boolean);
  const blocks = (tracking.relations ?? [])
    .filter((relation) => relation.toTrackedItemId === item.id)
    .map((relation) => byId.get(relation.fromTrackedItemId))
    .filter(Boolean);
  const monitors = tracking.monitorPoints.filter(
    (monitor) => monitor.subjectKind === 'TRACKED_ITEM' && monitor.subjectId === item.id,
  );
  const attention = (tracking.attentionIssues ?? []).filter(
    (issue) => issue.subjectTrackedItemId === item.id,
  );
  const events = (tracking.history ?? []).filter((event) => event.subjectEntityId === item.id);
  const names = (items: Array<typeof item | undefined>) =>
    items.length ? items.map((value) => value?.title).join(', ') : 'No dependency recorded';
  return (
    <Stack spacing={2}>
      <Box>
        <Stack direction="row" spacing={1} sx={{ alignItems: 'center', flexWrap: 'wrap' }}>
          <Typography variant="h1" sx={{ fontSize: 24 }}>
            {item.title}
          </Typography>
          <Chip size="small" variant="outlined" label={item.itemType ?? 'ITEM'} />
          <Chip
            size="small"
            color={
              item.progression?.currentState ? stateColor(item.progression.currentState) : 'default'
            }
            label={
              item.progression?.currentState
                ? stepStateLabel(item.progression.currentState)
                : item.lifecycleState
            }
          />
        </Stack>
        <Typography variant="caption" color="text.secondary">
          {item.id}
        </Typography>
      </Box>
      <Divider />
      <DetailRow
        label="Workflow position"
        value={
          step ? `${step.title} · ${item.progression?.progressPercent ?? 0}%` : 'No active stage'
        }
      />
      <DetailRow
        label="Parent"
        value={
          item.parentItemId
            ? (byId.get(item.parentItemId)?.title ?? 'Unavailable parent')
            : 'No parent recorded'
        }
      />
      <DetailRow
        label="Children"
        value={
          children.length ? children.map((child) => child.title).join(', ') : 'No children recorded'
        }
      />
      <DetailRow label="Depends On" value={names(dependsOn)} />
      <DetailRow label="Blocks" value={names(blocks)} />
      <Box>
        <Typography variant="overline" color="text.secondary">
          Attention / expectation
        </Typography>
        {monitors.length || attention.length ? (
          <>
            {monitors.map((monitor) => (
              <Paper key={monitor.id} variant="outlined" sx={{ p: 1, mt: 0.5 }}>
                <Typography variant="body2" sx={{ fontWeight: 620 }}>
                  {monitor.title}
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  {monitorKindLabel(monitor.kind)} · {describeExpectation(monitor, tracking)} ·{' '}
                  {monitor.state}
                </Typography>
              </Paper>
            ))}
            {attention.map((issue) => (
              <Alert
                key={issue.id}
                severity={
                  issue.severity === 'CRITICAL'
                    ? 'error'
                    : issue.severity === 'WARNING'
                      ? 'warning'
                      : 'info'
                }
                sx={{ mt: 0.75 }}
              >
                <Typography variant="body2" sx={{ fontWeight: 620 }}>
                  {issue.reason}
                </Typography>
                {issue.recommendedAction && (
                  <Typography variant="caption">{issue.recommendedAction}</Typography>
                )}
              </Alert>
            ))}
          </>
        ) : (
          <Typography variant="body2">No active expectation</Typography>
        )}
      </Box>
      <HistorySection title="Item History" events={events} />
    </Stack>
  );
}

function DetailRow({ label, value }: { label: string; value: string }) {
  return (
    <Box>
      <Typography variant="overline" color="text.secondary">
        {label}
      </Typography>
      <Typography variant="body2">{value}</Typography>
    </Box>
  );
}
