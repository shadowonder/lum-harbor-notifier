import assert from 'node:assert/strict';
import { existsSync, readFileSync } from 'node:fs';
import test from 'node:test';
import type { app } from '../wailsjs/go/models';
import {
  mapCenterNotification,
  mapNotificationCenter,
} from '../src/api/notifications/notificationDtoAdapter.ts';
import type { CenterNotification } from '../src/api/notifications/notificationsProductApi.ts';
import {
  buildHistoryEntries,
  computeExpiryReloadDelay,
  countByFilter,
  formatRelativeTime,
  matchesFilter,
  matchesSearch,
  scheduleExpiryReload,
  severityStyleKey,
  snoozeOptions,
  splitByDay,
} from '../src/notifications/notificationCenterModel.ts';
import { runTrackingMutation } from '../src/tracking/trackingMutation.ts';

const source = (path: string) => readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8');

const dto = (over: Record<string, unknown> = {}) =>
  ({
    notificationId: 'n-1',
    attentionIssueId: 'a-1',
    workItemId: 'wi-1',
    workItemTitle: 'Release',
    subjectTrackedItemId: null,
    title: 'Ship it',
    message: '“Ship it” was due Oct 15. The Work Item is still ACTIVE; expected COMPLETED.',
    severity: 'WARNING',
    issueType: 'MONITOR_OVERDUE_UNMET',
    recommendedAction: 'Bring the Work Item to the expected state.',
    deliveryState: 'SENT',
    sentAt: '2026-10-16T00:00:00Z',
    readAt: null,
    dismissedAt: null,
    attentionState: 'OPEN',
    snoozedUntil: null,
    resolvedAt: null,
    disposition: 'NEEDS_ACTION',
    createdAt: '2026-10-16T00:00:00Z',
    updatedAt: '2026-10-16T00:00:00Z',
    reason: {
      version: 1,
      monitorPointId: 'm-1',
      monitorKind: 'FINAL_TARGET',
      monitorTitle: 'Ship it',
      dueAt: '2026-10-15T22:00:00Z',
      timezone: 'UTC',
      subjectKind: 'WORK_ITEM',
      subjectId: 'wi-1',
      subjectTitle: null,
      expectedKind: 'WORK_ITEM_LIFECYCLE',
      expectedWorkItemLifecycle: 'COMPLETED',
      expectedStepId: null,
      expectedStepTitle: null,
      actualWorkItemLifecycle: 'ACTIVE',
      actualStepState: null,
    },
    ...over,
  }) as unknown as app.NotificationCenterItemDTO;

const item = (over: Record<string, unknown> = {}): CenterNotification =>
  mapCenterNotification(dto(over));

test('the adapter maps a delivered notification with explicit nullables and validated enums', () => {
  const n = item();
  assert.equal(n.workItemId, 'wi-1');
  assert.equal(n.readAt, null);
  assert.equal(n.snoozedUntil, null);
  assert.equal(n.disposition, 'NEEDS_ACTION');
  assert.equal(n.reason.expectedWorkItemLifecycle, 'COMPLETED');
  assert.throws(() => item({ severity: 'URGENT' }));
  assert.throws(() => item({ disposition: 'DISMISSED' }));
  assert.throws(() => item({ attentionState: 'SNOOZED' }));
  assert.throws(() => item({ sentAt: 'soon' }));
});

test('PENDING and CANCELLED can never enter the product model', () => {
  for (const deliveryState of ['PENDING', 'CANCELLED']) {
    assert.throws(() => item({ deliveryState }), /notification center received/i);
  }
  assert.equal(
    mapNotificationCenter({
      at: '2026-10-16T00:00:00Z',
      items: [dto()],
    } as unknown as app.NotificationCenterDTO).items.length,
    1,
  );
});

test('a valid empty center maps to an empty collection (not an error)', () => {
  const center = mapNotificationCenter({
    at: '2026-10-16T00:00:00Z',
    items: [],
  } as unknown as app.NotificationCenterDTO);
  assert.deepEqual(center.items, []);
  const nullItems = mapNotificationCenter({
    at: '2026-10-16T00:00:00Z',
    items: null,
  } as unknown as app.NotificationCenterDTO);
  assert.deepEqual(nullItems.items, []);
});

