import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import type {
  CenterNotification,
  NotificationCenter,
} from '../src/api/notifications/notificationsProductApi.ts';
import {
  createTestNotification,
  mergeAlertStack,
} from '../src/notifications/useInAppNotificationAlerts.ts';
import {
  createNotificationLifetime,
  notificationLifetimeElapsedMs,
  notificationLifetimeRemainingMs,
  resolveNotificationDeck,
} from '../src/components/notifications/notificationDeck.ts';

function createMockNotification(
  notificationId: string,
  overrides: Partial<CenterNotification> = {},
): CenterNotification {
  return {
    notificationId,
    attentionIssueId: `attn-${notificationId}`,
    workItemId: null,
    workItemTitle: null,
    plannerItemId: null,
    subjectTrackedItemId: null,
    title: `Notification ${notificationId}`,
    message: `Message for ${notificationId}`,
    severity: 'INFO',
    recommendedAction: 'Complete the task',
    sentAt: new Date().toISOString(),
    readAt: null,
    attentionState: 'OPEN',
    snoozedUntil: null,
    resolvedAt: null,
    disposition: 'NEEDS_ACTION',
    reason: {
      monitorTitle: 'Monitor',
      monitorKind: 'TODO_REMINDER',
      dueAt: new Date().toISOString(),
      timezone: 'UTC',
      subjectKind: 'PLANNER_ITEM',
      subjectTitle: 'Test Todo',
      expectedKind: 'PLANNER_STATUS',
      expectedWorkItemLifecycle: null,
      expectedStepTitle: null,
      actualWorkItemLifecycle: null,
      actualStepState: null,
    },
    ...overrides,
  };
}

test('in-app alert logic: cold launch seeds existing items to prevent alert spam', () => {
  const existing1 = createMockNotification('n-old-1');
  const existing2 = createMockNotification('n-old-2');

  const seenIds = new Set<string>();
  let initialLoaded = false;
  let currentAlert: CenterNotification | null = null;

  const simulatePoll = (center: NotificationCenter) => {
    const items = center.items ?? [];

    if (!initialLoaded) {
      for (const item of items) {
        seenIds.add(item.notificationId);
      }
      initialLoaded = true;
      return;
    }

    const brandNew = items.filter(
      (item) =>
        !seenIds.has(item.notificationId) &&
        item.readAt === null &&
        item.disposition !== 'RESOLVED',
    );

    if (brandNew.length > 0) {
      for (const item of brandNew) {
        seenIds.add(item.notificationId);
      }
      currentAlert = brandNew[0];
    }
  };

  // Initial poll with existing items on cold launch
  simulatePoll({ at: new Date().toISOString(), items: [existing1, existing2] });
  assert.equal(
    currentAlert,
    null,
    'cold launch must not pop up toast for historical notifications',
  );
  assert.equal(seenIds.has('n-old-1'), true);
  assert.equal(seenIds.has('n-old-2'), true);

  // Subsequent poll receives a newly created notification
  const newNotif = createMockNotification('n-new-1', {
    plannerItemId: 'todo-123',
    title: 'Reminder: Due in 15 minutes',
  });
  simulatePoll({ at: new Date().toISOString(), items: [newNotif, existing1, existing2] });

  assert.notEqual(currentAlert, null, 'genuinely new notification must trigger alert toast');
  assert.equal(currentAlert?.notificationId, 'n-new-1');
  assert.equal(currentAlert?.plannerItemId, 'todo-123');

  // Dismissing the toast
  currentAlert = null;
  assert.equal(currentAlert, null);

  // Next poll with same items does not re-alert (duplicate suppression)
  simulatePoll({ at: new Date().toISOString(), items: [newNotif, existing1, existing2] });
  assert.equal(currentAlert, null, 'already seen notification must not re-trigger toast');
});

