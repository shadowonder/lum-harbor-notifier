import ArrowBackRounded from '@mui/icons-material/ArrowBackRounded';
import CalendarMonthRounded from '@mui/icons-material/CalendarMonthRounded';
import CheckRounded from '@mui/icons-material/CheckRounded';
import CheckBoxOutlineBlankRounded from '@mui/icons-material/CheckBoxOutlineBlankRounded';
import ExpandMoreRounded from '@mui/icons-material/ExpandMoreRounded';
import {
  Box,
  Button,
  ButtonBase,
  Chip,
  CircularProgress,
  Collapse,
  Divider,
  IconButton,
  Menu,
  MenuItem,
  Stack,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from '@mui/material';
import { useState, type MouseEvent } from 'react';
import { removeTodoReminder, setTodoReminder } from '../../api/plannerApi';
import {
  calendarEventsForDate,
  dateKey,
  formatDuration,
  formatPlannerAuthority,
  formatPlannerItemSource,
  formatPlannerItemTime,
  formatTime,
  getCalendarEventTimeProjection,
  getCalendarPlannerItemsForDate,
  groupCurrentOpenPlannerItems,
  getPlannerItemsForDate,
  type CalendarEvent,
  type PlannerItem,
  type PlannerRailMode,
  type SelectedPlannerItem,
} from '../../pages/plannerData';

interface DayPanelProps {
  date: Date;
  today: string;
  timezone: string;
  calendarEvents: CalendarEvent[];
  calendarItems: PlannerItem[];
  currentOpenItems: PlannerItem[];
  completedTodayItems: PlannerItem[];
  mode: PlannerRailMode;
  onModeChange: (mode: PlannerRailMode) => void;
  selectedItem: SelectedPlannerItem | null;
  onSelectItem: (item: SelectedPlannerItem) => void;
  /** Routes every LOCAL Todo completion to the page-owned mutation handler. */
  onCompletePlannerItem: (item: PlannerItem) => Promise<void>;
  /** Identifies items whose mutation or authoritative refresh is still pending. */
  completingItemIds: ReadonlySet<string>;
  onBack: () => void;
  proposedItems?: PlannerItem[];
  proposedEnrichments?: Record<string, { before: string; after: string }>;
  onRefresh?: () => void;
}

function formatOffsetLabel(seconds: number): string {
  if (seconds === 0) return 'At due time';
  if (seconds % 86400 === 0) return `${seconds / 86400}d before`;
  if (seconds % 3600 === 0) return `${seconds / 3600}h before`;
  if (seconds % 60 === 0) return `${seconds / 60}m before`;
  return `${seconds}s before`;
}

const sectionLabelSx = {
  color: 'text.secondary',
  fontSize: 10,
  fontWeight: 700,
  letterSpacing: '0.08em',
  textTransform: 'uppercase',
};

function itemDateLabel(dateKey: string) {
  const [year, month, day] = dateKey.split('-').map(Number);
  return new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric' }).format(
    new Date(year, month - 1, day, 12),
  );
}

function DetailField({ label, value }: { label: string; value: string }) {
  return (
    <Box sx={{ minWidth: 0 }}>
      <Typography sx={sectionLabelSx}>{label}</Typography>
      <Typography sx={{ mt: 0.35, fontWeight: 600, overflowWrap: 'anywhere' }}>{value}</Typography>
    </Box>
  );
}

function EventRows({
  events,
  timezone,
  onSelectItem,
}: {
  events: CalendarEvent[];
  timezone: string;
  onSelectItem: DayPanelProps['onSelectItem'];
}) {
  return (
    <Stack spacing={0.35}>
      {events.map((event) => (
        <ButtonBase
          key={event.id}
          onClick={() => onSelectItem(event)}
          sx={{
            width: '100%',
            minWidth: 0,
            px: 0.75,
            py: 0.9,
            borderRadius: 1,
            alignItems: 'flex-start',
            justifyContent: 'flex-start',
            textAlign: 'left',
            color: 'text.primary',
            '&:hover': { bgcolor: 'surface.secondary' },
            '&:focus-visible': {
              outline: '2px solid',
              outlineColor: 'primary.main',
              outlineOffset: -2,
            },
          }}
        >
          <Box sx={{ width: 54, pr: 0.5, flexShrink: 0, pt: 0.1, textAlign: 'right' }}>
            <Typography
              variant="body2"
              color="text.secondary"
              sx={{ fontVariantNumeric: 'tabular-nums' }}
            >
              {formatTime(getCalendarEventTimeProjection(event, timezone).startMinutes)}
            </Typography>
          </Box>
          <Box
            sx={{
              width: 2,
              minHeight: 35,
              mr: 1.1,
              borderRadius: 1,
              bgcolor: 'primary.main',
              opacity: 0.72,
            }}
          />
          <Box sx={{ flex: 1, minWidth: 0 }}>
            <Typography sx={{ fontWeight: 650 }} noWrap>
              {event.title}
            </Typography>
            <Typography variant="caption" color="text.secondary">
              {event.isAllDay
                ? 'Calendar event'
                : formatDuration(getCalendarEventTimeProjection(event, timezone).durationMinutes)}
              {event.relatedPlannerItemIds?.length
                ? ` · ${event.relatedPlannerItemIds.length} actions`
                : ''}
            </Typography>
          </Box>
          <CalendarMonthRounded
            sx={{ ml: 0.75, mt: 0.15, flexShrink: 0, fontSize: 16, color: 'primary.main' }}
          />
        </ButtonBase>
      ))}
      {events.length === 0 && (
        <Typography variant="body2" color="text.secondary" sx={{ py: 0.8 }}>
          No calendar events.
        </Typography>
      )}
    </Stack>
  );
}

