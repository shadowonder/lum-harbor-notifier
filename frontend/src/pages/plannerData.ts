/** Presentation-only history entry retained by the current Planner inspector. */
export interface RecentChangeEntry {
  id: string;
  label: string;
  description: string;
}

/** Identifies whether Lum Harbor or an external system owns an entity. */
export type PlannerAuthority = 'LOCAL' | 'EXTERNAL';

/** Describes whether a Planner Item has enough context to be actionable. */
export type PlannerContextStatus = 'COMPLETE' | 'INCOMPLETE' | 'UNKNOWN';

/** Supported Planner Item categories exposed by the Go Planner domain. */
export type PlannerItemType = 'TODO' | 'CHECKPOINT' | 'FOLLOW_UP' | 'REMINDER';

/** Supported Planner Item lifecycle states exposed by the Go Planner domain. */
export type PlannerItemStatus = 'OPEN' | 'COMPLETED';

/**
 * Frontend Calendar Event model matching the GetPlannerView transport
 * semantics. Timed events use exact instants; all-day events use an exclusive
 * date-only range.
 */
export interface CalendarEvent {
  kind: 'event';
  id: string;
  authority: PlannerAuthority;
  title: string;
  details: string | null;
  location: string | null;
  startAt: string | null;
  endAt: string | null;
  startDate: string | null;
  endDate: string | null;
  isAllDay: boolean;
  timezone: string | null;
  recentChanges: RecentChangeEntry[];
  relatedPlannerItemIds?: string[];
  relatedWorkItemId?: string;
}

export interface TodoReminder {
  id: string;
  plannerItemId: string;
  state: 'ACTIVE' | 'SATISFIED' | 'CANCELLED';
  offsetsSeconds: number[];
  firedOffsets: number[];
  remindMode: string;
}

export interface PlannerRecentChange {
  eventId: string;
  eventType: string;
  summary: string;
  occurredAt: string;
  actor: string;
}

/**
 * Read-only provenance of an EXTERNAL Planner Item, resolved by the backend
 * from its external identity and producing Source/Plugin.
 */
export interface PlannerExternalOrigin {
  /** The external system that owns the item, e.g. "Apple Reminders". */
  systemLabel: string;
  /** The providing Plugin's manifest name, when known. */
  providerName: string | null;
  /** The providing Plugin's extension ID, when known. */
  extensionId: string | null;
}

/**
 * Frontend Planner Item model preserving exact-time and date-only placement as
 * independent values from the Go Planner domain.
 */
export interface PlannerItem {
  kind: 'plannerItem';
  id: string;
  type: PlannerItemType;
  title: string;
  details?: string | null;
  status: PlannerItemStatus;
  scheduledAt?: string | null;
  scheduledDate?: string | null;
  dueAt?: string | null;
  dueDate?: string | null;
  completedAt?: string | null;
  contextStatus: PlannerContextStatus;
  missingContext: string[];
  authority: PlannerAuthority;
  recentChanges: RecentChangeEntry[];
  relatedWorkItemId?: string;
  workItemId?: string | null;
  workflowId?: string | null;
  workflowStepId?: string | null;
  workItemTitle?: string | null;
  workflowName?: string | null;
  workflowStepTitle?: string | null;
  trackedItemId?: string | null;
  trackedItemTitle?: string | null;
  reminder?: TodoReminder | null;
  /** Lum Harbor-owned note; for EXTERNAL items it never replaces `details`. */
  localNote?: string | null;
  externalOrigin?: PlannerExternalOrigin | null;
}

/** Inspector selection; the discriminant preserves Event and Item identities. */
export type SelectedPlannerItem = CalendarEvent | PlannerItem;

/** Local-only right rail mode; changing it never requires a backend read. */
export type PlannerRailMode = 'day' | 'open';

/** Derived Calendar placement for a Planner Item; it is not authoritative state. */
export interface PlannerItemTimeProjection {
  item: PlannerItem;
  date: string;
  precision: 'datetime' | 'date';
  minutes: number | null;
}

/**
 * Presentation groups derived only after backend Current Open membership has
 * been established. These groups never decide whether an item is Current Open.
 */
export interface CurrentOpenPlannerItemGroups {
  pastDue: PlannerItem[];
  today: PlannerItem[];
  noDate: PlannerItem[];
}

/** Calendar placement derived from one event without changing its domain shape. */
export interface CalendarEventTimeProjection {
  date: string;
  startMinutes: number | null;
  durationMinutes: number;
}

/** Complete set of projections returned by the Planner page-level backend API. */
export interface PlannerViewData {
  calendarEvents: CalendarEvent[];
  calendarItems: PlannerItem[];
  currentOpenItems: PlannerItem[];
  /** Completions inside the visible Calendar range for selected-day views. */
  completedItems: PlannerItem[];
  /** Completions on request `today`, independent of the visible Calendar range. */
  completedTodayItems: PlannerItem[];
}

