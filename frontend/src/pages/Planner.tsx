import ChevronLeftRounded from '@mui/icons-material/ChevronLeftRounded';
import ChevronRightRounded from '@mui/icons-material/ChevronRightRounded';
import {
  Box,
  Button,
  ButtonGroup,
  Paper,
  Stack,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from '@mui/material';
import { useEffect, useMemo, useState, type MouseEvent } from 'react';
import MonthView from '../components/planner/MonthView';
import WeekView from '../components/planner/WeekView';
import DayPanel from '../components/planner/DayPanel';
import PageContainer from '../components/layout/PageContainer';
import PageHeader from '../components/layout/PageHeader';
import { usePlannerItemCompletion } from '../hooks/usePlannerItemCompletion';
import { usePlannerView } from '../hooks/usePlannerView';
import {
  getBrowserTimezone,
  getCalendarPlannerItems,
  getLocalTodayKey,
  getVisiblePlannerRange,
  getWeekDates,
  groupCurrentOpenPlannerItems,
  type PlannerItem,
  type PlannerRailMode,
  type SelectedPlannerItem,
} from './plannerData';
import type { ProposedPlannerItemDto } from '../api/proposals';
import type {
  ProposalPreviewState,
  ProposalProjection,
} from '../presentation/proposals/buildPreviewState';
import type { PlannerNavigationIntent } from '../navigation/plannerNavigation';

type PlannerView = 'month' | 'week';

interface PlannerProps {
  onAssistantContextChange?: (context: string) => void;
  navigationIntent: PlannerNavigationIntent | null;
  onConsumeNavigationIntent: () => void;
  proposalState: ProposalProjection;
  preview: ProposalPreviewState | null;
}

function sameDate(date: Date) {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate(), 12);
}

/** Parses a local navigation date without a UTC conversion or midnight instant. */
function plannerDateFromLocalKey(value?: string) {
  if (!value) return sameDate(new Date());
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(value);
  if (!match) return sameDate(new Date());
  const [, year, month, day] = match;
  const date = new Date(Number(year), Number(month) - 1, Number(day), 12);
  return Number.isNaN(date.getTime()) ? sameDate(new Date()) : date;
}

function addDays(date: Date, amount: number) {
  const next = sameDate(date);
  next.setDate(next.getDate() + amount);
  return next;
}

function addMonths(date: Date, amount: number) {
  const targetMonth = new Date(date.getFullYear(), date.getMonth() + amount, 1, 12);
  const lastDay = new Date(targetMonth.getFullYear(), targetMonth.getMonth() + 1, 0, 12).getDate();
  targetMonth.setDate(Math.min(date.getDate(), lastDay));
  return targetMonth;
}

function dateContext(date: Date) {
  return `Planner · ${new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric' }).format(date)}`;
}

function formatWeekRange(period: Date) {
  const days = getWeekDates(period);
  const start = days[0];
  const end = days[days.length - 1];
  const sameYear = start.getFullYear() === end.getFullYear();
  const sameMonth = sameYear && start.getMonth() === end.getMonth();
  const monthDay = new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric' });

  if (sameMonth) {
    return `${monthDay.format(start)}–${end.getDate()}, ${end.getFullYear()}`;
  }

  if (sameYear) {
    return `${monthDay.format(start)} – ${monthDay.format(end)}, ${end.getFullYear()}`;
  }

  const fullDate = new Intl.DateTimeFormat('en-US', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
  });
  return `${fullDate.format(start)} – ${fullDate.format(end)}`;
}

function periodLabel(view: PlannerView, period: Date) {
  if (view === 'month') {
    return new Intl.DateTimeFormat('en-US', { month: 'long', year: 'numeric' }).format(period);
  }
  return formatWeekRange(period);
}

function toPlannerItem(item: ProposedPlannerItemDto): PlannerItem {
  const type: PlannerItem['type'] = ['TODO', 'CHECKPOINT', 'FOLLOW_UP', 'REMINDER'].includes(
    item.type,
  )
    ? (item.type as PlannerItem['type'])
    : 'TODO';
  return {
    kind: 'plannerItem',
    id: item.id,
    type,
    title: item.title,
    details: item.details,
    status: item.status,
    scheduledAt: item.scheduledAt,
    scheduledDate: item.scheduledDate,
    dueAt: item.dueAt,
    dueDate: item.dueDate,
    contextStatus: 'UNKNOWN',
    missingContext: [],
    authority: item.authority === 'EXTERNAL' ? 'EXTERNAL' : 'LOCAL',
    recentChanges: [],
    relatedWorkItemId: item.relatedWorkItemId,
  };
}

