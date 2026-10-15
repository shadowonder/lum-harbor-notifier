/**
 * Dashboard — Active Workflows Roadmap (frontend product surface, Experiment 1.1).
 *
 * Everything in this file is pure presentation
 * logic: date-window math, adaptive timeline geometry, and the small
 * resolver that turns a raw (start, current target, previous target) tuple
 * into one of the milestone/span/unscheduled display modes. It has no
 * dependency on the real Dashboard backend projection, no dependency on
 * the WorkItem Dependency Explorer/Workflow Stage renderer, and no
 * dependency on any generated Wails/backend type.
 *
 * Row data comes either from the production `roadmapAuthoritativeAdapter.ts`
 * or the isolated-test `roadmapPrototypeAdapter.ts`. This module and the
 * renderer built on it never know or care which adapter supplied a row.
 *
 * All date arithmetic mirrors the existing local-calendar-date convention
 * established in `src/pages/plannerData.ts` (`YYYY-MM-DD` keys, parsed at
 * local noon to stay stable across daylight-saving transitions) rather than
 * introducing a second date-handling scheme.
 */

function parseLocalDateKey(key: string): Date {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(key);
  if (!match) throw new Error(`Invalid local calendar date: ${key}`);
  const [, year, month, day] = match;
  return new Date(Number(year), Number(month) - 1, Number(day), 12);
}

/** Advances a YYYY-MM-DD value by local calendar days (see plannerData.ts's identical convention). */
export function addRoadmapDays(localDate: string, amount: number): string {
  const date = parseLocalDateKey(localDate);
  date.setDate(date.getDate() + amount);
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  return `${year}-${month}-${day}`;
}

/** Whole calendar days from `fromKey` to `toKey` (positive when `toKey` is later). */
export function daysBetweenLocalDates(fromKey: string, toKey: string): number {
  const from = parseLocalDateKey(fromKey);
  const to = parseLocalDateKey(toKey);
  return Math.round((to.getTime() - from.getTime()) / (24 * 60 * 60 * 1000));
}

/** Compact "Sep 21" formatting for a local date key. */
export function formatRoadmapDateLabel(dateKey: string): string {
  const [year, month, day] = dateKey.split('-').map(Number);
  return new Intl.DateTimeFormat('en-US', { month: 'short', day: 'numeric' }).format(
    new Date(year!, month! - 1, day!, 12),
  );
}

export interface RoadmapWindow {
  startDate: string;
  endDate: string;
}

/** Today is always visible without padding-only defaults shrinking to exclude it. */
export const DEFAULT_PAST_PADDING_DAYS = 7;
export const DEFAULT_FUTURE_PADDING_DAYS = 60;
const TARGET_DATE_EDGE_PADDING_DAYS = 3;

/**
 * Deterministic V1 visible window: Today plus a sensible default look-back/
 * look-ahead, widened (never narrowed) to keep every real timeline date
 * visible with a little edge padding. The window only ever grows to fit
 * outliers — it never shrinks below the default span that already contains
 * Today, so Today is always visible by construction.
 */
export function computeRoadmapWindow(
  todayKey: string,
  timelineDateKeys: ReadonlyArray<string | null | undefined>,
): RoadmapWindow {
  let startDate = addRoadmapDays(todayKey, -DEFAULT_PAST_PADDING_DAYS);
  let endDate = addRoadmapDays(todayKey, DEFAULT_FUTURE_PADDING_DAYS);

  for (const key of timelineDateKeys) {
    if (!key) continue;
    const candidateStart = addRoadmapDays(key, -TARGET_DATE_EDGE_PADDING_DAYS);
    const candidateEnd = addRoadmapDays(key, TARGET_DATE_EDGE_PADDING_DAYS);
    if (candidateStart < startDate) startDate = candidateStart;
    if (candidateEnd > endDate) endDate = candidateEnd;
  }

  return { startDate, endDate };
}