test('the five tabs use the backend disposition, read state and never re-derive Attention', () => {
  const needs = item({ notificationId: 'needs', disposition: 'NEEDS_ACTION' });
  const read = item({
    notificationId: 'read',
    disposition: 'NEEDS_ACTION',
    readAt: '2026-10-16T00:30:00Z',
  });
  const snoozed = item({
    notificationId: 'snoozed',
    disposition: 'SNOOZED',
    snoozedUntil: '2026-10-16T05:00:00Z',
  });
  const resolved = item({
    notificationId: 'resolved',
    disposition: 'RESOLVED',
    attentionState: 'RESOLVED',
    resolvedAt: '2026-10-16T01:00:00Z',
    readAt: '2026-10-16T00:40:00Z',
  });
  const all = [needs, read, snoozed, resolved];
  const ids = (filter: Parameters<typeof matchesFilter>[1]) =>
    all.filter((i) => matchesFilter(i, filter)).map((i) => i.notificationId);
  assert.deepEqual(ids('all'), ['needs', 'read', 'snoozed', 'resolved']);
  assert.deepEqual(ids('needsAction'), ['needs', 'read']);
  assert.deepEqual(ids('unread'), ['needs', 'snoozed']);
  assert.deepEqual(ids('snoozed'), ['snoozed']);
  assert.deepEqual(
    ids('resolved'),
    ['resolved'],
    'Resolved keeps the communication; it stays in All',
  );
  assert.deepEqual(countByFilter(all), { unread: 2, needsAction: 2, snoozed: 1 });
  // Read is not resolve: a read row is still Needs Action; a resolved row keeps its read state.
  assert.equal(matchesFilter(read, 'resolved'), false);
  assert.equal(resolved.readAt, '2026-10-16T00:40:00Z');
});

test('the frontend decides no urgency: filtering ignores time, age and lifecycle', () => {
  const model = source('notifications/notificationCenterModel.ts');
  const filter = model.slice(
    model.indexOf('export function matchesFilter'),
    model.indexOf('export function matchesSearch'),
  );
  assert.doesNotMatch(filter, /snoozedUntil|Date\.|now|sentAt|resolvedAt/);
  for (const file of ['pages/Notifications.tsx', 'components/notifications/NotificationRow.tsx']) {
    assert.doesNotMatch(
      source(file),
      /Date\.now\(\)\s*[<>]|snoozedUntil\)?\s*[<>]|Date\.parse\([^)]*snoozedUntil/,
      file,
    );
  }
});

test('search matches title, message and Work Item title only', () => {
  const n = item();
  assert.equal(matchesSearch(n, 'release'), true);
  assert.equal(matchesSearch(n, 'expected completed'), true);
  assert.equal(matchesSearch(n, 'nope'), false);
  assert.equal(matchesSearch(n, '   '), true);
});

test('snooze options are absolute future instants; This afternoon disappears after 3 PM', () => {
  const morning = new Date(2026, 9, 16, 10, 0, 0);
  const options = snoozeOptions(morning);
  assert.deepEqual(
    options.map((o) => o.label),
    ['1 hour', 'This afternoon', 'Tomorrow morning'],
  );
  assert.equal(options[0].until.getTime(), morning.getTime() + 3_600_000);
  assert.equal(options[1].until.getHours(), 15);
  assert.equal(options[2].until.getDate(), 17);
  assert.equal(options[2].until.getHours(), 9);
  for (const o of options) assert.ok(o.until.getTime() > morning.getTime());
  const evening = new Date(2026, 9, 16, 18, 0, 0);
  assert.deepEqual(
    snoozeOptions(evening).map((o) => o.label),
    ['1 hour', 'Tomorrow morning'],
  );
  assert.ok(
    !snoozeOptions(new Date(2026, 9, 16, 14, 59, 30)).some((o) => o.label === 'This afternoon'),
  );
});

test('long-open page: an authoritative reload is scheduled at the earliest snooze expiry, then only that', () => {
  const base = Date.parse('2026-10-16T00:00:00Z');
  const soon = item({
    notificationId: 'soon',
    disposition: 'SNOOZED',
    snoozedUntil: '2026-10-16T01:00:00Z',
  });
  const later = item({
    notificationId: 'later',
    disposition: 'SNOOZED',
    snoozedUntil: '2026-10-16T05:00:00Z',
  });
  const active = item({ notificationId: 'active', disposition: 'NEEDS_ACTION' });
  assert.equal(computeExpiryReloadDelay([active], base), null);
  assert.equal(computeExpiryReloadDelay([], base), null);
  assert.equal(computeExpiryReloadDelay([later, soon, active], base), 3_600_000 + 1000);
  assert.equal(
    computeExpiryReloadDelay([soon], Date.parse('2026-10-16T02:00:00Z')),
    1000,
    'an already-expired snooze reloads immediately',
  );
  const far = item({ disposition: 'SNOOZED', snoozedUntil: '2100-01-01T00:00:00Z' });
  assert.equal(
    computeExpiryReloadDelay([far], base),
    2_147_483_647,
    'the delay is capped to a safe timer value',
  );
});

