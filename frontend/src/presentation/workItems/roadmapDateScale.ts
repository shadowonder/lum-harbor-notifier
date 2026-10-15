import type { RoadmapRange } from './roadmapViewModels';

const dayMs = 24 * 60 * 60 * 1000;

export interface RoadmapDateRange {
  start: number;
  end: number;
  today: number;
  totalDays: number;
  timelineWidth: number;
}

export interface RoadmapBarLayout {
  plannedStartX: number;
  plannedEndX: number;
  progressStartX: number;
  progressEndX: number;
  targetX: number;
  targetVisible: boolean;
  forecastX?: number;
  forecastVisible: boolean;
  continuesLeft: boolean;
  continuesRight: boolean;
  forecastLate: boolean;
}

export function toUtcDay(value: string) {
  const datePart = value.slice(0, 10);
  return Date.parse(`${datePart}T00:00:00Z`);
}

export function formatRoadmapDate(value: string | number, includeYear = false) {
  return new Intl.DateTimeFormat('en-US', {
    month: 'short', day: 'numeric', ...(includeYear ? { year: 'numeric' } : {}), timeZone: 'UTC',
  }).format(new Date(typeof value === 'number' ? value : toUtcDay(value)));
}

export function differenceInRoadmapDays(from: string, to: string) {
  return Math.round((toUtcDay(to) - toUtcDay(from)) / dayMs);
}

export function createRoadmapDateRange(asOf: string, range: RoadmapRange): RoadmapDateRange {
  const today = toUtcDay(asOf);
  const settings = {
    '2W': { before: 4, days: 14, width: 840 },
    '4W': { before: 8, days: 28, width: 1120 },
    '3M': { before: 27, days: 91, width: 1680 },
  }[range];
  return {
    start: today - settings.before * dayMs,
    end: today + (settings.days - settings.before) * dayMs,
    today,
    totalDays: settings.days,
    timelineWidth: settings.width,
  };
}

export function dateToX(value: string | number, range: RoadmapDateRange) {
  const timestamp = typeof value === 'number' ? value : toUtcDay(value);
  return ((timestamp - range.start) / (range.end - range.start)) * range.timelineWidth;
}

export function createRoadmapTicks(range: RoadmapDateRange, zoom: RoadmapRange) {
  const intervalDays = zoom === '2W' ? 2 : 7;
  const firstDay = new Date(range.start);
  const daysUntilMonday = (8 - firstDay.getUTCDay()) % 7;
  const first = zoom === '2W' ? range.start : range.start + daysUntilMonday * dayMs;
  const ticks: number[] = [];
  for (let value = first; value <= range.end; value += intervalDays * dayMs) ticks.push(value);
  return ticks;
}

export function layoutRoadmapBar(
  startAt: string,
  targetAt: string,
  forecastAt: string | undefined,
  progressRatio: number,
  range: RoadmapDateRange,
): RoadmapBarLayout {
  const rawStartX = dateToX(startAt, range);
  const rawTargetX = dateToX(targetAt, range);
  const targetX = Math.min(range.timelineWidth, Math.max(0, rawTargetX));
  const plannedStartX = Math.min(range.timelineWidth, Math.max(0, rawStartX));
  const plannedEndX = targetX;
  const rawProgressEndX = rawStartX + (rawTargetX - rawStartX) * Math.min(1, Math.max(0, progressRatio));
  const forecastLate = Boolean(forecastAt && toUtcDay(forecastAt) > toUtcDay(targetAt));
  return {
    plannedStartX,
    plannedEndX,
    progressStartX: plannedStartX,
    progressEndX: Math.min(plannedEndX, Math.max(plannedStartX, rawProgressEndX)),
    targetX,
    targetVisible: rawTargetX >= 0 && rawTargetX <= range.timelineWidth,
    forecastX: forecastLate && forecastAt ? Math.min(range.timelineWidth, Math.max(0, dateToX(forecastAt, range))) : undefined,
    forecastVisible: Boolean(forecastLate && forecastAt && dateToX(forecastAt, range) >= 0 && dateToX(forecastAt, range) <= range.timelineWidth),
    continuesLeft: rawStartX < 0,
    continuesRight: rawTargetX > range.timelineWidth,
    forecastLate,
  };
}