function PlannerItemRows({
  items,
  onSelectItem,
  onCompletePlannerItem,
  completingItemIds,
  proposedEnrichments = {},
  pastDue = false,
  today,
  timezone,
}: {
  items: PlannerItem[];
  onSelectItem: DayPanelProps['onSelectItem'];
  onCompletePlannerItem: DayPanelProps['onCompletePlannerItem'];
  completingItemIds: DayPanelProps['completingItemIds'];
  proposedEnrichments?: DayPanelProps['proposedEnrichments'];
  pastDue?: boolean;
  today: string;
  timezone: string;
}) {
  return (
    <Stack spacing={0.2}>
      {items.map((item) => {
        const time = formatPlannerItemTime(item, today, timezone);
        const completing = completingItemIds.has(item.id);
        const completed = item.status === 'COMPLETED';
        const canComplete = item.authority === 'LOCAL' && item.status === 'OPEN';
        return (
          <Stack
            key={item.id}
            direction="row"
            sx={{
              width: '100%',
              minWidth: 0,
              borderRadius: 1,
              alignItems: 'center',
              color: 'text.primary',
              '&:hover': { bgcolor: 'surface.secondary' },
            }}
          >
            <IconButton
              size="small"
              disabled={!canComplete || completing}
              aria-label={
                completed
                  ? `${item.title} is completed`
                  : item.authority === 'EXTERNAL'
                    ? `${item.title} is managed by an external source`
                    : `Complete ${item.title}`
              }
              title={item.authority === 'EXTERNAL' ? 'Managed by external source' : undefined}
              onClick={(event) => {
                // Completion is an independent row action. Prevent this click
                // from opening or changing the current Detail selection.
                event.stopPropagation();
                if (canComplete) void onCompletePlannerItem(item);
              }}
              sx={{
                ml: 0.25,
                flexShrink: 0,
                color: pastDue ? 'error.main' : 'icon.muted',
              }}
            >
              {completing ? (
                <CircularProgress size={16} color="inherit" />
              ) : completed ? (
                <CheckRounded sx={{ fontSize: 18, color: 'success.main' }} />
              ) : (
                <CheckBoxOutlineBlankRounded sx={{ fontSize: 18 }} />
              )}
            </IconButton>
            <ButtonBase
              onClick={() => onSelectItem(item)}
              sx={{
                minWidth: 0,
                flex: 1,
                px: 0.45,
                py: 0.8,
                justifyContent: 'flex-start',
                textAlign: 'left',
                color: 'inherit',
                '&:focus-visible': {
                  outline: '2px solid',
                  outlineColor: 'primary.main',
                  outlineOffset: -2,
                },
              }}
            >
              <Typography
                sx={{
                  minWidth: 0,
                  fontWeight: 600,
                  textDecoration: completed ? 'line-through' : 'none',
                }}
                noWrap
              >
                {item.title}
              </Typography>
              {time !== 'No date' && (
                <Typography
                  variant="caption"
                  color={pastDue ? 'error.main' : 'text.secondary'}
                  sx={{ ml: 'auto', pl: 0.75, flexShrink: 0 }}
                >
                  {time}
                </Typography>
              )}
              {proposedEnrichments[item.id] && (
                <Typography variant="caption" sx={{ ml: 0.6, color: '#7969C7', fontWeight: 700 }}>
                  Proposed detail
                </Typography>
              )}
            </ButtonBase>
          </Stack>
        );
      })}
    </Stack>
  );
}

function ProposedRows({
  items,
  today,
  timezone,
}: {
  items: PlannerItem[];
  today: string;
  timezone: string;
}) {
  return items.map((item) => (
    <Box
      key={item.id}
      sx={{
        display: 'flex',
        minWidth: 0,
        alignItems: 'center',
        px: 0.75,
        py: 0.75,
        border: 1,
        borderStyle: 'dashed',
        borderColor: '#7969C7',
        borderRadius: 1,
        bgcolor: 'rgba(121, 105, 199, .09)',
        color: '#7969C7',
      }}
    >
      <CheckBoxOutlineBlankRounded sx={{ mr: 1, flexShrink: 0, fontSize: 18 }} />
      <Box sx={{ minWidth: 0, flex: 1 }}>
        <Typography sx={{ fontWeight: 650 }} noWrap>
          {item.title}
        </Typography>
        <Typography
          variant="caption"
          sx={{ fontWeight: 720, letterSpacing: '.04em', textTransform: 'uppercase' }}
        >
          Proposed
        </Typography>
      </Box>
      {formatPlannerItemTime(item, today, timezone) !== 'No date' && (
        <Typography variant="caption" sx={{ pl: 0.75, flexShrink: 0 }}>
          {formatPlannerItemTime(item, today, timezone)}
        </Typography>
      )}
    </Box>
  ));
}

