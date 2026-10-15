import CheckBoxOutlineBlankRounded from '@mui/icons-material/CheckBoxOutlineBlankRounded';
import CheckRounded from '@mui/icons-material/CheckRounded';
import { Box, ButtonBase, CircularProgress, IconButton, Stack, Typography } from '@mui/material';
import {
  calendarEventOccursOnDate,
  dateKey,
  formatDuration,
  formatTime,
  getCalendarEventTimeProjection,
  getWeekDates,
  type CalendarEvent,
  type PlannerItemTimeProjection,
} from '../../pages/plannerData';

interface WeekViewProps {
  period: Date;
  selectedDate: Date;
  today: string;
  onSelectDate: (date: Date) => void;
  onSelectItem: (item: PlannerItemTimeProjection['item'], date: Date) => void;
  /** Delegates compact Calendar checkbox completion to the page owner. */
  onCompletePlannerItem: (item: PlannerItemTimeProjection['item']) => Promise<void>;
  /** Disables repeat completion while mutation or refresh is pending. */
  completingItemIds: ReadonlySet<string>;
  events: CalendarEvent[];
  plannerItems: PlannerItemTimeProjection[];
  proposedItems: PlannerItemTimeProjection[];
}

const START_HOUR = 8;
const END_HOUR = 18;
const HOUR_HEIGHT = 48;
const EVENT_GAP = 4;
const MIN_EVENT_HEIGHT = 24;
const timelineHours = Array.from(
  { length: END_HOUR - START_HOUR },
  (_, index) => START_HOUR + index,
);

function hourLabel(hour: number) {
  const date = new Date(2026, 0, 1, hour);
  return new Intl.DateTimeFormat('en-US', { hour: 'numeric' }).format(date);
}

function getEventTop(startMinutes: number) {
  return ((startMinutes - START_HOUR * 60) / 60) * HOUR_HEIGHT + EVENT_GAP / 2;
}

function getEventHeight(durationMinutes: number) {
  const durationHeight = (durationMinutes / 60) * HOUR_HEIGHT - EVENT_GAP;
  return Math.max(MIN_EVENT_HEIGHT, durationHeight);
}