test('the expiry timer is one-shot with fake timers, calls reload only, and is cancelled on cleanup', () => {
  const scheduled: Array<{ callback: () => void; ms: number; id: number }> = [];
  const cleared: number[] = [];
  const timers = {
    set: (callback: () => void, ms: number) => {
      const id = scheduled.length + 1;
      scheduled.push({ callback, ms, id });
      return id;
    },
    clear: (handle: unknown) => void cleared.push(handle as number),
  };
  let reloads = 0;
  const snoozed = item({ disposition: 'SNOOZED', snoozedUntil: '2026-10-16T01:00:00Z' });
  const base = Date.parse('2026-10-16T00:00:00Z');
  const cancel = scheduleExpiryReload([snoozed], base, () => void (reloads += 1), timers);
  assert.equal(scheduled.length, 1, 'exactly one timer, not a poll');
  assert.equal(scheduled[0].ms, 3_601_000);
  scheduled[0].callback(); // the timer fires
  assert.equal(reloads, 1, 'the timer only triggers one authoritative reload');
  cancel(); // unmount / data change
  assert.deepEqual(cleared, [1], 'cleanup clears the timer');
  // With nothing snoozed no timer exists and cancel is inert.
  const before = scheduled.length;
  const noop = scheduleExpiryReload([item()], base, () => undefined, timers);
  noop();
  assert.equal(scheduled.length, before);
  assert.equal(cleared.length, 1);
});