function CompletedSection({
  items,
  label,
  onSelectItem,
  defaultOpen = false,
}: {
  items: PlannerItem[];
  label: string;
  onSelectItem: DayPanelProps['onSelectItem'];
  /** Opens completion-only days without requiring an extra disclosure click. */
  defaultOpen?: boolean;
}) {
  const [open, setOpen] = useState(defaultOpen);
  return (
    <Box sx={{ px: 2.25, pb: 1.5 }}>
      <ButtonBase
        onClick={() => setOpen((value) => !value)}
        aria-expanded={open}
        sx={{
          width: '100%',
          py: 0.7,
          justifyContent: 'flex-start',
          color: 'text.secondary',
          borderRadius: 1,
        }}
      >
        <ExpandMoreRounded
          sx={{
            mr: 0.65,
            fontSize: 17,
            transform: open ? 'rotate(180deg)' : 'none',
            transition: 'transform 120ms ease',
          }}
        />
        <Typography sx={sectionLabelSx}>{label}</Typography>
        <Typography variant="caption" sx={{ ml: 'auto' }}>
          {items.length}
        </Typography>
      </ButtonBase>
      <Collapse in={open}>
        <Stack spacing={0.15} sx={{ pt: 0.25 }}>
          {items.map((item) => (
            <ButtonBase
              key={item.id}
              onClick={() => onSelectItem(item)}
              sx={{
                width: '100%',
                minWidth: 0,
                px: 0.75,
                py: 0.65,
                justifyContent: 'flex-start',
                borderRadius: 1,
                color: 'text.secondary',
                '&:hover': { bgcolor: 'surface.secondary' },
              }}
            >
              <CheckRounded sx={{ mr: 1, flexShrink: 0, fontSize: 17, color: 'success.main' }} />
              <Typography sx={{ minWidth: 0, textDecoration: 'line-through' }} noWrap>
                {item.title}
              </Typography>
            </ButtonBase>
          ))}
        </Stack>
      </Collapse>
    </Box>
  );
}

