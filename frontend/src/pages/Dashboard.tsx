import ArrowForwardRounded from '@mui/icons-material/ArrowForwardRounded';
import CalendarMonthRounded from '@mui/icons-material/CalendarMonthRounded';
import {
  Alert,
  Box,
  Button,
  ButtonBase,
  Checkbox,
  Chip,
  CircularProgress,
  Divider,
  Paper,
  Stack,
  Typography,
} from '@mui/material';
import { useCallback, useEffect, useMemo, useState } from 'react';
import ActiveWorkflowsRoadmap from '../components/dashboard/activeWorkflowsRoadmap/ActiveWorkflowsRoadmap.tsx';
import { buildAuthoritativeRoadmapRows } from '../components/dashboard/activeWorkflowsRoadmap/roadmapAuthoritativeAdapter.ts';
import PageContainer from '../components/layout/PageContainer';
import PageHeader from '../components/layout/PageHeader';
import { useDashboardView } from '../hooks/useDashboardView';
import { usePlannerItemCompletion } from '../hooks/usePlannerItemCompletion';
import type {
  DashboardProposalSummary,
  ProposalProductApi,
} from '../api/proposals/proposalProductApi.ts';
import { wailsProposalProductApi } from '../api/proposals/wailsProposalProductApi.ts';
import type { PlannerNavigationIntent } from '../navigation/plannerNavigation';
import {
  addLocalCalendarDays,
  formatDuration,
  formatTime,
  getBrowserTimezone,
  getCalendarEventTimeProjection,
  getLocalTodayKey,
  getPlannerItemTimeProjection,
  groupCurrentOpenPlannerItems,
  type CalendarEvent,
  type PlannerItem,
} from './plannerData';

interface DashboardTodoRow {
  item: PlannerItem;
  group: 'Past due' | 'Today' | 'No date';
}

const panelSx = {
  borderColor: 'divider',
  overflow: 'hidden',
};

/** Orders all-day Events before timed Events without merging their domain models. */
function sortDashboardEvents(events: CalendarEvent[], timezone: string) {
  return [...events].sort((left, right) => {
    const leftMinutes = getCalendarEventTimeProjection(left, timezone).startMinutes ?? -1;
    const rightMinutes = getCalendarEventTimeProjection(right, timezone).startMinutes ?? -1;
    return leftMinutes - rightMinutes || left.title.localeCompare(right.title);
  });
}

/** Builds compact Event presentation text from the Calendar Event projection. */
function dashboardEventContext(event: CalendarEvent, timezone: string) {
  const placement = getCalendarEventTimeProjection(event, timezone);
  const timing = event.isAllDay ? 'All-day event' : formatDuration(placement.durationMinutes);
  return [event.location, timing, event.details].filter(Boolean).join(' · ');
}

/** Formats a local date key without first converting it to a UTC instant. */
function formatDashboardDate(localDate: string) {
  const [year, month, day] = localDate.split('-').map(Number);
  return new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric' }).format(
    new Date(year, month - 1, day, 12),
  );
}

function formatDashboardInstant(value: string) {
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(
    new Date(value),
  );
}

/** Builds compact Todo timing text while preserving date-only semantics. */
function dashboardTodoContext(
  item: PlannerItem,
  timezone: string,
  group: DashboardTodoRow['group'],
) {
  const placement = getPlannerItemTimeProjection(item, timezone);
  const timing =
    group === 'No date'
      ? 'No date'
      : group === 'Past due' && placement
        ? formatDashboardDate(placement.date)
        : placement?.precision === 'datetime' && placement.minutes !== null
          ? formatTime(placement.minutes)
          : 'Today';
  return [timing, item.details].filter(Boolean).join(' · ');
}