test('after the reload the backend result flips the row; the frontend never flips it itself', () => {
  // Backend result at reload time: same notification, now NEEDS_ACTION.
  const before = item({ disposition: 'SNOOZED', snoozedUntil: '2026-10-16T01:00:00Z' });
  const after = item({ disposition: 'NEEDS_ACTION', snoozedUntil: '2026-10-16T01:00:00Z' });
  assert.equal(matchesFilter(before, 'needsAction'), false);
  assert.equal(matchesFilter(after, 'needsAction'), true);
  assert.equal(
    computeExpiryReloadDelay([after], Date.parse('2026-10-16T01:00:00Z')),
    null,
    'no timer once it needs action again',
  );
  const hook = source('notifications/useNotificationCenter.ts');
  assert.match(hook, /scheduleExpiryReload\(view\.items, Date\.now\(\), \(\) => void load\(\)/);
  assert.doesNotMatch(hook, /setInterval/);
});

test('read interactions and Snooze run once and then reload the authoritative center', async () => {
  const calls: string[] = [];
  const outcome = await runTrackingMutation(
    async () => void calls.push('markRead'),
    async () => void calls.push('reload'),
  );
  assert.deepEqual(calls, ['markRead', 'reload']);
  assert.equal(outcome.status, 'ok');
  let sends = 0;
  const refreshFailed = await runTrackingMutation(
    async () => void (sends += 1),
    async () => {
      throw new Error('center unavailable');
    },
  );
  assert.equal(sends, 1, 'a committed change is never re-sent');
  assert.equal(refreshFailed.status, 'refreshFailed');
  const page = source('pages/Notifications.tsx');
  assert.match(page, /center\.run\(\(\) => api\.markRead\(item\.notificationId\)\)/);
  assert.match(page, /center\.run\(\(\) => api\.snooze\(issueId, until\)\)/);
  assert.match(
    page,
    /if \(item\.readAt !== null\) return;/,
    'an already-read row sends no second mutation',
  );
  assert.match(page, /const toggleNotification = \(item:[\s\S]*void markRead\(item\)/);
  assert.match(page, /const openItem = \(item:[\s\S]*void markRead\(item\)/);
  assert.doesNotMatch(
    source('components/notifications/NotificationRow.tsx'),
    /Mark Read/,
    'read state has no standalone action',
  );
  const hook = source('notifications/useNotificationCenter.ts');
  assert.match(hook, /runTrackingMutation\(mutate, reloadOrThrow\)/);
  assert.match(hook, /Your change was saved, but the list could not be refreshed/);
});

test('Open Item uses the real WorkItem ID through the existing AppShell navigation', () => {
  const page = source('pages/Notifications.tsx');
  assert.match(
    page,
    /onOpenWorkItem\?\.\(item\.workItemId, item\.workItemTitle \?\? 'Work Item'\)/,
  );
  const shell = source('components/AppShell.tsx');
  assert.match(shell, /<Notifications[\s\S]*onOpenWorkItem=\{onOpenWorkItem\}/);
  assert.match(shell, /const openWorkItem = \(id: string, title: string\)/);
  assert.doesNotMatch(page, /title.*find|find\([^)]*title/i);
});

test('production Notifications has no prototype data, fallback, dismissed state or Wails-model leakage', () => {
  assert.equal(existsSync(new URL('../src/pages/notificationsData.ts', import.meta.url)), false);
  for (const file of [
    'pages/Notifications.tsx',
    'components/notifications/NotificationRow.tsx',
    'notifications/notificationCenterModel.ts',
    'notifications/useNotificationCenter.ts',
    'api/notifications/notificationsProductApi.ts',
  ]) {
    const text = source(file);
    assert.doesNotMatch(
      text,
      /initialNotifications|MOCK_TODAY_KEY|notificationsData|Apple Reminders|Production Deployment/,
      file,
    );
    assert.doesNotMatch(text, /dismissed|Dismiss|Force Resolve|Resolve\b.*button/i, file);
  }
  for (const file of [
    'pages/Notifications.tsx',
    'components/notifications/NotificationRow.tsx',
    'notifications/useNotificationCenter.ts',
  ]) {
    assert.doesNotMatch(source(file), /wailsjs/, file);
  }
  const page = source('pages/Notifications.tsx');
  assert.doesNotMatch(page, /catch[^}]*initial|fallback|demo/i);
});

test('honest loading / valid-empty / error states exist', () => {
  const page = source('pages/Notifications.tsx');
  assert.match(page, /Loading notifications…/);
  assert.match(page, /No notifications yet\./);
  assert.match(page, /No notifications match the current filter\./);
  assert.match(page, /Notifications could not be loaded:/);
  assert.match(page, /All[\s\S]*Needs Action|FILTER_LABELS/);
});

test('layout markers of the existing design are preserved', () => {
  const page = source('pages/Notifications.tsx');
  for (const marker of [
    'Filter notifications',
    'Search notifications',
    'Stay informed',
    'Review alerts and items',
    'notification-section-',
  ]) {
    assert.ok(page.includes(marker), marker);
  }
  const row = source('components/notifications/NotificationRow.tsx');
  for (const marker of ['Open Item', 'Snooze', 'Why you were notified', 'Notification History']) {
    assert.ok(row.includes(marker), marker);
  }
  assert.ok(!row.includes('Mark Read'));
});

test('opening the Notifications page does not mass-mark rows read', () => {
  const page = source('pages/Notifications.tsx');
  const shell = source('components/AppShell.tsx');
  assert.match(page, /void markRead\(item\)/, 'row interaction marks that row read');
  assert.doesNotMatch(page, /items\.forEach\([^)]*markRead|Promise\.all\([^)]*markRead/);
  assert.match(shell, /unreadCount=\{unreadCount\}/, 'header badge uses unread count');
});

test('severity is a visual mapping only; history and time labels come from real timestamps', () => {
  assert.equal(severityStyleKey('WARNING'), 'attention');
  assert.equal(severityStyleKey('CRITICAL'), 'high');
  assert.equal(severityStyleKey('INFO'), 'info');
  const n = item({
    readAt: '2026-10-16T00:30:00Z',
    snoozedUntil: '2026-10-16T02:00:00Z',
    disposition: 'SNOOZED',
  });
  const entries = buildHistoryEntries(n);
  assert.deepEqual(
    entries.map((e) => e.id),
    ['snooze', 'read', 'sent'],
  );
  assert.equal(entries[0].description, 'Snoozed until');
  const now = new Date('2026-10-16T00:12:00Z');
  assert.equal(formatRelativeTime('2026-10-16T00:00:00Z', now), '12 min ago');
  assert.equal(formatRelativeTime('2026-10-16T00:11:40Z', now), 'Just now');
  const groups = splitByDay([item()], new Date(2026, 9, 16, 12, 0, 0));
  assert.equal(groups.today.length + groups.earlier.length, 1);
});

test('the Assistant frontend has no Notification access', () => {
  for (const file of [
    'api/assistant/assistantApi.ts',
    'api/assistant/wailsAssistantApi.ts',
    'assistant/assistantConversation.ts',
    'components/AssistantPanel.tsx',
  ]) {
    assert.doesNotMatch(source(file), /notification|Notification/, file);
  }
});