test('notification read semantics: attention resolution does not clear unread count', () => {
  // Verifies the strict invariant: Reminder != Attention != Notification
  // Completing a Todo sets AttentionState to RESOLVED, but historical Notification remains unread (readAt === null)
  const item1 = createMockNotification('n-1', { readAt: null, disposition: 'NEEDS_ACTION' });
  const item2 = createMockNotification('n-2', {
    readAt: null,
    attentionState: 'RESOLVED',
    disposition: 'RESOLVED',
  });
  const item3 = createMockNotification('n-3', {
    readAt: new Date().toISOString(),
    disposition: 'RESOLVED',
  });

  const items = [item1, item2, item3];

  // Unread count strictly tracks readAt === null, independent of disposition
  const unreadCount = items.filter((i) => i.readAt === null).length;
  assert.equal(
    unreadCount,
    2,
    'resolved attention does not mark notification as read; unreadCount remains 2',
  );

  // After user explicitly marks item2 as read:
  item2.readAt = new Date().toISOString();
  const nextUnreadCount = items.filter((i) => i.readAt === null).length;
  assert.equal(nextUnreadCount, 1, 'marking notification read decrements unreadCount to 1');
});

test('toast action label determination matches item context', () => {
  const todoItem = createMockNotification('n-todo', { plannerItemId: 'todo-99' });
  const workItem = createMockNotification('n-wi', { workItemId: 'wi-99' });
  const generalItem = createMockNotification('n-gen');

  const getActionLabel = (item: CenterNotification) =>
    item.plannerItemId ? 'Open Todo' : item.workItemId ? 'Open Work Item' : 'View Notifications';

  assert.equal(getActionLabel(todoItem), 'Open Todo');
  assert.equal(getActionLabel(workItem), 'Open Work Item');
  assert.equal(getActionLabel(generalItem), 'View Notifications');
});

test('synthetic test notification is presentation-only content with no product target', () => {
  const preview = createTestNotification('preview-1', new Date('2026-10-02T12:00:00Z'));
  assert.equal(preview.notificationId, 'preview-1');
  assert.equal(preview.title, 'Test notification');
  assert.match(preview.message, /synthetic preview content/i);
  assert.equal(preview.plannerItemId, null);
  assert.equal(preview.workItemId, null);
  assert.equal(preview.reason.monitorKind, 'PREVIEW');
});

test('new in-app notifications stack without replacing visible alerts or duplicating IDs', () => {
  const existing = createMockNotification('existing');
  const newest = createMockNotification('newest');
  const next = createMockNotification('next');
  const stack = mergeAlertStack([existing], [newest, next, existing]);
  assert.deepEqual(
    stack.map((item) => item.notificationId),
    ['newest', 'next', 'existing'],
  );
});

test('one notification and two-to-three notifications use the normal readable stack', () => {
  assert.deepEqual(resolveNotificationDeck(1, false).modes, ['full']);
  assert.deepEqual(resolveNotificationDeck(2, false).modes, ['full', 'full']);
  assert.deepEqual(resolveNotificationDeck(3, false).modes, ['full', 'full', 'full']);
  assert.equal(resolveNotificationDeck(3, true).compact, false);
});

test('more than three notifications collapse to a primary card and two real summaries', () => {
  const deck = resolveNotificationDeck(6, false);
  assert.equal(deck.compact, true);
  assert.equal(deck.expanded, false);
  assert.equal(deck.additionalCount, 5);
  assert.deepEqual(deck.modes, [
    'primary',
    'compressed',
    'compressed',
    'hidden',
    'hidden',
    'hidden',
  ]);
});

test('expanding and collapsing the compact deck changes only presentation modes', () => {
  const collapsed = resolveNotificationDeck(5, false);
  const expanded = resolveNotificationDeck(5, true);
  assert.equal(collapsed.expanded, false);
  assert.equal(expanded.expanded, true);
  assert.deepEqual(expanded.modes, ['full', 'full', 'full', 'full', 'full']);
  assert.deepEqual(resolveNotificationDeck(5, false), collapsed);
});

