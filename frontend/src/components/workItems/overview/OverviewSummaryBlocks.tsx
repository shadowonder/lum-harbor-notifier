import { Box, Stack, Tooltip, Typography, useTheme } from '@mui/material';
import type { Theme } from '@mui/material/styles';
import type {
  ProductOverviewTrackedItem,
  ProductOverviewTrackingSignal,
} from '../../../api/workItems/productApi.ts';
import type {
  OverviewAttentionView,
  OverviewTargetView,
  OverviewTrackingView,
} from '../../../presentation/workItems/workItemOverviewPresentation.ts';
import {
  groupOverviewItemsByType,
  overviewItemTypeGroup,
  overviewMarkerTooltip,
} from '../../../presentation/workItems/workItemOverviewPresentation.ts';
import WorkflowNodeShape from '../workflow/WorkflowNodeShape.tsx';

export function OverviewTargetBlock({ target }: { target: OverviewTargetView }) {
  return (
    <Box>
      <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
        Final target
      </Typography>
      {target.status === 'set' ? (
        <>
          <Typography variant="caption">{target.dateLabel}</Typography>
          <Typography variant="caption" sx={{ display: 'block', fontWeight: 650 }}>
            {target.timingLabel}
          </Typography>
        </>
      ) : (
        <Typography variant="caption" color="text.secondary">
          {target.status === 'unavailable' ? 'Unavailable' : 'None set'}
        </Typography>
      )}
    </Box>
  );
}

const distributionLabels = [
  ['inProgress', 'in progress'],
  ['blocked', 'blocked'],
  ['waiting', 'waiting'],
  ['failed', 'failed'],
  ['notStarted', 'not started'],
  ['unknown', 'unknown'],
  ['completed', 'complete'],
  ['skipped', 'skipped'],
] as const;

export function OverviewRollup({ tracking }: { tracking: OverviewTrackingView }) {
  if (tracking.status === 'unavailable')
    return (
      <Typography variant="body2" color="text.secondary" sx={{ display: 'block' }}>
        Tracking unavailable
      </Typography>
    );
  if (tracking.status === 'no-workflow')
    return (
      <Typography variant="body2" color="text.secondary" sx={{ display: 'block' }}>
        No workflow · {tracking.trackedItemCount} tracked
      </Typography>
    );
  if (tracking.status === 'workflow-empty')
    return (
      <Typography variant="body2" color="text.secondary" sx={{ display: 'block' }}>
        0 tracked items
      </Typography>
    );
  const entries = distributionLabels.filter(([key]) => tracking.stateDistribution[key] > 0);
  return (
    <Box>
      <Typography variant="body2" sx={{ display: 'block', fontWeight: 700 }}>
        {tracking.trackedItemCount} tracked {tracking.trackedItemCount === 1 ? 'item' : 'items'}
      </Typography>
      <Box
        sx={{
          display: 'grid',
          gridTemplateColumns: 'repeat(2, minmax(0, 1fr))',
          columnGap: 1.25,
          rowGap: 0.15,
          mt: 0.35,
        }}
      >
        {entries.map(([key, label]) => (
          <Typography key={key} variant="body2" color="text.secondary" noWrap>
            <Box component="span" sx={{ color: 'text.primary', fontWeight: 700 }}>
              {tracking.stateDistribution[key]}
            </Box>{' '}
            {label}
          </Typography>
        ))}
      </Box>
    </Box>
  );
}

function signalColor(theme: Theme, signal: ProductOverviewTrackingSignal): string {
  switch (signal) {
    case 'SATISFIED':
      return theme.palette.success.main;
    case 'WATCH':
      return theme.palette.warning.main;
    case 'PROBLEM':
      return theme.palette.error.main;
    default:
      return theme.palette.text.disabled;
  }
}

function signalLabel(item: ProductOverviewTrackedItem): string {
  switch (item.signal.state) {
    case 'SATISFIED':
      return 'Todo due in more than 3 days';
    case 'WATCH':
      return 'Todo due within 3 days';
    case 'PROBLEM':
      return 'Todo due within 1 day or overdue';
    default:
      return 'no open linked Todo with a due target';
  }
}

function OverviewTypeMarker({
  item,
  stageTitle,
}: {
  item: ProductOverviewTrackedItem;
  stageTitle: string;
}) {
  const theme = useTheme();
  const group = overviewItemTypeGroup(item.itemType);
  const color = signalColor(theme, item.signal.state);
  const execution = item.complete
    ? 'completed execution'
    : (item.currentState ?? 'unknown execution state');
  const label = `${item.title} · ${item.itemType ?? 'unknown type'} · ${execution} · ${signalLabel(item)} · ${stageTitle}`;
  const tip = overviewMarkerTooltip(item, stageTitle);
  return (
    <Tooltip
      placement="top"
      enterDelay={150}
      slotProps={{
        tooltip: {
          sx: {
            maxWidth: 260,
            px: 1.1,
            py: 0.75,
            bgcolor: 'background.paper',
            color: 'text.primary',
            border: 1,
            borderColor: 'divider',
            borderRadius: 1,
            boxShadow: 2,
          },
        },
        arrow: {
          sx: { color: 'background.paper', '&::before': { border: 1, borderColor: 'divider' } },
        },
      }}
      title={
        <Box data-testid="marker-tooltip">
          <Typography
            variant="body2"
            sx={{ fontWeight: 650, lineHeight: 1.3, overflowWrap: 'anywhere' }}
          >
            {tip.headline}
          </Typography>
          <Typography variant="caption" color="text.secondary" sx={{ fontWeight: 700 }}>
            {tip.state}
          </Typography>
        </Box>
      }
    >
      <Box
        component="span"
        role="img"
        tabIndex={0}
        aria-label={label}
        data-testid="overview-marker"
        sx={{
          display: 'inline-flex',
          flex: '0 0 auto',
          borderRadius: '2px',
          '&:focus-visible': {
            outline: '2px solid',
            outlineColor: 'primary.main',
            outlineOffset: 1,
          },
        }}
      >
        <Box
          component="svg"
          viewBox="0 0 14 14"
          aria-hidden
          sx={{ width: 12, height: 12, overflow: 'visible', flex: '0 0 auto' }}
        >
          {group === 'other' ? (
            <rect
              x="2"
              y="2"
              width="10"
              height="10"
              fill={item.resolved ? color : theme.palette.background.paper}
              stroke={color}
              strokeWidth="1.7"
              strokeDasharray="2 2"
            />
          ) : (
            <WorkflowNodeShape
              x={7}
              y={7}
              itemType={group}
              color={color}
              bgColor={theme.palette.background.paper}
              radius={5}
              isResolved={item.resolved}
            />
          )}
        </Box>
      </Box>
    </Tooltip>
  );
}