function DayOverview({
  date,
  today,
  timezone,
  calendarEvents,
  calendarItems,
  currentOpenItems,
  completedTodayItems,
  onCompletePlannerItem,
  completingItemIds,
  mode,
  onSelectItem,
  proposedItems = [],
  proposedEnrichments = {},
}: DayPanelProps) {
  const selectedDate = dateKey(date);
  const isToday = selectedDate === today;
  const usesCurrentOpenProjection = mode === 'open' || isToday;
  const events = calendarEventsForDate(calendarEvents, date, timezone);

  // Current Open membership is owned by Go. The frontend only groups that
  // projection for the existing rail presentation; it never rebuilds it from
  // visible-range Calendar Items.
  const currentGroups = groupCurrentOpenPlannerItems(currentOpenItems, today, timezone);
  const proposedCurrentGroups = groupCurrentOpenPlannerItems(proposedItems, today, timezone);
  const selectedCalendarItems = usesCurrentOpenProjection
    ? []
    : getCalendarPlannerItemsForDate(calendarItems, date, timezone);
  const dayItems = usesCurrentOpenProjection
    ? currentGroups.today
    : selectedCalendarItems.filter((item) => item.status === 'OPEN');
  const noDateItems = usesCurrentOpenProjection ? currentGroups.noDate : [];
  const proposedDayItems = usesCurrentOpenProjection
    ? proposedCurrentGroups.today
    : getPlannerItemsForDate(proposedItems, date, timezone);
  const proposedNoDateItems = usesCurrentOpenProjection ? proposedCurrentGroups.noDate : [];
  // Today/Open uses completion activity from completedTodayItems. Historical
  // Day mode instead splits selected Calendar membership by status: a task
  // scheduled Sep 3 and completed Sep 16 still belongs in Sep 3 Completed.
  const visibleCompletedItems =
    mode === 'open' || isToday
      ? completedTodayItems
      : selectedCalendarItems.filter((item) => item.status === 'COMPLETED');
  const visibleActionCount = usesCurrentOpenProjection
    ? currentGroups.pastDue.length +
      dayItems.length +
      noDateItems.length +
      proposedCurrentGroups.pastDue.length +
      proposedDayItems.length +
      proposedNoDateItems.length
    : dayItems.length + proposedDayItems.length;

  return (
    <>
      {mode === 'day' && (
        <>
          <Box sx={{ px: 2.25, pt: 1.8, pb: 1.5 }}>
            <Stack
              direction="row"
              sx={{ mb: 0.8, alignItems: 'center', justifyContent: 'space-between' }}
            >
              <Typography sx={sectionLabelSx}>Events</Typography>
              <Typography variant="caption" color="text.secondary">
                {events.length}
              </Typography>
            </Stack>
            <EventRows events={events} timezone={timezone} onSelectItem={onSelectItem} />
          </Box>
          <Divider sx={{ mx: 2.25 }} />
        </>
      )}

      <Box sx={{ px: 2.25, py: 1.7 }}>
        <Typography sx={{ ...sectionLabelSx, mb: 0.75 }}>
          {mode === 'open' ? 'Open Actions' : 'Tasks'}
        </Typography>
        {usesCurrentOpenProjection &&
          (currentGroups.pastDue.length > 0 || proposedCurrentGroups.pastDue.length > 0) && (
            <Box sx={{ mb: 1.25 }}>
              <Stack
                direction="row"
                sx={{ mb: 0.35, alignItems: 'center', justifyContent: 'space-between' }}
              >
                <Typography variant="caption" color="error.main" sx={{ fontWeight: 700 }}>
                  Past Due
                </Typography>
                <Typography variant="caption" color="error.main">
                  {currentGroups.pastDue.length + proposedCurrentGroups.pastDue.length}
                </Typography>
              </Stack>
              <PlannerItemRows
                items={currentGroups.pastDue}
                onSelectItem={onSelectItem}
                onCompletePlannerItem={onCompletePlannerItem}
                completingItemIds={completingItemIds}
                proposedEnrichments={proposedEnrichments}
                today={today}
                timezone={timezone}
                pastDue
              />
              <ProposedRows
                items={proposedCurrentGroups.pastDue}
                today={today}
                timezone={timezone}
              />
            </Box>
          )}
        {usesCurrentOpenProjection && (
          <Typography
            variant="caption"
            color="text.secondary"
            sx={{ display: 'block', mb: 0.35, fontWeight: 700 }}
          >
            {mode === 'open' ? 'Open / Today' : 'Today / Open'}
          </Typography>
        )}
        <PlannerItemRows
          items={dayItems}
          onSelectItem={onSelectItem}
          onCompletePlannerItem={onCompletePlannerItem}
          completingItemIds={completingItemIds}
          proposedEnrichments={proposedEnrichments}
          today={today}
          timezone={timezone}
        />
        <ProposedRows items={proposedDayItems} today={today} timezone={timezone} />
        {usesCurrentOpenProjection &&
          (noDateItems.length > 0 || proposedNoDateItems.length > 0) && (
            <Box sx={{ mt: 1.15 }}>
              <Typography
                variant="caption"
                color="text.secondary"
                sx={{ display: 'block', mb: 0.35, fontWeight: 700 }}
              >
                No Date
              </Typography>
              <PlannerItemRows
                items={noDateItems}
                onSelectItem={onSelectItem}
                onCompletePlannerItem={onCompletePlannerItem}
                completingItemIds={completingItemIds}
                proposedEnrichments={proposedEnrichments}
                today={today}
                timezone={timezone}
              />
              <ProposedRows items={proposedNoDateItems} today={today} timezone={timezone} />
            </Box>
          )}
        {visibleActionCount === 0 && (
          <Typography variant="body2" color="text.secondary" sx={{ py: 0.8 }}>
            {mode === 'open'
              ? 'No items need immediate attention.'
              : 'No open tasks for this date.'}
          </Typography>
        )}
      </Box>
      {/* Completion rendering is owned only by the completed projection. It
          remains independent from events and open/date-item emptiness. */}
      {visibleCompletedItems.length > 0 && (
        <CompletedSection
          key={`${mode === 'open' || isToday ? 'today' : selectedDate}-${visibleActionCount === 0 ? 'completion-only' : 'with-open'}`}
          items={visibleCompletedItems}
          label={mode === 'open' || isToday ? 'Completed Today' : 'Completed'}
          onSelectItem={onSelectItem}
          defaultOpen={visibleActionCount === 0}
        />
      )}
    </>
  );
}

function RailHeader({
  date,
  mode,
  openCount,
  onModeChange,
}: {
  date: Date;
  mode: PlannerRailMode;
  openCount: number;
  onModeChange: DayPanelProps['onModeChange'];
}) {
  const dateLabel = new Intl.DateTimeFormat('en-US', {
    weekday: 'long',
    month: 'long',
    day: 'numeric',
  }).format(date);
  const handleChange = (_event: MouseEvent<HTMLElement>, nextMode: PlannerRailMode | null) => {
    if (nextMode) onModeChange(nextMode);
  };
  return (
    <Box
      sx={{
        position: 'sticky',
        top: 0,
        zIndex: 2,
        px: 2.25,
        py: 1.4,
        bgcolor: 'surface.secondary',
        borderBottom: 1,
        borderColor: 'divider',
      }}
    >
      <Stack
        direction="row"
        spacing={1.25}
        sx={{ minWidth: 0, alignItems: 'center', justifyContent: 'space-between' }}
      >
        <Box sx={{ minWidth: 0 }}>
          <Typography
            component="h2"
            sx={{ fontSize: 15, fontWeight: 680, letterSpacing: '-0.015em' }}
            noWrap
          >
            {dateLabel}
          </Typography>
          <Typography variant="caption" color="text.secondary">
            Your time and commitments
          </Typography>
        </Box>
        <ToggleButtonGroup
          exclusive
          size="small"
          value={mode}
          onChange={handleChange}
          aria-label="Planner side panel mode"
          sx={{
            flexShrink: 0,
            '& .MuiToggleButton-root': {
              minHeight: 28,
              px: 1,
              py: 0.2,
              borderColor: 'divider',
              color: 'text.secondary',
              fontSize: 11,
              fontWeight: 700,
              textTransform: 'none',
              '&.Mui-selected': { bgcolor: 'primary.light', color: 'primary.dark' },
            },
          }}
        >
          <ToggleButton value="day">Day</ToggleButton>
          <ToggleButton value="open">Open {openCount}</ToggleButton>
        </ToggleButtonGroup>
      </Stack>
    </Box>
  );
}