export default function WeekView({
  period,
  selectedDate,
  today,
  onSelectDate,
  onSelectItem,
  onCompletePlannerItem,
  completingItemIds,
  events: allEvents,
  plannerItems,
  proposedItems,
}: WeekViewProps) {
  const days = getWeekDates(period);
  const selectedKey = dateKey(selectedDate);
  const todayKey = today;

  return (
    <Box sx={{ minWidth: 0, overflow: 'hidden' }}>
      <Box
        sx={{
          display: 'grid',
          gridTemplateColumns: '52px repeat(7, minmax(0, 1fr))',
          minHeight: 48,
          borderBottom: 1,
          borderColor: 'divider',
          bgcolor: 'surface.subtle',
        }}
      >
        <Box />
        {days.map((date) => {
          const key = dateKey(date);
          const isSelected = key === selectedKey;
          const isToday = key === todayKey;
          return (
            <ButtonBase
              key={key}
              onClick={() => onSelectDate(date)}
              aria-label={`Select ${new Intl.DateTimeFormat('en-US', { dateStyle: 'full' }).format(date)}`}
              aria-pressed={isSelected}
              sx={{
                minWidth: 0,
                borderLeft: 1,
                borderLeftColor: 'divider',
                borderBottom: isSelected ? 2 : 0,
                borderBottomColor: 'primary.main',
                bgcolor: isSelected ? 'surface.secondary' : 'transparent',
                color: 'text.primary',
                '&:hover': { bgcolor: 'surface.secondary' },
                '&:focus-visible': {
                  outline: '2px solid',
                  outlineColor: 'primary.main',
                  outlineOffset: -2,
                },
              }}
            >
              <Stack spacing={0.15} sx={{ alignItems: 'center', minWidth: 0 }}>
                <Typography
                  variant="caption"
                  color="text.secondary"
                  noWrap
                  sx={{ fontSize: 9.5, textTransform: 'uppercase' }}
                >
                  {new Intl.DateTimeFormat('en-US', { weekday: 'short' }).format(date)}
                </Typography>
                <Box
                  sx={{
                    display: 'grid',
                    placeItems: 'center',
                    width: 22,
                    height: 22,
                    borderRadius: '50%',
                    border: isToday ? 1 : 0,
                    borderColor: 'primary.main',
                    color: isToday ? 'primary.main' : 'text.primary',
                    fontSize: 11,
                    fontWeight: isToday || isSelected ? 700 : 550,
                  }}
                >
                  {date.getDate()}
                </Box>
              </Stack>
            </ButtonBase>
          );
        })}
      </Box>

      <Box
        sx={{
          display: 'grid',
          gridTemplateColumns: '52px repeat(7, minmax(0, 1fr))',
          minHeight: 38,
          borderBottom: 1,
          borderColor: 'divider',
        }}
      >
        <Box sx={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-end', pr: 0.8 }}>
          <Typography variant="caption" color="text.secondary" sx={{ fontSize: 9 }}>
            all-day
          </Typography>
        </Box>
        {days.map((date) => {
          const key = dateKey(date);
          const allDayEvents = allEvents.filter(
            (event) => event.isAllDay && calendarEventOccursOnDate(event, key),
          );
          const dateItems = plannerItems.filter(
            (entry) => entry.date === key && entry.precision === 'date',
          );
          const proposedDateItems = proposedItems.filter(
            (entry) => entry.date === key && entry.precision === 'date',
          );
          return (
            <Box
              key={dateKey(date)}
              sx={{ minWidth: 0, p: 0.45, borderLeft: 1, borderColor: 'divider' }}
            >
              {allDayEvents.map((event) => (
                <Box
                  key={event.id}
                  component="button"
                  type="button"
                  onClick={() => onSelectDate(date)}
                  sx={{
                    display: 'block',
                    width: '100%',
                    minWidth: 0,
                    p: 0.35,
                    border: 0,
                    borderLeft: 2,
                    borderColor: 'primary.main',
                    borderRadius: 0.5,
                    bgcolor: 'primary.light',
                    color: 'primary.dark',
                    font: 'inherit',
                    fontSize: 9.5,
                    fontWeight: 650,
                    textAlign: 'left',
                    overflow: 'hidden',
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                    cursor: 'pointer',
                    '&:hover': { filter: 'brightness(0.97)' },
                    '&:focus-visible': {
                      outline: '2px solid',
                      outlineColor: 'primary.main',
                      outlineOffset: 1,
                    },
                  }}
                >
                  {event.title}
                </Box>
              ))}
              {dateItems.map(({ item }) => (
                <Stack
                  key={item.id}
                  direction="row"
                  sx={{
                    width: '100%',
                    minWidth: 0,
                    mt: 0.3,
                    overflow: 'hidden',
                    borderLeft: 2,
                    borderColor: 'text.secondary',
                    borderRadius: 0.5,
                    bgcolor: 'surface.secondary',
                    color: 'text.primary',
                    alignItems: 'center',
                  }}
                >
                  <IconButton
                    size="small"
                    disabled={
                      item.status !== 'OPEN' ||
                      item.authority !== 'LOCAL' ||
                      completingItemIds.has(item.id)
                    }
                    aria-label={
                      item.status === 'COMPLETED'
                        ? `${item.title} is completed`
                        : item.authority === 'EXTERNAL'
                          ? `${item.title} is managed by an external source`
                          : `Complete ${item.title}`
                    }
                    onClick={(event) => {
                      event.stopPropagation();
                      if (item.status === 'OPEN' && item.authority === 'LOCAL') {
                        void onCompletePlannerItem(item);
                      }
                    }}
                    sx={{ width: 18, height: 18, p: 0.2, flexShrink: 0 }}
                  >
                    {completingItemIds.has(item.id) ? (
                      <CircularProgress size={11} color="inherit" />
                    ) : item.status === 'COMPLETED' ? (
                      <CheckRounded sx={{ fontSize: 12, color: 'success.main' }} />
                    ) : (
                      <CheckBoxOutlineBlankRounded sx={{ fontSize: 12 }} />
                    )}
                  </IconButton>
                  <ButtonBase
                    onClick={() => onSelectItem(item, date)}
                    sx={{
                      minWidth: 0,
                      flex: 1,
                      p: 0.35,
                      justifyContent: 'flex-start',
                      color: 'inherit',
                      fontSize: 9.5,
                      fontWeight: 650,
                      textAlign: 'left',
                    }}
                  >
                    <Typography
                      component="span"
                      noWrap
                      sx={{
                        fontSize: 'inherit',
                        textDecoration: item.status === 'COMPLETED' ? 'line-through' : 'none',
                      }}
                    >
                      {item.title}
                    </Typography>
                  </ButtonBase>
                </Stack>
              ))}
              {proposedDateItems.map(({ item }) => (
                <Box
                  key={item.id}
                  sx={{
                    mt: 0.3,
                    p: 0.3,
                    overflow: 'hidden',
                    border: 1,
                    borderStyle: 'dashed',
                    borderColor: '#7969C7',
                    borderRadius: 0.5,
                    color: '#7969C7',
                    bgcolor: 'rgba(121, 105, 199, .09)',
                    fontSize: 9,
                    fontWeight: 700,
                    textOverflow: 'ellipsis',
                    whiteSpace: 'nowrap',
                  }}
                >
                  {item.title} · Proposed
                </Box>
              ))}
            </Box>
          );
        })}
      </Box>

      <Box sx={{ display: 'grid', gridTemplateColumns: '52px minmax(0, 1fr)' }}>
        <Box sx={{ position: 'relative', height: timelineHours.length * HOUR_HEIGHT }}>
          {timelineHours.map((hour) => (
            <Typography
              key={hour}
              variant="caption"
              color="text.secondary"
              sx={{
                position: 'absolute',
                top: (hour - START_HOUR) * HOUR_HEIGHT - 7,
                right: 8,
                fontSize: 9,
                fontVariantNumeric: 'tabular-nums',
              }}
            >
              {hourLabel(hour)}
            </Typography>
          ))}
        </Box>

        <Box
          sx={{ display: 'grid', gridTemplateColumns: 'repeat(7, minmax(0, 1fr))', minWidth: 0 }}
        >
          {days.map((date) => {
            const key = dateKey(date);
            const events = allEvents
              .filter((event) => !event.isAllDay && calendarEventOccursOnDate(event, key))
              .map((event) => ({ event, placement: getCalendarEventTimeProjection(event) }))
              .filter(
                ({ placement }) =>
                  placement.startMinutes !== null &&
                  placement.startMinutes >= START_HOUR * 60 &&
                  placement.startMinutes < END_HOUR * 60,
              );
            const timedItems = plannerItems.filter(
              (entry) =>
                entry.date === key &&
                entry.precision === 'datetime' &&
                entry.minutes !== null &&
                entry.minutes >= START_HOUR * 60 &&
                entry.minutes < END_HOUR * 60,
            );
            const proposedTimedItems = proposedItems.filter(
              (entry) =>
                entry.date === key &&
                entry.precision === 'datetime' &&
                entry.minutes !== null &&
                entry.minutes >= START_HOUR * 60 &&
                entry.minutes < END_HOUR * 60,
            );
            const isSelected = dateKey(date) === selectedKey;
            return (
              <Box
                key={dateKey(date)}
                onClick={() => onSelectDate(date)}
                sx={{
                  position: 'relative',
                  minWidth: 0,
                  height: timelineHours.length * HOUR_HEIGHT,
                  borderLeft: 1,
                  borderColor: 'divider',
                  bgcolor: isSelected ? 'surface.subtle' : 'background.paper',
                  boxShadow: isSelected ? 'inset 0 2px 0 0' : 'none',
                  color: isSelected ? 'primary.main' : 'text.primary',
                  cursor: 'pointer',
                }}
              >
                {timelineHours.map((hour) => (
                  <Box
                    key={hour}
                    sx={{
                      height: HOUR_HEIGHT,
                      borderTop: hour === START_HOUR ? 0 : 1,
                      borderColor: 'divider',
                    }}
                  />
                ))}
                {events.map(({ event, placement }) => {
                  const top = getEventTop(placement.startMinutes!);
                  const height = getEventHeight(placement.durationMinutes);
                  const overlapsPlannerItem = timedItems.some(
                    (entry) =>
                      entry.minutes !== null &&
                      entry.minutes >= placement.startMinutes! &&
                      entry.minutes < placement.startMinutes! + placement.durationMinutes,
                  );
                  return (
                    <Box
                      key={event.id}
                      component="button"
                      type="button"
                      onClick={(clickEvent) => {
                        clickEvent.stopPropagation();
                        onSelectDate(date);
                      }}
                      sx={{
                        position: 'absolute',
                        zIndex: 1,
                        top,
                        left: 3,
                        right: overlapsPlannerItem ? '51%' : 3,
                        height,
                        minHeight: MIN_EVENT_HEIGHT,
                        minWidth: 0,
                        p: 0.5,
                        overflow: 'hidden',
                        border: 0,
                        borderLeft: 2,
                        borderColor: 'primary.main',
                        borderRadius: 0.65,
                        bgcolor: 'primary.light',
                        color: 'primary.dark',
                        font: 'inherit',
                        textAlign: 'left',
                        cursor: 'pointer',
                        '&:hover': { filter: 'brightness(0.97)' },
                        '&:focus-visible': {
                          outline: '2px solid',
                          outlineColor: 'primary.main',
                          outlineOffset: 1,
                        },
                      }}
                    >
                      <Typography
                        component="span"
                        sx={{ display: 'block', fontSize: 9.5, lineHeight: 1.25, fontWeight: 700 }}
                        noWrap
                      >
                        {event.title}
                      </Typography>
                      {height >= 40 && (
                        <Typography
                          component="span"
                          sx={{ display: 'block', fontSize: 9, lineHeight: 1.3 }}
                          noWrap
                        >
                          {formatTime(placement.startMinutes)}
                        </Typography>
                      )}
                      {height >= 58 && (
                        <Typography
                          component="span"
                          sx={{ display: 'block', fontSize: 9, lineHeight: 1.3, opacity: 0.82 }}
                          noWrap
                        >
                          {formatDuration(placement.durationMinutes)}
                        </Typography>
                      )}
                    </Box>
                  );
                })}
                {timedItems.map(({ item, minutes }) => {
                  const overlapsEvent = events.some(
                    ({ placement }) =>
                      minutes !== null &&
                      minutes >= placement.startMinutes! &&
                      minutes < placement.startMinutes! + placement.durationMinutes,
                  );
                  return (
                    <Box
                      key={item.id}
                      component="button"
                      type="button"
                      onClick={(clickEvent) => {
                        clickEvent.stopPropagation();
                        onSelectItem(item, date);
                      }}
                      sx={{
                        position: 'absolute',
                        zIndex: 2,
                        top: getEventTop(minutes!),
                        left: overlapsEvent ? '51%' : 3,
                        right: 3,
                        minHeight: MIN_EVENT_HEIGHT,
                        p: 0.45,
                        overflow: 'hidden',
                        border: 1,
                        borderColor: 'divider',
                        borderLeft: 2,
                        borderLeftColor: 'text.secondary',
                        borderRadius: 0.65,
                        bgcolor: 'surface.secondary',
                        color: 'text.primary',
                        font: 'inherit',
                        fontSize: 9.5,
                        fontWeight: 700,
                        textAlign: 'left',
                        cursor: 'pointer',
                      }}
                    >
                      {item.title}
                      <Typography
                        component="span"
                        sx={{ display: 'block', fontSize: 9, color: 'text.secondary' }}
                      >
                        {formatTime(minutes)}
                      </Typography>
                    </Box>
                  );
                })}
                {proposedTimedItems.map(({ item, minutes }) => (
                  <Box
                    key={item.id}
                    sx={{
                      position: 'absolute',
                      zIndex: 3,
                      top: getEventTop(minutes!),
                      left: '51%',
                      right: 3,
                      minHeight: MIN_EVENT_HEIGHT,
                      p: 0.4,
                      overflow: 'hidden',
                      border: 1,
                      borderStyle: 'dashed',
                      borderColor: '#7969C7',
                      borderRadius: 0.65,
                      bgcolor: 'rgba(121, 105, 199, .12)',
                      color: '#7969C7',
                      fontSize: 9,
                      fontWeight: 700,
                    }}
                  >
                    {item.title} · Proposed
                  </Box>
                ))}
              </Box>
            );
          })}
        </Box>
      </Box>
    </Box>
  );
}