/** Planner calendar modes whose rendered boundaries determine backend reads. */
export type PlannerCalendarView = 'month' | 'week';

/** Half-open local-date range used by the Planner page read API. */
export interface PlannerVisibleRange {
  rangeStart: string;
  rangeEnd: string;
}

/**
 * Serializes a Date's local calendar fields as YYYY-MM-DD.
 *
 * This deliberately avoids `toISOString()`: UTC conversion can move a local
 * date to the previous or next calendar day.
 */
export function toLocalDateKey(date: Date) {
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  return `${year}-${month}-${day}`;
}

/** Returns today's calendar date in the user's local timezone. */
export function getLocalTodayKey(now = new Date()) {
  return toLocalDateKey(now);
}

/**
 * Advances a YYYY-MM-DD value by local calendar days without converting it to
 * UTC or assuming a fixed 24-hour duration. Local noon keeps the calculation
 * stable across daylight-saving transitions.
 */
export function addLocalCalendarDays(localDate: string, amount: number) {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(localDate);
  if (!match) throw new Error(`Invalid local calendar date: ${localDate}`);

  const [, year, month, day] = match;
  const value = new Date(Number(year), Number(month) - 1, Number(day), 12);
  if (toLocalDateKey(value) !== localDate) {
    throw new Error(`Invalid local calendar date: ${localDate}`);
  }

  value.setDate(value.getDate() + amount);
  return toLocalDateKey(value);
}

/** Returns the browser's IANA timezone used by the Planner backend request. */
export function getBrowserTimezone() {
  const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  if (!timezone) throw new Error('Browser did not provide an IANA timezone');
  return timezone;
}

/**
 * Returns the 42 Monday-first dates rendered by MonthView.
 *
 * Calendar arithmetic uses local noon to avoid manufacturing midnight
 * instants around daylight-saving transitions.
 */
export function getMonthGridDates(period: Date) {
  const first = new Date(period.getFullYear(), period.getMonth(), 1, 12);
  const start = new Date(first);
  start.setDate(first.getDate() - ((first.getDay() + 6) % 7));
  return Array.from({ length: 42 }, (_, index) => {
    const date = new Date(start);
    date.setDate(start.getDate() + index);
    return date;
  });
}

/** Returns the Monday-to-Sunday dates rendered by WeekView. */
export function getWeekDates(period: Date) {
  const mondayOffset = (period.getDay() + 6) % 7;
  const monday = new Date(period);
  monday.setHours(12, 0, 0, 0);
  monday.setDate(period.getDate() - mondayOffset);
  return Array.from({ length: 7 }, (_, index) => {
    const date = new Date(monday);
    date.setDate(monday.getDate() + index);
    return date;
  });
}

/**
 * Returns the exact half-open local-date range rendered by a Planner view.
 * Month uses its full 42-day grid; Week ends on the Monday after its Sunday.
 */
export function getVisiblePlannerRange(
  view: PlannerCalendarView,
  period: Date,
): PlannerVisibleRange {
  const dates = view === 'month' ? getMonthGridDates(period) : getWeekDates(period);
  const end = new Date(dates[dates.length - 1]);
  end.setDate(end.getDate() + 1);
  return { rangeStart: toLocalDateKey(dates[0]), rangeEnd: toLocalDateKey(end) };
}

/** Backward-compatible name for local calendar keys used throughout Planner UI. */
export const dateKey = toLocalDateKey;

function zonedInstantParts(value: string, timezone: string) {
  const instant = new Date(value);
  if (Number.isNaN(instant.getTime())) throw new Error(`Invalid RFC3339 instant: ${value}`);
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone: timezone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    hourCycle: 'h23',
  }).formatToParts(instant);
  const valueOf = (type: Intl.DateTimeFormatPartTypes) => {
    const part = parts.find((candidate) => candidate.type === type)?.value;
    if (!part) throw new Error(`Unable to resolve ${type} for ${value}`);
    return part;
  };
  const hour = Number(valueOf('hour'));
  const minute = Number(valueOf('minute'));
  return {
    date: `${valueOf('year')}-${valueOf('month')}-${valueOf('day')}`,
    minutes: hour * 60 + minute,
  };
}

/** Resolves an RFC3339 instant to its calendar date in an explicit IANA timezone. */
export function instantToLocalDateKey(value: string, timezone: string) {
  return zonedInstantParts(value, timezone).date;
}

/**
 * Derives calendar placement for rendering while preserving exact and
 * date-only boundaries on the CalendarEvent itself.
 */