function DetailInspector({
  item,
  date,
  today,
  timezone,
  plannerItems,
  onSelectItem,
  onCompletePlannerItem,
  completingItemIds,
  onBack,
  proposedEnrichments = {},
  onRefresh,
}: {
  item: SelectedPlannerItem;
  date: Date;
  today: string;
  timezone: string;
  plannerItems: PlannerItem[];
  onSelectItem: (item: SelectedPlannerItem) => void;
  onCompletePlannerItem: (item: PlannerItem) => Promise<void>;
  completingItemIds: ReadonlySet<string>;
  onBack: () => void;
  proposedEnrichments?: Record<string, { before: string; after: string }>;
  onRefresh?: () => void;
}) {
  const [reminderBusy, setReminderBusy] = useState(false);
  const [reminderMenuAnchor, setReminderMenuAnchor] = useState<null | HTMLElement>(null);

  const hasDueTarget = Boolean(
    item.kind === 'plannerItem' &&
      ((item.dueAt && item.dueAt.trim() !== '') || (item.dueDate && item.dueDate.trim() !== ''))
  );

  const handleSetReminder = async (plannerItemId: string, offsetsSeconds: number[]) => {
    if (!hasDueTarget) {
      return;
    }
    setReminderMenuAnchor(null);
    setReminderBusy(true);
    try {
      await setTodoReminder({ plannerItemId, offsetsSeconds });
      onRefresh?.();
    } finally {
      setReminderBusy(false);
    }
  };


  const handleRemoveReminder = async (plannerItemId: string) => {
    setReminderBusy(true);
    try {
      await removeTodoReminder(plannerItemId);
      onRefresh?.();
    } finally {
      setReminderBusy(false);
    }
  };

  const handleRemoveThreshold = async (plannerItemId: string, offset: number, currentOffsets: number[]) => {
    const nextOffsets = currentOffsets.filter((o) => o !== offset);
    if (nextOffsets.length === 0) {
      await handleRemoveReminder(plannerItemId);
    } else {
      await handleSetReminder(plannerItemId, nextOffsets);
    }
  };

  const handleAddThreshold = async (plannerItemId: string, offset: number, currentOffsets: number[]) => {
    const nextOffsets = Array.from(new Set([...currentOffsets, offset])).sort((a, b) => b - a);
    await handleSetReminder(plannerItemId, nextOffsets);
  };

  const relatedPlannerItems =
    item.kind === 'event'
      ? plannerItems.filter((plannerItem) => item.relatedPlannerItemIds?.includes(plannerItem.id))
      : [];
  const completing = item.kind === 'plannerItem' && completingItemIds.has(item.id);

  return (
    <>
      <Box sx={{ px: 2.25, pt: 1.7, pb: 1.5, bgcolor: 'surface.secondary' }}>
        <Button
          size="small"
          startIcon={<ArrowBackRounded />}
          onClick={onBack}
          sx={{
            ml: -1,
            mb: 1,
            color: 'text.secondary',
            '&:focus-visible': {
              outline: '2px solid',
              outlineColor: 'primary.main',
              outlineOffset: 1,
            },
          }}
        >
          {new Intl.DateTimeFormat('en-US', { month: 'long', day: 'numeric' }).format(date)}
        </Button>
        <Stack
          direction="row"
          spacing={1}
          sx={{ alignItems: 'flex-start', justifyContent: 'space-between' }}
        >
          <Box sx={{ minWidth: 0 }}>
            <Typography
              component="h2"
              sx={{ fontSize: 17, fontWeight: 680, letterSpacing: '-0.025em' }}
            >
              {item.title}
            </Typography>
            <Typography
              variant="caption"
              color="text.secondary"
              sx={{ display: 'block', mt: 0.45 }}
            >
              {item.kind === 'event' ? 'Calendar Event' : 'Planner Item'}
            </Typography>
          </Box>
          <Chip
            label={
              item.kind === 'event'
                ? 'Calendar Event'
                : item.status === 'OPEN'
                  ? 'Open'
                  : 'Completed'
            }
            size="small"
            variant="outlined"
            color={item.kind === 'plannerItem' && item.status === 'OPEN' ? 'warning' : 'primary'}
            sx={{ flexShrink: 0 }}
          />
        </Stack>
      </Box>
      <Divider />

      <Box sx={{ px: 2.25, py: 2 }}>
        <Box sx={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: 2 }}>
          <DetailField
            label="Type"
            value={item.kind === 'event' ? 'Calendar Event' : item.type.replace('_', ' ')}
          />
          <DetailField
            label="Status"
            value={
              item.kind === 'event'
                ? 'Calendar Event'
                : item.status === 'OPEN'
                  ? 'Open'
                  : 'Completed'
            }
          />
          <DetailField
            label="Date"
            value={
              item.kind === 'event'
                ? itemDateLabel(getCalendarEventTimeProjection(item, timezone).date)
                : formatPlannerItemTime(item, today, timezone)
            }
          />
          {item.kind === 'event' ? (
            <DetailField
              label="Time"
              value={formatTime(getCalendarEventTimeProjection(item, timezone).startMinutes)}
            />
          ) : (
            <DetailField
              label="Time precision"
              value={
                item.scheduledAt || item.dueAt
                  ? 'Exact time'
                  : item.scheduledDate || item.dueDate
                    ? 'Date only'
                    : 'No date'
              }
            />
          )}
          <Box sx={{ gridColumn: '1 / -1' }}>
            <DetailField
              label="Source"
              value={
                item.kind === 'event'
                  ? formatPlannerAuthority(item.authority)
                  : formatPlannerItemSource(item).source
              }
            />
          </Box>
          {item.kind === 'plannerItem' && formatPlannerItemSource(item).provider && (
            <Box sx={{ gridColumn: '1 / -1' }}>
              <DetailField label="Provided by" value={formatPlannerItemSource(item).provider ?? ''} />
            </Box>
          )}
          {item.kind === 'plannerItem' && (
            <Box sx={{ gridColumn: '1 / -1' }}>
              <DetailField
                label="Context"
                value={
                  item.contextStatus === 'COMPLETE'
                    ? 'Complete'
                    : item.contextStatus === 'INCOMPLETE'
                      ? item.missingContext.length > 0
                        ? `Incomplete · ${item.missingContext.join(', ')}`
                        : 'Incomplete'
                      : 'Unknown'
                }
              />
            </Box>
          )}
          {item.kind === 'plannerItem' && item.workItemId && (
            <Box sx={{ gridColumn: '1 / -1' }}>
              <DetailField label="Work Item" value={item.workItemTitle || item.workItemId} />
            </Box>
          )}
        </Box>

        {item.kind === 'plannerItem' && (
          <Box sx={{ mt: 2 }}>
            {item.status === 'COMPLETED' ? (
              <Stack
                direction="row"
                spacing={0.75}
                sx={{ alignItems: 'center', color: 'success.main' }}
              >
                <CheckRounded sx={{ fontSize: 18 }} />
                <Typography variant="body2" sx={{ fontWeight: 650 }}>
                  Task completed
                </Typography>
              </Stack>
            ) : item.authority === 'LOCAL' ? (
              <Button
                size="small"
                variant="contained"
                startIcon={
                  completing ? <CircularProgress size={14} color="inherit" /> : <CheckRounded />
                }
                disabled={completing}
                onClick={() => void onCompletePlannerItem(item)}
              >
                {completing ? 'Completing…' : 'Complete Task'}
              </Button>
            ) : (
              <Typography variant="body2" color="text.secondary">
                Managed by {formatPlannerItemSource(item).source}
              </Typography>
            )}

            {item.authority === 'LOCAL' && (
              <Box sx={{ mt: 2, pt: 1.5, borderTop: 1, borderColor: 'divider' }}>
                <Typography sx={sectionLabelSx}>Reminder</Typography>
                <Stack direction="row" spacing={1} sx={{ mt: 0.75, alignItems: 'center', flexWrap: 'wrap' }}>
                  {item.reminder && item.reminder.state === 'ACTIVE' ? (
                    <>
                      {item.reminder.offsetsSeconds.map((offset) => {
                        const isFired = item.reminder?.firedOffsets?.includes(offset);
                        return (
                          <Chip
                            key={offset}
                            label={`${formatOffsetLabel(offset)}${isFired ? ' (sent)' : ''}`}
                            size="small"
                            color={isFired ? 'default' : 'warning'}
                            variant={isFired ? 'filled' : 'outlined'}
                            onDelete={() => void handleRemoveThreshold(item.id, offset, item.reminder!.offsetsSeconds)}
                            disabled={reminderBusy}
                          />
                        );
                      })}
                      <Button
                        size="small"
                        color="primary"
                        disabled={reminderBusy}
                        onClick={(e) => setReminderMenuAnchor(e.currentTarget)}
                        sx={{ fontSize: 12 }}
                      >
                        + Add Time
                      </Button>
                      <Button
                        size="small"
                        color="inherit"
                        disabled={reminderBusy}
                        onClick={() => void handleRemoveReminder(item.id)}
                        sx={{ color: 'text.secondary', fontSize: 12 }}
                      >
                        Clear All
                      </Button>
                      <Menu
                        anchorEl={reminderMenuAnchor}
                        open={Boolean(reminderMenuAnchor)}
                        onClose={() => setReminderMenuAnchor(null)}
                      >
                        <MenuItem onClick={() => void handleAddThreshold(item.id, 0, item.reminder!.offsetsSeconds)}>At due time</MenuItem>
                        <MenuItem onClick={() => void handleAddThreshold(item.id, 900, item.reminder!.offsetsSeconds)}>15 minutes before</MenuItem>
                        <MenuItem onClick={() => void handleAddThreshold(item.id, 1800, item.reminder!.offsetsSeconds)}>30 minutes before</MenuItem>
                        <MenuItem onClick={() => void handleAddThreshold(item.id, 3600, item.reminder!.offsetsSeconds)}>1 hour before</MenuItem>
                        <MenuItem onClick={() => void handleAddThreshold(item.id, 86400, item.reminder!.offsetsSeconds)}>1 day before</MenuItem>
                        <MenuItem onClick={() => void handleAddThreshold(item.id, 172800, item.reminder!.offsetsSeconds)}>2 days before</MenuItem>
                        <MenuItem onClick={() => void handleAddThreshold(item.id, 259200, item.reminder!.offsetsSeconds)}>3 days before</MenuItem>
                      </Menu>
                    </>
                  ) : item.reminder && item.reminder.state === 'SATISFIED' ? (
                    <>
                      <Chip
                        label="Satisfied"
                        size="small"
                        color="success"
                        variant="outlined"
                      />
                      {item.reminder.firedOffsets.length > 0 && (
                        <Typography variant="caption" color="text.secondary">
                          Fired: {item.reminder.firedOffsets.map(formatOffsetLabel).join(', ')}
                        </Typography>
                      )}
                    </>
                  ) : item.status === 'OPEN' ? (
                    !hasDueTarget ? (
                      <Typography variant="body2" color="text.secondary" sx={{ fontStyle: 'italic' }}>
                        Set a due date before adding a due-time reminder.
                      </Typography>
                    ) : (
                      <>
                        <Typography variant="body2" color="text.secondary" sx={{ mr: 0.5 }}>
                          No reminder set
                        </Typography>
                        <Button
                          size="small"
                          variant="outlined"
                          disabled={reminderBusy}
                          onClick={(e) => setReminderMenuAnchor(e.currentTarget)}
                          sx={{ fontSize: 12 }}
                        >
                          Add Reminder
                        </Button>
                        <Menu
                          anchorEl={reminderMenuAnchor}
                          open={Boolean(reminderMenuAnchor)}
                          onClose={() => setReminderMenuAnchor(null)}
                        >
                          <MenuItem onClick={() => void handleSetReminder(item.id, [0])}>At due time</MenuItem>
                          <MenuItem onClick={() => void handleSetReminder(item.id, [900])}>15 minutes before</MenuItem>
                          <MenuItem onClick={() => void handleSetReminder(item.id, [1800])}>30 minutes before</MenuItem>
                          <MenuItem onClick={() => void handleSetReminder(item.id, [3600])}>1 hour before</MenuItem>
                          <MenuItem onClick={() => void handleSetReminder(item.id, [86400])}>1 day before</MenuItem>
                          <MenuItem onClick={() => void handleSetReminder(item.id, [172800])}>2 days before</MenuItem>
                          <MenuItem onClick={() => void handleSetReminder(item.id, [259200])}>3 days before</MenuItem>
                          <Divider />
                          <MenuItem onClick={() => void handleSetReminder(item.id, [259200, 172800, 86400])}>Standard Policy (3d, 2d, 1d before)</MenuItem>
                        </Menu>
                      </>
                    )
                  ) : (
                    <Typography variant="body2" color="text.secondary">
                      None
                    </Typography>
                  )}
                </Stack>
              </Box>
            )}
          </Box>
        )}

        <Box sx={{ mt: 2.5 }}>
          <Typography sx={sectionLabelSx}>
            {item.kind === 'plannerItem' && item.authority === 'EXTERNAL'
              ? `Notes · from ${formatPlannerItemSource(item).source}`
              : 'Description'}
          </Typography>
          <Typography color="text.secondary" sx={{ mt: 0.75 }}>
            {item.details ?? 'No details yet.'}
          </Typography>
        </Box>
        {item.kind === 'plannerItem' && item.authority === 'EXTERNAL' && (
          <Box sx={{ mt: 2 }}>
            <Typography sx={sectionLabelSx}>Lum Harbor note</Typography>
            <Typography color="text.secondary" sx={{ mt: 0.75, whiteSpace: 'pre-wrap' }}>
              {item.localNote ?? 'No Lum Harbor note. Ask the Assistant to add one.'}
            </Typography>
          </Box>
        )}
        {proposedEnrichments[item.id] && (
          <Box
            sx={{
              mt: 1.2,
              p: 1.1,
              border: 1,
              borderStyle: 'dashed',
              borderColor: '#7969C7',
              borderRadius: 1,
              bgcolor: 'rgba(121, 105, 199, .09)',
            }}
          >
            <Typography
              variant="caption"
              sx={{
                color: '#7969C7',
                fontWeight: 750,
                letterSpacing: '.05em',
                textTransform: 'uppercase',
              }}
            >
              Proposed detail
            </Typography>
            <Typography sx={{ mt: 0.4, color: '#7969C7' }}>
              {proposedEnrichments[item.id].after}
            </Typography>
          </Box>
        )}

        {item.kind === 'plannerItem' && (item.workItemId || item.workflowId || item.trackedItemId) && (
          <Box sx={{ mt: 2.5, pt: 1.5, borderTop: 1, borderColor: 'divider' }}>
            <Typography sx={sectionLabelSx}>Origin & Provenance</Typography>
            <Stack spacing={0.6} sx={{ mt: 0.75 }}>
              {item.workItemId && (
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                  <Typography variant="body2" color="text.secondary">Work Item:</Typography>
                  <Typography variant="body2" sx={{ fontWeight: 600 }}>
                    {item.workItemTitle || item.workItemId}
                  </Typography>
                </Box>
              )}
              {item.workflowName && (
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                  <Typography variant="body2" color="text.secondary">Workflow:</Typography>
                  <Typography variant="body2" sx={{ fontWeight: 600 }}>
                    {item.workflowName}
                  </Typography>
                </Box>
              )}
              {item.workflowStepTitle && (
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                  <Typography variant="body2" color="text.secondary">Step:</Typography>
                  <Typography variant="body2" sx={{ fontWeight: 600 }}>
                    {item.workflowStepTitle}
                  </Typography>
                </Box>
              )}
              {item.trackedItemId && (
                <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                  <Typography variant="body2" color="text.secondary">Tracked Item:</Typography>
                  <Typography variant="body2" sx={{ fontWeight: 600 }}>
                    {item.trackedItemTitle || item.trackedItemId}
                  </Typography>
                </Box>
              )}
            </Stack>
          </Box>
        )}
      </Box>

      {relatedPlannerItems.length > 0 && (
        <>
          <Divider sx={{ mx: 2.25 }} />
          <Box sx={{ px: 2.25, py: 2 }}>
            <Typography sx={sectionLabelSx}>Related Actions</Typography>
            <Stack spacing={0.35} sx={{ mt: 0.8 }}>
              {relatedPlannerItems.map((plannerItem) => (
                <ButtonBase
                  key={plannerItem.id}
                  onClick={() => onSelectItem(plannerItem)}
                  sx={{
                    width: '100%',
                    py: 0.65,
                    justifyContent: 'flex-start',
                    borderRadius: 1,
                    color: 'text.primary',
                    '&:hover': { bgcolor: 'surface.secondary' },
                    '&:focus-visible': {
                      outline: '2px solid',
                      outlineColor: 'primary.main',
                      outlineOffset: -2,
                    },
                  }}
                >
                  <CheckBoxOutlineBlankRounded sx={{ mr: 1, fontSize: 17, color: 'icon.muted' }} />
                  <Typography sx={{ fontWeight: 600 }} noWrap>
                    {plannerItem.title}
                  </Typography>
                </ButtonBase>
              ))}
            </Stack>
          </Box>
        </>
      )}

      <Divider sx={{ mx: 2.25 }} />
      <Box sx={{ px: 2.25, py: 2.1 }}>
        <Typography sx={sectionLabelSx}>Recent Changes</Typography>
        {item.recentChanges.length === 0 ? (
          <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
            No recent changes recorded.
          </Typography>
        ) : (
          <Stack sx={{ mt: 1.25 }}>
            {item.recentChanges.map((entry, index) => (
              <Stack key={entry.id} direction="row" spacing={1.25} sx={{ minWidth: 0 }}>
                <Box
                  sx={{ display: 'flex', width: 12, flexDirection: 'column', alignItems: 'center' }}
                >
                  <Box
                    sx={{
                      width: 7,
                      height: 7,
                      mt: 0.55,
                      flexShrink: 0,
                      borderRadius: '50%',
                      border: 1.5,
                      borderColor: 'primary.main',
                      bgcolor: 'surface.subtle',
                    }}
                  />
                  {index < item.recentChanges.length - 1 && (
                    <Box sx={{ width: 1, minHeight: 38, flex: 1, bgcolor: 'divider' }} />
                  )}
                </Box>
                <Box sx={{ minWidth: 0, pb: index < item.recentChanges.length - 1 ? 1.25 : 0 }}>
                  <Typography
                    variant="caption"
                    color="text.secondary"
                    sx={{ fontWeight: 600, fontVariantNumeric: 'tabular-nums' }}
                  >
                    {entry.label}
                  </Typography>
                  <Typography sx={{ mt: 0.15, fontWeight: 550 }}>{entry.description}</Typography>
                </Box>
              </Stack>
            ))}
          </Stack>
        )}
      </Box>
    </>
  );
}

