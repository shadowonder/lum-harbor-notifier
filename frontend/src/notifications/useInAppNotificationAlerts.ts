import { useCallback, useEffect, useRef, useState } from 'react';
import type {
  CenterNotification,
  ProductNotificationsApi,
} from '../api/notifications/notificationsProductApi.ts';

export interface UseInAppNotificationAlertsOptions {
  pollIntervalMs?: number;
  enabled?: boolean;
}

export interface UseInAppNotificationAlertsResult {
  alerts: CenterNotification[];
  unreadCount: number;
  dismissAlert: (notificationId: string) => void;
  presentTestNotification: () => void;
  refreshNotifications: () => Promise<void>;
}

export function createTestNotification(
  notificationId: string,
  now = new Date(),
): CenterNotification {
  const sentAt = now.toISOString();
  return {
    notificationId,
    attentionIssueId: `preview-attention-${notificationId}`,
    workItemId: null,
    workItemTitle: null,
    subjectTrackedItemId: null,
    plannerItemId: null,
    title: 'Test notification',
    message:
      'This is synthetic preview content. It only demonstrates Lum Harbor’s in-app notification presentation.',
    severity: 'WARNING',
    recommendedAction: null,
    sentAt,
    readAt: null,
    attentionState: 'OPEN',
    snoozedUntil: null,
    resolvedAt: null,
    disposition: 'NEEDS_ACTION',
    reason: {
      monitorTitle: 'Presentation preview',
      monitorKind: 'PREVIEW',
      dueAt: sentAt,
      timezone: 'UTC',
      subjectKind: 'PREVIEW',
      subjectTitle: 'Synthetic notification',
      expectedKind: 'PREVIEW',
      expectedWorkItemLifecycle: null,
      expectedStepTitle: null,
      actualWorkItemLifecycle: null,
      actualStepState: null,
    },
  };
}

export function mergeAlertStack(
  current: readonly CenterNotification[],
  incoming: readonly CenterNotification[],
): CenterNotification[] {
  const visibleIds = new Set(current.map((item) => item.notificationId));
  return [...incoming.filter((item) => !visibleIds.has(item.notificationId)), ...current];
}

/**
 * useInAppNotificationAlerts provides vanilla in-app notification awareness.
 *
 * Core Invariants:
 * 1. Driven strictly by authoritative Core Notification truth (ProductNotificationsApi.getCenter).
 * 2. Does NOT require any native OS notification plugin.
 * 3. On initial mount/restart, all pre-existing historical notifications are seeded as seen
 *    to prevent burst notification spam upon startup.
 * 4. Subsequent background polls detect unread, newly arrived notifications and surface them
 *    as an in-app alert stack.
 * 5. Dismissing an alert does NOT delete or alter the durable notification in SQLite.
 * 6. Repeated scheduler ticks will not duplicate alerts for already-seen notifications.
 * 7. Test notifications enter this same presentation collection without calling the backend.
 */
export function useInAppNotificationAlerts(
  api: ProductNotificationsApi,
  options: UseInAppNotificationAlertsOptions = {},
): UseInAppNotificationAlertsResult {
  const { pollIntervalMs = 4000, enabled = true } = options;
  const [alerts, setAlerts] = useState<CenterNotification[]>([]);
  const [unreadCount, setUnreadCount] = useState<number>(0);

  const seenNotificationIds = useRef<Set<string>>(new Set());
  const initialLoaded = useRef<boolean>(false);
  const previewSequence = useRef(0);

  const poll = useCallback(async () => {
    try {
      const center = await api.getCenter();
      const items = center.items ?? [];

      // Calculate unread count strictly based on readAt (independent of Attention disposition)
      const unread = items.filter((i) => i.readAt === null);
      setUnreadCount(unread.length);

      if (!initialLoaded.current) {
        // Cold start / first load: seed all existing items as seen to prevent historical spam
        for (const item of items) {
          seenNotificationIds.current.add(item.notificationId);
        }
        initialLoaded.current = true;
        return;
      }

      // Subsequent polls: check for genuinely new notifications
      const brandNew = items.filter(
        (item) =>
          !seenNotificationIds.current.has(item.notificationId) &&
          item.readAt === null &&
          item.disposition !== 'RESOLVED',
      );

      if (brandNew.length > 0) {
        // Mark as seen so they are never alerted a second time
        for (const item of brandNew) {
          seenNotificationIds.current.add(item.notificationId);
          void api.acknowledgePresented?.(item.notificationId);
        }
        // Keep every newly observed item in one presentation stack. The API is
        // newest-first, so prepend this batch while retaining visible alerts.
        setAlerts((current) => mergeAlertStack(current, brandNew));
      }
    } catch (err) {
      // Quiet fail on network/backend transient poll error
      console.warn('Notification poll error:', err);
    }
  }, [api]);

  useEffect(() => {
    if (!enabled) return;

    // Initial immediate check
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial immediate poll for notifications
    void poll();

    const timer = setInterval(() => {
      void poll();
    }, pollIntervalMs);

    return () => {
      clearInterval(timer);
    };
  }, [enabled, poll, pollIntervalMs]);

  const dismissAlert = useCallback((notificationId: string) => {
    setAlerts((current) => current.filter((item) => item.notificationId !== notificationId));
  }, []);

  const presentTestNotification = useCallback(() => {
    previewSequence.current += 1;
    const preview = createTestNotification(`preview-${Date.now()}-${previewSequence.current}`);
    setAlerts((current) => [preview, ...current]);
  }, []);

  return {
    alerts,
    unreadCount,
    dismissAlert,
    presentTestNotification,
    refreshNotifications: poll,
  };
}