export function getCalendarEventTimeProjection(
  event: CalendarEvent,
  timezone = getBrowserTimezone(),
): CalendarEventTimeProjection {
  if (event.isAllDay) {
    if (!event.startDate || !event.endDate) {
      throw new Error(`All-day Calendar Event ${event.id} is missing date boundaries`);
    }
    return { date: event.startDate, startMinutes: null, durationMinutes: 0 };
  }
  if (!event.startAt || !event.endAt) {
    throw new Error(`Timed Calendar Event ${event.id} is missing exact boundaries`);
  }
  const start = new Date(event.startAt);
  const end = new Date(event.endAt);
  const placement = zonedInstantParts(event.startAt, timezone);
  return {
    date: placement.date,
    startMinutes: placement.minutes,
    durationMinutes: Math.max(0, Math.round((end.getTime() - start.getTime()) / 60_000)),
  };
}

/**
 * Reports whether an event occupies a local calendar date.
 * All-day `endDate` is exclusive, matching the Go Calendar contract.
 */
export function calendarEventOccursOnDate(
  event: CalendarEvent,
  localDate: string,
  timezone = getBrowserTimezone(),
) {
  if (event.isAllDay) {
    return Boolean(
      event.startDate && event.endDate && event.startDate <= localDate && localDate < event.endDate,
    );
  }
  return getCalendarEventTimeProjection(event, timezone).date === localDate;
}

/** Selects visible Calendar Events for a day without performing a backend read. */
export function calendarEventsForDate(
  events: CalendarEvent[],
  date: Date,
  timezone = getBrowserTimezone(),
) {
  const localDate = toLocalDateKey(date);
  return events.filter((event) => calendarEventOccursOnDate(event, localDate, timezone));
}

function dateOnlyToLocal(value: string) {
  const [year, month, day] = value.split('-').map(Number);
  return new Date(year, month - 1, day, 12);
}

/**
 * Projects a Planner Item onto the local Calendar without converting
 * date-only values into artificial midnight timestamps.
 */
export function getPlannerItemTimeProjection(
  item: PlannerItem,
  timezone = getBrowserTimezone(),
): PlannerItemTimeProjection | null {
  const exact = item.scheduledAt ?? item.dueAt;
  if (exact) {
    const value = zonedInstantParts(exact, timezone);
    return {
      item,
      date: value.date,
      precision: 'datetime',
      minutes: value.minutes,
    };
  }
  const date = item.scheduledDate ?? item.dueDate;
  return date ? { item, date, precision: 'date', minutes: null } : null;
}

/** Projects only Planner Items that have a calendar placement. */
export function getCalendarPlannerItems(items: PlannerItem[]) {
  return items
    .map((item) => getPlannerItemTimeProjection(item))
    .filter((item): item is PlannerItemTimeProjection => item !== null);
}

function stablePlannerSort(items: PlannerItem[], timezone = getBrowserTimezone()) {
  return items
    .map((item, index) => ({
      item,
      index,
      projection: getPlannerItemTimeProjection(item, timezone),
    }))
    .sort((left, right) => {
      const dateOrder = (left.projection?.date ?? '9999-99-99').localeCompare(
        right.projection?.date ?? '9999-99-99',
      );
      const timeOrder =
        (left.projection?.minutes ?? Number.MAX_SAFE_INTEGER) -
        (right.projection?.minutes ?? Number.MAX_SAFE_INTEGER);
      return dateOrder || timeOrder || left.index - right.index;
    })
    .map(({ item }) => item);
}

/**
 * Selects every Planner Item belonging to one Calendar date by its scheduling
 * anchors. Status is deliberately preserved: Calendar-day membership answers
 * where work belongs, while `completedAt` answers when completion happened.
 */
export function getCalendarPlannerItemsForDate(
  items: PlannerItem[],
  date: Date,
  timezone = getBrowserTimezone(),
) {
  const selectedKey = dateKey(date);
  return stablePlannerSort(
    items.filter((item) => getPlannerItemTimeProjection(item, timezone)?.date === selectedKey),
    timezone,
  );
}

/** Selects open Planner Items placed on one local calendar date. */
export function getPlannerItemsForDate(
  items: PlannerItem[],
  date: Date,
  timezone = getBrowserTimezone(),
) {
  return getCalendarPlannerItemsForDate(items, date, timezone).filter(
    (item) => item.status === 'OPEN',
  );
}

/**
 * Returns every scheduling anchor used to present an already-current item.
 * Current Open membership uses OR semantics across all four backend fields, so
 * grouping must inspect every field rather than reuse Calendar's single-anchor
 * placement precedence.
 */
function getPlannerItemAnchorDates(item: PlannerItem, timezone: string) {
  const exactDates = [item.dueAt, item.scheduledAt]
    .filter((value): value is string => Boolean(value))
    .map((value) => instantToLocalDateKey(value, timezone));
  const localDates = [item.dueDate, item.scheduledDate].filter((value): value is string =>
    Boolean(value),
  );
  return [...exactDates, ...localDates];
}

