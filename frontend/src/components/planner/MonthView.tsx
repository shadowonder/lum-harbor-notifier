import CheckBoxOutlineBlankRounded from '@mui/icons-material/CheckBoxOutlineBlankRounded';
import CheckRounded from '@mui/icons-material/CheckRounded';
import { Box, ButtonBase, CircularProgress, IconButton, Stack, Typography } from '@mui/material';
import {
  calendarEventOccursOnDate,
  dateKey,
  formatTime,
  getCalendarEventTimeProjection,
  getMonthGridDates,
  type CalendarEvent,
  type PlannerItemTimeProjection,
} from '../../pages/plannerData';

interface MonthViewProps {
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

const weekDays = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'];

export default function MonthView({
  period,
  selectedDate,
  today,
  onSelectDate,
  onSelectItem,
  onCompletePlannerItem,
  completingItemIds,
  events,
  plannerItems,
  proposedItems,
}: MonthViewProps) {
  const days = getMonthGridDates(period);
  const selectedKey = dateKey(selectedDate);
  const todayKey = today;
  return (
    <Box sx={{ minWidth: 0, overflow: 'hidden' }}>
      <Box
        aria-hidden="true"
        sx={{
          display: 'grid',
          gridTemplateColumns: 'repeat(7, minmax(0, 1fr))',
          minHeight: 34,
          alignItems: 'center',
          bgcolor: 'surface.subtle',
          borderBottom: 1,
          borderColor: 'divider',
        }}
      >
        {weekDays.map((day) => (
          <Typography
            key={day}
            variant="caption"
            color="text.secondary"
            sx={{ px: 1, fontWeight: 650, letterSpacing: '.035em' }}
          >
            {day}
          </Typography>
        ))}
      </Box>
      <Box
        role="grid"
        aria-label={new Intl.DateTimeFormat('en-US', { month: 'long', year: 'numeric' }).format(
          period,
        )}
        sx={{ display: 'grid', gridTemplateColumns: 'repeat(7, minmax(0, 1fr))' }}
      >
        {days.map((date, index) => {
          const key = dateKey(date);
          const dayEvents = events.filter((event) => calendarEventOccursOnDate(event, key));
          const dayItems = plannerItems.filter((entry) => entry.date === key);
          const dayProposed = proposedItems.filter((entry) => entry.date === key);
          const visibleEvents = dayEvents.slice(0, 1);
          const visibleItems = dayItems.slice(0, 2);
          const hiddenCount = Math.max(
            0,
            dayEvents.length - visibleEvents.length + dayItems.length - visibleItems.length,
          );
          const isSelected = key === selectedKey;
          const isToday = key === todayKey;
          return (
            <ButtonBase
              component="div"
              key={key}
              role="gridcell"
              onClick={() => onSelectDate(date)}
              aria-label={`${new Intl.DateTimeFormat('en-US', { dateStyle: 'full' }).format(date)}, ${dayEvents.length} events, ${dayItems.length} scheduled planner items${dayProposed.length ? `, ${dayProposed.length} proposed` : ''}`}
              aria-selected={isSelected}
              sx={{
                display: 'block',
                position: 'relative',
                minWidth: 0,
                minHeight: 'clamp(88px, 8.3cqw, 116px)',
                p: 0.8,
                overflow: 'hidden',
                textAlign: 'left',
                borderRight: index % 7 === 6 ? 0 : 1,
                borderBottom: index >= 35 ? 0 : 1,
                borderColor: 'divider',
                bgcolor: isSelected ? 'surface.secondary' : 'background.paper',
                boxShadow: isSelected
                  ? 'inset 0 2px 0 0 currentColor, inset 0 0 0 1px currentColor'
                  : 'none',
                color: isSelected ? 'primary.main' : 'text.primary',
                opacity: date.getMonth() === period.getMonth() ? 1 : 0.66,
                alignItems: 'stretch',
                justifyContent: 'flex-start',
                '&:hover': { bgcolor: isSelected ? 'surface.secondary' : 'surface.subtle' },
                '&:focus-visible': {
                  outline: '2px solid',
                  outlineColor: 'primary.main',
                  outlineOffset: -2,
                },
              }}
            >
              <Box
                sx={{
                  display: 'grid',
                  placeItems: 'center',
                  width: 24,
                  height: 24,
                  mb: 0.45,
                  borderRadius: '50%',
                  border: isToday ? 1 : 0,
                  borderColor: 'primary.main',
                  color: isToday ? 'primary.main' : isSelected ? 'text.primary' : 'text.secondary',
                  fontSize: 11,
                  fontWeight: isToday || isSelected ? 700 : 550,
                  fontVariantNumeric: 'tabular-nums',
                }}
              >
                {date.getDate()}
              </Box>
              <Stack spacing={0.35} sx={{ minWidth: 0 }}>
                {visibleEvents.map((event) => (
                  <Box
                    key={event.id}
                    sx={{
                      minWidth: 0,
                      px: 0.55,
                      py: 0.25,
                      borderLeft: 2,
                      borderColor: 'primary.main',
                      borderRadius: 0.5,
                      bgcolor: 'primary.light',
                      color: 'primary.dark',
                      fontSize: 10,
                      lineHeight: 1.35,
                      overflow: 'hidden',
                      textOverflow: 'ellipsis',
                      whiteSpace: 'nowrap',
                    }}
                  >
                    <Box
                      component="span"
                      sx={{ fontWeight: 700, fontVariantNumeric: 'tabular-nums' }}
                    >
                      {event.isAllDay
                        ? ''
                        : `${formatTime(getCalendarEventTimeProjection(event).startMinutes)} `}
                    </Box>
                    {event.title}
                  </Box>
                ))}
                {visibleItems.map(({ item, precision, minutes }) => (
                  <Stack
                    key={item.id}
                    direction="row"
                    spacing={0.35}
                    sx={{ minWidth: 0, alignItems: 'center', color: 'text.secondary' }}
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
                      sx={{ width: 17, height: 17, p: 0, flexShrink: 0 }}
                    >
                      {completingItemIds.has(item.id) ? (
                        <CircularProgress size={11} color="inherit" />
                      ) : item.status === 'COMPLETED' ? (
                        <CheckRounded sx={{ fontSize: 12, color: 'success.main' }} />
                      ) : (
                        <CheckBoxOutlineBlankRounded sx={{ fontSize: 12, color: 'icon.muted' }} />
                      )}
                    </IconButton>
                    <ButtonBase
                      onClick={(event) => {
                        event.stopPropagation();
                        onSelectItem(item, date);
                      }}
                      sx={{ minWidth: 0, color: 'inherit', textAlign: 'left' }}
                    >
                      <Typography
                        variant="caption"
                        noWrap
                        sx={{
                          fontSize: 10,
                          textDecoration: item.status === 'COMPLETED' ? 'line-through' : 'none',
                        }}
                      >
                        <Box component="span" sx={{ fontWeight: 700 }}>
                          {precision === 'datetime' && minutes !== null
                            ? `${formatTime(minutes)} `
                            : ''}
                        </Box>
                        {item.title}
                      </Typography>
                    </ButtonBase>
                  </Stack>
                ))}
                {dayProposed.map(({ item, precision, minutes }) => (
                  <Typography
                    key={item.id}
                    variant="caption"
                    noWrap
                    sx={{
                      px: 0.45,
                      py: 0.15,
                      border: 1,
                      borderStyle: 'dashed',
                      borderColor: '#7969C7',
                      borderRadius: 0.5,
                      color: '#7969C7',
                      bgcolor: 'rgba(121, 105, 199, .09)',
                      fontSize: 9.5,
                      fontWeight: 700,
                    }}
                  >
                    {precision === 'datetime' && minutes !== null ? `${formatTime(minutes)} ` : ''}
                    {item.title} · Proposed
                  </Typography>
                ))}
                {hiddenCount > 0 && (
                  <Typography
                    variant="caption"
                    color="text.secondary"
                    sx={{ pl: 0.55, fontSize: 10, fontWeight: 650 }}
                  >
                    +{hiddenCount} more
                  </Typography>
                )}
              </Stack>
            </ButtonBase>
          );
        })}
      </Box>
    </Box>
  );
}