export default function DayPanel(props: DayPanelProps) {
  // The badge reflects backend Current Open membership. It is intentionally
  // independent from whichever Month or Week is visible.
  const openCount = props.currentOpenItems.length;
  return (
    <Box
      component="aside"
      aria-label={props.selectedItem ? 'Planner item details' : 'Selected day'}
      sx={{
        display: 'flex',
        width: '100%',
        height: '100%',
        minWidth: 0,
        minHeight: 0,
        flexDirection: 'column',
        bgcolor: 'surface.subtle',
        overflowX: 'hidden',
        overflowY: 'auto',
        overscrollBehavior: 'contain',
        scrollbarGutter: 'stable',
      }}
    >
      <RailHeader
        date={props.date}
        mode={props.mode}
        openCount={openCount}
        onModeChange={props.onModeChange}
      />
      {props.selectedItem ? (
        <DetailInspector
          item={props.selectedItem}
          date={props.date}
          today={props.today}
          timezone={props.timezone}
          plannerItems={props.calendarItems}
          onSelectItem={props.onSelectItem}
          onCompletePlannerItem={props.onCompletePlannerItem}
          completingItemIds={props.completingItemIds}
          onBack={props.onBack}
          proposedEnrichments={props.proposedEnrichments}
          onRefresh={props.onRefresh}
        />
      ) : (
        <DayOverview {...props} />
      )}
    </Box>
  );
}
