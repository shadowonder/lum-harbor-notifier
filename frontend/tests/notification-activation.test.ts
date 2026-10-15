import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import test from 'node:test';
import {
  mapNotificationActivation,
  NOTIFICATION_ACTIVATION_EVENT,
} from '../src/notifications/notificationActivation.ts';

const source = (path: string) => readFileSync(new URL(`../src/${path}`, import.meta.url), 'utf8');

test('native activation maps only existing notification and navigation identity', () => {
  assert.deepEqual(
    mapNotificationActivation({
      notificationId: 'notification-1',
      plannerItemId: 'todo-1',
      workItemId: null,
      unrelated: 'ignored',
    }),
    {
      notificationId: 'notification-1',
      plannerItemId: 'todo-1',
      workItemId: null,
    },
  );
  assert.equal(mapNotificationActivation({ plannerItemId: 'todo-1' }), null);
  assert.equal(NOTIFICATION_ACTIVATION_EVENT, 'lumharbor:notification-activate');
});

test('native activation reuses AppShell navigation and only marks the clicked notification read', () => {
  const shell = source('components/AppShell.tsx');
  const handler = shell.slice(
    shell.indexOf('const handleNativeNotificationActivation'),
    shell.indexOf('useEffect(() => {', shell.indexOf('const handleNativeNotificationActivation')),
  );
  assert.match(handler, /createWorkItemNavigationIntent\(activation\.workItemId\)/);
  assert.match(handler, /selectedPlannerItemId: activation\.plannerItemId/);
  assert.match(handler, /markNotificationReadForInteraction\(activation\.notificationId\)/);
  assert.match(shell, /ConsumeNotificationActivations/);
  assert.doesNotMatch(handler, /notification\.deliver|AttentionIssue|Proposal|Source/);
});

test('toast navigation uses the same interaction read path while previews stay presentation-only', () => {
  const shell = source('components/AppShell.tsx');
  const toast = source('components/notifications/NotificationToast.tsx');
  assert.match(toast, /onInteract\?\.\(item\.notificationId\)/);
  assert.match(shell, /notificationId\.startsWith\('preview-'\)/);
  assert.match(shell, /notificationsApi\.markRead\(notificationId\)/);
});