export function OverviewTrackingCenter({ tracking }: { tracking: OverviewTrackingView }) {
  if (tracking.status === 'unavailable')
    return (
      <Typography variant="caption" color="text.secondary">
        Tracking data unavailable
      </Typography>
    );
  if (tracking.status === 'no-workflow')
    return (
      <Typography variant="caption" color="text.secondary">
        No workflow configured
      </Typography>
    );
  const stages = tracking.workflow.stages;
  if (stages.length === 0)
    return (
      <Typography variant="caption" color="text.secondary">
        Workflow has no stages
      </Typography>
    );
  const items = tracking.status === 'available' ? tracking.items : [];
  return (
    <Box sx={{ width: '100%', minWidth: 0 }} data-testid="overview-mini-map">
      <Box sx={{ position: 'relative', minHeight: 72 }}>
        <Box
          sx={{ position: 'absolute', top: 6, left: 14, right: 14, height: 2, bgcolor: 'divider' }}
        />
        <Box
          sx={{
            display: 'grid',
            gridTemplateColumns: `repeat(${stages.length}, minmax(36px, 1fr))`,
            gap: 0.65,
            position: 'relative',
          }}
        >
          {stages.map((stage, stageIndex) => {
            const stagedItems = items.filter(
              (item) =>
                item.currentStepId === stage.id ||
                (item.complete && item.currentStepId === null && stageIndex === stages.length - 1),
            );
            const groups = groupOverviewItemsByType(stagedItems);
            return (
              <Box key={stage.id} sx={{ minWidth: 0, textAlign: 'center' }}>
                <Box
                  sx={{
                    width: 9,
                    height: 9,
                    borderRadius: '50%',
                    bgcolor: stage.terminal ? 'success.main' : 'background.paper',
                    border: 2,
                    borderColor: stage.terminal ? 'success.main' : 'text.disabled',
                    mx: 'auto',
                    mt: 0.15,
                  }}
                />
                <Typography
                  variant="caption"
                  title={stage.title}
                  sx={{
                    display: 'block',
                    whiteSpace: 'nowrap',
                    textOverflow: 'ellipsis',
                    overflow: 'hidden',
                    fontSize: 9,
                    lineHeight: 1.15,
                    minHeight: 12,
                    mt: 0.45,
                    color: 'text.secondary',
                  }}
                >
                  {stage.title}
                </Typography>
                <Stack spacing={0.55} sx={{ mt: 0.6, alignItems: 'center' }}>
                  {groups.map(([group, groupedItems]) => (
                    <Box
                      key={group}
                      data-marker-group={group}
                      sx={{
                        display: 'flex',
                        flexWrap: 'wrap',
                        justifyContent: 'center',
                        alignContent: 'flex-start',
                        gap: 0.45,
                        width: '100%',
                      }}
                    >
                      {groupedItems.map((item) => (
                        <OverviewTypeMarker key={item.id} item={item} stageTitle={stage.title} />
                      ))}
                    </Box>
                  ))}
                </Stack>
              </Box>
            );
          })}
        </Box>
      </Box>
      {tracking.status === 'workflow-empty' && (
        <Typography variant="caption" color="text.secondary">
          Workflow ready · no tracked items
        </Typography>
      )}
    </Box>
  );
}

/** Compact Attention text fact for the RIGHT detail area; never a tag or notification detail. */
export function OverviewAttentionText({ attention }: { attention: OverviewAttentionView }) {
  if (attention.status === 'unavailable')
    return (
      <Typography variant="body2" color="text.secondary">
        Attention unavailable
      </Typography>
    );
  if (attention.status === 'none')
    return (
      <Typography variant="body2" color="text.secondary">
        No open attention
      </Typography>
    );
  if (attention.actionableCount > 0) {
    const severity = attention.highestActionableSeverity ?? 'INFO';
    return (
      <Typography variant="body2">
        <Box component="span" sx={{ fontWeight: 700 }}>
          {attention.actionableCount}
        </Box>{' '}
        {attention.actionableCount === 1 ? 'needs' : 'need'} action ·{' '}
        {severity.charAt(0) + severity.slice(1).toLowerCase()}
      </Typography>
    );
  }
  return (
    <Typography variant="body2" color="text.secondary">
      {attention.snoozedCount} snoozed
    </Typography>
  );
}