/**
 * Applies committed proposal detail overlays to one backend projection without
 * changing that projection's membership or combining it with another one.
 */
function applyCommittedEnrichments(
  items: PlannerItem[],
  enrichments: Record<string, { before: string; after: string }>,
) {
  return items.map((item) =>
    enrichments[item.id] ? { ...item, details: enrichments[item.id].after } : item,
  );
}

export default function Planner({
  onAssistantContextChange,
  navigationIntent,
  onConsumeNavigationIntent,
  proposalState,
  preview,
}: PlannerProps) {
  const initialDate = plannerDateFromLocalKey(navigationIntent?.selectedDate);
  const [view, setView] = useState<PlannerView>('month');
  const [period, setPeriod] = useState(() => initialDate);
  const [selectedDate, setSelectedDate] = useState(() => initialDate);
  const [selectedItem, setSelectedItem] = useState<SelectedPlannerItem | null>(null);
  const [selectedPlannerItemId, setSelectedPlannerItemId] = useState<string | null>(
    () => navigationIntent?.selectedPlannerItemId ?? null,
  );
  const [railMode, setRailMode] = useState<PlannerRailMode>(
    () => navigationIntent?.preferredRailMode ?? 'day',
  );
  const today = getLocalTodayKey();
  const timezone = getBrowserTimezone();
  const visibleRange = useMemo(() => getVisiblePlannerRange(view, period), [period, view]);
  const {
    data: plannerViewData,
    loading: plannerViewLoading,
    refreshing: plannerViewRefreshing,
    error: plannerViewError,
    retry: retryPlannerView,
    refresh: refreshPlannerView,
  } = usePlannerView({ ...visibleRange, today, timezone });

  const {
    completingItemIds,
    notice: completionNotice,
    complete: handleCompletePlannerItem,
    retryRefresh: retryCompletionRefresh,
  } = usePlannerItemCompletion({
    refresh: refreshPlannerView,
    onCommitted: (completed) => {
      // A committed DTO may update the open inspector immediately; collection
      // membership still waits for the authoritative PlannerView refresh.
      setSelectedItem((current) =>
        (current?.kind === 'plannerItem' && current.id === completed.id) ||
        selectedPlannerItemId === completed.id
          ? {
              ...(current?.kind === 'plannerItem' ? current : completed),
              ...completed,
              recentChanges: current?.recentChanges ?? completed.recentChanges,
              relatedWorkItemId:
                current?.kind === 'plannerItem'
                  ? current.relatedWorkItemId
                  : completed.relatedWorkItemId,
            }
          : current,
      );
    },
  });

  // AppShell owns the transient intent only until this Planner
  // captures its date, rail mode, and item identity in local state.
  useEffect(() => {
    if (navigationIntent) {
      if (navigationIntent.selectedDate) {
        const nextDate = plannerDateFromLocalKey(navigationIntent.selectedDate);
        // eslint-disable-next-line react-hooks/set-state-in-effect -- sync navigation intent to local planner state
        setSelectedDate(nextDate);
        setPeriod(nextDate);
      }
      if (navigationIntent.selectedPlannerItemId) {
        setSelectedPlannerItemId(navigationIntent.selectedPlannerItemId);
      }
      if (navigationIntent.preferredRailMode) {
        setRailMode(navigationIntent.preferredRailMode);
      }
      onConsumeNavigationIntent();
    }
  }, [navigationIntent, onConsumeNavigationIntent]);

  useEffect(() => {
    if (selectedItem) {
      onAssistantContextChange?.(selectedItem.title);
    } else {
      onAssistantContextChange?.(
        railMode === 'day' ? dateContext(selectedDate) : 'Planner · Open items',
      );
    }
  }, [onAssistantContextChange, selectedItem, railMode, selectedDate]);

  const selectDate = (date: Date) => {
    const next = sameDate(date);
    setSelectedDate(next);
    setSelectedItem(null);
    setSelectedPlannerItemId(null);
    setRailMode('day');
    onAssistantContextChange?.(dateContext(next));
  };

  const selectItem = (item: SelectedPlannerItem) => {
    setSelectedItem(item);
    setSelectedPlannerItemId(item.kind === 'plannerItem' ? item.id : null);
    onAssistantContextChange?.(item.title);
  };

  /** Opens a Calendar task in Detail without triggering a new range request. */
  const selectCalendarPlannerItem = (item: PlannerItem, date: Date) => {
    const next = sameDate(date);
    setSelectedDate(next);
    setRailMode('day');
    selectItem(item);
  };

  const returnToDay = () => {
    setSelectedItem(null);
    setSelectedPlannerItemId(null);
    onAssistantContextChange?.(dateContext(selectedDate));
  };

  const changeRailMode = (mode: PlannerRailMode) => {
    setRailMode(mode);
    setSelectedItem(null);
    setSelectedPlannerItemId(null);
    onAssistantContextChange?.(mode === 'day' ? dateContext(selectedDate) : 'Planner · Open items');
  };

  const movePeriod = (direction: -1 | 1) => {
    const next = view === 'month' ? addMonths(period, direction) : addDays(period, direction * 7);
    setPeriod(next);
    selectDate(next);
  };

  const goToToday = () => {
    const localToday = sameDate(new Date());
    setPeriod(localToday);
    selectDate(localToday);
  };

  const changeView = (_event: MouseEvent<HTMLElement>, nextView: PlannerView | null) => {
    if (!nextView) return;
    setView(nextView);
    setPeriod(selectedDate);
  };

  const committedAdded = proposalState.planner.addedItems;
  const previewAdded = (preview?.proposed.planner.addedItems ?? []).filter(
    (item) => !committedAdded.some((current) => current.id === item.id),
  );
  const committedEnrichments = proposalState.planner.enrichments;
  const previewEnrichments = Object.fromEntries(
    Object.entries(preview?.proposed.planner.enrichments ?? {}).filter(
      ([id, value]) => committedEnrichments[id]?.after !== value.after,
    ),
  );
  const committedAddedItems = useMemo(() => committedAdded.map(toPlannerItem), [committedAdded]);
  const effectiveCalendarItems = useMemo(() => {
    if (!plannerViewData) return [];
    const backendIDs = new Set(plannerViewData.calendarItems.map((item) => item.id));
    return [
      ...applyCommittedEnrichments(plannerViewData.calendarItems, committedEnrichments),
      ...committedAddedItems.filter((item) => !backendIDs.has(item.id)),
    ];
  }, [committedAddedItems, committedEnrichments, plannerViewData]);
  const effectiveCurrentOpenItems = useMemo(() => {
    if (!plannerViewData) return [];
    const backendItems = applyCommittedEnrichments(
      plannerViewData.currentOpenItems,
      committedEnrichments,
    );
    const backendIDs = new Set(backendItems.map((item) => item.id));

    // Only frontend-only, not-yet-persisted additions need local eligibility.
    // Backend Current Open membership itself is never reconstructed here.
    const addedGroups = groupCurrentOpenPlannerItems(committedAddedItems, today, timezone);
    const visibleAdditions = [
      ...addedGroups.pastDue,
      ...addedGroups.today,
      ...addedGroups.noDate,
    ].filter((item) => !backendIDs.has(item.id));
    return [...backendItems, ...visibleAdditions];
  }, [committedAddedItems, committedEnrichments, plannerViewData, timezone, today]);
  const effectiveCompletedItems = useMemo(
    () =>
      plannerViewData
        ? applyCommittedEnrichments(plannerViewData.completedItems, committedEnrichments)
        : [],
    [committedEnrichments, plannerViewData],
  );
  const effectiveCompletedTodayItems = useMemo(
    () =>
      plannerViewData
        ? applyCommittedEnrichments(plannerViewData.completedTodayItems, committedEnrichments)
        : [],
    [committedEnrichments, plannerViewData],
  );

  // Resolve the inspector from current projections without synchronously
  // copying refreshed data back into selection state. Cross-page navigation
  // supplies only an ID, which resolves here from the newest backend-owned
  // projections rather than retaining a Dashboard object snapshot.
  const resolvedSelectedItem = useMemo(() => {
    const plannerItemID =
      selectedPlannerItemId ?? (selectedItem?.kind === 'plannerItem' ? selectedItem.id : null);
    if (!plannerItemID) return selectedItem;
    return (
      [
        ...effectiveCurrentOpenItems,
        ...effectiveCalendarItems,
        ...effectiveCompletedTodayItems,
        ...effectiveCompletedItems,
      ].find((item) => item.id === plannerItemID) ?? selectedItem
    );
  }, [
    effectiveCalendarItems,
    effectiveCompletedItems,
    effectiveCompletedTodayItems,
    effectiveCurrentOpenItems,
    selectedPlannerItemId,
    selectedItem,
  ]);
  const calendarPlannerItems = useMemo(
    () => getCalendarPlannerItems(effectiveCalendarItems),
    [effectiveCalendarItems],
  );
  const proposedPlannerItems = previewAdded.map(toPlannerItem);
  const proposedCalendarItems = getCalendarPlannerItems(proposedPlannerItems);

  return (
    <PageContainer>
      <PageHeader
        eyebrow="Plan your time"
        title="Planner"
        subtitle="Calendar events and todos in one place."
      />

      <Stack
        direction="row"
        spacing={1.5}
        useFlexGap
        sx={{
          mb: 1.5,
          minWidth: 0,
          minHeight: 48,
          px: 1,
          py: 0.75,
          alignItems: 'center',
          justifyContent: 'space-between',
          flexWrap: 'wrap',
          border: 1,
          borderColor: 'divider',
          borderRadius: 1,
          bgcolor: 'surface.subtle',
        }}
      >
        <Stack direction="row" spacing={1.5} sx={{ minWidth: 0, alignItems: 'center' }}>
          <ButtonGroup size="small" variant="outlined" aria-label="Change planner period">
            <Button
              onClick={() => movePeriod(-1)}
              aria-label="Previous period"
              sx={{ minWidth: 34, px: 0.65 }}
            >
              <ChevronLeftRounded fontSize="small" />
            </Button>
            <Button onClick={goToToday} sx={{ px: 1.35 }}>
              Today
            </Button>
            <Button
              onClick={() => movePeriod(1)}
              aria-label="Next period"
              sx={{ minWidth: 34, px: 0.65 }}
            >
              <ChevronRightRounded fontSize="small" />
            </Button>
          </ButtonGroup>
          <Typography
            component="h2"
            sx={{
              minWidth: 0,
              pl: 1.5,
              borderLeft: 1,
              borderColor: 'divider',
              fontSize: 14,
              fontWeight: 680,
              fontVariantNumeric: 'tabular-nums',
            }}
          >
            {periodLabel(view, period)}
          </Typography>
        </Stack>

        <Stack direction="row" spacing={1} sx={{ alignItems: 'center' }}>
          <Button
            size="small"
            variant="text"
            disabled={plannerViewLoading || plannerViewRefreshing}
            onClick={() => void refreshPlannerView().catch(() => undefined)}
          >
            {plannerViewRefreshing ? 'Refreshing…' : 'Refresh'}
          </Button>
          <ToggleButtonGroup
            exclusive
            size="small"
            value={view}
            onChange={changeView}
            aria-label="Planner view"
            sx={{
              '& .MuiToggleButton-root': {
                minHeight: 30,
                px: 1.4,
                py: 0.35,
                color: 'text.secondary',
                borderColor: 'divider',
                textTransform: 'none',
                fontSize: 11,
                fontWeight: 650,
                '&.Mui-selected': {
                  bgcolor: 'primary.light',
                  color: 'primary.dark',
                  boxShadow: 'inset 0 0 0 1px currentColor',
                },
                '&.Mui-selected:hover': { bgcolor: 'primary.light' },
                '&:focus-visible': {
                  outline: '2px solid',
                  outlineColor: 'primary.main',
                  outlineOffset: 1,
                },
              },
            }}
          >
            <ToggleButton value="month">Month</ToggleButton>
            <ToggleButton value="week">Week</ToggleButton>
          </ToggleButtonGroup>
        </Stack>
      </Stack>

      {completionNotice && (
        <Stack
          role="alert"
          direction="row"
          spacing={1}
          sx={{
            mb: 1.5,
            px: 1.25,
            py: 0.8,
            alignItems: 'center',
            justifyContent: 'space-between',
            border: 1,
            borderColor: completionNotice.refreshFailed ? 'warning.main' : 'error.main',
            borderRadius: 1,
            bgcolor: 'surface.subtle',
          }}
        >
          <Typography variant="body2">{completionNotice.message}</Typography>
          {completionNotice.refreshFailed && (
            <Button size="small" onClick={() => void retryCompletionRefresh()}>
              Retry refresh
            </Button>
          )}
        </Stack>
      )}

      <Paper
        variant="outlined"
        sx={{
          display: 'grid',
          gridTemplateColumns: 'minmax(0, 1fr)',
          minWidth: 0,
          overflow: 'hidden',
          '@container (min-width: 980px)': {
            gridTemplateColumns: 'minmax(0, 1fr) 360px',
          },
        }}
      >
        <Box sx={{ minWidth: 0, bgcolor: 'background.paper' }}>
          {plannerViewLoading && (
            <Box role="status" sx={{ display: 'grid', minHeight: 560, placeItems: 'center', p: 3 }}>
              <Typography variant="body2" color="text.secondary">
                Loading calendar…
              </Typography>
            </Box>
          )}
          {!plannerViewLoading && !plannerViewData && plannerViewError && (
            <Box role="alert" sx={{ display: 'grid', minHeight: 560, placeItems: 'center', p: 3 }}>
              <Stack spacing={1} sx={{ maxWidth: 420, alignItems: 'center', textAlign: 'center' }}>
                <Typography sx={{ fontWeight: 650 }}>Unable to load the calendar.</Typography>
                <Typography variant="body2" color="text.secondary">
                  {plannerViewError}
                </Typography>
                <Button size="small" variant="outlined" onClick={retryPlannerView}>
                  Retry
                </Button>
              </Stack>
            </Box>
          )}
          {plannerViewData && (
            <>
              {(plannerViewRefreshing || plannerViewError) && (
                <Stack
                  role={plannerViewError ? 'alert' : 'status'}
                  direction="row"
                  spacing={1}
                  sx={{
                    minHeight: 34,
                    px: 1.25,
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    borderBottom: 1,
                    borderColor: 'divider',
                    bgcolor: 'surface.subtle',
                  }}
                >
                  <Typography variant="caption" color="text.secondary">
                    {plannerViewError ?? 'Refreshing calendar…'}
                  </Typography>
                  {plannerViewError && (
                    <Button size="small" onClick={retryPlannerView}>
                      Retry
                    </Button>
                  )}
                </Stack>
              )}
              {view === 'month' ? (
                <MonthView
                  period={period}
                  selectedDate={selectedDate}
                  today={today}
                  onSelectDate={selectDate}
                  onSelectItem={selectCalendarPlannerItem}
                  onCompletePlannerItem={handleCompletePlannerItem}
                  completingItemIds={completingItemIds}
                  events={plannerViewData.calendarEvents}
                  plannerItems={calendarPlannerItems}
                  proposedItems={proposedCalendarItems}
                />
              ) : (
                <WeekView
                  period={period}
                  selectedDate={selectedDate}
                  today={today}
                  onSelectDate={selectDate}
                  onSelectItem={selectCalendarPlannerItem}
                  onCompletePlannerItem={handleCompletePlannerItem}
                  completingItemIds={completingItemIds}
                  events={plannerViewData.calendarEvents}
                  plannerItems={calendarPlannerItems}
                  proposedItems={proposedCalendarItems}
                />
              )}
            </>
          )}
        </Box>
        <Box
          sx={{
            position: 'relative',
            minWidth: 0,
            height: 'min(560px, 65vh)',
            overflow: 'hidden',
            borderTop: 1,
            borderColor: 'primary.main',
            '@container (min-width: 980px)': {
              height: 'auto',
              minHeight: 0,
              borderTop: 0,
              borderLeft: 1,
              borderColor: 'primary.main',
              '& > aside': {
                position: 'absolute',
                inset: 0,
              },
            },
          }}
        >
          <DayPanel
            date={selectedDate}
            today={today}
            timezone={timezone}
            calendarEvents={plannerViewData?.calendarEvents ?? []}
            calendarItems={effectiveCalendarItems}
            currentOpenItems={effectiveCurrentOpenItems}
            completedTodayItems={effectiveCompletedTodayItems}
            mode={railMode}
            onModeChange={changeRailMode}
            selectedItem={resolvedSelectedItem}
            onSelectItem={selectItem}
            onCompletePlannerItem={handleCompletePlannerItem}
            completingItemIds={completingItemIds}
            onBack={returnToDay}
            proposedItems={proposedPlannerItems}
            proposedEnrichments={previewEnrichments}
            onRefresh={refreshPlannerView}
          />
        </Box>
      </Paper>
    </PageContainer>
  );
}