function PanelHeader({ title, meta }: { title: string; meta: string }) {
  return (
    <Stack
      direction="row"
      spacing={2}
      sx={{ px: 2.25, pt: 2.1, alignItems: 'center', justifyContent: 'space-between' }}
    >
      <Typography component="h2" variant="h2">
        {title}
      </Typography>
      <Typography variant="caption" color="text.secondary">
        {meta}
      </Typography>
    </Stack>
  );
}

export default function Dashboard({
  todoDisplayLimit,
  onOpenPlanner,
  onOpenWorkItem,
  onOpenWorkItems,
  onReviewSuggestions,
  proposalApi = wailsProposalProductApi,
  refreshTrigger,
}: {
  /** Startup-validated presentation limit supplied by Runtime Config. */
  todoDisplayLimit: number;
  onOpenPlanner: (intent: PlannerNavigationIntent) => void;
  onOpenWorkItem: (id: string, title: string) => void;
  onOpenWorkItems: () => void;
  onReviewSuggestions: () => void;
  proposalApi?: ProposalProductApi;
  refreshTrigger?: number;
}) {
  const [proposalSummary, setProposalSummary] = useState<DashboardProposalSummary | null>(null);
  const [proposalLoading, setProposalLoading] = useState(true);
  const [proposalError, setProposalError] = useState<string | null>(null);
  const [proposalRefreshTrigger, setProposalRefreshTrigger] = useState(0);

  const retryProposalSummary = useCallback(() => {
    setProposalLoading(true);
    setProposalRefreshTrigger((c) => c + 1);
  }, []);

  useEffect(() => {
    let active = true;
    void proposalApi.getDashboardSummary().then(
      (summary) => {
        if (active) {
          setProposalSummary(summary);
          setProposalError(null);
          setProposalLoading(false);
        }
      },
      (err: unknown) => {
        if (active) {
          setProposalError(err instanceof Error ? err.message : String(err));
          setProposalSummary(null);
          setProposalLoading(false);
        }
      },
    );
    return () => {
      active = false;
    };
  }, [proposalApi, proposalRefreshTrigger, refreshTrigger]);

  const today = getLocalTodayKey();
  const timezone = getBrowserTimezone();
  const tomorrow = addLocalCalendarDays(today, 1);
  const {
    data: dashboard,
    loading: dashboardLoading,
    error: dashboardError,
    retry: retryDashboard,
    refresh: refreshDashboard,
  } = useDashboardView({ rangeStart: today, rangeEnd: tomorrow, today, timezone });
  const plannerToday = dashboard?.planner ?? null;
  const workItems = dashboard?.workItems ?? null;
  const {
    completingItemIds,
    notice: completionNotice,
    complete: completePlannerItem,
    retryRefresh: retryCompletionRefresh,
  } = usePlannerItemCompletion({ refresh: refreshDashboard });

  // Schedule is today's Calendar projection. Dashboard To-dos intentionally
  // use backend Current Open membership instead of today's calendarItems:
  // Current Open includes every past-due, today, and no-date action requiring
  // attention now, without asking Dashboard to reproduce backend eligibility.
  const schedule = useMemo(
    () => sortDashboardEvents(plannerToday?.calendarEvents ?? [], timezone),
    [plannerToday, timezone],
  );
  // The shared selector preserves backend membership and stable ordering
  // within each presentation group; concatenation establishes Dashboard's
  // Past Due, Today, then No Date priority without duplicating queue rules.
  const todos = useMemo(() => {
    const groups = groupCurrentOpenPlannerItems(
      plannerToday?.currentOpenItems ?? [],
      today,
      timezone,
    );
    return [
      ...groups.pastDue.map((item): DashboardTodoRow => ({ item, group: 'Past due' })),
      ...groups.today.map((item): DashboardTodoRow => ({ item, group: 'Today' })),
      ...groups.noDate.map((item): DashboardTodoRow => ({ item, group: 'No date' })),
    ];
  }, [plannerToday, timezone, today]);
  const roadmapRows = useMemo(
    () =>
      buildAuthoritativeRoadmapRows(
        workItems?.allCurrentItems ?? [],
        workItems?.trackingAvailable ?? true,
      ),
    [workItems],
  );
  const openTodoCount = plannerToday?.currentOpenItems.length ?? 0;
  const visibleTodos = todos.slice(0, todoDisplayLimit);
  const todayMeta = dashboardLoading
    ? 'Loading…'
    : dashboardError && !plannerToday
      ? 'Unavailable'
      : `${schedule.length} events · ${openTodoCount} open`;

  /** Transfers item identity and the appropriate rail context to Planner. */
  const openTodoInPlanner = ({ item, group }: DashboardTodoRow) => {
    onOpenPlanner({
      selectedPlannerItemId: item.id,
      selectedDate: group === 'Today' ? today : undefined,
      preferredRailMode: group === 'Today' ? 'day' : 'open',
    });
  };

  /** Opens the complete Current Open queue without selecting a task. */
  const openAllTodosInPlanner = () => {
    onOpenPlanner({ selectedDate: today, preferredRailMode: 'open' });
  };

  return (
    <PageContainer>
      <PageHeader
        eyebrow="Your day, at a glance"
        title="Dashboard"
        subtitle="A clear view of what needs your attention today."
        status={
          dashboard
            ? `${workItems?.currentCount ?? 0} current Work Items · ${openTodoCount} open tasks`
            : undefined
        }
      />

      {dashboardError && (
        <Alert
          severity="error"
          sx={{ mb: 2.25 }}
          action={<Button onClick={retryDashboard}>Retry</Button>}
        >
          Dashboard could not be loaded: {dashboardError}
        </Alert>
      )}

      {proposalError && (
        <Alert
          severity="warning"
          sx={{ mb: 2.25 }}
          action={
            <Button size="small" color="inherit" onClick={retryProposalSummary}>
              Retry
            </Button>
          }
        >
          Suggested changes could not be loaded: {proposalError}
        </Alert>
      )}

      {!proposalLoading && proposalSummary && proposalSummary.pendingCount > 0 && (
        <Paper
          variant="outlined"
          sx={{
            mb: 2.25,
            p: 2,
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            bgcolor: 'action.hover',
            borderColor: 'divider',
          }}
          data-testid="proposal-awareness-banner"
        >
          <Stack direction="row" spacing={1.5} sx={{ alignItems: 'center' }}>
            <Chip
              label={`${proposalSummary.pendingCount} pending`}
              size="small"
              color="primary"
              variant="filled"
              sx={{ fontWeight: 600 }}
            />
            <Typography variant="body2" sx={{ fontWeight: 600 }}>
              Suggested changes waiting for review
            </Typography>
            <Typography variant="caption" color="text.secondary">
              {proposalSummary.affectedWorkItemCount > 0
                ? `(${proposalSummary.affectedWorkItemCount} ${
                    proposalSummary.affectedWorkItemCount === 1 ? 'work item' : 'work items'
                  } affected)`
                : '(new items to review)'}
            </Typography>
          </Stack>
          <Button
            size="small"
            variant="outlined"
            endIcon={<ArrowForwardRounded />}
            onClick={onReviewSuggestions}
          >
            Review suggestions
          </Button>
        </Paper>
      )}

      <Box
        sx={{
          display: 'grid',
          gridTemplateColumns: 'minmax(0, 1fr)',
          gridTemplateAreas: '"today" "roadmap" "work"',
          gap: 2.25,
          alignItems: 'stretch',
        }}
      >
        <Paper
          variant="outlined"
          sx={{ ...panelSx, gridArea: 'today', display: 'flex', flexDirection: 'column' }}
        >
          <PanelHeader title="Today" meta={todayMeta} />
          {dashboardLoading && (
            <Box role="status" sx={{ px: 2.25, py: 3 }}>
              <Typography variant="body2" color="text.secondary">
                Loading today’s schedule…
              </Typography>
            </Box>
          )}
          {!dashboardLoading && !plannerToday && dashboardError && (
            <Stack role="alert" spacing={1} sx={{ px: 2.25, py: 3, alignItems: 'flex-start' }}>
              <Typography variant="body2" sx={{ fontWeight: 650 }}>
                Unable to load today’s schedule.
              </Typography>
              <Typography variant="caption" color="text.secondary">
                {dashboardError}
              </Typography>
              <Button size="small" variant="outlined" onClick={retryDashboard}>
                Retry
              </Button>
            </Stack>
          )}
          {plannerToday && (
            <>
              <Box sx={{ px: 2.25, pt: 2 }}>
                <Stack direction="row" sx={{ justifyContent: 'space-between' }}>
                  <Typography
                    variant="caption"
                    color="text.secondary"
                    sx={{ fontWeight: 650, letterSpacing: '0.07em', textTransform: 'uppercase' }}
                  >
                    Schedule
                  </Typography>
                  <Typography variant="caption" color="text.secondary">
                    {schedule.length} {schedule.length === 1 ? 'event' : 'events'}
                  </Typography>
                </Stack>
                {schedule.map((event) => {
                  const placement = getCalendarEventTimeProjection(event, timezone);
                  return (
                    <Stack
                      key={event.id}
                      direction="row"
                      spacing={1.25}
                      sx={{ py: 1.4, minWidth: 0, alignItems: 'center' }}
                    >
                      <Typography
                        variant="body2"
                        color="text.secondary"
                        sx={{ width: 57, flexShrink: 0, fontVariantNumeric: 'tabular-nums' }}
                      >
                        {formatTime(placement.startMinutes)}
                      </Typography>
                      <Box
                        sx={{
                          alignSelf: 'stretch',
                          width: 2,
                          borderRadius: 2,
                          bgcolor: 'primary.main',
                          opacity: 0.65,
                        }}
                      />
                      <Box sx={{ minWidth: 0, flex: 1 }}>
                        <Typography sx={{ fontWeight: 600 }} noWrap>
                          {event.title}
                        </Typography>
                        <Typography variant="caption" color="text.secondary">
                          {dashboardEventContext(event, timezone)}
                        </Typography>
                      </Box>
                      <CalendarMonthRounded sx={{ fontSize: 17, color: 'text.secondary' }} />
                    </Stack>
                  );
                })}
                {schedule.length === 0 && (
                  <Typography variant="body2" color="text.secondary" sx={{ py: 1.8 }}>
                    No calendar events today.
                  </Typography>
                )}
              </Box>
              <Divider sx={{ mx: 2.25 }} />
              <Box sx={{ px: 2.25, py: 2 }}>
                <Stack direction="row" sx={{ justifyContent: 'space-between' }}>
                  <Typography
                    variant="caption"
                    color="text.secondary"
                    sx={{ fontWeight: 650, letterSpacing: '0.07em', textTransform: 'uppercase' }}
                  >
                    To-dos
                  </Typography>
                  <Typography variant="caption" color="text.secondary">
                    {openTodoCount} open
                  </Typography>
                </Stack>
                {completionNotice && (
                  <Stack
                    role="alert"
                    direction="row"
                    spacing={1}
                    sx={{
                      mt: 1.2,
                      px: 1.1,
                      py: 0.8,
                      alignItems: 'center',
                      justifyContent: 'space-between',
                      border: 1,
                      borderColor: completionNotice.refreshFailed ? 'warning.main' : 'error.main',
                      borderRadius: 1,
                      bgcolor: 'surface.subtle',
                    }}
                  >
                    <Typography variant="caption">{completionNotice.message}</Typography>
                    {completionNotice.refreshFailed && (
                      <Button size="small" onClick={() => void retryCompletionRefresh()}>
                        Retry refresh
                      </Button>
                    )}
                  </Stack>
                )}
                {visibleTodos.map((row) => {
                  const { item, group } = row;
                  const completing = completingItemIds.has(item.id);
                  const completionEligible = item.authority === 'LOCAL' && item.status === 'OPEN';
                  const completionLabel = completionEligible
                    ? `Complete ${item.title}`
                    : `${item.title} is managed by an external source`;
                  return (
                    <Stack
                      key={item.id}
                      direction="row"
                      spacing={0.5}
                      sx={{ py: 0.35, minWidth: 0, alignItems: 'center' }}
                    >
                      <Checkbox
                        size="small"
                        title={completionLabel}
                        checked={false}
                        disabled={!completionEligible || completing}
                        onClick={(event) => {
                          // Completion is an independent row action; never let
                          // its click activate a parent navigation handler.
                          event.stopPropagation();
                        }}
                        onChange={() => {
                          if (completionEligible) void completePlannerItem(item);
                        }}
                        icon={
                          completing ? <CircularProgress size={17} color="inherit" /> : undefined
                        }
                        slotProps={{ input: { 'aria-label': completionLabel } }}
                        sx={{
                          flexShrink: 0,
                          color: 'text.secondary',
                          '&.Mui-disabled': { color: 'action.disabled' },
                        }}
                      />
                      <ButtonBase
                        onClick={() => openTodoInPlanner(row)}
                        sx={{
                          minWidth: 0,
                          flex: 1,
                          gap: 1,
                          px: 0.5,
                          py: 1,
                          borderRadius: 1,
                          justifyContent: 'flex-start',
                          textAlign: 'left',
                          '&:hover': { bgcolor: 'action.hover' },
                          '&:focus-visible': {
                            outline: '2px solid',
                            outlineColor: 'primary.main',
                            outlineOffset: 1,
                          },
                        }}
                      >
                        <Box sx={{ minWidth: 0, flex: 1 }}>
                          <Typography sx={{ fontWeight: 600 }} noWrap>
                            {item.title}
                          </Typography>
                          <Typography variant="caption" color="text.secondary" noWrap>
                            {dashboardTodoContext(item, timezone, group) || group}
                          </Typography>
                        </Box>
                        <Chip
                          label={group}
                          size="small"
                          sx={{
                            height: 21,
                            flexShrink: 0,
                            fontSize: 9,
                            bgcolor: 'surface.secondary',
                            color: group === 'Past due' ? 'error.main' : 'text.secondary',
                          }}
                        />
                      </ButtonBase>
                    </Stack>
                  );
                })}
                {todos.length === 0 && (
                  <Typography variant="body2" color="text.secondary" sx={{ py: 1.8 }}>
                    No open tasks need your attention.
                  </Typography>
                )}
                {openTodoCount > todoDisplayLimit && (
                  <ButtonBase
                    onClick={openAllTodosInPlanner}
                    sx={{
                      width: '100%',
                      mt: 0.5,
                      pt: 1.25,
                      justifyContent: 'space-between',
                      borderTop: 1,
                      borderColor: 'divider',
                      color: 'primary.main',
                      fontSize: 12,
                      fontWeight: 650,
                      '&:focus-visible': {
                        outline: '2px solid',
                        outlineColor: 'primary.main',
                        outlineOffset: 2,
                      },
                    }}
                  >
                    View all {openTodoCount} open tasks in Planner
                    <ArrowForwardRounded sx={{ fontSize: 17 }} />
                  </ButtonBase>
                )}
              </Box>
            </>
          )}
        </Paper>

        <Paper variant="outlined" sx={{ ...panelSx, gridArea: 'roadmap', minWidth: 0 }}>
          <PanelHeader
            title="Active Workflows"
            meta={
              dashboardLoading && !workItems
                ? 'Loading…'
                : dashboardError && !workItems
                  ? 'Unavailable'
                  : `${roadmapRows.length} tracked`
            }
          />
          {dashboardLoading && !workItems && (
            <Box role="status" sx={{ px: 2.25, py: 3 }}>
              <Typography variant="body2" color="text.secondary">
                Loading active workflows…
              </Typography>
            </Box>
          )}
          {dashboardError && !workItems && !dashboardLoading && (
            <Box role="alert" sx={{ px: 2.25, py: 3 }}>
              <Typography variant="body2" color="text.secondary">
                Active workflows are unavailable.
              </Typography>
            </Box>
          )}
          {workItems && workItems.trackingAvailable === false && (
            <Box role="status" sx={{ px: 2.25, py: 3 }}>
              <Typography variant="body2" color="text.secondary">
                Workflow timing is unavailable.
              </Typography>
            </Box>
          )}
          {workItems && workItems.trackingAvailable !== false && (
            <ActiveWorkflowsRoadmap
              rows={roadmapRows}
              today={today}
              onOpenWorkItem={onOpenWorkItem}
            />
          )}
        </Paper>

        <Paper variant="outlined" sx={{ ...panelSx, gridArea: 'work', minWidth: 0 }}>
          <PanelHeader
            title="Current Work"
            meta={
              dashboardLoading
                ? 'Loading…'
                : dashboardError && !workItems
                  ? 'Unavailable'
                  : `${workItems?.currentCount ?? 0} current`
            }
          />
          {dashboardLoading && !workItems && (
            <Box role="status" sx={{ px: 2.25, py: 3 }}>
              <Typography variant="body2" color="text.secondary">
                Loading Work Items…
              </Typography>
            </Box>
          )}
          {workItems && (
            <Box sx={{ px: 2.25, py: 2 }}>
              <Stack direction="row" spacing={1} useFlexGap sx={{ mb: 1.25, flexWrap: 'wrap' }}>
                <Chip size="small" label={`${workItems.pausedCount} paused`} variant="outlined" />
                <Chip
                  size="small"
                  label={`${workItems.completedCount} completed`}
                  variant="outlined"
                />
                <Chip
                  size="small"
                  label={`${workItems.cancelledCount} cancelled`}
                  variant="outlined"
                />
              </Stack>
              {workItems.currentItems.map((item) => (
                <ButtonBase
                  key={item.id}
                  onClick={() => onOpenWorkItem(item.id, item.title)}
                  sx={{
                    width: '100%',
                    gap: 1.25,
                    py: 1.2,
                    textAlign: 'left',
                    justifyContent: 'flex-start',
                    borderTop: 1,
                    borderColor: 'divider',
                    '&:focus-visible': {
                      outline: '2px solid',
                      outlineColor: 'primary.main',
                      outlineOffset: 1,
                    },
                  }}
                >
                  <Box sx={{ minWidth: 0, flex: 1 }}>
                    <Typography sx={{ fontWeight: 600 }} noWrap>
                      {item.title}
                    </Typography>
                    <Typography variant="caption" color="text.secondary">
                      Updated {formatDashboardInstant(item.updatedAt)}
                    </Typography>
                  </Box>
                  <Chip
                    size="small"
                    label={item.lifecycleState}
                    color={item.lifecycleState === 'PAUSED' ? 'default' : 'primary'}
                    variant="outlined"
                  />
                  <ArrowForwardRounded sx={{ fontSize: 17, color: 'text.secondary' }} />
                </ButtonBase>
              ))}
              {workItems.currentItems.length === 0 && (
                <Typography variant="body2" color="text.secondary" sx={{ py: 2 }}>
                  No current Work Items. Completed and cancelled work remains available on the Work
                  Items page.
                </Typography>
              )}
              <Button
                size="small"
                endIcon={<ArrowForwardRounded />}
                onClick={onOpenWorkItems}
                sx={{ mt: 1 }}
              >
                View all Work Items
              </Button>
              <Typography variant="caption" color="text.secondary" sx={{ ml: 1 }}>
                {workItems.closedCount} closed
              </Typography>
            </Box>
          )}
        </Paper>
      </Box>
    </PageContainer>
  );
}