/**
 * Adaptive timeline scale (Experiment 1.1). Pixels-per-day is no longer a
 * single fixed constant: it adapts to the roadmap viewport's actually
 * measured available width so a normal date span fills the panel instead
 * of leaving a large dead whitespace gutter. This is a *timeline scale*
 * change only — never a `transform: scale()` on rendered content, never a
 * font-size change, and never a shrink of the identity column. It mirrors
 * the Dependency Explorer's `computeStageColumnWidth` pattern: adapt
 * within [min, max], and once the minimum is reached, stop shrinking and
 * let content overflow (native horizontal scroll) instead of compressing
 * further into illegibility.
 */
export const ROADMAP_MIN_PIXELS_PER_DAY = 4;
export const ROADMAP_MAX_PIXELS_PER_DAY = 22;
/** Fallback used only before the viewport has been measured once. */
export const ROADMAP_DEFAULT_PIXELS_PER_DAY = 8;

/**
 * Picks one pixels-per-day value for the whole timeline, adapting to the
 * available (viewport minus sticky identity column) width without ever
 * upscaling past `max` or downscaling past `min`. A short visible span in
 * a wide panel fills the available width; a very wide span (an outlier
 * date far in the future) freezes at `min` and is expected to overflow
 * horizontally instead of compressing further.
 */
export function computeAdaptivePixelsPerDay(
  visibleSpanDays: number,
  availableTimelineWidthPx: number,
  options: { min?: number; max?: number } = {},
): number {
  const { min = ROADMAP_MIN_PIXELS_PER_DAY, max = ROADMAP_MAX_PIXELS_PER_DAY } = options;
  if (
    visibleSpanDays <= 0 ||
    !Number.isFinite(availableTimelineWidthPx) ||
    availableTimelineWidthPx <= 0
  ) {
    return ROADMAP_DEFAULT_PIXELS_PER_DAY;
  }
  const idealPxPerDay = availableTimelineWidthPx / visibleSpanDays;
  return Math.min(max, Math.max(min, idealPxPerDay));
}

/** X offset (in days from the window start) for a given date — multiply by
 * the current pixels-per-day value for a pixel position. */
export function computeDayOffset(dateKey: string, windowStartKey: string): number {
  return daysBetweenLocalDates(windowStartKey, dateKey);
}

/** Total pixel width of the timeline area for a given window at a given scale. */
export function computeTimelineWidth(window: RoadmapWindow, pixelsPerDay: number): number {
  return daysBetweenLocalDates(window.startDate, window.endDate) * pixelsPerDay;
}

const MONTH_LABELS = [
  'JAN',
  'FEB',
  'MAR',
  'APR',
  'MAY',
  'JUN',
  'JUL',
  'AUG',
  'SEP',
  'OCT',
  'NOV',
  'DEC',
] as const;

function chooseTickIntervalDays(spanDays: number): number {
  if (spanDays <= 45) return 7;
  if (spanDays <= 120) return 14;
  return 30;
}

export interface RoadmapTick {
  dateKey: string;
  dayLabel: string;
}

export interface RoadmapMonthGroup {
  label: string;
  ticks: RoadmapTick[];
}

/**
 * Groups timeline header ticks by month (`SEP` / `15 22 29`, then `OCT` /
 * `6 13 20 27`, matching the brief's own sketch) at a granularity chosen
 * from the visible span so a long window never renders one column per day.
 */
export function computeTimelineTickGroups(window: RoadmapWindow): RoadmapMonthGroup[] {
  const spanDays = daysBetweenLocalDates(window.startDate, window.endDate);
  const interval = chooseTickIntervalDays(spanDays);
  const groups: RoadmapMonthGroup[] = [];

  let cursor = window.startDate;
  let guard = 0;
  while (cursor <= window.endDate && guard < 500) {
    guard += 1;
    const [, monthNumber, day] = cursor.split('-').map(Number);
    const monthLabel = MONTH_LABELS[monthNumber! - 1]!;
    const tick: RoadmapTick = { dateKey: cursor, dayLabel: String(day) };
    const last = groups[groups.length - 1];
    if (last && last.label === monthLabel) {
      last.ticks.push(tick);
    } else {
      groups.push({ label: monthLabel, ticks: [tick] });
    }
    cursor = addRoadmapDays(cursor, interval);
  }

  return groups;
}