test('dismiss from expanded state leaves every remaining notification readable', () => {
  const ids = ['newest', 'second', 'third', 'fourth', 'oldest'];
  const remaining = ids.filter((id) => id !== 'third');
  const deck = resolveNotificationDeck(remaining.length, true);
  assert.deepEqual(remaining, ['newest', 'second', 'fourth', 'oldest']);
  assert.deepEqual(deck.modes, ['full', 'full', 'full', 'full']);
});

test('a new notification becomes primary while an existing compact deck remains collapsed', () => {
  const existing = ['one', 'two', 'three', 'four'].map((id) => createMockNotification(id));
  const newest = createMockNotification('newest');
  const stack = mergeAlertStack(existing, [newest]);
  const deck = resolveNotificationDeck(stack.length, false);
  assert.equal(stack[0]?.notificationId, 'newest');
  assert.equal(deck.modes[0], 'primary');
  assert.deepEqual(deck.modes.slice(1), ['compressed', 'compressed', 'hidden', 'hidden']);
});

test('auto-dismiss lifetime continues while collapsed and does not reset on expand/collapse', () => {
  const lifetime = createNotificationLifetime(1_000, 10_000);
  assert.equal(resolveNotificationDeck(5, false).modes[4], 'hidden');
  assert.equal(notificationLifetimeElapsedMs(lifetime, 5_000), 4_000);
  assert.equal(notificationLifetimeRemainingMs(lifetime, 5_000), 6_000);

  assert.equal(resolveNotificationDeck(5, true).modes[4], 'full');
  assert.equal(notificationLifetimeElapsedMs(lifetime, 7_500), 6_500);
  assert.equal(notificationLifetimeRemainingMs(lifetime, 7_500), 3_500);

  assert.equal(resolveNotificationDeck(5, false).modes[4], 'hidden');
  assert.equal(notificationLifetimeRemainingMs(lifetime, 11_000), 0);
});

test('toast deck, motion, lifetime indicator, and shadow-safe scroll padding stay localized', () => {
  const toast = readFileSync(
    new URL('../src/components/notifications/NotificationToast.tsx', import.meta.url),
    'utf8',
  );
  assert.match(toast, /notification-enter/);
  assert.match(toast, /notification-exit/);
  assert.match(toast, /notification-lifetime/);
  assert.match(toast, /settings\.autoDismissSeconds \* 1000/);
  assert.match(toast, /animation: `notification-lifetime \$\{lifetime\.durationMs\}ms/);
  assert.match(toast, /notificationLifetimeRemainingMs\(lifetime, Date\.now\(\)\)/);
  assert.match(toast, /visibility: 'hidden'/);
  assert.match(toast, /opacity: compressed \|\| hidden \? 0 : 1/);
  assert.match(toast, /aria-hidden=\{compressed \|\| hidden/);
  assert.match(toast, /aria-expanded=\{deckControl\.expanded\}/);
  assert.match(toast, /key=\{`\$\{item\.notificationId\}:\$\{autoDismissMs/);
  assert.doesNotMatch(toast, /key=\{[^\n]*mode/);
  assert.match(toast, /onClose\(item\.notificationId\)/);
  assert.match(toast, /onOpenTodo\(item\.plannerItemId\)/);
  assert.match(toast, /onOpenNotifications\(\)/);
  assert.match(toast, /overflowY: 'auto'/);
  assert.match(toast, /p: 2/);
  assert.doesNotMatch(toast, /framer-motion|react-spring|::before|::after/);
});

test('Settings offers the complete bounded auto-dismiss duration list', () => {
  const settings = readFileSync(new URL('../src/pages/Settings.tsx', import.meta.url), 'utf8');
  for (const choice of [
    "{ value: 10, label: '10 seconds' }",
    "{ value: 30, label: '30 seconds' }",
    "{ value: 60, label: '1 minute' }",
    "{ value: 300, label: '5 minutes' }",
    "{ value: 600, label: '10 minutes' }",
    "{ value: 900, label: '15 minutes' }",
    "{ value: 1800, label: '30 minutes' }",
  ]) {
    assert.match(settings, new RegExp(choice.replace(/[{}]/g, '\\$&')));
  }
});
