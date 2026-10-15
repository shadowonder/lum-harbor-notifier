import type {
  CenterNotification,
  NotificationSeverity,
} from '../api/notifications/notificationsProductApi.ts';

/**
 * Pure presentation helpers over the authoritative Notification Center.
 * Disposition, read state and snooze come from the backend; nothing here
 * decides whether an issue needs action, is snoozed or is resolved. The only
 * time-driven behavior is choosing WHEN to reload (see scheduleExpiryReload).
 */
export type NotificationFilter = 'all' | 'needsAction' | 'unread' | 'snoozed' | 'resolved';

export const FILTER_LABELS: Record<NotificationFilter, string> = {
  all: 'All',
  needsAction: 'Needs Action',
  unread: 'Unread',
  snoozed: 'Snoozed',
  resolved: 'Resolved',
};

export function matchesFilter(item: CenterNotification, filter: NotificationFilter): boolean {
  switch (filter) {
    case 'needsAction':
      return item.disposition === 'NEEDS_ACTION';
    case 'unread':
      return item.readAt === null;
    case 'snoozed':
      return item.disposition === 'SNOOZED';
    case 'resolved':
      return item.disposition === 'RESOLVED';
    default:
      return true;
  }
}

export function matchesSearch(item: CenterNotification, search: string): boolean {
  const query = search.trim().toLocaleLowerCase();
  if (!query) return true;
  return [item.title, item.message, item.workItemTitle]
    .filter((value): value is string => Boolean(value))
    .some((value) => value.toLocaleLowerCase().includes(query));
}

export function countByFilter(items: readonly CenterNotification[]) {
  return {
    unread: items.filter((i) => matchesFilter(i, 'unread')).length,
    needsAction: items.filter((i) => matchesFilter(i, 'needsAction')).length,
    snoozed: items.filter((i) => matchesFilter(i, 'snoozed')).length,
  };
}

const startOfLocalDay = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();

/** Display grouping by local calendar day of delivery. */
export function splitByDay(items: readonly CenterNotification[], now: Date) {
  const today = startOfLocalDay(now);
  return {
    today: items.filter((i) => startOfLocalDay(new Date(i.sentAt)) === today),
    earlier: items.filter((i) => startOfLocalDay(new Date(i.sentAt)) !== today),
  };
}

export type SeverityStyleKey = 'high' | 'attention' | 'info';

/** Visual mapping only; severity is never recomputed. */
export function severityStyleKey(severity: NotificationSeverity): SeverityStyleKey {
  switch (severity) {
    case 'CRITICAL':
      return 'high';
    case 'WARNING':
      return 'attention';
    default:
      return 'info';
  }
}

export function formatRelativeTime(iso: string, now: Date): string {
  const diffMs = now.getTime() - Date.parse(iso);
  const minutes = Math.floor(diffMs / 60000);
  if (minutes < 1) return 'Just now';
  if (minutes < 60) return `${minutes} min ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} ${hours === 1 ? 'hour' : 'hours'} ago`;
  return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
}

const clock = (iso: string) =>
  new Date(iso).toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: 'numeric',
    minute: '2-digit',
  });

export interface HistoryEntry {
  id: string;
  label: string;
  description: string;
}

/** Real timeline built only from persisted timestamps (newest first). */
export function buildHistoryEntries(item: CenterNotification): HistoryEntry[] {
  const entries: Array<HistoryEntry & { at: number }> = [
    { id: 'sent', at: Date.parse(item.sentAt), label: clock(item.sentAt), description: 'Notified' },
  ];
  if (item.readAt) {
    entries.push({
      id: 'read',
      at: Date.parse(item.readAt),
      label: clock(item.readAt),
      description: 'Marked read',
    });
  }
  if (item.snoozedUntil) {
    entries.push({
      id: 'snooze',
      at: Date.parse(item.snoozedUntil),
      label: clock(item.snoozedUntil),
      description: item.disposition === 'SNOOZED' ? 'Snoozed until' : 'Snooze ended',
    });
  }
  if (item.resolvedAt) {
    entries.push({
      id: 'resolved',
      at: Date.parse(item.resolvedAt),
      label: clock(item.resolvedAt),
      description: 'Resolved',
    });
  }
  return entries
    .sort((a, b) => b.at - a.at)
    .map(({ id, label, description }) => ({ id, label, description }));
}

export interface SnoozeOption {
  label: string;
  until: Date;
}

/**
 * Product-friendly snooze durations converted to an absolute instant. The
 * backend validates it against its own clock.
 */
export function snoozeOptions(now: Date): SnoozeOption[] {
  const options: SnoozeOption[] = [
    { label: '1 hour', until: new Date(now.getTime() + 60 * 60 * 1000) },
  ];
  const afternoon = new Date(now.getFullYear(), now.getMonth(), now.getDate(), 15, 0, 0, 0);
  if (afternoon.getTime() > now.getTime() + 60 * 1000) {
    options.push({ label: 'This afternoon', until: afternoon });
  }
  options.push({
    label: 'Tomorrow morning',
    until: new Date(now.getFullYear(), now.getMonth(), now.getDate() + 1, 9, 0, 0, 0),
  });
  return options;
}

const MAX_TIMER_MS = 2_147_483_647;
const EXPIRY_GRACE_MS = 1000;

/**
 * Delay until the authoritative reload that lets an expired snooze become
 * "needs action" again, or null when nothing is snoozed. It only chooses a
 * reload time from `snoozedUntil`; the resulting disposition still comes from
 * the backend.
 */
export function computeExpiryReloadDelay(
  items: readonly CenterNotification[],
  nowMs: number,
): number | null {
  const expiries = items
    .filter((i) => i.disposition === 'SNOOZED' && i.snoozedUntil)
    .map((i) => Date.parse(i.snoozedUntil as string));
  if (expiries.length === 0) return null;
  const earliest = Math.min(...expiries);
  return Math.min(Math.max(earliest - nowMs, 0) + EXPIRY_GRACE_MS, MAX_TIMER_MS);
}

export interface TimerApi {
  set: (callback: () => void, ms: number) => unknown;
  clear: (handle: unknown) => void;
}

/**
 * Schedules ONE authoritative reload at the earliest snooze expiry and returns
 * its cancel function. No polling, no local state flip.
 */
export function scheduleExpiryReload(
  items: readonly CenterNotification[],
  nowMs: number,
  reload: () => void,
  timers: TimerApi,
): () => void {
  const delay = computeExpiryReloadDelay(items, nowMs);
  if (delay === null) return () => undefined;
  const handle = timers.set(reload, delay);
  return () => timers.clear(handle);
}