export type RoadmapDateDescription =
  | { state: 'unscheduled' }
  | { state: 'overdue'; overdueDays: number }
  | { state: 'due-today' }
  | { state: 'upcoming'; remainingDays: number };

/**
 * Pure date-derived description — never a fabricated health/risk signal.
 * "Overdue"/"remaining" are direct arithmetic on real dates, nothing more.
 */
export function describeTargetDate(
  targetDateKey: string | null,
  todayKey: string,
): RoadmapDateDescription {
  if (!targetDateKey) return { state: 'unscheduled' };
  const diff = daysBetweenLocalDates(todayKey, targetDateKey);
  if (diff < 0) return { state: 'overdue', overdueDays: -diff };
  if (diff === 0) return { state: 'due-today' };
  return { state: 'upcoming', remainingDays: diff };
}

export function formatDateDescriptionLabel(description: RoadmapDateDescription): string {
  switch (description.state) {
    case 'unscheduled':
      return 'No target date';
    case 'overdue':
      return `Overdue ${description.overdueDays}d`;
    case 'due-today':
      return 'Due today';
    case 'upcoming':
      return `${description.remainingDays} day${description.remainingDays === 1 ? '' : 's'} remaining`;
  }
}

/**
 * The presentation modes a Roadmap row's timeline lane can render —
 * deliberately NOT a WorkItem domain enum. Whatever authoritative source
 * eventually supplies real dates (a future `WorkItem.start_at`, a
 * `MonitorPoint` FINAL_TARGET/DEADLINE, or anything else), that source's
 * adapter only needs to resolve into one of these shapes; the renderer
 * never needs to know where the dates came from.
 *
 * `span` is the PRIMARY mode for a real, actively-tracked Active Workflow:
 * a long-running WorkItem is a time span from a start date to its current
 * expected completion, not a single deadline point. `previousTargetDateKey`
 * is optional (V1 supports only "current + one prior" — not a full
 * revision-history framework) and is only meaningful when it differs from
 * `currentTargetDateKey`.
 */
export type RoadmapTimelineDescriptor =
  | { kind: 'unscheduled' }
  | { kind: 'milestone'; targetDateKey: string }
  | {
      kind: 'span';
      startDateKey: string;
      currentTargetDateKey: string;
      previousTargetDateKey?: string;
    };

/**
 * Resolves a raw (start, current target, previous target) tuple into a
 * presentation mode. A span requires BOTH a real start and a real current
 * target date — never fabricated from one alone. A lone target date
 * renders as a milestone marker, never a fake span. No target date at all
 * is an honest "unscheduled" row, not a filtered-out one. A start date
 * with no target has no meaningful endpoint and also resolves to
 * unscheduled rather than inventing one. A previous target equal to the
 * current target is not a revision and is dropped rather than displayed.
 */
export function resolveRoadmapRowTimelineMode(
  startDateKey: string | null,
  currentTargetDateKey: string | null,
  previousTargetDateKey?: string | null,
): RoadmapTimelineDescriptor {
  if (currentTargetDateKey && startDateKey) {
    const hasRevision = !!previousTargetDateKey && previousTargetDateKey !== currentTargetDateKey;
    return {
      kind: 'span',
      startDateKey,
      currentTargetDateKey,
      ...(hasRevision ? { previousTargetDateKey: previousTargetDateKey! } : {}),
    };
  }
  if (currentTargetDateKey) return { kind: 'milestone', targetDateKey: currentTargetDateKey };
  return { kind: 'unscheduled' };
}

/**
 * A Roadmap row's presentation shape. Deliberately not
 * `{ workItemId, title, targetDate }` — the flat-`targetDate` shape is
 * exactly the premature backend lock-in this round avoids (a real
 * authoritative deadline will very likely come from a future MonitorPoint
 * projection, not a `WorkItem.targetDate` column). The `timeline`
 * descriptor is the only thing the renderer consumes. `description` is
 * optional, reused verbatim from the WorkItem's own prototype description
 * for the hover summary — never a second, independently-authored blurb.
 */