function stableCurrentGroupSort(items: PlannerItem[], timezone: string) {
  return items
    .map((item, index) => ({
      item,
      index,
      firstDate: [...getPlannerItemAnchorDates(item, timezone)].sort()[0] ?? '9999-99-99',
    }))
    .sort(
      (left, right) => left.firstDate.localeCompare(right.firstDate) || left.index - right.index,
    )
    .map(({ item }) => item);
}

/**
 * Groups an existing Current Open projection for display.
 *
 * Membership belongs to the backend. The frontend only distinguishes past-due,
 * today, and no-date rows after receiving `currentOpenItems`. Date-only values
 * are compared as YYYY-MM-DD strings and are never converted to UTC midnight;
 * exact timestamps are resolved in the same IANA timezone used by Planner View.
 */
export function groupCurrentOpenPlannerItems(
  currentOpenItems: PlannerItem[],
  today: string,
  timezone: string,
): CurrentOpenPlannerItemGroups {
  const pastDue: PlannerItem[] = [];
  const todayItems: PlannerItem[] = [];
  const noDate: PlannerItem[] = [];

  currentOpenItems.forEach((item) => {
    const anchorDates = getPlannerItemAnchorDates(item, timezone);
    if (anchorDates.length === 0) noDate.push(item);
    else if (anchorDates.some((date) => date < today)) pastDue.push(item);
    else if (anchorDates.some((date) => date === today)) todayItems.push(item);
  });

  return {
    pastDue: stableCurrentGroupSort(pastDue, timezone),
    today: stableCurrentGroupSort(todayItems, timezone),
    noDate: stableCurrentGroupSort(noDate, timezone),
  };
}

/**
 * Filters backend completion-activity membership by local completion date.
 * This is intentionally different from Calendar-day membership: a task may
 * belong to Sep 3 by schedule and appear here on Sep 16 by `completedAt`.
 */
export function getCompletedPlannerItemsForDate(
  completedItems: PlannerItem[],
  date: Date | string,
  timezone: string,
) {
  const selectedKey = typeof date === 'string' ? date : dateKey(date);
  return completedItems.filter(
    (item) =>
      item.status === 'COMPLETED' &&
      Boolean(item.completedAt) &&
      instantToLocalDateKey(item.completedAt as string, timezone) === selectedKey,
  );
}

/** Formats a Planner Item's projected calendar placement for compact rows. */
export function formatPlannerItemTime(
  item: PlannerItem,
  today: Date | string = getLocalTodayKey(),
  timezone = getBrowserTimezone(),
) {
  const projection = getPlannerItemTimeProjection(item, timezone);
  if (!projection) return 'No date';
  const todayKey = typeof today === 'string' ? today : dateKey(today);
  const day =
    projection.date === todayKey
      ? 'Today'
      : new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric' }).format(
          dateOnlyToLocal(projection.date),
        );
  if (projection.precision === 'date' || projection.minutes === null) return day;
  return `${day} ${formatTime(projection.minutes)}`;
}

/** Formats local minutes after midnight, or an all-day label for null. */
export function formatTime(minutes: number | null) {
  if (minutes === null) return 'All day';
  const date = new Date(2026, 0, 1, Math.floor(minutes / 60), minutes % 60);
  return new Intl.DateTimeFormat('en-US', {
    hour: 'numeric',
    minute: minutes % 60 ? '2-digit' : undefined,
  }).format(date);
}

/** Formats a duration in compact minutes or hours. */
export function formatDuration(minutes: number) {
  if (minutes < 60) return `${minutes} min`;
  const hours = minutes / 60;
  return `${Number.isInteger(hours) ? hours : hours.toFixed(1)} hr`;
}

/** Converts a backend authority code into a restrained UI source label. */
export function formatPlannerAuthority(authority: PlannerAuthority) {
  return authority === 'LOCAL' ? 'Lum Harbor' : 'External source';
}

/**
 * Human-readable provenance of a Planner Item. EXTERNAL items use the
 * backend-resolved origin; without one they fall back to the generic label
 * rather than guessing a system from the item type.
 */
export function formatPlannerItemSource(
  item: Pick<PlannerItem, 'authority' | 'externalOrigin'>,
): { source: string; provider: string | null } {
  const origin = item.authority === 'EXTERNAL' ? item.externalOrigin : null;
  if (!origin) return { source: formatPlannerAuthority(item.authority), provider: null };
  const provider =
    origin.providerName && origin.extensionId
      ? `${origin.providerName} · ${origin.extensionId}`
      : (origin.providerName ?? origin.extensionId);
  return { source: origin.systemLabel, provider: provider ?? null };
}