export interface ActiveWorkflowRoadmapRow {
  workItemId: string;
  title: string;
  description?: string | null;
  timeline: RoadmapTimelineDescriptor;
}

/** Positive = the target moved later; negative = it moved earlier. */
export function computeScheduleRevisionDeltaDays(
  previousTargetDateKey: string,
  currentTargetDateKey: string,
): number {
  return daysBetweenLocalDates(previousTargetDateKey, currentTargetDateKey);
}

/** "Moved +9 days" / "Moved earlier by 3 days" — pure date arithmetic, no health language. */
export function formatScheduleRevisionLabel(deltaDays: number): string {
  if (deltaDays === 0) return 'Target unchanged';
  if (deltaDays > 0) return `Moved +${deltaDays} day${deltaDays === 1 ? '' : 's'}`;
  const earlier = -deltaDays;
  return `Moved earlier by ${earlier} day${earlier === 1 ? '' : 's'}`;
}

/**
 * The compact, read-only hover summary view model for a real workflow row.
 * Deliberately excludes Stage, item counts, dependencies, and health — see
 * the module-level ban on health language in `describeTargetDate`.
 */
export interface RoadmapHoverSummary {
  title: string;
  description: string | null;
  startLabel: string | null;
  currentTargetLabel: string | null;
  previousTargetLabel: string | null;
  scheduleRevisionLabel: string | null;
  dateStateLabel: string | null;
}

export function buildRoadmapHoverSummary(
  row: ActiveWorkflowRoadmapRow,
  todayKey: string,
): RoadmapHoverSummary {
  const base: RoadmapHoverSummary = {
    title: row.title,
    description: row.description ?? null,
    startLabel: null,
    currentTargetLabel: null,
    previousTargetLabel: null,
    scheduleRevisionLabel: null,
    dateStateLabel: null,
  };

  if (row.timeline.kind === 'unscheduled') return base;

  if (row.timeline.kind === 'milestone') {
    return {
      ...base,
      currentTargetLabel: formatRoadmapDateLabel(row.timeline.targetDateKey),
      dateStateLabel: formatDateDescriptionLabel(
        describeTargetDate(row.timeline.targetDateKey, todayKey),
      ),
    };
  }

  const { startDateKey, currentTargetDateKey, previousTargetDateKey } = row.timeline;
  return {
    ...base,
    startLabel: formatRoadmapDateLabel(startDateKey),
    currentTargetLabel: formatRoadmapDateLabel(currentTargetDateKey),
    previousTargetLabel: previousTargetDateKey
      ? formatRoadmapDateLabel(previousTargetDateKey)
      : null,
    scheduleRevisionLabel: previousTargetDateKey
      ? formatScheduleRevisionLabel(
          computeScheduleRevisionDeltaDays(previousTargetDateKey, currentTargetDateKey),
        )
      : null,
    dateStateLabel: formatDateDescriptionLabel(describeTargetDate(currentTargetDateKey, todayKey)),
  };
}

/** Bounded panel viewport policy: the panel never grows the Dashboard page
 * height with the number of active workflows — it scrolls internally. */
export const ROADMAP_PANEL_MIN_HEIGHT = 220;
export const ROADMAP_PANEL_MAX_HEIGHT = 420;
export const ROADMAP_ROW_HEIGHT = 50;
export const ROADMAP_IDENTITY_COLUMN_WIDTH = 220;

/** Every timeline date a row can contribute to the visible window (used to size it). */
export function timelineDateKeys(timeline: RoadmapTimelineDescriptor): string[] {
  switch (timeline.kind) {
    case 'unscheduled':
      return [];
    case 'milestone':
      return [timeline.targetDateKey];
    case 'span':
      return [
        timeline.startDateKey,
        timeline.currentTargetDateKey,
        ...(timeline.previousTargetDateKey ? [timeline.previousTargetDateKey] : []),
      ];
  }
}
